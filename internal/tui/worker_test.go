package tui

import (
	"reflect"
	"sync"
	"testing"
	"time"
)

func TestWorkerRunsWritesInOrderAndANewerOneReplacesAnOlderOneThatHasNotStarted(t *testing.T) {
	w := NewWorker(false)
	var mu sync.Mutex
	var ran, dropped []string
	add := func(dst *[]string, s string) { mu.Lock(); *dst = append(*dst, s); mu.Unlock() }
	gate, started := make(chan struct{}), make(chan struct{})
	w.Submit("a", func() { close(started); <-gate; add(&ran, "first") }, nil)
	<-started // "first" is running: the rest queue up behind it
	w.Submit("k", func() { add(&ran, "k1") }, func() { add(&dropped, "k1") })
	w.Submit("k", func() { add(&ran, "k2") }, func() { add(&dropped, "k2") })
	w.Submit("other", func() { add(&ran, "other") }, nil)
	close(gate)
	if !w.Drain(3 * time.Second) {
		t.Fatal("worker did not drain")
	}
	if want := []string{"first", "k2", "other"}; !reflect.DeepEqual(ran, want) {
		t.Fatalf("ran %v, want %v", ran, want)
	}
	if want := []string{"k1"}; !reflect.DeepEqual(dropped, want) {
		t.Fatalf("dropped %v, want %v", dropped, want)
	}
}

func TestWorkerNeverCoalescesWritesWithoutAKey(t *testing.T) {
	w := NewWorker(false)
	var ran []int
	gate, started := make(chan struct{}), make(chan struct{})
	w.Submit("", func() { close(started); <-gate }, nil)
	<-started
	for i := range 3 {
		w.Submit("", func() { ran = append(ran, i) }, nil)
	}
	close(gate)
	w.Drain(3 * time.Second)
	if !reflect.DeepEqual(ran, []int{0, 1, 2}) {
		t.Fatalf("ran %v", ran)
	}
}

func TestAFailingWriteDoesNotStopTheQueue(t *testing.T) {
	w := NewWorker(false)
	ran := false
	w.Submit("", func() { panic("a write that crashes") }, nil)
	w.Submit("", func() { ran = true }, nil)
	if !w.Drain(3*time.Second) || !ran {
		t.Fatal("the write after the failing one did not run")
	}
}

func TestWorkerWritesNeverOverlap(t *testing.T) {
	w := NewWorker(false)
	var mu sync.Mutex
	active, peak := 0, 0
	for i := range 20 {
		w.Submit(string(rune('a'+i)), func() {
			mu.Lock()
			active++
			peak = max(peak, active)
			mu.Unlock()
			time.Sleep(time.Millisecond)
			mu.Lock()
			active--
			mu.Unlock()
		}, nil)
	}
	w.Drain(3 * time.Second)
	if peak != 1 {
		t.Fatalf("%d writes ran at once", peak)
	}
}

func TestWorkerDrainGivesUpAtTheTimeoutAndSyncModeRunsOnTheSpot(t *testing.T) {
	w := NewWorker(false)
	gate := make(chan struct{})
	w.Submit("", func() { <-gate }, nil)
	if w.Drain(20 * time.Millisecond) {
		t.Fatal("drain reported idle with a write running")
	}
	if w.Idle() {
		t.Fatal("idle while busy")
	}
	close(gate)
	if !w.Drain(3*time.Second) || !w.Idle() {
		t.Fatal("not idle after the write")
	}

	s := NewWorker(true)
	ran := false
	s.Submit("k", func() { ran = true }, nil)
	if !ran || !s.Idle() {
		t.Fatal("a sync worker runs the write on the spot")
	}
}

func TestWorkerRestartsAfterGoingIdle(t *testing.T) {
	w := NewWorker(false)
	n := 0
	for range 3 {
		w.Submit("", func() { n++ }, nil)
		w.Drain(3 * time.Second)
	}
	if n != 3 {
		t.Fatalf("ran %d", n)
	}
}
