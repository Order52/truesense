package guitest

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/Order52/truesense/internal/gui"
)

func do(t *testing.T, l *Laptop, area, verb, body string) any {
	t.Helper()
	r, err := l.Do(context.Background(), area, verb, json.RawMessage(body))
	if err != nil {
		t.Fatalf("%s %s %s: %v", area, verb, body, err)
	}
	return r
}

func fails(t *testing.T, l *Laptop, area, verb, body, want string) {
	t.Helper()
	_, err := l.Do(context.Background(), area, verb, json.RawMessage(body))
	if err == nil || !strings.Contains(err.Error(), want) {
		t.Fatalf("%s %s %s: err = %v, want %q", area, verb, body, err, want)
	}
}

func TestUnknownCommandIsRecognised(t *testing.T) {
	_, err := New().Do(context.Background(), "nope", "never", nil)
	if !errors.Is(err, gui.ErrUnknownCommand) {
		t.Fatalf("got %v", err)
	}
}

func TestSettingsStick(t *testing.T) {
	l := New()
	do(t, l, "power", "set", `{"mode":"eco"}`)
	if got := do(t, l, "status", "get", `{}`).(map[string]any)["power_mode"]; got != "eco" {
		t.Fatalf("mode %v", got)
	}
	if do(t, l, "battery", "limit", `{"action":"off"}`) != false || do(t, l, "battery", "limit", `{"action":"toggle"}`) != true {
		t.Fatal("on/off/toggle")
	}
	fails(t, l, "power", "set", `{"mode":"ludicrous"}`, "mode")
	fails(t, l, "gpu", "set", `{"mode":"x"}`, "optimus")
	fails(t, l, "battery", "usb-charge", `{"level":15}`, "0 | 10 | 20 | 30")
}

func TestFanSafety(t *testing.T) {
	l := New()
	fails(t, l, "fan", "set", `{"cpu":10}`, "20..100")
	fails(t, l, "fan", "set", `{"cpu":0}`, "confirmation")
	fails(t, l, "fan", "off", `{}`, "confirmation")
	do(t, l, "power", "set", `{"mode":"eco"}`)
	do(t, l, "fan", "set", `{"cpu":40,"gpu":60}`)
	s := l.Snapshot()["fans"].(map[string]any)
	if s["mode"] != "manual" || s["duty_register_pct"].(map[string]any)["gpu"] != 60 {
		t.Fatalf("fans %v", s)
	}
	do(t, l, "power", "set", `{"mode":"turbo"}`)
	fails(t, l, "fan", "off", `{"confirm":true}`, "too warm")
}

func TestProfileLibrariesProtocol(t *testing.T) {
	l := New()
	list := do(t, l, "fan", "curve", `{"action":"list"}`).([]string)
	if list[0] != "native" || list[len(list)-1] != "my-curve" {
		t.Fatalf("list %v", list)
	}
	show := do(t, l, "fan", "curve", `{"action":"show","name":"native"}`).(map[string]any)
	if show["builtin"] != true || show["native"] != true {
		t.Fatalf("show %v", show)
	}
	fails(t, l, "fan", "curve", `{"action":"save","name":"native","curve":{"cpu":[[50,0],[90,100]],"gpu":[[50,0],[80,100]]}}`, "built in")
	fails(t, l, "fan", "curve", `{"action":"delete","name":"native"}`, "built in")
	fails(t, l, "fan", "curve", `{"action":"show","name":"missing"}`, "no fan curve")
	if n := do(t, l, "fan", "curve", `{"action":"copy","name":"native","new":"mine"}`); n != "mine" {
		t.Fatalf("copy %v", n)
	}
	if n := do(t, l, "fan", "curve", `{"action":"copy","name":"native","new":"mine"}`); n != "mine-2" {
		t.Fatalf("second copy %v", n)
	}
	if do(t, l, "fan", "curve", `{"action":"show","name":"mine"}`).(map[string]any)["builtin"] != false {
		t.Fatal("a copy must be editable")
	}
	do(t, l, "fan", "curve", `{"action":"delete","name":"mine"}`)
	fails(t, l, "fan", "curve", `{"action":"save","name":"bad name!","curve":{"cpu":[],"gpu":[]}}`, "names use")
	do(t, l, "fan", "curve", `{"action":"use","name":"quiet"}`)
	if cur := do(t, l, "fan", "curve", `{"action":"current"}`).(map[string]any); cur["name"] != "quiet" {
		t.Fatalf("current %v", cur)
	}
}

func TestPerKeyCells(t *testing.T) {
	l := New()
	cells := make([]string, 128)
	for i := range cells {
		cells[i] = "112233"
	}
	b, _ := json.Marshal(map[string]any{"cells": cells})
	do(t, l, "keyboard", "set-cells", string(b))
	got := do(t, l, "keyboard", "cells", `{}`).(map[string]any)["cells"].([]any)
	if len(got) != 128 || got[5] != "112233" {
		t.Fatalf("cells %v", got[:3])
	}
	fails(t, l, "keyboard", "set-cells", `{"cells":["000000"]}`, "128")
	cells[3] = "zzzzzz"
	b, _ = json.Marshal(map[string]any{"cells": cells})
	fails(t, l, "keyboard", "set-cells", string(b), "bad colour")
	do(t, l, "keyboard", "restore", `{}`)
	if do(t, l, "keyboard", "cells", `{}`).(map[string]any)["cells"].([]any)[0] != "00aec7" {
		t.Fatal("restore did not bring the stock colours back")
	}
}

