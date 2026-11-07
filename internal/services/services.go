// Package services is `truesense service check` (read-only) and `truesense service repair`
// (idempotent: running it twice changes nothing the second time).
//
// The system part (the daemon and its health timer, the kernel module) needs root; the user
// part (key service, audio service, start-up, update timer) runs as you. Every step says what
// it did, and the repair never touches the fans or any setting: it only gets the machinery
// running again. Every program (systemctl, modprobe, dkms) runs through an injected Runner, so
// the logic is tested against a fake systemd.
package services

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"slices"
	"strings"
	"time"
)

// The units TrueSense installs.
var (
	// SystemUnits run as root and are managed with `sudo truesense service repair`.
	SystemUnits = []string{"truesense.service", "truesense-health.timer"}
	// UserUnits run in the user's session and are repaired without sudo.
	UserUnits = []string{"truesense-keys.service", "truesense-audio.service", "truesense-startup.service", "truesense-update.timer"}
)

// Paths of the daemon's control socket and the kernel module's device.
const (
	SocketPath = "/run/truesense/truesense.sock"
	DevicePath = "/dev/truesense"
)

// Result is what a finished program left behind. Code 127 stands for "could not run it at all"
// (not installed, timed out), with the reason in Stderr, like the shell's convention.
type Result struct {
	Stdout string
	Stderr string
	Code   int
}

// Runner runs one external program to completion. It never returns an error: a program that
// cannot start is a Result with Code 127, because every caller treats both the same way.
type Runner func(ctx context.Context, name string, args ...string) Result

// ExecRunner is the production Runner: it kills the program after timeout (40 s is what the
// repair has always allowed for systemctl and dkms).
func ExecRunner(timeout time.Duration) Runner {
	return func(ctx context.Context, name string, args ...string) Result {
		ctx, cancel := context.WithTimeout(ctx, timeout)
		defer cancel()
		var stdout, stderr strings.Builder
		cmd := exec.CommandContext(ctx, name, args...) // #nosec G204 G702 -- fixed system tools (systemctl, modprobe, dkms)
		cmd.Stdout, cmd.Stderr = &stdout, &stderr
		err := cmd.Run()
		var exit *exec.ExitError
		switch {
		case err == nil:
			return Result{Stdout: stdout.String(), Stderr: stderr.String()}
		case errors.As(err, &exit) && ctx.Err() == nil:
			return Result{Stdout: stdout.String(), Stderr: stderr.String(), Code: exit.ExitCode()}
		default:
			return Result{Stdout: stdout.String(), Stderr: err.Error(), Code: 127}
		}
	}
}

// Pinger asks the daemon whether it is alive. It returns nil only when the daemon answered
// "pong"; the ipc client satisfies it through a one-line adapter.
type Pinger interface {
	Ping(ctx context.Context) error
}

// Manager checks and repairs the services. The zero value is not usable: use New.
type Manager struct {
	Run        Runner
	Exists     func(path string) bool // os.Stat based in production
	Ping       Pinger
	Sleep      func(time.Duration)
	Kernel     string        // release the DKMS rebuild is for (uname -r)
	Wait       time.Duration // how long a restarted daemon may take to answer
	SocketPath string
	DevicePath string
}

// New returns a Manager with production defaults for everything but the daemon connection.
func New(ping Pinger, kernelRelease string) *Manager {
	return &Manager{
		Run: ExecRunner(40 * time.Second), Ping: ping, Sleep: time.Sleep, Kernel: kernelRelease, Wait: 15 * time.Second,
		Exists: func(p string) bool { _, err := os.Stat(p); return err == nil }, SocketPath: SocketPath, DevicePath: DevicePath,
	}
}

// Unit is one row of the check. The JSON names are the 0.3.x ones.
type Unit struct {
	Unit    string `json:"unit"`
	Scope   string `json:"scope"`
	Enabled string `json:"enabled"`
	Active  string `json:"active"`
	OK      bool   `json:"ok"`
	Fix     string `json:"fix"`
}

