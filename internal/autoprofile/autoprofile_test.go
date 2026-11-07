package autoprofile

import (
	"math"
	"strings"
	"testing"

	"github.com/Order52/truesense/internal/sensors"
)

// truesenseProfile is the shipped "truesense" Smart profile.
func truesenseProfile() Params {
	return Params{ACIdle: "balanced", ACBrowsing: "balanced", ACBusy: "performance", ACTurbo: true, TurboAfterS: 30, TurboMaxCPU: 85, TurboMaxGPU: 75, BackoffHoldS: 90, IdleAfterS: 60,
		BatteryIdle: "eco", BatteryBrowsing: "eco", BatteryBusy: "balanced", BatteryHot: "quiet", BatteryLowPct: 20, UpDwellS: 5, DownDwellS: 60, BusyHoldS: 60, MinChangeS: 45, TurboMarginC: 6, TurboRetryS: 180}
}

func steadyProfile() Params {
	p := truesenseProfile()
	p.ACTurbo, p.BusyHoldS, p.DownDwellS, p.MinChangeS, p.UpDwellS = false, 180, 180, 180, 8
	return p
}

func batteryFirstProfile() Params {
	p := truesenseProfile()
	p.ACIdle, p.ACBrowsing, p.ACTurbo, p.BatteryBusy, p.BatteryLowPct, p.DownDwellS = "quiet", "quiet", false, "quiet", 30, 20
	return p
}

func performanceFirstProfile() Params {
	p := truesenseProfile()
	p.ACBrowsing, p.TurboAfterS, p.TurboMaxCPU, p.TurboMaxGPU, p.BackoffHoldS = "performance", 10, 90, 80, 45
	p.BatteryIdle, p.BatteryBrowsing, p.BatteryBusy, p.BatteryLowPct, p.UpDwellS = "quiet", "balanced", "performance", 15, 3
	return p
}

type change struct {
	t    float64
	mode string
}

// laptop drives the engine the way the daemon's loop does: smoothed temperatures, the
// classification, one decision per tick, the decision carried out and recorded.
type laptop struct {
	e       *Engine
	cls     Classifier
	tcpu    sensors.EMA
	tgpu    sensors.EMA
	mode    string
	now     float64
	wall    float64
	ac      bool
	battery float64
	kind    string
	game    string
	log     []Decision
}

func newLaptop(p Params) *laptop {
	return &laptop{e: New(p), cls: Classifier{HotCPU: 92, HotGPU: 82}, tcpu: sensors.EMA{Alpha: 0.4}, tgpu: sensors.EMA{Alpha: 0.4}, mode: "eco", wall: 1_000_000, ac: true, battery: 80, kind: "smart"}
}

// tick advances dt seconds with the given raw readings and returns the mode change, if any.
func (l *laptop) tick(dt, cpuT, gpuT, load, gpuUtil, gpuPower float64) (string, bool) {
	l.now += dt
	l.wall += dt
	r := Readings{TCPU: l.tcpu.Update(cpuT), TGPU: l.tgpu.Update(gpuT), CPULoad: load, GPUUtil: gpuUtil, GPUPower: gpuPower}
	act := l.cls.Classify(r)
	d := l.e.Step(Inputs{Kind: l.kind, Now: l.now, Wall: l.wall, Activity: act, Readings: r, AC: l.ac, BatteryPct: l.battery, CurMode: l.mode, NativeAC: "balanced", NativeBat: "eco", GameMode: l.game})
	l.log = append(l.log, d)
	if d.Mode != "" {
		l.mode = d.Mode
		l.e.Applied(l.now)
		return d.Mode, true
	}
	return "", false
}

// play runs seconds of game time (the Python tests' defaults: GPU 65 C, 16 % CPU load, GPU at 98 % and 71 W).
func (l *laptop) play(seconds float64, cpuOf func(t float64) float64, opts ...func(*playOpts)) []change {
	o := playOpts{gpu: 65, load: 16, gpuUtil: 98, gpuPower: 71, dt: 1}
	for _, f := range opts {
		f(&o)
	}
	var out []change
	for t := 0.0; t < seconds; t += o.dt {
		if m, ok := l.tick(o.dt, math.Round(cpuOf(t)), o.gpu, o.load, o.gpuUtil, o.gpuPower); ok {
			out = append(out, change{l.now, m})
		}
	}
	return out
}

type playOpts struct{ gpu, load, gpuUtil, gpuPower, dt float64 }

