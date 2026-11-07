package smartprofile

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func TestBuiltinsAreValid(t *testing.T) {
	want := []string{"native", "truesense", "steady", "battery-first", "performance-first", "silent"}
	if got := BuiltinNames(); !reflect.DeepEqual(got, want) {
		t.Fatalf("BuiltinNames = %v", got)
	}
	for _, n := range want {
		p, ok := Builtin(n)
		if !ok || p.Description == "" {
			t.Errorf("%s: missing or without description", n)
		}
		if err := p.Check(); err != nil {
			t.Errorf("%s: %v", n, err)
		}
		// Round trip through the JSON a client would see.
		b, _ := json.Marshal(p)
		back, err := Parse(b)
		if err != nil || !reflect.DeepEqual(back, p) {
			t.Errorf("%s: JSON round trip %+v -> %+v (%v)", n, p, back, err)
		}
	}
}

// Python: test_new_profile_settings_exist_validate_and_old_profiles_still_load.
func TestNewSettingsExistAndOldProfilesStillLoad(t *testing.T) {
	d, err := (&Store{Dir: t.TempDir()}).Load("truesense")
	if err != nil {
		t.Fatal(err)
	}
	if got := [5]int{d.BusyHoldS, d.MinChangeS, d.TurboMargin, d.TurboRetryS, d.DownDwellS}; got != [5]int{60, 45, 6, 180, 60} {
		t.Errorf("truesense settings = %v", got)
	}
	// An "old" profile without the four newer keys still loads with their defaults.
	b, _ := json.Marshal(d)
	var old map[string]any
	_ = json.Unmarshal(b, &old)
	for _, k := range []string{"busy_hold_s", "min_change_s", "turbo_margin_c", "turbo_retry_s"} {
		delete(old, k)
	}
	got, err := Validate(old)
	if err != nil || got.BusyHoldS != 60 {
		t.Errorf("old profile: %+v, %v", got, err)
	}
	n, _ := Builtin("native")
	if n.BusyHoldS != 0 || n.MinChangeS != 2 {
		t.Errorf("native must stay as simple as PredatorSense: %+v", n)
	}
}

func TestValidate(t *testing.T) {
	base := func(kv ...any) map[string]any {
		m := map[string]any{}
		for i := 0; i < len(kv); i += 2 {
			m[kv[i].(string)] = kv[i+1]
		}
		return m
	}
	tests := []struct {
		name    string
		in      any
		wantErr string
	}{
		{"empty object gets every default", base(), ""},
		{"not an object", []any{}, "a smart profile is an object"},
		{"null", nil, "a smart profile is an object"},
		{"text", "x", "a smart profile is an object"},
		{"unknown keys ignored", base("nope", 1), ""},
		{"bad mode from the Python test", base("ac_busy", "warp"), "ac_busy must be one of eco, quiet, balanced, performance, turbo"},
		{"mode is case sensitive", base("ac_idle", "Eco"), "ac_idle must be one of"},
		{"mode as number", base("battery_hot", 3), "battery_hot must be one of"},
		{"mode null", base("battery_busy", nil), "battery_busy must be one of"},
		{"every mode is accepted", base("ac_idle", "turbo", "ac_browsing", "eco", "ac_busy", "quiet", "battery_idle", "performance", "battery_browsing", "balanced"), ""},
		{"ac_turbo as text", base("ac_turbo", "yes"), "ac_turbo is true or false"},
		{"ac_turbo as number", base("ac_turbo", 1), "ac_turbo is true or false"},
		{"ac_turbo false", base("ac_turbo", false), ""},
		{"busy_hold_s 601 (Python)", base("busy_hold_s", 601), "busy_hold_s: 0..600"},
		{"busy_hold_s 0 is the minimum", base("busy_hold_s", 0), ""},
		{"busy_hold_s -1", base("busy_hold_s", -1), "busy_hold_s: 0..600"},
		{"turbo_margin_c 21 (Python)", base("turbo_margin_c", 21), "turbo_margin_c: 0..20"},
		{"turbo_retry_s 5 (Python)", base("turbo_retry_s", 5), "turbo_retry_s: 10..900"},
		{"min_change_s 1 (Python)", base("min_change_s", 1), "min_change_s: 2..600"},
		{"turbo_after_s 4", base("turbo_after_s", 4), "turbo_after_s: 5..300"},
		{"turbo_after_s 301", base("turbo_after_s", 301), "turbo_after_s: 5..300"},
		{"turbo_max_cpu 59", base("turbo_max_cpu", 59), "turbo_max_cpu: 60..95"},
		{"turbo_max_cpu 96", base("turbo_max_cpu", 96), "turbo_max_cpu: 60..95"},
		{"turbo_max_gpu 54", base("turbo_max_gpu", 54), "turbo_max_gpu: 55..85"},
		{"turbo_max_gpu 86", base("turbo_max_gpu", 86), "turbo_max_gpu: 55..85"},
		{"backoff_hold_s 9", base("backoff_hold_s", 9), "backoff_hold_s: 10..600"},
		{"idle_after_s 601", base("idle_after_s", 601), "idle_after_s: 10..600"},
		{"battery_low_pct 4", base("battery_low_pct", 4), "battery_low_pct: 5..50"},
		{"battery_low_pct 51", base("battery_low_pct", 51), "battery_low_pct: 5..50"},
		{"up_dwell_s 121", base("up_dwell_s", 121), "up_dwell_s: 2..120"},
		{"down_dwell_s 301", base("down_dwell_s", 301), "down_dwell_s: 2..300"},
		{"number as text", base("turbo_after_s", "30"), "turbo_after_s: 5..300"},
		{"number as bool", base("turbo_after_s", true), "turbo_after_s: 5..300"},
		{"number null", base("turbo_after_s", nil), "turbo_after_s: 5..300"},
		{"description of the wrong type is ignored", base("description", 5), ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := Validate(tt.in)
			if tt.wantErr == "" {
				if err != nil {
					t.Fatalf("unexpected error: %v", err)
				}
				return
			}
			if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
				t.Fatalf("error = %v, want %q", err, tt.wantErr)
			}
		})
	}
}

