// Package selftest is `truesense selftest`: set, read back and restore every firmware feature,
// then run the doctor. It prints PASS / FAIL / SKIP per item and the run fails on any FAIL.
//
// Safety is the point of the design: every item that changes something puts the original back
// from a deferred function, so an error in the middle, a panic or a cancelled context (Ctrl-C)
// still restores the setting, and the fans always end on firmware Auto. The restore uses a
// context that cannot be cancelled. While it runs the daemon's smart fan and automatic profile
// are paused (and put back afterwards), so nothing fights the test.
package selftest

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strings"
	"time"

	"github.com/Order52/truesense/internal/hw"
)

// HW is the firmware as the self-test uses it. *hw.HW satisfies it.
type HW interface {
	GetMode() (string, error)
	SetMode(name string) (string, error)
	SupportedModes() ([]string, error)
	ReadSensors() (hw.Sensors, error)
	FanManual(cpu, gpu int) error
	FanAuto() error
	GetFanState() (hw.FanState, error)
	GetOverdrive() (on, ok bool, err error)
	SetOverdrive(on bool) (bool, error)
	GetBootSound() (bool, error)
	SetBootSound(on bool) (bool, error)
	GetBatteryLimit() (bool, error)
	SetBatteryLimit(on bool) (bool, error)
	GetCalibration() (bool, error)
	SetCalibration(on bool) (bool, error)
	GetUSBCharge() (int, error)
	SetUSBCharge(level int) (int, error)
	GetKBTimeout() (bool, error)
	SetKBTimeout(on bool) (bool, error)
	GetGPUMode() (string, error)
	RearGet() (hw.Rear, error)
}

// CPUInfo is what the daemon reports about the CPU power limits.
type CPUInfo struct {
	PL1W int  `json:"pl1_w"`
	PL2W int  `json:"pl2_w"`
	User bool `json:"user"` // the limits were set by the user, not automatic
}

// Daemon is the control socket as the self-test uses it; internal/ipc's client satisfies it
// through a thin adapter. A nil Daemon means "no daemon": nothing is paused.
type Daemon interface {
	// Automation reads the smart fan and automatic profile switches.
	Automation(ctx context.Context) (smartFan, autoProfile bool, err error)
	// SetAutomation changes both switches.
	SetAutomation(ctx context.Context, smartFan, autoProfile bool) error
	CPUInfo(ctx context.Context) (CPUInfo, error)
}

// Keyboard reads the keyboard backlight (0..50) from the keyboard controller.
type Keyboard interface {
	Brightness() (int, error)
}

// Audio is the sound system as the self-test uses it; internal/audio's Controller has these
// methods (the interface lives here because selftest sits below audio in the layering).
type Audio interface {
	// OutputActive is the active output profile: "speakers" or "headphones".
	OutputActive(ctx context.Context) (string, error)
	// HeadsetBoost reads the headset microphone boost (0..3); found is false when the card has none.
	HeadsetBoost(ctx context.Context) (level int, found bool, err error)
	// SetHeadsetBoost sets it and verifies the read-back.
	SetHeadsetBoost(ctx context.Context, level int) error
	// MicMuted reports whether the default microphone is muted.
	MicMuted(ctx context.Context) (bool, error)
	// SetMicMute mutes or unmutes every input.
	SetMicMute(ctx context.Context, muted bool) error
}

// Deps is everything the self-test touches.
type Deps struct {
	HW       HW
	Daemon   Daemon
	Keyboard Keyboard
	Audio    Audio
	// Doctor runs the health check, writes its report to w and returns its exit code.
	Doctor func(ctx context.Context, w io.Writer) int
	Out    io.Writer
	Now    func() time.Time
	Sleep  func(time.Duration)
}

// Options choose what to skip.
type Options struct {
	SkipFans bool
}

// Outcome of one item.
type Outcome string

// The three outcomes; SKIP is not counted.
const (
	Pass Outcome = "PASS"
	Fail Outcome = "FAIL"
	Skip Outcome = "SKIP"
)

// Item is one line of the run.
type Item struct {
	Name    string  `json:"name"`
	Outcome Outcome `json:"outcome"`
	Detail  string  `json:"detail"`
}

// Report is the whole run.
type Report struct {
	Items  []Item
	Passed int
	Total  int
}

// OK reports whether nothing failed.
func (r Report) OK() bool { return r.Passed == r.Total }

