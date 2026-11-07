package perkeyfx

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/Order52/truesense/internal/firmware"
	"github.com/Order52/truesense/internal/keyboard"
	"github.com/Order52/truesense/internal/keymap"
	"github.com/Order52/truesense/internal/userstate"
)

func hexList(h []string) []keymap.RGB {
	out := make([]keymap.RGB, len(h))
	for i, s := range h {
		out[i], _ = keymap.ParseHex(s)
	}
	return out
}

func cellsHex(cv []keymap.RGB) string {
	var sb strings.Builder
	for _, c := range cv {
		sb.WriteString(c.Hex())
	}
	return sb.String()
}

func cyanBase() []keymap.RGB {
	b := keymap.Canvas(keymap.RGB{0x00, 0xAE, 0xC7})
	return b
}

// Frames produced by the Python implementation (perkeyfx.render) for every mode, two bases, several
// times and speeds, with and without a key selection.
func TestRenderMatchesPython(t *testing.T) {
	raw, err := os.ReadFile("testdata/golden.json")
	if err != nil {
		t.Fatal(err)
	}
	var g struct {
		Bases  map[string][]string `json:"bases"`
		Frames []struct {
			Mode  string  `json:"mode"`
			Base  string  `json:"base"`
			T     float64 `json:"t"`
			Speed int     `json:"speed"`
			Keys  []int   `json:"keys"`
			Cells string  `json:"cells"`
		} `json:"frames"`
	}
	if err := json.Unmarshal(raw, &g); err != nil {
		t.Fatal(err)
	}
	if len(g.Frames) < 600 {
		t.Fatalf("only %d golden frames", len(g.Frames))
	}
	bad := 0
	for _, f := range g.Frames {
		got := cellsHex(Render(f.Mode, f.T, hexList(g.Bases[f.Base]), f.Keys, f.Speed))
		if got != f.Cells {
			bad++
			if bad <= 5 {
				for c := 0; c < 128; c++ {
					if got[c*6:c*6+6] != f.Cells[c*6:c*6+6] {
						t.Errorf("%s base=%s t=%v speed=%d keys=%v: cell %d got %s want %s", f.Mode, f.Base, f.T, f.Speed, f.Keys, c, got[c*6:c*6+6], f.Cells[c*6:c*6+6])
						break
					}
				}
			}
		}
	}
	if bad > 0 {
		t.Errorf("%d of %d frames differ from Python", bad, len(g.Frames))
	}
}

func TestModesListMatchesPython(t *testing.T) {
	want := []string{"pulse", "wave", "cycle", "twinkle", "rainbow", "breathing", "scanner", "comet", "ripple", "rain", "matrix", "fire", "plasma", "aurora", "lightning", "confetti", "starlight", "mood"}
	if got := ModeNames(); !reflect.DeepEqual(got, want) {
		t.Errorf("%v", got)
	}
	for _, m := range Modes() {
		if m.Label == "" || !IsMode(m.Name) {
			t.Errorf("%+v", m)
		}
	}
	if IsMode("off") || IsMode("disco") || IsMode("") {
		t.Error("IsMode")
	}
}

// Animations only touch the chosen keys and change over time.
func TestAnimationsOnlyTouchTheChosenKeys(t *testing.T) {
	m := keymap.Verified()
	w, a, esc := m.Key("w")[0], m.Key("a")[0], m.Key("esc")[0]
	base := cyanBase()
	for _, mode := range ModeNames() {
		f0 := Render(mode, 0, base, []int{w, a}, 5)
		if len(f0) != 128 {
			t.Fatalf("%s: %d cells", mode, len(f0))
		}
		if f0[esc] != (keymap.RGB{0x00, 0xAE, 0xC7}) {
			t.Errorf("%s: keys that were not chosen must keep their colour", mode)
		}
		seen := map[[2]keymap.RGB]bool{}
		for i := 0; i < 60; i++ {
			f := Render(mode, 0.37*float64(i), base, []int{w, a}, 5)
			seen[[2]keymap.RGB{f[w], f[a]}] = true
		}
		if len(seen) <= 1 {
			t.Errorf("%s must change over time", mode)
		}
	}
	wave := Render("wave", 0.5, base, nil, 5)
	distinct := map[keymap.RGB]bool{}
	for c := 0; c < 126; c += 6 {
		distinct[wave[c]] = true
	}
	if len(distinct) <= 5 {
		t.Errorf("a wave over all keys shows many colours, got %d", len(distinct))
	}
}

