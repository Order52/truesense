package doctor

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/Order52/truesense/internal/config"
	"github.com/Order52/truesense/internal/hw"
	"github.com/Order52/truesense/internal/services"
)

const kernel = "7.3.0-test"

// fakeRun answers programs from a table keyed by the whole command line ("systemctl is-active
// truesense.service"), and records every call. Unknown commands behave like a missing program.
type fakeRun struct {
	mu    sync.Mutex
	table map[string]string
	calls []string
}

func (f *fakeRun) run(_ context.Context, name string, args ...string) services.Result {
	line := strings.Join(append([]string{name}, args...), " ")
	f.mu.Lock()
	f.calls = append(f.calls, line)
	out, found := f.table[line]
	f.mu.Unlock()
	if !found {
		return services.Result{Stderr: name + ": not installed", Code: 127}
	}
	return services.Result{Stdout: out + "\n"}
}

func (f *fakeRun) called(prefix string) bool {
	f.mu.Lock()
	defer f.mu.Unlock()
	for _, c := range f.calls {
		if strings.HasPrefix(c, prefix) {
			return true
		}
	}
	return false
}

type fakeFirmware struct {
	caps    map[string]bool
	capsErr error
	fan     hw.FanState
	fanErr  error
}

func (f *fakeFirmware) Capabilities() (map[string]bool, error) { return f.caps, f.capsErr }
func (f *fakeFirmware) GetFanState() (hw.FanState, error)      { return f.fan, f.fanErr }

type fakeDaemon struct {
	state     DaemonState
	stateErr  error
	callErr   map[string]error
	callsSeen []string
}

func (d *fakeDaemon) State(context.Context) (DaemonState, error) { return d.state, d.stateErr }
func (d *fakeDaemon) Call(_ context.Context, cmd string, args map[string]any) error {
	d.callsSeen = append(d.callsSeen, cmd)
	if args["action"] != "get" {
		return errors.New("the version probe must only read")
	}
	return d.callErr[cmd]
}

type fakeAudio struct {
	rnnoise, dts    bool
	headphones, mic bool
	jackErr         error
	known, ok       bool
	problem         string
	soundErr        error
	bare            []string
	bareErr         error
}

func (a *fakeAudio) HasRNNoise() bool { return a.rnnoise }
func (a *fakeAudio) HasDTS() bool     { return a.dts }
func (a *fakeAudio) JackStatus(context.Context) (bool, bool, error) {
	return a.headphones, a.mic, a.jackErr
}
func (a *fakeAudio) SoundCheck(context.Context) (bool, bool, string, error) {
	return a.known, a.ok, a.problem, a.soundErr
}
func (a *fakeAudio) PlainHeadsets(context.Context) ([]string, error) { return a.bare, a.bareErr }

// rig is a healthy machine in a temporary root; tests break one thing at a time.
type rig struct {
	root string
	run  *fakeRun
	fw   *fakeFirmware
	d    *fakeDaemon
	au   *fakeAudio
	env  *Env
	now  time.Time
}

func write(t testing.TB, root, rel, content string) {
	t.Helper()
	p := filepath.Join(root, rel)
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func mkdir(t testing.TB, root, rel string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Join(root, rel), 0o755); err != nil {
		t.Fatal(err)
	}
}

// installDKMS registers pkg for the kernels the way DKMS does: one kernel-<release>-<arch>
// link per kernel, pointing at a directory.
func installDKMS(t testing.TB, root, pkg string, kernels ...string) {
	t.Helper()
	for _, k := range kernels {
		mkdir(t, root, "var/lib/dkms/"+pkg+"/0.3.99/"+k+"/x86_64")
		if err := os.Symlink("0.3.99/"+k+"/x86_64", filepath.Join(root, "var/lib/dkms", pkg, "kernel-"+k+"-x86_64")); err != nil {
			t.Fatal(err)
		}
	}
}

func installKernels(t testing.TB, root string, kernels ...string) {
	t.Helper()
	for _, k := range kernels {
		mkdir(t, root, "lib/modules/"+k+"/build")
	}
}

func newRig(t *testing.T) *rig {
	t.Helper()
	root := t.TempDir()
	now := time.Unix(1_800_000_000, 0)
	r := &rig{root: root, now: now}
	for name, v := range map[string]string{"sys_vendor": "Acer", "product_name": "Predator PH16-71", "bios_version": "V1.18", "bios_date": "06/09/2026"} {
		write(t, root, "sys/class/dmi/id/"+name, v+"\n")
	}
	installKernels(t, root, kernel)
	installDKMS(t, root, "truesense-wmi", kernel)
	write(t, root, "sys/module/truesense_wmi/version", "0.3.17\n")
	write(t, root, "etc/modprobe.d/truesense.conf", "blacklist acer_wmi\n")
	write(t, root, "dev/truesense", "")
	write(t, root, "etc/udev/rules.d/70-truesense.rules", "# rule\n")
	write(t, root, "sys/devices/system/cpu/intel_pstate/no_turbo", "0\n")
	write(t, root, "run/truesense/state", "{}")
	if err := os.Chtimes(filepath.Join(root, "run/truesense/state"), now.Add(-time.Second), now.Add(-time.Second)); err != nil {
		t.Fatal(err)
	}
	r.run = &fakeRun{table: map[string]string{
		"modinfo -F version truesense_wmi":                     "0.3.17",
		"modinfo -F signer truesense_wmi":                      "",
		"mokutil --sb-state":                                   "SecureBoot disabled",
		"systemctl is-active truesense.service":                "active",
		"systemctl --user is-active truesense-keys.service":    "active",
		"systemctl --user is-active truesense-audio.service":   "active",
		"systemctl --user is-active truesense-startup.service": "active",
		"systemctl --user is-active pipewire.service":          "active",
	}}
	r.fw = &fakeFirmware{caps: map[string]bool{"gaming": true, "rear": true}, fan: hw.FanState{BehaviorRaw: "0x41", Mode: "auto"}}
	r.d = &fakeDaemon{state: DaemonState{Live: Live{Activity: "idle"}, Settings: Settings{AutoProfile: true}}}
	r.au = &fakeAudio{rnnoise: true, dts: true, headphones: false, mic: false, known: true, ok: true}
	r.env = &Env{
		Root: root, Run: r.run.run, Kernel: kernel, Version: "0.3.17", EUID: 1000,
		Getenv: func(string) string { return "" }, LookPath: func(n string) (string, error) { return "/usr/bin/" + n, nil },
		LookupUID: func(string) (int, error) { return 1000, nil }, Now: func() time.Time { return now },
		Firmware: r.fw, Daemon: r.d, Audio: r.au, Config: func() (*config.Config, error) { return config.Default(), nil },
		FnKeyNode: func() string { return "/dev/hidraw3" }, CeilingLow: func() []string { return nil }, Access: func(string) bool { return true }, Workers: 4,
	}
	return r
}

// one runs a single check by title.
func (r *rig) one(t *testing.T, title string) Result {
	t.Helper()
	for _, c := range Checks() {
		if c.Title == title {
			return runOne(context.Background(), r.env, c)
		}
	}
	t.Fatalf("no check called %q", title)
	return Result{}
}

func want(t *testing.T, got Result, status Status, detailPart, fixPart string) {
	t.Helper()
	if got.Status != status || !strings.Contains(got.Detail, detailPart) || !strings.Contains(got.Fix, fixPart) {
		t.Errorf("%s = {%s | %q | fix %q}, want status %s, detail containing %q, fix containing %q", got.Check, got.Status, got.Detail, got.Fix, status, detailPart, fixPart)
	}
}
