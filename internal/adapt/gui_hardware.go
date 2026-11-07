package adapt

import (
	"context"
	"fmt"

	"github.com/Order52/truesense/internal/api"
)

func (b *guiBackend) registerHardware() {
	h := b.handlers
	h["battery/status"] = func(context.Context, args) (any, error) { return b.a.BatteryStatus() }
	h["battery/limit"] = func(_ context.Context, a args) (any, error) {
		return onOff(a, b.a.BatteryLimit, b.a.SetBatteryLimit)
	}
	h["battery/calibration"] = func(_ context.Context, a args) (any, error) {
		return onOff(a, b.a.Calibration, b.a.SetCalibration)
	}
	h["battery/usb-charge"] = func(_ context.Context, a args) (any, error) {
		level, err := a.mustInt("level")
		if err != nil {
			return nil, err
		}
		return b.a.SetUSBCharge(level)
	}
	h["battery/full"] = func(_ context.Context, a args) (any, error) {
		action, err := a.action("get")
		if err != nil {
			return nil, err
		}
		switch action {
		case "get", "status":
			return b.a.ChargeFullStatus()
		case "start":
			return b.a.StartChargeFull()
		case "cancel":
			return b.a.CancelChargeFull()
		}
		return nil, fmt.Errorf("action: get | start | cancel")
	}
	h["display/overdrive"] = func(_ context.Context, a args) (any, error) {
		return onOff(a, func() (bool, error) {
			v, err := b.a.Overdrive()
			return v != nil && *v, err
		}, b.a.SetOverdrive)
	}
	h["boot/sound"] = func(_ context.Context, a args) (any, error) {
		return onOff(a, b.a.BootSound, b.a.SetBootSound)
	}
	h["gpu/set"] = func(_ context.Context, a args) (any, error) {
		mode, err := a.mustStr("mode")
		if err != nil {
			return nil, err
		}
		return b.a.SetGPUMode(mode)
	}
	h["gpu/undervolt"] = b.gpuUndervolt
	h["cpu/info"] = func(context.Context, args) (any, error) { return b.a.CPULimits() }
	h["cpu/limits"] = b.cpuLimits
	h["cpu/undervolt"] = b.cpuUndervolt
	h["touchpad/get"] = func(context.Context, args) (any, error) {
		v, err := b.a.Touchpad()
		if v == nil {
			return nil, err
		}
		return *v, err
	}
	h["touchpad/set"] = func(_ context.Context, a args) (any, error) {
		action, err := a.action("toggle")
		if err != nil {
			return nil, err
		}
		cur, err := b.a.Touchpad()
		if err != nil {
			return nil, err
		}
		on := cur != nil && *cur
		switch action {
		case "toggle":
			on = !on
		case "on":
			on = true
		case "off":
			on = false
		default:
			return nil, fmt.Errorf("action: on | off | toggle")
		}
		return b.a.SetTouchpad(on)
	}
}

func (b *guiBackend) cpuLimits(_ context.Context, a args) (any, error) {
	auto, err := a.boolean("auto")
	if err != nil {
		return nil, err
	}
	if auto {
		d, err := b.a.ResetCPULimits()
		return map[string]any{"pl1_w": d.PL1W, "pl2_w": d.PL2W}, err
	}
	pl1, err := a.floatOpt("pl1")
	if err != nil {
		return nil, err
	}
	pl2, err := a.floatOpt("pl2")
	if err != nil {
		return nil, err
	}
	force, err := a.boolean("force")
	if err != nil {
		return nil, err
	}
	if pl1 == nil {
		return nil, fmt.Errorf("pl1 is needed (or auto)")
	}
	second := *pl1
	if pl2 != nil {
		second = *pl2
	}
	return b.a.SetCPULimits(*pl1, second, force)
}

// uvArgs are the members every undervolt command may carry.
type uvArgs struct {
	action, name, newName string
	req                   api.UndervoltRequest
}

