package adapt

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/Order52/truesense/internal/apitest"
	"github.com/Order52/truesense/internal/gui"
	"github.com/Order52/truesense/internal/ipc"
)

// documentedCommands are the rows of the table in internal/gui/COMMANDS.md.
func documentedCommands(t *testing.T) []string {
	t.Helper()
	f, err := os.Open("../gui/COMMANDS.md")
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close() //nolint:errcheck // read-only
	row := regexp.MustCompile("^\\| `([a-z-]+)` \\| `([a-z-]+)`(?: / `([a-z-]+)`)? \\|")
	var out []string
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 1<<20), 1<<20)
	for sc.Scan() {
		if m := row.FindStringSubmatch(sc.Text()); m != nil {
			out = append(out, m[1]+"/"+m[2])
			if m[3] != "" {
				out = append(out, m[1]+"/"+m[3])
			}
		}
	}
	return out
}

// plausible are arguments that make a command do its work; a command that is not listed takes {}.
var plausible = map[string]string{
	"power/set": `{"mode":"turbo"}`, "power/auto": `{"action":"status"}`, "power/smart": `{"action":"list"}`, "power/game": `{"action":"list"}`,
	"power/managers": `{"action":"list"}`, "fan/smart": `{"action":"status"}`, "fan/set": `{"cpu":50}`, "fan/off": `{"confirm":true}`,
	"fan/curve": `{"action":"list"}`, "gpu/set": `{"mode":"auto"}`, "gpu/undervolt": `{"action":"list"}`, "cpu/limits": `{"pl1":60,"pl2":70}`,
	"cpu/undervolt": `{"action":"list"}`, "battery/limit": `{"action":"status"}`, "battery/calibration": `{"action":"status"}`,
	"battery/usb-charge": `{"level":20}`, "battery/full": `{"action":"get"}`, "display/overdrive": `{"action":"status"}`, "boot/sound": `{"action":"status"}`,
	"keyboard/brightness": `{"level":20}`, "keyboard/effect": `{"name":"static","color":"ff0000","speed":5,"brightness":20}`, "keyboard/color": `{"color":"00ff00"}`,
	"keyboard/speed": `{"level":4}`, "keyboard/operating": `{"action":"status"}`, "keyboard/timeout": `{"action":"status"}`,
	"keyboard/set-cells": `{"cells":["ff0000"]}`, "keyboard/fx": `{"mode":"off","speed":5}`, "keyboard/design": `{"action":"list"}`, "keyboard/profile": `{"action":"list"}`,
	"rear/set": `{"effect":"wave","color":"ff0000","speed":3,"brightness":50}`, "look/show": `{"name":"native"}`, "look/apply": `{"name":"native"}`,
	"look/save": `{"name":"mine"}`, "look/delete": `{"name":"mine"}`, "look/copy": `{"name":"native"}`, "startup/set": `{"target":"rear","value":"last"}`,
	"keys/profile": `{"action":"list"}`, "keys/test": `{"action":"notify:hi"}`, "profile/show": `{"name":"x"}`, "profile/apply": `{"name":"x"}`,
	"profile/save": `{"name":"mine"}`, "profile/delete": `{"name":"mine"}`, "audio/output": `{"action":"status"}`, "audio/speaker-filter": `{"action":"status"}`,
	"audio/dts": `{"action":"status"}`, "audio/mic": `{"action":"status"}`, "audio/voice": `{"action":"status"}`, "touchpad/set": `{"action":"toggle"}`,
	"gui/autostart": `{"action":"status"}`, "update/auto": `{"action":"status"}`, "update/apply": `{"yes":true}`, "selftest/run": `{"skip_fans":true}`,
}

func newBackend(t *testing.T) (*apitest.Fixture, *guiBackend) {
	t.Helper()
	f := apitest.New(t)
	return f, GUI(f.API(), f.S).(*guiBackend)
}

// Every row of the table is implemented: no documented command is unknown, and every command
// the backend has is documented (the table is the whole contract).
func TestEveryRowOfTheTableIsImplemented(t *testing.T) {
	f, b := newBackend(t)
	f.StartDaemon()
	documented := documentedCommands(t)
	if len(documented) < 90 {
		t.Fatalf("only %d rows found in COMMANDS.md: the pattern is broken", len(documented))
	}
	inTable := map[string]bool{}
	for _, c := range documented {
		inTable[c] = true
		if c == "gui/ping" || c == "gui/quit" { // answered by the gui package itself
			continue
		}
		area, verb, _ := strings.Cut(c, "/")
		args := plausible[c]
		if args == "" {
			args = "{}"
		}
		res, err := b.Do(t.Context(), area, verb, json.RawMessage(args))
		if errors.Is(err, gui.ErrUnknownCommand) {
			t.Errorf("%s is documented and not implemented", c)
			continue
		}
		if err == nil {
			if _, jerr := json.Marshal(res); jerr != nil {
				t.Errorf("%s: the result cannot be encoded: %v", c, jerr)
			}
		}
	}
	extra := map[string]bool{"power/hold": true, "keyboard/design": true}
	var undocumented []string
	for c := range b.handlers {
		if !inTable[c] && !extra[c] {
			undocumented = append(undocumented, c)
		}
	}
	sort.Strings(undocumented)
	if len(undocumented) > 0 {
		t.Errorf("implemented but not in COMMANDS.md: %v", undocumented)
	}
	if _, err := b.Do(t.Context(), "nonsense", "verb", nil); !errors.Is(err, gui.ErrUnknownCommand) {
		t.Fatalf("%v", err)
	}
}

