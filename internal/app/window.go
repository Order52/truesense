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
	open   func(url string) error
	run    func(ctx context.Context, o gui.Options) error
	log    io.Writer
}

func realWindow(stderr io.Writer) window {
	return window{resume: gui.Resume, open: gui.Open, run: gui.Run, log: stderr}
}

// show brings this user's window forward when one runs, and starts the window server (which
// opens the window and serves it as this user) when not. Two commands racing for the server
// end with the loser bringing the winner's window forward.
func (w window) show(ctx context.Context, a *api.API, s *assembly.Services, page string) error {
	if url, ok := w.resume(); ok {
		return w.open(gui.WithPage(url, page))
	}
	err := w.run(ctx, gui.Options{
		Commands: adapt.GUI(a, s), Page: page, Version: buildinfo.Version,
		Log: slog.New(slog.NewTextHandler(w.log, &slog.HandlerOptions{Level: slog.LevelInfo})),
	})
	if errors.Is(err, gui.ErrRunning) {
		if url, ok := w.resume(); ok {
			return w.open(gui.WithPage(url, page))
		}
	}
	return err
}
