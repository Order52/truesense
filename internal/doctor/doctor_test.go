package doctor

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Order52/truesense/internal/config"
	"github.com/Order52/truesense/internal/hw"
)

func TestTheTitlesAreTheOnesUsersAndDocsRefer(t *testing.T) {
	var titles []string
	for _, c := range Checks() {
		titles = append(titles, c.Title)
	}
	got := strings.Join(titles, "|")
	want := "Model|Kernel|Kernel headers|DKMS|Kernel module|Device|Conflicts|Power managers|CPU turbo|Daemon|Service version|User services|udev / keyboard|Config|Secure Boot|Audio|Fans|Window|Status bars"
	if got != want {
		t.Fatalf("titles = %s", got)
	}
}

func TestAHealthyMachineHasNoWarningsAndNoFailures(t *testing.T) {
	r := newRig(t)
	rep := Run(context.Background(), r.env, Checks())
	for _, c := range rep.Checks {
		if c.Status != OK && c.Status != INFO {
			t.Errorf("%s: %s %s (fix: %s)", c.Check, c.Status, c.Detail, c.Fix)
		}
	}
	if !rep.OK || rep.Failures != 0 || rep.Warnings != 0 || rep.ExitCode() != 0 {
		t.Fatalf("report = %+v", rep)
	}
	// the doctor only reads: no program it ran is anything but a status query
	for _, c := range r.run.calls {
		if !strings.HasPrefix(c, "systemctl is-active") && !strings.HasPrefix(c, "systemctl --user is-active") && !strings.HasPrefix(c, "modinfo") && !strings.HasPrefix(c, "mokutil") && !strings.HasPrefix(c, "dkms status") {
			t.Errorf("the doctor ran %q", c)
		}
	}
}

func TestChecksRunSideBySideAndAreReportedInOrder(t *testing.T) {
	// Five checks that cannot finish until all five have started prove they overlap.
	var started atomic.Int32
	gate := make(chan struct{})
	slow := func(i int) Check {
		return Check{Title: "T" + string(rune('0'+i)), Run: func(ctx context.Context, _ *Env) Outcome {
			if started.Add(1) == 5 {
				close(gate)
			}
			select {
			case <-gate:
			case <-time.After(5 * time.Second):
				return Outcome{Status: FAIL, Detail: "the checks did not overlap"}
			}
			return Outcome{Status: OK, Detail: "c" + string(rune('0'+i))}
		}}
	}
	checks := []Check{slow(0), slow(1), slow(2), slow(3), slow(4), {Title: "Broken", Run: func(context.Context, *Env) Outcome { panic("boom") }}, {Title: "Last", Run: func(context.Context, *Env) Outcome { return Outcome{Status: OK, Detail: "last"} }}}
	rep := Run(context.Background(), &Env{Workers: 8, Version: "1", Kernel: "k"}, checks)
	var titles []string
	for _, c := range rep.Checks {
		titles = append(titles, c.Check)
	}
	if got := strings.Join(titles, ","); got != "T0,T1,T2,T3,T4,Broken,Last" {
		t.Fatalf("order = %s", got)
	}
	for _, c := range rep.Checks[:5] {
		if c.Status != OK {
			t.Fatalf("%s = %+v", c.Check, c)
		}
	}
	broken := rep.Checks[5]
	if broken.Status != WARN || !strings.Contains(broken.Detail, "boom") || !strings.Contains(broken.Detail, "check crashed") {
		t.Fatalf("a panicking check = %+v", broken)
	}
	if rep.Warnings != 1 || rep.Failures != 0 || !rep.OK {
		t.Fatalf("counts = %+v", rep)
	}
}

