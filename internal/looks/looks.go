// Package looks stores lighting looks: a named snapshot of the keyboard look (effect, colour,
// speed, brightness, or per-key colours) and the rear light bar.
//
// Looks are saved per user in ~/.config/truesense/looks/NAME.json. One look can also be chosen
// for BOOT: the daemon (root) keeps a self-contained copy in /var/lib/truesense/boot-look.json
// and applies it as soon as it starts and again after every resume from suspend, before anyone
// logs in. The JSON of both files is what 0.3.x wrote.
package looks

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"

	"github.com/Order52/truesense/internal/firmware"
	"github.com/Order52/truesense/internal/fsx"
	"github.com/Order52/truesense/internal/hw"
	"github.com/Order52/truesense/internal/keyboard"
	"github.com/Order52/truesense/internal/userstate"
)

// DefaultBootFile is where the daemon keeps the boot look (TRUESENSE_BOOT_LOOK overrides it).
const DefaultBootFile = "/var/lib/truesense/boot-look.json"

// Look is the keyboard and the rear light bar. A nil part means "leave it unchanged".
type Look struct {
	Keyboard *keyboard.State `json:"keyboard"`
	Rear     *Rear           `json:"rear"`
}

// Rear is the light bar as hw remembers it ("rear_state" in the user state). The numbers are
// pointers because a look written by hand may lack them, which Apply reports instead of
// sending zeros (a brightness of 0 would switch the bar off).
type Rear struct {
	Mode       string   `json:"mode"`
	Color      string   `json:"color"`
	Speed      *int     `json:"speed"`
	Brightness *int     `json:"brightness"`
	Direction  *int     `json:"direction,omitempty"` // absent means 1
	Zones      []string `json:"zones"`               // nil (null): one colour for the whole bar
}

// isZero: a part without anything in it is as good as absent (an empty dict was falsy in Python).
func (r *Rear) isZero() bool {
	return r == nil || (r.Mode == "" && r.Color == "" && r.Speed == nil && r.Brightness == nil && r.Direction == nil && r.Zones == nil)
}

// IsEmpty reports a look that changes nothing.
func (l *Look) IsEmpty() bool { return l == nil || (kbZero(l.Keyboard) && l.Rear.isZero()) }

func kbZero(k *keyboard.State) bool { return k == nil || k.IsZero() }

func ptr(n int) *int { return &n }

// builtin looks cannot be saved over or deleted. "native" is what PredatorSense leaves on this
// laptop on Windows (from the captures): keyboard cells #00aec7 at brightness 12, rear bar
// Breathing #3723c7 speed 3 brightness 100.
func builtin() map[string]Look {
	cells := make([]string, 0, 128)
	for i := 0; i < 126; i++ {
		cells = append(cells, "00aec7")
	}
	cells = append(cells, "000000", "000000")
	return map[string]Look{"native": {
		Keyboard: &keyboard.State{Mode: keyboard.ModePerKey, Brightness: ptr(12), Cells: cells},
		Rear:     &Rear{Mode: "breathing", Color: "3723c7", Speed: ptr(3), Brightness: ptr(100), Direction: ptr(1)},
	}}
}

// BuiltinNames lists the built-in looks.
func BuiltinNames() []string { return []string{"native"} }

func isBuiltin(name string) bool { _, ok := builtin()[name]; return ok }

// KeyboardControl is what applying a look needs from the keyboard controller.
type KeyboardControl interface {
	Last() keyboard.State
	Replay(keyboard.State) (string, error)
	Off() error
}

// RearControl is what applying a look needs from the firmware's rear light.
type RearControl interface {
	RearSet(hw.RearOpts) (hw.Rear, error)
}

// Store is a directory of looks plus the boot look, and the devices a look is applied to.
type Store struct {
	Dir      string
	BootFile string
	State    *userstate.Store // where hw remembers the rear light; may be nil
	KB       KeyboardControl
	Rear     RearControl
}

// DefaultDir is ~/.config/truesense/looks.
func DefaultDir() string {
	home, err := os.UserHomeDir()
	if err != nil {
		return filepath.Join(os.TempDir(), "truesense-looks")
	}
	return filepath.Join(home, ".config", "truesense", "looks")
}

