package cli_test

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/Order52/truesense/internal/ipc"
)

func TestKeysCommands(t *testing.T) {
	m := newMachine(t)
	out := m.ok("keys", "show")
	if !strings.HasPrefix(out, "tap      power-next\n") || !strings.Contains(out, "(long press after 800 ms; taps within 450 ms count together)") {
		t.Fatalf("%q", out)
	}
	if out := m.ok("keys", "set", "double", "fan:max"); !strings.HasPrefix(out, "double -> fan:max   (the key service reloads by itself") {
		t.Fatal(out)
	}
	if r := m.run("keys", "set", "tap", "bogus"); r.code != 1 || !strings.Contains(r.err, "(truesense keys actions)") {
		t.Fatalf("%+v", r)
	}
	if r := m.run("keys", "set", "swipe", "fan:max"); r.code != 2 {
		t.Fatalf("%+v", r)
	}
	if out := m.ok("keys", "reset", "double"); strings.TrimSpace(out) != "reset: double" {
		t.Fatal(out)
	}
	if out := m.ok("keys", "timing"); !strings.Contains(out, `"long_ms": 800`) {
		t.Fatal(out)
	}
	if out := strings.TrimSpace(m.ok("keys", "timing", "--long-ms", "600")); out != "timing: {'long_ms': 600}" {
		t.Fatal(out)
	}
	if out := m.ok("keys", "profile", "save", "mine", "--tap", "power:turbo", "--long-ms", "700"); !strings.Contains(out, "saved key profile 'mine'") {
		t.Fatal(out)
	}
	out = m.ok("keys", "profile")
	if !strings.HasPrefix(out, "key profile in use: custom\n") || !strings.Contains(out, "  Tap         power-next") {
		t.Fatal(out)
	}
	if out := m.ok("keys", "profile", "use", "mine"); !strings.Contains(out, "key profile in use: mine") {
		t.Fatal(out)
	}
	list := m.ok("keys", "profile", "list")
	if !strings.Contains(list, "* mine") || !strings.Contains(list, "  default  - The standard behaviour") {
		t.Fatal(list)
	}
	if out := strings.TrimSpace(m.ok("keys", "test", "notify:hello")); out != "hello" {
		t.Fatal(out)
	}
	if out := strings.TrimSpace(m.ok("keys", "test", "none")); out != "ok" {
		t.Fatal(out)
	}
	if out := m.ok("keys", "actions"); !strings.Contains(out, "power-next") {
		t.Fatal(out)
	}
}

func TestProfileCommands(t *testing.T) {
	m := newMachine(t)
	if out := strings.TrimSpace(m.ok("profile", "list")); out != "(no profiles)" {
		t.Fatal(out)
	}
	if out := m.ok("profile", "save", "mine", "--description", "x"); !strings.Contains(out, "saved the current state as profile 'mine'") {
		t.Fatal(out)
	}
	if out := m.ok("profile"); !strings.HasPrefix(out, "mine             [  yours] x") {
		t.Fatalf("%q", out)
	}
	if out := m.ok("profile", "show", "mine"); !strings.Contains(out, `description = "x"`) {
		t.Fatal(out)
	}
	if out := m.ok("profile", "apply", "mine"); !strings.Contains(out, "Profile 'mine' applied") {
		t.Fatal(out)
	}
	if out := m.ok("--json", "profile", "list"); !strings.Contains(out, `"source": "yours"`) {
		t.Fatal(out)
	}
	if out := strings.TrimSpace(m.ok("profile", "delete", "mine")); out != "deleted mine" {
		t.Fatal(out)
	}
	if r := m.run("profile", "apply", "nope"); r.code != 1 {
		t.Fatalf("%+v", r)
	}
}

func TestStartupCommands(t *testing.T) {
	m := newMachine(t)
	if out := m.ok("startup", "set", "keyboard", "default"); !strings.Contains(out, "at login, keyboard: default") {
		t.Fatal(out)
	}
	if r := m.run("startup", "set", "keyboard", "profile:nope"); r.code != 1 {
		t.Fatalf("%+v", r)
	}
	if r := m.run("startup", "set", "motherboard", "x"); r.code != 2 {
		t.Fatalf("%+v", r)
	}
	if out := m.ok("startup", "apply"); !strings.Contains(out, "keyboard:") {
		t.Fatal(out)
	}
	d := m.f.StartDaemon()
	d.Reply("boot_look", nil)
	if out := strings.TrimSpace(m.ok("startup", "boot")); out != "no look is applied at boot" {
		t.Fatal(out)
	}
	d.Reply("boot_look", func(args ipc.Args) (any, error) { return args["look"], nil })
	if out := m.ok("startup", "boot", "native"); !strings.HasPrefix(out, "at boot and after suspend: ") {
		t.Fatal(out)
	}
	if out := strings.TrimSpace(m.ok("startup", "boot", "clear")); out != "nothing is applied at boot any more" {
		t.Fatal(out)
	}
}

