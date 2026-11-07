package services

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"reflect"
	"slices"
	"strings"
	"testing"
	"time"
)

// fakeSystem is systemctl, modprobe and dkms as far as the repair cares. calls is every
// command issued, as one slice per program run.
type fakeSystem struct {
	calls                      [][]string
	module, dkmsFixes          bool
	restartFixes, socket       bool
	moduleAfterDKMS            bool
	state                      map[unitKey]*unitInfo
	pings                      []bool // successive answers; the last one repeats
	pingCalls                  int
	alwaysAnswer, neverAnswers bool
}

type unitKey struct {
	user bool
	unit string
}

type unitInfo struct{ enabled, active string }

type fakeOpts struct {
	svc, enabled   string
	module         bool
	dkmsFixes      bool
	restartFixes   bool
	user           map[string]unitInfo
	removeUserUnit string
}

func newFake(o fakeOpts) *fakeSystem {
	if o.svc == "" {
		o.svc = "active"
	}
	if o.enabled == "" {
		o.enabled = "enabled"
	}
	f := &fakeSystem{module: o.module, dkmsFixes: o.dkmsFixes, restartFixes: o.restartFixes, socket: o.svc == "active", state: map[unitKey]*unitInfo{}}
	for _, u := range SystemUnits {
		active := "active"
		if u == "truesense.service" {
			active = o.svc
		}
		f.state[unitKey{false, u}] = &unitInfo{o.enabled, active}
	}
	for _, u := range UserUnits {
		st := unitInfo{"enabled", "active"}
		if over, ok := o.user[u]; ok {
			if over.enabled != "" {
				st.enabled = over.enabled
			}
			if over.active != "" {
				st.active = over.active
			}
		}
		f.state[unitKey{true, u}] = &st
	}
	if o.removeUserUnit != "" {
		delete(f.state, unitKey{true, o.removeUserUnit})
	}
	return f
}

// healthy is the default: everything running, the module present, the restart working.
func healthy() *fakeSystem { return newFake(fakeOpts{module: true, restartFixes: true}) }

func (f *fakeSystem) run(_ context.Context, name string, args ...string) Result {
	cmd := append([]string{name}, args...)
	f.calls = append(f.calls, cmd)
	var out, errText string
	code := 0
	switch name {
	case "systemctl":
		user := slices.Contains(args, "--user")
		var rest []string
		for _, a := range args {
			if a != "--user" {
				rest = append(rest, a)
			}
		}
		verb, unit := rest[0], rest[len(rest)-1]
		st := f.state[unitKey{user, unit}]
		switch {
		case verb == "is-enabled" || verb == "is-active":
			out, code = "not-found", 4
			if st != nil {
				out, code = st.active, 0
				if verb == "is-enabled" {
					out = st.enabled
				}
			}
		case (verb == "enable" || verb == "start" || verb == "restart") && st == nil:
			code, errText = 1, "Unit "+unit+" does not exist"
		case verb == "enable":
			st.enabled = "enabled"
			if slices.Contains(rest, "--now") {
				st.active = "active"
			}
		case verb == "start":
			st.active = "active"
		case verb == "restart":
			st.active = "failed"
			if f.restartFixes {
				st.active = "active"
			}
			f.socket = f.restartFixes
		}
	case "dkms":
		if f.dkmsFixes {
			f.moduleAfterDKMS = true
		}
	case "modprobe":
		if f.moduleAfterDKMS {
			f.module = true
		}
	}
	if out != "" {
		out += "\n"
	}
	return Result{Stdout: out, Stderr: errText, Code: code}
}

func (f *fakeSystem) exists(p string) bool {
	switch p {
	case DevicePath:
		return f.module
	case SocketPath:
		return f.socket
	}
	return false
}

// Ping answers from the scripted list.
func (f *fakeSystem) Ping(context.Context) error {
	f.pingCalls++
	ok := true
	switch {
	case f.neverAnswers:
		ok = false
	case f.alwaysAnswer:
	case len(f.pings) > 0:
		i := min(f.pingCalls-1, len(f.pings)-1)
		ok = f.pings[i]
	}
	if !ok {
		return errors.New("no pong")
	}
	return nil
}

func (f *fakeSystem) manager() *Manager {
	return &Manager{Run: f.run, Exists: f.exists, Ping: f, Sleep: func(time.Duration) {}, Kernel: "6.1.0-test", Wait: 15 * time.Second, SocketPath: SocketPath, DevicePath: DevicePath}
}

