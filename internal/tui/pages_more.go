package tui

import (
	"context"
	"fmt"
	"strings"
)

// light is the working copy of a keyboard or light bar setting: what the hardware has,
// overlaid with the changes made on this screen that the hardware has not confirmed.
type light struct {
	name       string
	color      string
	speed      int
	brightness int
	direction  int
}

func (l *light) overlay(m map[string]any) {
	if v, ok := m["name"].(string); ok {
		l.name = v
	}
	if v, ok := m["color"].(string); ok {
		l.color = v
	}
	if v, ok := m["speed"].(int); ok {
		l.speed = v
	}
	if v, ok := m["brightness"].(int); ok {
		l.brightness = v
	}
	if v, ok := m["direction"].(int); ok {
		l.direction = v
	}
}

// kbs is the keyboard setting: the last look this tool wrote, the firmware's own current
// brightness (Fn+F11/F12 change it too), then the unconfirmed changes of this screen.
func (a *App) kbs() light {
	s := light{name: "static", color: "00aec7", speed: 5, brightness: 25}
	if last, ok := getAs[KeyboardLook](a.data, "kblast"); ok {
		if last.Name != "" {
			s.name = last.Name
		}
		if last.Color != "" {
			s.color = last.Color
		}
		if last.Speed > 0 {
			s.speed = last.Speed
		}
		if last.Name != "off" {
			s.brightness, s.direction = last.Brightness, last.Direction
		}
	}
	if now, ok := getAs[int](a.data, "kbnow"); ok {
		s.brightness = now
	}
	s.overlay(a.kb)
	return s
}

func (a *App) rears() light {
	s := light{name: "static", color: "00aec7", speed: 3, brightness: 100, direction: 1}
	if r, ok := getAs[RearLook](a.data, "rear"); ok {
		if r.Mode != "" {
			s.name = r.Mode
		}
		if r.Color != "" {
			s.color = r.Color
		}
		if r.Speed > 0 {
			s.speed = r.Speed
		}
		s.brightness, s.direction = r.Brightness, r.Direction
	}
	s.overlay(a.rear)
	return s
}

// lit makes sure choosing an effect or a colour never leaves a light dark: if the
// brightness is 0 it comes back at the last brightness, at least lightFloor (setting the
// brightness itself to 0 is how you switch it off).
func (a *App) lit(which string, kw map[string]any, darkOK bool) map[string]any {
	if _, ok := kw["brightness"]; ok {
		return kw
	}
	var cur light
	if which == "kb" {
		cur = a.kbs()
	} else {
		cur = a.rears()
	}
	if cur.brightness > 0 {
		a.lastOn[which] = cur.brightness
	} else if !darkOK {
		out := map[string]any{"brightness": max(a.lastOn[which], lightFloor[which])}
		for k, v := range kw {
			out[k] = v
		}
		return out
	}
	return kw
}

func copyMap(m map[string]any) map[string]any {
	out := make(map[string]any, len(m))
	for k, v := range m {
		out[k] = v
	}
	return out
}

func (a *App) restoreLight(target *map[string]any, before map[string]any) func() {
	return func() { *target = copyMap(before) }
}

func (a *App) kbPut(kw map[string]any) {
	before := copyMap(a.kb)
	name, _ := kw["name"].(string)
	kw = a.lit("kb", kw, name == "off")
	if v, _ := kw["brightness"].(int); v > 0 {
		a.lastOn["kb"] = v
	}
	for k, v := range kw {
		a.kb[k] = v
	}
	s := a.kbs()
	undo := a.restoreLight(&a.kb, before)
	if s.name == "off" {
		a.run("Keyboard effect: off", func() (any, error) { return nil, a.b.SetKeyboard(a.ctx, KeyboardLook{Name: "off"}) }, runOpts{key: "kb", undo: undo})
		return
	}
	what := "Keyboard: " + s.name
	if s.brightness == 0 {
		what = "Keyboard light off"
	}
	look := KeyboardLook{Name: s.name, Color: s.color, Speed: s.speed, Brightness: s.brightness, Direction: s.direction}
	a.run(what, func() (any, error) { return nil, a.b.SetKeyboard(a.ctx, look) }, runOpts{key: "kb", undo: undo})
}

