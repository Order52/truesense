package daemon

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Order52/truesense/internal/ipc"
	"github.com/Order52/truesense/internal/looks"
)

func TestIdleHeavyHotAndBack(t *testing.T) {
	r := newRig(t)
	r.drive(25, 45, 45, 2)
	if r.snap().Fan != "auto" {
		t.Fatalf("idle: fans on the firmware, got %s", r.snap().Fan)
	}
	r.drive(8, 70, 60, 90)
	if r.snap().Fan != "manual" {
		t.Fatalf("heavy: manual fans, got %s", r.snap().Fan)
	}
	r.drive(14, 93, 70, 99)
	if d := r.snap().Duty; d != [2]int{100, 100} {
		t.Fatalf("hot: emergency 100/100 (the temperature is smoothed, so it takes a few ticks), got %v", d)
	}
	r.drive(45, 50, 48, 2)
	if r.snap().Fan != "auto" {
		t.Fatalf("calm again: back to the firmware, got %s", r.snap().Fan)
	}
}

func TestSmartFanOffIsHandsOffAndSettingsAreChecked(t *testing.T) {
	r := newRig(t)
	r.mustCall("set", ipc.Args{"key": "smart_fan", "value": false})
	r.drive(5, 95, 95, 99)
	if r.snap().Fan != "auto" {
		t.Fatal("smart fan off: the firmware keeps the fans even when hot")
	}
	r.mustCall("set", ipc.Args{"key": "smart_fan", "value": true})
	for _, bad := range []ipc.Args{{"key": "bogus", "value": true}, {"key": "smart_fan", "value": "yes"}, {"key": "auto_mode", "value": "turbo"}} {
		if _, err := r.call("set", bad); err == nil {
			t.Errorf("%v must be refused", bad)
		}
	}
}

func TestNativeAutoFollowsThePowerSourceOnly(t *testing.T) {
	r := newRig(t)
	r.mustCall("set", ipc.Args{"key": "auto_mode", "value": "native"})
	r.dm.curMode = "eco"
	r.sim.Run("set_mode 6")
	r.drive(8, 40, 40, 95)
	if r.snap().Mode != 1 {
		t.Fatalf("on AC: balanced even under load (no heat or load logic), got %d", r.snap().Mode)
	}
	r.ac = false
	r.drive(8, 40, 40, 95)
	if r.snap().Mode != 6 {
		t.Fatalf("on battery: eco, got %d", r.snap().Mode)
	}
}

func TestSmartAutoEscalatesAndBacksOffForHeat(t *testing.T) {
	r := newRig(t)
	r.mustCall("set", ipc.Args{"key": "auto_mode", "value": "smart"})
	r.mustCall("smart_profile", ipc.Args{"action": "use", "name": "truesense"})
	r.drive(70, 40, 40, 5)
	if r.snap().Mode != 1 {
		t.Fatalf("idle on AC: balanced, got %d", r.snap().Mode)
	}
	r.drive(10, 70, 60, 90)
	if r.snap().Mode != 4 {
		t.Fatalf("heavy load: performance first, got %d", r.snap().Mode)
	}
	r.drive(70, 70, 60, 90)
	if r.snap().Mode != 5 {
		t.Fatalf("sustained, cool and not changed for a while: turbo, got %d", r.snap().Mode)
	}
	r.drive(10, 93, 70, 95)
	if r.snap().Mode != 4 {
		t.Fatalf("hot: back off from turbo at once, got %d", r.snap().Mode)
	}
	r.drive(40, 70, 60, 90)
	if r.snap().Mode != 4 {
		t.Fatalf("and stay there during the back-off hold, got %d", r.snap().Mode)
	}
}

