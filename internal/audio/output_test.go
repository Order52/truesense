package audio

import (
	"context"
	"encoding/json"
	"reflect"
	"strings"
	"testing"
)

func TestJackStatusAndUse(t *testing.T) {
	r := newRig(t, false)
	j := r.c.Jack(ctx())
	if j.Headphones || j.HeadsetMic || j.Raw["Headphone Jack"] == nil || *j.Raw["Headphone Jack"] != "off" {
		t.Fatalf("jack = %+v", j)
	}
	if got := r.c.JackUse(ctx(), nil); got != "array" {
		t.Errorf("auto, nothing plugged: %s", got)
	}
	r.sys.headphoneJack, r.sys.headsetMicJack = "on", "on"
	j = r.c.Jack(ctx())
	if !j.Headphones || !j.HeadsetMic {
		t.Fatalf("jack = %+v", j)
	}
	for jack, want := range map[string]string{"auto": "headset", "headphones": "array", "headset": "headset"} {
		r.cfg.Audio.Jack = jack
		if got := r.c.JackUse(ctx(), nil); got != want {
			t.Errorf("[audio] jack = %s: microphone %s, want %s", jack, got, want)
		}
	}
	r.cfg.Audio.Jack = "auto"
	r.sys.headsetMicJack = "off"
	if got := r.c.JackUse(ctx(), &Jack{HeadsetMic: true}); got != "headset" {
		t.Errorf("an explicit status wins over the codec: %s", got)
	}
	hp, mic, err := r.c.JackStatus(ctx())
	if err != nil || !hp || mic {
		t.Errorf("doctor view = %v %v %v", hp, mic, err)
	}
	r.c.Run = func(context.Context, string, ...string) Result {
		return Result{Code: 127, Stderr: "amixer: not installed"}
	}
	j = r.c.Jack(ctx())
	if j.Headphones || j.HeadsetMic || j.Raw["Headphone Jack"] != nil {
		t.Fatalf("without amixer: %+v", j)
	}
	b, _ := jsonMarshal(Jack{Raw: map[string]*string{"Headphone Jack": nil}})
	if !strings.Contains(b, `"headphones":false`) || !strings.Contains(b, `"headset_mic":false`) || !strings.Contains(b, `"raw":{"Headphone Jack":null}`) {
		t.Errorf("json = %s", b)
	}
}

func TestAmixerUsesTheSofCardIndex(t *testing.T) {
	r := newRig(t, false)
	r.c.Jack(ctx())
	for _, c := range r.sys.sent() {
		if !strings.HasPrefix(c, "amixer -c0 cget iface=CARD,name=") {
			t.Errorf("amixer command = %q", c)
		}
	}
	r.c.ProcCards = "/nonexistent/cards"
	r.sys.calls = nil
	r.c.Jack(ctx())
	if !strings.HasPrefix(r.sys.sent()[0], "amixer -c1 ") {
		t.Errorf("without /proc/asound: %q", r.sys.sent()[0])
	}
}

func TestHeadsetBoost(t *testing.T) {
	r := newRig(t, false)
	lvl, found, err := r.c.HeadsetBoost(ctx())
	if err != nil || !found || lvl != 1 {
		t.Fatalf("boost = %d %v %v", lvl, found, err)
	}
	for _, want := range []int{0, 3, 2} {
		if err := r.c.SetHeadsetBoost(ctx(), want); err != nil {
			t.Fatal(err)
		}
		if got, _, _ := r.c.HeadsetBoost(ctx()); got != want {
			t.Errorf("boost %d read back %d", want, got)
		}
	}
	if !r.sys.did("amixer", "-c0", "cset", "iface=MIXER,name=Headset Mic Boost Volume", "3,3") {
		t.Errorf("sent %v", r.sys.sent())
	}
	for _, bad := range []int{-1, 4, 100} {
		r.sys.calls = nil
		if err := r.c.SetHeadsetBoost(ctx(), bad); err == nil || !strings.Contains(err.Error(), "0..3") || len(r.sys.calls) != 0 {
			t.Errorf("boost %d: %v (must be refused before any command)", bad, err)
		}
	}
	r.sys.boostBroken = true
	if err := r.c.SetHeadsetBoost(ctx(), 0); err == nil || !strings.Contains(err.Error(), "readback mismatch") {
		t.Errorf("a boost that does not stick: %v", err)
	}
	r.sys.noBoostControl = true
	if _, found, _ := r.c.HeadsetBoost(ctx()); found {
		t.Error("a card without the control")
	}
}