func TestValidateTruncatesNumbersAndKeepsDescription(t *testing.T) {
	p, err := Validate(map[string]any{"turbo_after_s": 45.9, "description": "mine", "ac_idle": "quiet"})
	if err != nil {
		t.Fatal(err)
	}
	if p.TurboAfterS != 45 || p.Description != "mine" || p.ACIdle != "quiet" || p.ACBusy != "performance" {
		t.Errorf("got %+v", p)
	}
}

func TestSettingsMetadata(t *testing.T) {
	if got := len(ModeSettings()); got != 7 {
		t.Errorf("ModeSettings has %d entries, want 7", got)
	}
	if got := len(NumSettings()); got != 12 {
		t.Errorf("NumSettings has %d entries, want 12", got)
	}
	// Every setting in the metadata exists in a Profile and the JSON keys line up.
	b, _ := json.Marshal(Defaults())
	var m map[string]any
	_ = json.Unmarshal(b, &m)
	p := Defaults()
	for _, ms := range ModeSettings() {
		if _, ok := m[ms.Key]; !ok || ms.Label == "" || p.modeField(ms.Key) == nil {
			t.Errorf("mode setting %q is not wired", ms.Key)
		}
	}
	for _, ns := range NumSettings() {
		if _, ok := m[ns.Key]; !ok || ns.Label == "" || ns.Min >= ns.Max || p.numField(ns.Key) == nil {
			t.Errorf("numeric setting %q is not wired", ns.Key)
		}
		if v := *p.numField(ns.Key); v < ns.Min || v > ns.Max {
			t.Errorf("default of %q (%d) is outside its own range", ns.Key, v)
		}
	}
	if got := len(m); got != 20 { // 7 modes + ac_turbo + 12 numbers; the empty description is omitted
		t.Errorf("Profile marshals %d keys: %v", got, m)
	}
}

func TestStoreRoundTripAndCopy(t *testing.T) {
	s := &Store{Dir: filepath.Join(t.TempDir(), "smart")}
	if names, err := s.Names(); err != nil || !reflect.DeepEqual(names, BuiltinNames()) {
		t.Fatalf("Names = %v, %v", names, err)
	}
	p := Defaults()
	p.ACBusy, p.TurboAfterS, p.Description = "turbo", 99, "mine"
	if _, err := s.Save("mine", p); err != nil {
		t.Fatal(err)
	}
	got, err := s.Load("mine")
	if err != nil || !reflect.DeepEqual(got, p) {
		t.Fatalf("Load = %+v, %v", got, err)
	}
	// Copying a built-in makes an editable profile; names never collide.
	c1, err := s.Copy("silent", "")
	if err != nil || c1 != "silent-copy" {
		t.Fatalf("Copy = %q, %v", c1, err)
	}
	c2, _ := s.Copy("silent", "")
	c3, _ := s.Copy("silent", "")
	if c2 != "silent-copy-2" || c3 != "silent-copy-3" {
		t.Errorf("collision names: %q %q", c2, c3)
	}
	c4, _ := s.Copy("mine", "native") // a built-in name is taken too
	if c4 != "native-2" {
		t.Errorf("copy onto a built-in name = %q", c4)
	}
	silent, _ := Builtin("silent")
	if copied, _ := s.Load("silent-copy"); copied.ACBusy != silent.ACBusy || copied.Description != silent.Description {
		t.Errorf("copy differs: %+v", copied)
	}
	names, _ := s.Names()
	want := append(BuiltinNames(), "mine", "native-2", "silent-copy", "silent-copy-2", "silent-copy-3")
	if !reflect.DeepEqual(names, want) {
		t.Errorf("Names = %v, want %v", names, want)
	}
	for _, n := range []string{"mine", "silent-copy"} {
		if err := s.Delete(n); err != nil {
			t.Errorf("Delete(%s): %v", n, err)
		}
	}
	if _, err := s.Load("mine"); err == nil || !strings.Contains(err.Error(), "no smart profile 'mine'") {
		t.Errorf("Load after delete: %v", err)
	}
	if _, err := s.Copy("ghost", ""); err == nil || !strings.Contains(err.Error(), "no smart profile 'ghost'") {
		t.Errorf("Copy of a missing profile: %v", err)
	}
	if _, err := s.Copy("silent", "bad/name"); err == nil {
		t.Error("Copy to a bad name accepted")
	}
}

