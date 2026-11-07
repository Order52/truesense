package assembly

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/Order52/truesense/internal/doctor"
	"github.com/Order52/truesense/internal/ipc"
	"github.com/Order52/truesense/internal/selftest"
)

// IPCDaemon adapts the control socket to what the health check, the self-test and the service
// check ask of the daemon. Those packages are deliberately independent of ipc.
type IPCDaemon struct{ c *ipc.Client }

// NewIPCDaemon wraps a control-socket client.
func NewIPCDaemon(c *ipc.Client) IPCDaemon { return IPCDaemon{c} }

// State reads the daemon's live readings and settings.
func (d IPCDaemon) State(_ context.Context) (doctor.DaemonState, error) {
	var st doctor.DaemonState
	err := d.c.CallInto("state", nil, &st)
	return st, err
}

// Call sends a command and discards the answer.
func (d IPCDaemon) Call(_ context.Context, command string, args map[string]any) error {
	return d.c.CallInto(command, ipc.Args(args), nil)
}

// Ping succeeds when the daemon answers "pong".
func (d IPCDaemon) Ping(_ context.Context) error {
	var pong string
	if err := d.c.CallInto("ping", nil, &pong); err != nil {
		return err
	}
	if pong != "pong" {
		return fmt.Errorf("the daemon answered %q to a ping", pong)
	}
	return nil
}

// Automation reads the smart fan and automatic profile switches.
func (d IPCDaemon) Automation(ctx context.Context) (smartFan, autoProfile bool, err error) {
	st, err := d.State(ctx)
	return st.Settings.SmartFan, st.Settings.AutoProfile, err
}

// SetAutomation changes both switches.
func (d IPCDaemon) SetAutomation(_ context.Context, smartFan, autoProfile bool) error {
	if err := d.c.CallInto("set", ipc.Args{"key": "smart_fan", "value": smartFan}, nil); err != nil {
		return err
	}
	return d.c.CallInto("set", ipc.Args{"key": "auto_profile", "value": autoProfile}, nil)
}

// CPUInfo reads the CPU power limits and whether the user set them.
func (d IPCDaemon) CPUInfo(_ context.Context) (selftest.CPUInfo, error) {
	var raw struct {
		PL1W float64 `json:"pl1_w"`
		PL2W float64 `json:"pl2_w"`
		User any     `json:"user"`
	}
	if err := d.c.CallInto("cpu_info", nil, &raw); err != nil {
		return selftest.CPUInfo{}, err
	}
	b, _ := json.Marshal(raw.User)
	return selftest.CPUInfo{PL1W: int(raw.PL1W), PL2W: int(raw.PL2W), User: string(b) != "null"}, nil
}
