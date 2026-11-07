package cli

import (
	"flag"
	"fmt"
	"strings"

	"github.com/Order52/truesense/internal/cli/command"
)

func (c *CLI) audio() *command.Cmd {
	return &command.Cmd{
		Name: "audio", Short: "output, microphone, headset jack, Purified Voice, DTS",
		Run: func(cc *command.Ctx) error {
			if err := cc.Want(0, 0); err != nil {
				return err
			}
			return fmt.Errorf("choose: output | mic | jack | voice | speaker-filter | dts   (truesense audio -h)")
		},
		Sub: []*command.Cmd{
			{Name: "output", Usage: "[status|speakers|headphones|toggle|cycle]", Short: "speakers / headphones while the aux jack is plugged in", Run: c.audioOutput},
			{Name: "mic", Usage: "[status|mute|unmute|toggle|volume|boost] [VALUE]", Run: c.audioMic, Flags: func(fs *flag.FlagSet) {
				fs.Bool("default-only", false, "act on the default input only (default: every input)")
			}},
			{Name: "jack", Usage: "[auto|headphones|headset]", Short: "what the codec sees on the 3.5 mm jack; `jack headphones|headset|auto` sets how the jack is used", Run: c.audioJack},
			{Name: "voice", Usage: "[status|set|enable|disable|next|apply|watch|notify] [A] [B]", Short: "Purified Voice (AI mic noise reduction)", Run: c.audioVoice},
			{Name: "repair", Short: "no sound? restart WirePlumber so it probes the sound card again, and check that an output exists", Run: c.audioRepair},
			{Name: "speaker-filter", Usage: "[on|off|status]", Short: "noise filter on the output (off by default: it also gates music)", Run: c.audioSpeakerFilter},
			{Name: "dts", Usage: "[status|on|off|mode|profiles|devices|all|setup] [NAME]", Short: "Acer DTS sound through AcerUltraX", Run: c.audioDTS},
		},
	}
}

func (c *CLI) audioOutput(cc *command.Ctx) error {
	if err := cc.Want(0, 1, "action"); err != nil {
		return err
	}
	action, err := cc.Choice(0, "action", "status", "status", "speakers", "headphones", "toggle", "cycle")
	if err != nil {
		return err
	}
	switch action {
	case "status", "toggle":
		var (
			st  any
			err error
		)
		if action == "toggle" {
			st, err = c.api.ToggleAudioOutput(cc.Context)
		} else {
			st, err = c.api.AudioOutput(cc.Context)
		}
		if err != nil {
			return err
		}
		cc.Emit(st, "")
	case "cycle":
		label, err := c.api.CycleAudioOutput(cc.Context)
		if err != nil {
			return err
		}
		cc.Emit(label, "")
	default:
		out, err := c.api.SetAudioOutput(cc.Context, action)
		if err != nil {
			return err
		}
		cc.Emit(out, "audio output: "+action)
	}
	return nil
}

func (c *CLI) audioMic(cc *command.Ctx) error {
	if err := cc.Want(0, 2, "action", "value"); err != nil {
		return err
	}
	action, err := cc.Choice(0, "action", "status", "status", "mute", "unmute", "toggle", "volume", "boost")
	if err != nil {
		return err
	}
	defaultOnly := flagBool(cc, "default-only")
	var value *int
	if cc.Arg(1) != "" {
		n, err := intArg("value", cc.Arg(1))
		if err != nil {
			return err
		}
		value = &n
	}
	opt := func(v *int) string {
		if v == nil {
			return "None"
		}
		return fmt.Sprint(*v)
	}
	switch action {
	case "status":
		cc.Emit(c.api.Mic(cc.Context), "")
	case "mute", "unmute":
		if err := c.api.SetMicMute(cc.Context, action == "mute", defaultOnly); err != nil {
			return err
		}
		cc.Emit(action+"d", "")
	case "toggle":
		m, err := c.api.ToggleMicMute(cc.Context, defaultOnly)
		if err != nil {
			return err
		}
		cc.Emit(m, pick(m, "microphones: MUTED", "microphones: live"))
	case "volume":
		var r *int
		if value == nil {
			r = c.api.MicVolume(cc.Context)
		} else if r, err = c.api.SetMicVolume(cc.Context, *value); err != nil {
			return err
		}
		cc.Emit(r, fmt.Sprintf("microphone volume: %s %%", opt(r)))
	case "boost":
		if value != nil {
			if err := c.api.SetHeadsetBoost(cc.Context, *value); err != nil {
				return err
			}
		}
		r, err := c.api.HeadsetBoost(cc.Context)
		if err != nil {
			return err
		}
		cc.Emit(r, fmt.Sprintf("headset mic boost: %s (0..3, +10 dB per step)", opt(r)))
	}
	return nil
}

func (c *CLI) audioJack(cc *command.Ctx) error {
	if err := cc.Want(0, 1, "use"); err != nil {
		return err
	}
	use, err := cc.Choice(0, "use", "", "auto", "headphones", "headset")
	if err != nil {
		return err
	}
	if use == "" {
		cc.Emit(c.api.Jack(cc.Context), "")
		return nil
	}
	if err := c.api.SetJack(cc.Context, use); err != nil {
		return err
	}
	text := "3.5 mm jack: " + use
	if use == "headset" {
		text += "  (headset microphone is used even if the codec does not detect it)"
	}
	cc.Emit(use, text)
	return nil
}

