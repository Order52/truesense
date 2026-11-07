package adapt_test

import (
	"strings"
	"testing"
	"time"

	"github.com/Order52/truesense/internal/adapt"
	"github.com/Order52/truesense/internal/api"
	"github.com/Order52/truesense/internal/apitest"
	"github.com/Order52/truesense/internal/tui"
)

// screen is the terminal UI on a text grid, running over the real adapter, the real API and the
// firmware simulator: a key press goes all the way to the simulated laptop and back.
type screen struct {
	t     *testing.T
	f     *apitest.Fixture
	a     *api.API
	app   *tui.App
	cv    *tui.GridCanvas
	clock time.Time
}

func newScreen(t *testing.T) *screen {
	t.Helper()
	f := apitest.New(t)
	s := &screen{t: t, f: f, a: f.API(), cv: tui.NewGridCanvas(110, 36, true), clock: time.Date(2026, 11, 7, 12, 0, 0, 0, time.UTC)}
	s.app = tui.NewApp(tui.Config{Canvas: s.cv, Backend: adapt.TUI(s.a, f.S), Clock: func() time.Time { return s.clock }, Sync: true, Version: "test", Context: t.Context()})
	for range 3 {
		s.app.Data().Fast(t.Context())
	}
	s.app.Refresh()
	return s
}

func (s *screen) keys(text string) {
	for _, r := range text {
		s.app.Key(tui.Key(r))
	}
}

func (s *screen) key(k tui.Key) { s.app.Key(k) }

// elapse lets the pause after a change run out, so what was chosen is written now.
func (s *screen) elapse() {
	s.clock = s.clock.Add(time.Second)
	s.app.Tick()
}

func (s *screen) text() string {
	s.app.Draw()
	return s.cv.Text()
}

func (s *screen) lastLines() string {
	lines := strings.Split(s.text(), "\n")
	return strings.Join(lines[len(lines)-4:], "\n")
}

func (s *screen) page(n int) { s.app.Goto(n) }

func TestThePowerModeReachesTheFirmwareAndIsHeld(t *testing.T) {
	s := newScreen(t)
	s.page(2)
	mode := func() string { st, _ := s.a.PowerStatus(); return st.Mode }
	s.keys("l")
	if mode() != "eco" {
		t.Fatal("not written before the pause")
	}
	s.key(tui.KeyEnter)
	if got := mode(); got != "quiet" {
		t.Fatalf("firmware mode %s", got)
	}
	if !strings.Contains(s.text(), "Power mode") {
		t.Fatal(s.text())
	}
}

func TestPowerModeThroughTheServiceCarriesTheHold(t *testing.T) {
	s := newScreen(t)
	d := s.f.StartDaemon()
	s.page(2)
	s.keys("l")
	s.key(tui.KeyEnter)
	c, ok := d.Last("power")
	if !ok || c.Args["mode"] != "quiet" || c.Args["hold_minutes"] != float64(30) {
		t.Fatalf("%+v", c)
	}
}

func TestToggleWritesTheFirmwareAndReadsItBack(t *testing.T) {
	s := newScreen(t)
	s.page(2)
	s.keys("jjjjjjj") // down to the battery limit
	for !strings.Contains(rowOf(s.text(), "▸"), "Battery limit") {
		s.keys("j")
	}
	s.keys(" ")
	on, err := s.a.BatteryLimit()
	if err != nil || !on {
		t.Fatalf("limit %v %v", on, err)
	}
	if !strings.Contains(s.text(), "Battery limit") {
		t.Fatal("message")
	}
}

func rowOf(text, marker string) string {
	for _, l := range strings.Split(text, "\n") {
		if strings.Contains(l, marker) {
			return l
		}
	}
	return ""
}

// The stop of a fan is asked first, and refused without the service: the fans stay as they were.
func TestStoppingAFanIsRefusedWithoutTheService(t *testing.T) {
	s := newScreen(t)
	s.page(1)
	s.keys("j")
	s.keys("hhhhhhhh") // the lowest stop is OFF
	s.key(tui.KeyEnter)
	s.key(tui.KeyEnter) // the question's default answer is No
	before := s.f.Sim.Snapshot()
	if before.Duty[0] == 0 || before.Fan != "auto" {
		t.Fatalf("%+v", before)
	}
	s.keys("hhhhhhhh")
	s.key(tui.KeyEnter)
	s.keys("y")
	after := s.f.Sim.Snapshot()
	if after.Duty != before.Duty || after.Fan != before.Fan {
		t.Fatalf("the fans changed: %+v -> %+v", before, after)
	}
	if !strings.Contains(s.lastLines(), "service") {
		t.Fatalf("the refusal is shown:\n%s", s.lastLines())
	}
}

