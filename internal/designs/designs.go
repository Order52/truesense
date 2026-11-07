// Package designs holds key designs (a still picture on the keyboard) and paints images
// across the keys.
//
// A design is plain data: {"base": "rrggbb", "keys": {"w": "ff6600", "arrows": "00aaff"},
// "cells": {"12": "ffffff"}}; keys and groups use the names of `keyboard list-keys`. Designs
// live in ~/.config/truesense/designs/NAME.json (yours win over the built-in ones of the same
// name). No code is loaded, so a design file cannot do anything but colour keys, and a broken
// file is skipped, never fatal.
package designs

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/Order52/truesense/internal/firmware"
	"github.com/Order52/truesense/internal/fsx"
	"github.com/Order52/truesense/internal/keymap"
)

// Where a design came from, as shown in lists.
const (
	SourceBuiltin = "built-in"
	SourceYours   = "yours"
)

const maxInfo = 80

// Design is one picture. JSON names are those of the 0.3.x files.
type Design struct {
	Info   string `json:"info"`
	Base   string `json:"base"`
	Keys   Colors `json:"keys"`
	Cells  Colors `json:"cells"`
	Source string `json:"source,omitempty"`
}

func kc(pairs ...string) Colors {
	c := make(Colors, 0, len(pairs)/2)
	for i := 0; i+1 < len(pairs); i += 2 {
		c = append(c, keymap.KeyColor{Key: pairs[i], Color: pairs[i+1]})
	}
	return c
}

// Builtin returns the designs that ship with the program, by name.
func Builtin() map[string]Design {
	return map[string]Design{
		"gaming": {Info: "WASD and the keys around it lit for play", Base: "101018", Keys: kc("wasd", "ff6a00", "lshift", "ff3300", "space", "ff3300", "ctrl", "ff3300",
			"q", "00aaff", "e", "00aaff", "r", "00aaff", "f", "00aaff", "numrow", "22dd44", "tab", "ff0066", "esc", "ff0066", "arrows", "ffd000")},
		"coding": {Info: "dark board, function keys and Esc picked out", Base: "08080f", Keys: kc("letters", "1d6f8a", "numrow", "2a8fb0", "fkeys", "ff9900",
			"esc", "ff2244", "enter", "55ee66", "backspace", "ff5566", "arrows", "7a7aff")},
		"arrows":  {Info: "only the arrow keys and WASD", Base: "000000", Keys: kc("wasd", "ffffff", "arrows", "ffffff")},
		"ice":     {Info: "cool blue to white", Base: "0a2a50", Keys: kc("letters", "3ab0ff", "numrow", "7fd0ff", "fkeys", "c8ecff", "esc", "ffffff")},
		"ember":   {Info: "warm red and orange", Base: "3a0600", Keys: kc("letters", "ff4a00", "numrow", "ff7a10", "fkeys", "ffb030", "esc", "ffe080")},
		"stealth": {Info: "almost off, just the letters", Base: "000000", Keys: kc("letters", "14323c")},
	}
}

// Store is a directory of your designs over the built-in ones.
type Store struct {
	Dir string
	Map *keymap.Map
}

// DefaultDir is $XDG_CONFIG_HOME/truesense/designs (default ~/.config/truesense/designs).
func DefaultDir() string {
	base := os.Getenv("XDG_CONFIG_HOME")
	if base == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			return filepath.Join(os.TempDir(), "truesense-designs")
		}
		base = filepath.Join(home, ".config")
	}
	return filepath.Join(base, "truesense", "designs")
}

// New returns a Store over dir (DefaultDir if empty) and the verified keymap.
func New(dir string) *Store {
	if dir == "" {
		dir = DefaultDir()
	}
	return &Store{Dir: dir, Map: keymap.Verified()}
}

func describeJSON(raw json.RawMessage) string {
	var v any
	if json.Unmarshal(raw, &v) != nil {
		return string(raw)
	}
	switch x := v.(type) {
	case string:
		return x
	case nil:
		return "None" // str(None) in the Python tool
	case bool:
		if x {
			return "True"
		}
		return "False"
	}
	return string(raw)
}

// Clean validates a design the way the tool always did and returns it normalised: info cut to
// 80 characters, every colour and key name checked against m.
func Clean(m *keymap.Map, d Design) (Design, error) {
	out := Design{Info: truncate(d.Info, maxInfo), Base: d.Base, Keys: append(Colors{}, d.Keys...), Cells: append(Colors{}, d.Cells...), Source: d.Source}
	if _, err := keymap.ParseHex(out.Base); err != nil {
		return Design{}, err
	}
	for _, e := range out.Keys {
		if _, err := keymap.ParseHex(e.Color); err != nil {
			return Design{}, err
		}
		if _, err := m.CellsFor(e.Key); err != nil {
			return Design{}, err
		}
	}
	for _, e := range out.Cells {
		if _, err := keymap.ParseHex(e.Color); err != nil {
			return Design{}, err
		}
		if _, err := cellIndex(e.Key); err != nil {
			return Design{}, err
		}
	}
	return out, nil
}

