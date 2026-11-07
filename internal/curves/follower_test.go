package curves

import (
	"reflect"
	"testing"
	"time"
)

const second = time.Second

func TestFollowerRisesAtOnceAndFallsSlowly(t *testing.T) {
	c, _ := Builtin("truesense")
	f := NewFollower(c)
	// 80 C looks up to 70 % and is taken immediately.
	if d := f.Step(SideCPU, 80, false, second, 0); d != 70 {
		t.Fatalf("rise: %v, want 70", d)
	}
	// The temperature drops to 74 C: hysteresis 4 puts the lookup at 70 C (= 52 %), lower than
	// 70 %, so the duty may fall, but only down_rate (1.5 %/s) per second.
	if d := f.Step(SideCPU, 74, false, second, 0); d != 68.5 {
		t.Fatalf("slow fall: %v, want 68.5", d)
	}
	if d := f.Step(SideCPU, 74, false, 4*second, 0); d != 62.5 {
		t.Fatalf("fall over 4 s: %v, want 62.5", d)
	}
	// It keeps falling until it reaches what the curve asks for at 74 C (58 %). The duty may
	// dip at most one step below it before the next step takes it back up: the hysteresis
	// lookup only bounds a single step's fall (ported as the Python does it).
	for i := 0; i < 100; i++ {
		f.Step(SideCPU, 74, false, second, 0)
	}
	if d, _ := f.Duty(); d < 58-c.DownRate || d > 58 {
		t.Fatalf("settled at %v, want within one step of 58", d)
	}
}

func TestFollowerFallIsBoundedByHysteresisLookup(t *testing.T) {
	// A huge down_rate shows the floor of a single step: the hysteresis-adjusted lookup.
	c := Curve{CPU: []Point{{40, 20}, {60, 100}}, GPU: []Point{{40, 20}, {60, 100}}, Hysteresis: 5, DownRate: 20, Couple: 15}
	f := NewFollower(c)
	f.Step(SideCPU, 60, false, second, 0) // 100 %
	// At 50 C the target is 60 %, the lookup 5 C lower is 40 %: the step falls to the 40 %
	// floor, not below it.
	if d := f.Step(SideCPU, 50, false, second, 0); d != 80 {
		// 100 - 20*1 = 80 is the rate limit; the floor of 40 is not reached in one step.
		t.Fatalf("one step fell to %v, want 80", d)
	}
	f.SetDuty(100, 0)
	if d := f.Step(SideCPU, 50, false, 10*second, 0); d != 40 {
		t.Fatalf("a big step stops at the hysteresis lookup (40), got %v", d)
	}
	g := NewFollower(Curve{CPU: []Point{{40, 20}, {60, 100}}, GPU: []Point{{40, 20}, {60, 100}}, Hysteresis: 0, DownRate: 5})
	g.SetDuty(60, 0)
	if d := g.Step(SideCPU, 50, false, second, 0); d != 60 {
		t.Fatalf("duty equal to the target must stay, got %v", d)
	}
}

func TestFollowerPredictAndBatteryShift(t *testing.T) {
	c := Curve{CPU: []Point{{40, 20}, {60, 100}}, GPU: []Point{{40, 20}, {60, 100}}, Hysteresis: 4, DownRate: 1, Predict: 5, Couple: 15, BatteryShift: 3}
	f := NewFollower(c)
	if got := f.Target(SideCPU, 50, false, 0); got != 60 {
		t.Errorf("steady target %v, want 60", got)
	}
	if got := f.Target(SideCPU, 50, true, 0); got != 80 {
		t.Errorf("rising looks 5 C ahead: %v, want 80", got)
	}
	if got := f.Target(SideCPU, 50, true, f.BatteryShift(false)); got != 68 {
		t.Errorf("on battery the lookup is 3 C lower: %v, want 68", got)
	}
	if got := f.BatteryShift(true); got != 0 {
		t.Errorf("no shift on AC: %v", got)
	}
}

func TestFollowerSidesAreIndependent(t *testing.T) {
	c, _ := Builtin("truesense")
	f := NewFollower(c)
	f.Step(SideCPU, 89, false, second, 0)
	cpu, gpu := f.Duty()
	if cpu != 100 || gpu != 0 {
		t.Errorf("duties = %v, %v; want 100, 0", cpu, gpu)
	}
}

func TestFollowerSetCurveResets(t *testing.T) {
	c, _ := Builtin("truesense")
	f := NewFollower(c)
	f.Step(SideCPU, 89, false, second, 0)
	q, _ := Builtin("quiet")
	f.SetCurve(q)
	if cpu, gpu := f.Duty(); cpu != 0 || gpu != 0 {
		t.Errorf("duties after SetCurve: %v, %v", cpu, gpu)
	}
	if !reflect.DeepEqual(f.Curve(), q) {
		t.Error("Curve() is not the new curve")
	}
}

