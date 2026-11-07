package looks

import (
	"bytes"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/Order52/truesense/internal/firmware"
	"github.com/Order52/truesense/internal/hw"
	"github.com/Order52/truesense/internal/keyboard"
	"github.com/Order52/truesense/internal/keymap"
	"github.com/Order52/truesense/internal/userstate"
)

type fakeRear struct {
	got []hw.RearOpts
	err error
}

func (f *fakeRear) RearSet(o hw.RearOpts) (hw.Rear, error) {
	f.got = append(f.got, o)
	return hw.Rear{}, f.err
}

type fixture struct {
	s     *Store
	kb    *keyboard.Keyboard
	sim   *keyboard.SimTransport
	rear  *fakeRear
	state *userstate.Store
}

func newFixture(t *testing.T) *fixture {
	t.Helper()
	root := t.TempDir()
	state := &userstate.Store{Path: filepath.Join(root, "user.json")}
	kb, sim := keyboard.NewSim(state)
	rear := &fakeRear{}
	return &fixture{
		s:     &Store{Dir: filepath.Join(root, "looks"), BootFile: filepath.Join(root, "var", "boot-look.json"), State: state, KB: kb, Rear: rear},
		kb:    kb,
		sim:   sim,
		rear:  rear,
		state: state,
	}
}

func isFirmwareError(err error) bool { var fe *firmware.Error; return errors.As(err, &fe) }

var waveLook = &Look{
	Keyboard: &keyboard.State{Mode: "effect", Name: "wave", Color: "00ff00", Speed: keyboard.Int(4), Brightness: keyboard.Int(20), Direction: keyboard.Int(0)},
	Rear:     &Rear{Mode: "breathing", Color: "ff0000", Speed: keyboard.Int(2), Brightness: keyboard.Int(80), Direction: keyboard.Int(1)},
}

func TestBuiltinNativeIsWhatPredatorSenseLeavesOnWindows(t *testing.T) {
	f := newFixture(t)
	l, err := f.s.Load("native")
	if err != nil {
		t.Fatal(err)
	}
	k := l.Keyboard
	if k.Mode != "perkey" || *k.Brightness != 12 || len(k.Cells) != 128 || k.Cells[0] != "00aec7" || k.Cells[125] != "00aec7" || k.Cells[126] != "000000" || k.Cells[127] != "000000" {
		t.Errorf("keyboard %+v", k)
	}
	r := l.Rear
	if r.Mode != "breathing" || r.Color != "3723c7" || *r.Speed != 3 || *r.Brightness != 100 || *r.Direction != 1 || r.Zones != nil {
		t.Errorf("rear %+v", r)
	}
	l.Keyboard.Cells[0] = "ffffff"
	*l.Rear.Speed = 5
	again, _ := f.s.Load("native")
	if again.Keyboard.Cells[0] != "00aec7" || *again.Rear.Speed != 3 {
		t.Error("a loaded built-in look must be a copy")
	}
	// encodes with the same keys the Python dict had, zones null
	b, _ := json.Marshal(again.Rear)
	if string(b) != `{"mode":"breathing","color":"3723c7","speed":3,"brightness":100,"direction":1,"zones":null}` {
		t.Errorf("%s", b)
	}
}

func TestSaveLoadRoundTripAndNames(t *testing.T) {
	f := newFixture(t)
	if got := f.s.Names(); !reflect.DeepEqual(got, []string{"native"}) {
		t.Errorf("%v", got)
	}
	for _, n := range []string{"zed", "alpha.1", "native"} {
		if n == "native" {
			continue
		}
		saved, err := f.s.Save(n, waveLook)
		if err != nil || saved != waveLook {
			t.Fatal(err)
		}
	}
	// a stray native.json must not shadow or duplicate the built-in look
	_ = os.WriteFile(filepath.Join(f.s.Dir, "native.json"), []byte(`{}`), 0o644)
	_ = os.WriteFile(filepath.Join(f.s.Dir, "readme.txt"), []byte(`x`), 0o644)
	if got := f.s.Names(); !reflect.DeepEqual(got, []string{"native", "alpha.1", "zed"}) {
		t.Errorf("builtin first, then yours sorted: %v", got)
	}
	got, err := f.s.Load("zed")
	if err != nil || !reflect.DeepEqual(got, waveLook) {
		t.Errorf("round trip: %+v %v", got, err)
	}
	raw, _ := os.ReadFile(filepath.Join(f.s.Dir, "zed.json"))
	if !strings.HasPrefix(string(raw), "{\n \"keyboard\": {\n  \"mode\": \"effect\"") {
		t.Errorf("indent 1 like the Python tool: %q", raw[:40])
	}
	var generic map[string]map[string]any
	if err := json.Unmarshal(raw, &generic); err != nil || generic["rear"]["zones"] != nil || generic["keyboard"]["direction"] != float64(0) {
		t.Errorf("%v %v", generic, err)
	}
}

