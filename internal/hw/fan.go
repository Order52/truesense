package hw

import (
	"fmt"

	"github.com/Order52/truesense/internal/firmware"
)

// Fan behaviour codes the firmware reports.
const (
	FanAutoBehavior = 0x41
	fanMaxBehavior  = 0x82
	fanManBehavior  = 0xC3
)

// Sensors is one reading of every temperature (degrees C) and both fan speeds (rpm).
type Sensors struct {
	CPUTemp   int `json:"cpu_temp"`
	SysTemp   int `json:"sys_temp"`
	GPUTemp   int `json:"gpu_temp"`
	CPUFanRPM int `json:"cpu_fan_rpm"`
	GPUFanRPM int `json:"gpu_fan_rpm"`
}

// ReadSensors reads everything in one firmware call.
func (h *HW) ReadSensors() (Sensors, error) {
	r, err := h.do("sensors")
	if err != nil {
		return Sensors{}, err
	}
	var s Sensors
	for key, dst := range map[string]*int{"cpu": &s.CPUTemp, "sys": &s.SysTemp, "gpu": &s.GPUTemp, "cpu_rpm": &s.CPUFanRPM, "gpu_rpm": &s.GPUFanRPM} {
		if *dst, err = r.Int(key); err != nil {
			return Sensors{}, err
		}
	}
	return s, nil
}

// Duty is the fan duty register of both fans, in percent.
type Duty struct {
	CPU int `json:"cpu"`
	GPU int `json:"gpu"`
}

// FanState is what the firmware reports about fan control.
type FanState struct {
	BehaviorRaw string `json:"behavior_raw"`
	Mode        string `json:"mode"` // auto | max | manual | unknown
	Duty        Duty   `json:"duty_register_pct"`
}

// GetFanState reads the fan behaviour and duty registers.
func (h *HW) GetFanState() (FanState, error) {
	r, err := h.do("get_fan")
	if err != nil {
		return FanState{}, err
	}
	beh, err := r.Int("behavior")
	if err != nil {
		return FanState{}, err
	}
	cpu, err := r.Int("cpu")
	if err != nil {
		return FanState{}, err
	}
	gpu, err := r.Int("gpu")
	if err != nil {
		return FanState{}, err
	}
	mode := "unknown"
	switch beh {
	case FanAutoBehavior:
		mode = "auto"
	case fanMaxBehavior:
		mode = "max"
	case fanManBehavior:
		mode = "manual"
	}
	return FanState{BehaviorRaw: fmt.Sprintf("0x%x", beh), Mode: mode, Duty: Duty{CPU: cpu, GPU: gpu}}, nil
}

func (h *HW) fanMode() (string, error) {
	s, err := h.GetFanState()
	return s.Mode, err
}

// FanAuto hands the fans back to the firmware.
func (h *HW) FanAuto() error {
	if _, err := h.do("set_fan_auto"); err != nil {
		return err
	}
	_, err := settle(h, h.fanMode, "auto", "fan auto")
	return err
}

// FanMax runs both fans at full speed.
func (h *HW) FanMax() error {
	if _, err := h.do("set_fan_max"); err != nil {
		return err
	}
	_, err := settle(h, h.fanMode, "max", "fan max")
	return err
}

// ValidDuty reports whether v is a duty the firmware accepts for one fan: 0 (stopped) or 20..100.
func ValidDuty(v int) bool { return v == 0 || (v >= 20 && v <= 100) }

// FanManual sets both fans' duty. 0 stops a fan: the caller (the daemon) is responsible for
// the confirmation and the temperature watchdog; a stop is proven by read-back, and if the
// firmware does not take it the fans are handed back to Auto.
func (h *HW) FanManual(cpu, gpu int) error {
	if !ValidDuty(cpu) || !ValidDuty(gpu) {
		return firmware.Errorf("manual duty must be 20..100 %% per fan (or 0 to stop that fan)")
	}
	if _, err := h.do(fmt.Sprintf("set_fan_manual %d %d", cpu, gpu)); err != nil {
		return err
	}
	if _, err := settle(h, h.fanMode, "manual", "fan manual"); err != nil {
		return err
	}
	if cpu == 0 || gpu == 0 {
		duty := func() (Duty, error) { s, err := h.GetFanState(); return s.Duty, err }
		if _, err := settle(h, duty, Duty{CPU: cpu, GPU: gpu}, "fan stop"); err != nil {
			_ = h.FanAuto() // a stopped fan was never seen working before: prove the firmware took it, or hand the fans back
			return firmware.Errorf("the firmware did not accept the stop (%v); the fans were returned to Auto", err)
		}
	}
	return nil
}
