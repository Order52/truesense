// Package client is how the command line, the terminal UI, the window and the user services
// ask for things: through the daemon when it runs (it owns the privileged and persistent
// state), directly on the hardware when it does not (only for what is safe without it).
package client

import (
	"fmt"

	"github.com/Order52/truesense/internal/firmware"
	"github.com/Order52/truesense/internal/hw"
	"github.com/Order52/truesense/internal/ipc"
)

// Client reaches the daemon over IPC and falls back to the hardware.
type Client struct {
	IPC *ipc.Client
	HW  *hw.HW
}

// New returns a Client for the socket at the default place and the given hardware.
func New(h *hw.HW) *Client { return &Client{IPC: &ipc.Client{}, HW: h} }

// DaemonAvailable reports whether the daemon's socket exists.
func (c *Client) DaemonAvailable() bool { return c.IPC.Available() }

// FanRequest is a fan control request. CPU and GPU are only used by "manual"; a duty of 0
// stops that fan and needs Confirm and the service.
type FanRequest struct {
	Mode    string // smart | auto | max | manual | off
	CPU     *int
	GPU     *int
	Confirm bool
}

// Fan sets the fans. Modes that stop a fan go only through the service (its temperature
// watchdog gives the fans back when it gets warm), and only when confirmed; the rest fall
// back to the hardware when there is no service, except "smart", which is the service's curve.
func (c *Client) Fan(r FanRequest) error {
	switch r.Mode {
	case "off":
		if !c.DaemonAvailable() {
			return firmware.Errorf("turning the fans off needs the TrueSense service (it brings them back when it gets warm)")
		}
		return c.IPC.CallInto("fan", ipc.Args{"mode": "off", "confirm": r.Confirm}, nil)
	case "manual":
		if r.CPU == nil {
			return firmware.Errorf("manual fan duty is 20..100 %% per fan, or 0 to stop that fan")
		}
		cpu, gpu := *r.CPU, *r.CPU
		if r.GPU != nil {
			gpu = *r.GPU
		}
		if !hw.ValidDuty(cpu) || !hw.ValidDuty(gpu) {
			return firmware.Errorf("manual fan duty is 20..100 %% per fan, or 0 to stop that fan")
		}
		if cpu == 0 || gpu == 0 {
			if !c.DaemonAvailable() {
				return firmware.Errorf("stopping a fan needs the TrueSense service (it brings the fans back when it gets warm)")
			}
			return c.IPC.CallInto("fan", ipc.Args{"mode": "manual", "cpu": cpu, "gpu": gpu, "confirm": r.Confirm}, nil)
		}
		if c.DaemonAvailable() {
			return c.IPC.CallInto("fan", ipc.Args{"mode": "manual", "cpu": cpu, "gpu": gpu}, nil)
		}
		return c.HW.FanManual(cpu, gpu)
	case "auto", "max", "smart":
		if c.DaemonAvailable() {
			return c.IPC.CallInto("fan", ipc.Args{"mode": r.Mode}, nil)
		}
		switch r.Mode {
		case "auto":
			return c.HW.FanAuto()
		case "max":
			return c.HW.FanMax()
		}
		return firmware.Errorf("the smart curve needs the TrueSense daemon")
	}
	return firmware.Errorf("fan mode: smart | auto | max | manual | off (got %q)", r.Mode)
}

// Power sets the power mode and, with holdMinutes > 0, holds it against the automatic profile.
// The daemon does both in one request (no settle delay); without it the hardware is written
// directly and the hold is skipped (there is nothing to hold it against).
func (c *Client) Power(mode string, holdMinutes float64) (string, error) {
	if c.DaemonAvailable() {
		args := ipc.Args{"mode": mode}
		if holdMinutes > 0 {
			args["hold_minutes"] = holdMinutes
		}
		var res any
		if err := c.IPC.CallInto("power", args, &res); err != nil {
			return "", err
		}
		return mode, nil
	}
	got, err := c.HW.SetMode(mode)
	if err != nil {
		return "", fmt.Errorf("power mode: %w", err)
	}
	return got, nil
}
