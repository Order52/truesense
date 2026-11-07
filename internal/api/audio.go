package api

import (
	"context"
	"fmt"
	"regexp"
	"time"

	"github.com/Order52/truesense/internal/audio"
	"github.com/Order52/truesense/internal/config"
)

// AudioOutput says which output profile is active (speakers or headphones) and whether
// something is plugged into the jack.
func (a *API) AudioOutput(ctx context.Context) (audio.OutputStatus, error) {
	return a.s.Audio.Output(ctx)
}

// SetAudioOutput switches between "speakers" and "headphones" (shows the on-screen message
// first: the switch takes about a second) and answers the output now active.
func (a *API) SetAudioOutput(ctx context.Context, which string) (string, error) {
	return a.s.Audio.SetOutput(ctx, which)
}

// ToggleAudioOutput flips between the speakers and the headphones.
func (a *API) ToggleAudioOutput(ctx context.Context) (audio.OutputStatus, error) {
	if _, err := a.s.Audio.OutputToggle(ctx); err != nil {
		return audio.OutputStatus{}, err
	}
	return a.s.Audio.Output(ctx)
}

// CycleAudioOutput is the F5 round robin over every output and answers the new one's label.
func (a *API) CycleAudioOutput(ctx context.Context) (string, error) {
	return a.s.Audio.OutputCycle(ctx)
}

// Mic reads the default microphone and every input.
func (a *API) Mic(ctx context.Context) audio.MicState { return a.s.Audio.Mic(ctx) }

// SetMicMute mutes or unmutes every input, or only the default one.
func (a *API) SetMicMute(ctx context.Context, muted, defaultOnly bool) error {
	if defaultOnly {
		return a.s.Audio.SetDefaultMicMute(ctx, muted)
	}
	return a.s.Audio.SetMicMute(ctx, muted)
}

// ToggleMicMute flips the mute state (of every input unless defaultOnly) and answers the new state.
func (a *API) ToggleMicMute(ctx context.Context, defaultOnly bool) (bool, error) {
	return a.s.Audio.ToggleMicMute(ctx, defaultOnly)
}

// MicVolume reads the default microphone's volume in percent (nil when unreadable).
func (a *API) MicVolume(ctx context.Context) *int { return a.s.Audio.MicVolume(ctx) }

// SetMicVolume sets the default microphone's volume (0..150 %) and answers what it reads back.
func (a *API) SetMicVolume(ctx context.Context, percent int) (*int, error) {
	return a.s.Audio.SetMicVolume(ctx, percent)
}

// HeadsetBoost reads the headset microphone boost (0..3, +10 dB per step); nil when the card
// has no such control.
func (a *API) HeadsetBoost(ctx context.Context) (*int, error) {
	level, found, err := a.s.Audio.HeadsetBoost(ctx)
	if err != nil || !found {
		return nil, err
	}
	return &level, nil
}

// SetHeadsetBoost sets the boost and reads it back; a value that did not stick is an error.
func (a *API) SetHeadsetBoost(ctx context.Context, level int) error {
	return a.s.Audio.SetHeadsetBoost(ctx, level)
}

// JackReport is what the codec sees on the 3.5 mm jack, how the jack is configured to be used
// and which microphone that makes the one in use.
type JackReport struct {
	audio.Jack
	Setting         string `json:"setting"`
	MicrophoneInUse string `json:"microphone_in_use"`
}

// Jack reads the jack controls and the configured use.
func (a *API) Jack(ctx context.Context) JackReport {
	j := a.s.Audio.Jack(ctx)
	return JackReport{Jack: j, Setting: a.cfg().Audio.Jack, MicrophoneInUse: a.s.Audio.JackUse(ctx, &j)}
}

// SetJack chooses how the jack is used (auto, headphones or headset: always use the headset
// microphone) and points the default microphone accordingly.
func (a *API) SetJack(ctx context.Context, use string) error {
	if err := config.WriteUser(map[string]any{"audio": map[string]any{"jack": use}}, a.UserConfig); err != nil {
		return err
	}
	_, err := a.s.Audio.ApplyMicPolicy(ctx, false)
	return err
}

// VoiceStatus is Purified Voice: the mode of each input, whether a headset microphone is
// detected, the default microphone and whether mode changes are announced.
type VoiceStatus struct {
	Modes         map[string]string `json:"modes"`
	Jack          bool              `json:"jack"`
	DefaultSource string            `json:"default_source"`
	Notify        bool              `json:"notify"`
}

// VoiceStatus reads Purified Voice.
func (a *API) VoiceStatus(ctx context.Context) VoiceStatus {
	return VoiceStatus{Modes: a.s.Audio.VoiceModes(), Jack: a.s.Audio.Jack(ctx).HeadsetMic, DefaultSource: a.s.Audio.DefaultSource(ctx), Notify: a.cfg().Audio.Notify}
}

// VoiceModes reads each input's noise-reduction mode.
func (a *API) VoiceModes() map[string]string { return a.s.Audio.VoiceModes() }

// SetVoice sets one input ("headset" or "array") to off, conference or personal and points the
// default microphone at the right input.
func (a *API) SetVoice(ctx context.Context, input, mode string) (string, error) {
	if input == "" || mode == "" {
		return "", fmt.Errorf("usage: truesense audio voice set headset|array off|conference|personal")
	}
	got, err := a.s.Audio.VoiceSet(ctx, input, mode)
	if err != nil {
		return "", err
	}
	_, err = a.s.Audio.ApplyMicPolicy(ctx, false)
	return got, err
}

