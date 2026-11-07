package actions

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/Order52/truesense/internal/hw"
)

func TestValidate(t *testing.T) {
	good := []string{"none", "power-next", "power:turbo", "power-cycle:eco,turbo", "fan:smart", "fan:60", "fan:max", "profile:gaming", "profile-next", "output-toggle", "output:speakers",
		"mic-toggle", "mic:mute", "voice-next", "touchpad-toggle", "touchpad:off", "keyboard:toggle", "keyboard-effect-next", "keyboard-profile:x", "rear:off", "rear-effect-next",
		"battery-limit-toggle", "overdrive-toggle", "exec:echo hi", "notify:hello", "auto:smart", "auto-next", "look:mine", "exec: a:b:c ", "smart-fan-toggle", "output-cycle", "fan-max-toggle",
		"power-prev", "fan:auto", "fan:20", "fan:100", "  power:eco  "}
	v := Validator{LookNames: func() []string { return []string{"mine"} }}
	for _, g := range good {
		if err := v.Validate(g); err != nil {
			t.Errorf("%q should be valid: %v", g, err)
		}
	}
	bad := map[string]string{
		"": "unknown action ''", "power": "needs an argument", "power:ludicrous": "power mode must be one of quiet, balanced, performance, turbo, eco", "fan:5": "fan:",
		"fan:101": "fan:", "fan:+60": "fan:", "fan:6 0": "fan:", "output:usb": "output:", "mic:x": "mic:", "touchpad:toggle": "touchpad:", "rear:dim": "rear: on | off | toggle",
		"keyboard:dim": "keyboard: on", "exec": "needs an argument", "frobnicate": "unknown action 'frobnicate'", "power-next:3": "takes no argument", "none:x": "takes no argument",
		"auto:on": "auto: off | native | smart", "look:gone": "no saved look 'gone'", "power-cycle:eco,,turbo": "comma list", "power-cycle:eco,ludicrous": "comma list", "notify": "needs an argument",
	}
	for spec, want := range bad {
		err := v.Validate(spec)
		if err == nil || !strings.Contains(err.Error(), want) {
			t.Errorf("%q: got %v, want it to contain %q", spec, err, want)
		}
	}
	if err := Validate("look:anything"); err != nil {
		t.Errorf("syntax-only Validate must not consult looks: %v", err)
	}
}

func TestSplit(t *testing.T) {
	for in, want := range map[string][2]string{
		"power:turbo": {"power", "turbo"}, "  exec : sh -c 'a:b' ": {"exec", "sh -c 'a:b'"}, "none": {"none", ""}, "": {"", ""}, ":x": {"", "x"},
	} {
		if n, a := Split(in); n != want[0] || a != want[1] {
			t.Errorf("Split(%q) = %q, %q; want %q, %q", in, n, a, want[0], want[1])
		}
	}
}

func TestCatalogueCoversEveryActionName(t *testing.T) {
	text := Catalogue()
	for _, n := range Names() {
		if !strings.Contains(text, n) {
			t.Errorf("%s is missing from the documented catalogue", n)
		}
	}
	if !strings.HasPrefix(text, "An action is `name` or `name:argument`.") || !strings.HasSuffix(text, "notify:<text>") {
		t.Errorf("catalogue boundaries changed: %q ... %q", text[:30], text[len(text)-30:])
	}
}

type step struct {
	name  string
	setup func(*world)
	spec  string
	msg   string
	log   []string
}

func runSteps(t *testing.T, steps []step) {
	t.Helper()
	for _, s := range steps {
		t.Run(s.name, func(t *testing.T) {
			w := newWorld()
			if s.setup != nil {
				s.setup(w)
			}
			msg, err := New(w.env()).Run(context.Background(), s.spec)
			if err != nil {
				t.Fatalf("Run(%q): %v", s.spec, err)
			}
			if msg != s.msg {
				t.Errorf("message %q, want %q", msg, s.msg)
			}
			if !reflect.DeepEqual(w.log, s.log) {
				t.Errorf("calls\n got %q\nwant %q", w.log, s.log)
			}
		})
	}
}