func do(t *testing.T, b *guiBackend, area, verb, args string) (any, error) {
	t.Helper()
	return b.Do(t.Context(), area, verb, json.RawMessage(args))
}

func ok(t *testing.T, b *guiBackend, area, verb, args string) any {
	t.Helper()
	res, err := do(t, b, area, verb, args)
	if err != nil {
		t.Fatalf("%s %s %s: %v", area, verb, args, err)
	}
	return res
}

// asJSON turns a result into what the page receives.
func asJSON(t *testing.T, v any) map[string]any {
	t.Helper()
	b, err := json.Marshal(v)
	if err != nil {
		t.Fatal(err)
	}
	var m map[string]any
	if err := json.Unmarshal(b, &m); err != nil {
		t.Fatalf("%s: %v", b, err)
	}
	return m
}

func TestStatusGetHasTheExtraFields(t *testing.T) {
	_, b := newBackend(t)
	m := asJSON(t, ok(t, b, "status", "get", `{}`))
	for _, k := range []string{"power_mode", "sensors", "fans", "daemon", "battery_limit", "ac", "battery_pct", "kb_brightness", "kb_look", "rear", "touchpad", "voice"} {
		if _, has := m[k]; !has {
			t.Errorf("status get lacks %q: %v", k, m)
		}
	}
	if m["ac"] != true || m["kb_brightness"] != float64(25) || m["battery_pct"] != nil {
		t.Fatalf("%v", m)
	}
	if v := asJSON(t, ok(t, b, "version", "get", `{}`)); v["truesense"] == "" {
		t.Fatal(v)
	}
}

// A stop, an undervolt and a power limit beyond the danger line are refused here, whatever the
// page asked the user first.
func TestTheRefusalsHoldEvenIfThePageAsks(t *testing.T) {
	f, b := newBackend(t)
	d := f.StartDaemon()
	for _, tc := range []struct{ area, verb, args, want string }{
		{"fan", "off", `{}`, "explicit confirmation"},
		{"fan", "off", `{"confirm":false}`, "explicit confirmation"},
		{"fan", "set", `{"cpu":0}`, "explicit confirmation"},
		{"fan", "set", `{"cpu":50,"gpu":0}`, "explicit confirmation"},
		{"cpu", "undervolt", `{"action":"apply","core":-20,"cache":-20}`, "explicit confirmation"},
		{"cpu", "undervolt", `{"action":"apply","core":-115,"cache":-95,"confirm":true}`, "force"},
		{"cpu", "undervolt", `{"action":"boot","name":"x","core":-20,"cache":-20}`, "explicit confirmation"},
		{"gpu", "undervolt", `{"action":"apply","offset":100,"cap":2400}`, "explicit confirmation"},
		{"gpu", "undervolt", `{"action":"boot","name":"x","offset":100}`, "explicit confirmation"},
		{"cpu", "limits", `{"pl1":115,"pl2":120}`, "force"},
		{"update", "apply", `{}`, "needs yes: true"},
	} {
		before := len(d.Calls())
		_, err := do(t, b, tc.area, tc.verb, tc.args)
		if err == nil || !strings.Contains(err.Error(), tc.want) {
			t.Errorf("%s %s %s: %v, want an error with %q", tc.area, tc.verb, tc.args, err, tc.want)
		}
		if len(d.Calls()) != before {
			t.Errorf("%s %s %s reached the service", tc.area, tc.verb, tc.args)
		}
	}
	if snap := f.Sim.Snapshot(); snap.Fan != "auto" {
		t.Fatalf("a refused request changed the fans: %+v", snap)
	}
	// with the confirmation (and force) they go through
	d.Reply("fan", "off")
	ok(t, b, "fan", "off", `{"confirm":true}`)
	d.Reply("undervolt", map[string]any{"core_mv": -115, "cache_mv": -95})
	ok(t, b, "cpu", "undervolt", `{"action":"apply","core":-115,"cache":-95,"confirm":true,"force":true}`)
	if c, _ := d.Last("undervolt"); c.Args["force"] != true || c.Args["confirm"] != true || c.Args["name"] != "manual" {
		t.Fatalf("%+v", c)
	}
}

func TestNeedsDaemonIsCoded(t *testing.T) {
	_, b := newBackend(t)
	_, err := do(t, b, "power", "auto", `{"action":"smart"}`)
	var ce interface{ Code() string }
	if !errors.As(err, &ce) || ce.Code() != "needs-daemon" {
		t.Fatalf("%v", err)
	}
	if !strings.Contains(err.Error(), "daemon is not running") {
		t.Fatalf("the message is the one the command line prints: %v", err)
	}
}