// DefaultBoot returns the boot look path: TRUESENSE_BOOT_LOOK or DefaultBootFile.
func DefaultBoot() string {
	if p := os.Getenv("TRUESENSE_BOOT_LOOK"); p != "" {
		return p
	}
	return DefaultBootFile
}

// New returns a Store over the default directories.
func New(state *userstate.Store, kb KeyboardControl, rear RearControl) *Store {
	return &Store{Dir: DefaultDir(), BootFile: DefaultBoot(), State: state, KB: kb, Rear: rear}
}

var nameRE = regexp.MustCompile(`^[A-Za-z0-9._-]{1,40}$`)

func (s *Store) path(name string) (string, error) {
	if !nameRE.MatchString(name) {
		return "", firmware.Errorf("look names use letters, digits, . _ - (max 40)")
	}
	return filepath.Join(s.Dir, name+".json"), nil
}

// Names lists the built-in looks first, then yours sorted.
func (s *Store) Names() []string {
	names := BuiltinNames()
	entries, err := os.ReadDir(s.Dir)
	if err != nil {
		return names
	}
	var mine []string
	for _, e := range entries {
		if n, ok := strings.CutSuffix(e.Name(), ".json"); ok && !isBuiltin(n) {
			mine = append(mine, n)
		}
	}
	sort.Strings(mine)
	return append(names, mine...)
}

// Capture returns what is on the lights now, as remembered by this tool (the hardware cannot be
// asked which effect is running).
func (s *Store) Capture() (*Look, error) {
	var kb *keyboard.State
	if last := s.KB.Last(); !last.IsZero() {
		kb = &last
	}
	var rear *Rear
	if s.State != nil {
		if m, ok := s.State.Get("rear_state").(map[string]any); ok && len(m) > 0 {
			var r Rear
			if err := s.State.GetInto("rear_state", &r); err == nil {
				rear = &r
			}
		}
	}
	if kb == nil && rear == nil {
		return nil, firmware.Errorf("nothing to save yet: set the keyboard or rear light first")
	}
	return &Look{Keyboard: kb, Rear: rear}, nil
}

// Save stores a look under name; nil look saves the current lights (Capture). It returns what was saved.
func (s *Store) Save(name string, look *Look) (*Look, error) {
	if isBuiltin(name) {
		return nil, firmware.Errorf("'%s' is built in and read-only; save your look under another name", name)
	}
	if look == nil {
		var err error
		if look, err = s.Capture(); err != nil {
			return nil, err
		}
	}
	p, err := s.path(name)
	if err != nil {
		return nil, err
	}
	b, err := json.MarshalIndent(look, "", " ")
	if err != nil {
		return nil, err
	}
	if err := fsx.WriteAtomic(p, b, 0o644); err != nil {
		return nil, fmt.Errorf("save look %q: %w", name, err)
	}
	return look, nil
}

// Parse decodes a look file or a look received over the control socket.
func Parse(data []byte) (*Look, error) {
	var l Look
	if err := json.Unmarshal(data, &l); err != nil {
		return nil, fmt.Errorf("parse look: %w", err)
	}
	return &l, nil
}

// Load returns a saved or built-in look.
func (s *Store) Load(name string) (*Look, error) {
	if b, ok := builtin()[name]; ok {
		return &b, nil
	}
	p, err := s.path(name)
	if err != nil {
		return nil, err
	}
	data, err := os.ReadFile(p)
	if errors.Is(err, fs.ErrNotExist) {
		return nil, firmware.Errorf("no look '%s' (truesense look list)", name)
	}
	if err != nil {
		return nil, fmt.Errorf("read look %q: %w", name, err)
	}
	l, err := Parse(data)
	if err != nil {
		return nil, fmt.Errorf("look %q: %w", name, err)
	}
	return l, nil
}

// Delete removes a saved look.
func (s *Store) Delete(name string) error {
	if isBuiltin(name) {
		return firmware.Errorf("'%s' is built in and cannot be deleted", name)
	}
	p, err := s.path(name)
	if err != nil {
		return err
	}
	if err := os.Remove(p); err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return firmware.Errorf("no look '%s'", name)
		}
		return fmt.Errorf("delete look %q: %w", name, err)
	}
	return nil
}

