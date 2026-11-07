// Package adapt connects the terminal UI to the application layer: it implements tui.Backend
// over api.API, so a setting changed in the terminal runs the same code, with the same safety
// rules, as the command line. It adds nothing of its own except the translation between the
// UI's small value types and the application's.
package adapt

import (
	"context"
	"math"
	"time"

	"github.com/Order52/truesense/internal/api"
	"github.com/Order52/truesense/internal/assembly"
	"github.com/Order52/truesense/internal/tui"
)

// backend is the tui.Backend over one API.
type backend struct {
	a *api.API
	s *assembly.Services
}

// TUI returns the terminal UI's backend.
func TUI(a *api.API, s *assembly.Services) tui.Backend { return &backend{a: a, s: s} }

// ---- LiveBackend

// number reads a numeric field of the service's live state (JSON numbers arrive as float64).
func number(m map[string]any, key string) (int, bool) {
	f, ok := m[key].(float64)
	if !ok {
		return 0, false
	}
	return int(math.Round(f)), true
}

func opt(m map[string]any, key string) tui.Opt[int] {
	if v, ok := number(m, key); ok {
		return tui.Some(v)
	}
	return tui.Opt[int]{}
}

func text(m map[string]any, key string) string {
	v, _ := m[key].(string)
	return v
}

// ServiceState is the service's state, nil when it is not running (the UI then reads the
// hardware itself).
func (b *backend) ServiceState(context.Context) (*tui.ServiceState, error) {
	if !b.a.DaemonAvailable() {
		return nil, nil
	}
	st, err := b.a.DaemonState()
	if err != nil {
		return nil, nil //nolint:nilerr // an unreachable service is the same as none: the UI reads the hardware
	}
	live := st.Live
	out := &tui.ServiceState{
		AutoMode: text(live, "auto_mode"), SmartProfile: st.String("smart_profile"), SmartFan: st.Bool("smart_fan"), FanCurve: st.String("fan_curve"),
		CPULoad: opt(live, "cpu"), GPULoad: opt(live, "gpu"), GPUWatts: opt(live, "gpuw"),
		Activity: text(live, "activity"), ModeReason: text(live, "mode_reason"), HoldLeft: time.Duration(st.HoldLeftS) * time.Second,
	}
	if out.AutoMode == "" {
		out.AutoMode = st.String("auto_mode")
	}
	if fo, ok := live["fan_off"].(bool); ok {
		out.FanOff = fo
	}
	if ac, ok := live["ac"].(bool); ok {
		out.AC = tui.Some(ac)
	}
	if m := text(live, "power_mode"); m != "" && m != "?" {
		out.PowerMode = m
	}
	rpm1, ok1 := number(live, "rpm1")
	rpm2, _ := number(live, "rpm2")
	tcpu, ok2 := number(live, "tcpu")
	if ok1 && ok2 { // the service already read these this second
		tgpu, _ := number(live, "tgpu")
		chassis, _ := number(live, "chassis")
		out.Sensors = &tui.Sensors{CPUTemp: tcpu, SysTemp: chassis, GPUTemp: tgpu, CPUFanRPM: rpm1, GPUFanRPM: rpm2}
	}
	return out, nil
}

func (b *backend) Sensors(context.Context) (tui.Sensors, error) {
	s, err := b.a.Sensors()
	return tui.Sensors{CPUTemp: s.CPUTemp, SysTemp: s.SysTemp, GPUTemp: s.GPUTemp, CPUFanRPM: s.CPUFanRPM, GPUFanRPM: s.GPUFanRPM}, err
}

func (b *backend) FanState(context.Context) (tui.FanState, error) {
	f, err := b.s.HW.GetFanState()
	return tui.FanState{Mode: f.Mode, CPUDuty: f.Duty.CPU, GPUDuty: f.Duty.GPU}, err
}

func (b *backend) PowerMode(context.Context) (string, error) { return b.s.HW.GetMode() }

func (b *backend) OnAC(context.Context) (bool, error) { return b.s.HW.OnAC(), nil }

// Battery reads the first battery of the power supply class.
func (b *backend) Battery() (tui.Battery, bool) {
	s, ok := b.a.Supply()
	return tui.Battery{Percent: s.Percent, Status: s.Status}, ok
}