func (a *App) rearPut(kw map[string]any) {
	before := copyMap(a.rear)
	kw = a.lit("rear", kw, false)
	if v, _ := kw["brightness"].(int); v > 0 {
		a.lastOn["rear"] = v
	}
	for k, v := range kw {
		a.rear[k] = v
	}
	s := a.rears()
	what := "Light bar: " + s.name
	if s.brightness == 0 {
		what = "Light bar off"
	}
	_, picked := a.rear["color"]
	keepZones := s.name == "static" && !picked // the three part colours set in the window stay until you pick a colour here
	look := RearLook{Mode: s.name, Color: s.color, Speed: s.speed, Brightness: s.brightness, Direction: s.direction}
	a.run(what, func() (any, error) { return nil, a.b.SetRear(a.ctx, look, keepZones) }, runOpts{key: "rear", undo: a.restoreLight(&a.rear, before)})
}

func isHexColor(s string) bool {
	if len(s) != 6 {
		return false
	}
	for _, c := range s {
		if !strings.ContainsRune("0123456789abcdefABCDEF", c) {
			return false
		}
	}
	return true
}

// customColor asks for a colour as six hex digits and hands it to put.
func (a *App) customColor(current func() string, put func(map[string]any)) {
	a.prompt("Colour as 6 hex digits (RRGGBB)", current(), func(t string) {
		t = strings.TrimLeft(strings.TrimSpace(t), "#")
		if isHexColor(t) {
			put(map[string]any{"color": strings.ToLower(t)})
		} else {
			a.say("A colour is six hex digits, like ff8800", StBad, hintSeconds, false)
		}
	})
}

func colorGet(c string) any {
	for _, o := range colorList {
		if o.Value == c {
			return c
		}
	}
	return nil
}

func (a *App) lightingPage() *Page {
	b, d := a.b, a.data
	caps := a.caps
	rearModes := b.RearModes()
	kbEffects := make([]string, len(caps.Effects))
	copy(kbEffects, caps.Effects)
	kb := func(k string, v any) { a.kbPut(map[string]any{k: v}) }
	rr := func(k string, v any) { a.rearPut(map[string]any{k: v}) }
	asI := func(v any) int { n, _ := asInt(v); return n }
	rows := []*Row{
		headerRow("Keyboard"),
		choiceRow("Effect", capitalized(kbEffects), func() any { return a.kbs().name }, func(v any) { kb("name", v) },
			Row{Hint: "The effects built into the keyboard. Per-key colours and your own images: truesense gui --page keys."}),
		choiceRow("Colour", colorList, func() any { return colorGet(a.kbs().color) }, func(v any) { kb("color", v) }, Row{Hint: "Not used by Wave, Neon and Off."}),
		buttonRow("Custom colour", "Type hex…", func() { a.customColor(func() string { return a.kbs().color }, a.kbPut) }, Row{Hint: "Any colour as RRGGBB."}),
		sliderRow("Brightness", intRange(0, caps.MaxBrightness, 5), func() any { return a.kbs().brightness }, func(v any) { kb("brightness", asI(v)) },
			Row{Fmt: func(v any) string { return fmt.Sprintf("%d %%", asI(v)*2) }}),
		sliderRow("Speed", intRange(caps.MinSpeed, caps.MaxSpeed, 1), func() any { return a.kbs().speed }, func(v any) { kb("speed", asI(v)) }, Row{Hint: "Slower is calmer."}),
		choiceRow("Direction", directions, func() any { return a.kbs().direction }, func(v any) { kb("direction", asI(v)) }, Row{}),
		choiceRow("Saved lighting", nil, func() any { return nil }, func(v any) {
			n := asString(v)
			a.run("Lighting: "+n, func() (any, error) { return nil, b.ApplySavedLighting(a.ctx, n) }, runOpts{})
		}, Row{Hint: "Per-key lightings you saved in the window.", Enabled: func() bool { n, _ := getAs[[]string](d, "klight"); return len(n) > 0 },
			OptionsFn: func() []Option {
				if n, _ := getAs[[]string](d, "klight"); len(n) > 0 {
					return namedOptions(n)
				}
				return []Option{{"", "none saved"}}
			}}),
		headerRow("Light bar (back of the laptop)"),
		choiceRow("Effect", capitalized(rearModes), func() any { return a.rears().name }, func(v any) { rr("name", v) }, Row{}),
		choiceRow("Colour", colorList, func() any { return colorGet(a.rears().color) }, func(v any) { rr("color", v) }, Row{}),
		buttonRow("Custom colour", "Type hex…", func() { a.customColor(func() string { return a.rears().color }, a.rearPut) }, Row{}),
		sliderRow("Brightness", intRange(0, 100, 10), func() any { return a.rears().brightness }, func(v any) { rr("brightness", asI(v)) },
			Row{Fmt: func(v any) string { return fmt.Sprintf("%d %%", asI(v)) }, Hint: "0 turns the bar off."}),
		sliderRow("Speed", []int{1, 2, 3, 4, 5}, func() any { return a.rears().speed }, func(v any) { rr("speed", asI(v)) }, Row{}),
	}
	reads := []Read{reader("kblast", b.KeyboardLast), reader("kbnow", b.KeyboardBrightness), reader("rear", b.Rear),
		{Key: "klight", Fn: func(context.Context) (any, error) { return b.SavedLightings(), nil }}}
	return newPage("Lighting", rows, reads, nil)
}