// Timing of the fan check: ONE manual write is made, then the fans are watched for 22 s; the
// first 8 s are the spin-up allowance. 2000 rpm is far above the idle speed of either fan.
const (
	fanHoldDuty   = 60
	fanWatch      = 22 * time.Second
	fanSpinUp     = 8 * time.Second
	fanMinRPM     = 2000
	fanMinPercent = 85
	pauseSettle   = 2500 * time.Millisecond
)

type runner struct {
	d      Deps
	ctx    context.Context
	report Report
}

// Run does the whole self-test, printing to d.Out as it goes. Every setting it touches is back
// to what it was when Run returns, whatever happened in between.
func Run(ctx context.Context, d Deps, o Options) Report {
	r := &runner{d: d, ctx: ctx}
	say(d.Out, "== firmware features (set -> read back -> restore)")
	restoreAutomation := r.pauseAutomation()
	r.check("power modes (all supported)", r.modes)
	r.check("sensors", r.sensors)
	if o.SkipFans {
		r.skip("fans", "--skip-fans")
	} else {
		r.check("fans: manual 60 % holds, then back to auto", r.fans)
	}
	restoreAutomation()

	h := d.HW
	r.check("LCD overdrive", func() (string, error) {
		return toggle(func() (bool, error) { on, ok, err := h.GetOverdrive(); return on, errUnknown(ok, err, "overdrive") }, h.SetOverdrive, not)
	})
	r.check("boot animation/sound", func() (string, error) { return toggle(h.GetBootSound, h.SetBootSound, not) })
	r.check("battery 80 % limit", func() (string, error) { return toggle(h.GetBatteryLimit, h.SetBatteryLimit, not) })
	r.check("battery calibration flag", func() (string, error) { return toggle(h.GetCalibration, h.SetCalibration, not) })
	r.check("USB charging level", func() (string, error) {
		return toggle(h.GetUSBCharge, h.SetUSBCharge, func(v int) int {
			if v != 10 {
				return 10
			}
			return 20
		})
	})
	r.check("keyboard backlight timeout", func() (string, error) { return toggle(h.GetKBTimeout, h.SetKBTimeout, not) })
	r.check("GPU mode (read)", func() (string, error) { return h.GetGPUMode() })
	r.check("rear light (read)", func() (string, error) {
		rear, err := h.RearGet()
		if err != nil {
			return "", err
		}
		b, _ := json.Marshal(rear)
		return string(b), nil
	})
	r.check("keyboard lighting interface", r.keyboard)
	r.check("audio output profile", r.audioOutput)
	r.check("headset mic boost", r.headsetBoost)
	r.check("microphone mute", r.micMute)
	r.check("CPU power limits (read)", r.cpuLimits)

	say(d.Out, "\n== stack health (doctor)")
	if d.Doctor == nil {
		r.skip("stack health (doctor)", "no health check wired in")
	} else {
		var buf strings.Builder
		rc := d.Doctor(ctx, &buf)
		say(d.Out, strings.TrimRight(buf.String(), "\n"))
		r.record(Item{Name: "stack health (doctor)", Outcome: outcomeOf(rc == 0)})
	}
	sayf(d.Out, "\n%d/%d passed\n", r.report.Passed, r.report.Total)
	return r.report
}

// say and sayf print a line of the run; a closed output must not stop a self-test that is
// restoring settings, so write errors are ignored.
func say(w io.Writer, a ...any)            { _, _ = fmt.Fprintln(w, a...) }
func sayf(w io.Writer, f string, a ...any) { _, _ = fmt.Fprintf(w, f, a...) }

func outcomeOf(ok bool) Outcome {
	if ok {
		return Pass
	}
	return Fail
}

func not(v bool) bool { return !v }

func errUnknown(ok bool, err error, what string) error {
	if err == nil && !ok {
		return fmt.Errorf("%s unknown", what)
	}
	return err
}

func (r *runner) record(it Item) {
	r.report.Items = append(r.report.Items, it)
	if it.Outcome != Skip {
		r.report.Total++
		if it.Outcome == Pass {
			r.report.Passed++
		}
	}
}

// check runs one item. A panic in it is a FAIL of that item, never the end of the run: the
// deferred restores inside the item have already run by then.
func (r *runner) check(name string, fn func() (string, error)) {
	detail, err := func() (detail string, err error) {
		defer func() {
			if p := recover(); p != nil {
				err = fmt.Errorf("panic: %v", p)
			}
		}()
		return fn()
	}()
	if err != nil {
		sayf(r.d.Out, "FAIL  %s: %v\n", name, err)
		r.record(Item{Name: name, Outcome: Fail, Detail: err.Error()})
		return
	}
	if detail != "" {
		sayf(r.d.Out, "PASS  %s  (%s)\n", name, detail)
	} else {
		sayf(r.d.Out, "PASS  %s\n", name)
	}
	r.record(Item{Name: name, Outcome: Pass, Detail: detail})
}

