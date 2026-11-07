// Package api is the application layer: one method per use case ("set the power mode",
// "save a fan curve", "run the health check"), shared by the command line, the terminal UI
// adapter and the window. It is the single place that knows what an operation does and which
// safety rules apply; the callers only decide how to ask (flags, keys, buttons) and how to
// show the answer.
//
// The methods return typed values whose JSON names are the ones the 0.3.x command line
// printed with --json, and errors worded as the 0.3.x tool worded them. Nothing here prints,
// parses a command line or exits: a caller that wants to show text formats the result.
package api

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"syscall"
	"time"

	"github.com/Order52/truesense/internal/actions"
	"github.com/Order52/truesense/internal/assembly"
	"github.com/Order52/truesense/internal/config"
	"github.com/Order52/truesense/internal/doctor"
	"github.com/Order52/truesense/internal/fnkeys"
	"github.com/Order52/truesense/internal/keybinds"
	"github.com/Order52/truesense/internal/keymap"
	"github.com/Order52/truesense/internal/models"
	"github.com/Order52/truesense/internal/profiles"
	"github.com/Order52/truesense/internal/services"
	"github.com/Order52/truesense/internal/startup"
	"github.com/Order52/truesense/internal/update"
)

// ActionRunner runs one PREDATOR-key action ("power-next", "rear:toggle", ...). The key
// actions are a vocabulary of their own (internal/actions); the use cases that are also
// actions (next power mode, light on/off) are the same code on purpose, so a key press and a
// command can never disagree. It is an interface here because the action runner is built by
// the application from the same Services.
type ActionRunner interface {
	Run(ctx context.Context, spec string) (string, error)
}

// ExecFunc runs an external program with the given output streams and returns its exit code.
// A program that cannot be started is an error.
type ExecFunc func(ctx context.Context, stdout, stderr io.Writer, argv ...string) (int, error)

// API is every use case, over the components of one Services value.
type API struct {
	s  *assembly.Services
	km *keymap.Map

	runner   *actions.Runner
	profiles *profiles.Store
	binds    *keybinds.Store
	login    *startup.Startup

	rapl raplMeter

	// Actions runs the key actions that commands share (next power mode, light toggles).
	// New wires the real runner; a nil Actions makes those commands fail with a clear message.
	Actions ActionRunner
	// SystemConfig and UserConfig are the configuration files that are read fresh for every
	// use and written by the commands that change a setting.
	SystemConfig, UserConfig string
	// Updater finds and installs releases; nil builds one for the repository the system
	// configuration names.
	Updater *update.Updater
	// Exec runs external programs for the service commands (systemctl, journalctl, sudo).
	Exec ExecFunc
	// Now is the clock (the live readings measure power between two instants).
	Now func() time.Time
	// Sleep pauses (the self-test waits for the firmware to settle); tests replace it.
	Sleep func(time.Duration)
	// Executable is the path of this program as the installed command (found on PATH first),
	// which `service repair` hands to sudo.
	Executable func() (string, error)
	// Self is the path of the running binary: what opens the window.
	Self func() (string, error)
	// Spawn starts a program detached (its own session, no input or output) and does not wait.
	Spawn func(argv ...string) error
	// EUID is the effective user id of the caller (root is 0).
	EUID int
	// ProbeOptions say where the model probe looks (the zero value is the real machine).
	ProbeOptions models.ProbeOptions
	// DoctorEnv builds the health check's view of the machine; nil is the real machine.
	DoctorEnv func() *doctor.Env
	// ServiceManager checks and repairs the units; nil is the real systemd.
	ServiceManager func() *services.Manager
	// FnKeyNode finds the keyboard's Fn-key interface for the health check; nil leaves it
	// unreported.
	FnKeyNode func() string
	// SysRoot and DevRoot are the prefixes of /sys and /dev ("" = the real ones) where the key
	// service looks for the keyboard's Fn-key interface; tests point them at an empty tree.
	SysRoot, DevRoot string
}

// New returns the API over s, reading the real configuration files and running real programs.
func New(s *assembly.Services) *API {
	a := &API{
		s: s, km: keymap.Verified(), SystemConfig: config.SystemPath, UserConfig: config.UserPath(),
		Exec: execProgram, Now: time.Now, Sleep: time.Sleep, Executable: executablePath, Self: os.Executable, Spawn: spawnDetached, EUID: os.Geteuid(),
	}
	a.FnKeyNode = a.fnKeyNode
	a.wire()
	return a
}

// UseConfigFiles points the API at other configuration files (the system file, then the
// user's) and rebuilds what holds those paths. Tests use it to keep clear of the real files.
func (a *API) UseConfigFiles(system, user string) {
	a.SystemConfig, a.UserConfig = system, user
	a.wire()
}

func (a *API) fnKeyNode() string {
	n, err := fnkeys.FindNode(a.SysRoot, a.DevRoot)
	if err != nil {
		return ""
	}
	return n
}

// Services returns the components the API was built from, for callers (the terminal UI's
// adapter) that need one directly.
func (a *API) Services() *assembly.Services { return a.s }

// LoadConfig reads the configuration now: the commands that change a setting write the
// user's file and the next read sees it, exactly as the 0.3.x tool re-read it every time.
func (a *API) LoadConfig() (*config.Config, error) {
	return config.Load(a.SystemConfig, a.UserConfig)
}

// cfg is the configuration, or the defaults when a file is broken (a command must not fail
// because of a typo in a setting it does not use).
func (a *API) cfg() *config.Config {
	c, err := a.LoadConfig()
	if err != nil {
		return config.Default()
	}
	return c
}

func (a *API) action(ctx context.Context, spec string) (string, error) {
	if a.Actions == nil {
		return "", fmt.Errorf("key actions are not available in this build")
	}
	return a.Actions.Run(ctx, spec)
}

// executablePath is this program's path, resolved through PATH first (the installed name).
func executablePath() (string, error) {
	if p, err := exec.LookPath("truesense"); err == nil {
		return p, nil
	}
	return os.Executable()
}

// execProgram is the production ExecFunc. The child shares the terminal's input, so sudo can
// ask for a password.
func execProgram(ctx context.Context, stdout, stderr io.Writer, argv ...string) (int, error) {
	if len(argv) == 0 {
		return 0, fmt.Errorf("no program to run")
	}
	cmd := exec.CommandContext(ctx, argv[0], argv[1:]...)
	cmd.Stdin, cmd.Stdout, cmd.Stderr = os.Stdin, stdout, stderr
	err := cmd.Run()
	var ee *exec.ExitError
	if errors.As(err, &ee) {
		return ee.ExitCode(), nil
	}
	return 0, err
}

// spawnDetached is the production SpawnFunc: the child leads its own session, so closing the
// terminal that started it does not end it, and the caller does not wait for it.
func spawnDetached(argv ...string) error {
	if len(argv) == 0 {
		return fmt.Errorf("no program to run")
	}
	cmd := exec.CommandContext(context.Background(), argv[0], argv[1:]...) // not bound to a caller: the program outlives it
	cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
	if err := cmd.Start(); err != nil {
		return err
	}
	go func() { _ = cmd.Wait() }() // reap it when it ends, whenever that is
	return nil
}

// OpenWindow starts `truesense gui` detached.
func (a *API) OpenWindow() error {
	self, err := a.Self()
	if err != nil {
		return err
	}
	return a.Spawn(self, "gui")
}
