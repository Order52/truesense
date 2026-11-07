package daemon

import (
	"context"
	"encoding/json"
	"maps"

	"github.com/Order52/truesense/internal/curves"
	"github.com/Order52/truesense/internal/fanctl"
	"github.com/Order52/truesense/internal/firmware"
	"github.com/Order52/truesense/internal/gamerules"
	"github.com/Order52/truesense/internal/gpuundervolt"
	"github.com/Order52/truesense/internal/hw"
	"github.com/Order52/truesense/internal/ipc"
	"github.com/Order52/truesense/internal/looks"
	"github.com/Order52/truesense/internal/power"
	"github.com/Order52/truesense/internal/smartprofile"
	"github.com/Order52/truesense/internal/undervolt"
)

func fail(format string, args ...any) error { return firmware.Errorf(format, args...) }

// Handlers is the control socket's command table. Every handler takes the daemon lock, so the
// control loop and the requests never change the hardware at the same time.
func (dm *Daemon) Handlers() map[string]ipc.Handler {
	locked := func(f func(ctx context.Context, a ipc.Args) (any, error)) ipc.Handler {
		return func(ctx context.Context, a ipc.Args) (any, error) {
			dm.mu.Lock()
			defer dm.mu.Unlock()
			return f(ctx, a)
		}
	}
	return map[string]ipc.Handler{
		"ping":           func(context.Context, ipc.Args) (any, error) { return "pong", nil },
		"state":          locked(dm.hState),
		"set":            locked(dm.hSet),
		"fan":            locked(dm.hFan),
		"power":          locked(dm.hPower),
		"fan_curve":      locked(dm.hFanCurve),
		"smart_profile":  locked(dm.hSmartProfile),
		"undervolt":      locked(dm.hUndervolt),
		"gpu_undervolt":  locked(dm.hGPUUndervolt),
		"battery_full":   locked(dm.hBatteryFull),
		"boot_look":      locked(dm.hBootLook),
		"hold":           locked(dm.hHold),
		"game_rules":     locked(dm.hGameRules),
		"power_managers": dm.hPowerManagers, // slow (systemctl): runs without the lock
		"cpu_limits":     locked(dm.hLimits),
		"cpu_info":       locked(dm.hCPUInfo),
		"touchpad":       locked(dm.hTouchpad),
	}
}

func (dm *Daemon) hState(context.Context, ipc.Args) (any, error) {
	left := max(0, int(dm.d.State.Float("profile_hold_until", 0)-float64(dm.d.Clock.Wall().Unix())))
	return map[string]any{"live": maps.Clone(dm.live), "settings": dm.d.State.Snapshot(), "hold_left_s": left}, nil
}

func (dm *Daemon) hSet(_ context.Context, a ipc.Args) (any, error) {
	key, err := a.String("key", "")
	if err != nil {
		return nil, err
	}
	st := dm.d.State
	if key == "auto_mode" {
		v, err := a.String("value", "")
		if err != nil || (v != "off" && v != "native" && v != "smart") {
			return nil, fail("auto_mode: off | native | smart")
		}
		st.Set("auto_mode", v)
		st.Set("auto_profile", v != "off")
		dm.log.Info("setting auto_mode", "value", v)
		if v != "off" { // choosing auto means: take over now
			dm.engine.TakeOverNow()
			st.Set("profile_hold_until", 0.0)
		}
		return st.Snapshot(), nil
	}
	switch key {
	case "smart_fan", "auto_profile", "operating_follow":
	default:
		return nil, fail("unknown setting %q", key)
	}
	on, ok := a["value"].(bool)
	if !ok {
		return nil, fail("value must be true or false")
	}
	if key == "auto_profile" {
		if on {
			st.Set("auto_mode", "smart")
		} else {
			st.Set("auto_mode", "off")
		}
	}
	st.Set(key, on)
	dm.log.Info("setting", "key", key, "value", on)
	if key == "smart_fan" && !on {
		dm.writer.Forget()
	}
	return st.Snapshot(), nil
}

