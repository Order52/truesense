package keyboard

import (
	"bytes"
	"encoding/hex"
	"errors"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/Order52/truesense/internal/firmware"
	"github.com/Order52/truesense/internal/hw"
	"github.com/Order52/truesense/internal/keymap"
)

var _ hw.KBTimeout = TimeoutFlag{} // the hw package can use the controller's flag

func hx(s string) []byte { b, _ := hex.DecodeString(s); return b }

func asHex(ps [][]byte) []string {
	var out []string
	for _, p := range ps {
		out = append(out, hex.EncodeToString(p))
	}
	return out
}

func isFirmwareError(err error) bool { var fe *firmware.Error; return errors.As(err, &fe) }

// Expected packets were produced by the Python implementation (keyboard.SIM["packets"]) for
// set_effect(name, "12ab34", 3, 20, 1).
func TestEveryEffectSendsTheVerifiedPackets(t *testing.T) {
	const pre = "b10000000000004e"
	cases := []struct {
		name string
		want []string
	}{
		{"off", []string{pre, "08020000000000f5", "14000000000000eb"}},
		{"static", []string{pre, "14000012ab3400fa", "08020109140102d4"}},
		{"breathing", []string{pre, "14000012ab3400fa", "08020209140102d3"}},
		{"wave", []string{pre, "14000000000000eb", "08020309140102d2"}},
		{"snake", []string{pre, "14000012ab3400fa", "08020509140102d0"}},
		{"ripple", []string{pre, "14000012ab3400fa", "08020609140102cf"}},
		{"neon", []string{pre, "14000000000000eb", "08020809140102cd"}},
		{"raindrop", []string{pre, "14000012ab3400fa", "08020a09140102cb"}},
		{"lightning", []string{pre, "14000012ab3400fa", "08021209140102c3"}},
		{"spot", []string{pre, "14000012ab3400fa", "08022509140102b0"}},
		{"star", []string{pre, "14000012ab3400fa", "08022609140102af"}},
		{"fireball", []string{pre, "14000012ab3400fa", "08022709140102ae"}},
		{"snow", []string{pre, "14000012ab3400fa", "08022809140102ad"}},
		{"heartbeat", []string{pre, "14000012ab3400fa", "08022909140102ac"}},
	}
	if len(cases) != len(EffectNames()) {
		t.Fatalf("table covers %d effects, package has %d", len(cases), len(EffectNames()))
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			k, d, _, _ := newTestKeyboard(t)
			if err := k.SetEffect(tc.name, "12AB34", 3, 20, 1); err != nil {
				t.Fatal(err)
			}
			if got := asHex(d.sets()); !reflect.DeepEqual(got, tc.want) {
				t.Errorf("packets\n got %v\nwant %v", got, tc.want)
			}
		})
	}
}

func TestEffectRemembersTheLook(t *testing.T) {
	k, _, _, _ := newTestKeyboard(t)
	if err := k.SetEffect("breathing", "#AB12CD", 7, 33, 1); err != nil {
		t.Fatal(err)
	}
	want := State{Mode: "effect", Name: "breathing", Color: "ab12cd", Speed: Int(7), Brightness: Int(33), Direction: Int(1)}
	if got := k.Last(); !reflect.DeepEqual(got, want) {
		t.Errorf("Last = %+v, want %+v", got, want)
	}
	if err := k.SetEffect("off", "", 5, 30, 0); err != nil {
		t.Fatal(err)
	}
	if got := k.Last(); !reflect.DeepEqual(got, State{Mode: "off"}) {
		t.Errorf("Last after off = %+v", got)
	}
}

