// Package keybinds keeps the PREDATOR key binding profiles: a named set of what each gesture
// does (tap, double tap, triple tap, long press, Fn+Esc) and the press timing.
//
// Built-in profiles are read-only (copy one to make your own); yours live in
// ~/.config/truesense/keys/NAME.json, the files 0.3.x wrote. Using a profile writes its actions
// into the user's configuration ([keys]) and records its name; the key service notices the
// change and reloads by itself. Every action is validated when a profile is loaded or saved,
// never at key-press time.
package keybinds

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"sort"
	"strings"

	"github.com/Order52/truesense/internal/actions"
	"github.com/Order52/truesense/internal/config"
	"github.com/Order52/truesense/internal/fsx"
)

// Gestures are the bindable gestures, in the order they are shown.
func Gestures() []string { return []string{"tap", "double", "triple", "long", "fn_esc"} }

// Label is the name of a gesture in menus.
func Label(gesture string) string {
	switch gesture {
	case "tap":
		return "Tap"
	case "double":
		return "Double tap"
	case "triple":
		return "Triple tap"
	case "long":
		return "Long press"
	case "fn_esc":
		return "Fn + Esc"
	}
	return gesture
}

// Allowed press timings in milliseconds: how long a press must be held to be a long press and
// how long to wait for another tap.
const (
	LongMSMin  = 300
	LongMSMax  = 2000
	MultiMSMin = 200
	MultiMSMax = 1000
)

// Bindings are the gestures' actions and the timing: the part of a profile the key service
// uses. The JSON names are the 0.3.x ones.
type Bindings struct {
	Tap     string `json:"tap"`
	Double  string `json:"double"`
	Triple  string `json:"triple"`
	Long    string `json:"long"`
	FnEsc   string `json:"fn_esc"`
	LongMS  int    `json:"long_ms"`
	MultiMS int    `json:"multi_ms"`
}

// Action returns the action bound to a gesture ("" for an unknown gesture).
func (b *Bindings) Action(gesture string) string {
	if p := b.slot(gesture); p != nil {
		return *p
	}
	return ""
}

// SetAction binds an action to a gesture and reports whether the gesture exists.
func (b *Bindings) SetAction(gesture, spec string) bool {
	p := b.slot(gesture)
	if p == nil {
		return false
	}
	*p = spec
	return true
}

func (b *Bindings) slot(gesture string) *string {
	switch gesture {
	case "tap":
		return &b.Tap
	case "double":
		return &b.Double
	case "triple":
		return &b.Triple
	case "long":
		return &b.Long
	case "fn_esc":
		return &b.FnEsc
	}
	return nil
}

// Profile is a named set of bindings with a description.
type Profile struct {
	Description string `json:"description"`
	Bindings
}

// Default is the standard behaviour; the other built-in profiles change a few gestures of it.
func Default() Bindings {
	return Bindings{Tap: "power-next", Double: "output-toggle", Triple: "none", Long: "fan-max-toggle", FnEsc: "none", LongMS: 800, MultiMS: 450}
}

// BuiltinNames lists the built-in profiles, in the order they are shown.
func BuiltinNames() []string { return []string{"native", "default", "gaming", "work", "lights"} }

// Builtin returns a built-in profile.
func Builtin(name string) (Profile, bool) {
	p := Profile{Bindings: Default()}
	switch name {
	case "native":
		p.Description = "PredatorSense default: the PREDATOR key opens the app, nothing else is bound"
		p.Tap, p.Double, p.Long = "exec:truesense gui", "none", "none"
	case "default":
		p.Description = "The standard behaviour: tap steps the power mode, double tap switches speakers/headphones, long press toggles Max fans"
	case "gaming":
		p.Description = "Gaming: tap toggles Performance and Turbo, double tap Max fans, triple tap the next lighting effect, long press Smart auto, Fn+Esc mutes the microphone"
		p.Tap, p.Double, p.Triple, p.Long, p.FnEsc = "power-cycle:performance,turbo", "fan-max-toggle", "keyboard-effect-next", "auto-next", "mic-toggle"
	case "work":
		p.Description = "Work: tap steps Eco, Quiet and Balanced, double tap switches audio, triple tap mutes the microphone, long press toggles the touchpad"
		p.Tap, p.Double, p.Triple, p.Long = "power-cycle:eco,quiet,balanced", "output-toggle", "mic-toggle", "touchpad-toggle"
	case "lights":
		p.Description = "Lighting: tap the next keyboard effect, double tap the next rear effect, triple tap the keyboard light, long press the rear light"
		p.Tap, p.Double, p.Triple, p.Long = "keyboard-effect-next", "rear-effect-next", "keyboard:toggle", "rear:toggle"
	default:
		return Profile{}, false
	}
	return p, true
}

