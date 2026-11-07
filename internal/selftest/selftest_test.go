package selftest

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/Order52/truesense/internal/firmware"
	"github.com/Order52/truesense/internal/hw"
	"github.com/Order52/truesense/internal/userstate"
)

// clock is a fake time source: Sleep advances Now, so the 22 s fan watch takes no real time.
type clock struct {
	mu  sync.Mutex
	now time.Time
}

func (c *clock) Now() time.Time { c.mu.Lock(); defer c.mu.Unlock(); return c.now }
func (c *clock) Sleep(d time.Duration) {
	c.mu.Lock()
	c.now = c.now.Add(d)
	c.mu.Unlock()
}

// fakeDaemon records every automation change and can fail on a chosen call.
type fakeDaemon struct {
	smart, auto bool
	history     [][2]bool
	setErrAt    int // 1-based call number of SetAutomation that fails; 0 = never
	getErr      error
	cpu         CPUInfo
	cpuErr      error
}

func (d *fakeDaemon) Automation(context.Context) (bool, bool, error) {
	return d.smart, d.auto, d.getErr
}
func (d *fakeDaemon) SetAutomation(_ context.Context, smart, auto bool) error {
	d.history = append(d.history, [2]bool{smart, auto})
	if d.setErrAt == len(d.history) {
		return errors.New("socket closed")
	}
	d.smart, d.auto = smart, auto
	return nil
}
func (d *fakeDaemon) CPUInfo(context.Context) (CPUInfo, error) { return d.cpu, d.cpuErr }

type fakeKeyboard struct {
	b   int
	err error
}

func (k fakeKeyboard) Brightness() (int, error) { return k.b, k.err }

// fakeAudio is a sound system with state, so restores can be checked.
type fakeAudio struct {
	active    string
	boost     int
	hasBoost  bool
	muted     bool
	failBoost int // fail the Nth SetHeadsetBoost (1-based), 0 = never
	boostSets int
	muteStuck bool // SetMicMute is accepted but the state never changes
	muteSets  []bool
}

func (a *fakeAudio) OutputActive(context.Context) (string, error) { return a.active, nil }
func (a *fakeAudio) HeadsetBoost(context.Context) (int, bool, error) {
	return a.boost, a.hasBoost, nil
}
func (a *fakeAudio) SetHeadsetBoost(_ context.Context, level int) error {
	a.boostSets++
	if a.failBoost == a.boostSets {
		return errors.New("boost readback mismatch")
	}
	a.boost = level
	return nil
}
func (a *fakeAudio) MicMuted(context.Context) (bool, error) { return a.muted, nil }
func (a *fakeAudio) SetMicMute(_ context.Context, muted bool) error {
	a.muteSets = append(a.muteSets, muted)
	if !a.muteStuck {
		a.muted = muted
	}
	return nil
}

// flaky wraps the real hw.HW and fails the Nth write (after performing it, like a firmware
// whose read-back does not confirm): the worst case for restoring, because the setting HAS changed.
type flaky struct {
	*hw.HW
	failAt int // 1-based index of the write that reports failure; 0 = never
	writes int
	panics bool
	log    []string
}

func (f *flaky) write(name string, do func() error) error {
	f.writes++
	f.log = append(f.log, name)
	err := do()
	if f.failAt == f.writes {
		if f.panics {
			panic("simulated crash in " + name)
		}
		return fmt.Errorf("simulated failure of write %d (%s)", f.writes, name)
	}
	return err
}

func (f *flaky) SetMode(n string) (string, error) {
	var out string
	err := f.write("mode "+n, func() (e error) { out, e = f.HW.SetMode(n); return })
	return out, err
}
func (f *flaky) SetOverdrive(on bool) (bool, error) {
	var out bool
	err := f.write("overdrive", func() (e error) { out, e = f.HW.SetOverdrive(on); return })
	return out, err
}
func (f *flaky) SetBootSound(on bool) (bool, error) {
	var out bool
	err := f.write("boot", func() (e error) { out, e = f.HW.SetBootSound(on); return })
	return out, err
}
func (f *flaky) SetBatteryLimit(on bool) (bool, error) {
	var out bool
	err := f.write("battery limit", func() (e error) { out, e = f.HW.SetBatteryLimit(on); return })
	return out, err
}
func (f *flaky) SetCalibration(on bool) (bool, error) {
	var out bool
	err := f.write("calibration", func() (e error) { out, e = f.HW.SetCalibration(on); return })
	return out, err
}
func (f *flaky) SetUSBCharge(l int) (int, error) {
	var out int
	err := f.write("usb", func() (e error) { out, e = f.HW.SetUSBCharge(l); return })
	return out, err
}
func (f *flaky) SetKBTimeout(on bool) (bool, error) {
	var out bool
	err := f.write("kbtimeout", func() (e error) { out, e = f.HW.SetKBTimeout(on); return })
	return out, err
}
func (f *flaky) FanManual(c, g int) error {
	return f.write("fan manual", func() error { return f.HW.FanManual(c, g) })
}
func (f *flaky) FanAuto() error { return f.write("fan auto", f.HW.FanAuto) }

