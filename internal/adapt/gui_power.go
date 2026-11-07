package adapt

import (
	"context"
	"fmt"

	"github.com/Order52/truesense/internal/api"
	"github.com/Order52/truesense/internal/curves"
)

func (b *guiBackend) registerStatus() {
	h := b.handlers
	h["status/get"] = func(ctx context.Context, _ args) (any, error) { return b.a.StatusFull(ctx), nil }
	h["version/get"] = func(context.Context, args) (any, error) {
		return map[string]string{"truesense": b.a.Version()}, nil
	}
}

func (b *guiBackend) registerPower() {
	h := b.handlers
	h["power/status"] = func(context.Context, args) (any, error) { return b.a.PowerStatus() }
	h["power/set"] = func(_ context.Context, a args) (any, error) {
		mode, err := a.mustStr("mode")
		if err != nil {
			return nil, err
		}
		return b.a.SetPower(mode, 30) // held 30 min against the automatic profile
	}
	h["power/auto"] = func(_ context.Context, a args) (any, error) {
		action, err := a.action("status")
		if err != nil {
			return nil, err
		}
		return b.a.ApplyAutoMode(action)
	}
	h["power/release"] = func(context.Context, args) (any, error) {
		return api.MsgHoldReleased, b.a.ReleaseHold()
	}
	h["power/hold"] = func(_ context.Context, a args) (any, error) {
		m, err := a.floatOpt("minutes")
		if err != nil {
			return nil, err
		}
		minutes := 30.0
		if m != nil {
			minutes = *m
		}
		return fmt.Sprintf("holding the current power mode for %g min", minutes), b.a.HoldPower(minutes)
	}
	h["power/game"] = func(_ context.Context, a args) (any, error) {
		action, err := a.action("list")
		if err != nil {
			return nil, err
		}
		name, err := a.str("name")
		if err != nil {
			return nil, err
		}
		mode, err := a.str("mode")
		if err != nil {
			return nil, err
		}
		return b.a.GameRules(action, name, mode)
	}
	h["power/managers"] = func(_ context.Context, a args) (any, error) {
		action, err := a.action("list")
		if err != nil {
			return nil, err
		}
		switch action {
		case "list":
			return b.a.PowerManagersStatus()
		case "stop":
			return b.a.StopPowerManagers()
		case "restore":
			return b.a.RestorePowerManagers()
		}
		return nil, fmt.Errorf("action: list | stop | restore")
	}
	h["power/smart"] = b.smart
}

func (b *guiBackend) smart(_ context.Context, a args) (any, error) {
	action, err := a.action("current")
	if err != nil {
		return nil, err
	}
	name, err := a.str("name")
	if err != nil {
		return nil, err
	}
	newName, err := a.str("new")
	if err != nil {
		return nil, err
	}
	var settings map[string]any
	if a.has("profile") {
		if err := a.decode("profile", &settings); err != nil {
			return nil, err
		}
	}
	switch action {
	case "list":
		l, err := b.a.SmartProfiles()
		return names(l.Names), err
	case "current":
		d, err := b.a.SmartProfileInUse()
		return map[string]any{"name": d.Name, "profile": d.Profile}, err
	case "show":
		n := name
		if n == "" {
			n = "truesense"
		}
		p, err := b.a.SmartProfile(n)
		if err != nil {
			return nil, err
		}
		return withBuiltin(p, api.SmartProfileIsBuiltin(n))
	case "use":
		var used string
		if settings != nil {
			used, err = b.a.UseSmartProfileData(name, settings)
		} else {
			used, err = b.a.UseSmartProfile(name)
		}
		return map[string]string{"name": used}, err
	case "copy":
		return b.a.CopySmartProfile(name, newName)
	case "save":
		if settings == nil {
			return nil, fmt.Errorf("profile: the settings to save are needed")
		}
		return b.a.SaveSmartProfileData(name, settings)
	case "delete":
		return name, b.a.DeleteSmartProfile(name)
	}
	return nil, fmt.Errorf("action: list | current | show | use | copy | save | delete")
}

func (b *guiBackend) registerFan() {
	h := b.handlers
	h["fan/auto"] = func(context.Context, args) (any, error) { return api.MsgFanAuto, b.a.FanAuto() }
	h["fan/max"] = func(context.Context, args) (any, error) { return api.MsgFanMax, b.a.FanMax() }
	h["fan/smart"] = func(_ context.Context, a args) (any, error) {
		return onOff(a, b.a.SmartFan, b.a.SetSmartFan)
	}
	h["fan/off"] = func(_ context.Context, a args) (any, error) {
		confirm, err := a.boolean("confirm")
		if err != nil {
			return nil, err
		}
		return api.MsgFanOff, b.a.FanOff(confirm)
	}
	h["fan/set"] = func(_ context.Context, a args) (any, error) {
		cpu, err := a.mustInt("cpu")
		if err != nil {
			return nil, err
		}
		gpu, err := a.intOpt("gpu")
		if err != nil {
			return nil, err
		}
		confirm, err := a.boolean("confirm")
		if err != nil {
			return nil, err
		}
		if err := b.a.FanSet(cpu, gpu, confirm); err != nil {
			return nil, err
		}
		g := cpu
		if gpu != nil {
			g = *gpu
		}
		text := fmt.Sprintf("fans: manual %d %% / %d %%", cpu, g)
		if cpu == 0 || g == 0 {
			text += api.FanStopNote
		}
		return text, nil
	}
	h["fan/curve"] = b.curve
}

func (b *guiBackend) curve(_ context.Context, a args) (any, error) {
	action, err := a.action("current")
	if err != nil {
		return nil, err
	}
	name, err := a.str("name")
	if err != nil {
		return nil, err
	}
	newName, err := a.str("new")
	if err != nil {
		return nil, err
	}
	var edited *curves.Curve
	if a.has("curve") {
		edited = &curves.Curve{}
		if err := a.decode("curve", edited); err != nil {
			return nil, err
		}
	}
	switch action {
	case "list":
		l, err := b.a.FanCurves()
		return names(l.Names), err
	case "current":
		d, err := b.a.FanCurveInUse()
		return map[string]any{"name": d.Name, "curve": d.Curve}, err
	case "show":
		n := name
		if n == "" {
			n = "truesense"
		}
		c, err := b.a.FanCurve(n)
		if err != nil {
			return nil, err
		}
		return withBuiltin(c, api.FanCurveIsBuiltin(n))
	case "use":
		var used string
		if edited != nil {
			used, err = b.a.UseFanCurveData(name, *edited)
		} else {
			used, err = b.a.UseFanCurve(name)
		}
		return map[string]string{"name": used}, err
	case "copy":
		return b.a.CopyFanCurve(name, newName)
	case "save":
		if edited == nil {
			return nil, fmt.Errorf("curve: the curve to save is needed")
		}
		return b.a.SaveFanCurveValue(name, *edited)
	case "delete":
		return name, b.a.DeleteFanCurve(name)
	}
	return nil, fmt.Errorf("action: list | current | show | use | copy | save | delete")
}
