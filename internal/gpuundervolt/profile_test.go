package gpuundervolt

import (
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func TestBuiltins(t *testing.T) {
	if got := BuiltinNames(); !reflect.DeepEqual(got, []string{"native", "truesense", "mild", "cool", "efficient"}) {
		t.Fatalf("BuiltinNames = %v", got)
	}
	want := map[string][2]int{"native": {0, 0}, "truesense": {120, 2400}, "mild": {60, 2500}, "cool": {100, 2100}, "efficient": {150, 1800}}
	s := &Store{Dir: t.TempDir()}
	for n, v := range want {
		p, err := s.Load(n)
		if err != nil || p.Offset != v[0] || p.Cap != v[1] || p.Description == "" {
			t.Errorf("%s = %+v, %v", n, p, err)
		}
		if lvl, _ := Risk(p.Offset, p.Cap); lvl != Safe {
			t.Errorf("built-in %s is rated %s", n, lvl)
		}
	}
	if p, err := s.Load("stock"); err != nil || p.Offset != 0 {
		t.Errorf("stock = %+v, %v", p, err)
	}
}

func TestValidate(t *testing.T) {
	tests := []struct {
		name    string
		in      any
		want    Profile
		wantErr string
	}{
		{"valid", map[string]any{"offset": 120, "cap": 2400, "description": "d"}, Profile{"d", 120, 2400}, ""},
		{"cap defaults to none", map[string]any{"offset": 50}, Profile{"", 50, 0}, ""},
		{"edges", map[string]any{"offset": -300, "cap": 600}, Profile{"", -300, 600}, ""},
		{"upper edges", map[string]any{"offset": 250, "cap": 3300}, Profile{"", 250, 3300}, ""},
		{"floats truncate", map[string]any{"offset": 99.9, "cap": 2400.7}, Profile{"", 99, 2400}, ""},
		{"offset too high (Python test: 900)", map[string]any{"offset": 900, "cap": 2400}, Profile{}, "offset: -300..250 MHz"},
		{"offset 251", map[string]any{"offset": 251}, Profile{}, "offset: -300..250 MHz"},
		{"offset -301", map[string]any{"offset": -301}, Profile{}, "offset: -300..250 MHz"},
		{"cap 50 (Python test)", map[string]any{"offset": 100, "cap": 50}, Profile{}, "cap: 0 (none) or 600..3300 MHz"},
		{"cap 599", map[string]any{"offset": 0, "cap": 599}, Profile{}, "cap: 0 (none) or 600..3300 MHz"},
		{"cap 3301", map[string]any{"offset": 0, "cap": 3301}, Profile{}, "cap: 0 (none) or 600..3300 MHz"},
		{"cap negative", map[string]any{"offset": 0, "cap": -1}, Profile{}, "cap: 0 (none) or 600..3300 MHz"},
		{"cap as text", map[string]any{"offset": 0, "cap": "2400"}, Profile{}, "cap: 0 (none)"},
		{"cap null", map[string]any{"offset": 0, "cap": nil}, Profile{}, "cap: 0 (none)"},
		{"offset missing", map[string]any{"cap": 2400}, Profile{}, "offset: -300..250 MHz"},
		{"offset as bool", map[string]any{"offset": true}, Profile{}, "offset: -300..250 MHz"},
		{"offset null", map[string]any{"offset": nil}, Profile{}, "offset: -300..250 MHz"},
		{"not an object", []any{1}, Profile{}, "a GPU undervolt profile has 'offset'"},
		{"null", nil, Profile{}, "a GPU undervolt profile has 'offset'"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := Validate(tt.in)
			if tt.wantErr != "" {
				if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
					t.Fatalf("err = %v, want %q", err, tt.wantErr)
				}
				return
			}
			if err != nil || got != tt.want {
				t.Fatalf("got %+v, %v; want %+v", got, err, tt.want)
			}
		})
	}
}