func systemctl(user bool, args ...string) []string {
	if user {
		return append([]string{"--user"}, args...)
	}
	return args
}

func (m *Manager) systemctl(ctx context.Context, user bool, args ...string) Result {
	return m.Run(ctx, "systemctl", systemctl(user, args...)...)
}

func orUnknown(s string) string {
	if s = strings.TrimSpace(s); s != "" {
		return s
	}
	return "unknown"
}

// UnitState asks systemd about one unit.
func (m *Manager) UnitState(ctx context.Context, unit string, user bool) Unit {
	scope := "system"
	if user {
		scope = "user"
	}
	return Unit{
		Unit: unit, Scope: scope,
		Enabled: orUnknown(m.systemctl(ctx, user, "is-enabled", unit).Stdout),
		Active:  orUnknown(m.systemctl(ctx, user, "is-active", unit).Stdout),
	}
}

// wanted is what a healthy unit looks like: enabled (or static/linked), and for a service
// active; a timer is active when waiting.
func wanted(u Unit) bool {
	return slices.Contains([]string{"enabled", "static", "linked", "enabled-runtime", "alias"}, u.Enabled) && u.Active == "active"
}

// Check is read-only: one row per unit plus the kernel module and the control socket.
func (m *Manager) Check(ctx context.Context) []Unit {
	var rows []Unit
	for _, group := range []struct {
		user  bool
		units []string
	}{{false, SystemUnits}, {true, UserUnits}} {
		for _, name := range group.units {
			u := m.UnitState(ctx, name, group.user)
			u.OK = wanted(u)
			if !u.OK {
				u.Fix = "sudo truesense service repair"
				if group.user {
					u.Fix = "truesense service repair"
				}
			}
			rows = append(rows, u)
		}
	}
	mod := m.Exists(m.DevicePath)
	row := Unit{Unit: "kernel module (/dev/truesense)", Scope: "system", Enabled: "-", Active: "missing", OK: mod}
	if mod {
		row.Active = "present"
	} else {
		row.Fix = "sudo truesense service repair"
	}
	rows = append(rows, row)

	sock := m.Exists(m.SocketPath)
	answered := sock && m.answers(ctx)
	row = Unit{Unit: "control socket", Scope: "system", Enabled: "-", OK: answered}
	switch {
	case answered:
		row.Active = "answers"
	case sock:
		row.Active, row.Fix = "silent", "sudo truesense service repair"
	default:
		row.Active, row.Fix = "missing", "sudo truesense service repair"
	}
	return append(rows, row)
}

func (m *Manager) answers(ctx context.Context) bool {
	if m.Ping == nil {
		return false
	}
	return m.Ping.Ping(ctx) == nil
}

// Step is one thing the repair did and whether it worked. It serialises as the 0.3.x
// [what, ok] pair.
type Step struct {
	What string
	OK   bool
}

// MarshalJSON writes the pair form clients already parse.
func (s Step) MarshalJSON() ([]byte, error) { return json.Marshal([]any{s.What, s.OK}) }

// UnmarshalJSON reads the pair form.
func (s *Step) UnmarshalJSON(b []byte) error {
	var pair [2]json.RawMessage
	if err := json.Unmarshal(b, &pair); err != nil {
		return fmt.Errorf("a repair step is [what, ok]: %w", err)
	}
	if err := json.Unmarshal(pair[0], &s.What); err != nil {
		return err
	}
	return json.Unmarshal(pair[1], &s.OK)
}

func firstLineLimit(r Result, n int) string {
	text := r.Stderr
	if text == "" {
		text = r.Stdout
	}
	text = strings.TrimSpace(text)
	if rs := []rune(text); len(rs) > n {
		text = string(rs[:n])
	}
	return text
}

