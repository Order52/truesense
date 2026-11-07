package powermgr

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"sync"
	"testing"
)

type fakeUnit struct{ active, enabled bool }

// system is a fake machine: a CPU sysfs tree, systemd units, and a call log.
type system struct {
	t     *testing.T
	cpu   string
	rec   string
	mu    sync.Mutex
	units map[string]*fakeUnit
	calls [][]string
	deny  map[string]bool // verbs systemctl refuses ("disable")
}

func newSystem(t *testing.T) *system {
	t.Helper()
	d := t.TempDir()
	s := &system{t: t, cpu: filepath.Join(d, "cpu"), rec: filepath.Join(d, "state", "rec.json"), deny: map[string]bool{}}
	must := func(err error) {
		if err != nil {
			t.Fatal(err)
		}
	}
	must(os.MkdirAll(filepath.Join(s.cpu, "intel_pstate"), 0o755))
	for n := 0; n < 3; n++ {
		dir := filepath.Join(s.cpu, "cpu"+string(rune('0'+n)), "cpufreq")
		must(os.MkdirAll(dir, 0o755))
		s.write(filepath.Join(dir, "scaling_governor"), "performance")
		s.write(filepath.Join(dir, "energy_performance_preference"), "performance")
		s.write(filepath.Join(dir, "cpuinfo_max_freq"), "5000000")
		s.write(filepath.Join(dir, "scaling_max_freq"), "2100000") // the ceiling turbo-off left behind
	}
	s.write(filepath.Join(s.cpu, "intel_pstate", "no_turbo"), "1")
	s.units = map[string]*fakeUnit{"auto-cpufreq": {true, true}, "tlp": {false, false}, "ananicy-cpp": {true, true}}
	return s
}

func (s *system) write(path, text string) {
	s.t.Helper()
	if err := os.WriteFile(path, []byte(text), 0o644); err != nil {
		s.t.Fatal(err)
	}
}

func (s *system) read(rel string) string {
	s.t.Helper()
	b, err := os.ReadFile(filepath.Join(s.cpu, rel))
	if err != nil {
		s.t.Fatal(err)
	}
	return string(b)
}

func (s *system) run(ctx context.Context, name string, args ...string) (int, string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.calls = append(s.calls, append([]string{name}, args...))
	verb, unit := args[0], strings.TrimSuffix(args[len(args)-1], ".service")
	u := s.units[unit]
	if u == nil {
		u = &fakeUnit{}
	}
	switch verb {
	case "is-active":
		if u.active {
			return 0, "active"
		}
		return 3, "inactive"
	case "is-enabled":
		if u.enabled {
			return 0, "enabled"
		}
		return 1, "disabled"
	case "disable":
		if s.deny["disable"] {
			return 1, "Access denied: " + strings.Repeat("x", 100)
		}
		u.active, u.enabled = false, false
	case "enable":
		u.enabled = true
	case "start":
		u.active = true
	}
	return 0, ""
}

func (s *system) manager() *Manager {
	return &Manager{Run: s.run, CPUDir: s.cpu, RecordPath: s.rec}
}

var ctx = context.Background()

func str(s string) *string { return &s }

func TestStatusListsOnlyKnownRunningOrEnabledManagers(t *testing.T) {
	s := newSystem(t)
	got := s.manager().Status(ctx)
	// ananicy-cpp and tlp (not running) are not on the list.
	if want := []Unit{{"auto-cpufreq", true, true}}; !reflect.DeepEqual(got, want) {
		t.Errorf("Status = %+v, want %+v", got, want)
	}
	s.units["tlp"] = &fakeUnit{false, true}
	s.units["tuned"] = &fakeUnit{true, false}
	got = s.manager().Status(ctx)
	if want := []Unit{{"auto-cpufreq", true, true}, {"tlp", false, true}, {"tuned", true, false}}; !reflect.DeepEqual(got, want) {
		t.Errorf("Status = %+v, want %+v", got, want)
	}
	for _, c := range s.calls {
		if c[0] != "systemctl" || (c[1] != "is-active" && c[1] != "is-enabled") {
			t.Errorf("Status ran %v: it must only ask", c)
		}
	}
	none := newSystem(t)
	none.units = map[string]*fakeUnit{}
	if got := none.manager().Status(ctx); got == nil || len(got) != 0 {
		t.Errorf("no managers must be an empty list (JSON []), got %#v", got)
	}
}

