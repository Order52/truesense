package daemon

import (
	"context"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/Order52/truesense/internal/chargefull"
	"github.com/Order52/truesense/internal/config"
	"github.com/Order52/truesense/internal/crashguard"
	"github.com/Order52/truesense/internal/curves"
	"github.com/Order52/truesense/internal/firmware"
	"github.com/Order52/truesense/internal/gamerules"
	"github.com/Order52/truesense/internal/gpuundervolt"
	"github.com/Order52/truesense/internal/hw"
	"github.com/Order52/truesense/internal/ipc"
	"github.com/Order52/truesense/internal/looks"
	"github.com/Order52/truesense/internal/power"
	"github.com/Order52/truesense/internal/powermgr"
	"github.com/Order52/truesense/internal/sensors"
	"github.com/Order52/truesense/internal/smartprofile"
	"github.com/Order52/truesense/internal/state"
	"github.com/Order52/truesense/internal/undervolt"
)

// ---- fakes ----

type fakeSampler struct {
	mu      sync.Mutex
	load    float64
	pkg     float64
	havePkg bool
	batt    float64
	gpu     sensors.Reading
}

func (f *fakeSampler) CPULoad() float64 { f.mu.Lock(); defer f.mu.Unlock(); return f.load }
func (f *fakeSampler) PackageTemp() (float64, bool) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.pkg, f.havePkg
}
func (f *fakeSampler) BatteryPct() float64      { f.mu.Lock(); defer f.mu.Unlock(); return f.batt }
func (f *fakeSampler) GPU() sensors.Reading     { f.mu.Lock(); defer f.mu.Unlock(); return f.gpu }
func (f *fakeSampler) StartGPU(context.Context) {}

type fakeCap struct {
	caps     []int
	uncapped int
}

func (f *fakeCap) Apply(k int) { f.caps = append(f.caps, k) }
func (f *fakeCap) Uncap()      { f.uncapped++ }

type fakePower struct {
	raw     power.RawLimits
	written []power.RawLimits
	set     [][2]float64
}

func (f *fakePower) Info(context.Context) power.Info   { return power.Info{} }
func (f *fakePower) ReadRaw() (power.RawLimits, error) { return f.raw, nil }
func (f *fakePower) WriteRaw(l power.RawLimits) error {
	f.raw = l
	f.written = append(f.written, l)
	return nil
}
func (f *fakePower) SetLimits(a, b float64) (power.Applied, error) {
	f.set = append(f.set, [2]float64{a, b})
	f.raw = power.RawLimits{PL1: int64(a * 1e6), PL2: int64(b * 1e6)}
	return power.Applied{PL1W: a, PL2W: b}, nil
}

type fakeUV struct {
	core, cache int
	applied     [][3]int
	failApply   bool
}

func (f *fakeUV) Read() (undervolt.Offsets, error) {
	return undervolt.Offsets{CoreMV: f.core, CacheMV: f.cache}, nil
}
func (f *fakeUV) Apply(c, h int, confirm bool) (undervolt.Offsets, error) {
	if f.failApply {
		return undervolt.Offsets{}, firmware.Errorf("the CPU kept its stock voltage")
	}
	f.core, f.cache = c, h
	f.applied = append(f.applied, [3]int{c, h, b2i(confirm)})
	return undervolt.Offsets{CoreMV: c, CacheMV: h}, nil
}
func (f *fakeUV) Reset() (undervolt.Offsets, error) {
	f.core, f.cache = 0, 0
	return undervolt.Offsets{}, nil
}

func b2i(b bool) int {
	if b {
		return 1
	}
	return 0
}

type fakeGPUUV struct{ offset, capMHz int }

func (f *fakeGPUUV) Read() (gpuundervolt.State, error) {
	return gpuundervolt.State{OffsetMHz: f.offset, RangeMHz: [2]int{-300, 250}}, nil
}
func (f *fakeGPUUV) Apply(o, c int) (gpuundervolt.Applied, error) {
	f.offset, f.capMHz = o, c
	return gpuundervolt.Applied{OffsetMHz: o, CapMHz: c}, nil
}
func (f *fakeGPUUV) Reset() (gpuundervolt.Applied, error) {
	f.offset, f.capMHz = 0, 0
	return gpuundervolt.Applied{}, nil
}

type fakePM struct {
	found    []powermgr.Unit
	recorded bool
}

func (f *fakePM) Status(context.Context) []powermgr.Unit { return f.found }
func (f *fakePM) Takeover(context.Context) (powermgr.TakeoverReport, error) {
	f.recorded = true
	r := powermgr.TakeoverReport{}
	for _, u := range f.found {
		r.Stopped = append(r.Stopped, u.Unit)
	}
	f.found = nil
	return r, nil
}
func (f *fakePM) Restore(context.Context) powermgr.RestoreReport {
	f.recorded = false
	return powermgr.RestoreReport{}
}
func (f *fakePM) Recorded() bool { return f.recorded }

type fakeLooks struct {
	boot    *looks.Look
	applied []*looks.Look
}

func (f *fakeLooks) ReadBoot() *looks.Look         { return f.boot }
func (f *fakeLooks) WriteBoot(l *looks.Look) error { f.boot = l; return nil }
func (f *fakeLooks) Apply(l *looks.Look) ([]string, error) {
	f.applied = append(f.applied, l)
	return []string{"keyboard"}, nil
}

