package tui

import (
	"context"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/gdamore/tcell/v2"
)

// snapScreen snapshots the screen text on the drawing goroutine: tcell's simulation
// screen hands out its cell buffer unlocked, so reading it from the test while Run draws
// would race.
type snapScreen struct {
	tcell.SimulationScreen
	mu   sync.Mutex
	text string
}

func (s *snapScreen) Show() { s.SimulationScreen.Show(); s.snapshot() }
func (s *snapScreen) Sync() { s.SimulationScreen.Sync(); s.snapshot() }

func (s *snapScreen) snapshot() {
	cells, w, h := s.GetContents()
	var lines []string
	for y := range h {
		var b strings.Builder
		for x := range w {
			c := cells[y*w+x]
			if len(c.Runes) == 0 {
				b.WriteByte(' ')
				continue
			}
			b.WriteRune(c.Runes[0])
		}
		lines = append(lines, strings.TrimRight(b.String(), " "))
	}
	s.mu.Lock()
	s.text = strings.Join(lines, "\n")
	s.mu.Unlock()
}

// simRun runs the real event loop on a simulation screen against a fake laptop.
type simRun struct {
	t      *testing.T
	scr    *snapScreen
	b      *fakeBackend
	cancel context.CancelFunc
	done   chan error
	ended  chan struct{}
}

func startSim(t *testing.T, w, h int, opts Options, mod ...func(*fakeBackend)) *simRun {
	t.Helper()
	sim := tcell.NewSimulationScreen("UTF-8")
	if err := sim.Init(); err != nil {
		t.Fatal(err)
	}
	sim.SetSize(w, h)
	scr := &snapScreen{SimulationScreen: sim}
	b := newFake()
	for _, m := range mod {
		m(b)
	}
	opts.Screen = scr
	if opts.Version == "" {
		opts.Version = "sim"
	}
	ctx, cancel := context.WithCancel(context.Background())
	r := &simRun{t: t, scr: scr, b: b, cancel: cancel, done: make(chan error, 1), ended: make(chan struct{})}
	go func() { r.done <- Run(ctx, b, opts); close(r.ended) }()
	t.Cleanup(func() { cancel(); <-r.ended; scr.Fini() })
	return r
}

func (r *simRun) screenText() string {
	r.scr.mu.Lock()
	defer r.scr.mu.Unlock()
	return r.scr.text
}

func (r *simRun) waitFor(what string, cond func() bool) {
	r.t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if cond() {
			return
		}
		time.Sleep(2 * time.Millisecond)
	}
	r.t.Fatalf("timed out waiting for %s; screen:\n%s", what, r.screenText())
}

func (r *simRun) waitText(s string) {
	r.t.Helper()
	r.waitFor("text "+s, func() bool { return strings.Contains(r.screenText(), s) })
}

func (r *simRun) key(k tcell.Key, ch rune) { r.scr.InjectKey(k, ch, tcell.ModNone) }

func (r *simRun) typeKeys(s string) {
	for _, c := range s {
		r.key(tcell.KeyRune, c)
	}
}

func (r *simRun) wait() error {
	r.t.Helper()
	select {
	case err := <-r.done:
		return err
	case <-time.After(5 * time.Second):
		r.t.Fatal("Run did not return")
		return nil
	}
}

func TestRunDrawsTheDashboardAndQuitsOnQ(t *testing.T) {
	r := startSim(t, 100, 30, Options{})
	r.waitText("Temperatures")
	if txt := r.screenText(); !strings.Contains(txt, "sim") || !strings.Contains(txt, "1 Dashboard") {
		t.Fatalf("\n%s", txt)
	}
	r.typeKeys("q")
	if err := r.wait(); err != nil {
		t.Fatal(err)
	}
}

func TestRunWritesWhatYouChooseAndFlushesOnQuit(t *testing.T) {
	r := startSim(t, 100, 30, Options{})
	r.waitText("Power mode")
	r.typeKeys("l") // quiet, shown at once, not yet written
	r.waitFor("the change on screen", func() bool { return strings.Contains(r.screenText(), "↵") })
	r.typeKeys("q") // quitting sends it
	if err := r.wait(); err != nil {
		t.Fatal(err)
	}
	r.b.mu.Lock()
	mode := r.b.mode
	r.b.mu.Unlock()
	if mode != "quiet" {
		t.Fatalf("mode %q: the change was lost on quit", mode)
	}
}

