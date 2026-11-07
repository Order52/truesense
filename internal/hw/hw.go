// Package hw gives each firmware feature of the Predator Helios 16 a typed function: power
// mode, fans, battery, display, boot sound, GPU mode, rear light bar and sensors.
//
// Everything goes through a firmware.Device. Every setter writes, then reads back (retrying
// for about a second, because the firmware can answer -1 or the old value right after a
// write) and returns the verified new value or an error: a value that cannot be verified is
// never reported as set.
package hw

import (
	"context"
	"fmt"
	"os/exec"
	"strings"
	"time"

	"github.com/Order52/truesense/internal/firmware"
	"github.com/Order52/truesense/internal/userstate"
)

// Power modes, in the firmware's numbering.
var (
	// ModeNames lists the modes in firmware order.
	ModeNames = []string{"quiet", "balanced", "performance", "turbo", "eco"}
	modeCode  = map[string]int{"quiet": 0, "balanced": 1, "performance": 4, "turbo": 5, "eco": 6}
	// GPUModeNames lists the GPU (MUX) modes.
	GPUModeNames = []string{"optimus", "nvidia", "auto"}
	gpuCode      = map[string]int{"optimus": 1, "nvidia": 2, "auto": 3}
	// USBLevels are the battery levels USB charging can be limited to while the laptop is off.
	USBLevels = []int{0, 10, 20, 30}
)

// KBTimeout is the keyboard controller's own "backlight off after 30 s" flag. It lives behind
// an interface because the keyboard package (which owns the HID device) sits above hw.
type KBTimeout interface {
	// Get returns the flag, or (nil, nil) when the controller cannot say.
	Get() (*bool, error)
	Set(on bool) error
}

// Runner runs an external program (nvidia-smi) and returns its standard output.
type Runner func(ctx context.Context, name string, args ...string) ([]byte, error)

// HW is the hardware, as one value that carries its dependencies.
type HW struct {
	Dev      firmware.Device
	State    *userstate.Store // remembers the rear light's colours; may be nil
	KBT      KBTimeout        // may be nil
	Exec     Runner
	SysRoot  string // prefix of /sys paths (empty = the real /sys); tests point it at a temp tree
	Tries    int
	Delay    time.Duration
	SleepFor func(time.Duration)
}

// New returns a HW over dev with production timing (10 read-backs, 100 ms apart).
func New(dev firmware.Device, state *userstate.Store) *HW {
	return &HW{Dev: dev, State: state, Exec: defaultExec, Tries: 10, Delay: 100 * time.Millisecond, SleepFor: time.Sleep}
}

func defaultExec(ctx context.Context, name string, args ...string) ([]byte, error) {
	return exec.CommandContext(ctx, name, args...).Output()
}

func (h *HW) do(command string) (firmware.Reply, error) { return firmware.Do(h.Dev, command) }

func (h *HW) get(command, key string) (int, error) {
	r, err := h.do(command)
	if err != nil {
		return 0, err
	}
	return r.Int(key)
}

// settle polls get until it reports want, or fails after the configured tries.
func settle[T comparable](h *HW, get func() (T, error), want T, what string) (T, error) {
	var got T
	var have bool
	for i := 0; i < h.Tries; i++ {
		if v, err := get(); err == nil {
			got, have = v, true
			if v == want {
				return want, nil
			}
		} else {
			have = false
		}
		h.SleepFor(h.Delay)
	}
	if !have {
		return want, firmware.Errorf("%s readback mismatch (wanted %v, last read nothing)", what, want)
	}
	return want, firmware.Errorf("%s readback mismatch (wanted %v, last read %v)", what, want, got)
}

// Version is the kernel module's version.
func (h *HW) Version() (string, error) {
	r, err := h.do("version")
	if err != nil {
		return "", err
	}
	v, ok := r["version"]
	if !ok {
		return "", firmware.Errorf("missing version in reply")
	}
	return v, nil
}

// Capabilities reports which firmware interfaces the module found.
func (h *HW) Capabilities() (map[string]bool, error) {
	r, err := h.do("caps")
	if err != nil {
		return nil, err
	}
	out := map[string]bool{}
	for k, v := range r {
		out[k] = v == "1"
	}
	return out, nil
}

// ---- power mode ----

// GetMode returns the current power mode name ("unknown(N)" for a code we do not know).
func (h *HW) GetMode() (string, error) {
	n, err := h.get("get_mode", "mode")
	if err != nil {
		return "", err
	}
	for name, code := range modeCode {
		if code == n {
			return name, nil
		}
	}
	return fmt.Sprintf("unknown(%d)", n), nil
}

// SetMode sets the power mode and verifies it.
func (h *HW) SetMode(name string) (string, error) {
	code, ok := modeCode[name]
	if !ok {
		return "", firmware.Errorf("power mode must be one of: %s", strings.Join(ModeNames, ", "))
	}
	if _, err := h.do(fmt.Sprintf("set_mode %d", code)); err != nil {
		return "", err
	}
	return settle(h, h.GetMode, name, "power mode")
}

// SupportedModes lists the modes this laptop's firmware accepts.
func (h *HW) SupportedModes() ([]string, error) {
	mask, err := h.get("get_modes", "mask")
	if err != nil {
		return nil, err
	}
	var out []string
	for _, n := range ModeNames {
		if mask>>modeCode[n]&1 == 1 {
			out = append(out, n)
		}
	}
	return out, nil
}