func TestEffectValidation(t *testing.T) {
	for _, tc := range []struct {
		name, color      string
		speed, bri, dir  int
		wantErrSubstring string
	}{
		{"nonsense", "ffffff", 5, 30, 0, "effect: off, static"},
		{"wave", "ffffff", 0, 30, 0, "speed 1..12, brightness 0..50"},
		{"wave", "ffffff", 13, 30, 0, "speed 1..12"},
		{"wave", "ffffff", 5, 51, 0, "brightness 0..50"},
		{"wave", "ffffff", 5, -1, 0, "brightness 0..50"},
		{"static", "zzz", 5, 30, 0, "colour must be RRGGBB"},
		{"static", "ffffff", 5, 30, 255, "direction"},
	} {
		k, d, st, _ := newTestKeyboard(t)
		err := k.SetEffect(tc.name, tc.color, tc.speed, tc.bri, tc.dir)
		if err == nil || !strings.Contains(err.Error(), tc.wantErrSubstring) || !isFirmwareError(err) {
			t.Errorf("%+v: err = %v", tc, err)
		}
		if len(d.sets()) != 0 || len(st.Load()) != 0 {
			t.Errorf("%+v: a refused effect must not reach the keyboard or the state", tc)
		}
	}
	// a colourless effect never looks at the colour
	k, _, _, _ := newTestKeyboard(t)
	if err := k.SetEffect("wave", "not a colour", 5, 30, 0); err != nil {
		t.Errorf("wave ignores the colour: %v", err)
	}
}

func TestSpeedScaleCoversTheKeyboardsWholeRange(t *testing.T) {
	if SpeedMin != 1 || SpeedMax != 12 {
		t.Fatalf("scale = %d..%d", SpeedMin, SpeedMax)
	}
	var got []int
	for s := 1; s <= 12; s++ {
		k, d, _, _ := newTestKeyboard(t)
		if err := k.SetEffect("wave", "00aec7", s, 25, 0); err != nil {
			t.Fatal(err)
		}
		sets := d.sets()
		got = append(got, int(sets[len(sets)-1][3]))
	}
	want := []int{11, 10, 9, 8, 7, 6, 5, 4, 3, 2, 1, 0}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("speed bytes = %v, want %v (never above 11: the keyboard turns those into a medium speed)", got, want)
	}
	k, _, _, _ := newTestKeyboard(t)
	for _, bad := range []int{0, 13, -1, 99} {
		if err := k.SetEffect("wave", "00aec7", bad, 25, 0); err == nil {
			t.Errorf("speed %d accepted", bad)
		}
	}
	if err := k.SetSpeed(13); err == nil {
		t.Error("SetSpeed(13) accepted")
	}
}

func TestOperatingMode(t *testing.T) {
	k, d, _, _ := newTestKeyboard(t)
	if err := k.SetEffect("operatingmode", "00ff10", 5, 30, 0); err != nil {
		t.Fatal(err)
	}
	if got, want := asHex(d.sets()), []string{"14000100ff1000db", "0803220532020198"}; !reflect.DeepEqual(got, want) {
		t.Errorf("packets %v want %v", got, want)
	}
	if got := k.Last(); !reflect.DeepEqual(got, State{Mode: "operating", Color: "00ff10", Brightness: Int(0x32)}) {
		t.Errorf("state %+v", got)
	}
	// colour packets as the Python implementation builds them
	for _, tc := range []struct {
		mode   string
		colors map[string]string
		want   string
	}{
		{"turbo", nil, "140001ff000000eb"},
		{"unknown-mode", map[string]string{"eco": "010203"}, "14000100aec70075"},
		{"eco", map[string]string{"eco": "010203"}, "14000101020300e4"},
	} {
		d.reset()
		if err := k.FollowMode(tc.mode, tc.colors); err != nil {
			t.Fatal(err)
		}
		if got := asHex(d.sets()); got[0] != tc.want || got[1] != "0803220532020198" {
			t.Errorf("FollowMode(%s) sent %v, want %s first", tc.mode, got, tc.want)
		}
	}
	if err := k.SetOperatingMode("zz", 5); err == nil {
		t.Error("bad colour accepted")
	}
}

func TestBrightnessReadsByte4AfterThe88Packet(t *testing.T) {
	k, d, _, _ := newTestKeyboard(t)
	for i := 0; i < 3; i++ {
		b, err := k.Brightness()
		if err != nil || b != 25 {
			t.Fatalf("Brightness = %d, %v", b, err)
		}
	}
	want := []rec{{"set", hx("88000000000000" + "77")}, {"get", []byte{8}}}
	if !reflect.DeepEqual(d.log[:2], want) {
		t.Errorf("first transfers %v, want %v", d.log[:2], want)
	}
	if d.opens != 1 || len(d.closes) != 0 {
		t.Errorf("opens=%d closes=%v: one open for all of it", d.opens, d.closes)
	}
}

