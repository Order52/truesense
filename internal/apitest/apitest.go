// Package apitest builds a complete, private Services value for the tests of the application
// layer and its clients: the firmware simulator, a simulated keyboard, temporary directories for
// every store, a sound system that is not there, and (on request) a fake control socket. Nothing
// it builds reaches the real machine, the real home directory or the real daemon.
package apitest

import (
	"context"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/Order52/truesense/internal/api"
	"github.com/Order52/truesense/internal/assembly"
	"github.com/Order52/truesense/internal/audio"
	"github.com/Order52/truesense/internal/client"
	"github.com/Order52/truesense/internal/config"
	"github.com/Order52/truesense/internal/curves"
	"github.com/Order52/truesense/internal/designs"
	"github.com/Order52/truesense/internal/doctor"
	"github.com/Order52/truesense/internal/firmware"
	"github.com/Order52/truesense/internal/gpuundervolt"
	"github.com/Order52/truesense/internal/hw"
	"github.com/Order52/truesense/internal/ipc"
	"github.com/Order52/truesense/internal/keyboard"
	"github.com/Order52/truesense/internal/keyprofiles"
	"github.com/Order52/truesense/internal/looks"
	"github.com/Order52/truesense/internal/models"
	"github.com/Order52/truesense/internal/osd"
	"github.com/Order52/truesense/internal/power"
	"github.com/Order52/truesense/internal/powermgr"
	"github.com/Order52/truesense/internal/sensors"
	"github.com/Order52/truesense/internal/services"
	"github.com/Order52/truesense/internal/smartprofile"
	"github.com/Order52/truesense/internal/touchpad"
	"github.com/Order52/truesense/internal/undervolt"
	"github.com/Order52/truesense/internal/userstate"
)

// Fixture is one private machine.
type Fixture struct {
	T        testing.TB
	Dir      string
	S        *assembly.Services
	Sim      *firmware.Simulator
	Keyboard *keyboard.SimTransport

	mu       sync.Mutex
	exec     [][]string
	spawned  [][]string
	notified []string
	daemon   *Daemon
}

// New builds the machine. The audio system answers "not installed" to every program, so the
// sound commands report no card instead of touching the real sound server.
func New(t testing.TB) *Fixture {
	t.Helper()
	dir := t.TempDir()
	// Stores that live in the home directory (profiles, key bindings) must find a private one,
	// and the shipped profiles must come from an empty place: nothing of the real user is read
	// or written.
	t.Setenv("HOME", filepath.Join(dir, "home"))
	t.Setenv("XDG_CONFIG_HOME", "")
	t.Setenv("TRUESENSE_PROFILES_DIR", filepath.Join(dir, "shipped"))
	sub := func(name string) string { return filepath.Join(dir, name) }
	f := &Fixture{T: t, Dir: dir, Sim: firmware.NewSimulator()}

	user := &userstate.Store{Path: sub("user.json")}
	h := hw.New(f.Sim, user)
	h.Tries, h.Delay, h.SleepFor, h.SysRoot = 2, 0, func(time.Duration) {}, sub("root")
	kb, kbt := keyboard.NewSim(user)
	f.Keyboard = kbt
	h.KBT = kb.Timeout()

	notifier := &osd.Notifier{Style: "native", Run: func(_ context.Context, _ []string, _ string, name string, args ...string) (string, int) {
		f.mu.Lock()
		f.notified = append(f.notified, strings.Join(append([]string{name}, args...), " "))
		f.mu.Unlock()
		return "", 0
	}, RuntimeDir: sub("run-user"), Lookup: func(string) (int, string, error) { return os.Getuid(), dir, nil }}

	sound := audio.New(user, osdAdapter{notifier})
	sound.Run = func(context.Context, string, ...string) audio.Result {
		return audio.Result{Code: 127, Stderr: "not installed"}
	}
	sound.Cfg = config.Default
	sound.ConfDir, sound.ProcCards, sound.Plugin, sound.DTSConfigPath = sub("pipewire"), sub("cards"), "", sub("acerultrax.toml")
	sound.DTSBinary = func() string { return "" }
	sound.Lock = func() (func(), error) { return func() {}, nil }
	sound.Sleep = func(context.Context, time.Duration) {}
	sound.Log = slog.New(slog.NewTextHandler(io.Discard, nil))

	ipcClient := &ipc.Client{Path: sub("no-daemon.sock"), Timeout: 2 * time.Second}
	cl := client.New(h)
	cl.IPC = ipcClient

	f.S = &assembly.Services{
		Cfg: config.Default(), User: user, Dev: f.Sim, HW: h, KB: kb,
		Looks:    &looks.Store{Dir: sub("looks"), BootFile: sub("boot-look.json"), State: user, KB: kb, Rear: h},
		KeyProf:  keyprofiles.New(sub("lighting"), kb),
		Designs:  designs.New(sub("designs")),
		OSD:      notifier,
		Audio:    sound,
		IPC:      ipcClient,
		Client:   cl,
		Curves:   &curves.Store{Dir: sub("curves")},
		Smart:    &smartprofile.Store{Dir: sub("smart")},
		UVStore:  &undervolt.Store{Dir: sub("undervolt")},
		GPUStore: &gpuundervolt.Store{Dir: sub("gpu-undervolt")},
		PM:       &powermgr.Manager{Run: func(context.Context, string, ...string) (int, string) { return 1, "" }, CPUDir: sub("cpu"), RecordPath: sub("pm.json")},
		Power:    &power.Controller{SysRoot: sub("root")},
		UV:       undervolt.New(sub("msr")),
		GPUUV:    gpuundervolt.New(func() (gpuundervolt.NVML, error) { return nil, os.ErrNotExist }),
		Touchpad: &touchpad.Switch{DevicesFile: sub("devices"), SysInput: sub("input")},
		Sampler:  sensors.New(),
	}
	f.S.Sampler.Root = sub("root")
	return f
}

