package keyprofiles

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/Order52/truesense/internal/firmware"
	"github.com/Order52/truesense/internal/keyboard"
	"github.com/Order52/truesense/internal/keymap"
	"github.com/Order52/truesense/internal/userstate"
)

type uploaded struct {
	cells      []keymap.RGB
	brightness int
}

type fakeKB struct{ got []uploaded }

func (f *fakeKB) PerKey(c []keymap.RGB, b int) error {
	f.got = append(f.got, uploaded{append([]keymap.RGB(nil), c...), b})
	return nil
}

func newStore(t *testing.T) (*Store, *fakeKB) {
	t.Helper()
	kb := &fakeKB{}
	return New(filepath.Join(t.TempDir(), "lighting"), kb), kb
}

func TestSaveLoadRoundTrip(t *testing.T) {
	s, kb := newStore(t)
	cells := keymap.Rainbow()
	if err := s.Save("my_profile-2", cells); err != nil {
		t.Fatal(err)
	}
	got, err := s.Load("my_profile-2")
	if err != nil || !reflect.DeepEqual(got, cells) {
		t.Fatalf("Load: %v", err)
	}
	if !s.Exists("my_profile-2") || s.Exists("nope") || s.Exists("bad/name") {
		t.Error("Exists")
	}
	if err := s.Apply("my_profile-2", keyboard.KeepBrightness); err != nil {
		t.Fatal(err)
	}
	if len(kb.got) != 1 || !reflect.DeepEqual(kb.got[0].cells, cells) || kb.got[0].brightness != keyboard.KeepBrightness {
		t.Errorf("Apply uploaded %+v", kb.got)
	}
}

// A file as the Python tool wrote it (json.dumps with default separators, 128 cells).
func TestLoadsFilesWrittenBy03x(t *testing.T) {
	s, _ := newStore(t)
	if err := os.MkdirAll(s.Dir, 0o755); err != nil {
		t.Fatal(err)
	}
	py := `{"cells": [[0, 174, 199], [255, 0, 0], [1, 2, 3]]}`
	if err := os.WriteFile(filepath.Join(s.Dir, "old.json"), []byte(py), 0o644); err != nil {
		t.Fatal(err)
	}
	got, err := s.Load("old")
	if err != nil || len(got) != 3 || got[0] != (keymap.RGB{0, 174, 199}) || got[1] != (keymap.RGB{255, 0, 0}) {
		t.Errorf("%v %v", got, err)
	}
	// and what we write is readable by the Python loader: an object with a "cells" list of [r,g,b]
	if err := s.Save("new", []keymap.RGB{{9, 8, 7}}); err != nil {
		t.Fatal(err)
	}
	raw, _ := os.ReadFile(filepath.Join(s.Dir, "new.json"))
	var v map[string][][]int
	if err := json.Unmarshal(raw, &v); err != nil || !reflect.DeepEqual(v["cells"], [][]int{{9, 8, 7}}) {
		t.Errorf("%s %v", raw, err)
	}
}

func TestNameValidation(t *testing.T) {
	s, _ := newStore(t)
	for _, name := range []string{"", "a/b", "../x", "a.b", "a b", "x\x00", "a\n"} {
		if ValidName(name) {
			t.Errorf("%q valid", name)
		}
		err := s.Save(name, nil)
		var fe *firmware.Error
		if err == nil || !asFirmware(err, &fe) {
			t.Errorf("Save(%q) = %v", name, err)
		}
	}
	for _, name := range []string{"a", "A-b_9", "Größe", "日本"} {
		if !ValidName(name) {
			t.Errorf("%q invalid", name)
		}
	}
	if _, err := os.Stat(s.Dir); err == nil {
		t.Error("a refused name must not create anything")
	}
}

func asFirmware(err error, target **firmware.Error) bool { return errors.As(err, target) }

func TestNamesAreSortedAndOnlyJSON(t *testing.T) {
	s, _ := newStore(t)
	if got := s.Names(); got == nil || len(got) != 0 {
		t.Errorf("no directory: %v", got)
	}
	for _, n := range []string{"zeta", "alpha", "mid"} {
		if err := s.Save(n, nil); err != nil {
			t.Fatal(err)
		}
	}
	_ = os.WriteFile(filepath.Join(s.Dir, "notes.txt"), []byte("x"), 0o644)
	_ = os.Mkdir(filepath.Join(s.Dir, "dir.json"), 0o755)
	if got := s.Names(); !reflect.DeepEqual(got, []string{"alpha", "mid", "zeta"}) {
		t.Errorf("%v", got)
	}
}

func TestMissingAndDelete(t *testing.T) {
	s, _ := newStore(t)
	if _, err := s.Load("ghost"); err == nil || err.Error() != "no profile 'ghost' (truesense keyboard profile list)" {
		t.Errorf("%v", err)
	}
	if err := s.Delete("ghost"); err == nil || err.Error() != "no lighting profile 'ghost'" {
		t.Errorf("%v", err)
	}
	_ = s.Save("x", nil)
	if err := s.Delete("x"); err != nil || s.Exists("x") {
		t.Errorf("delete: %v", err)
	}
	if err := s.Apply("ghost", 10); err == nil {
		t.Error("Apply of a missing profile")
	}
}