func TestRunWritesAfterThePauseAndReportsAtTheBottom(t *testing.T) {
	r := startSim(t, 100, 30, Options{})
	r.waitText("Power mode")
	r.typeKeys("l")
	r.waitText("Power mode: Quiet (held 30 min)")
	r.waitFor("the write", func() bool { return r.b.count("SetPowerMode quiet") == 1 })
}

func TestRunTabsMouseAndPageOption(t *testing.T) {
	r := startSim(t, 100, 30, Options{Page: "fans"})
	r.waitText("Fan control")
	r.scr.InjectMouse(26, 1, tcell.Button1, tcell.ModNone) // the Power tab
	r.scr.InjectMouse(26, 1, tcell.ButtonNone, tcell.ModNone)
	r.waitText("Battery and charging")
	r.key(tcell.KeyTab, 0)
	r.waitText("Keyboard")
	r.key(tcell.KeyBacktab, 0)
	r.key(tcell.KeyBacktab, 0)
	r.waitText("Fan control")
	r.typeKeys("?")
	r.waitText("Keys")
	r.key(tcell.KeyEscape, 0)
	r.waitFor("help closed", func() bool { return !strings.Contains(r.screenText(), " Keys ") })
	r.key(tcell.KeyCtrlC, 0)
	if err := r.wait(); err != nil {
		t.Fatal(err)
	}
}

func TestRunDragsASliderWithThePointer(t *testing.T) {
	r := startSim(t, 100, 30, Options{Page: "lighting"})
	r.waitText("Brightness")
	y := -1
	for i, l := range strings.Split(r.screenText(), "\n") {
		if strings.Contains(l, "Brightness") {
			y = i
			break
		}
	}
	line := strings.Split(r.screenText(), "\n")[y]
	bar := strings.Index(line, "━")
	if bar < 0 {
		bar = strings.Index(line, "─")
	}
	col := len([]rune(line[:bar]))
	r.scr.InjectMouse(col, y, tcell.Button1, tcell.ModNone)
	r.scr.InjectMouse(col+10, y, tcell.Button1, tcell.ModNone)
	r.scr.InjectMouse(col+30, y, tcell.Button1, tcell.ModNone)
	r.scr.InjectMouse(col+30, y, tcell.ButtonNone, tcell.ModNone)
	r.waitFor("the drag to be written", func() bool {
		r.b.mu.Lock()
		defer r.b.mu.Unlock()
		return r.b.kbNow >= 40
	})
}

func TestRunResizesLive(t *testing.T) {
	r := startSim(t, 100, 30, Options{})
	r.waitText("Temperatures")
	r.scr.SetSize(44, 12)
	r.waitFor("the small layout", func() bool {
		txt := r.screenText()
		return !strings.Contains(txt, "Temperatures") || strings.Contains(txt, "quit")
	})
	r.scr.SetSize(30, 8)
	r.waitText("44x12")
}

func TestRunStopsWhenTheContextEnds(t *testing.T) {
	r := startSim(t, 100, 30, Options{})
	r.waitText("Temperatures")
	r.cancel()
	if err := r.wait(); err != nil {
		t.Fatal(err)
	}
}

func TestRunRejectsAnUnknownPage(t *testing.T) {
	err := Run(context.Background(), newFake(), Options{Page: "nowhere"})
	if err == nil || !strings.Contains(err.Error(), "unknown page") || !strings.Contains(err.Error(), "dashboard, fans") {
		t.Fatalf("%v", err)
	}
}

func TestPageIndex(t *testing.T) {
	for _, tc := range []struct {
		name string
		want int
		ok   bool
	}{{"fans", 1, true}, {"FANS", 1, true}, {"7", 6, true}, {"system", 6, true}, {"0", 0, false}, {"8", 0, false}, {"", 0, false}} {
		if i, ok := PageIndex(tc.name); ok != tc.ok || (ok && i != tc.want) {
			t.Errorf("%q: %d %v", tc.name, i, ok)
		}
	}
	if got := PageNames(); len(got) != pageCount || got[0] != "Dashboard" {
		t.Fatalf("%v", got)
	}
}