func TestLayoutMatchesTheVerifiedKeyMap(t *testing.T) {
	l := New()
	keys := do(t, l, "keyboard", "layout", `{}`).(map[string]any)["keys"].(map[string]any)
	if len(keys) != 105 {
		t.Fatalf("%d key names", len(keys))
	}
	esc := keys["esc"].([]any)[0].(map[string]any)
	if esc["cell"] != 2 || esc["col"] != 0 || esc["row"] != 2 {
		t.Fatalf("esc %v", esc)
	}
}

func TestLooksAndBootLook(t *testing.T) {
	l := New()
	do(t, l, "look", "save", `{"name":"mine"}`)
	if names := do(t, l, "look", "list", `{}`).([]string); len(names) != 2 || names[1] != "mine" {
		t.Fatalf("names %v", names)
	}
	do(t, l, "startup", "boot", `{"what":"current"}`)
	if do(t, l, "startup", "boot", `{}`) == nil {
		t.Fatal("boot look not set")
	}
	do(t, l, "startup", "boot", `{"what":"clear"}`)
	if do(t, l, "startup", "boot", `{}`) != nil {
		t.Fatal("boot look not cleared")
	}
	fails(t, l, "look", "delete", `{"name":"native"}`, "built in")
}

func TestUndervoltNeedsConfirmationAndForce(t *testing.T) {
	l := New()
	fails(t, l, "cpu", "undervolt", `{"action":"apply","core":-50,"cache":-40}`, "confirmation")
	fails(t, l, "cpu", "undervolt", `{"action":"apply","core":-100,"cache":-40,"confirm":true}`, "force")
	do(t, l, "cpu", "undervolt", `{"action":"apply","core":-100,"cache":-40,"confirm":true,"force":true}`)
	if got := do(t, l, "cpu", "undervolt", `{"action":"get"}`).(map[string]any); got["core_mv"] != -100 {
		t.Fatalf("get %v", got)
	}
	do(t, l, "cpu", "undervolt", `{"action":"reset"}`)
	fails(t, l, "gpu", "undervolt", `{"action":"apply","offset":250,"cap":2000,"confirm":true}`, "force")
	fails(t, l, "cpu", "limits", `{"pl1":90,"pl2":80}`, "PL2")
}

func TestDaemonDownSimulation(t *testing.T) {
	l := New()
	l.NoDaemon = true
	fails(t, l, "power", "auto", `{"action":"smart"}`, "daemon is not running")
	if l.Snapshot()["daemon"] != nil {
		t.Fatal("the stream still reports a daemon")
	}
	if _, ok := do(t, l, "status", "get", `{}`).(map[string]any)["daemon"].(string); !ok {
		t.Fatal("status get must say ERROR: ... for the daemon")
	}
	do(t, l, "gpu", "set", `{"mode":"auto"}`) // does not need the daemon
}

func TestFailNextAndCalls(t *testing.T) {
	l := New()
	l.FailNext("power", "set", "injected")
	fails(t, l, "power", "set", `{"mode":"eco"}`, "injected")
	do(t, l, "power", "set", `{"mode":"eco"}`)
	calls := l.Calls()
	if len(calls) != 2 || calls[1].Args["mode"] != "eco" {
		t.Fatalf("calls %v", calls)
	}
}

func TestStreamEmitsAndStops(t *testing.T) {
	l := New()
	l.Tick = 5 * time.Millisecond
	ctx, cancel := context.WithCancel(context.Background())
	var mu sync.Mutex
	var msgs []json.RawMessage
	done := make(chan struct{})
	go func() {
		l.Stream(ctx, func(m json.RawMessage) {
			mu.Lock()
			msgs = append(msgs, m)
			mu.Unlock()
		})
		close(done)
	}()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		mu.Lock()
		n := len(msgs)
		mu.Unlock()
		if n >= 3 {
			break
		}
		time.Sleep(2 * time.Millisecond)
	}
	cancel()
	<-done
	mu.Lock()
	defer mu.Unlock()
	if len(msgs) < 3 || !json.Valid(msgs[0]) {
		t.Fatalf("%d messages", len(msgs))
	}
	var m struct {
		Sensors struct {
			CPU int `json:"cpu_temp"`
		} `json:"sensors"`
	}
	if err := json.Unmarshal(msgs[0], &m); err != nil || m.Sensors.CPU < 20 || m.Sensors.CPU > 110 {
		t.Fatalf("sensors %s (%v)", msgs[0], err)
	}
}

func TestEverySimulatedVerbAnswersAnEmptyRequest(t *testing.T) {
	// a request without arguments must produce an answer or a message, never a panic
	l := New()
	for _, c := range []string{
		"status/get", "version/get", "power/status", "power/smart", "power/game", "power/managers", "fan/curve", "battery/status", "battery/full",
		"keyboard/status", "keyboard/layout", "keyboard/cells", "keyboard/design", "keyboard/profile", "rear/status", "look/list", "startup/show", "startup/boot",
		"keys/profile", "profile/list", "profile/current", "audio/output", "audio/dts", "audio/jack", "audio/mic", "audio/voice", "audio/sound", "audio/repair",
		"touchpad/get", "update/check", "doctor/run", "service/check", "service/logs", "selftest/run", "config/show", "config/validate", "cpu/info", "cpu/undervolt", "gpu/undervolt",
	} {
		area, verb, _ := strings.Cut(c, "/")
		func() {
			defer func() {
				if p := recover(); p != nil {
					t.Errorf("%s panicked: %v", c, p)
				}
			}()
			if _, err := l.Do(context.Background(), area, verb, json.RawMessage(`{}`)); errors.Is(err, gui.ErrUnknownCommand) {
				t.Errorf("%s: %v", c, fmt.Sprint(err))
			}
		}()
	}
}
