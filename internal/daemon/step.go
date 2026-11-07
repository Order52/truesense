package daemon

import (
	"context"
	"math"
	"time"

	"github.com/Order52/truesense/internal/autoprofile"
	"github.com/Order52/truesense/internal/fanctl"
	"github.com/Order52/truesense/internal/gamerules"
	"github.com/Order52/truesense/internal/power"
)

// step is one pass of the control loop: watchdogs first, then the readings and decisions.
func (dm *Daemon) step() {
	wall, mono := dm.d.Clock.Wall(), dm.d.Clock.Mono()
	dm.withLock(func() {
		if dm.fanOff && dm.fanMustReturn() {
			dm.fanOff = false
			_ = dm.d.HW.FanAuto()
			dm.writer.Forget()
			dm.log.Warn("fans back on Auto: the laptop got warm")
			dm.say("Fans back on Auto: it got warm", "fan")
		}
	})
	if dm.d.GPUGuard != nil {
		dm.d.GPUGuard.Tick()
	}
	if dm.d.UVGuard != nil {
		dm.d.UVGuard.Tick() // the boot undervolt survived ten minutes: the crash-guard flag goes
	}
	// Wall clock ran while the monotonic clock did not: the machine was suspended.
	if !dm.lastWall.IsZero() && wall.Sub(dm.lastWall).Seconds()-(mono-dm.lastMono) > 5 {
		dm.applyBootLook("resume from suspend")
	}
	dm.lastWall, dm.lastMono = wall, mono

	var smart, pmOn bool
	var tick int
	dm.withLock(func() {
		dm.runStep(wall)
		smart = dm.smartActive() || dm.autoKind() == "smart"
		// Adaptive pace: once a second while something smart is working or the laptop is busy,
		// every two seconds when nothing smart is on or it has been calm for a while.
		if (smart && (dm.activity != autoprofile.Idle || dm.fan.Calm() < 30)) || dm.fanOff {
			dm.interval = time.Second
		} else {
			dm.interval = 2 * time.Second
		}
		tick = dm.tick
		pmOn = dm.d.State.Bool("pm_takeover", false)
	})
	if smart && dm.gpuCtx != nil {
		dm.d.Sensors.StartGPU(dm.gpuCtx)
	}
	if tick%5 == 0 {
		dm.chargeFullCheck()
	}
	if tick%150 == 5 && pmOn {
		dm.pmGuard() // while "TrueSense manages power" is on, a manager that came back (update, reboot) is stopped again
	}
}

// withLock runs f with the daemon lock held and always releases it, even if f panics: a lock left
// held by a failed step would deadlock the very fallback that drives the fans to full speed.
func (dm *Daemon) withLock(f func()) {
	dm.mu.Lock()
	defer dm.mu.Unlock()
	f()
}

func (dm *Daemon) fanMustReturn() bool {
	return fanctl.MustReturnFans(dm.tcpuNow(), dm.tgpuNow(), dm.hwOK)
}