// EnableVoice restores the configured modes of both inputs; DisableVoice leaves both unfiltered.
func (a *API) EnableVoice(ctx context.Context, on bool) (map[string]string, error) {
	modes, err := a.s.Audio.VoiceEnable(ctx, on)
	if err != nil {
		return nil, err
	}
	_, err = a.s.Audio.ApplyMicPolicy(ctx, false)
	return modes, err
}

// NextVoice cycles the mode of the input in use (off, conference, personal) and answers the
// input and its new mode.
func (a *API) NextVoice(ctx context.Context) (device, mode string, err error) {
	device, mode, err = a.s.Audio.VoiceNext(ctx, "")
	if err != nil {
		return "", "", err
	}
	_, err = a.s.Audio.ApplyMicPolicy(ctx, false)
	return device, mode, err
}

// ApplyMicPolicy points the default microphone at the right input, announcing it.
func (a *API) ApplyMicPolicy(ctx context.Context) (audio.MicPolicy, error) {
	return a.s.Audio.ApplyMicPolicy(ctx, true)
}

// SetVoiceNotify switches the on-screen message of microphone mode changes.
func (a *API) SetVoiceNotify(on bool) error {
	return config.WriteUser(map[string]any{"audio": map[string]any{"notify": on}}, a.UserConfig)
}

// WatchMic keeps the default microphone on the right input until ctx ends: it re-picks it
// whenever the headset is plugged or a filter changes, and gives new outputs DTS once.
func (a *API) WatchMic(ctx context.Context) { a.s.Audio.NewMicWatcher().Run(ctx) }

// SoundState judges whether the sound card has an output.
func (a *API) SoundState(ctx context.Context) (audio.SoundState, error) {
	return a.s.Audio.SoundState(ctx)
}

// RepairSound brings the sound output back (restarting WirePlumber if needed) and verifies it.
func (a *API) RepairSound(ctx context.Context) (audio.RepairResult, error) {
	return a.s.Audio.Repair(ctx, audio.RepairOptions{})
}

// SpeakerFilter reports whether the speaker noise filter is configured.
func (a *API) SpeakerFilter() bool { return a.s.Audio.SpeakerFilterOn() }

// SetSpeakerFilter switches the noise filter on the output (off by default: it also gates
// music) and answers its new state.
func (a *API) SetSpeakerFilter(ctx context.Context, on bool) (bool, error) {
	return a.s.Audio.SpeakerFilter(ctx, on, "conference")
}

const dtsTimeout = 30 * time.Second

// dtsOn finds "DTS: on" in AcerUltraX's status text, whatever the column spacing.
var dtsOn = regexp.MustCompile(`(?m)\bDTS:\s+on\b`)

// DTSOn reports whether AcerUltraX's status text says DTS is on.
func DTSOn(status string) bool { return dtsOn.MatchString(status) }

// DTSStatus is AcerUltraX's own status text.
func (a *API) DTSStatus(ctx context.Context) (string, error) { return a.s.Audio.DTSStatus(ctx) }

// SetDTSPower switches DTS on or off and answers AcerUltraX's message.
func (a *API) SetDTSPower(ctx context.Context, on bool) (string, error) {
	return a.s.Audio.DTSPower(ctx, on)
}

// SetDTSMode picks a DTS profile (music, movie, voice, game1..3, off, plain, "custom audio")
// for every output, or only the one playing when [audio] dts_all is off.
func (a *API) SetDTSMode(ctx context.Context, name string) (string, error) {
	if name == "" {
		return "", fmt.Errorf(`usage: truesense audio dts mode music|movie|voice|game1|game2|game3|off|plain|"custom audio"   (every output; [audio] dts_all = false: only the one playing)`)
	}
	return a.s.Audio.DTSMode(ctx, name, audio.DTSConfigured)
}

// DTSProfiles lists the sound profiles AcerUltraX knows, in its own words.
func (a *API) DTSProfiles(ctx context.Context) (string, error) {
	return a.s.Audio.DTS(ctx, dtsTimeout, "profiles")
}

// DTSDevices lists every output AcerUltraX knows and its profile.
func (a *API) DTSDevices(ctx context.Context) ([]audio.DTSDevice, error) {
	return a.s.Audio.DTSDevices(ctx)
}

// DTSEverywhere gives every output DTS now (each handled once) and, with a mode, sets it on
// every output. It answers what changed.
func (a *API) DTSEverywhere(ctx context.Context, mode string) ([]string, error) {
	done, err := a.s.Audio.DTSCover(ctx, true)
	if err != nil {
		return nil, err
	}
	if mode != "" {
		if _, err := a.s.Audio.DTSMode(ctx, mode, audio.DTSEverywhere); err != nil {
			return nil, err
		}
		done = append(done, "every output: "+mode)
	}
	return done, nil
}

// SetupDTS runs AcerUltraX's installer-side setup (it can take half an hour).
func (a *API) SetupDTS(ctx context.Context) (string, error) {
	return a.s.Audio.DTS(ctx, 30*time.Minute, "setup")
}