func TestTheWorkerLimitIsHonoured(t *testing.T) {
	var inFlight, peak atomic.Int32
	var checks []Check
	for i := 0; i < 12; i++ {
		checks = append(checks, Check{Title: "c", Run: func(context.Context, *Env) Outcome {
			n := inFlight.Add(1)
			for {
				p := peak.Load()
				if n <= p || peak.CompareAndSwap(p, n) {
					break
				}
			}
			time.Sleep(5 * time.Millisecond)
			inFlight.Add(-1)
			return Outcome{Status: OK}
		}})
	}
	Run(context.Background(), &Env{Workers: 3}, checks)
	if p := peak.Load(); p > 3 || p < 2 {
		t.Fatalf("peak concurrency = %d, want 2..3", p)
	}
}

func TestModelGrades(t *testing.T) {
	for _, tc := range []struct {
		product, vendor, bios string
		status                Status
	}{
		{"Predator PH16-71", "Acer", "V1.18", OK}, {"Predator PH16-71", "Acer", "V1.99", INFO}, {"Predator PH18-71", "Acer", "V1", WARN}, {"Latitude", "Dell Inc.", "A1", FAIL},
	} {
		r := newRig(t)
		write(t, r.root, "sys/class/dmi/id/product_name", tc.product)
		write(t, r.root, "sys/class/dmi/id/sys_vendor", tc.vendor)
		write(t, r.root, "sys/class/dmi/id/bios_version", tc.bios)
		got := r.one(t, "Model")
		if got.Status != tc.status {
			t.Errorf("%s: %+v", tc.product, got)
		}
		if tc.status == WARN && !strings.Contains(got.Fix, "truesense probe") || tc.status == FAIL && !strings.Contains(got.Fix, "Acer Predator") {
			t.Errorf("%s fix = %q", tc.product, got.Fix)
		}
	}
}

func TestKernelAndHeaders(t *testing.T) {
	r := newRig(t)
	for k, status := range map[string]Status{"7.3.0-cachyos": OK, "6.6.0": OK, "6.5.9": WARN, "5.15.0": WARN, "6.18.1-lts": OK, "weird": WARN} {
		r.env.Kernel = k
		if got := r.one(t, "Kernel"); got.Status != status {
			t.Errorf("kernel %s = %+v", k, got)
		}
	}
	r.env.Kernel = kernel
	want(t, r.one(t, "Kernel headers"), OK, "present", "")
	r.env.Kernel = "9.9.9-missing"
	want(t, r.one(t, "Kernel headers"), FAIL, "MISSING", "dkms autoinstall")
}

func TestDKMSReadsItsOwnDirectories(t *testing.T) {
	r := newRig(t)
	// installed for the running kernel and another one
	root := t.TempDir()
	r.env.Root = root
	installDKMS(t, root, "truesense-wmi", kernel, "6.18.1-lts")
	installKernels(t, root, kernel, "6.18.1-lts")
	got := r.one(t, "DKMS")
	want(t, got, OK, "6.18.1-lts", "")
	want(t, got, OK, kernel, "")
	if r.run.called("dkms") {
		t.Error("ran dkms")
	}
}

func TestDKMSNotRegisteredNotBuiltForThisKernelAndMissingForAnother(t *testing.T) {
	r := newRig(t)
	root := t.TempDir()
	r.env.Root = root
	mkdir(t, root, "var/lib/dkms") // DKMS is there, the package is not
	installKernels(t, root, kernel)
	want(t, r.one(t, "DKMS"), FAIL, "not registered", "tools/install.sh")

	installDKMS(t, root, "truesense-wmi", "6.18.1-lts")
	installKernels(t, root, "6.18.1-lts")
	want(t, r.one(t, "DKMS"), FAIL, "running kernel "+kernel, "dkms autoinstall")

	installDKMS(t, root, "truesense-wmi", kernel)
	installKernels(t, root, "7.4.0-new")
	want(t, r.one(t, "DKMS"), WARN, "7.4.0-new", "dkms autoinstall")

	// a dangling kernel link does not count as built
	if err := os.RemoveAll(filepath.Join(root, "var/lib/dkms/truesense-wmi/0.3.99/"+kernel)); err != nil {
		t.Fatal(err)
	}
	want(t, r.one(t, "DKMS"), FAIL, "running kernel", "")
}