// runStep is the body of a step, with the lock held.
func (dm *Daemon) runStep(wall time.Time) {
	dm.tick++
	load := dm.d.Sensors.CPULoad()
	pkg, havePkg := dm.d.Sensors.PackageTemp()
	s, err := dm.d.HW.ReadSensors()
	if err != nil {
		if dm.hwOK || time.Since(dm.lastTickError) > time.Minute {
			dm.log.Warn("device unavailable, waiting", "err", err)
			dm.lastTickError = time.Now()
		}
		dm.hwOK = false
	} else {
		dm.hwOK = true
	}
	rawC := 0.0
	if havePkg {
		rawC = pkg
	}
	rawG := 0.0
	if err == nil {
		rawC = math.Max(rawC, float64(s.CPUTemp))
		rawG = float64(s.GPUTemp)
	}
	gpu := dm.d.Sensors.GPU()
	if !gpu.Seen.IsZero() && wall.Sub(gpu.Seen) <= 6*time.Second && gpu.Temp > 0 {
		rawG = math.Max(rawG, gpu.Temp)
	}
	if rawC <= 0 { // no sensors at all: the firmware decides
		dm.writer.Write(0, 0)
		return
	}
	tc := dm.tcpu.Update(rawC)
	tg := rawC
	if rawG > 0 {
		tg = rawG
	}
	tg = dm.tgpu.Update(tg)
	chassis := 0.0
	if err == nil {
		dm.rpm = [2]int{s.CPUFanRPM, s.GPUFanRPM}
		if s.SysTemp != 0 {
			chassis = dm.tchassis.Update(float64(s.SysTemp))
		}
	}
	if v, ok := dm.tchassis.Value(); ok && chassis == 0 {
		chassis = v
	}
	readings := autoprofile.Readings{TCPU: tc, TGPU: tg, CPULoad: load, GPUUtil: gpu.Util, GPUPower: gpu.Power}
	dm.activity = dm.cls.Classify(readings)
	if dm.tick%15 == 0 {
		dm.writer.Verify()
	}
	ac := dm.d.HW.OnAC()
	if dm.lastAC != nil && ac != *dm.lastAC { // plug or unplug can reset the driver state
		dm.writer.Forget()
		dm.engine.Resync()
		dm.gov.Resync(dm.tick)
	}
	dm.lastAC = &ac
	if dm.hwOK && (dm.curMode == "" || dm.tick%3 == 0) {
		if m, err := dm.d.HW.GetMode(); err == nil {
			dm.curMode = m
		}
	}
	smart := dm.d.State.Bool("smart_fan", false) && !dm.legacy("predator-fan-mode") && dm.hwOK
	if smart {
		c, g := dm.fan.Step(fanctl.Inputs{Activity: dm.activity, TCPU: tc, TGPU: tg, TChassis: chassis, GPUPower: gpu.Power, OnAC: ac, Dt: dm.interval})
		dm.writer.Write(c, g)
	} else {
		dm.writer.Forget()
		dm.fan.Reset()
	}
	if dm.tick%3 == 1 {
		dm.enforcePowerLimit(false)
	}
	dm.thermalGovernor(tc)
	if dm.hwOK {
		dm.autoProfile(readings, ac, wall)
		dm.operatingLighting()
	}
	dm.publish(smart, gpu.Util, gpu.Power, load, tc, tg, chassis)
}

func (dm *Daemon) thermalGovernor(tcpu float64) {
	if dm.gov.Step(dm.tick, tcpu, dm.smartActive()) {
		dm.d.Cap.Apply(dm.gov.Cap)
	}
}

// capturePowerLimits remembers what the firmware set (once), to hand it back when nothing smart is on.
func (dm *Daemon) capturePowerLimits() {
	if dm.plOrig != nil || dm.d.Power == nil {
		return
	}
	if raw, err := dm.d.Power.ReadRaw(); err == nil {
		dm.plOrig = &raw
	}
}

// enforcePowerLimit keeps the CPU package power limits where the user (or the automatic limits
// of the configuration) want them, and hands the firmware's own limits back when nothing smart
// is on.
func (dm *Daemon) enforcePowerLimit(forceNow bool) {
	if dm.d.Power == nil {
		return
	}
	dm.capturePowerLimits()
	user := dm.userLimits()
	if user == nil && !dm.smartActive() && !forceNow {
		if dm.plTouched && dm.plOrig != nil {
			if cur, err := dm.d.Power.ReadRaw(); err == nil && cur != *dm.plOrig {
				_ = dm.d.Power.WriteRaw(*dm.plOrig)
			}
			dm.plTouched = false
		}
		return
	}
	dm.plTouched = true
	var want power.RawLimits
	if user != nil {
		want = power.RawLimits{PL1: int64(user[0] * 1_000_000), PL2: int64(user[1] * 1_000_000)}
	} else {
		w := dm.d.Config.CPU.PLNormalW
		if dm.activity == autoprofile.Gaming {
			w = dm.d.Config.CPU.PLGamingW
		}
		want = power.RawLimits{PL1: int64(w) * 1_000_000, PL2: int64(w) * 1_000_000}
	}
	if cur, err := dm.d.Power.ReadRaw(); err != nil || cur != want {
		_ = dm.d.Power.WriteRaw(want)
	}
}

// userLimits returns the user's own CPU power limits (pl1, pl2 in watts), or nil.
func (dm *Daemon) userLimits() []float64 {
	v, ok := dm.d.State.Get("pl_user")
	if !ok || v == nil {
		return nil
	}
	b, err := asJSON(v)
	if err != nil {
		return nil
	}
	var u struct{ PL1, PL2 float64 }
	if jsonUnmarshalPL(b, &u) != nil {
		return nil
	}
	return []float64{u.PL1, u.PL2}
}