func TestLoadsLooksWrittenBy03x(t *testing.T) {
	f := newFixture(t)
	_ = os.MkdirAll(f.s.Dir, 0o755)
	// as keyboard.last() and hw.rear_state() produced them
	py := `{
 "keyboard": {
  "mode": "perkey",
  "brightness": 12,
  "cells": ["00aec7", "ff0000"]
 },
 "rear": {
  "mode": "static",
  "color": "ff0000",
  "speed": 3,
  "brightness": 100,
  "direction": 1,
  "zones": ["ff0000", "00ff00", "0000ff"]
 }
}`
	_ = os.WriteFile(filepath.Join(f.s.Dir, "old.json"), []byte(py), 0o644)
	l, err := f.s.Load("old")
	if err != nil {
		t.Fatal(err)
	}
	if l.Keyboard.Mode != "perkey" || len(l.Keyboard.Cells) != 2 || !reflect.DeepEqual(l.Rear.Zones, []string{"ff0000", "00ff00", "0000ff"}) {
		t.Errorf("%+v %+v", l.Keyboard, l.Rear)
	}
	// a look with only one part (startup apply passes these)
	_ = os.WriteFile(filepath.Join(f.s.Dir, "kbonly.json"), []byte(`{"keyboard": {"mode": "off"}}`), 0o644)
	if l, err := f.s.Load("kbonly"); err != nil || l.Rear != nil || l.Keyboard.Mode != "off" {
		t.Errorf("%+v %v", l, err)
	}
}

func TestErrors(t *testing.T) {
	f := newFixture(t)
	for _, tc := range []struct {
		name string
		run  func() error
		want string
	}{
		{"save builtin", func() error { _, e := f.s.Save("native", waveLook); return e }, "'native' is built in and read-only; save your look under another name"},
		{"delete builtin", func() error { return f.s.Delete("native") }, "'native' is built in and cannot be deleted"},
		{"delete missing", func() error { return f.s.Delete("ghost") }, "no look 'ghost'"},
		{"load missing", func() error { _, e := f.s.Load("ghost"); return e }, "no look 'ghost' (truesense look list)"},
		{"bad name slash", func() error { _, e := f.s.Save("bad/name", waveLook); return e }, "look names use letters, digits, . _ - (max 40)"},
		{"bad name empty", func() error { _, e := f.s.Save("", waveLook); return e }, "look names use letters, digits, . _ - (max 40)"},
		{"bad name long", func() error { _, e := f.s.Save(strings.Repeat("a", 41), waveLook); return e }, "look names use letters, digits, . _ - (max 40)"},
		{"bad name newline", func() error { _, e := f.s.Load("a\n"); return e }, "look names use letters, digits, . _ - (max 40)"},
		{"nothing to capture", func() error { _, e := f.s.Save("x", nil); return e }, "nothing to save yet: set the keyboard or rear light first"},
		{"delete bad name", func() error { return f.s.Delete("../x") }, "look names use letters, digits, . _ - (max 40)"},
	} {
		err := tc.run()
		if err == nil || err.Error() != tc.want || !isFirmwareError(err) {
			t.Errorf("%s: %v, want %q", tc.name, err, tc.want)
		}
	}
	if _, err := os.Stat(f.s.Dir); err == nil {
		t.Error("refused saves must not create the directory")
	}
	if _, err := f.s.Save(strings.Repeat("a", 40), waveLook); err != nil {
		t.Errorf("40 characters are allowed: %v", err)
	}
}

