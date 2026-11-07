package audio

import (
	"regexp"
	"strconv"
	"strings"
)

// Everything in this file is a pure function of the text a tool printed (pactl, amixer,
// acerultrax-cli, /proc/asound/cards, a PipeWire config). It never runs anything, so every
// format the sound system can produce is covered by a table test and a fuzz target.

// cardIndex finds the sound card TrueSense drives in /proc/asound/cards ("sof-hda-dsp"); "1"
// when the text does not name it (the card is index 1 on the PH16-71).
func cardIndex(cards string) string {
	for _, l := range strings.Split(cards, "\n") {
		if strings.Contains(l, "sof-hda-dsp") {
			if f := strings.Fields(l); len(f) > 0 {
				return f[0]
			}
		}
	}
	return "1"
}

// controlValue is the value of an `amixer cget` control: the text after ": values=" on its last
// line ("on", "off", "2", "1,1"). ok is false when amixer printed nothing.
func controlValue(out string) (value string, ok bool) {
	lines := strings.Split(strings.TrimSpace(out), "\n")
	last := strings.TrimSpace(lines[len(lines)-1])
	if last == "" {
		return "", false
	}
	return strings.ReplaceAll(last, ": values=", ""), true
}

var boostRE = regexp.MustCompile(`(?m)^\s*: values=(\d+)`)

// boostValue reads the ": values=N" line of a volume control, not the "values=2" channel count
// in its header.
func boostValue(out string) (int, bool) {
	m := boostRE.FindStringSubmatch(out)
	if m == nil {
		return 0, false
	}
	n, err := strconv.Atoi(m[1])
	return n, err == nil
}

// shortColumn returns column col (0-based, tab separated) of every line of a `pactl list short`
// output that has more than one column.
func shortColumn(out string, col int) []string {
	var names []string
	for _, l := range strings.Split(out, "\n") {
		if f := strings.Split(l, "\t"); len(f) > 1 && col < len(f) {
			names = append(names, f[col])
		}
	}
	return names
}

// shortSources is [id, name] of every real input of `pactl list short sources`: monitors of
// outputs are not microphones.
func shortSources(out string) [][2]string {
	var res [][2]string
	for _, l := range strings.Split(out, "\n") {
		f := strings.Split(l, "\t")
		if len(f) > 1 && !strings.Contains(l, "monitor") {
			res = append(res, [2]string{f[0], f[1]})
		}
	}
	return res
}

// firstFields is the first whitespace-separated word of every non-empty line (the id of
// `pactl list short sink-inputs`).
func firstFields(out string) []string {
	var ids []string
	for _, l := range strings.Split(out, "\n") {
		if f := strings.Fields(l); len(f) > 0 {
			ids = append(ids, f[0])
		}
	}
	return ids
}

// cardName finds the PipeWire name of the card whose short listing contains match.
func cardName(shortCards, match string) (string, bool) {
	for _, l := range strings.Split(shortCards, "\n") {
		if f := strings.Split(l, "\t"); len(f) > 1 && strings.Contains(l, match) {
			return f[1], true
		}
	}
	return "", false
}

// cardBlock is the part of `pactl list cards` that follows "Name: <card>", up to the next card
// when untilNext is set. ok is false when the card is not listed.
func cardBlock(listCards, card string, untilNext bool) (string, bool) {
	_, rest, found := strings.Cut(listCards, "Name: "+card)
	if !found {
		return "", false
	}
	if untilNext {
		rest, _, _ = strings.Cut(rest, "\nCard #")
	}
	return rest, true
}

// activeProfile is the "Active Profile:" of a card block ("?" when it has none).
func activeProfile(block string) string {
	for _, l := range strings.Split(block, "\n") {
		if strings.Contains(l, "Active Profile:") {
			_, v, _ := strings.Cut(l, ":")
			return strings.TrimSpace(v)
		}
	}
	return "?"
}

// profilesOffered lists the profile names under "Profiles:" of a card block.
func profilesOffered(block string) []string {
	var out []string
	on := false
	for _, line := range strings.Split(block, "\n") {
		s := strings.TrimSpace(line)
		switch {
		case strings.HasPrefix(s, "Profiles:"):
			on = true
		case strings.HasPrefix(s, "Active Profile:"):
			return out
		case on && s != "" && strings.Contains(s, ": "):
			name, _, _ := strings.Cut(s, ": ")
			out = append(out, name)
		}
	}
	return out
}

