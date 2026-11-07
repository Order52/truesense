package audio

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"time"

	"github.com/Order52/truesense/internal/fsx"
)

// Purified Voice, per input device. Configuring one restarts PipeWire once (at configuration
// time only); plugging and unplugging a headset never does.

// voiceMode is the RNNoise voice-activity gate of one mode: threshold in percent, grace and
// retroactive grace in milliseconds.
type voiceMode struct{ threshold, grace, retro int }

var voiceModes = map[string]voiceMode{"conference": {50, 20, 0}, "personal": {85, 150, 100}}

// VoiceModeNames are the modes in the order `voice next` cycles through them.
var VoiceModeNames = []string{"off", "conference", "personal"}

// voiceDevice is one input a filter chain can be put in front of.
type voiceDevice struct {
	name        string
	sourceName  string // the PipeWire source suffix of the ALSA input
	description string
}

var voiceDevices = []voiceDevice{
	{"headset", "Mic2__source", "Purified Voice (headset mic)"},
	{"array", "Mic1__source", "Purified Voice (mic array)"},
}

func lookupDevice(name string) (voiceDevice, bool) {
	for _, d := range voiceDevices {
		if d.name == name {
			return d, true
		}
	}
	return voiceDevice{}, false
}

func (d voiceDevice) source() string { return "alsa_input." + cardMatch + ".HiFi__" + d.sourceName }
func (d voiceDevice) node() string   { return "truesense_pv_" + d.name }

func (c *Controller) confPath(d voiceDevice) string {
	return filepath.Join(c.ConfDir, "truesense-pv-"+d.name+".conf")
}

func (c *Controller) speakerConfPath() string {
	return filepath.Join(c.ConfDir, "truesense-pv-speaker.conf")
}

func (c *Controller) reload(ctx context.Context) {
	c.run(ctx, "systemctl", "--user", "restart", "pipewire.service")
}

func (c *Controller) sourceNames(ctx context.Context) map[string]bool {
	names := map[string]bool{}
	for _, n := range shortColumn(c.out(ctx, "pactl", "list", "short", "sources"), 1) {
		names[n] = true
	}
	return names
}

func (c *Controller) nodeExists(ctx context.Context, name string) bool {
	return c.sourceNames(ctx)[name]
}

// waitNode waits (up to 8 s) for the filter node to appear or vanish after a PipeWire reload.
func (c *Controller) waitNode(ctx context.Context, d voiceDevice, present bool) bool {
	end := c.Now().Add(8 * time.Second)
	for c.Now().Before(end) {
		if c.nodeExists(ctx, d.node()) == present {
			return true
		}
		c.sleep(ctx, 200*time.Millisecond)
		if ctx.Err() != nil {
			return false
		}
	}
	return false
}

func exists(path string) bool { _, err := os.Stat(path); return err == nil }

// VoiceSet configures one input's filter ("headset" or "array") to off, conference or
// personal. It restarts PipeWire only when the configuration actually changes.
func (c *Controller) VoiceSet(ctx context.Context, dev, mode string) (string, error) {
	d, ok := lookupDevice(dev)
	if !ok {
		return "", errors.New("device: headset | array")
	}
	path := c.confPath(d)
	if mode == "off" {
		if exists(path) {
			if err := os.Remove(path); err != nil {
				return "", fmt.Errorf("remove %s: %w", path, err)
			}
			c.reload(ctx)
			c.waitNode(ctx, d, false)
		}
		return "off", nil
	}
	m, ok := voiceModes[mode]
	if !ok {
		return "", errors.New("mode: off | conference | personal")
	}
	if c.Plugin == "" {
		return "", errors.New("RNNoise LADSPA plugin missing (Arch: sudo pacman -S noise-suppression-for-voice)")
	}
	want := voiceConf(d, m, c.Plugin)
	if have, err := os.ReadFile(path); err != nil || string(have) != want {
		if err := fsx.WriteAtomic(path, []byte(want), 0o644); err != nil {
			return "", fmt.Errorf("write %s: %w", path, err)
		}
		c.reload(ctx)
		c.waitNode(ctx, d, true)
	}
	return mode, nil
}

