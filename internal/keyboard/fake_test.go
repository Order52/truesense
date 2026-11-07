package keyboard

import (
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/Order52/truesense/internal/userstate"
)

// rec is one report as the keyboard received it.
type rec struct {
	Kind string // "set" feature, "get" feature, "out" output report
	Data []byte
}

// fakeDevice stands in for the hidraw node: it records every report in order across handles,
// answers the two queries like the real controller, and can fail on demand.
type fakeDevice struct {
	mu       sync.Mutex
	log      []rec
	opens    int
	closes   []int
	failNext int // the next N transfers fail
	openErr  error
	lastSet  []byte
	reply    func(lastSet []byte) []byte
}

func newFakeDevice() *fakeDevice {
	d := &fakeDevice{}
	d.reply = func(set []byte) []byte {
		switch set[0] {
		case 0x88:
			return []byte{0x88, 0, 3, 9, 25, 1, 1, 0x50}
		case 0xB0:
			return []byte{0xB0, 0, 1, 0, 0, 0, 0, 0x4F}
		}
		return make([]byte, 8)
	}
	return d
}

type fakeHandle struct {
	d  *fakeDevice
	id int
}

func (d *fakeDevice) opener() Opener {
	return func() (Transport, error) {
		d.mu.Lock()
		defer d.mu.Unlock()
		if d.openErr != nil {
			return nil, d.openErr
		}
		d.opens++
		return &fakeHandle{d: d, id: 100 + d.opens - 1}, nil
	}
}

func (h *fakeHandle) fail() bool {
	if h.d.failNext > 0 {
		h.d.failNext--
		return true
	}
	return false
}

func (h *fakeHandle) SetFeature(p []byte) error {
	h.d.mu.Lock()
	defer h.d.mu.Unlock()
	if h.fail() {
		return errors.New("stale handle")
	}
	h.d.log = append(h.d.log, rec{"set", append([]byte(nil), p...)})
	h.d.lastSet = append([]byte(nil), p...)
	return nil
}

func (h *fakeHandle) GetFeature(n int) ([]byte, error) {
	h.d.mu.Lock()
	defer h.d.mu.Unlock()
	if h.fail() {
		return nil, errors.New("stale handle")
	}
	r := h.d.reply(h.d.lastSet)
	h.d.log = append(h.d.log, rec{"get", []byte{byte(n)}})
	return r, nil
}

func (h *fakeHandle) WriteOutput(p []byte) error {
	h.d.mu.Lock()
	defer h.d.mu.Unlock()
	if h.fail() {
		return errors.New("stale handle")
	}
	h.d.log = append(h.d.log, rec{"out", append([]byte(nil), p...)})
	return nil
}

func (h *fakeHandle) Close() error {
	h.d.mu.Lock()
	defer h.d.mu.Unlock()
	h.d.closes = append(h.d.closes, h.id)
	return nil
}

// sets returns the feature reports sent, as hex-comparable byte slices.
func (d *fakeDevice) sets() [][]byte {
	d.mu.Lock()
	defer d.mu.Unlock()
	var out [][]byte
	for _, r := range d.log {
		if r.Kind == "set" {
			out = append(out, r.Data)
		}
	}
	return out
}

func (d *fakeDevice) reset() { d.mu.Lock(); d.log = nil; d.mu.Unlock() }

type sleepLog struct {
	mu sync.Mutex
	d  []time.Duration
}

func (s *sleepLog) sleep(d time.Duration) { s.mu.Lock(); s.d = append(s.d, d); s.mu.Unlock() }

func newTestKeyboard(t *testing.T) (*Keyboard, *fakeDevice, *userstate.Store, *sleepLog) {
	t.Helper()
	d := newFakeDevice()
	st := &userstate.Store{Path: t.TempDir() + "/user.json"}
	sl := &sleepLog{}
	return New(d.opener(), st, WithSleep(sl.sleep)), d, st, sl
}