func TestPowerAndFanRows(t *testing.T) {
	f, b := newBackend(t)
	if ok(t, b, "power", "set", `{"mode":"turbo"}`) != "turbo" {
		t.Fatal("power set")
	}
	if _, err := do(t, b, "power", "set", `{}`); err == nil || !strings.Contains(err.Error(), "mode is needed") {
		t.Fatalf("%v", err)
	}
	if _, err := do(t, b, "power", "set", `{"mode":5}`); err == nil || !strings.Contains(err.Error(), "wrong kind of value") {
		t.Fatalf("%v", err)
	}
	if _, err := do(t, b, "power", "set", `[1]`); err == nil || !strings.Contains(err.Error(), "bad arguments") {
		t.Fatalf("%v", err)
	}
	if msg := ok(t, b, "fan", "set", `{"cpu":60}`); msg != "fans: manual 60 % / 60 %" || f.Sim.Snapshot().Duty != [2]int{60, 60} {
		t.Fatalf("%v", msg)
	}
	if _, err := do(t, b, "fan", "set", `{"cpu":10}`); err == nil || !strings.Contains(err.Error(), "20..100") {
		t.Fatalf("%v", err)
	}
	if ok(t, b, "fan", "max", `{}`) == "" || f.Sim.Snapshot().Fan != "max" {
		t.Fatal("max")
	}
	d := f.StartDaemon()
	d.Reply("set", func(args ipc.Args) (any, error) {
		return map[string]any{"smart_fan": args["value"], "auto_mode": args["value"]}, nil
	})
	if ok(t, b, "fan", "smart", `{"action":"on"}`) != true {
		t.Fatal("smart")
	}
	d.Reply("state", map[string]any{"live": map[string]any{}, "settings": map[string]any{"auto_mode": "native"}, "hold_left_s": 0})
	if ok(t, b, "power", "auto", `{"action":"status"}`) != "native" {
		t.Fatal("auto")
	}
	if msg := ok(t, b, "power", "release", `{}`); msg != "hold released: the automatic profile is in charge again" {
		t.Fatal(msg)
	}
}

func TestProfileLibrariesShareOneProtocol(t *testing.T) {
	f, b := newBackend(t)
	d := f.StartDaemon()
	d.Reply("fan_curve", func(args ipc.Args) (any, error) {
		return map[string]any{"name": orDefault(asString(args["name"]), "truesense"), "curve": map[string]any{}, "names": []string{}}, nil
	})
	if names := ok(t, b, "fan", "curve", `{"action":"list"}`).([]string); names[0] != "native" {
		t.Fatal(names)
	}
	show := asJSON(t, ok(t, b, "fan", "curve", `{"action":"show","name":"quiet"}`))
	if show["builtin"] != true || show["cpu"] == nil {
		t.Fatalf("%v", show)
	}
	curve := `{"cpu":[[52,0],[60,35],[70,55],[80,75],[89,100]],"gpu":[[48,0],[60,40],[78,100]],"hysteresis":4,"down_rate":1.5,"predict":5,"couple":15,"battery_shift":3}`
	ok(t, b, "fan", "curve", `{"action":"save","name":"mine","curve":`+curve+`}`)
	if m := asJSON(t, ok(t, b, "fan", "curve", `{"action":"show","name":"mine"}`)); m["builtin"] != false {
		t.Fatalf("%v", m)
	}
	if _, err := do(t, b, "fan", "curve", `{"action":"save","name":"quiet","curve":`+curve+`}`); err == nil {
		t.Fatal("a built-in curve is read-only")
	}
	if _, err := do(t, b, "fan", "curve", `{"action":"save","name":"cool","curve":{"cpu":[[52,0],[60,35]],"gpu":[[48,0],[60,40]]}}`); err == nil {
		t.Fatal("a curve too cool for the load is refused")
	}
	if _, err := do(t, b, "fan", "curve", `{"action":"use","name":"custom","curve":{"cpu":[[52,0],[60,35]],"gpu":[[48,0],[60,40]]}}`); err == nil {
		t.Fatal("an edited curve is validated before it reaches the service")
	}
	ok(t, b, "fan", "curve", `{"action":"use","name":"custom","curve":`+curve+`}`)
	if c, _ := d.Last("fan_curve"); c.Args["data"] == nil || c.Args["name"] != "custom" {
		t.Fatalf("%+v", c)
	}
	if n := ok(t, b, "fan", "curve", `{"action":"copy","name":"mine"}`); n != "mine-copy" {
		t.Fatal(n)
	}

	// Smart auto profiles: the same verbs, the settings object for save
	d.Reply("smart_profile", func(args ipc.Args) (any, error) {
		return map[string]any{"name": orDefault(asString(args["name"]), "truesense"), "profile": map[string]any{}, "names": []string{}}, nil
	})
	smart := `{"ac_busy":"turbo","ac_idle":"balanced","ac_browsing":"balanced","ac_turbo":true,"battery_idle":"eco","battery_browsing":"eco","battery_busy":"balanced","battery_hot":"quiet"}`
	ok(t, b, "power", "smart", `{"action":"save","name":"mine","profile":`+smart+`}`)
	if m := asJSON(t, ok(t, b, "power", "smart", `{"action":"show","name":"mine"}`)); m["builtin"] != false || m["ac_busy"] != "turbo" {
		t.Fatalf("%v", m)
	}
	if _, err := do(t, b, "power", "smart", `{"action":"save","name":"x","profile":{"ac_busy":"ludicrous"}}`); err == nil {
		t.Fatal("a bad mode")
	}
	ok(t, b, "power", "smart", `{"action":"use","name":"custom","profile":`+smart+`}`)
	if c, _ := d.Last("smart_profile"); c.Args["data"] == nil {
		t.Fatalf("%+v", c)
	}
	if res := asJSON(t, ok(t, b, "power", "smart", `{"action":"use","name":"steady"}`)); res["name"] != "steady" {
		t.Fatal(res)
	}
}

func asString(v any) string { s, _ := v.(string); return s }

