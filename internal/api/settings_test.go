package api_test

import (
	"strings"
	"testing"

	"github.com/Order52/truesense/internal/api"
	"github.com/Order52/truesense/internal/ipc"
)

func TestFirmwareSettingsAreWrittenAndReadBack(t *testing.T) {
	f, a := machine(t)

	on, err := a.SetBatteryLimit(true)
	must(t, err)
	if !on {
		t.Fatal("battery limit")
	}
	if cal, _ := a.SetCalibration(true); !cal {
		t.Fatal("calibration")
	}
	st, err := a.BatteryStatus()
	must(t, err)
	if !st.Limit80 || !st.Calibration || st.USBChargeStop != 30 {
		t.Fatalf("%+v", st)
	}
	if lv, err := a.SetUSBCharge(10); err != nil || lv != 10 {
		t.Fatalf("%d %v", lv, err)
	}
	if _, err := a.SetUSBCharge(15); err == nil {
		t.Fatal("15 is not a USB charging level")
	}

	od, err := a.Overdrive()
	must(t, err)
	if od == nil || !*od {
		t.Fatalf("overdrive starts on in the simulator: %v", od)
	}
	if v, _ := a.SetOverdrive(false); v {
		t.Fatal("overdrive off")
	}
	if v, err := a.SetBootSound(true); err != nil || !v {
		t.Fatal("boot sound")
	}
	if v, err := a.SetKeyboardTimeout(false); err != nil || v {
		t.Fatal("keyboard timeout")
	}
	if m, err := a.SetGPUMode("auto"); err != nil || m != "auto" {
		t.Fatal(m, err)
	}
	if _, err := a.SetGPUMode("bogus"); err == nil {
		t.Fatal("unknown GPU mode")
	}
	if s := f.Sim.Snapshot(); s.Overdrive != 0 || s.USB != 10 {
		t.Fatalf("%+v", s)
	}
}

func TestChargeFull(t *testing.T) {
	f, a := machine(t)
	_, err := a.StartChargeFull()
	wantErr(t, err, "daemon is not running")
	d := f.StartDaemon()
	d.Reply("battery_full", func(args ipc.Args) (any, error) {
		if args["action"] == "get" {
			return map[string]any{"active": false, "limit": true, "capacity": 80.0, "ac": true}, nil
		}
		return map[string]any{"active": args["action"] == "start"}, nil
	})
	r, err := a.ChargeFullStatus()
	must(t, err)
	if r.Active || r.Limit == nil || !*r.Limit || r.Capacity == nil || *r.Capacity != 80 {
		t.Fatalf("%+v", r)
	}
	if r, _ := a.StartChargeFull(); !r.Active {
		t.Fatal("start")
	}
	if r, _ := a.CancelChargeFull(); r.Active {
		t.Fatal("cancel")
	}
}

func TestRear(t *testing.T) {
	f, a := machine(t)
	spec := api.DefaultRearSpec("wave")
	spec.Color, spec.Speed = "00ff88", 2
	r, err := a.SetRear(spec)
	must(t, err)
	if r.Mode != "wave" || r.Brightness != 100 || r.Speed != 2 {
		t.Fatalf("%+v", r)
	}
	spec = api.DefaultRearSpec("static")
	spec.Zones = []string{"ff0000", "00ff00", "0000ff"}
	if _, err := a.SetRear(spec); err != nil {
		t.Fatal(err)
	}
	if got := f.Sim.Snapshot().Zones; len(got) == 0 {
		t.Fatal("the three zones were not written")
	}
	spec.Mode = "disco"
	if _, err := a.SetRear(spec); err == nil {
		t.Fatal("unknown effect")
	}
}

// keepZones writes the part colours the window saved, so a brightness change does not flatten them.
func TestSetRearLookKeepsTheSavedZones(t *testing.T) {
	f, a := machine(t)
	must(t, f.S.User.Set("rear_zones", []string{"ff0000", "00ff00", "0000ff"}))
	spec := api.DefaultRearSpec("static")
	spec.Color = "112233"
	must(t, a.SetRearLook(spec, true))
	if z := f.Sim.Snapshot().Zones; len(z) != 3 {
		t.Fatalf("zones kept: %v", z)
	}
	f.Sim.Reset()
	must(t, a.SetRearLook(spec, false))
	z := f.Sim.Snapshot().Zones
	for _, c := range z {
		if c == "ff0000" {
			t.Fatalf("zones were written without keepZones: %v", z)
		}
	}
}

func TestRearPowerRunsTheKeyAction(t *testing.T) {
	_, a := machine(t)
	if msg, err := a.RearPower(t.Context(), "on"); err != nil || msg != "Rear light: on" {
		t.Fatalf("%q %v", msg, err)
	}
	if msg, _ := a.RearPower(t.Context(), "off"); msg != "Rear light: off" {
		t.Fatal(msg)
	}
}

func TestTouchpad(t *testing.T) {
	f, a := machine(t)
	if on, err := a.Touchpad(); err != nil || on != nil {
		t.Fatalf("no touchpad on a machine without one: %v %v", on, err)
	}
	_, err := a.SetTouchpad(true)
	wantErr(t, err, "daemon is not running")
	d := f.StartDaemon()
	d.Reply("touchpad", false)
	if on, err := a.SetTouchpad(false); err != nil || on {
		t.Fatalf("%v %v", on, err)
	}
	if c, _ := d.Last("touchpad"); c.Args["action"] != "off" {
		t.Fatalf("%+v", c)
	}
}

