package api

import (
	"errors"
	"fmt"
	"slices"

	"github.com/Order52/truesense/internal/client"
	"github.com/Order52/truesense/internal/curves"
	"github.com/Order52/truesense/internal/hw"
	"github.com/Order52/truesense/internal/ipc"
)

// FanStatus is the fans as the firmware reports them (behaviour and duty registers), their
// speed, and whether the service's smart curve is in control (nil without a service).
type FanStatus struct {
	hw.FanState
	CPUFanRPM int   `json:"cpu_fan_rpm"`
	GPUFanRPM int   `json:"gpu_fan_rpm"`
	Smart     *bool `json:"smart"`
}

// FanStatus reads the fans.
func (a *API) FanStatus() (FanStatus, error) {
	fs, err := a.s.HW.GetFanState()
	if err != nil {
		return FanStatus{}, err
	}
	sn, err := a.s.HW.ReadSensors()
	if err != nil {
		return FanStatus{}, err
	}
	st := FanStatus{FanState: fs, CPUFanRPM: sn.CPUFanRPM, GPUFanRPM: sn.GPUFanRPM}
	if d, err := a.DaemonState(); err == nil {
		smart := d.Bool("smart_fan")
		st.Smart = &smart
	}
	return st, nil
}

// FanAuto hands the fans to the firmware (they stop when cool).
func (a *API) FanAuto() error { return a.s.Client.Fan(client.FanRequest{Mode: "auto"}) }

// FanMax runs both fans at full speed until the smart curve (or Auto) takes over again.
func (a *API) FanMax() error { return a.s.Client.Fan(client.FanRequest{Mode: "max"}) }

// FanSmart hands the fans to the service's smart curve.
func (a *API) FanSmart() error { return a.s.Client.Fan(client.FanRequest{Mode: "smart"}) }

// FanOff stops both fans. It needs the service (its temperature watchdog gives the fans back
// when the laptop gets warm) and an explicit confirmation.
func (a *API) FanOff(confirm bool) error {
	if !confirm && a.DaemonAvailable() { // without the service the client refuses the stop anyway
		return errors.New(errStopNeedsConfirm)
	}
	return a.s.Client.Fan(client.FanRequest{Mode: "off", Confirm: confirm})
}

// FanSet sets the duty of each fan: 20..100 %, or 0 to stop that fan (which needs the service
// and the confirmation, like FanOff). A nil gpu uses the CPU's duty for both.
func (a *API) FanSet(cpu int, gpu *int, confirm bool) error {
	if (cpu == 0 || (gpu != nil && *gpu == 0)) && !confirm && a.DaemonAvailable() {
		return errors.New(errStopNeedsConfirm) // whoever asks: a stop is never silent
	}
	return a.s.Client.Fan(client.FanRequest{Mode: "manual", CPU: &cpu, GPU: gpu, Confirm: confirm})
}

// SmartFan reports whether the service's smart curve is in control.
func (a *API) SmartFan() (bool, error) { return a.daemonBool("smart_fan") }

// SetSmartFan switches the service's smart curve and answers the value it holds. Switching
// it off gives the fans back to the firmware's own control.
func (a *API) SetSmartFan(on bool) (bool, error) { return a.daemonSetBool("smart_fan", on) }

// FanCurveInUse is the service's answer to which fan curve runs.
type FanCurveInUse struct {
	Name  string       `json:"name"`
	Curve curves.Curve `json:"curve"`
	Names []string     `json:"names,omitempty"`
}

// FanCurves lists the fan curves and, when the service runs, the one in use.
func (a *API) FanCurves() (NamedList, error) {
	names, err := a.s.Curves.Names()
	if err != nil {
		return NamedList{}, err
	}
	l := NamedList{Names: names, Descriptions: map[string]string{}}
	for _, n := range names {
		if c, ok := curves.Builtin(n); ok {
			l.Descriptions[n] = c.Description
		}
	}
	if a.DaemonAvailable() {
		cur, err := a.FanCurveInUse()
		if err != nil {
			return NamedList{}, err
		}
		l.Current = cur.Name
	}
	return l, nil
}

// FanCurveInUse asks the service which curve runs (it keeps it, so it works before login).
func (a *API) FanCurveInUse() (FanCurveInUse, error) {
	var r FanCurveInUse
	err := a.s.IPC.CallInto("fan_curve", ipc.Args{"action": "get"}, &r)
	return r, err
}

// FanCurve reads one curve, built-in or yours.
func (a *API) FanCurve(name string) (curves.Curve, error) { return a.s.Curves.Load(name) }