func TestShortReplyIsAnErrorNotAPanic(t *testing.T) {
	k, d, _, _ := newTestKeyboard(t)
	d.reply = func([]byte) []byte { return []byte{0x88, 0} }
	if _, err := k.Brightness(); err == nil {
		t.Error("a short report must be an error")
	}
	if _, err := k.HIDTimeoutGet(); err == nil {
		t.Error("a short report must be an error")
	}
}

func perKeyData(d *fakeDevice) []byte {
	var data []byte
	for _, r := range d.log {
		if r.Kind == "out" {
			data = append(data, r.Data...)
		}
	}
	return data
}

func TestPerKeyUploadSequence(t *testing.T) {
	k, d, _, sl := newTestKeyboard(t)
	cells := make([]keymap.RGB, 130) // more than 128: the surplus is dropped
	for i := range cells {
		cells[i] = keymap.RGB{uint8(i), uint8(i + 1), uint8(i + 2)}
	}
	if err := k.PerKey(cells, 20); err != nil {
		t.Fatal(err)
	}
	var kinds []string
	for _, r := range d.log {
		kinds = append(kinds, r.Kind)
	}
	wantKinds := []string{"set", "get", "set", "out", "out", "out", "out", "out", "out", "out", "out", "set"}
	if !reflect.DeepEqual(kinds, wantKinds) {
		t.Fatalf("transfer order %v", kinds)
	}
	sets := asHex(d.sets())
	// 88.. (prime), 12 00 00 08 .. (start), then brightness packet 08 02 33 05 <20> 08 01 chk
	wantSets := []string{"88000000000000" + "77", "12000008000000e5", "08023305140801a0"}
	if !reflect.DeepEqual(sets, wantSets) {
		t.Errorf("feature reports %v want %v", sets, wantSets)
	}
	for _, r := range d.log {
		if r.Kind == "out" && len(r.Data) != 64 {
			t.Errorf("output report of %d bytes", len(r.Data))
		}
	}
	data := perKeyData(d)
	if len(data) != 512 {
		t.Fatalf("%d bytes uploaded", len(data))
	}
	for i := 0; i < 128; i++ {
		if got, want := data[i*4:i*4+4], []byte{0, byte(i), byte(i + 1), byte(i + 2)}; !bytes.Equal(got, want) {
			t.Fatalf("cell %d = %x want %x", i, got, want)
		}
	}
	var want []time.Duration
	for i := 0; i < 8; i++ {
		want = append(want, 16*time.Millisecond)
	}
	want = append(want, 50*time.Millisecond)
	if !reflect.DeepEqual(sl.d, want) {
		t.Errorf("pacing %v want %v (Windows spaces frames ~15 ms; unpaced writes lose the last frame)", sl.d, want)
	}
	got := k.Last()
	if got.Mode != "perkey" || *got.Brightness != 20 || len(got.Cells) != 128 || got.Cells[3] != "030405" {
		t.Errorf("state %+v", got)
	}
}

func TestPerKeyPadsWithBlackAndKeepsBrightnessByDefault(t *testing.T) {
	k, d, _, _ := newTestKeyboard(t)
	if err := k.PerKey([]keymap.RGB{{1, 2, 3}}, KeepBrightness); err != nil {
		t.Fatal(err)
	}
	data := perKeyData(d)
	if !bytes.Equal(data[:4], []byte{0, 1, 2, 3}) || !bytes.Equal(data[4:], make([]byte, 508)) {
		t.Error("missing cells must be black")
	}
	sets := d.sets()
	if last := sets[len(sets)-1]; last[4] != 25 {
		t.Errorf("kept brightness = %d, the keyboard reported 25", last[4])
	}
	// brightness reads as 0 (dark): never upload into darkness
	d.reply = func([]byte) []byte { return []byte{0x88, 0, 0, 0, 0, 0, 0, 0x77} }
	d.reset()
	if err := k.PerKey(nil, KeepBrightness); err != nil {
		t.Fatal(err)
	}
	if sets := d.sets(); sets[len(sets)-1][4] != 25 {
		t.Errorf("dark keyboard: uploaded brightness %d", sets[len(sets)-1][4])
	}
	if err := k.PerKey(nil, 51); err == nil || !isFirmwareError(err) {
		t.Error("brightness 51 accepted")
	}
	if err := k.PerKey(nil, -5); err == nil {
		t.Error("brightness -5 accepted")
	}
}

