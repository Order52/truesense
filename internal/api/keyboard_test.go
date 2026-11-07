package api_test

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/Order52/truesense/internal/api"
	"github.com/Order52/truesense/internal/keyboard"
	"github.com/Order52/truesense/internal/keymap"
)

func TestKeyboardStatusStartsWithAnEmptyLook(t *testing.T) {
	_, a := machine(t)
	st, err := a.KeyboardStatus()
	must(t, err)
	b, _ := json.Marshal(st)
	if string(b) != `{"look":{},"brightness":25,"max":50,"backlight_timeout":true,"lighting_profiles":[]}` {
		t.Fatalf("%s", b)
	}
}

func TestKeyboardEffectBrightnessRules(t *testing.T) {
	f, a := machine(t)
	must(t, a.SetKeyboardEffect(api.EffectSpec{Name: "static", Color: "ff0000", Speed: 5}))
	if f.S.KB.Last().BrightnessOr(-1) != 25 {
		t.Fatalf("an effect keeps the keyboard's own brightness: %+v", f.S.KB.Last())
	}
	must(t, a.SetKeyboardEffect(api.EffectSpec{Name: "neon", Color: "ffffff", Speed: 5}))
	if f.S.KB.Last().BrightnessOr(-1) != 50 {
		t.Fatalf("neon without a brightness is 50: %+v", f.S.KB.Last())
	}
	must(t, a.SetKeyboardEffect(api.EffectSpec{Name: "wave", Speed: 5, Brightness: ptr(40)}))
	if f.S.KB.Last().BrightnessOr(-1) != 40 {
		t.Fatalf("%+v", f.S.KB.Last())
	}
	must(t, a.SetKeyboardEffect(api.EffectSpec{Name: "operatingmode", Color: "00aec7"}))
	if f.S.KB.Last().Mode != keyboard.ModeOperating {
		t.Fatalf("%+v", f.S.KB.Last())
	}
	if err := a.SetKeyboardEffect(api.EffectSpec{Name: "disco", Color: "ffffff", Speed: 5}); err == nil {
		t.Fatal("unknown effect")
	}
	must(t, a.SetKeyboardEffect(api.EffectSpec{Name: "static", Color: "ff0000", Speed: 5}))
	must(t, a.SetKeyboardColor("00ff00"))
	if f.S.KB.Last().Color != "00ff00" {
		t.Fatal("colour")
	}
	wantErr(t, a.SetKeyboardColor("zz"), "")
	wantErr(t, a.SetKeyboardSpeed(3), "no animation speed")
	must(t, a.SetKeyboardEffect(api.EffectSpec{Name: "breathing", Color: "ff0000", Speed: 5}))
	must(t, a.SetKeyboardSpeed(3))
	wantErr(t, a.SetKeyboardSpeed(13), "")
	must(t, a.SetKeyboardBrightness(10))
	if b, _ := a.KeyboardBrightness(); b != 10 {
		t.Fatal(b)
	}
}

func TestPaintingKeys(t *testing.T) {
	f, a := machine(t)
	n, err := a.PaintKeys([]string{"w=ff0000", "esc=00ff00"}, nil)
	must(t, err)
	if n != 2 {
		t.Fatal(n)
	}
	cells := f.Keyboard.LastCells()
	w, _ := keymap.Verified().CellsFor("w")
	if cells[w[0]] != (keymap.RGB{255, 0, 0}) {
		t.Fatalf("w: %v", cells[w[0]])
	}
	_, err = a.PaintKeys([]string{"w"}, nil)
	wantErr(t, err, "'w': use key=RRGGBB")
	_, err = a.PaintKeys([]string{"nokey=ff0000"}, nil)
	if err == nil {
		t.Fatal("unknown key")
	}
	must(t, a.SetKeyColors([]string{"wasd=ff0000"}, "101010", ptr(20)))
	if b := f.Keyboard.Brightness(); b != 20 {
		t.Fatalf("brightness %d", b)
	}
	wantErr(t, a.SetKeyColors([]string{"wasd=ff0000"}, "bogus", nil), "")
	must(t, a.FillKeyboard("0000ff", nil))
	must(t, a.GradientKeyboard("ff0000", "0000ff", nil))
	must(t, a.RainbowKeyboard(nil))
	wantErr(t, a.FillKeyboard("nope", nil), "")
	must(t, a.RestoreKeyboard())
	if c := f.Keyboard.LastCells()[0]; c != (keymap.RGB{0x00, 0xAE, 0xC7}) {
		t.Fatalf("stock cyan expected, %v", c)
	}
	if f.Keyboard.Brightness() != 20 {
		t.Fatal("restore keeps the user's brightness")
	}
}

