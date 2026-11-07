package crashguard

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func exists(p string) bool { _, err := os.Stat(p); return err == nil }

func TestNormalBootArmsThenProvesStable(t *testing.T) {
	p := filepath.Join(t.TempDir(), "sub", "uv-pending")
	now := time.Unix(1_000, 0)
	g := New(p)
	g.Now = func() time.Time { return now }
	if g.Tripped() {
		t.Fatal("no flag: not tripped")
	}
	if err := g.Arm(); err != nil || !exists(p) || !g.Armed() {
		t.Fatalf("arm: %v", err)
	}
	now = now.Add(9 * time.Minute)
	g.Tick()
	if !exists(p) {
		t.Fatal("not stable yet")
	}
	now = now.Add(2 * time.Minute)
	g.Tick()
	if exists(p) || g.Armed() {
		t.Fatal("after ten minutes the flag must go")
	}
}

func TestACrashLeavesTheFlagAndTheNextStartDropsTheSetting(t *testing.T) {
	p := filepath.Join(t.TempDir(), "uv-pending")
	if err := New(p).Arm(); err != nil { // the previous boot armed it and never got to remove it
		t.Fatal(err)
	}
	g := New(p)
	if !g.Tripped() {
		t.Fatal("the surviving flag means the last boot crashed")
	}
	if exists(p) {
		t.Fatal("tripping consumes the flag so the boot after that is normal")
	}
	if g.Tripped() {
		t.Fatal("only once")
	}
}

func TestFailedApplyDisarms(t *testing.T) {
	p := filepath.Join(t.TempDir(), "uv-pending")
	g := New(p)
	if err := g.Arm(); err != nil {
		t.Fatal(err)
	}
	g.Disarm()
	if exists(p) || g.Armed() {
		t.Fatal("a setting that failed to apply must not leave the flag")
	}
	g.Disarm() // idempotent
}

func TestUnwritableFlagRefusesToArm(t *testing.T) {
	if err := New("/proc/nonexistent/uv-pending").Arm(); err == nil {
		t.Fatal("without a flag there is no protection: Arm must fail so the caller does not apply")
	}
}
