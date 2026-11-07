package client

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Order52/truesense/internal/firmware"
	"github.com/Order52/truesense/internal/hw"
	"github.com/Order52/truesense/internal/ipc"
)

func ptr(n int) *int { return &n }

func newClient(t *testing.T) (*Client, *firmware.Simulator) {
	t.Helper()
	sim := firmware.NewSimulator()
	h := hw.New(sim, nil)
	h.SleepFor = func(time.Duration) {}
	return &Client{IPC: &ipc.Client{Path: "/nonexistent/none.sock"}, HW: h}, sim
}

// withDaemon starts a fake daemon that records every request it gets.
func withDaemon(t *testing.T, c *Client) *[]ipc.Request {
	t.Helper()
	dir, err := os.MkdirTemp("", "tscl")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(dir) })
	var got []ipc.Request
	rec := func(name string) ipc.Handler {
		return func(_ context.Context, a ipc.Args) (any, error) {
			got = append(got, ipc.Request{Cmd: name, Args: a})
			return nil, nil
		}
	}
	s := &ipc.Server{Path: filepath.Join(dir, "d.sock"), Allowed: map[uint32]bool{uint32(os.Getuid()): true}, Handlers: map[string]ipc.Handler{"fan": rec("fan"), "power": rec("power")}}
	if err := s.Listen(); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() { _ = s.Serve(ctx); close(done) }()
	t.Cleanup(func() { cancel(); <-done; s.Close() })
	c.IPC = &ipc.Client{Path: s.Path, Timeout: 2 * time.Second}
	return &got
}

func TestWithoutADaemonSafeThingsGoToTheHardware(t *testing.T) {
	c, sim := newClient(t)
	if err := c.Fan(FanRequest{Mode: "max"}); err != nil || sim.Snapshot().Fan != "max" {
		t.Fatalf("max: %v %+v", err, sim.Snapshot())
	}
	if err := c.Fan(FanRequest{Mode: "manual", CPU: ptr(50)}); err != nil || sim.Snapshot().Duty != [2]int{50, 50} {
		t.Fatalf("manual with one duty applies to both fans: %v %+v", err, sim.Snapshot())
	}
	if err := c.Fan(FanRequest{Mode: "auto"}); err != nil || sim.Snapshot().Fan != "auto" {
		t.Fatal("auto", err)
	}
	if got, err := c.Power("turbo", 30); err != nil || got != "turbo" || sim.Snapshot().Mode != 5 {
		t.Fatalf("power: %q %v", got, err)
	}
}

func TestWithoutADaemonStoppingAFanAndSmartAreRefused(t *testing.T) {
	c, sim := newClient(t)
	before := sim.Snapshot()
	for name, r := range map[string]FanRequest{
		"off":        {Mode: "off", Confirm: true},
		"stop one":   {Mode: "manual", CPU: ptr(0), GPU: ptr(50), Confirm: true},
		"smart":      {Mode: "smart"},
		"bad duty":   {Mode: "manual", CPU: ptr(10)},
		"no duty":    {Mode: "manual"},
		"bad mode":   {Mode: "turbo"},
		"duty > 100": {Mode: "manual", CPU: ptr(30), GPU: ptr(101)},
	} {
		if err := c.Fan(r); err == nil {
			t.Errorf("%s must be refused", name)
		}
	}
	if s := sim.Snapshot(); s.Fan != before.Fan || s.Duty != before.Duty {
		t.Fatalf("a refused request must not reach the firmware: %+v", s)
	}
}

func TestWithADaemonRequestsGoThereAndStopsCarryTheConfirm(t *testing.T) {
	c, sim := newClient(t)
	got := withDaemon(t, c)
	if err := c.Fan(FanRequest{Mode: "smart"}); err != nil {
		t.Fatal(err)
	}
	if err := c.Fan(FanRequest{Mode: "manual", CPU: ptr(0), GPU: ptr(60), Confirm: true}); err != nil {
		t.Fatal(err)
	}
	if err := c.Fan(FanRequest{Mode: "off", Confirm: false}); err != nil {
		t.Fatal(err)
	}
	if _, err := c.Power("quiet", 30); err != nil {
		t.Fatal(err)
	}
	if len(*got) != 4 {
		t.Fatalf("requests: %+v", *got)
	}
	if a := (*got)[1].Args; a["cpu"] != 0.0 || a["confirm"] != true {
		t.Fatalf("a fan stop must carry its duties and the confirm: %v", a)
	}
	if a := (*got)[2].Args; a["confirm"] != false {
		t.Fatalf("an unconfirmed off must say so (the daemon refuses it): %v", a)
	}
	if a := (*got)[3].Args; a["hold_minutes"] != 30.0 {
		t.Fatalf("power must hold: %v", a)
	}
	if s := sim.Snapshot(); s.Mode != 6 || s.Fan != "auto" {
		t.Fatalf("with a daemon the client must not touch the hardware itself: %+v", s)
	}
	if err := c.Fan(FanRequest{Mode: "manual", CPU: ptr(5)}); err == nil || !strings.Contains(err.Error(), "20..100") {
		t.Fatalf("an invalid duty is refused locally, before the daemon: %v", err)
	}
}
