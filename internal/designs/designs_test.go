package designs

import (
	"bytes"
	"encoding/json"
	"image"
	"image/color"
	"image/jpeg"
	"image/png"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/Order52/truesense/internal/keymap"
)

func newStore(t *testing.T) *Store { t.Helper(); return New(filepath.Join(t.TempDir(), "designs")) }

func hexCells(cv []keymap.RGB) []string {
	out := make([]string, len(cv))
	for i, c := range cv {
		out[i] = c.Hex()
	}
	return out
}

// Every design the Python tool ships must come out of Go as the same 128 colours.
func TestBuiltinDesignsMatchPython(t *testing.T) {
	b, err := os.ReadFile("testdata/golden.json")
	if err != nil {
		t.Fatal(err)
	}
	var golden map[string]struct {
		Design struct {
			Info   string            `json:"info"`
			Base   string            `json:"base"`
			Keys   map[string]string `json:"keys"`
			Cells  map[string]string `json:"cells"`
			Source string            `json:"source"`
		} `json:"design"`
		Cells []string `json:"cells"`
	}
	if err := json.Unmarshal(b, &golden); err != nil {
		t.Fatal(err)
	}
	s := newStore(t)
	all := s.LoadAll()
	if len(all) != len(golden) {
		t.Fatalf("%d designs, Python has %d", len(all), len(golden))
	}
	for name, g := range golden {
		d, ok := all[name]
		if !ok {
			t.Errorf("missing %s", name)
			continue
		}
		cv, err := CellsOf(s.Map, d)
		if err != nil || len(cv) != 128 {
			t.Errorf("%s: %d cells, %v", name, len(cv), err)
		}
		if !reflect.DeepEqual(hexCells(cv), g.Cells) {
			t.Errorf("%s: cells differ from Python", name)
		}
		if d.Info != g.Design.Info || d.Base != g.Design.Base || d.Source != g.Design.Source || len(d.Keys) != len(g.Design.Keys) {
			t.Errorf("%s: %+v vs %+v", name, d, g.Design)
		}
		for _, kc := range d.Keys {
			if g.Design.Keys[kc.Key] != kc.Color {
				t.Errorf("%s key %s", name, kc.Key)
			}
		}
	}
}

func TestUserFilesWinAndBrokenOnesAreSkipped(t *testing.T) {
	s := newStore(t)
	_ = os.MkdirAll(s.Dir, 0o755)
	write := func(name, body string) { _ = os.WriteFile(filepath.Join(s.Dir, name), []byte(body), 0o644) }
	write("gaming.json", `{"base": "ffffff"}`)
	write("broken.json", `{nope`)
	write("badkey.json", `{"keys": {"nokey": "ffffff"}}`)
	write("badcolor.json", `{"base": "12345"}`)
	write("badcell.json", `{"cells": {"128": "ffffff"}}`)
	write("badcell2.json", `{"cells": {"+5": "ffffff"}}`)
	write("notjson.txt", `{"base": "ffffff"}`)
	write("array.json", `[1]`)
	write("nullbase.json", `{"keys": {"w": 5}}`)
	write("mine.json", `{"info": "x", "keys": {"w": "010203"}, "cells": {"7": "040506"}}`)
	all := s.LoadAll()
	if all["gaming"].Source != SourceYours || all["gaming"].Base != "ffffff" || len(all["gaming"].Keys) != 0 {
		t.Errorf("a user file replaces the built-in design wholesale: %+v", all["gaming"])
	}
	for _, n := range []string{"broken", "badkey", "badcolor", "badcell", "badcell2", "notjson", "array", "nullbase"} {
		if _, ok := all[n]; ok {
			t.Errorf("%s must be skipped", n)
		}
	}
	if all["coding"].Source != SourceBuiltin || all["mine"].Source != SourceYours {
		t.Error("sources")
	}
	names := s.Names()
	if !reflect.DeepEqual(names, []string{"arrows", "coding", "ember", "gaming", "ice", "mine", "stealth"}) {
		t.Errorf("%v", names)
	}
}