func truncate(s string, n int) string {
	if utf8.RuneCountInString(s) <= n {
		return s
	}
	return string([]rune(s)[:n])
}

func cellIndex(k string) (int, error) {
	bad := firmware.Errorf("cell '%s': 0..127", k)
	if k == "" || strings.Trim(k, "0123456789") != "" {
		return 0, bad
	}
	n, err := strconv.Atoi(k)
	if err != nil || n >= keymap.NumCells {
		return 0, bad
	}
	return n, nil
}

// Parse decodes and validates one design file.
func Parse(m *keymap.Map, data []byte) (Design, error) {
	var raw map[string]json.RawMessage
	if err := json.Unmarshal(data, &raw); err != nil || raw == nil {
		return Design{}, firmware.Errorf("a design is a JSON object")
	}
	d := Design{Base: "000000"}
	if v, ok := raw["info"]; ok {
		d.Info = describeJSON(v)
	}
	if v, ok := raw["base"]; ok {
		if err := json.Unmarshal(v, &d.Base); err != nil {
			return Design{}, firmware.Errorf("design base must be a colour string")
		}
	}
	for name, dst := range map[string]*Colors{"keys": &d.Keys, "cells": &d.Cells} {
		if v, ok := raw[name]; ok {
			if err := json.Unmarshal(v, dst); err != nil {
				return Design{}, firmware.Errorf("design %s: %v", name, err)
			}
		}
	}
	return Clean(m, d)
}

// LoadAll returns every design by name: the built-in ones, then the files of the user folder
// (which win). A broken file is skipped.
func (s *Store) LoadAll() map[string]Design {
	res := map[string]Design{}
	for n, d := range Builtin() {
		if c, err := Clean(s.Map, d); err == nil {
			c.Source = SourceBuiltin
			res[n] = c
		}
	}
	entries, err := os.ReadDir(s.Dir)
	if err != nil {
		return res
	}
	names := make([]string, 0, len(entries))
	for _, e := range entries {
		names = append(names, e.Name())
	}
	sort.Strings(names)
	for _, fn := range names {
		n, ok := strings.CutSuffix(fn, ".json")
		if !ok {
			continue
		}
		data, err := os.ReadFile(filepath.Join(s.Dir, fn))
		if err != nil {
			continue
		}
		d, err := Parse(s.Map, data)
		if err != nil {
			continue
		}
		d.Source = SourceYours
		res[n] = d
	}
	return res
}

// Names lists every design name, sorted.
func (s *Store) Names() []string {
	all := s.LoadAll()
	names := make([]string, 0, len(all))
	for n := range all {
		names = append(names, n)
	}
	sort.Strings(names)
	return names
}

// Get returns one design.
func (s *Store) Get(name string) (Design, error) {
	d, ok := s.LoadAll()[name]
	if !ok {
		return Design{}, firmware.Errorf("no design '%s' (see: truesense keyboard design list)", name)
	}
	return d, nil
}

// CellsOf returns the 128 cell colours for a design: base, then keys and groups, then single cells.
func CellsOf(m *keymap.Map, d Design) ([]keymap.RGB, error) {
	base, err := keymap.ParseHex(d.Base)
	if err != nil {
		return nil, err
	}
	cv := keymap.Canvas(base)
	for _, e := range d.Keys {
		rgb, err := keymap.ParseHex(e.Color)
		if err != nil {
			return nil, err
		}
		cells, err := m.CellsFor(e.Key)
		if err != nil {
			return nil, err
		}
		for _, c := range cells {
			cv[c] = rgb
		}
	}
	for _, e := range d.Cells {
		rgb, err := keymap.ParseHex(e.Color)
		if err != nil {
			return nil, err
		}
		i, err := cellIndex(e.Key)
		if err != nil {
			return nil, err
		}
		cv[i] = rgb
	}
	return cv, nil
}

// savedDesign is the file Save writes: only the picture, no key names.
type savedDesign struct {
	Info  string `json:"info"`
	Base  string `json:"base"`
	Cells Colors `json:"cells"`
}

// Save stores the picture now on the keyboard as one of your designs and returns the file path.
func (s *Store) Save(name string, cells []keymap.RGB) (string, error) {
	if name == "" || strings.ContainsAny(name, `/\.`) || utf8.RuneCountInString(name) > 40 {
		return "", firmware.Errorf("design name: letters, digits, - and _")
	}
	d := savedDesign{Info: "saved from the keyboard", Base: "000000", Cells: Colors{}}
	for i, c := range cells {
		if i >= keymap.LitCells {
			break
		}
		d.Cells = append(d.Cells, keymap.KeyColor{Key: strconv.Itoa(i), Color: c.Hex()})
	}
	b, err := json.MarshalIndent(d, "", " ")
	if err != nil {
		return "", err
	}
	p := filepath.Join(s.Dir, name+".json")
	if err := fsx.WriteAtomic(p, b, 0o644); err != nil {
		return "", fmt.Errorf("save design %q: %w", name, err)
	}
	return p, nil
}
