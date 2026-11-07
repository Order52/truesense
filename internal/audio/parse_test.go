package audio

import (
	"reflect"
	"strings"
	"testing"
)

const realCards = ` 0 [sofhdadsp     ]: sof-hda-dsp - sof-hda-dsp
                      Intel Corp.-Predator PH16-71-V1.18-Type1Sku0
 1 [Device         ]: USB-Audio - USB Audio Device
                      GeneralPlus USB Audio Device at usb-0000:00:14.0-3, full speed
`

func TestCardIndex(t *testing.T) {
	for name, tc := range map[string]struct{ in, want string }{
		"first card":     {realCards, "0"},
		"second card":    {" 0 [PCH]: HDA-Intel - HDA Intel PCH\n 3 [sofhdadsp]: sof-hda-dsp - sof-hda-dsp\n", "3"},
		"no sof card":    {" 0 [PCH]: HDA-Intel - HDA Intel PCH\n", "1"},
		"empty":          {"", "1"},
		"blank lines":    {"\n\n", "1"},
		"name but no id": {"sof-hda-dsp", "sof-hda-dsp"},
	} {
		if got := cardIndex(tc.in); got != tc.want {
			t.Errorf("%s: cardIndex = %q, want %q", name, got, tc.want)
		}
	}
}

func TestControlValue(t *testing.T) {
	for name, tc := range map[string]struct {
		in   string
		want string
		ok   bool
	}{
		"jack on":    {"numid=33,iface=CARD,name='Headphone Jack'\n  ; type=BOOLEAN,access=r-------,values=1\n  : values=on\n", "on", true},
		"jack off":   {"numid=34,iface=CARD,name='Headset Mic Jack'\n  ; type=BOOLEAN,access=r-------,values=1\n  : values=off\n", "off", true},
		"integer":    {"numid=20\n  ; type=INTEGER,values=2,min=0,max=3\n  : values=2,2\n  | dBscale\n", "| dBscale", true}, // the LAST line is what counts for booleans
		"one line":   {": values=on", "on", true},
		"nothing":    {"", "", false},
		"whitespace": {" \n\t\n", "", false},
	} {
		got, ok := controlValue(tc.in)
		if got != tc.want || ok != tc.ok {
			t.Errorf("%s: controlValue = %q, %v; want %q, %v", name, got, ok, tc.want, tc.ok)
		}
	}
}

func TestBoostValue(t *testing.T) {
	real := "numid=20,iface=MIXER,name='Headset Mic Boost Volume'\n  ; type=INTEGER,access=rw---R--,values=2,min=0,max=3,step=0\n  : values=2,2\n  | dBscale-min=0.00dB,step=10.00dB,mute=0\n"
	for name, tc := range map[string]struct {
		in   string
		want int
		ok   bool
	}{
		"real output: the values line, not the channel count in the header": {real, 2, true},
		"single value": {"  : values=3\n", 3, true},
		"zero":         {"  : values=0,0\n", 0, true},
		"no control":   {"amixer: Cannot find the given element\n", 0, false},
		"empty":        {"", 0, false},
		"header only":  {"  ; type=INTEGER,values=2\n", 0, false},
	} {
		got, ok := boostValue(tc.in)
		if got != tc.want || ok != tc.ok {
			t.Errorf("%s: boostValue = %d, %v; want %d, %v", name, got, ok, tc.want, tc.ok)
		}
	}
}

const pactlShortSinks = "37\tacerultrax_sink\tPipeWire\ts32le 2ch 48000Hz\tRUNNING\n" +
	"4126\talsa_output.pci-0000_00_1f.3-platform-skl_hda_dsp_generic.HiFi__Speaker__sink\tPipeWire\ts32le 2ch 48000Hz\tSUSPENDED\n" +
	"4127\talsa_output.pci-0000_00_03.0.hdmi-stereo\tPipeWire\ts32le 2ch 48000Hz\tSUSPENDED\n" + "\n" + "garbage without tabs\n"

