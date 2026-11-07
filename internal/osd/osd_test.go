package osd

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

type call struct {
	env    []string
	asUser string
	cmd    string
}

func recorder(replies map[string]struct {
	out  string
	code int
}) (Runner, *[]call) {
	var calls []call
	return func(_ context.Context, env []string, asUser, name string, args ...string) (string, int) {
		calls = append(calls, call{env, asUser, name + " " + strings.Join(args, " ")})
		if r, ok := replies[name]; ok {
			return r.out, r.code
		}
		return "", -1
	}, &calls
}

type reply = struct {
	out  string
	code int
}

func TestNativeTriesTheCompositorThenTheDaemon(t *testing.T) {
	t.Setenv("TRUESENSE_OSD", "")
	run, calls := recorder(map[string]reply{"hyprctl": {"ok\n", 0}})
	n := &Notifier{Run: run}
	if !n.Show("Power mode: turbo", "power") || len(*calls) != 1 || !strings.HasPrefix((*calls)[0].cmd, "hyprctl notify") {
		t.Fatalf("calls: %+v", *calls)
	}
	run, calls = recorder(map[string]reply{"hyprctl": {"no\n", 1}, "notify-send": {"", 0}})
	n = &Notifier{Run: run}
	if !n.Show("x", "power") || len(*calls) != 2 || !strings.HasPrefix((*calls)[1].cmd, "notify-send") {
		t.Fatalf("fallback: %+v", *calls)
	}
	run, _ = recorder(nil)
	if (&Notifier{Run: run}).Show("x", "power") {
		t.Fatal("no helper installed: false, never an error")
	}
}

func TestNoneIsSilentAndTheEnvironmentOverridesTheConfig(t *testing.T) {
	t.Setenv("TRUESENSE_OSD", "")
	run, calls := recorder(nil)
	if (&Notifier{Style: "none", Run: run}).Show("x", "fan") || len(*calls) != 0 {
		t.Fatal("none: no program runs")
	}
	t.Setenv("TRUESENSE_OSD", "none")
	if (&Notifier{Style: "native", Run: run}).Show("x", "fan") || len(*calls) != 0 {
		t.Fatal("TRUESENSE_OSD wins over the configuration")
	}
}

func TestNoctaliaUsesItsIconNames(t *testing.T) {
	t.Setenv("TRUESENSE_OSD", "")
	run, calls := recorder(map[string]reply{"noctalia": {"", 0}})
	if !(&Notifier{Style: "noctalia", Run: run}).Show("Mic muted", "mic") || !strings.Contains((*calls)[0].cmd, "osd-show microphone Mic muted") {
		t.Fatalf("%+v", *calls)
	}
}

func TestAsRootTheMessageGoesToTheUsersSessionAsThatUser(t *testing.T) {
	t.Setenv("TRUESENSE_OSD", "")
	rt := t.TempDir()
	uidDir := filepath.Join(rt, "1000")
	if err := os.MkdirAll(filepath.Join(uidDir, "hypr", "abc123"), 0o755); err != nil {
		t.Fatal(err)
	}
	for _, f := range []string{"wayland-1", "wayland-1.lock"} {
		if err := os.WriteFile(filepath.Join(uidDir, f), nil, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	run, calls := recorder(map[string]reply{"hyprctl": {"ok", 0}})
	n := &Notifier{User: "ali", IsRoot: true, RuntimeDir: rt, Run: run, Lookup: func(string) (int, string, error) { return 1000, "/home/ali", nil }}
	if !n.Show("hi", "power") {
		t.Fatal("must show")
	}
	c := (*calls)[0]
	env := strings.Join(c.env, " ")
	for _, want := range []string{"XDG_RUNTIME_DIR=" + uidDir, "WAYLAND_DISPLAY=wayland-1", "HYPRLAND_INSTANCE_SIGNATURE=abc123", "HOME=/home/ali"} {
		if !strings.Contains(env, want) {
			t.Errorf("environment lacks %q: %s", want, env)
		}
	}
	if strings.Contains(env, "wayland-1.lock") || c.asUser != "ali" {
		t.Fatalf("a lock file is not a display, and it must run as the user: %+v", c)
	}
	n.Lookup = func(string) (int, string, error) { return 0, "", os.ErrNotExist }
	if n.Show("hi", "power") {
		t.Fatal("an unknown user cannot get a message")
	}
}

func TestQuietPairsDisableWithEnableEvenWhenNested(t *testing.T) {
	run, calls := recorder(nil)
	n := &Notifier{Run: run}
	outer := n.Quiet(0)
	inner := n.Quiet(0)
	inner()
	outer()
	var seq []string
	for _, c := range *calls {
		seq = append(seq, c.cmd)
	}
	if got := strings.Join(seq, "|"); got != "noctalia msg osd-disable|noctalia msg osd-enable" {
		t.Fatalf("one disable and one enable for nested sections: %s", got)
	}
	_ = time.Second
}
