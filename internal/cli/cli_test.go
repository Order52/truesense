package cli_test

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/Order52/truesense/internal/api"
	"github.com/Order52/truesense/internal/apitest"
	"github.com/Order52/truesense/internal/cli"
	"github.com/Order52/truesense/internal/cli/command"
)

// result is one run of the command line.
type result struct {
	code     int
	out, err string
}

type machine struct {
	t     *testing.T
	f     *apitest.Fixture
	a     *api.API
	stdin string
	hooks cli.Hooks
}

func newMachine(t *testing.T) *machine {
	f := apitest.New(t)
	return &machine{t: t, f: f, a: f.API()}
}

func (m *machine) run(args ...string) result {
	m.t.Helper()
	var out, errw bytes.Buffer
	root := cli.New(m.a, m.hooks, strings.NewReader(m.stdin))
	code := root.Execute(context.Background(), args, &out, &errw)
	return result{code, out.String(), errw.String()}
}

// ok runs a command that must succeed and returns its output.
func (m *machine) ok(args ...string) string {
	m.t.Helper()
	r := m.run(args...)
	if r.code != 0 {
		m.t.Fatalf("%v: exit %d\nstdout: %s\nstderr: %s", args, r.code, r.out, r.err)
	}
	return r.out
}

func (m *machine) json(args ...string) map[string]any {
	m.t.Helper()
	var v map[string]any
	if err := json.Unmarshal([]byte(m.ok(append([]string{"--json"}, args...)...)), &v); err != nil {
		m.t.Fatalf("%v: %v", args, err)
	}
	return v
}

// every command line of the table must print what it should and exit as it should.
func TestHumanOutput(t *testing.T) {
	m := newMachine(t)
	for _, tc := range []struct {
		args []string
		want string
	}{
		{[]string{"sensors"}, "CPU 50 C   GPU 48 C   board 52 C   fans 0 / 0 rpm"},
		{[]string{"power"}, "power mode: eco"},
		{[]string{"power", "list"}, "quiet  balanced  performance  turbo  eco"},
		{[]string{"power", "set", "turbo"}, "power mode: turbo   (held 30 min against the automatic profile; `truesense power release` to end)"},
		{[]string{"fan", "status"}, "fans: auto   CPU 0 rpm   GPU 0 rpm   smart curve: unknown"},
		{[]string{"fan", "set", "50"}, "fans: manual 50 % / 50 %"},
		{[]string{"fan", "auto"}, "fans: firmware auto (they stop when cool)"},
		{[]string{"fan", "max"}, "fans: MAX  (`truesense fan smart on` hands them back)"},
		{[]string{"battery", "limit"}, "80 % battery limit: off"},
		{[]string{"battery", "limit", "toggle"}, "80 % battery limit: on"},
		{[]string{"battery", "calibration", "on"}, "battery calibration (needs AC, takes hours): on"},
		{[]string{"battery", "usb-charge", "20"}, "USB charging while off, stop level: 20"},
		{[]string{"display", "overdrive", "off"}, "LCD overdrive: off"},
		{[]string{"boot", "sound", "on"}, "boot sound: on"},
		{[]string{"gpu"}, "GPU mode: nvidia"},
		{[]string{"gpu", "set", "auto"}, "GPU mode: auto   (takes effect after a reboot)"},
		{[]string{"keyboard", "brightness"}, "keyboard brightness: 25 / 50"},
		{[]string{"keyboard", "brightness", "30"}, "keyboard brightness: 30 / 50"},
		{[]string{"keyboard", "timeout", "off"}, "backlight timeout (off after 30 s): off"},
		{[]string{"keyboard", "paint", "w=ff0000", "esc=00ff00"}, "painted 2 key(s)"},
		{[]string{"keyboard", "color", "ff0000"}, "sent"},
		{[]string{"look", "list"}, "native"},
		{[]string{"config", "validate"}, "configuration valid"},
		{[]string{"startup", "set", "rear", "off"}, "at login, rear: off"},
		{[]string{"touchpad", "status"}, "touchpad: unknown"},
	} {
		if tc.args[0] == "keyboard" && tc.args[1] == "color" {
			m.ok("keyboard", "effect", "static", "--color", "00ff00")
		}
		if got := strings.TrimSpace(m.ok(tc.args...)); got != tc.want {
			t.Errorf("%v:\n got %q\nwant %q", tc.args, got, tc.want)
		}
	}
}

