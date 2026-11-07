package startup

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/Order52/truesense/internal/config"
	"github.com/Order52/truesense/internal/hw"
	"github.com/Order52/truesense/internal/keyboard"
	"github.com/Order52/truesense/internal/looks"
	"github.com/Order52/truesense/internal/userstate"
)

type world struct {
	log []string

	profiles   []string
	keyProfs   map[string]bool
	savedLooks map[string]*looks.Look
	last       keyboard.State
	replayMsg  string
	rearState  map[string]any

	// failures: a call prefix and how many times it fails before it works
	fail    map[string]int
	sleeps  []time.Duration
	cfgPath string
}

func newWorld(t *testing.T) *world {
	return &world{profiles: []string{"gaming", "quiet"}, keyProfs: map[string]bool{"mine": true}, savedLooks: map[string]*looks.Look{}, fail: map[string]int{}, cfgPath: filepath.Join(t.TempDir(), "config.toml"), replayMsg: "effect wave"}
}

func (w *world) rec(format string, a ...any) error {
	line := fmt.Sprintf(format, a...)
	w.log = append(w.log, line)
	for p, n := range w.fail {
		if strings.HasPrefix(line, p) && n > 0 {
			w.fail[p] = n - 1
			return errors.New("not ready")
		}
	}
	return nil
}

func (w *world) startup() *Startup {
	return New(Env{
		Config:      func() (*config.Config, error) { return config.Load("", w.cfgPath) },
		ConfigPath:  w.cfgPath,
		Profiles:    (*fProfiles)(w),
		KeyProfiles: (*fKeyProfiles)(w),
		Looks:       (*fLooks)(w),
		Keyboard:    (*fKB)(w),
		Rear:        (*fRear)(w),
		State:       (*fState)(w),
		Delay:       2 * time.Second,
		Sleep:       func(_ context.Context, d time.Duration) { w.sleeps = append(w.sleeps, d) },
	})
}

type fProfiles world

func (p *fProfiles) Names() []string { return p.profiles }
func (p *fProfiles) Apply(_ context.Context, name string) (string, error) {
	return "Profile '" + name + "' applied (1 settings)", (*world)(p).rec("profile %s", name)
}

type fKeyProfiles world

func (k *fKeyProfiles) Names() []string      { return []string{"mine"} }
func (k *fKeyProfiles) Exists(n string) bool { return k.keyProfs[n] }
func (k *fKeyProfiles) Apply(n string, b int) error {
	return (*world)(k).rec("keyprofile %s b=%d", n, b)
}

type fLooks world

func (l *fLooks) Names() []string {
	out := []string{"native"}
	for n := range l.savedLooks {
		out = append(out, n)
	}
	return out
}
func (l *fLooks) Load(name string) (*looks.Look, error) {
	if lk, ok := l.savedLooks[name]; ok {
		return lk, nil
	}
	return nil, fmt.Errorf("no look '%s'", name)
}
func (l *fLooks) Apply(lk *looks.Look) ([]string, error) {
	var out []string
	if err := (*world)(l).rec("look apply kb=%v rear=%v", lk.Keyboard != nil, lk.Rear != nil); err != nil {
		return nil, err
	}
	if lk.Keyboard != nil {
		out = append(out, "keyboard: effect "+lk.Keyboard.Name)
	}
	if lk.Rear != nil {
		out = append(out, "rear: "+lk.Rear.Mode)
	}
	return out, nil
}

type fKB world

func (k *fKB) Last() keyboard.State { return k.last }
func (k *fKB) Replay(s keyboard.State) (string, error) {
	if err := (*world)(k).rec("kb replay %s", s.Name); err != nil {
		return "", err
	}
	return k.replayMsg, nil
}
func (k *fKB) RestoreDefault() error { return (*world)(k).rec("kb default") }
func (k *fKB) Off() error            { return (*world)(k).rec("kb off") }

type fRear world

func (r *fRear) RearSet(o hw.RearOpts) (hw.Rear, error) {
	return hw.Rear{}, (*world)(r).rec("rear %s %s s=%d b=%d d=%d zones=%v verify=%v", o.Mode, o.Color, o.Speed, o.Brightness, o.Direction, o.Zones, o.Verify)
}

type fState world