func TestStoppingBothFansNeedsTheConfirmationAndTheService(t *testing.T) {
	s := newScreen(t)
	b := adapt.TUI(s.a, s.f.S)
	if err := b.SetFan(t.Context(), tui.FanRequest{Mode: "off"}); err == nil || !strings.Contains(err.Error(), "confirmation") {
		t.Fatalf("unconfirmed: %v", err)
	}
	if err := b.SetFan(t.Context(), tui.FanRequest{Mode: "manual", CPU: 0, GPU: 40}); err == nil || !strings.Contains(err.Error(), "confirmation") {
		t.Fatalf("a manual duty of 0 is a stop: %v", err)
	}
	if err := b.SetFan(t.Context(), tui.FanRequest{Mode: "off", Confirm: true}); err == nil || !strings.Contains(err.Error(), "needs the TrueSense service") {
		t.Fatalf("confirmed but no service: %v", err)
	}
	if snap := s.f.Sim.Snapshot(); snap.Fan != "auto" {
		t.Fatalf("%+v", snap)
	}
	if err := b.SetFan(t.Context(), tui.FanRequest{Mode: "manual", CPU: 60, GPU: 40}); err != nil {
		t.Fatal(err)
	}
	if snap := s.f.Sim.Snapshot(); snap.Duty != [2]int{60, 40} {
		t.Fatalf("%+v", snap)
	}
	if err := b.SetFan(t.Context(), tui.FanRequest{Mode: "max"}); err != nil {
		t.Fatal(err)
	}
	if err := b.SetFan(t.Context(), tui.FanRequest{Mode: "warp"}); err == nil {
		t.Fatal("unknown mode")
	}
}

func TestKeyboardEffectAndOffAreRememberedForTheScreen(t *testing.T) {
	s := newScreen(t)
	b := adapt.TUI(s.a, s.f.S)
	last, _ := b.KeyboardLast(t.Context())
	if last.Name != "" {
		t.Fatalf("nothing is remembered yet: %+v", last)
	}
	if err := b.SetKeyboard(t.Context(), tui.KeyboardLook{Name: "breathing", Color: "ff0000", Speed: 5, Brightness: 30}); err != nil {
		t.Fatal(err)
	}
	last, _ = b.KeyboardLast(t.Context())
	if last.Name != "breathing" || last.Color != "ff0000" || last.Brightness != 30 {
		t.Fatalf("%+v", last)
	}
	if err := b.SetKeyboard(t.Context(), tui.KeyboardLook{Name: "off"}); err != nil {
		t.Fatal(err)
	}
	last, _ = b.KeyboardLast(t.Context())
	if last.Name != "off" {
		t.Fatalf("%+v", last)
	}
	if now, _ := b.KeyboardBrightness(t.Context()); now != 0 {
		t.Fatalf("off is brightness 0, not %d", now)
	}
	caps := b.KeyboardCaps()
	if caps.MaxBrightness != 50 || caps.MaxSpeed != 12 || caps.Effects[0] != "off" {
		t.Fatalf("%+v", caps)
	}
}

func TestTheLightingPageSetsTheKeyboard(t *testing.T) {
	s := newScreen(t)
	s.page(3)
	s.keys("l") // the effect: next one after the default
	if last := s.f.S.KB.Last(); last.Name != "" {
		t.Fatalf("not written before the pause: %+v", last)
	}
	s.elapse()
	if last := s.f.S.KB.Last(); last.Name == "" {
		t.Fatalf("the keyboard was not set: %+v", last)
	}
}

func TestRearKeepsTheSavedPartColours(t *testing.T) {
	s := newScreen(t)
	if err := s.f.S.User.Set("rear_zones", []string{"ff0000", "00ff00", "0000ff"}); err != nil {
		t.Fatal(err)
	}
	b := adapt.TUI(s.a, s.f.S)
	look := tui.RearLook{Mode: "static", Color: "00aec7", Speed: 3, Brightness: 60, Direction: 1}
	if err := b.SetRear(t.Context(), look, true); err != nil {
		t.Fatal(err)
	}
	if z := s.f.Sim.Snapshot().Zones; len(z) != 3 {
		t.Fatalf("zones %v", z)
	}
	r, err := b.Rear(t.Context())
	if err != nil || r.Mode != "static" || r.Brightness != 60 {
		t.Fatalf("%+v %v", r, err)
	}
	if err := b.SetRear(t.Context(), tui.RearLook{Mode: "wave", Color: "ffffff", Speed: 3, Brightness: 50, Direction: 1}, true); err != nil {
		t.Fatal(err)
	}
}