func quiet(o *playOpts) { o.load, o.gpuUtil, o.gpuPower = 20, 15, 18 }
func calm(o *playOpts)  { o.load, o.gpuUtil, o.gpuPower = 3, 2, 5 }
func every(dt float64) func(*playOpts) {
	return func(o *playOpts) { o.dt = dt }
}
func constant(v float64) func(float64) float64 { return func(float64) float64 { return v } }

// settled returns a laptop idle on power at 40 C for 70 s: in Balanced, the starting point of the scenarios.
func settled(t *testing.T, p Params) *laptop { return settledTo(t, p, "balanced") }

// settledTo is settled for a profile whose idle mode is another one.
func settledTo(t *testing.T, p Params, want string) *laptop {
	t.Helper()
	l := newLaptop(p)
	for i := 0; i < 70; i++ {
		l.tick(1, 40, 40, 5, 5, 5)
	}
	if l.mode != want {
		t.Fatalf("idle on power must settle on %s, got %s", want, l.mode)
	}
	return l
}

func modesOf(c []change) []string {
	var m []string
	for _, x := range c {
		m = append(m, x.mode)
	}
	return m
}

func TestClassifier(t *testing.T) {
	c := Classifier{HotCPU: 92, HotGPU: 82}
	tests := []struct {
		name string
		r    Readings
		want Activity
	}{
		{"idle", Readings{TCPU: 40, TGPU: 40, CPULoad: 2, GPUUtil: 1}, Idle},
		{"browsing", Readings{TCPU: 50, TGPU: 45, CPULoad: 20, GPUUtil: 15}, Browsing},
		{"heavy by cpu", Readings{TCPU: 70, TGPU: 50, CPULoad: 80}, Heavy},
		{"gaming by gpu util", Readings{TCPU: 60, TGPU: 60, CPULoad: 10, GPUUtil: 70}, Gaming},
		{"gaming by gpu power", Readings{TCPU: 60, TGPU: 60, GPUPower: 40}, Gaming},
		{"hot", Readings{TCPU: 93, TGPU: 60, CPULoad: 99}, Hot},
	}
	for _, tc := range tests {
		c := c
		if got := c.Classify(tc.r); got != tc.want {
			t.Errorf("%s: %s, want %s", tc.name, got, tc.want)
		}
	}
}

func TestOnceHotItStaysHotUntilFiveDegreesBelow(t *testing.T) {
	c := Classifier{HotCPU: 92, HotGPU: 82}
	if c.Classify(Readings{TCPU: 92, CPULoad: 99}) != Hot {
		t.Fatal("at the line it is hot")
	}
	if c.Classify(Readings{TCPU: 89, CPULoad: 99}) != Hot {
		t.Fatal("below the line but within 5 degrees it stays hot (no flapping on the threshold)")
	}
	if c.Classify(Readings{TCPU: 86, CPULoad: 99}) == Hot {
		t.Fatal("more than 5 degrees below the line it cools down")
	}
}

func TestNativeFollowsThePowerSourceOnly(t *testing.T) {
	l := newLaptop(truesenseProfile())
	l.kind = "native"
	l.tick(2, 40, 40, 95, 5, 5) // under load, on power: balanced, no heat or load logic
	if l.mode != "balanced" {
		t.Fatalf("on AC: %s", l.mode)
	}
	l.ac = false
	l.tick(1, 40, 40, 95, 5, 5)
	if l.mode != "eco" {
		t.Fatalf("on battery: %s", l.mode)
	}
}

func TestSmartEscalatesAndBacksOffForHeat(t *testing.T) {
	l := settled(t, truesenseProfile())
	l.play(10, constant(70), func(o *playOpts) { o.load, o.gpuUtil, o.gpuPower = 90, 5, 5 })
	if l.mode != "performance" {
		t.Fatalf("heavy load: performance first, got %s", l.mode)
	}
	l.play(70, constant(70), func(o *playOpts) { o.load, o.gpuUtil, o.gpuPower = 90, 5, 5 })
	if l.mode != "turbo" {
		t.Fatalf("sustained and cool enough: turbo, got %s", l.mode)
	}
	l.play(10, constant(93), func(o *playOpts) { o.load, o.gpu, o.gpuUtil, o.gpuPower = 95, 70, 5, 5 })
	if l.mode != "performance" {
		t.Fatalf("hot: back off from turbo at once, got %s", l.mode)
	}
	l.play(40, constant(70), func(o *playOpts) { o.load, o.gpuUtil, o.gpuPower = 90, 5, 5 })
	if l.mode != "performance" {
		t.Fatalf("and stay there during the back-off hold, got %s", l.mode)
	}
}