func programs(calls [][]string) map[string]bool {
	out := map[string]bool{}
	for _, c := range calls {
		out[c[0]] = true
	}
	return out
}

// changes lists the calls that alter something: restart, enable, start, dkms and modprobe.
func changes(calls [][]string) [][]string {
	var out [][]string
	for _, c := range calls {
		if c[0] == "dkms" || c[0] == "modprobe" || (c[0] == "systemctl" && (slices.Contains(c, "restart") || slices.Contains(c, "enable") || slices.Contains(c, "start"))) {
			out = append(out, c)
		}
	}
	return out
}

func hasCall(calls [][]string, want ...string) bool {
	return slices.ContainsFunc(calls, func(c []string) bool { return slices.Equal(c, want) })
}

func TestAHealthyCheckIsAllOK(t *testing.T) {
	f := healthy()
	f.alwaysAnswer = true
	rows := f.manager().Check(context.Background())
	for _, r := range rows {
		if !r.OK {
			t.Errorf("not ok: %+v", r)
		}
	}
	if want := len(SystemUnits) + len(UserUnits) + 2; len(rows) != want {
		t.Fatalf("%d rows, want %d", len(rows), want)
	}
	if p := programs(f.calls); len(p) != 1 || !p["systemctl"] {
		t.Fatalf("programs run: %v", p)
	}
	if c := changes(f.calls); len(c) != 0 {
		t.Fatalf("check changed something: %v", c)
	}
}

func TestEveryProblemIsListedWithItsFix(t *testing.T) {
	f := newFake(fakeOpts{svc: "failed", user: map[string]unitInfo{"truesense-keys.service": {active: "inactive"}}})
	f.alwaysAnswer = true
	rows := f.manager().Check(context.Background())
	bad := map[string]Unit{}
	for _, r := range rows {
		if !r.OK {
			bad[r.Unit] = r
		}
	}
	for _, name := range []string{"truesense.service", "kernel module (/dev/truesense)", "truesense-keys.service"} {
		if _, ok := bad[name]; !ok {
			t.Errorf("%s not reported (bad = %v)", name, bad)
		}
	}
	if !strings.Contains(bad["truesense.service"].Fix, "sudo truesense service repair") {
		t.Errorf("fix = %q", bad["truesense.service"].Fix)
	}
	if bad["truesense-keys.service"].Fix != "truesense service repair" {
		t.Errorf("a user service is fixed without sudo, got %q", bad["truesense-keys.service"].Fix)
	}
	if !strings.Contains(Render(rows), "!!") {
		t.Error("render must mark problems with !!")
	}
}

func TestASocketThatExistsButDoesNotAnswerIsFlagged(t *testing.T) {
	f := healthy()
	f.neverAnswers = true
	var sock Unit
	for _, r := range f.manager().Check(context.Background()) {
		if r.Unit == "control socket" {
			sock = r
		}
	}
	if sock.OK || sock.Active != "silent" {
		t.Fatalf("socket row = %+v", sock)
	}
	f.socket = false
	for _, r := range f.manager().Check(context.Background()) {
		if r.Unit == "control socket" && (r.OK || r.Active != "missing") {
			t.Fatalf("missing socket row = %+v", r)
		}
	}
}

func TestUnitRowJSONNamesAreTheOldOnes(t *testing.T) {
	b, err := json.Marshal(Unit{Unit: "u", Scope: "system", Enabled: "e", Active: "a", OK: true})
	if err != nil {
		t.Fatal(err)
	}
	if string(b) != `{"unit":"u","scope":"system","enabled":"e","active":"a","ok":true,"fix":""}` {
		t.Fatalf("json = %s", b)
	}
	b, _ = json.Marshal([]Step{{"did it", true}})
	if string(b) != `[["did it",true]]` {
		t.Fatalf("steps json = %s (clients read [what, ok] pairs)", b)
	}
	var back []Step
	if err := json.Unmarshal(b, &back); err != nil || !reflect.DeepEqual(back, []Step{{"did it", true}}) {
		t.Fatalf("round trip: %v %v", back, err)
	}
	if err := json.Unmarshal([]byte(`["x"]`), &back); err == nil {
		t.Error("a one-element pair must be rejected")
	}
}

func allOK(steps []Step) bool {
	for _, s := range steps {
		if !s.OK {
			return false
		}
	}
	return true
}