type rig struct {
	sim *firmware.Simulator
	hw  *hw.HW
	fl  *flaky
	clk *clock
	d   *fakeDaemon
	au  *fakeAudio
	out *bytes.Buffer
	dep Deps
}

func newRig(t *testing.T) *rig {
	t.Helper()
	sim := firmware.NewSimulator()
	sim.SetSensors([3]int{55, 50, 48}, [2]int{3000, 3100}) // fans that follow a 60 % request
	h := hw.New(sim, &userstate.Store{Path: t.TempDir() + "/user.json"})
	h.SleepFor = func(time.Duration) {}
	r := &rig{sim: sim, hw: h, fl: &flaky{HW: h}, clk: &clock{now: time.Unix(1_800_000_000, 0)}, d: &fakeDaemon{smart: true, auto: true, cpu: CPUInfo{PL1W: 80, PL2W: 100}},
		au: &fakeAudio{active: "speakers", boost: 2, hasBoost: true, muted: false}, out: &bytes.Buffer{}}
	r.dep = Deps{HW: r.fl, Daemon: r.d, Keyboard: fakeKeyboard{b: 25}, Audio: r.au, Out: r.out, Now: r.clk.Now, Sleep: r.clk.Sleep,
		Doctor: func(_ context.Context, w io.Writer) int { fmt.Fprintln(w, "ALL GOOD"); return 0 }}
	return r
}

// settings is every firmware setting the self-test touches, as the simulator holds them. The
// fan duty register is left out: it is a read-only view of the last manual duty, not a setting
// (the fan MODE is what matters, and it is included).
type settings struct {
	Mode, GPU, Boot, Overdrive, USB, KBTimeout int
	Fan                                        string
	Battery                                    map[int]int
	Rear                                       string
}

func (r *rig) settings() settings {
	s := r.sim.Snapshot()
	return settings{s.Mode, s.GPU, s.Boot, s.Overdrive, s.USB, s.KBTimeout, s.Fan, s.Battery, fmt.Sprintf("%x", s.Rear)}
}

// randomise puts non-default values everywhere, so "restored" cannot pass by accident.
func (r *rig) randomise(t *testing.T) {
	t.Helper()
	for _, step := range []func() error{
		func() error { _, err := r.hw.SetMode("turbo"); return err },
		func() error { _, err := r.hw.SetBootSound(true); return err },
		func() error { _, err := r.hw.SetBatteryLimit(true); return err },
		func() error { _, err := r.hw.SetCalibration(true); return err },
		func() error { _, err := r.hw.SetUSBCharge(20); return err },
		func() error { _, err := r.hw.SetKBTimeout(false); return err },
		func() error { _, err := r.hw.SetOverdrive(false); return err },
		func() error { _, err := r.hw.SetGPUMode("auto"); return err },
	} {
		if err := step(); err != nil {
			t.Fatal(err)
		}
	}
}

func TestEverySettingIsRestoredAfterASuccessfulRun(t *testing.T) {
	r := newRig(t)
	r.randomise(t)
	before := r.settings()
	rep := r.run()
	if !rep.OK() {
		t.Fatalf("a healthy simulator failed:\n%s", r.out)
	}
	if after := r.settings(); !reflect.DeepEqual(before, after) {
		t.Fatalf("settings changed:\nbefore %+v\nafter  %+v", before, after)
	}
	if r.sim.Snapshot().Fan != "auto" {
		t.Fatal("fans must end on firmware auto")
	}
	if r.d.smart != true || r.d.auto != true {
		t.Fatalf("the daemon's automation was not restored: %+v", r.d)
	}
	if got := r.d.history; !reflect.DeepEqual(got, [][2]bool{{false, false}, {true, true}}) {
		t.Fatalf("daemon history = %v", got)
	}
	if r.au.boost != 2 || r.au.muted || len(r.au.muteSets) != 2 {
		t.Fatalf("audio not restored: %+v", r.au)
	}
	if !strings.Contains(r.out.String(), "PASS  power modes (all supported)  (cycled quiet,balanced,performance,turbo,eco, restored turbo)") {
		t.Errorf("output:\n%s", r.out)
	}
}

