package fanctl

import (
	"log/slog"
	"strconv"
	"sync"
)

// Hardware is what the Writer needs from the fans.
type Hardware interface {
	FanAuto() error
	FanManual(cpu, gpu int) error
	// FanIsAuto reports whether the firmware controls the fans (false = something set them manually or to max).
	FanIsAuto() (bool, error)
}

// Writer sends duties to the hardware only when they change, and notices when something else
// (a plug or unplug, a resume) reset the fans behind its back. (0, 0) means firmware Auto.
type Writer struct {
	HW  Hardware
	Log *slog.Logger

	mu      sync.Mutex
	written *[2]int
}

func (w *Writer) logger() *slog.Logger {
	if w.Log != nil {
		return w.Log
	}
	return slog.Default()
}

// Write applies the duties if they differ from the last ones written. A single write holds
// (verified on the hardware), so there is no periodic rewrite.
func (w *Writer) Write(cpu, gpu int) {
	w.mu.Lock()
	defer w.mu.Unlock()
	want := [2]int{cpu, gpu}
	if w.written != nil && *w.written == want {
		return
	}
	var err error
	if want == [2]int{0, 0} {
		err = w.HW.FanAuto()
	} else {
		err = w.HW.FanManual(max(20, cpu), max(20, gpu))
	}
	if err != nil {
		w.logger().Warn("fan write failed", "err", err)
		w.written = nil
		return
	}
	w.written = &want
}

// Forget makes the next Write send its duties again (after a plug or unplug, a resume, or any
// change of fan mode by someone else).
func (w *Writer) Forget() { w.mu.Lock(); w.written = nil; w.mu.Unlock() }

// Written reports what was last written ("auto" for firmware control, "cpu,gpu" otherwise).
func (w *Writer) Written() string {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.written == nil || *w.written == [2]int{0, 0} {
		return "auto"
	}
	return strconv.Itoa(w.written[0]) + "," + strconv.Itoa(w.written[1])
}


// Verify re-asserts the last duties if the firmware or the driver reset the fans behind our
// back (call it every 15 s while the smart fan runs).
func (w *Writer) Verify() {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.written == nil {
		return
	}
	auto, err := w.HW.FanIsAuto()
	if err != nil {
		return
	}
	if !auto != (*w.written != [2]int{0, 0}) {
		w.written = nil
	}
}