func TestHoldWinsAndEndsByItself(t *testing.T) {
	r := newRig(t)
	r.mustCall("set", ipc.Args{"key": "auto_mode", "value": "smart"})
	r.mustCall("smart_profile", ipc.Args{"action": "use", "name": "truesense"})
	r.drive(70, 40, 40, 5)
	r.mustCall("power", ipc.Args{"mode": "eco", "hold_minutes": 30.0})
	if r.snap().Mode != 6 {
		t.Fatal("power must set the mode")
	}
	r.drive(30, 40, 40, 5)
	if r.snap().Mode != 6 {
		t.Fatalf("same situation: your pick stays, got %d", r.snap().Mode)
	}
	r.drive(8, 70, 60, 90)
	if r.snap().Mode != 4 {
		t.Fatalf("a game starts: smart auto takes over without anyone pressing resume, got %d", r.snap().Mode)
	}
	st := r.mustCall("state", nil).(map[string]any)
	if st["hold_left_s"].(int) != 0 {
		t.Fatalf("the hold is gone: %v", st["hold_left_s"])
	}
	r.mustCall("power", ipc.Args{"mode": "eco", "hold_minutes": 30.0})
	r.ac = false
	r.drive(8, 70, 60, 90)
	if r.snap().Mode == 6 && r.st.Float("profile_hold_until", 0) != 0 {
		t.Fatal("unplugging also ends the hold")
	}
}

func TestChoosingSmartOrNativeAppliesImmediatelyEvenDuringAHold(t *testing.T) {
	r := newRig(t)
	r.mustCall("set", ipc.Args{"key": "auto_mode", "value": "off"})
	r.drive(5, 40, 40, 5)
	r.mustCall("power", ipc.Args{"mode": "turbo", "hold_minutes": 30.0})
	if r.snap().Mode != 5 {
		t.Fatal("turbo")
	}
	r.mustCall("set", ipc.Args{"key": "auto_mode", "value": "native"})
	r.drive(3, 40, 40, 5)
	if r.snap().Mode != 1 {
		t.Fatalf("native (AC: balanced) must not wait for the old hold to run out, got %d", r.snap().Mode)
	}
	if st := r.mustCall("state", nil).(map[string]any); st["hold_left_s"].(int) != 0 {
		t.Fatal("the hold must be cleared")
	}
}

func TestFanRequestsNeedConfirmationAndACoolMachineToStopAFan(t *testing.T) {
	r := newRig(t)
	r.drive(5, 40, 40, 2)
	for name, args := range map[string]ipc.Args{
		"unconfirmed off":    {"mode": "off"},
		"unconfirmed manual": {"mode": "manual", "cpu": 0.0, "gpu": 50.0},
		"bad duty":           {"mode": "manual", "cpu": 10.0},
		"duty too high":      {"mode": "manual", "cpu": 30.0, "gpu": 101.0},
		"no duty":            {"mode": "manual"},
		"unknown mode":       {"mode": "turbo"},
	} {
		before := r.snap()
		if _, err := r.call("fan", args); err == nil {
			t.Errorf("%s must be refused", name)
		}
		if r.snap().Duty != before.Duty || r.snap().Fan != before.Fan {
			t.Errorf("%s: a refused request must not reach the firmware", name)
		}
	}
	r.drive(10, 80, 70, 90)
	if _, err := r.call("fan", ipc.Args{"mode": "off", "confirm": true}); err == nil || !strings.Contains(err.Error(), "too warm to stop") {
		t.Fatalf("a warm machine: %v", err)
	}
}

func TestAStoppedFanComesBackWhenTheLaptopGetsWarm(t *testing.T) {
	r := newRig(t)
	r.drive(15, 40, 40, 2)
	res, err := r.call("fan", ipc.Args{"mode": "off", "confirm": true})
	if err != nil || res != "off" {
		t.Fatalf("stop: %v %v", res, err)
	}
	if d := r.snap().Duty; d != [2]int{0, 0} || r.snap().Fan != "manual" {
		t.Fatalf("both fans stopped: %+v", r.snap())
	}
	r.drive(3, 45, 45, 2)
	if d := r.snap().Duty; d != [2]int{0, 0} {
		t.Fatalf("cool: the stop stands, got %v", d)
	}
	r.drive(12, 75, 60, 40) // the CPU gets warm: the watchdog hands the fans back
	if r.snap().Fan != "auto" {
		t.Fatalf("the watchdog must return the fans to the firmware, got %+v", r.snap())
	}
	if r.dm.fanOff {
		t.Fatal("the stop is over")
	}
}