func TestOutputStatusAndSwitching(t *testing.T) {
	r := newRig(t, false)
	st, err := r.c.Output(ctx())
	if err != nil || st.Active != "speakers" || st.JackPlugged {
		t.Fatalf("status = %+v, %v", st, err)
	}
	r.sys.headphoneJack = "on"
	got, err := r.c.SetOutput(ctx(), "headphones")
	if err != nil || got != "headphones" {
		t.Fatalf("SetOutput = %q, %v", got, err)
	}
	if !r.sys.did("pactl", "set-card-profile", testCard, hifiHP) || r.sys.profile != hifiHP {
		t.Fatalf("sent %v", r.sys.sent())
	}
	if st, _ = r.c.Output(ctx()); st.Active != "headphones" || !st.JackPlugged {
		t.Fatalf("status = %+v", st)
	}
	if r.osd.max != 1 || r.osd.depth != 0 {
		t.Errorf("the shell's popups must be silenced during the flip and re-enabled after: max %d depth %d", r.osd.max, r.osd.depth)
	}
	if a, _ := r.c.OutputActive(ctx()); a != "headphones" {
		t.Errorf("OutputActive = %q", a)
	}
	if _, err := r.c.SetOutput(ctx(), "hdmi"); err == nil || !strings.Contains(err.Error(), "speakers | headphones") {
		t.Errorf("bad output: %v", err)
	}
	r.sys.cardPresent = false
	if _, err := r.c.SetOutput(ctx(), "speakers"); err == nil || !strings.Contains(err.Error(), "sound card not found") {
		t.Errorf("no card: %v", err)
	}
	if _, err := r.c.Output(ctx()); err == nil {
		t.Error("status without a card")
	}
}

func TestSetOutputWaitsForTheNewSinkThenVerifies(t *testing.T) {
	r := newRig(t, false)
	// the sink shows up only after three looks (measured on hardware: about 0.04 s)
	looks := 0
	run := r.c.Run
	r.c.Run = func(c2 context.Context, name string, args ...string) Result {
		if name == "pactl" && strings.Join(args, " ") == "list short sinks" {
			looks++
			if looks < 3 {
				return Result{Stdout: "37\tacerultrax_sink\tPipeWire\n"}
			}
		}
		return run(c2, name, args...)
	}
	if _, err := r.c.SetOutput(ctx(), "headphones"); err != nil {
		t.Fatal(err)
	}
	if looks != 3 || len(r.clk.sleeps) != 2 || r.clk.sleeps[0].Milliseconds() != 30 {
		t.Errorf("looks %d sleeps %v", looks, r.clk.sleeps)
	}
	// a profile that does not stick is reported
	r2 := newRig(t, false)
	run2 := r2.c.Run
	r2.c.Run = func(c2 context.Context, name string, args ...string) Result {
		if name == "pactl" && len(args) > 0 && args[0] == "set-card-profile" {
			return Result{Code: 1}
		}
		return run2(c2, name, args...)
	}
	if _, err := r2.c.SetOutput(ctx(), "headphones"); err == nil || !strings.Contains(err.Error(), "profile readback speakers != headphones") {
		t.Errorf("err = %v", err)
	}
}

func TestOutputToggle(t *testing.T) {
	r := newRig(t, false)
	got, err := r.c.OutputToggle(ctx())
	if err != nil || got != "headphones" {
		t.Fatalf("toggle = %q, %v", got, err)
	}
	if !reflect.DeepEqual(r.osd.shown, []string{"audio: Audio output: Headphones (3.5 mm jack)"}) {
		t.Errorf("osd = %v", r.osd.shown)
	}
	r.osd.shown = nil
	if got, _ = r.c.OutputToggle(ctx()); got != "speakers" {
		t.Fatalf("toggle back = %q", got)
	}
	if r.osd.shown[0] != "speaker: Audio output: Laptop speakers" {
		t.Errorf("osd = %v", r.osd.shown)
	}
	// Noctalia wants its popup after the slow switch, with the bare label
	r.osd.shown = nil
	r.cfg.OSD.Style = "noctalia"
	r.c.OutputToggle(ctx())
	if !reflect.DeepEqual(r.osd.shown, []string{"audio: Headphones (3.5 mm jack)"}) {
		t.Errorf("noctalia osd = %v", r.osd.shown)
	}
	r.osd.shown = nil
	r.cfg.OSD.Style = "native"
	r.env["TRUESENSE_OSD"] = "noctalia" // the environment wins over the config
	r.c.OutputToggle(ctx())
	if len(r.osd.shown) != 1 || !strings.HasPrefix(r.osd.shown[0], "speaker: Laptop") {
		t.Errorf("env noctalia osd = %v", r.osd.shown)
	}
	r.sys.cardPresent = false
	if _, err := r.c.OutputToggle(ctx()); err == nil {
		t.Error("toggle without a card")
	}
}