func TestSoundWithoutACard(t *testing.T) {
	s := newScreen(t)
	b := adapt.TUI(s.a, s.f.S)
	card, err := b.SoundCard(t.Context())
	if err != nil || card.Status != tui.SoundMissing || card.Problem == "" {
		t.Fatalf("%+v %v", card, err)
	}
	if _, err := b.Output(t.Context()); err == nil {
		t.Fatal("no output without a card")
	}
	m, err := b.Microphone(t.Context())
	if err != nil || m.Volume.OK {
		t.Fatalf("%+v %v", m, err)
	}
	v, _ := b.VoiceModes(t.Context())
	if v.Headset == "" || v.Array == "" {
		t.Fatalf("%+v", v)
	}
	if on, err := b.DTSOn(t.Context()); err == nil || on {
		t.Fatalf("AcerUltraX is not installed: %v %v", on, err)
	}
	s.page(4)
	if !strings.Contains(s.text(), "Sound card") {
		t.Fatal(s.text())
	}
}

func TestLiveReadingsWithAndWithoutTheService(t *testing.T) {
	s := newScreen(t)
	b := adapt.TUI(s.a, s.f.S)
	if st, err := b.ServiceState(t.Context()); st != nil || err != nil {
		t.Fatalf("no service: %+v %v", st, err)
	}
	sn, err := b.Sensors(t.Context())
	if err != nil || sn.CPUTemp != 50 || sn.GPUTemp != 48 {
		t.Fatalf("%+v %v", sn, err)
	}
	fs, _ := b.FanState(t.Context())
	if fs.Mode != tui.FirmwareFanAuto || fs.CPUDuty != 30 {
		t.Fatalf("%+v", fs)
	}
	if m, _ := b.PowerMode(t.Context()); m != "eco" {
		t.Fatal(m)
	}
	if ac, _ := b.OnAC(t.Context()); !ac {
		t.Fatal("no supply says otherwise: on AC")
	}
	if _, ok := b.Battery(); ok {
		t.Fatal("no battery in the test machine")
	}
	d := s.f.StartDaemon()
	d.Reply("state", map[string]any{
		"live": map[string]any{"tcpu": 61.0, "tgpu": 55.0, "chassis": 40.0, "rpm1": 2000.0, "rpm2": 2100.0, "cpu": 30.0, "gpu": 5.0, "gpuw": 12.0, "ac": false,
			"power_mode": "turbo", "auto_mode": "smart", "activity": "gaming", "mode_reason": "busy", "fan_off": true},
		"settings":    map[string]any{"smart_fan": true, "fan_curve": "quiet", "smart_profile": "steady"},
		"hold_left_s": 90,
	})
	st, err := b.ServiceState(t.Context())
	if err != nil || st == nil {
		t.Fatal(st, err)
	}
	if st.Sensors == nil || st.Sensors.CPUTemp != 61 || st.Sensors.CPUFanRPM != 2000 || st.PowerMode != "turbo" || st.AC.V || !st.AC.OK ||
		st.AutoMode != "smart" || st.SmartProfile != "steady" || !st.FanOff || !st.SmartFan || st.FanCurve != "quiet" ||
		st.CPULoad.V != 30 || st.GPUWatts.V != 12 || st.Activity != "gaming" || st.ModeReason != "busy" || st.HoldLeft != 90*time.Second {
		t.Fatalf("%+v", st)
	}
}

func TestBatteryFromTheSupply(t *testing.T) {
	s := newScreen(t)
	dir := s.f.Dir + "/root/sys/class/power_supply/BAT0"
	if err := mkfiles(dir, map[string]string{"capacity": "73\n", "status": "Charging\n"}); err != nil {
		t.Fatal(err)
	}
	b, ok := adapt.TUI(s.a, s.f.S).Battery()
	if !ok || b.Percent != 73 || b.Status != "Charging" {
		t.Fatalf("%+v %v", b, ok)
	}
}

