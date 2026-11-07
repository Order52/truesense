// Package autoprofile is the automatic power mode: it looks at what the laptop is doing (idle,
// everyday use, heavy work, gaming, too hot) and decides which power mode fits. It is a pure
// decision engine: every input (temperatures, load, the clock, the charger, the battery) is
// passed in, every output is a decision the caller carries out, so the whole behaviour can be
// tested at any speed without a laptop.
//
// Three kinds exist: "native" follows the power source only (what PredatorSense does on
// Windows), "smart" also watches heat, load and the battery, "off" does nothing.
package autoprofile

import (
	"fmt"
	"strings"
)

// Power modes in rank order (a higher rank is faster and louder).
var rank = map[string]int{"eco": 0, "quiet": 1, "balanced": 2, "performance": 3, "turbo": 4}

// Rank returns the speed rank of a mode (unknown modes rank as balanced).
func Rank(mode string) int {
	if r, ok := rank[mode]; ok {
		return r
	}
	return rank["balanced"]
}

// Activity is what the laptop is doing.
type Activity string

// The activities, from quiet to alarming.
const (
	Idle     Activity = "idle"
	Browsing Activity = "browsing"
	Heavy    Activity = "heavy"
	Gaming   Activity = "gaming"
	Hot      Activity = "hot"
)

// Readings are the measurements one decision is based on.
type Readings struct {
	TCPU, TGPU float64 // smoothed temperatures, degrees C
	CPULoad    float64 // percent
	GPUUtil    float64 // percent
	GPUPower   float64 // watts
}

// Classifier turns readings into an Activity. Once it says hot it keeps saying hot until both
// temperatures are 5 degrees below the line, so a reading on the threshold does not flap.
type Classifier struct {
	HotCPU, HotGPU float64
	hot            bool
}

// Classify decides the activity for one set of readings.
func (c *Classifier) Classify(r Readings) Activity {
	margin := 0.0
	if c.hot {
		margin = 5
	}
	c.hot = r.TCPU >= c.HotCPU-margin || r.TGPU >= c.HotGPU-margin
	switch {
	case c.hot:
		return Hot
	case r.GPUPower >= 35 || r.GPUUtil >= 40:
		return Gaming
	case r.CPULoad >= 55:
		return Heavy
	case r.CPULoad < 8 && r.GPUUtil < 10:
		return Idle
	}
	return Browsing
}

// Params are the Smart auto settings (a smart profile). Times are in real seconds.
type Params struct {
	ACIdle, ACBrowsing, ACBusy                            string
	ACTurbo                                               bool
	TurboAfterS                                           float64 // heavy load for this long before Turbo
	TurboMaxCPU, TurboMaxGPU                              float64 // no Turbo above these temperatures
	BackoffHoldS                                          float64 // after overheating, wait before Turbo again
	IdleAfterS                                            float64
	BatteryIdle, BatteryBrowsing, BatteryBusy, BatteryHot string
	BatteryLowPct                                         float64 // below this, force Eco
	UpDwellS, DownDwellS                                  float64
	BusyHoldS                                             float64 // keep the busy mode this long after the load drops
	MinChangeS                                            float64 // at least this long between automatic changes
	TurboMarginC                                          float64 // enter Turbo only this far below its limits
	TurboRetryS                                           float64 // after Turbo reaches its limit, wait before trying again
}

// Inputs is everything one decision looks at.
type Inputs struct {
	Kind       string  // off | native | smart
	Now        float64 // monotonic seconds (any origin)
	Wall       float64 // unix seconds (for the hold, which survives a restart)
	Activity   Activity
	Readings   Readings
	AC         bool
	BatteryPct float64
	CurMode    string
	NativeAC   string // mode "native" uses on power
	NativeBat  string // and on battery
	GameMode   string // mode asked for by a running game rule ("" = none)
	Disabled   bool   // another tool owns the mode (a legacy flag file)
}

// Decision is what the caller should do. Mode is empty when nothing is to be changed.
type Decision struct {
	Mode   string
	Urgent bool
	Reason string // why, in words, for the journal
	Notify bool   // worth an on-screen notice (at most one every 30 s, or when urgent)
}

