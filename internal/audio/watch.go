package audio

import (
	"context"
	"maps"
	"slices"
	"sort"
	"strings"
	"sync"
	"time"
)

// signature is everything the microphone policy depends on. The watcher re-applies the policy
// only when it CHANGES, so it never fights a manual choice made in between (e.g. Shift+F5).
type signature struct {
	jackUse string
	nodes   string // the Purified Voice nodes that exist, sorted
	modes   string // each input's configured mode, sorted
}

func (c *Controller) signature(ctx context.Context) signature {
	names := c.sourceNames(ctx)
	var nodes []string
	for _, d := range voiceDevices {
		if names[d.node()] {
			nodes = append(nodes, d.node())
		}
	}
	sort.Strings(nodes)
	modes := c.VoiceModes()
	keys := slices.Sorted(maps.Keys(modes))
	pairs := make([]string, len(keys))
	for i, k := range keys {
		pairs[i] = k + "=" + modes[k]
	}
	return signature{jackUse: c.JackUse(ctx, nil), nodes: strings.Join(nodes, ","), modes: strings.Join(pairs, ",")}
}

// Pauses of the watch loops (Python's sleeps): PipeWire needs a moment to finish creating or
// removing nodes, AcerUltraX to register a new output.
const (
	watchTick     = time.Second
	watchSettle   = 800 * time.Millisecond
	watchDTSDelay = 1500 * time.Millisecond
)

// MicWatcher re-picks the default microphone whenever something relevant changes: the headset
// is plugged or unplugged, a filter appears or vanishes, or a mode changes. It also gives every
// new output (a USB headset, a controller) DTS once.
type MicWatcher struct {
	c    *Controller
	last *signature
	tick int
	dts  DTSWatcher
}

// NewMicWatcher returns a watcher in its initial state.
func (c *Controller) NewMicWatcher() *MicWatcher {
	return &MicWatcher{c: c, dts: DTSWatcher{c: c}}
}

// Step is one pass of the loop; the caller waits Tick between passes. A pass that fails only
// logs: the loop must outlive every tool hiccup.
func (w *MicWatcher) Step(ctx context.Context) {
	c := w.c
	sig := c.signature(ctx)
	if w.last == nil || sig != *w.last {
		c.sleep(ctx, watchSettle) // let PipeWire finish creating/removing nodes
		notify := c.Cfg().Audio.Notify && (w.last == nil || sig.jackUse != w.last.jackUse)
		if p, err := c.ApplyMicPolicy(ctx, notify); err != nil {
			c.log().Warn("mic policy failed", "error", err)
		} else {
			c.log().Info("microphone policy applied", "device", p.Device, "mode", p.Mode, "default_source", p.DefaultSource)
		}
		s := sig
		w.last = &s
	}
	w.tick = (w.tick + 1) % 2
	if w.tick == 0 {
		w.dts.Step(ctx)
	}
}

// Run is the watch loop: it covers the DTS outputs once, starts the login sound repair, and
// then watches until ctx is done. It returns only after everything it started has stopped.
func (w *MicWatcher) Run(ctx context.Context) {
	c := w.c
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		c.AutoRepair(ctx, DefaultRepairDelays)
	}()
	defer wg.Wait()
	if c.HasDTS() {
		_ = w.dts.cover(ctx) // logged inside; the watch goes on without it
	}
	for ctx.Err() == nil {
		w.Step(ctx)
		c.sleep(ctx, watchTick)
	}
}

// DTSWatcher gives DTS to an output that appears after the first look (a USB headset plugged
// in, a controller connected).
type DTSWatcher struct {
	c     *Controller
	sinks map[string]bool // nil until the first look, which is only a baseline
}

func (w *DTSWatcher) cover(ctx context.Context) error {
	lines, err := w.c.DTSCover(ctx, false)
	if err != nil {
		w.c.log().Warn("DTS cover failed", "error", err)
		return err
	}
	for _, l := range lines {
		w.c.log().Info("DTS: " + l)
	}
	return nil
}

// Step looks at the outputs once and covers the new ones.
func (w *DTSWatcher) Step(ctx context.Context) {
	now := map[string]bool{}
	for _, s := range w.c.sinks(ctx) {
		now[s] = true
	}
	if w.sinks != nil && w.c.HasDTS() {
		fresh := false
		for s := range now {
			fresh = fresh || !w.sinks[s]
		}
		if fresh {
			w.c.sleep(ctx, watchDTSDelay) // let AcerUltraX register the new output first
			if err := w.cover(ctx); err != nil {
				w.sinks = map[string]bool{} // every output looks new next time: the cover is retried
				return
			}
		}
	}
	w.sinks = now
}
