package audio

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func TestMicState(t *testing.T) {
	r := newRig(t, false)
	r.sys.muted[testSrc2] = true
	r.sys.extraSources = []string{"alsa_input.usb-Logitech_Webcam-02.mono-fallback"}
	st := r.c.Mic(ctx())
	if st.Default != testSrc1 || st.Muted || st.Volume == nil || *st.Volume != 100 {
		t.Fatalf("state = %+v", st)
	}
	want := map[string]bool{"Mic1__source": false, "Mic2__source": true, "alsa_input.usb-Logitech_Webcam-02.mono-fallback": false}
	if !reflect.DeepEqual(st.Sources, want) {
		t.Errorf("sources = %v", st.Sources)
	}
	if got := r.c.DefaultSource(ctx()); got != testSrc1 {
		t.Errorf("default = %q", got)
	}
	if m, _ := r.c.MicMuted(ctx()); m {
		t.Error("not muted")
	}
	r.sys.volume = "Volume: unavailable\n"
	if st = r.c.Mic(ctx()); st.Volume != nil {
		t.Errorf("volume = %v", *st.Volume)
	}
}

func TestMuteEveryInputOrOnlyTheDefault(t *testing.T) {
	r := newRig(t, false)
	r.sys.extraSources = []string{"bluez_input.AA.1"}
	if err := r.c.SetMicMute(ctx(), true); err != nil {
		t.Fatal(err)
	}
	for _, src := range []string{testSrc1, testSrc2, "bluez_input.AA.1"} {
		if !r.sys.did("pactl", "set-source-mute", src, "1") || !r.sys.muted[src] {
			t.Errorf("%s not muted: %v", src, r.sys.sent())
		}
	}
	if r.sys.did("pactl", "set-source-mute", testHW+".monitor") {
		t.Error("an output's monitor is not a microphone")
	}
	r.c.SetMicMute(ctx(), false)
	if r.sys.muted[testSrc1] || r.sys.muted[testSrc2] {
		t.Error("not unmuted")
	}
	r.sys.calls = nil
	r.c.SetDefaultMicMute(ctx(), true)
	if got := r.sys.sent(); len(got) < 1 || !r.sys.did("pactl", "set-source-mute", testSrc1, "1") || r.sys.muted[testSrc2] {
		t.Errorf("default only: %v", got)
	}
}

func TestToggleMicMute(t *testing.T) {
	r := newRig(t, false)
	if m, err := r.c.ToggleMicMute(ctx(), false); err != nil || !m || !r.sys.muted[testSrc2] {
		t.Fatalf("toggle = %v, %v", m, err)
	}
	if m, _ := r.c.ToggleMicMute(ctx(), false); m || r.sys.muted[testSrc2] {
		t.Fatalf("toggle back = %v", m)
	}
	if m, _ := r.c.ToggleMicMute(ctx(), true); !m || r.sys.muted[testSrc2] || !r.sys.muted[testSrc1] {
		t.Fatalf("default only toggle = %v %v", m, r.sys.muted)
	}
}

func TestMicVolume(t *testing.T) {
	r := newRig(t, false)
	v, err := r.c.SetMicVolume(ctx(), 75)
	if err != nil || v == nil || *v != 75 {
		t.Fatalf("volume = %v, %v", v, err)
	}
	if !r.sys.did("wpctl", "set-volume", "@DEFAULT_AUDIO_SOURCE@", "75%") {
		t.Errorf("sent %v", r.sys.sent())
	}
	if got := r.c.MicVolume(ctx()); got == nil || *got != 75 {
		t.Errorf("read = %v", got)
	}
	for _, bad := range []int{-1, 151} {
		r.sys.calls = nil
		if _, err := r.c.SetMicVolume(ctx(), bad); err == nil || !strings.Contains(err.Error(), "0..150") || len(r.sys.calls) != 0 {
			t.Errorf("volume %d: %v", bad, err)
		}
	}
	if _, err := r.c.SetMicVolume(ctx(), 150); err != nil {
		t.Errorf("150 %% is allowed: %v", err)
	}
	if _, err := r.c.SetMicVolume(ctx(), 0); err != nil {
		t.Errorf("0 %% is allowed: %v", err)
	}
}

