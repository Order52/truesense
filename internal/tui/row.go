package tui

import "time"

// Clock tells the time; the tests replace it so pauses and grace periods are exact.
type Clock func() time.Time

// RowKind is what a row looks like and how it reacts.
type RowKind int

// The kinds of row.
const (
	KindHeader RowKind = iota
	KindInfo
	KindChoice
	KindToggle
	KindSlider
	KindButton
)

// RowState is where a change of a row is on its way to the hardware.
type RowState int

// A row is idle, or its change is waiting for the pause to end (or Enter), waiting for a
// Yes, or being written.
const (
	StateIdle RowState = iota
	StatePending
	StateAsking
	StateWriting
)

// Option is one choice of a choice row; Value is what Get returns and Put receives.
type Option struct {
	Value any
	Label string
}

// valueGrace is how long a freshly chosen value stays on screen after its write finished,
// so a slow hardware reading does not flash the old value back.
const valueGrace = 1500 * time.Millisecond

// localHold is how long a chosen value is shown while it waits to be written.
const localHold = 15 * time.Second

// Row is one setting. Get returns the current value (nil = unknown) and Put writes a chosen
// one; Put returns at once, the write runs on the Worker. Guard returns a question to ask
// before a value is written, or "". Start is where an unknown slider begins (else the
// middle, never the lowest value, which may be a stop). Exact0 makes a slider show its
// lowest value (a stopped fan) only when the reading is exactly that.
type Row struct {
	Kind  RowKind
	Label string
	Get   func() any
	Put   func(any)
	Press func() // buttons

	Options   []Option
	OptionsFn func() []Option
	Values    []int
	Fmt       func(any) string
	Hint      string
	Guard     func(any) string
	Enabled   func() bool
	Hide      func() bool
	InfoStyle func(any) Style
	Wrap      bool
	Colors    map[string]Style // choice: style per option value (the power modes)
	Text      string           // button caption
	Start     Opt[int]
	Exact0    bool

	now        Clock
	local      any
	hasLocal   bool
	localUntil time.Time
	due        time.Time
	state      RowState
}

// Selectable is true for a row the cursor can rest on and act on.
func (r *Row) Selectable() bool {
	if r.Kind == KindHeader || r.Kind == KindInfo {
		return false
	}
	if r.Enabled != nil && !r.Enabled() {
		return false
	}
	return r.Hide == nil || !r.Hide()
}

// Busy is true while a change of this row is on its way.
func (r *Row) Busy() bool { return r.state != StateIdle }

// State is where the row's change is.
func (r *Row) State() RowState { return r.state }

func (r *Row) clock() time.Time {
	if r.now == nil {
		return time.Now()
	}
	return r.now()
}

// Value is what to show: the value just chosen until the hardware has caught up (or the
// write failed), else the latest reading.
func (r *Row) Value() any {
	if r.hasLocal {
		if r.clock().Before(r.localUntil) {
			return r.local
		}
		r.hasLocal, r.local, r.state = false, nil, StateIdle
	}
	if r.Get == nil {
		return nil
	}
	return r.Get()
}

// setLocal shows v now and marks the change as waiting.
func (r *Row) setLocal(v any) {
	r.local, r.hasLocal, r.localUntil, r.state = v, true, r.clock().Add(localHold), StatePending
}

// settle is called when a write finished: keep the chosen value on screen for a moment so
// a slow reading does not flash the old one; on failure show the reading again.
func (r *Row) settle(ok bool) {
	if ok && r.hasLocal {
		r.localUntil = r.clock().Add(valueGrace)
	} else {
		r.local, r.hasLocal = nil, false
	}
	if r.state == StateWriting {
		r.state = StateIdle
	}
}

// forget drops a change that was not written.
func (r *Row) forget() { r.local, r.hasLocal, r.state, r.due = nil, false, StateIdle, time.Time{} }

// options is the list of choices now.
func (r *Row) options() []Option {
	if r.OptionsFn != nil {
		return r.OptionsFn()
	}
	return r.Options
}

// asInt converts a slider reading.
func asInt(v any) (int, bool) {
	switch n := v.(type) {
	case int:
		return n, true
	case int64:
		return int(n), true
	case float64:
		return int(n), true
	}
	return 0, false
}

// Index is the position of the current value among the options or slider values; ok is
// false when unknown. An exact0 slider shows its first value only for an exact reading of it.
func (r *Row) Index() (int, bool) {
	v := r.Value()
	switch r.Kind {
	case KindChoice:
		for i, o := range r.options() {
			if o.Value == v {
				return i, true
			}
		}
	case KindSlider:
		n, ok := asInt(v)
		if !ok || len(r.Values) == 0 {
			return 0, false
		}
		skipFirst := r.Exact0 && n != r.Values[0]
		best, bestD := -1, 0
		for i, x := range r.Values {
			if skipFirst && x == r.Values[0] {
				continue
			}
			if d := abs(x - n); best < 0 || d < bestD {
				best, bestD = i, d
			}
		}
		return best, best >= 0
	}
	return 0, false
}

// StartIndex is where an unknown slider begins: Start, else the middle (never the lowest
// value, which may be a stop).
func (r *Row) StartIndex() int {
	if len(r.Values) == 0 {
		return 0
	}
	if r.Start.OK {
		best, bestD := 0, -1
		for i, x := range r.Values {
			if d := abs(x - r.Start.V); bestD < 0 || d < bestD {
				best, bestD = i, d
			}
		}
		return best
	}
	return len(r.Values) / 2
}

// Show formats a value; an unknown one is an ellipsis.
func (r *Row) Show(v any) string {
	if v == nil {
		return "…"
	}
	if r.Fmt != nil {
		return r.Fmt(v)
	}
	return anyString(v)
}

func abs(n int) int {
	if n < 0 {
		return -n
	}
	return n
}
