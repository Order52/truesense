package guitest

import (
	"fmt"
	"math"
	"sort"
	"time"
)

var smartDefaults = map[string]any{
	"ac_idle": "balanced", "ac_browsing": "balanced", "ac_busy": "performance", "ac_turbo": true, "turbo_after_s": 30, "turbo_max_cpu": 85, "turbo_max_gpu": 75,
	"backoff_hold_s": 90, "idle_after_s": 60, "battery_idle": "eco", "battery_browsing": "eco", "battery_busy": "balanced", "battery_hot": "quiet", "battery_low_pct": 20,
	"up_dwell_s": 5, "down_dwell_s": 60, "busy_hold_s": 60, "min_change_s": 45, "turbo_margin_c": 6, "turbo_retry_s": 180,
}

func with(base map[string]any, over map[string]any) map[string]any {
	out := cloneMap(base)
	for k, v := range over {
		out[k] = v
	}
	return out
}

func pts(p ...[2]int) []any {
	out := make([]any, len(p))
	for i, x := range p {
		out[i] = []any{x[0], x[1]}
	}
	return out
}

func (l *Laptop) initLibraries() {
	l.curves = newLibrary("fan curve")
	curveBase := map[string]any{"hysteresis": 4.0, "down_rate": 1.5, "predict": 5.0, "couple": 15, "battery_shift": 3.0}
	l.curves.addBuiltin("native", with(curveBase, map[string]any{"description": "PredatorSense default: the firmware controls the fans exactly as it does on Windows (they stop when cool). Nothing of ours runs", "native": true, "cpu": pts([2]int{52, 0}, [2]int{89, 100}), "gpu": pts([2]int{48, 0}, [2]int{78, 100})}))
	l.curves.addBuiltin("truesense", with(curveBase, map[string]any{"description": "Our curve for this laptop: silent when cool, early and smooth ramp, full speed before throttling",
		"cpu": pts([2]int{52, 0}, [2]int{56, 30}, [2]int{62, 38}, [2]int{68, 48}, [2]int{74, 58}, [2]int{80, 70}, [2]int{85, 85}, [2]int{89, 100}),
		"gpu": pts([2]int{48, 0}, [2]int{54, 30}, [2]int{60, 38}, [2]int{65, 48}, [2]int{70, 60}, [2]int{74, 78}, [2]int{78, 100})}))
	l.curves.addBuiltin("quiet", with(curveBase, map[string]any{"description": "Lowest noise: the fans stay off longer and ramp later", "predict": 3.0, "down_rate": 1.0,
		"cpu": pts([2]int{58, 0}, [2]int{66, 30}, [2]int{72, 40}, [2]int{78, 55}, [2]int{84, 75}, [2]int{89, 100}),
		"gpu": pts([2]int{52, 0}, [2]int{60, 30}, [2]int{66, 40}, [2]int{72, 58}, [2]int{77, 80}, [2]int{80, 100})}))
	l.curves.addBuiltin("performance", with(curveBase, map[string]any{"description": "Cooler and louder: starts early and ramps hard", "predict": 6.0, "down_rate": 2.5,
		"cpu": pts([2]int{45, 30}, [2]int{55, 40}, [2]int{62, 55}, [2]int{70, 70}, [2]int{78, 85}, [2]int{84, 100}),
		"gpu": pts([2]int{42, 30}, [2]int{52, 40}, [2]int{60, 55}, [2]int{66, 70}, [2]int{72, 85}, [2]int{76, 100})}))
	l.curves.addBuiltin("max-cooling", with(curveBase, map[string]any{"description": "Fans at full speed above 60 C", "predict": 8.0, "down_rate": 4.0,
		"cpu": pts([2]int{40, 40}, [2]int{60, 100}), "gpu": pts([2]int{40, 40}, [2]int{58, 100})}))
	l.curves.user["my-curve"] = true
	l.curves.data["my-curve"] = with(curveBase, map[string]any{"description": "", "cpu": pts([2]int{50, 0}, [2]int{60, 40}, [2]int{70, 70}, [2]int{85, 100}), "gpu": pts([2]int{46, 0}, [2]int{58, 40}, [2]int{70, 70}, [2]int{80, 100})})

	l.smart = newLibrary("smart profile")
	l.smart.addBuiltin("native", with(smartDefaults, map[string]any{"description": "PredatorSense default: Balanced on power, Eco on battery, nothing else (no load or heat logic, like Native auto)",
		"ac_busy": "balanced", "ac_turbo": false, "battery_busy": "eco", "battery_hot": "eco", "up_dwell_s": 2, "down_dwell_s": 2, "busy_hold_s": 0, "min_change_s": 2, "turbo_margin_c": 0, "turbo_retry_s": 10}))
	l.smart.addBuiltin("truesense", with(smartDefaults, map[string]any{"description": "Our profile for this laptop: Balanced day to day, Performance when you game, Turbo only when sustained and cool, Eco on battery"}))
	l.smart.addBuiltin("steady", with(smartDefaults, map[string]any{"description": "For long games and cooling that is not the laptop's own (external fans): Performance when busy, never Turbo", "ac_turbo": false, "busy_hold_s": 180, "down_dwell_s": 180, "min_change_s": 180, "up_dwell_s": 8}))
	l.smart.addBuiltin("battery-first", with(smartDefaults, map[string]any{"description": "Longest battery and quietest: no Turbo, light modes everywhere", "ac_idle": "quiet", "ac_browsing": "quiet", "ac_turbo": false, "battery_busy": "quiet", "battery_low_pct": 30, "down_dwell_s": 20}))
	l.smart.addBuiltin("performance-first", with(smartDefaults, map[string]any{"description": "Fastest: Performance for everyday use on power, Turbo after 10 s of heavy load", "ac_browsing": "performance", "turbo_after_s": 10, "turbo_max_cpu": 90, "turbo_max_gpu": 80, "backoff_hold_s": 45, "battery_idle": "quiet", "battery_browsing": "balanced", "battery_busy": "performance", "battery_low_pct": 15, "up_dwell_s": 3}))
	l.smart.addBuiltin("silent", with(smartDefaults, map[string]any{"description": "As quiet as possible: Quiet and Balanced only, no Turbo", "ac_idle": "quiet", "ac_browsing": "quiet", "ac_busy": "balanced", "ac_turbo": false, "battery_busy": "quiet", "battery_hot": "eco"}))

	keyBase := map[string]any{"tap": "power-next", "double": "output-toggle", "triple": "none", "long": "fan-max-toggle", "fn_esc": "none", "long_ms": 800, "multi_ms": 450}
	l.keys = newLibrary("key profile")
	l.keys.addBuiltin("native", with(keyBase, map[string]any{"description": "PredatorSense default: the PREDATOR key opens the app, nothing else is bound", "tap": "exec:truesense gui", "double": "none", "long": "none"}))
	l.keys.addBuiltin("default", with(keyBase, map[string]any{"description": "The standard behaviour: tap steps the power mode, double tap switches speakers/headphones, long press toggles Max fans"}))
	l.keys.addBuiltin("gaming", with(keyBase, map[string]any{"description": "Gaming: tap toggles Performance and Turbo, double tap Max fans, triple tap the next lighting effect, long press Smart auto, Fn+Esc mutes the microphone",
		"tap": "power-cycle:performance,turbo", "double": "fan-max-toggle", "triple": "keyboard-effect-next", "long": "auto-next", "fn_esc": "mic-toggle"}))
	l.keys.addBuiltin("work", with(keyBase, map[string]any{"description": "Work: tap steps Eco, Quiet and Balanced, double tap switches audio, triple tap mutes the microphone, long press toggles the touchpad",
		"tap": "power-cycle:eco,quiet,balanced", "double": "output-toggle", "triple": "mic-toggle", "long": "touchpad-toggle"}))
	l.keys.addBuiltin("lights", with(keyBase, map[string]any{"description": "Lighting: tap the next keyboard effect, double tap the next rear effect, triple tap the keyboard light, long press the rear light",
		"tap": "keyboard-effect-next", "double": "rear-effect-next", "triple": "keyboard:toggle", "long": "rear:toggle"}))
	l.keysCurrent = cloneMap(l.keys.data["default"])

	l.cpuUV = newLibrary("undervolt profile")
	l.cpuUV.addBuiltin("native", map[string]any{"description": "PredatorSense default: no undervolt (0 mV)", "core": 0, "cache": 0})
	l.cpuUV.addBuiltin("truesense", map[string]any{"description": "Our profile for the i7-13700HX: a conservative offset that usually gives a few degrees less heat at the same speed. Unverified on your unit: test under load", "core": -50, "cache": -40})
	l.cpuUV.addBuiltin("mild", map[string]any{"description": "Very safe: small offset", "core": -30, "cache": -30})
	l.cpuUV.addBuiltin("balanced", map[string]any{"description": "Moderate: more heat saved, more chance of instability", "core": -70, "cache": -50})
	l.cpuUV.addBuiltin("aggressive", map[string]any{"description": "Maximum offered: only if you have tested it; can freeze or reboot", "core": -100, "cache": -80})
	l.gpuUV = newLibrary("GPU undervolt profile")
	l.gpuUV.addBuiltin("native", map[string]any{"description": "PredatorSense default: no offset and no clock cap", "offset": 0, "cap": 0})
	l.gpuUV.addBuiltin("truesense", map[string]any{"description": "Our profile for the RTX 4060 Laptop: a moderate curve shift with a clock cap, aiming for less power and heat at about the same speed. Unverified on your unit: test under load", "offset": 120, "cap": 2400})
	l.gpuUV.addBuiltin("mild", map[string]any{"description": "Very safe: small shift, high cap", "offset": 60, "cap": 2500})
	l.gpuUV.addBuiltin("cool", map[string]any{"description": "Cooler and quieter: lower cap, bigger shift", "offset": 100, "cap": 2100})
	l.gpuUV.addBuiltin("efficient", map[string]any{"description": "Lowest power for light gaming: some games will run slower", "offset": 150, "cap": 1800})

	l.looks = newLibrary("look")
	l.looks.addBuiltin("native", map[string]any{
		"keyboard": map[string]any{"mode": "perkey", "brightness": 12, "cells": toAny(stockCells())},
		"rear":     map[string]any{"mode": "breathing", "color": "3723c7", "speed": 3, "brightness": 100, "direction": 1, "zones": nil},
	})
	for n, d := range map[string]string{"gaming": "Maximum performance: turbo, smart fans, overdrive on", "balanced": "Everyday use: balanced power, smart fans", "quiet": "As quiet as it gets: quiet power mode, firmware fan control, rear light off", "battery-saver": "Away from the charger: eco, firmware fans, rear light off, 80 % battery limit, backlight timeout on"} {
		l.profiles[n] = map[string]any{"description": d}
	}
	l.profiles["gaming"]["power"] = map[string]any{"mode": "turbo"}
	l.profiles["gaming"]["fan"] = map[string]any{"mode": "smart"}
	l.profiles["gaming"]["display"] = map[string]any{"overdrive": true}
	l.profiles["balanced"]["power"] = map[string]any{"mode": "balanced"}
	l.profiles["balanced"]["fan"] = map[string]any{"mode": "smart"}
	l.profiles["quiet"]["power"] = map[string]any{"mode": "quiet"}
	l.profiles["quiet"]["fan"] = map[string]any{"mode": "auto"}
	l.profiles["quiet"]["rear"] = map[string]any{"on": false}
	l.profiles["battery-saver"]["power"] = map[string]any{"mode": "eco"}
	l.profiles["battery-saver"]["fan"] = map[string]any{"mode": "auto"}
	l.profiles["battery-saver"]["battery"] = map[string]any{"limit": true}
}