func TestPartialStopAndModes(t *testing.T) {
	r := newRig(t)
	r.drive(5, 40, 40, 2)
	if res := r.mustCall("fan", ipc.Args{"mode": "manual", "cpu": 0.0, "gpu": 60.0, "confirm": true}); res != "partial-off" {
		t.Fatalf("one fan stopped: %v", res)
	}
	if d := r.snap().Duty; d != [2]int{0, 60} {
		t.Fatalf("duty = %v", d)
	}
	if res := r.mustCall("fan", ipc.Args{"mode": "max"}); res != "max" || r.snap().Fan != "max" {
		t.Fatalf("max: %v %+v", res, r.snap())
	}
	if res := r.mustCall("fan", ipc.Args{"mode": "manual", "cpu": 45.0}); res != "manual" || r.snap().Duty != [2]int{45, 45} {
		t.Fatalf("manual with one duty applies to both: %v %+v", res, r.snap())
	}
	r.mustCall("fan", ipc.Args{"mode": "auto"})
	if r.st.Bool("smart_fan", true) {
		t.Fatal("an explicit fan mode switches the smart curve off")
	}
	if res := r.mustCall("fan", ipc.Args{"mode": "smart"}); res != "smart" || !r.st.Bool("smart_fan", false) {
		t.Fatal("smart switches the curve on")
	}
}

func TestFanCurveIsFollowedAndCustomCurvesAreChecked(t *testing.T) {
	r := newRig(t)
	r.drive(25, 45, 45, 2)
	if r.snap().Fan != "auto" {
		t.Fatal("native curve idle: auto")
	}
	r.mustCall("fan_curve", ipc.Args{"action": "use", "name": "max-cooling"})
	r.drive(8, 65, 50, 40)
	if r.snap().Fan != "manual" || r.snap().Duty[0] < 90 {
		t.Fatalf("max-cooling: strong duties, got %+v", r.snap())
	}
	weak := map[string]any{"cpu": []any{[]any{50.0, 0.0}, []any{70.0, 50.0}}, "gpu": []any{[]any{50.0, 0.0}, []any{78.0, 100.0}}}
	if _, err := r.call("fan_curve", ipc.Args{"action": "use", "name": "weak", "data": weak}); err == nil {
		t.Fatal("a weak curve must be refused")
	}
	if got := r.mustCall("fan_curve", ipc.Args{"action": "get"}).(map[string]any)["name"]; got != "max-cooling" {
		t.Fatalf("the failed switch changed nothing: %v", got)
	}
	r.mustCall("fan_curve", ipc.Args{"action": "use", "name": "native"})
	if r.st.Bool("smart_fan", true) || r.snap().Fan != "auto" {
		t.Fatal("the native curve is no curve: the firmware runs the fans")
	}
}

func TestSmartProfileSwitching(t *testing.T) {
	r := newRig(t)
	r.mustCall("set", ipc.Args{"key": "auto_mode", "value": "smart"})
	r.mustCall("smart_profile", ipc.Args{"action": "use", "name": "battery-first"})
	r.drive(70, 40, 40, 5)
	r.drive(60, 70, 60, 90)
	if r.snap().Mode != 4 {
		t.Fatalf("battery-first never uses Turbo, even when sustained, got %d", r.snap().Mode)
	}
	r.mustCall("smart_profile", ipc.Args{"action": "use", "name": "performance-first"})
	r.drive(25, 70, 60, 90)
	if r.snap().Mode != 5 {
		t.Fatalf("performance-first: Turbo after 10 s, got %d", r.snap().Mode)
	}
	if _, err := r.call("smart_profile", ipc.Args{"action": "use", "name": "bad", "data": map[string]any{"ac_busy": "warp"}}); err == nil {
		t.Fatal("an invalid profile must be refused")
	}
	if got := r.mustCall("smart_profile", ipc.Args{"action": "get"}).(map[string]any)["name"]; got != "performance-first" {
		t.Fatalf("name = %v", got)
	}
}