func TestSmartOnBatterySavesPower(t *testing.T) {
	l := newLaptop(truesenseProfile())
	l.ac = false
	for i := 0; i < 40; i++ {
		l.tick(1, 40, 40, 4, 4, 4)
	}
	if l.mode != "eco" {
		t.Fatalf("idle on battery: eco, got %s", l.mode)
	}
	for i := 0; i < 40; i++ {
		l.tick(1, 70, 60, 90, 5, 5)
	}
	if l.mode != "balanced" {
		t.Fatalf("busy on battery: balanced, never turbo, got %s", l.mode)
	}
	l.battery = 10
	l.tick(1, 40, 40, 4, 4, 4)
	if l.mode != "eco" {
		t.Fatalf("a nearly empty battery forces eco at once, got %s", l.mode)
	}
}

func TestACPUOnTheTurboLineDoesNotMakeTheModeFlap(t *testing.T) {
	l := settled(t, truesenseProfile())
	// the real session: GPU at 98 %, CPU between 80 and 89 C for minutes
	changes := l.play(900, func(t float64) float64 { return 84.5 + 4.5*math.Sin(t/9) })
	if len(changes) > 2 {
		t.Fatalf("mode changes during 15 minutes at the Turbo line: %v", changes)
	}
	if l.mode != "performance" {
		t.Fatalf("it settles on performance, got %s", l.mode)
	}
}

func TestTurboIsEnteredWellBelowItsLimitAndLeftOnlyAtItWithALongRetryPause(t *testing.T) {
	l := settled(t, truesenseProfile())
	l.play(200, constant(70))
	if l.mode != "turbo" {
		t.Fatalf("cool: up to turbo, got %s", l.mode)
	}
	l.play(20, constant(86))
	if l.mode != "performance" {
		t.Fatalf("at the limit it steps down at once, got %s", l.mode)
	}
	tDown := l.now
	l.play(120, constant(70))
	if l.mode != "performance" {
		t.Fatalf("and does not climb back for 180 s even though it is cool again, got %s", l.mode)
	}
	l.play(120, constant(70))
	if l.mode != "turbo" || l.now-tDown < 180 {
		t.Fatalf("after the pause it may try again (mode %s after %.0f s)", l.mode, l.now-tDown)
	}
	l.play(60, constant(82))
	if l.mode != "turbo" {
		t.Fatalf("82 C is under the limit: it stays, got %s", l.mode)
	}
}

func TestAQuietMomentInAGameKeepsTheMode(t *testing.T) {
	l := settled(t, truesenseProfile())
	l.play(40, constant(70))
	if l.mode != "performance" {
		t.Fatalf("40 s of game: performance, got %s", l.mode)
	}
	if c := l.play(45, constant(70), quiet); len(c) != 0 || l.mode != "performance" {
		t.Fatalf("a 45 s menu does not drop the mode: %v %s", c, l.mode)
	}
	if last := l.log[len(l.log)-1]; !strings.Contains(last.Reason, "holding the busy mode") {
		t.Fatalf("the reason must say so: %q", last.Reason)
	}
	l.play(400, constant(50), calm)
	if l.mode != "balanced" && l.mode != "quiet" {
		t.Fatalf("really quiet for minutes: it does come back down, got %s", l.mode)
	}
}

func TestAQuietMomentNeverStartsTurbo(t *testing.T) {
	l := settled(t, truesenseProfile())
	l.play(20, constant(70))
	l.play(120, constant(70), quiet)
	if l.mode == "turbo" {
		t.Fatal("a held quiet moment keeps the current mode, it never raises it")
	}
}

func TestSteadyNeverEntersTurboAndChangesRarely(t *testing.T) {
	l := settled(t, steadyProfile())
	changes := l.play(900, func(t float64) float64 { return 70 + 8*math.Sin(t/15) })
	for _, m := range modesOf(changes) {
		if m == "turbo" {
			t.Fatal("no Turbo with the steady profile")
		}
	}
	if len(changes) > 1 {
		t.Fatalf("changes: %v", changes)
	}
}

func TestTimersAreRealSecondsWhateverTheTickLength(t *testing.T) {
	for _, dt := range []float64{1, 2} { // the service ticks every 1 s or every 2 s
		l := settled(t, truesenseProfile())
		start := l.now
		changes := l.play(140, constant(70), every(dt))
		var turboAt float64 = -1
		for _, c := range changes {
			if c.mode == "turbo" {
				turboAt = c.t
				break
			}
		}
		if turboAt < 0 || turboAt-start < 30 {
			t.Fatalf("dt=%v: Turbo not before 30 s of heavy load: %v (game started at %v)", dt, changes, start)
		}
	}
}

