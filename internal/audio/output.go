package audio

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"
)

// OutputStatus says which card profile is active (the laptop plays on the speakers OR the
// headphone jack, never both) and whether something is plugged into the jack.
type OutputStatus struct {
	Active      string `json:"active"`
	JackPlugged bool   `json:"jack_plugged"`
}

var errNoCard = errors.New("sound card not found")

// cardOf finds the PipeWire name of the sound card.
func (c *Controller) cardOf(ctx context.Context) (string, bool) {
	return cardName(c.out(ctx, "pactl", "list", "short", "cards"), cardMatch)
}

// Output reads the active output profile.
func (c *Controller) Output(ctx context.Context) (OutputStatus, error) {
	card, ok := c.cardOf(ctx)
	if !ok {
		return OutputStatus{}, errNoCard
	}
	block, ok := cardBlock(c.out(ctx, "pactl", "list", "cards"), card, false)
	if !ok {
		return OutputStatus{}, errNoCard
	}
	profile := activeProfile(block)
	active := profile
	for which, p := range profileOf {
		if p == profile {
			active = which
		}
	}
	return OutputStatus{Active: active, JackPlugged: c.Jack(ctx).Headphones}, nil
}

// OutputActive is the active profile's name, for callers that need nothing else.
func (c *Controller) OutputActive(ctx context.Context) (string, error) {
	st, err := c.Output(ctx)
	return st.Active, err
}

// SetOutput switches between "speakers" and "headphones". They share one playback PCM, so ALSA
// offers them as two exclusive card profiles; flipping the profile rebinds the stream. It
// waits until the new sink exists (measured: about 0.04 s) instead of a fixed sleep, and
// verifies the result.
func (c *Controller) SetOutput(ctx context.Context, which string) (string, error) {
	profile, ok := profileOf[which]
	if !ok {
		return "", errors.New("output: speakers | headphones")
	}
	card, ok := c.cardOf(ctx)
	if !ok {
		return "", errNoCard
	}
	end := c.quiet()
	defer end()
	c.run(ctx, "pactl", "set-card-profile", card, profile)
	want := "Speaker__sink"
	if which == "headphones" {
		want = "Headphones__sink"
	}
	for i := 0; i < 100; i++ {
		if strings.Contains(c.out(ctx, "pactl", "list", "short", "sinks"), want) {
			break
		}
		c.sleep(ctx, 30*time.Millisecond)
	}
	st, err := c.Output(ctx)
	if err != nil {
		return "", err
	}
	if st.Active != which {
		return "", fmt.Errorf("profile readback %s != %s", st.Active, which)
	}
	return st.Active, nil
}

func (c *Controller) sinks(ctx context.Context) []string {
	return shortColumn(c.out(ctx, "pactl", "list", "short", "sinks"), 1)
}

func (c *Controller) defaultSink(ctx context.Context) string {
	return strings.TrimSpace(c.out(ctx, "pactl", "get-default-sink"))
}

// OutputToggle flips between the speakers and the headphones and returns the new output.
func (c *Controller) OutputToggle(ctx context.Context) (string, error) {
	cur, err := c.Output(ctx)
	if err != nil {
		return "", err
	}
	next := "speakers"
	if cur.Active == "speakers" {
		next = "headphones"
	}
	noctalia := c.noctaliaStyle()
	if !noctalia {
		c.say("Audio output: "+Label(next), Icon(next)) // instant, before the ~1 s switch
	}
	if _, err := c.SetOutput(ctx, next); err != nil {
		return "", err
	}
	if noctalia {
		c.say(Label(next), Icon(next))
	}
	return next, nil
}

// OutputCycle is the F5-style round robin: laptop speakers -> headphones (only if the jack is
// plugged in) -> other devices; it flips the card profile when needed and returns the label of
// the new output. The AcerUltraX virtual sink (if present) stays the default so the DTS
// processing keeps working; it follows the hardware output.
func (c *Controller) OutputCycle(ctx context.Context) (string, error) {
	if c.Lock != nil {
		unlock, err := c.Lock() // two quick presses must not interleave
		if err != nil {
			return "", err
		}
		defer unlock()
	}
	end := c.quiet()
	defer end()
	return c.outputCycle(ctx)
}

func (c *Controller) outputCycle(ctx context.Context) (string, error) {
	var sinks, others []string
	for _, n := range c.sinks(ctx) {
		if !strings.Contains(strings.ToLower(n), "hdmi") {
			sinks = append(sinks, n)
		}
	}
	front := ""
	for _, n := range sinks {
		if n == frontSink {
			front = frontSink
		} else if !strings.Contains(n, cardMatch) {
			others = append(others, n)
		}
	}
	stops := []string{"int:speakers"}
	if c.Jack(ctx).Headphones {
		stops = append(stops, "int:headphones")
	}
	stops = append(stops, others...)

	cur := c.defaultSink(ctx)
	if cur == frontSink || strings.Contains(cur, cardMatch) {
		active := "speakers"
		if st, err := c.Output(ctx); err == nil {
			if _, known := profileOf[st.Active]; known {
				active = st.Active
			}
		} else {
			return "", err
		}
		cur = "int:" + active
	}
	idx := -1
	for i, s := range stops {
		if s == cur {
			idx = i
		}
	}
	next := stops[(idx+1)%len(stops)]
	internal := strings.HasPrefix(next, "int:")
	which := strings.TrimPrefix(next, "int:")
	label, icon := Label(which), Icon(which)
	if !internal {
		label, icon = sinkDescription(c.out(ctx, "pactl", "list", "sinks"), next), "audio"
	}
	noctalia := c.noctaliaStyle()
	if !noctalia {
		c.say("Audio output: "+label, icon)
	}
	target := next
	if internal {
		if _, err := c.SetOutput(ctx, which); err != nil {
			return "", err
		}
		part := "Speaker"
		if which == "headphones" {
			part = "Headphones"
		}
		target = front
		if target == "" {
			for _, n := range c.sinks(ctx) {
				if strings.Contains(n, cardMatch) && strings.Contains(n, part) {
					target = n
					break
				}
			}
		}
	}
	if target == "" {
		return "", errors.New("no sink to switch to")
	}
	c.run(ctx, "pactl", "set-default-sink", target)
	for _, id := range firstFields(c.out(ctx, "pactl", "list", "short", "sink-inputs")) {
		c.run(ctx, "pactl", "move-sink-input", id, target)
	}
	if noctalia {
		c.say(label, icon)
	}
	return label, nil
}
