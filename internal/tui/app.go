package tui

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

// Defaults of the app's timing.
const (
	pageCount     = 7
	logKeep       = 40
	drainTimeout  = 4 * time.Second
	hintSeconds   = 5.0
	errorSeconds  = 8.0
	holdMinutes   = 30
	delaySlider   = 300 * time.Millisecond
	delayChoice   = 350 * time.Millisecond
	delayDefault  = 300 * time.Millisecond
	delayCycleKey = 250 * time.Millisecond
)

// Config is what an App needs; everything outside the process comes in here.
type Config struct {
	Canvas  Canvas
	Backend Backend
	// Clock defaults to time.Now.
	Clock Clock
	// Sync makes writes run on the spot and reads on the caller's goroutine; for tests.
	Sync bool
	// Version is the program version shown in the header and on the System page.
	Version string
	// Context bounds every backend call; defaults to context.Background().
	Context context.Context
}

type logEntry struct {
	at    string
	text  string
	style Style
}

// message is the line above the keys; ctx ties a hint to the row (page, row) it belongs to.
type message struct {
	text  string
	style Style
	until time.Time
	ctx   *pos
}

type pos struct{ page, row int }

type hitZone int

const (
	zoneRow hitZone = iota
	zoneToggle
	zoneButton
	zoneOpt
	zonePrev
	zoneNext
	zoneBar
)

// hit is a clickable rectangle drawn on the last frame; extra is the option index, or the
// bar width for a slider.
type hit struct {
	y, x0, x1 int
	row       *Row
	zone      hitZone
	extra     int
}

type tabHit struct{ x0, x1, page int }

type dragState struct {
	row   *Row
	x0    int
	width int
}

// App is the whole screen as a state machine: feed it Key and Mouse events and a clock,
// call Draw, and read the Canvas. The tcell loop (Run) does exactly that.
//
// All state is guarded by one mutex taken by the exported methods and by the completion of
// a write, so the Worker's goroutine and the event loop never race. A sync App (tests) has
// one goroutine and takes no lock.
type App struct {
	mu   sync.Mutex
	sync bool

	cv      Canvas
	b       Backend
	ctx     context.Context
	now     Clock
	version string
	caps    KeyboardCaps

	data   *Data
	worker *Worker

	pages  []*Page
	pageI  int
	acting *Row

	pending []*Row
	kb      map[string]any
	rear    map[string]any
	lastOn  map[string]int
	drag    *dragState

	log       []logEntry
	msg       message
	modal     *modal
	hits      []hit
	tabHits   []tabHit
	modalHits []modalHit

	quit    bool
	repaint bool
	dirty   atomic.Bool
	wake    chan struct{}

	yes int // Yes answers given: the stress test compares them with the stops written
}

// NewApp builds the pages and an empty Data; call Data().Fast / Refresh or start the loops.
func NewApp(cfg Config) *App {
	a := &App{
		sync: cfg.Sync, cv: cfg.Canvas, b: cfg.Backend, now: cfg.Clock, version: cfg.Version, ctx: cfg.Context,
		kb: map[string]any{}, rear: map[string]any{}, lastOn: map[string]int{"kb": 25, "rear": 100},
		wake: make(chan struct{}, 1), worker: NewWorker(cfg.Sync), msg: message{style: StDim},
	}
	if a.now == nil {
		a.now = time.Now
	}
	if a.ctx == nil {
		a.ctx = context.Background()
	}
	a.caps = a.b.KeyboardCaps()
	a.data = NewData(a.b, a.touch)
	a.pages = a.build()
	for _, p := range a.pages {
		p.init(a.now)
	}
	a.touch()
	return a
}

// Data exposes the readings (the event loop starts its goroutines).
func (a *App) Data() *Data { return a.data }

func (a *App) lock() {
	if !a.sync {
		a.mu.Lock()
	}
}

func (a *App) unlock() {
	if !a.sync {
		a.mu.Unlock()
	}
}

// touch marks the screen as out of date and wakes the event loop.
func (a *App) touch() {
	a.dirty.Store(true)
	select {
	case a.wake <- struct{}{}:
	default:
	}
}

// Dirty reports (and clears) whether the screen needs drawing.
func (a *App) Dirty() bool { return a.dirty.Swap(false) }