func TestCorruptFilesAreRefused(t *testing.T) {
	s, kb := newStore(t)
	_ = os.MkdirAll(s.Dir, 0o755)
	for name, body := range map[string]string{
		"junk":    "{nope",
		"nocells": `{}`,
		"short":   `{"cells": [[1, 2]]}`,
		"big":     `{"cells": [[1, 2, 256]]}`,
		"neg":     `{"cells": [[1, 2, -1]]}`,
		"text":    `{"cells": [["a", "b", "c"]]}`,
		"null":    `null`,
	} {
		_ = os.WriteFile(filepath.Join(s.Dir, name+".json"), []byte(body), 0o644)
		if err := s.Apply(name, 10); err == nil || !strings.Contains(err.Error(), "lighting profile") {
			t.Errorf("%s: %v", name, err)
		}
	}
	if len(kb.got) != 0 {
		t.Error("nothing corrupt may reach the keyboard")
	}
}

func TestCommand(t *testing.T) {
	s, kb := newStore(t)
	if names, msg, err := s.Command("list", "", nil); err != nil || len(names) != 0 || msg != "" {
		t.Errorf("list: %v %q %v", names, msg, err)
	}
	for _, tc := range []struct {
		action, name string
		spec         []string
		msg          string
	}{
		{"save", "smoke", nil, "saved lighting profile 'smoke'"},
		{"save", "smoke1", []string{"fill", "ff0000"}, "saved lighting profile 'smoke1'"},
		{"save", "smoke2", []string{"gradient", "ff0000", "0000ff"}, "saved lighting profile 'smoke2'"},
		{"save", "smoke3", []string{"rainbow"}, "saved lighting profile 'smoke3'"},
		{"load", "smoke", nil, "loaded lighting profile 'smoke'"},
		{"delete", "smoke", nil, "deleted lighting profile 'smoke'"},
	} {
		_, msg, err := s.Command(tc.action, tc.name, tc.spec)
		if err != nil || msg != tc.msg {
			t.Errorf("%+v: %q %v", tc, msg, err)
		}
	}
	if names, _, _ := s.Command("list", "", nil); !reflect.DeepEqual(names, []string{"smoke1", "smoke2", "smoke3"}) {
		t.Errorf("%v", names)
	}
	if kb.got[0].brightness != keyboard.KeepBrightness || kb.got[0].cells[0] != (keymap.RGB{0, 0xAE, 0xC7}) {
		t.Errorf("default save is the stock cyan fill, load keeps brightness: %+v", kb.got[0].cells[0])
	}
	c, _ := s.Load("smoke1")
	if c[0] != (keymap.RGB{255, 0, 0}) || c[126] != (keymap.RGB{}) {
		t.Error("fill ff0000")
	}
	g, _ := s.Load("smoke2")
	if !reflect.DeepEqual(g, keymap.Gradient(keymap.RGB{255, 0, 0}, keymap.RGB{0, 0, 255})) {
		t.Error("gradient")
	}
	for _, tc := range []struct {
		action, name string
		spec         []string
		want         string
	}{
		{"load", "", nil, "profile load needs a name"},
		{"save", "", nil, "profile save needs a name"},
		{"bogus", "", nil, "profile bogus needs a name"},
		{"bogus", "x", nil, "profile list | load NAME | save NAME ... | delete NAME"},
		{"save", "x", []string{"fill"}, "save NAME fill COLOR | gradient C1 C2 | rainbow"},
		{"save", "x", []string{"gradient", "ff0000"}, "save NAME fill COLOR | gradient C1 C2 | rainbow"},
		{"save", "x", []string{"fill", "zz"}, "bad colour 'zz', use RRGGBB"},
		{"save", "bad/name", []string{"rainbow"}, "profile names: letters, digits, - and _"},
		{"load", "ghost", nil, "no profile 'ghost' (truesense keyboard profile list)"},
	} {
		if _, _, err := s.Command(tc.action, tc.name, tc.spec); err == nil || err.Error() != tc.want {
			t.Errorf("%+v: %v, want %q", tc, err, tc.want)
		}
	}
}

func TestApplyThroughTheSimulatedKeyboard(t *testing.T) {
	kb, sim := keyboard.NewSim(&userstate.Store{Path: filepath.Join(t.TempDir(), "u.json")})
	s := New(t.TempDir(), kb)
	if err := s.Save("p", keymap.Gradient(keymap.RGB{255, 0, 0}, keymap.RGB{0, 0, 255})); err != nil {
		t.Fatal(err)
	}
	if err := s.Apply("p", 30); err != nil {
		t.Fatal(err)
	}
	if sim.LastCells()[0] != (keymap.RGB{255, 0, 0}) || sim.Brightness() != 30 {
		t.Error("profile did not reach the keyboard")
	}
}

func FuzzParse(f *testing.F) {
	f.Add([]byte(`{"cells": [[0, 174, 199], [255, 0, 0]]}`))
	f.Add([]byte(`{"cells": [[1,2]]}`))
	f.Add([]byte(`{"cells": null}`))
	f.Add([]byte(`[`))
	f.Fuzz(func(t *testing.T, b []byte) {
		cells, err := Parse(b)
		if err != nil {
			return
		}
		if len(cells) > keymap.NumCells {
			t.Fatal("too many cells")
		}
		out, err := Marshal(cells)
		if err != nil {
			t.Fatal(err)
		}
		again, err := Parse(out)
		if err != nil || !reflect.DeepEqual(again, cells) {
			t.Fatalf("round trip: %v", err)
		}
	})
}
