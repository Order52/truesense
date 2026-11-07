package adapt

import (
	"context"
	"fmt"

	"github.com/Order52/truesense/internal/api"
	"github.com/Order52/truesense/internal/looks"
)

func (b *guiBackend) registerKeyboard() {
	h := b.handlers
	h["keyboard/status"] = func(context.Context, args) (any, error) { return b.a.KeyboardStatus() }
	for _, v := range []string{"on", "off"} {
		h["keyboard/"+v] = func(ctx context.Context, _ args) (any, error) { return b.a.KeyboardPower(ctx, v) }
	}
	h["keyboard/brightness"] = func(_ context.Context, a args) (any, error) {
		level, err := a.mustInt("level")
		if err != nil {
			return nil, err
		}
		return level, b.a.SetKeyboardBrightness(level)
	}
	h["keyboard/effect"] = func(_ context.Context, a args) (any, error) {
		name, err := a.mustStr("name")
		if err != nil {
			return nil, err
		}
		color, err := a.str("color")
		if err != nil {
			return nil, err
		}
		speed, err := a.intOpt("speed")
		if err != nil {
			return nil, err
		}
		bright, err := a.intOpt("brightness")
		if err != nil {
			return nil, err
		}
		dir, err := a.intOpt("direction")
		if err != nil {
			return nil, err
		}
		spec := api.EffectSpec{Name: name, Color: orDefault(color, "00aec7"), Speed: 5, Brightness: bright}
		if speed != nil {
			spec.Speed = *speed
		}
		if dir != nil {
			spec.Direction = *dir
		}
		return "sent", b.a.SetKeyboardEffect(spec)
	}
	h["keyboard/color"] = func(_ context.Context, a args) (any, error) {
		color, err := a.mustStr("color")
		if err != nil {
			return nil, err
		}
		return "sent", b.a.SetKeyboardColor(color)
	}
	h["keyboard/speed"] = func(_ context.Context, a args) (any, error) {
		level, err := a.mustInt("level")
		if err != nil {
			return nil, err
		}
		if err := b.a.SetKeyboardSpeed(level); err != nil {
			return nil, err
		}
		return fmt.Sprintf("keyboard effect speed: %d / 12", level), nil
	}
	h["keyboard/operating"] = func(_ context.Context, a args) (any, error) {
		return onOff(a, b.a.OperatingFollow, b.a.SetOperatingFollow, "on", "off", "status")
	}
	h["keyboard/timeout"] = func(_ context.Context, a args) (any, error) {
		return onOff(a, b.a.KeyboardTimeout, b.a.SetKeyboardTimeout)
	}
	h["keyboard/restore"] = func(context.Context, args) (any, error) { return "sent", b.a.RestoreKeyboard() }
	h["keyboard/layout"] = func(context.Context, args) (any, error) { return b.a.KeyLayout(), nil }
	h["keyboard/cells"] = func(context.Context, args) (any, error) {
		return map[string][]string{"cells": b.a.KeyboardCells()}, nil
	}
	h["keyboard/set-cells"] = func(_ context.Context, a args) (any, error) {
		cells, err := a.strings("cells")
		if err != nil {
			return nil, err
		}
		if cells == nil {
			return nil, fmt.Errorf("cells is needed")
		}
		bright, err := a.intOpt("brightness")
		if err != nil {
			return nil, err
		}
		return "sent", b.a.SetKeyboardCells(cells, bright)
	}
	h["keyboard/fx"] = b.fx
	h["keyboard/design"] = b.design
	h["keyboard/profile"] = b.lightingProfile
}

func (b *guiBackend) fx(_ context.Context, a args) (any, error) {
	mode, err := a.mustStr("mode")
	if err != nil {
		return nil, err
	}
	speed, err := a.intOpt("speed")
	if err != nil {
		return nil, err
	}
	var cells []int
	if a.has("cells") {
		if err := a.decode("cells", &cells); err != nil {
			return nil, err
		}
	}
	base, err := a.strings("base")
	if err != nil {
		return nil, err
	}
	sp := 5
	if speed != nil {
		sp = *speed
	}
	return b.a.SetKeyboardFXCells(mode, sp, cells, base)
}

func (b *guiBackend) design(_ context.Context, a args) (any, error) {
	action, err := a.action("list")
	if err != nil {
		return nil, err
	}
	name, err := a.str("name")
	if err != nil {
		return nil, err
	}
	cells, err := a.strings("cells")
	if err != nil {
		return nil, err
	}
	switch action {
	case "list":
		return b.a.Designs(), nil
	case "show":
		return b.a.ShowDesign(name)
	case "save":
		return b.a.SaveDesignCells(name, cells)
	case "apply":
		return "design " + name + " applied", b.a.ApplyDesign(name, nil)
	}
	return nil, fmt.Errorf("action: list | show | save | apply")
}

