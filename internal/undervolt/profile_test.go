package undervolt

import (
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func TestBuiltins(t *testing.T) {
	if got := BuiltinNames(); !reflect.DeepEqual(got, []string{"native", "truesense", "mild", "balanced", "aggressive"}) {
		t.Fatalf("BuiltinNames = %v", got)
	}
	want := map[string][2]int{"native": {0, 0}, "truesense": {-50, -40}, "mild": {-30, -30}, "balanced": {-70, -50}, "aggressive": {-100, -80}}
	s := &Store{Dir: t.TempDir()}
	for n, v := range want {
		p, err := s.Load(n)
		if err != nil || p.Core != v[0] || p.Cache != v[1] || p.Description == "" {
			t.Errorf("%s = %+v, %v", n, p, err)
		}
	}
}

// Python: self.assertEqual(undervolt.load("stock")["core"], 0).
func TestStockIsTheOldNameOfNative(t *testing.T) {
	p, err := (&Store{Dir: t.TempDir()}).Load("stock")
	if err != nil || p.Core != 0 || p.Cache != 0 {
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
		{"valid", map[string]any{"core": -50, "cache": -40, "description": "d"}, Profile{"d", -50, -40}, ""},
		{"zero", map[string]any{"core": 0, "cache": 0}, Profile{"", 0, 0}, ""},
		{"limits", map[string]any{"core": -120, "cache": -120}, Profile{"", -120, -120}, ""},
		{"floats truncate", map[string]any{"core": -50.9, "cache": -0.5}, Profile{"", -50, 0}, ""},
		{"unknown keys dropped", map[string]any{"core": -1, "cache": -1, "x": 1}, Profile{"", -1, -1}, ""},
		{"beyond the limit", map[string]any{"core": -121, "cache": -40}, Profile{}, "core: 0 to -120 mV"},
		{"cache beyond the limit", map[string]any{"core": -1, "cache": -121}, Profile{}, "cache: 0 to -120 mV"},
		{"positive core", map[string]any{"core": 1, "cache": 0}, Profile{}, "core: 0 to -120 mV"},
		{"positive cache", map[string]any{"core": 0, "cache": 0.5}, Profile{}, "cache: 0 to -120 mV"},
		{"missing core", map[string]any{"cache": -10}, Profile{}, "core: 0 to -120 mV"},
		{"missing cache", map[string]any{"core": -10}, Profile{}, "cache: 0 to -120 mV"},
		{"core as text", map[string]any{"core": "-50", "cache": 0}, Profile{}, "core: 0 to -120 mV"},
		{"core as bool", map[string]any{"core": false, "cache": 0}, Profile{}, "core: 0 to -120 mV"},
		{"core null", map[string]any{"core": nil, "cache": 0}, Profile{}, "core: 0 to -120 mV"},
		{"not an object", []any{-50}, Profile{}, "an undervolt profile has 'core' and 'cache'"},
		{"null", nil, Profile{}, "an undervolt profile has 'core' and 'cache'"},
		{"text", "x", Profile{}, "an undervolt profile has 'core' and 'cache'"},
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
	s := &Store{Dir: filepath.Join(t.TempDir(), "undervolt")}
	if names, err := s.Names(); err != nil || !reflect.DeepEqual(names, BuiltinNames()) {
		t.Fatalf("Names = %v, %v", names, err)
	}
	// Python: undervolt.save("truesense", ...) is refused, built-ins are read-only.
	for _, n := range BuiltinNames() {
		if _, err := s.Save(n, Profile{Core: -10, Cache: -10}); err == nil || !strings.Contains(err.Error(), "built in and read-only") {
			t.Errorf("Save(%s): %v", n, err)
		}
		if err := s.Delete(n); err == nil || !strings.Contains(err.Error(), "cannot be deleted") {
			t.Errorf("Delete(%s): %v", n, err)
		}
	}
	saved, err := s.Save("mine", Profile{"my profile", -45, -35})
	if err != nil {
		t.Fatal(err)
	}
	got, err := s.Load("mine")
	if err != nil || got != saved {
		t.Fatalf("Load = %+v, %v", got, err)
	}
	c1, err := s.Copy("mild", "")
	c2, _ := s.Copy("mild", "")
	c3, _ := s.Copy("mine", "mine")
	if err != nil || c1 != "mild-copy" || c2 != "mild-copy-2" || c3 != "mine-2" {
		t.Errorf("copies = %q %q %q (%v)", c1, c2, c3, err)
	}
	if cp, _ := s.Load("mild-copy"); cp.Core != -30 || cp.Description != "Very safe: small offset" {
		t.Errorf("copy = %+v", cp)
	}
	if cp, err := s.Copy("stock", "mystock"); err != nil || cp != "mystock" {
		t.Errorf("copying the old name: %q, %v", cp, err)
	}
	names, _ := s.Names()
	if want := append(BuiltinNames(), "mild-copy", "mild-copy-2", "mine", "mine-2", "mystock"); !reflect.DeepEqual(names, want) {
		t.Errorf("Names = %v, want %v", names, want)
	}
	if err := s.Delete("mine"); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Load("mine"); err == nil || !strings.Contains(err.Error(), "no undervolt profile 'mine'") {
		t.Errorf("after delete: %v", err)
	}
	if err := s.Delete("mine"); err == nil || !strings.Contains(err.Error(), "no undervolt profile 'mine'") {
		t.Errorf("second delete: %v", err)
	}
	if _, err := s.Copy("ghost", ""); err == nil {
		t.Error("copy of a missing profile")
	}
	if _, err := s.Save("bad", Profile{Core: -200}); err == nil {
		t.Error("an out-of-range profile was saved")
	}
	if _, err := os.Stat(filepath.Join(s.Dir, "bad.json")); err == nil {
		t.Error("a rejected profile left a file")
	}
}

func TestStoreNamesAndBrokenFiles(t *testing.T) {
	dir := t.TempDir()
	s := &Store{Dir: dir}
	for _, n := range []string{"", "../x", "a b", strings.Repeat("n", 41), "a/b"} {
		if _, err := s.Save(n, Profile{}); err == nil || !strings.Contains(err.Error(), "profile names use letters") {
			t.Errorf("Save(%q): %v", n, err)
		}
		if _, err := s.Load(n); err == nil {
			t.Errorf("Load(%q) accepted", n)
		}
		if err := s.Delete(n); err == nil {
			t.Errorf("Delete(%q) accepted", n)
		}
	}
	for name, text := range map[string]string{"broken.json": "{", "wrong.json": `{"core":-500,"cache":0}`, "x.txt": ""} {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(text), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := s.Load("broken"); err == nil || !strings.Contains(err.Error(), "not valid JSON") {
		t.Errorf("broken: %v", err)
	}
	if _, err := s.Load("wrong"); err == nil || !strings.Contains(err.Error(), "core: 0 to -120 mV") {
		t.Errorf("wrong: %v", err)
	}
}

func TestJSONKeysMatchPython(t *testing.T) {
	b, _ := json.Marshal(Profile{"d", -1, -2})
	if string(b) != `{"description":"d","core":-1,"cache":-2}` {
		t.Errorf("Profile JSON = %s", b)
	}
	b, _ = json.Marshal(Offsets{-3, -4})
	if string(b) != `{"core_mv":-3,"cache_mv":-4}` {
		t.Errorf("Offsets JSON = %s", b)
	}
}

func TestDefaultDir(t *testing.T) {
	t.Setenv("HOME", "/home/u")
	if got := DefaultDir(); got != "/home/u/.config/truesense/undervolt" {
		t.Errorf("DefaultDir = %q", got)
	}
}

func FuzzValidate(f *testing.F) {
	for _, s := range []string{`{"core":-50,"cache":-40}`, `{}`, `null`, `[]`, `{"core":"x","cache":0}`, `{"core":-1e999,"cache":0}`, `{"core":-0.5,"cache":-120}`} {
		f.Add([]byte(s))
	}
	f.Fuzz(func(t *testing.T, data []byte) {
		p, err := Parse(data)
		if err != nil {
			return
		}
		if p.Core > 0 || p.Core < -MaxMV || p.Cache > 0 || p.Cache < -MaxMV {
			t.Fatalf("accepted %+v", p)
		}
		b, _ := json.Marshal(p)
		if again, err := Parse(b); err != nil || again != p {
			t.Fatalf("round trip %+v -> %s -> %+v (%v)", p, b, again, err)
		}
	})
}