func (r *runner) skip(name, why string) {
	sayf(r.d.Out, "SKIP  %s: %s\n", name, why)
	r.record(Item{Name: name, Outcome: Skip, Detail: why})
}

// detached is a context that survives Ctrl-C: restoring a setting must not be cancelled.
func (r *runner) detached() context.Context { return context.WithoutCancel(r.ctx) }

// pauseAutomation stops the daemon's smart fan and automatic profile, and returns the function
// that puts them back. The restore is armed BEFORE the first change, so a failure half way
// through pausing still restores what was changed.
func (r *runner) pauseAutomation() (restore func()) {
	d := r.d.Daemon
	if d == nil {
		return func() {}
	}
	smart, auto, err := d.Automation(r.ctx)
	if err != nil {
		return func() {} // no daemon: nothing to pause
	}
	restore = func() {
		if err := d.SetAutomation(r.detached(), smart, auto); err != nil {
			sayf(r.d.Out, "FAIL  restoring the daemon's smart fan / auto profile: %v\n", err)
			r.record(Item{Name: "restore the daemon's automation", Outcome: Fail, Detail: err.Error()})
		}
	}
	if err := d.SetAutomation(r.ctx, false, false); err != nil {
		restore()
		return func() {}
	}
	r.d.Sleep(pauseSettle)
	return restore
}

// modes cycles every supported power mode and puts the original back.
func (r *runner) modes() (detail string, err error) {
	h := r.d.HW
	orig, err := h.GetMode()
	if err != nil {
		return "", err
	}
	defer func() {
		if _, rerr := h.SetMode(orig); rerr != nil {
			err = errors.Join(err, fmt.Errorf("RESTORE FAILED: power mode %s: %w", orig, rerr))
		}
	}()
	supported, err := h.SupportedModes()
	if err != nil {
		return "", err
	}
	var seen []string
	for _, m := range supported {
		got, err := h.SetMode(m)
		if err != nil {
			return "", err
		}
		seen = append(seen, got)
	}
	return "cycled " + strings.Join(seen, ",") + ", restored " + orig, nil
}

func (r *runner) sensors() (string, error) {
	s, err := r.d.HW.ReadSensors()
	if err != nil {
		return "", err
	}
	if s.CPUTemp <= 10 || s.CPUTemp >= 110 || s.GPUTemp <= 10 || s.GPUTemp >= 110 {
		return "", fmt.Errorf("temperatures out of range: CPU %d C, GPU %d C", s.CPUTemp, s.GPUTemp)
	}
	return fmt.Sprintf("CPU %d C, GPU %d C, fans %d/%d rpm", s.CPUTemp, s.GPUTemp, s.CPUFanRPM, s.GPUFanRPM), nil
}

// fans writes ONE manual duty (no refresh) and watches whether the firmware keeps both fans at
// it. The fans go back to firmware Auto from a deferred call whatever happens.
func (r *runner) fans() (detail string, err error) {
	h := r.d.HW
	defer func() {
		if aerr := h.FanAuto(); aerr != nil {
			err = errors.Join(err, fmt.Errorf("FANS NOT BACK ON AUTO: %w", aerr))
			return
		}
		st, serr := h.GetFanState()
		switch {
		case serr != nil:
			err = errors.Join(err, fmt.Errorf("fans not confirmed back on firmware auto: %w", serr))
		case st.Mode != "auto":
			err = errors.Join(err, fmt.Errorf("fans not back on firmware auto (state %q)", st.Mode))
		}
	}()
	if err := h.FanManual(fanHoldDuty, fanHoldDuty); err != nil {
		return "", err
	}
	var held, samples int
	start := r.d.Now()
	for r.d.Now().Sub(start) < fanWatch {
		if err := r.ctx.Err(); err != nil {
			return "", err
		}
		r.d.Sleep(time.Second)
		if r.d.Now().Sub(start) > fanSpinUp {
			s, err := h.ReadSensors()
			if err != nil {
				return "", err
			}
			samples++
			if s.CPUFanRPM > fanMinRPM && s.GPUFanRPM > fanMinRPM {
				held++
			}
		}
	}
	if samples == 0 {
		return "", errors.New("no fan speed sample was taken")
	}
	pct := 100 * held / samples
	if pct < fanMinPercent {
		return "", fmt.Errorf("fans held %d %% in only %d %% of samples", fanHoldDuty, pct)
	}
	return fmt.Sprintf("one write held both fans >%d rpm in %d %% of samples for %d s", fanMinRPM, pct, int(fanWatch/time.Second)), nil
}