func (a *App) audioPage() *Page {
	b, d := a.b, a.data
	voiceOf := func(input string) func() any {
		return func() any {
			v, ok := getAs[VoiceModes](d, "voice")
			switch {
			case !ok:
				return nil
			case input == VoiceHeadset:
				return v.Headset
			}
			return v.Array
		}
	}
	voicePut := func(input, label string) func(any) {
		return func(v any) {
			m := asString(v)
			a.run(fmt.Sprintf("%s: %s", label, m), func() (any, error) { return nil, b.SetVoiceMode(a.ctx, input, m) }, runOpts{})
		}
	}
	voiceOpts := capitalized([]string{"off", "conference", "personal"})
	micGet := func() (Microphone, bool) { return getAs[Microphone](d, "mic") }
	rows := []*Row{
		headerRow("Sound"),
		infoRow("Sound card", func() any {
			c, ok := getAs[SoundCard](d, "sound")
			switch {
			case !ok:
				return nil
			case c.Status == SoundOK:
				return "output present"
			case c.Status == SoundMissing:
				return "no sound card"
			case c.Problem == "":
				return "NO OUTPUT: ?"
			}
			return "NO OUTPUT: " + c.Problem
		}, Row{InfoStyle: func(v any) Style {
			if v == "output present" {
				return StGood
			}
			return StBad
		}}),
		buttonRow("No sound at all?", "Fix sound", func() {
			a.run("Repairing the sound output", func() (any, error) { return b.RepairSound(a.ctx) }, runOpts{ok: func(r any) string {
				res, _ := r.(RepairResult)
				steps := strings.Join(res.Steps, "; ")
				switch {
				case res.Fixed && res.Changed:
					return "Sound output repaired: " + steps
				case res.Fixed:
					return "The sound output is there: nothing to repair"
				}
				return "Could not repair: " + steps
			}})
		}, Row{Hint: "Restarts WirePlumber so it probes the sound card again. Nothing is written to the sound chip."}),
		choiceRow("Play sound on", []Option{{"speakers", "Speakers"}, {"headphones", "Headphones"}}, func() any { return xValue[string](d, "out") }, func(v any) {
			o := asString(v)
			a.run("Output: "+o, func() (any, error) { return nil, b.SetOutput(a.ctx, o) }, runOpts{})
		}, Row{Hint: "This laptop plays on the speakers or the headphone jack, not both."}),
		toggleRow("Noise filter on the speakers", func() any { return xValue[bool](d, "spk") }, func(v any) {
			on := asBool(v)
			a.run("Speaker noise filter "+onOffText(v), func() (any, error) { return nil, b.SetSpeakerFilter(a.ctx, on) }, runOpts{})
		}, Row{Hint: "Off by default: it also gates music. For calls on the speakers."}),
		headerRow("Acer DTS sound"),
		toggleRow("DTS", func() any { return xValue[bool](d, "dts") }, func(v any) {
			on := asBool(v)
			a.run("DTS "+onOffText(v), func() (any, error) { return nil, b.SetDTS(a.ctx, on) }, runOpts{})
		}, Row{Hint: "Needs AcerUltraX."}),
		choiceRow("DTS sound mode", namedOptions(dtsModes), func() any {
			devs, _ := getAs[[]DTSDevice](d, "dtsdev")
			for _, dv := range devs {
				if dv.Current {
					return dv.Profile
				}
			}
			return nil
		}, func(v any) {
			m := asString(v)
			a.run(fmt.Sprintf("DTS sound: %s on every output", m), func() (any, error) { return nil, b.SetDTSMode(a.ctx, m) }, runOpts{})
		}, Row{Hint: "Applies to every output: speakers, jack, USB and Bluetooth headsets. Plain is no processing."}),
		buttonRow("A headset still sounds flat?", "Use DTS everywhere", func() {
			a.run("DTS on every output", func() (any, error) {
				s, err := b.DTSEverywhere(a.ctx)
				if err == nil && s == "" {
					s = "DTS is on for every output"
				}
				return s, err
			}, runOpts{ok: func(r any) string { return "DTS: " + asString(r) }})
		}, Row{Hint: "New outputs get DTS by themselves; this does it again for all of them now."}),
		infoRow("Outputs", func() any {
			devs, _ := getAs[[]DTSDevice](d, "dtsdev")
			if len(devs) == 0 {
				return nil
			}
			parts := make([]string, len(devs))
			for i, dv := range devs {
				parts[i] = dv.Label + ": " + dv.Profile
			}
			return strings.Join(parts, " · ")
		}, Row{Wrap: true}),
		headerRow("Microphone"),
		toggleRow("Mute every microphone", func() any {
			if m, ok := micGet(); ok {
				return m.Muted
			}
			return nil
		}, func(v any) {
			muted := asBool(v)
			what := "live"
			if muted {
				what = "muted"
			}
			a.run("Microphones "+what, func() (any, error) { return nil, b.SetMicMuted(a.ctx, muted) }, runOpts{})
		}, Row{}),
		sliderRow("Microphone volume", intRange(0, 150, 5), func() any {
			if m, ok := micGet(); ok && m.Volume.OK {
				return m.Volume.V
			}
			return nil
		}, func(v any) {
			n, _ := asInt(v)
			a.run(fmt.Sprintf("Microphone volume: %d %%", n), func() (any, error) { return nil, b.SetMicVolume(a.ctx, n) }, runOpts{})
		}, Row{Fmt: func(v any) string { return fmt.Sprintf("%v %%", v) }}),
		choiceRow("Headset mic noise reduction", voiceOpts, voiceOf(VoiceHeadset), voicePut(VoiceHeadset, "Headset mic"), Row{}),
		choiceRow("Built-in mic noise reduction", voiceOpts, voiceOf(VoiceArray), voicePut(VoiceArray, "Built-in mics"), Row{}),
	}
	reads := []Read{reader("sound", b.SoundCard), reader("out", b.Output), reader("spk", b.SpeakerFilter), reader("mic", b.Microphone), reader("voice", b.VoiceModes),
		reader("dtsdev", b.DTSDevices), reader("dts", b.DTSOn)}
	return newPage("Audio", rows, reads, nil)
}