func TestPerKeyFrameDoesNotReplaceTheRememberedLook(t *testing.T) {
	k, _, _, _ := newTestKeyboard(t)
	if err := k.SetEffect("static", "ff0000", 5, 30, 0); err != nil {
		t.Fatal(err)
	}
	if err := k.PerKeyFrame(nil, 10); err != nil {
		t.Fatal(err)
	}
	if k.Last().Mode != "effect" {
		t.Errorf("animation frames must not become the look: %+v", k.Last())
	}
}

func TestRestoreDefaultIsTheFactoryLook(t *testing.T) {
	k, d, _, _ := newTestKeyboard(t)
	if err := k.RestoreDefault(); err != nil {
		t.Fatal(err)
	}
	data := perKeyData(d)
	for i := 0; i < 126; i++ {
		if !bytes.Equal(data[i*4:i*4+4], []byte{0, 0x00, 0xAE, 0xC7}) {
			t.Fatalf("cell %d = %x", i, data[i*4:i*4+4])
		}
	}
	if !bytes.Equal(data[126*4:], make([]byte, 8)) {
		t.Error("cells 126 and 127 stay black")
	}
	if sets := d.sets(); sets[len(sets)-1][4] != 12 {
		t.Errorf("brightness %d, want 12", sets[len(sets)-1][4])
	}
}

func TestSetBrightness(t *testing.T) {
	brightnessPacket := func(level byte) string { return hex.EncodeToString(pkt(0x08, 0x02, 0x33, 0x05, level, 0x08, 0x01)) }
	t.Run("unknown look uses the per-key brightness packet", func(t *testing.T) {
		k, d, st, _ := newTestKeyboard(t)
		if err := k.SetBrightness(17); err != nil {
			t.Fatal(err)
		}
		if got := asHex(d.sets()); !reflect.DeepEqual(got, []string{brightnessPacket(17)}) {
			t.Errorf("%v", got)
		}
		if len(st.Load()) != 0 {
			t.Error("nothing to remember for an unknown look")
		}
	})
	t.Run("a running effect is re-sent with the new level", func(t *testing.T) {
		k, d, _, _ := newTestKeyboard(t)
		_ = k.SetEffect("wave", "00aec7", 4, 30, 1)
		d.reset()
		if err := k.SetBrightness(10); err != nil {
			t.Fatal(err)
		}
		want := []string{"080233050a0801aa", "b10000000000004e", "14000000000000eb", "080203080a0102dd"}
		if got := asHex(d.sets()); !reflect.DeepEqual(got, want) {
			t.Errorf("%v want %v", got, want)
		}
		if k.Last().BrightnessOr(-1) != 10 || k.Last().Name != "wave" {
			t.Errorf("state %+v", k.Last())
		}
	})
	t.Run("operating mode keeps its colour", func(t *testing.T) {
		k, d, _, _ := newTestKeyboard(t)
		_ = k.SetOperatingMode("112233", 50)
		d.reset()
		if err := k.SetBrightness(5); err != nil {
			t.Fatal(err)
		}
		sets := asHex(d.sets())
		if len(sets) != 2 || sets[0] != "1400011122330084" || sets[1] != "08032205050201c5" {
			t.Errorf("%v", sets)
		}
	})
	t.Run("per-key look remembers the level", func(t *testing.T) {
		k, d, _, _ := newTestKeyboard(t)
		_ = k.PerKey([]keymap.RGB{{9, 9, 9}}, 30)
		d.reset()
		if err := k.SetBrightness(7); err != nil {
			t.Fatal(err)
		}
		if got := asHex(d.sets()); !reflect.DeepEqual(got, []string{brightnessPacket(7)}) {
			t.Errorf("%v", got)
		}
		if s := k.Last(); s.Mode != "perkey" || *s.Brightness != 7 || s.Cells[0] != "090909" {
			t.Errorf("state %+v", s)
		}
	})
	t.Run("off effect is not re-sent", func(t *testing.T) {
		k, d, _, _ := newTestKeyboard(t)
		_ = k.SetEffect("off", "", 5, 30, 0)
		d.reset()
		_ = k.SetBrightness(20)
		if got := asHex(d.sets()); !reflect.DeepEqual(got, []string{brightnessPacket(20)}) {
			t.Errorf("%v", got)
		}
	})
	t.Run("range", func(t *testing.T) {
		k, _, _, _ := newTestKeyboard(t)
		for _, bad := range []int{-1, 51} {
			if err := k.SetBrightness(bad); err == nil || !isFirmwareError(err) {
				t.Errorf("%d accepted", bad)
			}
		}
	})
}