func TestCorruptLookFile(t *testing.T) {
	f := newFixture(t)
	_ = os.MkdirAll(f.s.Dir, 0o755)
	_ = os.WriteFile(filepath.Join(f.s.Dir, "bad.json"), []byte(`{nope`), 0o644)
	_ = os.WriteFile(filepath.Join(f.s.Dir, "wrongtype.json"), []byte(`{"keyboard": 5}`), 0o644)
	for _, n := range []string{"bad", "wrongtype"} {
		if _, err := f.s.Load(n); err == nil || !strings.Contains(err.Error(), "look \""+n+"\"") {
			t.Errorf("%s: %v", n, err)
		}
	}
}

func TestCaptureAndSaveCurrent(t *testing.T) {
	f := newFixture(t)
	if err := f.kb.SetEffect("wave", "00ff00", 4, 20, 0); err != nil {
		t.Fatal(err)
	}
	// hw remembers the rear light like this (rear.go: remember("rear_state", ...))
	_ = f.state.Set("rear_state", map[string]any{"mode": "breathing", "color": "ff0000", "speed": 2, "brightness": 80, "direction": 1, "zones": nil})
	saved, err := f.s.Save("mine", nil)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(saved, waveLook) {
		t.Errorf("captured %+v %+v", saved.Keyboard, saved.Rear)
	}
	t.Run("keyboard only", func(t *testing.T) {
		g := newFixture(t)
		_ = g.kb.SetEffect("static", "010203", 5, 30, 0)
		l, err := g.s.Capture()
		if err != nil || l.Rear != nil || l.Keyboard.Name != "static" {
			t.Errorf("%+v %v", l, err)
		}
	})
	t.Run("rear only", func(t *testing.T) {
		g := newFixture(t)
		_ = g.state.Set("rear_state", map[string]any{"mode": "static", "color": "010203", "speed": 3, "brightness": 50, "direction": 1, "zones": []string{"010203", "010203", "010203"}})
		l, err := g.s.Capture()
		if err != nil || l.Keyboard != nil || len(l.Rear.Zones) != 3 {
			t.Errorf("%+v %v", l, err)
		}
		b, _ := json.Marshal(l)
		if !strings.HasPrefix(string(b), `{"keyboard":null,"rear":{`) {
			t.Errorf("an absent part is null: %s", b)
		}
	})
	t.Run("rear state of the wrong type is ignored", func(t *testing.T) {
		g := newFixture(t)
		_ = g.state.Set("rear_state", "garbage")
		if _, err := g.s.Capture(); err == nil {
			t.Error("garbage is not a look")
		}
	})
}

func TestCopy(t *testing.T) {
	f := newFixture(t)
	_, _ = f.s.Save("a", waveLook)
	for i, want := range []string{"a-copy", "a-copy-2", "a-copy-3"} {
		got, err := f.s.Copy("a", "")
		if err != nil || got != want {
			t.Errorf("copy %d = %q, %v", i, got, err)
		}
	}
	if got, _ := f.s.Copy("a", "other"); got != "other" {
		t.Errorf("%q", got)
	}
	if got, _ := f.s.Copy("a", "other"); got != "other-2" {
		t.Errorf("%q", got)
	}
	if got, _ := f.s.Copy("native", "n"); got != "n" {
		t.Errorf("a built-in look can be copied: %q", got)
	}
	if got, _ := f.s.Copy("native", "native"); got != "native-2" {
		t.Errorf("a copy never takes a built-in name: %q", got)
	}
	if _, err := f.s.Copy("ghost", ""); err == nil {
		t.Error("copy of a missing look")
	}
}

// Expected strings produced by looks.describe in the Python implementation.
func TestDescribe(t *testing.T) {
	f := newFixture(t)
	native, _ := f.s.Load("native")
	for _, tc := range []struct {
		look *Look
		want string
	}{
		{native, "keyboard perkey brightness 12; rear breathing brightness 100"},
		{waveLook, "keyboard wave brightness 20 speed 4; rear breathing brightness 80"},
		{&Look{}, "keyboard unchanged; rear unchanged"},
		{&Look{Keyboard: &keyboard.State{Mode: "off"}}, "keyboard off brightness ?; rear unchanged"},
		{&Look{Keyboard: &keyboard.State{Mode: "effect", Name: "static"}, Rear: &Rear{Mode: "static"}}, "keyboard static brightness ?; rear static brightness None"},
		{&Look{Keyboard: &keyboard.State{Mode: "effect", Name: "static", Speed: keyboard.Int(0), Brightness: keyboard.Int(0)}}, "keyboard static brightness 0; rear unchanged"},
	} {
		if got := Describe(tc.look); got != tc.want {
			t.Errorf("Describe = %q, want %q", got, tc.want)
		}
	}
}