func TestUndervoltRows(t *testing.T) {
	f, b := newBackend(t)
	d := f.StartDaemon()
	d.Reply("undervolt", func(args ipc.Args) (any, error) {
		if args["action"] == "get" {
			return map[string]any{"core_mv": -20, "cache_mv": -10, "applied": nil, "boot": nil, "pending": false}, nil
		}
		return map[string]any{"core_mv": args["core"], "cache_mv": args["cache"]}, nil
	})
	if m := asJSON(t, ok(t, b, "cpu", "undervolt", `{"action":"get"}`)); m["core_mv"] != float64(-20) || m["pending"] != false {
		t.Fatalf("%v", m)
	}
	ok(t, b, "cpu", "undervolt", `{"action":"save","name":"mine","core":-30,"cache":-20}`)
	if m := asJSON(t, ok(t, b, "cpu", "undervolt", `{"action":"show","name":"mine"}`)); m["core"] != float64(-30) || m["builtin"] != false {
		t.Fatalf("%v", m)
	}
	ok(t, b, "cpu", "undervolt", `{"action":"use","name":"mine","confirm":true}`)
	if ok(t, b, "cpu", "undervolt", `{"action":"boot","name":"off"}`) != nil {
		t.Fatal("boot off is null")
	}
	d.Reply("gpu_undervolt", func(args ipc.Args) (any, error) {
		return map[string]any{"offset_mhz": args["offset"], "cap_mhz": args["cap"]}, nil
	})
	ok(t, b, "gpu", "undervolt", `{"action":"save","name":"g","offset":100,"cap":2400}`)
	if m := asJSON(t, ok(t, b, "gpu", "undervolt", `{"action":"apply","offset":120,"cap":2300,"confirm":true}`)); m["offset_mhz"] != float64(120) || m["cap_mhz"] != float64(2300) {
		t.Fatalf("%v", m)
	}
	ok(t, b, "gpu", "undervolt", `{"action":"boot","name":"mine","offset":100,"cap":0,"confirm":true}`)
	if c, _ := d.Last("gpu_undervolt"); c.Args["action"] != "boot" || c.Args["name"] != "mine" {
		t.Fatalf("%+v", c)
	}
	d.Reply("cpu_info", map[string]any{"pl1_w": 80.0, "pl2_w": 80.0, "user": nil, "auto": map[string]any{"normal_w": 80, "gaming_w": 65}})
	if m := asJSON(t, ok(t, b, "cpu", "info", `{}`)); m["pl1_w"] != 80.0 || m["auto"] == nil {
		t.Fatalf("%v", m)
	}
	d.Reply("cpu_limits", map[string]any{"pl1_w": 60.0, "pl2_w": 70.0})
	if m := asJSON(t, ok(t, b, "cpu", "limits", `{"pl1":60,"pl2":70}`)); m["pl1_w"] != 60.0 {
		t.Fatalf("%v", m)
	}
	if m := asJSON(t, ok(t, b, "cpu", "limits", `{"auto":true}`)); m["pl1_w"] != 80.0 {
		t.Fatalf("%v", m)
	}
}

func TestFirmwareSettingsRows(t *testing.T) {
	f, b := newBackend(t)
	for _, tc := range []struct{ area, verb, args string }{
		{"battery", "limit", `{"action":"on"}`}, {"battery", "calibration", `{"action":"on"}`}, {"display", "overdrive", `{"action":"off"}`}, {"boot", "sound", `{"action":"toggle"}`},
	} {
		want := tc.verb != "overdrive" // overdrive starts on in the simulator: off turns it off
		if tc.verb == "overdrive" {
			want = false
		}
		if got := ok(t, b, tc.area, tc.verb, tc.args); got != want {
			t.Errorf("%s %s: %v", tc.area, tc.verb, got)
		}
	}
	if _, err := do(t, b, "battery", "limit", `{"action":"maybe"}`); err == nil || !strings.Contains(err.Error(), "action:") {
		t.Fatalf("%v", err)
	}
	if ok(t, b, "battery", "usb-charge", `{"level":10}`) != 10 || f.Sim.Snapshot().USB != 10 {
		t.Fatal("usb")
	}
	if ok(t, b, "gpu", "set", `{"mode":"auto"}`) != "auto" {
		t.Fatal("gpu")
	}
	if st := asJSON(t, ok(t, b, "battery", "status", `{}`)); st["limit_80"] != true || st["usb_charge_stop"] != float64(10) {
		t.Fatalf("%v", st)
	}
	if ok(t, b, "touchpad", "get", `{}`) != nil {
		t.Fatal("no touchpad in the test machine: null")
	}
	d := f.StartDaemon()
	d.Reply("battery_full", func(args ipc.Args) (any, error) {
		if args["action"] == "get" {
			return map[string]any{"active": false, "limit": true, "capacity": 80.0, "ac": true}, nil
		}
		return map[string]any{"active": args["action"] == "start"}, nil
	})
	if m := asJSON(t, ok(t, b, "battery", "full", `{"action":"start"}`)); m["active"] != true {
		t.Fatal(m)
	}
	if m := asJSON(t, ok(t, b, "battery", "full", `{}`)); m["capacity"] != 80.0 {
		t.Fatal(m)
	}
	d.Reply("touchpad", true)
	if ok(t, b, "touchpad", "set", `{"action":"on"}`) != true {
		t.Fatal("touchpad")
	}
}

