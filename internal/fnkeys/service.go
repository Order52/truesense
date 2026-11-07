package fnkeys

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"sync"
	"time"

	"github.com/Order52/truesense/internal/actions"
	"github.com/Order52/truesense/internal/config"
	"github.com/Order52/truesense/internal/keybinds"
)

// Reports is the stream of input reports of the keyboard's Fn-key interface. Read blocks for
// the next report; it is behind an interface so a fake can feed reports in tests.
type Reports interface {
	Read() ([]byte, error)
	Close() error
}

// ActionRunner runs one action: *actions.Runner.
type ActionRunner interface {
	Run(ctx context.Context, spec string) (string, error)
}

// OSD shows a message on screen: *osd.Notifier.
type OSD interface {
	Show(text, icon string) bool
}

// Clock is the time source of the service. It is injectable so the loop can be driven
// deterministically; After returns a channel that receives once d has passed and a function
// that releases the timer.
type Clock interface {
	Now() time.Time
	After(d time.Duration) (<-chan time.Time, func())
}

// RealClock is the wall clock.
type RealClock struct{}

// Now is the current time.
func (RealClock) Now() time.Time { return time.Now() }

// After waits on a real timer.
func (RealClock) After(d time.Duration) (<-chan time.Time, func()) {
	t := time.NewTimer(d)
	return t.C, func() { t.Stop() }
}

// Env is what the service needs from the outside. Only Actions and Config are required.
type Env struct {
	// SysRoot and DevRoot are the prefixes of /sys and /dev ("" = real), used to find the node.
	SysRoot, DevRoot string
	// Open opens the hidraw node; nil opens the file itself.
	Open func(path string) (Reports, error)
	// Config reads the effective configuration; it is read at start and again whenever the
	// user's config file changes.
	Config func() (*config.Config, error)
	// ConfigPath is the user config file watched for changes (default config.UserPath()).
	ConfigPath string
	Actions    ActionRunner
	// Validate checks an action at start and on every reload; nil uses actions.Validate.
	Validate func(spec string) error
	OSD      OSD
	Clock    Clock
	Log      *slog.Logger
	// ActionTimeout bounds one action (default 30 s); PollEvery is how often the config file
	// is looked at (default 1.5 s).
	ActionTimeout, PollEvery time.Duration
	// Background are loops that run for as long as the service does (the per-key lighting
	// animations): each is started once and must return when its context ends.
	Background []func(ctx context.Context)
}

// Defaults of Env's timings.
const (
	DefaultActionTimeout = 30 * time.Second
	DefaultPollEvery     = 1500 * time.Millisecond
)

// OSD icon for every message of this service.
const osdIcon = "power"

type service struct {
	env      Env
	log      *slog.Logger
	clock    Clock
	m        *Machine
	bindings keybinds.Bindings
	cfgMTime int64 // 0 = the file does not exist
	ctx      context.Context
	wg       sync.WaitGroup
	spawn    func(func())
}

func (e Env) withDefaults() Env {
	if e.Clock == nil {
		e.Clock = RealClock{}
	}
	if e.Log == nil {
		e.Log = slog.New(slog.NewTextHandler(discard{}, nil))
	}
	if e.ActionTimeout <= 0 {
		e.ActionTimeout = DefaultActionTimeout
	}
	if e.PollEvery <= 0 {
		e.PollEvery = DefaultPollEvery
	}
	if e.Validate == nil {
		e.Validate = actions.Validate
	}
	if e.ConfigPath == "" {
		e.ConfigPath = config.UserPath()
	}
	if e.Open == nil {
		e.Open = openHidraw
	}
	return e
}

type discard struct{}

func (discard) Write(p []byte) (int, error) { return len(p), nil }

// Run is the key service: it finds the Fn-key node, then reads reports until ctx ends (nil)
// or the node fails (an error: the supervisor restarts the service, which then finds the
// keyboard again). An action never blocks the event loop and its failure never stops the
// service: it is logged and shown on screen.
func Run(ctx context.Context, env Env) error {
	env = env.withDefaults()
	cfg, err := env.Config()
	if err != nil {
		return fmt.Errorf("read configuration: %w", err)
	}
	node, err := FindNode(env.SysRoot, env.DevRoot)
	if err != nil {
		return err
	}
	rep, err := env.Open(node)
	if err != nil {
		return fmt.Errorf("open %s: %w", node, err)
	}
	s := newService(ctx, env, cfg)
	s.log.Info("listening", "node", node, "bindings", s.bindingsText())
	return s.loop(ctx, rep)
}

