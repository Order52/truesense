package power

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

type fakeMSR struct {
	val uint64
	err error
}

func (f fakeMSR) Read(offset uint32) (uint64, error) {
	if offset != 0x610 {
		return 0, errors.New("unexpected register")
	}
	return f.val, f.err
}

// raplTree builds a fake /sys with both RAPL constraints set to uw.
func raplTree(t *testing.T, uw string) (root, dir string) {
	t.Helper()
	root = t.TempDir()
	dir = filepath.Join(root, "class", "powercap", "intel-rapl:0")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	for _, n := range []string{pl1File, pl2File} {
		write(t, filepath.Join(dir, n), uw)
	}
	write(t, filepath.Join(dir, "enabled"), "1\n")
	return root, dir
}

func write(t *testing.T, path, text string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(text), 0o644); err != nil {
		t.Fatal(err)
	}
}

func read(t *testing.T, path string) string {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

func smiOK(out string) Runner {
	return func(ctx context.Context, name string, args ...string) (Result, error) {
		return Result{Stdout: out}, nil
	}
}

func TestInfo(t *testing.T) {
	root, _ := raplTree(t, "80000000")
	c := &Controller{SysRoot: root, MSR: fakeMSR{val: 1 << 63}, Run: smiOK("95.00, 80.00, 35.00, 115.00\n")}
	got := c.Info(context.Background())
	if got.PL1W == nil || *got.PL1W != 80 || *got.PL2W != 80 {
		t.Errorf("limits: %+v", got)
	}
	if got.RAPLEnabled == nil || *got.RAPLEnabled != "1" {
		t.Errorf("enabled: %v", got.RAPLEnabled)
	}
	if got.MSRLockBit != true {
		t.Errorf("lock bit: %v", got.MSRLockBit)
	}
	want := NvidiaLimits{Current: "95.00", Default: "80.00", Min: "35.00", Max: "115.00"}
	if got.NvidiaLimit == nil || *got.NvidiaLimit != want {
		t.Errorf("nvidia: %+v", got.NvidiaLimit)
	}
}

func TestInfoDegradesGracefully(t *testing.T) {
	tests := []struct {
		name    string
		c       *Controller
		wantLim bool
		wantMSR any
		wantGPU bool
	}{
		{"no RAPL, no MSR, no smi", &Controller{SysRoot: t.TempDir()}, false, "unreadable (needs root + msr module)", false},
		{"MSR unreadable", &Controller{SysRoot: t.TempDir(), MSR: fakeMSR{err: errors.New("denied")}}, false, "unreadable (needs root + msr module)", false},
		{"MSR unlocked", &Controller{SysRoot: t.TempDir(), MSR: fakeMSR{val: 5}}, false, false, false},
		{"smi missing", &Controller{SysRoot: t.TempDir(), Run: func(context.Context, string, ...string) (Result, error) { return Result{}, errors.New("not found") }}, false, "unreadable (needs root + msr module)", false},
		{"smi fails", &Controller{SysRoot: t.TempDir(), Run: func(context.Context, string, ...string) (Result, error) {
			return Result{Stdout: "NVIDIA-SMI has failed", ExitCode: 9}, nil
		}}, false, "unreadable (needs root + msr module)", false},
		{"smi garbage", &Controller{SysRoot: t.TempDir(), Run: smiOK("a,b")}, false, "unreadable (needs root + msr module)", false},
		{"smi with two GPUs reads the first", &Controller{SysRoot: t.TempDir(), Run: smiOK("1,2,3,4\n5,6,7,8\n")}, false, "unreadable (needs root + msr module)", true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := tt.c.Info(context.Background())
			if (got.PL1W != nil) != tt.wantLim || got.RAPLEnabled != nil {
				t.Errorf("limits/enabled should be null: %+v", got)
			}
			if got.MSRLockBit != tt.wantMSR {
				t.Errorf("lock bit = %v, want %v", got.MSRLockBit, tt.wantMSR)
			}
			if (got.NvidiaLimit != nil) != tt.wantGPU {
				t.Errorf("nvidia = %+v", got.NvidiaLimit)
			}
		})
	}
}

func TestInfoUnparseableLimitIsNull(t *testing.T) {
	root, dir := raplTree(t, "80000000")
	write(t, filepath.Join(dir, pl1File), "max\n")
	got := (&Controller{SysRoot: root}).Info(context.Background())
	if got.PL1W != nil || got.PL2W == nil {
		t.Errorf("%+v", got)
	}
}