// hFan is one request = smart curve off + hardware write, atomically. A fan stop needs the
// explicit confirm and a cool machine, and is watched by the temperature watchdog.
func (dm *Daemon) hFan(_ context.Context, a ipc.Args) (any, error) {
	mode, err := a.String("mode", "")
	if err != nil {
		return nil, err
	}
	confirm, err := a.Bool("confirm", false)
	if err != nil {
		return nil, err
	}
	st := dm.d.State
	if mode == "smart" {
		st.Set("smart_fan", true)
		dm.fanOff = false
		dm.log.Info("setting smart_fan", "value", true)
		return "smart", nil
	}
	cpu, gpu := 0, 0
	if mode == "off" {
		mode = "manual"
	} else if mode == "manual" {
		if !a.Has("cpu") || a["cpu"] == nil {
			return nil, fail("manual fan duty is 20..100 %% per fan, or 0 to stop that fan")
		}
		if cpu, err = a.Int("cpu", 0); err != nil {
			return nil, err
		}
		gpu = cpu
		if a.Has("gpu") && a["gpu"] != nil {
			if gpu, err = a.Int("gpu", 0); err != nil {
				return nil, err
			}
		}
	}
	switch mode {
	case "auto", "max", "manual":
	default:
		return nil, fail("fan mode %q", mode)
	}
	stop := false
	if mode == "manual" {
		if !hw.ValidDuty(cpu) || !hw.ValidDuty(gpu) {
			return nil, fail("manual fan duty is 20..100 %% per fan, or 0 to stop that fan")
		}
		stop = cpu == 0 || gpu == 0
		if stop {
			if !confirm {
				return nil, fail("stopping a fan needs explicit confirmation")
			}
			tc, known := dm.tcpu.Value()
			tg, _ := dm.tgpu.Value()
			if err := fanctl.CanStop(tc, tg, known); err != nil {
				return nil, fail("%v", err)
			}
		}
	}
	dm.fanOff = false
	st.Set("smart_fan", false)
	dm.log.Info("setting smart_fan = false", "fan", mode)
	dm.writer.Forget()
	dm.fan.Reset()
	switch mode {
	case "auto":
		err = dm.d.HW.FanAuto()
	case "max":
		err = dm.d.HW.FanMax()
	default:
		err = dm.d.HW.FanManual(cpu, gpu)
	}
	if err != nil {
		return nil, err
	}
	if stop {
		dm.fanOff = true
		which := "the GPU fan"
		switch {
		case cpu == 0 && gpu == 0:
			which = "both fans"
		case cpu == 0:
			which = "the CPU fan"
		}
		dm.log.Warn("fan stopped by the user; the watchdog gives it back when it gets warm", "which", which, "back_at_cpu_c", fanctl.StopMaxCPU, "back_at_gpu_c", fanctl.StopMaxGPU)
		if cpu == 0 && gpu == 0 {
			return "off", nil
		}
		return "partial-off", nil
	}
	return mode, nil
}

func (dm *Daemon) hPower(_ context.Context, a ipc.Args) (any, error) {
	mode, err := a.String("mode", "")
	if err != nil {
		return nil, err
	}
	hold, err := a.Float("hold_minutes", 0)
	if err != nil {
		return nil, err
	}
	got, err := dm.d.HW.SetMode(mode)
	if err != nil {
		return nil, err
	}
	dm.curMode = got
	if hold > 0 {
		dm.setHold(hold)
	}
	return got, nil
}

func (dm *Daemon) hHold(_ context.Context, a ipc.Args) (any, error) {
	m, err := a.Float("minutes", 0)
	if err != nil {
		return nil, err
	}
	return dm.setHold(m), nil
}