func TestUndervoltNeedsConfirmationAndBootHasACrashGuard(t *testing.T) {
	r := newRig(t)
	if _, err := r.call("undervolt", ipc.Args{"action": "apply", "core": -50.0, "cache": -40.0}); err == nil {
		t.Fatal("no confirmation: refused")
	}
	if _, err := r.call("undervolt", ipc.Args{"action": "apply", "core": -300.0, "cache": -40.0, "confirm": true}); err == nil {
		t.Fatal("beyond the limit: refused")
	}
	if r.uv.core != 0 || r.uv.cache != 0 || len(r.uv.applied) != 0 {
		t.Fatal("nothing must have been applied")
	}
	r.mustCall("undervolt", ipc.Args{"action": "apply", "core": -50.0, "cache": -40.0, "confirm": true, "name": "truesense"})
	if r.uv.core != -50 || r.uv.cache != -40 {
		t.Fatalf("applied: %+v", r.uv)
	}
	r.mustCall("undervolt", ipc.Args{"action": "reset"})
	if r.uv.core != 0 {
		t.Fatal("reset")
	}
	r.mustCall("undervolt", ipc.Args{"action": "boot", "core": -30.0, "cache": -30.0, "confirm": true, "name": "mild"})
	if got := r.mustCall("undervolt", ipc.Args{"action": "get"}).(map[string]any)["boot"].(map[string]any)["name"]; got != "mild" {
		t.Fatalf("boot = %v", got)
	}
	r.dm.applyUVBoot()
	if r.uv.core != -30 || !r.uvGuard.Armed() {
		t.Fatal("applied at boot with the guard flag set")
	}
	// the next boot: the flag survived, so the previous boot crashed
	r.uv.core, r.uv.cache = 0, 0
	r2 := newRig(t)
	r2.st.Set("uv_boot", map[string]any{"name": "mild", "core": -30.0, "cache": -30.0})
	if err := r2.uvGuard.Arm(); err != nil {
		t.Fatal(err)
	}
	r2.dm.applyUVBoot()
	if r2.uv.core != 0 {
		t.Fatal("the previous boot never proved stable: do not apply again")
	}
	if v, _ := r2.st.Get("uv_boot"); v != nil {
		t.Fatal("and the setting is forgotten")
	}
}

func TestADangerousUndervoltNeedsForce(t *testing.T) {
	r := newRig(t)
	_, err := r.call("undervolt", ipc.Args{"action": "apply", "core": -100.0, "cache": -60.0, "confirm": true})
	if err == nil {
		t.Skip("this value is not in the danger zone of the shipped limits")
	}
	if !strings.Contains(err.Error(), "force") {
		t.Fatalf("a danger-zone value must ask for the force flag: %v", err)
	}
	r.mustCall("undervolt", ipc.Args{"action": "apply", "core": -100.0, "cache": -60.0, "confirm": true, "force": true})
}

func TestAFailedUndervoltIsRolledBack(t *testing.T) {
	r := newRig(t)
	r.uv.core = -10
	r.uv.failApply = true
	if _, err := r.call("undervolt", ipc.Args{"action": "apply", "core": -50.0, "cache": -40.0, "confirm": true}); err == nil {
		t.Fatal("must fail")
	}
	if r.uv.core != 0 {
		t.Fatalf("a failed apply resets to stock: %d", r.uv.core)
	}
}

func TestGPUUndervoltNeedsConfirmationAndBootHasACrashGuard(t *testing.T) {
	r := newRig(t)
	if _, err := r.call("gpu_undervolt", ipc.Args{"action": "apply", "offset": -100.0, "cap": 2000.0}); err == nil {
		t.Fatal("no confirmation: refused")
	}
	r.mustCall("gpu_undervolt", ipc.Args{"action": "apply", "offset": -100.0, "cap": 2000.0, "confirm": true, "name": "x"})
	if r.gpuuv.offset != -100 || r.gpuuv.capMHz != 2000 {
		t.Fatalf("%+v", r.gpuuv)
	}
	r.mustCall("gpu_undervolt", ipc.Args{"action": "reset"})
	r.mustCall("gpu_undervolt", ipc.Args{"action": "boot", "offset": -50.0, "cap": 0.0, "confirm": true})
	r.dm.applyGPUUVBoot()
	if r.gpuuv.offset != -50 || !r.gpuGuard.Armed() {
		t.Fatal("applied at boot with the guard flag set")
	}
	r2 := newRig(t)
	r2.st.Set("gpu_uv_boot", map[string]any{"offset": -50.0, "cap": 0.0})
	_ = r2.gpuGuard.Arm()
	r2.dm.applyGPUUVBoot()
	if r2.gpuuv.offset != 0 {
		t.Fatal("the previous boot never proved stable")
	}
}

