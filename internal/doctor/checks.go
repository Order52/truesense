package doctor

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"

	"github.com/Order52/truesense/internal/models"
)

// legacyModules are the old drivers that fight TrueSense over the fans and power modes.
var legacyModules = []string{"acer_wmi", "linuwu_sense", "acer_rear_wmi"}

// ---- helpers ----

func (e *Env) loaded(module string) bool { return isDir(e.path("/sys/module/" + module)) }

func isDir(p string) bool  { st, err := os.Stat(p); return err == nil && st.IsDir() }
func exists(p string) bool { _, err := os.Stat(p); return err == nil }

func readTrim(p string) (string, bool) {
	b, err := os.ReadFile(p)
	if err != nil {
		return "", false
	}
	return strings.TrimSpace(string(b)), true
}

// sh runs a program and returns its trimmed standard output ("" when it could not run).
func (e *Env) sh(ctx context.Context, name string, args ...string) string {
	return strings.TrimSpace(e.Run(ctx, name, args...).Stdout)
}

// asUser wraps a user-session command for the person who invoked sudo: a root doctor would
// otherwise inspect root's empty session.
func (e *Env) asUser(cmd []string) []string {
	user := e.Getenv("SUDO_USER")
	if e.EUID != 0 || user == "" || user == "root" || e.LookupUID == nil {
		return cmd
	}
	uid, err := e.LookupUID(user)
	if err != nil {
		return cmd // an unknown SUDO_USER: inspect our own session instead of failing the check
	}
	run := fmt.Sprintf("/run/user/%d", uid)
	return append([]string{"runuser", "-u", user, "--", "env", "XDG_RUNTIME_DIR=" + run, "DBUS_SESSION_BUS_ADDRESS=unix:path=" + run + "/bus"}, cmd...)
}

// active reports whether a systemd unit is active, in the system or in the user's session.
func (e *Env) active(ctx context.Context, unit string, user bool) bool {
	cmd := []string{"systemctl", "is-active", unit}
	if user {
		cmd = e.asUser([]string{"systemctl", "--user", "is-active", unit})
	}
	return e.sh(ctx, cmd[0], cmd[1:]...) == "active"
}

func joinLimit(parts []string, sep string, n int) string {
	s := strings.Join(parts, sep)
	if r := []rune(s); len(r) > n {
		return string(r[:n])
	}
	return s
}

func onOff(b bool, on, off string) string {
	if b {
		return on
	}
	return off
}

// ---- checks ----

func checkModel(_ context.Context, e *Env) Outcome {
	m := models.Identify(e.path(models.DMIRoot))
	switch m.Tier {
	case models.TierSupported:
		if m.BIOSVerified != nil && *m.BIOSVerified {
			return ok(m.Message)
		}
		return info(m.Message, "")
	case models.TierUnverified:
		return warn(m.Message, "truesense probe   (read-only) and share the report; the kernel module does not bind on an unverified model unless forced")
	}
	return fail(m.Message, "TrueSense is for Acer Predator laptops; the kernel module refuses to bind elsewhere")
}

var releaseRE = regexp.MustCompile(`^(\d+)\.(\d+)`)

func checkKernel(_ context.Context, e *Env) Outcome {
	m := releaseRE.FindStringSubmatch(e.Kernel)
	if m == nil {
		return warn(fmt.Sprintf("kernel %q is not a release number I understand", e.Kernel), "kernels older than 6.6 are untested")
	}
	major, _ := strconv.Atoi(m[1])
	minor, _ := strconv.Atoi(m[2])
	return graded(major > 6 || (major == 6 && minor >= 6), "kernel "+e.Kernel, "kernels older than 6.6 are untested")
}

func checkHeaders(_ context.Context, e *Env) Outcome {
	present := exists(e.path("/lib/modules/" + e.Kernel + "/build"))
	detail := fmt.Sprintf("kernel headers for %s: %s", e.Kernel, onOff(present, "present", "MISSING"))
	if present {
		return ok(detail)
	}
	return fail(detail, "install your kernel's -headers package (e.g. linux-cachyos-headers), then: sudo dkms autoinstall")
}