func TestStoreBuiltinsAreReadOnly(t *testing.T) {
	s := &Store{Dir: t.TempDir()}
	for _, n := range BuiltinNames() {
		// Python: sp.save("silent", dict(sp.DEFAULTS)) must be refused.
		if _, err := s.Save(n, Defaults()); err == nil || !strings.Contains(err.Error(), "built in and read-only") {
			t.Errorf("Save(%s): %v", n, err)
		}
		if err := s.Delete(n); err == nil || !strings.Contains(err.Error(), "cannot be deleted") {
			t.Errorf("Delete(%s): %v", n, err)
		}
	}
	if err := s.Delete("missing"); err == nil || !strings.Contains(err.Error(), "no smart profile 'missing'") {
		t.Errorf("Delete(missing): %v", err)
	}
}

func TestStoreNamesAndBadFiles(t *testing.T) {
	dir := t.TempDir()
	s := &Store{Dir: dir}
	for _, n := range []string{"", "../x", "a b", strings.Repeat("n", 41)} {
		if _, err := s.Save(n, Defaults()); err == nil || !strings.Contains(err.Error(), "profile names use letters") {
			t.Errorf("Save(%q): %v", n, err)
		}
		if _, err := s.Load(n); err == nil {
			t.Errorf("Load(%q) accepted", n)
		}
	}
	bad := Defaults()
	bad.ACIdle = "warp"
	if _, err := s.Save("bad", bad); err == nil {
		t.Error("an invalid profile was saved")
	}
	for name, text := range map[string]string{"broken.json": "{", "wrong.json": `{"ac_idle":"warp"}`, "readme.txt": "x"} {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(text), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := s.Load("broken"); err == nil || !strings.Contains(err.Error(), "not valid JSON") {
		t.Errorf("broken: %v", err)
	}
	if _, err := s.Load("wrong"); err == nil || !strings.Contains(err.Error(), "ac_idle must be one of") {
		t.Errorf("wrong: %v", err)
	}
	if names, _ := s.Names(); !reflect.DeepEqual(names, append(BuiltinNames(), "broken", "wrong")) {
		t.Errorf("Names = %v", names)
	}
}

func TestDefaultDir(t *testing.T) {
	t.Setenv("HOME", "/home/u")
	if got := DefaultDir(); got != "/home/u/.config/truesense/smart" {
		t.Errorf("DefaultDir = %q", got)
	}
}

func FuzzValidate(f *testing.F) {
	for _, s := range []string{`{}`, `null`, `[]`, `{"ac_idle":"turbo"}`, `{"ac_busy":"warp"}`, `{"turbo_after_s":1e999}`, `{"turbo_after_s":45.5,"ac_turbo":false}`, `{"ac_turbo":"x"}`,
		fmt.Sprintf(`{"description":%q}`, "d")} {
		f.Add([]byte(s))
	}
	f.Fuzz(func(t *testing.T, data []byte) {
		p, err := Parse(data)
		if err != nil {
			return
		}
		if err := p.Check(); err != nil {
			t.Fatalf("Parse accepted a profile that fails Check: %v", err)
		}
		b, err := json.Marshal(p)
		if err != nil {
			t.Fatal(err)
		}
		again, err := Parse(b)
		if err != nil || !reflect.DeepEqual(again, p) {
			t.Fatalf("round trip: %+v -> %s -> %+v (%v)", p, b, again, err)
		}
	})
}
