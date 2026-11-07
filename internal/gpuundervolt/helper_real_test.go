package gpuundervolt

import (
	"context"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// buildHelper compiles the real C helper and a FAKE NVML library into a temp dir, and points the
// dynamic loader at the fake. The test refuses to go on unless the fake answers (its curve
// offset reads 123456): the real driver can never be written by a test.
func buildHelper(t *testing.T) *helperNVML {
	t.Helper()
	cc, err := exec.LookPath("cc")
	if err != nil {
		t.Skip("no C compiler: the helper is not built here")
	}
	dir := t.TempDir()
	run := func(args ...string) {
		t.Helper()
		if out, err := exec.CommandContext(context.Background(), cc, args...).CombinedOutput(); err != nil {
			t.Fatalf("cc %v: %v\n%s", args, err, out)
		}
	}
	helper := filepath.Join(dir, HelperName)
	run("-O2", "-Wall", "-Wextra", "-Werror", "-o", helper, "../../tools/nvml/truesense-nvml.c", "-ldl")
	run("-shared", "-fPIC", "-o", filepath.Join(dir, "libnvidia-ml.so.1"), "testdata/fakenvml.c")
	t.Setenv("LD_LIBRARY_PATH", dir)
	t.Setenv("FAKE_NVML_STATE", filepath.Join(dir, "offset"))
	h := newHelper(helper, ExecRun, 5*time.Second)
	off, err := h.Offset()
	if err != nil || off != 123456 {
		t.Fatalf("the fake driver is not the one answering (offset %d, %v): refusing to continue", off, err)
	}
	return h
}

func TestTheRealHelperAgainstAFakeDriver(t *testing.T) {
	h := buildHelper(t)
	if lo, hi, err := h.OffsetRange(); err != nil || lo != -300 || hi != 250 {
		t.Fatalf("range: %d %d %v", lo, hi, err)
	}
	if err := h.SetOffset(-150); err != nil {
		t.Fatal(err)
	}
	if off, _ := h.Offset(); off != -150 {
		t.Fatalf("the offset must have been set: %d", off)
	}
	if err := h.LockClocks(210, 2000); err != nil {
		t.Fatal(err)
	}
	if err := h.LockClocks(3000, 2000); err == nil {
		t.Fatal("the driver's refusal must come back as an error")
	}
	if err := h.ResetClocks(); err != nil {
		t.Fatal(err)
	}
}

func TestTheRealHelperReportsDriverTroubleInWords(t *testing.T) {
	h := buildHelper(t)
	for mode, want := range map[string]string{"noinit": "could not start NVML", "nodevice": "no NVIDIA GPU found", "set4": "needs root (the TrueSense service)"} {
		t.Setenv("FAKE_NVML_MODE", mode)
		var err error
		if mode == "set4" {
			err = h.SetOffset(-10)
		} else {
			_, err = h.Offset()
		}
		if err == nil || !strings.Contains(err.Error(), want) {
			t.Errorf("%s: %v, want %q", mode, err, want)
		}
	}
}

func TestTheRealHelperRefusesBadArgumentsBeforeTouchingTheDriver(t *testing.T) {
	h := buildHelper(t)
	for _, args := range [][]string{{}, {"bogus"}, {"set-offset"}, {"set-offset", "abc"}, {"set-offset", "99999999"}, {"lock", "1"}, {"lock", "-1", "5"}, {"offset", "extra"}} {
		out, err := ExecRun(context.Background(), h.path, args...)
		if err != nil || strings.TrimSpace(out) != "err usage" {
			t.Errorf("%v -> %q, %v; want \"err usage\"", args, out, err)
		}
	}
	if off, _ := h.Offset(); off != 123456 {
		t.Fatalf("a refused request must not have changed anything: %d", off)
	}
}
