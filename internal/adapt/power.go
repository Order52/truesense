package adapt

import (
	"context"
	"errors"
	"time"

	"github.com/Order52/truesense/internal/tui"
)

// ---- PowerBackend

// SetPowerMode switches through the service when it runs (which also holds the pick), else on
// the hardware.
func (b *backend) SetPowerMode(_ context.Context, mode string, hold time.Duration) error {
	_, err := b.a.SetPower(mode, hold.Minutes())
	return err
}

func (b *backend) SetAutoMode(_ context.Context, mode string) error { return b.a.SetAutoMode(mode) }

func (b *backend) ReleaseHold(context.Context) error { return b.a.ReleaseHold() }

func (b *backend) SmartProfileNames() []string {
	names, _ := b.s.Smart.Names()
	return names
}

func (b *backend) SmartProfile(context.Context) (string, error) {
	p, err := b.a.SmartProfileInUse()
	return p.Name, err
}

func (b *backend) UseSmartProfile(_ context.Context, name string) error {
	_, err := b.a.UseSmartProfile(name)
	return err
}

// ---- FanBackend

var errStopNeedsConfirm = errors.New("stopping a fan needs explicit confirmation")

// SetFan applies a request. A request that stops a fan (Mode off, or a manual duty of 0) is
// refused here unless it is confirmed; whether the service and its temperature watchdog are
// there to take it is decided below, by the client.
func (b *backend) SetFan(_ context.Context, r tui.FanRequest) error {
	switch r.Mode {
	case "smart":
		return b.a.FanSmart()
	case "auto":
		return b.a.FanAuto()
	case "max":
		return b.a.FanMax()
	case "off":
		if !r.Confirm {
			return errStopNeedsConfirm
		}
		return b.a.FanOff(true)
	case "manual":
		if (r.CPU == 0 || r.GPU == 0) && !r.Confirm {
			return errStopNeedsConfirm
		}
		return b.a.FanSet(r.CPU, &r.GPU, r.Confirm)
	}
	return errors.New("fan mode: smart | auto | max | manual | off")
}

func (b *backend) FanCurveNames() []string {
	names, _ := b.s.Curves.Names()
	return names
}

func (b *backend) FanCurve(context.Context) (string, error) {
	c, err := b.a.FanCurveInUse()
	return c.Name, err
}

func (b *backend) UseFanCurve(_ context.Context, name string) error {
	_, err := b.a.UseFanCurve(name)
	return err
}

// ---- BatteryBackend

func (b *backend) BatteryLimit(context.Context) (bool, error) { return b.a.BatteryLimit() }
func (b *backend) SetBatteryLimit(_ context.Context, on bool) error {
	_, err := b.a.SetBatteryLimit(on)
	return err
}
func (b *backend) Calibration(context.Context) (bool, error) { return b.a.Calibration() }
func (b *backend) SetCalibration(_ context.Context, on bool) error {
	_, err := b.a.SetCalibration(on)
	return err
}
func (b *backend) USBCharge(context.Context) (int, error) { return b.a.USBCharge() }
func (b *backend) SetUSBCharge(_ context.Context, percent int) error {
	_, err := b.a.SetUSBCharge(percent)
	return err
}

func (b *backend) ChargeToFullActive(context.Context) (bool, error) {
	r, err := b.a.ChargeFullStatus()
	return r.Active, err
}

func (b *backend) ChargeToFull(_ context.Context, start bool) error {
	var err error
	if start {
		_, err = b.a.StartChargeFull()
	} else {
		_, err = b.a.CancelChargeFull()
	}
	return err
}

// ---- DisplayBackend

func (b *backend) GPUMode(context.Context) (string, error) { return b.a.GPUMode() }
func (b *backend) SetGPUMode(_ context.Context, mode string) error {
	_, err := b.a.SetGPUMode(mode)
	return err
}

// Overdrive reads the panel overdrive; a firmware that cannot say reads as off.
func (b *backend) Overdrive(context.Context) (bool, error) {
	v, err := b.a.Overdrive()
	return v != nil && *v, err
}
func (b *backend) SetOverdrive(_ context.Context, on bool) error {
	_, err := b.a.SetOverdrive(on)
	return err
}
func (b *backend) BootSound(context.Context) (bool, error) { return b.a.BootSound() }
func (b *backend) SetBootSound(_ context.Context, on bool) error {
	_, err := b.a.SetBootSound(on)
	return err
}
func (b *backend) KeyboardTimeout(context.Context) (bool, error) { return b.a.KeyboardTimeout() }
func (b *backend) SetKeyboardTimeout(_ context.Context, on bool) error {
	_, err := b.a.SetKeyboardTimeout(on)
	return err
}
