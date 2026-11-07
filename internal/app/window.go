package app

import (
	"context"
	"errors"
	"io"
	"log/slog"

	"github.com/Order52/truesense/internal/adapt"
	"github.com/Order52/truesense/internal/api"
	"github.com/Order52/truesense/internal/assembly"
	"github.com/Order52/truesense/internal/buildinfo"
	"github.com/Order52/truesense/internal/gui"
)

// window is `truesense gui`: every effect of showing the window is a field, so the order of
// "bring the running window forward, else start one" is tested without a browser or a server.
type window struct {
	resume func() (string, bool)
	show   func(page string) bool // ask the open window to go to a page
	raise  func() bool            // bring that window to the front
	open   func(url string) error
	run    func(ctx context.Context, o gui.Options) error
	log    io.Writer
}

func realWindow(stderr io.Writer) window {
	return window{resume: gui.Resume, show: gui.Show, raise: gui.Raise, open: gui.Open, run: gui.Run, log: stderr}
}

// forward uses the window that is already open: it is told to show the page and brought to the
// front, so a second launch (the PREDATOR key) never piles up windows. It reports whether that
// worked; when the desktop cannot raise a window, a new one is opened instead (it is visible).
func (w window) forward(page string) bool {
	return w.show != nil && w.raise != nil && w.show(page) && w.raise()
}

// showWindow brings this user's window forward when one runs, and starts the window server (which
// opens the window and serves it as this user) when not. Two commands racing for the server
// end with the loser bringing the winner's window forward.
func (w window) showWindow(ctx context.Context, a *api.API, s *assembly.Services, page string) error {
	if w.forward(page) {
		return nil
	}
	if url, ok := w.resume(); ok {
		return w.open(gui.WithPage(url, page))
	}
	err := w.run(ctx, gui.Options{
		Commands: adapt.GUI(a, s), Page: page, Version: buildinfo.Version,
		Log: slog.New(slog.NewTextHandler(w.log, &slog.HandlerOptions{Level: slog.LevelInfo})),
	})
	if errors.Is(err, gui.ErrRunning) {
		if w.forward(page) {
			return nil
		}
		if url, ok := w.resume(); ok {
			return w.open(gui.WithPage(url, page))
		}
	}
	return err
}
