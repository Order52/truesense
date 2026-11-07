package fnkeys

import "encoding/hex"

// Event is what one vendor report from the keyboard means.
type Event int

// The reports the keyboard sends.
const (
	// EventNone is a report that is not a vendor key report (too short, or another id).
	EventNone Event = iota
	// EventUnknown is a vendor report with a code we do not know, e.g. Fn+F11 / Fn+F12: the
	// keyboard changes its brightness itself and the report is only logged.
	EventUnknown
	EventFnF10
	EventFnEsc
	EventPredatorDown
	// EventRelease ends the PREDATOR key and every Fn combination alike.
	EventRelease
)

// Decode classifies one input report read from the hidraw node.
func Decode(report []byte) Event {
	if len(report) < 3 || report[0] != 0x04 {
		return EventNone
	}
	switch [2]byte{report[1], report[2]} {
	case [2]byte{0x80, 0xFF}:
		return EventFnF10
	case [2]byte{0x8A, 0xFF}:
		return EventFnEsc
	case [2]byte{0x81, 0xFF}:
		return EventPredatorDown
	case [2]byte{0x00, 0x00}:
		return EventRelease
	}
	return EventUnknown
}

func hexOf(b []byte) string { return hex.EncodeToString(b) }