func TestRenderEdgeCases(t *testing.T) {
	base := cyanBase()
	if got := Render("off", 3, base, nil, 5); !reflect.DeepEqual(got, base) {
		t.Error("off draws the base")
	}
	if got := Render("nosuchmode", 3, base, []int{1}, 5); !reflect.DeepEqual(got, base) {
		t.Error("an unknown mode draws the base")
	}
	short := Render("pulse", 1, []keymap.RGB{{9, 9, 9}}, nil, 5)
	if len(short) != 128 {
		t.Errorf("a short base is padded: %d", len(short))
	}
	// out-of-range cells are ignored, speed is clamped, negative time does not panic
	_ = Render("starlight", -3.5, base, []int{-1, 500, 127}, 99)
	_ = Render("confetti", -1e6, base, nil, 0)
	if !reflect.DeepEqual(Render("wave", 1, base, nil, 99), Render("wave", 1, base, nil, 10)) || !reflect.DeepEqual(Render("wave", 1, base, nil, -4), Render("wave", 1, base, nil, 1)) {
		t.Error("speed is clamped to 1..10")
	}
	// the last two cells are only drawn when named
	f := Render("wave", 1, base, nil, 5)
	if f[126] != (keymap.RGB{}) || f[127] != (keymap.RGB{}) {
		t.Error("cells 126 and 127 stay black unless chosen")
	}
	if f := Render("wave", 1, base, []int{127}, 5); f[127] == (keymap.RGB{}) {
		t.Error("an explicitly chosen cell 127 is drawn")
	}
}

func TestHashMatchesPythonVectors(t *testing.T) {
	// perkeyfx._h(i, k, z) in the Python implementation
	for _, tc := range []struct {
		i, k, z int
		want    float64
	}{
		{0, 0, 0, 0.272125244140625}, {1, 1, 0, 0.8271636962890625}, {125, 7, 0, 0.57080078125},
		{3, -2, 12, 0.5669097900390625}, {-5, 9, 0, 0.6617431640625}, {21, 8, 0, 0.211517333984375},
		{100000, 100000, 100000, 0.977325439453125},
	} {
		if got := hash(tc.i, tc.k, tc.z); got != tc.want {
			t.Errorf("hash(%d, %d, %d) = %v, want %v", tc.i, tc.k, tc.z, got, tc.want)
		}
	}
}

type fakeCells struct{ cells []keymap.RGB }

func (f fakeCells) CurrentCells() []keymap.RGB { return f.cells }

func newController(t *testing.T) (*Controller, *userstate.Store, *time.Time) {
	t.Helper()
	st := &userstate.Store{Path: filepath.Join(t.TempDir(), "user.json")}
	now := time.Unix(1_700_000_000, 500_000_000)
	c := NewController(st, fakeCells{cells: cyanBase()})
	c.Now = func() time.Time { return now }
	return c, st, &now
}

