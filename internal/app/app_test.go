package app

import (
	"bytes"
	"context"
	"path/filepath"
	"strings"
	"testing"
)

// simulated points the whole process at the firmware simulator, a private home and state, no
// control socket and a PATH with no programs in it.
func simulated(t *testing.T) {
	t.Helper()
	dir := t.TempDir()
	t.Setenv("TRUESENSE_SIM", "1")
	t.Setenv("TRUESENSE_USERSTATE", filepath.Join(dir, "user.json"))
	t.Setenv("TRUESENSE_SOCKET", filepath.Join(dir, "none.sock"))
	t.Setenv("HOME", filepath.Join(dir, "home"))
	t.Setenv("XDG_CONFIG_HOME", "")
	t.Setenv("PATH", dir)
}

func run(args ...string) (int, string, string) {
	var out, errw bytes.Buffer
	code := Run(append([]string{"truesense"}, args...), &out, &errw)
	return code, out.String(), errw.String()
}

func TestRunDispatchesTheCommandLine(t *testing.T) {
	simulated(t)
	code, out, errw := run("sensors")
	if code != 0 || !strings.HasPrefix(out, "CPU 50 C") || errw != "" {
		t.Fatalf("%d %q %q", code, out, errw)
	}
	if code, out, _ := run("--version"); code != 0 || !strings.HasPrefix(out, "truesense ") {
		t.Fatalf("%d %q", code, out)
	}
	if code, _, errw := run("nosuch"); code != 2 || !strings.Contains(errw, "unknown command") {
		t.Fatalf("%d %q", code, errw)
	}
	if code, _, _ := run(); code != 2 {
		t.Fatalf("no arguments: %d", code)
	}
	if Run(nil, &bytes.Buffer{}, &bytes.Buffer{}) != 2 {
		t.Fatal("no program name")
	}
}

func TestTruesenseFastIsSelectedByTheProgramName(t *testing.T) {
	simulated(t)
	var out, errw bytes.Buffer
	if code := Run([]string{"/usr/bin/truesense-fast", "fan", "max"}, &out, &errw); code != 0 || errw.String() != "" {
		t.Fatalf("%d %q", code, errw.String())
	}
	if code := Run([]string{"truesense-fast"}, &out, &errw); code != 2 || !strings.Contains(errw.String(), "usage: truesense-fast") {
		t.Fatalf("%d %q", code, errw.String())
	}
}

func TestTUINeedsATerminal(t *testing.T) {
	simulated(t)
	code, out, errw := run("tui")
	if code != 1 || out != "" || errw != "truesense tui needs a terminal\n" {
		t.Fatalf("%d %q %q", code, out, errw)
	}
	if code, _, errw := run("tui", "--page", "nowhere"); code != 2 || !strings.Contains(errw, "invalid choice") {
		t.Fatalf("%d %q", code, errw)
	}
}

func TestAnInterruptedCommandThatFailsExitsWith130(t *testing.T) {
	simulated(t)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	var out, errw bytes.Buffer
	if code := runCLI(ctx, []string{"fan", "off"}, strings.NewReader(""), &out, &errw); code != exitInterrupted {
		t.Fatalf("%d %q", code, errw.String())
	}
	if code := runCLI(ctx, []string{"power", "list"}, strings.NewReader(""), &out, &errw); code != 0 {
		t.Fatalf("a command that succeeded is not turned into a failure: %d", code)
	}
}