func TestOffAndOnRestoreTheLook(t *testing.T) {
	k, d, st, _ := newTestKeyboard(t)
	d.reply = func(set []byte) []byte { return []byte{0x88, 0, 0, 0, 40, 0, 0, 0} }
	_ = k.SetEffect("static", "ff0000", 5, 40, 0)
	if err := k.Off(); err != nil {
		t.Fatal(err)
	}
	var saved State
	if err := st.GetInto("kb_restore", &saved); err != nil || saved.Name != "static" || *saved.Brightness != 40 {
		t.Fatalf("kb_restore %+v, %v", saved, err)
	}
	if k.Last().BrightnessOr(-1) != 0 {
		t.Errorf("Off leaves brightness %v", k.Last().Brightness)
	}
	d.reset()
	if err := k.On(); err != nil {
		t.Fatal(err)
	}
	if got := k.Last(); got.Name != "static" || got.BrightnessOr(-1) != 40 || got.Color != "ff0000" {
		t.Errorf("On restored %+v", got)
	}
	// when it was already dark nothing new is saved
	d.reply = func([]byte) []byte { return []byte{0x88, 0, 0, 0, 0, 0, 0, 0} }
	_ = st.Set("kb_restore", State{Mode: "effect", Name: "snow", Brightness: Int(12)})
	if err := k.Off(); err != nil {
		t.Fatal(err)
	}
	_ = st.GetInto("kb_restore", &saved)
	if saved.Name != "snow" {
		t.Errorf("a dark keyboard must not overwrite the saved look: %+v", saved)
	}
}

func TestOnWithoutAnythingSavedUses25(t *testing.T) {
	k, d, _, _ := newTestKeyboard(t)
	if err := k.On(); err != nil {
		t.Fatal(err)
	}
	if got := asHex(d.sets()); len(got) != 1 || got[0][8:10] != "19" {
		t.Errorf("%v", got)
	}
}

func TestReplay(t *testing.T) {
	cases := []struct {
		name string
		s    State
		want string
		pkts int
	}{
		{"effect", State{Mode: "effect", Name: "wave", Color: "00ff00", Speed: Int(4), Brightness: Int(20), Direction: Int(0)}, "effect wave", 3},
		{"effect defaults", State{Mode: "effect", Name: "static"}, "effect static", 3},
		{"operating", State{Mode: "operating", Color: "112233"}, "operating mode", 2},
		{"per key", State{Mode: "perkey", Cells: []string{"010203"}}, "per-key colours", 3},
		{"unknown effect", State{Mode: "effect", Name: "nosuch"}, "", 0},
		{"empty", State{}, "", 0},
		{"off", State{Mode: "off"}, "", 0},
		{"perkey without cells", State{Mode: "perkey"}, "", 0},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			k, d, _, _ := newTestKeyboard(t)
			got, err := k.Replay(tc.s)
			if err != nil || got != tc.want {
				t.Fatalf("Replay = %q, %v", got, err)
			}
			if n := len(d.sets()); n != tc.pkts {
				t.Errorf("%d feature reports, want %d", n, tc.pkts)
			}
		})
	}
	k, d, _, _ := newTestKeyboard(t)
	if _, err := k.Replay(State{Mode: "effect", Name: "static"}); err != nil {
		t.Fatal(err)
	}
	if got, want := asHex(d.sets()), []string{"b10000000000004e", "140000ffffff00ee", "08020107190101d2"}; !reflect.DeepEqual(got, want) {
		t.Errorf("defaults (colour ffffff, speed 5 = byte 7, brightness 25) %v want %v", got, want)
	}
	if _, err := k.Replay(State{Mode: "perkey", Cells: []string{"nothex"}}); err == nil {
		t.Error("a corrupt remembered cell must be an error")
	}
}