func TestASystemWithNoDKMSDirectoryFallsBackToTheCommand(t *testing.T) {
	r := newRig(t)
	root := t.TempDir()
	r.env.Root = root
	installKernels(t, root, kernel)
	r.run.table["dkms status truesense-wmi"] = "truesense-wmi/0.3.99, " + kernel + ", x86_64: installed"
	want(t, r.one(t, "DKMS"), OK, kernel, "")
	r.run.table["dkms status truesense-wmi"] = ""
	want(t, r.one(t, "DKMS"), FAIL, "not registered", "")
	delete(r.run.table, "dkms status truesense-wmi") // dkms not even installed
	want(t, r.one(t, "DKMS"), FAIL, "not registered", "")
}

func TestOldDKMSPackagesAreFoundFromTheDirectoryListing(t *testing.T) {
	r := newRig(t)
	installDKMS(t, r.root, "linuwu-sense", kernel)
	got := r.one(t, "Conflicts")
	want(t, got, WARN, "old DKMS", "migrate-legacy.sh")
	want(t, got, WARN, "linuwu-sense", "")
	if err := os.RemoveAll(filepath.Join(r.root, "var/lib/dkms/linuwu-sense")); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(r.one(t, "Conflicts").Detail, "old DKMS") {
		t.Error("still reported after removal")
	}
}

func TestConflicts(t *testing.T) {
	r := newRig(t)
	want(t, r.one(t, "Conflicts"), OK, "old drivers are blocked", "")
	mkdir(t, r.root, "sys/module/acer_wmi")
	want(t, r.one(t, "Conflicts"), FAIL, "acer_wmi", "migrate-legacy.sh")
	if err := os.RemoveAll(filepath.Join(r.root, "sys/module/acer_wmi")); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(filepath.Join(r.root, "etc/modprobe.d/truesense.conf")); err != nil {
		t.Fatal(err)
	}
	want(t, r.one(t, "Conflicts"), WARN, "no modprobe block", "tools/install.sh")
	// with no DKMS directory the legacy packages are looked up with the command
	r.env.Root = t.TempDir()
	write(t, r.env.Root, "etc/modprobe.d/truesense.conf", "")
	r.run.table["dkms status"] = "linuwu-sense/1.0, 7.3.0, x86_64: installed\ntruesense-wmi/0.3.17, 7.3.0, x86_64: installed"
	want(t, r.one(t, "Conflicts"), WARN, "linuwu-sense", "")
}

func TestKernelModule(t *testing.T) {
	r := newRig(t)
	want(t, r.one(t, "Kernel module"), OK, "truesense_wmi 0.3.17 loaded", "")
	r.run.table["modinfo -F version truesense_wmi"] = "0.3.18"
	want(t, r.one(t, "Kernel module"), WARN, "loaded 0.3.17 but 0.3.18 is installed", "reboot")
	delete(r.run.table, "modinfo -F version truesense_wmi") // modinfo missing: nothing to compare
	want(t, r.one(t, "Kernel module"), OK, "", "")
	if err := os.RemoveAll(filepath.Join(r.root, "sys/module/truesense_wmi")); err != nil {
		t.Fatal(err)
	}
	want(t, r.one(t, "Kernel module"), FAIL, "not loaded", "modprobe truesense_wmi")
}

func TestDevice(t *testing.T) {
	r := newRig(t)
	want(t, r.one(t, "Device"), OK, "interfaces: gaming=yes, rear=yes", "")
	r.fw.caps["rear"] = false
	want(t, r.one(t, "Device"), WARN, "rear=NO", "the firmware lacks rear")
	r.fw.capsErr = errors.New("device busy")
	want(t, r.one(t, "Device"), FAIL, "device busy", "dmesg")
	r.env.Access = func(string) bool { return false }
	want(t, r.one(t, "Device"), WARN, "no access", "uaccess")
	if err := os.Remove(filepath.Join(r.root, "dev/truesense")); err != nil {
		t.Fatal(err)
	}
	want(t, r.one(t, "Device"), FAIL, "/dev/truesense missing", "dmesg | grep truesense")
}