func (c *CLI) audioVoice(cc *command.Ctx) error {
	if err := cc.Want(0, 3, "action", "a", "b"); err != nil {
		return err
	}
	action, err := cc.Choice(0, "action", "status", "status", "set", "enable", "disable", "next", "apply", "watch", "notify")
	if err != nil {
		return err
	}
	a, b := cc.Arg(1), cc.Arg(2)
	switch action {
	case "status":
		cc.Emit(c.api.VoiceStatus(cc.Context), "")
	case "set":
		got, err := c.api.SetVoice(cc.Context, a, b)
		if err != nil {
			return err
		}
		cc.Emit(got, fmt.Sprintf("Purified Voice (%s): %s", a, b))
	case "enable", "disable":
		modes, err := c.api.EnableVoice(cc.Context, action == "enable")
		if err != nil {
			return err
		}
		cc.Emit(modes, "")
	case "next":
		dev, mode, err := c.api.NextVoice(cc.Context)
		if err != nil {
			return err
		}
		cc.Emit(fmt.Sprintf("Purified Voice (%s): %s", dev, mode), "")
	case "apply":
		p, err := c.api.ApplyMicPolicy(cc.Context)
		if err != nil {
			return err
		}
		cc.Emit(p, "")
	case "watch":
		c.api.WatchMic(cc.Context)
	case "notify":
		if a != "on" && a != "off" {
			return fmt.Errorf("usage: truesense audio voice notify on|off")
		}
		if err := c.api.SetVoiceNotify(a == "on"); err != nil {
			return err
		}
		cc.Emit("mic mode notification: "+a, "")
	}
	return nil
}

func (c *CLI) audioRepair(cc *command.Ctx) error {
	if err := cc.Want(0, 0); err != nil {
		return err
	}
	st, err := c.api.SoundState(cc.Context)
	if err != nil {
		return err
	}
	switch {
	case st.OK == nil:
		cc.Emit(st, "no sound card found in PipeWire: "+st.Problem)
	case st.IsOK():
		cc.Emit(st, "the sound output is there ("+strings.Join(st.Sinks, ", ")+"): nothing to repair")
	default:
		if !cc.JSON {
			cc.Println("problem: " + st.Problem + "\nrepairing (restarting WirePlumber if needed)...")
		}
		r, err := c.api.RepairSound(cc.Context)
		if err != nil {
			return err
		}
		prefix := "NOT FIXED: "
		if r.Fixed {
			prefix = "FIXED: "
		}
		text := prefix + strings.Join(r.Steps, "; ")
		if !r.Fixed {
			text += "\nCheck the kernel log (journalctl -k -b | grep -i sof) and try a reboot."
		}
		cc.Emit(r, text)
		if !r.Fixed {
			return command.ExitCode(1)
		}
	}
	return nil
}

func (c *CLI) audioSpeakerFilter(cc *command.Ctx) error {
	if err := cc.Want(0, 1, "action"); err != nil {
		return err
	}
	action, err := cc.Choice(0, "action", "status", "on", "off", "status")
	if err != nil {
		return err
	}
	if action == "status" {
		on := c.api.SpeakerFilter()
		cc.Emit(on, "speaker filter: "+pick(on, "on", "off"))
		return nil
	}
	if _, err := c.api.SetSpeakerFilter(cc.Context, action == "on"); err != nil {
		return err
	}
	cc.Emit("speaker filter: "+action, "")
	return nil
}

// audioDTS prints AcerUltraX's own words as they are (the 0.3.x command did not wrap them in
// JSON either).
func (c *CLI) audioDTS(cc *command.Ctx) error {
	if err := cc.Want(0, 2, "action", "name"); err != nil {
		return err
	}
	action, err := cc.Choice(0, "action", "status", "status", "on", "off", "mode", "profiles", "devices", "all", "setup")
	if err != nil {
		return err
	}
	var out string
	switch action {
	case "status":
		out, err = c.api.DTSStatus(cc.Context)
	case "on", "off":
		out, err = c.api.SetDTSPower(cc.Context, action == "on")
	case "mode":
		out, err = c.api.SetDTSMode(cc.Context, cc.Arg(1))
	case "profiles":
		out, err = c.api.DTSProfiles(cc.Context)
	case "devices":
		var devs []string
		ds, derr := c.api.DTSDevices(cc.Context)
		if derr != nil {
			return derr
		}
		for _, d := range ds {
			devs = append(devs, fmt.Sprintf("%s %-44s %-18s %s", pick(d.Current, "*", " "), d.Label, d.Kind, d.Profile))
		}
		out = join(devs)
	case "all":
		var done []string
		if done, err = c.api.DTSEverywhere(cc.Context, cc.Arg(1)); err == nil {
			out = orElse(join(done), "DTS is on for every output")
		}
	case "setup":
		out, err = c.api.SetupDTS(cc.Context)
	}
	if err != nil {
		return err
	}
	cc.Println(out)
	return nil
}