// Engine holds the state between decisions.
type Engine struct {
	P Params

	busyKind     Activity
	busyStrong   float64
	tPrev        float64
	havePrev     bool
	heavyFor     float64
	idleFor      float64
	turboBlock   float64
	modeChanged  float64
	lastTry      float64
	lastSay      float64
	lastTarget   string
	latched      bool
	holdUntil    float64 // unix seconds; 0 = none
	holdAC       bool
	holdBucket   string
	holdHasCtx   bool
	lastActivity Activity
}

// New returns an Engine with the given settings and no history.
func New(p Params) *Engine {
	return &Engine{P: p, modeChanged: -1e9, lastTry: -1e9, lastSay: -1e9}
}

// HoldUntil is the unix time the user's own pick stays in force (0 = no hold). The caller
// persists it so it survives a restart.
func (e *Engine) HoldUntil() float64 { return e.holdUntil }

// RestoreHold sets a hold read back from persistent state (it has no situation to break on).
func (e *Engine) RestoreHold(until float64) { e.holdUntil, e.holdHasCtx = until, false }

// SetHold makes the user's own pick override the automatic mode for minutes (0 or less clears it).
// The hold ends early when the situation it was picked in changes (plug or unplug, load starts
// or stops, heat). It returns the minutes held.
func (e *Engine) SetHold(wall, minutes float64, ac bool, act Activity) float64 {
	if minutes <= 0 {
		e.holdUntil, e.holdHasCtx = 0, false
		return 0
	}
	e.holdUntil = wall + minutes*60
	e.holdAC, e.holdBucket, e.holdHasCtx = ac, bucket(act), true
	return minutes
}

// TakeOverNow forgets the hold and every timer: choosing auto means "take over now".
func (e *Engine) TakeOverNow() {
	e.holdUntil, e.holdHasCtx = 0, false
	e.modeChanged, e.lastTarget, e.heavyFor = -1e9, "", 0
}

// Resync forgets what was last tried (a plug or unplug, a resume from suspend can reset the
// driver state, so the next decision starts fresh).
func (e *Engine) Resync() { e.lastTarget = "" }

// Applied records that the mode was really changed at now.
func (e *Engine) Applied(now float64) { e.modeChanged = now }

// bucket is the coarse situation a hold was picked in: busy (gaming, heavy work, heat) or light.
func bucket(a Activity) string {
	if a == Gaming || a == Heavy || a == Hot {
		return "busy"
	}
	return "light"
}

// holdBroken reports (and drops the hold) once the situation the mode was picked in is over.
func (e *Engine) holdBroken(in Inputs) bool {
	if !e.holdHasCtx {
		return false
	}
	low := !in.AC && in.BatteryPct < e.P.BatteryLowPct
	if e.holdAC != in.AC || e.holdBucket != bucket(in.Activity) || in.Activity == Hot || low {
		e.holdUntil, e.holdHasCtx = 0, false
		e.modeChanged, e.lastTarget = -1e9, ""
		return true
	}
	return false
}

// busyLatched is for the power-mode decision only: "gaming" and "heavy" start at once but end
// only after BusyHoldS seconds without either, so a menu, a loading screen or a frame cap does
// not look like "browsing" and drop the mode. (The fans keep using the plain classification.)
func (e *Engine) busyLatched(a Activity, now, hold float64) Activity {
	if a == Gaming || a == Heavy {
		e.busyKind, e.busyStrong = a, now
		return a
	}
	if a != Hot && e.busyKind != "" && hold > 0 && now-e.busyStrong < hold {
		return e.busyKind
	}
	if a != Hot {
		e.busyKind = ""
	}
	return a
}