// Wake is signalled whenever the screen may need drawing.
func (a *App) Wake() <-chan struct{} { return a.wake }

// Quit reports whether the user asked to leave.
func (a *App) Quit() bool { a.lock(); defer a.unlock(); return a.quit }

// RequestQuit asks to leave (Ctrl-C).
func (a *App) RequestQuit() { a.lock(); a.quit = true; a.unlock() }

// NeedsFastTick is true while a change waits for its pause or a drag is on, so the event
// loop wakes sooner.
func (a *App) NeedsFastTick() bool {
	a.lock()
	defer a.unlock()
	return len(a.pending) > 0 || a.drag != nil
}

// TakeRepaint reports (and clears) a request to redraw the whole terminal (Ctrl-L).
func (a *App) TakeRepaint() bool {
	a.lock()
	defer a.unlock()
	r := a.repaint
	a.repaint = false
	return r
}

// ---- feedback

func (a *App) ctxPos() pos { return pos{a.pageI, a.page().sel} }

// say shows a line at the bottom. here ties it to the selected row: it is gone when you
// move to another row or page.
func (a *App) say(text string, st Style, secs float64, here bool) {
	m := message{text: text, style: st, until: a.now().Add(time.Duration(secs * float64(time.Second)))}
	if here {
		p := a.ctxPos()
		m.ctx = &p
	}
	a.msg = m
	if len(text) > 0 && !strings.HasPrefix(text, "…") && (st == StGood || st == StBad) && !here {
		a.log = append(a.log, logEntry{a.now().Format("15:04:05"), text, st})
		if len(a.log) > logKeep {
			a.log = a.log[len(a.log)-logKeep:]
		}
	}
	a.touch()
}

// moved is called when the selection or the page changed: a hint of the old place goes away.
func (a *App) moved() {
	if a.msg.ctx != nil && *a.msg.ctx != a.ctxPos() {
		a.msg = message{style: StDim}
	}
}

// runOpts tunes App.run.
type runOpts struct {
	row    *Row
	key    string
	okText string
	ok     func(any) string
	undo   func()
}

// run queues fn (a hardware write, 100 ms or more) behind earlier writes, says what
// happened and reads everything again afterwards. A write that fails puts the shown value
// back (and calls undo). The row defaults to the one last acted on.
func (a *App) run(label string, fn func() (any, error), o runOpts) {
	row := o.row
	if row == nil {
		row = a.acting
	}
	key := o.key
	if key == "" && row != nil {
		key = fmt.Sprintf("row:%p", row)
	}
	a.say("… "+label, StDim, 30, false)
	if row != nil {
		row.state = StateWriting
	}
	work := func() {
		res, err := safely(fn)
		a.lock()
		defer a.unlock()
		text, good := label, true
		switch {
		case err != nil:
			text, good = fmt.Sprintf("%s: %s", label, errText(err)), false
		case o.ok != nil:
			text = o.ok(res)
		case o.okText != "":
			text = o.okText
		}
		if !good && o.undo != nil {
			o.undo()
		}
		if row != nil {
			row.settle(good)
		}
		if good {
			a.say(text, StGood, hintSeconds, false)
		} else {
			a.say(text, StBad, errorSeconds, false)
		}
		a.refresh()
	}
	var drop func()
	if row != nil {
		drop = func() { row.settle(true) }
	}
	a.worker.Submit(key, work, drop)
}

// safely runs a backend call and turns a panic into an error: a row must never stay
// marked as writing because a call crashed.
func safely(fn func() (any, error)) (res any, err error) {
	defer func() {
		if p := recover(); p != nil {
			res, err = nil, fmt.Errorf("internal error: %v", p)
		}
	}()
	return fn()
}

// Refresh reads everything again.
func (a *App) Refresh() { a.lock(); defer a.unlock(); a.refresh() }

func (a *App) refresh() {
	a.data.Wake()
	if reads := a.page().reads; len(reads) > 0 {
		if a.sync {
			a.data.SlowSync(a.ctx, reads)
		} else {
			a.data.Slow(a.ctx, reads)
		}
	}
	a.touch()
}

func (a *App) page() *Page { return a.pages[a.pageI] }