func hasStep(steps []Step, contains string, ok bool) bool {
	return slices.ContainsFunc(steps, func(s Step) bool { return strings.Contains(s.What, contains) && s.OK == ok })
}

func TestARepairOfAHealthySystemLeavesItAlone(t *testing.T) {
	f := healthy()
	f.alwaysAnswer = true
	steps := f.manager().RepairSystem(context.Background())
	if !allOK(steps) {
		t.Fatalf("steps = %v", steps)
	}
	if c := changes(f.calls); len(c) != 0 {
		t.Fatalf("changed a healthy system: %v", c)
	}
	if !hasStep(steps, "left alone", true) {
		t.Fatalf("steps = %v", steps)
	}
}

func TestADeadDaemonIsRestartedAndAwaited(t *testing.T) {
	f := newFake(fakeOpts{svc: "failed", module: true, restartFixes: true})
	f.pings = []bool{false, false, true}
	var slept []time.Duration
	m := f.manager()
	m.Sleep = func(d time.Duration) { slept = append(slept, d) }
	steps := m.RepairSystem(context.Background())
	if !allOK(steps) || !hasCall(f.calls, "systemctl", "restart", "truesense.service") || !hasStep(steps, "answers", true) {
		t.Fatalf("steps = %v calls = %v", steps, f.calls)
	}
	if len(slept) == 0 || slept[0] != 500*time.Millisecond {
		t.Fatalf("it should poll every 0.5 s, slept %v", slept)
	}
}

func TestADisabledUnitIsEnabled(t *testing.T) {
	f := newFake(fakeOpts{enabled: "disabled", module: true, restartFixes: true})
	f.alwaysAnswer = true
	f.manager().RepairSystem(context.Background())
	for _, u := range SystemUnits {
		if !hasCall(f.calls, "systemctl", "enable", u) {
			t.Errorf("%s not enabled; calls = %v", u, f.calls)
		}
	}
}

func TestAMissingModuleIsRebuiltWithDKMSForThisKernelThenLoaded(t *testing.T) {
	f := newFake(fakeOpts{module: false, dkmsFixes: true, restartFixes: true})
	f.alwaysAnswer = true
	steps := f.manager().RepairSystem(context.Background())
	if !hasCall(f.calls, "dkms", "autoinstall", "-k", "6.1.0-test") || !f.module || !hasStep(steps, "kernel module loaded", true) {
		t.Fatalf("steps = %v calls = %v", steps, f.calls)
	}
}

func TestAModuleThatNeverAppearsIsAClearFailure(t *testing.T) {
	f := newFake(fakeOpts{module: false, restartFixes: true})
	f.alwaysAnswer = true
	steps := f.manager().RepairSystem(context.Background())
	if !hasStep(steps, "kernel module still missing", false) || !hasStep(steps, "supported laptop", false) {
		t.Fatalf("steps = %v", steps)
	}
}

func TestADaemonThatCannotStartIsReportedWithWhereToLook(t *testing.T) {
	f := newFake(fakeOpts{svc: "failed", module: true})
	f.neverAnswers = true
	steps := f.manager().RepairSystem(context.Background())
	if !hasStep(steps, "journalctl -u truesense", false) {
		t.Fatalf("steps = %v", steps)
	}
}

func TestTheSecondRepairChangesNothing(t *testing.T) {
	f := newFake(fakeOpts{svc: "failed", enabled: "disabled", module: true, restartFixes: true})
	f.alwaysAnswer = true
	m := f.manager()
	if first := m.RepairSystem(context.Background()); len(first) == 0 || len(f.calls) == 0 {
		t.Fatal("first run did nothing")
	}
	f.calls = nil
	m.RepairSystem(context.Background())
	if c := changes(f.calls); len(c) != 0 {
		t.Fatalf("not idempotent: %v", c)
	}
}

func TestARepairOnlyEverRunsSystemctlModprobeAndDKMS(t *testing.T) {
	f := newFake(fakeOpts{svc: "failed", enabled: "disabled", module: false, dkmsFixes: true, restartFixes: true})
	f.alwaysAnswer = true
	m := f.manager()
	m.RepairSystem(context.Background())
	m.RepairUser(context.Background())
	for p := range programs(f.calls) {
		if p != "systemctl" && p != "modprobe" && p != "dkms" {
			t.Errorf("ran %s: the repair never touches fans or settings", p)
		}
	}
}

