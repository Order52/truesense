package app

import (
	"context"
	"errors"
	"io"
	"strings"
	"testing"

	"github.com/Order52/truesense/internal/apitest"
	"github.com/Order52/truesense/internal/gui"
)

type windowRig struct {
	w      window
	opened []string
	ran    []gui.Options
}

func newRig(resumeURL string, runErr error) *windowRig {
	r := &windowRig{}
	r.w = window{
		resume: func() (string, bool) { return resumeURL, resumeURL != "" },
		open:   func(u string) error { r.opened = append(r.opened, u); return nil },
		run:    func(_ context.Context, o gui.Options) error { r.ran = append(r.ran, o); return runErr },
		log:    io.Discard,
	}
	return r
}

func TestAWindowThatRunsIsBroughtForward(t *testing.T) {
	f := apitest.New(t)
	r := newRig("http://127.0.0.1:5555/#code=abc", nil)
	if err := r.w.showWindow(t.Context(), f.API(), f.S, "keys"); err != nil {
		t.Fatal(err)
	}
	if len(r.ran) != 0 || len(r.opened) != 1 || !strings.Contains(r.opened[0], "page=keys") || !strings.Contains(r.opened[0], "code=abc") {
		t.Fatalf("%+v", r)
	}
}

func TestWithoutAWindowTheServerIsStartedOnThePage(t *testing.T) {
	f := apitest.New(t)
	r := newRig("", nil)
	if err := r.w.showWindow(t.Context(), f.API(), f.S, "audio"); err != nil {
		t.Fatal(err)
	}
	if len(r.ran) != 1 || r.ran[0].Page != "audio" || r.ran[0].Commands == nil || r.ran[0].Version == "" || len(r.opened) != 0 {
		t.Fatalf("%+v", r)
	}
	// the commands are the real ones, over the simulator
	res, err := r.ran[0].Commands.Do(t.Context(), "sensors-not-a-command", "x", nil)
	if err == nil || !errors.Is(err, gui.ErrUnknownCommand) || res != nil {
		t.Fatalf("%v %v", res, err)
	}
}

func TestTheLoserOfTheRaceBringsTheWinnersWindowForward(t *testing.T) {
	f := apitest.New(t)
	r := newRig("", gui.ErrRunning)
	calls := 0
	r.w.resume = func() (string, bool) {
		calls++
		return "http://127.0.0.1:5556/#code=x", calls > 1 // nothing before the attempt, the winner's window after it
	}
	if err := r.w.showWindow(t.Context(), f.API(), f.S, ""); err != nil {
		t.Fatal(err)
	}
	if len(r.opened) != 1 {
		t.Fatalf("%+v", r.opened)
	}
	// a server that cannot be resumed after ErrRunning is an error, not silence
	r = newRig("", gui.ErrRunning)
	if err := r.w.showWindow(t.Context(), f.API(), f.S, ""); !errors.Is(err, gui.ErrRunning) {
		t.Fatalf("%v", err)
	}
	r = newRig("", errors.New("no port"))
	if err := r.w.showWindow(t.Context(), f.API(), f.S, ""); err == nil || err.Error() != "no port" {
		t.Fatalf("%v", err)
	}
}

func TestAnOpenWindowIsToldToShowThePageAndRaisedNotDuplicated(t *testing.T) {
	f := apitest.New(t)
	r := newRig("http://127.0.0.1:5555/#code=abc", nil)
	var shown []string
	r.w.show = func(page string) bool { shown = append(shown, page); return true }
	r.w.raise = func() bool { return true }
	if err := r.w.showWindow(t.Context(), f.API(), f.S, "curves"); err != nil {
		t.Fatal(err)
	}
	if len(shown) != 1 || shown[0] != "curves" || len(r.opened) != 0 || len(r.ran) != 0 {
		t.Fatalf("a window that is open must be reused: shown %v, opened %v", shown, r.opened)
	}
}

func TestIfTheWindowCannotBeRaisedANewOneIsOpenedSoTheKeyPressStillDoesSomething(t *testing.T) {
	f := apitest.New(t)
	r := newRig("http://127.0.0.1:5555/#code=abc", nil)
	r.w.show = func(string) bool { return true }
	r.w.raise = func() bool { return false } // a desktop with no way to focus another program's window
	if err := r.w.showWindow(t.Context(), f.API(), f.S, "home"); err != nil {
		t.Fatal(err)
	}
	if len(r.opened) != 1 {
		t.Fatalf("a launch must show something: %v", r.opened)
	}
	r2 := newRig("http://127.0.0.1:5555/#code=abc", nil)
	r2.w.show = func(string) bool { return false } // a server with no page connected
	r2.w.raise = func() bool { t.Fatal("nothing to raise when no page heard the request"); return true }
	if err := r2.w.showWindow(t.Context(), f.API(), f.S, "home"); err != nil || len(r2.opened) != 1 {
		t.Fatalf("%v %v", err, r2.opened)
	}
}