func (a *App) profilesPage() *Page {
	b := a.b
	rows := []*Row{headerRow("Profiles: a whole setup in one go (power, fans, lighting, audio)")}
	names := b.ProfileNames()
	for _, n := range names {
		rows = append(rows, buttonRow(n, "Apply", func() {
			a.run("Profile "+n, func() (any, error) { return nil, b.ApplyProfile(a.ctx, n) }, runOpts{okText: fmt.Sprintf("Profile %s applied", n)})
		}, Row{Hint: b.DescribeProfile(n)}))
	}
	if len(names) == 0 {
		rows = append(rows, infoRow("", func() any { return "no profiles found (truesense profile list)" }, Row{}))
	}
	rows = append(rows, headerRow("Looks: the lighting at boot, as saved in the window"))
	for _, n := range b.LookNames() {
		rows = append(rows, buttonRow(n, "Apply", func() {
			a.run("Look "+n, func() (any, error) { return nil, b.ApplyLook(a.ctx, n) }, runOpts{})
		}, Row{Hint: "Applies the saved keyboard and light-bar look."}))
	}
	return newPage("Profiles", rows, nil, nil)
}

func (a *App) systemPage() *Page {
	b, d := a.b, a.data
	results := func() []DoctorResult { r, _ := getAs[[]DoctorResult](d, "doctor"); return r }
	find := func(title string) (DoctorResult, bool) {
		for _, r := range results() {
			if r.Check == title {
				return r, true
			}
		}
		return DoctorResult{}, false
	}
	rows := []*Row{
		headerRow("TrueSense"),
		infoRow("Version", func() any { return a.version }, Row{}),
		infoRow("Kernel module", func() any { return xValue[string](d, "modver") }, Row{}),
		infoRow("Service", func() any {
			if d.daemon() {
				return "running"
			}
			return "not running (direct hardware access)"
		}, Row{InfoStyle: func(v any) Style {
			if v == "running" {
				return StGood
			}
			return StWarn
		}}),
		infoRow("Window", func() any { return "truesense gui" }, Row{}),
		buttonRow("Open the window", "Open", func() {
			a.run("Opening the window", func() (any, error) { return nil, b.OpenWindow(a.ctx) }, runOpts{})
		}, Row{Hint: "The TrueSense window (the same as running truesense gui)."}),
		headerRow("Health check"),
		buttonRow("Run the check", "Run doctor", func() {
			a.run("Checking everything", func() (any, error) {
				r, err := b.RunDoctor(a.ctx)
				if err == nil {
					d.Set("doctor", r)
				}
				return nil, err
			}, runOpts{okText: "Check finished"})
		}, Row{Hint: "Looks at the module, the service, power managers, audio and more. Read-only."}),
		infoRow("", func() any { return "not run yet: press Enter on the button above" }, Row{Hide: func() bool { return len(results()) > 0 }}),
	}
	for _, title := range b.DoctorChecks() {
		rows = append(rows, infoRow(title, func() any {
			r, ok := find(title)
			switch {
			case ok:
				s := fmt.Sprintf("%s  %s", r.Status, r.Detail)
				if r.Fix != "" && (r.Status == DoctorWarn || r.Status == DoctorFail) {
					s += "   -> " + r.Fix
				}
				return s
			case len(results()) == 0:
				return "not checked yet"
			}
			return "…"
		}, Row{Wrap: true, Hide: func() bool { return len(results()) == 0 }, InfoStyle: func(any) Style {
			r, ok := find(title)
			switch {
			case !ok:
				return StDim
			case r.Status == DoctorOK:
				return StGood
			case r.Status == DoctorWarn:
				return StWarn
			case r.Status == DoctorFail:
				return StBad
			}
			return StText
		}}))
	}
	return newPage("System", rows, []Read{reader("modver", b.ModuleVersion)}, nil)
}