func TestApplyEffectAndRear(t *testing.T) {
	f := newFixture(t)
	done, err := f.s.Apply(waveLook)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(done, []string{"keyboard: effect wave", "rear: breathing"}) {
		t.Errorf("%v", done)
	}
	if got := f.kb.Last(); got.Name != "wave" || got.Color != "00ff00" || f.sim.Brightness() != 20 {
		t.Errorf("keyboard %+v", got)
	}
	want := hw.RearOpts{Mode: "breathing", Color: "ff0000", Speed: 2, Brightness: 80, Direction: 1, Verify: true}
	if len(f.rear.got) != 1 || !reflect.DeepEqual(f.rear.got[0], want) {
		t.Errorf("rear %+v want %+v", f.rear.got, want)
	}
}

func TestApplyPerKeyAndNative(t *testing.T) {
	f := newFixture(t)
	native, _ := f.s.Load("native")
	done, err := f.s.Apply(native)
	if err != nil || !reflect.DeepEqual(done, []string{"keyboard: per-key colours", "rear: breathing"}) {
		t.Fatalf("%v %v", done, err)
	}
	cells := f.sim.LastCells()
	if cells[0] != (keymap.RGB{0, 0xAE, 0xC7}) || cells[127] != (keymap.RGB{}) || f.sim.Brightness() != 12 {
		t.Error("native keyboard look")
	}
	if o := f.rear.got[0]; o.Color != "3723c7" || o.Speed != 3 || o.Brightness != 100 || o.Direction != 1 || o.Zones != nil {
		t.Errorf("%+v", o)
	}
}

func TestApplyOffAndPartialLooks(t *testing.T) {
	f := newFixture(t)
	_ = f.kb.SetEffect("static", "ff0000", 5, 40, 0)
	done, err := f.s.Apply(&Look{Keyboard: &keyboard.State{Mode: "off"}})
	if err != nil || !reflect.DeepEqual(done, []string{"keyboard: off"}) {
		t.Fatalf("%v %v", done, err)
	}
	if f.sim.Brightness() != 0 || len(f.rear.got) != 0 {
		t.Errorf("brightness %d, rear calls %d", f.sim.Brightness(), len(f.rear.got))
	}
	done, _ = f.s.Apply(&Look{Keyboard: &keyboard.State{Mode: "effect", Name: "nosuch"}})
	if !reflect.DeepEqual(done, []string{"keyboard: nothing to restore"}) {
		t.Errorf("%v", done)
	}
	if done, err := f.s.Apply(&Look{}); err != nil || len(done) != 0 {
		t.Errorf("an empty look does nothing: %v %v", done, err)
	}
	done, _ = f.s.Apply(&Look{Rear: &Rear{Mode: "static", Color: "ffffff", Speed: keyboard.Int(1), Brightness: keyboard.Int(0), Zones: []string{"010203", "040506", "070809"}}})
	if !reflect.DeepEqual(done, []string{"rear: static"}) {
		t.Errorf("%v", done)
	}
	if o := f.rear.got[0]; o.Direction != 1 || o.Brightness != 0 || len(o.Zones) != 3 {
		t.Errorf("direction defaults to 1, brightness 0 is kept: %+v", o)
	}
}