func toAny(s []string) []any {
	out := make([]any, len(s))
	for i, v := range s {
		out[i] = v
	}
	return out
}

func (l *Laptop) statusGet(args) (any, error) {
	now := time.Now()
	cpu, gpu, sys, rc, rg := l.sensors(now)
	daemon := l.daemonState(now)
	if daemon == nil {
		daemon = "ERROR: the TrueSense daemon is not running (sudo systemctl start truesense)"
	}
	return map[string]any{
		"power_mode": l.mode, "sensors": map[string]any{"cpu_temp": cpu, "gpu_temp": gpu, "sys_temp": sys, "cpu_fan_rpm": rc, "gpu_fan_rpm": rg},
		"fans": l.fansState(), "daemon": daemon, "battery_limit": l.limit80, "usb_charge_stop": l.usb, "overdrive": l.overdrive,
		"boot_sound": l.bootSound, "kb_backlight_timeout": l.kbTimeout, "gpu_mode": l.gpuMode, "rear": l.rearState(), "touchpad": l.touchpad,
		"audio_output": map[string]any{"active": l.audio.output, "jack_plugged": false},
		"ac":           l.ac, "battery_pct": l.batteryPct, "kb_brightness": l.kbBrightness, "kb_look": l.kbLook,
	}, nil
}