func TestChargeToFullOnce(t *testing.T) {
	r := newRig(t)
	r.drive(6, 40, 40, 2)
	r.sim.Run("set_battery 1 1")
	r.mustCall("battery_full", ipc.Args{"action": "start"})
	if r.snap().Battery[1] != 0 {
		t.Fatal("the 80 % limit is lifted for one charge")
	}
	r.ac = false
	r.drive(6, 40, 40, 2)
	if r.snap().Battery[1] != 1 || r.st.Bool("charge_full", false) {
		t.Fatalf("unplugged: the limit is back and the state cleared: %+v", r.snap().Battery)
	}
	if _, err := r.call("battery_full", ipc.Args{"action": "start"}); err == nil {
		t.Fatal("start needs the charger")
	}
}

func TestPowerLimitsAreEnforcedAndHandedBack(t *testing.T) {
	r := newRig(t)
	r.dm.withLock(r.dm.capturePowerLimits) // the daemon does this at start, before anything can change the limits
	r.mustCall("cpu_limits", ipc.Args{"pl1": 70.0, "pl2": 90.0})
	r.drive(10, 40, 40, 2)
	if r.pow.raw.PL1 != 70_000_000 || r.pow.raw.PL2 != 90_000_000 {
		t.Fatalf("the user's limits are kept: %+v", r.pow.raw)
	}
	r.pow.raw.PL1 = 50_000_000 // something else changes them
	r.drive(4, 40, 40, 2)
	if r.pow.raw.PL1 != 70_000_000 {
		t.Fatalf("they are re-asserted: %+v", r.pow.raw)
	}
	r.mustCall("cpu_limits", ipc.Args{"reset": true})
	r.mustCall("set", ipc.Args{"key": "smart_fan", "value": false})
	r.mustCall("set", ipc.Args{"key": "auto_mode", "value": "native"})
	r.drive(10, 40, 40, 2)
	if r.pow.raw.PL1 != 80_000_000 {
		t.Fatalf("nothing smart on: the firmware's own limits come back, got %+v", r.pow.raw)
	}
	if _, err := r.call("cpu_limits", ipc.Args{"pl1": 200.0, "pl2": 300.0}); err == nil {
		t.Fatal("limits beyond the safe range need force")
	}
}

func TestGameRulesRaiseTheModeWhileTheGameRuns(t *testing.T) {
	r := newRig(t)
	r.mustCall("set", ipc.Args{"key": "auto_mode", "value": "native"})
	r.mustCall("game_rules", ipc.Args{"action": "add", "name": "HL2_Linux", "mode": "performance"})
	r.procs = nil
	r.drive(6, 40, 40, 5)
	if r.snap().Mode != 1 {
		t.Fatalf("no game: native balanced, got %d", r.snap().Mode)
	}
	r.procs = procsNamed("hl2_linux")
	r.drive(8, 40, 40, 5)
	if r.snap().Mode != 4 {
		t.Fatalf("the game runs: performance, got %d", r.snap().Mode)
	}
	got := r.mustCall("game_rules", ipc.Args{"action": "list"}).(map[string]any)
	if got["running"] != "performance" {
		t.Fatalf("running = %v", got["running"])
	}
	if _, err := r.call("game_rules", ipc.Args{"action": "add", "name": "x", "mode": "warp"}); err == nil {
		t.Fatal("invalid mode")
	}
	if _, err := r.call("game_rules", ipc.Args{"action": "remove", "name": "nothere"}); err == nil {
		t.Fatal("removing an unknown rule")
	}
}