// VoiceModes reads each input's mode back from its configuration file (off, conference,
// personal, or "?" for a file this program did not write).
func (c *Controller) VoiceModes() map[string]string {
	out := map[string]string{}
	for _, d := range voiceDevices {
		b, err := os.ReadFile(c.confPath(d))
		if err != nil {
			out[d.name] = "off"
			continue
		}
		out[d.name] = "?"
		thr := voiceThreshold(string(b))
		names := make([]string, 0, len(voiceModes))
		for k := range voiceModes {
			names = append(names, k)
		}
		sort.Strings(names)
		for _, k := range names {
			if voiceModes[k].threshold == thr {
				out[d.name] = k
				break
			}
		}
	}
	return out
}

// VoiceEnable restores the configured modes ([audio] headset_mode / array_mode) when on, and
// leaves both inputs unfiltered when off.
func (c *Controller) VoiceEnable(ctx context.Context, on bool) (map[string]string, error) {
	a := c.Cfg().Audio
	headset, array := "off", "off"
	if on {
		headset, array = a.HeadsetMode, a.ArrayMode
	}
	if _, err := c.VoiceSet(ctx, "headset", headset); err != nil {
		return nil, err
	}
	if _, err := c.VoiceSet(ctx, "array", array); err != nil {
		return nil, err
	}
	return c.VoiceModes(), nil
}

// VoiceNext cycles the mode of an input (the one in use when dev is empty): off -> conference
// -> personal -> off. It returns the input and its new mode.
func (c *Controller) VoiceNext(ctx context.Context, dev string) (device, mode string, err error) {
	if dev == "" {
		dev = "array"
		if c.Jack(ctx).HeadsetMic {
			dev = "headset"
		}
	}
	next := map[string]string{"off": "conference", "conference": "personal", "personal": "off"}[c.VoiceModes()[dev]]
	if next == "" {
		next = "conference"
	}
	if _, err := c.VoiceSet(ctx, dev, next); err != nil {
		return "", "", err
	}
	return dev, next, nil
}

// MicPolicy is what ApplyMicPolicy chose. The JSON names are the 0.3.x ones.
type MicPolicy struct {
	Device        string `json:"device"`
	Mode          string `json:"mode"`
	DefaultSource string `json:"default_source"`
}

// ApplyMicPolicy points the default microphone at the right input: headset plugged -> headset
// filter (or the raw headset mic); else the array filter (or the raw array). Streams already
// recording are moved along.
func (c *Controller) ApplyMicPolicy(ctx context.Context, notify bool) (MicPolicy, error) {
	devName := c.JackUse(ctx, nil)
	d, _ := lookupDevice(devName)
	mode := c.VoiceModes()[devName]
	want := d.source()
	if mode != "off" && c.nodeExists(ctx, d.node()) {
		want = d.node()
	}
	if c.defaultSource(ctx) != want {
		c.run(ctx, "pactl", "set-default-source", want)
		for _, id := range firstFields(c.out(ctx, "pactl", "list", "short", "source-outputs")) {
			c.run(ctx, "pactl", "move-source-output", id, want)
		}
	}
	if notify {
		where := "Mic array"
		if devName == "headset" {
			where = "Headset"
		}
		c.say(fmt.Sprintf("Microphone: %s - %s", where, ModeLabel(mode)), "mic")
	}
	return MicPolicy{Device: devName, Mode: mode, DefaultSource: want}, nil
}

// SpeakerFilter switches the speaker-side noise filter (off by default: it also gates music;
// meant for calls on the speakers) and returns its new state.
func (c *Controller) SpeakerFilter(ctx context.Context, on bool, mode string) (bool, error) {
	path := c.speakerConfPath()
	if !on {
		if exists(path) {
			if err := os.Remove(path); err != nil {
				return false, fmt.Errorf("remove %s: %w", path, err)
			}
			c.reload(ctx)
		}
		return false, nil
	}
	if c.Plugin == "" {
		return false, errors.New("RNNoise LADSPA plugin missing")
	}
	m, ok := voiceModes[mode]
	if !ok {
		return false, errors.New("mode: conference | personal")
	}
	if err := fsx.WriteAtomic(path, []byte(speakerConf(m, c.Plugin)), 0o644); err != nil {
		return false, fmt.Errorf("write %s: %w", path, err)
	}
	c.reload(ctx)
	return true, nil
}

// SpeakerFilterOn reports whether the speaker noise filter is configured.
func (c *Controller) SpeakerFilterOn() bool { return exists(c.speakerConfPath()) }
