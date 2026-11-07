package gpuundervolt

import (
	"errors"
	"fmt"
	"strings"
	"testing"
)

// fakeNVML is a GPU with an offset, a locked clock and an allowed range. A field can make one
// call fail with a driver code.
type fakeNVML struct {
	offset        int
	lo, hi        int
	rangeErr      error
	locked        [2]uint // min, max of the lock, zero = none
	setErr        error
	lockErr       error
	resetErr      error
	kept          *int // if set, SetOffset stores this instead of the request (a driver limit)
	offsetReadErr error
	setCalls      int
	lockCalls     int
	resetCalls    int
}

func newGPU() *fakeNVML { return &fakeNVML{lo: -1000, hi: 1000} }

func (f *fakeNVML) Offset() (int, error) {
	if f.offsetReadErr != nil {
		return 0, f.offsetReadErr
	}
	return f.offset, nil
}
func (f *fakeNVML) OffsetRange() (int, int, error) { return f.lo, f.hi, f.rangeErr }
func (f *fakeNVML) SetOffset(mhz int) error {
	f.setCalls++
	if f.setErr != nil {
		return f.setErr
	}
	f.offset = mhz
	if f.kept != nil {
		f.offset = *f.kept
	}
	return nil
}
func (f *fakeNVML) LockClocks(lo, hi uint) error {
	f.lockCalls++
	if f.lockErr != nil {
		return f.lockErr
	}
	f.locked = [2]uint{lo, hi}
	return nil
}
func (f *fakeNVML) ResetClocks() error {
	f.resetCalls++
	if f.resetErr != nil {
		return f.resetErr
	}
	f.locked = [2]uint{}
	return nil
}

func opener(f *fakeNVML, opens *int) Opener {
	return func() (NVML, error) {
		if opens != nil {
			*opens++
		}
		return f, nil
	}
}

// Python: test_gpu_undervolt_needs_confirmation_and_boot_has_a_crash_guard (the NVML part).
func TestApplyAndReset(t *testing.T) {
	g := newGPU()
	c := New(opener(g, nil))
	st, err := c.Read()
	if err != nil || st != (State{0, [2]int{-1000, 1000}}) {
		t.Fatalf("Read = %+v, %v", st, err)
	}
	got, err := c.Apply(120, 2400)
	if err != nil || got != (Applied{120, 2400}) {
		t.Fatalf("Apply = %+v, %v", got, err)
	}
	if g.offset != 120 || g.locked != [2]uint{210, 2400} {
		t.Errorf("GPU state: offset %d, locked %v", g.offset, g.locked)
	}
	// Applying with no cap lifts a previous cap.
	if got, err := c.Apply(60, 0); err != nil || got != (Applied{60, 0}) || g.locked != [2]uint{} {
		t.Errorf("Apply without cap = %+v, %v, locked %v", got, err, g.locked)
	}
	c.Apply(100, 2100)
	got, err = c.Reset()
	if err != nil || got != (Applied{0, 0}) || g.offset != 0 || g.locked != [2]uint{} {
		t.Errorf("Reset = %+v, %v; GPU %d %v", got, err, g.offset, g.locked)
	}
}

func TestApplyEnforcesTheDriverRange(t *testing.T) {
	g := newGPU()
	g.lo, g.hi = -200, 200
	c := New(opener(g, nil))
	for _, off := range []int{201, -201, 900} {
		if _, err := c.Apply(off, 2400); err == nil || !strings.Contains(err.Error(), "the driver allows offsets -200..200 MHz") {
			t.Errorf("Apply(%d): %v", off, err)
		}
	}
	if g.setCalls != 0 {
		t.Error("an out-of-range offset reached the driver")
	}
	if _, err := c.Apply(200, 2400); err != nil {
		t.Errorf("200 is the edge: %v", err)
	}
}

func TestUnknownRangeTurnsTheRangeCheckOff(t *testing.T) {
	g := newGPU()
	g.rangeErr = CodeNotSupported
	c := New(opener(g, nil))
	st, err := c.Read()
	if err != nil || st.RangeMHz != [2]int{0, 0} {
		t.Fatalf("Read = %+v, %v", st, err)
	}
	if _, err := c.Apply(120, 2400); err != nil {
		t.Errorf("Apply with an unknown range: %v", err)
	}
}

func TestApplyVerifiesTheOffset(t *testing.T) {
	g := newGPU()
	kept := 100
	g.kept = &kept
	_, err := New(opener(g, nil)).Apply(150, 2400)
	if err == nil || !strings.Contains(err.Error(), "the GPU kept offset 100 MHz instead of 150") {
		t.Errorf("err = %v", err)
	}
}