// Python: test_power_limits_are_kept_and_risky_values_need_force (the part that is this package).
func TestSetLimits(t *testing.T) {
	root, dir := raplTree(t, "80000000")
	c := &Controller{SysRoot: root}
	got, err := c.SetLimits(70, 90)
	if err != nil || got.PL1W != 70 || got.PL2W != 90 {
		t.Fatalf("SetLimits = %+v, %v", got, err)
	}
	if read(t, filepath.Join(dir, pl1File)) != "70000000" || read(t, filepath.Join(dir, pl2File)) != "90000000" {
		t.Errorf("files hold %q / %q", read(t, filepath.Join(dir, pl1File)), read(t, filepath.Join(dir, pl2File)))
	}
	// A value beyond the danger line is in range for the hardware (the daemon needs force for it).
	if _, err := c.SetLimits(105, 118); err != nil {
		t.Errorf("105/118 is inside 25..120: %v", err)
	}
	if got, err := c.SetLimits(70.5, 90.25); err != nil || got.PL1W != 70.5 || got.PL2W != 90.25 {
		t.Errorf("fractions: %+v, %v", got, err)
	}
}

func TestSetLimitsRangeTable(t *testing.T) {
	tests := []struct {
		pl1, pl2 float64
		ok       bool
	}{
		{25, 25, true},
		{120, 120, true},
		{70, 90, true},
		{80, 80, true},
		{24.9, 80, false},
		{70, 120.1, false},
		{70, 69, false}, // PL2 below PL1
		{0, 0, false},
		{-5, 80, false},
		{200, 200, false},
	}
	for _, tt := range tests {
		root, dir := raplTree(t, "80000000")
		_, err := (&Controller{SysRoot: root}).SetLimits(tt.pl1, tt.pl2)
		if (err == nil) != tt.ok {
			t.Errorf("SetLimits(%v, %v) err = %v, ok want %v", tt.pl1, tt.pl2, err, tt.ok)
		}
		if !tt.ok {
			if err == nil || !strings.Contains(err.Error(), "PL1/PL2 must be 25..120 W with PL2 >= PL1") {
				t.Errorf("message: %v", err)
			}
			if read(t, filepath.Join(dir, pl1File)) != "80000000" {
				t.Errorf("a refused request changed the limit")
			}
		}
	}
}

func TestSetLimitsRestoresWhenTheHardwareDidNotKeepThem(t *testing.T) {
	root, dir := raplTree(t, "80000000")
	// Firmware that clamps PL2 to 85 W whatever is asked.
	clamp := func(path, value string) error {
		if filepath.Base(path) == pl2File && value != "80000000" {
			value = "85000000"
		}
		return os.WriteFile(path, []byte(value), 0o644)
	}
	_, err := (&Controller{SysRoot: root, WriteFile: clamp}).SetLimits(70, 90)
	if err == nil || !strings.Contains(err.Error(), "readback (70, 85) != requested (70, 90); restored previous limits") {
		t.Fatalf("err = %v", err)
	}
	if read(t, filepath.Join(dir, pl1File)) != "80000000" || read(t, filepath.Join(dir, pl2File)) != "80000000" {
		t.Errorf("previous limits not restored: %q / %q", read(t, filepath.Join(dir, pl1File)), read(t, filepath.Join(dir, pl2File)))
	}
}

func TestSetLimitsRefusesToStartWhenTheCurrentLimitsAreUnreadable(t *testing.T) {
	root, dir := raplTree(t, "80000000")
	write(t, filepath.Join(dir, pl2File), "garbage")
	wrote := false
	spy := func(path, value string) error { wrote = true; return nil }
	_, err := (&Controller{SysRoot: root, WriteFile: spy}).SetLimits(70, 90)
	if err == nil || wrote {
		t.Fatalf("err = %v, wrote = %v: nothing may be written when it cannot be undone", err, wrote)
	}
}

func TestSetLimitsNoRAPL(t *testing.T) {
	_, err := (&Controller{SysRoot: t.TempDir()}).SetLimits(70, 90)
	if err == nil || !strings.Contains(err.Error(), "cannot write RAPL") {
		t.Errorf("err = %v", err)
	}
}