func TestAFailedStepSaysWhy(t *testing.T) {
	m := &Manager{
		Run: func(_ context.Context, name string, args ...string) Result {
			if name == "systemctl" && args[0] == "daemon-reload" {
				return Result{Stderr: "Failed to connect to bus", Code: 1}
			}
			return Result{}
		},
		Exists: func(string) bool { return true }, Ping: nil, Sleep: func(time.Duration) {}, Wait: time.Second,
	}
	steps := m.RepairSystem(context.Background())
	if steps[0].OK || steps[0].What != "systemd re-reads the unit files (Failed to connect to bus)" {
		t.Fatalf("first step = %+v", steps[0])
	}
}

func TestStoppedUserServicesAreEnabledAndStartedAndRunningOnesLeftAlone(t *testing.T) {
	f := newFake(fakeOpts{user: map[string]unitInfo{"truesense-keys.service": {active: "inactive", enabled: "disabled"}}})
	m := f.manager()
	steps := m.RepairUser(context.Background())
	if !hasCall(f.calls, "systemctl", "--user", "enable", "--now", "truesense-keys.service") || !allOK(steps) {
		t.Fatalf("steps = %v calls = %v", steps, f.calls)
	}
	n := 0
	for _, c := range f.calls {
		if slices.Contains(c, "enable") {
			n++
		}
	}
	if n != 1 {
		t.Fatalf("%d enable calls, want only the broken one", n)
	}
	f.calls = nil
	steps = m.RepairUser(context.Background())
	for _, c := range f.calls {
		if slices.Contains(c, "enable") {
			t.Fatalf("second run enabled something: %v", c)
		}
	}
	if !hasStep(steps, "already running", true) {
		t.Fatalf("steps = %v", steps)
	}
}

func TestAUnitThatIsNotInstalledIsReportedNotRaised(t *testing.T) {
	f := newFake(fakeOpts{removeUserUnit: "truesense-audio.service"})
	steps := f.manager().RepairUser(context.Background())
	if !hasStep(steps, "truesense-audio.service", false) {
		t.Fatalf("steps = %v", steps)
	}
}

func TestRenderLayout(t *testing.T) {
	got := Render([]Unit{
		{Unit: "truesense.service", Scope: "system", Enabled: "enabled", Active: "active", OK: true},
		{Unit: "control socket", Scope: "system", Enabled: "-", Active: "silent", Fix: "sudo truesense service repair"},
	})
	want := "  [ok ] truesense.service                  system enabled   active\n" +
		"  [!! ] control socket                     system -         silent   -> sudo truesense service repair"
	if got != want {
		t.Fatalf("render =\n%s\nwant\n%s", got, want)
	}
}

// TestHelperProcess is the test binary playing "an external program" for ExecRunner (the
// standard os/exec test idiom): no real program is ever run.
func TestHelperProcess(t *testing.T) {
	mode := os.Getenv("TRUESENSE_HELPER")
	if mode == "" {
		t.Skip("only runs as a child of ExecRunner")
	}
	switch mode {
	case "exit3":
		fmt.Println("out")
		fmt.Fprintln(os.Stderr, "err")
		os.Exit(3)
	case "sleep":
		time.Sleep(10 * time.Second)
	}
	os.Exit(0)
}

func TestExecRunnerSeparatesFailureFromNotInstalled(t *testing.T) {
	self := os.Args[0]
	helper := func(mode string) []string {
		t.Setenv("TRUESENSE_HELPER", mode)
		return []string{"-test.run=^TestHelperProcess$"}
	}
	run := ExecRunner(5 * time.Second)
	if r := run(context.Background(), "/nonexistent/program"); r.Code != 127 || r.Stderr == "" {
		t.Errorf("missing program = %+v", r)
	}
	if r := run(context.Background(), self, helper("exit3")...); r.Code != 3 || !strings.Contains(r.Stdout, "out\n") || r.Stderr != "err\n" {
		t.Errorf("exit 3 = %+v", r)
	}
	if r := run(context.Background(), self, helper("ok")...); r.Code != 0 {
		t.Errorf("exit 0 = %+v", r)
	}
	if r := ExecRunner(100*time.Millisecond)(context.Background(), self, helper("sleep")...); r.Code != 127 {
		t.Errorf("a timeout is Code 127 like the Python, got %+v", r)
	}
}
