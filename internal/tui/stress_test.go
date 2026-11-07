package tui

import (
	"math/rand"
	"testing"
	"time"
)

var fuzzKeys = func() []Key {
	var ks []Key
	for _, r := range "jkhlqm r?ynx0123456789\t\n \x1b" {
		ks = append(ks, Key(r))
	}
	return append(ks, KeyUp, KeyDown, KeyLeft, KeyRight, KeyBacktab, KeyHome, KeyEnd, KeyPageDown, KeyPageUp, KeyCtrlL, KeyBackspace, KeyReturn, 'Y', 'N', '#', 'f')
}()

// checkInvariants are the rules that hold after every single event.
func checkInvariants(t *testing.T, h *harness, step int, ctx string) {
	t.Helper()
	a := h.app
	if a.modal == nil || a.modal.kind != modalConfirm {
		for _, p := range a.pages {
			for _, r := range p.rows {
				if r.state == StateAsking {
					t.Fatalf("%s step %d: row %q is asking with no question on screen", ctx, step, r.Label)
				}
			}
		}
	}
	for _, r := range a.pending {
		if r.state != StatePending {
			t.Fatalf("%s step %d: pending row %q is in state %v", ctx, step, r.Label, r.state)
		}
	}
	if a.worker.Idle() {
		for _, p := range a.pages {
			for _, r := range p.rows {
				if r.state == StateWriting && r != a.acting {
					t.Fatalf("%s step %d: row %q is writing with an idle worker", ctx, step, r.Label)
				}
			}
		}
	}
}

// randomEvent feeds one random key, press, drag, release, wheel, tick or resize.
func randomEvent(rnd *rand.Rand, h *harness, w, hh *int, sizes [][2]int) {
	a := h.app
	switch r := rnd.Float64(); {
	case r < 0.55:
		a.Key(fuzzKeys[rnd.Intn(len(fuzzKeys))])
	case r < 0.72:
		a.Mouse(rnd.Intn(*w+2)-1, rnd.Intn(*hh+2)-1, []int{0, 0, -1, 1}[rnd.Intn(4)], rnd.Float64() < 0.8)
	case r < 0.82:
		a.MouseMove(rnd.Intn(*w+10)-5, rnd.Intn(*hh))
	case r < 0.87:
		a.MouseRelease()
	case r < 0.91:
		a.Tick()
	case r < 0.95:
		h.elapse()
	case r < 0.97:
		sz := sizes[rnd.Intn(len(sizes))]
		*w, *hh = sz[0], sz[1]
		h.cv.Resize(*w, *hh)
	default:
		a.Data().Fast(a.ctx)
	}
	if a.quit {
		a.quit = false
	}
}

// TestRandomInputNeverRaisesNeverLeavesWorkBehindAndNeverStopsAFanWithoutAYes drives
// thousands of random events at several sizes, with and without the service (the only
// way a fan can ever be stopped).
func TestRandomInputNeverRaisesNeverLeavesWorkBehindAndNeverStopsAFanWithoutAYes(t *testing.T) {
	sizes := [][2]int{{100, 30}, {60, 20}, {44, 12}, {140, 50}, {80, 24}, {30, 8}, {200, 12}}
	for _, service := range []bool{false, true} {
		for seed, sz := range sizes[:5] {
			rnd := rand.New(rand.NewSource(int64(seed*2 + 1)))
			h := newHarness(t, sz[0], sz[1], func(b *fakeBackend) { b.service = service })
			w, hh := sz[0], sz[1]
			ctx := "no service"
			if service {
				ctx = "service"
			}
			for step := range 4000 {
				randomEvent(rnd, h, &w, &hh, sizes)
				if step%5 == 0 {
					h.app.Draw()
					if got := widest(h.cv.Text()); got > w {
						t.Fatalf("%s seed %d step %d: a line of %d cells on a %d wide screen", ctx, seed, step, got, w)
					}
				}
				checkInvariants(t, h, step, ctx)
			}
			h.app.modal = nil
			h.app.Flush()
			h.elapse()
			h.app.Draw()
			if len(h.app.pending) != 0 || !h.app.worker.Idle() {
				t.Fatalf("%s seed %d: work left behind: %d pending", ctx, seed, len(h.app.pending))
			}
			b := h.b
			t.Logf("%s seed %d: %d writes, %d stops, %d yes answers", ctx, seed, len(b.writeCalls()), len(b.stops), h.app.yes)
			if !service && (len(b.stops) != 0 || b.duty[0] == 0 || b.duty[1] == 0 || b.fanOff) {
				t.Fatalf("seed %d: a fan was stopped without the service: %+v", seed, b.stops)
			}
			if len(b.stops) > h.app.yes {
				t.Fatalf("%s seed %d: %d stops but only %d Yes answers", ctx, seed, len(b.stops), h.app.yes)
			}
			for _, s := range b.stops {
				if !s.req.Confirm {
					t.Fatalf("%s seed %d: a stop was written without confirmation: %+v", ctx, seed, s.req)
				}
			}
			for _, c := range b.callList() {
				if c == "SetFan manual 0 0 confirm=false" {
					t.Fatalf("a stop was requested unconfirmed")
				}
			}
		}
	}
}