func TestSaveAndGet(t *testing.T) {
	s := newStore(t)
	cells := make([]keymap.RGB, 128)
	for i := range cells {
		cells[i] = keymap.RGB{1, 2, 3}
	}
	p, err := s.Save("mine", cells)
	if err != nil || p != filepath.Join(s.Dir, "mine.json") {
		t.Fatalf("%q %v", p, err)
	}
	raw, _ := os.ReadFile(p)
	var f struct {
		Info  string            `json:"info"`
		Base  string            `json:"base"`
		Cells map[string]string `json:"cells"`
	}
	if err := json.Unmarshal(raw, &f); err != nil || f.Info != "saved from the keyboard" || f.Base != "000000" || len(f.Cells) != 126 || f.Cells["125"] != "010203" || f.Cells["126"] != "" {
		t.Errorf("%s %v", raw, err)
	}
	if !strings.HasPrefix(string(raw), "{\n \"info\"") {
		t.Errorf("indent 1 like the Python tool: %q", raw)
	}
	d, err := s.Get("mine")
	if err != nil {
		t.Fatal(err)
	}
	cv, _ := CellsOf(s.Map, d)
	if cv[7] != (keymap.RGB{1, 2, 3}) || cv[126] != (keymap.RGB{}) {
		t.Error("saved cells come back (126 and 127 stay black)")
	}
	if _, err := s.Get("ghost"); err == nil || err.Error() != "no design 'ghost' (see: truesense keyboard design list)" {
		t.Errorf("%v", err)
	}
	// a short list saves what there is
	if _, err := s.Save("short", cells[:3]); err != nil {
		t.Error(err)
	}
	for _, bad := range []string{"../x", "", "a/b", `a\b`, "a.b", strings.Repeat("x", 41)} {
		if _, err := s.Save(bad, nil); err == nil || err.Error() != "design name: letters, digits, - and _" {
			t.Errorf("%q: %v", bad, err)
		}
	}
	if _, err := s.Save(strings.Repeat("ä", 40), nil); err != nil {
		t.Errorf("40 characters, not bytes: %v", err)
	}
}

func TestOrderOfKeysDecidesOverlaps(t *testing.T) {
	m := keymap.Verified()
	w := m.Key("w")[0]
	d, err := Parse(m, []byte(`{"keys": {"wasd": "ff0000", "w": "00ff00"}}`))
	if err != nil {
		t.Fatal(err)
	}
	cv, _ := CellsOf(m, d)
	if cv[w] != (keymap.RGB{0, 255, 0}) {
		t.Error("the later entry wins")
	}
	d, _ = Parse(m, []byte(`{"keys": {"w": "00ff00", "wasd": "ff0000"}}`))
	cv, _ = CellsOf(m, d)
	if cv[w] != (keymap.RGB{255, 0, 0}) {
		t.Error("the later entry wins (reversed)")
	}
	// a repeated key keeps its first place with the last value, like a Python dict
	d, _ = Parse(m, []byte(`{"keys": {"w": "00ff00", "wasd": "ff0000", "w": "0000ff"}}`))
	cv, _ = CellsOf(m, d)
	if cv[w] != (keymap.RGB{255, 0, 0}) || len(d.Keys) != 2 {
		t.Errorf("%+v", d.Keys)
	}
	// single cells come after keys
	d, _ = Parse(m, []byte(`{"cells": {"07": "010203", "7": "040506"}, "keys": {"all": "ffffff"}}`))
	cv, _ = CellsOf(m, d)
	if cv[7] != (keymap.RGB{4, 5, 6}) || cv[8] != (keymap.RGB{255, 255, 255}) {
		t.Error("cells after keys, later cell wins")
	}
}

