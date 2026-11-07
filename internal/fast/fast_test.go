package fast

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Order52/truesense/internal/client"
	"github.com/Order52/truesense/internal/firmware"
	"github.com/Order52/truesense/internal/hw"
	"github.com/Order52/truesense/internal/ipc"
)

func newClient(t *testing.T) (*client.Client, *firmware.Simulator) {
	sim := firmware.NewSimulator()
	h := hw.New(sim, nil)
	h.SleepFor = func(time.Duration) {}
	return &client.Client{IPC: &ipc.Client{Path: "/nonexistent/x.sock"}, HW: h}, sim
}

func run(c *client.Client, args ...string) (int, string, string) {
	var o, e bytes.Buffer
	code := Run(args, c, &o, &e)
	return code, o.String(), e.String()
}

func TestWithoutADaemonTheHardwareDoesWhatIsSafe(t *testing.T) {
	c, sim := newClient(t)
	if code, _, e := run(c, "fan", "max"); code != 0 || sim.Snapshot().Fan != "max" {
		t.Fatalf("max: %d %s", code, e)
	}
	if code, _, e := run(c, "fan", "55"); code != 0 || sim.Snapshot().Duty != [2]int{55, 55} {
		t.Fatalf("55 %%: %d %s", code, e)
	}
	if code, _, e := run(c, "power", "turbo"); code != 0 || sim.Snapshot().Mode != 5 {
		t.Fatalf("power: %d %s", code, e)
	}
	if code, _, e := run(c, "fan", "smart"); code != 1 || !strings.Contains(e, "daemon") {
		t.Fatalf("smart needs the service: %d %s", code, e)
	}
	if code, _, e := run(c, "auto", "smart"); code != 1 || !strings.Contains(e, "service") {
		t.Fatalf("auto needs the service: %d %s", code, e)
	}
}

func TestUsageErrorsAreExitCodeTwoAndNeverTouchTheHardware(t *testing.T) {
	c, sim := newClient(t)
	before := sim.Snapshot()
	for _, args := range [][]string{{}, {"fan"}, {"fan", "fast"}, {"power"}, {"power", "turbo", "-3"}, {"power", "turbo", "x"}, {"auto", "maybe"}, {"nonsense", "x"}} {
		if code, _, e := run(c, args...); code != 2 || e == "" {
			t.Errorf("%v: code %d, stderr %q", args, code, e)
		}
	}
	if after := sim.Snapshot(); after.Mode != before.Mode || after.Fan != before.Fan {
		t.Fatal("a usage error must not change anything")
	}
	if code, _, _ := run(c, "fan", "10"); code != 1 || sim.Snapshot().Fan != "auto" {
		t.Fatal("a duty below 20 is refused before it reaches the firmware")
	}
}

func TestWithADaemonItIsOneRequest(t *testing.T) {
	c, sim := newClient(t)
	dir, err := os.MkdirTemp("", "tsfast")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(dir) })
	var got []string
	h := func(name string) ipc.Handler {
		return func(_ context.Context, a ipc.Args) (any, error) { got = append(got, name); _ = a; return nil, nil }
	}
	s := &ipc.Server{Path: filepath.Join(dir, "d.sock"), Allowed: map[uint32]bool{uint32(os.Getuid()): true}, Handlers: map[string]ipc.Handler{"fan": h("fan"), "power": h("power"), "set": h("set")}}
	if err := s.Listen(); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() { _ = s.Serve(ctx); close(done) }()
	t.Cleanup(func() { cancel(); <-done; s.Close() })
	c.IPC = &ipc.Client{Path: s.Path, Timeout: time.Second}
	for _, args := range [][]string{{"fan", "smart"}, {"fan", "70"}, {"power", "quiet", "15"}, {"auto", "on"}} {
		if code, _, e := run(c, args...); code != 0 {
			t.Fatalf("%v: %d %s", args, code, e)
		}
	}
	if strings.Join(got, ",") != "fan,fan,power,set" {
		t.Fatalf("requests: %v", got)
	}
	if sim.Snapshot().Mode != 6 {
		t.Fatal("with a daemon the hardware is not touched by the client")
	}
}