func TestSettingsSetAndGet(t *testing.T) {
	c, st, _ := newController(t)
	if got := c.Get(); got.Mode != "off" || got.Base != nil {
		t.Errorf("nothing set: %+v", got)
	}
	got, err := c.Set("wave", 4, []int{20, 5, 20, 7}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if got.Mode != "wave" || got.Speed != 4 || !reflect.DeepEqual(got.Keys, []int{5, 7, 20}) || len(got.Base) != 128 || got.Base[0] != "00aec7" || got.Base[127] != "000000" || got.T0 != 1_700_000_000.5 {
		t.Errorf("%+v", got)
	}
	// the stored form carries the 0.3.x field names
	var raw map[string]any
	b, _ := json.Marshal(st.Load()["perkey_fx"])
	_ = json.Unmarshal(b, &raw)
	for _, k := range []string{"mode", "speed", "keys", "base", "t0"} {
		if _, ok := raw[k]; !ok {
			t.Errorf("stored settings lack %q: %v", k, raw)
		}
	}
	if _, err := c.Set("off", 5, nil, nil); err != nil {
		t.Fatal(err)
	}
	if got := c.Get(); got.Mode != "off" || got.Keys != nil {
		t.Errorf("%+v", got)
	}
	custom := []keymap.RGB{{1, 2, 3}}
	if got, _ := c.Set("pulse", 5, nil, custom); len(got.Base) != 1 || got.Base[0] != "010203" {
		t.Errorf("an explicit base is kept: %+v", got.Base)
	}
}

func TestSettingsValidation(t *testing.T) {
	c, st, _ := newController(t)
	for _, tc := range []struct {
		mode  string
		speed int
		keys  []int
		want  string
	}{
		{"disco", 5, nil, "animation: off, pulse, wave, cycle"},
		{"wave", 50, nil, "speed 1..10"},
		{"wave", 0, nil, "speed 1..10"},
		{"wave", 5, []int{128}, "cell 128: 0..127"},
		{"wave", 5, []int{-1}, "cell -1: 0..127"},
	} {
		_, err := c.Set(tc.mode, tc.speed, tc.keys, nil)
		var fe *firmware.Error
		if !errors.As(err, &fe) || !strings.HasPrefix(err.Error(), tc.want) {
			t.Errorf("%+v: %v", tc, err)
		}
	}
	if len(st.Load()) != 0 {
		t.Error("refused settings are not stored")
	}
}

func TestSettingsReadOldFiles(t *testing.T) {
	c, st, _ := newController(t)
	// as written by perkeyfx.set_fx: keys null
	_ = st.Set("perkey_fx", map[string]any{"mode": "wave", "speed": 4, "keys": nil, "base": []string{"00aec7"}, "t0": 1700000000.25})
	got := c.Get()
	if got.Mode != "wave" || got.Keys != nil || got.T0 != 1700000000.25 {
		t.Errorf("%+v", got)
	}
	_ = st.Set("perkey_fx", "garbage")
	if c.Get().Mode != "off" {
		t.Error("garbage reads as off")
	}
	var nilStore Controller
	if nilStore.Get().Mode != "off" {
		t.Error("no store")
	}
}

// ---- runner ----

type fakeSender struct {
	bright     int
	brightErr  error
	brightRead int
	uploads    []upload
	failNext   int
	cells      []keymap.RGB
}

type upload struct {
	cells      []keymap.RGB
	brightness int
}

func (f *fakeSender) Brightness() (int, error) { f.brightRead++; return f.bright, f.brightErr }
func (f *fakeSender) PerKeyFrame(c []keymap.RGB, b int) error {
	if f.failNext > 0 {
		f.failNext--
		return errors.New("usb gone")
	}
	f.uploads = append(f.uploads, upload{append([]keymap.RGB(nil), c...), b})
	return nil
}
func (f *fakeSender) CurrentCells() []keymap.RGB { return f.cells }

type rig struct {
	r     *Runner
	s     *fakeSender
	now   time.Time
	fx    Settings
	waits []time.Duration
}

func newRig() *rig {
	g := &rig{s: &fakeSender{bright: 40, cells: cyanBase()}, now: time.Unix(1_700_000_000, 0)}
	g.r = &Runner{Settings: func() Settings { return g.fx }, Sender: g.s, Now: func() time.Time { return g.now }}
	return g
}

func (g *rig) on(mode string) {
	g.fx = Settings{Mode: mode, Speed: 5, Base: hexOf(cyanBase()), T0: Seconds(g.now)}
}

func hexOf(c []keymap.RGB) []string {
	out := make([]string, len(c))
	for i, v := range c {
		out[i] = v.Hex()
	}
	return out
}

func (g *rig) step() time.Duration {
	d := g.r.Step()
	g.now = g.now.Add(d)
	return d
}

func TestRunnerIsIdleWhileNothingIsOn(t *testing.T) {
	g := newRig()
	for _, fx := range []Settings{{Mode: "off"}, {Mode: "wave"}, {Mode: "disco", Base: []string{"000000"}}, {Mode: "wave", Base: []string{"zz"}}} {
		g.fx = fx
		if d := g.step(); d != 500*time.Millisecond {
			t.Errorf("%+v: idle wait %v", fx, d)
		}
	}
	if len(g.s.uploads) != 0 || g.s.brightRead != 0 {
		t.Error("an idle runner never touches the keyboard")
	}
}

func TestRunnerDrawsFramesAtASteadyPaceAndDoesNotResendIdenticalOnes(t *testing.T) {
	g := newRig()
	g.on("wave")
	for i := 0; i < 10; i++ {
		if d := g.step(); d != FrameInterval {
			t.Fatalf("frame wait %v", d)
		}
	}
	if len(g.s.uploads) != 10 {
		t.Fatalf("%d uploads, want one per step", len(g.s.uploads))
	}
	for i, u := range g.s.uploads {
		want := Render("wave", 0.2*float64(i), hexList(g.fx.Base), nil, 5)
		if !reflect.DeepEqual(u.cells, want) || u.brightness != 40 {
			t.Errorf("frame %d wrong (brightness %d)", i, u.brightness)
		}
	}
	// a static-looking animation (everything dark under "off"-like draw) is sent once:
	g2 := newRig()
	g2.on("breathing")
	g2.fx.Base = hexOf(make([]keymap.RGB, 128))
	g2.fx.Keys = []int{3}
	g2.fx.Speed = 1
	g2.fx.T0 = 0 // t = 0 for ever: the frame never changes
	for i := 0; i < 5; i++ {
		g2.step()
	}
	if len(g2.s.uploads) != 1 {
		t.Errorf("identical frames: %d uploads, want 1", len(g2.s.uploads))
	}
	g2.s.bright = 10 // Fn+F11 changed the level
	g2.now = g2.now.Add(6 * time.Second)
	g2.step()
	if len(g2.s.uploads) != 2 || g2.s.uploads[1].brightness != 10 {
		t.Errorf("a new brightness forces a new upload: %+v", len(g2.s.uploads))
	}
}

func TestRunnerRefreshesBrightnessEveryFiveSeconds(t *testing.T) {
	g := newRig()
	g.on("pulse")
	for i := 0; i < 24; i++ { // 24 * 0.2 s = 4.8 s
		g.step()
	}
	if g.s.brightRead != 1 {
		t.Errorf("%d brightness reads in 4.8 s", g.s.brightRead)
	}
	g.step() // frame at 4.8 s
	g.step() // frame at 5.0 s: not yet "more than 5 s"
	if g.s.brightRead != 1 {
		t.Errorf("%d brightness reads at exactly 5 s", g.s.brightRead)
	}
	g.step() // frame at 5.2 s
	if g.s.brightRead != 2 {
		t.Errorf("%d brightness reads after 5.2 s", g.s.brightRead)
	}
}

func TestRunnerKeepsTheLastBrightnessWhenItCannotBeRead(t *testing.T) {
	g := newRig()
	g.on("pulse")
	g.step()
	g.s.brightErr = errors.New("busy")
	g.s.bright = 0
	g.now = g.now.Add(6 * time.Second)
	g.step()
	g.s.brightErr = nil // a keyboard that reads 0 (dark) is not animated into darkness either
	g.now = g.now.Add(6 * time.Second)
	g.step()
	for i, u := range g.s.uploads {
		if u.brightness != 40 {
			t.Errorf("upload %d at brightness %d", i, u.brightness)
		}
	}
	h := newRig()
	h.on("pulse")
	h.s.bright = 0
	h.step()
	if h.s.uploads[0].brightness != 25 {
		t.Errorf("a dark keyboard from the start uses 25, got %d", h.s.uploads[0].brightness)
	}
}

func TestRunnerBacksOffAfterAFailedUploadAndRetries(t *testing.T) {
	g := newRig()
	g.on("wave")
	g.s.failNext = 2
	if d := g.step(); d != ErrorBackoff+FrameInterval {
		t.Errorf("after a failure %v (1 s pause, then the frame interval)", d)
	}
	if d := g.step(); d != 1200*time.Millisecond {
		t.Errorf("%v", d)
	}
	if d := g.step(); d != FrameInterval || len(g.s.uploads) != 1 {
		t.Errorf("recovered: %v, %d uploads", d, len(g.s.uploads))
	}
}

func TestRunnerHandsTheBaseColoursBackOnceWhenSwitchedOff(t *testing.T) {
	g := newRig()
	g.on("wave")
	g.step()
	g.step()
	n := len(g.s.uploads)
	g.fx.Mode = "off"
	if d := g.step(); d != IdleInterval {
		t.Errorf("%v", d)
	}
	if len(g.s.uploads) != n+1 {
		t.Fatalf("hand-back uploads: %d", len(g.s.uploads)-n)
	}
	last := g.s.uploads[n]
	if !reflect.DeepEqual(last.cells, cyanBase()) || last.brightness != keyboard.KeepBrightness {
		t.Errorf("base handed back with brightness %d", last.brightness)
	}
	g.step()
	g.step()
	if len(g.s.uploads) != n+1 {
		t.Error("the hand-back happens once")
	}
	// without base colours in the settings the keyboard's current ones are used
	h := newRig()
	h.on("wave")
	h.step()
	h.fx = Settings{Mode: "off"}
	h.step()
	if got := h.s.uploads[len(h.s.uploads)-1]; !reflect.DeepEqual(got.cells, h.s.cells) {
		t.Error("fallback to the current cells")
	}
}

func TestRunStopsWithTheContextAndRestoresTheBase(t *testing.T) {
	g := newRig()
	g.on("wave")
	ctx, cancel := context.WithCancel(context.Background())
	waits := 0
	g.r.Wait = func(ctx context.Context, d time.Duration) error {
		g.waits = append(g.waits, d)
		g.now = g.now.Add(d)
		waits++
		if waits == 5 {
			cancel()
		}
		return ctx.Err()
	}
	done := make(chan struct{})
	go func() { g.r.Run(ctx); close(done) }()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("Run did not stop")
	}
	if len(g.waits) != 5 {
		t.Errorf("waits %v", g.waits)
	}
	for _, d := range g.waits {
		if d != FrameInterval {
			t.Errorf("steady pace expected, got %v", d)
		}
	}
	if n := len(g.s.uploads); n != 6 || g.s.uploads[n-1].brightness != keyboard.KeepBrightness {
		t.Errorf("5 frames and the base handed back on stop: %d uploads", n)
	}
	// already cancelled: no work at all
	g.s.uploads = nil
	g.r.running = false
	g.r.Run(ctx)
	if len(g.s.uploads) != 0 {
		t.Error("a cancelled context draws nothing")
	}
}