var dkmsKernelRE = regexp.MustCompile(`^kernel-(.+)-([A-Za-z0-9_]+)$`)

// dkmsInstalled lists the kernels a DKMS package is built and installed for, read from DKMS's
// own directories (one `kernel-<release>-<arch>` link per installed kernel): the `dkms`
// command takes about a second for the same answer. known is false when this system keeps its
// DKMS state elsewhere (the caller then asks the command); an empty list means the package is
// not registered at all.
func (e *Env) dkmsInstalled(pkg string) (kernels []string, known bool) {
	if !isDir(e.path(dkmsRoot)) {
		return nil, false
	}
	base := e.path(dkmsRoot + "/" + pkg)
	if !isDir(base) {
		return nil, true
	}
	links, _ := filepath.Glob(filepath.Join(base, "kernel-*-*"))
	for _, link := range links {
		if m := dkmsKernelRE.FindStringSubmatch(filepath.Base(link)); m != nil && isDir(link) {
			kernels = append(kernels, m[1])
		}
	}
	sort.Strings(kernels)
	return kernels, true
}

var dkmsStatusRE = regexp.MustCompile(`,\s*([^,]+),\s*x86_64:\s*installed`)

func (e *Env) installedKernels() []string {
	builds, _ := filepath.Glob(e.path("/lib/modules/*/build"))
	out := make([]string, len(builds))
	for i, b := range builds {
		out[i] = filepath.Base(filepath.Dir(b))
	}
	sort.Strings(out)
	return out
}

func checkDKMS(ctx context.Context, e *Env) Outcome {
	notRegistered := fail("truesense-wmi is not registered with DKMS", "sudo tools/install.sh")
	built, known := e.dkmsInstalled("truesense-wmi")
	if !known { // an unusual layout: ask the command
		out := e.sh(ctx, "dkms", "status", "truesense-wmi")
		if out == "" {
			return notRegistered
		}
		for _, m := range dkmsStatusRE.FindAllStringSubmatch(out, -1) {
			built = append(built, m[1])
		}
	} else if !isDir(e.path(dkmsRoot + "/truesense-wmi")) {
		return notRegistered
	}
	var missing []string
	for _, k := range e.installedKernels() {
		if !contains(built, k) {
			missing = append(missing, k)
		}
	}
	switch {
	case !contains(built, e.Kernel):
		return fail("not built for the running kernel "+e.Kernel, "sudo dkms autoinstall   (or: sudo tools/install.sh)")
	case len(missing) > 0:
		return warn(fmt.Sprintf("built for %s; missing for installed kernel(s) %s", strings.Join(built, ", "), strings.Join(missing, ", ")), "sudo dkms autoinstall")
	}
	return ok("installed for " + strings.Join(built, ", "))
}

func contains(list []string, s string) bool {
	for _, x := range list {
		if x == s {
			return true
		}
	}
	return false
}

func checkModule(ctx context.Context, e *Env) Outcome {
	if !e.loaded(moduleName) {
		return fail("truesense_wmi is not loaded", "sudo modprobe truesense_wmi   (or reinstall: sudo tools/install.sh)")
	}
	v, found := readTrim(e.path("/sys/module/" + moduleName + "/version"))
	if !found {
		v = "?"
	}
	if disk := e.sh(ctx, "modinfo", "-F", "version", moduleName); disk != "" && v != disk {
		return warn(fmt.Sprintf("loaded %s but %s is installed", v, disk), "reboot (or: sudo modprobe -r truesense_wmi && sudo modprobe truesense_wmi)")
	}
	return ok(fmt.Sprintf("truesense_wmi %s loaded", v))
}