// ---- on/off firmware settings ----

func b2i(on bool) int {
	if on {
		return 1
	}
	return 0
}

func (h *HW) getBool(command, key string) (bool, error) {
	n, err := h.get(command, key)
	return n != 0, err
}

func (h *HW) setBool(set, what string, getter func() (bool, error), on bool) (bool, error) {
	if _, err := h.do(fmt.Sprintf("%s %d", set, b2i(on))); err != nil {
		return false, err
	}
	return settle(h, getter, on, what)
}

// GetOverdrive reports panel overdrive; ok is false when the firmware answers "unknown" (-1).
func (h *HW) GetOverdrive() (on, ok bool, err error) {
	n, err := h.get("get_overdrive", "overdrive")
	if err != nil || n < 0 {
		return false, false, err
	}
	return n != 0, true, nil
}

func (h *HW) overdriveKnown() (bool, error) {
	on, ok, err := h.GetOverdrive()
	if err != nil {
		return false, err
	}
	if !ok {
		return false, firmware.Errorf("overdrive unknown")
	}
	return on, nil
}

// SetOverdrive switches panel overdrive and verifies it.
func (h *HW) SetOverdrive(on bool) (bool, error) {
	return h.setBool("set_overdrive", "overdrive", h.overdriveKnown, on)
}

// GetBootSound / SetBootSound: the firmware's start-up chime.
func (h *HW) GetBootSound() (bool, error) { return h.getBool("get_boot", "boot") }
func (h *HW) SetBootSound(on bool) (bool, error) {
	return h.setBool("set_boot", "boot sound", h.GetBootSound, on)
}

func (h *HW) wmiKBTimeout() (bool, error) {
	n, err := h.get("get_kbtimeout", "kbtimeout")
	if err != nil {
		return false, err
	}
	if n < 0 {
		return false, firmware.Errorf("keyboard timeout unknown")
	}
	return n != 0, nil
}

// GetKBTimeout reports "backlight off after 30 s of no typing". The keyboard controller's own
// flag is the one that acts; the firmware flag is kept in step and is the fallback.
func (h *HW) GetKBTimeout() (bool, error) {
	if h.KBT != nil {
		if v, err := h.KBT.Get(); err == nil && v != nil {
			return *v, nil
		}
	}
	return h.wmiKBTimeout()
}

// SetKBTimeout sets both flags and verifies the one that acts.
func (h *HW) SetKBTimeout(on bool) (bool, error) {
	if _, err := h.do(fmt.Sprintf("set_kbtimeout %d", b2i(on))); err != nil {
		return false, err
	}
	if h.KBT != nil {
		_ = h.KBT.Set(on) // the controller flag is best effort: the firmware flag above already took
	}
	return settle(h, h.GetKBTimeout, on, "backlight timeout")
}

// GetUSBCharge / SetUSBCharge: charge phones over USB while the laptop is off, down to this battery level (0 = off).
func (h *HW) GetUSBCharge() (int, error) { return h.get("get_usb", "usb") }
func (h *HW) SetUSBCharge(level int) (int, error) {
	ok := false
	for _, l := range USBLevels {
		ok = ok || l == level
	}
	if !ok {
		return 0, firmware.Errorf("usb-charge level: 0 (off), 10, 20 or 30")
	}
	if _, err := h.do(fmt.Sprintf("set_usb %d", level)); err != nil {
		return 0, err
	}
	return settle(h, h.GetUSBCharge, level, "usb charging")
}

// GetBatteryLimit / SetBatteryLimit: stop charging at 80 %.
func (h *HW) GetBatteryLimit() (bool, error) { return h.getBool("get_battery 1", "battery") }
func (h *HW) SetBatteryLimit(on bool) (bool, error) {
	return h.setBool("set_battery 1", "battery limit", h.GetBatteryLimit, on)
}

// GetCalibration / SetCalibration: the firmware's battery calibration cycle (needs AC power; takes hours).
func (h *HW) GetCalibration() (bool, error) { return h.getBool("get_battery 2", "battery") }
func (h *HW) SetCalibration(on bool) (bool, error) {
	return h.setBool("set_battery 2", "battery calibration", h.GetCalibration, on)
}

// ---- GPU mode (MUX; takes effect after a reboot) ----

// GetGPUMode returns optimus | nvidia | auto.
func (h *HW) GetGPUMode() (string, error) {
	n, err := h.get("get_gpu", "gpu")
	if err != nil {
		return "", err
	}
	for name, code := range gpuCode {
		if code == n {
			return name, nil
		}
	}
	return fmt.Sprintf("unknown(%d)", n), nil
}

// SetGPUMode sets the GPU mode and verifies it. It applies after a reboot.
func (h *HW) SetGPUMode(name string) (string, error) {
	code, ok := gpuCode[name]
	if !ok {
		return "", firmware.Errorf("gpu mode: optimus | nvidia | auto (applies after reboot)")
	}
	if _, err := h.do(fmt.Sprintf("set_gpu %d", code)); err != nil {
		return "", err
	}
	return settle(h, h.GetGPUMode, name, "gpu mode")
}