func TestRawRoundTrip(t *testing.T) {
	root, _ := raplTree(t, "95000000")
	c := &Controller{SysRoot: root}
	l, err := c.ReadRaw()
	if err != nil || l != (RawLimits{95000000, 95000000}) {
		t.Fatalf("ReadRaw = %+v, %v", l, err)
	}
	if err := c.WriteRaw(RawLimits{80000000, 90000000}); err != nil {
		t.Fatal(err)
	}
	if l, _ := c.ReadRaw(); l != (RawLimits{80000000, 90000000}) {
		t.Errorf("after WriteRaw: %+v", l)
	}
	if _, err := (&Controller{SysRoot: t.TempDir()}).ReadRaw(); err == nil {
		t.Error("ReadRaw on a machine without RAPL must fail")
	}
	if err := (&Controller{SysRoot: t.TempDir()}).WriteRaw(RawLimits{1, 2}); err == nil {
		t.Error("WriteRaw must not create files")
	}
}

func TestSetNvidiaLimit(t *testing.T) {
	var gotArgs []string
	ok := func(ctx context.Context, name string, args ...string) (Result, error) {
		gotArgs = append([]string{name}, args...)
		return Result{Stdout: "Power limit for GPU 0 was set to 95.00 W\n"}, nil
	}
	out, err := (&Controller{Run: ok}).SetNvidiaLimit(context.Background(), 95.7)
	if err != nil || out != "Power limit for GPU 0 was set to 95.00 W" {
		t.Fatalf("out = %q, err = %v", out, err)
	}
	if strings.Join(gotArgs, " ") != "nvidia-smi -pl 95" {
		t.Errorf("command = %v", gotArgs)
	}
	fail := func(ctx context.Context, name string, args ...string) (Result, error) {
		return Result{Stdout: "Provided power limit 5 is not a valid.\n", Stderr: "Terminating early.\n", ExitCode: 4}, nil
	}
	if _, err := (&Controller{Run: fail}).SetNvidiaLimit(context.Background(), 5); err == nil || !strings.Contains(err.Error(), "not a valid") || !strings.Contains(err.Error(), "Terminating early.") {
		t.Errorf("failure message: %v", err)
	}
	gone := func(context.Context, string, ...string) (Result, error) {
		return Result{}, errors.New("executable file not found")
	}
	if _, err := (&Controller{Run: gone}).SetNvidiaLimit(context.Background(), 90); err == nil || !strings.Contains(err.Error(), "not found") {
		t.Errorf("missing binary: %v", err)
	}
	if _, err := (&Controller{}).SetNvidiaLimit(context.Background(), 90); err == nil {
		t.Error("no runner must be an error")
	}
	for _, w := range []float64{0, -1, 1001} {
		called := false
		c := &Controller{Run: func(context.Context, string, ...string) (Result, error) { called = true; return Result{}, nil }}
		if _, err := c.SetNvidiaLimit(context.Background(), w); err == nil || called {
			t.Errorf("w = %v: err = %v, ran = %v", w, err, called)
		}
	}
}

func TestRisk(t *testing.T) {
	tests := []struct {
		pl1, pl2 float64
		want     Level
		contains string
	}{
		{65, 80, Safe, "Inside the safe range."}, // Python test
		{90, 100, Caution, "Above stock (80 W sustained / 100 W burst)"},
		{110, 115, Danger, "Beyond the danger line (100 W sustained / 115 W burst)"},
		{80, 100, Safe, ""},
		{81, 100, Caution, ""},
		{80, 101, Caution, ""},
		{100, 115, Caution, ""},
		{101, 100, Danger, ""},
		{100, 116, Danger, ""},
		{34, 80, Caution, "Very low limits"},
		{35, 34, Caution, "Very low limits"},
		{35, 35, Safe, ""},
	}
	for _, tt := range tests {
		got, msg := Risk(tt.pl1, tt.pl2)
		if got != tt.want || !strings.Contains(msg, tt.contains) || msg == "" {
			t.Errorf("Risk(%v, %v) = %q, %q; want %q containing %q", tt.pl1, tt.pl2, got, msg, tt.want, tt.contains)
		}
	}
}

func FuzzRisk(f *testing.F) {
	f.Add(65.0, 80.0)
	f.Add(-1.0, 1e300)
	f.Fuzz(func(t *testing.T, a, b float64) {
		l, msg := Risk(a, b)
		if msg == "" || (l != Safe && l != Caution && l != Danger) {
			t.Fatalf("Risk(%v, %v) = %q, %q", a, b, l, msg)
		}
	})
}

func FuzzInfoParsing(f *testing.F) {
	for _, s := range []string{"95.00, 80.00, 35.00, 115.00\n", "", "a", ",,,", "1,2,3,4\n5,6,7,8"} {
		f.Add(s)
	}
	f.Fuzz(func(t *testing.T, out string) {
		c := &Controller{SysRoot: t.TempDir(), Run: smiOK(out)}
		_ = c.Info(context.Background()) // must not panic whatever nvidia-smi prints
	})
}