func TestSetSpeed(t *testing.T) {
	k, d, _, _ := newTestKeyboard(t)
	for _, s := range []State{{}, {Mode: "off"}, {Mode: "effect", Name: "static"}, {Mode: "operating"}} {
		_ = k.state.Set("keyboard", s)
		if err := k.SetSpeed(5); err == nil || !strings.Contains(err.Error(), "no animation speed") {
			t.Errorf("%+v: %v", s, err)
		}
	}
	_ = k.SetEffect("snake", "0000ff", 2, 33, 1)
	d.reset()
	if err := k.SetSpeed(12); err != nil {
		t.Fatal(err)
	}
	if got, want := asHex(d.sets()), []string{"b10000000000004e", "1400000000ff00ec", "08020500210102cc"}; !reflect.DeepEqual(got, want) {
		t.Errorf("fastest = byte 0, colour, brightness 33 and direction kept: %v want %v", got, want)
	}
	if k.Last().Color != "0000ff" || k.Last().BrightnessOr(0) != 33 || *k.Last().Speed != 12 {
		t.Errorf("%+v", k.Last())
	}
}

func TestSetColor(t *testing.T) {
	t.Run("effect", func(t *testing.T) {
		k, d, _, _ := newTestKeyboard(t)
		_ = k.SetEffect("snake", "0000ff", 3, 33, 0)
		d.reset()
		if err := k.SetColor("#FF8800"); err != nil {
			t.Fatal(err)
		}
		if k.Last().Color != "ff8800" || k.Last().Name != "snake" || k.Last().BrightnessOr(0) != 25 {
			t.Errorf("%+v (brightness comes from the keyboard: 25)", k.Last())
		}
	})
	t.Run("effects without colour refuse", func(t *testing.T) {
		for _, n := range []string{"wave", "neon"} {
			k, _, _, _ := newTestKeyboard(t)
			_ = k.SetEffect(n, "", 3, 33, 0)
			if err := k.SetColor("ff0000"); err == nil || !strings.Contains(err.Error(), "has no colour") {
				t.Errorf("%s: %v", n, err)
			}
		}
	})
	t.Run("operating", func(t *testing.T) {
		k, _, _, _ := newTestKeyboard(t)
		_ = k.SetOperatingMode("112233", 50)
		if err := k.SetColor("445566"); err != nil {
			t.Fatal(err)
		}
		if s := k.Last(); s.Mode != "operating" || s.Color != "445566" || *s.Brightness != 25 {
			t.Errorf("%+v", s)
		}
	})
	t.Run("otherwise fills the keyboard", func(t *testing.T) {
		k, d, _, _ := newTestKeyboard(t)
		if err := k.SetColor("010203"); err != nil {
			t.Fatal(err)
		}
		data := perKeyData(d)
		if !bytes.Equal(data[:4], []byte{0, 1, 2, 3}) || !bytes.Equal(data[125*4:126*4], []byte{0, 1, 2, 3}) || !bytes.Equal(data[126*4:], make([]byte, 8)) {
			t.Error("fill must light cells 0..125")
		}
	})
	t.Run("bad colour", func(t *testing.T) {
		k, _, _, _ := newTestKeyboard(t)
		if err := k.SetColor("zzz"); err == nil || err.Error() != "colour must be RRGGBB" {
			t.Errorf("%v", err)
		}
	})
}

func TestPaint(t *testing.T) {
	m := keymap.Verified()
	k, d, _, _ := newTestKeyboard(t)
	n, err := k.Paint(m, []keymap.KeyColor{{Key: "w", Color: "ff0000"}, {Key: "arrows", Color: "00ff00"}}, KeepBrightness)
	if err != nil || n != 2 {
		t.Fatalf("Paint = %d, %v", n, err)
	}
	data := perKeyData(d)
	w := m.Key("w")[0]
	up := m.Key("up")[0]
	esc := m.Key("esc")[0]
	if !bytes.Equal(data[w*4:w*4+4], []byte{0, 255, 0, 0}) || !bytes.Equal(data[up*4:up*4+4], []byte{0, 0, 255, 0}) || !bytes.Equal(data[esc*4:esc*4+4], []byte{0, 0, 0xAE, 0xC7}) {
		t.Error("painted keys change, the others keep the stock cyan")
	}
	for _, bad := range [][]keymap.KeyColor{{{Key: "nokey", Color: "ffffff"}}, {{Key: "w", Color: "zz"}}} {
		if _, err := k.Paint(m, bad, KeepBrightness); err == nil || !isFirmwareError(err) {
			t.Errorf("%v accepted", bad)
		}
	}
	if err := k.PaintCells(map[int]string{7: "010203", 127: "ffffff"}, 10); err != nil {
		t.Fatal(err)
	}
	cells := k.Last().Cells
	if cells[7] != "010203" || cells[127] != "ffffff" || cells[8] != "00aec7" {
		t.Errorf("cells %v", cells[:10])
	}
	for _, bad := range []map[int]string{{128: "ffffff"}, {-1: "ffffff"}, {1: "zz"}} {
		if err := k.PaintCells(bad, 10); err == nil {
			t.Errorf("%v accepted", bad)
		}
	}
}