// hFanCurve: get -> the active curve; use -> switch to a built-in or saved one by name, or to a
// curve sent as data (kept by the daemon, so it works before login).
func (dm *Daemon) hFanCurve(_ context.Context, a ipc.Args) (any, error) {
	action, _ := a.String("action", "get")
	st := dm.d.State
	switch action {
	case "get":
		names, _ := dm.d.Curves.Names()
		return map[string]any{"name": st.String("fan_curve", "native"), "curve": dm.fan.Curve(), "names": names}, nil
	case "use":
	default:
		return nil, fail("fan_curve: get | use")
	}
	name, _ := a.String("name", "")
	var c curves.Curve
	if a.Has("data") && a["data"] != nil {
		var err error
		if c, err = curves.Validate(a["data"]); err != nil {
			return nil, err
		}
		st.Set("fan_curve_data", c)
		if name == "" {
			name = "custom"
		}
		st.Set("fan_curve", name)
	} else {
		if name == "" {
			name = "native"
		}
		var err error
		if c, err = dm.d.Curves.Load(name); err != nil {
			return nil, err
		}
		if curves.IsBuiltin(name) {
			st.Set("fan_curve_data", nil)
		} else {
			st.Set("fan_curve_data", c)
		}
		st.Set("fan_curve", name)
	}
	if c.Native { // the native curve is no curve: the firmware runs the fans, like PredatorSense on Windows
		st.Set("smart_fan", false)
		dm.fanOff = false
		dm.writer.Forget()
		if err := dm.d.HW.FanAuto(); err != nil {
			dm.log.Warn("fan auto", "err", err)
		}
		return map[string]any{"name": "native", "curve": c}, nil
	}
	dm.fan.SetCurve(c)
	dm.writer.Forget()
	dm.log.Info("setting fan_curve", "name", name)
	return map[string]any{"name": st.String("fan_curve", ""), "curve": c}, nil
}

// hSmartProfile: get -> the active Smart auto profile; use -> a saved one by name, or one sent as data.
func (dm *Daemon) hSmartProfile(_ context.Context, a ipc.Args) (any, error) {
	action, _ := a.String("action", "get")
	st := dm.d.State
	switch action {
	case "get":
		names, _ := dm.d.Profiles.Names()
		return map[string]any{"name": st.String("smart_profile", "truesense"), "profile": dm.smartCfg(), "names": names}, nil
	case "use":
	default:
		return nil, fail("smart_profile: get | use")
	}
	name, _ := a.String("name", "")
	var p smartprofile.Profile
	if a.Has("data") && a["data"] != nil {
		var err error
		if p, err = smartprofile.Validate(a["data"]); err != nil {
			return nil, err
		}
		st.Set("smart_profile_data", p)
		if name == "" {
			name = "custom"
		}
		st.Set("smart_profile", name)
	} else {
		if name == "" {
			name = "truesense"
		}
		var err error
		if p, err = dm.d.Profiles.Load(name); err != nil {
			return nil, err
		}
		if smartprofile.IsBuiltin(name) {
			st.Set("smart_profile_data", nil)
		} else {
			st.Set("smart_profile_data", p)
		}
		st.Set("smart_profile", name)
	}
	dm.engine.P = paramsFrom(p)
	dm.log.Info("setting smart_profile", "name", name)
	return map[string]any{"name": st.String("smart_profile", ""), "profile": p}, nil
}

// withSetting returns m with extra keys, for results that add the daemon's own state to a reading.
func with(m map[string]any, extra map[string]any) map[string]any {
	maps.Copy(m, extra)
	return m
}

func structMap(v any) map[string]any {
	m := map[string]any{}
	if b, err := json.Marshal(v); err == nil {
		_ = json.Unmarshal(b, &m)
	}
	return m
}