func (r *rig) run() Report { return Run(context.Background(), r.dep, Options{}) }

func TestEverySettingIsRestoredAfterAFailureInTheMiddle(t *testing.T) {
	// Count the writes of a clean run, then fail each one in turn (after it took effect).
	probe := newRig(t)
	probe.randomise(t)
	probe.run()
	total := probe.fl.writes
	if total < 20 {
		t.Fatalf("a clean run only wrote %d times: the test would prove little", total)
	}
	for n := 1; n <= total; n++ {
		r := newRig(t)
		r.randomise(t)
		before := r.settings()
		r.fl.failAt = n
		name := r.fl.log
		rep := r.run()
		out := r.out.String()
		after := r.settings()
		restoreFailed := strings.Contains(out, "RESTORE FAILED") || strings.Contains(out, "FANS NOT BACK") || strings.Contains(out, "fans not back")
		if !reflect.DeepEqual(before, after) && !restoreFailed {
			t.Fatalf("write %d failed (%v): settings differ and nothing says so\nbefore %+v\nafter  %+v\n%s", n, name, before, after, out)
		}
		if rep.OK() {
			t.Fatalf("write %d failed (%v) but the run passed:\n%s", n, name, out)
		}
		if r.d.smart != true || r.d.auto != true {
			t.Fatalf("write %d: automation not restored: %+v", n, r.d)
		}
	}
}

func TestAFailedChangeStillRestoresTheOriginal(t *testing.T) {
	for name, tc := range map[string]struct {
		fail   string
		before func(*rig)
		read   func(*rig) any
	}{
		"overdrive":     {"overdrive", func(r *rig) { _, _ = r.hw.SetOverdrive(false) }, func(r *rig) any { return r.sim.Snapshot().Overdrive }},
		"boot sound":    {"boot", func(r *rig) { _, _ = r.hw.SetBootSound(true) }, func(r *rig) any { return r.sim.Snapshot().Boot }},
		"battery limit": {"battery limit", func(r *rig) { _, _ = r.hw.SetBatteryLimit(true) }, func(r *rig) any { return r.sim.Snapshot().Battery[1] }},
		"calibration":   {"calibration", func(r *rig) { _, _ = r.hw.SetCalibration(true) }, func(r *rig) any { return r.sim.Snapshot().Battery[2] }},
		"usb":           {"usb", func(r *rig) { _, _ = r.hw.SetUSBCharge(10) }, func(r *rig) any { return r.sim.Snapshot().USB }},
		"kb timeout":    {"kbtimeout", func(r *rig) { _, _ = r.hw.SetKBTimeout(false) }, func(r *rig) any { return r.sim.Snapshot().KBTimeout }},
	} {
		t.Run(name, func(t *testing.T) {
			r := newRig(t)
			tc.before(r)
			want := tc.read(r)
			r.dep.HW = &failOn{flaky: r.fl, name: tc.fail}
			rep := r.run()
			if rep.OK() || !strings.Contains(r.out.String(), "FAIL  ") {
				t.Fatalf("a failing write passed:\n%s", r.out)
			}
			if got := tc.read(r); got != want {
				t.Fatalf("not restored: %v, want %v\n%s", got, want, r.out)
			}
			if strings.Contains(r.out.String(), "RESTORE FAILED") {
				t.Fatalf("restore failed:\n%s", r.out)
			}
		})
	}
}

// failOn fails the FIRST write of one named setting (the change), not its restore.
type failOn struct {
	*flaky
	name string
	done bool
}

