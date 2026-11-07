package tui

import (
	"sync"
	"time"
)

type job struct {
	key    string
	fn     func()
	onDrop func()
}

// Worker runs hardware writes one at a time, in the order they were made, off the screen's
// goroutine. A newer write with the same key replaces an older one that has not started
// (dragging a slider sends the last position, not every step); the replaced write's onDrop
// runs so its row can settle. The zero value is not usable: use NewWorker.
//
// A Worker that is sync runs every write on the spot, on the caller's goroutine: the tests
// use it so a key press and its write are one deterministic step.
type Worker struct {
	sync bool

	mu      sync.Mutex
	queue   []job
	running bool // a goroutine is draining the queue or a write is executing
	idleCh  chan struct{}
}

// NewWorker makes a Worker; sync selects the run-on-the-spot mode.
func NewWorker(sync bool) *Worker { return &Worker{sync: sync, idleCh: make(chan struct{})} }

// Submit queues fn. An empty key never coalesces.
func (w *Worker) Submit(key string, fn func(), onDrop func()) {
	if w.sync {
		w.runOne(fn)
		return
	}
	w.mu.Lock()
	var dropped []func()
	if key != "" {
		kept := w.queue[:0:0]
		for _, j := range w.queue {
			if j.key == key {
				if j.onDrop != nil {
					dropped = append(dropped, j.onDrop)
				}
				continue
			}
			kept = append(kept, j)
		}
		w.queue = kept
	}
	w.queue = append(w.queue, job{key, fn, onDrop})
	start := !w.running
	w.running = true
	w.mu.Unlock()
	for _, d := range dropped {
		d()
	}
	if start {
		go w.loop()
	}
}

// loop drains the queue and ends when it is empty, so an idle Worker holds no goroutine.
func (w *Worker) loop() {
	for {
		w.mu.Lock()
		if len(w.queue) == 0 {
			w.running = false
			close(w.idleCh)
			w.idleCh = make(chan struct{})
			w.mu.Unlock()
			return
		}
		j := w.queue[0]
		w.queue = w.queue[1:]
		w.mu.Unlock()
		w.runOne(j.fn)
	}
}

// runOne runs a write; fn reports its own errors and the queue must never die, so a panic
// is contained here.
func (w *Worker) runOne(fn func()) {
	defer func() { _ = recover() }()
	fn()
}

// Idle is true when nothing is queued or running.
func (w *Worker) Idle() bool {
	if w.sync {
		return true
	}
	w.mu.Lock()
	defer w.mu.Unlock()
	return !w.running
}

// Drain waits until every queued write is done (on quit, so a change made a moment ago is
// not lost) and reports whether the Worker is idle.
func (w *Worker) Drain(timeout time.Duration) bool {
	if w.sync {
		return true
	}
	deadline := time.NewTimer(timeout)
	defer deadline.Stop()
	for {
		w.mu.Lock()
		if !w.running {
			w.mu.Unlock()
			return true
		}
		ch := w.idleCh
		w.mu.Unlock()
		select {
		case <-ch:
		case <-deadline.C:
			return w.Idle()
		}
	}
}
