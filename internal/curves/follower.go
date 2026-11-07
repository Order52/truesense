package curves

import (
	"math"
	"time"
)

// Follower turns temperatures into fan duties: it rises at once, falls slowly and only after
// the temperature has dropped by the hysteresis. It is a pure state machine: every input
// (temperatures, whether they are rising, the time since the last step, the battery shift)
// is passed in, it reads no clock and does no I/O, so the same inputs always give the same
// outputs. It is not safe for concurrent use; the daemon's control loop owns it.
type Follower struct {
	curve Curve
	duty  [2]float64
}

// NewFollower returns a Follower on curve with both duties at 0 (firmware Auto).
func NewFollower(c Curve) *Follower { return &Follower{curve: c} }

// SetCurve switches curve and resets both duties, so the new curve takes effect cleanly.
func (f *Follower) SetCurve(c Curve) { f.curve, f.duty = c, [2]float64{} }

// Curve returns the curve in use.
func (f *Follower) Curve() Curve { return f.curve }

// Duty returns the current CPU and GPU duties in percent, before quantising.
func (f *Follower) Duty() (cpu, gpu float64) { return f.duty[SideCPU], f.duty[SideGPU] }

// SetDuty forces both duties. The daemon uses it when it overrides the curve: 0,0 when the
// laptop is calm (hand the fans back to the firmware) and 100,100 when it is hot.
func (f *Follower) SetDuty(cpu, gpu float64) { f.duty = [2]float64{cpu, gpu} }

// BatteryShift is the temperature shift to pass to Step: nothing on AC power, the curve's
// battery_shift on battery (it makes the fans come in later and the laptop quieter).
func (f *Follower) BatteryShift(onAC bool) float64 {
	if onAC {
		return 0
	}
	return f.curve.BatteryShift
}

// Target is the duty the curve asks for right now. A rising temperature is looked up
// `predict` degrees ahead so the fans start before the heat arrives.
func (f *Follower) Target(side Side, temp float64, rising bool, shift float64) float64 {
	t := temp - shift
	if rising {
		t += f.curve.Predict
	}
	return Interp(f.curve.Points(side), t)
}

// Step advances one side by dt and returns its new duty. A higher target is taken at once;
// a lower one is followed only when the temperature, less the hysteresis, would also give a
// lower duty, and then no faster than down_rate percent per second.
func (f *Follower) Step(side Side, temp float64, rising bool, dt time.Duration, shift float64) float64 {
	up := f.Target(side, temp, rising, shift)
	cur := f.duty[side]
	if up >= cur {
		cur = up
	} else {
		down := Interp(f.curve.Points(side), temp-shift-f.curve.Hysteresis)
		if down < cur {
			cur = math.Max(down, cur-f.curve.DownRate*dt.Seconds())
		}
	}
	f.duty[side] = cur
	return cur
}

// quantise rounds a duty to steps of 5 %; anything below the fans' minimum of 20 % is 0.
// Halves round to even, as the Python round() the original used does.
func quantise(d float64) int {
	if d < MinDutyPct {
		return 0
	}
	return int(5 * math.RoundToEven(d/5))
}

// Output is the (cpu %, gpu %) to write, in steps of 5; (0, 0) means firmware Auto. The two
// fans share heatpipes, so the quieter one never trails the louder one by more than `couple`.
// floor raises both duties to at least that value (the activity floor); 0 disables it.
func (f *Follower) Output(floor int) (cpu, gpu int) {
	c, g := quantise(f.duty[SideCPU]), quantise(f.duty[SideGPU])
	if floor != 0 {
		c, g = max(c, floor), max(g, floor)
	}
	if c == 0 && g == 0 {
		return 0, 0
	}
	cp := f.curve.Couple
	return int(math.Max(math.Max(float64(c), float64(g)-cp), MinDutyPct)), int(math.Max(math.Max(float64(g), float64(c)-cp), MinDutyPct))
}