func TestPowerActions(t *testing.T) {
	runSteps(t, []step{
		{"next from eco", func(w *world) { w.mode = "eco" }, "power-next", "Power mode: quiet", []string{"power quiet hold=10"}},
		{"next wraps", func(w *world) { w.mode = "turbo" }, "power-next", "Power mode: eco", []string{"power eco hold=10"}},
		{"prev from eco wraps to turbo", func(w *world) { w.mode = "eco" }, "power-prev", "Power mode: turbo", []string{"power turbo hold=10"}},
		{"prev", func(w *world) { w.mode = "balanced" }, "power-prev", "Power mode: quiet", []string{"power quiet hold=10"}},
		{"explicit", nil, "power:turbo", "Power mode: turbo", []string{"power turbo hold=10"}},
		{"hold follows the config", func(w *world) { w.cfg.Profile.HoldMinutes = 45 }, "power:eco", "Power mode: eco", []string{"power eco hold=45"}},
		{"cycle", func(w *world) { w.mode = "performance" }, "power-cycle:performance,turbo", "Power mode: turbo", []string{"power turbo hold=10"}},
		{"cycle back", func(w *world) { w.mode = "turbo" }, "power-cycle:performance,turbo", "Power mode: performance", []string{"power performance hold=10"}},
		{"cycle from outside the list starts at its first", func(w *world) { w.mode = "eco" }, "power-cycle:performance,turbo", "Power mode: performance", []string{"power performance hold=10"}},
		{"prev from outside the list is one before the end", func(w *world) { w.cfg.Keys.PowerCycle = []string{"quiet", "balanced", "turbo"}; w.mode = "eco" }, "power-prev", "Power mode: balanced", []string{"power balanced hold=10"}},
		{"unsupported modes are skipped", func(w *world) { w.supported = []string{"balanced", "turbo"}; w.mode = "balanced" }, "power-next", "Power mode: turbo", []string{"power turbo hold=10"}},
		{"nothing supported falls back to eco balanced turbo", func(w *world) { w.supported = nil; w.mode = "balanced" }, "power-next", "Power mode: turbo", []string{"power turbo hold=10"}},
		{"configured cycle", func(w *world) { w.cfg.Keys.PowerCycle = []string{"quiet", "turbo"}; w.mode = "quiet" }, "power-next", "Power mode: turbo", []string{"power turbo hold=10"}},
	})
}

func TestFanActions(t *testing.T) {
	runSteps(t, []step{
		{"smart", nil, "fan:smart", "Fans: smart curve", []string{"fan smart"}},
		{"auto", nil, "fan:auto", "Fans: firmware auto", []string{"fan auto"}},
		{"max", nil, "fan:max", "Fans: MAX", []string{"fan max"}},
		{"percent", nil, "fan:60", "Fans: 60 %", []string{"fan manual cpu=60 gpu=60"}},
		{"max toggle on", func(w *world) { w.fan.Mode = "auto" }, "fan-max-toggle", "Fans: MAX", []string{"fan max"}},
		{"max toggle off", func(w *world) { w.fan.Mode = "max" }, "fan-max-toggle", "Fans: smart curve", []string{"fan smart"}},
		{"smart toggle on", nil, "smart-fan-toggle", "Fans: smart curve", []string{"daemon state", "daemon set smart_fan=true"}},
		{"smart toggle off", func(w *world) { w.daemonSet["smart_fan"] = true }, "smart-fan-toggle", "Fans: smart curve OFF", []string{"daemon state", "daemon set smart_fan=false"}},
	})
}

func TestFanZeroIsNeverSentByAKey(t *testing.T) {
	for _, spec := range []string{"fan:0", "fan:19", "fan:-20", "fan:"} {
		if err := Validate(spec); err == nil {
			t.Errorf("%q must be refused: a key never stops or starves a fan", spec)
		}
	}
}

func TestSmartFanToggleNeedsTheDaemon(t *testing.T) {
	w := newWorld()
	w.daemonUp = false
	_, err := New(w.env()).Run(context.Background(), "smart-fan-toggle")
	if err == nil || err.Error() != "the TrueSense daemon is not running" {
		t.Fatalf("got %v", err)
	}
}

func TestModeControlActions(t *testing.T) {
	runSteps(t, []step{
		{"auto set", nil, "auto:smart", "Mode control: smart", []string{"daemon set auto_mode=smart"}},
		{"next from off", nil, "auto-next", "Mode control: native", []string{"daemon state", "daemon set auto_mode=native"}},
		{"next from native", func(w *world) { w.daemonSet["auto_mode"] = "native" }, "auto-next", "Mode control: smart", []string{"daemon state", "daemon set auto_mode=smart"}},
		{"next from smart", func(w *world) { w.daemonSet["auto_mode"] = "smart" }, "auto-next", "Mode control: off", []string{"daemon state", "daemon set auto_mode=off"}},
		{"next from an unknown value", func(w *world) { w.daemonSet["auto_mode"] = "weird" }, "auto-next", "Mode control: smart", []string{"daemon state", "daemon set auto_mode=smart"}},
		{"next with no setting means off", func(w *world) { delete(w.daemonSet, "auto_mode") }, "auto-next", "Mode control: native", []string{"daemon state", "daemon set auto_mode=native"}},
	})
}