func newService(ctx context.Context, env Env, cfg *config.Config) *service {
	s := &service{env: env, log: env.Log, clock: env.Clock, ctx: ctx}
	s.spawn = func(f func()) {
		s.wg.Add(1)
		go func() {
			defer s.wg.Done()
			f()
		}()
	}
	s.bindings = keybinds.FromConfig(cfg).Bindings
	for _, g := range keybinds.Gestures() { // a typo in the config must not break the key at press time
		if err := env.Validate(s.bindings.Action(g)); err != nil {
			s.log.Warn("invalid key binding; using 'none'", "gesture", g, "action", s.bindings.Action(g), "error", err)
			s.bindings.SetAction(g, "none")
		}
	}
	s.bindings.LongMS, s.bindings.MultiMS = timingOr(cfg.Keys.LongMS, keybinds.Default().LongMS), timingOr(cfg.Keys.MultiMS, keybinds.Default().MultiMS)
	s.m = NewMachine(ms(s.bindings.LongMS), ms(s.bindings.MultiMS))
	s.cfgMTime = s.statConfig()
	return s
}

// timingOr guards the timers against a nonsense value (a zero would make every press a long
// press).
func timingOr(v, def int) int {
	if v <= 0 {
		return def
	}
	return v
}

func ms(n int) time.Duration { return time.Duration(n) * time.Millisecond }

func (s *service) bindingsText() string {
	return fmt.Sprintf("tap=%q double=%q triple=%q long=%q fn_esc=%q long_ms=%d multi_ms=%d",
		s.bindings.Tap, s.bindings.Double, s.bindings.Triple, s.bindings.Long, s.bindings.FnEsc, s.bindings.LongMS, s.bindings.MultiMS)
}

type readResult struct {
	report []byte
	err    error
}

func (s *service) loop(ctx context.Context, rep Reports) error {
	ctx, cancel := context.WithCancel(ctx)
	s.ctx = ctx
	var bg sync.WaitGroup
	defer func() {
		cancel()
		_ = rep.Close() // unblocks the reader
		bg.Wait()
		s.wg.Wait()
	}()
	for _, f := range s.env.Background {
		bg.Add(1)
		go func() {
			defer bg.Done()
			f(ctx)
		}()
	}

	reads := make(chan readResult)
	go func() {
		for {
			r, err := rep.Read()
			select {
			case reads <- readResult{r, err}:
			case <-ctx.Done():
				return
			}
			if err != nil {
				return
			}
		}
	}()

	pollAt := s.clock.Now().Add(s.env.PollEvery)
	for {
		wake := pollAt
		if d, ok := s.m.Deadline(); ok && d.Before(wake) {
			wake = d
		}
		timer, stop := s.clock.After(max(0, wake.Sub(s.clock.Now())))
		select {
		case <-ctx.Done():
			stop()
			return nil
		case r := <-reads:
			stop()
			if r.err != nil {
				if ctx.Err() != nil {
					return nil
				}
				return fmt.Errorf("read Fn-key reports: %w", r.err)
			}
			s.handleReport(r.report, s.clock.Now())
		case <-timer:
			stop()
			now := s.clock.Now()
			s.advance(now)
			if !now.Before(pollAt) {
				s.reload()
				pollAt = now.Add(s.env.PollEvery)
			}
		}
	}
}

// handleReport routes one input report.
func (s *service) handleReport(r []byte, now time.Time) {
	switch Decode(r) {
	case EventNone:
	case EventUnknown:
		s.log.Info("ignored vendor report", "report", hexOf(r))
	case EventPredatorDown:
		s.fire(s.m.Press(now))
	case EventRelease:
		s.fire(s.m.Release(now))
	case EventFnF10:
		s.dispatch("fn_f10", "touchpad-toggle")
	case EventFnEsc:
		s.dispatch("fn_esc", s.bindings.FnEsc)
	}
}

func (s *service) advance(now time.Time) { s.fire(s.m.Advance(now)) }

