package api_test

import (
	"os"
	"strings"
	"testing"
)

// The test machine has no sound system: every program answers "not installed". The commands
// must say so in the words the user knows, and never crash.
func TestAudioWithoutASoundSystem(t *testing.T) {
	_, a := machine(t)
	ctx := t.Context()
	if _, err := a.AudioOutput(ctx); err == nil {
		t.Fatal("no sound card")
	}
	st, err := a.SoundState(ctx)
	must(t, err)
	if st.OK != nil {
		t.Fatalf("no card to judge: %+v", st)
	}
	if a.SpeakerFilter() {
		t.Fatal("the speaker filter is off")
	}
	if v := a.MicVolume(ctx); v != nil {
		t.Fatalf("unreadable volume is nil: %d", *v)
	}
	if b, err := a.HeadsetBoost(ctx); b != nil && err == nil {
		t.Fatalf("no control: %v", b)
	}
	if modes := a.VoiceModes(); modes["headset"] == "" || modes["array"] == "" {
		t.Fatalf("%v", modes)
	}
	_, err = a.SetVoice(ctx, "", "")
	wantErr(t, err, "usage: truesense audio voice set headset|array off|conference|personal")
	_, err = a.SetDTSMode(ctx, "")
	wantErr(t, err, "usage: truesense audio dts mode")
	_, err = a.DTSStatus(ctx)
	wantErr(t, err, "AcerUltraX is not installed")
}

func TestJackSettingIsWrittenToTheUsersConfig(t *testing.T) {
	f, a := machine(t)
	ctx := t.Context()
	must(t, a.SetJack(ctx, "headset"))
	b, err := os.ReadFile(a.UserConfig)
	must(t, err)
	if !strings.Contains(string(b), "jack") || !strings.Contains(string(b), "headset") {
		t.Fatalf("%s", b)
	}
	j := a.Jack(ctx)
	if j.Setting != "headset" || j.MicrophoneInUse != "headset" {
		t.Fatalf("%+v", j)
	}
	must(t, a.SetVoiceNotify(false))
	if a.VoiceStatus(ctx).Notify {
		t.Fatal("notify off")
	}
	_ = f
}
