package tui

import (
	"context"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/gdamore/tcell/v2"
)

// Options configure Run.
type Options struct {
	// Page opens a page directly: a name (fans) or a number (2); empty is the dashboard.
	Page string
	// Version is shown in the header and on the System page.
	Version string
	// Screen is the terminal to draw on. Nil opens the real terminal, which Run also
	// finalises on return. A screen passed in must already be initialised and stays
	// the caller's: the tests pass a tcell.SimulationScreen they size and inspect.
	Screen tcell.Screen
	// ASCII forces the plain character set (the default follows the locale).
	ASCII bool
	// Clock replaces time.Now.
	Clock Clock
}

const (
	idleTick   = 100 * time.Millisecond
	activeTick = 30 * time.Millisecond
)

// Run shows the terminal UI until the user quits or ctx ends. On the way out it sends the
// changes that are still waiting and waits for the writes to finish.
func Run(ctx context.Context, b Backend, opts Options) error {
	start := 0
	if opts.Page != "" {
		i, ok := PageIndex(opts.Page)
		if !ok {
			return fmt.Errorf("unknown page %q (%s)", opts.Page, strings.ToLower(strings.Join(pageNames[:], ", ")))
		}
		start = i
	}
	scr := opts.Screen
	if scr == nil {
		s, err := tcell.NewScreen()
		if err != nil {
			return fmt.Errorf("open the terminal: %w", err)
		}
		if err := s.Init(); err != nil {
			return fmt.Errorf("start the terminal: %w", err)
		}
		defer s.Fini()
		scr = s
	}
	scr.EnableMouse(tcell.MouseButtonEvents | tcell.MouseDragEvents)
	scr.HideCursor()

	unicode := !opts.ASCII
	if opts.Screen == nil && unicode {
		unicode = unicodeTerminal(os.Getenv)
	}
	color := scr.Colors() >= 8 && os.Getenv("NO_COLOR") == ""
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()

	app := NewApp(Config{Canvas: newTCellCanvas(scr, unicode, color), Backend: b, Clock: opts.Clock, Version: opts.Version, Context: ctx})
	app.Goto(start)
	go app.Data().Loop(ctx)

	events := make(chan tcell.Event, 64)
	done := make(chan struct{})
	defer close(done)
	go func() {
		for {
			ev := scr.PollEvent()
			if ev == nil {
				return
			}
			select {
			case events <- ev:
			case <-done:
				return
			}
		}
	}()

	var mouse mouseState
	timer := time.NewTimer(idleTick)
	defer timer.Stop()
	defer func() { app.Flush() }()
	lastW, lastH := scr.Size()
	for !app.Quit() {
		if w, h := scr.Size(); w != lastW || h != lastH { // a resize the terminal did not announce
			lastW, lastH = w, h
			app.touch()
		}
		if app.Dirty() {
			app.Draw()
			if app.TakeRepaint() {
				scr.Sync()
			} else {
				scr.Show()
			}
		}
		period := idleTick
		if app.NeedsFastTick() {
			period = activeTick
		}
		timer.Reset(period)
		select {
		case <-ctx.Done():
			app.RequestQuit()
		case <-app.Wake():
		case <-timer.C:
		case ev := <-events:
			switch ev := ev.(type) {
			case *tcell.EventKey:
				if k, quit, ok := translateKey(ev); quit {
					app.RequestQuit()
				} else if ok {
					app.Key(k)
				}
			case *tcell.EventMouse:
				mouse.handle(app, ev)
			case *tcell.EventResize:
				scr.Sync()
				app.touch()
			}
		}
		if !timer.Stop() {
			select {
			case <-timer.C:
			default:
			}
		}
		app.Tick()
	}
	return nil
}