func TestShortColumn(t *testing.T) {
	got := shortColumn(pactlShortSinks, 1)
	want := []string{"acerultrax_sink", "alsa_output.pci-0000_00_1f.3-platform-skl_hda_dsp_generic.HiFi__Speaker__sink", "alsa_output.pci-0000_00_03.0.hdmi-stereo"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("sinks = %v", got)
	}
	if got := shortColumn("a\tb\n", 5); got != nil {
		t.Errorf("a column that does not exist = %v", got)
	}
	if got := shortColumn("", 1); got != nil {
		t.Errorf("empty = %v", got)
	}
}

func TestShortSourcesSkipsMonitors(t *testing.T) {
	in := "50\talsa_input.pci-0000_00_1f.3-platform-skl_hda_dsp_generic.HiFi__Mic1__source\tPipeWire\ts32le 2ch 48000Hz\tSUSPENDED\n" +
		"51\talsa_output.pci-0000_00_1f.3-platform-skl_hda_dsp_generic.HiFi__Speaker__sink.monitor\tPipeWire\ts32le 2ch 48000Hz\tSUSPENDED\n" +
		"52\ttruesense_pv_headset\tPipeWire\tfloat32le 2ch 48000Hz\tIDLE\n"
	got := shortSources(in)
	if len(got) != 2 || got[0][0] != "50" || got[1][1] != "truesense_pv_headset" {
		t.Errorf("sources = %v", got)
	}
}

func TestFirstFields(t *testing.T) {
	got := firstFields("12\t77\tPipeWire\tFirefox\n\n  34  x\n")
	if !reflect.DeepEqual(got, []string{"12", "34"}) {
		t.Errorf("ids = %v", got)
	}
}

func TestCardName(t *testing.T) {
	in := "70\talsa_card.usb-Other\talsa\n71\t" + testCard + "\talsa\n"
	if got, ok := cardName(in, cardMatch); !ok || got != testCard {
		t.Errorf("cardName = %q, %v", got, ok)
	}
	for _, bad := range []string{"", "70\talsa_card.usb-Other\talsa\n", cardMatch + " with no tab\n"} {
		if got, ok := cardName(bad, cardMatch); ok {
			t.Errorf("cardName(%q) = %q", bad, got)
		}
	}
}

const listCards = "Card #70\n\tName: alsa_card.usb-Other\n\tProfiles:\n\t\toff: Off (sinks: 0, sources: 0, priority: 0, available: yes)\n\tActive Profile: off\n" +
	"Card #71\n\tName: " + testCard + "\n\tDriver: alsa\n\tProfiles:\n" +
	"\t\toff: Off (sinks: 0, sources: 0, priority: 0, available: yes)\n" +
	"\t\tHiFi (Headphones, Mic1, Mic2): Play HiFi quality Music (sinks: 1, sources: 2, priority: 8000, available: yes)\n" +
	"\t\tHiFi (Mic1, Mic2, Speaker): Play HiFi quality Music (sinks: 1, sources: 2, priority: 8000, available: yes)\n" +
	"\t\tpro-audio: Pro Audio (sinks: 4, sources: 4, priority: 1, available: yes)\n" +
	"\tActive Profile: HiFi (Mic1, Mic2, Speaker)\n\tPorts:\n\t\tanalog-output-speaker: Speaker (type: Speaker, priority: 100)\n" +
	"Card #72\n\tName: alsa_card.pci-0000_00_03.0\n\tActive Profile: output:hdmi-stereo\n"