// hUndervolt: get | apply (core, cache in mV, needs confirm) | reset | boot (apply at boot, needs
// confirm) | boot_clear. Offsets never persist across a reboot.
func (dm *Daemon) hUndervolt(_ context.Context, a ipc.Args) (any, error) {
	action, _ := a.String("action", "get")
	st := dm.d.State
	switch action {
	case "get":
		cur := map[string]any{}
		if off, err := dm.d.UV.Read(); err != nil {
			cur["error"] = err.Error()
		} else {
			cur = structMap(off)
		}
		applied, _ := st.Get("uv_applied")
		boot, _ := st.Get("uv_boot")
		return with(cur, map[string]any{"applied": applied, "boot": boot, "pending": dm.guardPending(dm.d.UVGuard)}), nil
	case "reset":
		off, err := dm.d.UV.Reset()
		if err != nil {
			return nil, err
		}
		st.Set("uv_applied", nil)
		dm.log.Info("undervolt reset to stock")
		return off, nil
	case "boot_clear":
		st.Set("uv_boot", nil)
		return nil, nil
	case "apply", "boot":
	default:
		return nil, fail("undervolt: get | apply | reset | boot | boot_clear")
	}
	confirm, _ := a.Bool("confirm", false)
	if !confirm {
		return nil, fail("an undervolt needs explicit confirmation (it can freeze or reboot the laptop if too aggressive)")
	}
	p, err := undervolt.Validate(map[string]any{"core": a["core"], "cache": a["cache"]})
	if err != nil {
		return nil, err
	}
	force, _ := a.Bool("force", false)
	if lvl, why := undervolt.Risk(p.Core, p.Cache); string(lvl) == "danger" && !force {
		return nil, fail("%s Add the explicit force flag if you really mean it (CLI: --force).", why)
	}
	name, _ := a.String("name", "")
	if action == "boot" {
		boot := map[string]any{"name": name, "core": p.Core, "cache": p.Cache}
		st.Set("uv_boot", boot)
		return boot, nil
	}
	off, err := dm.d.UV.Apply(p.Core, p.Cache, true)
	if err != nil {
		_, _ = dm.d.UV.Reset()
		return nil, err
	}
	st.Set("uv_applied", map[string]any{"name": name, "core": p.Core, "cache": p.Cache})
	dm.log.Info("undervolt applied", "core_mv", p.Core, "cache_mv", p.Cache)
	return off, nil
}

func (dm *Daemon) guardPending(g interface{ Armed() bool }) bool { return g != nil && g.Armed() }

// hGPUUndervolt: get | apply (offset MHz, cap MHz; needs confirm) | reset | boot | boot_clear.
func (dm *Daemon) hGPUUndervolt(_ context.Context, a ipc.Args) (any, error) {
	action, _ := a.String("action", "get")
	st := dm.d.State
	switch action {
	case "get":
		cur := map[string]any{}
		if s, err := dm.d.GPUUV.Read(); err != nil {
			cur["error"] = err.Error()
		} else {
			cur = structMap(s)
		}
		applied, _ := st.Get("gpu_uv_applied")
		boot, _ := st.Get("gpu_uv_boot")
		return with(cur, map[string]any{"applied": applied, "boot": boot, "pending": dm.guardPending(dm.d.GPUGuard)}), nil
	case "reset":
		r, err := dm.d.GPUUV.Reset()
		if err != nil {
			return nil, err
		}
		st.Set("gpu_uv_applied", nil)
		dm.log.Info("GPU undervolt reset")
		return r, nil
	case "boot_clear":
		st.Set("gpu_uv_boot", nil)
		return nil, nil
	case "apply", "boot":
	default:
		return nil, fail("gpu_undervolt: get | apply | reset | boot | boot_clear")
	}
	confirm, _ := a.Bool("confirm", false)
	if !confirm {
		return nil, fail("a GPU undervolt needs explicit confirmation (it can crash games or the driver if too aggressive)")
	}
	capv := a["cap"]
	if capv == nil {
		capv = 0.0
	}
	p, err := gpuundervolt.Validate(map[string]any{"offset": a["offset"], "cap": capv})
	if err != nil {
		return nil, err
	}
	force, _ := a.Bool("force", false)
	if lvl, why := gpuundervolt.Risk(p.Offset, p.Cap); string(lvl) == "danger" && !force {
		return nil, fail("%s Add the explicit force flag if you really mean it (CLI: --force).", why)
	}
	name, _ := a.String("name", "")
	if action == "boot" {
		boot := map[string]any{"name": name, "offset": p.Offset, "cap": p.Cap}
		st.Set("gpu_uv_boot", boot)
		return boot, nil
	}
	r, err := dm.d.GPUUV.Apply(p.Offset, p.Cap)
	if err != nil {
		_, _ = dm.d.GPUUV.Reset()
		return nil, err
	}
	st.Set("gpu_uv_applied", map[string]any{"name": name, "offset": p.Offset, "cap": p.Cap})
	dm.log.Info("GPU undervolt applied", "offset_mhz", p.Offset, "cap_mhz", p.Cap)
	return r, nil
}