var allModes = []string{"quiet", "balanced", "performance", "turbo", "eco"}

func (l *Laptop) powerStatus(args) (any, error) {
	return map[string]any{"mode": l.mode, "supported": allModes, "auto_profile": l.autoMode != "off", "hold_left_s": 0}, nil
}

func (l *Laptop) powerSet(a args) (any, error) {
	m := a.str("mode")
	for _, x := range allModes {
		if x == m {
			l.mode = m
			l.holdUntil = time.Now().Add(30 * time.Minute)
			return m, nil
		}
	}
	return nil, fmt.Errorf("mode: %v", allModes)
}

func (l *Laptop) powerAuto(a args) (any, error) {
	switch v := a.str("action"); v {
	case "off", "native", "smart":
		l.autoMode = v
		l.holdUntil = time.Time{}
	case "on":
		l.autoMode = "smart"
	case "toggle":
		if l.autoMode == "off" {
			l.autoMode = "smart"
		} else {
			l.autoMode = "off"
		}
	case "", "status":
	default:
		return nil, fmt.Errorf("auto: off | native | smart")
	}
	return l.autoMode, nil
}

func (l *Laptop) powerHold(a args) (any, error) {
	l.holdUntil = time.Now().Add(time.Duration(a.num("minutes", 30) * float64(time.Minute)))
	return "holding", nil
}

