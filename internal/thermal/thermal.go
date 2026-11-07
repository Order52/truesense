// Package thermal is the CPU clock governor: when the CPU gets hot it lowers the maximum clock
// a step at a time, and gives the clock back when it cools, so the fans and the firmware do
// not have to fight a sustained load. The decision (Governor) is separate from the writing of
// the cpufreq files (Writer).
package thermal

import (
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

// Params are the governor's thresholds (degrees C and kHz).
type Params struct {
	Enabled bool
	Hi, Lo  int // cap down at or above Hi, up at or below Lo
	MaxKHz  int // no cap
	MinKHz  int // never lower than this
	DownKHz int // step down
	UpKHz   int // step up
}

// Governor remembers the current cap and when it last changed (in ticks of the daemon's loop).
type Governor struct {
	P           Params
	Cap         int
	changedTick int
}

// New returns a Governor with no cap applied.
func New(p Params) *Governor { return &Governor{P: p, Cap: p.MaxKHz} }

// Step decides the cap for this tick. active says whether TrueSense is managing the CPU at
// all (a smart feature is on); when it is not, the full clock is handed back. It returns
// whether the cpufreq files must be (re)written.
func (g *Governor) Step(tick int, tcpu float64, active bool) (apply bool) {
	p := g.P
	if !p.Enabled || !active {
		if g.Cap < p.MaxKHz {
			g.Cap = p.MaxKHz
			return true
		}
		return false
	}
	next := g.Cap
	switch {
	case tcpu >= float64(p.Hi) && tick-g.changedTick >= 2:
		next = max(p.MinKHz, g.Cap-p.DownKHz)
	case tcpu <= float64(p.Lo) && g.Cap < p.MaxKHz && tick-g.changedTick >= 3:
		next = min(p.MaxKHz, g.Cap+p.UpKHz)
	}
	if next != g.Cap {
		g.Cap, g.changedTick = next, tick
		return true
	}
	// Something else (a power manager, a resume) may have raised the limit behind our back:
	// while capped, re-assert it every 30 ticks.
	return g.Cap < p.MaxKHz && tick%30 == 0
}

// Resync tells the governor the situation changed (plug or unplug, resume): the next change
// is allowed only after the minimum interval.
func (g *Governor) Resync(tick int) { g.changedTick = tick }

// Release forgets the cap (the full clock is handed back by the caller).
func (g *Governor) Release() { g.Cap = g.P.MaxKHz }

// Writer sets the maximum clock of every CPU through cpufreq.
type Writer struct {
	Root string // prefix of /sys (empty = the real one)
}

func (w Writer) policies() []string {
	m, _ := filepath.Glob(filepath.Join(w.Root, "/sys/devices/system/cpu/cpu[0-9]*/cpufreq/scaling_max_freq"))
	return m
}

// Apply writes khz to every CPU whose limit differs.
func (w Writer) Apply(khz int) {
	want := strconv.Itoa(khz)
	for _, p := range w.policies() {
		if b, err := os.ReadFile(p); err == nil && strings.TrimSpace(string(b)) == want {
			continue
		}
		_ = os.WriteFile(p, []byte(want), 0o644)
	}
}

// Uncap gives every CPU its own hardware maximum back.
func (w Writer) Uncap() {
	for _, p := range w.policies() {
		if b, err := os.ReadFile(filepath.Join(filepath.Dir(p), "cpuinfo_max_freq")); err == nil {
			_ = os.WriteFile(p, []byte(strings.TrimSpace(string(b))), 0o644) //nolint:gosec // p comes from a glob of the cpufreq tree, not from input
		}
	}
}