// UseFanCurve makes a curve the one the service follows. A saved curve is sent as data, so
// the service does not depend on files in a home directory.
func (a *API) UseFanCurve(name string) (string, error) {
	if name == "" {
		return "", fmt.Errorf("which curve? (truesense fan curve list)")
	}
	args := ipc.Args{"action": "use", "name": name, "data": nil}
	if !curves.IsBuiltin(name) {
		c, err := a.s.Curves.Load(name)
		if err != nil {
			return "", err
		}
		args["data"] = c
	}
	var r FanCurveInUse
	if err := a.s.IPC.CallInto("fan_curve", args, &r); err != nil {
		return "", err
	}
	return r.Name, nil
}

// CurveEdit is what `fan curve save` changes: points as "52:0,60:35,...", the follower
// parameters when given, and the curve to start from.
type CurveEdit struct {
	CPU, GPU     string
	Hysteresis   *float64
	DownRate     *float64
	Predict      *float64
	Couple       *float64
	BatteryShift *float64
	From         string
}

// SaveFanCurve saves a curve: start from From (or from your own curve of that name) and
// change only what the edit gives. It checks the curve like the service does, so a curve that
// is too cool for the load is refused.
func (a *API) SaveFanCurve(name string, e CurveEdit) (curves.Curve, error) {
	if name == "" {
		return curves.Curve{}, fmt.Errorf("save under which name?")
	}
	c := curves.Defaults()
	switch {
	case e.From != "":
		b, err := a.s.Curves.Load(e.From)
		if err != nil {
			return c, err
		}
		c = b
	case !curves.IsBuiltin(name):
		if names, _ := a.s.Curves.Names(); slices.Contains(names, name) {
			b, err := a.s.Curves.Load(name)
			if err != nil {
				return c, err
			}
			c = b
		}
	}
	c.Description = ""
	if e.CPU != "" {
		pts, err := curves.ParsePoints(e.CPU)
		if err != nil {
			return c, err
		}
		c.CPU = pts
	}
	if e.GPU != "" {
		pts, err := curves.ParsePoints(e.GPU)
		if err != nil {
			return c, err
		}
		c.GPU = pts
	}
	for _, p := range []struct {
		v   *float64
		dst *float64
	}{{e.Hysteresis, &c.Hysteresis}, {e.DownRate, &c.DownRate}, {e.Predict, &c.Predict}, {e.Couple, &c.Couple}, {e.BatteryShift, &c.BatteryShift}} {
		if p.v != nil {
			*p.dst = *p.v
		}
	}
	if len(c.CPU) == 0 || len(c.GPU) == 0 {
		return c, fmt.Errorf("give --cpu and --gpu points (or --from an existing curve)")
	}
	return a.s.Curves.Save(name, c)
}

// CopyFanCurve saves a copy under newName (NAME-copy when empty) and answers the name.
func (a *API) CopyFanCurve(name, newName string) (string, error) {
	if name == "" {
		return "", fmt.Errorf("copy which curve? (truesense fan curve list)")
	}
	if newName == "" {
		newName = name + "-copy"
	}
	c, err := a.s.Curves.Load(name)
	if err != nil {
		return "", err
	}
	if _, err := a.s.Curves.Save(newName, c); err != nil {
		return "", err
	}
	return newName, nil
}

// DeleteFanCurve removes one of your curves (built-in ones are read-only).
func (a *API) DeleteFanCurve(name string) error { return a.s.Curves.Delete(name) }

// UseFanCurveData makes a curve that is not saved (the window's editor) the one the service
// follows. name is only a label. The curve is checked like a saved one: a curve that is too
// cool for the load is refused.
func (a *API) UseFanCurveData(name string, c curves.Curve) (string, error) {
	c, err := c.Validated()
	if err != nil {
		return "", err
	}
	var r FanCurveInUse
	if err := a.s.IPC.CallInto("fan_curve", ipc.Args{"action": "use", "name": orElse(name, "custom"), "data": c}, &r); err != nil {
		return "", err
	}
	return r.Name, nil
}

// SaveFanCurveValue saves a whole curve as you edited it (built-in names are refused).
func (a *API) SaveFanCurveValue(name string, c curves.Curve) (curves.Curve, error) {
	if name == "" {
		return curves.Curve{}, fmt.Errorf("save under which name?")
	}
	c.Description = ""
	return a.s.Curves.Save(name, c)
}

// FanCurveIsBuiltin reports whether a curve is read-only.
func FanCurveIsBuiltin(name string) bool { return curves.IsBuiltin(name) }

func orElse(s, def string) string {
	if s == "" {
		return def
	}
	return s
}
