package api

import (
	"context"
	"fmt"

	"github.com/Order52/truesense/internal/ipc"
	"github.com/Order52/truesense/internal/powermgr"
)

// PowerStatus is the power mode, and what the service adds when it runs: whether the
// automatic profile is on and how long the user's pick is still held.
type PowerStatus struct {
	Mode        string   `json:"mode"`
	Supported   []string `json:"supported"`
	AutoProfile *bool    `json:"auto_profile,omitempty"`
	HoldLeftS   *int     `json:"hold_left_s,omitempty"`
}

// PowerStatus reads the power mode from the firmware and, if the service answers, its
// automatic profile switch and hold. A missing service is not an error here.
func (a *API) PowerStatus() (PowerStatus, error) {
	mode, err := a.s.HW.GetMode()
	if err != nil {
		return PowerStatus{}, err
	}
	supported, err := a.s.HW.SupportedModes()
	if err != nil {
		return PowerStatus{}, err
	}
	st := PowerStatus{Mode: mode, Supported: supported}
	if d, err := a.DaemonState(); err == nil {
		auto, hold := d.Bool("auto_profile"), d.HoldLeftS
		st.AutoProfile, st.HoldLeftS = &auto, &hold
	}
	return st, nil
}

// PowerModes lists the modes this laptop's firmware accepts.
func (a *API) PowerModes() ([]string, error) { return a.s.HW.SupportedModes() }

// SetPower switches the power mode and holds it against the automatic profile for
// holdMinutes (0 = no hold). Through the service when it runs (one request), on the
// hardware otherwise.
func (a *API) SetPower(mode string, holdMinutes float64) (string, error) {
	return a.s.Client.Power(mode, holdMinutes)
}

// NextPower switches to the next mode of the configured cycle (the PREDATOR key's
// power-next action) and answers the message the key would show.
func (a *API) NextPower(ctx context.Context) (string, error) { return a.action(ctx, "power-next") }

// PrevPower is NextPower in the other direction.
func (a *API) PrevPower(ctx context.Context) (string, error) { return a.action(ctx, "power-prev") }

// Auto power modes.
const (
	AutoOff    = "off"
	AutoNative = "native"
	AutoSmart  = "smart"
)

// AutoMode is the automatic power mode the service runs: off, native or smart.
func (a *API) AutoMode() (string, error) {
	d, err := a.DaemonState()
	if err != nil {
		return "", err
	}
	if m := d.String("auto_mode"); m != "" {
		return m, nil
	}
	if d.Bool("auto_profile") {
		return AutoSmart, nil
	}
	return AutoOff, nil
}

// SetAutoMode sets the automatic power mode (the service takes over at once for native and
// smart).
func (a *API) SetAutoMode(mode string) error {
	_, err := a.DaemonSet("auto_mode", mode)
	return err
}

// HoldPower keeps the current power mode against the automatic profile for minutes.
func (a *API) HoldPower(minutes float64) error {
	return a.s.IPC.CallInto("hold", ipc.Args{"minutes": minutes}, nil)
}

// ReleaseHold ends a hold: the automatic profile is in charge again.
func (a *API) ReleaseHold() error {
	return a.s.IPC.CallInto("hold", ipc.Args{"minutes": nil}, nil)
}

// GameRules is the program-to-power-mode rules and the mode a running program asks for now.
type GameRules struct {
	Rules   map[string]string `json:"rules"`
	Running *string           `json:"running"`
}

// GameRules lists (action "list"), adds, removes or clears the rules that raise the power
// mode while a program runs. add needs a program and a mode, remove a program.
func (a *API) GameRules(action, name, mode string) (GameRules, error) {
	if action == "add" && (name == "" || mode == "") {
		return GameRules{}, fmt.Errorf("usage: truesense power game add PROGRAM MODE   (e.g. add eldenring.exe turbo)")
	}
	if action == "remove" && name == "" {
		return GameRules{}, fmt.Errorf("usage: truesense power game remove PROGRAM")
	}
	args := ipc.Args{"action": action, "name": nilIfEmpty(name), "mode": nilIfEmpty(mode)}
	var r GameRules
	err := a.s.IPC.CallInto("game_rules", args, &r)
	return r, err
}

func nilIfEmpty(s string) any {
	if s == "" {
		return nil
	}
	return s
}

// PowerManagers is the other power managers that run or are enabled, and whether TrueSense
// manages power (it stopped them).
type PowerManagers struct {
	On       bool            `json:"on"`
	Found    []powermgr.Unit `json:"found"`
	Recorded bool            `json:"recorded"`
}

// PowerManagersStatus lists the other power managers (needs the service).
func (a *API) PowerManagersStatus() (PowerManagers, error) {
	var r PowerManagers
	err := a.s.IPC.CallInto("power_managers", ipc.Args{"action": "status"}, &r)
	return r, err
}

// PowerManagersStopped is what "TrueSense manages power" did.
type PowerManagersStopped struct {
	powermgr.TakeoverReport
	On bool `json:"on"`
}

// StopPowerManagers makes TrueSense manage power: it stops the other managers, sets sane CPU
// defaults and remembers it all so RestorePowerManagers can undo it.
func (a *API) StopPowerManagers() (PowerManagersStopped, error) {
	var r PowerManagersStopped
	err := a.s.IPC.CallInto("power_managers", ipc.Args{"action": "takeover"}, &r)
	return r, err
}

// PowerManagersRestored is what handing power back did.
type PowerManagersRestored struct {
	Restored  []string          `json:"restored"`
	CPUNotSet []string          `json:"cpu_not_set,omitempty"`
	CPU       powermgr.CPUState `json:"cpu"`
	Note      string            `json:"note,omitempty"`
	On        bool              `json:"on"`
}

// RestorePowerManagers starts the other managers again exactly as they were.
func (a *API) RestorePowerManagers() (PowerManagersRestored, error) {
	var r PowerManagersRestored
	err := a.s.IPC.CallInto("power_managers", ipc.Args{"action": "restore"}, &r)
	return r, err
}

// ApplyAutoMode is the command `power auto ACTION`: off, native, smart, on (= smart), toggle
// (off when on, smart when off) or status. It answers the mode in force.
func (a *API) ApplyAutoMode(action string) (string, error) {
	cur, err := a.AutoMode()
	if err != nil {
		return "", err
	}
	want := action
	switch action {
	case "on":
		want = AutoSmart
	case "status", "":
		want = cur
	case "toggle":
		want = AutoSmart
		if cur != AutoOff {
			want = AutoOff
		}
	case AutoOff, AutoNative, AutoSmart:
	default:
		return "", fmt.Errorf("auto_mode: off | native | smart")
	}
	if want != cur {
		if err := a.SetAutoMode(want); err != nil {
			return "", err
		}
	}
	return want, nil
}