func TestDriverErrorCodesAreNamed(t *testing.T) {
	tests := []struct {
		code Code
		want string
	}{
		{2, "invalid argument"},
		{3, "not supported by this GPU or driver"},
		{4, "needs root (the TrueSense service)"},
		{9, "driver not loaded"},
		{15, "GPU is lost"},
		{999, "unknown error"},
		{7, "NVML error 7"},
		{-1, "NVML error -1"},
	}
	for _, tt := range tests {
		if got := tt.code.Error(); got != tt.want {
			t.Errorf("Code(%d) = %q, want %q", tt.code, got, tt.want)
		}
	}
	g := newGPU()
	g.setErr = CodeNoPermission
	if _, err := New(opener(g, nil)).Apply(100, 2400); err == nil || err.Error() != "setting the GPU offset: needs root (the TrueSense service)" {
		t.Errorf("set error = %v", err)
	}
	g = newGPU()
	g.lockErr = CodeInvalidArgument
	if _, err := New(opener(g, nil)).Apply(100, 2400); err == nil || err.Error() != "capping the GPU clock: invalid argument" {
		t.Errorf("lock error = %v", err)
	}
	g = newGPU()
	g.offsetReadErr = Code(3)
	if _, err := New(opener(g, nil)).Read(); err == nil || err.Error() != "reading the GPU offset: not supported by this GPU or driver" {
		t.Errorf("read error = %v", err)
	}
}

func TestResetTriesBothStepsAndToleratesUnsupportedClockLock(t *testing.T) {
	g := newGPU()
	g.setErr = CodeNoPermission
	_, err := New(opener(g, nil)).Reset()
	if err == nil || !strings.Contains(err.Error(), "resetting the GPU offset") {
		t.Errorf("err = %v", err)
	}
	if g.resetCalls != 1 {
		t.Error("the clock cap must still be lifted when the offset reset fails")
	}
	g = newGPU()
	g.offset = 50
	g.resetErr = CodeNotSupported
	got, err := New(opener(g, nil)).Reset()
	if err != nil || got != (Applied{0, 0}) {
		t.Errorf("a GPU without clock locking: %+v, %v", got, err)
	}
	g = newGPU()
	g.resetErr = CodeGPUIsLost
	if _, err := New(opener(g, nil)).Reset(); err == nil || !strings.Contains(err.Error(), "GPU is lost") {
		t.Errorf("a real failure to lift the cap must be reported: %v", err)
	}
	g = newGPU()
	g.resetErr = CodeNoPermission
	if _, err := New(opener(g, nil)).Apply(0, 0); err == nil || !strings.Contains(err.Error(), "lifting the GPU clock cap") {
		t.Errorf("Apply without cap, lift fails: %v", err)
	}
}

func TestConnectsLazilyOnceAndRetriesAfterAFailure(t *testing.T) {
	g := newGPU()
	opens := 0
	c := New(opener(g, &opens))
	if opens != 0 {
		t.Fatal("New must not connect")
	}
	c.Read()
	c.Apply(10, 0)
	c.Reset()
	if opens != 1 {
		t.Errorf("opened %d times, want once", opens)
	}
	fails := 2
	flaky := func() (NVML, error) {
		if fails > 0 {
			fails--
			return nil, errors.New("NVIDIA driver library (libnvidia-ml) not found")
		}
		return g, nil
	}
	c = New(flaky)
	for _, call := range []func() error{
		func() error { _, err := c.Read(); return err },
		func() error { _, err := c.Apply(1, 0); return err },
	} {
		if err := call(); err == nil || !strings.Contains(err.Error(), "NVIDIA driver library (libnvidia-ml) not found") {
			t.Errorf("err = %v", err)
		}
	}
	if _, err := c.Read(); err != nil {
		t.Errorf("a later call must reconnect: %v", err)
	}
	c = New(func() (NVML, error) { return nil, errors.New("no NVIDIA GPU found") })
	if _, err := c.Reset(); err == nil || !strings.Contains(err.Error(), "no NVIDIA GPU found") {
		t.Errorf("Reset without a GPU: %v", err)
	}
}

func TestRisk(t *testing.T) {
	tests := []struct {
		offset, cap int
		want        Level
		contains    string
	}{
		{120, 2400, Safe, "Inside the safe range."},
		{180, 2400, Caution, "Past the safety line (+150 MHz)"},
		{220, 2400, Danger, "Beyond the danger line (+200 MHz)"},
		{100, 0, Caution, "without a clock cap"},
		{0, 0, Safe, "Stock."},
		{150, 2400, Safe, ""},
		{151, 2400, Caution, ""},
		{200, 2400, Caution, ""},
		{201, 2400, Danger, ""},
		{-100, 0, Safe, ""},
		{-101, 0, Caution, "A large negative offset"},
		{0, 1499, Caution, "A cap this low"},
		{0, 1500, Safe, "Inside the safe range."},
		{0, 2400, Safe, "Inside the safe range."},
		{-50, 0, Safe, "Inside the safe range."},
	}
	for _, tt := range tests {
		got, msg := Risk(tt.offset, tt.cap)
		if got != tt.want || !strings.Contains(msg, tt.contains) || msg == "" {
			t.Errorf("Risk(%d, %d) = %q, %q; want %q containing %q", tt.offset, tt.cap, got, msg, tt.want, tt.contains)
		}
	}
}

func FuzzRisk(f *testing.F) {
	f.Add(120, 2400)
	f.Fuzz(func(t *testing.T, o, c int) {
		if l, m := Risk(o, c); m == "" || (l != Safe && l != Caution && l != Danger) {
			t.Fatalf("Risk(%d, %d) = %q, %q", o, c, l, m)
		}
	})
}

func ExampleCode_Error() {
	fmt.Println(Code(3))
	// Output: not supported by this GPU or driver
}
