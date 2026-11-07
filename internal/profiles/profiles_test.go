package profiles

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"testing"

	toml "github.com/pelletier/go-toml/v2"

	"github.com/Order52/truesense/internal/audio"
	"github.com/Order52/truesense/internal/client"
	"github.com/Order52/truesense/internal/curves"
	"github.com/Order52/truesense/internal/hw"
	"github.com/Order52/truesense/internal/ipc"
)

const shipped = "../../packaging/profiles"

// world is a fake machine; every effect is appended to log.
type world struct {
	log []string

	mode      string
	fan       hw.FanState
	rear      hw.Rear
	battery   bool
	usb       int
	overdrive bool
	boot      bool
	kbTimeout bool
	kbBright  int
	output    string
	voice     map[string]string
	audioDown bool

	daemonUp  bool
	settings  map[string]any
	curveName string
	state     map[string]any
	curves    map[string]curves.Curve

	failOn string
}

func newWorld() *world {
	return &world{mode: "balanced", fan: hw.FanState{Mode: "auto"}, rear: hw.Rear{Mode: "static", Color: "112233", Speed: 3, Brightness: 100, Direction: 1},
		usb: 10, daemonUp: true, settings: map[string]any{"smart_fan": false, "auto_mode": "off"}, curveName: "native", state: map[string]any{},
		output: "speakers", voice: map[string]string{"headset": "off", "array": "conference"}, curves: map[string]curves.Curve{}, kbBright: 25}
}

func (w *world) rec(format string, a ...any) error {
	line := fmt.Sprintf(format, a...)
	w.log = append(w.log, line)
	if w.failOn != "" && strings.HasPrefix(line, w.failOn) {
		return errors.New("boom")
	}
	return nil
}

func (w *world) store(t *testing.T) *Store {
	t.Helper()
	return &Store{
		Files: Files{UserDir: filepath.Join(t.TempDir(), "user"), SysDirs: []string{shipped}},
		Env:   Env{Actions: (*fActions)(w), Fans: (*fFan)(w), Daemon: (*fDaemon)(w), HW: (*fHW)(w), Keyboard: (*fKB)(w), Audio: (*fAudio)(w), Curves: (*fCurves)(w), State: (*fState)(w)},
	}
}

type fActions world

func (a *fActions) Run(_ context.Context, spec string) (string, error) {
	return "", (*world)(a).rec("action %s", spec)
}

type fFan world

func (f *fFan) Fan(r client.FanRequest) error {
	if r.Mode == "manual" {
		return (*world)(f).rec("fan manual %d %d", *r.CPU, *r.GPU)
	}
	return (*world)(f).rec("fan %s", r.Mode)
}

type fDaemon world

func (d *fDaemon) CallInto(cmd string, args ipc.Args, out any) error {
	w := (*world)(d)
	if !d.daemonUp {
		return ipc.ErrNotRunning
	}
	switch cmd {
	case "state":
		*(out.(*struct {
			Settings map[string]any `json:"settings"`
		})) = struct {
			Settings map[string]any `json:"settings"`
		}{d.settings}
		return nil
	case "fan_curve":
		if args["action"] == "get" {
			*(out.(*struct {
				Name string `json:"name"`
			})) = struct {
				Name string `json:"name"`
			}{d.curveName}
			return nil
		}
		if c, ok := args["data"].(curves.Curve); ok {
			return w.rec("daemon fan_curve use %v data=%d points", args["name"], len(c.CPU))
		}
		return w.rec("daemon fan_curve use %v data=%v", args["name"], args["data"])
	case "set":
		return w.rec("daemon set %v=%v", args["key"], args["value"])
	}
	return fmt.Errorf("unexpected %s", cmd)
}

type fHW world

