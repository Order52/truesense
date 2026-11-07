package fnkeys

import "time"

// Gesture is what a PREDATOR key press pattern resolved to.
type Gesture string

// The gestures of the PREDATOR key. Fn+Esc is a separate key combination, not a gesture of it.
const (
	Tap    Gesture = "tap"
	Double Gesture = "double"
	Triple Gesture = "triple"
	Long   Gesture = "long"
)

// Machine turns PREDATOR key press and release events into tap, double, triple and long.
//
// It is a pure state machine: it never reads a clock and starts no timer. Every method takes
// the time of the event, first fires whatever deadline has passed by then (in the order the
// deadlines fell), and returns the gestures that resolved. The caller asks Deadline for the
// next moment something can happen and calls Advance at that time. That makes the timing
// deterministic in tests and leaves the event loop in control of waiting.
//
// Timing, as in 0.3.x:
//   - a long press fires while the key is still held, LongAfter after the press, so it feels
//     instant, and the release that follows is not also a tap;
//   - taps are counted; a third tap resolves at once, otherwise the count resolves
//     MultiWindow after the last release unless another press comes first;
//   - a press cancels the pending resolution and keeps the tap count, but a long press discards
//     the taps that came before it.
type Machine struct {
	longAfter, multiWindow time.Duration

	taps     int
	down     bool
	longDone bool

	longAt, multiAt   time.Time
	longSet, multiSet bool
}

// NewMachine returns a machine with the given long-press and multi-tap timings.
func NewMachine(longAfter, multiWindow time.Duration) *Machine {
	return &Machine{longAfter: longAfter, multiWindow: multiWindow}
}

// SetTiming changes the timings for presses and releases from now on; a deadline already
// running keeps its time.
func (m *Machine) SetTiming(longAfter, multiWindow time.Duration) {
	m.longAfter, m.multiWindow = longAfter, multiWindow
}

// Press is a PREDATOR key press at time now. A press while the key is already down (a
// repeated report, not a new press) is ignored.
func (m *Machine) Press(now time.Time) []Gesture {
	out := m.Advance(now)
	if m.down {
		return out
	}
	m.multiSet = false
	m.longDone = false
	m.down = true
	m.longAt, m.longSet = now.Add(m.longAfter), true
	return out
}

// Release is the key coming up at time now. A release that does not belong to a PREDATOR
// press is ignored: Fn+F10, Fn+F11, Fn+F12 and Fn+Esc all end with the same `04 00 00`
// report, and none of them may count as a tap.
func (m *Machine) Release(now time.Time) []Gesture {
	out := m.Advance(now)
	if !m.down {
		return out
	}
	m.down = false
	m.longSet = false
	if m.longDone {
		m.taps = 0
		return out
	}
	m.taps++
	if m.taps >= 3 {
		return append(out, m.flush())
	}
	m.multiAt, m.multiSet = now.Add(m.multiWindow), true
	return out
}

// Advance fires every deadline that has passed by now and returns the gestures that resolved.
func (m *Machine) Advance(now time.Time) []Gesture {
	var out []Gesture
	if m.longSet && !now.Before(m.longAt) {
		m.longSet = false
		m.longDone = true
		m.taps = 0
		out = append(out, Long)
	}
	if m.multiSet && !now.Before(m.multiAt) {
		m.multiSet = false
		if g := m.flush(); g != "" {
			out = append(out, g)
		}
	}
	return out
}

// Deadline is the next time Advance has something to do, if any.
func (m *Machine) Deadline() (time.Time, bool) {
	switch {
	case m.longSet && m.multiSet:
		if m.longAt.Before(m.multiAt) {
			return m.longAt, true
		}
		return m.multiAt, true
	case m.longSet:
		return m.longAt, true
	case m.multiSet:
		return m.multiAt, true
	}
	return time.Time{}, false
}

func (m *Machine) flush() Gesture {
	n := m.taps
	m.taps = 0
	m.multiSet = false
	switch {
	case n <= 0:
		return ""
	case n == 1:
		return Tap
	case n == 2:
		return Double
	}
	return Triple
}
