// Package keyprofiles stores named per-key colour profiles (128 colours each) in the user's
// lighting directory (~/.config/truesense/lighting/NAME.json) and applies them through the
// keyboard. The file format is the one 0.3.x wrote: {"cells": [[r, g, b], ...]}.
package keyprofiles

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"unicode"

	"github.com/Order52/truesense/internal/firmware"
	"github.com/Order52/truesense/internal/fsx"
	"github.com/Order52/truesense/internal/keyboard"
	"github.com/Order52/truesense/internal/keymap"
)

// Keyboard is what applying a profile needs from the keyboard package.
type Keyboard interface {
	// PerKey uploads the colours; brightness keyboard.KeepBrightness keeps the current one.
	PerKey(cells []keymap.RGB, brightness int) error
}

// Store is a directory of profiles.
type Store struct {
	Dir string
	KB  Keyboard
}

// DefaultDir is ~/.config/truesense/lighting. (The Python tool did not honour XDG_CONFIG_HOME
// here, unlike for designs; the files must stay where 0.3.x put them.)
func DefaultDir() string {
	home, err := os.UserHomeDir()
	if err != nil {
		return filepath.Join(os.TempDir(), "truesense-lighting")
	}
	return filepath.Join(home, ".config", "truesense", "lighting")
}

// New returns a Store over dir (DefaultDir if empty) that applies profiles through kb.
func New(dir string, kb Keyboard) *Store {
	if dir == "" {
		dir = DefaultDir()
	}
	return &Store{Dir: dir, KB: kb}
}

// ValidName reports whether name is usable: letters, digits, '-' and '_' (never a path).
func ValidName(name string) bool {
	if name == "" {
		return false
	}
	for _, r := range name {
		if r != '-' && r != '_' && !unicode.IsLetter(r) && !unicode.IsNumber(r) {
			return false
		}
	}
	return true
}

func (s *Store) path(name string) (string, error) {
	if !ValidName(name) {
		return "", firmware.Errorf("profile names: letters, digits, - and _")
	}
	return filepath.Join(s.Dir, name+".json"), nil
}

type file struct {
	Cells [][]int `json:"cells"`
}

// Marshal encodes colours in the profile file format.
func Marshal(cells []keymap.RGB) ([]byte, error) {
	f := file{Cells: make([][]int, len(cells))}
	for i, c := range cells {
		f.Cells[i] = []int{int(c[0]), int(c[1]), int(c[2])}
	}
	return json.Marshal(f)
}

// Parse decodes a profile file. A profile with cells that are not three numbers 0..255 is
// refused whole rather than loaded with wrong colours; at most 128 cells are kept.
func Parse(data []byte) ([]keymap.RGB, error) {
	var f file
	if err := json.Unmarshal(data, &f); err != nil {
		return nil, fmt.Errorf("parse profile: %w", err)
	}
	if f.Cells == nil {
		return nil, errors.New("parse profile: no cells")
	}
	out := make([]keymap.RGB, 0, len(f.Cells))
	for i, c := range f.Cells {
		if len(c) != 3 {
			return nil, fmt.Errorf("parse profile: cell %d is not r, g, b", i)
		}
		var rgb keymap.RGB
		for j, v := range c {
			if v < 0 || v > 255 {
				return nil, fmt.Errorf("parse profile: cell %d: %d is not a colour value", i, v)
			}
			rgb[j] = uint8(v)
		}
		out = append(out, rgb)
	}
	if len(out) > keymap.NumCells {
		out = out[:keymap.NumCells]
	}
	return out, nil
}

// Save stores a profile.
func (s *Store) Save(name string, cells []keymap.RGB) error {
	p, err := s.path(name)
	if err != nil {
		return err
	}
	b, err := Marshal(cells)
	if err != nil {
		return err
	}
	if err := fsx.WriteAtomic(p, b, 0o644); err != nil {
		return fmt.Errorf("save lighting profile %q: %w", name, err)
	}
	return nil
}

// Exists reports whether a profile of that name is stored.
func (s *Store) Exists(name string) bool {
	p, err := s.path(name)
	return err == nil && fsx.Exists(p)
}

