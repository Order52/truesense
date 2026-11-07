package audio

import (
	"context"
	"errors"
	"fmt"
	"os"
	"sort"
	"strings"
	"time"

	toml "github.com/pelletier/go-toml/v2"
)

// The DTS sound (AcerUltraX). TrueSense only drives acerultrax-cli; it does not bundle Acer's
// files.

// DTS runs acerultrax-cli with args and returns its trimmed output. A failing command is an
// error carrying the last line of what it said.
func (c *Controller) DTS(ctx context.Context, timeout time.Duration, args ...string) (string, error) {
	bin := c.dtsBinary()
	if bin == "" {
		return "", errNoDTS
	}
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	r := c.Run(ctx, bin, args...)
	if r.Code != 0 {
		text := r.Stderr
		if text == "" {
			text = r.Stdout
		}
		if msg := lastLine(text); msg != "" {
			return "", errors.New(msg)
		}
		return "", errors.New("acerultrax-cli failed")
	}
	return strings.TrimSpace(r.Stdout), nil
}

const dtsTimeout = 30 * time.Second

func (c *Controller) dts(ctx context.Context, args ...string) (string, error) {
	return c.DTS(ctx, dtsTimeout, args...)
}

func (c *Controller) dtsBinary() string {
	if c.DTSBinary == nil {
		return ""
	}
	return c.DTSBinary()
}

// HasDTS reports whether AcerUltraX is installed.
func (c *Controller) HasDTS() bool { return c.dtsBinary() != "" }

// HasRNNoise reports whether the RNNoise LADSPA plugin (Purified Voice) is installed.
func (c *Controller) HasRNNoise() bool { return c.Plugin != "" }

// DTSStatus is AcerUltraX's own status text.
func (c *Controller) DTSStatus(ctx context.Context) (string, error) { return c.dts(ctx, "status") }

// DTSPower switches DTS on or off.
func (c *Controller) DTSPower(ctx context.Context, on bool) (string, error) {
	state := "off"
	if on {
		state = "on"
	}
	return c.dts(ctx, "power", state)
}

// DTSDevices lists every output AcerUltraX knows.
func (c *Controller) DTSDevices(ctx context.Context) ([]DTSDevice, error) {
	out, err := c.dts(ctx, "devices")
	if err != nil {
		return nil, err
	}
	return parseDTSDevices(out), nil
}

// hasEQ tells whether AcerUltraX holds a headphone EQ for this output (its config file; a
// missing file or key simply means no).
func (c *Controller) hasEQ(ref string) bool {
	b, err := os.ReadFile(c.DTSConfigPath)
	if err != nil {
		return false
	}
	var cfg struct {
		Headphones map[string]any `toml:"headphones"`
	}
	if toml.Unmarshal(b, &cfg) != nil {
		return false
	}
	_, ok := cfg.Headphones[ref]
	return ok
}

// DTSScope says which outputs a DTS mode choice goes to.
type DTSScope int

// The scopes. The default follows the setting `[audio] dts_all` (on unless changed).
const (
	DTSConfigured DTSScope = iota
	DTSEverywhere
	DTSCurrentOnly
)

// DTSMode picks a DTS profile. Normally (`[audio] dts_all`, default on) it goes to EVERY output
// (speakers, jack, USB and Bluetooth headsets, controllers), not just the one playing now, so
// switching the output never lands on an unprocessed one.
func (c *Controller) DTSMode(ctx context.Context, name string, scope DTSScope) (string, error) {
	out, err := c.dts(ctx, "mode", name)
	if err != nil {
		return "", err
	}
	everywhere := scope == DTSEverywhere || scope == DTSConfigured && c.Cfg().Audio.DTSAll
	if !everywhere {
		return out, nil
	}
	devs, err := c.DTSDevices(ctx)
	if err != nil {
		return "", err
	}
	for _, d := range devs {
		if !d.Current && !strings.EqualFold(d.Profile, name) {
			if _, err := c.dts(ctx, "mode", name, "--device", d.Ref); err != nil {
				return "", err
			}
		}
	}
	return out, nil
}

