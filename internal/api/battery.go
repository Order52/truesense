package api

import "github.com/Order52/truesense/internal/ipc"

// BatteryStatus is the firmware's three battery settings.
type BatteryStatus struct {
	Limit80       bool `json:"limit_80"`
	Calibration   bool `json:"calibration"`
	USBChargeStop int  `json:"usb_charge_stop"`
}

// BatteryStatus reads the 80 % limit, the calibration cycle and the USB charging level.
func (a *API) BatteryStatus() (BatteryStatus, error) {
	var st BatteryStatus
	var err error
	if st.Limit80, err = a.s.HW.GetBatteryLimit(); err != nil {
		return st, err
	}
	if st.Calibration, err = a.s.HW.GetCalibration(); err != nil {
		return st, err
	}
	st.USBChargeStop, err = a.s.HW.GetUSBCharge()
	return st, err
}

// BatteryLimit reports whether charging stops at 80 %.
func (a *API) BatteryLimit() (bool, error) { return a.s.HW.GetBatteryLimit() }

// SetBatteryLimit turns the 80 % limit on or off (verified by read-back).
func (a *API) SetBatteryLimit(on bool) (bool, error) { return a.s.HW.SetBatteryLimit(on) }

// Calibration reports whether the battery calibration cycle runs (it needs AC and takes hours).
func (a *API) Calibration() (bool, error) { return a.s.HW.GetCalibration() }

// SetCalibration starts or stops the calibration cycle (verified by read-back).
func (a *API) SetCalibration(on bool) (bool, error) { return a.s.HW.SetCalibration(on) }

// USBCharge is the battery level down to which USB ports charge while the laptop is off (0 = off).
func (a *API) USBCharge() (int, error) { return a.s.HW.GetUSBCharge() }

// SetUSBCharge sets that level (0, 10, 20 or 30), verified by read-back.
func (a *API) SetUSBCharge(level int) (int, error) { return a.s.HW.SetUSBCharge(level) }

// ChargeFull is "charge to 100 % once": whether it runs and, for a status question, the
// battery limit, level and mains state the service sees.
type ChargeFull struct {
	Active   bool     `json:"active"`
	Limit    *bool    `json:"limit,omitempty"`
	Capacity *float64 `json:"capacity,omitempty"`
	AC       *bool    `json:"ac,omitempty"`
}

// ChargeFullStatus asks the service whether a one-time full charge runs.
func (a *API) ChargeFullStatus() (ChargeFull, error) { return a.chargeFull("get") }

// StartChargeFull lifts the 80 % limit once: it comes back when you unplug, when the battery
// has been full for 5 minutes, or after 24 hours. The charger must be plugged in.
func (a *API) StartChargeFull() (ChargeFull, error) { return a.chargeFull("start") }

// CancelChargeFull puts the 80 % limit back now.
func (a *API) CancelChargeFull() (ChargeFull, error) { return a.chargeFull("cancel") }

func (a *API) chargeFull(action string) (ChargeFull, error) {
	var r ChargeFull
	err := a.s.IPC.CallInto("battery_full", ipc.Args{"action": action}, &r)
	return r, err
}
