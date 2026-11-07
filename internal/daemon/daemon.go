package daemon

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"github.com/Order52/truesense/internal/autoprofile"
	"github.com/Order52/truesense/internal/curves"
	"github.com/Order52/truesense/internal/fanctl"
	"github.com/Order52/truesense/internal/ipc"
	"github.com/Order52/truesense/internal/power"
	"github.com/Order52/truesense/internal/sdnotify"
	"github.com/Order52/truesense/internal/sensors"
	"github.com/Order52/truesense/internal/smartprofile"
	"github.com/Order52/truesense/internal/thermal"
)

// Daemon owns the control loop and the settings only root may change or that must outlive a login.
type Daemon struct {
	d   Deps
	log *slog.Logger

	mu sync.Mutex // the control loop and every request handler take it: one thing changes the hardware at a time

	// readings
	tick     int
	tcpu     sensors.EMA
	tgpu     sensors.EMA
	tchassis sensors.EMA
	rpm      [2]int
	cls      autoprofile.Classifier
	activity autoprofile.Activity
	hwOK     bool
	lastAC   *bool
	curMode  string
	reason   string

	// decisions
	engine *autoprofile.Engine
	fan    *fanctl.Smart
	writer *fanctl.Writer
	gov    *thermal.Governor
	fanOff bool // a fan was stopped by the user: the watchdog gives it back when it gets warm

	// power limit
	plOrig    *power.RawLimits
	plTouched bool

	// boot guards, resume detection
	lastWall      time.Time
	lastMono      float64
	opMode        string
	game          string
	gameTick      int
	lastTickError time.Time

	live     map[string]any
	interval time.Duration
	gpuCtx   context.Context
}

// New builds the daemon from its dependencies.
func New(d Deps) *Daemon {
	if d.Log == nil {
		d.Log = slog.Default()
	}
	if d.Clock.Wall == nil {
		d.Clock = RealClock()
	}
	if d.LegacyFlag == nil {
		d.LegacyFlag = func(string) bool { return false }
	}
	c := d.Config
	dm := &Daemon{d: d, log: d.Log, hwOK: true, live: map[string]any{}, interval: time.Second}
	dm.tcpu, dm.tgpu, dm.tchassis = sensors.EMA{Alpha: 0.4}, sensors.EMA{Alpha: 0.4}, sensors.EMA{Alpha: 0.2}
	dm.cls = autoprofile.Classifier{HotCPU: float64(c.Fan.HotCPU), HotGPU: float64(c.Fan.HotGPU)}
	dm.activity = autoprofile.Idle
	dm.engine = autoprofile.New(dm.smartParams())
	dm.engine.RestoreHold(dm.d.State.Float("profile_hold_until", 0))
	dm.fan = fanctl.NewSmart(fanctl.Params{CalmCPU: float64(c.Fan.CalmCPU), CalmGPU: float64(c.Fan.CalmGPU), CalmHold: c.Fan.CalmHold}, dm.loadCurve())
	dm.writer = &fanctl.Writer{HW: fanAdapter{d.HW}, Log: d.Log}
	dm.gov = thermal.New(thermal.Params{Enabled: c.CPU.GovEnabled, Hi: c.CPU.GovHi, Lo: c.CPU.GovLo, MaxKHz: c.CPU.CapMaxKHz, MinKHz: c.CPU.CapMinKHz, DownKHz: c.CPU.CapDownKHz, UpKHz: c.CPU.CapUpKHz})
	return dm
}

// fanAdapter gives fanctl the one extra question it asks: is the firmware in charge of the fans?
type fanAdapter struct{ Hardware }

func (a fanAdapter) FanIsAuto() (bool, error) {
	s, err := a.GetFanState()
	return s.Mode == "auto", err
}

// ---- settings helpers ----

// AutoKind is the automatic power mode: off | native | smart.
func (dm *Daemon) autoKind() string {
	switch k := dm.d.State.String("auto_mode", ""); k {
	case "off", "native", "smart":
		return k
	}
	if dm.d.State.Bool("auto_profile", false) {
		return "smart"
	}
	return "off"
}

// smartActive is true when TrueSense manages the machine itself (a smart feature was chosen).
// Otherwise the firmware is left alone, like PredatorSense on Windows.
func (dm *Daemon) smartActive() bool {
	v, ok := dm.d.State.Get("pl_user")
	user := ok && v != nil
	return dm.d.State.Bool("smart_fan", false) || dm.autoKind() == "smart" || user
}

func asJSON(v any) ([]byte, error) { return json.Marshal(v) }

// loadCurve returns the active curve: the one the daemon keeps as data (so it works before
// login), else the named one, falling back to the built-in "truesense" on any problem.
func (dm *Daemon) loadCurve() curves.Curve {
	name := dm.d.State.String("fan_curve", "truesense")
	if name == "native" {
		name = "truesense"
	}
	if raw, ok := dm.d.State.Get("fan_curve_data"); ok && raw != nil {
		if b, err := asJSON(raw); err == nil {
			if c, err := curves.Parse(b); err == nil {
				return c
			}
			dm.log.Warn("fan curve invalid, using the built-in")
		}
	}
	if c, err := dm.d.Curves.Load(name); err == nil {
		return c
	}
	c, _ := dm.d.Curves.Load("truesense")
	return c
}

