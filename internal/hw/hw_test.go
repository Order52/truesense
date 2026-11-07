package hw

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/Order52/truesense/internal/firmware"
	"github.com/Order52/truesense/internal/userstate"
)

func newHW(t *testing.T) (*HW, *firmware.Simulator) {
	t.Helper()
	sim := firmware.NewSimulator()
	h := New(sim, &userstate.Store{Path: filepath.Join(t.TempDir(), "user.json")})
	h.SleepFor = func(time.Duration) {} // no real waiting in tests
	return h, sim
}

func TestPowerModes(t *testing.T) {
	h, sim := newHW(t)
	for _, m := range ModeNames {
		got, err := h.SetMode(m)
		if err != nil || got != m {
			t.Fatalf("SetMode(%s) = %q, %v", m, got, err)
		}
		if cur, _ := h.GetMode(); cur != m {
			t.Fatalf("GetMode = %q, want %q", cur, m)
		}
	}
	if _, err := h.SetMode("ludicrous"); err == nil || !strings.Contains(err.Error(), "must be one of") {
		t.Fatalf("err = %v", err)
	}
	if got := sim.Snapshot().Mode; got != 6 {
		t.Fatalf("the last SetMode(eco) must have written 6, sim has %d", got)
	}
	modes, err := h.SupportedModes()
	if err != nil || !reflect.DeepEqual(modes, ModeNames) {
		t.Fatalf("supported = %v, %v", modes, err)
	}
}

func TestOnOffSettingsReadBack(t *testing.T) {
	h, _ := newHW(t)
	for name, tc := range map[string]struct {
		set func(bool) (bool, error)
		get func() (bool, error)
	}{
		"boot sound": {h.SetBootSound, h.GetBootSound}, "battery limit": {h.SetBatteryLimit, h.GetBatteryLimit},
		"calibration": {h.SetCalibration, h.GetCalibration}, "kb timeout": {h.SetKBTimeout, h.GetKBTimeout}, "overdrive": {h.SetOverdrive, func() (bool, error) { v, _, err := h.GetOverdrive(); return v, err }},
	} {
		for _, want := range []bool{true, false} {
			if got, err := tc.set(want); err != nil || got != want {
				t.Fatalf("%s set %v = %v, %v", name, want, got, err)
			}
			if got, _ := tc.get(); got != want {
				t.Fatalf("%s get = %v, want %v", name, got, want)
			}
		}
	}
	if _, err := h.SetUSBCharge(7); err == nil {
		t.Fatal("an unsupported USB level must be refused before it reaches the firmware")
	}
	if got, err := h.SetUSBCharge(20); err != nil || got != 20 {
		t.Fatalf("usb = %d, %v", got, err)
	}
	if got, err := h.SetGPUMode("nvidia"); err != nil || got != "nvidia" {
		t.Fatalf("gpu = %q, %v", got, err)
	}
	if _, err := h.SetGPUMode("quantum"); err == nil {
		t.Fatal("unknown GPU mode must be refused")
	}
}

// laggy answers reads with the old value for the first n reads of a command: the firmware
// really does this right after a write.
type laggy struct {
	firmware.Device
	n     int
	reads map[string]int
}

func (l *laggy) Run(c string) (string, error) {
	if strings.HasPrefix(c, "get_mode") {
		l.reads[c]++
		if l.reads[c] <= l.n {
			return "ok mode=6\n", nil
		}
	}
	return l.Device.Run(c)
}

func TestSettleRetriesAndGivesUp(t *testing.T) {
	h, _ := newHW(t)
	h.Dev = &laggy{Device: h.Dev, n: 3, reads: map[string]int{}}
	if got, err := h.SetMode("turbo"); err != nil || got != "turbo" {
		t.Fatalf("a slow firmware must be waited for: %q, %v", got, err)
	}
	h.Dev = &laggy{Device: h.Dev, n: 1000, reads: map[string]int{}}
	_, err := h.SetMode("quiet")
	var fe *firmware.Error
	if !errors.As(err, &fe) || !strings.Contains(err.Error(), "readback mismatch") {
		t.Fatalf("a value that never verifies must be an error: %v", err)
	}
}

func TestFans(t *testing.T) {
	h, sim := newHW(t)
	if err := h.FanMax(); err != nil {
		t.Fatal(err)
	}
	if s, _ := h.GetFanState(); s.Mode != "max" || s.BehaviorRaw != "0x82" {
		t.Fatalf("%+v", s)
	}
	if err := h.FanManual(40, 55); err != nil {
		t.Fatal(err)
	}
	if s, _ := h.GetFanState(); s.Mode != "manual" || s.Duty.CPU != 40 || s.Duty.GPU != 55 {
		t.Fatalf("%+v", s)
	}
	for _, bad := range [][2]int{{10, 50}, {50, 101}, {19, 19}, {-1, 50}} {
		before := sim.Snapshot()
		if err := h.FanManual(bad[0], bad[1]); err == nil {
			t.Fatalf("%v must be refused", bad)
		}
		if !reflect.DeepEqual(before.Duty, sim.Snapshot().Duty) {
			t.Fatalf("%v: a refused duty must not reach the firmware", bad)
		}
	}
	if err := h.FanAuto(); err != nil {
		t.Fatal(err)
	}
	if sim.Snapshot().Fan != "auto" {
		t.Fatal("fans must be back on auto")
	}
}