// TestAsyncWorkerUnderRandomInputIsRaceFree runs the real Worker and the background
// readers while random events arrive; run with -race.
func TestAsyncWorkerUnderRandomInputIsRaceFree(t *testing.T) {
	sizes := [][2]int{{100, 30}, {60, 20}, {44, 12}}
	for seed := range 3 {
		b := newFake()
		b.service = seed%2 == 0
		b.delay = 100 * time.Microsecond
		clock := newTestClock()
		cv := NewGridCanvas(100, 30, true)
		app := NewApp(Config{Canvas: cv, Backend: b, Clock: clock.Now, Version: "test"})
		ctx, cancel := contextWithCancel()
		app.ctx = ctx
		go app.Data().Loop(ctx)
		h := &harness{t: t, app: app, cv: cv, b: b, clock: clock}
		rnd := rand.New(rand.NewSource(int64(seed + 40)))
		w, hh := 100, 30
		for step := range 2500 {
			randomEvent(rnd, h, &w, &hh, sizes)
			if step%7 == 0 {
				app.Draw()
			}
			if step%50 == 0 {
				time.Sleep(time.Millisecond)
			}
		}
		app.lock()
		app.modal = nil
		app.unlock()
		app.Flush()
		cancel()
		if !app.worker.Idle() {
			t.Fatalf("seed %d: the worker is still busy after Flush", seed)
		}
		app.lock()
		left := len(app.pending)
		app.unlock()
		if left != 0 {
			t.Fatalf("seed %d: %d changes left pending", seed, left)
		}
		for _, s := range b.stops {
			if !s.req.Confirm {
				t.Fatalf("seed %d: unconfirmed stop %+v", seed, s.req)
			}
		}
		if !b.service && len(b.stops) != 0 {
			t.Fatalf("seed %d: stop without a service", seed)
		}
	}
}

// TestWritesForTheSameSettingAreCoalescedAndOrderedThroughTheApp checks the whole chain: a
// drag is many edits and one write, and different settings are written in the order made.
func TestWritesForTheSameSettingAreCoalescedAndOrderedThroughTheApp(t *testing.T) {
	b := newFake()
	clock := newTestClock()
	app := NewApp(Config{Canvas: NewGridCanvas(100, 30, true), Backend: b, Clock: clock.Now, Version: "test"})
	app.Data().Fast(app.ctx)
	app.Goto(3)
	var row *Row
	for _, r := range app.page().rows {
		if r.Label == "Brightness" {
			row = r
			break
		}
	}
	gate, started := make(chan struct{}), make(chan struct{})
	app.worker.Submit("blocker", func() { close(started); <-gate }, nil) // keep the worker busy
	<-started
	app.lock()
	for _, v := range []int{5, 10, 15, 20, 25, 30} {
		app.edit(row, v, true)
		row.state = StatePending // each edit is a fresh change of the same setting
	}
	app.unlock()
	close(gate)
	app.Flush()
	if n := b.count("SetKeyboard"); n != 1 {
		t.Fatalf("%d writes for one setting, want 1: %v", n, b.callList())
	}
	if b.kbNow != 30 {
		t.Fatalf("the last value must win: %d", b.kbNow)
	}
}

// TestFanStopsHappenOnlyThroughAConfirmedQuestion hammers the Fans page with the keys that
// lead to a stop (down to the bottom of a slider, Enter, y, n, Esc, arrows in the question)
// and checks every stop that reached the laptop was asked and answered Yes.
func TestFanStopsHappenOnlyThroughAConfirmedQuestion(t *testing.T) {
	alphabet := []Key{'j', 'k', 'h', 'h', 'h', 'l', KeyEnter, KeyEnter, ' ', 'y', 'n', KeyEsc, KeyLeft, KeyRight, KeyTab, '2'}
	totalStops := 0
	for seed := range 6 {
		rnd := rand.New(rand.NewSource(int64(seed + 100)))
		h := newHarness(t, 100, 30, func(b *fakeBackend) { b.service = true })
		h.app.Goto(1)
		for step := range 3000 {
			switch rnd.Intn(10) {
			case 0:
				h.elapse()
			case 1:
				h.app.Goto(1)
			default:
				h.app.Key(alphabet[rnd.Intn(len(alphabet))])
			}
			h.app.quit = false
			if h.app.modal != nil && h.app.modal.kind == modalConfirm {
				for _, r := range h.app.page().rows {
					if r.state == StateAsking && r.Guard == nil {
						t.Fatalf("step %d: a row without a guard is asking", step)
					}
				}
			}
		}
		h.app.modal = nil
		h.app.Flush()
		totalStops += len(h.b.stops)
		if len(h.b.stops) > h.app.yes {
			t.Fatalf("seed %d: %d stops, %d Yes answers", seed, len(h.b.stops), h.app.yes)
		}
		for _, s := range h.b.stops {
			if !s.req.Confirm || !s.service {
				t.Fatalf("seed %d: unconfirmed stop %+v", seed, s)
			}
		}
	}
	if totalStops == 0 {
		t.Fatal("the test never reached a stop: it proves nothing")
	}
}
