package fanctl

import (
	"errors"
	"testing"
	"time"

	"github.com/Order52/truesense/internal/autoprofile"
	"github.com/Order52/truesense/internal/curves"
)

func smart(t *testing.T) *Smart {
	t.Helper()
	c, ok := curves.Builtin("truesense")
	if !ok {
		t.Fatal("the truesense curve must be built in")
	}
	return NewSmart(Params{CalmCPU: 58, CalmGPU: 55, CalmHold: 20}, c)
}

func in(a autoprofile.Activity, cpu, gpu float64) Inputs {
	return Inputs{Activity: a, TCPU: cpu, TGPU: gpu, OnAC: true, Dt: time.Second}
}

func TestIdleHeavyHotAndBack(t *testing.T) {
	s := smart(t)
	// idle and cool for a while: the firmware keeps the fans
	var c, g int
	for i := 0; i < 25; i++ {
		c, g = s.Step(in(autoprofile.Idle, 45, 45))
	}
	if c != 0 || g != 0 {
		t.Fatalf("idle: firmware Auto, got %d/%d", c, g)
	}
	// busy: the curve takes over
	for i := 0; i < 8; i++ {
		c, g = s.Step(in(autoprofile.Heavy, 70, 60))
	}
	if c < 20 || g < 20 {
		t.Fatalf("heavy load: manual duties, got %d/%d", c, g)
	}
	// hot: both fans to 100 %
	c, g = s.Step(in(autoprofile.Hot, 93, 70))
	if c != 100 || g != 100 {
		t.Fatalf("hot: 100/100, got %d/%d", c, g)
	}
	// cool again for a long time: back to the firmware
	for i := 0; i < 45; i++ {
		c, g = s.Step(in(autoprofile.Idle, 50, 48))
	}
	if c != 0 || g != 0 {
		t.Fatalf("calm again: firmware Auto, got %d/%d", c, g)
	}
}

func TestFloorWhileGamingAndWorking(t *testing.T) {
	tests := []struct {
		a      autoprofile.Activity
		gpuW   float64
		chassi float64
		want   int
	}{
		{autoprofile.Idle, 0, 0, 0},
		{autoprofile.Browsing, 0, 0, 0},
		{autoprofile.Heavy, 10, 0, 40},
		{autoprofile.Gaming, 40, 0, 50},
		{autoprofile.Gaming, 90, 0, 60},
		{autoprofile.Heavy, 0, 64, 50}, // a hot board raises the floor
		{autoprofile.Gaming, 40, 70, 60},
		{autoprofile.Idle, 0, 70, 0}, // never forces the fans on when idle
	}
	for _, tc := range tests {
		if got := Floor(tc.a, tc.gpuW, tc.chassi); got != tc.want {
			t.Errorf("Floor(%s, %.0f W, chassis %.0f) = %d, want %d", tc.a, tc.gpuW, tc.chassi, got, tc.want)
		}
	}
}

func TestTheFloorIsAppliedToTheOutput(t *testing.T) {
	s := smart(t)
	i := in(autoprofile.Gaming, 50, 50)
	i.GPUPower = 90
	c, g := s.Step(i)
	if c < 60 || g < 60 {
		t.Fatalf("a gaming floor of 60 %% must hold even at 50 C: %d/%d", c, g)
	}
}

func TestBatteryMakesTheFansComeInLater(t *testing.T) {
	ac, bat := smart(t), smart(t)
	a := in(autoprofile.Browsing, 66, 60)
	b := a
	b.OnAC = false
	var ac1, bat1 int
	for i := 0; i < 5; i++ {
		ac1, _ = ac.Step(a)
		bat1, _ = bat.Step(b)
	}
	if bat1 > ac1 {
		t.Fatalf("on battery the fans must not run harder: ac %d, battery %d", ac1, bat1)
	}
}

func TestFanStopGuards(t *testing.T) {
	if err := CanStop(50, 45, true); err != nil {
		t.Fatalf("cool enough to stop: %v", err)
	}
	for _, tc := range []struct {
		c, g  float64
		known bool
	}{{61, 45, true}, {50, 56, true}, {50, 45, false}} {
		if err := CanStop(tc.c, tc.g, tc.known); err == nil {
			t.Errorf("%+v: too warm or unknown: refuse", tc)
		}
	}
	if MustReturnFans(69, 64, true) {
		t.Fatal("below the watchdog line the stop stands")
	}
	for _, tc := range []struct {
		c, g float64
		ok   bool
	}{{70, 40, true}, {40, 65, true}, {40, 40, false}} {
		if !MustReturnFans(tc.c, tc.g, tc.ok) {
			t.Errorf("%+v: the fans must go back to the firmware", tc)
		}
	}
}

// fakeFans records what the writer sends.
type fakeFans struct {
	calls []string
	auto  bool
	fail  error
}

func (f *fakeFans) FanAuto() error {
	f.calls = append(f.calls, "auto")
	f.auto = true
	return f.fail
}
func (f *fakeFans) FanManual(c, g int) error {
	f.calls = append(f.calls, "manual")
	f.auto = false
	return f.fail
}
func (f *fakeFans) FanIsAuto() (bool, error) { return f.auto, nil }

func TestWriterOnlyWritesOnChange(t *testing.T) {
	f := &fakeFans{auto: true}
	w := &Writer{HW: f}
	w.Write(40, 40)
	w.Write(40, 40)
	w.Write(50, 50)
	w.Write(0, 0)
	w.Write(0, 0)
	if got := len(f.calls); got != 3 {
		t.Fatalf("calls = %v", f.calls)
	}
	if w.Written() != "auto" {
		t.Fatalf("written = %s", w.Written())
	}
	w.Write(35, 60)
	if w.Written() != "35,60" {
		t.Fatalf("written = %s", w.Written())
	}
}

func TestWriterNeverSendsADutyUnder20(t *testing.T) {
	f := &fakeFans{}
	w := &Writer{HW: f}
	w.Write(0, 40) // a partial result (cpu hands back, gpu runs): the firmware would refuse 0, the writer raises it to the minimum
	if w.Written() != "20,40" && w.Written() != "0,40" {
		t.Fatalf("written = %s", w.Written())
	}
}

func TestAFailedWriteIsRetried(t *testing.T) {
	f := &fakeFans{fail: errors.New("busy")}
	w := &Writer{HW: f}
	w.Write(40, 40)
	f.fail = nil
	w.Write(40, 40)
	if len(f.calls) != 2 {
		t.Fatalf("a failed write must be tried again: %v", f.calls)
	}
}

func TestVerifyNoticesFansResetBehindOurBack(t *testing.T) {
	f := &fakeFans{}
	w := &Writer{HW: f}
	w.Write(50, 50)
	f.auto = true // something handed the fans back to the firmware (resume, plug)
	w.Verify()
	w.Write(50, 50)
	if len(f.calls) != 2 {
		t.Fatalf("after a reset the duties must be asserted again: %v", f.calls)
	}
	w.Verify() // consistent now: nothing to do
	w.Write(50, 50)
	if len(f.calls) != 2 {
		t.Fatalf("no spurious rewrite: %v", f.calls)
	}
}