func TestCurrentCellsDefaultsToTheStockCyan(t *testing.T) {
	k, _, _, _ := newTestKeyboard(t)
	c := k.CurrentCells()
	if len(c) != 128 || c[0] != (keymap.RGB{0, 0xAE, 0xC7}) || c[125] != (keymap.RGB{0, 0xAE, 0xC7}) || c[126] != (keymap.RGB{}) || c[127] != (keymap.RGB{}) {
		t.Errorf("%v", c[120:])
	}
	_ = k.state.Set("keyboard", State{Mode: "perkey", Cells: []string{"zzzzzz"}})
	if c := k.CurrentCells(); c[0] != (keymap.RGB{0, 0xAE, 0xC7}) {
		t.Error("corrupt remembered cells fall back to the stock look")
	}
}

func TestHIDTimeoutFlag(t *testing.T) {
	k, d, _, _ := newTestKeyboard(t)
	got, err := k.HIDTimeoutGet()
	if err != nil || got == nil || !*got {
		t.Fatalf("Get = %v, %v (factory state is on)", got, err)
	}
	if want := []rec{{"set", hx("b0000000000000" + "4f")}, {"get", []byte{8}}}; !reflect.DeepEqual(d.log, want) {
		t.Errorf("transfers %v", d.log)
	}
	d.reply = func([]byte) []byte { return []byte{0xB0, 0, 0, 0, 0, 0, 0, 0x4F} }
	if got, _ := k.Timeout().Get(); got == nil || *got {
		t.Errorf("off reads as %v", got)
	}
	d.reply = func([]byte) []byte { return []byte{0x00, 0, 1, 0, 0, 0, 0, 0} }
	if got, err := k.HIDTimeoutGet(); got != nil || err != nil {
		t.Errorf("an unexpected report cannot say: %v, %v", got, err)
	}
	d.reset()
	_ = k.HIDTimeoutSet(true)
	_ = k.Timeout().Set(false)
	if got, want := asHex(d.sets()), []string{"30010100000000cd", "30010000000000ce"}; !reflect.DeepEqual(got, want) {
		t.Errorf("set packets %v want %v", got, want)
	}
}

func TestTheDeviceIsOpenedOnceAndKept(t *testing.T) {
	k, d, _, _ := newTestKeyboard(t)
	for i := 0; i < 3; i++ {
		if b, _ := k.Brightness(); b != 25 {
			t.Fatal("brightness")
		}
	}
	_, _ = k.HIDTimeoutGet()
	_, _ = k.HIDTimeoutGet()
	_ = k.send(pkt(0x30, 0x01, 1, 0, 0, 0, 0))
	_ = k.PerKeyFrame(make([]keymap.RGB, 128), 20)
	if d.opens != 1 || len(d.closes) != 0 {
		t.Errorf("opens=%d closes=%v: one open for all of that, not one per operation", d.opens, d.closes)
	}
}

func TestStaleHandleIsReopenedOnceAndTheCallStillSucceeds(t *testing.T) {
	k, d, _, _ := newTestKeyboard(t)
	_, _ = k.Brightness()
	d.failNext = 1
	if b, err := k.Brightness(); err != nil || b != 25 {
		t.Fatalf("Brightness = %d, %v", b, err)
	}
	if d.opens != 2 || !reflect.DeepEqual(d.closes, []int{100}) {
		t.Errorf("opens=%d closes=%v", d.opens, d.closes)
	}
}