// smartCfg returns the active Smart auto profile with the same fallback.
func (dm *Daemon) smartCfg() smartprofile.Profile {
	if raw, ok := dm.d.State.Get("smart_profile_data"); ok && raw != nil {
		if b, err := asJSON(raw); err == nil {
			if p, err := smartprofile.Parse(b); err == nil {
				return p
			}
			dm.log.Warn("smart profile invalid, using the built-in")
		}
	} else if p, err := dm.d.Profiles.Load(dm.d.State.String("smart_profile", "truesense")); err == nil {
		return p
	}
	p, _ := dm.d.Profiles.Load("truesense")
	return p
}

func (dm *Daemon) smartParams() autoprofile.Params { return paramsFrom(dm.smartCfg()) }

func paramsFrom(p smartprofile.Profile) autoprofile.Params {
	return autoprofile.Params{
		ACIdle: p.ACIdle, ACBrowsing: p.ACBrowsing, ACBusy: p.ACBusy, ACTurbo: p.ACTurbo,
		TurboAfterS: float64(p.TurboAfterS), TurboMaxCPU: float64(p.TurboMaxCPU), TurboMaxGPU: float64(p.TurboMaxGPU),
		BackoffHoldS: float64(p.BackoffHoldS), IdleAfterS: float64(p.IdleAfterS),
		BatteryIdle: p.BatteryIdle, BatteryBrowsing: p.BatteryBrowsing, BatteryBusy: p.BatteryBusy, BatteryHot: p.BatteryHot, BatteryLowPct: float64(p.BatteryLowPct),
		UpDwellS: float64(p.UpDwellS), DownDwellS: float64(p.DownDwellS), BusyHoldS: float64(p.BusyHoldS), MinChangeS: float64(p.MinChangeS),
		TurboMarginC: float64(p.TurboMargin), TurboRetryS: float64(p.TurboRetryS),
	}
}

func (dm *Daemon) say(text, icon string) {
	if dm.d.Config.Profile.OSD && dm.d.OSD != nil {
		dm.d.OSD.Show(text, icon)
	}
}

func (dm *Daemon) legacy(name string) bool {
	return dm.d.Config.Compat.PredatorWidgets && dm.d.LegacyFlag(name)
}

// ---- running ----

// Run starts the control loop and the socket and blocks until ctx is cancelled. The fans and the
// clocks are handed back on every way out.
func (dm *Daemon) Run(ctx context.Context, socket string, allowedUIDs []uint32) error {
	allowed := map[uint32]bool{}
	for _, u := range allowedUIDs {
		allowed[u] = true
	}
	srv := &ipc.Server{Path: socket, Allowed: allowed, Handlers: dm.Handlers(), Log: dm.log}
	if err := srv.Listen(); err != nil {
		return fmt.Errorf("control socket: %w", err)
	}
	defer srv.Close()
	defer dm.Shutdown()
	go func() { _ = srv.Serve(ctx) }()
	dm.gpuCtx = ctx
	dm.withLock(dm.capturePowerLimits)
	dm.applyBootLook("boot")
	dm.applyUVBoot()
	dm.applyGPUUVBoot()
	if err := sdnotify.Ready(); err != nil {
		dm.log.Warn("sd_notify READY failed", "err", err)
	}
	return dm.loop(ctx)
}

// loop runs the control steps at the adaptive pace until ctx ends.
func (dm *Daemon) loop(ctx context.Context) error {
	mono := dm.d.Clock.Mono
	next := mono()
	for ctx.Err() == nil {
		dm.safeStep()
		_ = sdnotify.Watchdog()
		next += dm.interval.Seconds()
		now := mono()
		if next < now-5 { // woke from suspend: resync
			next = now
			dm.mu.Lock()
			dm.writer.Forget()
			dm.gov.Resync(dm.tick)
			dm.mu.Unlock()
		}
		wait := time.Duration(max(0, next-now) * float64(time.Second))
		select {
		case <-ctx.Done():
			return nil
		case <-time.After(wait):
		}
	}
	return nil
}

// safeStep runs one step and never lets a failure out: a stuck low fan under load is the
// worst outcome, so when a step fails while the CPU is hot (or unknown) the fans go to 100 %.
func (dm *Daemon) safeStep() {
	defer func() {
		if r := recover(); r != nil {
			dm.log.Error("step failed", "panic", r)
			var hot bool
			dm.withLock(func() { hot = dm.tick == 0 || dm.tcpuNow() >= 80 })
			if hot {
				_ = dm.d.HW.FanManual(100, 100)
			}
		}
	}()
	dm.step()
}

func (dm *Daemon) tcpuNow() float64 { v, _ := dm.tcpu.Value(); return v }
func (dm *Daemon) tgpuNow() float64 { v, _ := dm.tgpu.Value(); return v }

// Shutdown hands the fans back to the firmware and lifts the clock cap. It is safe to call twice.
func (dm *Daemon) Shutdown() {
	_ = dm.d.HW.FanAuto()
	dm.mu.Lock()
	dm.gov.Release()
	dm.mu.Unlock()
	dm.d.Cap.Uncap()
	_ = sdnotify.Stopping()
}