func TestPowerManagers(t *testing.T) {
	r := newRig(t)
	want(t, r.one(t, "Power managers"), OK, "no other power manager", "")
	r.run.table["systemctl is-active thermald.service"] = "active"
	want(t, r.one(t, "Power managers"), INFO, "also running: thermald", "")
	r.run.table["systemctl is-active tlp.service"] = "active"
	r.run.table["systemctl is-active power-profiles-daemon.service"] = "active"
	got := r.one(t, "Power managers")
	want(t, got, WARN, "also running: tlp, thermald, power-profiles-daemon", "truesense power managers stop")
}

func TestCPUTurbo(t *testing.T) {
	r := newRig(t)
	want(t, r.one(t, "CPU turbo"), OK, "turbo boost is on", "")
	r.env.CeilingLow = func() []string { return []string{"cpu0", "cpu1"} }
	want(t, r.one(t, "CPU turbo"), WARN, "2 core(s) have a frequency ceiling", "power managers stop")
	write(t, r.root, "sys/devices/system/cpu/intel_pstate/no_turbo", "1\n")
	want(t, r.one(t, "CPU turbo"), WARN, "turbo boost is OFF", "no_turbo")
	if err := os.Remove(filepath.Join(r.root, "sys/devices/system/cpu/intel_pstate/no_turbo")); err != nil {
		t.Fatal(err)
	}
	want(t, r.one(t, "CPU turbo"), INFO, "no intel_pstate", "")
}

func TestDaemon(t *testing.T) {
	r := newRig(t)
	want(t, r.one(t, "Daemon"), OK, "running; smart_fan=off, auto_profile=on, activity=idle", "")
	r.d.state.Settings.SmartFan = true
	r.d.state.Live.Activity = ""
	want(t, r.one(t, "Daemon"), OK, "smart_fan=on, auto_profile=on, activity=?", "")

	no := false
	r.d.state.Live.HWOK = &no
	want(t, r.one(t, "Daemon"), WARN, "cannot reach the kernel device", "module")
	r.d.state.Live.HWOK = nil

	old := r.now.Add(-42 * time.Second)
	if err := os.Chtimes(filepath.Join(r.root, "run/truesense/state"), old, old); err != nil {
		t.Fatal(err)
	}
	want(t, r.one(t, "Daemon"), WARN, "daemon state is 42s old", "restart truesense")
	if err := os.Remove(filepath.Join(r.root, "run/truesense/state")); err != nil { // no state file: no age to judge
		t.Fatal(err)
	}
	want(t, r.one(t, "Daemon"), OK, "", "")

	r.d.stateErr = errors.New("timeout")
	want(t, r.one(t, "Daemon"), WARN, "control socket does not answer: timeout", "restart truesense")
	r.run.table["systemctl is-active truesense.service"] = "failed"
	want(t, r.one(t, "Daemon"), FAIL, "not running", "journalctl -u truesense")
}

func TestServiceVersion(t *testing.T) {
	r := newRig(t)
	want(t, r.one(t, "Service version"), OK, "same version (0.3.17)", "")
	if strings.Join(r.d.callsSeen, ",") != "fan_curve,battery_full" {
		t.Errorf("probed %v", r.d.callsSeen)
	}
	r.d.callErr = map[string]error{"battery_full": errors.New("unknown command: battery_full")}
	want(t, r.one(t, "Service version"), WARN, "older than the installed app", "restart truesense")
	r.d.callErr = map[string]error{"fan_curve": errors.New("socket closed")}
	want(t, r.one(t, "Service version"), WARN, "did not answer a version probe: socket closed", "")
	r.run.table["systemctl is-active truesense.service"] = "inactive"
	want(t, r.one(t, "Service version"), INFO, "service not running", "")
}