// sinkDescription is the "Description:" of the named sink in `pactl list sinks`, or the name
// itself when there is none.
func sinkDescription(listSinks, name string) string {
	_, rest, found := strings.Cut(listSinks, "Name: "+name)
	if !found {
		return name
	}
	for _, l := range strings.Split(rest, "\n") {
		if s := strings.TrimSpace(l); strings.HasPrefix(s, "Description:") {
			_, v, _ := strings.Cut(s, ":")
			return strings.TrimSpace(v)
		}
	}
	return name
}

// isMuted reads `pactl get-source-mute` ("Mute: yes").
func isMuted(out string) bool { return strings.Contains(out, "yes") }

var percentRE = regexp.MustCompile(`(\d+)%`)

// volumePercent is the first percentage of `pactl get-source-volume`.
func volumePercent(out string) (int, bool) {
	m := percentRE.FindStringSubmatch(out)
	if m == nil {
		return 0, false
	}
	n, err := strconv.Atoi(m[1])
	return n, err == nil
}

var thresholdRE = regexp.MustCompile(`"VAD Threshold \(%\)" = (\d+)`)

// voiceThreshold reads the VAD threshold out of a Purified Voice config; -1 when absent.
func voiceThreshold(conf string) int {
	m := thresholdRE.FindStringSubmatch(conf)
	if m == nil {
		return -1
	}
	n, err := strconv.Atoi(m[1])
	if err != nil {
		return -1
	}
	return n
}

// DTSProfiles are the sound profiles AcerUltraX knows.
var DTSProfiles = []string{"Off", "Music", "Movie", "Voice", "Game1", "Game2", "Game3", "Custom Audio", "Plain"}

// DTSDevice is one output AcerUltraX knows. The JSON names are the 0.3.x ones. Ref is what
// `--device` takes.
type DTSDevice struct {
	Label   string `json:"label"`
	Kind    string `json:"kind"`
	Profile string `json:"profile"`
	Ref     string `json:"ref"`
	Current bool   `json:"current"`
}

var (
	dtsRowRE  = regexp.MustCompile(`^(.*\S)\s+(speakers|headphones|bluetooth-speaker|bluetooth|other)\s+(` + strings.Join(DTSProfiles, "|") + `)\s*$`)
	headsetRE = regexp.MustCompile(`(?i)head(set|phone)|ear(phone|bud|piece)|buds|airpods|galea|gaming|dualsense|pc38`)
)

// isHeadsetLabel tells headset-like outputs (the ones that get a headphone EQ) from the rest.
func isHeadsetLabel(label string) bool { return headsetRE.MatchString(label) }

// parseDTSDevices reads `acerultrax-cli devices`: a row per output ("* " marks the current
// one) followed by an indented line with the reference `--device` takes. A row without a
// reference is dropped.
func parseDTSDevices(out string) []DTSDevice {
	var devs []DTSDevice
	for _, line := range strings.Split(out, "\n") {
		line = strings.TrimRight(line, "\r")
		if strings.HasPrefix(line, "    ") && len(devs) > 0 {
			devs[len(devs)-1].Ref = strings.TrimSpace(line)
			continue
		}
		if strings.TrimSpace(line) == "" {
			continue
		}
		if m := dtsRowRE.FindStringSubmatch(strings.TrimLeft(line, "* ")); m != nil {
			devs = append(devs, DTSDevice{Label: strings.TrimSpace(m[1]), Kind: m[2], Profile: m[3], Current: strings.HasPrefix(line, "*")})
		}
	}
	kept := devs[:0]
	for _, d := range devs {
		if d.Ref != "" {
			kept = append(kept, d)
		}
	}
	return kept
}

// lastLine is the last line of text, or "" for blank text.
func lastLine(text string) string {
	lines := strings.Split(strings.TrimSpace(text), "\n")
	return strings.TrimSpace(lines[len(lines)-1])
}
