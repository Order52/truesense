// Package fanctl is the smart fan: it turns temperatures into fan duties with the active curve,
// raises them at once and lowers them slowly, holds a floor while gaming or working hard, hands
// the fans back to the firmware when the laptop is calm (the firmware is the only way to reach
// 0 rpm) and drives them to 100 % when it is hot. The decisions (Smart) are separate from the
// hardware writes (Writer), and both are separate from the user's explicit fan stop (the guards).
//
// Measured fan response (manual duty, both fans): 20 % about 1700 rpm, 30 2160, 40 2850,
// 50 3460, 60 4070, 70 4600, 80 5100, 100 6050.
package fanctl

import (
	"fmt"
	"time"

	"github.com/Order52/truesense/internal/autoprofile"
	"github.com/Order52/truesense/internal/curves"
)

// Fans may only be stopped below the Start temperatures, and the watchdog gives them back to the
// firmware at the Max temperatures.
const (
	StopStartCPU = 60
	StopStartGPU = 55
	StopMaxCPU   = 70
	StopMaxGPU   = 65
)

// CanStop says whether a fan may be stopped at these temperatures (nil) or why not.
func CanStop(tcpu, tgpu float64, known bool) error {
	if !known || tcpu >= StopStartCPU || tgpu >= StopStartGPU {
		return fmt.Errorf("too warm to stop a fan (CPU %.0f C, GPU %.0f C; a fan may only be stopped below %d / %d C)", tcpu, tgpu, StopStartCPU, StopStartGPU)
	}
	return nil
}

// MustReturnFans is the watchdog for a user-stopped fan: when the laptop warms up (or the
// hardware stops answering) the fans must go back to firmware Auto.
func MustReturnFans(tcpu, tgpu float64, hwOK bool) bool {
	return tcpu >= StopMaxCPU || tgpu >= StopMaxGPU || !hwOK
}

// Params are the settings of the smart fan that do not come from the curve.
type Params struct {
	CalmCPU, CalmGPU float64 // calm below these temperatures
	CalmHold         int     // calm for this many ticks before the fans go back to the firmware
}

// Inputs are the readings one step looks at.
type Inputs struct {
	Activity   autoprofile.Activity
	TCPU, TGPU float64
	TChassis   float64 // 0 when unknown
	GPUPower   float64
	OnAC       bool
	Dt         time.Duration // time since the previous step
}

// Smart is the curve-driven fan controller. It is not safe for concurrent use: the daemon's
// control loop owns it.
type Smart struct {
	P        Params
	follower *curves.Follower
	calm     int
	histC    []float64
	histG    []float64
}

// NewSmart returns a controller following curve.
func NewSmart(p Params, curve curves.Curve) *Smart {
	return &Smart{P: p, follower: curves.NewFollower(curve)}
}

// SetCurve switches the curve cleanly (both duties start again from 0).
func (s *Smart) SetCurve(c curves.Curve) { s.follower.SetCurve(c) }

// Curve returns the curve in use.
func (s *Smart) Curve() curves.Curve { return s.follower.Curve() }

// Reset forgets the calm counter (the smart fan was off).
func (s *Smart) Reset() { s.calm = 0 }

// Calm is how many ticks in a row the laptop has been calm.
func (s *Smart) Calm() int { return s.calm }

// slope is the temperature change per sample over the last five samples (0 until there are five).
func slope(hist *[]float64, v float64) float64 {
	*hist = append(*hist, v)
	if len(*hist) > 5 {
		*hist = (*hist)[1:]
	}
	if len(*hist) == 5 {
		return ((*hist)[4] - (*hist)[0]) / 4
	}
	return 0
}

// Floor is the minimum duty for this activity: a game or a heavy job needs air even when the
// temperature has not moved yet, and a hot board or VRM (the chassis sensor) raises it further.
func Floor(a autoprofile.Activity, gpuPower, tchassis float64) int {
	f := 0
	switch {
	case a == autoprofile.Gaming && gpuPower >= 80:
		f = 60
	case a == autoprofile.Gaming:
		f = 50
	case a == autoprofile.Heavy:
		f = 40
	}
	if f > 0 && tchassis > 0 {
		switch {
		case tchassis >= 68:
			f = max(f, 60)
		case tchassis >= 63:
			f = max(f, 50)
		}
	}
	return f
}

// Step returns the duties to write: (0, 0) means "hand the fans to the firmware".
func (s *Smart) Step(in Inputs) (cpu, gpu int) {
	calmNow := (in.Activity == autoprofile.Idle || in.Activity == autoprofile.Browsing) && in.TCPU < s.P.CalmCPU && in.TGPU < s.P.CalmGPU
	if calmNow {
		s.calm++
	} else {
		s.calm = 0
	}
	c, g := s.follower.Duty()
	if s.calm >= s.P.CalmHold || (max(c, g) < 20 && calmNow) {
		s.follower.SetDuty(0, 0)
		return 0, 0
	}
	if in.Activity == autoprofile.Hot {
		s.follower.SetDuty(100, 100)
		return 100, 100
	}
	shift := s.follower.BatteryShift(in.OnAC)
	s.follower.Step(curves.SideCPU, in.TCPU, slope(&s.histC, in.TCPU) >= 1, in.Dt, shift)
	s.follower.Step(curves.SideGPU, in.TGPU, slope(&s.histG, in.TGPU) >= 1, in.Dt, shift)
	return s.follower.Output(Floor(in.Activity, in.GPUPower, in.TChassis))
}