func TestDoctorAndTheWindow(t *testing.T) {
	s := newScreen(t)
	b := adapt.TUI(s.a, s.f.S)
	titles := b.DoctorChecks()
	res, err := b.RunDoctor(t.Context())
	if err != nil || len(res) != len(titles) || res[0].Check != titles[0] {
		t.Fatalf("%d results for %d checks (%v)", len(res), len(titles), err)
	}
	for _, r := range res {
		if r.Status != tui.DoctorOK && r.Status != tui.DoctorWarn && r.Status != tui.DoctorFail {
			t.Fatalf("%+v", r)
		}
	}
	if err := b.OpenWindow(t.Context()); err != nil {
		t.Fatal(err)
	}
	if got := s.f.Spawned(); len(got) != 1 || strings.Join(got[0], " ") != "/opt/truesense gui" {
		t.Fatalf("%v", got)
	}
	if v, err := b.ModuleVersion(t.Context()); err != nil || v == "" {
		t.Fatalf("%q %v", v, err)
	}
	s.page(6)
	s.keys("jj")
	if !strings.Contains(s.text(), "Health check") {
		t.Fatal(s.text())
	}
}

func TestProfilesAndLooks(t *testing.T) {
	s := newScreen(t)
	b := adapt.TUI(s.a, s.f.S)
	if len(b.ProfileNames()) != 0 {
		t.Fatal(b.ProfileNames())
	}
	if _, err := s.a.SaveProfile(t.Context(), "mine", "x"); err != nil {
		t.Fatal(err)
	}
	if names := b.ProfileNames(); len(names) != 1 || names[0] != "mine" {
		t.Fatal(names)
	}
	if b.DescribeProfile("mine") == "" && b.DescribeProfile("nope") != "" {
		t.Fatal("describe")
	}
	if err := b.ApplyProfile(t.Context(), "mine"); err != nil {
		t.Fatal(err)
	}
	if err := b.ApplyProfile(t.Context(), "nope"); err == nil {
		t.Fatal("unknown profile")
	}
	if err := b.ApplyLook(t.Context(), "native"); err != nil {
		t.Fatal(err)
	}
	if len(b.LookNames()) == 0 {
		t.Fatal("looks")
	}
}

func TestSettingsPassThroughWithoutAService(t *testing.T) {
	s := newScreen(t)
	b := adapt.TUI(s.a, s.f.S)
	ctx := t.Context()
	must := func(err error) {
		t.Helper()
		if err != nil {
			t.Fatal(err)
		}
	}
	must(b.SetBatteryLimit(ctx, true))
	must(b.SetCalibration(ctx, true))
	must(b.SetUSBCharge(ctx, 20))
	must(b.SetGPUMode(ctx, "auto"))
	must(b.SetOverdrive(ctx, false))
	must(b.SetBootSound(ctx, true))
	must(b.SetKeyboardTimeout(ctx, false))
	for name, f := range map[string]func() (bool, error){
		"limit": func() (bool, error) { return b.BatteryLimit(ctx) }, "calibration": func() (bool, error) { return b.Calibration(ctx) },
		"boot": func() (bool, error) { return b.BootSound(ctx) },
	} {
		if v, err := f(); err != nil || !v {
			t.Fatalf("%s: %v %v", name, v, err)
		}
	}
	if v, _ := b.Overdrive(ctx); v {
		t.Fatal("overdrive")
	}
	if v, _ := b.KeyboardTimeout(ctx); v {
		t.Fatal("timeout")
	}
	if v, _ := b.USBCharge(ctx); v != 20 {
		t.Fatal(v)
	}
	if v, _ := b.GPUMode(ctx); v != "auto" {
		t.Fatal(v)
	}
	// what needs the service says so
	for name, err := range map[string]error{
		"auto mode": b.SetAutoMode(ctx, "smart"),
		"release":   b.ReleaseHold(ctx),
		"curve":     b.UseFanCurve(ctx, "quiet"),
		"profile":   b.UseSmartProfile(ctx, "steady"),
		"full":      b.ChargeToFull(ctx, true),
		"fan smart": b.SetFan(ctx, tui.FanRequest{Mode: "smart"}),
	} {
		if err == nil || !strings.Contains(err.Error(), "daemon") {
			t.Errorf("%s: %v", name, err)
		}
	}
	if err := b.SetPowerMode(ctx, "turbo", 30*time.Minute); err != nil {
		t.Fatal(err)
	}
	if names := b.FanCurveNames(); len(names) == 0 || names[0] != "native" {
		t.Fatal(names)
	}
	if names := b.SmartProfileNames(); len(names) == 0 {
		t.Fatal(names)
	}
	if len(b.RearModes()) != 5 || len(b.SavedLightings()) != 0 {
		t.Fatal("rear modes / lightings")
	}
}
