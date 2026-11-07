package tui

import (
	"testing"
	"time"
)

func rowAt(c *testClock, r Row) *Row {
	r.now = c.Now
	return &r
}

func TestRowShowsWhatWasChosenUntilTheHardwareCaughtUp(t *testing.T) {
	c := newTestClock()
	reading := 5
	r := rowAt(c, Row{Kind: KindSlider, Values: []int{0, 5, 10}, Get: func() any { return reading }})
	r.setLocal(10)
	if r.Value() != 10 || r.State() != StatePending {
		t.Fatalf("value %v state %v", r.Value(), r.State())
	}
	r.state = StateWriting
	r.settle(true)
	if r.State() != StateIdle || r.Value() != 10 {
		t.Fatalf("after a good write the chosen value is held: %v %v", r.Value(), r.State())
	}
	c.Advance(valueGrace + time.Millisecond)
	if r.Value() != 5 {
		t.Fatalf("after the grace period the reading is back: %v", r.Value())
	}
}

func TestAFailedWriteShowsTheReadingAgainAtOnce(t *testing.T) {
	c := newTestClock()
	r := rowAt(c, Row{Kind: KindToggle, Get: func() any { return true }})
	r.setLocal(false)
	r.state = StateWriting
	r.settle(false)
	if r.Value() != true || r.State() != StateIdle {
		t.Fatalf("value %v state %v", r.Value(), r.State())
	}
}

func TestAChosenValueThatIsNeverWrittenExpires(t *testing.T) {
	c := newTestClock()
	r := rowAt(c, Row{Kind: KindToggle, Get: func() any { return true }})
	r.setLocal(false)
	c.Advance(localHold + time.Second)
	if r.Value() != true || r.Busy() {
		t.Fatalf("value %v busy %v", r.Value(), r.Busy())
	}
}

func TestForgetDropsAnUnsentChange(t *testing.T) {
	c := newTestClock()
	r := rowAt(c, Row{Kind: KindToggle, Get: func() any { return true }})
	r.setLocal(false)
	r.due = c.Now()
	r.forget()
	if r.Value() != true || r.Busy() || !r.due.IsZero() {
		t.Fatal("change not forgotten")
	}
}

func TestSliderIndexAndExactZero(t *testing.T) {
	c := newTestClock()
	duties := []int{0, 20, 25, 30, 100}
	for _, tc := range []struct {
		reading any
		exact0  bool
		want    int
		known   bool
	}{
		{10, true, 1, true},   // 10 % is not a stop
		{0, true, 0, true},    // exactly 0 is
		{12, false, 1, true},  // nearer to 20 than 0 anyway
		{9, false, 0, true},   // without exact0 the nearest wins
		{27, true, 2, true},   // nearest of the rest
		{500, true, 4, true},  // beyond the end
		{nil, true, 0, false}, // unknown
		{"x", true, 0, false}, // not a number
	} {
		r := rowAt(c, Row{Kind: KindSlider, Values: duties, Exact0: tc.exact0, Get: func() any { return tc.reading }})
		if i, ok := r.Index(); ok != tc.known || (ok && i != tc.want) {
			t.Errorf("reading %v exact0=%v: index %d known %v, want %d %v", tc.reading, tc.exact0, i, ok, tc.want, tc.known)
		}
	}
}

func TestAnUnknownSliderStartsAtStartOrInTheMiddleNeverAtTheLowestValue(t *testing.T) {
	c := newTestClock()
	vals := []int{0, 20, 25, 30, 35, 40, 45}
	if i := rowAt(c, Row{Kind: KindSlider, Values: vals, Start: Some(40)}).StartIndex(); vals[i] != 40 {
		t.Fatalf("start %d", vals[i])
	}
	if i := rowAt(c, Row{Kind: KindSlider, Values: vals}).StartIndex(); i != len(vals)/2 || vals[i] == 0 {
		t.Fatalf("middle %d", i)
	}
	if i := rowAt(c, Row{Kind: KindSlider}).StartIndex(); i != 0 {
		t.Fatalf("no values: %d", i)
	}
}

func TestChoiceIndexMatchesByValue(t *testing.T) {
	c := newTestClock()
	cur := any("b")
	r := rowAt(c, Row{Kind: KindChoice, Options: []Option{{"a", "A"}, {"b", "B"}}, Get: func() any { return cur }})
	if i, ok := r.Index(); !ok || i != 1 {
		t.Fatalf("%d %v", i, ok)
	}
	cur = "zzz"
	if _, ok := r.Index(); ok {
		t.Fatal("an unknown value must have no index")
	}
	r.OptionsFn = func() []Option { return []Option{{"zzz", "Z"}} }
	if i, ok := r.Index(); !ok || i != 0 {
		t.Fatal("dynamic options not used")
	}
}

func TestRowSelectability(t *testing.T) {
	c := newTestClock()
	enabled, hidden := true, false
	r := rowAt(c, Row{Kind: KindToggle, Enabled: func() bool { return enabled }, Hide: func() bool { return hidden }})
	if !r.Selectable() {
		t.Fatal("toggle should be selectable")
	}
	enabled = false
	if r.Selectable() {
		t.Fatal("disabled")
	}
	enabled, hidden = true, true
	if r.Selectable() {
		t.Fatal("hidden")
	}
	for _, k := range []RowKind{KindHeader, KindInfo} {
		if rowAt(c, Row{Kind: k}).Selectable() {
			t.Fatalf("kind %v must never be selectable", k)
		}
	}
}

func TestRowShow(t *testing.T) {
	r := &Row{}
	if r.Show(nil) != "…" || r.Show(7) != "7" || r.Show("x") != "x" {
		t.Fatal("default formatting")
	}
	r.Fmt = func(v any) string { return "<" + anyString(v) + ">" }
	if r.Show(7) != "<7>" || r.Show(nil) != "…" {
		t.Fatal("custom formatting")
	}
}

func TestEditPendingCommitGuardAndSend(t *testing.T) {
	h := newHarness(t, 100, 30)
	a := h.app
	var sent []any
	asked := ""
	r := &Row{Kind: KindChoice, Label: "x", Options: []Option{{1, "one"}, {2, "two"}}, Get: func() any { return 1 },
		Guard: func(v any) string {
			if v == 2 {
				return "really two?"
			}
			return ""
		}, Put: func(v any) { sent = append(sent, v) }}
	r.now = h.clock.Now
	a.edit(r, 1, false)
	a.commit(r)
	if len(sent) != 1 || r.state != StateWriting {
		t.Fatalf("unguarded value must go straight out: %v %v", sent, r.state)
	}
	r.settle(true)
	r.forget()

	a.edit(r, 2, false)
	if len(a.pending) != 1 || r.state != StatePending {
		t.Fatal("not pending")
	}
	h.elapse() // pause over: the guard asks
	if a.modal == nil || r.state != StateAsking || len(a.pending) != 0 {
		t.Fatalf("modal %v state %v", a.modal, r.state)
	}
	asked = a.modal.text
	if asked != "really two?" || a.modal.focus != 1 {
		t.Fatalf("question %q focus %d", asked, a.modal.focus)
	}
	a.Key('n')
	if len(sent) != 1 || r.Busy() {
		t.Fatalf("No sends nothing: %v busy %v", sent, r.Busy())
	}
	a.edit(r, 2, true)
	a.Key('y')
	if len(sent) != 2 || sent[1] != 2 {
		t.Fatalf("Yes sends: %v", sent)
	}
}