func TestKeyboardCommands(t *testing.T) {
	m := newMachine(t)
	if out := m.ok("keyboard", "design", "list"); !strings.Contains(out, "gaming       built-in") {
		t.Fatal(out)
	}
	if out := strings.TrimSpace(m.ok("keyboard", "design", "apply", "ice")); out != "design ice applied" {
		t.Fatal(out)
	}
	if out := strings.TrimSpace(m.ok("keyboard", "fx", "wave", "--keys", "wasd")); !strings.HasPrefix(out, "per-key animation: wave on wasd (needs the key service") {
		t.Fatal(out)
	}
	if out := strings.TrimSpace(m.ok("keyboard", "fx", "off")); out != "per-key animation: off" {
		t.Fatal(out)
	}
	m.ok("keyboard", "effect", "breathing")
	if out := strings.TrimSpace(m.ok("keyboard", "speed", "4")); out != "keyboard effect speed: 4 / 12" {
		t.Fatal(out)
	}
	if out := strings.TrimSpace(m.ok("keyboard", "profile", "list")); out != "(no lighting profiles)" {
		t.Fatal(out)
	}
	if out := m.ok("keyboard", "profile", "save", "red", "fill", "ff0000"); !strings.Contains(out, "red") {
		t.Fatal(out)
	}
	if out := m.ok("--json", "keyboard", "profile", "list"); !strings.Contains(out, `"red"`) {
		t.Fatal(out)
	}
	if r := m.run("keyboard", "paint", "w"); r.code != 1 || !strings.Contains(r.err, "'w': use key=RRGGBB") {
		t.Fatalf("%+v", r)
	}
	if r := m.run("keyboard", "paint"); r.code != 2 {
		t.Fatalf("%+v", r)
	}
	if out := m.ok("keyboard", "off"); !strings.Contains(out, "Keyboard light: off") {
		t.Fatal(out)
	}
	if out := m.ok("keyboard", "toggle"); !strings.Contains(out, "Keyboard light: on") {
		t.Fatal(out)
	}
	if out := m.ok("rear", "off"); !strings.Contains(out, "Rear light: off") {
		t.Fatal(out)
	}
}

func TestPowerSubcommands(t *testing.T) {
	m := newMachine(t)
	if out := m.ok("power", "next"); !strings.HasPrefix(out, "Power mode: ") {
		t.Fatal(out)
	}
	if out := m.ok("power", "smart", "list"); !strings.Contains(out, "  native  - PredatorSense default") {
		t.Fatal(out)
	}
	if out := m.ok("power", "smart", "show", "silent"); !strings.HasPrefix(out, "silent\n  ac_idle = quiet\n") || !strings.Contains(out, "ac_turbo = False") {
		t.Fatal(out)
	}
	if out := m.ok("power", "smart", "save", "mine", "--set", "ac_busy=turbo"); !strings.Contains(out, "saved smart profile 'mine'") {
		t.Fatal(out)
	}
	if r := m.run("power", "smart", "save", "mine", "--set", "bogus=1"); r.code != 1 || !strings.Contains(r.err, "--set key=value, key one of: ac_idle") {
		t.Fatalf("%+v", r)
	}
	d := m.f.StartDaemon()
	d.Reply("game_rules", map[string]any{"rules": map[string]any{"b.exe": "turbo", "a.exe": "eco"}, "running": "turbo"})
	if out := m.ok("power", "game"); out != "  a.exe                        eco\n  b.exe                        turbo\n  running now: turbo mode\n" {
		t.Fatalf("%q", out)
	}
	d.Reply("power_managers", map[string]any{"on": false, "found": []any{}, "recorded": false})
	if out := m.ok("power", "managers"); out != "TrueSense manages power: off\n  no other power manager is running or enabled\n" {
		t.Fatalf("%q", out)
	}
	d.Reply("hold", nil)
	if out := strings.TrimSpace(m.ok("power", "hold", "12.5")); out != "holding the current power mode for 12.5 min" {
		t.Fatal(out)
	}
	if out := strings.TrimSpace(m.ok("power", "release")); out != "hold released: the automatic profile is in charge again" {
		t.Fatal(out)
	}
	d.Reply("state", map[string]any{"live": map[string]any{}, "settings": map[string]any{"auto_profile": true, "auto_mode": "native"}, "hold_left_s": 125})
	if out := strings.TrimSpace(m.ok("power", "status")); out != "power mode: quiet   (automatic profile on, manual choice held for 2 more min)" {
		t.Fatal(out)
	}
	d.Reply("set", map[string]any{})
	if out := strings.TrimSpace(m.ok("power", "auto", "toggle")); out != "automatic power mode: off (you choose)" {
		t.Fatal(out)
	}
	if c, _ := d.Last("set"); c.Args["value"] != "off" {
		t.Fatalf("%+v", c)
	}
}

