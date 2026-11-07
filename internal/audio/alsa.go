package audio

import (
	"context"
	"errors"
	"fmt"
	"os"
	"strconv"
)

// card is the ALSA card index of the laptop's sound card. With no sound subsystem at all (a
// server, a container) it is "1" and the callers fail with a clear message from amixer.
func (c *Controller) card() string {
	b, err := os.ReadFile(c.ProcCards)
	if err != nil {
		return "1"
	}
	return cardIndex(string(b))
}

func (c *Controller) amixer(ctx context.Context, args ...string) string {
	return c.out(ctx, "amixer", append([]string{"-c" + c.card()}, args...)...)
}

// control reads one ALSA control's value, nil when amixer could not.
func (c *Controller) control(ctx context.Context, iface, name string) *string {
	v, ok := controlValue(c.amixer(ctx, "cget", fmt.Sprintf("iface=%s,name=%s", iface, name)))
	if !ok {
		return nil
	}
	return &v
}

// Jack is what the codec sees on the 3.5 mm jack. The JSON names are the 0.3.x ones; Raw holds
// the controls' own text (null when a control could not be read).
type Jack struct {
	Headphones bool               `json:"headphones"`
	HeadsetMic bool               `json:"headset_mic"`
	Raw        map[string]*string `json:"raw"`
}

// Jack reads the two jack-detection controls: headphones plugged, and whether a headset
// microphone was detected.
func (c *Controller) Jack(ctx context.Context) Jack {
	hp, mic := c.control(ctx, "CARD", "Headphone Jack"), c.control(ctx, "CARD", "Headset Mic Jack")
	return Jack{Headphones: hp != nil && *hp == "on", HeadsetMic: mic != nil && *mic == "on", Raw: map[string]*string{"Headphone Jack": hp, "Headset Mic Jack": mic}}
}

// JackUse is which microphone the 3.5 mm jack is treated as: the setting `[audio] jack` = auto
// (what the codec detects), headphones (never use the headset mic) or headset (always use it).
// It answers "headset" or "array". A nil status is read now.
func (c *Controller) JackUse(ctx context.Context, status *Jack) string {
	switch c.Cfg().Audio.Jack {
	case "headset":
		return "headset"
	case "headphones":
		return "array"
	}
	if status == nil {
		j := c.Jack(ctx)
		status = &j
	}
	if status.HeadsetMic {
		return "headset"
	}
	return "array"
}

const boostControl = "Headset Mic Boost Volume"

// HeadsetBoost reads the headset microphone boost (0..3, +10 dB per step); found is false
// when the card has no such control.
func (c *Controller) HeadsetBoost(ctx context.Context) (level int, found bool, err error) {
	level, found = boostValue(c.amixer(ctx, "cget", "iface=MIXER,name="+boostControl))
	return level, found, nil
}

// SetHeadsetBoost sets the boost and reads it back: a value that did not stick is an error.
func (c *Controller) SetHeadsetBoost(ctx context.Context, level int) error {
	if level < 0 || level > 3 {
		return errors.New("headset mic boost is 0..3")
	}
	c.amixer(ctx, "cset", "iface=MIXER,name="+boostControl, strconv.Itoa(level)+","+strconv.Itoa(level))
	if got, found, _ := c.HeadsetBoost(ctx); !found || got != level {
		return errors.New("boost readback mismatch")
	}
	return nil
}