// toggle changes a setting, checks that it reads back, and restores the original from a
// deferred call, so the original is back even if the change or the read-back failed.
func toggle[T comparable](get func() (T, error), set func(T) (T, error), alt func(T) T) (detail string, err error) {
	orig, err := get()
	if err != nil {
		return "", err
	}
	defer func() {
		if _, serr := set(orig); serr != nil {
			err = errors.Join(err, fmt.Errorf("RESTORE FAILED: %w", serr))
			return
		}
		back, gerr := get()
		switch {
		case gerr != nil:
			err = errors.Join(err, fmt.Errorf("RESTORE FAILED: cannot read it back: %w", gerr))
		case back != orig:
			err = errors.Join(err, fmt.Errorf("RESTORE FAILED: reads %v, wanted %v", back, orig))
		}
	}()
	next := alt(orig)
	if _, err := set(next); err != nil {
		return "", err
	}
	got, err := get()
	if err != nil {
		return "", err
	}
	if got != next {
		return "", errors.New("readback mismatch")
	}
	return fmt.Sprintf("%v -> %v -> restored", orig, next), nil
}

func (r *runner) keyboard() (string, error) {
	if r.d.Keyboard == nil {
		return "", errors.New("no keyboard interface")
	}
	b, err := r.d.Keyboard.Brightness()
	if err != nil {
		return "", err
	}
	if b < 0 || b > 50 {
		return "", fmt.Errorf("brightness %d is outside 0..50", b)
	}
	return fmt.Sprintf("brightness %d/50 read back from the keyboard", b), nil
}

func (r *runner) audioOutput() (string, error) {
	active, err := r.d.Audio.OutputActive(r.ctx)
	if err != nil {
		return "", err
	}
	if active != "speakers" && active != "headphones" {
		return "", fmt.Errorf("unexpected output profile %q", active)
	}
	return "active=" + active, nil
}

func (r *runner) headsetBoost() (detail string, err error) {
	a := r.d.Audio
	cur, found, err := a.HeadsetBoost(r.ctx)
	if err != nil {
		return "", err
	}
	if !found {
		return "no headset mic boost control on this card (skipped)", nil
	}
	defer func() {
		if rerr := a.SetHeadsetBoost(r.detached(), cur); rerr != nil {
			err = errors.Join(err, fmt.Errorf("RESTORE FAILED: headset mic boost %d: %w", cur, rerr))
		}
	}()
	for _, level := range []int{0, 3, 1} {
		if err := a.SetHeadsetBoost(r.ctx, level); err != nil {
			return "", fmt.Errorf("boost %d did not read back: %w", level, err)
		}
	}
	return fmt.Sprintf("0, 3 and 1 read back, restored to %d", cur), nil
}

func (r *runner) micMute() (detail string, err error) {
	a := r.d.Audio
	muted, err := a.MicMuted(r.ctx)
	if err != nil {
		return "", err
	}
	defer func() {
		if rerr := a.SetMicMute(r.detached(), muted); rerr != nil {
			err = errors.Join(err, fmt.Errorf("RESTORE FAILED: microphone mute %v: %w", muted, rerr))
		}
	}()
	if err := a.SetMicMute(r.ctx, !muted); err != nil {
		return "", err
	}
	now, err := a.MicMuted(r.ctx)
	if err != nil {
		return "", err
	}
	if now != !muted {
		return "", errors.New("mute did not read back")
	}
	return fmt.Sprintf("mute toggled and restored (%v)", muted), nil
}

func (r *runner) cpuLimits() (string, error) {
	if r.d.Daemon == nil {
		return "", errors.New("the TrueSense daemon is not running")
	}
	info, err := r.d.Daemon.CPUInfo(r.ctx)
	if err != nil {
		if strings.Contains(err.Error(), "unknown command") {
			return "the running service is older than this app (skipped: re-run the installer)", nil
		}
		return "", err
	}
	who := "automatic"
	if info.User {
		who = "yours"
	}
	return fmt.Sprintf("PL1 %d W / PL2 %d W readable (%s)", info.PL1W, info.PL2W, who), nil
}