func TestUserServices(t *testing.T) {
	r := newRig(t)
	want(t, r.one(t, "User services"), OK, "truesense-keys=active, truesense-audio=active, truesense-startup=active", "")
	r.run.table["systemctl --user is-active truesense-audio.service"] = "inactive"
	delete(r.run.table, "systemctl --user is-active truesense-startup.service")
	want(t, r.one(t, "User services"), WARN, "truesense-audio=inactive, truesense-startup=inactive", "systemctl --user enable --now truesense-audio.service truesense-startup.service")
}

func TestUdev(t *testing.T) {
	r := newRig(t)
	want(t, r.one(t, "udev / keyboard"), OK, "Fn-key interface /dev/hidraw3 readable", "")
	r.env.Access = func(string) bool { return false }
	want(t, r.one(t, "udev / keyboard"), WARN, "NOT readable", "")
	r.env.FnKeyNode = func() string { return "" }
	want(t, r.one(t, "udev / keyboard"), WARN, "Fn-key interface not found", "04f2:0117")
	if err := os.Remove(filepath.Join(r.root, "etc/udev/rules.d/70-truesense.rules")); err != nil {
		t.Fatal(err)
	}
	want(t, r.one(t, "udev / keyboard"), FAIL, "udev rule missing", "tools/install.sh")
	write(t, r.root, "usr/lib/udev/rules.d/70-truesense.rules", "") // the packaged location counts too
	want(t, r.one(t, "udev / keyboard"), WARN, "Fn-key interface not found", "")
}

func TestConfig(t *testing.T) {
	r := newRig(t)
	want(t, r.one(t, "Config"), OK, "configuration valid", "")
	bad := config.Default()
	bad.Audio.Jack = "sideways"
	bad.Update.CheckHours = 0
	r.env.Config = func() (*config.Config, error) { return bad, nil }
	got := r.one(t, "Config")
	want(t, got, FAIL, "[audio] jack must be auto, headphones or headset", "config.toml")
	want(t, got, FAIL, "[update] check_hours must be 1..168", "")
	r.env.Config = func() (*config.Config, error) { return nil, errors.New("config.toml: line 3: bad") }
	want(t, r.one(t, "Config"), FAIL, "config does not parse: config.toml: line 3: bad", "TOML syntax")
}

func TestSecureBoot(t *testing.T) {
	r := newRig(t)
	want(t, r.one(t, "Secure Boot"), INFO, "Secure Boot off or unknown; module signer: none", "")
	r.run.table["mokutil --sb-state"] = "SecureBoot enabled"
	want(t, r.one(t, "Secure Boot"), WARN, "module signer: NONE", "mokutil --import")
	r.run.table["modinfo -F signer truesense_wmi"] = "DKMS module signing key"
	want(t, r.one(t, "Secure Boot"), OK, "module signer: DKMS module signing key", "")
}

