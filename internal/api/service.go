package api

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"strings"

	"github.com/Order52/truesense/internal/assembly"
	"github.com/Order52/truesense/internal/services"
)

const daemonUnit = "truesense.service"

// ServiceStatus shows systemd's status of the daemon on the given streams and answers the
// exit code of systemctl.
func (a *API) ServiceStatus(ctx context.Context, out, errw io.Writer) (int, error) {
	return a.Exec(ctx, out, errw, "systemctl", "--no-pager", "status", daemonUnit)
}

// RestartService restarts the daemon through sudo.
func (a *API) RestartService(ctx context.Context, out, errw io.Writer) (int, error) {
	return a.Exec(ctx, out, errw, "sudo", "systemctl", "restart", daemonUnit)
}

// ServiceLogs shows the daemon's last 60 log lines of this boot.
func (a *API) ServiceLogs(ctx context.Context, out, errw io.Writer) (int, error) {
	return a.Exec(ctx, out, errw, "journalctl", "-u", daemonUnit, "-b", "--no-pager", "-n", "60")
}

// serviceManager is the unit check and repair over the real systemd and the control socket.
func (a *API) serviceManager() *services.Manager {
	if a.ServiceManager != nil {
		return a.ServiceManager()
	}
	return services.New(assembly.NewIPCDaemon(a.s.IPC), kernelRelease())
}

// CheckServices reads the state of every unit, the kernel module and the control socket
// (read-only).
func (a *API) CheckServices(ctx context.Context) []services.Unit {
	return a.serviceManager().Check(ctx)
}

// RenderServices is the check as text.
func RenderServices(rows []services.Unit) string { return services.Render(rows) }

// ServicesOK reports whether every row of a check is fine.
func ServicesOK(rows []services.Unit) bool {
	for _, r := range rows {
		if !r.OK {
			return false
		}
	}
	return true
}

// StepsOK reports whether every step of a repair worked.
func StepsOK(steps []services.Step) bool {
	for _, s := range steps {
		if !s.OK {
			return false
		}
	}
	return true
}

// RepairSystemServices is the part of `service repair` that needs root: the daemon, its
// health timer and the kernel module. It touches no setting.
func (a *API) RepairSystemServices(ctx context.Context) ([]services.Step, error) {
	if a.EUID != 0 {
		return nil, fmt.Errorf("the system part needs root: sudo truesense service repair")
	}
	return a.serviceManager().RepairSystem(ctx), nil
}

// RepairServices gets the daemon, its health timer, the module and your user services running
// again. Your own part runs as you; the system part runs through sudo (as this program with
// `service repair --system`), or directly when you are root. It is safe to repeat and
// touches no setting.
func (a *API) RepairServices(ctx context.Context) ([]services.Step, error) {
	var steps []services.Step
	if a.EUID == 0 {
		steps = append(steps, services.Step{What: "your user services were not touched (you ran this as root): run it again without sudo for them", OK: true})
		return append(steps, a.serviceManager().RepairSystem(ctx)...), nil
	}
	steps = append(steps, a.serviceManager().RepairUser(ctx)...)
	exe, err := a.Executable()
	if err != nil {
		return steps, err
	}
	var out strings.Builder
	if _, err := a.Exec(ctx, &out, io.Discard, "sudo", exe, "service", "repair", "--system"); err == nil {
		lines := strings.Split(strings.TrimSpace(out.String()), "\n")
		var system []services.Step
		if json.Unmarshal([]byte(lines[len(lines)-1]), &system) == nil && len(system) > 0 {
			return append(steps, system...), nil
		}
	}
	return append(steps, services.Step{What: "the system part did not run (sudo was refused or failed)", OK: false}), nil
}