func TestProfileLookNotifyNone(t *testing.T) {
	runSteps(t, []step{
		{"profile", nil, "profile:gaming", "applied gaming", []string{"profile gaming"}},
		{"profile next", nil, "profile-next", "applied quiet", []string{"profile quiet"}},
		{"look", nil, "look:mine", "Look: mine", []string{"look load mine", "look apply"}},
		{"notify", nil, "notify:hello: world", "hello: world", nil},
		{"none", nil, "none", "", nil},
		{"exec", nil, "exec:truesense gui", "", []string{"exec truesense gui"}},
	})
}

func TestAudioActions(t *testing.T) {
	runSteps(t, []step{
		{"output toggle shows its own message", nil, "output-toggle", "", []string{"audio output-toggle"}},
		{"output cycle", nil, "output-cycle", "", []string{"audio output-cycle"}},
		{"output speakers", nil, "output:speakers", "Audio output: Laptop speakers", []string{"audio output speakers"}},
		{"output headphones", nil, "output:headphones", "Audio output: Headphones (3.5 mm jack)", []string{"audio output headphones"}},
		{"mic toggle mutes", nil, "mic-toggle", "Microphones: MUTED", []string{"audio mic-toggle defaultOnly=false"}},
		{"mic toggle unmutes", func(w *world) { w.micMuted = true }, "mic-toggle", "Microphones: live", []string{"audio mic-toggle defaultOnly=false"}},
		{"mic mute", nil, "mic:mute", "Microphones: MUTED", []string{"audio mic true"}},
		{"mic unmute", nil, "mic:unmute", "Microphones: live", []string{"audio mic false"}},
		{"voice next", nil, "voice-next", "Purified Voice (headset): Conference Call", []string{`audio voice-next dev=""`}},
	})
}

func TestTouchpadActions(t *testing.T) {
	runSteps(t, []step{
		{"toggle off through the daemon", nil, "touchpad-toggle", "Touchpad: off", []string{"daemon touchpad toggle"}},
		{"toggle on", func(w *world) { w.touch = false }, "touchpad-toggle", "Touchpad: on", []string{"daemon touchpad toggle"}},
		{"on", func(w *world) { w.touch = false }, "touchpad:on", "Touchpad: on", []string{"daemon touchpad on"}},
		{"off", nil, "touchpad:off", "Touchpad: off", []string{"daemon touchpad off"}},
		{"root without a daemon does it itself", func(w *world) { w.daemonUp = false; w.root = true }, "touchpad-toggle", "Touchpad: off", []string{"touchpad direct false"}},
		{"root without a daemon, explicit on", func(w *world) { w.daemonUp = false; w.root = true; w.touch = false }, "touchpad:on", "Touchpad: on", []string{"touchpad direct true"}},
	})
	w := newWorld()
	w.daemonUp = false
	if _, err := New(w.env()).Run(context.Background(), "touchpad-toggle"); err == nil {
		t.Error("a user without the daemon cannot switch the touchpad: the error must surface")
	}
}

func TestKeyboardActions(t *testing.T) {
	runSteps(t, []step{
		{"off", nil, "keyboard:off", "Keyboard light: off", []string{"kb off", "state keyboard_on=false"}},
		{"toggle from on goes off", nil, "keyboard:toggle", "Keyboard light: off", []string{"kb off", "state keyboard_on=false"}},
		{"toggle from off with a remembered look turns it back on", func(w *world) {
			w.state["keyboard_on"] = false
			w.state["kb_restore"] = map[string]any{"mode": "effect"}
		}, "keyboard:toggle", "Keyboard light: on", []string{"kb on", "state keyboard_on=true"}},
		{"on without memory restores the stock look", func(w *world) { w.state["keyboard_on"] = false }, "keyboard:on", "Keyboard light: on", []string{"kb restore-default", "state keyboard_on=true"}},
		{"on without memory uses the remembered profile", func(w *world) {
			w.state["keyboard_profile"] = "mine"
			w.kbProfiles["mine"] = true
		}, "keyboard:on", "Keyboard light: on", []string{"kbprofile mine b=-1", "state keyboard_on=true"}},
		{"on with a forgotten profile falls back to the stock look", func(w *world) { w.state["keyboard_profile"] = "gone" }, "keyboard:on", "Keyboard light: on", []string{"kb restore-default", "state keyboard_on=true"}},
		{"profile", nil, "keyboard-profile:mine", "Keyboard profile: mine", []string{"kbprofile mine b=-1", "state keyboard_profile=mine", "state keyboard_on=true"}},
	})
}