// RepairSystem gets the system part running again. Run it as root. It records every step and
// never fails: a step that did not work is a Step with OK false and the reason.
func (m *Manager) RepairSystem(ctx context.Context) []Step {
	var steps []Step
	do := func(what string, name string, args ...string) bool {
		r := m.Run(ctx, name, args...)
		ok := r.Code == 0
		if !ok {
			reason := firstLineLimit(r, 100)
			if reason == "" {
				reason = "failed"
			}
			what += " (" + reason + ")"
		}
		steps = append(steps, Step{what, ok})
		return ok
	}
	do("systemd re-reads the unit files", "systemctl", "daemon-reload")
	if !m.Exists(m.DevicePath) {
		m.Run(ctx, "modprobe", "truesense_wmi")
		if !m.Exists(m.DevicePath) {
			do(fmt.Sprintf("rebuilt the kernel module for %s (DKMS)", m.Kernel), "dkms", "autoinstall", "-k", m.Kernel)
			m.Run(ctx, "modprobe", "truesense_wmi")
		}
		if present := m.Exists(m.DevicePath); present {
			steps = append(steps, Step{"kernel module loaded", true})
		} else {
			steps = append(steps, Step{"kernel module still missing: is this a supported laptop? (truesense doctor)", false})
		}
	}
	for _, u := range SystemUnits {
		if st := m.UnitState(ctx, u, false); !slices.Contains([]string{"enabled", "static", "linked", "alias"}, st.Enabled) {
			do("enabled "+u, "systemctl", "enable", u)
		}
	}
	m.Run(ctx, "systemctl", "reset-failed", "truesense.service")
	svc := m.UnitState(ctx, "truesense.service", false)
	timer := m.UnitState(ctx, "truesense-health.timer", false)
	if timer.Active != "active" {
		do("started the health timer", "systemctl", "start", "truesense-health.timer")
	}
	if svc.Active == "active" && m.Exists(m.SocketPath) && m.answers(ctx) {
		return append(steps, Step{"truesense.service is running and answers: left alone", true})
	}
	do("restarted truesense.service", "systemctl", "restart", "truesense.service")
	for i := 0; i < int(m.Wait/(500*time.Millisecond)); i++ {
		if m.Exists(m.SocketPath) && m.answers(ctx) {
			break
		}
		m.Sleep(500 * time.Millisecond)
	}
	if m.Exists(m.SocketPath) && m.answers(ctx) {
		return append(steps, Step{"the service answers on its control socket", true})
	}
	return append(steps, Step{"the service does not answer: see  journalctl -u truesense -b", false})
}

// RepairUser enables what is not enabled and starts what is not running, as the user. A
// running unit is left alone.
func (m *Manager) RepairUser(ctx context.Context) []Step {
	r := m.systemctl(ctx, true, "daemon-reload")
	steps := []Step{{"user systemd re-reads the unit files", r.Code == 0}}
	for _, u := range UserUnits {
		if wanted(m.UnitState(ctx, u, true)) {
			continue
		}
		r := m.systemctl(ctx, true, "enable", "--now", u)
		ok := r.Code == 0 || m.UnitState(ctx, u, true).Active == "active"
		switch {
		case ok:
			steps = append(steps, Step{"enabled and started " + u, true})
		default:
			reason := firstLineLimit(r, 90)
			if reason == "" {
				reason = "is it installed?"
			}
			steps = append(steps, Step{fmt.Sprintf("could not start %s (%s)", u, reason), false})
		}
	}
	if len(steps) == 1 {
		steps = append(steps, Step{"every user service is already running", true})
	}
	return steps
}

// Render is the check as text.
func Render(rows []Unit) string {
	lines := make([]string, len(rows))
	for i, r := range rows {
		mark := "!! "
		if r.OK {
			mark = "ok "
		}
		line := fmt.Sprintf("  [%s] %-34s %-6s %-9s %s", mark, r.Unit, r.Scope, r.Enabled, r.Active)
		if r.Fix != "" {
			line += "   -> " + r.Fix
		}
		lines[i] = line
	}
	return strings.Join(lines, "\n")
}