// Goto opens a page (modulo the number of pages).
func (a *App) Goto(i int) { a.lock(); defer a.unlock(); a.gotoPage(i) }

func (a *App) gotoPage(i int) {
	n := len(a.pages)
	a.pageI = ((i % n) + n) % n
	a.hits = nil
	if a.worker.Idle() && len(a.pending) == 0 {
		clear(a.kb)
		clear(a.rear)
	}
	a.moved()
	a.refresh()
}

// PageIndex finds a page by name (case-insensitive) or by its number 1-7.
func PageIndex(name string) (int, bool) {
	for i, n := range pageNames {
		if strings.EqualFold(n, name) || name == fmt.Sprint(i+1) {
			return i, true
		}
	}
	return 0, false
}

// ---- changing a value: choose (shown at once) -> pause -> guard (ask) -> write (queued) -> settle

func (a *App) delayFor(r *Row) time.Duration {
	switch r.Kind {
	case KindSlider:
		return delaySlider
	case KindChoice:
		return delayChoice
	}
	return delayDefault
}

// edit shows v for the row at once and writes it after a short pause (so a run of key
// presses is one write), or immediately when now is true.
func (a *App) edit(r *Row, v any, now bool) {
	a.editAfter(r, v, now, a.delayFor(r))
}

func (a *App) editAfter(r *Row, v any, now bool, delay time.Duration) {
	r.setLocal(v)
	a.acting = r
	if now {
		a.commit(r)
		return
	}
	r.due = a.now().Add(delay)
	if !a.isPending(r) {
		a.pending = append(a.pending, r)
	}
	a.touch()
}

func (a *App) isPending(r *Row) bool {
	for _, p := range a.pending {
		if p == r {
			return true
		}
	}
	return false
}

func (a *App) unpend(r *Row) {
	for i, p := range a.pending {
		if p == r {
			a.pending = append(a.pending[:i], a.pending[i+1:]...)
			return
		}
	}
}

// commit sends a chosen value, or asks first when the row has a question for it.
func (a *App) commit(r *Row) {
	a.unpend(r)
	v, ok := r.local, r.hasLocal
	r.due = time.Time{}
	if !ok || r.state != StatePending {
		return
	}
	if r.Guard != nil {
		if q := r.Guard(v); q != "" {
			r.state = StateAsking
			a.confirm(q, func() { a.send(r, v) }, func() { r.forget() })
			return
		}
	}
	a.send(r, v)
}

func (a *App) send(r *Row, v any) {
	r.state = StateWriting
	a.acting = r
	if r.Put != nil {
		r.Put(v)
	}
}

// undoAll takes back every change that has not been sent yet (Esc).
func (a *App) undoAll() {
	n := 0
	for _, p := range a.pages {
		for _, r := range p.rows {
			if r.state == StatePending || r.state == StateAsking {
				r.forget()
				a.unpend(r)
				n++
			}
		}
	}
	if n > 0 {
		a.say("Change undone: nothing was sent", StDim, 3, false)
	}
}

// press runs a button: one at a time, and its result belongs to this row.
func (a *App) press(r *Row) {
	if r.state == StateWriting {
		a.say("Still working on that…", StDim, 2, true)
		return
	}
	a.acting = r
	if r.Press != nil {
		r.Press()
	}
}

// Tick sends the changes whose pause is over; the event loop calls it every cycle.
func (a *App) Tick() {
	a.lock()
	defer a.unlock()
	a.tick()
}

func (a *App) tick() {
	now := a.now()
	var due []*Row
	for _, r := range a.pending {
		if !r.due.IsZero() && !now.Before(r.due) {
			due = append(due, r)
		}
	}
	for _, r := range due {
		a.commit(r)
	}
}

// Flush is called on quit: send what is still waiting, then wait for the writes to finish
// (a change made a moment ago must not be lost). A question that was not answered is a No.
func (a *App) Flush() {
	a.lock()
	for _, r := range append([]*Row(nil), a.pending...) {
		if r.Guard != nil && r.Guard(r.local) != "" {
			r.forget()
			a.unpend(r)
		} else {
			a.commit(r)
		}
	}
	a.unlock()
	a.worker.Drain(drainTimeout)
}
