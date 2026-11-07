package keybinds

import (
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/Order52/truesense/internal/config"
)

func newStore(t *testing.T) *Store {
	t.Helper()
	d := t.TempDir()
	return &Store{
		Dir:        filepath.Join(d, "keys"),
		ConfigPath: filepath.Join(d, "config.toml"),
		LoadConfig: func() (*config.Config, error) { return config.Load("", filepath.Join(d, "config.toml")) },
	}
}

func TestBuiltinsAreValidAndMatchThePythonSets(t *testing.T) {
	s := newStore(t)
	want := map[string][5]string{
		"native":  {"exec:truesense gui", "none", "none", "none", "none"},
		"default": {"power-next", "output-toggle", "none", "fan-max-toggle", "none"},
		"gaming":  {"power-cycle:performance,turbo", "fan-max-toggle", "keyboard-effect-next", "auto-next", "mic-toggle"},
		"work":    {"power-cycle:eco,quiet,balanced", "output-toggle", "mic-toggle", "touchpad-toggle", "none"},
		"lights":  {"keyboard-effect-next", "rear-effect-next", "keyboard:toggle", "rear:toggle", "none"},
	}
	if got := s.Names(); !reflect.DeepEqual(got, BuiltinNames()) || len(got) != len(want) {
		t.Fatalf("names %v", got)
	}
	for name, w := range want {
		p, err := s.Load(name)
		if err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		if got := [5]string{p.Tap, p.Double, p.Triple, p.Long, p.FnEsc}; got != w {
			t.Errorf("%s: %v, want %v", name, got, w)
		}
		if p.LongMS != 800 || p.MultiMS != 450 || p.Description == "" {
			t.Errorf("%s: %+v", name, p)
		}
	}
}

func TestSaveLoadUseDeleteLikeTheWindowDoes(t *testing.T) { // tests/test_gui.py key profile test
	s := newStore(t)
	if _, err := s.Save("default", Profile{}); err == nil || !strings.Contains(err.Error(), "built in and read-only") {
		t.Errorf("a built-in must be read-only: %v", err)
	}
	p, _ := s.Load("default")
	p.Tap = "power:turbo"
	p.LongMS = 700
	if _, err := s.Save("mine", p); err != nil {
		t.Fatal(err)
	}
	if got, err := s.Load("mine"); err != nil || got.Tap != "power:turbo" || got.LongMS != 700 {
		t.Fatalf("%+v, %v", got, err)
	}
	if got := s.Names(); got[len(got)-1] != "mine" || len(got) != 6 {
		t.Errorf("names %v", got)
	}
	if _, err := s.Use("mine"); err != nil {
		t.Fatal(err)
	}
	cur, err := s.Current()
	if err != nil || cur.Tap != "power:turbo" || cur.Profile != "mine" || cur.LongMS != 700 || cur.Double != "output-toggle" {
		t.Fatalf("%+v, %v", cur, err)
	}
	if _, err := s.Use("default"); err != nil {
		t.Fatal(err)
	}
	if cur, _ := s.Current(); cur.Tap != "power-next" || cur.Profile != "default" {
		t.Errorf("%+v", cur)
	}
	if err := s.Delete("mine"); err != nil {
		t.Fatal(err)
	}
	if err := s.Delete("mine"); err == nil || err.Error() != "no key profile 'mine'" {
		t.Errorf("%v", err)
	}
	if err := s.Delete("gaming"); err == nil || !strings.Contains(err.Error(), "built in and cannot be deleted") {
		t.Errorf("%v", err)
	}
}