// ignoresStop accepts set_fan_manual but keeps the old duty when asked to stop a fan.
type ignoresStop struct{ firmware.Device }

func (d ignoresStop) Run(c string) (string, error) {
	if c == "set_fan_manual 0 50" {
		return "ok\n", nil
	}
	return d.Device.Run(c)
}

func TestAFanStopThatTheFirmwareIgnoresHandsTheFansBack(t *testing.T) {
	h, sim := newHW(t)
	h.Dev = ignoresStop{h.Dev}
	if err := h.FanManual(30, 30); err != nil {
		t.Fatal(err)
	}
	_, _ = sim.Run("set_fan_manual 30 30")
	// "set_fan_manual 0 50" is swallowed: the mode never becomes manual-with-0, so settle fails
	// on the duty and the fans must be handed back.
	err := h.FanManual(0, 50)
	if err == nil {
		t.Fatal("an unverified stop must be an error")
	}
	if got := sim.Snapshot().Fan; got != "auto" {
		t.Fatalf("fans = %q, want auto after an unverified stop", got)
	}
}

func TestRear(t *testing.T) {
	h, sim := newHW(t)
	o := DefaultRear()
	o.Mode, o.Color, o.Brightness = "wave", "00FF40", 60
	r, err := h.RearSet(o)
	if err != nil || r.Mode != "wave" || r.Brightness != 60 || r.Color != "00ff40" {
		t.Fatalf("%+v, %v", r, err)
	}
	// Brightness 0 is a valid setting (the light is off), not an error.
	o.Brightness = 0
	if r, err = h.RearSet(o); err != nil || r.Brightness != 0 {
		t.Fatalf("%+v, %v", r, err)
	}
	// Static: the three parts are written separately, left to right.
	o = DefaultRear()
	o.Zones = []string{"ff0000", "00ff00", "0000ff"}
	if r, err = h.RearSet(o); err != nil || !reflect.DeepEqual(r.Zones, o.Zones) {
		t.Fatalf("%+v, %v", r, err)
	}
	if z := sim.Snapshot().Zones; z[1] != "ff0000" || z[2] != "00ff00" || z[4] != "0000ff" {
		t.Fatalf("zones = %v", z)
	}
	if got := h.State.Get("rear_zones"); got == nil {
		t.Fatal("the zone colours must be remembered")
	}
	for _, bad := range []RearOpts{{Mode: "disco", Color: "ffffff", Speed: 3, Brightness: 1}, {Mode: "static", Color: "nothex", Speed: 3, Brightness: 1},
		{Mode: "static", Color: "ffffff", Speed: 9, Brightness: 1}, {Mode: "static", Color: "ffffff", Speed: 3, Brightness: 101},
		{Mode: "wave", Color: "ffffff", Speed: 3, Brightness: 1, Zones: []string{"ff0000"}}, {Mode: "static", Color: "ffffff", Speed: 3, Brightness: 1, Zones: []string{"ff0000", "00ff00"}}} {
		if _, err := h.RearSet(bad); err == nil {
			t.Errorf("%+v must be refused", bad)
		}
	}
}

func TestSensorsReading(t *testing.T) {
	h, sim := newHW(t)
	sim.SetSensors([3]int{61, 58, 55}, [2]int{2400, 2300})
	s, err := h.ReadSensors()
	if err != nil || s != (Sensors{CPUTemp: 61, SysTemp: 58, GPUTemp: 55, CPUFanRPM: 2400, GPUFanRPM: 2300}) {
		t.Fatalf("%+v, %v", s, err)
	}
}

func TestSysfsReadings(t *testing.T) {
	h, _ := newHW(t)
	h.SysRoot = t.TempDir()
	write := func(p, s string) {
		p = filepath.Join(h.SysRoot, p)
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(s), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	if _, ok := h.CPUTemp(); ok {
		t.Fatal("no hwmon: no temperature")
	}
	write("sys/class/hwmon/hwmon3/name", "coretemp\n")
	write("sys/class/hwmon/hwmon3/temp1_input", "55000\n")
	write("sys/class/hwmon/hwmon3/temp2_input", "71000\n")
	write("sys/class/hwmon/hwmon1/name", "nvme\n")
	write("sys/class/hwmon/hwmon1/temp1_input", "99000\n")
	if v, ok := h.CPUTemp(); !ok || v != 71 {
		t.Fatalf("CPUTemp = %v, %v (the hottest coretemp sensor, not another device)", v, ok)
	}
	if !h.OnAC() {
		t.Fatal("no supply information means mains")
	}
	write("sys/class/power_supply/ADP0/online", "0\n")
	if h.OnAC() {
		t.Fatal("ADP0 says battery")
	}
	h.Exec = func(ctx context.Context, name string, args ...string) ([]byte, error) { return []byte("63\n"), nil }
	if v, ok := h.GPUTemp(); !ok || v != 63 {
		t.Fatalf("GPUTemp = %v, %v", v, ok)
	}
	h.Exec = func(ctx context.Context, name string, args ...string) ([]byte, error) {
		return nil, errors.New("no nvidia-smi")
	}
	if _, ok := h.GPUTemp(); ok {
		t.Fatal("a missing nvidia-smi is not a temperature")
	}
}
