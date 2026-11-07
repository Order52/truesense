package adapt

import (
	"context"
	"fmt"

	"github.com/Order52/truesense/internal/api"
	"github.com/Order52/truesense/internal/keybinds"
)

func (b *guiBackend) registerKeys() {
	h := b.handlers
	h["keys/profile"] = b.keyProfile
	h["keys/test"] = func(ctx context.Context, a args) (any, error) {
		action, err := a.mustStr("action")
		if err != nil {
			return nil, err
		}
		return b.a.TestAction(ctx, action)
	}
	h["profile/list"] = func(context.Context, args) (any, error) { return b.a.Profiles(), nil }
	h["profile/show"] = func(_ context.Context, a args) (any, error) {
		name, err := a.mustStr("name")
		if err != nil {
			return nil, err
		}
		return b.a.Profile(name)
	}
	h["profile/apply"] = func(ctx context.Context, a args) (any, error) {
		name, err := a.mustStr("name")
		if err != nil {
			return nil, err
		}
		return b.a.ApplyProfile(ctx, name)
	}
	h["profile/next"] = func(ctx context.Context, _ args) (any, error) { return b.a.NextProfile(ctx) }
	h["profile/save"] = func(ctx context.Context, a args) (any, error) {
		name, err := a.mustStr("name")
		if err != nil {
			return nil, err
		}
		description, err := a.str("description")
		if err != nil {
			return nil, err
		}
		return b.a.SaveProfile(ctx, name, description)
	}
	h["profile/delete"] = func(_ context.Context, a args) (any, error) {
		name, err := a.mustStr("name")
		if err != nil {
			return nil, err
		}
		return "deleted " + name, b.a.DeleteProfile(name)
	}
	h["profile/current"] = func(context.Context, args) (any, error) {
		var name any
		if n := b.a.CurrentProfile(); n != "" {
			name = n
		}
		return map[string]any{"name": name}, nil
	}
}

func (b *guiBackend) keyProfile(_ context.Context, a args) (any, error) {
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
	switch action {
	case "list":
		l, err := b.a.KeyProfiles()
		return names(l.Names), err
	case "current":
		return b.a.KeyProfileInUse()
	case "show":
		n := orDefault(name, "default")
		p, err := b.a.KeyProfile(n)
		if err != nil {
			return nil, err
		}
		_, builtin := keybinds.Builtin(n)
		return withBuiltin(p, builtin)
	case "use":
		return name, b.a.UseKeyProfile(name)
	case "copy":
		return b.a.CopyKeyProfile(name, newName)
	case "save":
		edit := api.KeyProfileEdit{Actions: map[string]string{}}
		if edit.From, err = a.str("from"); err != nil {
			return nil, err
		}
		for _, g := range keybinds.Gestures() {
			v, err := a.str(g)
			if err != nil {
				return nil, err
			}
			if v != "" {
				edit.Actions[g] = v
			}
		}
		if edit.LongMS, err = a.intOpt("long_ms"); err != nil {
			return nil, err
		}
		if edit.MultiMS, err = a.intOpt("multi_ms"); err != nil {
			return nil, err
		}
		return b.a.SaveKeyProfile(name, edit)
	case "delete":
		return name, b.a.DeleteKeyProfile(name)
	}
	return nil, fmt.Errorf("action: list | current | show | use | copy | save | delete")
}

func (b *guiBackend) registerAudio() {
	h := b.handlers
	h["audio/output"] = func(ctx context.Context, a args) (any, error) {
		action, err := a.action("status")
		if err != nil {
			return nil, err
		}
		switch action {
		case "status":
		case "speakers", "headphones":
			if _, err := b.a.SetAudioOutput(ctx, action); err != nil {
				return nil, err
			}
		default:
			return nil, fmt.Errorf("output: speakers | headphones | status")
		}
		return b.a.AudioOutput(ctx)
	}
	h["audio/speaker-filter"] = func(ctx context.Context, a args) (any, error) {
		return onOff(a, func() (bool, error) { return b.a.SpeakerFilter(), nil },
			func(on bool) (bool, error) { return b.a.SetSpeakerFilter(ctx, on) }, "on", "off", "status")
	}
	h["audio/dts"] = b.dts
	h["audio/jack"] = func(ctx context.Context, a args) (any, error) {
		use, err := a.str("use")
		if err != nil {
			return nil, err
		}
		switch use {
		case "":
		case "auto", "headphones", "headset":
			if err := b.a.SetJack(ctx, use); err != nil {
				return nil, err
			}
		default:
			return nil, fmt.Errorf("jack: auto | headphones | headset")
		}
		return b.a.Jack(ctx), nil
	}
	h["audio/mic"] = b.mic
	h["audio/voice"] = b.voice
	h["audio/sound"] = func(ctx context.Context, _ args) (any, error) { return b.a.SoundOutput(ctx) }
	h["audio/repair"] = func(ctx context.Context, _ args) (any, error) {
		r, err := b.a.RepairSound(ctx)
		return map[string]any{"fixed": r.Fixed, "changed": r.Changed, "steps": names(r.Steps)}, err
	}
}

func (b *guiBackend) dts(ctx context.Context, a args) (any, error) {
	action, err := a.action("status")
	if err != nil {
		return nil, err
	}
	name, err := a.str("name")
	if err != nil {
		return nil, err
	}
	switch action {
	case "status":
	case "on", "off":
		if _, err := b.a.SetDTSPower(ctx, action == "on"); err != nil {
			return nil, err
		}
	case "mode":
		if _, err := b.a.SetDTSMode(ctx, name); err != nil {
			return nil, err
		}
	case "all":
		done, err := b.a.DTSEverywhere(ctx, name)
		return map[string][]string{"done": names(done)}, err
	default:
		return nil, fmt.Errorf("action: status | on | off | mode | all")
	}
	return b.a.DTSInfo(ctx)
}

func (b *guiBackend) mic(ctx context.Context, a args) (any, error) {
	action, err := a.action("status")
	if err != nil {
		return nil, err
	}
	value, err := a.intOpt("value")
	if err != nil {
		return nil, err
	}
	switch action {
	case "status":
	case "mute", "unmute":
		if err := b.a.SetMicMute(ctx, action == "mute", false); err != nil {
			return nil, err
		}
	case "volume":
		if value == nil {
			return b.a.MicVolume(ctx), nil
		}
		return b.a.SetMicVolume(ctx, *value)
	case "boost":
		if value != nil {
			if err := b.a.SetHeadsetBoost(ctx, *value); err != nil {
				return nil, err
			}
		}
		return b.a.HeadsetBoost(ctx)
	default:
		return nil, fmt.Errorf("action: status | mute | unmute | volume | boost")
	}
	return b.a.MicStatus(ctx)
}

func (b *guiBackend) voice(ctx context.Context, a args) (any, error) {
	action, err := a.action("status")
	if err != nil {
		return nil, err
	}
	first, err := a.str("a")
	if err != nil {
		return nil, err
	}
	second, err := a.str("b")
	if err != nil {
		return nil, err
	}
	switch action {
	case "status":
	case "set":
		if _, err := b.a.SetVoice(ctx, first, second); err != nil {
			return nil, err
		}
	case "notify":
		if first != "on" && first != "off" {
			return nil, fmt.Errorf("usage: truesense audio voice notify on|off")
		}
		if err := b.a.SetVoiceNotify(first == "on"); err != nil {
			return nil, err
		}
	default:
		return nil, fmt.Errorf("action: status | set | notify")
	}
	return b.a.VoiceStatus(ctx), nil
}