func readUV(a args) (uvArgs, error) {
	var u uvArgs
	var err error
	if u.action, err = a.action("get"); err != nil {
		return u, err
	}
	if u.name, err = a.str("name"); err != nil {
		return u, err
	}
	if u.newName, err = a.str("new"); err != nil {
		return u, err
	}
	if u.req.Confirm, err = a.boolean("confirm"); err != nil {
		return u, err
	}
	u.req.Force, err = a.boolean("force")
	return u, err
}

func (b *guiBackend) gpuUndervolt(_ context.Context, a args) (any, error) {
	u, err := readUV(a)
	if err != nil {
		return nil, err
	}
	offset, err := a.intOpt("offset")
	if err != nil {
		return nil, err
	}
	capMHz, err := a.intOpt("cap")
	if err != nil {
		return nil, err
	}
	cp := 0
	if capMHz != nil {
		cp = *capMHz
	}
	switch u.action {
	case "get":
		return b.a.GPUUndervoltState()
	case "list":
		l, err := b.a.GPUUndervolts()
		return names(l.Names), err
	case "show":
		n := orDefault(u.name, "truesense")
		p, err := b.a.GPUUndervoltProfile(n)
		if err != nil {
			return nil, err
		}
		return withBuiltin(p, api.GPUUndervoltIsBuiltin(n))
	case "use":
		return b.a.ApplyGPUUndervolt(u.name, u.req)
	case "apply":
		if offset == nil {
			return nil, fmt.Errorf("offset is needed")
		}
		return b.a.ApplyGPUUndervoltValues(orDefault(u.name, "manual"), *offset, cp, u.req)
	case "reset":
		return b.a.ResetGPUUndervolt()
	case "copy":
		return b.a.CopyGPUUndervolt(u.name, u.newName)
	case "save":
		return b.a.SaveGPUUndervolt(u.name, offset, cp)
	case "delete":
		return u.name, b.a.DeleteGPUUndervolt(u.name)
	case "boot":
		if u.name == "" || u.name == "off" {
			return b.a.SetGPUUndervoltBoot("off", u.req)
		}
		if offset != nil {
			return b.a.SetGPUUndervoltBootValues(u.name, *offset, cp, u.req)
		}
		return b.a.SetGPUUndervoltBoot(u.name, u.req)
	}
	return nil, fmt.Errorf("action: get | list | show | use | apply | reset | copy | save | delete | boot")
}

func (b *guiBackend) cpuUndervolt(_ context.Context, a args) (any, error) {
	u, err := readUV(a)
	if err != nil {
		return nil, err
	}
	core, err := a.intOpt("core")
	if err != nil {
		return nil, err
	}
	cache, err := a.intOpt("cache")
	if err != nil {
		return nil, err
	}
	switch u.action {
	case "get":
		return b.a.CPUUndervoltState()
	case "list":
		l, err := b.a.CPUUndervolts()
		return names(l.Names), err
	case "show":
		n := orDefault(u.name, "truesense")
		p, err := b.a.CPUUndervoltProfile(n)
		if err != nil {
			return nil, err
		}
		return withBuiltin(p, api.CPUUndervoltIsBuiltin(n))
	case "use":
		return b.a.ApplyCPUUndervolt(u.name, u.req)
	case "apply":
		if core == nil || cache == nil {
			return nil, fmt.Errorf("core and cache are needed")
		}
		return b.a.ApplyCPUUndervoltValues(orDefault(u.name, "manual"), *core, *cache, u.req)
	case "reset":
		return b.a.ResetCPUUndervolt()
	case "copy":
		return b.a.CopyCPUUndervolt(u.name, u.newName)
	case "save":
		return b.a.SaveCPUUndervolt(u.name, core, cache)
	case "delete":
		return u.name, b.a.DeleteCPUUndervolt(u.name)
	case "boot":
		if u.name == "" || u.name == "off" {
			return b.a.SetCPUUndervoltBoot("off", u.req)
		}
		if core != nil && cache != nil {
			return b.a.SetCPUUndervoltBootValues(u.name, *core, *cache, u.req)
		}
		return b.a.SetCPUUndervoltBoot(u.name, u.req)
	}
	return nil, fmt.Errorf("action: get | list | show | use | apply | reset | copy | save | delete | boot")
}

func orDefault(s, def string) string {
	if s == "" {
		return def
	}
	return s
}