// DTSCover gives every output DTS. AcerUltraX leaves anything it does not know (a USB headset,
// a controller, a dock) on the Plain profile, i.e. no processing at all, so a headset worked on
// the jack and did nothing over USB. Each output is handled ONCE (remembered in the user
// state), so a later "Plain" chosen on purpose stays. New headsets get the profile of the
// laptop speakers plus a headphone EQ: AcerUltraX's own pick by name when it knows the model,
// else Acer's general one. It returns what changed, e.g. ["Headset PC38X: Music + headphone EQ"].
func (c *Controller) DTSCover(ctx context.Context, force bool) ([]string, error) {
	devs, err := c.DTSDevices(ctx)
	if err != nil {
		return nil, err
	}
	var seenList []string
	if c.State != nil {
		_ = c.State.GetInto("dts_seen", &seenList)
	}
	seen := map[string]bool{}
	for _, s := range seenList {
		seen[s] = true
	}
	speakers := "Music"
	for _, d := range devs {
		if d.Kind == "speakers" {
			speakers = d.Profile
			break
		}
	}
	want := ""
	for _, d := range devs {
		if d.Kind == "speakers" && d.Profile != "Plain" && d.Profile != "Off" {
			want = d.Profile
			break
		}
	}
	if want == "" && force {
		want = "Music"
	}
	done := []string{}
	for _, d := range devs {
		first := !seen[d.Ref]
		if (!first && !force) || d.Kind == "speakers" || d.Kind == "headphones" || (d.Kind == "bluetooth-speaker" && !force) {
			continue // AcerUltraX keeps Plain for Bluetooth speakers on purpose
		}
		seen[d.Ref] = true
		if want == "" || (d.Profile != "Plain" && d.Profile != "Off" && !force) {
			continue // speakers on Plain/Off = the user wants no DTS; a chosen profile stays
		}
		var bits []string
		if d.Profile != want {
			if _, err := c.dts(ctx, "mode", want, "--device", d.Ref); err != nil {
				return nil, err
			}
			bits = append(bits, want)
		}
		if d.Kind != "bluetooth-speaker" && isHeadsetLabel(d.Label) {
			for _, pick := range []string{"auto", "default"} { // "auto" is a no-op when AcerUltraX does not recognise the name
				if !c.hasEQ(d.Ref) {
					if _, err := c.dts(ctx, "headphone", pick, "--device", d.Ref); err != nil {
						return nil, err
					}
				}
			}
			bits = append(bits, "headphone EQ")
		}
		if len(bits) > 0 {
			done = append(done, d.Label+": "+strings.Join(bits, " + "))
		}
	}
	if c.State != nil {
		all := make([]string, 0, len(seen))
		for s := range seen {
			all = append(all, s)
		}
		sort.Strings(all)
		_ = c.State.Set("dts_seen", all) // a convenience file: failing to save it is not an error
	}
	if len(done) == 0 && force {
		return []string{fmt.Sprintf("every output is already on %s", speakers)}, nil
	}
	return done, nil
}

// PlainHeadsets lists the headset-like outputs left on the Plain profile (no DTS processing):
// the doctor's view.
func (c *Controller) PlainHeadsets(ctx context.Context) ([]string, error) {
	devs, err := c.DTSDevices(ctx)
	if err != nil {
		return nil, err
	}
	var labels []string
	for _, d := range devs {
		if d.Profile == "Plain" && d.Kind != "bluetooth-speaker" && isHeadsetLabel(d.Label) {
			labels = append(labels, d.Label)
		}
	}
	return labels, nil
}

// JackStatus is the doctor's view of the jack.
func (c *Controller) JackStatus(ctx context.Context) (headphones, headsetMic bool, err error) {
	j := c.Jack(ctx)
	return j.Headphones, j.HeadsetMic, nil
}