func TestKeyboardEffectNext(t *testing.T) {
	runSteps(t, []step{
		{"first effect, no brightness read means 30", nil, "keyboard-effect-next", "Keyboard effect: static",
			[]string{"state kb_effect=0", "kb effect static 00aec7 s=5 b=30 d=0", "state keyboard_on=true"}},
		{"keeps the keyboard's own brightness", func(w *world) { w.bright = 42; w.state["kb_effect"] = 1.0 }, "keyboard-effect-next", "Keyboard effect: wave",
			[]string{"state kb_effect=2", "kb effect wave 00aec7 s=5 b=42 d=0", "state keyboard_on=true"}},
		{"neon is never dimmer than 30", func(w *world) { w.bright = 12; w.state["kb_effect"] = 4.0 }, "keyboard-effect-next", "Keyboard effect: neon",
			[]string{"state kb_effect=5", "kb effect neon 00aec7 s=5 b=30 d=0", "state keyboard_on=true"}},
		{"other effects keep a dim level", func(w *world) { w.bright = 12; w.state["kb_effect"] = 5.0 }, "keyboard-effect-next", "Keyboard effect: raindrop",
			[]string{"state kb_effect=6", "kb effect raindrop 00aec7 s=5 b=12 d=0", "state keyboard_on=true"}},
		{"wraps", func(w *world) { w.state["kb_effect"] = 12.0 }, "keyboard-effect-next", "Keyboard effect: static",
			[]string{"state kb_effect=0", "kb effect static 00aec7 s=5 b=30 d=0", "state keyboard_on=true"}},
	})
}

func TestRearActions(t *testing.T) {
	runSteps(t, []step{
		{"toggle off remembers the look and keeps the zones", func(w *world) { w.state["rear_zones"] = []any{"ff0000", "00ff00", "0000ff"} }, "rear:toggle", "Rear light: off",
			[]string{"state rear_last=map[brightness:100 color:112233 direction:1 mode:static speed:3]", "rear static 112233 s=3 b=0 d=1 zones=[ff0000 00ff00 0000ff] verify=true"}},
		{"off in a moving effect drops no zones", func(w *world) {
			w.rear.Mode = "wave"
			w.state["rear_zones"] = []any{"a", "b", "c"}
		}, "rear:off", "Rear light: off",
			[]string{"state rear_last=map[brightness:100 color:112233 direction:1 mode:wave speed:3]", "rear wave 112233 s=3 b=0 d=1 zones=[] verify=true"}},
		{"on after off restores what was remembered", func(w *world) {
			w.rear.Brightness = 0
			w.state["rear_last"] = map[string]any{"mode": "breathing", "color": "ff0000", "speed": 4.0, "brightness": 70.0, "direction": 0.0}
		}, "rear:on", "Rear light: on", []string{"rear breathing ff0000 s=4 b=70 d=0 zones=[] verify=true"}},
		{"on with nothing remembered uses the stock look", func(w *world) { w.rear.Brightness = 0 }, "rear:on", "Rear light: on",
			[]string{"rear static 00aec7 s=3 b=100 d=1 zones=[] verify=true"}},
		{"on with a remembered zero brightness uses 100", func(w *world) {
			w.rear.Brightness = 0
			w.state["rear_last"] = map[string]any{"mode": "static", "color": "ff0000", "speed": 3.0, "brightness": 0.0, "direction": 1.0}
		}, "rear:on", "Rear light: on", []string{"rear static ff0000 s=3 b=100 d=1 zones=[] verify=true"}},
		{"on with an unknown remembered effect uses static", func(w *world) {
			w.rear.Brightness = 0
			w.state["rear_last"] = map[string]any{"mode": "disco", "color": "ff0000", "speed": 3.0, "brightness": 50.0, "direction": 1.0}
		}, "rear:on", "Rear light: on", []string{"rear static ff0000 s=3 b=50 d=1 zones=[] verify=true"}},
		{"on restores the three part colours of the static effect", func(w *world) {
			w.rear.Brightness = 0
			w.state["rear_zones"] = []any{"111111", "222222", "333333"}
			w.state["rear_last"] = map[string]any{"mode": "static", "color": "111111", "speed": 3.0, "brightness": 80.0, "direction": 1.0}
		}, "rear:on", "Rear light: on", []string{"rear static 111111 s=3 b=80 d=1 zones=[111111 222222 333333] verify=true"}},
		{"zones of the wrong length are ignored", func(w *world) {
			w.rear.Brightness = 0
			w.state["rear_zones"] = []any{"111111"}
		}, "rear:on", "Rear light: on", []string{"rear static 00aec7 s=3 b=100 d=1 zones=[] verify=true"}},
		{"already on, rear:on re-sends it", nil, "rear:on", "Rear light: on", []string{"rear static 00aec7 s=3 b=100 d=1 zones=[] verify=true"}},
	})
}

