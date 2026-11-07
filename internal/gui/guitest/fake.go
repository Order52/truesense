package guitest

import (
	"context"
	_ "embed"
	"encoding/json"
	"fmt"
	"math"
	"sync"
	"time"

	"github.com/Order52/truesense/internal/gui"
)

//go:embed layout.json
var layoutJSON []byte

// Call is one command the window sent, as recorded by Laptop.Calls.
type Call struct {
	Area, Verb string
	Args       map[string]any
}

// Laptop is the simulated machine. It is safe for concurrent use (the server calls it from
// many goroutines).
type Laptop struct {
	mu sync.Mutex

	// Tick is the interval of the live stream (one second like the real thing by default).
	Tick time.Duration
	// NoDaemon makes every command that needs the daemon fail like on a machine where it is stopped.
	NoDaemon bool

	t0    time.Time
	calls []Call
	fail  map[string]string

	layout map[string][]int

	mode, autoMode, smartProfile, curveName  string
	holdUntil                                time.Time
	smartFan, operating                      bool
	fanMode                                  string
	dutyCPU, dutyGPU                         int
	gpuMode                                  string
	ac, limit80, calibration                 bool
	batteryPct                               int
	usb                                      int
	fullActive                               bool
	overdrive, bootSound, kbTimeout          bool
	touchpad                                 bool
	kbBrightness                             int
	kbLook                                   map[string]any
	cells                                    []string
	fx                                       map[string]any
	rear                                     map[string]any
	looks                                    *library
	keySets                                  map[string][]string
	bootLook                                 map[string]any
	designs                                  map[string]map[string]any
	startup                                  map[string]any
	autostart                                bool
	gameRules                                map[string]string
	managersOn                               bool
	curves, smart, keys, cpuUV, gpuUV        *library
	keysCurrent                              map[string]any
	keysProfileName                          string
	cpuApplied, gpuApplied, cpuBoot, gpuBoot map[string]any
	pl                                       map[string]any
	profiles                                 map[string]map[string]any
	profileUser                              map[string]bool
	lastProfile                              string
	audio                                    audioState
	updateAuto                               bool
	updateAvailable                          bool
}

// New returns a laptop in the state the window's screenshots show: Balanced, Smart auto,
// firmware-friendly fans, the stock lighting.
func New() *Laptop {
	l := &Laptop{
		Tick: time.Second, t0: time.Now(), fail: map[string]string{},
		mode: "balanced", autoMode: "smart", smartProfile: "truesense", curveName: "truesense", smartFan: true,
		fanMode: "auto", dutyCPU: 40, dutyGPU: 40, gpuMode: "nvidia", ac: true, limit80: true, usb: 30, batteryPct: 87,
		overdrive: true, kbTimeout: true, touchpad: true, kbBrightness: 25, keySets: map[string][]string{},
		gameRules: map[string]string{"hl2_linux": "performance"}, managersOn: true, updateAuto: true,
		startup:  map[string]any{"profile": "", "keyboard": "last", "rear": "last"},
		pl:       map[string]any{"user": nil},
		rear:     map[string]any{"mode": "breathing", "speed": 3, "brightness": 100, "direction": 1, "color": "3723c7", "zones": nil},
		profiles: map[string]map[string]any{}, profileUser: map[string]bool{},
		keysProfileName: "default",
	}
	var raw struct {
		Keys map[string][]int `json:"keys"`
	}
	if err := json.Unmarshal(layoutJSON, &raw); err != nil {
		panic(fmt.Sprintf("guitest: layout.json: %v", err))
	}
	l.layout = raw.Keys
	l.cells = stockCells()
	l.kbLook = map[string]any{"mode": "effect", "name": "wave", "color": "00aec7", "speed": 5, "brightness": 25}
	l.initLibraries()
	l.initAudio()
	l.designs = builtinDesigns(l.layout)
	return l
}