func (l *Laptop) powerSmart(a args) (any, error) {
	return libVerb(l.smart, a, func(name string) { l.smartProfile = name }, l.smartProfile, func(a args) map[string]any {
		p := a.obj("profile")
		if p == nil {
			return nil
		}
		return with(smartDefaults, p)
	})
}

// libVerb is the list | current | show | use | copy | save | delete protocol of a profile library.
func libVerb(lib *library, a args, use func(name string), current string, build func(a args) map[string]any) (any, error) {
	name := a.str("name")
	switch act := a.str("action"); act {
	case "list", "":
		return lib.names(), nil
	case "current":
		p, err := lib.show(current)
		if err != nil {
			return nil, err
		}
		return map[string]any{"name": current, "profile": p, "curve": p}, nil
	case "show":
		return lib.show(name)
	case "use":
		if _, err := lib.show(name); err != nil && build(a) == nil {
			return nil, err
		}
		use(name)
		return map[string]any{"name": name}, nil
	case "copy":
		return lib.copy(name, a.str("new"))
	case "save":
		p := build(a)
		if p == nil {
			return nil, fmt.Errorf("nothing to save: give the settings of the profile")
		}
		if err := lib.save(name, p); err != nil {
			return nil, err
		}
		return p, nil
	case "delete":
		return name, lib.delete(name)
	default:
		return nil, fmt.Errorf("unknown action %q", act)
	}
}

func (l *Laptop) powerGame(a args) (any, error) {
	switch a.str("action") {
	case "add":
		n := a.str("name")
		if n == "" {
			return nil, fmt.Errorf("usage: truesense power game add PROGRAM MODE")
		}
		l.gameRules[n] = a.str("mode")
	case "remove":
		if _, ok := l.gameRules[a.str("name")]; !ok {
			return nil, fmt.Errorf("no rule for '%s'", a.str("name"))
		}
		delete(l.gameRules, a.str("name"))
	case "clear":
		l.gameRules = map[string]string{}
	}
	rules := map[string]any{}
	for k, v := range l.gameRules {
		rules[k] = v
	}
	return map[string]any{"rules": rules, "running": nil}, nil
}

func (l *Laptop) powerManagers(a args) (any, error) {
	switch a.str("action") {
	case "stop":
		l.managersOn = true
		return map[string]any{"stopped": []any{"power-profiles-daemon.service"}, "failed": []any{}, "cpu": map[string]any{"governor": "powersave", "epp": "balance_performance", "no_turbo": "0"}}, nil
	case "restore":
		l.managersOn = false
		return map[string]any{"restored": []any{"power-profiles-daemon.service"}}, nil
	}
	found := []any{}
	if !l.managersOn {
		found = append(found, map[string]any{"unit": "power-profiles-daemon.service", "active": true, "enabled": true})
	}
	return map[string]any{"on": l.managersOn, "found": found}, nil
}