func TestCurvesAndProfilesText(t *testing.T) {
	m := newMachine(t)
	if out := m.ok("fan", "curve", "list"); !strings.Contains(out, "  native  - PredatorSense default: the firmware controls the fans") {
		t.Fatal(out)
	}
	out := m.ok("fan", "curve", "show", "quiet")
	if !strings.Contains(out, "  cpu [[58, 0], [66, 30]") || !strings.Contains(out, "hysteresis=4.0  down_rate=1.0  predict=3.0  couple=15  battery_shift=3.0") {
		t.Fatal(out)
	}
	if out := strings.TrimSpace(m.ok("fan", "curve", "copy", "quiet")); !strings.HasPrefix(out, "copied 'quiet' to 'quiet-copy'") {
		t.Fatal(out)
	}
	if out := m.ok("gpu", "undervolt", "list"); !strings.Contains(out, "offset") {
		t.Fatal(out)
	}
	if r := m.run("gpu", "undervolt", "use", "mild"); r.code != 1 || !strings.Contains(r.err, "daemon is not running") {
		t.Fatalf("%+v", r)
	}
	if out := m.ok("cpu", "undervolt", "list"); !strings.Contains(out, "core") {
		t.Fatal(out)
	}
	if r := m.run("cpu", "limits"); r.code != 1 {
		t.Fatalf("%+v", r)
	}
	if out := m.ok("audio", "speaker-filter"); strings.TrimSpace(out) != "speaker filter: off" {
		t.Fatal(out)
	}
	if r := m.run("audio", "output"); r.code != 1 || !strings.Contains(r.err, "sound card not found") && !strings.Contains(r.err, "error:") {
		t.Fatalf("%+v", r)
	}
	if r := m.run("audio", "voice", "notify", "maybe"); r.code != 1 || !strings.Contains(r.err, "usage: truesense audio voice notify on|off") {
		t.Fatalf("%+v", r)
	}
}

func TestVerbsForTheWindow(t *testing.T) {
	m := newMachine(t)
	if out := m.json("status", "get"); out["ac"] == nil || out["kb_brightness"] == nil || out["power_mode"] == nil {
		t.Fatalf("%v", out)
	}
	if out := m.json("version", "get"); out["truesense"] == nil {
		t.Fatal(out)
	}
	if out := strings.TrimSpace(m.ok("touchpad", "status")); out != "touchpad: unknown" {
		t.Fatal(out)
	}
	if out := strings.TrimSpace(m.ok("--json", "touchpad", "get")); out != "null" {
		t.Fatal(out)
	}
	if r := m.run("touchpad", "set", "sideways"); r.code != 2 {
		t.Fatalf("%+v", r)
	}
	var rep map[string]any
	if r := m.run("--json", "doctor", "run"); json.Unmarshal([]byte(r.out), &rep) != nil || rep["checks"] == nil || r.code != 1 {
		t.Fatalf("a machine without the service has a failing check: %+v", r) // doctor exits 1 on a failure, as before
	}
	if r := m.run("selftest", "run", "--skip-fans"); !strings.Contains(r.out, "SKIP  fans") || r.code != 1 {
		t.Fatalf("the self-test fails on a machine without the service: %+v", r)
	}
	if r := m.run("config", "reset-all"); r.code != 1 || !strings.Contains(r.out, "[ok ] fans: firmware auto") || !strings.Contains(r.out, "[!! ]") {
		t.Fatalf("without the service some steps fail and the exit code says so: %+v", r)
	}
	if out := m.ok("profile", "current"); strings.TrimSpace(out) != "no profile applied yet" {
		t.Fatal(out)
	}
	if out := strings.TrimSpace(m.ok("audio", "sound")); !strings.HasPrefix(out, "no sound card found in PipeWire") {
		t.Fatal(out)
	}
	m.ok("keyboard", "effect", "static", "--color", "ff0000")
	m.ok("look", "save", "mine")
	if out := strings.TrimSpace(m.ok("look", "copy", "mine")); out != "copied look 'mine' to 'mine-copy'" {
		t.Fatal(out)
	}
	if out := m.json("keyboard", "cells"); len(out["cells"].([]any)) != 128 {
		t.Fatal(out)
	}
	if out := strings.TrimSpace(m.ok("keyboard", "set-cells", "ff0000,00ff00", "--brightness", "20")); out != "sent" {
		t.Fatal(out)
	}
	if r := m.run("keyboard", "set-cells", "ff0000,zz"); r.code != 1 || !strings.Contains(r.err, "cell 1") {
		t.Fatalf("%+v", r)
	}
	if out := strings.TrimSpace(m.ok("gui", "autostart", "status")); out != "window opens at login: off" {
		t.Fatal(out)
	}
	m.ok("gui", "autostart", "on")
	if out := strings.TrimSpace(m.ok("--json", "gui", "autostart")); out != "true" {
		t.Fatal(out)
	}
	m.ok("gui", "autostart", "off")
}