func (f *failOn) fail(name string) bool {
	if name == f.name && !f.done {
		f.done = true
		return true
	}
	return false
}
func (f *failOn) SetOverdrive(on bool) (bool, error) {
	out, err := f.HW.SetOverdrive(on)
	if f.fail("overdrive") {
		return out, errors.New("simulated")
	}
	return out, err
}
func (f *failOn) SetBootSound(on bool) (bool, error) {
	out, err := f.HW.SetBootSound(on)
	if f.fail("boot") {
		return out, errors.New("simulated")
	}
	return out, err
}
func (f *failOn) SetBatteryLimit(on bool) (bool, error) {
	out, err := f.HW.SetBatteryLimit(on)
	if f.fail("battery limit") {
		return out, errors.New("simulated")
	}
	return out, err
}
func (f *failOn) SetCalibration(on bool) (bool, error) {
	out, err := f.HW.SetCalibration(on)
	if f.fail("calibration") {
		return out, errors.New("simulated")
	}
	return out, err
}
func (f *failOn) SetUSBCharge(l int) (int, error) {
	out, err := f.HW.SetUSBCharge(l)
	if f.fail("usb") {
		return out, errors.New("simulated")
	}
	return out, err
}
func (f *failOn) SetKBTimeout(on bool) (bool, error) {
	out, err := f.HW.SetKBTimeout(on)
	if f.fail("kbtimeout") {
		return out, errors.New("simulated")
	}
	return out, err
}

func TestAPanicInTheMiddleIsAFailOfThatItemAndStillRestores(t *testing.T) {
	r := newRig(t)
	r.randomise(t)
	before := r.settings()
	// write 3 is a power mode change during the cycle
	r.fl.failAt, r.fl.panics = 3, true
	rep := r.run()
	if rep.OK() || !strings.Contains(r.out.String(), "panic: simulated crash") {
		t.Fatalf("output:\n%s", r.out)
	}
	if after := r.settings(); !reflect.DeepEqual(before, after) {
		t.Fatalf("not restored after a panic:\nbefore %+v\nafter  %+v", before, after)
	}
	if !strings.Contains(r.out.String(), "PASS  LCD overdrive") {
		t.Errorf("the run must go on after a failed item:\n%s", r.out)
	}
}

func TestFansAlwaysEndOnFirmwareAuto(t *testing.T) {
	t.Run("they hold the duty", func(t *testing.T) {
		r := newRig(t)
		rep := r.run()
		if !rep.OK() || !strings.Contains(r.out.String(), "PASS  fans: manual 60 % holds, then back to auto  (one write held both fans >2000 rpm in 100 % of samples for 22 s)") {
			t.Fatalf("output:\n%s", r.out)
		}
		if got := r.fl.log; countOf(got, "fan manual") != 1 || countOf(got, "fan auto") != 1 {
			t.Errorf("exactly ONE manual write, no refresh, and one auto: %v", got)
		}
	})
	t.Run("they do not hold it", func(t *testing.T) {
		r := newRig(t)
		r.sim.SetSensors([3]int{55, 50, 48}, [2]int{0, 0})
		rep := r.run()
		if rep.OK() || !strings.Contains(r.out.String(), "FAIL  fans: manual 60 % holds, then back to auto: fans held 60 % in only 0 % of samples") {
			t.Fatalf("output:\n%s", r.out)
		}
		if r.sim.Snapshot().Fan != "auto" {
			t.Fatal("fans left off auto")
		}
	})
	t.Run("the manual write itself fails", func(t *testing.T) {
		r := newRig(t)
		r.fl.failAt = fanManualIndex(t)
		r.run()
		if r.sim.Snapshot().Fan != "auto" {
			t.Fatal("fans left off auto")
		}
	})
	t.Run("Ctrl-C during the watch", func(t *testing.T) {
		r := newRig(t)
		ctx, cancel := context.WithCancel(context.Background())
		r.dep.Sleep = func(d time.Duration) {
			r.clk.Sleep(d)
			if r.clk.Now().Sub(time.Unix(1_800_000_000, 0)) > 5*time.Second {
				cancel()
			}
		}
		rep := Run(ctx, r.dep, Options{})
		if r.sim.Snapshot().Fan != "auto" {
			t.Fatal("fans left off auto after a cancel")
		}
		if rep.OK() || !strings.Contains(r.out.String(), "context canceled") {
			t.Fatalf("output:\n%s", r.out)
		}
		if r.d.smart != true || r.d.auto != true {
			t.Fatalf("automation not restored after a cancel: %+v", r.d)
		}
	})
	t.Run("--skip-fans never touches them", func(t *testing.T) {
		r := newRig(t)
		rep := Run(context.Background(), r.dep, Options{SkipFans: true})
		if !rep.OK() || !strings.Contains(r.out.String(), "SKIP  fans: --skip-fans") || countOf(r.fl.log, "fan manual") != 0 {
			t.Fatalf("output:\n%s\nlog %v", r.out, r.fl.log)
		}
		if rep.Total != rep.Passed || len(rep.Items) <= rep.Total {
			t.Errorf("a SKIP is listed but not counted: %+v", rep)
		}
	})
}

