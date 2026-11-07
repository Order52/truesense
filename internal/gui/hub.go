package gui

import (
	"bytes"
	"context"
	"encoding/json"
	"log/slog"
	"sync"
	"time"
)

// hub runs the Commands stream while at least one page listens and fans each state out to
// every listener. A slow listener only ever misses old states: the newest one wins.
type hub struct {
	cmds Commands
	log  *slog.Logger

	mu     sync.Mutex
	subs   map[chan json.RawMessage]struct{}
	cancel context.CancelFunc
	last   json.RawMessage

	// restartDelay is the pause before an early-returning Stream is called again.
	restartDelay time.Duration
}

func newHub(cmds Commands, log *slog.Logger) *hub {
	return &hub{cmds: cmds, log: log, subs: map[chan json.RawMessage]struct{}{}, restartDelay: time.Second}
}

// subscribe registers a listener (starting the stream with the first one) and hands it the
// latest state at once, so a new page does not wait a second for its first values.
func (h *hub) subscribe() (<-chan json.RawMessage, func()) {
	ch := make(chan json.RawMessage, 1)
	h.mu.Lock()
	h.subs[ch] = struct{}{}
	if h.last != nil {
		ch <- h.last
	}
	if h.cancel == nil {
		ctx, cancel := context.WithCancel(context.Background())
		h.cancel = cancel
		go h.run(ctx)
	}
	h.mu.Unlock()
	return ch, func() { h.unsubscribe(ch) }
}

func (h *hub) unsubscribe(ch chan json.RawMessage) {
	h.mu.Lock()
	defer h.mu.Unlock()
	delete(h.subs, ch)
	if len(h.subs) == 0 && h.cancel != nil {
		h.cancel()
		h.cancel, h.last = nil, nil
	}
}

// stop ends the stream whatever the listeners do (server shutdown).
func (h *hub) stop() {
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.cancel != nil {
		h.cancel()
		h.cancel, h.last = nil, nil
	}
}

// run keeps Stream alive: an implementation that returns early (a failed read) is called
// again after a second, so the page keeps getting values once the problem is gone.
func (h *hub) run(ctx context.Context) {
	for ctx.Err() == nil {
		h.cmds.Stream(ctx, func(raw json.RawMessage) { h.publish(ctx, raw) })
		select {
		case <-ctx.Done():
		case <-time.After(h.restartDelay):
		}
	}
}

func (h *hub) publish(ctx context.Context, raw json.RawMessage) {
	var buf bytes.Buffer
	if json.Compact(&buf, raw) != nil {
		h.log.Warn("stream: dropped a state that is not valid JSON")
		return
	}
	msg := json.RawMessage(buf.Bytes())
	h.mu.Lock()
	defer h.mu.Unlock()
	if ctx.Err() != nil {
		return // a stale emit from a stream that was already stopped
	}
	h.last = msg
	for ch := range h.subs {
		select {
		case ch <- msg:
		default:
			select {
			case <-ch:
			default:
			}
			ch <- msg
		}
	}
}