func readConf(t *testing.T, r *rig, dev string) string {
	t.Helper()
	b, err := os.ReadFile(filepath.Join(r.dir, "pw", "truesense-pv-"+dev+".conf"))
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

func TestVoiceConfigIsByteForByteWhatTheOldVersionWrote(t *testing.T) {
	want := `context.modules = [
  { name = libpipewire-module-filter-chain
    args = {
      node.description = "Purified Voice (headset mic)"
      media.name = "Purified Voice (headset mic)"
      filter.graph = {
        nodes = [ { type = ladspa name = rnnoise plugin = "/usr/lib/ladspa/librnnoise_ladspa.so" label = noise_suppressor_stereo
                    control = { "VAD Threshold (%)" = 50 "VAD Grace Period (ms)" = 20 "Retroactive VAD Grace (ms)" = 0 } } ]
      }
      capture.props = {
                node.passive = true
                target.object = "alsa_input.pci-0000_00_1f.3-platform-skl_hda_dsp_generic.HiFi__Mic2__source"
      }
      playback.props = {
                media.class = Audio/Source
                node.name = "truesense_pv_headset"
                node.description = "Purified Voice (headset mic)"
                audio.position = [ FL FR ]
      }
    }
  }
]
`
	if got := voiceConf(voiceDevices[0], voiceModes["conference"], "/usr/lib/ladspa/librnnoise_ladspa.so"); got != want {
		t.Fatalf("headset conference config differs:\n%s", got)
	}
	arr := voiceConf(voiceDevices[1], voiceModes["personal"], "/p.so")
	for _, s := range []string{`"VAD Threshold (%)" = 85 "VAD Grace Period (ms)" = 150 "Retroactive VAD Grace (ms)" = 100`, `node.name = "truesense_pv_array"`, "HiFi__Mic1__source", "Purified Voice (mic array)", `plugin = "/p.so"`} {
		if !strings.Contains(arr, s) {
			t.Errorf("array personal config lacks %q", s)
		}
	}
	spk := speakerConf(voiceModes["conference"], "/p.so")
	wantSpk := `context.modules = [
  { name = libpipewire-module-filter-chain
    args = {
      node.description = "Purified Voice Speaker"
      filter.graph = { nodes = [ { type = ladspa name = rnnoise plugin = "/p.so" label = noise_suppressor_stereo
                    control = { "VAD Threshold (%)" = 50 "VAD Grace Period (ms)" = 20 "Retroactive VAD Grace (ms)" = 0 } } ] }
      capture.props = { media.class = Audio/Sink node.name = "truesense_pv_speaker" node.description = "Purified Voice Speaker" audio.position = [ FL FR ] }
      playback.props = { node.passive = true }
    }
  }
]
`
	if spk != wantSpk {
		t.Fatalf("speaker config differs:\n%s", spk)
	}
}

func TestVoiceSetWritesTheFilterAndRestartsPipeWireOnlyWhenItChanges(t *testing.T) {
	r := newRig(t, false)
	if m, err := r.c.VoiceSet(ctx(), "headset", "conference"); err != nil || m != "conference" {
		t.Fatalf("VoiceSet = %q, %v", m, err)
	}
	if !strings.Contains(readConf(t, r, "headset"), `"VAD Threshold (%)" = 50`) || r.sys.count("systemctl", "--user", "restart", "pipewire.service") != 1 {
		t.Fatalf("sent %v", r.sys.sent())
	}
	if got := r.c.VoiceModes(); !reflect.DeepEqual(got, map[string]string{"headset": "conference", "array": "off"}) {
		t.Errorf("modes = %v", got)
	}
	// the same again: no restart
	r.c.VoiceSet(ctx(), "headset", "conference")
	if n := r.sys.count("systemctl", "--user", "restart", "pipewire.service"); n != 1 {
		t.Errorf("an unchanged config restarted PipeWire (%d restarts)", n)
	}
	r.c.VoiceSet(ctx(), "headset", "personal")
	if n := r.sys.count("systemctl", "--user", "restart", "pipewire.service"); n != 2 {
		t.Errorf("a changed mode must restart once more, got %d", n)
	}
	if r.c.VoiceModes()["headset"] != "personal" {
		t.Errorf("modes = %v", r.c.VoiceModes())
	}
	// off removes the file and restarts; off again does nothing
	r.c.VoiceSet(ctx(), "headset", "off")
	if exists(filepath.Join(r.dir, "pw", "truesense-pv-headset.conf")) || r.sys.count("systemctl", "--user", "restart", "pipewire.service") != 3 {
		t.Errorf("off: %v", r.sys.sent())
	}
	if m, _ := r.c.VoiceSet(ctx(), "headset", "off"); m != "off" || r.sys.count("systemctl", "--user", "restart", "pipewire.service") != 3 {
		t.Error("off twice restarted PipeWire")
	}
}

func TestVoiceSetRefusals(t *testing.T) {
	r := newRig(t, false)
	for _, tc := range []struct{ dev, mode, want string }{
		{"speaker", "off", "headset | array"}, {"headset", "loud", "off | conference | personal"}, {"", "off", "headset | array"},
	} {
		if _, err := r.c.VoiceSet(ctx(), tc.dev, tc.mode); err == nil || !strings.Contains(err.Error(), tc.want) {
			t.Errorf("%+v: %v", tc, err)
		}
	}
	r.c.Plugin = ""
	if _, err := r.c.VoiceSet(ctx(), "headset", "conference"); err == nil || !strings.Contains(err.Error(), "RNNoise LADSPA plugin missing") {
		t.Errorf("no plugin: %v", err)
	}
	if _, err := r.c.VoiceSet(ctx(), "headset", "off"); err != nil {
		t.Errorf("off needs no plugin: %v", err)
	}
	if r.sys.did("systemctl") {
		t.Error("a refused change restarted PipeWire")
	}
}

func TestVoiceModesReadsWhatWasWritten(t *testing.T) {
	r := newRig(t, false)
	r.c.VoiceSet(ctx(), "array", "personal")
	r.c.VoiceSet(ctx(), "headset", "conference")
	if got := r.c.VoiceModes(); got["array"] != "personal" || got["headset"] != "conference" {
		t.Errorf("modes = %v", got)
	}
	if err := os.WriteFile(filepath.Join(r.dir, "pw", "truesense-pv-array.conf"), []byte("# someone else's config\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if got := r.c.VoiceModes()["array"]; got != "?" {
		t.Errorf("a foreign config = %q", got)
	}
	if err := os.WriteFile(filepath.Join(r.dir, "pw", "truesense-pv-array.conf"), []byte(`"VAD Threshold (%)" = 12`), 0o600); err != nil {
		t.Fatal(err)
	}
	if got := r.c.VoiceModes()["array"]; got != "?" {
		t.Errorf("an unknown threshold = %q", got)
	}
}

func TestVoiceEnableRestoresTheConfiguredModes(t *testing.T) {
	r := newRig(t, false)
	r.cfg.Audio.HeadsetMode, r.cfg.Audio.ArrayMode = "personal", "conference"
	modes, err := r.c.VoiceEnable(ctx(), true)
	if err != nil || modes["headset"] != "personal" || modes["array"] != "conference" {
		t.Fatalf("enable = %v, %v", modes, err)
	}
	modes, err = r.c.VoiceEnable(ctx(), false)
	if err != nil || modes["headset"] != "off" || modes["array"] != "off" {
		t.Fatalf("disable = %v, %v", modes, err)
	}
	r.cfg.Audio.HeadsetMode = "bogus"
	if _, err := r.c.VoiceEnable(ctx(), true); err == nil {
		t.Error("a bad configured mode must surface")
	}
}

func TestVoiceNextCyclesTheModeOfTheInputInUse(t *testing.T) {
	r := newRig(t, false)
	var seen []string
	for i := 0; i < 4; i++ {
		dev, mode, err := r.c.VoiceNext(ctx(), "")
		if err != nil || dev != "array" {
			t.Fatalf("next = %q %q %v", dev, mode, err)
		}
		seen = append(seen, mode)
	}
	if !reflect.DeepEqual(seen, []string{"conference", "personal", "off", "conference"}) {
		t.Errorf("cycle = %v", seen)
	}
	r.sys.headsetMicJack = "on"
	if dev, mode, _ := r.c.VoiceNext(ctx(), ""); dev != "headset" || mode != "conference" {
		t.Errorf("with a headset plugged: %q %q", dev, mode)
	}
	if dev, _, _ := r.c.VoiceNext(ctx(), "array"); dev != "array" {
		t.Errorf("explicit device: %q", dev)
	}
	if _, _, err := r.c.VoiceNext(ctx(), "speaker"); err == nil {
		t.Error("an unknown device")
	}
}

func TestApplyMicPolicy(t *testing.T) {
	t.Run("nothing plugged: the raw array mic", func(t *testing.T) {
		r := newRig(t, false)
		r.sys.defaultSource = testSrc2
		r.sys.sourceOutputs = []string{"5\t90\tPipeWire\tzoom"}
		p, err := r.c.ApplyMicPolicy(ctx(), true)
		if err != nil || p.Device != "array" || p.Mode != "off" || p.DefaultSource != testSrc1 {
			t.Fatalf("policy = %+v, %v", p, err)
		}
		if r.sys.defaultSource != testSrc1 || !r.sys.did("pactl", "move-source-output", "5", testSrc1) {
			t.Errorf("sent %v", r.sys.sent())
		}
		if !reflect.DeepEqual(r.osd.shown, []string{"mic: Microphone: Mic array - All Off"}) {
			t.Errorf("osd = %v", r.osd.shown)
		}
	})
	t.Run("headset plugged with a filter: the filter node", func(t *testing.T) {
		r := newRig(t, false)
		r.sys.headsetMicJack = "on"
		r.c.VoiceSet(ctx(), "headset", "personal") // the fake PipeWire now has the node
		p, _ := r.c.ApplyMicPolicy(ctx(), true)
		if p.Device != "headset" || p.Mode != "personal" || p.DefaultSource != "truesense_pv_headset" || r.sys.defaultSource != "truesense_pv_headset" {
			t.Fatalf("policy = %+v default %s", p, r.sys.defaultSource)
		}
		if got := r.osd.shown[len(r.osd.shown)-1]; got != "mic: Microphone: Headset - Personal Call" {
			t.Errorf("osd = %q", got)
		}
	})
	t.Run("a filter that is configured but whose node is not there yet: the raw mic", func(t *testing.T) {
		r := newRig(t, false)
		r.sys.confDir = "" // PipeWire has not created the node
		r.c.VoiceSet(ctx(), "array", "conference")
		p, _ := r.c.ApplyMicPolicy(ctx(), false)
		if p.DefaultSource != testSrc1 || p.Mode != "conference" || len(r.osd.shown) != 0 {
			t.Fatalf("policy = %+v osd %v", p, r.osd.shown)
		}
	})
	t.Run("already right: nothing is moved", func(t *testing.T) {
		r := newRig(t, false)
		r.sys.sourceOutputs = []string{"5\tx"}
		r.c.ApplyMicPolicy(ctx(), false)
		if r.sys.did("pactl", "set-default-source") || r.sys.did("pactl", "move-source-output") {
			t.Errorf("sent %v", r.sys.sent())
		}
	})
	t.Run("the setting forces the headset or the array", func(t *testing.T) {
		r := newRig(t, false)
		r.cfg.Audio.Jack = "headset"
		if p, _ := r.c.ApplyMicPolicy(ctx(), false); p.Device != "headset" || p.DefaultSource != testSrc2 {
			t.Errorf("policy = %+v", p)
		}
		r.cfg.Audio.Jack = "headphones"
		r.sys.headsetMicJack = "on"
		if p, _ := r.c.ApplyMicPolicy(ctx(), false); p.Device != "array" {
			t.Errorf("policy = %+v", p)
		}
	})
	t.Run("a mode label for a foreign config does not crash", func(t *testing.T) {
		r := newRig(t, false)
		os.MkdirAll(filepath.Join(r.dir, "pw"), 0o755)
		os.WriteFile(filepath.Join(r.dir, "pw", "truesense-pv-array.conf"), []byte("# foreign"), 0o600)
		if _, err := r.c.ApplyMicPolicy(ctx(), true); err != nil {
			t.Fatal(err)
		}
		if got := r.osd.shown[0]; got != "mic: Microphone: Mic array - ?" {
			t.Errorf("osd = %q", got)
		}
	})
}

func TestSpeakerFilter(t *testing.T) {
	r := newRig(t, false)
	if r.c.SpeakerFilterOn() {
		t.Fatal("off by default")
	}
	if on, err := r.c.SpeakerFilter(ctx(), true, "conference"); err != nil || !on || !r.c.SpeakerFilterOn() {
		t.Fatalf("on = %v, %v", on, err)
	}
	b, _ := os.ReadFile(filepath.Join(r.dir, "pw", "truesense-pv-speaker.conf"))
	if !strings.Contains(string(b), `node.name = "truesense_pv_speaker"`) || r.sys.count("systemctl", "--user", "restart", "pipewire.service") != 1 {
		t.Errorf("conf %s sent %v", b, r.sys.sent())
	}
	if on, err := r.c.SpeakerFilter(ctx(), false, ""); err != nil || on || r.c.SpeakerFilterOn() || r.sys.count("systemctl", "--user", "restart", "pipewire.service") != 2 {
		t.Fatalf("off = %v, %v", on, err)
	}
	if _, err := r.c.SpeakerFilter(ctx(), false, ""); err != nil || r.sys.count("systemctl", "--user", "restart", "pipewire.service") != 2 {
		t.Error("off twice restarted PipeWire")
	}
	if _, err := r.c.SpeakerFilter(ctx(), true, "loud"); err == nil {
		t.Error("an unknown mode")
	}
	r.c.Plugin = ""
	if _, err := r.c.SpeakerFilter(ctx(), true, "conference"); err == nil || !strings.Contains(err.Error(), "plugin missing") {
		t.Errorf("no plugin: %v", err)
	}
}
