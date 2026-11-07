package smartprofile

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

var nameRE = regexp.MustCompile(`^[A-Za-z0-9._-]{1,40}$`)

// Store keeps the user's own profiles, one JSON file per profile (NAME.json) in Dir. The
// built-in profiles are always available and read-only.
type Store struct {
	Dir string
}

// DefaultDir is where the user's profiles live: ~/.config/truesense/smart.
func DefaultDir() string {
	home, err := os.UserHomeDir()
	if err != nil {
		home = "~"
	}
	return filepath.Join(home, ".config", "truesense", "smart")
}

// NewStore returns a Store over the default directory.
func NewStore() *Store { return &Store{Dir: DefaultDir()} }

func (s *Store) path(name string) (string, error) {
	if !nameRE.MatchString(name) {
		return "", firmware.Errorf("profile names use letters, digits, . _ - (max 40)")
	}
	return filepath.Join(s.Dir, name+".json"), nil
}

// Names lists the built-in profiles first, then the user's, sorted. A directory that does not
// exist yet is not an error.
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

// Load returns a built-in or saved profile, validated.
func (s *Store) Load(name string) (Profile, error) {
	if p, ok := Builtin(name); ok {
		return p, p.Check()
	}
	path, err := s.path(name)
	if err != nil {
		return Profile{}, err
	}
	data, err := os.ReadFile(path)
	if errors.Is(err, fs.ErrNotExist) {
		return Profile{}, firmware.Errorf("no smart profile '%s' (truesense power smart list)", name)
	}
	if err != nil {
		return Profile{}, err
	}
	if !json.Valid(data) {
		return Profile{}, firmware.Errorf("smart profile '%s' is not valid JSON", name)
	}
	return Parse(data)
}

// Save validates a profile and writes it atomically under name. Built-in names are refused.
func (s *Store) Save(name string, p Profile) (Profile, error) {
	if IsBuiltin(name) {
		return Profile{}, firmware.Errorf("'%s' is built in and read-only; copy it (power smart copy %s) and edit the copy", name, name)
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
		return firmware.Errorf("no smart profile '%s'", name)
	} else if err != nil {
		return err
	}
	return nil
}

// Copy saves a copy of profile name under newName, or under name+"-copy" (then "-copy-2", ...)
// when newName is empty or taken, and returns the name it used. This is how a built-in
// profile becomes editable.
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