func (s *fState) GetInto(key string, out any) error {
	v, ok := s.rearState, s.rearState != nil
	if key != "rear_state" || !ok {
		return userstate.ErrNotFound
	}
	st := out.(*looks.Rear)
	mode, _ := v["mode"].(string)
	color, _ := v["color"].(string)
	st.Mode, st.Color = mode, color
	for k, dst := range map[string]**int{"speed": &st.Speed, "brightness": &st.Brightness, "direction": &st.Direction} {
		if n, ok := v[k].(int); ok {
			*dst = &n
		}
	}
	if z, ok := v["zones"].([]string); ok {
		st.Zones = z
	}
	return nil
}

func writeCfg(t *testing.T, w *world, body string) {
	t.Helper()
	if body == "" {
		return
	}
	if err := os.WriteFile(w.cfgPath, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

func apply(t *testing.T, w *world, body string) []string {
	t.Helper()
	writeCfg(t, w, body)
	out, err := w.startup().Apply(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	return out
}

func TestApplyDefaultsRestoreLast(t *testing.T) {
	w := newWorld(t)
	w.last = keyboard.State{Mode: "effect", Name: "wave"}
	w.rearState = map[string]any{"mode": "breathing", "color": "ff0000", "speed": 2, "brightness": 60, "direction": 0}
	out := apply(t, w, "")
	if want := []string{"keyboard: effect wave", "rear: restored"}; !reflect.DeepEqual(out, want) {
		t.Errorf("%q", out)
	}
	if want := []string{"kb replay wave", "rear breathing ff0000 s=2 b=60 d=0 zones=[] verify=true"}; !reflect.DeepEqual(w.log, want) {
		t.Errorf("%q", w.log)
	}
}

func TestApplyNothingSaved(t *testing.T) {
	w := newWorld(t)
	w.replayMsg = ""
	out := apply(t, w, "")
	if want := []string{"keyboard: nothing saved", "rear: nothing saved"}; !reflect.DeepEqual(out, want) {
		t.Errorf("%q", out)
	}
	if len(w.log) != 1 || w.log[0] != "kb replay " {
		t.Errorf("an unknown look is replayed as nothing, and no rear setting is invented: %q", w.log)
	}
}

func TestApplyEachChoice(t *testing.T) {
	for name, tc := range map[string]struct {
		cfg string
		out []string
		log []string
	}{
		"none leaves everything as it is": {"[startup]\nkeyboard = \"none\"\nrear = \"none\"\n", nil, nil},
		"default": {"[startup]\nkeyboard = \"default\"\nrear = \"default\"\n", []string{"keyboard: stock look", "rear: stock look"},
			[]string{"kb default", "rear breathing 3723c7 s=3 b=100 d=1 zones=[] verify=true"}},
		"off": {"[startup]\nkeyboard = \"off\"\nrear = \"off\"\n", []string{"keyboard: off", "rear: off"},
			[]string{"kb off", "rear static 00aec7 s=3 b=0 d=1 zones=[] verify=true"}},
		"a lighting profile":               {"[startup]\nkeyboard = \"profile:mine\"\nrear = \"none\"\n", []string{"keyboard: profile:mine"}, []string{"keyprofile mine b=-1"}},
		"a profile first, then the lights": {"[startup]\nprofile = \"gaming\"\nkeyboard = \"off\"\nrear = \"none\"\n", []string{"profile: Profile 'gaming' applied (1 settings)", "keyboard: off"}, []string{"profile gaming", "kb off"}},
	} {
		w := newWorld(t)
		out := apply(t, w, tc.cfg)
		if !reflect.DeepEqual(out, tc.out) || !reflect.DeepEqual(w.log, tc.log) {
			t.Errorf("%s:\n out %q want %q\n log %q want %q", name, out, tc.out, w.log, tc.log)
		}
	}
}

func TestApplyLooksSetKeyboardAndRearTogether(t *testing.T) {
	w := newWorld(t)
	w.savedLooks["cosy"] = &looks.Look{Keyboard: &keyboard.State{Mode: "effect", Name: "snow"}, Rear: &looks.Rear{Mode: "wave"}}
	out := apply(t, w, "[startup]\nkeyboard = \"look:cosy\"\nrear = \"look:cosy\"\n")
	if want := []string{"keyboard: keyboard: effect snow", "rear: rear: wave"}; !reflect.DeepEqual(out, want) {
		t.Errorf("%q", out)
	}
	if want := []string{"look apply kb=true rear=false", "look apply kb=false rear=true"}; !reflect.DeepEqual(w.log, want) {
		t.Errorf("each half is applied on its own: %q", w.log)
	}
}

func TestApplyLookOnlyForOneHalfLeavesTheOtherChoice(t *testing.T) {
	w := newWorld(t)
	w.savedLooks["cosy"] = &looks.Look{Keyboard: &keyboard.State{Mode: "effect", Name: "snow"}}
	out := apply(t, w, "[startup]\nkeyboard = \"look:cosy\"\nrear = \"off\"\n")
	if want := []string{"keyboard: keyboard: effect snow", "rear: off"}; !reflect.DeepEqual(out, want) {
		t.Errorf("%q", out)
	}
	// and the reverse: rear takes the look, keyboard keeps "off"
	w = newWorld(t)
	w.savedLooks["cosy"] = &looks.Look{Rear: &looks.Rear{Mode: "wave"}}
	out = apply(t, w, "[startup]\nkeyboard = \"off\"\nrear = \"look:cosy\"\n")
	if want := []string{"rear: rear: wave", "keyboard: off"}; !reflect.DeepEqual(out, want) {
		t.Errorf("%q", out)
	}
}

func TestApplyLookWithoutThatHalfIsUnchanged(t *testing.T) {
	w := newWorld(t)
	w.savedLooks["kbonly"] = &looks.Look{Keyboard: &keyboard.State{Mode: "effect", Name: "snow"}}
	out := apply(t, w, "[startup]\nkeyboard = \"look:kbonly\"\nrear = \"look:kbonly\"\n")
	if want := []string{"keyboard: keyboard: effect snow", "rear: unchanged"}; !reflect.DeepEqual(out, want) {
		t.Errorf("%q", out)
	}
	if len(w.log) != 1 {
		t.Errorf("nothing is sent for a missing half: %q", w.log)
	}
}

func TestApplyBothLooksUseTheKeyboardsLook(t *testing.T) { // a 0.3.x quirk kept as it was, see the report
	w := newWorld(t)
	w.savedLooks["a"] = &looks.Look{Keyboard: &keyboard.State{Mode: "effect", Name: "snow"}, Rear: &looks.Rear{Mode: "wave"}}
	w.savedLooks["b"] = &looks.Look{Rear: &looks.Rear{Mode: "neon"}}
	out := apply(t, w, "[startup]\nkeyboard = \"look:a\"\nrear = \"look:b\"\n")
	if want := []string{"keyboard: keyboard: effect snow", "rear: rear: wave"}; !reflect.DeepEqual(out, want) {
		t.Errorf("%q", out)
	}
}

func TestApplyMissingLookIsReportedNotFatal(t *testing.T) {
	w := newWorld(t)
	out := apply(t, w, "[startup]\nkeyboard = \"look:gone\"\nrear = \"off\"\n")
	if len(out) != 2 || !strings.HasPrefix(out[0], "keyboard: FAILED (no look 'gone')") || out[1] != "rear: off" {
		t.Errorf("%q", out)
	}
}

func TestApplyFailuresAreReportedPerStep(t *testing.T) {
	w := newWorld(t)
	w.fail["kb default"] = 99
	out := apply(t, w, "[startup]\nprofile = \"quiet\"\nkeyboard = \"default\"\nrear = \"default\"\n")
	if len(out) != 3 || out[0] != "profile: Profile 'quiet' applied (1 settings)" || out[1] != "keyboard: FAILED (not ready)" || out[2] != "rear: stock look" {
		t.Errorf("%q", out)
	}
	if len(w.sleeps) != 0 {
		t.Errorf("without Retries it tries once, as 0.3.x: %v", w.sleeps)
	}
}

func TestApplyProfileErrorIsAStep(t *testing.T) {
	w := newWorld(t)
	w.fail["profile"] = 1
	out := apply(t, w, "[startup]\nprofile = \"gaming\"\nkeyboard = \"none\"\nrear = \"none\"\n")
	if len(out) != 1 || !strings.HasPrefix(out[0], "profile: FAILED") {
		t.Errorf("%q", out)
	}
}

func TestApplyRetriesAKeyboardThatHasNotEnumerated(t *testing.T) {
	w := newWorld(t)
	w.fail["kb default"] = 2
	writeCfg(t, w, "[startup]\nkeyboard = \"default\"\nrear = \"none\"\n")
	s := w.startup()
	s.env.Retries = 3
	out, err := s.Apply(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if want := []string{"keyboard: stock look"}; !reflect.DeepEqual(out, want) {
		t.Errorf("%q", out)
	}
	if want := []time.Duration{2 * time.Second, 2 * time.Second}; !reflect.DeepEqual(w.sleeps, want) {
		t.Errorf("sleeps %v", w.sleeps)
	}
	if len(w.log) != 3 {
		t.Errorf("%q", w.log)
	}
}

func TestApplyGivesUpAfterTheRetries(t *testing.T) {
	w := newWorld(t)
	w.fail["rear"] = 99
	writeCfg(t, w, "[startup]\nkeyboard = \"none\"\nrear = \"off\"\n")
	s := w.startup()
	s.env.Retries = 2
	out, _ := s.Apply(context.Background())
	if want := []string{"rear: FAILED (not ready)"}; !reflect.DeepEqual(out, want) || len(w.log) != 3 || len(w.sleeps) != 2 {
		t.Errorf("%q %q %v", out, w.log, w.sleeps)
	}
}

func TestApplyProfileIsNeverRetried(t *testing.T) {
	w := newWorld(t)
	w.fail["profile"] = 99
	writeCfg(t, w, "[startup]\nprofile = \"gaming\"\nkeyboard = \"none\"\nrear = \"none\"\n")
	s := w.startup()
	s.env.Retries = 5
	_, _ = s.Apply(context.Background())
	if len(w.log) != 1 {
		t.Errorf("a profile reapplied would redo changes the user may have made since: %q", w.log)
	}
}

func TestApplyStopsRetryingWhenCancelled(t *testing.T) {
	w := newWorld(t)
	w.fail["kb off"] = 99
	writeCfg(t, w, "[startup]\nkeyboard = \"off\"\nrear = \"none\"\n")
	ctx, cancel := context.WithCancel(context.Background())
	s := w.startup()
	s.env.Retries = 50
	s.env.Sleep = func(context.Context, time.Duration) { cancel() }
	out, _ := s.Apply(ctx)
	if len(w.log) != 1 || len(out) != 1 {
		t.Errorf("%q %q", w.log, out)
	}
}

func TestRearLastIncompleteState(t *testing.T) {
	w := newWorld(t)
	w.rearState = map[string]any{"mode": "wave"}
	out := apply(t, w, "[startup]\nkeyboard = \"none\"\nrear = \"last\"\n")
	if len(out) != 1 || !strings.HasPrefix(out[0], "rear: FAILED (") {
		t.Errorf("%q", out)
	}
}

func TestRearLastKeepsZones(t *testing.T) {
	w := newWorld(t)
	w.rearState = map[string]any{"mode": "static", "color": "ff0000", "speed": 3, "brightness": 100, "direction": 1, "zones": []string{"ff0000", "00ff00", "0000ff"}}
	apply(t, w, "[startup]\nkeyboard = \"none\"\nrear = \"last\"\n")
	if want := []string{"rear static ff0000 s=3 b=100 d=1 zones=[ff0000 00ff00 0000ff] verify=true"}; !reflect.DeepEqual(w.log, want) {
		t.Errorf("%q", w.log)
	}
}

func TestApplyConfigError(t *testing.T) {
	w := newWorld(t)
	s := w.startup()
	s.env.Config = func() (*config.Config, error) { return nil, errors.New("syntax error") }
	if _, err := s.Apply(context.Background()); err == nil || !strings.Contains(err.Error(), "syntax error") {
		t.Errorf("%v", err)
	}
}

// ---- SetOption

func TestSetOption(t *testing.T) { // tests/test_gui.py and the CLI smoke test
	w := newWorld(t)
	w.savedLooks["smokelook"] = &looks.Look{}
	s := w.startup()
	for _, tc := range []struct{ target, value, want string }{
		{"keyboard", "default", "default"}, {"keyboard", "last", "last"}, {"keyboard", "none", "none"}, {"keyboard", "off", "off"},
		{"keyboard", "profile:mine", "profile:mine"}, {"keyboard", "look:smokelook", "look:smokelook"},
		{"rear", "off", "off"}, {"rear", "look:smokelook", "look:smokelook"}, {"rear", "default", "default"},
	} {
		got, err := s.SetOption(tc.target, tc.value)
		if err != nil || got != tc.want {
			t.Errorf("%s %s: %q, %v", tc.target, tc.value, got, err)
		}
		cfg, _ := w.startup().env.Config()
		field := map[string]string{"keyboard": cfg.Startup.Keyboard, "rear": cfg.Startup.Rear}[tc.target]
		if field != tc.want {
			t.Errorf("%s %s stored as %q", tc.target, tc.value, field)
		}
	}
	if got, err := s.SetOption("profile", "gaming"); err != nil || got != "gaming" {
		t.Errorf("%q, %v", got, err)
	}
	if cfg, _ := s.env.Config(); cfg.Startup.Profile != "gaming" {
		t.Errorf("%+v", cfg.Startup)
	}
	if got, err := s.SetOption("profile", "none"); err != nil || got != "" {
		t.Errorf("none clears the profile: %q, %v", got, err)
	}
	if cfg, _ := s.env.Config(); cfg.Startup.Profile != "" {
		t.Errorf("%+v", cfg.Startup)
	}
}

func TestSetOptionRefusals(t *testing.T) {
	w := newWorld(t)
	w.savedLooks["smokelook"] = &looks.Look{}
	s := w.startup()
	for _, tc := range []struct{ target, value, want string }{
		{"colour", "red", "target: keyboard | rear | profile"},
		{"profile", "ghost", "no profile 'ghost': gaming, quiet"},
		{"keyboard", "profile:ghost", "no lighting profile 'ghost': mine"},
		{"keyboard", "look:nosuchlook", "no look 'nosuchlook': "},
		{"rear", "look:nosuchlook", "no look 'nosuchlook': "},
		{"keyboard", "bogus", "[startup] keyboard must be"},
		{"rear", "profile:x", "[startup] rear must be"},
		{"rear", "profile:mine", "[startup] rear must be"},
		{"keyboard", "", "[startup] keyboard must be"},
		{"profile", "look:smokelook", "no profile 'look:smokelook'"},
	} {
		_, err := s.SetOption(tc.target, tc.value)
		if err == nil || !strings.Contains(err.Error(), tc.want) {
			t.Errorf("%s %q: %v, want %q", tc.target, tc.value, err, tc.want)
		}
	}
	if cfg, _ := s.env.Config(); cfg.Startup.Keyboard != "last" || cfg.Startup.Rear != "last" {
		t.Errorf("a refused option must write nothing: %+v", cfg.Startup)
	}
}

func TestSetOptionKeepsOtherConfig(t *testing.T) {
	w := newWorld(t)
	writeCfg(t, w, "[audio]\nnotify = false\n[startup]\nrear = \"off\"\n")
	if _, err := w.startup().SetOption("keyboard", "off"); err != nil {
		t.Fatal(err)
	}
	cfg, _ := config.Load("", w.cfgPath)
	if cfg.Audio.Notify || cfg.Startup.Rear != "off" || cfg.Startup.Keyboard != "off" {
		t.Errorf("%+v %+v", cfg.Audio, cfg.Startup)
	}
}

func TestPyRepr(t *testing.T) {
	for in, want := range map[string]string{"x": "'x'", "it's": `"it's"`, `a"b`: `'a"b'`, `a'"b`: `'a\'"b'`, "": "''"} {
		if got := pyRepr(in); got != want {
			t.Errorf("%q: %s, want %s", in, got, want)
		}
	}
}

func FuzzSetOption(f *testing.F) {
	for _, s := range []string{"keyboard\x00last", "rear\x00off", "profile\x00none", "keyboard\x00profile:mine", "rear\x00look:x", "x\x00y", "keyboard\x00look:", "profile\x00"} {
		f.Add(s)
	}
	dir := f.TempDir()
	f.Fuzz(func(t *testing.T, in string) {
		target, value, _ := strings.Cut(in, "\x00")
		w := &world{profiles: []string{"gaming"}, keyProfs: map[string]bool{"mine": true}, savedLooks: map[string]*looks.Look{"x": {}}, fail: map[string]int{}, cfgPath: filepath.Join(dir, "c.toml")}
		s := w.startup()
		got, err := s.SetOption(target, value)
		cfg, cerr := s.env.Config()
		if cerr != nil {
			t.Fatalf("config unreadable after SetOption(%q, %q): %v", target, value, cerr)
		}
		if p := cfg.Validate(); err == nil && len(filterStartup(p)) > 0 {
			t.Fatalf("accepted %q %q but the config is invalid: %v", target, value, p)
		}
		_ = got
	})
}

func filterStartup(in []string) []string {
	var out []string
	for _, p := range in {
		if strings.Contains(p, "[startup]") {
			out = append(out, p)
		}
	}
	return out
}
