// Package undervolt is the CPU undervolt: the Intel voltage-offset mailbox (MSR 0x150) with
// its safety zones, a risk rating, and the store of undervolt profiles.
//
// Safety: reading the offset is harmless (a mailbox READ request). Writing needs the explicit
// confirm flag, refuses anything beyond -120 mV, and is read back and verified; Reset writes 0.
// Nothing here applies a voltage by itself, and the CPU does not keep an offset across a reboot.
package undervolt

import (
	"errors"
	"fmt"
	"math"

	"github.com/Order52/truesense/internal/firmware"
)

// Voltage-offset mailbox (verified layout, 13th-gen HX): a write of this register is a request,
// the following read returns the answer.
const (
	mailboxMSR = 0x150

	// PlaneCore is the CPU core voltage plane; PlaneCache the ring/cache plane.
	PlaneCore  Plane = 0
	PlaneCache Plane = 2

	// MaxMV is the largest offset ever written (0 to -MaxMV millivolts).
	MaxMV = 120

	// The safety zones: up to the Safe values an offset is usually fine on 13th-gen HX; beyond
	// them, up to the Caution values, it may be unstable and must be tested under load; beyond
	// those freezes are likely.
	SafeCoreMV     = 60
	SafeCacheMV    = 50
	CautionCoreMV  = 90
	CautionCacheMV = 70

	// verifyToleranceMV is how far the read-back offset may differ from the request: the
	// mailbox works in 1/1.024 mV steps.
	verifyToleranceMV = 2
)

// Plane is a voltage plane of the mailbox.
type Plane uint64

// Offsets are the current core and cache offsets in millivolts. The JSON names are the
// Python dict keys.
type Offsets struct {
	CoreMV  int `json:"core_mv"`
	CacheMV int `json:"cache_mv"`
}

// Controller drives the mailbox through an MSR.
type Controller struct {
	MSR MSR
}

// New returns a Controller over the real MSR device at path (empty = CPU 0).
func New(path string) *Controller { return &Controller{MSR: DevMSR{Path: path}} }

// command builds a mailbox request. Offsets are in units of 1/1.024 mV, two's complement in
// the 11 bits at bit 21; the Python implementation rounded half to even.
func command(plane Plane, write bool, offsetMV int) uint64 {
	var off uint64
	op := uint64(0x10) // read the offset
	if write {
		op = 0x11
		off = uint64(int64(math.RoundToEven(float64(offsetMV)*1.024))&0xFFF) << 21
	}
	return 1<<63 | uint64(plane)<<40 | op<<32 | off
}

// decode extracts the signed offset (in mV) from a mailbox answer.
func decode(v uint64) int {
	raw := int((v >> 21) & 0x7FF)
	if raw&0x400 != 0 {
		raw -= 0x800
	}
	return int(math.RoundToEven(float64(raw) / 1.024))
}

// Get reads a plane's current offset in mV (a mailbox READ request: harmless). It needs root
// and the msr module.
func (c *Controller) Get(plane Plane) (int, error) {
	if err := c.MSR.Write(mailboxMSR, command(plane, false, 0)); err != nil {
		return 0, notAccessible(err)
	}
	v, err := c.MSR.Read(mailboxMSR)
	if err != nil {
		return 0, notAccessible(err)
	}
	return decode(v), nil
}

func notAccessible(err error) error {
	return fmt.Errorf("MSR 0x150 not accessible: %w (sudo modprobe msr; run as root)", err)
}

// Set writes one plane's offset (0 to -MaxMV mV) and verifies it by reading it back. Without
// confirm it refuses: an aggressive undervolt can freeze or reboot the machine. It returns
// the offset the CPU reports.
func (c *Controller) Set(plane Plane, offsetMV int, confirm bool) (int, error) {
	if !confirm {
		return 0, firmware.Errorf("undervolt needs explicit --confirm (can crash the machine if too aggressive)")
	}
	if offsetMV > 0 || -offsetMV > MaxMV {
		return 0, firmware.Errorf("offset must be between 0 and -%d mV (start with -10)", MaxMV)
	}
	if err := c.MSR.Write(mailboxMSR, command(plane, true, offsetMV)); err != nil {
		return 0, fmt.Errorf("MSR write refused: %w (BIOS may lock undervolting)", err)
	}
	got, err := c.Get(plane)
	if err != nil {
		return 0, err
	}
	if d := got - offsetMV; d > verifyToleranceMV || d < -verifyToleranceMV {
		return 0, firmware.Errorf("readback %d mV != %d mV (BIOS lock?); reset with: uv reset", got, offsetMV)
	}
	return got, nil
}

// Apply sets both planes (core, then cache) with confirmation and verification. If either
// fails the offsets are reset to stock before the error is returned, so a half-applied pair
// never stays.
func (c *Controller) Apply(coreMV, cacheMV int, confirm bool) (Offsets, error) {
	_, err := c.Set(PlaneCore, coreMV, confirm)
	if err == nil {
		_, err = c.Set(PlaneCache, cacheMV, confirm)
	}
	if err != nil {
		if confirm {
			_, _ = c.Reset() // best effort: the error that matters is the original one
		}
		return Offsets{}, err
	}
	return c.Read()
}

// Read returns both planes' offsets.
func (c *Controller) Read() (Offsets, error) {
	core, err := c.Get(PlaneCore)
	if err != nil {
		return Offsets{}, err
	}
	cache, err := c.Get(PlaneCache)
	if err != nil {
		return Offsets{}, err
	}
	return Offsets{CoreMV: core, CacheMV: cache}, nil
}

// Reset writes 0 to both planes (stock voltage, never needs confirmation) and reads them
// back. A write that is refused does not stop the other plane from being reset; if the planes
// do not read back as 0 the offsets are returned together with an error.
func (c *Controller) Reset() (Offsets, error) {
	var werr error
	for _, p := range []Plane{PlaneCore, PlaneCache} {
		if err := c.MSR.Write(mailboxMSR, command(p, true, 0)); err != nil {
			werr = errors.Join(werr, err)
		}
	}
	got, err := c.Read()
	if err != nil {
		return Offsets{}, err
	}
	if got != (Offsets{}) {
		return got, firmware.Errorf("reset did not take: core %d mV, cache %d mV%s", got.CoreMV, got.CacheMV, joinedNote(werr))
	}
	return got, nil
}

func joinedNote(err error) string {
	if err == nil {
		return ""
	}
	return " (" + err.Error() + ")"
}
