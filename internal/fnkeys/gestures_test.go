package fnkeys

import (
	"reflect"
	"testing"
	"time"
)

// ev is one step of a scripted timeline: at is the offset from the start in milliseconds.
type ev struct {
	at   int
	what string // down | up | tick
}

var t0 = time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC)

func at(ms int) time.Time { return t0.Add(time.Duration(ms) * time.Millisecond) }

// play runs a timeline the way the event loop does: before each event it fires the machine's
// deadlines that fall before it (a timer firing on its own), then delivers the event; after the
// last event it lets time run on far enough for every deadline.
func play(m *Machine, steps []ev) []Gesture {
	var out []Gesture
	for _, s := range steps {
		for {
			d, ok := m.Deadline()
			if !ok || d.After(at(s.at)) {
				break
			}
			out = append(out, m.Advance(d)...)
		}
		switch s.what {
		case "down":
			out = append(out, m.Press(at(s.at))...)
		case "up":
			out = append(out, m.Release(at(s.at))...)
		case "tick":
			out = append(out, m.Advance(at(s.at))...)
		}
	}
	for {
		d, ok := m.Deadline()
		if !ok {
			return out
		}
		out = append(out, m.Advance(d)...)
	}
}

func TestGestureTimelines(t *testing.T) {
	// the timings of tests/test_core.py::TestGestures (long 250 ms, multi 100 ms) as a virtual timeline
	tap := func(from int) []ev { return []ev{{from, "down"}, {from + 30, "up"}} }
	cat := func(parts ...[]ev) []ev {
		var all []ev
		for _, p := range parts {
			all = append(all, p...)
		}
		return all
	}
	for _, tc := range []struct {
		name  string
		steps []ev
		want  []Gesture
	}{
		{"tap", tap(0), []Gesture{Tap}},
		{"double", cat(tap(0), tap(50)), []Gesture{Double}},
		{"triple", cat(tap(0), tap(50), tap(100)), []Gesture{Triple}},
		{"long is not also a tap", []ev{{0, "down"}, {500, "up"}}, []Gesture{Long}},
		{"two taps too far apart", cat(tap(0), tap(280)), []Gesture{Tap, Tap}},
		{"long then tap", cat([]ev{{0, "down"}, {400, "up"}}, tap(430)), []Gesture{Long, Tap}},
		{"four quick taps are a triple and a tap", cat(tap(0), tap(50), tap(100), tap(150)), []Gesture{Triple, Tap}},
		{"release without a press is ignored", []ev{{0, "up"}, {20, "up"}}, nil},
		{"a stray release before a tap changes nothing", cat([]ev{{0, "up"}, {20, "up"}}, tap(40)), []Gesture{Tap}},
		{"a tap then a long press: the tap is dropped", cat(tap(0), []ev{{60, "down"}, {400, "up"}}), []Gesture{Long}},
		{"a press inside the window keeps counting", cat(tap(0), []ev{{100, "down"}, {120, "up"}}), []Gesture{Double}},
		{"a long press that is released exactly at the threshold is long", []ev{{0, "down"}, {250, "up"}}, []Gesture{Long}},
		{"released one ms before the threshold is a tap", []ev{{0, "down"}, {249, "up"}}, []Gesture{Tap}},
		{"a repeated down report does not restart or double the long press", []ev{{0, "down"}, {100, "down"}, {600, "up"}}, []Gesture{Long}},
		{"a repeated down after the long press has fired adds no tap", []ev{{0, "down"}, {300, "down"}, {600, "up"}}, []Gesture{Long}},
		{"nothing at all", nil, nil},
		{"two long presses", []ev{{0, "down"}, {400, "up"}, {500, "down"}, {900, "up"}}, []Gesture{Long, Long}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := play(NewMachine(250*time.Millisecond, 100*time.Millisecond), tc.steps); !reflect.DeepEqual(got, tc.want) {
				t.Errorf("got %v, want %v", got, tc.want)
			}
		})
	}
}

