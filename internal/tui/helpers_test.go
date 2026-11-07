package tui

import (
	"context"
	"os"
	"strings"
	"sync"
	"testing"
	"time"
	"unicode/utf8"
)

// testClock is a hand-driven clock: pauses and grace periods are exact and instant.
type testClock struct {
	mu sync.Mutex
	t  time.Time
}

func newTestClock() *testClock {
	return &testClock{t: time.Date(2026, 11, 7, 12, 0, 0, 0, time.UTC)}
}

func (c *testClock) Now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.t
}

func (c *testClock) Advance(d time.Duration) {
	c.mu.Lock()
	c.t = c.t.Add(d)
	c.mu.Unlock()
}

// harness is a sync App on a text grid with a fake laptop: every key and its write is one step.
type harness struct {
	t     *testing.T
	app   *App
	cv    *GridCanvas
	b     *fakeBackend
	clock *testClock
}

func newHarness(t *testing.T, w, h int, mod ...func(*fakeBackend)) *harness {
	t.Helper()
	b := newFake()
	for _, m := range mod {
		m(b)
	}
	return newHarnessWith(t, w, h, b, true)
}

func newHarnessWith(t *testing.T, w, h int, b *fakeBackend, unicode bool) *harness {
	t.Helper()
	clock := newTestClock()
	cv := NewGridCanvas(w, h, unicode)
	app := NewApp(Config{Canvas: cv, Backend: b, Clock: clock.Now, Sync: true, Version: "test"})
	for range 3 {
		app.Data().Fast(app.ctx)
	}
	app.Refresh()
	return &harness{t: t, app: app, cv: cv, b: b, clock: clock}
}

// elapse lets the pause after a change run out, so what was chosen is written now.
func (h *harness) elapse() {
	h.clock.Advance(time.Second)
	h.app.Tick()
}

// sel selects the nth row with this label on the open page.
func (h *harness) sel(label string, nth ...int) *Row {
	h.t.Helper()
	n := 0
	if len(nth) > 0 {
		n = nth[0]
	}
	for i, r := range h.app.page().rows {
		if r.Label == label {
			if n == 0 {
				h.app.page().sel = i
				return r
			}
			n--
		}
	}
	h.t.Fatalf("no row %q (#%d) on page %s", label, n, h.app.page().name)
	return nil
}

func (h *harness) keys(s string) {
	for _, r := range s {
		h.app.Key(Key(r))
	}
}

func (h *harness) text() string {
	h.app.Draw()
	return h.cv.Text()
}

func (h *harness) lastLine() string {
	lines := strings.Split(h.text(), "\n")
	return lines[len(lines)-1]
}

// click presses the first place text is drawn (after a draw); y < 0 means any line.
func (h *harness) click(text string, y int) bool {
	for yy, line := range strings.Split(h.text(), "\n") {
		if (y < 0 || yy == y) && strings.Contains(line, text) {
			x := utf8.RuneCountInString(line[:strings.Index(line, text)]) + 1
			h.app.Mouse(x, yy, 0, true)
			return true
		}
	}
	return false
}

// barHit finds the slider bar drawn for a row.
func (h *harness) barHit(r *Row) hit {
	h.t.Helper()
	h.app.Draw()
	for _, ht := range h.app.hits {
		if ht.row == r && ht.zone == zoneBar {
			return ht
		}
	}
	h.t.Fatalf("no slider bar for %q", r.Label)
	return hit{}
}

func (h *harness) setFan(f *FanState) {
	h.app.data.mu.Lock()
	h.app.data.fan = f
	h.app.data.mu.Unlock()
}

func msgOf(a *App) (string, Style) { return a.msg.text, a.msg.style }

func widest(text string) int {
	m := 0
	for _, l := range strings.Split(text, "\n") {
		m = max(m, utf8.RuneCountInString(l))
	}
	return m
}

func readFile(p string) (string, error) {
	b, err := os.ReadFile(p)
	return string(b), err
}

func contextWithCancel() (context.Context, context.CancelFunc) {
	return context.WithCancel(context.Background())
}