func TestJSONOutputKeepsTheOldFieldNames(t *testing.T) {
	m := newMachine(t)
	for _, tc := range []struct {
		args []string
		keys []string
	}{
		{[]string{"status"}, []string{"power_mode", "sensors", "fans", "daemon", "battery_limit", "usb_charge_stop", "overdrive", "boot_sound", "kb_backlight_timeout", "gpu_mode", "rear", "touchpad", "audio_output", "jack", "voice"}},
		{[]string{"sensors"}, []string{"cpu_temp", "sys_temp", "gpu_temp", "cpu_fan_rpm", "gpu_fan_rpm"}},
		{[]string{"power", "status"}, []string{"mode", "supported"}},
		{[]string{"fan", "status"}, []string{"behavior_raw", "mode", "duty_register_pct", "cpu_fan_rpm", "gpu_fan_rpm", "smart"}},
		{[]string{"battery", "status"}, []string{"limit_80", "calibration", "usb_charge_stop"}},
		{[]string{"rear", "status"}, []string{"raw", "mode", "speed", "brightness", "direction", "color"}},
		{[]string{"keyboard", "status"}, []string{"look", "brightness", "max", "backlight_timeout", "lighting_profiles"}},
		{[]string{"keyboard", "list-keys"}, []string{"keys", "groups"}},
		{[]string{"keys", "show"}, []string{"profile", "tap", "double", "triple", "long", "fn_esc", "long_ms", "multi_ms", "power_cycle"}},
		{[]string{"startup", "show"}, []string{"profile", "keyboard", "rear"}},
		{[]string{"cpu", "info"}, []string{"pl1_w", "pl2_w", "rapl_enabled", "msr_lock_bit", "nvidia_power_limit_w"}},
		{[]string{"version"}, []string{"truesense"}},
	} {
		got := m.json(tc.args...)
		for _, k := range tc.keys {
			if _, ok := got[k]; !ok {
				t.Errorf("%v: key %q missing in %v", tc.args, k, got)
			}
		}
	}
}

func TestOnOffTogglePattern(t *testing.T) {
	m := newMachine(t)
	if out := m.ok("--json", "battery", "limit", "on"); strings.TrimSpace(out) != "true" {
		t.Fatalf("%q", out)
	}
	if out := m.ok("battery", "limit", "toggle"); strings.TrimSpace(out) != "80 % battery limit: off" {
		t.Fatalf("%q", out)
	}
	if out := m.ok("--json", "battery", "limit"); strings.TrimSpace(out) != "false" {
		t.Fatalf("%q", out)
	}
	if r := m.run("battery", "limit", "maybe"); r.code != command.ExitUsage || !strings.Contains(r.err, "argument action: invalid choice: 'maybe' (choose from 'on', 'off', 'toggle', 'status')") {
		t.Fatalf("%+v", r)
	}
	if r := m.run("battery", "limit", "on", "off"); r.code != command.ExitUsage || !strings.Contains(r.err, "unrecognized arguments: off") {
		t.Fatalf("%+v", r)
	}
	// keyboard operating has no toggle
	if r := m.run("keyboard", "operating", "toggle"); r.code != command.ExitUsage {
		t.Fatalf("%+v", r)
	}
}

func TestExitCodes(t *testing.T) {
	m := newMachine(t)
	for _, tc := range []struct {
		args []string
		code int
		in   string
	}{
		{[]string{"power", "set", "ludicrous"}, 2, "invalid choice"},
		{[]string{"power", "set"}, 2, "the following arguments are required: mode"},
		{[]string{"nosuch"}, 2, "unknown command"},
		{[]string{"power", "stauts"}, 2, `did you mean "status"`},
		{[]string{"fan", "set", "x"}, 2, "invalid int value: 'x'"},
		{[]string{"fan", "set", "10"}, 1, "error: manual fan duty is 20..100"},
		{[]string{"fan", "off"}, 1, "error: turning the fans off needs the TrueSense service"},
		{[]string{"power", "auto"}, 1, "error: the TrueSense daemon is not running"},
		{[]string{"look", "show", "nope"}, 1, "error:"},
		{[]string{"rear", "set", "wave", "--direction", "7"}, 2, "invalid choice: '7' (choose from '0', '1')"},
		{[]string{"audio"}, 1, "error: choose: output | mic | jack"},
		{[]string{"gui", "--page", "nope"}, 2, "invalid choice"},
		{[]string{"gui"}, 1, "the window is not built into this binary"},
		{[]string{"tui"}, 1, "the terminal UI is not built into this binary"},
		{[]string{"power", "game", "add", "x.exe"}, 1, "usage: truesense power game add PROGRAM MODE"},
	} {
		r := m.run(tc.args...)
		if r.code != tc.code || !strings.Contains(r.err, tc.in) {
			t.Errorf("%v: exit %d, stderr %q; want exit %d with %q", tc.args, r.code, r.err, tc.code, tc.in)
		}
	}
	if r := m.run(); r.code != 2 || !strings.Contains(r.out, "areas:") {
		t.Fatalf("no arguments shows the help and exits 2: %+v", r)
	}
	if r := m.run("--version"); r.code != 0 || !regexp.MustCompile(`^truesense \d+\.\d+\.\d+\n$`).MatchString(r.out) {
		t.Fatalf("%+v", r)
	}
}

