package undervolt

import (
	"errors"
	"math"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// fakeMSR plays the voltage mailbox: a write of a request is remembered, the next read returns
// the answer. It can also refuse writes (a BIOS lock) or accept them without effect.
type fakeMSR struct {
	planes  map[uint64]uint64 // plane -> raw 11-bit offset field
	answer  uint64
	writes  []uint64
	refuse  bool // every write fails
	ignore  bool // writes succeed but change nothing
	readErr error
}

func newFake() *fakeMSR { return &fakeMSR{planes: map[uint64]uint64{}} }

func (f *fakeMSR) Write(offset uint32, v uint64) error {
	if offset != 0x150 {
		return errors.New("unexpected register")
	}
	if f.refuse {
		return errors.New("operation not permitted")
	}
	f.writes = append(f.writes, v)
	plane, op := (v>>40)&0xFF, (v>>32)&0xFF
	switch op {
	case 0x11:
		if !f.ignore {
			f.planes[plane] = (v >> 21) & 0x7FF
		}
	case 0x10:
		f.answer = f.planes[plane] << 21
	default:
		return errors.New("bad opcode")
	}
	return nil
}

func (f *fakeMSR) Read(offset uint32) (uint64, error) {
	if f.readErr != nil {
		return 0, f.readErr
	}
	return f.answer, nil
}

func TestCommandEncoding(t *testing.T) {
	// Values computed with the Python _uv_cmd.
	tests := []struct {
		plane Plane
		write bool
		mv    int
		want  uint64
	}{
		{PlaneCore, false, 0, 0x8000001000000000},
		{PlaneCache, false, 0, 0x8000021000000000},
		{PlaneCore, true, -50, 0x80000011f9a00000},
		{PlaneCache, true, -40, 0x80000211fae00000},
		{PlaneCore, true, 0, 0x8000001100000000},
		{PlaneCore, true, -120, 0x80000011f0a00000},
		{PlaneCache, true, -1, 0x80000211ffe00000},
		{PlaneCore, true, -30, 0x80000011fc200000},
		{PlaneCore, true, -5, 0x80000011ff600000},
		{PlaneCore, true, -10, 0x80000011fec00000},
	}
	for _, tt := range tests {
		if got := command(tt.plane, tt.write, tt.mv); got != tt.want {
			t.Errorf("command(%d, %v, %d) = %#x, want %#x", tt.plane, tt.write, tt.mv, got, tt.want)
		}
	}
}

func TestDecode(t *testing.T) {
	tests := []struct {
		raw  uint64
		want int
	}{{0, 0}, {0x7cd, -50}, {0x400, -1000}, {0x7ff, -1}, {0x3ff, 999}, {1, 1}}
	for _, tt := range tests {
		if got := decode(tt.raw << 21); got != tt.want {
			t.Errorf("decode(%#x) = %d, want %d", tt.raw, got, tt.want)
		}
	}
}

// Every valid offset must read back as itself, within the verification tolerance.
func TestEncodeDecodeRoundTrip(t *testing.T) {
	for mv := 0; mv >= -MaxMV; mv-- {
		raw := (command(PlaneCore, true, mv) >> 21) & 0x7FF
		if got := decode(raw << 21); math.Abs(float64(got-mv)) > verifyToleranceMV {
			t.Errorf("%d mV reads back as %d", mv, got)
		}
	}
}

// Python: test_undervolt_needs_confirmation_and_boot_has_a_crash_guard (the part that is this package).
func TestSetNeedsConfirmationAndRange(t *testing.T) {
	f := newFake()
	c := &Controller{MSR: f}
	if _, err := c.Set(PlaneCore, -50, false); err == nil || !strings.Contains(err.Error(), "explicit --confirm") {
		t.Fatalf("no confirm: %v", err)
	}
	for _, mv := range []int{-300, -121, 1, 50} {
		if _, err := c.Set(PlaneCore, mv, true); err == nil || !strings.Contains(err.Error(), "between 0 and -120 mV") {
			t.Errorf("Set(%d): %v", mv, err)
		}
	}
	if len(f.writes) != 0 {
		t.Fatalf("a refused request still wrote to the MSR: %#x", f.writes)
	}
	got, err := c.Apply(-50, -40, true)
	if err != nil || got != (Offsets{-50, -40}) {
		t.Fatalf("Apply = %+v, %v", got, err)
	}
	if rd, _ := c.Read(); rd != (Offsets{-50, -40}) {
		t.Errorf("Read = %+v", rd)
	}
	reset, err := c.Reset()
	if err != nil || reset != (Offsets{}) {
		t.Errorf("Reset = %+v, %v", reset, err)
	}
	if rd, _ := c.Read(); rd != (Offsets{}) {
		t.Errorf("after reset Read = %+v", rd)
	}
}

func TestSetEdges(t *testing.T) {
	for _, mv := range []int{0, -1, -60, -120} {
		c := &Controller{MSR: newFake()}
		got, err := c.Set(PlaneCache, mv, true)
		if err != nil || math.Abs(float64(got-mv)) > 2 {
			t.Errorf("Set(%d) = %d, %v", mv, got, err)
		}
	}
}

func TestGetIsHarmless(t *testing.T) {
	f := newFake()
	c := &Controller{MSR: f}
	if _, err := c.Get(PlaneCore); err != nil {
		t.Fatal(err)
	}
	for _, w := range f.writes {
		if (w>>32)&0xFF != 0x10 {
			t.Errorf("reading wrote a non-read request %#x", w)
		}
	}
}

func TestSetFailsWhenTheBIOSLocksOrIgnoresTheWrite(t *testing.T) {
	f := newFake()
	f.refuse = true
	if _, err := (&Controller{MSR: f}).Set(PlaneCore, -50, true); err == nil || !strings.Contains(err.Error(), "MSR write refused") || !strings.Contains(err.Error(), "BIOS may lock") {
		t.Errorf("refused: %v", err)
	}
	g := newFake()
	g.ignore = true
	_, err := (&Controller{MSR: g}).Set(PlaneCore, -50, true)
	if err == nil || !strings.Contains(err.Error(), "readback 0 mV != -50 mV") {
		t.Errorf("ignored write must not look like success: %v", err)
	}
}

func TestMSRNotAccessible(t *testing.T) {
	f := newFake()
	f.readErr = errors.New("no such device")
	if _, err := (&Controller{MSR: f}).Get(PlaneCore); err == nil || !strings.Contains(err.Error(), "MSR 0x150 not accessible") || !strings.Contains(err.Error(), "modprobe msr") {
		t.Errorf("read error: %v", err)
	}
	f = newFake()
	f.refuse = true
	if _, err := (&Controller{MSR: f}).Get(PlaneCore); err == nil || !strings.Contains(err.Error(), "not accessible") {
		t.Errorf("write error: %v", err)
	}
	if _, err := (&Controller{MSR: f}).Read(); err == nil {
		t.Error("Read must fail when the MSR is unreachable")
	}
	if _, err := (&Controller{MSR: f}).Reset(); err == nil {
		t.Error("Reset must fail when the MSR is unreachable (nothing verified)")
	}
}

// If the second plane fails, the first must be put back to stock.
func TestApplyRollsBackAHalfAppliedPair(t *testing.T) {
	f := &planeFlaky{fakeMSR: newFake()}
	c := &Controller{MSR: f}
	_, err := c.Apply(-50, -40, true)
	if err == nil {
		t.Fatal("expected an error")
	}
	if f.planes[0] != 0 {
		t.Errorf("core plane left at raw %#x after a failed apply", f.planes[0])
	}
	// And without confirmation nothing is written at all.
	g := newFake()
	if _, err := (&Controller{MSR: g}).Apply(-50, -40, false); err == nil || len(g.writes) != 0 {
		t.Errorf("unconfirmed apply: %v, writes %d", err, len(g.writes))
	}
}

// planeFlaky refuses to program the cache plane with a non-zero offset.
type planeFlaky struct{ *fakeMSR }

func (f *planeFlaky) Write(offset uint32, v uint64) error {
	if (v>>40)&0xFF == 2 && (v>>32)&0xFF == 0x11 && (v>>21)&0x7FF != 0 {
		return errors.New("locked")
	}
	return f.fakeMSR.Write(offset, v)
}

func TestResetReportsWhatDidNotTake(t *testing.T) {
	f := newFake()
	f.planes[0], f.planes[2] = 0x7cd, 0x7d8 // -50 and -40 were applied
	f.ignore = true
	got, err := (&Controller{MSR: f}).Reset()
	if err == nil || !strings.Contains(err.Error(), "reset did not take") || got.CoreMV != -50 {
		t.Errorf("Reset = %+v, %v", got, err)
	}
	// One plane refusing the write does not stop the other.
	g := &planeFlaky2{fakeMSR: newFake()}
	g.planes[0], g.planes[2] = 0x7cd, 0x7d8
	got, err = (&Controller{MSR: g}).Reset()
	if err == nil || got.CoreMV != 0 || got.CacheMV == 0 {
		t.Errorf("partial reset = %+v, %v", got, err)
	}
	if !strings.Contains(err.Error(), "locked") {
		t.Errorf("the write failure should be mentioned: %v", err)
	}
}

type planeFlaky2 struct{ *fakeMSR }

func (f *planeFlaky2) Write(offset uint32, v uint64) error {
	if (v>>40)&0xFF == 2 && (v>>32)&0xFF == 0x11 {
		return errors.New("locked")
	}
	return f.fakeMSR.Write(offset, v)
}

// DevMSR against a plain file standing in for /dev/cpu/0/msr: offsets are file offsets and the
// value is 8 little-endian bytes.
func TestDevMSRReadsAndWritesAtTheRegisterOffset(t *testing.T) {
	path := filepath.Join(t.TempDir(), "msr")
	if err := os.WriteFile(path, make([]byte, 0x200), 0o600); err != nil {
		t.Fatal(err)
	}
	d := DevMSR{Path: path}
	if err := d.Write(0x150, 0x0123456789abcdef); err != nil {
		t.Fatal(err)
	}
	if v, err := d.Read(0x150); err != nil || v != 0x0123456789abcdef {
		t.Errorf("Read = %#x, %v", v, err)
	}
	raw, _ := os.ReadFile(path)
	if raw[0x150] != 0xef || raw[0x157] != 0x01 {
		t.Errorf("not little-endian at the register offset: % x", raw[0x150:0x158])
	}
	if _, err := (DevMSR{Path: filepath.Join(t.TempDir(), "none")}).Read(0x150); err == nil {
		t.Error("a missing device must be an error")
	}
	if err := (DevMSR{Path: filepath.Join(t.TempDir(), "none")}).Write(0x150, 1); err == nil {
		t.Error("a missing device must be an error")
	}
	if _, err := d.Read(0x1000); err == nil {
		t.Error("reading past the end must fail")
	}
	if got := (DevMSR{}).path(); got != "/dev/cpu/0/msr" {
		t.Errorf("default path = %q", got)
	}
	if New("").MSR.(DevMSR).path() != DefaultMSRPath {
		t.Error("New with no path must use the default device")
	}
}

func TestRisk(t *testing.T) {
	tests := []struct {
		core, cache int
		want        Level
		contains    string
	}{
		{0, 0, Safe, "Stock voltage."},
		{-50, -40, Safe, "Inside the safe range."},
		{-75, -40, Caution, "Past the safety line (core 60 mV / cache 50 mV)"},
		{-100, -40, Danger, "Beyond the danger line (core 90 mV / cache 70 mV)"},
		{-60, -50, Safe, ""},
		{-61, -50, Caution, ""},
		{-60, -51, Caution, ""},
		{-90, -70, Caution, ""},
		{-91, -70, Danger, ""},
		{-90, -71, Danger, ""},
		{-120, 0, Danger, ""},
		{0, -1, Safe, "Inside the safe range."},
	}
	for _, tt := range tests {
		got, msg := Risk(tt.core, tt.cache)
		if got != tt.want || !strings.Contains(msg, tt.contains) {
			t.Errorf("Risk(%d, %d) = %q, %q; want %q containing %q", tt.core, tt.cache, got, msg, tt.want, tt.contains)
		}
	}
}

func FuzzRisk(f *testing.F) {
	f.Add(-50, -40)
	f.Add(math.MinInt, math.MaxInt)
	f.Fuzz(func(t *testing.T, a, b int) {
		if l, msg := Risk(a, b); msg == "" || (l != Safe && l != Caution && l != Danger) {
			t.Fatalf("Risk(%d, %d) = %q, %q", a, b, l, msg)
		}
	})
}

// FuzzMailbox: any register value an unreliable MSR returns must decode without panicking,
// and anything we encode must decode back within tolerance.
func FuzzMailbox(f *testing.F) {
	f.Add(uint64(0), -50)
	f.Add(^uint64(0), 120)
	f.Fuzz(func(t *testing.T, v uint64, mv int) {
		d := decode(v)
		if d < -1000 || d > 1000 {
			t.Fatalf("decode(%#x) = %d is outside the 11-bit range", v, d)
		}
		if mv <= 0 && mv >= -MaxMV {
			if got := decode(((command(PlaneCore, true, mv) >> 21) & 0x7FF) << 21); math.Abs(float64(got-mv)) > verifyToleranceMV {
				t.Fatalf("%d mV round-trips to %d", mv, got)
			}
		}
	})
}