func TestParseCleansLikePython(t *testing.T) {
	m := keymap.Verified()
	long := strings.Repeat("é", 100)
	d, err := Parse(m, []byte(`{"info": "`+long+`", "keys": null, "cells": null}`))
	if err != nil {
		t.Fatal(err)
	}
	if len([]rune(d.Info)) != 80 || d.Base != "000000" || len(d.Keys) != 0 || len(d.Cells) != 0 {
		t.Errorf("%+v", d)
	}
	if d, _ := Parse(m, []byte(`{"info": 5}`)); d.Info != "5" {
		t.Errorf("info %q", d.Info)
	}
	if d, _ := Parse(m, []byte(`{"info": null}`)); d.Info != "None" {
		t.Errorf("info %q", d.Info)
	}
	if d, _ := Parse(m, []byte(`{"info": true}`)); d.Info != "True" {
		t.Errorf("info %q", d.Info)
	}
	// '#' is accepted in colours, kept as written
	if d, err := Parse(m, []byte(`{"base": "#AABBCC"}`)); err != nil || d.Base != "#AABBCC" {
		t.Errorf("%+v %v", d, err)
	}
	for _, bad := range []string{`[]`, `5`, `null`, `"x"`, `{"keys": {"w": "zz"}}`, `{"keys": []}`, `{"keys": {"w": 5}}`, `{"cells": {"x": "ffffff"}}`, `{"base": 5}`} {
		if _, err := Parse(m, []byte(bad)); err == nil {
			t.Errorf("%s accepted", bad)
		}
	}
}

func TestDesignJSONRoundTrip(t *testing.T) {
	m := keymap.Verified()
	for name, d := range Builtin() {
		c, err := Clean(m, d)
		if err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		b, err := json.Marshal(c)
		if err != nil {
			t.Fatal(err)
		}
		again, err := Parse(m, b)
		if err != nil || !reflect.DeepEqual(again, c) {
			t.Errorf("%s round trip: %v", name, err)
		}
	}
	var empty Design
	b, _ := json.Marshal(empty)
	if string(b) != `{"info":"","base":"","keys":{},"cells":{}}` {
		t.Errorf("%s", b)
	}
}

func TestDefaultDirHonoursXDG(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", "/x/cfg")
	if DefaultDir() != "/x/cfg/truesense/designs" {
		t.Error(DefaultDir())
	}
	t.Setenv("XDG_CONFIG_HOME", "")
	t.Setenv("HOME", "/home/u")
	if DefaultDir() != "/home/u/.config/truesense/designs" {
		t.Error(DefaultDir())
	}
}

func FuzzParse(f *testing.F) {
	m := keymap.Verified()
	f.Add([]byte(`{"base": "101018", "keys": {"wasd": "ff6a00"}, "cells": {"12": "ffffff"}}`))
	f.Add([]byte(`{"keys": {"w": 5}}`))
	f.Add([]byte(`{"cells": {"99999999999999999999": "ffffff"}}`))
	f.Add([]byte(`{"info": [1,2]}`))
	f.Add([]byte(`null`))
	f.Fuzz(func(t *testing.T, b []byte) {
		d, err := Parse(m, b)
		if err != nil {
			return
		}
		cv, err := CellsOf(m, d)
		if err != nil || len(cv) != keymap.NumCells {
			t.Fatalf("a design that parsed must paint: %v", err)
		}
		out, err := json.Marshal(d)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := Parse(m, out); err != nil {
			t.Fatalf("re-parse: %v", err)
		}
	})
}

// ---- images ----

func TestImageIsScaledToTheKeyGrid(t *testing.T) {
	var buf bytes.Buffer
	im := image.NewRGBA(image.Rect(0, 0, 40, 12))
	for y := 0; y < 12; y++ {
		for x := 0; x < 40; x++ {
			im.Set(x, y, color.RGBA{200, 10, 20, 255})
		}
	}
	if err := png.Encode(&buf, im); err != nil {
		t.Fatal(err)
	}
	cv, err := ImageCells(&buf)
	if err != nil {
		t.Fatal(err)
	}
	if len(cv) != 128 || cv[5] != (keymap.RGB{200, 10, 20}) || cv[125] != (keymap.RGB{200, 10, 20}) || cv[126] != (keymap.RGB{}) || cv[127] != (keymap.RGB{}) {
		t.Errorf("cells 5=%v 126=%v 127=%v", cv[5], cv[126], cv[127])
	}
	if _, err := ImageCellsFile(filepath.Join(t.TempDir(), "missing.png")); err == nil || !strings.HasPrefix(err.Error(), "cannot read the image: ") {
		t.Errorf("%v", err)
	}
}