func TestCardBlockActiveProfileAndProfilesOffered(t *testing.T) {
	block, ok := cardBlock(listCards, testCard, true)
	if !ok {
		t.Fatal("card not found")
	}
	if strings.Contains(block, "Card #72") || strings.Contains(block, "hdmi-stereo") {
		t.Errorf("block runs into the next card:\n%s", block)
	}
	if got := activeProfile(block); got != "HiFi (Mic1, Mic2, Speaker)" {
		t.Errorf("active = %q", got)
	}
	want := []string{"off", "HiFi (Headphones, Mic1, Mic2)", "HiFi (Mic1, Mic2, Speaker)", "pro-audio"}
	if got := profilesOffered(block); !reflect.DeepEqual(got, want) {
		t.Errorf("offered = %v", got)
	}
	// the un-cut block (what output_status reads) ends wherever the text ends, but the FIRST Active Profile still wins
	uncut, _ := cardBlock(listCards, testCard, false)
	if got := activeProfile(uncut); got != "HiFi (Mic1, Mic2, Speaker)" {
		t.Errorf("uncut active = %q", got)
	}
	if _, ok := cardBlock(listCards, "alsa_card.nonexistent", true); ok {
		t.Error("a card that is not listed was found")
	}
	if got := activeProfile("\tDriver: alsa\n"); got != "?" {
		t.Errorf("no active profile = %q", got)
	}
	if got := profilesOffered("\tDriver: alsa\n"); got != nil {
		t.Errorf("no profiles = %v", got)
	}
	if got := profilesOffered("\tProfiles:\n\t\toff: Off (x)\n"); !reflect.DeepEqual(got, []string{"off"}) {
		t.Errorf("a block that ends without Active Profile = %v", got)
	}
}

func TestSinkDescription(t *testing.T) {
	in := "Sink #37\n\tState: RUNNING\n\tName: acerultrax_sink\n\tDescription: AcerUltraX\n\nSink #38\n\tName: bluez_output.AA.1\n\tDescription: Some Speaker\n\tDriver: PipeWire\n"
	if got := sinkDescription(in, "bluez_output.AA.1"); got != "Some Speaker" {
		t.Errorf("got %q", got)
	}
	if got := sinkDescription(in, "gone"); got != "gone" {
		t.Errorf("unknown sink = %q", got)
	}
	if got := sinkDescription("Sink #1\n\tName: nodesc\n\tDriver: x\n", "nodesc"); got != "nodesc" {
		t.Errorf("no description = %q", got)
	}
}

func TestMuteAndVolume(t *testing.T) {
	if !isMuted("Mute: yes\n") || isMuted("Mute: no\n") || isMuted("") {
		t.Error("isMuted")
	}
	for in, want := range map[string]int{
		"Volume: front-left: 65536 / 100% / 0.00 dB,   front-right: 65536 / 100% / 0.00 dB\n        balance 0.00\n": 100,
		"Volume: mono: 98304 / 150% / 10.57 dB": 150, "Volume: 0%": 0,
	} {
		if got, ok := volumePercent(in); !ok || got != want {
			t.Errorf("volumePercent(%q) = %d, %v", in, got, ok)
		}
	}
	if _, ok := volumePercent("Volume: unavailable"); ok {
		t.Error("no percentage")
	}
}

func TestVoiceThreshold(t *testing.T) {
	for _, mode := range []string{"conference", "personal"} {
		conf := voiceConf(voiceDevices[0], voiceModes[mode], "/p.so")
		if got := voiceThreshold(conf); got != voiceModes[mode].threshold {
			t.Errorf("%s: threshold = %d", mode, got)
		}
	}
	if voiceThreshold("nothing here") != -1 || voiceThreshold(`"VAD Threshold (%)" = x`) != -1 {
		t.Error("an unreadable config must give -1")
	}
}

func TestParseDTSDevices(t *testing.T) {
	got := parseDTSDevices(strings.TrimSpace(realDevices)) // the real helper's output is stripped: the first row loses its indent
	want := []DTSDevice{
		{"Laptop speakers", "speakers", "Music", "@speakers", false},
		{"Headphones (3.5 mm jack)", "headphones", "Music", "@headphones", false},
		{"DualSense wireless controller (PS5) Direct DualSense Wireless Controller", "other", "Plain", "alsa_output.usb-Sony_DualSense-00.Direct__Direct__sink", false},
		{"Headset PC38X", "other", "Plain", "alsa_output.usb-GeneralPlus_USB_Audio_Device-00.analog-stereo", true},
		{"Some Speaker", "bluetooth-speaker", "Plain", "bluez_output.AA.1", false},
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("devices:\n got %+v\nwant %+v", got, want)
	}
	// the same text with its indent intact, CRLF line ends, and a row that has no reference
	crlf := strings.ReplaceAll(realDevices, "\n", "\r\n") + "  Orphan                                             other              Plain\r\n"
	if got := parseDTSDevices(crlf); !reflect.DeepEqual(got, want) {
		t.Errorf("CRLF / orphan row:\n got %+v", got)
	}
	for _, in := range []string{"", "\n\n", "No devices found", "    ref-without-a-row\n", "  Row without kind    Plain\n    ref\n"} {
		if got := parseDTSDevices(in); len(got) != 0 {
			t.Errorf("parseDTSDevices(%q) = %+v", in, got)
		}
	}
	for _, p := range DTSProfiles {
		in := "  Dev  other  " + p + "\n    ref1\n"
		if got := parseDTSDevices(in); len(got) != 1 || got[0].Profile != p {
			t.Errorf("profile %q: %+v", p, got)
		}
	}
	if got := parseDTSDevices("  Dev  bluetooth  Game2\n    r\n"); len(got) != 1 || got[0].Kind != "bluetooth" {
		t.Errorf("plain bluetooth kind: %+v", got)
	}
}