func checkDevice(_ context.Context, e *Env) Outcome {
	dev := e.path(devicePath)
	if !exists(dev) {
		return fail(devicePath+" missing", "the module did not bind: check `dmesg | grep truesense`")
	}
	if e.Access != nil && !e.Access(dev) {
		return warn(devicePath+" exists but you have no access", "log in on the laptop's seat (uaccess) or use sudo; check /etc/udev/rules.d/70-truesense.rules")
	}
	if e.Firmware == nil {
		return fail("no firmware connection", "see dmesg")
	}
	caps, err := e.Firmware.Capabilities()
	if err != nil {
		return fail(err.Error(), "see dmesg")
	}
	names := make([]string, 0, len(caps))
	var missing []string
	for k := range caps {
		names = append(names, k)
	}
	sort.Strings(names)
	parts := make([]string, len(names))
	for i, k := range names {
		parts[i] = k + "=" + onOff(caps[k], "yes", "NO")
		if !caps[k] {
			missing = append(missing, k)
		}
	}
	detail := "interfaces: " + strings.Join(parts, ", ")
	if len(missing) > 0 {
		return warn(detail, "the firmware lacks "+strings.Join(missing, ", "))
	}
	return ok(detail)
}

var legacyDKMSRE = regexp.MustCompile(`linuwu|acer-rear`)

func checkConflicts(ctx context.Context, e *Env) Outcome {
	var bad []string
	for _, m := range legacyModules {
		if e.loaded(m) {
			bad = append(bad, m)
		}
	}
	if len(bad) > 0 {
		return fail("conflicting kernel module(s) loaded: "+strings.Join(bad, ", "), "sudo tools/migrate-legacy.sh   (they fight over the fans and power modes)")
	}
	blocked := exists(e.path(modprobeCfg))
	var legacyDKMS []string
	if entries, err := os.ReadDir(e.path(dkmsRoot)); err == nil {
		for _, d := range entries {
			if legacyDKMSRE.MatchString(d.Name()) {
				legacyDKMS = append(legacyDKMS, d.Name())
			}
		}
		sort.Strings(legacyDKMS)
	} else {
		for _, line := range strings.Split(e.sh(ctx, "dkms", "status"), "\n") {
			if legacyDKMSRE.MatchString(line) {
				legacyDKMS = append(legacyDKMS, line)
			}
		}
	}
	switch {
	case len(legacyDKMS) > 0:
		return warn("old DKMS package(s) still registered: "+joinLimit(legacyDKMS, "; ", 80), "sudo tools/migrate-legacy.sh")
	case !blocked:
		return warn("no modprobe block for the old drivers", "sudo tools/install.sh")
	}
	return ok("no conflicting driver loaded; old drivers are blocked from loading")
}

// checkPowerManagers: other daemons that change CPU governors or power limits can fight the
// power modes (they do not touch the firmware path).
func checkPowerManagers(ctx context.Context, e *Env) Outcome {
	var on, risky []string
	for _, u := range []string{"tuned", "tlp", "auto-cpufreq", "system76-power", "thermald", "power-profiles-daemon"} {
		if e.active(ctx, u+".service", false) {
			on = append(on, u)
			if u != "thermald" && u != "power-profiles-daemon" {
				risky = append(risky, u)
			}
		}
	}
	switch {
	case len(on) == 0:
		return ok("no other power manager is running")
	case len(risky) > 0:
		return warn("also running: "+strings.Join(on, ", "), "these can override CPU frequency, turbo or limits. Let TrueSense stop them (undoable): truesense power managers stop   (or Settings, TrueSense manages power)")
	}
	return info("also running: "+strings.Join(on, ", ")+" (they do not touch the firmware power modes)", "")
}

func checkTurbo(_ context.Context, e *Env) Outcome {
	t, found := readTrim(e.path("/sys/devices/system/cpu/intel_pstate/no_turbo"))
	switch {
	case !found:
		return info("no intel_pstate turbo switch on this CPU", "")
	case t == "1":
		return warn("CPU turbo boost is OFF: the CPU is held at its base clock", "a power manager may have left it off: truesense power managers stop   (or: echo 0 | sudo tee /sys/devices/system/cpu/intel_pstate/no_turbo)")
	}
	if e.CeilingLow != nil {
		if low := e.CeilingLow(); len(low) > 0 {
			return warn(fmt.Sprintf("CPU turbo is on but %d core(s) have a frequency ceiling below their maximum (left over from turbo being off, or a thermal cap)", len(low)),
				"truesense power managers stop   (raises the ceilings; the service re-applies a thermal cap if it needs one)")
		}
	}
	return ok("CPU turbo boost is on")
}