func TestAudio(t *testing.T) {
	r := newRig(t)
	r.au.headphones, r.au.mic = true, true
	want(t, r.one(t, "Audio"), OK, "PipeWire running; output sink present; RNNoise plugin found; AcerUltraX found; jack: headphones in, headset mic detected", "")

	r.au.known, r.au.ok, r.au.problem = true, false, "the sound card is switched off; its speaker and headphone profiles are missing"
	got := r.one(t, "Audio")
	want(t, got, WARN, "NO SOUND OUTPUT: the sound card is switched off", "truesense audio repair")
	want(t, got, WARN, "PipeWire running", "")

	r.au.known = false // no card to judge
	r.au.bare = []string{"Headset PC38X"}
	want(t, r.one(t, "Audio"), WARN, "DTS is off (Plain) for: Headset PC38X; PipeWire running", "truesense audio dts all")
	r.env.EUID = 0 // the DTS service is per user: a root doctor does not ask
	want(t, r.one(t, "Audio"), OK, "PipeWire running", "")
	r.env.EUID = 1000
	r.au.bareErr = errors.New("cli failed")
	want(t, r.one(t, "Audio"), OK, "", "")
	r.au.jackErr = errors.New("no amixer")
	if got := r.one(t, "Audio"); strings.Contains(got.Detail, "jack:") {
		t.Errorf("jack detail without a jack: %q", got.Detail)
	}

	r.run.table["systemctl --user is-active pipewire.service"] = "inactive"
	r.au.known, r.au.ok = true, false // not consulted when PipeWire is down
	got = r.one(t, "Audio")
	want(t, got, WARN, "PipeWire NOT running", "systemctl --user start pipewire")
	if strings.Contains(got.Detail, "NO SOUND OUTPUT") {
		t.Errorf("a dead PipeWire is not a dead card: %q", got.Detail)
	}
	r.au.rnnoise, r.au.dts = false, false
	want(t, r.one(t, "Audio"), WARN, "RNNoise plugin missing (Purified Voice off); AcerUltraX not installed (optional)", "")
}

func TestFans(t *testing.T) {
	r := newRig(t)
	want(t, r.one(t, "Fans"), OK, "fans: firmware auto", "")
	r.fw.fan = hw.FanState{BehaviorRaw: "0xc3", Mode: "manual"}
	want(t, r.one(t, "Fans"), OK, "fans: manual", "")
	r.fw.fan = hw.FanState{BehaviorRaw: "0x7", Mode: "unknown"}
	want(t, r.one(t, "Fans"), OK, "fans: 0x7", "")
	r.fw.fan = hw.FanState{BehaviorRaw: "0x82", Mode: "max"}
	want(t, r.one(t, "Fans"), OK, "fans: MAX", "")
	r.d.state.Settings.SmartFan = true
	want(t, r.one(t, "Fans"), WARN, "MAX while the smart curve is on", "truesense fan smart on")
	r.d.stateErr = errors.New("down") // no daemon: nothing to contradict
	want(t, r.one(t, "Fans"), OK, "fans: MAX", "")
	r.fw.fanErr = errors.New("busy")
	want(t, r.one(t, "Fans"), INFO, "fan state unreadable", "")
}

func TestWindowAndStatusBars(t *testing.T) {
	r := newRig(t)
	want(t, r.one(t, "Window"), OK, "launcher found", "")
	want(t, r.one(t, "Status bars"), OK, "fast path", "")
	r.env.LookPath = func(string) (string, error) { return "", errors.New("no") }
	want(t, r.one(t, "Window"), INFO, "xdg-open", "xdg-utils")
	want(t, r.one(t, "Status bars"), INFO, "truesense-fast not found", "installer")
}

func TestAUserSessionCommandIsRunAsTheSudoUser(t *testing.T) {
	r := newRig(t)
	r.env.EUID = 0
	r.env.Getenv = func(k string) string {
		if k == "SUDO_USER" {
			return "alice"
		}
		return ""
	}
	r.env.LookupUID = func(string) (int, error) { return 1234, nil }
	r.run.table["runuser -u alice -- env XDG_RUNTIME_DIR=/run/user/1234 DBUS_SESSION_BUS_ADDRESS=unix:path=/run/user/1234/bus systemctl --user is-active truesense-keys.service"] = "active"
	if !r.env.active(context.Background(), "truesense-keys.service", true) {
		t.Fatalf("calls = %v", r.run.calls)
	}
	// system units are never wrapped
	r.env.active(context.Background(), "truesense.service", false)
	if !r.run.called("systemctl is-active truesense.service") {
		t.Fatalf("calls = %v", r.run.calls)
	}
	// an unknown SUDO_USER, root itself, or no SUDO_USER: our own session
	for _, who := range []string{"root", ""} {
		r.env.Getenv = func(string) string { return who }
		if got := r.env.asUser([]string{"x"}); len(got) != 1 {
			t.Errorf("SUDO_USER=%q wrapped: %v", who, got)
		}
	}
	r.env.Getenv = func(string) string { return "ghost" }
	r.env.LookupUID = func(string) (int, error) { return 0, errors.New("no such user") }
	if got := r.env.asUser([]string{"x"}); len(got) != 1 {
		t.Errorf("unknown user wrapped: %v", got)
	}
}