func (l *Laptop) fan(verb string) handlerFunc {
	return func(a args) (any, error) {
		switch verb {
		case "auto":
			l.fanMode, l.smartFan = "auto", false
			return "fans: firmware auto (they stop when cool)", nil
		case "max":
			l.fanMode, l.smartFan = "max", false
			return "fans: MAX", nil
		case "smart":
			switch a.str("action") {
			case "on":
				l.smartFan = true
			case "off":
				l.smartFan = false
			case "toggle":
				l.smartFan = !l.smartFan
			}
			return l.smartFan, nil
		case "off":
			if !a.flag("confirm") {
				return nil, fmt.Errorf("stopping a fan needs explicit confirmation")
			}
			cpu, gpu, _, _, _ := l.sensors(time.Now())
			if cpu >= 60 || gpu >= 55 {
				return nil, fmt.Errorf("too warm to stop a fan (CPU %d C, GPU %d C; a fan may only be stopped below 60 / 55 C)", cpu, gpu)
			}
			l.fanMode, l.smartFan, l.dutyCPU, l.dutyGPU = "manual", false, 0, 0
			return "fans: OFF", nil
		default:
			c, g := int(a.num("cpu", 40)), int(a.num("gpu", a.num("cpu", 40)))
			for _, v := range []int{c, g} {
				if v != 0 && (v < 20 || v > 100) {
					return nil, fmt.Errorf("manual fan duty is 20..100 %% per fan, or 0 to stop that fan")
				}
				if v == 0 && !a.flag("confirm") {
					return nil, fmt.Errorf("stopping a fan needs explicit confirmation")
				}
			}
			l.fanMode, l.smartFan, l.dutyCPU, l.dutyGPU = "manual", false, c, g
			return fmt.Sprintf("fans: manual %d %% / %d %%", c, g), nil
		}
	}
}

func (l *Laptop) fanCurve(a args) (any, error) {
	if l.NoDaemon && (a.str("action") == "current" || a.str("action") == "use") {
		return nil, errNoDaemon
	}
	return libVerb(l.curves, a, func(name string) { l.curveName = name }, l.curveName, func(a args) map[string]any {
		c := a.obj("curve")
		if c == nil {
			return nil
		}
		return with(map[string]any{"hysteresis": 4.0, "down_rate": 1.5, "predict": 5.0, "couple": 15, "battery_shift": 3.0}, c)
	})
}

func (l *Laptop) gpuSet(a args) (any, error) {
	m := a.str("mode")
	if m != "optimus" && m != "nvidia" && m != "auto" {
		return nil, fmt.Errorf("mode: optimus | nvidia | auto")
	}
	l.gpuMode = m
	return m, nil
}

func (l *Laptop) usbCharge(a args) (any, error) {
	v := int(a.num("level", -1))
	if v != 0 && v != 10 && v != 20 && v != 30 {
		return nil, fmt.Errorf("level: 0 | 10 | 20 | 30")
	}
	l.usb = v
	return v, nil
}

func (l *Laptop) batteryFull(a args) (any, error) {
	switch a.str("action") {
	case "start":
		if !l.ac {
			return nil, fmt.Errorf("plug the charger in first")
		}
		if !l.limit80 {
			return nil, fmt.Errorf("the 80 %% limit is off already: the battery charges to 100 %%")
		}
		l.limit80, l.fullActive = false, true
		return map[string]any{"active": true}, nil
	case "cancel":
		l.limit80, l.fullActive = true, false
		return map[string]any{"active": false}, nil
	}
	return map[string]any{"active": l.fullActive, "limit": l.limit80, "capacity": l.batteryPct, "ac": l.ac}, nil
}

func (l *Laptop) cpuInfo(args) (any, error) {
	d := map[string]any{"pl1_w": 80.0, "pl2_w": 80.0, "rapl_enabled": "1", "user": l.pl["user"], "auto": map[string]any{"normal_w": 80, "gaming_w": 65}}
	if u, ok := l.pl["user"].(map[string]any); ok {
		d["pl1_w"], d["pl2_w"] = u["pl1"], u["pl2"]
	}
	return d, nil
}

func (l *Laptop) cpuLimits(a args) (any, error) {
	if a.flag("auto") {
		l.pl["user"] = nil
		return map[string]any{"pl1_w": 80.0, "pl2_w": 80.0}, nil
	}
	p1, p2 := a.num("pl1", 0), a.num("pl2", 0)
	if p2 < p1 {
		return nil, fmt.Errorf("PL2 (burst) must not be below PL1 (sustained)")
	}
	if (p1 > 100 || p2 > 115) && !a.flag("force") {
		return nil, fmt.Errorf("beyond the danger line: needs force")
	}
	l.pl["user"] = map[string]any{"pl1": p1, "pl2": p2}
	return map[string]any{"pl1_w": p1, "pl2_w": p2}, nil
}

