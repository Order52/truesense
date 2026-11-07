package api

import (
	"fmt"

	"github.com/Order52/truesense/internal/ipc"
)

// DaemonState is the service's answer to "state": the live readings, the settings that
// survive reboots and how long a power-mode pick is still held.
type DaemonState struct {
	Live      map[string]any `json:"live"`
	Settings  map[string]any `json:"settings"`
	HoldLeftS int            `json:"hold_left_s"`
}

// Bool returns a setting as a boolean (false when absent or of another type).
func (d DaemonState) Bool(key string) bool {
	v, _ := d.Settings[key].(bool)
	return v
}

// String returns a setting as text ("" when absent or of another type).
func (d DaemonState) String(key string) string {
	v, _ := d.Settings[key].(string)
	return v
}

// DaemonAvailable reports whether the service's socket exists.
func (a *API) DaemonAvailable() bool { return a.s.IPC.Available() }

// DaemonState asks the service for its state. Without a service the error says how to start it.
func (a *API) DaemonState() (DaemonState, error) {
	var st DaemonState
	if err := a.s.IPC.CallInto("state", nil, &st); err != nil {
		return DaemonState{}, err
	}
	return st, nil
}

// DaemonSet changes one of the service's switches (smart_fan, auto_profile, operating_follow,
// auto_mode) and returns the settings the service now holds.
func (a *API) DaemonSet(key string, value any) (map[string]any, error) {
	var settings map[string]any
	if err := a.s.IPC.CallInto("set", ipc.Args{"key": key, "value": value}, &settings); err != nil {
		return nil, err
	}
	return settings, nil
}

// daemonSetBool is DaemonSet for a switch, answering the value the service reports back.
func (a *API) daemonSetBool(key string, on bool) (bool, error) {
	settings, err := a.DaemonSet(key, on)
	if err != nil {
		return false, err
	}
	v, ok := settings[key].(bool)
	if !ok {
		return false, fmt.Errorf("the service did not report %s", key)
	}
	return v, nil
}

// daemonBool reads one boolean switch from the service's settings.
func (a *API) daemonBool(key string) (bool, error) {
	st, err := a.DaemonState()
	if err != nil {
		return false, err
	}
	return st.Bool(key), nil
}
