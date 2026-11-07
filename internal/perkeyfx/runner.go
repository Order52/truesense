package perkeyfx

import (
	"context"
	"slices"
	"time"

	"github.com/Order52/truesense/internal/keyboard"
	"github.com/Order52/truesense/internal/keymap"
)

// Timing of the runner. The keyboard takes about 190 ms for a full upload, so the animation
// draws one frame per FrameInterval after the upload, not on a fixed clock.
const (
	// FrameInterval is the pause after each frame.
	FrameInterval = 200 * time.Millisecond
	// IdleInterval is how often the settings are looked at while no animation is on (a cheap file check).
	IdleInterval = 500 * time.Millisecond
	// ErrorBackoff is the extra pause after a failed upload (the keyboard may be re-enumerating after suspend).
	ErrorBackoff = time.Second
	// BrightnessRefresh is how often the keyboard's brightness is read again, so Fn+F11/F12 changes
	// are followed without a transfer per frame.
	BrightnessRefresh = 5 * time.Second

	defaultBrightness = 25
)

// Sender is the keyboard as the runner uses it.
type Sender interface {
	Brightness() (int, error)
	// PerKeyFrame uploads a picture without remembering it as the look (keyboard.KeepBrightness keeps the current level).
	PerKeyFrame(cells []keymap.RGB, brightness int) error
	CurrentCells() []keymap.RGB
}

// Runner draws the animation described by the settings. Everything it depends on is injected:
// the settings source, the keyboard, the clock and the wait, so the timing logic runs in tests
// without a keyboard or real time.
type Runner struct {
	Settings func() Settings
	Sender   Sender
	Now      func() time.Time
	// Wait pauses for d or until ctx ends (returning its error).
	Wait func(ctx context.Context, d time.Duration) error

	running    bool
	bright     int
	brightAt   time.Time
	lastFrame  []keymap.RGB
	lastBright int
	haveLast   bool
}

// NewRunner returns a Runner on the wall clock.
func NewRunner(settings func() Settings, sender Sender) *Runner {
	return &Runner{Settings: settings, Sender: sender, Now: time.Now, Wait: sleepCtx}
}

func sleepCtx(ctx context.Context, d time.Duration) error {
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-t.C:
		return nil
	}
}

// Run draws until ctx ends. It is idle (a cheap check twice a second) unless an animation is
// switched on, and hands the base colours back when one is switched off or ctx ends mid-animation.
func (r *Runner) Run(ctx context.Context) {
	for ctx.Err() == nil {
		if r.Wait(ctx, r.Step()) != nil {
			break
		}
	}
	r.handBack() // a stop while animating must not leave the last frame on the keyboard
}

// Step does one iteration of the loop and returns how long to wait before the next.
func (r *Runner) Step() time.Duration {
	fx := r.Settings()
	if base, ok := parseBase(fx.Base); ok && IsMode(fx.Mode) && len(base) > 0 {
		return r.frame(fx, base)
	}
	if r.running {
		r.handBackBase(fx)
	}
	return IdleInterval
}

func (r *Runner) frame(fx Settings, base []keymap.RGB) time.Duration {
	now := r.Now()
	if r.bright == 0 {
		r.bright = defaultBrightness
	}
	if r.brightAt.IsZero() || now.Sub(r.brightAt) > BrightnessRefresh {
		if b, err := r.Sender.Brightness(); err == nil && b > 0 {
			r.bright = b // an unreadable or dark keyboard keeps the last level: never animate into darkness
		}
		r.brightAt = now
	}
	t := Seconds(now) - fx.T0
	if fx.T0 == 0 {
		t = 0
	}
	frame := Render(fx.Mode, t, base, fx.Keys, fx.Speed)
	// an identical frame is not sent again (less USB traffic)
	if !r.haveLast || r.lastBright != r.bright || !slices.Equal(frame, r.lastFrame) {
		if err := r.Sender.PerKeyFrame(frame, r.bright); err != nil {
			r.haveLast = false
			return ErrorBackoff + FrameInterval
		}
		r.running, r.haveLast, r.lastFrame, r.lastBright = true, true, frame, r.bright
	}
	return FrameInterval
}

// handBackBase puts the base colours back after an animation was switched off.
func (r *Runner) handBackBase(fx Settings) {
	cells, ok := parseBase(fx.Base)
	if !ok || len(cells) == 0 {
		cells = r.Sender.CurrentCells()
	}
	_ = r.Sender.PerKeyFrame(cells, keyboard.KeepBrightness) // best effort: the keyboard may be away
	r.running, r.haveLast = false, false
}

func (r *Runner) handBack() {
	if r.running {
		r.handBackBase(r.Settings())
	}
}