func TestKeyboardRows(t *testing.T) {
	f, b := newBackend(t)
	if m := asJSON(t, ok(t, b, "keyboard", "status", `{}`)); m["brightness"] != float64(25) || m["max"] != float64(50) {
		t.Fatalf("%v", m)
	}
	if ok(t, b, "keyboard", "brightness", `{"level":30}`) != 30 {
		t.Fatal("brightness")
	}
	ok(t, b, "keyboard", "effect", `{"name":"breathing","color":"ff0000","speed":5,"brightness":30}`)
	if st := f.S.KB.Last(); st.Name != "breathing" || st.Color != "ff0000" {
		t.Fatalf("%+v", st)
	}
	ok(t, b, "keyboard", "color", `{"color":"00ff00"}`)
	if msg := ok(t, b, "keyboard", "speed", `{"level":4}`); msg != "keyboard effect speed: 4 / 12" {
		t.Fatal(msg)
	}
	if _, err := do(t, b, "keyboard", "effect", `{"name":"disco"}`); err == nil {
		t.Fatal("unknown effect")
	}
	if ok(t, b, "keyboard", "timeout", `{"action":"off"}`) != false {
		t.Fatal("timeout")
	}
	if msg := ok(t, b, "keyboard", "off", `{}`); msg != "Keyboard light: off" {
		t.Fatal(msg)
	}
	ok(t, b, "keyboard", "on", `{}`)
	ok(t, b, "keyboard", "restore", `{}`)

	// per-key colours travel as lists of 128 "rrggbb"
	cells := asJSON(t, ok(t, b, "keyboard", "cells", `{}`))["cells"].([]any)
	if len(cells) != 128 || cells[0] != "00aec7" || cells[127] != "000000" {
		t.Fatalf("%d cells, first %v last %v", len(cells), cells[0], cells[127])
	}
	painted := make([]string, 128)
	for i := range painted {
		painted[i] = "ff0000"
	}
	pj, _ := json.Marshal(painted)
	ok(t, b, "keyboard", "set-cells", `{"cells":`+string(pj)+`,"brightness":20}`)
	if got := asJSON(t, ok(t, b, "keyboard", "cells", `{}`))["cells"].([]any); got[5] != "ff0000" || f.Keyboard.Brightness() != 20 {
		t.Fatalf("%v %d", got[5], f.Keyboard.Brightness())
	}
	if _, err := do(t, b, "keyboard", "set-cells", `{"cells":["zz"]}`); err == nil || !strings.Contains(err.Error(), "cell 0") {
		t.Fatalf("%v", err)
	}
	if _, err := do(t, b, "keyboard", "set-cells", `{}`); err == nil {
		t.Fatal("cells are needed")
	}
	if l := asJSON(t, ok(t, b, "keyboard", "layout", `{}`)); l["keys"] == nil || l["groups"] == nil {
		t.Fatalf("%v", l)
	}

	// animations: keys as cell numbers, the colours under them given
	res := asJSON(t, ok(t, b, "keyboard", "fx", `{"mode":"wave","speed":4,"cells":[12,13],"base":`+string(pj)+`}`))
	if res["mode"] != "wave" || res["speed"] != float64(4) || len(res["keys"].([]any)) != 2 || len(res["base"].([]any)) != 128 {
		t.Fatalf("%v", res)
	}
	ok(t, b, "keyboard", "fx", `{"mode":"off"}`)

	// designs and lighting profiles, with colours from the page
	if d := ok(t, b, "keyboard", "design", `{"action":"list"}`); len(asJSON(t, d)) == 0 {
		t.Fatal("designs")
	}
	if m := asJSON(t, ok(t, b, "keyboard", "design", `{"action":"show","name":"gaming"}`)); len(m["cells"].([]any)) != 128 || m["source"] != "built-in" {
		t.Fatalf("%v", m["source"])
	}
	path := ok(t, b, "keyboard", "design", `{"action":"save","name":"mine","cells":`+string(pj)+`}`).(string)
	if _, err := os.Stat(path); err != nil {
		t.Fatal(err)
	}
	if msg := ok(t, b, "keyboard", "profile", `{"action":"save","name":"red","cells":`+string(pj)+`}`); !strings.Contains(msg.(string), "saved lighting profile 'red'") {
		t.Fatal(msg)
	}
	if names := ok(t, b, "keyboard", "profile", `{"action":"list"}`).([]string); len(names) != 1 || names[0] != "red" {
		t.Fatal(names)
	}
	ok(t, b, "keyboard", "profile", `{"action":"load","name":"red"}`)
	ok(t, b, "keyboard", "profile", `{"action":"delete","name":"red"}`)
}