// hBatteryFull: get | start | cancel: charge to 100 % once, then the 80 % limit returns by itself.
func (dm *Daemon) hBatteryFull(_ context.Context, a ipc.Args) (any, error) {
	action, _ := a.String("action", "get")
	st := dm.d.State
	ac := dm.lastAC != nil && *dm.lastAC
	cf, _ := st.Get("charge_full")
	switch action {
	case "get":
		limit, err := dm.d.HW.GetBatteryLimit()
		if err != nil {
			return nil, err
		}
		return map[string]any{"active": cf != nil, "limit": limit, "capacity": dm.d.Sensors.BatteryPct(), "ac": ac}, nil
	case "start":
		if !ac {
			return nil, fail("plug the charger in first")
		}
		limit, err := dm.d.HW.GetBatteryLimit()
		if err != nil {
			return nil, err
		}
		if !limit {
			return nil, fail("the 80 %% limit is off already: the battery charges to 100 %%")
		}
		if _, err := dm.d.HW.SetBatteryLimit(false); err != nil {
			return nil, err
		}
		st.Set("charge_full", map[string]any{"since": float64(dm.d.Clock.Wall().Unix()), "full_at": nil})
		dm.log.Info("charging to 100 % once")
		return map[string]any{"active": true}, nil
	case "cancel":
		if _, err := dm.d.HW.SetBatteryLimit(true); err != nil {
			return nil, err
		}
		st.Set("charge_full", nil)
		return map[string]any{"active": false}, nil
	}
	return nil, fail("battery_full: get | start | cancel")
}

func (dm *Daemon) hBootLook(_ context.Context, a ipc.Args) (any, error) {
	action, _ := a.String("action", "get")
	switch action {
	case "get":
		return dm.d.Looks.ReadBoot(), nil
	case "clear":
		return nil, dm.d.Looks.WriteBoot(nil)
	case "set":
		var look looks.Look
		if err := a.Decode("look", &look); err != nil || look.IsEmpty() {
			return nil, fail("a look needs keyboard and/or rear settings")
		}
		if err := dm.d.Looks.WriteBoot(&look); err != nil {
			return nil, err
		}
		return look, nil
	}
	return nil, fail("boot_look: get | set | clear")
}

// hGameRules maps a program name to a power mode while it runs: list | add | remove | clear.
func (dm *Daemon) hGameRules(_ context.Context, a ipc.Args) (any, error) {
	action, _ := a.String("action", "list")
	rules := dm.gameRules()
	if rules == nil {
		rules = gamerules.Rules{}
	}
	name, _ := a.String("name", "")
	var err error
	switch action {
	case "add":
		mode, _ := a.String("mode", "")
		if rules, err = rules.Add(name, mode); err != nil {
			return nil, err
		}
	case "remove":
		if rules, err = rules.Remove(name); err != nil {
			return nil, err
		}
	case "clear":
		rules = gamerules.Rules{}
	case "list":
	default:
		return nil, fail("game_rules: list | add | remove | clear")
	}
	if action != "list" {
		dm.d.State.Set("game_rules", rules)
		dm.engine.Resync()
	}
	running := ""
	if dm.d.Procs != nil {
		running, _ = gamerules.Running(rules, dm.d.Procs())
	}
	var runningV any
	if running != "" {
		runningV = running
	}
	return map[string]any{"rules": rules, "running": runningV}, nil
}

