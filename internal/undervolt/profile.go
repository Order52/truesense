package undervolt

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

// Profile is a named pair of offsets. Values are common conservative starting points for
// 13th-gen HX chips, NOT validated on every unit: test each under load before trusting it.
// The JSON names are the Python dict keys.
type Profile struct {
	Description string `json:"description"`
	Core        int    `json:"core"`
	Cache       int    `json:"cache"`
}

var builtinOrder = []string{"native", "truesense", "mild", "balanced", "aggressive"}

func builtinProfile(name string) (Profile, bool) {
	switch name {
	case "native":
		return Profile{"PredatorSense default: no undervolt (0 mV)", 0, 0}, true
	case "truesense":
		return Profile{"Our profile for the i7-13700HX: a conservative offset that usually gives a few degrees less heat at the same speed. Unverified on your unit: test under load", -50, -40}, true
	case "mild":
		return Profile{"Very safe: small offset", -30, -30}, true
	case "balanced":
		return Profile{"Moderate: more heat saved, more chance of instability", -70, -50}, true
	case "aggressive":
		return Profile{"Maximum offered: only if you have tested it; can freeze or reboot", -100, -80}, true
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

// Check verifies that both offsets are 0 to -MaxMV mV.
func (p Profile) Check() error {
	for _, o := range []struct {
		key string
		mv  int
	}{{"core", p.Core}, {"cache", p.Cache}} {
		if o.mv > 0 || o.mv < -MaxMV {
			return offsetError(o.key)
		}
	}
	return nil
}

func offsetError(key string) error { return firmware.Errorf("%s: 0 to -%d mV", key, MaxMV) }

// Validate checks a profile that arrived as decoded JSON and returns it normalised: only the
// three known keys are kept, offsets are truncated to whole millivolts.
func Validate(v any) (Profile, error) {
	data, err := json.Marshal(v)
	if err != nil {
		return Profile{}, firmware.Errorf("an undervolt profile has 'core' and 'cache' offsets in mV")
	}
	return Parse(data)
}

// Parse is Validate for JSON text.
func Parse(data []byte) (Profile, error) {
	var obj map[string]json.RawMessage
	if err := json.Unmarshal(data, &obj); err != nil || obj == nil {
		return Profile{}, firmware.Errorf("an undervolt profile has 'core' and 'cache' offsets in mV")
	}
	var p Profile
	var s *string
	if json.Unmarshal(obj["description"], &s) == nil && s != nil {
		p.Description = *s
	}
	for _, o := range []struct {
		key string
		dst *int
	}{{"core", &p.Core}, {"cache", &p.Cache}} {
		var f *float64
		if json.Unmarshal(obj[o.key], &f) != nil || f == nil || *f > 0 || *f < -MaxMV {
			return Profile{}, offsetError(o.key)
		}
		*o.dst = int(*f)
	}
	return p, nil
}

// Level is the safety rating of an offset pair.
type Level string

// The three risk levels.
const (
	Safe    Level = "safe"
	Caution Level = "caution"
	Danger  Level = "danger"
)

func abs(n int) int {
	if n < 0 {
		return -n
	}
	return n
}

// Risk rates a pair of offsets (in mV, negative) and explains it: the safety line is where
// instability becomes likely, the danger line where freezes are likely. The daemon refuses a
// Danger pair unless the caller says force.
func Risk(core, cache int) (Level, string) {
	c, k := abs(core), abs(cache)
	switch {
	case c > CautionCoreMV || k > CautionCacheMV:
		return Danger, fmt.Sprintf("Beyond the danger line (core %d mV / cache %d mV). Freezes and reboots are likely; unsaved work can be lost.", CautionCoreMV, CautionCacheMV)
	case c > SafeCoreMV || k > SafeCacheMV:
		return Caution, fmt.Sprintf("Past the safety line (core %d mV / cache %d mV). It may be unstable: test under load before trusting it.", SafeCoreMV, SafeCacheMV)
	case c != 0 || k != 0:
		return Safe, "Inside the safe range."
	}
	return Safe, "Stock voltage."
}

var nameRE = regexp.MustCompile(`^[A-Za-z0-9._-]{1,40}$`)

// Store keeps the user's own undervolt profiles, one JSON file per profile (NAME.json) in Dir.
// The built-in profiles are always available and read-only. Saving a profile applies nothing.
type Store struct {
	Dir string
}

// DefaultDir is where the user's profiles live: ~/.config/truesense/undervolt.
func DefaultDir() string {
	home, err := os.UserHomeDir()
	if err != nil {
		home = "~"
	}
	return filepath.Join(home, ".config", "truesense", "undervolt")
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
		return Profile{}, firmware.Errorf("no undervolt profile '%s' (truesense cpu undervolt list)", name)
	}
	if err != nil {
		return Profile{}, err
	}
	if !json.Valid(data) {
		return Profile{}, firmware.Errorf("undervolt profile '%s' is not valid JSON", name)
	}
	return Parse(data)
}

// Save validates a profile and writes it atomically under name. Built-in names are refused.
func (s *Store) Save(name string, p Profile) (Profile, error) {
	if IsBuiltin(name) {
		return Profile{}, firmware.Errorf("'%s' is built in and read-only; copy it (cpu undervolt copy %s) and edit the copy", name, name)
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
		return firmware.Errorf("no undervolt profile '%s'", name)
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