// The pictures were scaled by Pillow's LANCZOS in the Python tool; the Go resampler must give the same bytes.
func TestImagesMatchPillow(t *testing.T) {
	b, err := os.ReadFile("testdata/image_golden.json")
	if err != nil {
		t.Fatal(err)
	}
	var golden map[string][]string
	if err := json.Unmarshal(b, &golden); err != nil {
		t.Fatal(err)
	}
	for name, want := range golden {
		t.Run(name, func(t *testing.T) {
			cv, err := ImageCellsFile(filepath.Join("testdata", name+".png"))
			if err != nil {
				t.Fatal(err)
			}
			got := hexCells(cv)
			diff := 0
			for i := range want {
				if got[i] != want[i] {
					diff++
					if diff < 4 {
						t.Errorf("cell %d: got %s want %s", i, got[i], want[i])
					}
				}
			}
			if diff > 0 {
				t.Errorf("%d of %d cells differ", diff, len(want))
			}
		})
	}
}

func TestOrientationOfTheGrid(t *testing.T) {
	// the picture's column c, row r lands on cell c*6+r
	im := image.NewRGBA(image.Rect(0, 0, 22, 6))
	im.Set(3, 4, color.RGBA{255, 0, 0, 255})
	var buf bytes.Buffer
	_ = png.Encode(&buf, im)
	cv, err := ImageCells(&buf)
	if err != nil || cv[3*6+4] != (keymap.RGB{255, 0, 0}) || cv[4*6+3] != (keymap.RGB{}) {
		t.Errorf("%v %v", cv[3*6+4], err)
	}
}

func TestJPEGIsDecoded(t *testing.T) {
	im := image.NewRGBA(image.Rect(0, 0, 44, 12))
	for y := 0; y < 12; y++ {
		for x := 0; x < 44; x++ {
			im.Set(x, y, color.RGBA{0, 200, 0, 255})
		}
	}
	var buf bytes.Buffer
	if err := jpeg.Encode(&buf, im, &jpeg.Options{Quality: 95}); err != nil {
		t.Fatal(err)
	}
	cv, err := ImageCells(&buf)
	if err != nil || cv[0][1] < 190 || cv[0][0] > 20 {
		t.Errorf("%v %v", cv[0], err)
	}
}

func TestBrokenAndHugeImagesAreRefused(t *testing.T) {
	for name, data := range map[string][]byte{
		"empty":     nil,
		"text":      []byte("not an image"),
		"truncated": []byte("\x89PNG\r\n\x1a\n\x00\x00\x00\x0dIHDR"),
	} {
		if _, err := ImageCells(bytes.NewReader(data)); err == nil || !strings.HasPrefix(err.Error(), "cannot read the image: ") {
			t.Errorf("%s: %v", name, err)
		}
	}
	// a valid header announcing 100000 x 100000 pixels must be refused before allocating
	huge := []byte("\x89PNG\r\n\x1a\n\x00\x00\x00\x0dIHDR\x00\x01\x86\xa0\x00\x01\x86\xa0\x08\x02\x00\x00\x00\x00\x00\x00\x00")
	if _, err := ImageCells(bytes.NewReader(huge)); err == nil {
		t.Error("huge picture accepted")
	}
}

func FuzzImageCells(f *testing.F) {
	var buf bytes.Buffer
	_ = png.Encode(&buf, image.NewRGBA(image.Rect(0, 0, 5, 5)))
	f.Add(buf.Bytes())
	f.Add([]byte("GIF89a"))
	f.Add([]byte("\xff\xd8\xff"))
	f.Fuzz(func(t *testing.T, b []byte) {
		cv, err := ImageCells(bytes.NewReader(b))
		if err == nil && len(cv) != keymap.NumCells {
			t.Fatalf("%d cells", len(cv))
		}
	})
}

func TestResizeIdentityWhenTheSizeAlreadyFits(t *testing.T) {
	src := make([]byte, 22*6*3)
	for i := range src {
		src[i] = byte(i * 7)
	}
	if got := resizeLanczos(src, 22, 6, 22, 6); !bytes.Equal(got, src) {
		t.Error("a 22 x 6 picture is used as it is")
	}
}
