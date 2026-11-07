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
	if err := r.w.show(t.Context(), f.API(), f.S, "keys"); err != nil {
		t.Fatal(err)
	}
	if len(r.ran) != 0 || len(r.opened) != 1 || !strings.Contains(r.opened[0], "page=keys") || !strings.Contains(r.opened[0], "code=abc") {
		t.Fatalf("%+v", r)
	}
}

func TestWithoutAWindowTheServerIsStartedOnThePage(t *testing.T) {
	f := apitest.New(t)
	r := newRig("", nil)
	if err := r.w.show(t.Context(), f.API(), f.S, "audio"); err != nil {
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
	if err := r.w.show(t.Context(), f.API(), f.S, ""); err != nil {
		t.Fatal(err)
	}
	if len(r.opened) != 1 {
		t.Fatalf("%+v", r.opened)
	}
	// a server that cannot be resumed after ErrRunning is an error, not silence
	r = newRig("", gui.ErrRunning)
	if err := r.w.show(t.Context(), f.API(), f.S, ""); !errors.Is(err, gui.ErrRunning) {
		t.Fatalf("%v", err)
	}
	r = newRig("", errors.New("no port"))
	if err := r.w.show(t.Context(), f.API(), f.S, ""); err == nil || err.Error() != "no port" {
		t.Fatalf("%v", err)
	}
}