func TestEverySubcommandHasHelp(t *testing.T) {
	m := newMachine(t)
	help := m.ok("--help")
	for _, area := range []string{"status", "sensors", "power", "fan", "battery", "display", "boot", "gpu", "rear", "update", "look", "startup", "keyboard", "audio", "keys", "profile", "cpu", "touchpad", "service", "config", "doctor", "selftest", "completion", "gui", "probe", "tui"} {
		if !regexp.MustCompile(`(?m)^  ` + area + ` `).MatchString(help) {
			t.Errorf("area %q is not in the help", area)
		}
		r := m.run(area, "-h")
		if r.code != 0 || !strings.Contains(r.out, "usage: truesense "+area) {
			t.Errorf("%s -h: %+v", area, r)
		}
	}
	for _, hidden := range []string{"daemon", "version"} {
		if regexp.MustCompile(`(?m)^  ` + hidden + ` `).MatchString(help) {
			t.Errorf("%q must stay out of the help", hidden)
		}
	}
	if r := m.run("power", "set", "-h"); r.code != 0 || !strings.Contains(r.out, "usage: truesense power set MODE") {
		t.Fatalf("%+v", r)
	}
}

// The systemd units call these exact command lines.
func TestUnitCommandsExist(t *testing.T) {
	m := newMachine(t)
	root := cli.New(m.a, m.hooks, strings.NewReader(""))
	files, _ := filepath.Glob("../../packaging/*.service")
	if len(files) == 0 {
		t.Skip("no unit files")
	}
	re := regexp.MustCompile(`(?m)^Exec\w+=-?@PREFIX@/bin/truesense (.+)$`)
	found := 0
	for _, f := range files {
		b, err := os.ReadFile(f)
		if err != nil {
			t.Fatal(err)
		}
		for _, mt := range re.FindAllStringSubmatch(string(b), -1) {
			found++
			words := strings.Fields(mt[1])
			// "-h" on the command resolves it without running it
			var out, errw bytes.Buffer
			if code := root.Execute(context.Background(), append(words, "-h"), &out, &errw); code != 0 {
				t.Errorf("%s: `truesense %s` does not exist: %s", filepath.Base(f), mt[1], errw.String())
			}
		}
	}
	if found < 5 {
		t.Fatalf("found only %d commands in the unit files", found)
	}
}

func TestCompletionIsGeneratedForEveryShell(t *testing.T) {
	m := newMachine(t)
	bash := m.ok("completion", "bash")
	for _, w := range []string{"power", "keyboard", "turbo", "wave", "usb-charge", "--confirm"} {
		if !strings.Contains(bash, w) {
			t.Errorf("bash completion lacks %q", w)
		}
	}
	for _, hidden := range []string{"auto-run", "daemon"} {
		if strings.Contains(bash, hidden) {
			t.Errorf("hidden command %q is completed", hidden)
		}
	}
	if !strings.Contains(m.ok("completion", "zsh"), "bashcompinit") || !strings.Contains(m.ok("completion", "fish"), "complete -c truesense") {
		t.Fatal("zsh or fish")
	}
	if r := m.run("completion", "tcsh"); r.code != 2 {
		t.Fatalf("%+v", r)
	}
}

func TestUpdateApplyAsksFirst(t *testing.T) {
	m := newMachine(t)
	srv := releaseServer(t, "v0.4.0")
	m.a.Updater = updaterFor(m.f, srv, "0.3.17")
	m.stdin = "n\n"
	r := m.run("update", "apply")
	if r.code != 1 || !strings.Contains(r.err, "error: cancelled") || !strings.Contains(r.out, "Install TrueSense 0.4.0 (you have 0.3.17)?") || !strings.Contains(r.out, "Continue? [y/N] ") {
		t.Fatalf("%+v", r)
	}
	m.stdin = ""
	if r := m.run("update", "apply"); r.code != 1 || !strings.Contains(r.err, "cancelled") {
		t.Fatalf("no answer is no: %+v", r)
	}
	m.a.Updater = updaterFor(m.f, srv, "0.4.0")
	if out := m.ok("update", "apply"); !strings.Contains(out, "already the newest version") {
		t.Fatalf("%q", out)
	}
	m.a.Updater = updaterFor(m.f, srv, "0.3.17")
	if out := strings.TrimSpace(m.ok("update", "check")); out != "TrueSense 0.3.17: update 0.4.0 is available   (truesense update apply)" {
		t.Fatalf("%q", out)
	}
}

