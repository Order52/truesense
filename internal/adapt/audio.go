package adapt

import (
	"context"
	"strings"

	"github.com/Order52/truesense/internal/api"
	"github.com/Order52/truesense/internal/tui"
)

// ---- AudioBackend

func (b *backend) SoundCard(ctx context.Context) (tui.SoundCard, error) {
	st, err := b.a.SoundState(ctx)
	switch {
	case err != nil:
		return tui.SoundCard{}, err
	case st.OK == nil:
		return tui.SoundCard{Status: tui.SoundMissing, Problem: st.Problem}, nil
	case *st.OK:
		return tui.SoundCard{Status: tui.SoundOK}, nil
	}
	return tui.SoundCard{Status: tui.SoundBroken, Problem: st.Problem}, nil
}

func (b *backend) RepairSound(ctx context.Context) (tui.RepairResult, error) {
	r, err := b.a.RepairSound(ctx)
	return tui.RepairResult{Fixed: r.Fixed, Changed: r.Changed, Steps: r.Steps}, err
}

func (b *backend) Output(ctx context.Context) (string, error) {
	o, err := b.a.AudioOutput(ctx)
	return o.Active, err
}

func (b *backend) SetOutput(ctx context.Context, output string) error {
	_, err := b.a.SetAudioOutput(ctx, output)
	return err
}

func (b *backend) SpeakerFilter(context.Context) (bool, error) { return b.a.SpeakerFilter(), nil }

func (b *backend) SetSpeakerFilter(ctx context.Context, on bool) error {
	_, err := b.a.SetSpeakerFilter(ctx, on)
	return err
}

func (b *backend) DTSOn(ctx context.Context) (bool, error) {
	s, err := b.a.DTSStatus(ctx)
	return api.DTSOn(s), err
}

func (b *backend) SetDTS(ctx context.Context, on bool) error {
	_, err := b.a.SetDTSPower(ctx, on)
	return err
}

func (b *backend) DTSDevices(ctx context.Context) ([]tui.DTSDevice, error) {
	ds, err := b.a.DTSDevices(ctx)
	out := make([]tui.DTSDevice, len(ds))
	for i, d := range ds {
		out[i] = tui.DTSDevice{Label: d.Label, Profile: d.Profile, Current: d.Current}
	}
	return out, err
}

func (b *backend) SetDTSMode(ctx context.Context, mode string) error {
	_, err := b.a.SetDTSMode(ctx, mode)
	return err
}

func (b *backend) DTSEverywhere(ctx context.Context) (string, error) {
	done, err := b.a.DTSEverywhere(ctx, "")
	return strings.Join(done, "; "), err
}

func (b *backend) Microphone(ctx context.Context) (tui.Microphone, error) {
	m := b.a.Mic(ctx)
	mic := tui.Microphone{Muted: m.Muted}
	if m.Volume != nil {
		mic.Volume = tui.Some(*m.Volume)
	}
	return mic, nil
}

func (b *backend) SetMicMuted(ctx context.Context, muted bool) error {
	return b.a.SetMicMute(ctx, muted, false)
}

func (b *backend) SetMicVolume(ctx context.Context, percent int) error {
	_, err := b.a.SetMicVolume(ctx, percent)
	return err
}

func (b *backend) VoiceModes(context.Context) (tui.VoiceModes, error) {
	m := b.a.VoiceModes()
	return tui.VoiceModes{Headset: m["headset"], Array: m["array"]}, nil
}

// SetVoiceMode sets the noise reduction of one input and points the default microphone at
// the right input.
func (b *backend) SetVoiceMode(ctx context.Context, input, mode string) error {
	_, err := b.a.SetVoice(ctx, input, mode)
	return err
}