func countOf(list []string, s string) int {
	n := 0
	for _, x := range list {
		if x == s {
			n++
		}
	}
	return n
}

func fanManualIndex(t *testing.T) int {
	t.Helper()
	probe := newRig(t)
	probe.run()
	for i, w := range probe.fl.log {
		if w == "fan manual" {
			return i + 1
		}
	}
	t.Fatal("no fan manual write in a clean run")
	return 0
}

func TestTheDaemonsAutomationIsPausedAndPutBack(t *testing.T) {
	r := newRig(t)
	r.d.smart, r.d.auto = true, false // the user's own mix must come back exactly
	rep := r.run()
	if !rep.OK() {
		t.Fatalf("output:\n%s", r.out)
	}
	if r.d.smart != true || r.d.auto != false {
		t.Fatalf("restored %+v", r.d)
	}
	var settled time.Duration
	r2 := newRig(t)
	r2.dep.Sleep = func(d time.Duration) {
		if d == 2500*time.Millisecond {
			settled = d
		}
		r2.clk.Sleep(d)
	}
	r2.run()
	if settled != 2500*time.Millisecond {
		t.Error("the daemon must get 2.5 s to notice it was paused")
	}
}

func TestAHalfFailedPauseStillRestores(t *testing.T) {
	r := newRig(t)
	r.d.setErrAt = 1 // the very first change fails
	rep := r.run()
	if !rep.OK() {
		t.Fatalf("output:\n%s", r.out)
	}
	if r.d.smart != true || r.d.auto != true {
		t.Fatalf("state = %+v", r.d)
	}
	if len(r.d.history) < 2 {
		t.Fatalf("an attempted pause must be followed by a restore: %v", r.d.history)
	}
}

func TestARestoreOfTheDaemonThatFailsIsReported(t *testing.T) {
	r := newRig(t)
	r.d.setErrAt = 2
	rep := r.run()
	if rep.OK() || !strings.Contains(r.out.String(), "restoring the daemon's smart fan / auto profile") {
		t.Fatalf("output:\n%s", r.out)
	}
}

func TestWithoutADaemonNothingIsPausedAndTheCPULimitsItemFails(t *testing.T) {
	r := newRig(t)
	r.dep.Daemon = nil
	rep := r.run()
	if rep.OK() || !strings.Contains(r.out.String(), "FAIL  CPU power limits (read): the TrueSense daemon is not running") {
		t.Fatalf("output:\n%s", r.out)
	}
	r = newRig(t)
	r.d.getErr = errors.New("not running")
	if rep := r.run(); len(r.d.history) != 0 {
		t.Fatalf("nothing to pause: %v (%v)", r.d.history, rep.OK())
	}
}

func TestCPULimits(t *testing.T) {
	for name, tc := range map[string]struct {
		info CPUInfo
		err  error
		want string
	}{
		"automatic": {CPUInfo{PL1W: 65, PL2W: 80}, nil, "PASS  CPU power limits (read)  (PL1 65 W / PL2 80 W readable (automatic))"},
		"yours":     {CPUInfo{PL1W: 45, PL2W: 60, User: true}, nil, "(PL1 45 W / PL2 60 W readable (yours))"},
		"old":       {CPUInfo{}, errors.New("unknown command: cpu_info"), "(the running service is older than this app (skipped: re-run the installer))"},
		"broken":    {CPUInfo{}, errors.New("timeout"), "FAIL  CPU power limits (read): timeout"},
	} {
		t.Run(name, func(t *testing.T) {
			r := newRig(t)
			r.d.cpu, r.d.cpuErr = tc.info, tc.err
			r.run()
			if !strings.Contains(r.out.String(), tc.want) {
				t.Fatalf("output:\n%s", r.out)
			}
		})
	}
}

func TestToggleReportsAReadbackMismatchAndStillRestores(t *testing.T) {
	stored := true
	_, err := toggle(func() (bool, error) { return stored, nil }, func(v bool) (bool, error) {
		if v != true { // the change is "accepted" but never takes effect; restoring works
			return stored, nil
		}
		stored = v
		return v, nil
	}, func(v bool) bool { return !v })
	if err == nil || !strings.Contains(err.Error(), "readback mismatch") {
		t.Fatalf("err = %v", err)
	}
	// a restore that does not stick is called out loudly
	stored = false
	_, err = toggle(func() (bool, error) { return stored, nil }, func(v bool) (bool, error) {
		if v == true { // the change works, the restore (to false) does not
			stored = true
		}
		return v, nil
	}, func(v bool) bool { return !v })
	if err == nil || !strings.Contains(err.Error(), "RESTORE FAILED") {
		t.Fatalf("err = %v", err)
	}
}