// Copy saves a look under a new name (default NAME-copy, numbered until free) and returns that name.
func (s *Store) Copy(name, newName string) (string, error) {
	look, err := s.Load(name)
	if err != nil {
		return "", err
	}
	base := newName
	if base == "" {
		base = name + "-copy"
	}
	taken := map[string]bool{}
	for _, n := range s.Names() {
		taken[n] = true
	}
	target := base
	for n := 2; taken[target]; n++ {
		target = fmt.Sprintf("%s-%d", base, n)
	}
	if _, err := s.Save(target, look); err != nil {
		return "", err
	}
	return target, nil
}

// Describe summarises a look in one line, as `truesense look show` prints it.
func Describe(l *Look) string {
	kd, rd := "keyboard unchanged", "rear unchanged"
	if k := l.Keyboard; !kbZero(k) {
		name := k.Name
		if name == "" {
			name = k.Mode
		}
		kd = fmt.Sprintf("keyboard %s brightness %s", name, intOr(k.Brightness, "?"))
		if k.Speed != nil && *k.Speed != 0 {
			kd += fmt.Sprintf(" speed %d", *k.Speed)
		}
	}
	if r := l.Rear; !r.isZero() {
		mode := r.Mode
		if mode == "" {
			mode = "None"
		}
		rd = fmt.Sprintf("rear %s brightness %s", mode, intOr(r.Brightness, "None"))
	}
	return kd + "; " + rd
}

func intOr(p *int, def string) string {
	if p == nil {
		return def
	}
	return fmt.Sprint(*p)
}

func (r *Rear) opts() (hw.RearOpts, error) {
	if r.Mode == "" || r.Color == "" || r.Speed == nil || r.Brightness == nil {
		return hw.RearOpts{}, firmware.Errorf("the rear part of this look needs mode, color, speed and brightness")
	}
	o := hw.RearOpts{Mode: r.Mode, Color: r.Color, Speed: *r.Speed, Brightness: *r.Brightness, Direction: 1, Zones: r.Zones, Verify: true}
	if r.Direction != nil {
		o.Direction = *r.Direction
	}
	return o, nil
}

// Apply puts a look on the lights and returns a list of what was done.
func (s *Store) Apply(look *Look) ([]string, error) {
	var done []string
	if k := look.Keyboard; !kbZero(k) {
		r, err := s.KB.Replay(*k)
		if err != nil {
			return done, err
		}
		if r == "" && k.Mode == keyboard.ModeOff {
			if err := s.KB.Off(); err != nil {
				return done, err
			}
			r = "off"
		}
		if r == "" {
			r = "nothing to restore"
		}
		done = append(done, "keyboard: "+r)
	}
	if r := look.Rear; !r.isZero() {
		o, err := r.opts()
		if err != nil {
			return done, err
		}
		if _, err := s.Rear.RearSet(o); err != nil {
			return done, err
		}
		done = append(done, "rear: "+r.Mode)
	}
	return done, nil
}

// ReadBoot returns the boot look, or nil when none is set or the file is unreadable.
func (s *Store) ReadBoot() *Look {
	data, err := os.ReadFile(s.BootFile)
	if err != nil {
		return nil
	}
	l, err := Parse(data)
	if err != nil {
		return nil
	}
	return l
}

// WriteBoot stores the boot look; nil removes it. The daemon calls this as root, and the file
// is self-contained so nothing in a user's home is needed before login.
func (s *Store) WriteBoot(look *Look) error {
	if look == nil {
		if err := os.Remove(s.BootFile); err != nil && !errors.Is(err, fs.ErrNotExist) {
			return fmt.Errorf("remove boot look: %w", err)
		}
		return nil
	}
	b, err := json.Marshal(look)
	if err != nil {
		return err
	}
	if err := fsx.WriteAtomic(s.BootFile, b, 0o644); err != nil {
		return fmt.Errorf("write boot look: %w", err)
	}
	return nil
}