func TestSecondFailureIsReturnedAndHandleDropped(t *testing.T) {
	k, d, _, _ := newTestKeyboard(t)
	_, _ = k.Brightness()
	d.failNext = 2
	if _, err := k.Brightness(); err == nil {
		t.Fatal("expected the error")
	}
	if k.t != nil {
		t.Error("nothing stale is kept")
	}
	if b, err := k.Brightness(); err != nil || b != 25 {
		t.Errorf("the next call opens a fresh handle: %d, %v", b, err)
	}
}

func TestNoPermissionGivesAClearMessage(t *testing.T) {
	k, d, _, _ := newTestKeyboard(t)
	d.openErr = firmware.Errorf("no permission on /dev/hidraw3: add yourself to group plugdev and install the udev rule")
	_, err := k.Brightness()
	if err == nil || !strings.Contains(err.Error(), "no permission") || !strings.Contains(err.Error(), "plugdev") {
		t.Errorf("%v", err)
	}
	if err := k.PerKey(nil, 10); err == nil {
		t.Error("PerKey must fail too")
	}
}

func TestTransfersOfDifferentGoroutinesNeverInterleave(t *testing.T) {
	k, d, _, _ := newTestKeyboard(t)
	var wg sync.WaitGroup
	for g := 0; g < 4; g++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for i := 0; i < 150; i++ {
				if _, err := k.Brightness(); err != nil {
					t.Error(err)
				}
			}
		}()
	}
	wg.Add(1)
	go func() {
		defer wg.Done()
		for i := 0; i < 5; i++ {
			_ = k.PerKeyFrame(nil, 10)
		}
	}()
	wg.Wait()
	// a request is always followed by its own reply; an upload is one uninterrupted run
	for i := 0; i < len(d.log); i++ {
		r := d.log[i]
		if r.Kind == "set" && r.Data[0] == 0x88 && (i+1 >= len(d.log) || d.log[i+1].Kind != "get") {
			t.Fatalf("request at %d not followed by its reply", i)
		}
		if r.Kind == "out" && d.log[i-1].Kind != "set" && d.log[i-1].Kind != "out" {
			t.Fatalf("output report at %d inside someone else's sequence", i)
		}
	}
	if d.opens != 1 {
		t.Errorf("opens = %d", d.opens)
	}
}

func TestPrewarmOpensInTheBackgroundWithoutASecondOpen(t *testing.T) {
	k, d, _, _ := newTestKeyboard(t)
	select {
	case <-k.Prewarm():
	case <-time.After(3 * time.Second):
		t.Fatal("prewarm did not finish")
	}
	if d.opens != 1 {
		t.Fatalf("opens = %d", d.opens)
	}
	if b, _ := k.Brightness(); b != 25 || d.opens != 1 {
		t.Errorf("the foreground call reuses the background open (opens=%d)", d.opens)
	}
	if err := k.Close(); err != nil || !reflect.DeepEqual(d.closes, []int{100}) {
		t.Errorf("Close: %v %v", err, d.closes)
	}
	if err := k.Close(); err != nil {
		t.Error("Close twice")
	}
	if _, err := k.Brightness(); err != nil || d.opens != 2 {
		t.Error("a closed controller reopens on use")
	}
}

func TestStateJSONIsCompatibleWith03x(t *testing.T) {
	// as written by keyboard.py
	k, _, st, _ := newTestKeyboard(t)
	if err := st.Set("keyboard", map[string]any{"mode": "effect", "name": "wave", "color": "00ff00", "speed": 4, "brightness": 0, "direction": 0}); err != nil {
		t.Fatal(err)
	}
	got := k.Last()
	if got.Name != "wave" || got.BrightnessOr(9) != 0 || got.DirectionOr(9) != 0 || got.SpeedOr(9) != 4 {
		t.Errorf("%+v: brightness 0 and direction 0 are real values", got)
	}
	_ = k.SetEffect("wave", "00ff00", 4, 0, 0)
	raw := st.Load()["keyboard"].(map[string]any)
	if raw["brightness"] != float64(0) || raw["direction"] != float64(0) || raw["mode"] != "effect" || len(raw) != 6 {
		t.Errorf("0.3.x readers need these fields: %v", raw)
	}
}
