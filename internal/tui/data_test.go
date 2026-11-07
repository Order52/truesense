package tui

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func TestSlowReadsKeepResultsAndErrorsPerKey(t *testing.T) {
	d := NewData(newFake(), nil)
	ctx := context.Background()
	d.SlowSync(ctx, []Read{{"a", func(context.Context) (any, error) { return 1, nil }}})
	if v, _ := d.Get("a"); v != 1 {
		t.Fatalf("a = %v", v)
	}
	d.SlowSync(ctx, []Read{{"b", func(context.Context) (any, error) { return nil, errors.New("boom") }}})
	if _, ok := d.Get("b"); ok {
		t.Fatal("a failed read left a value")
	}
	if e, ok := d.ReadError("b"); !ok || e != "boom" {
		t.Fatalf("error %q %v", e, ok)
	}
	d.SlowSync(ctx, []Read{{"b", func(context.Context) (any, error) { return 2, nil }}})
	if _, ok := d.ReadError("b"); ok {
		t.Fatal("a good read must clear the error")
	}
}

func TestSlowReadsNeverOverlapAndTheNewestRequestRunsAfterTheCurrentOne(t *testing.T) {
	d := NewData(newFake(), nil)
	var active, peak, runs atomic.Int32
	var mu sync.Mutex
	var order []string
	gate := make(chan struct{})
	read := func(name string, wait bool) Read {
		return Read{name, func(context.Context) (any, error) {
			n := active.Add(1)
			peak.Store(max(peak.Load(), n))
			runs.Add(1)
			if wait {
				<-gate
			}
			mu.Lock()
			order = append(order, name)
			mu.Unlock()
			active.Add(-1)
			return name, nil
		}}
	}
	d.Slow(context.Background(), []Read{read("first", true)})
	for runs.Load() == 0 {
		time.Sleep(time.Millisecond)
	}
	d.Slow(context.Background(), []Read{read("skipped", false)})
	d.Slow(context.Background(), []Read{read("newest", false)})
	close(gate)
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		if _, ok := d.Get("newest"); ok {
			break
		}
		time.Sleep(time.Millisecond)
	}
	mu.Lock()
	defer mu.Unlock()
	if peak.Load() != 1 || len(order) != 2 || order[0] != "first" || order[1] != "newest" {
		t.Fatalf("peak %d order %v", peak.Load(), order)
	}
	if _, ok := d.Get("skipped"); ok {
		t.Fatal("a request replaced by a newer one still ran")
	}
}

func TestFastKeepsThePreviousReadingsWhenTheHardwareFails(t *testing.T) {
	b := newFake()
	d := NewData(b, nil)
	ctx := context.Background()
	d.Fast(ctx)
	if s, ok := d.sensors(); !ok || s.CPUTemp != 50 {
		t.Fatal("first reading")
	}
	b.failOn("Sensors", errors.New("gone"))
	b.mu.Lock()
	b.temps.CPUTemp = 99
	b.mu.Unlock()
	d.Fast(ctx)
	if s, _ := d.sensors(); s.CPUTemp != 50 {
		t.Fatalf("a failed read replaced the reading: %d", s.CPUTemp)
	}
	if v := d.view(); v.err != "gone" || len(v.hist[histTCPU]) != 1 {
		t.Fatalf("err %q history %d", v.err, len(v.hist[histTCPU]))
	}
	b.failOn("Sensors", nil)
	d.Fast(ctx)
	if v := d.view(); v.err != "" || v.sens.CPUTemp != 99 {
		t.Fatalf("recovery: err %q temp %d", v.err, v.sens.CPUTemp)
	}
}

func TestFastPrefersTheServicesReadingsAndFallsBackToTheHardware(t *testing.T) {
	b := newFake()
	b.service = true
	d := NewData(b, nil)
	d.Fast(context.Background())
	if !d.daemon() || d.powerMode() != "eco" || d.service().Activity != "idle" {
		t.Fatal("service state not used")
	}
	b.service = false
	d.Fast(context.Background())
	if d.daemon() {
		t.Fatal("service state kept after it went away")
	}
	if d.powerMode() != "eco" {
		t.Fatal("power mode not read from the hardware")
	}
}

func TestAnUnknownPowerModeFromTheServiceIsReadFromTheHardware(t *testing.T) {
	b := newFake()
	b.service = true
	d := NewData(b, nil)
	d.Fast(context.Background())
	b.mu.Lock()
	b.mode = "weird"
	b.mu.Unlock()
	d.Fast(context.Background())
	if d.powerMode() != "weird" {
		t.Fatalf("mode %q", d.powerMode())
	}
}

func TestHistoryIsABoundedRing(t *testing.T) {
	var r ring
	for i := range historyLen + 50 {
		r.push(i)
	}
	if len(r.vals) != historyLen || r.vals[0] != 50 || r.vals[historyLen-1] != historyLen+49 {
		t.Fatalf("len %d first %d last %d", len(r.vals), r.vals[0], r.vals[len(r.vals)-1])
	}
}

func TestDataLoopReadsUntilTheContextEnds(t *testing.T) {
	b := newFake()
	var ticks atomic.Int32
	d := NewData(b, func() { ticks.Add(1) })
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() { d.Loop(ctx); close(done) }()
	for ticks.Load() == 0 {
		time.Sleep(time.Millisecond)
	}
	d.Wake()
	d.Wake() // a second wake must not block
	cancel()
	select {
	case <-done:
	case <-time.After(3 * time.Second):
		t.Fatal("Loop did not stop")
	}
}

func TestSparkline(t *testing.T) {
	g := NewGlyphs(true)
	if got := spark(g, []int{0, 50, 100}, 5, 0, 100); got != "   ▄█" {
		t.Fatalf("%q", got)
	}
	if got := spark(g, []int{1, 2, 3}, 2, 0, 0); got != "  " {
		t.Fatalf("a flat scale draws blanks: %q", got)
	}
	if got := spark(NewGlyphs(false), []int{100}, 3, 0, 100); got != "  %" {
		t.Fatalf("%q", got)
	}
}