type osdAdapter struct{ n *osd.Notifier }

func (a osdAdapter) Show(text, icon string) { a.n.Show(text, icon) }
func (a osdAdapter) Quiet() func()          { return func() {} }

// API returns the application layer over the machine, reading and writing configuration in the
// fixture's own directory, running no real program and starting no real process. Every command
// it would have run is recorded (Exec, Spawned).
func (f *Fixture) API() *api.API {
	a := api.New(f.S)
	a.UseConfigFiles(filepath.Join(f.Dir, "system.toml"), filepath.Join(f.Dir, "config", "config.toml"))
	f.S.Audio.Cfg = func() *config.Config { // the sound system reads the fixture's configuration, like the real one reads the real files
		if c, err := a.LoadConfig(); err == nil {
			return c
		}
		return config.Default()
	}
	a.ProbeOptions = models.ProbeOptions{DMIRoot: filepath.Join(f.Dir, "root", "dmi"), SysRoot: filepath.Join(f.Dir, "root", "sys"), DevicePath: filepath.Join(f.Dir, "root", "dev-truesense"), Kernel: "test-kernel", Version: "test"}
	a.SysRoot, a.DevRoot = filepath.Join(f.Dir, "root"), filepath.Join(f.Dir, "root")
	a.EUID = 1000
	a.Exec = func(_ context.Context, stdout, _ io.Writer, argv ...string) (int, error) {
		f.mu.Lock()
		f.exec = append(f.exec, argv)
		f.mu.Unlock()
		_, _ = io.WriteString(stdout, "ran "+strings.Join(argv, " ")+"\n")
		return 0, nil
	}
	a.Spawn = func(argv ...string) error {
		f.mu.Lock()
		f.spawned = append(f.spawned, argv)
		f.mu.Unlock()
		return nil
	}
	a.DoctorEnv = func() *doctor.Env { return f.doctorEnv(a) }
	a.ServiceManager = func() *services.Manager { return f.serviceManager() }
	a.Self = func() (string, error) { return "/opt/truesense", nil }
	a.Sleep = func(time.Duration) {}
	a.Now = func() time.Time { return time.Unix(1_700_000_000, 0) }
	a.Executable = func() (string, error) { return "/usr/bin/truesense", nil }
	return a
}

// Exec lists the external programs the API would have run, as argument lists.
func (f *Fixture) Exec() [][]string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([][]string(nil), f.exec...)
}

// Spawned lists the detached programs the API would have started.
func (f *Fixture) Spawned() [][]string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([][]string(nil), f.spawned...)
}

// Notified lists the desktop notifications that would have been shown.
func (f *Fixture) Notified() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]string(nil), f.notified...)
}

// noPrograms is a Runner that finds no program: every command is "not installed".
func noPrograms(context.Context, string, ...string) services.Result {
	return services.Result{Code: 127, Stderr: "not installed"}
}

// doctorEnv is the health check over the fixture: a temporary root instead of the real
// filesystem, no programs, and the fixture's firmware and audio.
func (f *Fixture) doctorEnv(a *api.API) *doctor.Env {
	e := doctor.NewEnv(f.S.HW, assembly.NewIPCDaemon(f.S.IPC), f.S.Audio)
	e.Root, e.Run, e.Kernel, e.Version = filepath.Join(f.Dir, "root"), noPrograms, "test-kernel", "test"
	e.Getenv = func(string) string { return "" }
	e.LookPath = func(string) (string, error) { return "", os.ErrNotExist }
	e.Access = func(string) bool { return false }
	e.Config = a.LoadConfig
	e.CeilingLow = func() []string { return nil }
	return e
}

// serviceManager is the unit check over a systemd that has none of the units.
func (f *Fixture) serviceManager() *services.Manager {
	m := services.New(assembly.NewIPCDaemon(f.S.IPC), "test-kernel")
	m.Run, m.Exists, m.Sleep, m.Wait = noPrograms, func(string) bool { return false }, func(time.Duration) {}, 0
	m.SocketPath, m.DevicePath = filepath.Join(f.Dir, "none.sock"), filepath.Join(f.Dir, "none-device")
	return m
}