func TestAudioItems(t *testing.T) {
	t.Run("no boost control", func(t *testing.T) {
		r := newRig(t)
		r.au.hasBoost = false
		r.run()
		if !strings.Contains(r.out.String(), "PASS  headset mic boost  (no headset mic boost control on this card (skipped))") || r.au.boostSets != 0 {
			t.Fatalf("output:\n%s", r.out)
		}
	})
	t.Run("boost fails in the middle and is restored", func(t *testing.T) {
		r := newRig(t)
		r.au.failBoost = 2 // the "3" step
		r.run()
		if !strings.Contains(r.out.String(), "FAIL  headset mic boost: boost 3 did not read back") || r.au.boost != 2 {
			t.Fatalf("boost = %d\n%s", r.au.boost, r.out)
		}
	})
	t.Run("a mute that does not read back is restored", func(t *testing.T) {
		r := newRig(t)
		r.au.muted, r.au.muteStuck = true, true
		r.run()
		if !strings.Contains(r.out.String(), "FAIL  microphone mute: mute did not read back") || len(r.au.muteSets) != 2 || r.au.muteSets[1] != true {
			t.Fatalf("sets = %v\n%s", r.au.muteSets, r.out)
		}
	})
	t.Run("an output profile that is neither", func(t *testing.T) {
		r := newRig(t)
		r.au.active = "pro-audio"
		r.run()
		if !strings.Contains(r.out.String(), `FAIL  audio output profile: unexpected output profile "pro-audio"`) {
			t.Fatalf("output:\n%s", r.out)
		}
	})
}

func TestKeyboardSensorsAndDoctorItems(t *testing.T) {
	r := newRig(t)
	r.dep.Keyboard = fakeKeyboard{b: 99}
	r.sim.SetSensors([3]int{5, 50, 48}, [2]int{3000, 3000})
	r.dep.Doctor = func(_ context.Context, w io.Writer) int { fmt.Fprintln(w, "PROBLEMS FOUND"); return 1 }
	rep := r.run()
	out := r.out.String()
	for _, want := range []string{"FAIL  keyboard lighting interface: brightness 99 is outside 0..50", "FAIL  sensors: temperatures out of range: CPU 5 C, GPU 48 C", "PROBLEMS FOUND", "\n\n== stack health (doctor)\n"} {
		if !strings.Contains(out, want) {
			t.Errorf("output lacks %q:\n%s", want, out)
		}
	}
	if rep.OK() || rep.Passed == rep.Total {
		t.Fatalf("report = %+v", rep)
	}
	last := rep.Items[len(rep.Items)-1]
	if last.Name != "stack health (doctor)" || last.Outcome != Fail {
		t.Fatalf("last item = %+v", last)
	}
	r = newRig(t)
	r.dep.Keyboard = fakeKeyboard{err: errors.New("no hidraw")}
	r.dep.Doctor = nil
	r.run()
	if !strings.Contains(r.out.String(), "FAIL  keyboard lighting interface: no hidraw") || !strings.Contains(r.out.String(), "SKIP  stack health (doctor)") {
		t.Fatalf("output:\n%s", r.out)
	}
}

func TestSummaryLineAndReadItems(t *testing.T) {
	r := newRig(t)
	rep := r.run()
	if !rep.OK() || rep.Total != 17 {
		t.Fatalf("total = %d, ok = %v\n%s", rep.Total, rep.OK(), r.out)
	}
	if !strings.HasSuffix(r.out.String(), "\n17/17 passed\n") {
		t.Errorf("output ends %q", r.out.String()[max(0, r.out.Len()-40):])
	}
	for _, want := range []string{"PASS  GPU mode (read)  (nvidia)", "PASS  rear light (read)  ({", "PASS  keyboard lighting interface  (brightness 25/50 read back from the keyboard)", "PASS  audio output profile  (active=speakers)",
		"PASS  sensors  (CPU 55 C, GPU 48 C, fans 3000/3100 rpm)", "PASS  USB charging level  (30 -> 10 -> restored)", "PASS  LCD overdrive  (true -> false -> restored)"} {
		if !strings.Contains(r.out.String(), want) {
			t.Errorf("output lacks %q:\n%s", want, r.out)
		}
	}
}