// hPowerManagers: status | takeover (stop other managers, fix the CPU defaults, remember) | restore.
func (dm *Daemon) hPowerManagers(ctx context.Context, a ipc.Args) (any, error) {
	action, _ := a.String("action", "status")
	st := dm.d.State
	switch action {
	case "status":
		return map[string]any{"on": st.Bool("pm_takeover", false), "found": dm.d.PM.Status(ctx), "recorded": dm.d.PM.Recorded()}, nil
	case "takeover":
		st.Set("pm_takeover", true)
		r, err := dm.d.PM.Takeover(ctx)
		if err != nil {
			return nil, err
		}
		if len(r.Failed) > 0 {
			dm.log.Warn("power managers: could not stop some", "failed", r.Failed)
		}
		dm.log.Info("TrueSense manages power", "stopped", r.Stopped)
		return with(structMap(r), map[string]any{"on": true}), nil
	case "restore":
		st.Set("pm_takeover", false)
		r := dm.d.PM.Restore(ctx)
		dm.log.Info("power managers restored", "restored", r.Restored)
		return with(structMap(r), map[string]any{"on": false}), nil
	}
	return nil, fail("power_managers: status | takeover | restore")
}

// hLimits sets the CPU power limits and keeps them (re-asserted every few seconds); reset goes
// back to the automatic limits from the configuration.
func (dm *Daemon) hLimits(ctx context.Context, a ipc.Args) (any, error) {
	reset, _ := a.Bool("reset", false)
	if reset {
		dm.d.State.Set("pl_user", nil)
		dm.enforcePowerLimit(true)
		return with(structMap(dm.d.Power.Info(ctx)), map[string]any{"automatic": true}), nil
	}
	pl1, err := a.Float("pl1", -1)
	if err != nil {
		return nil, err
	}
	pl2, err := a.Float("pl2", -1)
	if err != nil {
		return nil, err
	}
	if !a.Has("pl1") || !a.Has("pl2") {
		return nil, fail("give pl1 and pl2 (watts), or reset")
	}
	force, _ := a.Bool("force", false)
	if lvl, why := power.Risk(pl1, pl2); string(lvl) == "danger" && !force {
		return nil, fail("%s Add the explicit force flag if you really mean it.", why)
	}
	r, err := dm.d.Power.SetLimits(pl1, pl2)
	if err != nil {
		return nil, err
	}
	dm.d.State.Set("pl_user", map[string]any{"pl1": pl1, "pl2": pl2})
	dm.log.Info("setting power limits", "pl1_w", pl1, "pl2_w", pl2)
	return r, nil
}

func (dm *Daemon) hCPUInfo(ctx context.Context, _ ipc.Args) (any, error) {
	user, _ := dm.d.State.Get("pl_user")
	c := dm.d.Config.CPU
	return with(structMap(dm.d.Power.Info(ctx)), map[string]any{"user": user, "auto": map[string]any{"normal_w": c.PLNormalW, "gaming_w": c.PLGamingW}}), nil
}

func (dm *Daemon) hTouchpad(_ context.Context, a ipc.Args) (any, error) {
	action, _ := a.String("action", "status")
	cur, ok := dm.d.Touchpad.Enabled()
	var curV any
	if ok {
		curV = cur
	}
	if action == "status" {
		return curV, nil
	}
	var want bool
	switch action {
	case "on":
		want = true
	case "off":
		want = false
	case "toggle":
		want = !cur
	default:
		return nil, fail("touchpad: status | on | off | toggle")
	}
	return dm.d.Touchpad.Set(want)
}