func TestStoreRoundTripAndCopy(t *testing.T) {
	s := &Store{Dir: filepath.Join(t.TempDir(), "gpu")}
	if names, err := s.Names(); err != nil || !reflect.DeepEqual(names, BuiltinNames()) {
		t.Fatalf("Names = %v, %v", names, err)
	}
	// Python: gu.save("truesense", {"offset": 10, "cap": 0}) is refused.
	for _, n := range BuiltinNames() {
		if _, err := s.Save(n, Profile{Offset: 10}); err == nil || !strings.Contains(err.Error(), "built in and read-only") {
			t.Errorf("Save(%s): %v", n, err)
		}
		if err := s.Delete(n); err == nil || !strings.Contains(err.Error(), "cannot be deleted") {
			t.Errorf("Delete(%s): %v", n, err)
		}
	}
	saved, err := s.Save("mine", Profile{"my profile", 80, 2300})
	if err != nil {
		t.Fatal(err)
	}
	if got, err := s.Load("mine"); err != nil || got != saved {
		t.Fatalf("Load = %+v, %v", got, err)
	}
	c1, err := s.Copy("cool", "")
	c2, _ := s.Copy("cool", "")
	c3, _ := s.Copy("mine", "cool")
	if err != nil || c1 != "cool-copy" || c2 != "cool-copy-2" || c3 != "cool-2" {
		t.Errorf("copies = %q %q %q (%v)", c1, c2, c3, err)
	}
	if cp, _ := s.Load("cool-copy"); cp.Offset != 100 || cp.Cap != 2100 {
		t.Errorf("copy = %+v", cp)
	}
	names, _ := s.Names()
	if want := append(BuiltinNames(), "cool-2", "cool-copy", "cool-copy-2", "mine"); !reflect.DeepEqual(names, want) {
		t.Errorf("Names = %v, want %v", names, want)
	}
	if err := s.Delete("mine"); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Load("mine"); err == nil || !strings.Contains(err.Error(), "no GPU undervolt profile 'mine'") {
		t.Errorf("after delete: %v", err)
	}
	if err := s.Delete("mine"); err == nil || !strings.Contains(err.Error(), "no GPU undervolt profile 'mine'") {
		t.Errorf("second delete: %v", err)
	}
	if _, err := s.Save("bad", Profile{Offset: 900}); err == nil {
		t.Error("an out-of-range profile was saved")
	}
	if _, err := os.Stat(filepath.Join(s.Dir, "bad.json")); err == nil {
		t.Error("a rejected profile left a file")
	}
}

func TestStoreNamesAndBrokenFiles(t *testing.T) {
	dir := t.TempDir()
	s := &Store{Dir: dir}
	for _, n := range []string{"", "../x", "a b", strings.Repeat("n", 41)} {
		if _, err := s.Save(n, Profile{}); err == nil || !strings.Contains(err.Error(), "profile names use letters") {
			t.Errorf("Save(%q): %v", n, err)
		}
		if _, err := s.Load(n); err == nil {
			t.Errorf("Load(%q) accepted", n)
		}
	}
	for name, text := range map[string]string{"broken.json": "{", "wrong.json": `{"offset":999}`} {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(text), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := s.Load("broken"); err == nil || !strings.Contains(err.Error(), "not valid JSON") {
		t.Errorf("broken: %v", err)
	}
	if _, err := s.Load("wrong"); err == nil || !strings.Contains(err.Error(), "offset: -300..250 MHz") {
		t.Errorf("wrong: %v", err)
	}
}

func TestJSONKeysMatchPython(t *testing.T) {
	b, _ := json.Marshal(Profile{"d", 1, 2})
	if string(b) != `{"description":"d","offset":1,"cap":2}` {
		t.Errorf("Profile = %s", b)
	}
	b, _ = json.Marshal(State{5, [2]int{-9, 9}})
	if string(b) != `{"offset_mhz":5,"range_mhz":[-9,9]}` {
		t.Errorf("State = %s", b)
	}
	b, _ = json.Marshal(Applied{5, 6})
	if string(b) != `{"offset_mhz":5,"cap_mhz":6}` {
		t.Errorf("Applied = %s", b)
	}
}

func TestDefaultDir(t *testing.T) {
	t.Setenv("HOME", "/home/u")
	if got := DefaultDir(); got != "/home/u/.config/truesense/gpu-undervolt" {
		t.Errorf("DefaultDir = %q", got)
	}
}

func FuzzValidate(f *testing.F) {
	for _, s := range []string{`{"offset":120,"cap":2400}`, `{}`, `null`, `[]`, `{"offset":"x"}`, `{"offset":1e999}`, `{"offset":-300,"cap":600}`, `{"offset":0,"cap":0.5}`} {
		f.Add([]byte(s))
	}
	f.Fuzz(func(t *testing.T, data []byte) {
		p, err := Parse(data)
		if err != nil {
			return
		}
		if err := p.Check(); err != nil {
			t.Fatalf("accepted %+v but Check fails: %v", p, err)
		}
		b, _ := json.Marshal(p)
		if again, err := Parse(b); err != nil || again != p {
			t.Fatalf("round trip %+v -> %s -> %+v (%v)", p, b, again, err)
		}
	})
}