func TestHeadsetLabels(t *testing.T) {
	for label, want := range map[string]bool{
		"Headset PC38X": true, "Logitech G Pro Gaming": true, "AirPods Pro": true, "Galea": true, "Sony Earbuds": true, "Wired Headphones": true, "Mi Buds": true,
		"DualSense wireless controller": true, "Laptop speakers": false, "HDMI / DisplayPort": false, "Some Speaker": false, "Dock": false, "": false,
	} {
		if got := isHeadsetLabel(label); got != want {
			t.Errorf("isHeadsetLabel(%q) = %v", label, got)
		}
	}
}

func TestLastLine(t *testing.T) {
	for in, want := range map[string]string{"": "", "boom": "boom", "a\nb\n": "b", "\n\n  last  \n": "last"} {
		if got := lastLine(in); got != want {
			t.Errorf("lastLine(%q) = %q", in, got)
		}
	}
}

// ---- fuzz targets: the tools' output is external input ----

func FuzzParsePactl(f *testing.F) {
	for _, s := range []string{listCards, pactlShortSinks, "Mute: yes", "Volume: front-left: 65536 / 100% / 0.00 dB", "", "\t\t\t", "Name: ", "Card #1\n\tName: x\n\tActive Profile:"} {
		f.Add(s)
	}
	f.Fuzz(func(t *testing.T, in string) {
		shortColumn(in, 1)
		shortSources(in)
		firstFields(in)
		cardName(in, cardMatch)
		if block, ok := cardBlock(in, testCard, true); ok {
			activeProfile(block)
			profilesOffered(block)
		}
		activeProfile(in)
		profilesOffered(in)
		sinkDescription(in, "acerultrax_sink")
		isMuted(in)
		if v, ok := volumePercent(in); ok && v < 0 {
			t.Fatalf("negative volume %d", v)
		}
	})
}

func FuzzParseAmixer(f *testing.F) {
	for _, s := range []string{"  : values=on\n", "numid=20\n  : values=2,2\n", "", ": values=", "  ; type=INTEGER,values=2\n"} {
		f.Add(s)
	}
	f.Fuzz(func(t *testing.T, in string) {
		controlValue(in)
		if n, ok := boostValue(in); ok && n < 0 {
			t.Fatalf("negative boost %d", n)
		}
		cardIndex(in)
		voiceThreshold(in)
	})
}

func FuzzParseDTSDevices(f *testing.F) {
	f.Add(realDevices)
	f.Add(strings.TrimSpace(realDevices))
	f.Add("* a  speakers  Music\n    ref\n")
	f.Add("")
	f.Add("    \n    \n")
	f.Fuzz(func(t *testing.T, in string) {
		for _, d := range parseDTSDevices(in) {
			if d.Ref == "" || d.Label == "" || d.Kind == "" || d.Profile == "" {
				t.Fatalf("incomplete device %+v from %q", d, in)
			}
			if !contains(DTSProfiles, d.Profile) {
				t.Fatalf("unknown profile %q", d.Profile)
			}
			if d.Label != strings.TrimSpace(d.Label) || d.Ref != strings.TrimSpace(d.Ref) {
				t.Fatalf("untrimmed %+v", d)
			}
		}
	})
}