func (s *service) fire(gs []Gesture) {
	for _, g := range gs {
		s.dispatch(string(g), s.bindings.Action(string(g)))
	}
}

// dispatch runs the action in its own goroutine under a timeout, so a slow action (a profile
// that restarts the sound system) never stalls the key; the outcome is logged and shown. A
// panic in an action is contained: the service must outlive every action.
func (s *service) dispatch(label, spec string) {
	s.spawn(func() {
		ctx, cancel := context.WithTimeout(s.ctx, s.env.ActionTimeout)
		defer cancel()
		msg, err := s.runAction(ctx, spec)
		switch {
		case err != nil && s.ctx.Err() != nil: // shutting down
		case err != nil:
			s.log.Error("key action failed", "gesture", label, "action", spec, "error", err)
			s.show("TrueSense: " + err.Error())
		default:
			if msg == "" {
				s.log.Info("key action", "gesture", label, "action", spec, "result", "ok")
				return
			}
			s.log.Info("key action", "gesture", label, "action", spec, "result", msg)
			s.show(msg)
		}
	})
}

type actionResult struct {
	msg string
	err error
}

// runAction runs the action and gives up waiting at the deadline: the backends are not all
// cancellable, and a hung one must not leave the key dead (it is abandoned, not killed).
func (s *service) runAction(ctx context.Context, spec string) (string, error) {
	done := make(chan actionResult, 1)
	go func() {
		defer func() {
			if p := recover(); p != nil {
				done <- actionResult{err: fmt.Errorf("action panicked: %v", p)}
			}
		}()
		msg, err := s.env.Actions.Run(ctx, spec)
		done <- actionResult{msg, err}
	}()
	select {
	case r := <-done:
		return r.msg, r.err
	case <-ctx.Done():
		if errors.Is(ctx.Err(), context.DeadlineExceeded) {
			return "", fmt.Errorf("%s timed out after %s", spec, s.env.ActionTimeout)
		}
		return "", ctx.Err()
	}
}

func (s *service) show(text string) {
	if s.env.OSD != nil {
		s.env.OSD.Show(text, osdIcon)
	}
}

func (s *service) statConfig() int64 {
	fi, err := os.Stat(s.env.ConfigPath)
	if err != nil {
		return 0
	}
	return fi.ModTime().UnixNano()
}

// reload picks up changes made in the window or with `truesense keys ...` without a restart:
// when the user config's modification time changes, the bindings and timings are re-read. A
// binding that no longer validates keeps its previous action; a file that cannot be read
// keeps everything.
func (s *service) reload() {
	m := s.statConfig()
	if m == s.cfgMTime {
		return
	}
	s.cfgMTime = m
	cfg, err := s.env.Config()
	if err != nil { // half-written or invalid file: keep what we have
		s.log.Warn("key bindings not reloaded", "error", err)
		return
	}
	fresh := keybinds.FromConfig(cfg).Bindings
	for _, g := range keybinds.Gestures() {
		if err := s.env.Validate(fresh.Action(g)); err == nil {
			s.bindings.SetAction(g, fresh.Action(g))
		} else {
			s.log.Warn("invalid key binding kept as it was", "gesture", g, "action", fresh.Action(g), "error", err)
		}
	}
	s.bindings.LongMS, s.bindings.MultiMS = timingOr(fresh.LongMS, s.bindings.LongMS), timingOr(fresh.MultiMS, s.bindings.MultiMS)
	s.m.SetTiming(ms(s.bindings.LongMS), ms(s.bindings.MultiMS))
	s.log.Info("key bindings reloaded", "bindings", s.bindingsText())
}

// openHidraw opens the real node.
func openHidraw(path string) (Reports, error) {
	f, err := os.Open(path) //nolint:gosec // path comes from FindNode
	if err != nil {
		return nil, err
	}
	return &fileReports{f: f}, nil
}

type fileReports struct {
	f   *os.File
	buf [64]byte
}

// Read returns one report. hidraw hands out exactly one report per read.
func (r *fileReports) Read() ([]byte, error) {
	n, err := r.f.Read(r.buf[:])
	if err != nil {
		return nil, err
	}
	return append([]byte(nil), r.buf[:n]...), nil
}

func (r *fileReports) Close() error { return r.f.Close() }