func TestMaxCoolingGivesFullSpeedWhenWarm(t *testing.T) {
	// Python: select max-cooling, drive 65 C / 50 C; the CPU fan must be at 90 % or more.
	c, _ := Builtin("max-cooling")
	f := NewFollower(c)
	f.Step(SideCPU, 65, false, second, 0)
	f.Step(SideGPU, 50, false, second, 0)
	cpu, gpu := f.Output(0)
	if cpu < 90 {
		t.Errorf("cpu duty %d, want >= 90", cpu)
	}
	if gpu < 20 {
		t.Errorf("gpu duty %d is below the fan minimum", gpu)
	}
}

func TestOutput(t *testing.T) {
	tests := []struct {
		name     string
		cpu, gpu float64
		couple   float64
		floor    int
		wantC    int
		wantG    int
	}{
		{"both below 20 is firmware auto", 19.9, 0, 15, 0, 0, 0},
		{"zero", 0, 0, 15, 0, 0, 0},
		{"quantised to steps of 5", 41, 41, 15, 0, 40, 40},
		{"rounds up", 43, 43, 15, 0, 45, 45},
		{"halves round to even like Python round()", 22.5, 22.5, 15, 0, 20, 20},
		{"halves round to even (up case)", 27.5, 27.5, 15, 0, 30, 30},
		{"quiet fan is lifted to couple below the loud one", 90, 0, 15, 0, 90, 75},
		{"and the other way", 0, 60, 15, 0, 45, 60},
		{"never below the 20 % minimum", 20, 0, 15, 0, 20, 20},
		{"floor lifts both", 0, 0, 15, 35, 35, 35},
		{"floor does not lower", 80, 80, 15, 35, 80, 80},
		{"floor with a loud side", 80, 0, 15, 35, 80, 65},
		{"couple 0 forces equal duties", 90, 40, 0, 0, 90, 90},
		{"couple 50 leaves them apart", 100, 20, 50, 0, 100, 50},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			f := NewFollower(Curve{Couple: tt.couple})
			f.SetDuty(tt.cpu, tt.gpu)
			c, g := f.Output(tt.floor)
			if c != tt.wantC || g != tt.wantG {
				t.Errorf("Output = %d, %d; want %d, %d", c, g, tt.wantC, tt.wantG)
			}
			for _, d := range []int{c, g} {
				if d != 0 && (d < 20 || d > 100) {
					t.Errorf("duty %d violates the fan rule (0 or 20..100)", d)
				}
			}
		})
	}
}

// TestFollowerIsDeterministic drives two followers with the same sequence of inputs: the
// Follower has no clock and no hidden state, so they must agree at every step, and a
// third run after a reset must reproduce the first.
func TestFollowerIsDeterministic(t *testing.T) {
	c, _ := Builtin("truesense")
	type in struct {
		cpu, gpu float64
		rising   bool
		dt       time.Duration
		ac       bool
	}
	var seq []in
	for i := 0; i < 400; i++ {
		temp := 50 + 40*float64(i%100)/100
		if i%100 >= 50 {
			temp = 90 - 40*float64(i%100-50)/50
		}
		seq = append(seq, in{temp, temp - 6, i%100 < 50, time.Duration(500+i%7*100) * time.Millisecond, i%3 != 0})
	}
	type out struct{ c, g int }
	run := func(f *Follower) []out {
		var res []out
		for _, s := range seq {
			shift := f.BatteryShift(s.ac)
			f.Step(SideCPU, s.cpu, s.rising, s.dt, shift)
			f.Step(SideGPU, s.gpu, s.rising, s.dt, shift)
			c, g := f.Output(0)
			res = append(res, out{c, g})
		}
		return res
	}
	a, b := NewFollower(c), NewFollower(c)
	ra, rb := run(a), run(b)
	if !reflect.DeepEqual(ra, rb) {
		t.Fatal("two followers given the same inputs disagree")
	}
	a.SetCurve(c)
	if !reflect.DeepEqual(run(a), ra) {
		t.Fatal("a reset follower does not reproduce the first run")
	}
	// Sanity: the run actually exercised both rising and falling.
	var peak, low = 0, 100
	for _, o := range ra {
		peak, low = max(peak, o.c), min(low, o.c)
	}
	if peak < 80 || low != 0 {
		t.Errorf("the scenario did not cover the range: peak %d, low %d", peak, low)
	}
}
