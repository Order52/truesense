package audio

import (
	"context"
	"errors"
	"strconv"
	"strings"
)

// MicState is the microphone's state. The JSON names are the 0.3.x ones; Sources is every real
// input (by its short name) and whether it is muted.
type MicState struct {
	Default string          `json:"default"`
	Muted   bool            `json:"muted"`
	Sources map[string]bool `json:"sources"`
	Volume  *int            `json:"volume"` // percent; nil when it cannot be read
}

func (c *Controller) sources(ctx context.Context) [][2]string {
	return shortSources(c.out(ctx, "pactl", "list", "short", "sources"))
}

func (c *Controller) defaultSource(ctx context.Context) string {
	return strings.TrimSpace(c.out(ctx, "pactl", "get-default-source"))
}

// DefaultSource is the name of the default microphone.
func (c *Controller) DefaultSource(ctx context.Context) string { return c.defaultSource(ctx) }

func (c *Controller) sourceMuted(ctx context.Context, name string) bool {
	return isMuted(c.out(ctx, "pactl", "get-source-mute", name))
}

// Mic reads the default microphone and every input.
func (c *Controller) Mic(ctx context.Context) MicState {
	def := c.defaultSource(ctx)
	st := MicState{Default: def, Muted: c.sourceMuted(ctx, def), Sources: map[string]bool{}}
	for _, s := range c.sources(ctx) {
		name := s[1]
		parts := strings.Split(name, "HiFi__")
		st.Sources[parts[len(parts)-1]] = c.sourceMuted(ctx, name)
	}
	if v, ok := volumePercent(c.out(ctx, "pactl", "get-source-volume", def)); ok {
		st.Volume = &v
	}
	return st
}

// MicMuted reports whether the default microphone is muted.
func (c *Controller) MicMuted(ctx context.Context) (bool, error) { return c.Mic(ctx).Muted, nil }

func muteArg(muted bool) string {
	if muted {
		return "1"
	}
	return "0"
}

// SetMicMute mutes or unmutes EVERY input: internal array, headset, USB, Bluetooth.
func (c *Controller) SetMicMute(ctx context.Context, muted bool) error {
	for _, s := range c.sources(ctx) {
		c.run(ctx, "pactl", "set-source-mute", s[1], muteArg(muted))
	}
	return nil
}

// SetDefaultMicMute mutes or unmutes only the default microphone.
func (c *Controller) SetDefaultMicMute(ctx context.Context, muted bool) error {
	c.run(ctx, "pactl", "set-source-mute", c.defaultSource(ctx), muteArg(muted))
	return nil
}

// ToggleMicMute flips the mute state of the default microphone (and of every input unless
// defaultOnly) and returns the new state.
func (c *Controller) ToggleMicMute(ctx context.Context, defaultOnly bool) (bool, error) {
	muted := !c.Mic(ctx).Muted
	if defaultOnly {
		return muted, c.SetDefaultMicMute(ctx, muted)
	}
	return muted, c.SetMicMute(ctx, muted)
}

// MicVolume reads the default microphone's volume in percent (nil when unreadable).
func (c *Controller) MicVolume(ctx context.Context) *int { return c.Mic(ctx).Volume }

// SetMicVolume sets the default microphone's volume (0..150 %) and returns what it reads back.
func (c *Controller) SetMicVolume(ctx context.Context, percent int) (*int, error) {
	if percent < 0 || percent > 150 {
		return nil, errors.New("mic volume is 0..150 %")
	}
	c.run(ctx, "wpctl", "set-volume", "@DEFAULT_AUDIO_SOURCE@", strconv.Itoa(percent)+"%")
	return c.Mic(ctx).Volume, nil
}