func (b *guiBackend) lightingProfile(_ context.Context, a args) (any, error) {
	action, err := a.action("list")
	if err != nil {
		return nil, err
	}
	name, err := a.str("name")
	if err != nil {
		return nil, err
	}
	cells, err := a.strings("cells")
	if err != nil {
		return nil, err
	}
	switch action {
	case "list":
		return names(b.a.LightingProfileNames()), nil
	case "save":
		return b.a.SaveLightingProfileCells(name, cells)
	case "load", "delete":
		r, err := b.a.LightingProfile(action, name, nil)
		return r.Message, err
	}
	return nil, fmt.Errorf("action: list | load | save | delete")
}

func (b *guiBackend) registerLooks() {
	h := b.handlers
	h["rear/status"] = func(context.Context, args) (any, error) { return b.a.RearStatus() }
	for _, v := range []string{"on", "off"} {
		h["rear/"+v] = func(ctx context.Context, _ args) (any, error) { return b.a.RearPower(ctx, v) }
	}
	h["rear/set"] = b.rearSet
	h["look/list"] = func(context.Context, args) (any, error) { return names(b.a.Looks()), nil }
	h["look/show"] = func(_ context.Context, a args) (any, error) {
		name, err := a.mustStr("name")
		if err != nil {
			return nil, err
		}
		l, err := b.a.Look(name)
		if err != nil {
			return nil, err
		}
		return withBuiltin(l, b.a.LookIsBuiltin(name))
	}
	h["look/save"] = func(_ context.Context, a args) (any, error) {
		name, err := a.mustStr("name")
		if err != nil {
			return nil, err
		}
		var l *looks.Look
		if a.has("look") {
			l = &looks.Look{}
			if err := a.decode("look", l); err != nil {
				return nil, err
			}
		}
		return b.a.SaveLookValue(name, l)
	}
	h["look/apply"] = func(_ context.Context, a args) (any, error) {
		name, err := a.mustStr("name")
		if err != nil {
			return nil, err
		}
		done, err := b.a.ApplyLook(name)
		return names(done), err
	}
	h["look/delete"] = func(_ context.Context, a args) (any, error) {
		name, err := a.mustStr("name")
		if err != nil {
			return nil, err
		}
		return name, b.a.DeleteLook(name)
	}
	h["look/copy"] = func(_ context.Context, a args) (any, error) {
		name, err := a.mustStr("name")
		if err != nil {
			return nil, err
		}
		newName, err := a.str("new")
		if err != nil {
			return nil, err
		}
		return b.a.CopyLook(name, newName)
	}
	h["startup/show"] = func(context.Context, args) (any, error) { return b.a.Startup() }
	h["startup/set"] = func(_ context.Context, a args) (any, error) {
		target, err := a.mustStr("target")
		if err != nil {
			return nil, err
		}
		value, err := a.str("value")
		if err != nil {
			return nil, err
		}
		return b.a.SetStartup(target, value)
	}
	h["startup/apply"] = func(ctx context.Context, _ args) (any, error) {
		done, err := b.a.ApplyStartup(ctx)
		return names(done), err
	}
	h["startup/boot"] = func(_ context.Context, a args) (any, error) {
		what, err := a.str("what")
		if err != nil {
			return nil, err
		}
		switch what {
		case "":
			return b.a.BootLook()
		case "clear":
			return nil, b.a.ClearBootLook()
		}
		return b.a.SetBootLook(what)
	}
}

func (b *guiBackend) rearSet(_ context.Context, a args) (any, error) {
	effect, err := a.mustStr("effect")
	if err != nil {
		return nil, err
	}
	spec := api.DefaultRearSpec(effect)
	if c, err := a.str("color"); err != nil {
		return nil, err
	} else if c != "" {
		spec.Color = c
	}
	for key, dst := range map[string]*int{"speed": &spec.Speed, "brightness": &spec.Brightness, "direction": &spec.Direction} {
		v, err := a.intOpt(key)
		if err != nil {
			return nil, err
		}
		if v != nil {
			*dst = *v
		}
	}
	if spec.Zones, err = a.strings("zones"); err != nil {
		return nil, err
	}
	verify := true
	if a.has("verify") {
		if err := a.decode("verify", &verify); err != nil {
			return nil, err
		}
	}
	if !verify {
		return b.a.SetRearNoVerify(spec)
	}
	if _, err := b.a.SetRear(spec); err != nil {
		return nil, err
	}
	return b.a.RearStatus()
}