// Names lists the stored profiles, sorted. (The Python tool listed every file in the
// directory with its last five characters cut off; only *.json files are listed here.)
func (s *Store) Names() []string {
	entries, err := os.ReadDir(s.Dir)
	if err != nil {
		return []string{}
	}
	names := []string{}
	for _, e := range entries {
		if n, ok := strings.CutSuffix(e.Name(), ".json"); ok && !e.IsDir() {
			names = append(names, n)
		}
	}
	sort.Strings(names)
	return names
}

// Load reads a profile.
func (s *Store) Load(name string) ([]keymap.RGB, error) {
	p, err := s.path(name)
	if err != nil {
		return nil, err
	}
	b, err := os.ReadFile(p)
	if errors.Is(err, fs.ErrNotExist) {
		return nil, firmware.Errorf("no profile '%s' (truesense keyboard profile list)", name)
	}
	if err != nil {
		return nil, fmt.Errorf("read lighting profile %q: %w", name, err)
	}
	cells, err := Parse(b)
	if err != nil {
		return nil, fmt.Errorf("lighting profile %q: %w", name, err)
	}
	return cells, nil
}

// Apply puts a profile on the keyboard (brightness keyboard.KeepBrightness keeps the current level).
func (s *Store) Apply(name string, brightness int) error {
	cells, err := s.Load(name)
	if err != nil {
		return err
	}
	return s.KB.PerKey(cells, brightness)
}

// Delete removes a profile.
func (s *Store) Delete(name string) error {
	p, err := s.path(name)
	if err != nil {
		return err
	}
	if err := os.Remove(p); err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return firmware.Errorf("no lighting profile '%s'", name)
		}
		return fmt.Errorf("delete lighting profile %q: %w", name, err)
	}
	return nil
}

// Command runs `truesense keyboard profile list|load|save|delete NAME [save: fill COLOR | gradient C1 C2 | rainbow]`.
// It returns the names for "list" and a message for the others.
func (s *Store) Command(action, name string, spec []string) (names []string, msg string, err error) {
	if action == "list" {
		return s.Names(), "", nil
	}
	if name == "" {
		return nil, "", firmware.Errorf("profile %s needs a name", action)
	}
	switch action {
	case "load":
		if err := s.Apply(name, keyboard.KeepBrightness); err != nil {
			return nil, "", err
		}
		return nil, fmt.Sprintf("loaded lighting profile '%s'", name), nil
	case "delete":
		if err := s.Delete(name); err != nil {
			return nil, "", err
		}
		return nil, fmt.Sprintf("deleted lighting profile '%s'", name), nil
	case "save":
		cells, err := presetCells(spec)
		if err != nil {
			return nil, "", err
		}
		if err := s.Save(name, cells); err != nil {
			return nil, "", err
		}
		return nil, fmt.Sprintf("saved lighting profile '%s'", name), nil
	}
	return nil, "", firmware.Errorf("profile list | load NAME | save NAME ... | delete NAME")
}

// presetCells turns "fill COLOR", "gradient C1 C2" or "rainbow" into a picture (default: the stock cyan fill).
func presetCells(spec []string) ([]keymap.RGB, error) {
	if len(spec) == 0 {
		spec = []string{"fill", "00aec7"}
	}
	usage := firmware.Errorf("save NAME fill COLOR | gradient C1 C2 | rainbow")
	switch {
	case spec[0] == "rainbow":
		return keymap.Rainbow(), nil
	case spec[0] == "gradient" && len(spec) == 3:
		c1, err := keymap.ParseHex(spec[1])
		if err != nil {
			return nil, err
		}
		c2, err := keymap.ParseHex(spec[2])
		if err != nil {
			return nil, err
		}
		return keymap.Gradient(c1, c2), nil
	case spec[0] == "fill" && len(spec) == 2:
		c, err := keymap.ParseHex(spec[1])
		if err != nil {
			return nil, err
		}
		return keymap.Canvas(c), nil
	}
	return nil, usage
}