type fakeTouchpad struct{ on bool }

func (f *fakeTouchpad) Enabled() (bool, bool)     { return f.on, true }
func (f *fakeTouchpad) Set(on bool) (bool, error) { f.on = on; return on, nil }

type fakeOSD struct{ shown []string }

func (f *fakeOSD) Show(t, _ string) bool { f.shown = append(f.shown, t); return true }

// ---- the laptop ----

type rig struct {
	t        *testing.T
	dm       *Daemon
	sim      *firmware.Simulator
	hw       *hw.HW
	samp     *fakeSampler
	cap      *fakeCap
	pow      *fakePower
	uv       *fakeUV
	gpuuv    *fakeGPUUV
	pm       *fakePM
	looks    *fakeLooks
	tp       *fakeTouchpad
	osd      *fakeOSD
	st       *state.Store
	run      string
	wall     time.Time
	mono     float64
	ac       bool
	procs    []gamerules.Process
	uvGuard  *crashguard.Guard
	gpuGuard *crashguard.Guard
}

// acHW lets a test plug and unplug the charger.
type acHW struct {
	*hw.HW
	ac *bool
}

func (a acHW) OnAC() bool { return *a.ac }

func newRig(t *testing.T) *rig {
	t.Helper()
	dir := t.TempDir()
	sim := firmware.NewSimulator()
	h := hw.New(sim, nil)
	h.SleepFor = func(time.Duration) {}
	r := &rig{t: t, sim: sim, hw: h, samp: &fakeSampler{batt: 80, havePkg: false}, cap: &fakeCap{}, pow: &fakePower{raw: power.RawLimits{PL1: 80e6, PL2: 100e6}}, uv: &fakeUV{}, gpuuv: &fakeGPUUV{},
		pm: &fakePM{}, looks: &fakeLooks{}, tp: &fakeTouchpad{on: true}, osd: &fakeOSD{}, run: filepath.Join(dir, "run"), wall: time.Unix(1_800_000_000, 0), ac: true}
	cfg := config.Default()
	cfg.User = "tester"
	cfg.Daemon.AutoProfile = true
	r.st = state.Open(filepath.Join(dir, "state.json"), map[string]any{"smart_fan": true, "auto_profile": true, "auto_mode": "native", "fan_curve": "native", "fan_curve_data": nil, "pl_user": nil,
		"smart_profile": "native", "smart_profile_data": nil, "operating_follow": false, "profile_hold_until": 0.0})
	r.uvGuard = crashguard.New(filepath.Join(dir, "uv-pending"))
	r.gpuGuard = crashguard.New(filepath.Join(dir, "gpu-uv-pending"))
	r.dm = New(Deps{Config: cfg, State: r.st, HW: acHW{h, &r.ac}, Sensors: r.samp, Cap: r.cap, Power: r.pow, UV: r.uv, GPUUV: r.gpuuv, PM: r.pm,
		Curves: &curves.Store{Dir: filepath.Join(dir, "curves")}, Profiles: &smartprofile.Store{Dir: filepath.Join(dir, "smart")}, Looks: r.looks, OSD: r.osd, Touchpad: r.tp,
		Procs: func() []gamerules.Process { return r.procs }, UVGuard: r.uvGuard, GPUGuard: r.gpuGuard,
		Clock: Clock{Wall: func() time.Time { return r.wall }, Mono: func() float64 { return r.mono }}, RunDir: r.run, TmpState: filepath.Join(dir, "tmp-state")})
	return r
}

// drive runs n steps of one second at the given readings (CPU/GPU temperature in C, CPU load in %).
func (r *rig) drive(n int, cpu, gpu int, load float64) {
	r.t.Helper()
	for i := 0; i < n; i++ {
		r.sim.SetSensors([3]int{cpu, 55, gpu}, [2]int{0, 0})
		r.samp.mu.Lock()
		r.samp.load = load
		r.samp.mu.Unlock()
		r.dm.step()
		r.wall = r.wall.Add(time.Second)
		r.mono++
	}
}

// call sends a request through the real wire handler (the same path a client takes).
func (r *rig) call(cmd string, args ipc.Args) (any, error) {
	r.t.Helper()
	h, ok := r.dm.Handlers()[cmd]
	if !ok {
		r.t.Fatalf("no handler %q", cmd)
	}
	return h(context.Background(), args)
}

func (r *rig) mustCall(cmd string, args ipc.Args) any {
	r.t.Helper()
	v, err := r.call(cmd, args)
	if err != nil {
		r.t.Fatalf("%s %v: %v", cmd, args, err)
	}
	return v
}

func (r *rig) snap() firmware.Snapshot { return r.sim.Snapshot() }

func exists(p string) bool { _, err := os.Stat(p); return err == nil }

var _ = chargefull.MaxDuration

type hwSensors = hw.Sensors

type panicSampler struct{}

func (panicSampler) CPULoad() float64             { panic("sensor exploded") }
func (panicSampler) PackageTemp() (float64, bool) { return 0, false }
func (panicSampler) BatteryPct() float64          { return 100 }
func (panicSampler) GPU() sensors.Reading         { return sensors.Reading{} }
func (panicSampler) StartGPU(context.Context)     {}

// procsNamed builds a process list holding one process of that name.
func procsNamed(name string) []gamerules.Process {
	return []gamerules.Process{{Comm: name, Cmdline: []byte(name + "\x00")}}
}
