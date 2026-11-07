package gui

import (
	"bufio"
	"context"
	"errors"
	"net/http"
	"os/exec"
	"strings"
	"testing"
	"time"
)

type raiseRig struct {
	have     map[string]bool
	env      map[string]string
	out      map[string]string // command line -> stdout
	ran      []string
	fail     map[string]bool // programs that fail
	failLine map[string]bool // exact command lines that fail
}

func (r *raiseRig) raiser() Raiser {
	return Raiser{
		LookPath: func(n string) (string, error) {
			if r.have[n] {
				return "/usr/bin/" + n, nil
			}
			return "", exec.ErrNotFound
		},
		Getenv: func(k string) string { return r.env[k] },
		Run: func(_ context.Context, name string, args ...string) (string, error) {
			line := name + " " + strings.Join(args, " ")
			r.ran = append(r.ran, line)
			if r.fail[name] || r.failLine[line] {
				return "", errors.New("failed")
			}
			return r.out[line], nil
		},
	}
}

func TestRaiseOnHyprlandFocusesOnlyAWindowThatExists(t *testing.T) {
	r := &raiseRig{have: map[string]bool{"hyprctl": true}, env: map[string]string{"HYPRLAND_INSTANCE_SIGNATURE": "x"},
		out: map[string]string{"hyprctl clients -j": `[{"class": "firefox"}, {"class": "truesense", "title": "TrueSense"}]`}}
	if !r.raiser().Raise() {
		t.Fatalf("a TrueSense window exists: %v", r.ran)
	}
	if got := r.ran[len(r.ran)-1]; got != `hyprctl dispatch hl.dsp.focus({ window = "class:^truesense$" })` {
		t.Fatalf("a current Hyprland takes the Lua dispatcher: %q", got)
	}
	// an older Hyprland rejects the Lua form: the classic one is tried next
	old := &raiseRig{have: map[string]bool{"hyprctl": true}, env: map[string]string{"HYPRLAND_INSTANCE_SIGNATURE": "x"},
		out:      map[string]string{"hyprctl clients -j": `[{"class": "truesense"}]`},
		failLine: map[string]bool{`hyprctl dispatch hl.dsp.focus({ window = "class:^truesense$" })`: true}}
	if !old.raiser().Raise() || old.ran[len(old.ran)-1] != "hyprctl dispatch focuswindow class:^truesense$" {
		t.Fatalf("fallback to the classic dispatcher: %v", old.ran)
	}
	r2 := &raiseRig{have: map[string]bool{"hyprctl": true}, env: map[string]string{"HYPRLAND_INSTANCE_SIGNATURE": "x"}, out: map[string]string{"hyprctl clients -j": `[{"class": "firefox"}]`}}
	if r2.raiser().Raise() || len(r2.ran) != 1 {
		t.Fatalf("no window of ours: nothing is focused and the caller opens one: %v", r2.ran)
	}
}

func TestRaiseOnSwayChecksTheAnswer(t *testing.T) {
	line := `swaymsg [app_id="truesense"] focus`
	ok := &raiseRig{have: map[string]bool{"swaymsg": true}, env: map[string]string{"SWAYSOCK": "/run/s"}, out: map[string]string{line: `[{"success": true}]`}}
	if !ok.raiser().Raise() {
		t.Fatal("sway focused it")
	}
	none := &raiseRig{have: map[string]bool{"swaymsg": true}, env: map[string]string{"SWAYSOCK": "/run/s"}, out: map[string]string{line: `[{"success": false, "error": "No matching node."}]`}}
	if none.raiser().Raise() {
		t.Fatal("sway found nothing: false")
	}
}

func TestRaiseOnX11UsesWmctrlThenXdotool(t *testing.T) {
	r := &raiseRig{have: map[string]bool{"wmctrl": true, "xdotool": true}, env: map[string]string{"DISPLAY": ":0"}}
	if !r.raiser().Raise() || r.ran[0] != "wmctrl -x -a truesense" {
		t.Fatalf("wmctrl first: %v", r.ran)
	}
	r2 := &raiseRig{have: map[string]bool{"xdotool": true}, env: map[string]string{"DISPLAY": ":0"}, out: map[string]string{"xdotool search --class truesense": "4194311\n"}}
	if !r2.raiser().Raise() || r2.ran[len(r2.ran)-1] != "xdotool windowactivate 4194311" {
		t.Fatalf("xdotool: %v", r2.ran)
	}
	r3 := &raiseRig{have: map[string]bool{"xdotool": true}, env: map[string]string{"DISPLAY": ":0"}}
	if r3.raiser().Raise() {
		t.Fatal("no window id: false")
	}
}

func TestRaiseWithoutAnyKnownToolIsFalse(t *testing.T) {
	r := &raiseRig{have: map[string]bool{}, env: map[string]string{"WAYLAND_DISPLAY": "wayland-1"}}
	if r.raiser().Raise() || len(r.ran) != 0 {
		t.Fatal("GNOME or KDE on Wayland: no portable way, so false and the caller opens a window")
	}
}

// A page that has the stream open hears the request; a name that is not a page id becomes home.
func TestAShowRequestReachesTheOpenPageAsAnEvent(t *testing.T) {
	rig := start(t, Options{})
	base, token := rig.parts(t)
	req, _ := http.NewRequestWithContext(t.Context(), http.MethodGet, base+"/api/stream", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	res, err := http.DefaultClient.Do(req) //nolint:bodyclose // closed by the deferred call below
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close() //nolint:errcheck // test
	events := make(chan string, 8)
	go func() {
		sc := bufio.NewScanner(res.Body)
		ev := ""
		for sc.Scan() {
			line := sc.Text()
			switch {
			case strings.HasPrefix(line, "event: "):
				ev = strings.TrimPrefix(line, "event: ")
			case strings.HasPrefix(line, "data: ") && ev == "show":
				events <- strings.TrimPrefix(line, "data: ")
			}
		}
	}()
	time.Sleep(200 * time.Millisecond)
	if !ShowIn(rig.dir, "curves") {
		t.Fatal("a page is connected: ShowIn must report that it heard")
	}
	select {
	case got := <-events:
		if got != `"curves"` {
			t.Fatalf("event data %q", got)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("the page did not get the show event")
	}
	if !ShowIn(rig.dir, `"><script>alert(1)</script>`) {
		t.Fatal("heard")
	}
	select {
	case got := <-events:
		if got != `"home"` {
			t.Fatalf("a name that is not a page id must become home, got %q", got)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("no second event")
	}
}

func TestShowWithNoPageOpenSaysSo(t *testing.T) {
	rig := start(t, Options{})
	if ShowIn(rig.dir, "home") {
		t.Fatal("no page is connected: false, so the caller opens a window")
	}
	if ShowIn(t.TempDir(), "home") {
		t.Fatal("no server at all: false")
	}
}