func TestRearEffectNext(t *testing.T) {
	runSteps(t, []step{
		{"first", nil, "rear-effect-next", "Rear effect: static", []string{"state rear_effect=0", "rear static 112233 s=3 b=100 d=1 zones=[] verify=true"}},
		{"keeps colour, speed, direction and brightness", func(w *world) {
			w.state["rear_effect"] = 1.0
			w.rear = hw.Rear{Mode: "static", Color: "abcdef", Speed: 5, Brightness: 40, Direction: 0}
		}, "rear-effect-next", "Rear effect: neon", []string{"state rear_effect=2", "rear neon abcdef s=5 b=40 d=0 zones=[] verify=true"}},
		{"a dark bar comes on at 100", func(w *world) { w.rear.Brightness = 0; w.state["rear_effect"] = 3.0 }, "rear-effect-next", "Rear effect: twinkling",
			[]string{"state rear_effect=4", "rear twinkling 112233 s=3 b=100 d=1 zones=[] verify=true"}},
		{"wraps", func(w *world) { w.state["rear_effect"] = 4.0 }, "rear-effect-next", "Rear effect: static", []string{"state rear_effect=0", "rear static 112233 s=3 b=100 d=1 zones=[] verify=true"}},
	})
}

func TestBatteryAndOverdrive(t *testing.T) {
	runSteps(t, []step{
		{"battery limit on", nil, "battery-limit-toggle", "Battery 80 % limit: on", []string{"battery true"}},
		{"battery limit off", func(w *world) { w.battery = true }, "battery-limit-toggle", "Battery 80 % limit: off", []string{"battery false"}},
		{"overdrive on", nil, "overdrive-toggle", "LCD overdrive: on", []string{"overdrive true"}},
		{"overdrive off", func(w *world) { w.overdrive.on, w.overdrive.ok = true, true }, "overdrive-toggle", "LCD overdrive: off", []string{"overdrive false"}},
		{"unknown overdrive turns on", nil, "overdrive-toggle", "LCD overdrive: on", []string{"overdrive true"}},
	})
}

func TestPowerCycleAndBattery(t *testing.T) { // tests/test_core.py::test_power_cycle_and_battery
	w := newWorld()
	w.mode = "eco"
	r := New(w.env())
	ctx := context.Background()
	// In this order: each action starts from what the previous one left (a map would run them in random order).
	for _, step := range [][2]string{{"power-next", "quiet"}, {"power-prev", "eco"}, {"power:turbo", "turbo"}, {"battery-limit-toggle", "on"}} {
		spec, want := step[0], step[1]
		msg, err := r.Run(ctx, spec)
		if err != nil || !strings.Contains(msg, want) {
			t.Errorf("%s: %q, %v; want it to contain %q", spec, msg, err, want)
		}
	}
	if msg, _ := r.Run(ctx, "battery-limit-toggle"); !strings.Contains(msg, "off") {
		t.Errorf("second toggle: %q", msg)
	}
}

func TestRunRefusesInvalidAndStaleActions(t *testing.T) {
	w := newWorld()
	r := New(w.env())
	for _, spec := range []string{"", "bogus", "power:ludicrous", "look:deleted-since", "none:1"} {
		if _, err := r.Run(context.Background(), spec); err == nil {
			t.Errorf("%q must fail", spec)
		}
	}
	if len(w.log) != 0 {
		t.Errorf("an invalid action must touch nothing, did %q", w.log)
	}
}