// smartTarget picks the mode from what the laptop is doing and how hot it is. urgent decisions
// skip the dwell timers (a thermal back-off or an empty battery cannot wait).
func (e *Engine) smartTarget(in Inputs) (mode string, urgent bool) {
	s, now := e.P, in.Now
	a := e.busyLatched(in.Activity, now, s.BusyHoldS)
	e.latched = a != in.Activity
	dt := 0.0
	if e.havePrev {
		dt = max(0, min(5, now-e.tPrev))
	}
	e.tPrev, e.havePrev = now, true
	heavy, hot := a == Gaming || a == Heavy, a == Hot
	if heavy {
		e.heavyFor += dt
	} else {
		e.heavyFor = max(0, e.heavyFor-dt)
	}
	if a == Idle {
		e.idleFor += dt
	} else {
		e.idleFor = 0
	}
	if hot {
		e.turboBlock = now + s.BackoffHoldS
	}
	r := in.Readings
	if in.AC {
		if heavy || hot {
			t := s.ACBusy
			if hot && in.CurMode == "turbo" {
				e.turboBlock = max(e.turboBlock, now+s.TurboRetryS)
				return "performance", true // thermal back-off, at once
			}
			if s.ACTurbo {
				switch {
				case in.CurMode == "turbo": // already in Turbo: stay until it really reaches the limit (no margin to LEAVE)
					if r.TCPU < s.TurboMaxCPU && r.TGPU < s.TurboMaxGPU {
						t = "turbo"
					} else {
						e.turboBlock = max(e.turboBlock, now+s.TurboRetryS)
						return "performance", true // too hot: step down and do not retry for a while
					}
				case (in.Activity == Gaming || in.Activity == Heavy) && e.heavyFor >= s.TurboAfterS &&
					r.TCPU < s.TurboMaxCPU-s.TurboMarginC && r.TGPU < s.TurboMaxGPU-s.TurboMarginC && now >= e.turboBlock:
					t = "turbo" // to ENTER it: well below the limit
				}
			}
			return t, false
		}
		if a == Browsing || e.idleFor < s.IdleAfterS {
			return s.ACBrowsing, false
		}
		return s.ACIdle, false
	}
	switch {
	case in.BatteryPct < s.BatteryLowPct:
		return "eco", true
	case hot:
		return s.BatteryHot, true
	case heavy:
		return s.BatteryBusy, false
	case a == Browsing:
		return s.BatteryBrowsing, false
	}
	return s.BatteryIdle, false
}

// Step makes one decision. The caller carries it out (sets the mode) and, when the mode really
// changed, calls Applied. Nothing is decided while the hold is in force.
func (e *Engine) Step(in Inputs) Decision {
	e.lastActivity = in.Activity
	if in.Kind == "off" || in.Kind == "" || in.Disabled || in.CurMode == "" {
		return Decision{}
	}
	if in.Wall < e.holdUntil && !e.holdBroken(in) {
		return Decision{} // your own pick is still in force
	}
	var target string
	var urgent bool
	if in.Kind == "native" {
		target, urgent = in.NativeBat, true // follows the plug at once
		if in.AC {
			target = in.NativeAC
		}
	} else {
		target, urgent = e.smartTarget(in)
	}
	if g := in.GameMode; g != "" && Rank(g) > Rank(target) { // a configured game is running: it can only raise the mode
		if in.Activity == Hot && g == "turbo" {
			g = "performance" // same thermal back-off as Smart auto
		}
		if !in.AC && in.BatteryPct < 20 {
			g = "" // a nearly empty battery wins
		}
		if g != "" {
			target = g
		}
	}
	reason := e.reason(in)
	if target == in.CurMode {
		e.lastTarget = target
		return Decision{Reason: reason}
	}
	now := in.Now
	if target == e.lastTarget && now-e.lastTry < 30 {
		return Decision{Reason: reason} // it already failed recently
	}
	dwell := e.P.DownDwellS
	if Rank(target) > Rank(in.CurMode) {
		dwell = e.P.UpDwellS
	}
	if !urgent && (now-e.modeChanged < dwell || (in.Kind == "smart" && now-e.modeChanged < e.P.MinChangeS)) {
		return Decision{Reason: reason} // no flapping
	}
	e.lastTarget, e.lastTry = target, now
	d := Decision{Mode: target, Urgent: urgent, Reason: reason}
	if now-e.lastSay >= 30 || urgent { // one notification at most every 30 s (the journal has every change)
		e.lastSay, d.Notify = now, true
	}
	return d
}

func (e *Engine) reason(in Inputs) string {
	var b strings.Builder
	power := "on battery"
	if in.AC {
		power = "on power"
	}
	fmt.Fprintf(&b, "%s, %s", in.Activity, power)
	if in.Kind == "smart" && e.heavyFor > 0 {
		fmt.Fprintf(&b, ", %d s heavy", int(e.heavyFor))
	}
	if in.Kind == "smart" && e.latched {
		b.WriteString(" (holding the busy mode through a quiet moment)")
	}
	return b.String()
}

// HeavyFor is how long the load has been heavy, in seconds.
func (e *Engine) HeavyFor() float64 { return e.heavyFor }