func TestProfilesChangeHowSmartBehaves(t *testing.T) {
	l := settledTo(t, batteryFirstProfile(), "quiet")
	l.play(60, constant(70), func(o *playOpts) { o.load, o.gpuUtil, o.gpuPower = 90, 5, 5 })
	if l.mode == "turbo" {
		t.Fatal("battery-first never uses Turbo")
	}
	l2 := settled(t, performanceFirstProfile())
	l2.play(60, constant(70))
	if l2.mode != "turbo" {
		t.Fatalf("performance-first: Turbo after 10 s, got %s", l2.mode)
	}
}

func TestHoldKeepsYourPickAndEndsWhenTheSituationChanges(t *testing.T) {
	l := settled(t, truesenseProfile())
	l.mode = "eco"
	l.e.SetHold(l.wall, 30, l.ac, l.cls.Classify(Readings{TCPU: 40, CPULoad: 5}))
	l.play(30, constant(40), calm)
	if l.mode != "eco" {
		t.Fatalf("same situation: your pick stays, got %s", l.mode)
	}
	l.play(8, constant(70)) // a game starts: smart auto takes over without anyone pressing resume
	if l.mode == "eco" {
		t.Fatalf("a game starts: the hold must end by itself, got %s", l.mode)
	}
	l.e.SetHold(l.wall, 30, l.ac, Gaming)
	l.mode = "eco"
	l.ac = false // unplugging ends the hold too
	l.play(8, constant(70))
	if l.mode == "eco" && l.e.HoldUntil() != 0 {
		t.Fatal("unplugging must end the hold")
	}
}

func TestHoldWinsWhileTheSituationIsTheSame(t *testing.T) {
	l := settled(t, truesenseProfile())
	l.e.SetHold(l.wall, 30, l.ac, Idle)
	l.mode = "turbo"
	if m, changed := l.tick(5, 40, 40, 4, 4, 4); changed {
		t.Fatalf("a hold must be respected: %s", m)
	}
	l.e.SetHold(l.wall, 0, l.ac, Idle)
	if _, changed := l.tick(61, 40, 40, 4, 4, 4); !changed {
		t.Fatal("after the hold is released the automatic mode takes over")
	}
}

func TestChoosingAutoTakesOverImmediately(t *testing.T) {
	l := newLaptop(truesenseProfile())
	l.kind = "off"
	l.tick(5, 40, 40, 5, 5, 5)
	l.mode = "turbo"
	l.e.SetHold(l.wall, 30, l.ac, Idle)
	l.kind = "native"
	l.e.TakeOverNow()
	l.tick(1, 40, 40, 5, 5, 5)
	if l.mode != "balanced" {
		t.Fatalf("native (AC: balanced) must not wait for the old hold, got %s", l.mode)
	}
	if l.e.HoldUntil() != 0 {
		t.Fatal("the hold must be cleared")
	}
}

func TestGameRulesCanOnlyRaiseTheModeAndBackOffForHeatAndBattery(t *testing.T) {
	l := newLaptop(truesenseProfile())
	l.game = "turbo"
	l.tick(1, 40, 40, 5, 5, 5)
	if l.mode != "turbo" {
		t.Fatalf("a running game rule raises the mode, got %s", l.mode)
	}
	l.game = "eco"
	l.mode = "performance"
	l.e.TakeOverNow()
	l.tick(1, 40, 40, 5, 5, 5)
	if l.mode == "eco" {
		t.Fatal("a rule can never lower the mode below what auto wants")
	}
	l2 := newLaptop(truesenseProfile())
	l2.game, l2.ac, l2.battery = "turbo", false, 10
	l2.tick(1, 40, 40, 5, 5, 5)
	if l2.mode != "eco" {
		t.Fatalf("a nearly empty battery wins over a game rule, got %s", l2.mode)
	}
}

func TestOffDoesNothing(t *testing.T) {
	l := newLaptop(truesenseProfile())
	l.kind = "off"
	if _, changed := l.tick(1, 99, 99, 99, 99, 99); changed {
		t.Fatal("off: hands off")
	}
}

func TestEveryDecisionCarriesItsReason(t *testing.T) {
	l := settled(t, truesenseProfile())
	l.play(30, constant(70))
	var reasons []string
	for _, d := range l.log {
		if d.Mode == "performance" {
			reasons = append(reasons, d.Reason)
		}
	}
	if len(reasons) == 0 || !strings.Contains(reasons[0], "gaming, on power") {
		t.Fatalf("reasons: %v", reasons)
	}
}
