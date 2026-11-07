package keyboard

import (
	"os"
	"sync"
	"time"

	"github.com/Order52/truesense/internal/keymap"
	"github.com/Order52/truesense/internal/userstate"
)

// SimTransport is an in-memory keyboard controller: it records every report, tracks the
// brightness and the timeout flag the way the controller does, and answers the two queries
// (0x88 brightness, 0xB0 timeout). It backs TRUESENSE_SIM=1, so the command line, the TUI and
// the window run without hardware, and the unit tests of everything above.
type SimTransport struct {
	mu         sync.Mutex
	packets    [][]byte
	reports    [][]byte
	brightness int
	timeout    bool
	pending    byte // the query the next GetFeature answers
}

// NewSimTransport returns a controller in its factory state: brightness 25, timeout flag on.
func NewSimTransport() *SimTransport { return &SimTransport{brightness: 25, timeout: true} }

// SetFeature records a feature report and applies its effect on the simulated controller.
func (s *SimTransport) SetFeature(payload []byte) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	p := append([]byte(nil), payload...)
	s.packets = append(s.packets, p)
	if len(p) < PacketLen {
		return nil
	}
	switch p[0] {
	case 0x08: // effect / brightness packets carry the brightness in byte 4
		s.brightness = int(p[4])
	case 0x30:
		s.timeout = p[2] != 0
	case 0x88, 0xB0:
		s.pending = p[0]
	}
	return nil
}

// GetFeature answers the query sent last (the report echoes its command byte).
func (s *SimTransport) GetFeature(n int) ([]byte, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	r := make([]byte, n)
	if n < PacketLen {
		return r, nil
	}
	r[0] = s.pending
	switch s.pending {
	case 0x88:
		r[4] = byte(s.brightness) //nolint:gosec // the simulator only stores bytes it received
	case 0xB0:
		if s.timeout {
			r[2] = 1
		}
	}
	r[PacketLen-1] = checksum(r[:PacketLen-1])
	return r, nil
}

// WriteOutput records one per-key output report.
func (s *SimTransport) WriteOutput(data []byte) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.reports = append(s.reports, append([]byte(nil), data...))
	return nil
}

// Close does nothing: the simulated keyboard never goes away.
func (s *SimTransport) Close() error { return nil }

// Packets returns every feature report sent so far.
func (s *SimTransport) Packets() [][]byte {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([][]byte(nil), s.packets...)
}

// Frames returns how many per-key output reports were written (8 per upload).
func (s *SimTransport) Frames() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return len(s.reports)
}

// LastCells returns the colours of the last complete per-key upload (nil before the first).
func (s *SimTransport) LastCells() []keymap.RGB {
	s.mu.Lock()
	defer s.mu.Unlock()
	per := keymap.NumCells * 4 / OutputLen
	if len(s.reports) < per {
		return nil
	}
	cells := make([]keymap.RGB, 0, keymap.NumCells)
	for _, r := range s.reports[len(s.reports)-per:] {
		for i := 0; i+3 < len(r); i += 4 {
			cells = append(cells, keymap.RGB{r[i+1], r[i+2], r[i+3]})
		}
	}
	return cells
}

// Brightness returns the simulated brightness.
func (s *SimTransport) Brightness() int { s.mu.Lock(); defer s.mu.Unlock(); return s.brightness }

// Timeout returns the simulated controller's own timeout flag.
func (s *SimTransport) Timeout() bool { s.mu.Lock(); defer s.mu.Unlock(); return s.timeout }

// The simulated keyboard is process-wide, like firmware.Sim: several parts of one process
// (the daemon's handlers, the key service, a command) must see the same keyboard, exactly as
// they would see the one real device.
var (
	simOnce sync.Once
	simInst *SimTransport
)

// Sim returns the process-wide simulated keyboard.
func Sim() *SimTransport {
	simOnce.Do(func() { simInst = NewSimTransport() })
	return simInst
}

// NewSim returns a controller over a fresh simulated keyboard, without frame pacing.
func NewSim(state *userstate.Store) (*Keyboard, *SimTransport) {
	sim := NewSimTransport()
	return newSimKeyboard(sim, state), sim
}

func newSimKeyboard(sim *SimTransport, state *userstate.Store) *Keyboard {
	return New(func() (Transport, error) { return sim, nil }, state, WithSleep(func(_ time.Duration) {}))
}

// FromEnv returns the controller for this process: the process-wide simulated keyboard when
// TRUESENSE_SIM is set (like firmware.FromEnv), otherwise the real keyboard found under /sys.
func FromEnv(state *userstate.Store) *Keyboard {
	if os.Getenv("TRUESENSE_SIM") != "" {
		return newSimKeyboard(Sim(), state)
	}
	return New(HidrawOpener("", ""), state)
}