func TestErrorsSurface(t *testing.T) {
	for _, tc := range []struct{ spec, fail string }{
		{"power:turbo", "power"}, {"fan:max", "fan"}, {"battery-limit-toggle", "battery"}, {"overdrive-toggle", "overdrive"}, {"rear:off", "rear"},
		{"keyboard-profile:x", "kbprofile"}, {"keyboard:off", "kb off"}, {"look:mine", "look load"}, {"output:speakers", "audio output"}, {"auto:off", "daemon set"},
		{"mic-toggle", "audio mic-toggle"}, {"voice-next", "audio voice-next"}, {"exec:x", "exec"}, {"profile:gaming", "profile"}, {"rear-effect-next", "rear"}, {"keyboard-effect-next", "kb effect"},
	} {
		w := newWorld()
		w.failOn = tc.fail
		msg, err := New(w.env()).Run(context.Background(), tc.spec)
		if err == nil || !strings.Contains(err.Error(), "boom") {
			t.Errorf("%s: want the failure surfaced, got %q, %v", tc.spec, msg, err)
		}
		if msg != "" {
			t.Errorf("%s: a failed action shows no success message, got %q", tc.spec, msg)
		}
	}
}

func TestConfigIsReadOnEveryRun(t *testing.T) {
	w := newWorld()
	r := New(w.env())
	w.cfg.Profile.HoldMinutes = 1
	_, _ = r.Run(context.Background(), "power:eco")
	w.cfg.Profile.HoldMinutes = 2
	_, _ = r.Run(context.Background(), "power:eco")
	if got := strings.Join(w.log, ";"); got != "power eco hold=1;power eco hold=2" {
		t.Errorf("hot-reloaded config not honoured: %s", got)
	}
}

func TestExecNotAvailable(t *testing.T) {
	w := newWorld()
	env := w.env()
	env.Exec = nil
	if _, err := New(env).Run(context.Background(), "exec:true"); err == nil {
		t.Error("exec without an Exec must be refused")
	}
}

func TestShellExecRunsDetachedThroughTheShell(t *testing.T) {
	dir := t.TempDir()
	out := filepath.Join(dir, "out")
	start := time.Now()
	// the command outlives the call (sleep) yet the call returns at once; the file proves the shell ran it, with its quoting and redirection
	if err := ShellExec(context.Background(), "echo 'a b' > '"+out+"'; sleep 2"); err != nil {
		t.Fatal(err)
	}
	if time.Since(start) > time.Second {
		t.Error("ShellExec must not wait for the command")
	}
	deadline := time.Now().Add(3 * time.Second)
	for {
		b, err := os.ReadFile(out)
		if err == nil && string(b) == "a b\n" {
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("command did not run: %q, %v", b, err)
		}
		time.Sleep(10 * time.Millisecond)
	}
}

func TestShellExecSurvivesACancelledContext(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	dir := t.TempDir()
	out := filepath.Join(dir, "out")
	if err := ShellExec(ctx, "sleep 0.2; echo done > '"+out+"'"); err != nil {
		t.Fatal(err)
	}
	cancel() // the key service's per-action timeout must not kill a launched window
	deadline := time.Now().Add(3 * time.Second)
	for {
		if _, err := os.Stat(out); err == nil {
			return
		}
		if time.Now().After(deadline) {
			t.Fatal("the command was killed with the context")
		}
		time.Sleep(10 * time.Millisecond)
	}
}

func FuzzValidateAndSplit(f *testing.F) {
	for _, s := range []string{"", "none", "power:turbo", "power-cycle:eco,,turbo", "fan:60", "fan:+60", "exec:a:b", "look:x", ":", "::", "  ", "power-next:1", "fan:٦٠", "keyboard:toggle\x00"} {
		f.Add(s)
	}
	v := Validator{LookNames: func() []string { return []string{"x"} }}
	f.Fuzz(func(t *testing.T, spec string) {
		name, arg := Split(spec)
		if name != strings.TrimSpace(name) || arg != strings.TrimSpace(arg) || strings.Contains(name, ":") {
			t.Fatalf("Split(%q) = %q, %q", spec, name, arg)
		}
		err := v.Validate(spec)
		if err == nil && name != "none" {
			// an accepted action must run against the fakes without panicking
			w := newWorld()
			_, _ = New(w.env()).Run(context.Background(), spec)
			if strings.HasPrefix(name, "fan") && len(w.log) > 0 && strings.HasPrefix(w.log[0], "fan manual") {
				var cpu, gpu int
				_, _ = fmt.Sscanf(w.log[0], "fan manual cpu=%d gpu=%d", &cpu, &gpu)
				if !hw.ValidDuty(cpu) || cpu == 0 {
					t.Fatalf("%q sent duty %d", spec, cpu)
				}
			}
		}
	})
}