func TestShutdownReturnsFansToAutoAndLiftsTheClockCap(t *testing.T) {
	r := newRig(t)
	r.mustCall("fan", ipc.Args{"mode": "manual", "cpu": 70.0, "gpu": 70.0})
	r.dm.Shutdown()
	if r.snap().Fan != "auto" {
		t.Fatalf("fans must be back on the firmware: %+v", r.snap())
	}
	if r.cap.uncapped == 0 {
		t.Fatal("the clock cap must be lifted")
	}
	r.dm.Shutdown() // twice is fine
}

func TestThermalGovernorCapsAHotCPUOnlyWhileManagingIt(t *testing.T) {
	r := newRig(t)
	r.mustCall("set", ipc.Args{"key": "smart_fan", "value": false})
	r.mustCall("set", ipc.Args{"key": "auto_mode", "value": "native"})
	r.drive(20, 92, 60, 99)
	if len(r.cap.caps) != 0 {
		t.Fatalf("nothing smart on: the CPU is left alone, got caps %v", r.cap.caps)
	}
	r.mustCall("set", ipc.Args{"key": "smart_fan", "value": true})
	r.drive(20, 92, 60, 99)
	if len(r.cap.caps) == 0 {
		t.Fatal("a smart feature is on and the CPU is hot: the clock is capped")
	}
	if last := r.cap.caps[len(r.cap.caps)-1]; last >= 5_000_000 {
		t.Fatalf("the cap must be lower than the maximum: %d", last)
	}
}

func TestTheLiveStateIsPublishedForStatusBarsAndHealthChecks(t *testing.T) {
	r := newRig(t)
	r.drive(3, 50, 48, 10)
	b, err := os.ReadFile(filepath.Join(r.run, "state"))
	if err != nil {
		t.Fatal(err)
	}
	txt := string(b)
	for _, want := range []string{"mode=smart\n", "activity=", "tcpu=", "power_mode=", "ac=1\n", "auto_mode=native\n", "hw_ok=1\n", "fan_off=0\n"} {
		if !strings.Contains(txt, want) {
			t.Errorf("state file lacks %q:\n%s", want, txt)
		}
	}
	if !exists(filepath.Join(filepath.Dir(r.run), "tmp-state")) {
		t.Fatal("the world-readable copy must exist too")
	}
	st := r.mustCall("state", nil).(map[string]any)
	if st["live"].(map[string]any)["hw_ok"] != true {
		t.Fatalf("live: %v", st["live"])
	}
}

func TestADeadDeviceIsWaitedForNotFatal(t *testing.T) {
	r := newRig(t)
	r.drive(3, 50, 48, 10)
	r.dm.d.HW = deadHW{r.dm.d.HW}
	r.samp.mu.Lock()
	r.samp.pkg, r.samp.havePkg = 55, true
	r.samp.mu.Unlock()
	r.dm.step() // must not panic
	if r.dm.hwOK {
		t.Fatal("the loop must notice the device is gone")
	}
}

type deadHW struct{ Hardware }

func (deadHW) ReadSensors() (hwSensors, error) { return hwSensors{}, os.ErrNotExist }

func TestAStepThatPanicsWhileHotDrivesTheFansToFull(t *testing.T) {
	r := newRig(t)
	r.drive(20, 90, 80, 90)
	r.dm.d.Sensors = panicSampler{}
	r.dm.safeStep()
	if d := r.snap().Duty; d != [2]int{100, 100} {
		t.Fatalf("a failing step while hot must not leave the fans low: %+v", r.snap())
	}
}