// uvVerb is `cpu undervolt` and `gpu undervolt`: profiles plus apply / reset / boot.
func (l *Laptop) uvVerb(kind string) handlerFunc {
	lib, applied, boot := l.cpuUV, &l.cpuApplied, &l.cpuBoot
	if kind == "gpu" {
		lib, applied, boot = l.gpuUV, &l.gpuApplied, &l.gpuBoot
	}
	danger := func(a args) bool {
		if kind == "cpu" {
			return math.Abs(a.num("core", 0)) > 90 || math.Abs(a.num("cache", 0)) > 70
		}
		return a.num("offset", 0) > 200
	}
	values := func(a args) map[string]any {
		if kind == "cpu" {
			return map[string]any{"core": int(a.num("core", 0)), "cache": int(a.num("cache", 0))}
		}
		return map[string]any{"offset": int(a.num("offset", 0)), "cap": int(a.num("cap", 0))}
	}
	return func(a args) (any, error) {
		name := a.str("name")
		switch act := a.str("action"); act {
		case "get":
			if kind == "cpu" {
				r := map[string]any{"core_mv": 0, "cache_mv": 0, "applied": *applied, "boot": *boot, "pending": false}
				if *applied != nil {
					r["core_mv"], r["cache_mv"] = (*applied)["core"], (*applied)["cache"]
				}
				return r, nil
			}
			r := map[string]any{"offset_mhz": 0, "range_mhz": []any{-1000, 1000}, "applied": *applied, "boot": *boot}
			if *applied != nil {
				r["offset_mhz"] = (*applied)["offset"]
			}
			return r, nil
		case "apply":
			if !a.flag("confirm") {
				return nil, fmt.Errorf("applying an undervolt needs explicit confirmation")
			}
			if danger(a) && !a.flag("force") {
				return nil, fmt.Errorf("beyond the danger line: needs force")
			}
			v := values(a)
			v["name"] = name
			*applied = v
			if kind == "cpu" {
				return map[string]any{"core_mv": v["core"], "cache_mv": v["cache"]}, nil
			}
			return map[string]any{"offset_mhz": v["offset"], "cap_mhz": v["cap"]}, nil
		case "reset":
			*applied = nil
			if kind == "cpu" {
				return map[string]any{"core_mv": 0, "cache_mv": 0}, nil
			}
			return map[string]any{"offset_mhz": 0, "cap_mhz": 0}, nil
		case "boot":
			if name == "off" || name == "" {
				*boot = nil
				return nil, nil
			}
			if !a.flag("confirm") {
				return nil, fmt.Errorf("applying an undervolt at boot needs explicit confirmation")
			}
			v := values(a)
			v["name"] = name
			*boot = v
			return v, nil
		default:
			return libVerb(lib, a, func(string) {}, "native", func(a args) map[string]any {
				if kind == "cpu" && a.has("core") {
					return with(map[string]any{"description": ""}, values(a))
				}
				if kind == "gpu" && a.has("offset") {
					return with(map[string]any{"description": ""}, values(a))
				}
				return nil
			})
		}
	}
}

func (l *Laptop) resetAll(args) (any, error) {
	l.curveName, l.smartProfile, l.autoMode, l.fanMode, l.smartFan = "native", "native", "native", "auto", false
	l.pl["user"], l.cpuApplied, l.gpuApplied, l.cpuBoot, l.gpuBoot = nil, nil, nil, nil, nil
	l.keysProfileName, l.keysCurrent = "native", cloneMap(l.keys.data["native"])
	l.cells = stockCells()
	l.rear = map[string]any{"mode": "breathing", "speed": 3, "brightness": 100, "direction": 1, "color": "3723c7", "zones": nil}
	steps := []string{"fan curve", "Smart auto profile", "fans", "mode control", "PREDATOR key", "power limits", "CPU undervolt at boot", "CPU undervolt", "GPU undervolt at boot", "GPU undervolt", "keyboard lighting", "rear light"}
	out := make([]any, len(steps))
	for i, s := range steps {
		out[i] = map[string]any{"step": s, "ok": true}
	}
	return map[string]any{"steps": out}, nil
}

func sortedKeys(m map[string]map[string]any) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}