// Store is a directory of saved profiles plus the configuration file they are used through.
type Store struct {
	// Dir holds NAME.json files (default ~/.config/truesense/keys).
	Dir string
	// Check validates one action; nil uses actions.Validate. The application passes the
	// validator that knows the saved looks.
	Check func(spec string) error
	// ConfigPath is the user's config file that Use writes to (default config.UserPath()).
	ConfigPath string
	// LoadConfig reads the effective configuration, for Current (default config.LoadDefault).
	LoadConfig func() (*config.Config, error)
}

// DefaultDir is ~/.config/truesense/keys.
func DefaultDir() string {
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".config", "truesense", "keys")
}

// New returns a Store over the default places.
func New() *Store { return &Store{} }

func (s *Store) dir() string {
	if s.Dir != "" {
		return s.Dir
	}
	return DefaultDir()
}

func (s *Store) configPath() string {
	if s.ConfigPath != "" {
		return s.ConfigPath
	}
	return config.UserPath()
}

func (s *Store) check(spec string) error {
	if s.Check != nil {
		return s.Check(spec)
	}
	return actions.Validate(spec)
}

var nameRE = regexp.MustCompile(`^[A-Za-z0-9._-]{1,40}$`)

// ValidName reports whether name can be a saved profile's name (letters, digits, . _ -, at
// most 40: never a path).
func ValidName(name string) bool { return nameRE.MatchString(name) }

func (s *Store) path(name string) (string, error) {
	if !ValidName(name) {
		return "", errors.New("profile names use letters, digits, . _ - (max 40)")
	}
	return filepath.Join(s.dir(), name+".json"), nil
}

// Validate checks a decoded profile object and returns it with defaults filled in for what
// is missing. Every action is checked here, so a bad action is refused when it is set.
func (s *Store) Validate(v any) (Profile, error) {
	m, ok := v.(map[string]any)
	if !ok {
		return Profile{}, errors.New("a key profile is an object with tap, double, triple, long and fn_esc")
	}
	out := Profile{Bindings: Default()}
	out.Description, _ = m["description"].(string)
	for _, g := range Gestures() {
		spec := out.Action(g)
		if raw, present := m[g]; present {
			str, isStr := raw.(string)
			if !isStr {
				return Profile{}, fmt.Errorf("%s: an action is text", Label(g))
			}
			spec = str
		}
		if err := s.check(spec); err != nil {
			return Profile{}, fmt.Errorf("%s: %w", Label(g), err)
		}
		out.SetAction(g, spec)
	}
	var err error
	if out.LongMS, err = timing(m, "long_ms", out.LongMS, LongMSMin, LongMSMax); err != nil {
		return Profile{}, err
	}
	if out.MultiMS, err = timing(m, "multi_ms", out.MultiMS, MultiMSMin, MultiMSMax); err != nil {
		return Profile{}, err
	}
	return out, nil
}

// timing reads an integer in lo..hi. A JSON number with a fractional part, a string or a
// boolean is refused, as 0.3.x did.
func timing(m map[string]any, key string, def, lo, hi int) (int, error) {
	raw, present := m[key]
	if !present {
		return def, nil
	}
	n, ok := raw.(json.Number)
	if !ok {
		return 0, fmt.Errorf("%s: %d..%d", key, lo, hi)
	}
	v, err := n.Int64()
	if err != nil || v < int64(lo) || v > int64(hi) {
		return 0, fmt.Errorf("%s: %d..%d", key, lo, hi)
	}
	return int(v), nil
}

// Parse decodes and validates a profile from JSON text.
func (s *Store) Parse(data []byte) (Profile, error) {
	dec := json.NewDecoder(bytes.NewReader(data))
	dec.UseNumber()
	var v any
	if err := dec.Decode(&v); err != nil {
		return Profile{}, errInvalidJSON
	}
	if dec.More() { // trailing data: Python's json.load refuses it too
		return Profile{}, errInvalidJSON
	}
	return s.Validate(v)
}

var errInvalidJSON = errors.New("not valid JSON")

// Names lists the built-in profiles first, then yours sorted.
func (s *Store) Names() []string {
	out := BuiltinNames()
	entries, _ := os.ReadDir(s.dir())
	var mine []string
	for _, e := range entries {
		n, ok := strings.CutSuffix(e.Name(), ".json")
		if ok && !slices.Contains(out, n) {
			mine = append(mine, n)
		}
	}
	sort.Strings(mine)
	return append(out, mine...)
}