func (h *fHW) GetMode() (string, error)          { return h.mode, nil }
func (h *fHW) GetFanState() (hw.FanState, error) { return h.fan, nil }
func (h *fHW) RearGet() (hw.Rear, error)         { return h.rear, nil }
func (h *fHW) RearSet(o hw.RearOpts) (hw.Rear, error) {
	return hw.Rear{}, (*world)(h).rec("rear %s %s s=%d b=%d d=%d verify=%v", o.Mode, o.Color, o.Speed, o.Brightness, o.Direction, o.Verify)
}
func (h *fHW) GetBatteryLimit() (bool, error) { return h.battery, nil }
func (h *fHW) SetBatteryLimit(on bool) (bool, error) {
	return on, (*world)(h).rec("battery %v", on)
}
func (h *fHW) GetUSBCharge() (int, error) { return h.usb, nil }
func (h *fHW) SetUSBCharge(l int) (int, error) {
	return l, (*world)(h).rec("usb %d", l)
}
func (h *fHW) GetOverdrive() (bool, bool, error) { return h.overdrive, true, nil }
func (h *fHW) SetOverdrive(on bool) (bool, error) {
	return on, (*world)(h).rec("overdrive %v", on)
}
func (h *fHW) GetBootSound() (bool, error) { return h.boot, nil }
func (h *fHW) SetBootSound(on bool) (bool, error) {
	return on, (*world)(h).rec("boot %v", on)
}
func (h *fHW) GetKBTimeout() (bool, error) { return h.kbTimeout, nil }
func (h *fHW) SetKBTimeout(on bool) (bool, error) {
	return on, (*world)(h).rec("kbtimeout %v", on)
}

type fKB world

func (k *fKB) Brightness() (int, error) { return k.kbBright, nil }
func (k *fKB) SetEffect(name, color string, speed, brightness, direction int) error {
	return (*world)(k).rec("kb effect %s %s s=%d b=%d d=%d", name, color, speed, brightness, direction)
}

type fAudio world

func (a *fAudio) Output(context.Context) (audio.OutputStatus, error) {
	if a.audioDown {
		return audio.OutputStatus{}, errors.New("no sound server")
	}
	return audio.OutputStatus{Active: a.output}, nil
}
func (a *fAudio) VoiceModes() map[string]string { return a.voice }
func (a *fAudio) SetOutput(_ context.Context, w string) (string, error) {
	return w, (*world)(a).rec("audio output %s", w)
}
func (a *fAudio) VoiceSet(_ context.Context, dev, mode string) (string, error) {
	return mode, (*world)(a).rec("audio voice %s %s", dev, mode)
}
func (a *fAudio) SetMicMute(_ context.Context, m bool) error {
	return (*world)(a).rec("audio mic %v", m)
}
func (a *fAudio) DTSMode(_ context.Context, name string, scope audio.DTSScope) (string, error) {
	return name, (*world)(a).rec("audio dts %s scope=%d", name, scope)
}

type fCurves world

func (c *fCurves) Load(name string) (curves.Curve, error) {
	cv, ok := c.curves[name]
	if !ok {
		return curves.Curve{}, fmt.Errorf("no curve %q", name)
	}
	return cv, nil
}

type fState world

func (s *fState) Get(k string) any { return s.state[k] }
func (s *fState) Set(k string, v any) error {
	s.state[k] = v
	return nil
}