func TestUseKeepsTheRestOfTheUserConfig(t *testing.T) {
	s := newStore(t)
	if err := os.WriteFile(s.ConfigPath, []byte("[audio]\nnotify = false\n\n[keys]\npower_cycle = [\"eco\", \"turbo\"]\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Use("gaming"); err != nil {
		t.Fatal(err)
	}
	cfg, err := s.LoadConfig()
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Audio.Notify || !reflect.DeepEqual(cfg.Keys.PowerCycle, []string{"eco", "turbo"}) || cfg.Keys.Tap != "power-cycle:performance,turbo" || cfg.Keys.FnEsc != "mic-toggle" {
		t.Errorf("%+v", cfg.Keys)
	}
}

func TestLoadsFilesWritten_By_0_3_x(t *testing.T) {
	s := newStore(t)
	if err := os.MkdirAll(s.Dir, 0o755); err != nil {
		t.Fatal(err)
	}
	// what Python's json.dump(p, f, indent=1) wrote
	old := "{\n \"description\": \"my set\",\n \"tap\": \"power:turbo\",\n \"double\": \"exec:echo \\u00e9 > /tmp/x\",\n \"triple\": \"none\",\n \"long\": \"fan-max-toggle\",\n \"fn_esc\": \"none\",\n \"long_ms\": 800,\n \"multi_ms\": 450\n}"
	if err := os.WriteFile(filepath.Join(s.Dir, "old.json"), []byte(old), 0o644); err != nil {
		t.Fatal(err)
	}
	p, err := s.Load("old")
	if err != nil || p.Description != "my set" || p.Tap != "power:turbo" || p.Double != "exec:echo é > /tmp/x" {
		t.Fatalf("%+v, %v", p, err)
	}
	// and what we write is the same shape: same names, same order, indent of one space
	if _, err := s.Save("new", p); err != nil {
		t.Fatal(err)
	}
	b, _ := os.ReadFile(filepath.Join(s.Dir, "new.json"))
	want := "{\n \"description\": \"my set\",\n \"tap\": \"power:turbo\",\n \"double\": \"exec:echo é > /tmp/x\",\n \"triple\": \"none\",\n \"long\": \"fan-max-toggle\",\n \"fn_esc\": \"none\",\n \"long_ms\": 800,\n \"multi_ms\": 450\n}\n"
	if string(b) != want {
		t.Errorf("written:\n%s\nwant:\n%s", b, want)
	}
}

func TestMissingKeysTakeDefaults(t *testing.T) {
	s := newStore(t)
	p, err := s.Parse([]byte(`{"tap":"power:eco"}`))
	if err != nil || p.Tap != "power:eco" || p.Double != "output-toggle" || p.Long != "fan-max-toggle" || p.LongMS != 800 || p.MultiMS != 450 || p.Description != "" {
		t.Errorf("%+v, %v", p, err)
	}
}

func TestValidateRefusals(t *testing.T) {
	s := newStore(t)
	for in, want := range map[string]string{
		`[]`:                               "a key profile is an object",
		`"x"`:                              "a key profile is an object",
		`null`:                             "a key profile is an object",
		`{"tap":5}`:                        "Tap: an action is text",
		`{"tap":null}`:                     "Tap: an action is text",
		`{"double":"bogus"}`:               "Double tap: unknown action 'bogus'",
		`{"triple":"power:warp"}`:          "Triple tap: power mode must be one of",
		`{"long":""}`:                      "Long press: unknown action ''",
		`{"fn_esc":"fan:5"}`:               "Fn + Esc: fan:",
		`{"long_ms":299}`:                  "long_ms: 300..2000",
		`{"long_ms":2001}`:                 "long_ms: 300..2000",
		`{"multi_ms":199}`:                 "multi_ms: 200..1000",
		`{"multi_ms":1001}`:                "multi_ms: 200..1000",
		`{"long_ms":800.0}`:                "long_ms: 300..2000",
		`{"long_ms":"800"}`:                "long_ms: 300..2000",
		`{"long_ms":true}`:                 "long_ms: 300..2000",
		`{"multi_ms":1e2}`:                 "multi_ms: 200..1000",
		`{"long_ms":99999999999999999999}`: "long_ms: 300..2000",
		`{`:                                "not valid JSON",
		``:                                 "not valid JSON",
		`{} {}`:                            "not valid JSON",
	} {
		_, err := s.Parse([]byte(in))
		if err == nil || !strings.Contains(err.Error(), want) {
			t.Errorf("%s: got %v, want %q", in, err, want)
		}
	}
	for _, ok := range []string{`{"long_ms":300,"multi_ms":200}`, `{"long_ms":2000,"multi_ms":1000}`, `{"description":5}`} {
		if _, err := s.Parse([]byte(ok)); err != nil {
			t.Errorf("%s: %v", ok, err)
		}
	}
}

func TestInjectedValidatorDecidesWhichActionsAreAllowed(t *testing.T) {
	s := newStore(t)
	s.Check = func(spec string) error {
		if spec == "look:ghost" {
			return errors.New("no saved look 'ghost' (truesense look list)")
		}
		return nil
	}
	if _, err := s.Parse([]byte(`{"tap":"look:ghost"}`)); err == nil || !strings.Contains(err.Error(), "Tap: no saved look 'ghost'") {
		t.Errorf("%v", err)
	}
	if _, err := s.Parse([]byte(`{"tap":"anything goes with this validator"}`)); err != nil {
		t.Error(err)
	}
}

func TestNamesCannotEscape(t *testing.T) {
	s := newStore(t)
	for _, bad := range []string{"../etc/passwd", "a b", "", "x/y", strings.Repeat("a", 41), "a\x00b"} {
		if _, err := s.Load(bad); err == nil || !strings.Contains(err.Error(), "profile names use letters") {
			t.Errorf("Load(%q): %v", bad, err)
		}
		if _, err := s.Save(bad, Profile{}); err == nil {
			t.Errorf("Save(%q) must fail", bad)
		}
		if err := s.Delete(bad); err == nil {
			t.Errorf("Delete(%q) must fail", bad)
		}
	}
	for _, good := range []string{"a", "my.set-1_x", strings.Repeat("a", 40), "..x"} {
		if !ValidName(good) {
			t.Errorf("%q should be valid", good)
		}
	}
}

func TestLoadErrors(t *testing.T) {
	s := newStore(t)
	if _, err := s.Load("nothere"); err == nil || err.Error() != "no key profile 'nothere' (truesense keys profile list)" {
		t.Errorf("%v", err)
	}
	_ = os.MkdirAll(s.Dir, 0o755)
	_ = os.WriteFile(filepath.Join(s.Dir, "junk.json"), []byte("{not json"), 0o644)
	if _, err := s.Load("junk"); err == nil || err.Error() != "key profile 'junk' is not valid JSON" {
		t.Errorf("%v", err)
	}
	_ = os.WriteFile(filepath.Join(s.Dir, "bad.json"), []byte(`{"tap":"bogus"}`), 0o644)
	if _, err := s.Load("bad"); err == nil || !strings.Contains(err.Error(), "unknown action 'bogus'") {
		t.Errorf("%v", err)
	}
}

func TestNamesListsOnlyJSONAndHidesShadowedBuiltins(t *testing.T) {
	s := newStore(t)
	_ = os.MkdirAll(s.Dir, 0o755)
	for _, f := range []string{"zeta.json", "alpha.json", "gaming.json", "notes.txt", "x.json.tmp"} {
		_ = os.WriteFile(filepath.Join(s.Dir, f), []byte("{}"), 0o644)
	}
	want := append(BuiltinNames(), "alpha", "zeta")
	if got := s.Names(); !reflect.DeepEqual(got, want) {
		t.Errorf("%v, want %v", got, want)
	}
}

func TestCopy(t *testing.T) {
	s := newStore(t)
	for i, want := range []string{"gaming-copy", "gaming-copy-2", "gaming-copy-3"} {
		got, err := s.Copy("gaming", "")
		if err != nil || got != want {
			t.Fatalf("copy %d: %q, %v; want %q", i, got, err, want)
		}
	}
	if got, err := s.Copy("gaming", "kcopy"); err != nil || got != "kcopy" {
		t.Errorf("%q, %v", got, err)
	}
	if got, _ := s.Copy("gaming", "kcopy"); got != "kcopy-2" {
		t.Errorf("a taken name is numbered: %q", got)
	}
	if got, _ := s.Copy("gaming", "default"); got != "default-2" {
		t.Errorf("a built-in name is taken too: %q", got)
	}
	if _, err := s.Copy("nothere", ""); err == nil {
		t.Error("copy of a missing profile must fail")
	}
	if _, err := s.Copy("gaming", "bad name"); err == nil {
		t.Error("copy to an invalid name must fail")
	}
	p, _ := s.Load("kcopy")
	if p.Tap != "power-cycle:performance,turbo" {
		t.Errorf("%+v", p)
	}
}

func TestSaveWritesNothingWhenInvalid(t *testing.T) {
	s := newStore(t)
	if _, err := s.Save("x", Profile{Bindings: Bindings{Tap: "bogus", LongMS: 800, MultiMS: 450}}); err == nil {
		t.Fatal("must fail")
	}
	if _, err := os.Stat(s.Dir); err == nil {
		entries, _ := os.ReadDir(s.Dir)
		if len(entries) != 0 {
			t.Errorf("left %v", entries)
		}
	}
	// a Profile with zero timings (never set) is refused, not silently written
	if _, err := s.Save("y", Profile{}); err == nil {
		t.Error("zero timing must be refused")
	}
}

func TestCurrentDefaultsTheProfileName(t *testing.T) {
	cfg := config.Default()
	cfg.Keys.Profile = ""
	if a := FromConfig(cfg); a.Profile != "default" || a.Tap != "power-next" {
		t.Errorf("%+v", a)
	}
}

func TestBindingsAccessors(t *testing.T) {
	b := Default()
	for _, g := range Gestures() {
		if !b.SetAction(g, "x-"+g) || b.Action(g) != "x-"+g {
			t.Errorf("%s", g)
		}
	}
	if b.SetAction("nope", "x") || b.Action("nope") != "" {
		t.Error("unknown gesture")
	}
	if Label("fn_esc") != "Fn + Esc" || Label("long") != "Long press" || Label("zzz") != "zzz" {
		t.Error("labels")
	}
}

func FuzzParse(f *testing.F) {
	for _, s := range []string{`{}`, `{"tap":"power:turbo"}`, `{"long_ms":800,"multi_ms":450}`, `[]`, `{"tap":5}`, `{"long_ms":1e400}`, "{\"tap\":\"\\u0000\"}", `{"description":{"a":1}}`, `{"tap":"exec:a"} x`} {
		f.Add([]byte(s))
	}
	s := &Store{Dir: "/nonexistent"}
	f.Fuzz(func(t *testing.T, data []byte) {
		p, err := s.Parse(data)
		if err != nil {
			return
		}
		// whatever was accepted is fully valid and round-trips through validation again
		again, err := s.Validate(toObject(p))
		if err != nil || again != p {
			t.Fatalf("accepted %+v but re-validation gave %+v, %v", p, again, err)
		}
	})
}
