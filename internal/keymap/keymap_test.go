package keymap

import (
	"encoding/json"
	"errors"
	"os"
	"reflect"
	"sort"
	"testing"

	"github.com/Order52/truesense/internal/firmware"
)

type golden struct {
	Names    map[string][]int `json:"names"`
	Groups   map[string][]int `json:"groups"`
	HSV      [][]any          `json:"hsv"`
	Gradient [][3]int         `json:"gradient"`
	Rainbow  [][3]int         `json:"rainbow"`
}

func loadGolden(t *testing.T) golden {
	t.Helper()
	b, err := os.ReadFile("testdata/golden.json")
	if err != nil {
		t.Fatal(err)
	}
	var g golden
	if err := json.Unmarshal(b, &g); err != nil {
		t.Fatal(err)
	}
	return g
}

func sorted(c []int) []int { c = append([]int(nil), c...); sort.Ints(c); return c }

// The Go table must equal the one the Python implementation builds from the same capture.
func TestNamesAndGroupsMatchPython(t *testing.T) {
	g := loadGolden(t)
	m := Verified()
	if got := m.Names(); !reflect.DeepEqual(got, g.Names) {
		for k, v := range g.Names {
			if !reflect.DeepEqual(got[k], v) {
				t.Errorf("name %q: got %v want %v", k, got[k], v)
			}
		}
		t.Fatalf("names differ (%d vs %d)", len(got), len(g.Names))
	}
	for name, want := range g.Groups {
		if got := m.Groups()[name]; !reflect.DeepEqual(sorted(got), sorted(want)) {
			t.Errorf("group %q: got %v want %v", name, got, want)
		}
	}
}

func TestVerifiedCorrections(t *testing.T) {
	m := Verified()
	for name, cell := range map[string]int{"esc": 2, "caps": 5, "minus": 70, "w": m.Key("w")[0]} {
		if got := m.Key(name); len(got) == 0 || got[0] != cell {
			t.Errorf("%s = %v, want cell %d first", name, got, cell)
		}
	}
	if !reflect.DeepEqual(m.Key("enter"), m.Key("return")) && len(m.Key("return")) > 0 {
		t.Error("enter must alias return")
	}
	if len(m.Key("enter")) == 0 || len(m.Key("win")) == 0 {
		t.Error("enter and win must resolve")
	}
}

func TestCellsFor(t *testing.T) {
	m := Verified()
	for _, tc := range []struct {
		in   string
		want []int
		bad  bool
	}{
		{in: "cell0", want: []int{0}},
		{in: "CELL127", want: []int{127}},
		{in: "cell128", bad: true},
		{in: "cell99999999999999999999999", bad: true},
		{in: "nosuchkey", bad: true},
		{in: "", bad: true},
		{in: "ESC", want: []int{2}},
	} {
		got, err := m.CellsFor(tc.in)
		if tc.bad {
			var fe *firmware.Error
			if !errors.As(err, &fe) {
				t.Errorf("%q: want a firmware error, got %v", tc.in, err)
			}
			continue
		}
		if err != nil || !reflect.DeepEqual(got, tc.want) {
			t.Errorf("%q = %v, %v; want %v", tc.in, got, err, tc.want)
		}
	}
	if all, _ := m.CellsFor("all"); len(all) != LitCells {
		t.Errorf("all has %d cells", len(all))
	}
	if w, _ := m.CellsFor("wasd"); len(w) != 4 {
		t.Errorf("wasd = %v", w)
	}
	// Mutating a result must not corrupt the table.
	a, _ := m.CellsFor("arrows")
	a[0] = 999
	if b, _ := m.CellsFor("arrows"); b[0] == 999 {
		t.Error("CellsFor leaked an internal slice")
	}
}

func TestParseHex(t *testing.T) {
	for _, tc := range []struct {
		in   string
		want RGB
		bad  bool
	}{
		{"ff0080", RGB{255, 0, 128}, false},
		{"#FF0080", RGB{255, 0, 128}, false},
		{"##00aec7", RGB{0, 0xAE, 0xC7}, false},
		{"fff", RGB{}, true},
		{"gggggg", RGB{}, true},
		{"ff00800", RGB{}, true},
		{"", RGB{}, true},
	} {
		got, err := ParseHex(tc.in)
		if (err != nil) != tc.bad || got != tc.want {
			t.Errorf("ParseHex(%q) = %v, %v", tc.in, got, err)
		}
	}
	if (RGB{1, 2, 255}).Hex() != "0102ff" {
		t.Error("Hex format")
	}
}

func TestHSVMatchesPython(t *testing.T) {
	g := loadGolden(t)
	for _, row := range g.HSV {
		h := row[0].(float64)
		var want RGB
		for i, v := range row[1].([]any) {
			want[i] = uint8(v.(float64))
		}
		if got := HSV(h); got != want {
			t.Errorf("HSV(%v) = %v, want %v", h, got, want)
		}
	}
}

func TestPicturesMatchPython(t *testing.T) {
	g := loadGolden(t)
	cmp := func(name string, got []RGB, want [][3]int) {
		t.Helper()
		if len(got) != NumCells {
			t.Fatalf("%s has %d cells", name, len(got))
		}
		for i, w := range want {
			if got[i] != (RGB{uint8(w[0]), uint8(w[1]), uint8(w[2])}) {
				t.Errorf("%s[%d] = %v, want %v", name, i, got[i], w)
			}
		}
		for i := LitCells; i < NumCells; i++ {
			if got[i] != (RGB{}) {
				t.Errorf("%s[%d] must stay black", name, i)
			}
		}
	}
	cmp("gradient", Gradient(RGB{255, 51, 0}, RGB{102, 0, 255}), g.Gradient)
	cmp("rainbow", Rainbow(), g.Rainbow)
	cv := Canvas(RGB{1, 2, 3})
	if cv[0] != (RGB{1, 2, 3}) || cv[125] != (RGB{1, 2, 3}) || cv[126] != (RGB{}) {
		t.Error("canvas fills cells 0..125 only")
	}
}

func TestLerpRoundsHalvesToEven(t *testing.T) {
	// Python's round(0.5) is 0 and round(1.5) is 2; 0.3.x frames depend on it.
	if got := Lerp(RGB{0, 1, 0}, RGB{1, 2, 0}, 0.5); got != (RGB{0, 2, 0}) {
		t.Errorf("Lerp = %v", got)
	}
}

func TestEmbeddedMapParses(t *testing.T) {
	if _, err := parseMap(verifiedJSON); err != nil {
		t.Fatal(err)
	}
}

func FuzzParseHex(f *testing.F) {
	for _, s := range []string{"ff0000", "#00AEC7", "", "zzzzzz", "##", "ff00", "0x1234", "\x00\x00\x00\x00\x00\x00"} {
		f.Add(s)
	}
	f.Fuzz(func(t *testing.T, s string) {
		c, err := ParseHex(s)
		if err != nil {
			return
		}
		if again, err := ParseHex(c.Hex()); err != nil || again != c {
			t.Fatalf("round trip of %q failed: %v %v", s, again, err)
		}
	})
}

func FuzzParseMap(f *testing.F) {
	f.Add(verifiedJSON)
	f.Add([]byte(`{"cells":{"3":{"label":null}}}`))
	f.Add([]byte(`{"cells":{"x":{}}}`))
	f.Fuzz(func(t *testing.T, b []byte) {
		m, err := parseMap(b)
		if err != nil {
			return
		}
		for _, name := range []string{"all", "wasd", "numpad", "nosuch"} {
			_, _ = m.CellsFor(name)
		}
	})
}