func TestUpdateAutoRunIsQuiet(t *testing.T) {
	m := newMachine(t)
	m.a.Updater = updaterFor(m.f, releaseServer(t, "v0.4.0"), "0.3.17")
	if out := m.ok("update", "auto", "on"); !strings.Contains(out, "check for updates once a day: on") {
		t.Fatal(out)
	}
	r := m.run("update", "auto-run")
	if r.code != 0 || r.out != "" || r.err != "" {
		t.Fatalf("%+v", r)
	}
	n := 0
	for _, c := range m.f.Notified() {
		if strings.HasPrefix(c, "notify-send") {
			n++
		}
	}
	if n != 1 {
		t.Fatalf("%v", m.f.Notified())
	}
}

func TestFanStopThroughTheService(t *testing.T) {
	m := newMachine(t)
	d := m.f.StartDaemon()
	if r := m.run("fan", "set", "0", "40"); r.code != 1 || !strings.Contains(r.err, "stopping a fan needs explicit confirmation") {
		t.Fatalf("%+v", r)
	}
	if _, reached := d.Last("fan"); reached {
		t.Fatal("an unconfirmed stop must not reach the service")
	}
	m.ok("fan", "set", "0", "40", "--confirm")
	if c, _ := d.Last("fan"); c.Args["confirm"] != true || c.Args["cpu"] != float64(0) {
		t.Fatalf("%+v", c)
	}
	out := m.ok("fan", "set", "0", "--confirm")
	if !strings.Contains(out, "a stopped fan: all fans return to Auto at CPU 70 C / GPU 65 C") {
		t.Fatal(out)
	}
	if !strings.Contains(m.ok("fan", "off", "--confirm"), "fans: OFF") {
		t.Fatal("off")
	}
}

func TestServiceRepairSystemPrintsOneJSONLine(t *testing.T) {
	m := newMachine(t)
	m.a.EUID = 0
	r := m.run("service", "repair", "--system")
	lines := strings.Split(strings.TrimSpace(r.out), "\n")
	var steps [][]any
	if len(lines) != 1 || json.Unmarshal([]byte(lines[0]), &steps) != nil || len(steps) == 0 || len(steps[0]) != 2 {
		t.Fatalf("%+v", r)
	}
	m.a.EUID = 1000
	if r := m.run("service", "repair", "--system"); r.code != 1 || !strings.Contains(r.err, "the system part needs root") {
		t.Fatalf("%+v", r)
	}
}

func TestServiceCheckExitsWithTheResult(t *testing.T) {
	m := newMachine(t)
	r := m.run("service", "check")
	if r.code != 1 || !strings.Contains(r.out, "something is not running: truesense service repair") || r.err != "" {
		t.Fatalf("%+v", r)
	}
	var rows []map[string]any
	if err := json.Unmarshal([]byte(m.run("--json", "service", "check").out), &rows); err != nil || len(rows) == 0 || rows[0]["unit"] == nil {
		t.Fatal(err, rows)
	}
}

func TestGuiAndTuiHooks(t *testing.T) {
	m := newMachine(t)
	var gotGUI, gotTUI string
	m.hooks = cli.Hooks{
		GUI: func(_ context.Context, page string) error { gotGUI = page; return nil },
		TUI: func(_ context.Context, page string) error { gotTUI = page; return errors.New("boom") },
	}
	m.ok("gui", "--page", "keys")
	if gotGUI != "predator" { // the Qt window called the PREDATOR key page "keys"
		t.Fatal(gotGUI)
	}
	if r := m.run("tui", "--page", "fans"); r.code != 1 || gotTUI != "fans" || !strings.Contains(r.err, "error: boom") {
		t.Fatalf("%+v %q", r, gotTUI)
	}
}

func TestNegativeUndervoltOffsetIsAPositional(t *testing.T) {
	m := newMachine(t)
	d := m.f.StartDaemon()
	d.Reply("undervolt", map[string]any{"core_mv": -10, "cache_mv": 0, "applied": nil, "boot": nil, "pending": false})
	out := m.ok("cpu", "undervolt", "set", "-10", "--confirm")
	if !strings.Contains(out, "core -10 mV, cache 0 mV") {
		t.Fatal(out)
	}
	if c, _ := d.Last("undervolt"); c.Args["core"] != float64(-10) || c.Args["confirm"] != true {
		t.Fatalf("%+v", c)
	}
	if r := m.run("cpu", "undervolt", "set"); r.code != 1 || !strings.Contains(r.err, "usage: truesense cpu undervolt set -10 --confirm") {
		t.Fatalf("%+v", r)
	}
}