func TestTheReportRendersLikeTheOldOne(t *testing.T) {
	rep := Report{Version: "0.3.17", Kernel: "7.3.0", Failures: 1, Warnings: 1, Checks: []Result{
		{"Model", OK, "Predator PH16-71, BIOS V1.18", ""}, {"Audio", WARN, "no output", "truesense audio repair"}, {"DKMS", FAIL, "not built", "sudo dkms autoinstall"}, {"Window", INFO, "no browser", "install one"},
	}}
	var buf bytes.Buffer
	RenderText(&buf, rep, false)
	want := "TrueSense 0.3.17 doctor (7.3.0)\n" +
		"  [OK  ] Model            Predator PH16-71, BIOS V1.18\n" +
		"  [WARN] Audio            no output\n" +
		"         fix: truesense audio repair\n" +
		"  [FAIL] DKMS             not built\n" +
		"         fix: sudo dkms autoinstall\n" +
		"  [INFO] Window           no browser\n" +
		"\nPROBLEMS FOUND: 1 failure(s), 1 warning(s)\n"
	if buf.String() != want {
		t.Fatalf("text =\n%s\nwant\n%s", buf.String(), want)
	}
	buf.Reset()
	RenderText(&buf, rep, true)
	if !strings.Contains(buf.String(), "[\033[32mOK  \033[0m]") || !strings.Contains(buf.String(), "\033[31mFAIL\033[0m") {
		t.Errorf("colour output = %q", buf.String())
	}
	for _, tc := range []struct {
		f, w int
		want string
	}{{0, 0, "ALL GOOD: 0 failure(s), 0 warning(s)"}, {0, 2, "OK with warnings: 0 failure(s), 2 warning(s)"}} {
		buf.Reset()
		RenderText(&buf, Report{Version: "1", Kernel: "k", Failures: tc.f, Warnings: tc.w}, false)
		if !strings.Contains(buf.String(), tc.want) {
			t.Errorf("verdict for %+v = %q", tc, buf.String())
		}
	}
	buf.Reset()
	if err := RenderJSON(&buf, rep); err != nil {
		t.Fatal(err)
	}
	var m map[string]any
	if err := json.Unmarshal(buf.Bytes(), &m); err != nil {
		t.Fatal(err)
	}
	for _, k := range []string{"version", "kernel", "ok", "failures", "warnings", "checks"} {
		if _, ok := m[k]; !ok {
			t.Errorf("JSON lacks %q", k)
		}
	}
	first := m["checks"].([]any)[0].(map[string]any)
	for _, k := range []string{"check", "status", "detail", "fix"} {
		if _, ok := first[k]; !ok {
			t.Errorf("a check lacks %q", k)
		}
	}
	if rep.ExitCode() != 1 || (Report{Warnings: 5}).ExitCode() != 0 {
		t.Error("exit code: 1 only for failures")
	}
}

func TestNewEnvIsTheRealMachineAndRunsNothingByItself(t *testing.T) {
	e := NewEnv(nil, nil, nil)
	if e.Root != "" || e.Kernel == "" || e.Version == "" || e.Workers != 8 || e.Run == nil || e.Access == nil || e.LookupUID == nil {
		t.Fatalf("env = %+v", e)
	}
	if uid, err := e.LookupUID("root"); err != nil || uid != 0 {
		t.Errorf("LookupUID(root) = %d, %v", uid, err)
	}
}