func TestOutputCycleGoesSpeakersHeadphonesThenOtherDevices(t *testing.T) {
	r := newRig(t, false)
	r.sys.headphoneJack = "on"
	r.sys.extraSinks = []string{"alsa_output.pci-0000_00_03.0.hdmi-stereo", "bluez_output.AA.1"}
	r.sys.sinkDescs["bluez_output.AA.1"] = "Some Speaker"
	r.sys.sinkInputs = []string{"12\t77\tPipeWire\tFirefox", "13\t78\tPipeWire\tMpv"}
	// default is the AcerUltraX sink on the speakers: next stop is the headphones
	label, err := r.c.OutputCycle(ctx())
	if err != nil || label != "Headphones (3.5 mm jack)" {
		t.Fatalf("cycle 1 = %q, %v", label, err)
	}
	if r.sys.defaultSink != "acerultrax_sink" {
		t.Errorf("the DTS sink must stay the default, got %s", r.sys.defaultSink)
	}
	for _, id := range []string{"12", "13"} {
		if !r.sys.did("pactl", "move-sink-input", id, "acerultrax_sink") {
			t.Errorf("stream %s was not moved: %v", id, r.sys.sent())
		}
	}
	if r.sys.profile != hifiHP {
		t.Errorf("profile = %s", r.sys.profile)
	}
	// headphones -> the first other device (HDMI is skipped), then back to the speakers
	label, _ = r.c.OutputCycle(ctx())
	if label != "Some Speaker" || r.sys.defaultSink != "bluez_output.AA.1" {
		t.Fatalf("cycle 2 = %q default %s", label, r.sys.defaultSink)
	}
	label, _ = r.c.OutputCycle(ctx())
	if label != "Laptop speakers" || r.sys.defaultSink != "acerultrax_sink" || r.sys.profile != hifiSpk {
		t.Fatalf("cycle 3 = %q default %s profile %s", label, r.sys.defaultSink, r.sys.profile)
	}
	for _, c := range r.sys.sent() {
		if strings.Contains(c, "hdmi") && strings.Contains(c, "set-default-sink") {
			t.Errorf("HDMI must never be a stop: %s", c)
		}
	}
	if r.locks != 0 {
		t.Errorf("the lock was not released: %d", r.locks)
	}
}

func TestOutputCycleWithoutTheDTSSinkTargetsTheHardwareSink(t *testing.T) {
	r := newRig(t, false)
	r.sys.defaultSink = testHW
	r.sys.headphoneJack = "on"
	// strip the virtual sink: the hardware one is the target
	run := r.c.Run
	r.c.Run = func(c2 context.Context, name string, args ...string) Result {
		res := run(c2, name, args...)
		if name == "pactl" && strings.Join(args, " ") == "list short sinks" {
			res.Stdout = strings.Replace(res.Stdout, "30\tacerultrax_sink\tPipeWire\ts32le 2ch 48000Hz\tSUSPENDED\n", "", 1)
		}
		return res
	}
	if _, err := r.c.OutputCycle(ctx()); err != nil {
		t.Fatal(err)
	}
	if r.sys.defaultSink != testHP {
		t.Errorf("default = %s, want %s", r.sys.defaultSink, testHP)
	}
}

func TestOutputCycleWithOnlyTheSpeakersStaysOnThem(t *testing.T) {
	r := newRig(t, false)
	label, err := r.c.OutputCycle(ctx())
	if err != nil || label != "Laptop speakers" {
		t.Fatalf("cycle = %q, %v", label, err)
	}
	// an unknown current sink starts at the first stop
	r.sys.defaultSink = "something_else"
	r.sys.extraSinks = []string{"something_else"}
	r.sys.sinkDescs["something_else"] = "Else"
	if label, _ = r.c.OutputCycle(ctx()); label != "Laptop speakers" {
		t.Errorf("a default that is not a stop -> %q", label)
	}
	r.sys.cardPresent = false
	r.sys.defaultSink = "acerultrax_sink"
	if _, err := r.c.OutputCycle(ctx()); err == nil {
		t.Error("cycling without a card must fail with a message")
	}
}

func TestOutputCycleHonoursTheNoctaliaOrder(t *testing.T) {
	r := newRig(t, false)
	r.cfg.OSD.Style = "noctalia"
	r.sys.headphoneJack = "on"
	r.c.OutputCycle(ctx())
	if !reflect.DeepEqual(r.osd.shown, []string{"audio: Headphones (3.5 mm jack)"}) {
		t.Errorf("osd = %v", r.osd.shown)
	}
}

func jsonMarshal(v any) (string, error) { b, err := json.Marshal(v); return string(b), err }
