package curves

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func customCurve() Curve {
	c := Defaults()
	c.CPU = []Point{{60, 0}, {70, 40}, {95, 100}}
	c.GPU = []Point{{55, 0}, {85, 100}}
	c.Description = "custom"
	return c
}

func TestStoreRoundTrip(t *testing.T) {
	s := &Store{Dir: filepath.Join(t.TempDir(), "curves")} // the directory does not exist yet
	names, err := s.Names()
	if err != nil || !reflect.DeepEqual(names, BuiltinNames()) {
		t.Fatalf("Names on a missing dir = %v, %v", names, err)
	}
	saved, err := s.Save("zeta", customCurve())
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.Save("alpha.v2", customCurve()); err != nil {
		t.Fatal(err)
	}
	got, err := s.Load("zeta")
	if err != nil || !reflect.DeepEqual(got, saved) {
		t.Fatalf("Load = %+v, %v; want %+v", got, err, saved)
	}
	names, _ = s.Names()
	if want := append(BuiltinNames(), "alpha.v2", "zeta"); !reflect.DeepEqual(names, want) {
		t.Errorf("Names = %v, want %v", names, want)
	}
	if err := s.Delete("zeta"); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Load("zeta"); err == nil || !strings.Contains(err.Error(), "no fan curve 'zeta'") {
		t.Errorf("Load after Delete: %v", err)
	}
	if err := s.Delete("zeta"); err == nil || !strings.Contains(err.Error(), "no fan curve 'zeta'") {
		t.Errorf("second Delete: %v", err)
	}
}

func TestStoreBuiltinsAreReadOnly(t *testing.T) {
	s := &Store{Dir: t.TempDir()}
	for _, n := range BuiltinNames() {
		if _, err := s.Save(n, customCurve()); err == nil || !strings.Contains(err.Error(), "is built in") {
			t.Errorf("Save(%q): %v", n, err)
		}
		if err := s.Delete(n); err == nil || !strings.Contains(err.Error(), "cannot be deleted") {
			t.Errorf("Delete(%q): %v", n, err)
		}
		if _, err := s.Load(n); err != nil {
			t.Errorf("Load(%q): %v", n, err)
		}
	}
	if entries, _ := os.ReadDir(s.Dir); len(entries) != 0 {
		t.Errorf("a refused save left files behind: %v", entries)
	}
}

func TestStoreRejectsBadNamesAndBadCurves(t *testing.T) {
	s := &Store{Dir: t.TempDir()}
	for _, n := range []string{"", "../x", "a/b", "a b", strings.Repeat("x", 41), "ü"} {
		if _, err := s.Save(n, customCurve()); err == nil || !strings.Contains(err.Error(), "curve names use letters") {
			t.Errorf("Save(%q): %v", n, err)
		}
		if _, err := s.Load(n); err == nil {
			t.Errorf("Load(%q) accepted", n)
		}
		if err := s.Delete(n); err == nil {
			t.Errorf("Delete(%q) accepted", n)
		}
	}
	if _, err := s.Save("40"+strings.Repeat("x", 38), customCurve()); err != nil {
		t.Errorf("a 40-character name is valid: %v", err)
	}
	weak := customCurve()
	weak.CPU = []Point{{50, 0}, {70, 50}}
	if _, err := s.Save("weak", weak); err == nil {
		t.Error("a weak curve was saved")
	}
	if _, err := os.Stat(filepath.Join(s.Dir, "weak.json")); err == nil {
		t.Error("a rejected curve left a file")
	}
}

func TestStoreLoadErrors(t *testing.T) {
	dir := t.TempDir()
	s := &Store{Dir: dir}
	write := func(name, text string) {
		t.Helper()
		if err := os.WriteFile(filepath.Join(dir, name), []byte(text), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	write("broken.json", "{not json")
	write("wrong.json", `{"cpu":[[50,0],[70,50]],"gpu":[[50,0],[85,100]]}`)
	write("notjson.txt", "x")
	write("native.json", "{}") // shadowed by the built-in
	if _, err := s.Load("broken"); err == nil || !strings.Contains(err.Error(), "not valid JSON") {
		t.Errorf("broken: %v", err)
	}
	if _, err := s.Load("wrong"); err == nil || !strings.Contains(err.Error(), "must reach 100") {
		t.Errorf("wrong: %v", err)
	}
	if _, err := s.Load("missing"); err == nil || !strings.Contains(err.Error(), "no fan curve 'missing'") {
		t.Errorf("missing: %v", err)
	}
	names, _ := s.Names()
	if want := append(BuiltinNames(), "broken", "wrong"); !reflect.DeepEqual(names, want) {
		t.Errorf("Names = %v, want %v (non-.json files and built-in shadows are not listed)", names, want)
	}
}

func TestDefaultDir(t *testing.T) {
	t.Setenv("HOME", "/home/someone")
	if got := DefaultDir(); got != "/home/someone/.config/truesense/curves" {
		t.Errorf("DefaultDir = %q", got)
	}
}