func TestRealWaitHonoursTheContext(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := sleepCtx(ctx, time.Hour); err == nil {
		t.Error("cancelled")
	}
	if err := sleepCtx(context.Background(), time.Millisecond); err != nil {
		t.Error(err)
	}
	r := NewRunner(func() Settings { return Settings{Mode: "off"} }, &fakeSender{})
	if r.Now == nil || r.Wait == nil {
		t.Error("NewRunner wires the clock and the wait")
	}
}

func TestRunnerThroughTheSimulatedKeyboard(t *testing.T) {
	st := &userstate.Store{Path: filepath.Join(t.TempDir(), "u.json")}
	kb, sim := keyboard.NewSim(st)
	c := NewController(st, kb)
	if _, err := c.Set("wave", 5, nil, nil); err != nil {
		t.Fatal(err)
	}
	r := NewRunner(c.Get, kb)
	now := time.Now()
	r.Now = func() time.Time { return now }
	r.Step()
	if sim.Frames() != 8 {
		t.Errorf("one frame is 8 reports, got %d", sim.Frames())
	}
	if kb.Last().Mode == "perkey" {
		t.Error("animation frames are not remembered as the look")
	}
	_, _ = c.Set("off", 5, nil, nil)
	r.Step()
	if sim.Frames() != 16 {
		t.Errorf("the base is handed back: %d", sim.Frames())
	}
}

func FuzzRender(f *testing.F) {
	f.Add("wave", 0.5, 5, 3)
	f.Add("starlight", -1e9, 99, -4)
	f.Add("confetti", 1e12, 0, 500)
	f.Fuzz(func(t *testing.T, mode string, tt float64, speed, key int) {
		if tt != tt { // NaN is not a time
			return
		}
		fr := Render(mode, tt, cyanBase(), []int{key}, speed)
		if len(fr) != 128 {
			t.Fatal("frame length")
		}
	})
}

func FuzzParseBase(f *testing.F) {
	f.Add("00aec7,ff0000")
	f.Add("zz,,")
	f.Fuzz(func(t *testing.T, s string) {
		parts := strings.Split(s, ",")
		if base, ok := parseBase(parts); ok {
			again := make([]string, len(base))
			for i, c := range base {
				again[i] = c.Hex()
			}
			if _, ok := parseBase(again); !ok {
				t.Fatal("re-parse")
			}
		}
	})
}