func writeProfile(t *testing.T, s *Store, name, body string) {
	t.Helper()
	if err := os.MkdirAll(s.UserDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(s.UserDir, name+".toml"), []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

// ---- files

func TestShippedProfilesLoadAndApply(t *testing.T) { // tests/test_core.py::test_shipped_profiles_load_and_apply
	w := newWorld()
	s := w.store(t)
	for _, n := range []string{"gaming", "quiet", "balanced", "battery-saver"} {
		if !slices.Contains(s.Names(), n) {
			t.Errorf("shipped profile %s not listed in %v", n, s.Names())
		}
		if s.Source(n) != "shipped" {
			t.Errorf("%s source %q", n, s.Source(n))
		}
	}
	msg, err := s.Apply(context.Background(), "battery-saver")
	if err != nil || !strings.Contains(msg, "applied") || strings.Contains(msg, "FAILED") {
		t.Fatalf("%q, %v", msg, err)
	}
	want := []string{"action power:eco", "action fan:auto", "kbtimeout true", "action rear:off", "battery true", "overdrive false"}
	if !reflect.DeepEqual(w.log, want) {
		t.Errorf("calls\n got %q\nwant %q", w.log, want)
	}
	if msg != "Profile 'battery-saver' applied (6 settings)" {
		t.Errorf("%q", msg)
	}
	if w.state["profile"] != "battery-saver" {
		t.Errorf("the applied profile is remembered: %v", w.state)
	}
}

func TestEveryShippedProfileAppliesCleanly(t *testing.T) {
	for _, n := range (&Files{SysDirs: []string{shipped}}).Names() {
		w := newWorld()
		msg, err := w.store(t).Apply(context.Background(), n)
		if err != nil || strings.Contains(msg, "FAILED") {
			t.Errorf("%s: %q, %v", n, msg, err)
		}
	}
}

func TestNamesCannotEscape(t *testing.T) {
	s := newWorld().store(t)
	for _, bad := range []string{"../etc/passwd", "a b", "", "x/y", "a.b", "..", "a\x00"} {
		if _, err := s.Load(bad); err == nil || err.Error() != "profile names: letters, digits, - and _" {
			t.Errorf("Load(%q): %v", bad, err)
		}
		if _, err := s.Save(bad, Profile{}); err == nil {
			t.Errorf("Save(%q) must fail", bad)
		}
		if err := s.Delete(bad); err == nil {
			t.Errorf("Delete(%q) must fail", bad)
		}
	}
	for _, ok := range []string{"gaming", "my-set_2", "Ünï"} {
		if !ValidName(ok) {
			t.Errorf("%q", ok)
		}
	}
}

func TestUserProfilesWinListSourceDelete(t *testing.T) {
	s := newWorld().store(t)
	writeProfile(t, s, "gaming", "description = \"mine\"\n[power]\nmode = \"eco\"\n")
	writeProfile(t, s, "extra", "description = \"x\"\n")
	if got := s.Source("gaming"); got != "yours" {
		t.Errorf("source %q", got)
	}
	d, err := s.Load("gaming")
	if err != nil || d["description"] != "mine" {
		t.Fatalf("%v, %v", d, err)
	}
	names := s.Names()
	if !slices.IsSorted(names) || len(slices.Compact(slices.Clone(names))) != len(names) || !slices.Contains(names, "extra") || !slices.Contains(names, "quiet") {
		t.Errorf("%v", names)
	}
	if s.Source("nothere") != "" || s.Source("a/b") != "" {
		t.Error("unknown profiles have no source")
	}
	if err := s.Delete("quiet"); err == nil || !strings.Contains(err.Error(), "shipped profile") {
		t.Errorf("%v", err)
	}
	if err := s.Delete("gaming"); err != nil {
		t.Fatal(err)
	}
	if got := s.Source("gaming"); got != "shipped" {
		t.Errorf("deleting yours reveals the shipped one: %q", got)
	}
	if err := s.Delete("nothere"); err == nil || err.Error() != "no profile 'nothere'" {
		t.Errorf("%v", err)
	}
	if _, err := s.Load("nothere"); err == nil || err.Error() != "no profile 'nothere' (truesense profile list)" {
		t.Errorf("%v", err)
	}
}

func TestLoadReportsBadTOMLWithItsFile(t *testing.T) {
	s := newWorld().store(t)
	writeProfile(t, s, "bad", "[power\nmode = ")
	_, err := s.Load("bad")
	if err == nil || !strings.Contains(err.Error(), filepath.Join(s.UserDir, "bad.toml")) {
		t.Errorf("%v", err)
	}
}

func TestSaveRoundTrip(t *testing.T) {
	s := newWorld().store(t)
	in := Profile{"description": "mine", "power": map[string]any{"mode": "turbo", "auto": "smart"}, "fan": map[string]any{"mode": "manual", "cpu": int64(60), "gpu": int64(70)},
		"rear": map[string]any{"on": true, "color": "ff0000"}, "battery": map[string]any{"limit": false, "usb_charge": int64(30)}, "empty": map[string]any{}}
	p, err := s.Save("mine", in)
	if err != nil || p != filepath.Join(s.UserDir, "mine.toml") {
		t.Fatalf("%q, %v", p, err)
	}
	got, err := s.Load("mine")
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(got, in) {
		t.Errorf("round trip:\n got %#v\nwant %#v", got, in)
	}
	raw, _ := os.ReadFile(p)
	if !strings.HasPrefix(string(raw), "description = ") {
		t.Errorf("scalars come first, as before 0.3.x readers expect:\n%s", raw)
	}
	if s.Source("mine") != "yours" {
		t.Error("source")
	}
}

func TestParse(t *testing.T) {
	d, err := Parse([]byte(""), "x")
	if err != nil || d == nil || len(d) != 0 {
		t.Errorf("%v, %v", d, err)
	}
	if _, err := Parse([]byte("a = "), "x.toml"); err == nil || !strings.HasPrefix(err.Error(), "x.toml: ") {
		t.Errorf("%v", err)
	}
}

func TestDefaultSysDirs(t *testing.T) {
	if got := DefaultSysDirs(func(string) string { return "/tmp/p" }); !reflect.DeepEqual(got, []string{"/tmp/p"}) {
		t.Errorf("%v", got)
	}
	if got := DefaultSysDirs(func(string) string { return "" }); !reflect.DeepEqual(got, []string{"/usr/local/share/truesense/profiles", "/usr/share/truesense/profiles"}) {
		t.Errorf("%v", got)
	}
}

// ---- capture

func TestCaptureRoundTripThroughTOML(t *testing.T) { // tests/test_core.py::test_capture_roundtrip
	w := newWorld()
	w.mode = "turbo"
	w.battery, w.usb, w.boot, w.kbTimeout, w.overdrive = true, 30, true, true, true
	w.fan = hw.FanState{Mode: "manual", Duty: hw.Duty{CPU: 55, GPU: 65}}
	w.settings = map[string]any{"smart_fan": false, "auto_mode": "native"}
	w.curveName = "truesense"
	w.output = "headphones"
	s := w.store(t)
	cap, err := s.Capture(context.Background(), "t")
	if err != nil {
		t.Fatal(err)
	}
	if table(cap, "power")["mode"] != "turbo" {
		t.Errorf("%v", cap)
	}
	b, err := toml.Marshal(map[string]any(cap))
	if err != nil {
		t.Fatal(err)
	}
	var back Profile
	if err := toml.Unmarshal(b, &back); err != nil {
		t.Fatal(err)
	}
	want := Profile{
		"description": "t",
		"power":       map[string]any{"mode": "turbo", "auto": "native"},
		"fan":         map[string]any{"mode": "manual", "curve": "truesense", "cpu": int64(55), "gpu": int64(65)},
		"rear":        map[string]any{"on": true, "effect": "static", "color": "112233", "speed": int64(3), "brightness": int64(100)},
		"battery":     map[string]any{"limit": true, "usb_charge": int64(30)},
		"display":     map[string]any{"overdrive": true},
		"boot":        map[string]any{"sound": true},
		"keyboard":    map[string]any{"timeout": true},
		"audio":       map[string]any{"output": "headphones", "voice_headset": "off", "voice_array": "conference"},
	}
	if !reflect.DeepEqual(back, want) {
		t.Errorf("\n got %#v\nwant %#v", back, want)
	}
}

func TestCaptureSmartFanAndMissingPieces(t *testing.T) {
	w := newWorld()
	w.settings["smart_fan"] = true
	w.rear.Brightness = 0
	w.overdrive = false
	w.audioDown = true
	cap, err := w.store(t).Capture(context.Background(), "")
	if err != nil {
		t.Fatal(err)
	}
	if cap["description"] != "captured" || table(cap, "fan")["mode"] != "smart" {
		t.Errorf("%v", cap)
	}
	if r := table(cap, "rear"); !reflect.DeepEqual(r, map[string]any{"on": false}) {
		t.Errorf("a dark bar records only that: %v", r)
	}
	if _, ok := cap["audio"]; ok {
		t.Error("no sound server: no audio section")
	}
	w.daemonUp = false
	w.rear = hw.Rear{Mode: "weird", Brightness: 50}
	cap, err = w.store(t).Capture(context.Background(), "")
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := table(cap, "power")["auto"]; ok {
		t.Error("no daemon: no auto")
	}
	if _, ok := table(cap, "fan")["curve"]; ok {
		t.Error("no daemon: no curve")
	}
	if table(cap, "fan")["mode"] != "auto" {
		t.Errorf("without the daemon the firmware's own fan mode is recorded: %v", cap["fan"])
	}
	if r := table(cap, "rear"); !reflect.DeepEqual(r, map[string]any{"on": true}) {
		t.Errorf("an effect we do not know is not recorded: %v", r)
	}
}

// ---- apply

func applyNow(t *testing.T, w *world, body string) string {
	t.Helper()
	s := w.store(t)
	writeProfile(t, s, "p", body)
	msg, err := s.Apply(context.Background(), "p")
	if err != nil {
		t.Fatal(err)
	}
	return msg
}

func TestApplyTouchesOnlyWhatIsListed(t *testing.T) {
	for name, tc := range map[string]struct {
		body string
		log  []string
	}{
		"empty":              {"description = \"nothing\"\n", nil},
		"only power":         {"[power]\nmode = \"eco\"\n", []string{"action power:eco"}},
		"only battery limit": {"[battery]\nlimit = false\n", []string{"battery false"}},
		"only usb":           {"[battery]\nusb_charge = 20\n", []string{"usb 20"}},
		"only boot":          {"[boot]\nsound = true\n", []string{"boot true"}},
		"only mic":           {"[audio]\nmic_muted = true\n", []string{"audio mic true"}},
		"only fan auto":      {"[fan]\nmode = \"auto\"\n", []string{"action fan:auto"}},
		"unknown sections and keys are left alone": {"[gpu]\nmode = \"nvidia\"\n[undervolt]\nx = 1\n[power]\nbogus = 1\n", nil},
		"a section with nothing we know":           {"[keyboard]\ncolor = \"ff0000\"\n[rear]\nspeed = 3\n", nil},
	} {
		w := newWorld()
		msg := applyNow(t, w, tc.body)
		if !reflect.DeepEqual(w.log, tc.log) {
			t.Errorf("%s: calls %q, want %q", name, w.log, tc.log)
		}
		if !strings.Contains(msg, fmt.Sprintf("(%d settings)", len(tc.log))) {
			t.Errorf("%s: %q", name, msg)
		}
	}
}

func TestApplyEverythingInOrder(t *testing.T) {
	w := newWorld()
	w.curves["mycurve"] = curves.Curve{CPU: []curves.Point{{40, 30}, {80, 100}}}
	msg := applyNow(t, w, `
description = "all"
[power]
mode = "turbo"
auto = "smart"
[fan]
mode = "manual"
cpu = 60
gpu = 70
curve = "mycurve"
[keyboard]
effect = "wave"
color = "ff00ff"
speed = 7
brightness = 40
timeout = false
[rear]
effect = "breathing"
color = "ff0000"
speed = 2
brightness = 80
direction = 0
[battery]
limit = true
usb_charge = 30
[display]
overdrive = true
[boot]
sound = false
[audio]
output = "headphones"
voice_headset = "conference"
voice_array = "personal"
mic_muted = false
dts_mode = "Movie"
`)
	want := []string{
		"daemon set auto_mode=smart", "action power:turbo", "daemon fan_curve use mycurve data=2 points", "fan manual 60 70",
		"kb effect wave ff00ff s=7 b=40 d=0", "kbtimeout false",
		"rear breathing ff0000 s=2 b=80 d=0 verify=true", "battery true", "usb 30", "overdrive true", "boot false",
		"audio output headphones", "audio voice headset conference", "audio voice array personal", "audio mic false", "audio dts Movie scope=0",
	}
	if !reflect.DeepEqual(w.log, want) {
		t.Errorf("calls\n got %q\nwant %q", w.log, want)
	}
	if msg != "Profile 'p' applied (16 settings)" {
		t.Errorf("%q", msg)
	}
}

func TestApplyFanCurveBuiltinGoesByNameOnly(t *testing.T) {
	w := newWorld()
	applyNow(t, w, "[fan]\ncurve = \"truesense\"\n")
	if want := []string{"daemon fan_curve use truesense data=<nil>"}; !reflect.DeepEqual(w.log, want) {
		t.Errorf("%q", w.log)
	}
}

func TestApplyKeyboardChoiceOrder(t *testing.T) {
	for name, tc := range map[string]struct {
		body string
		log  []string
	}{
		"a lighting profile wins over an effect": {"[keyboard]\nprofile = \"mine\"\neffect = \"wave\"\non = false\n", []string{"action keyboard-profile:mine"}},
		"an effect wins over on":                 {"[keyboard]\neffect = \"wave\"\non = false\n", []string{"kb effect wave 00aec7 s=5 b=25 d=0"}},
		"on":                                     {"[keyboard]\non = true\n", []string{"action keyboard:on"}},
		"off":                                    {"[keyboard]\non = false\n", []string{"action keyboard:off"}},
		"timeout beside a profile":               {"[keyboard]\nprofile = \"x\"\ntimeout = true\n", []string{"action keyboard-profile:x", "kbtimeout true"}},
	} {
		w := newWorld()
		applyNow(t, w, tc.body)
		if !reflect.DeepEqual(w.log, tc.log) {
			t.Errorf("%s: %q, want %q", name, w.log, tc.log)
		}
	}
	w := newWorld()
	w.kbBright = 0
	applyNow(t, w, "[keyboard]\neffect = \"snow\"\n")
	if want := []string{"kb effect snow 00aec7 s=5 b=30 d=0"}; !reflect.DeepEqual(w.log, want) {
		t.Errorf("a dark keyboard gets 30: %q", w.log)
	}
}

func TestApplyRearChoiceAndDefaults(t *testing.T) {
	w := newWorld()
	applyNow(t, w, "[rear]\neffect = \"neon\"\non = false\n")
	if want := []string{"rear neon ffffff s=3 b=100 d=1 verify=true"}; !reflect.DeepEqual(w.log, want) {
		t.Errorf("%q", w.log)
	}
	w = newWorld()
	applyNow(t, w, "[rear]\non = true\n")
	if want := []string{"action rear:on"}; !reflect.DeepEqual(w.log, want) {
		t.Errorf("%q", w.log)
	}
}

func TestApplyReportsFailuresAndContinues(t *testing.T) {
	w := newWorld()
	w.failOn = "battery"
	msg := applyNow(t, w, "[power]\nmode = \"eco\"\n[battery]\nlimit = true\nusb_charge = 10\n[display]\noverdrive = true\n")
	if msg != "Profile 'p' applied (3 settings, 1 FAILED: battery limit: boom)" {
		t.Errorf("%q", msg)
	}
	if w.state["profile"] != "p" {
		t.Error("a partly applied profile is still the current one")
	}
	w = newWorld()
	w.failOn = "action"
	msg = applyNow(t, w, "[power]\nmode = \"eco\"\n[fan]\nmode = \"auto\"\n")
	if msg != "Profile 'p' applied (0 settings, 2 FAILED: power: boom; fan: boom)" {
		t.Errorf("%q", msg)
	}
}

func TestApplyBadValuesFailTheirSettingOnly(t *testing.T) {
	w := newWorld()
	msg := applyNow(t, w, "[fan]\nmode = \"manual\"\ncpu = \"abc\"\ngpu = 60\n[battery]\nusb_charge = [1]\nlimit = true\n[rear]\neffect = \"wave\"\ncolor = 5\n[keyboard]\neffect = \"wave\"\nspeed = \"fast\"\n")
	for _, want := range []string{"fan: \"abc\" is not a number", "usb charge: ", "rear light: color must be text", "keyboard effect: \"fast\" is not a number", "4 FAILED"} {
		if !strings.Contains(msg, want) {
			t.Errorf("%q missing %q", msg, want)
		}
	}
	if !reflect.DeepEqual(w.log, []string{"battery true"}) {
		t.Errorf("%q", w.log)
	}
	w = newWorld()
	msg = applyNow(t, w, "[fan]\nmode = \"manual\"\ncpu = 60\n")
	if !strings.Contains(msg, "fan: manual fans need cpu and gpu") {
		t.Errorf("%q", msg)
	}
}

func TestApplyManualFanZeroIsNotSpecial(t *testing.T) {
	// a profile may ask for 0; the fan service refuses it without the explicit confirm path, and the failure is reported
	w := newWorld()
	w.failOn = "fan manual 0"
	msg := applyNow(t, w, "[fan]\nmode = \"manual\"\ncpu = 0\ngpu = 0\n")
	if !strings.Contains(msg, "1 FAILED: fan: boom") {
		t.Errorf("%q", msg)
	}
	if w.log[0] != "fan manual 0 0" {
		t.Errorf("%q", w.log)
	}
}

func TestApplyUnknownProfile(t *testing.T) {
	if _, err := newWorld().store(t).Apply(context.Background(), "nothere"); err == nil || !strings.Contains(err.Error(), "no profile 'nothere'") {
		t.Errorf("%v", err)
	}
}

func TestApplyWithoutDaemonReportsTheDaemonSettings(t *testing.T) {
	w := newWorld()
	w.daemonUp = false
	msg := applyNow(t, w, "[power]\nauto = \"smart\"\nmode = \"eco\"\n[fan]\ncurve = \"truesense\"\n")
	if !strings.Contains(msg, "2 FAILED: power auto: ") || !strings.Contains(msg, "fan curve: ") || !strings.Contains(msg, "(1 settings") {
		t.Errorf("%q", msg)
	}
}

func TestNextName(t *testing.T) {
	w := newWorld()
	s := w.store(t)
	names := s.Names()
	if got, _ := s.NextName(); got != names[0] {
		t.Errorf("first: %q", got)
	}
	w.state["profile"] = names[0]
	if got, _ := s.NextName(); got != names[1] {
		t.Errorf("next: %q", got)
	}
	w.state["profile"] = names[len(names)-1]
	if got, _ := s.NextName(); got != names[0] {
		t.Errorf("wraps: %q", got)
	}
	w.state["profile"] = "vanished"
	if got, _ := s.NextName(); got != names[0] {
		t.Errorf("unknown current: %q", got)
	}
	empty := &Store{Files: Files{UserDir: t.TempDir()}, Env: s.Env}
	if _, err := empty.NextName(); err == nil || err.Error() != "no profiles saved" {
		t.Errorf("%v", err)
	}
}

// ---- describe

func TestDescribe(t *testing.T) {
	load := func(n string) Profile {
		d, err := (&Files{SysDirs: []string{shipped}}).Load(n)
		if err != nil {
			t.Fatal(err)
		}
		return d
	}
	lines := Describe(load("battery-saver")) // tests/test_gui.py
	joined := strings.Join(lines, "\n")
	for _, want := range []string{"Eco", "80 % limit on", "Rear light: off"} {
		if !strings.Contains(joined, want) {
			t.Errorf("missing %q in\n%s", want, joined)
		}
	}
	if got := Describe(Profile{}); !reflect.DeepEqual(got, []string{"(nothing)"}) {
		t.Errorf("%v", got)
	}
	for name, tc := range map[string]struct {
		in   string
		want []string
	}{
		"power":                           {"[power]\nmode = \"TURBO\"\nauto = \"smart\"\n", []string{"Power: Turbo mode (held for a while), mode control smart"}},
		"power auto only":                 {"[power]\nauto = \"off\"\n", []string{"Power: mode control off"}},
		"fan smart":                       {"[fan]\nmode = \"smart\"\ncurve = \"truesense\"\n", []string{"Fans: smart curve, curve truesense"}},
		"fan auto":                        {"[fan]\nmode = \"auto\"\n", []string{"Fans: firmware Auto"}},
		"fan max":                         {"[fan]\nmode = \"max\"\n", []string{"Fans: maximum"}},
		"fan manual":                      {"[fan]\nmode = \"manual\"\ncpu = 60\ngpu = 70\n", []string{"Fans: manual 60 % / 70 %"}},
		"fan manual unknown duty":         {"[fan]\nmode = \"manual\"\n", []string{"Fans: manual ? % / ? %"}},
		"fan curve only":                  {"[fan]\ncurve = \"x\"\n", []string{"Fans: curve x"}},
		"keyboard":                        {"[keyboard]\neffect = \"wave\"\nprofile = \"mine\"\non = false\ntimeout = true\n", []string{"Keyboard: wave effect, key set mine, backlight off, light off after 30 s on"}},
		"keyboard with only unknown keys": {"[keyboard]\ncolor = \"x\"\n", []string{"Keyboard: "}},
		"rear effect":                     {"[rear]\neffect = \"breathing\"\ncolor = \"ff0000\"\n", []string{"Rear light: breathing ff0000"}},
		"rear effect without colour":      {"[rear]\neffect = \"wave\"\n", []string{"Rear light: wave"}},
		"rear on":                         {"[rear]\non = true\n", []string{"Rear light: on"}},
		"rear off":                        {"[rear]\non = false\n", []string{"Rear light: off"}},
		"battery":                         {"[battery]\nlimit = false\nusb_charge = 30\n", []string{"Battery: 80 % limit off, USB charging while off 30"}},
		"screen":                          {"[display]\noverdrive = true\n", []string{"Screen: overdrive on"}},
		"boot":                            {"[boot]\nsound = false\n", []string{"Boot sound off"}},
		"audio":                           {"[audio]\noutput = \"speakers\"\nvoice_headset = \"conference\"\nvoice_array = \"off\"\n", []string{"Audio: speakers, headset noise reduction conference, built-in noise reduction off"}},
		"audio with only a mic":           {"[audio]\nmic_muted = true\n", []string{"Audio: "}},
		"unknown sections say nothing":    {"[gpu]\nmode = \"x\"\n", []string{"(nothing)"}},
		"empty sections say nothing":      {"[power]\n[display]\n", []string{"(nothing)"}},
	} {
		d, err := Parse([]byte(tc.in), name)
		if err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		if got := Describe(d); !reflect.DeepEqual(got, tc.want) {
			t.Errorf("%s:\n got %q\nwant %q", name, got, tc.want)
		}
	}
}

// ---- fuzz

func FuzzParseDescribeSave(f *testing.F) {
	for _, s := range []string{"", "description = \"x\"\n[power]\nmode = \"eco\"\n", "[fan]\nmode = 5\n", "[a.b.c]\nd = [1, 2]\n", "x = 1979-05-27T07:32:00Z", "[power", "a = 'b", "[rear]\neffect = {a = 1}\n"} {
		f.Add([]byte(s))
	}
	dir := f.TempDir()
	s := &Files{UserDir: dir}
	f.Fuzz(func(t *testing.T, data []byte) {
		d, err := Parse(data, "fuzz")
		if err != nil {
			return
		}
		_ = Describe(d) // never panics, whatever the types
		if _, err := s.Save("fuzz", d); err != nil {
			return // something TOML cannot encode back is an error, not a crash
		}
		back, err := s.Load("fuzz")
		if err != nil {
			t.Fatalf("a saved profile must load: %v", err)
		}
		if len(back) != len(d) {
			t.Fatalf("round trip lost keys: %v vs %v", back, d)
		}
	})
}

func FuzzApplyNeverPanics(f *testing.F) {
	for _, s := range []string{"[power]\nmode = 5\n", "[fan]\nmode = \"manual\"\ncpu = 1.5e300\ngpu = true\n", "[keyboard]\non = \"yes\"\neffect = 3\n", "[rear]\neffect = []\n", "[audio]\noutput = {}\n", "[battery]\nusb_charge = \"x\"\n"} {
		f.Add([]byte(s))
	}
	dir := f.TempDir()
	f.Fuzz(func(t *testing.T, data []byte) {
		if err := os.WriteFile(filepath.Join(dir, "p.toml"), data, 0o644); err != nil {
			t.Fatal(err)
		}
		w := newWorld()
		s := &Store{Files: Files{UserDir: dir}, Env: Env{Actions: (*fActions)(w), Fans: (*fFan)(w), Daemon: (*fDaemon)(w), HW: (*fHW)(w), Keyboard: (*fKB)(w), Audio: (*fAudio)(w), Curves: (*fCurves)(w), State: (*fState)(w)}}
		if msg, err := s.Apply(context.Background(), "p"); err == nil && !strings.HasPrefix(msg, "Profile 'p' applied (") {
			t.Fatalf("%q", msg)
		}
	})
}