func TestBootLookIsStoredAndApplied(t *testing.T) {
	r := newRig(t)
	if got := r.mustCall("boot_look", ipc.Args{"action": "get"}); got != (*looks.Look)(nil) {
		t.Fatalf("no boot look yet: %v", got)
	}
	if _, err := r.call("boot_look", ipc.Args{"action": "set", "look": map[string]any{}}); err == nil {
		t.Fatal("an empty look is refused")
	}
	look := map[string]any{"rear": map[string]any{"mode": "breathing", "color": "ff0000", "speed": 2.0, "brightness": 80.0, "direction": 1.0}}
	r.mustCall("boot_look", ipc.Args{"action": "set", "look": look})
	if r.looks.boot == nil || r.looks.boot.Rear == nil {
		t.Fatal("stored")
	}
	r.dm.applyBootLook("test")
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		r.dm.mu.Lock()
		n := len(r.looks.applied)
		r.dm.mu.Unlock()
		if n > 0 {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	r.dm.mu.Lock()
	n := len(r.looks.applied)
	r.dm.mu.Unlock()
	if n == 0 {
		t.Fatal("the boot look should have been applied")
	}
	r.mustCall("boot_look", ipc.Args{"action": "clear"})
	if r.looks.boot != nil {
		t.Fatal("cleared")
	}
}

func TestPowerManagersAndTouchpad(t *testing.T) {
	r := newRig(t)
	r.pm.found = nil
	if got := r.mustCall("power_managers", ipc.Args{"action": "status"}).(map[string]any); got["on"] != false {
		t.Fatalf("%v", got)
	}
	r.mustCall("power_managers", ipc.Args{"action": "takeover"})
	if !r.st.Bool("pm_takeover", false) {
		t.Fatal("takeover is remembered")
	}
	r.mustCall("power_managers", ipc.Args{"action": "restore"})
	if r.st.Bool("pm_takeover", true) {
		t.Fatal("restore clears it")
	}
	if got := r.mustCall("touchpad", ipc.Args{"action": "toggle"}); got != false {
		t.Fatalf("toggle: %v", got)
	}
	if got := r.mustCall("touchpad", ipc.Args{"action": "status"}); got != false {
		t.Fatalf("status: %v", got)
	}
	if _, err := r.call("touchpad", ipc.Args{"action": "sideways"}); err == nil {
		t.Fatal("bad action")
	}
}

func TestEveryRequestGoesThroughTheWireAndUnknownCommandsAreErrors(t *testing.T) {
	r := newRig(t)
	srv := &ipc.Server{Handlers: r.dm.Handlers()}
	out := string(srv.HandleLine(context.Background(), 0, []byte(`{"cmd":"ping"}`)))
	if !strings.Contains(out, `"result":"pong"`) {
		t.Fatalf("%s", out)
	}
	out = string(srv.HandleLine(context.Background(), 0, []byte(`{"cmd":"fan","args":{"mode":"manual","cpu":"fast"}}`)))
	if !strings.Contains(out, `"ok":false`) {
		t.Fatalf("a wrong argument type is an error reply: %s", out)
	}
	for cmd := range r.dm.Handlers() {
		out := string(srv.HandleLine(context.Background(), 0, []byte(`{"cmd":"`+cmd+`","args":{"action":"nonsense","mode":"nonsense","key":"nonsense"}}`)))
		if !strings.HasSuffix(out, "\n") {
			t.Errorf("%s: reply is not a line", cmd)
		}
	}
}

func TestRunHandsBackOnCancel(t *testing.T) {
	r := newRig(t)
	sock := filepath.Join(os.TempDir(), "tsd-"+filepath.Base(t.TempDir())+".sock")
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- r.dm.Run(ctx, sock, []uint32{uint32(os.Getuid())}) }()
	c := &ipc.Client{Path: sock, Timeout: time.Second}
	for i := 0; i < 100 && !c.Available(); i++ {
		time.Sleep(20 * time.Millisecond)
	}
	var pong string
	if err := c.CallInto("ping", nil, &pong); err != nil || pong != "pong" {
		t.Fatalf("ping: %q %v", pong, err)
	}
	r.mustCall("fan", ipc.Args{"mode": "manual", "cpu": 60.0})
	cancel()
	select {
	case err := <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("Run must return after cancel")
	}
	if r.snap().Fan != "auto" {
		t.Fatalf("leaving must hand the fans back: %+v", r.snap())
	}
	if exists(sock) {
		t.Fatal("the socket must be removed")
	}
}