// gameMode is the mode asked for by a running game rule ("" if none). The process list is read
// every third tick (cheap, and games do not start every second).
func (dm *Daemon) gameMode() string {
	rules := dm.gameRules()
	if len(rules) == 0 || dm.d.Procs == nil {
		dm.game = ""
		return ""
	}
	if dm.tick%3 == 0 || dm.gameTick == 0 {
		dm.gameTick = dm.tick
		dm.game, _ = gamerules.Running(rules, dm.d.Procs())
	}
	return dm.game
}

func (dm *Daemon) gameRules() gamerules.Rules {
	v, ok := dm.d.State.Get("game_rules")
	if !ok || v == nil {
		return nil
	}
	b, err := asJSON(v)
	if err != nil {
		return nil
	}
	r, err := gamerules.Parse(b)
	if err != nil {
		return nil
	}
	return r
}

// autoProfile makes the automatic power-mode decision and carries it out.
func (dm *Daemon) autoProfile(r autoprofile.Readings, ac bool, wall time.Time) {
	kind := dm.autoKind()
	cfg := dm.d.Config
	now := dm.d.Clock.Mono()
	bat := dm.d.Sensors.BatteryPct()
	dec := dm.engine.Step(autoprofile.Inputs{
		Kind: kind, Now: now, Wall: float64(wall.UnixNano()) / 1e9, Activity: dm.activity, Readings: r, AC: ac, BatteryPct: bat, CurMode: dm.curMode,
		NativeAC: cfg.Native.AC, NativeBat: cfg.Native.Battery, GameMode: dm.gameMode(), Disabled: dm.legacy("predator-auto-off"),
	})
	if dec.Reason != "" {
		dm.reason = dec.Reason
	}
	if h := dm.engine.HoldUntil(); h != dm.d.State.Float("profile_hold_until", 0) {
		dm.d.State.Set("profile_hold_until", h) // the engine dropped the hold (the situation changed): remember that
	}
	if dec.Mode == "" {
		return
	}
	before := dm.curMode
	got, err := dm.d.HW.SetMode(dec.Mode)
	if err != nil {
		dm.log.Warn("auto profile", "err", err)
		return
	}
	dm.curMode = got
	dm.engine.Applied(now)
	dm.log.Info("auto power mode", "from", before, "to", dec.Mode, "kind", kind, "reason", dec.Reason, "cpu_c", int(math.Round(r.TCPU)), "gpu_c", int(math.Round(r.TGPU)), "gpu_util", int(math.Round(r.GPUUtil)))
	if dec.Notify {
		what := string(dm.activity)
		if dm.activity == autoprofile.Idle || dm.activity == autoprofile.Browsing {
			what = "on battery"
			if ac {
				what = "on power"
			}
		}
		dm.say("Power mode: "+dec.Mode+" ("+kind+" auto: "+what+")", "power")
	}
}

// operatingLighting is opt-in ([keyboard] operating_follow): it re-sends the operating-mode
// colour when the power mode changes (the keyboard does not track it by itself).
func (dm *Daemon) operatingLighting() {
	if !dm.d.State.Bool("operating_follow", false) || dm.tick%2 != 0 || dm.curMode == "" || dm.d.Lights == nil {
		return
	}
	if dm.curMode != dm.opMode {
		dm.opMode = dm.curMode
		colors := map[string]string{}
		for k, v := range dm.d.Config.Keyboard.OperatingColors {
			if s, ok := v.(string); ok {
				colors[k] = s
			}
		}
		if err := dm.d.Lights.FollowMode(dm.curMode, colors); err != nil {
			dm.log.Warn("operating lighting failed", "err", err)
		}
	}
}

func (dm *Daemon) pmGuard() {
	if dm.d.PM == nil {
		return
	}
	ctx := context.Background()
	if len(dm.d.PM.Status(ctx)) == 0 {
		return
	}
	r, err := dm.d.PM.Takeover(ctx)
	if err != nil {
		dm.log.Warn("power manager check failed", "err", err)
		return
	}
	dm.log.Info("power managers stopped again", "stopped", r.Stopped, "failed", r.Failed)
	if len(r.Stopped) > 0 {
		dm.say("Stopped another power manager: "+joinComma(r.Stopped), "power")
	}
}