// Load returns a built-in or saved profile, validated.
func (s *Store) Load(name string) (Profile, error) {
	if p, ok := Builtin(name); ok {
		return s.Validate(toObject(p))
	}
	path, err := s.path(name)
	if err != nil {
		return Profile{}, err
	}
	data, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return Profile{}, fmt.Errorf("no key profile '%s' (truesense keys profile list)", name)
	}
	if err != nil {
		return Profile{}, fmt.Errorf("read key profile '%s': %w", name, err)
	}
	p, err := s.Parse(data)
	if errors.Is(err, errInvalidJSON) {
		return Profile{}, fmt.Errorf("key profile '%s' is not valid JSON", name)
	}
	return p, err
}

// toObject is a profile as the generic JSON object Validate takes, so built-ins and saved
// files go through exactly the same checks.
func toObject(p Profile) map[string]any {
	m := map[string]any{"description": p.Description, "long_ms": json.Number(fmt.Sprint(p.LongMS)), "multi_ms": json.Number(fmt.Sprint(p.MultiMS))}
	for _, g := range Gestures() {
		m[g] = p.Action(g)
	}
	return m
}

// Save validates and writes a profile. Built-in names are refused so a built-in can never be
// altered: copy it under another name instead.
func (s *Store) Save(name string, profile Profile) (Profile, error) {
	if _, ok := Builtin(name); ok {
		return Profile{}, fmt.Errorf("'%s' is built in and read-only; copy it (keys profile copy %s) and edit the copy", name, name)
	}
	p, err := s.Validate(toObject(profile))
	if err != nil {
		return Profile{}, err
	}
	path, err := s.path(name)
	if err != nil {
		return Profile{}, err
	}
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false) // an exec: action may contain < > &
	enc.SetIndent("", " ")
	if err := enc.Encode(p); err != nil {
		return Profile{}, err
	}
	if err := fsx.WriteAtomic(path, buf.Bytes(), 0o644); err != nil {
		return Profile{}, fmt.Errorf("save key profile '%s': %w", name, err)
	}
	return p, nil
}

// Delete removes a saved profile.
func (s *Store) Delete(name string) error {
	if _, ok := Builtin(name); ok {
		return fmt.Errorf("'%s' is built in and cannot be deleted", name)
	}
	path, err := s.path(name)
	if err != nil {
		return err
	}
	if err := os.Remove(path); errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("no key profile '%s'", name)
	} else if err != nil {
		return fmt.Errorf("delete key profile '%s': %w", name, err)
	}
	return nil
}

// Copy saves a profile under a new name (default NAME-copy, numbered until free) and returns
// that name.
func (s *Store) Copy(name, newName string) (string, error) {
	base := newName
	if base == "" {
		base = name + "-copy"
	}
	taken := s.Names()
	target, n := base, 2
	for slices.Contains(taken, target) {
		target = fmt.Sprintf("%s-%d", base, n)
		n++
	}
	p, err := s.Load(name)
	if err != nil {
		return "", err
	}
	if _, err := s.Save(target, p); err != nil {
		return "", err
	}
	return target, nil
}

// Use makes a profile the active one: its actions go into the user's configuration under
// [keys] together with its name; the key service reloads on its own.
func (s *Store) Use(name string) (Profile, error) {
	p, err := s.Load(name)
	if err != nil {
		return Profile{}, err
	}
	keys := map[string]any{"profile": name, "long_ms": p.LongMS, "multi_ms": p.MultiMS}
	for _, g := range Gestures() {
		keys[g] = p.Action(g)
	}
	if err := config.WriteUser(map[string]any{"keys": keys}, s.configPath()); err != nil {
		return Profile{}, fmt.Errorf("use key profile '%s': %w", name, err)
	}
	return p, nil
}

// Active is the bindings in force now (what the key service uses) and the profile they came
// from.
type Active struct {
	Bindings
	Profile string `json:"profile"`
}

// Current returns the bindings in the effective configuration.
func (s *Store) Current() (Active, error) {
	load := s.LoadConfig
	if load == nil {
		load = config.LoadDefault
	}
	cfg, err := load()
	if err != nil {
		return Active{}, err
	}
	return FromConfig(cfg), nil
}

// FromConfig reads the bindings out of a configuration.
func FromConfig(cfg *config.Config) Active {
	k := cfg.Keys
	a := Active{Bindings: Bindings{Tap: k.Tap, Double: k.Double, Triple: k.Triple, Long: k.Long, FnEsc: k.FnEsc, LongMS: k.LongMS, MultiMS: k.MultiMS}, Profile: k.Profile}
	if a.Profile == "" {
		a.Profile = "default"
	}
	return a
}