func TestCPULimitsAndUndervolt(t *testing.T) {
	f, a := machine(t)
	d := f.StartDaemon()
	d.Reply("cpu_limits", map[string]any{"pl1_w": 70.0, "pl2_w": 90.0})
	r, err := a.SetCPULimits(70, 90, false)
	must(t, err)
	if r.PL1W != 70 || r.PL2W != 90 {
		t.Fatalf("%+v", r)
	}
	if c, _ := d.Last("cpu_limits"); c.Args["force"] != false || c.Args["pl1"] != 70.0 {
		t.Fatalf("%+v", c)
	}
	d.Reply("cpu_info", map[string]any{"pl1_w": 80.0, "pl2_w": 80.0, "user": nil, "auto": map[string]any{"normal_w": 80, "gaming_w": 65}})
	lim, err := a.ResetCPULimits()
	must(t, err)
	if c, _ := d.Last("cpu_limits"); c.Args["reset"] != true || lim.Auto.GamingW != 65 || lim.User != nil {
		t.Fatalf("%+v %+v", c, lim)
	}

	// an undervolt names its profile, carries the confirmation and keeps the other plane
	d.Reply("undervolt", func(args ipc.Args) (any, error) {
		if args["action"] == "get" {
			return map[string]any{"core_mv": -20, "cache_mv": -10, "applied": nil, "boot": nil, "pending": false}, nil
		}
		return map[string]any{"core_mv": args["core"], "cache_mv": args["cache"]}, nil
	})
	if _, err := a.SetCPUUndervolt(2, -30, api.UndervoltRequest{Confirm: true}); err != nil {
		t.Fatal(err)
	}
	c, _ := d.Last("undervolt")
	if c.Args["core"] != -20.0 || c.Args["cache"] != -30.0 || c.Args["confirm"] != true || c.Args["name"] != "manual" {
		t.Fatalf("%+v", c)
	}
	prof, err := a.SaveCPUUndervolt("x", ptr(-30), ptr(-20))
	must(t, err)
	if prof.Core != -30 {
		t.Fatal(prof)
	}
	if _, err := a.SaveCPUUndervolt("x", nil, ptr(-20)); err == nil {
		t.Fatal("both offsets are needed")
	}
	calls := len(d.Calls())
	_, err = a.ApplyCPUUndervolt("x", api.UndervoltRequest{})
	wantErr(t, err, "needs explicit confirmation")
	if len(d.Calls()) != calls {
		t.Fatal("an unconfirmed undervolt must not even reach the service")
	}
	// beyond the danger line: the confirmation alone is not enough, force is needed too
	if _, err := a.ApplyCPUUndervoltValues("deep", -110, -90, api.UndervoltRequest{Confirm: true}); err == nil || !strings.Contains(err.Error(), "force") {
		t.Fatalf("%v", err)
	}
	if _, err := a.ApplyCPUUndervoltValues("deep", -110, -90, api.UndervoltRequest{Confirm: true, Force: true}); err != nil {
		t.Fatal(err)
	}
	if _, err := a.SetCPULimits(110, 120, false); err == nil || !strings.Contains(err.Error(), "force") {
		t.Fatalf("a power limit beyond the danger line needs force: %v", err)
	}
	if b, err := a.SetCPUUndervoltBoot("off", api.UndervoltRequest{}); err != nil || b != nil {
		t.Fatal(b, err)
	}
	if c, _ := d.Last("undervolt"); c.Args["action"] != "boot_clear" {
		t.Fatalf("%+v", c)
	}
	_, err = a.ApplyCPUUndervolt("nope", api.UndervoltRequest{Confirm: true})
	if err == nil {
		t.Fatal("unknown profile")
	}
}

func TestGPUUndervolt(t *testing.T) {
	f, a := machine(t)
	d := f.StartDaemon()
	d.Reply("gpu_undervolt", func(args ipc.Args) (any, error) {
		return map[string]any{"offset_mhz": args["offset"], "cap_mhz": args["cap"]}, nil
	})
	if _, err := a.SaveGPUUndervolt("g", ptr(100), 2400); err != nil {
		t.Fatal(err)
	}
	r, err := a.ApplyGPUUndervolt("g", api.UndervoltRequest{Confirm: true})
	must(t, err)
	if r.OffsetMHz != 100 || r.CapMHz != 2400 {
		t.Fatalf("%+v", r)
	}
	if _, err := a.SaveGPUUndervolt("g", nil, 0); err == nil {
		t.Fatal("an offset is needed")
	}
	b, err := a.SetGPUUndervoltBoot("g", api.UndervoltRequest{Confirm: true, Force: true})
	if err != nil {
		t.Fatal(err)
	}
	if b == nil {
		t.Fatal("boot")
	}
	if c, _ := d.Last("gpu_undervolt"); c.Args["force"] != true || c.Args["action"] != "boot" {
		t.Fatalf("%+v", c)
	}
}