func TestRearAndLookRows(t *testing.T) {
	f, b := newBackend(t)
	if err := f.S.User.Set("rear_zones", []string{"ff0000", "00ff00", "0000ff"}); err != nil {
		t.Fatal(err)
	}
	st := asJSON(t, ok(t, b, "rear", "status", `{}`))
	if st["zones"] == nil || len(st["zones"].([]any)) != 3 || st["raw"] == nil {
		t.Fatalf("%v", st)
	}
	r := asJSON(t, ok(t, b, "rear", "set", `{"effect":"wave","speed":2,"brightness":40,"color":"00ff88"}`))
	if r["mode"] != "wave" || r["speed"] != float64(2) || r["brightness"] != float64(40) {
		t.Fatalf("%v", r)
	}
	ok(t, b, "rear", "set", `{"effect":"static","brightness":60,"verify":false}`)
	if got := asJSON(t, ok(t, b, "rear", "status", `{}`)); got["brightness"] != float64(60) {
		t.Fatalf("%v", got)
	}
	ok(t, b, "rear", "set", `{"effect":"static","zones":["ff0000","00ff00","0000ff"]}`)
	if msg := ok(t, b, "rear", "off", `{}`); msg != "Rear light: off" {
		t.Fatal(msg)
	}
	ok(t, b, "rear", "on", `{}`)
	if _, err := do(t, b, "rear", "set", `{"effect":"disco"}`); err == nil {
		t.Fatal("unknown effect")
	}

	if names := ok(t, b, "look", "list", `{}`).([]string); names[0] != "native" {
		t.Fatal(names)
	}
	if m := asJSON(t, ok(t, b, "look", "show", `{"name":"native"}`)); m["builtin"] != true {
		t.Fatalf("%v", m)
	}
	saved := asJSON(t, ok(t, b, "look", "save", `{"name":"mine","look":{"keyboard":{"mode":"effect","name":"wave","color":"00aec7","speed":5,"brightness":20},"rear":null}}`))
	if saved["keyboard"] == nil {
		t.Fatalf("%v", saved)
	}
	if m := asJSON(t, ok(t, b, "look", "show", `{"name":"mine"}`)); m["builtin"] != false {
		t.Fatalf("%v", m)
	}
	if n := ok(t, b, "look", "copy", `{"name":"mine"}`); n != "mine-copy" {
		t.Fatal(n)
	}
	if done := ok(t, b, "look", "apply", `{"name":"mine"}`).([]string); len(done) == 0 {
		t.Fatal("applied")
	}
	if n := ok(t, b, "look", "delete", `{"name":"mine"}`); n != "mine" {
		t.Fatal(n)
	}
	if _, err := do(t, b, "look", "delete", `{"name":"native"}`); err == nil {
		t.Fatal("built-in")
	}
	ok(t, b, "look", "save", `{"name":"now"}`) // absent look: what is on the lights now
}

func TestStartupKeysAndProfileRows(t *testing.T) {
	f, b := newBackend(t)
	if ok(t, b, "startup", "set", `{"target":"rear","value":"off"}`) != "off" {
		t.Fatal("startup set")
	}
	if m := asJSON(t, ok(t, b, "startup", "show", `{}`)); m["rear"] != "off" {
		t.Fatalf("%v", m)
	}
	if lines := ok(t, b, "startup", "apply", `{}`).([]string); len(lines) == 0 {
		t.Fatal("startup apply")
	}
	d := f.StartDaemon()
	d.Reply("boot_look", nil)
	if ok(t, b, "startup", "boot", `{}`) != nil {
		t.Fatal("no boot look")
	}
	d.Reply("boot_look", func(args ipc.Args) (any, error) { return args["look"], nil })
	ok(t, b, "startup", "boot", `{"what":"native"}`)
	ok(t, b, "startup", "boot", `{"what":"clear"}`)

	if names := ok(t, b, "keys", "profile", `{"action":"list"}`).([]string); names[0] != "native" {
		t.Fatal(names)
	}
	saved := asJSON(t, ok(t, b, "keys", "profile", `{"action":"save","name":"mine","tap":"power:turbo","long_ms":700}`))
	if saved["tap"] != "power:turbo" || saved["long_ms"] != float64(700) || saved["double"] == "" {
		t.Fatalf("%v", saved)
	}
	if m := asJSON(t, ok(t, b, "keys", "profile", `{"action":"show","name":"mine"}`)); m["builtin"] != false {
		t.Fatalf("%v", m)
	}
	if m := asJSON(t, ok(t, b, "keys", "profile", `{"action":"show","name":"native"}`)); m["builtin"] != true || m["description"] == "" {
		t.Fatalf("%v", m)
	}
	if ok(t, b, "keys", "profile", `{"action":"use","name":"mine"}`) != "mine" {
		t.Fatal("use")
	}
	if cur := asJSON(t, ok(t, b, "keys", "profile", `{}`)); cur["profile"] != "mine" || cur["tap"] != "power:turbo" {
		t.Fatalf("%v", cur)
	}
	if _, err := do(t, b, "keys", "profile", `{"action":"save","name":"bad","tap":"nonsense"}`); err == nil {
		t.Fatal("a bad action is refused when saved")
	}
	if msg := ok(t, b, "keys", "test", `{"action":"notify:hello"}`); msg != "hello" {
		t.Fatal(msg)
	}

	if ok(t, b, "profile", "current", `{}`).(map[string]any)["name"] != nil {
		t.Fatal("nothing applied yet")
	}
	if path := ok(t, b, "profile", "save", `{"name":"mine","description":"x"}`).(string); path == "" {
		t.Fatal("path")
	}
	if list := ok(t, b, "profile", "list", `{}`); asJSON2(t, list)[0]["name"] != "mine" {
		t.Fatalf("%v", list)
	}
	if m := asJSON(t, ok(t, b, "profile", "show", `{"name":"mine"}`)); m["description"] != "x" {
		t.Fatalf("%v", m)
	}
	if msg := ok(t, b, "profile", "apply", `{"name":"mine"}`); msg == "" {
		t.Fatal("apply")
	}
	if cur := ok(t, b, "profile", "current", `{}`).(map[string]any); cur["name"] != "mine" {
		t.Fatalf("%v", cur)
	}
	if msg := ok(t, b, "profile", "next", `{}`); msg == "" {
		t.Fatal("next")
	}
	if msg := ok(t, b, "profile", "delete", `{"name":"mine"}`); msg != "deleted mine" {
		t.Fatal(msg)
	}
}