// Python: test_takeover_stops_only_known_managers_fixes_the_cpu_and_restore_undoes_it.
func TestTakeoverStopsOnlyKnownManagersFixesTheCPUAndRestoreUndoesIt(t *testing.T) {
	s := newSystem(t)
	m := s.manager()
	r, err := m.Takeover(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(r.Stopped, []string{"auto-cpufreq"}) || len(r.Failed) != 0 || len(r.CPUNotSet) != 0 {
		t.Fatalf("report = %+v", r)
	}
	if u := s.units["auto-cpufreq"]; u.active || u.enabled {
		t.Error("auto-cpufreq still runs or is enabled")
	}
	if !s.units["ananicy-cpp"].active {
		t.Error("unrelated services are left alone")
	}
	if want := (CPUState{str("0"), str("powersave"), str("balance_performance")}); !reflect.DeepEqual(m.CPUState(), want) {
		t.Errorf("CPU state = %+v", m.CPUState())
	}
	for _, c := range s.calls {
		unit := strings.TrimSuffix(c[len(c)-1], ".service")
		if !isKnownUnit(unit) {
			t.Errorf("touched a service that is not on the list: %v", c)
		}
	}
	rec := m.LoadRecord()
	if want := (CPUState{str("1"), str("performance"), str("performance")}); rec.CPU == nil || !reflect.DeepEqual(*rec.CPU, want) {
		t.Errorf("recorded CPU = %+v", rec.CPU)
	}
	if low := m.CeilingLow(); len(low) != 0 {
		t.Errorf("the frequency ceilings left low by turbo-off must be raised: %v", low)
	}
	if got := s.read("cpu1/cpufreq/scaling_max_freq"); got != "5000000" {
		t.Errorf("cpu1 ceiling = %q", got)
	}
	if !r.CeilingFixed {
		t.Error("ceiling_fixed must be true")
	}
	// A second takeover keeps the ORIGINAL state to go back to.
	if _, err := m.Takeover(ctx); err != nil {
		t.Fatal(err)
	}
	if got := m.LoadRecord().CPU; got == nil || *got.Governor != "performance" {
		t.Errorf("a second takeover overwrote the original state: %+v", got)
	}
	rr := m.Restore(ctx)
	if !reflect.DeepEqual(rr.Restored, []string{"auto-cpufreq"}) {
		t.Errorf("restored = %v", rr.Restored)
	}
	if u := s.units["auto-cpufreq"]; !u.active || !u.enabled {
		t.Error("auto-cpufreq was not started and enabled again")
	}
	if want := (CPUState{str("1"), str("performance"), str("performance")}); !reflect.DeepEqual(m.CPUState(), want) {
		t.Errorf("CPU after restore = %+v", m.CPUState())
	}
	if rec := m.LoadRecord(); !rec.empty() {
		t.Errorf("the record must be gone after a restore: %+v", rec)
	}
	if _, err := os.Stat(s.rec); err == nil {
		t.Error("record file still exists")
	}
}

// Python: test_a_hand_edited_record_cannot_touch_other_services_and_failures_are_reported.
func TestAHandEditedRecordCannotTouchOtherServices(t *testing.T) {
	s := newSystem(t)
	if err := os.MkdirAll(filepath.Dir(s.rec), 0o755); err != nil {
		t.Fatal(err)
	}
	s.write(s.rec, `{"units": [{"unit": "sshd", "was_active": true, "was_enabled": true}], "cpu": null}`)
	s.calls = nil
	rr := s.manager().Restore(ctx)
	if len(s.calls) != 0 {
		t.Errorf("only units from the fixed list are ever started or enabled: %v", s.calls)
	}
	if len(rr.Restored) != 0 {
		t.Errorf("restored = %v", rr.Restored)
	}
}

func TestFailuresAreReported(t *testing.T) {
	s := newSystem(t)
	s.deny["disable"] = true
	r, err := s.manager().Takeover(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(r.Stopped) != 0 || len(r.Failed) != 1 || !strings.HasPrefix(r.Failed[0], "auto-cpufreq: Access denied") {
		t.Fatalf("report = %+v", r)
	}
	if got := len(r.Failed[0]) - len("auto-cpufreq: "); got != 80 {
		t.Errorf("failure text is %d characters, want it cut to 80", got)
	}
}

func TestADisableThatFailedButLeftTheUnitStoppedCountsAsStopped(t *testing.T) {
	s := newSystem(t)
	m := s.manager()
	m.Run = func(c context.Context, name string, args ...string) (int, string) {
		if args[0] == "disable" {
			s.units["auto-cpufreq"].active = false
			return 1, "Failed to disable unit: not found"
		}
		return s.run(c, name, args...)
	}
	r, _ := m.Takeover(ctx)
	if !reflect.DeepEqual(r.Stopped, []string{"auto-cpufreq"}) || len(r.Failed) != 0 {
		t.Errorf("report = %+v", r)
	}
}

func TestTheRecordIsWrittenBeforeAnythingIsStopped(t *testing.T) {
	s := newSystem(t)
	m := s.manager()
	var atDisable Record
	m.Run = func(c context.Context, name string, args ...string) (int, string) {
		if args[0] == "disable" {
			atDisable = m.LoadRecord()
		}
		return s.run(c, name, args...)
	}
	if _, err := m.Takeover(ctx); err != nil {
		t.Fatal(err)
	}
	if len(atDisable.Units) != 1 || atDisable.Units[0] != (RecordUnit{"auto-cpufreq", true, true}) || atDisable.CPU == nil {
		t.Errorf("record at the time of the first stop: %+v", atDisable)
	}
}

func TestTakeoverChangesNothingWhenTheRecordCannotBeWritten(t *testing.T) {
	s := newSystem(t)
	m := s.manager()
	m.RecordPath = filepath.Join(s.cpu, "intel_pstate", "no_turbo", "nope.json") // a "directory" that is a file
	_, err := m.Takeover(ctx)
	if err == nil || !strings.Contains(err.Error(), "could not remember what was stopped") || !strings.Contains(err.Error(), "nothing was changed") {
		t.Fatalf("err = %v", err)
	}
	if !s.units["auto-cpufreq"].active || s.read("intel_pstate/no_turbo") != "1" {
		t.Error("something was changed although nothing could be remembered")
	}
	for _, c := range s.calls {
		if c[1] != "is-active" && c[1] != "is-enabled" {
			t.Errorf("ran %v before the record was safe", c)
		}
	}
}

// Python: test_a_low_ceiling_is_repaired_even_when_no_manager_is_running.
func TestALowCeilingIsRepairedEvenWhenNoManagerIsRunning(t *testing.T) {
	s := newSystem(t)
	s.units["auto-cpufreq"] = &fakeUnit{}
	s.write(filepath.Join(s.cpu, "intel_pstate", "no_turbo"), "0")
	for n := 0; n < 3; n++ {
		s.write(filepath.Join(s.cpu, "cpu"+string(rune('0'+n)), "cpufreq", "scaling_governor"), "schedutil")
	}
	m := s.manager()
	if got := len(m.CeilingLow()); got != 3 {
		t.Fatalf("CeilingLow = %d cores, want 3", got)
	}
	r, err := m.Takeover(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(r.Found) != 0 || !r.CeilingFixed {
		t.Errorf("report = %+v", r)
	}
	if got := *m.CPUState().Governor; got != "schedutil" {
		t.Errorf("governor = %q: your governor is not touched when nothing was in charge and turbo is on", got)
	}
}

func TestTurboLeftOffIsFixedEvenWithoutAManager(t *testing.T) {
	s := newSystem(t)
	s.units["auto-cpufreq"] = &fakeUnit{}
	r, _ := s.manager().Takeover(ctx)
	if got := s.read("intel_pstate/no_turbo"); got != "0" {
		t.Errorf("no_turbo = %q, want 0", got)
	}
	if len(r.CPUNotSet) != 0 || *r.CPU.Governor != "powersave" {
		t.Errorf("report = %+v", r)
	}
}

func TestCPUNotSetReportsWhatDidNotTake(t *testing.T) {
	s := newSystem(t)
	m := s.manager()
	// Turbo cannot be set when the file is a directory entry we cannot write: make it read-only.
	turbo := filepath.Join(s.cpu, "intel_pstate", "no_turbo")
	if err := os.Chmod(turbo, 0o444); err != nil {
		t.Fatal(err)
	}
	if os.Geteuid() == 0 {
		t.Skip("root ignores file modes")
	}
	r, err := m.Takeover(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if !contains(r.CPUNotSet, "turbo") {
		t.Errorf("cpu_not_set = %v, want turbo", r.CPUNotSet)
	}
}

func contains(l []string, s string) bool {
	for _, x := range l {
		if x == s {
			return true
		}
	}
	return false
}

func TestSetCPUWithoutIntelPstateOrFiles(t *testing.T) {
	m := &Manager{CPUDir: t.TempDir()}
	if bad := m.setCPU(defaultCPU()); len(bad) != 0 {
		t.Errorf("a machine without those files has nothing that failed: %v", bad)
	}
	if st := m.CPUState(); st.NoTurbo != nil || st.Governor != nil || st.EPP != nil {
		t.Errorf("CPUState on an empty tree = %+v", st)
	}
	if low := m.CeilingLow(); low == nil || len(low) != 0 {
		t.Errorf("CeilingLow = %#v", low)
	}
}

func TestCeilingLowIgnoresUnreadableAndNonNumericFiles(t *testing.T) {
	s := newSystem(t)
	dir := filepath.Join(s.cpu, "cpu0", "cpufreq")
	s.write(filepath.Join(dir, "cpuinfo_max_freq"), "unknown")
	s.write(filepath.Join(s.cpu, "cpu1", "cpufreq", "scaling_max_freq"), "5000000")
	if err := os.Remove(filepath.Join(s.cpu, "cpu2", "cpufreq", "scaling_max_freq")); err != nil {
		t.Fatal(err)
	}
	if low := s.manager().CeilingLow(); len(low) != 0 {
		t.Errorf("CeilingLow = %v", low)
	}
	s.write(filepath.Join(s.cpu, "cpu1", "cpufreq", "scaling_max_freq"), "-5")
	if low := s.manager().CeilingLow(); len(low) != 0 {
		t.Errorf("a negative number is not a frequency: %v", low)
	}
}

func TestFixCeilingReportsCoresThatDidNotTakeIt(t *testing.T) {
	s := newSystem(t)
	f := filepath.Join(s.cpu, "cpu1", "cpufreq", "scaling_max_freq")
	if err := os.Chmod(f, 0o444); err != nil {
		t.Fatal(err)
	}
	if os.Geteuid() == 0 {
		t.Skip("root ignores file modes")
	}
	bad := s.manager().FixCeiling()
	if !reflect.DeepEqual(bad, []string{"cpu1"}) {
		t.Errorf("FixCeiling = %v, want [cpu1]", bad)
	}
}

func TestRestoreWithNothingOnRecord(t *testing.T) {
	s := newSystem(t)
	rr := s.manager().Restore(ctx)
	if rr.Note != "nothing was changed by TrueSense" || len(s.calls) != 0 {
		t.Errorf("report = %+v, calls %v", rr, s.calls)
	}
	b, err := json.Marshal(rr)
	if err != nil {
		t.Fatal(err)
	}
	var m map[string]any
	_ = json.Unmarshal(b, &m)
	if _, has := m["cpu_not_set"]; has || m["note"] == nil || m["restored"] == nil || m["cpu"] == nil {
		t.Errorf("JSON = %s", b)
	}
}

func TestReportJSONShapesMatchPython(t *testing.T) {
	s := newSystem(t)
	m := s.manager()
	r, _ := m.Takeover(ctx)
	b, _ := json.Marshal(r)
	var got map[string]any
	_ = json.Unmarshal(b, &got)
	for _, k := range []string{"found", "stopped", "failed", "cpu_not_set", "cpu", "ceiling_fixed"} {
		if _, ok := got[k]; !ok {
			t.Errorf("takeover JSON lacks %q: %s", k, b)
		}
	}
	if got["failed"] == nil || got["cpu_not_set"] == nil {
		t.Errorf("empty lists must be [] not null: %s", b)
	}
	rr, _ := json.Marshal(m.Restore(ctx))
	var back map[string]any
	_ = json.Unmarshal(rr, &back)
	for _, k := range []string{"restored", "cpu_not_set", "cpu"} {
		if _, ok := back[k]; !ok {
			t.Errorf("restore JSON lacks %q: %s", k, rr)
		}
	}
	if _, ok := back["note"]; ok {
		t.Errorf("a real restore has no note: %s", rr)
	}
	cpu, _ := json.Marshal(CPUState{})
	if string(cpu) != `{"no_turbo":null,"governor":null,"epp":null}` {
		t.Errorf("unreadable CPU state JSON = %s", cpu)
	}
}

func TestRecordReading(t *testing.T) {
	dir := t.TempDir()
	m := &Manager{RecordPath: filepath.Join(dir, "rec.json")}
	tests := []struct {
		name      string
		text      string
		wantUnits int
		wantCPU   bool
	}{
		{"missing file", "", 0, false},
		{"not json", "{", 0, false},
		{"json but not an object", "[1]", 0, false},
		{"null", "null", 0, false},
		{"valid", `{"units":[{"unit":"tlp","was_active":true,"was_enabled":false}],"cpu":{"no_turbo":"1","governor":"performance","epp":null}}`, 1, true},
		{"malformed unit entries are skipped", `{"units":["x",{"unit":"tlp"},{"was_active":true},7]}`, 1, false},
		{"non-text cpu values are ignored", `{"cpu":{"no_turbo":1,"governor":["x"],"epp":"balance_power"}}`, 0, true},
		{"cpu of the wrong type", `{"cpu":"performance"}`, 0, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_ = os.Remove(m.RecordPath)
			if tt.text != "" {
				if err := os.WriteFile(m.RecordPath, []byte(tt.text), 0o644); err != nil {
					t.Fatal(err)
				}
			}
			r := m.LoadRecord()
			if len(r.Units) != tt.wantUnits || (r.CPU != nil) != tt.wantCPU {
				t.Errorf("record = %+v", r)
			}
			if m.Recorded() != !r.empty() {
				t.Error("Recorded disagrees with the record")
			}
		})
	}
	_ = os.WriteFile(m.RecordPath, []byte(`{"cpu":{"no_turbo":1,"governor":["x"],"epp":"balance_power"}}`), 0o644)
	if c := m.LoadRecord().CPU; c.NoTurbo != nil || c.Governor != nil || *c.EPP != "balance_power" {
		t.Errorf("cpu = %+v", c)
	}
}

func TestExecRunner(t *testing.T) {
	code, out := ExecRunner(ctx, "sh", "-c", "echo hello; echo err >&2; exit 3")
	if code != 3 || out != "hello" {
		t.Errorf("= %d, %q", code, out)
	}
	code, out = ExecRunner(ctx, "sh", "-c", "echo only-err >&2")
	if code != 0 || out != "only-err" {
		t.Errorf("stderr fallback = %d, %q", code, out)
	}
	code, out = ExecRunner(ctx, "/nonexistent/truesense-test-binary")
	if code != 1 || out == "" {
		t.Errorf("missing binary = %d, %q", code, out)
	}
	code, out = ExecRunner(ctx, "sh", "-c", "echo $LC_ALL")
	if code != 0 || out != "C" {
		t.Errorf("LC_ALL = %d, %q", code, out)
	}
}

func TestNewAndRecordPathFromEnv(t *testing.T) {
	t.Setenv("TRUESENSE_PM_RECORD", "")
	if got := RecordPathFromEnv(); got != DefaultRecordPath {
		t.Errorf("default = %q", got)
	}
	t.Setenv("TRUESENSE_PM_RECORD", "/tmp/x/rec.json")
	m := New()
	if m.RecordPath != "/tmp/x/rec.json" || m.CPUDir != DefaultCPUDir || m.Run == nil {
		t.Errorf("New = %+v", m)
	}
}

func TestUnitsListIsTheFixedOne(t *testing.T) {
	want := []string{"auto-cpufreq", "tlp", "tuned", "tuned-ppd", "power-profiles-daemon", "system76-power"}
	if !reflect.DeepEqual(Units(), want) {
		t.Errorf("Units = %v", Units())
	}
	for _, bad := range []string{"", "sshd", "tlp.service", "TLP", "auto-cpufreq ", "../tlp"} {
		if isKnownUnit(bad) {
			t.Errorf("%q must not be on the list", bad)
		}
	}
}

// FuzzRecord: a record file is external input (hand-editable, possibly truncated): reading it
// must never panic, and Restore on whatever was read must only ever name units from the list.
func FuzzRecord(f *testing.F) {
	for _, s := range []string{
		`{"units":[{"unit":"tlp","was_active":true,"was_enabled":true}],"cpu":{"no_turbo":"1","governor":"performance","epp":"performance"}}`,
		`{}`, `null`, `[]`, `{"units":"x"}`, `{"cpu":{"no_turbo":[1]}}`, `{"units":[{"unit":"sshd","was_active":true}]}`, "{",
	} {
		f.Add([]byte(s))
	}
	f.Fuzz(func(t *testing.T, data []byte) {
		s := newSystem(t)
		if err := os.MkdirAll(filepath.Dir(s.rec), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(s.rec, data, 0o644); err != nil {
			t.Fatal(err)
		}
		s.manager().Restore(ctx)
		for _, c := range s.calls {
			if c[0] != "systemctl" || !isKnownUnit(strings.TrimSuffix(c[len(c)-1], ".service")) || (c[1] != "enable" && c[1] != "start") {
				t.Fatalf("Restore ran %v for record %q", c, data)
			}
		}
	})
}