func TestDesignsAndImages(t *testing.T) {
	f, a := machine(t)
	ds := a.Designs()
	if ds["gaming"].Source != "built-in" || ds["gaming"].Info == "" {
		t.Fatalf("%+v", ds["gaming"])
	}
	wantErr(t, a.ApplyDesign("", nil), "give a design name")
	must(t, a.ApplyDesign("gaming", nil))
	path, err := a.SaveDesign("mine")
	must(t, err)
	if _, err := os.Stat(path); err != nil {
		t.Fatal(err)
	}
	if a.Designs()["mine"].Source != "yours" {
		t.Fatal("saved design")
	}
	wantErr(t, a.ApplyDesign("nope", nil), "")
	if err := a.ImageKeyboard(filepath.Join(f.Dir, "missing.png"), nil); err == nil {
		t.Fatal("missing image")
	}
}

func TestKeyNamesAndLayout(t *testing.T) {
	_, a := machine(t)
	k := a.ListKeys()
	if len(k.Keys) < 80 || len(k.Groups) < 5 {
		t.Fatalf("%d keys, %d groups", len(k.Keys), len(k.Groups))
	}
	l := a.KeyLayout()
	cells := l.Keys["esc"]
	if len(cells) != 1 || cells[0].Cell != cells[0].Col*6+cells[0].Row {
		t.Fatalf("%+v", cells)
	}
}

func TestKeyboardFX(t *testing.T) {
	f, a := machine(t)
	must(t, a.SetKeyboardFX(api.KeyboardFXSpec{Mode: "wave", Speed: 4, Keys: "wasd, arrows"}))
	if f.Keyboard.Frames() == 0 {
		t.Fatal("the starting picture is uploaded first")
	}
	var fx struct {
		Mode  string `json:"mode"`
		Speed int    `json:"speed"`
		Keys  []int  `json:"keys"`
	}
	must(t, f.S.User.GetInto("perkey_fx", &fx))
	if fx.Mode != "wave" || fx.Speed != 4 || len(fx.Keys) < 8 {
		t.Fatalf("%+v", fx)
	}
	must(t, a.SetKeyboardFX(api.KeyboardFXSpec{Mode: "off"}))
	wantErr(t, a.SetKeyboardFX(api.KeyboardFXSpec{Mode: "wave", Speed: 4, Keys: "nokey"}), "")
	wantErr(t, a.SetKeyboardFX(api.KeyboardFXSpec{Mode: "wave", Speed: 99}), "")
}

func TestLightingProfiles(t *testing.T) {
	_, a := machine(t)
	r, err := a.LightingProfile("list", "", nil)
	must(t, err)
	if r.Names == nil || len(r.Names) != 0 {
		t.Fatalf("an empty list is not null: %#v", r.Names)
	}
	r, err = a.LightingProfile("save", "red", []string{"fill", "ff0000"})
	must(t, err)
	if r.Message == "" {
		t.Fatal("a message is expected")
	}
	if names := a.LightingProfileNames(); len(names) != 1 || names[0] != "red" {
		t.Fatal(names)
	}
	must(t, a.ApplyLightingProfile("red"))
	if _, err := a.LightingProfile("load", "nope", nil); err == nil {
		t.Fatal("unknown profile")
	}
}

func TestKeyboardLastReportsOff(t *testing.T) {
	_, a := machine(t)
	if l := a.KeyboardLast(); l.Name != "" {
		t.Fatalf("nothing is remembered yet: %+v", l)
	}
	must(t, a.ApplyKeyboardLook(api.KeyboardLook{Name: "wave", Speed: 5, Brightness: 30}))
	if l := a.KeyboardLast(); l.Name != "wave" || l.Brightness != 30 || l.Speed != 5 {
		t.Fatalf("%+v", l)
	}
	must(t, a.ApplyKeyboardLook(api.KeyboardLook{Name: "off"}))
	if l := a.KeyboardLast(); l.Name != "off" {
		t.Fatalf("%+v", l)
	}
	// choosing an effect brings the light back
	must(t, a.ApplyKeyboardLook(api.KeyboardLook{Name: "static", Color: "ff0000", Speed: 5, Brightness: 30}))
	if l := a.KeyboardLast(); l.Name != "static" {
		t.Fatalf("%+v", l)
	}
}