func asJSON2(t *testing.T, v any) []map[string]any {
	t.Helper()
	b, _ := json.Marshal(v)
	var out []map[string]any
	if err := json.Unmarshal(b, &out); err != nil {
		t.Fatal(err)
	}
	return out
}

func TestAudioRowsOnAMachineWithoutASoundSystem(t *testing.T) {
	_, b := newBackend(t)
	snd := asJSON(t, ok(t, b, "audio", "sound", `{}`))
	if snd["ok"] != nil || snd["sinks"] == nil {
		t.Fatalf("%v", snd)
	}
	if _, err := do(t, b, "audio", "output", `{"action":"status"}`); err == nil {
		t.Fatal("no card")
	}
	if _, err := do(t, b, "audio", "output", `{"action":"sideways"}`); err == nil {
		t.Fatal("unknown output")
	}
	if on := ok(t, b, "audio", "speaker-filter", `{"action":"status"}`); on != false {
		t.Fatal(on)
	}
	if m := asJSON(t, ok(t, b, "audio", "mic", `{}`)); m["boost"] != nil || m["volume"] != nil {
		t.Fatalf("%v", m)
	}
	if v := asJSON(t, ok(t, b, "audio", "voice", `{}`)); v["modes"] == nil {
		t.Fatalf("%v", v)
	}
	if _, err := do(t, b, "audio", "voice", `{"action":"set","a":"headset"}`); err == nil || !strings.Contains(err.Error(), "usage: truesense audio voice set") {
		t.Fatalf("%v", err)
	}
	if _, err := do(t, b, "audio", "voice", `{"action":"notify","a":"maybe"}`); err == nil {
		t.Fatal("notify")
	}
	if v := asJSON(t, ok(t, b, "audio", "voice", `{"action":"notify","a":"off"}`)); v["notify"] != false {
		t.Fatalf("%v", v)
	}
	if j := asJSON(t, ok(t, b, "audio", "jack", `{"use":"headset"}`)); j["setting"] != "headset" || j["microphone_in_use"] != "headset" {
		t.Fatalf("%v", j)
	}
	if _, err := do(t, b, "audio", "jack", `{"use":"loud"}`); err == nil {
		t.Fatal("jack")
	}
	if _, err := do(t, b, "audio", "dts", `{"action":"status"}`); err == nil || !strings.Contains(err.Error(), "AcerUltraX is not installed") {
		t.Fatalf("%v", err)
	}
	rep := asJSON(t, ok(t, b, "audio", "repair", `{}`))
	if rep["steps"] == nil {
		t.Fatalf("%v", rep)
	}
}

func TestSystemRows(t *testing.T) {
	f, b := newBackend(t)
	if ok(t, b, "gui", "autostart", `{"action":"status"}`) != false {
		t.Fatal("autostart starts off")
	}
	if ok(t, b, "gui", "autostart", `{"action":"on"}`) != true {
		t.Fatal("on")
	}
	home, _ := os.UserConfigDir()
	body, err := os.ReadFile(home + "/autostart/truesense.desktop")
	if err != nil || !strings.Contains(string(body), "Exec=truesense gui") {
		t.Fatalf("%q %v", body, err)
	}
	if ok(t, b, "gui", "autostart", `{"action":"off"}`) != false {
		t.Fatal("off")
	}
	if _, err := os.Stat(home + "/autostart/truesense.desktop"); err == nil {
		t.Fatal("the entry is removed")
	}
	if ok(t, b, "gui", "autostart", `{"action":"off"}`) != false {
		t.Fatal("off twice is fine")
	}

	if ok(t, b, "update", "auto", `{"action":"on"}`) != true {
		t.Fatal("update auto")
	}
	if rep := asJSON(t, ok(t, b, "doctor", "run", `{}`)); rep["checks"] == nil || rep["ok"] == nil {
		t.Fatalf("%v", rep)
	}
	rows := asJSON2(t, ok(t, b, "service", "check", `{}`))
	if len(rows) == 0 || rows[0]["unit"] == nil {
		t.Fatalf("%v", rows)
	}
	if logs := asJSON(t, ok(t, b, "service", "logs", `{}`)); !strings.Contains(logs["text"].(string), "journalctl -u truesense.service") {
		t.Fatalf("%v", logs)
	}
	steps := asJSON2(t, ok(t, b, "service", "repair", `{}`))
	last := steps[len(steps)-1]
	if !strings.Contains(last["step"].(string), "sudo truesense service repair") {
		t.Fatalf("it says when the system part needs sudo: %v", steps)
	}
	st := asJSON(t, ok(t, b, "selftest", "run", `{"skip_fans":true}`))
	if st["ok"] == nil || !strings.Contains(st["output"].(string), "SKIP") {
		t.Fatalf("%v", st)
	}
	if snap := f.Sim.Snapshot(); snap.Fan != "auto" {
		t.Fatalf("the self-test touched the fans: %+v", snap)
	}
	if v := ok(t, b, "config", "validate", `{}`).([]string); len(v) != 0 {
		t.Fatal(v)
	}
	if cfg := asJSON(t, ok(t, b, "config", "show", `{}`)); cfg["daemon"] == nil {
		t.Fatalf("%v", cfg)
	}
}

