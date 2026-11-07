package gpuundervolt

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
)

// Profile is a named curve offset and clock cap. The JSON names are the Python dict keys.
type Profile struct {
	Description string `json:"description"`
	Offset      int    `json:"offset"` // MHz of curve shift
	Cap         int    `json:"cap"`    // MHz, 0 = no cap
}

var builtinOrder = []string{"native", "truesense", "mild", "cool", "efficient"}

func builtinProfile(name string) (Profile, bool) {
	switch name {
	case "native":
		return Profile{"PredatorSense default: no offset and no clock cap", 0, 0}, true
	case "truesense":
		return Profile{"Our profile for the RTX 4060 Laptop: a moderate curve shift with a clock cap, aiming for less power and heat at about the same speed. Unverified on your unit: test under load", 120, 2400}, true
	case "mild":
		return Profile{"Very safe: small shift, high cap", 60, 2500}, true
	case "cool":
		return Profile{"Cooler and quieter: lower cap, bigger shift", 100, 2100}, true
	case "efficient":
		return Profile{"Lowest power for light gaming: some games will run slower", 150, 1800}, true
	}
	return Profile{}, false
}

// BuiltinNames lists the built-in profiles in display order.
func BuiltinNames() []string { return append([]string(nil), builtinOrder...) }

// Builtin returns a built-in profile, or false when there is none of that name.
func Builtin(name string) (Profile, bool) { return builtinProfile(name) }

// IsBuiltin reports whether name is a built-in (read-only) profile.
func IsBuiltin(name string) bool {
	_, ok := builtinProfile(name)
	return ok
}

// Check verifies the offset (-300..250 MHz) and the cap (0 or 600..3300 MHz).
func (p Profile) Check() error {
	if p.Offset < OffsetMinMHz || p.Offset > OffsetMaxMHz {
		return offsetError()
	}
	if p.Cap != 0 && (p.Cap < CapMinMHz || p.Cap > CapMaxMHz) {
		return capError()
	}
	return nil
}

func offsetError() error {
	return firmware.Errorf("offset: %d..%d MHz", OffsetMinMHz, OffsetMaxMHz)
}

func capError() error {
	return firmware.Errorf("cap: 0 (none) or %d..%d MHz", CapMinMHz, CapMaxMHz)
}

// Validate checks a profile that arrived as decoded JSON and returns it normalised: only the
// known keys are kept, a missing cap means 0 (none), numbers are truncated to whole MHz.
func Validate(v any) (Profile, error) {
	data, err := json.Marshal(v)
	if err != nil {
		return Profile{}, firmware.Errorf("a GPU undervolt profile has 'offset' (MHz) and 'cap' (MHz, 0 = none)")
	}
	return Parse(data)
}

// Parse is Validate for JSON text.
func Parse(data []byte) (Profile, error) {
	var obj map[string]json.RawMessage
	if err := json.Unmarshal(data, &obj); err != nil || obj == nil {
		return Profile{}, firmware.Errorf("a GPU undervolt profile has 'offset' (MHz) and 'cap' (MHz, 0 = none)")
	}
	var p Profile
	var s *string
	if json.Unmarshal(obj["description"], &s) == nil && s != nil {
		p.Description = *s
	}
	var o *float64
	if json.Unmarshal(obj["offset"], &o) != nil || o == nil || *o < OffsetMinMHz || *o > OffsetMaxMHz {
		return Profile{}, offsetError()
	}
	p.Offset = int(*o)
	if raw, present := obj["cap"]; present {
		var c *float64
		if json.Unmarshal(raw, &c) != nil || c == nil || !(*c == 0 || (*c >= CapMinMHz && *c <= CapMaxMHz)) {
			return Profile{}, capError()
		}
		p.Cap = int(*c)
	}
	return p, nil
}

// Level is the safety rating of an offset and cap.
type Level string

// The three risk levels.
const (
	Safe    Level = "safe"
	Caution Level = "caution"
	Danger  Level = "danger"
)

// An offset up to safeOffsetMHz is usually fine with a clock cap; past dangerOffsetMHz games
// and the driver can crash.
const (
	safeOffsetMHz   = 150
	dangerOffsetMHz = 200
	lowCapMHz       = 1500
)