func TestApplyErrors(t *testing.T) {
	f := newFixture(t)
	f.rear.err = firmware.Errorf("rear mode: static")
	if _, err := f.s.Apply(&Look{Rear: &Rear{Mode: "x", Color: "ffffff", Speed: keyboard.Int(1), Brightness: keyboard.Int(1)}}); err == nil || err.Error() != "rear mode: static" {
		t.Errorf("%v", err)
	}
	f.rear.err = nil
	for _, r := range []*Rear{
		{Mode: "static", Color: "ffffff", Brightness: keyboard.Int(1)}, // no speed
		{Mode: "static", Color: "ffffff", Speed: keyboard.Int(1)},      // no brightness: would silently switch the bar off
		{Mode: "static", Speed: keyboard.Int(1), Brightness: keyboard.Int(1)},
		{Color: "ffffff", Speed: keyboard.Int(1), Brightness: keyboard.Int(1)},
	} {
		if _, err := f.s.Apply(&Look{Rear: r}); err == nil || !isFirmwareError(err) {
			t.Errorf("%+v: %v", r, err)
		}
	}
	if len(f.rear.got) != 1 {
		t.Errorf("a malformed rear part must not reach the firmware (%d calls)", len(f.rear.got))
	}
	if _, err := f.s.Apply(&Look{Keyboard: &keyboard.State{Mode: "perkey", Cells: []string{"zz"}}}); err == nil {
		t.Error("corrupt cells")
	}
}

func TestBootLook(t *testing.T) {
	f := newFixture(t)
	if f.s.ReadBoot() != nil {
		t.Error("no boot look yet")
	}
	if err := f.s.WriteBoot(nil); err != nil {
		t.Errorf("clearing nothing is not an error: %v", err)
	}
	if err := f.s.WriteBoot(waveLook); err != nil {
		t.Fatal(err)
	}
	if got := f.s.ReadBoot(); !reflect.DeepEqual(got, waveLook) {
		t.Errorf("%+v", got)
	}
	if _, err := os.Stat(f.s.BootFile + ".tmp"); err == nil {
		t.Error("no temp file left")
	}
	if err := f.s.WriteBoot(nil); err != nil || f.s.ReadBoot() != nil {
		t.Errorf("clear: %v", err)
	}
	_ = os.MkdirAll(filepath.Dir(f.s.BootFile), 0o755)
	_ = os.WriteFile(f.s.BootFile, []byte("{nope"), 0o644)
	if f.s.ReadBoot() != nil {
		t.Error("a broken boot file reads as none")
	}
	_ = os.WriteFile(f.s.BootFile, []byte("[1,2]"), 0o644)
	if f.s.ReadBoot() != nil {
		t.Error("a boot file that is not a look reads as none")
	}
}

func TestIsEmpty(t *testing.T) {
	for _, tc := range []struct {
		l    *Look
		want bool
	}{
		{nil, true}, {&Look{}, true}, {&Look{Keyboard: &keyboard.State{}, Rear: &Rear{}}, true},
		{waveLook, false}, {&Look{Rear: &Rear{Mode: "static"}}, false}, {&Look{Keyboard: &keyboard.State{Mode: "off"}}, false},
	} {
		if tc.l.IsEmpty() != tc.want {
			t.Errorf("%+v", tc.l)
		}
	}
}

func TestDefaultBoot(t *testing.T) {
	t.Setenv("TRUESENSE_BOOT_LOOK", "")
	if DefaultBoot() != "/var/lib/truesense/boot-look.json" {
		t.Error(DefaultBoot())
	}
	t.Setenv("TRUESENSE_BOOT_LOOK", "/tmp/x.json")
	if DefaultBoot() != "/tmp/x.json" {
		t.Error(DefaultBoot())
	}
}

func FuzzParse(f *testing.F) {
	b, _ := json.Marshal(waveLook)
	f.Add(b)
	f.Add([]byte(`{"keyboard": null, "rear": null}`))
	f.Add([]byte(`{"keyboard": {"mode": "perkey", "cells": ["zz"]}}`))
	f.Add([]byte(`null`))
	f.Add([]byte(`[`))
	f.Fuzz(func(t *testing.T, data []byte) {
		l, err := Parse(data)
		if err != nil {
			return
		}
		_ = Describe(l)
		_ = l.IsEmpty()
		out, err := json.Marshal(l)
		if err != nil {
			t.Fatal(err)
		}
		again, err := Parse(out)
		if err != nil {
			t.Fatalf("re-parse: %v", err)
		}
		if out2, _ := json.Marshal(again); !bytes.Equal(out, out2) { // empty and absent lists are the same look
			t.Fatalf("encoding is not stable: %s vs %s", out, out2)
		}
		// applying whatever was parsed must fail cleanly or work, never panic
		fx := &fixture{}
		kb, _ := keyboard.NewSim(nil)
		fx.s = &Store{KB: kb, Rear: &fakeRear{}}
		_, _ = fx.s.Apply(l)
	})
}