func TestDeadlinesAreExact(t *testing.T) {
	m := NewMachine(800*time.Millisecond, 450*time.Millisecond)
	if _, ok := m.Deadline(); ok {
		t.Fatal("idle machine has no deadline")
	}
	m.Press(at(0))
	if d, ok := m.Deadline(); !ok || !d.Equal(at(800)) {
		t.Fatalf("long deadline %v", d)
	}
	if g := m.Advance(at(799)); g != nil {
		t.Fatalf("early: %v", g)
	}
	m.Release(at(100)) // a tap: now the multi window runs
	if d, ok := m.Deadline(); !ok || !d.Equal(at(550)) {
		t.Fatalf("multi deadline %v", d)
	}
	if g := m.Advance(at(549)); g != nil {
		t.Fatalf("early: %v", g)
	}
	if g := m.Advance(at(550)); !reflect.DeepEqual(g, []Gesture{Tap}) {
		t.Fatalf("on time: %v", g)
	}
	if _, ok := m.Deadline(); ok {
		t.Error("nothing left pending")
	}
	if g := m.Advance(at(10_000)); g != nil {
		t.Errorf("a resolved gesture fires once: %v", g)
	}
}

func TestLongFiresWhileStillHeld(t *testing.T) {
	m := NewMachine(800*time.Millisecond, 450*time.Millisecond)
	m.Press(at(0))
	if g := m.Advance(at(800)); !reflect.DeepEqual(g, []Gesture{Long}) {
		t.Fatalf("long must fire at the threshold, before the release: %v", g)
	}
	if g := m.Release(at(5000)); g != nil {
		t.Errorf("the release after a long press is silent: %v", g)
	}
	if g := m.Advance(at(9000)); g != nil {
		t.Errorf("%v", g)
	}
}

func TestEventsResolveDueDeadlinesFirst(t *testing.T) {
	// Press / Release arriving after a deadline passed without a tick still see it fire first, in order
	m := NewMachine(250*time.Millisecond, 100*time.Millisecond)
	m.Press(at(0))
	m.Release(at(10))
	if g := m.Press(at(500)); !reflect.DeepEqual(g, []Gesture{Tap}) { // the tap's window closed at 110
		t.Fatalf("press after the window: %v", g)
	}
	if g := m.Release(at(1000)); !reflect.DeepEqual(g, []Gesture{Long}) { // held past 250: long, then the release is silent
		t.Fatalf("release after the long threshold: %v", g)
	}
}

func TestSetTimingAppliesToLaterPresses(t *testing.T) {
	m := NewMachine(800*time.Millisecond, 450*time.Millisecond)
	m.Press(at(0)) // long deadline stays at 800
	m.SetTiming(300*time.Millisecond, 200*time.Millisecond)
	if d, _ := m.Deadline(); !d.Equal(at(800)) {
		t.Errorf("a running deadline keeps its time: %v", d)
	}
	m.Release(at(50))
	if d, _ := m.Deadline(); !d.Equal(at(250)) {
		t.Errorf("the new multi window applies: %v", d)
	}
	m.Advance(at(250))
	m.Press(at(1000))
	if d, _ := m.Deadline(); !d.Equal(at(1300)) {
		t.Errorf("the new long threshold applies: %v", d)
	}
}

func FuzzMachine(f *testing.F) {
	f.Add([]byte{0, 1, 2, 3, 0, 1, 0, 1, 3})
	f.Add([]byte{0, 3, 3, 3, 1, 1, 2})
	f.Fuzz(func(t *testing.T, script []byte) {
		m := NewMachine(250*time.Millisecond, 100*time.Millisecond)
		now := t0
		var fired []Gesture
		for _, b := range script {
			now = now.Add(time.Duration(b%7) * 60 * time.Millisecond)
			switch b >> 4 % 3 {
			case 0:
				fired = append(fired, m.Press(now)...)
			case 1:
				fired = append(fired, m.Release(now)...)
			default:
				fired = append(fired, m.Advance(now)...)
			}
			if d, ok := m.Deadline(); ok && d.Before(now.Add(-time.Hour)) {
				t.Fatal("deadline in the distant past")
			}
		}
		for {
			d, ok := m.Deadline()
			if !ok {
				break
			}
			fired = append(fired, m.Advance(d)...)
		}
		for _, g := range fired {
			if g != Tap && g != Double && g != Triple && g != Long {
				t.Fatalf("bad gesture %q", g)
			}
		}
		if _, ok := m.Deadline(); ok {
			t.Fatal("a drained machine has no deadline")
		}
	})
}