// Risk rates an offset and cap and explains the rating. The daemon refuses a Danger setting
// unless the caller says force.
func Risk(offset, capMHz int) (Level, string) {
	switch {
	case offset > dangerOffsetMHz:
		return Danger, fmt.Sprintf("Beyond the danger line (+%d MHz). Games and the driver can crash; the screen can glitch.", dangerOffsetMHz)
	case offset > safeOffsetMHz:
		return Caution, fmt.Sprintf("Past the safety line (+%d MHz). It may crash in some games: test under load.", safeOffsetMHz)
	case offset > 0 && capMHz == 0:
		return Caution, "A positive offset without a clock cap can push the clock above stock: set a cap (2400 MHz is typical) or keep the offset small."
	case offset < -100:
		return Caution, "A large negative offset only slows the GPU; it does not save voltage the way a positive offset with a cap does."
	case capMHz != 0 && capMHz < lowCapMHz:
		return Caution, "A cap this low will cost a lot of speed in games."
	case offset != 0 || capMHz != 0:
		return Safe, "Inside the safe range."
	}
	return Safe, "Stock."
}

var nameRE = regexp.MustCompile(`^[A-Za-z0-9._-]{1,40}$`)

// Store keeps the user's own GPU undervolt profiles, one JSON file per profile (NAME.json) in
// Dir. The built-in profiles are always available and read-only. Saving applies nothing.
type Store struct {
	Dir string
}

// DefaultDir is where the user's profiles live: ~/.config/truesense/gpu-undervolt.
func DefaultDir() string {
	home, err := os.UserHomeDir()
	if err != nil {
		home = "~"
	}
	return filepath.Join(home, ".config", "truesense", "gpu-undervolt")
}

// NewStore returns a Store over the default directory.
func NewStore() *Store { return &Store{Dir: DefaultDir()} }

func (s *Store) path(name string) (string, error) {
	if !nameRE.MatchString(name) {
		return "", firmware.Errorf("profile names use letters, digits, . _ - (max 40)")
	}
	return filepath.Join(s.Dir, name+".json"), nil
}

// Names lists the built-in profiles first, then the user's, sorted.
func (s *Store) Names() ([]string, error) {
	out := BuiltinNames()
	entries, err := os.ReadDir(s.Dir)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return out, nil
		}
		return nil, err
	}
	var own []string
	for _, e := range entries {
		n, ok := strings.CutSuffix(e.Name(), ".json")
		if ok && nameRE.MatchString(n) && !IsBuiltin(n) {
			own = append(own, n)
		}
	}
	sort.Strings(own)
	return append(out, own...), nil
}

// Load returns a built-in or saved profile, validated. "stock" is the old name of "native".
func (s *Store) Load(name string) (Profile, error) {
	if name == "stock" {
		name = "native"
	}
	if p, ok := Builtin(name); ok {
		return p, p.Check()
	}
	path, err := s.path(name)
	if err != nil {
		return Profile{}, err
	}
	data, err := os.ReadFile(path)
	if errors.Is(err, fs.ErrNotExist) {
		return Profile{}, firmware.Errorf("no GPU undervolt profile '%s' (truesense gpu undervolt list)", name)
	}
	if err != nil {
		return Profile{}, err
	}
	if !json.Valid(data) {
		return Profile{}, firmware.Errorf("GPU undervolt profile '%s' is not valid JSON", name)
	}
	return Parse(data)
}

// Save validates a profile and writes it atomically under name. Built-in names are refused.
func (s *Store) Save(name string, p Profile) (Profile, error) {
	if IsBuiltin(name) {
		return Profile{}, firmware.Errorf("'%s' is built in and read-only; copy it (gpu undervolt copy %s) and edit the copy", name, name)
	}
	if err := p.Check(); err != nil {
		return Profile{}, err
	}
	path, err := s.path(name)
	if err != nil {
		return Profile{}, err
	}
	data, err := json.MarshalIndent(p, "", " ")
	if err != nil {
		return Profile{}, err
	}
	if err := fsx.WriteAtomic(path, data, 0o644); err != nil {
		return Profile{}, err
	}
	return p, nil
}

// Delete removes a saved profile. Built-in profiles cannot be deleted.
func (s *Store) Delete(name string) error {
	if IsBuiltin(name) {
		return firmware.Errorf("'%s' is built in and cannot be deleted", name)
	}
	path, err := s.path(name)
	if err != nil {
		return err
	}
	if err := os.Remove(path); errors.Is(err, fs.ErrNotExist) {
		return firmware.Errorf("no GPU undervolt profile '%s'", name)
	} else if err != nil {
		return err
	}
	return nil
}

// Copy saves a copy of profile name under newName, or name+"-copy" (then "-copy-2", ...) when
// newName is empty or taken, and returns the name it used.
func (s *Store) Copy(name, newName string) (string, error) {
	base := newName
	if base == "" {
		base = name + "-copy"
	}
	taken, err := s.Names()
	if err != nil {
		return "", err
	}
	used := make(map[string]bool, len(taken))
	for _, n := range taken {
		used[n] = true
	}
	target := base
	for n := 2; used[target]; n++ {
		target = fmt.Sprintf("%s-%d", base, n)
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
