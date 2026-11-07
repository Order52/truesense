package sensors

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func tree(t *testing.T, files map[string]string) string {
	t.Helper()
	root := t.TempDir()
	for p, c := range files {
		full := filepath.Join(root, p)
		if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(full, []byte(c), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return root
}

func TestEMA(t *testing.T) {
	e := EMA{Alpha: 0.5}
	if _, ok := e.Value(); ok {
		t.Fatal("no samples yet")
	}
	if got := e.Update(10); got != 10 {
		t.Fatalf("the first sample is taken as it is: %v", got)
	}
	if got := e.Update(20); got != 15 {
		t.Fatalf("got %v", got)
	}
}

func TestCPULoadFromProcStat(t *testing.T) {
	root := tree(t, map[string]string{"proc/stat": "cpu  100 0 100 800 0 0 0 0 0 0\ncpu0 1 1 1 1 1 1 1 1\n"})
	s := New()
	s.Root = root
	if got := s.CPULoad(); got != 0 {
		t.Fatalf("the first sample has nothing to compare with: %v", got)
	}
	// 200 more busy ticks and 200 more idle ticks: 50 % busy, smoothed with alpha 0.25 from the first value.
	if err := os.WriteFile(filepath.Join(root, "proc/stat"), []byte("cpu  200 0 200 1000 0 0 0 0 0 0\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if got := s.CPULoad(); got != 50 { // first update is taken as it is
		t.Fatalf("load = %v, want 50", got)
	}
}

func TestMissingFilesAreNotFatal(t *testing.T) {
	s := New()
	s.Root = t.TempDir()
	if s.CPULoad() != 0 {
		t.Fatal("no /proc/stat: zero load")
	}
	if _, ok := s.PackageTemp(); ok {
		t.Fatal("no coretemp: no temperature")
	}
	if s.BatteryPct() != 100 {
		t.Fatal("no battery: 100 %")
	}
}

func TestPackageTempAndBattery(t *testing.T) {
	root := tree(t, map[string]string{
		"sys/class/hwmon/hwmon1/name": "nvme\n", "sys/class/hwmon/hwmon1/temp1_input": "99000\n",
		"sys/class/hwmon/hwmon4/name": "coretemp\n", "sys/class/hwmon/hwmon4/temp1_input": "64500\n",
		"sys/class/power_supply/BAT1/capacity": "73\n",
	})
	s := New()
	s.Root = root
	if v, ok := s.PackageTemp(); !ok || v != 64.5 {
		t.Fatalf("temp = %v, %v", v, ok)
	}
	if s.BatteryPct() != 73 {
		t.Fatalf("battery = %v", s.BatteryPct())
	}
}

func TestParseGPULine(t *testing.T) {
	s := New()
	now := time.Unix(1000, 0)
	s.Now = func() time.Time { return now }
	for _, bad := range []string{"", "1,2", "a, b, c", "1,2,3,4", "61, 10, x"} {
		if s.ParseGPULine(bad) {
			t.Errorf("%q must be ignored", bad)
		}
	}
	if !s.ParseGPULine("61, 40, 55.5") {
		t.Fatal("a good line")
	}
	g := s.GPU()
	if g.Temp != 61 || g.Util != 40 || g.Power != 55.5 || !g.Seen.Equal(now) {
		t.Fatalf("%+v", g)
	}
	s.ParseGPULine("61, 80, 55.5")
	if g := s.GPU(); g.Util != 52 { // 40 + 0.3*(80-40)
		t.Fatalf("util = %v", g.Util)
	}
}

func FuzzParseGPULine(f *testing.F) {
	for _, s := range []string{"61, 40, 55.5", "", ",,", "a,b,c", strings.Repeat("9,", 50)} {
		f.Add(s)
	}
	s := New()
	f.Fuzz(func(t *testing.T, line string) { s.ParseGPULine(line) })
}