func TestResetAllRunsEveryStepEvenWhenOneFails(t *testing.T) {
	f, b := newBackend(t)
	res := asJSON(t, ok(t, b, "config", "reset-all", `{}`)) // no service: the service steps fail
	steps := res["steps"].([]any)
	if len(steps) != 12 {
		t.Fatalf("%d steps", len(steps))
	}
	var failed, passed int
	for _, s := range steps {
		if s.(map[string]any)["ok"] == true {
			passed++
		} else {
			failed++
			if s.(map[string]any)["error"] == "" {
				t.Fatalf("a failed step says why: %v", s)
			}
		}
	}
	if failed == 0 || passed == 0 {
		t.Fatalf("passed %d failed %d", passed, failed)
	}
	// what works without the service was done: the fans are Auto, the keyboard is stock cyan, the bar is breathing
	if f.Sim.Snapshot().Fan != "auto" {
		t.Fatal("fans")
	}
	if c := f.Keyboard.LastCells()[0]; c.Hex() != "00aec7" {
		t.Fatalf("keyboard %v", c)
	}
	if r := asJSON(t, ok(t, b, "rear", "status", `{}`)); r["mode"] != "breathing" || r["color"] != "3723c7" {
		t.Fatalf("%v", r)
	}
	d := f.StartDaemon()
	d.Reply("fan_curve", map[string]any{"name": "native", "curve": map[string]any{}})
	d.Reply("smart_profile", map[string]any{"name": "native", "profile": map[string]any{}})
	d.Reply("cpu_info", map[string]any{})
	d.Reply("fan", "auto")
	res = asJSON(t, ok(t, b, "config", "reset-all", `{}`))
	for _, s := range res["steps"].([]any) {
		if s.(map[string]any)["ok"] != true {
			t.Errorf("with a service every step works: %v", s)
		}
	}
}

func TestTheLiveStream(t *testing.T) {
	f, b := newBackend(t)
	b.tick = 5 * time.Millisecond
	f.S.HW.State = f.S.User
	var got []map[string]any
	ctx, cancel := context.WithCancel(t.Context())
	b.Stream(streamCtx{ctx, cancel, func(m json.RawMessage) {
		var v map[string]any
		if err := json.Unmarshal(m, &v); err != nil {
			t.Error(err)
		}
		got = append(got, v)
	}, 3}.run())
	if len(got) < 3 {
		t.Fatalf("%d messages", len(got))
	}
	m := got[0]
	for _, k := range []string{"t", "sensors", "power_mode", "ac", "battery_pct", "fans", "load", "power", "daemon"} {
		if _, has := m[k]; !has {
			t.Errorf("the stream lacks %q: %v", k, m)
		}
	}
	if m["daemon"] != nil || m["power_mode"] != "eco" || m["ac"] != true || m["battery_pct"] != nil {
		t.Fatalf("%v", m)
	}
	if m["sensors"].(map[string]any)["cpu_temp"] != float64(50) {
		t.Fatalf("%v", m["sensors"])
	}
	// with the service: its state is passed on as it is and its readings are used
	d := f.StartDaemon()
	d.Reply("state", map[string]any{"live": map[string]any{"tcpu": 61.0, "tgpu": 55.0, "chassis": 40.0, "rpm1": 2000.0, "rpm2": 2100.0, "cpu": 30.0, "gpu": 5.0, "gpuw": 12.5, "ac": false, "power_mode": "turbo"},
		"settings": map[string]any{"smart_fan": true}, "hold_left_s": 7})
	got = nil
	ctx, cancel = context.WithCancel(t.Context())
	b.Stream(streamCtx{ctx, cancel, func(m json.RawMessage) {
		var v map[string]any
		_ = json.Unmarshal(m, &v)
		got = append(got, v)
	}, 1}.run())
	m = got[0]
	live := m["daemon"].(map[string]any)
	if live["hold_left_s"] != float64(7) || live["settings"].(map[string]any)["smart_fan"] != true ||
		m["ac"] != false || m["power_mode"] != "turbo" || m["load"].(map[string]any)["cpu_pct"] != 30.0 || m["power"].(map[string]any)["gpu_w"] != 12.5 ||
		m["sensors"].(map[string]any)["cpu_fan_rpm"] != float64(2000) {
		t.Fatalf("%v", m)
	}
}

// streamCtx cancels the stream after n messages.
type streamCtx struct {
	ctx    context.Context
	cancel context.CancelFunc
	emit   func(json.RawMessage)
	n      int
}

func (s streamCtx) run() (context.Context, func(json.RawMessage)) {
	count := 0
	return s.ctx, func(m json.RawMessage) {
		s.emit(m)
		if count++; count >= s.n {
			s.cancel()
		}
	}
}

// Every command the page's source calls is implemented (the page and the table cannot drift
// from the backend).
func TestEveryCommandTheFrontEndCallsIsImplemented(t *testing.T) {
	_, b := newBackend(t)
	call := regexp.MustCompile(`\b(?:call|tryCall)\(\s*"([a-z-]+)",\s*"([a-z-]+)"`)
	seen := map[string]bool{}
	err := filepath.WalkDir("../gui/web/js", func(p string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() || !strings.HasSuffix(p, ".js") {
			return err
		}
		src, err := os.ReadFile(p)
		for _, m := range call.FindAllStringSubmatch(string(src), -1) {
			seen[m[1]+"/"+m[2]] = true
		}
		return err
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(seen) < 60 {
		t.Fatalf("only %d commands found in the front-end: the pattern is broken", len(seen))
	}
	for c := range seen {
		if c == "gui/ping" || c == "gui/quit" {
			continue
		}
		if _, ok := b.handlers[c]; !ok {
			t.Errorf("the page calls %s, which is not implemented", c)
		}
	}
}