func checkDaemon(ctx context.Context, e *Env) Outcome {
	if !e.active(ctx, "truesense.service", false) {
		return fail("truesense.service is not running", "sudo systemctl enable --now truesense.service ; journalctl -u truesense -b")
	}
	st, err := e.Daemon.State(ctx)
	if err != nil {
		return warn("service running but the control socket does not answer: "+err.Error(), "sudo systemctl restart truesense")
	}
	var age float64
	var haveAge bool
	if fi, err := os.Stat(e.path(statePath)); err == nil {
		age, haveAge = float64(e.Now().Sub(fi.ModTime()))/1e9, true
	}
	switch {
	case st.Live.HWOK != nil && !*st.Live.HWOK:
		return warn("daemon cannot reach the kernel device", "check the module (above)")
	case haveAge && age > 10:
		return warn(fmt.Sprintf("daemon state is %.0fs old (stalled?)", age), "sudo systemctl restart truesense")
	}
	activity := st.Live.Activity
	if activity == "" {
		activity = "?"
	}
	return ok(fmt.Sprintf("running; smart_fan=%s, auto_profile=%s, activity=%s", onOff(st.Settings.SmartFan, "on", "off"), onOff(st.Settings.AutoProfile, "on", "off"), activity))
}

// checkServiceVersion: the running service must know every command the app and CLI use; after
// an upgrade it has to be restarted (the installer does that).
func checkServiceVersion(ctx context.Context, e *Env) Outcome {
	if !e.active(ctx, "truesense.service", false) {
		return info("service not running", "")
	}
	for _, probe := range []string{"fan_curve", "battery_full"} {
		if err := e.Daemon.Call(ctx, probe, map[string]any{"action": "get"}); err != nil {
			if strings.Contains(err.Error(), "unknown command") {
				return warn("the running service is older than the installed app", "sudo systemctl restart truesense   (or re-run sudo tools/install.sh)")
			}
			return warn("service did not answer a version probe: "+err.Error(), "")
		}
	}
	return ok(fmt.Sprintf("service and app are the same version (%s)", e.Version))
}

func checkUserServices(ctx context.Context, e *Env) Outcome {
	units := []string{"truesense-keys.service", "truesense-audio.service", "truesense-startup.service"}
	var bad, parts []string
	for _, u := range units {
		a := e.active(ctx, u, true)
		parts = append(parts, strings.SplitN(u, ".", 2)[0]+"="+onOff(a, "active", "inactive"))
		if !a {
			bad = append(bad, u)
		}
	}
	if len(bad) > 0 {
		return warn(strings.Join(parts, ", "), "systemctl --user enable --now "+strings.Join(bad, " "))
	}
	return ok(strings.Join(parts, ", "))
}

func checkUdev(_ context.Context, e *Env) Outcome {
	rule := exists(e.path("/etc/udev/rules.d/70-truesense.rules")) || exists(e.path("/usr/lib/udev/rules.d/70-truesense.rules"))
	node := ""
	if e.FnKeyNode != nil {
		node = e.FnKeyNode()
	}
	switch {
	case !rule:
		return fail("udev rule missing", "sudo tools/install.sh")
	case node == "":
		return warn("keyboard Fn-key interface not found", "is the internal keyboard (04f2:0117) present?")
	}
	readable := e.Access == nil || e.Access(node)
	return graded(readable, fmt.Sprintf("rule installed; Fn-key interface %s %s", node, onOff(readable, "readable", "NOT readable")), "")
}

func checkConfig(_ context.Context, e *Env) Outcome {
	cfg, err := e.Config()
	if err != nil {
		return fail("config does not parse: "+err.Error(), "fix the TOML syntax")
	}
	problems := cfg.Validate()
	if len(problems) > 0 {
		return fail(strings.Join(problems, "; "), "edit /etc/truesense/config.toml or ~/.config/truesense/config.toml")
	}
	return ok("configuration valid")
}