func stockCells() []string {
	c := make([]string, 128)
	for i := range c {
		c[i] = "00aec7"
		if i >= 126 {
			c[i] = "000000"
		}
	}
	return c
}

// Calls returns every command received so far, oldest first.
func (l *Laptop) Calls() []Call {
	l.mu.Lock()
	defer l.mu.Unlock()
	return append([]Call(nil), l.calls...)
}

// FailNext makes the next "area/verb" fail with message (once).
func (l *Laptop) FailNext(area, verb, message string) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.fail[area+"/"+verb] = message
}

// Do implements gui.Commands.
func (l *Laptop) Do(ctx context.Context, area, verb string, raw json.RawMessage) (any, error) {
	var a args
	if len(raw) > 0 {
		if err := json.Unmarshal(raw, &a); err != nil {
			return nil, fmt.Errorf("bad arguments: %w", err)
		}
	}
	if a == nil {
		a = args{}
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	l.calls = append(l.calls, Call{Area: area, Verb: verb, Args: a})
	if msg, ok := l.fail[area+"/"+verb]; ok {
		delete(l.fail, area+"/"+verb)
		return nil, fmt.Errorf("%s", msg)
	}
	h := l.handler(area, verb)
	if h == nil {
		return nil, fmt.Errorf("%w: %s %s", gui.ErrUnknownCommand, area, verb)
	}
	return h(a)
}

type handlerFunc func(a args) (any, error)

var errNoDaemon = fmt.Errorf("the TrueSense daemon is not running (sudo systemctl start truesense)")

func (l *Laptop) needsDaemon(h handlerFunc) handlerFunc {
	return func(a args) (any, error) {
		if l.NoDaemon {
			return nil, errNoDaemon
		}
		return h(a)
	}
}

func (l *Laptop) handler(area, verb string) handlerFunc {
	d := l.needsDaemon
	switch area + "/" + verb {
	case "status/get":
		return l.statusGet
	case "version/get":
		return func(args) (any, error) { return map[string]any{"truesense": "0.4.0-sim"}, nil }
	case "power/status":
		return l.powerStatus
	case "power/set":
		return l.powerSet
	case "power/auto":
		return d(l.powerAuto)
	case "power/smart":
		return d(l.powerSmart)
	case "power/release":
		return d(func(args) (any, error) {
			l.holdUntil = time.Time{}
			return "hold released: the automatic profile is in charge again", nil
		})
	case "power/hold":
		return d(l.powerHold)
	case "power/game":
		return d(l.powerGame)
	case "power/managers":
		return d(l.powerManagers)
	case "fan/auto", "fan/max", "fan/smart", "fan/set", "fan/off":
		return l.fan(verb)
	case "fan/curve":
		return l.fanCurve
	case "gpu/set":
		return l.gpuSet
	case "battery/limit":
		return l.boolVerb(&l.limit80)
	case "battery/calibration":
		return l.boolVerb(&l.calibration)
	case "battery/usb-charge":
		return l.usbCharge
	case "battery/full":
		return d(l.batteryFull)
	case "battery/status":
		return func(args) (any, error) {
			return map[string]any{"limit_80": l.limit80, "calibration": l.calibration, "usb_charge_stop": l.usb}, nil
		}
	case "display/overdrive":
		return l.boolVerb(&l.overdrive)
	case "boot/sound":
		return l.boolVerb(&l.bootSound)
	case "touchpad/get":
		return func(args) (any, error) { return l.touchpad, nil }
	case "touchpad/set":
		return l.boolVerb(&l.touchpad)
	case "gui/autostart":
		return l.boolVerb(&l.autostart)
	case "cpu/info":
		return d(l.cpuInfo)
	case "cpu/limits":
		return d(l.cpuLimits)
	case "cpu/undervolt":
		return d(l.uvVerb("cpu"))
	case "gpu/undervolt":
		return d(l.uvVerb("gpu"))
	case "keyboard/status", "keyboard/on", "keyboard/off", "keyboard/brightness", "keyboard/effect", "keyboard/color", "keyboard/speed", "keyboard/operating",
		"keyboard/restore", "keyboard/layout", "keyboard/cells", "keyboard/set-cells", "keyboard/fx", "keyboard/design", "keyboard/profile", "keyboard/timeout":
		return l.keyboard(verb)
	case "rear/status", "rear/on", "rear/off", "rear/set":
		return l.rearVerb(verb)
	case "look/list", "look/show", "look/save", "look/apply", "look/delete", "look/copy":
		return l.lookVerb(verb)
	case "startup/show", "startup/set", "startup/apply", "startup/boot":
		return l.startupVerb(verb)
	case "keys/profile":
		return l.keysProfile
	case "keys/test":
		return func(a args) (any, error) { return "ran " + a.str("action"), nil }
	case "profile/list", "profile/show", "profile/apply", "profile/next", "profile/save", "profile/delete", "profile/current":
		return l.profileVerb(verb)
	case "update/check":
		return l.updateCheck
	case "update/apply":
		return func(args) (any, error) {
			return map[string]any{"version": "0.4.1", "returncode": 0, "sha256": "ab", "verified": true}, nil
		}
	case "update/auto":
		return l.boolVerb(&l.updateAuto)
	case "doctor/run":
		return func(args) (any, error) { return l.doctor(), nil }
	case "service/check":
		return func(args) (any, error) { return l.services(), nil }
	case "service/logs":
		return func(args) (any, error) {
			return map[string]any{"text": "Nov 07 10:00:01 truesense[812]: daemon started\nNov 07 10:00:02 truesense[812]: smart fan on\n"}, nil
		}
	case "service/repair":
		return func(args) (any, error) {
			return []any{map[string]any{"step": "your user services are running", "ok": true}, map[string]any{"step": "the daemon answers", "ok": true}}, nil
		}
	case "selftest/run":
		return func(args) (any, error) {
			return map[string]any{"ok": true, "output": "== firmware features (set -> read back -> restore)\n  [ok] power modes (all supported)\n  [ok] sensors\n  [skip] fans (--skip-fans)\n"}, nil
		}
	case "config/show":
		return func(args) (any, error) {
			return map[string]any{"user": "sloppy", "daemon": map[string]any{"smart_fan": true, "auto_profile": true}, "update": map[string]any{"auto_check": l.updateAuto}}, nil
		}
	case "config/validate":
		return func(args) (any, error) { return []any{}, nil }
	case "config/reset-all":
		return l.resetAll
	}
	if area == "audio" {
		return l.audioVerb(verb)
	}
	return nil
}

// boolVerb is the on / off / toggle / status pattern of every boolean setting.
func (l *Laptop) boolVerb(p *bool) handlerFunc {
	return func(a args) (any, error) {
		switch a.str("action") {
		case "on":
			*p = true
		case "off":
			*p = false
		case "toggle":
			*p = !*p
		case "", "status":
		default:
			return nil, fmt.Errorf("action: on | off | toggle | status")
		}
		return *p, nil
	}
}

// args is the decoded JSON object of a command.
type args map[string]any

func (a args) str(k string) string {
	s, _ := a[k].(string)
	return s
}

func (a args) num(k string, def float64) float64 {
	if f, ok := a[k].(float64); ok {
		return f
	}
	return def
}

func (a args) has(k string) bool { _, ok := a[k]; return ok }

func (a args) flag(k string) bool { b, _ := a[k].(bool); return b }

func (a args) obj(k string) map[string]any { m, _ := a[k].(map[string]any); return m }

func (a args) list(k string) []any { s, _ := a[k].([]any); return s }

func (a args) strings(k string) []string {
	var out []string
	for _, v := range a.list(k) {
		if s, ok := v.(string); ok {
			out = append(out, s)
		}
	}
	return out
}

// ---- live state --------------------------------------------------------------------------------------------------------------------

// sensors returns plausible readings that move with the power mode and the time.
func (l *Laptop) sensors(now time.Time) (cpu, gpu, sys, rpmC, rpmG int) {
	t := now.Sub(l.t0).Seconds()
	base := map[string]float64{"eco": 44, "quiet": 48, "balanced": 54, "performance": 63, "turbo": 72}[l.mode]
	cpu = int(math.Round(base + 4*math.Sin(t/7) + 2*math.Sin(t/2.3)))
	gpu = int(math.Round(base - 5 + 3*math.Sin(t/9+1)))
	sys = int(math.Round(base - 8))
	duty := float64(l.dutyCPU)
	if l.fanMode == "max" {
		duty = 100
	}
	rpmC = int(math.Round(duty*52 + 40*math.Sin(t)))
	rpmG = int(math.Round(duty*50 + 40*math.Sin(t+2)))
	if duty < 25 && l.fanMode == "auto" && cpu < 55 {
		rpmC, rpmG = 0, 0
	}
	return
}

func (l *Laptop) daemonState(now time.Time) any {
	if l.NoDaemon {
		return nil
	}
	hold := 0
	if l.holdUntil.After(now) {
		hold = int(l.holdUntil.Sub(now).Seconds())
	}
	return map[string]any{
		"live": map[string]any{
			"smart_profile": l.smartProfile, "fan_curve": l.curveName, "power_mode": l.mode, "ac": l.ac, "auto_mode": l.autoMode,
			"mode_reason": "browsing, on power", "fan_off": false, "hw_ok": true,
		},
		"settings": map[string]any{
			"auto_mode": l.autoMode, "auto_profile": l.autoMode != "off", "smart_fan": l.smartFan, "operating_follow": l.operating,
		},
		"hold_left_s": hold,
	}
}

func (l *Laptop) fansState() map[string]any {
	return map[string]any{"mode": l.fanMode, "duty_register_pct": map[string]any{"cpu": l.dutyCPU, "gpu": l.dutyGPU}}
}

// Snapshot is one message of the live stream.
func (l *Laptop) Snapshot() map[string]any {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.snapshot(time.Now())
}

func (l *Laptop) snapshot(now time.Time) map[string]any {
	cpu, gpu, sys, rc, rg := l.sensors(now)
	t := now.Sub(l.t0).Seconds()
	busy := map[string]float64{"eco": 6, "quiet": 9, "balanced": 14, "performance": 38, "turbo": 62}[l.mode]
	return map[string]any{
		"t":          now.Unix(),
		"sensors":    map[string]any{"cpu_temp": cpu, "gpu_temp": gpu, "sys_temp": sys, "cpu_fan_rpm": rc, "gpu_fan_rpm": rg},
		"power_mode": l.mode, "ac": l.ac, "battery_pct": l.batteryPct,
		"fans":   l.fansState(),
		"load":   map[string]any{"cpu_pct": int(busy + 8*math.Sin(t/3) + 8), "gpu_pct": int(busy/3 + 3*math.Sin(t/5) + 3)},
		"power":  map[string]any{"cpu_w": math.Round((12+busy*0.7+3*math.Sin(t/2))*10) / 10, "gpu_w": math.Round((5+busy*0.2)*10) / 10},
		"daemon": l.daemonState(now),
	}
}

// Stream implements gui.Commands.
func (l *Laptop) Stream(ctx context.Context, emit func(json.RawMessage)) {
	tick := l.Tick
	if tick <= 0 {
		tick = time.Second
	}
	t := time.NewTicker(tick)
	defer t.Stop()
	for {
		l.mu.Lock()
		b, _ := json.Marshal(l.snapshot(time.Now()))
		l.mu.Unlock()
		emit(b)
		select {
		case <-ctx.Done():
			return
		case <-t.C:
		}
	}
}

var _ gui.Commands = (*Laptop)(nil)
