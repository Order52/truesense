package audio

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"strings"
	"time"
)

// "There is no sound": detect and repair.
//
// Seen on the PH16-71 (Nov 2026): at boot the sound DSP (SOF) rejected a stream while
// WirePlumber probed the card (`ipc tx error ... -22` in the kernel log), so WirePlumber built
// the card with only `off` and `pro-audio` profiles: no speaker/headphone sink at all, only the
// virtual DTS sink in front of nothing. The hardware and the UCM profiles were fine (direct
// ALSA playback worked); restarting WirePlumber so that it probes the card again brought the
// HiFi profiles and the sink back. Nothing here touches the codec.

// SoundState is the verdict on the sound card. OK is nil when there is no card to judge, true
// when the card has an output sink, false when it has none. Which other fields are filled
// (and appear in the JSON) depends on the verdict, as in 0.3.x.
type SoundState struct {
	OK          *bool
	Card        string
	Profile     string
	Sinks       []string // hardware sinks, when OK
	Offered     []string // profiles the card offers, when broken
	HifiOffered bool     // the HiFi profiles are among them, when broken
	Problem     string
}

// IsOK reports whether the card is known and has an output.
func (s SoundState) IsOK() bool { return s.OK != nil && *s.OK }

// IsBroken reports whether the card is known and has no output.
func (s SoundState) IsBroken() bool { return s.OK != nil && !*s.OK }

// MarshalJSON writes the three shapes of the 0.3.x dict.
func (s SoundState) MarshalJSON() ([]byte, error) {
	m := map[string]any{"ok": s.OK}
	switch {
	case s.OK == nil:
		m["problem"] = s.Problem
	case *s.OK:
		m["card"], m["profile"], m["sinks"] = s.Card, s.Profile, nonNil(s.Sinks)
	default:
		m["card"], m["profile"], m["offered"], m["hifi_offered"], m["problem"] = s.Card, s.Profile, nonNil(s.Offered), s.HifiOffered, s.Problem
	}
	return json.Marshal(m)
}

func nonNil(s []string) []string {
	if s == nil {
		return []string{}
	}
	return s
}

func boolPtr(b bool) *bool { return &b }

// SoundState judges whether the card has an output sink.
func (c *Controller) SoundState(ctx context.Context) (SoundState, error) {
	card, ok := c.cardOf(ctx)
	if !ok {
		return SoundState{Problem: "no sound card known to PipeWire"}, nil
	}
	block, ok := cardBlock(c.out(ctx, "pactl", "list", "cards"), card, true)
	if !ok {
		return SoundState{}, fmt.Errorf("card %s is not in the card list", card)
	}
	profile, offered := activeProfile(block), profilesOffered(block)
	var hardware []string
	for _, s := range c.sinks(ctx) {
		if strings.HasPrefix(s, "alsa_output."+cardMatch) {
			hardware = append(hardware, s)
		}
	}
	if len(hardware) > 0 {
		return SoundState{OK: boolPtr(true), Card: card, Profile: profile, Sinks: hardware}, nil
	}
	hifi := slices.ContainsFunc(offered, func(p string) bool { return p == profileOf["speakers"] || p == profileOf["headphones"] })
	why := fmt.Sprintf("the sound card has no output (profile: %s)", profile)
	if profile == "off" {
		why = "the sound card is switched off"
	}
	if !hifi {
		why += "; its speaker and headphone profiles are missing"
	}
	return SoundState{OK: boolPtr(false), Card: card, Profile: profile, Offered: offered, HifiOffered: hifi, Problem: why}, nil
}

// RepairResult is what Repair did. The JSON names are the 0.3.x ones.
type RepairResult struct {
	Fixed   bool       `json:"fixed"`
	Changed bool       `json:"changed"`
	Steps   []string   `json:"steps"`
	State   SoundState `json:"state"`
}

// RepairOptions bound how long the repair waits for the sink to come back.
type RepairOptions struct {
	Timeout time.Duration // default 10 s
	Poll    time.Duration // default 250 ms
}