func checkSecureBoot(ctx context.Context, e *Env) Outcome {
	sb := strings.ToLower(e.sh(ctx, "mokutil", "--sb-state"))
	signer := e.sh(ctx, "modinfo", "-F", "signer", moduleName)
	if strings.Contains(sb, "enabled") {
		shown := signer
		if shown == "" {
			shown = "NONE"
		}
		return graded(signer != "", "Secure Boot on; module signer: "+shown, "enrol the DKMS key: sudo mokutil --import /var/lib/dkms/mok.pub, then reboot")
	}
	shown := signer
	if shown == "" {
		shown = "none"
	}
	return info("Secure Boot off or unknown; module signer: "+shown, "")
}

func checkAudio(ctx context.Context, e *Env) Outcome {
	pw := e.active(ctx, "pipewire.service", true)
	bits := []string{
		"PipeWire " + onOff(pw, "running", "NOT running"),
		"RNNoise plugin " + onOff(e.Audio.HasRNNoise(), "found", "missing (Purified Voice off)"),
		"AcerUltraX " + onOff(e.Audio.HasDTS(), "found", "not installed (optional)"),
	}
	if hp, mic, err := e.Audio.JackStatus(ctx); err == nil {
		bits = append(bits, "jack: headphones "+onOff(hp, "in", "out")+", headset mic "+onOff(mic, "detected", "not detected"))
	}
	var known, healthy bool
	var problem string
	if pw {
		if k, h, p, err := e.Audio.SoundCheck(ctx); err == nil {
			known, healthy, problem = k, h, p
		}
	}
	if known && !healthy {
		return warn("NO SOUND OUTPUT: "+problem+"; "+strings.Join(bits, "; "), "truesense audio repair   (restarts WirePlumber so it probes the card again)")
	}
	if known && healthy {
		bits = append(bits[:1], append([]string{"output sink present"}, bits[1:]...)...)
	}
	if e.EUID != 0 && e.Audio.HasDTS() { // the service is per user: ask it as the user, not as root
		if bare, err := e.Audio.PlainHeadsets(ctx); err == nil && len(bare) > 0 {
			return warn("DTS is off (Plain) for: "+strings.Join(bare, ", ")+"; "+strings.Join(bits, "; "), "truesense audio dts all   (gives every output DTS; new ones get it by themselves)")
		}
	}
	if !pw {
		return warn(strings.Join(bits, "; "), "systemctl --user start pipewire")
	}
	return ok(strings.Join(bits, "; "))
}

func checkFans(ctx context.Context, e *Env) Outcome {
	st, err := e.Firmware.GetFanState()
	if err != nil {
		return info("fan state unreadable", "")
	}
	mode := st.BehaviorRaw
	switch st.Mode {
	case "auto":
		mode = "firmware auto"
	case "max":
		mode = "MAX"
	case "manual":
		mode = "manual"
	}
	smart := false
	if ds, err := e.Daemon.State(ctx); err == nil {
		smart = ds.Settings.SmartFan
	}
	if st.Mode == "max" && smart {
		return warn("fans are on MAX while the smart curve is on (the daemon should have taken over)", "truesense fan smart on")
	}
	return ok("fans: " + mode)
}

// checkWindow: the window (truesense gui) is built into the binary and opens in a web browser
// as an app window; it needs a way to open one.
func checkWindow(_ context.Context, e *Env) Outcome {
	if _, err := e.LookPath("xdg-open"); err != nil {
		return info("the window (truesense gui) opens in your web browser and needs xdg-open to find it; everything works from the command line without it", "sudo pacman -S xdg-utils")
	}
	return ok("web browser launcher found (truesense gui)")
}

// checkStatusBars: status-bar buttons call `truesense-fast`, a link to the same binary that
// skips everything but the control socket.
func checkStatusBars(_ context.Context, e *Env) Outcome {
	if _, err := e.LookPath("truesense-fast"); err != nil {
		return info("truesense-fast not found: status-bar buttons fall back to the slower `truesense` command", "re-run the installer: sudo tools/install.sh")
	}
	return ok("truesense-fast found: status-bar buttons use the fast path")
}
