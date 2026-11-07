// Package gpuundervolt is the NVIDIA GPU undervolt through NVML (the interface nvidia-smi
// uses): a clock-curve offset plus an optional cap on the maximum clock, with a risk rating
// and the store of profiles.
//
// Linux drivers do not expose GPU voltage directly. Shifting the voltage/frequency curve UP by
// an offset (so each clock needs less voltage) and capping the maximum clock is how an
// undervolt is done here: the GPU reaches its cap at a lower voltage. Needs root (the
// TrueSense service). Neither setting survives a reboot. Values are conservative starting
// points for the RTX 4060 Laptop, NOT validated on every unit.
//
// NVML is reached through the NVML interface (nvml.go); the one real implementation loads the
// driver library at run time, and everything else here is plain logic over that interface.
package gpuundervolt

import (
	"errors"
	"fmt"
	"sync"

	"github.com/Order52/truesense/internal/firmware"
)

// Limits of what a profile may contain. The driver reports its own allowed offset range too
// (about +-1000 MHz), which Apply also enforces.
const (
	OffsetMinMHz = -300
	OffsetMaxMHz = 250
	CapMinMHz    = 600 // a cap is 0 (none) or CapMinMHz..CapMaxMHz
	CapMaxMHz    = 3300

	// minLockedClockMHz is the lower end of the locked range: the GPU's idle clock, so only
	// the top is capped.
	minLockedClockMHz = 210
)

// State is the current curve offset and the range the driver allows. The JSON names are the
// Python dict keys.
type State struct {
	OffsetMHz int    `json:"offset_mhz"`
	RangeMHz  [2]int `json:"range_mhz"`
}

// Applied is the verified result of Apply and Reset.
type Applied struct {
	OffsetMHz int `json:"offset_mhz"`
	CapMHz    int `json:"cap_mhz"`
}

// Controller operates the GPU through an Opener. It connects on first use, keeps the
// connection, and serialises calls (NVML wants one caller at a time).
type Controller struct {
	open Opener

	mu sync.Mutex
	nv NVML
}

// New returns a Controller that connects with open (OpenNVML in production).
func New(open Opener) *Controller { return &Controller{open: open} }

// connect returns the NVML connection, opening it if needed. A failed open is not cached, so a
// driver that loads later is picked up.
func (c *Controller) connect() (NVML, error) {
	if c.nv != nil {
		return c.nv, nil
	}
	nv, err := c.open()
	if err != nil {
		return nil, err
	}
	c.nv = nv
	return nv, nil
}

// Read returns the current curve offset and the allowed range (a harmless read). A driver that
// cannot say its range reports [0, 0], which turns the range check in Apply off.
func (c *Controller) Read() (State, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.read()
}

func (c *Controller) read() (State, error) {
	nv, err := c.connect()
	if err != nil {
		return State{}, err
	}
	off, err := nv.Offset()
	if err != nil {
		return State{}, fmt.Errorf("reading the GPU offset: %w", err)
	}
	lo, hi, err := nv.OffsetRange()
	if err != nil {
		lo, hi = 0, 0 // unknown range
	}
	return State{OffsetMHz: off, RangeMHz: [2]int{lo, hi}}, nil
}

// Apply shifts the curve by offset MHz and caps the clock at capMHz (0 = no cap), then reads
// the offset back. The offset must be inside the range the driver reports. The caller is
// responsible for the confirmation and the Validate/Risk checks; on an error it should Reset.
func (c *Controller) Apply(offset, capMHz int) (Applied, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	st, err := c.read()
	if err != nil {
		return Applied{}, err
	}
	lo, hi := st.RangeMHz[0], st.RangeMHz[1]
	if hi != 0 && (offset < lo || offset > hi) {
		return Applied{}, firmware.Errorf("the driver allows offsets %d..%d MHz", lo, hi)
	}
	nv := c.nv
	if err := nv.SetOffset(offset); err != nil {
		return Applied{}, fmt.Errorf("setting the GPU offset: %w", err)
	}
	if capMHz != 0 {
		if err := nv.LockClocks(minLockedClockMHz, uint(capMHz)); err != nil {
			return Applied{}, fmt.Errorf("capping the GPU clock: %w", err)
		}
	} else if err := resetClocks(nv); err != nil {
		return Applied{}, fmt.Errorf("lifting the GPU clock cap: %w", err)
	}
	after, err := nv.Offset()
	if err != nil {
		return Applied{}, fmt.Errorf("reading the GPU offset: %w", err)
	}
	if after != offset {
		return Applied{}, firmware.Errorf("the GPU kept offset %d MHz instead of %d (driver limit?)", after, offset)
	}
	return Applied{OffsetMHz: after, CapMHz: capMHz}, nil
}

// Reset puts the curve offset back to 0 and lifts the clock cap, and returns what the GPU
// reports. Both steps are tried even if the first fails, so a cap can never stay behind just
// because the offset could not be reset.
func (c *Controller) Reset() (Applied, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if _, err := c.connect(); err != nil {
		return Applied{}, err
	}
	var errs []error
	if err := c.nv.SetOffset(0); err != nil {
		errs = append(errs, fmt.Errorf("resetting the GPU offset: %w", err))
	}
	if err := resetClocks(c.nv); err != nil {
		errs = append(errs, fmt.Errorf("lifting the GPU clock cap: %w", err))
	}
	if err := errors.Join(errs...); err != nil {
		return Applied{}, err
	}
	st, err := c.read()
	if err != nil {
		return Applied{}, err
	}
	return Applied{OffsetMHz: st.OffsetMHz, CapMHz: 0}, nil
}

// resetClocks lifts the clock lock. A GPU or driver that does not support locked clocks has
// nothing to lift, which is success: the Python implementation ignored the result entirely;
// any other failure is reported because a cap that stays on silently costs speed.
func resetClocks(nv NVML) error {
	if err := nv.ResetClocks(); err != nil && !errors.Is(err, CodeNotSupported) {
		return err
	}
	return nil
}
