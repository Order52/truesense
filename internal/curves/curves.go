// Package curves is the fan-curve data model for the smart fan: temperature (degC) to fan
// duty (%), separately for the CPU fan and the GPU fan.
//
// A curve is {"cpu": [[temp, pct], ...], "gpu": [...], "hysteresis": degC, "down_rate": %/s,
// "predict": degC, "couple": %, "battery_shift": degC}. Between points the duty is
// interpolated; 0 % means "let the firmware run the fans" (they stop when cool). The duty may
// only rise with temperature. Built-in curves live here; the user's own live in
// ~/.config/truesense/curves/NAME.json (see Store). The Follower turns temperatures into
// duties and is a pure state machine: it has no clock and reads nothing.
package curves

import (
	"fmt"
	"math"
	"strconv"
	"strings"

	"github.com/Order52/truesense/internal/firmware"
)

// Point is one curve point: [temperature in degC, duty in percent]. It marshals as a
// two-element JSON array, the format the Python implementation and its clients use.
type Point [2]float64

// Temp is the point's temperature in degC.
func (p Point) Temp() float64 { return p[0] }

// Duty is the point's fan duty in percent (0 = firmware Auto).
func (p Point) Duty() float64 { return p[1] }

// Curve is a fan curve with its follower parameters. The JSON field names are the Python
// dict keys: the daemon exposes this structure on the control socket.
type Curve struct {
	Description string  `json:"description,omitempty"`
	Native      bool    `json:"native,omitempty"` // "nothing of ours runs": the firmware controls the fans
	CPU         []Point `json:"cpu"`
	GPU         []Point `json:"gpu"`
	// Hysteresis is how far (degC) the temperature must fall below a point before the duty
	// may follow it down, so the fans do not hunt around one temperature.
	Hysteresis float64 `json:"hysteresis"`
	// DownRate is the fastest the duty may fall, in percent per second: rising is immediate,
	// falling is slow, which is what keeps the noise steady.
	DownRate float64 `json:"down_rate"`
	// Predict is added to a rising temperature before the lookup, so the fans start
	// before the heat arrives.
	Predict float64 `json:"predict"`
	// Couple is how far (percent) the quieter fan may trail the louder one: the two fans
	// share heatpipes, so one fan alone cannot cool its own chip.
	Couple float64 `json:"couple"`
	// BatteryShift is subtracted from the temperature on battery, so the fans come in later.
	BatteryShift float64 `json:"battery_shift"`
}

// Side selects the CPU fan or the GPU fan.
type Side int

// The two fans.
const (
	SideCPU Side = iota
	SideGPU
)

func (s Side) String() string {
	if s == SideGPU {
		return "gpu"
	}
	return "cpu"
}

// Points returns the curve's points for one side.
func (c Curve) Points(s Side) []Point {
	if s == SideGPU {
		return c.GPU
	}
	return c.CPU
}

// Limits of what a curve may contain. The temperature caps are where each chip starts to
// throttle: a curve that is not at 100 % by then could let the laptop overheat.
const (
	MinPoints  = 2
	MaxPoints  = 12
	MinTempC   = 20
	MaxTempC   = 110
	MinDutyPct = 20 // a fan duty is 0 (firmware Auto) or 20..100, never anything else
	MaxDutyPct = 100
	cpuFullByC = 95
	gpuFullByC = 85
)

// Defaults returns the follower parameters a curve gets when it does not give them.
func Defaults() Curve {
	return Curve{Hysteresis: 4.0, DownRate: 1.5, Predict: 5.0, Couple: 15, BatteryShift: 3.0}
}

// Interp is the piecewise-linear duty for temperature t, clamped to the first and last
// point. The points must be sorted by temperature; an empty list gives 0 (firmware Auto).
func Interp(points []Point, t float64) float64 {
	if len(points) == 0 {
		return 0
	}
	if t <= points[0].Temp() {
		return points[0].Duty()
	}
	for i := 0; i+1 < len(points); i++ {
		t0, p0, t1, p1 := points[i].Temp(), points[i].Duty(), points[i+1].Temp(), points[i+1].Duty()
		if t <= t1 {
			if t1 > t0 {
				return p0 + (p1-p0)*(t-t0)/(t1-t0)
			}
			return p1
		}
	}
	return points[len(points)-1].Duty()
}

const pointsHelp = "points look like 55:0,62:35,70:55,80:80,88:100  (temperature:percent)"

// ParsePoints parses the command-line form "52:0,60:35,70:55,88:100". It only parses: the
// result still has to pass Curve validation. Spaces are ignored and empty items skipped.
func ParsePoints(text string) ([]Point, error) {
	var out []Point
	for _, item := range strings.Split(strings.ReplaceAll(text, " ", ""), ",") {
		if item == "" {
			continue
		}
		parts := strings.Split(item, ":")
		if len(parts) != 2 {
			return nil, firmware.Errorf("%s", pointsHelp)
		}
		t, errT := parseFinite(parts[0])
		p, errP := parseFinite(parts[1])
		if errT != nil || errP != nil {
			return nil, firmware.Errorf("%s", pointsHelp)
		}
		out = append(out, Point{t, p})
	}
	return out, nil
}

// parseFinite parses a number the way a person types it. NaN and infinities are refused:
// they can never be a temperature or a duty.
func parseFinite(s string) (float64, error) {
	f, err := strconv.ParseFloat(strings.TrimSpace(s), 64)
	if err != nil {
		return 0, err
	}
	if math.IsNaN(f) || math.IsInf(f, 0) {
		return 0, fmt.Errorf("%q is not a finite number", s)
	}
	return f, nil
}
