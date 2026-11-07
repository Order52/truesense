package curves

import (
	"encoding/json"
	"errors"
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

// Store keeps the user's own curves, one JSON file per curve (NAME.json) in Dir. The built-in
// curves are always available and read-only. The directory is a field so tests and other
// users can point it elsewhere.
type Store struct {
	Dir string
}

// DefaultDir is where the user's curves live: ~/.config/truesense/curves.
func DefaultDir() string {
	home, err := os.UserHomeDir()
	if err != nil {
		home = "~"
	}
	return filepath.Join(home, ".config", "truesense", "curves")
}

// NewStore returns a Store over the default directory.
func NewStore() *Store { return &Store{Dir: DefaultDir()} }

func (s *Store) path(name string) (string, error) {
	if !nameRE.MatchString(name) {
		return "", firmware.Errorf("curve names use letters, digits, . _ - (max 40)")
	}
	return filepath.Join(s.Dir, name+".json"), nil
}

// Names lists the built-in curves first, then the user's, sorted. A directory that does not
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

// Load returns a built-in or saved curve, validated.
func (s *Store) Load(name string) (Curve, error) {
	if c, ok := Builtin(name); ok {
		return c.Validated()
	}
	p, err := s.path(name)
	if err != nil {
		return Curve{}, err
	}
	data, err := os.ReadFile(p)
	if errors.Is(err, fs.ErrNotExist) {
		return Curve{}, firmware.Errorf("no fan curve '%s' (truesense fan curve list)", name)
	}
	if err != nil {
		return Curve{}, err
	}
	if !json.Valid(data) {
		return Curve{}, firmware.Errorf("fan curve '%s' is not valid JSON", name)
	}
	return Parse(data)
}

// Save validates a curve and writes it atomically under name. Built-in names are refused so a
// built-in can never be altered; copy it under another name instead.
func (s *Store) Save(name string, c Curve) (Curve, error) {
	if IsBuiltin(name) {
		return Curve{}, firmware.Errorf("'%s' is built in; save your curve under another name", name)
	}
	v, err := c.Validated()
	if err != nil {
		return Curve{}, err
	}
	p, err := s.path(name)
	if err != nil {
		return Curve{}, err
	}
	data, err := json.MarshalIndent(v, "", " ")
	if err != nil {
		return Curve{}, err
	}
	if err := fsx.WriteAtomic(p, data, 0o644); err != nil {
		return Curve{}, err
	}
	return v, nil
}

// Delete removes a saved curve. Built-in curves cannot be deleted.
func (s *Store) Delete(name string) error {
	if IsBuiltin(name) {
		return firmware.Errorf("'%s' is built in and cannot be deleted", name)
	}
	p, err := s.path(name)
	if err != nil {
		return err
	}
	if err := os.Remove(p); errors.Is(err, fs.ErrNotExist) {
		return firmware.Errorf("no fan curve '%s'", name)
	} else if err != nil {
		return err
	}
	return nil
}