func TestTranslateKey(t *testing.T) {
	for _, tc := range []struct {
		ev   *tcell.EventKey
		want Key
		quit bool
		ok   bool
	}{
		{tcell.NewEventKey(tcell.KeyRune, 'j', tcell.ModNone), 'j', false, true},
		{tcell.NewEventKey(tcell.KeyEnter, 0, tcell.ModNone), KeyEnter, false, true},
		{tcell.NewEventKey(tcell.KeyEscape, 0, tcell.ModNone), KeyEsc, false, true},
		{tcell.NewEventKey(tcell.KeyBackspace2, 0, tcell.ModNone), KeyBackspace, false, true},
		{tcell.NewEventKey(tcell.KeyBacktab, 0, tcell.ModNone), KeyBacktab, false, true},
		{tcell.NewEventKey(tcell.KeyPgDn, 0, tcell.ModNone), KeyPageDown, false, true},
		{tcell.NewEventKey(tcell.KeyCtrlL, 0, tcell.ModCtrl), KeyCtrlL, false, true},
		{tcell.NewEventKey(tcell.KeyCtrlC, 0, tcell.ModCtrl), 0, true, true},
		{tcell.NewEventKey(tcell.KeyF5, 0, tcell.ModNone), 0, false, false},
	} {
		k, quit, ok := translateKey(tc.ev)
		if k != tc.want || quit != tc.quit || ok != tc.ok {
			t.Errorf("%v: got %v %v %v", tc.ev.Name(), k, quit, ok)
		}
	}
}

func TestUnicodeTerminalDetection(t *testing.T) {
	env := func(m map[string]string) func(string) string { return func(k string) string { return m[k] } }
	for name, tc := range map[string]struct {
		env  map[string]string
		want bool
	}{
		"utf8 locale":    {map[string]string{"LANG": "en_US.UTF-8", "TERM": "xterm"}, true},
		"utf8 lowercase": {map[string]string{"LC_ALL": "C.utf8", "TERM": "xterm"}, true},
		"LC_ALL wins":    {map[string]string{"LC_ALL": "C", "LANG": "en_US.UTF-8", "TERM": "xterm"}, false},
		"bare console":   {map[string]string{"LANG": "en_US.UTF-8", "TERM": "linux"}, false},
		"no locale":      {map[string]string{"TERM": "xterm"}, false},
		"latin1":         {map[string]string{"LANG": "de_DE.ISO-8859-1", "TERM": "xterm"}, false},
	} {
		if got := unicodeTerminal(env(tc.env)); got != tc.want {
			t.Errorf("%s: %v", name, got)
		}
	}
}

func TestEveryStyleHasALookWithAndWithoutColour(t *testing.T) {
	names := []Style{StText, StDim, StAccent, StGood, StWarn, StBad, StSel, StPill, StTab, StTabSel, StTitle, StKey, StHead, StOn, StOff, StBar, StBarOff}
	for _, st := range modeStyle {
		names = append(names, st, st+selSuffix)
	}
	for _, colour := range []bool{true, false} {
		p := palette(colour)
		for _, n := range names {
			if _, ok := p[n]; !ok {
				t.Errorf("colour=%v: no look for %q", colour, n)
			}
		}
	}
}

func TestTCellCanvasClipsAndDraws(t *testing.T) {
	scr := tcell.NewSimulationScreen("UTF-8")
	if err := scr.Init(); err != nil {
		t.Fatal(err)
	}
	defer scr.Fini()
	scr.SetSize(10, 3)
	cv := newTCellCanvas(scr, true, true)
	cv.Put(0, -2, "abcdef", StGood)
	cv.Put(1, 8, "xyz", StDim)
	cv.Put(-1, 0, "no", StText)
	cv.Put(5, 0, "no", StText)
	scr.Show()
	cells, w, _ := scr.GetContents()
	if got := string(cells[0].Runes[0]); got != "c" {
		t.Fatalf("first cell %q", got)
	}
	if got := string(cells[w+8].Runes[0]) + string(cells[w+9].Runes[0]); got != "xy" {
		t.Fatalf("clipped %q", got)
	}
	if cw, ch := cv.Size(); cw != 10 || ch != 3 || cv.Glyphs() == nil {
		t.Fatal("size or glyphs")
	}
	cv.Clear()
}
