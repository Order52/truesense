package guitest

import (
	"fmt"
	"time"
)

type audioState struct {
	output      string
	speakerFilt bool
	dts         bool
	dtsMode     string
	jack        string
	mute        bool
	volume      int
	boost       int
	voiceHead   string
	voiceArr    string
	notify      bool
	soundOK     bool
}

func (l *Laptop) initAudio() {
	l.audio = audioState{output: "speakers", dts: true, dtsMode: "Music", jack: "auto", volume: 100, voiceHead: "off", voiceArr: "conference", notify: true, soundOK: true}
}

func (l *Laptop) audioVerb(verb string) handlerFunc {
	return func(a args) (any, error) {
		s := &l.audio
		switch verb {
		case "output":
			switch v := a.str("action"); v {
			case "speakers", "headphones":
				s.output = v
			case "", "status":
			default:
				return nil, fmt.Errorf("output: speakers | headphones | status")
			}
			return map[string]any{"active": s.output, "jack_plugged": false}, nil
		case "speaker-filter":
			return l.boolVerb(&s.speakerFilt)(a)
		case "dts":
			switch act := a.str("action"); act {
			case "on":
				s.dts = true
			case "off":
				s.dts = false
			case "mode":
				s.dtsMode = a.str("name")
			case "all":
				return map[string]any{"done": []any{"Speakers: " + s.dtsMode, "USB headset: " + s.dtsMode}}, nil
			}
			return map[string]any{"on": s.dts, "mode": s.dtsMode, "devices": []any{
				map[string]any{"label": "Speakers", "kind": "builtin", "profile": s.dtsMode, "current": s.output == "speakers"},
				map[string]any{"label": "USB headset", "kind": "usb", "profile": s.dtsMode, "current": s.output == "headphones"},
			}}, nil
		case "jack":
			if u := a.str("use"); u != "" {
				if u != "auto" && u != "headphones" && u != "headset" {
					return nil, fmt.Errorf("jack: auto | headphones | headset")
				}
				s.jack = u
			}
			return map[string]any{"headphones": false, "headset_mic": false, "raw": map[string]any{"HeadphoneJack": "off", "HeadsetMicJack": "off"}, "setting": s.jack, "microphone_in_use": "array"}, nil
		case "mic":
			switch act := a.str("action"); act {
			case "mute":
				s.mute = true
			case "unmute":
				s.mute = false
			case "volume":
				s.volume = int(a.num("value", 100))
				return s.volume, nil
			case "boost":
				if a.has("value") {
					s.boost = int(a.num("value", 0))
				}
				return s.boost, nil
			}
			return map[string]any{"default": "truesense_pv_array", "muted": s.mute, "volume": s.volume, "boost": s.boost, "sources": map[string]any{}}, nil
		case "voice":
			switch act := a.str("action"); act {
			case "set":
				dev, mode := a.str("a"), a.str("b")
				if mode != "off" && mode != "conference" && mode != "personal" {
					return nil, fmt.Errorf("usage: truesense audio voice set headset|array off|conference|personal")
				}
				switch dev {
				case "headset":
					s.voiceHead = mode
				case "array":
					s.voiceArr = mode
				default:
					return nil, fmt.Errorf("device: headset | array")
				}
			case "notify":
				s.notify = a.str("a") == "on"
			}
			return map[string]any{"modes": map[string]any{"headset": s.voiceHead, "array": s.voiceArr}, "jack": false, "default_source": "truesense_pv_array", "notify": s.notify}, nil
		case "sound":
			if s.soundOK {
				return map[string]any{"ok": true, "problem": "", "sinks": []any{"alsa_output.pci-0000_00_1f.3.analog-stereo"}}, nil
			}
			return map[string]any{"ok": false, "problem": "no output sink", "sinks": []any{}}, nil
		case "repair":
			s.soundOK = true
			return map[string]any{"fixed": true, "changed": false, "steps": []any{"the output is there"}}, nil
		}
		return nil, fmt.Errorf("unknown audio command %q", verb)
	}
}

func (l *Laptop) updateCheck(a args) (any, error) {
	return map[string]any{"current": "0.4.0-sim", "latest": "0.4.1", "available": l.updateAvailable, "error": "", "info": map[string]any{"tag": "v0.4.1", "url": "https://github.com/Order52/truesense/releases/tag/v0.4.1"}}, nil
}

// SetUpdateAvailable makes `update check` report a newer release.
func (l *Laptop) SetUpdateAvailable(v bool) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.updateAvailable = v
}

// SetAC changes the power source (and with it what Smart auto would do).
func (l *Laptop) SetAC(ac bool) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.ac = ac
}

func (l *Laptop) doctor() map[string]any {
	checks := []any{
		map[string]any{"check": "kernel module", "status": "OK", "detail": "truesense_wmi loaded, /dev/truesense present", "fix": ""},
		map[string]any{"check": "daemon", "status": "OK", "detail": "truesense.service active, socket answers", "fix": ""},
		map[string]any{"check": "firmware", "status": "OK", "detail": "model PH16-71 verified", "fix": ""},
		map[string]any{"check": "sensors", "status": "OK", "detail": "CPU 54 C, GPU 49 C", "fix": ""},
		map[string]any{"check": "audio", "status": "WARN", "detail": "headset microphone not detected by the codec", "fix": "unplug and replug the headset"},
		map[string]any{"check": "updates", "status": "INFO", "detail": "automatic check on", "fix": ""},
	}
	return map[string]any{"version": "0.4.0-sim", "kernel": "7.3.0-4-cachyos", "ok": true, "failures": 0, "warnings": 1, "checks": checks, "at": time.Now().Unix()}
}

func (l *Laptop) services() []any {
	row := func(unit, scope, enabled, active string, ok bool) any {
		fix := ""
		if !ok {
			fix = "truesense service repair"
		}
		return map[string]any{"unit": unit, "scope": scope, "enabled": enabled, "active": active, "ok": ok, "fix": fix}
	}
	return []any{
		row("truesense.service", "system", "enabled", "active", !l.NoDaemon),
		row("truesense-health.timer", "system", "enabled", "active", true),
		row("truesense-keys.service", "user", "enabled", "active", true),
		row("truesense-audio.service", "user", "enabled", "active", true),
		row("kernel module (/dev/truesense)", "system", "-", "present", true),
	}
}