// Repair brings the sound output back: if the card still offers its HiFi profiles, pick one;
// otherwise restart WirePlumber so it probes the card again (it then restores the saved
// profile). It verifies that a hardware sink exists afterwards, never forces a profile the card
// does not offer, and never writes codec settings.
func (c *Controller) Repair(ctx context.Context, o RepairOptions) (RepairResult, error) {
	if o.Timeout == 0 {
		o.Timeout = 10 * time.Second
	}
	if o.Poll == 0 {
		o.Poll = 250 * time.Millisecond
	}
	st, err := c.SoundState(ctx)
	if err != nil {
		return RepairResult{}, err
	}
	steps := []string{}
	if !st.IsBroken() {
		return RepairResult{Fixed: st.IsOK(), Steps: steps, State: st}, nil
	}
	waitForSink := func() bool {
		end := c.Now().Add(o.Timeout)
		for c.Now().Before(end) {
			if s, err := c.SoundState(ctx); err == nil && s.IsOK() {
				return true
			}
			c.sleep(ctx, o.Poll)
			if ctx.Err() != nil {
				return false
			}
		}
		return false
	}
	selectSpeakers := func(card string) {
		c.run(ctx, "pactl", "set-card-profile", card, profileOf["speakers"])
		steps = append(steps, "selected the speaker profile")
	}
	state := func() SoundState { s, _ := c.SoundState(ctx); return s }
	if st.HifiOffered {
		selectSpeakers(st.Card)
		if waitForSink() {
			return RepairResult{Fixed: true, Changed: true, Steps: steps, State: state()}, nil
		}
	}
	c.run(ctx, "systemctl", "--user", "restart", "wireplumber.service")
	steps = append(steps, "restarted WirePlumber so it probes the sound card again")
	if !waitForSink() {
		if now := state(); now.IsBroken() && now.HifiOffered {
			selectSpeakers(now.Card)
			waitForSink()
		}
	}
	final := state()
	return RepairResult{Fixed: final.IsOK(), Changed: true, Steps: steps, State: final}, nil
}

// DefaultRepairDelays are the moments after login at which AutoRepair looks: the card is
// probed a few seconds after boot, and WirePlumber may be slow on a busy session.
var DefaultRepairDelays = []time.Duration{20 * time.Second, 90 * time.Second}

// AutoRepair is run from the login audio service: a few checks after login, and a repair when
// the output is missing (at most once per check). The delays are measured from the start.
func (c *Controller) AutoRepair(ctx context.Context, delays []time.Duration) {
	var prev time.Duration
	for _, d := range delays {
		c.sleep(ctx, d-prev)
		prev = d
		if ctx.Err() != nil {
			return
		}
		st, err := c.SoundState(ctx)
		if err != nil {
			c.log().Warn("sound check failed", "error", err)
			continue
		}
		if !st.IsBroken() {
			return
		}
		r, err := c.Repair(ctx, RepairOptions{})
		if err != nil {
			c.log().Warn("sound check failed", "error", err)
			continue
		}
		verdict := "still missing"
		if r.Fixed {
			verdict = "FIXED"
		}
		c.log().Info(fmt.Sprintf("sound output was missing: %s: %s", strings.Join(r.Steps, "; "), verdict))
		if r.Fixed {
			c.say("Sound output repaired", "audio")
		}
		return
	}
}

// SoundCheck is the doctor's view: known is false with no card to judge; problem says what is
// wrong when known and not ok.
func (c *Controller) SoundCheck(ctx context.Context) (known, ok bool, problem string, err error) {
	st, err := c.SoundState(ctx)
	if err != nil {
		return false, false, "", err
	}
	return st.OK != nil, st.IsOK(), st.Problem, nil
}

var errNoDTS = errors.New("AcerUltraX is not installed (https://github.com/fawaz7/AcerUltraX); TrueSense only drives it, it does not bundle Acer's files")
