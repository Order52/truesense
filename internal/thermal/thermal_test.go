package thermal

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

var params = Params{Enabled: true, Hi: 88, Lo: 83, MaxKHz: 5_000_000, MinKHz: 2_800_000, DownKHz: 200_000, UpKHz: 100_000}

func TestHotLowersTheCapAtMostEveryTwoTicksAndCoolRaisesItEveryThree(t *testing.T) {
	g := New(params)
	if !g.Step(10, 90, true) || g.Cap != 4_800_000 {
		t.Fatalf("hot: cap = %d", g.Cap)
	}
	if g.Step(11, 95, true) {
		t.Fatal("one tick later it must wait (no flapping)")
	}
	if !g.Step(12, 95, true) || g.Cap != 4_600_000 {
		t.Fatalf("cap = %d", g.Cap)
	}
	if g.Step(14, 80, true) {
		t.Fatal("two ticks after the last change is too soon to go back up")
	}
	if !g.Step(15, 80, true) || g.Cap != 4_700_000 {
		t.Fatalf("cap = %d", g.Cap)
	}
	if g.Step(20, 85, true) {
		t.Fatal("between Lo and Hi nothing changes")
	}
}

func TestNeverBelowTheMinimumOrAboveTheMaximum(t *testing.T) {
	g := New(params)
	for tick := 0; tick < 200; tick += 2 {
		g.Step(tick, 99, true)
	}
	if g.Cap != params.MinKHz {
		t.Fatalf("cap = %d, want the floor %d", g.Cap, params.MinKHz)
	}
	for tick := 300; tick < 700; tick += 3 {
		g.Step(tick, 50, true)
	}
	if g.Cap != params.MaxKHz {
		t.Fatalf("cap = %d, want back to the maximum", g.Cap)
	}
}

func TestNotManagingHandsTheFullClockBack(t *testing.T) {
	g := New(params)
	g.Step(10, 99, true)
	if g.Cap == params.MaxKHz {
		t.Fatal("setup: should be capped")
	}
	if !g.Step(11, 99, false) || g.Cap != params.MaxKHz {
		t.Fatal("when nothing smart is on the cap must be lifted at once")
	}
	if g.Step(12, 99, false) {
		t.Fatal("and nothing is written again")
	}
	g2 := New(Params{Enabled: false, MaxKHz: 5_000_000})
	if g2.Step(1, 99, true) {
		t.Fatal("a disabled governor never caps")
	}
}

func TestACorSuspendResyncDelaysTheNextChange(t *testing.T) {
	g := New(params)
	g.Resync(100)
	if g.Step(101, 99, true) {
		t.Fatal("too soon after a resync")
	}
	if !g.Step(102, 99, true) {
		t.Fatal("two ticks later it may act")
	}
}

func TestWhileCappedTheLimitIsReassertedEveryThirtyTicks(t *testing.T) {
	g := New(params)
	g.Step(10, 90, true)
	if !g.Step(30, 85, true) {
		t.Fatal("tick 30 must re-assert a cap that something else may have raised")
	}
	if g.Step(31, 85, true) {
		t.Fatal("other ticks must not")
	}
}

func cpus(t *testing.T) string {
	root := t.TempDir()
	for i := 0; i < 3; i++ {
		d := filepath.Join(root, "sys/devices/system/cpu", "cpu"+string(rune('0'+i)), "cpufreq")
		if err := os.MkdirAll(d, 0o755); err != nil {
			t.Fatal(err)
		}
		_ = os.WriteFile(filepath.Join(d, "scaling_max_freq"), []byte("5000000\n"), 0o644)
		_ = os.WriteFile(filepath.Join(d, "cpuinfo_max_freq"), []byte("5400000\n"), 0o644)
	}
	return root
}

func TestWriterAppliesAndUncaps(t *testing.T) {
	root := cpus(t)
	w := Writer{Root: root}
	w.Apply(4_000_000)
	for i := 0; i < 3; i++ {
		b, _ := os.ReadFile(filepath.Join(root, "sys/devices/system/cpu", "cpu"+string(rune('0'+i)), "cpufreq/scaling_max_freq"))
		if strings.TrimSpace(string(b)) != "4000000" {
			t.Fatalf("cpu%d = %q", i, b)
		}
	}
	w.Uncap()
	b, _ := os.ReadFile(filepath.Join(root, "sys/devices/system/cpu/cpu0/cpufreq/scaling_max_freq"))
	if strings.TrimSpace(string(b)) != "5400000" {
		t.Fatalf("uncap must restore the CPU's own maximum: %q", b)
	}
}
