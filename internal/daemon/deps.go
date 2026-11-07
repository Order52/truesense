// Package daemon is the TrueSense service (root): the smart fan curve, the thermal governor,
// CPU power limits, the automatic power profile and the control socket. It is the wiring of the
// decision packages (fanctl, autoprofile, thermal, ...) to the hardware and the user's settings.
//
// Safety: the fans go back to firmware Auto and the clocks are uncapped on ANY exit (stop,
// signal, panic); if the control loop breaks while the machine is hot the fans go to 100 %;
// a missing or failing device never kills the daemon (it keeps waiting and retrying).
package daemon

import (
	"context"
	"log/slog"
	"time"

	"github.com/Order52/truesense/internal/chargefull"
	"github.com/Order52/truesense/internal/config"
	"github.com/Order52/truesense/internal/crashguard"
	"github.com/Order52/truesense/internal/curves"
	"github.com/Order52/truesense/internal/gamerules"
	"github.com/Order52/truesense/internal/gpuundervolt"
	"github.com/Order52/truesense/internal/hw"
	"github.com/Order52/truesense/internal/looks"
	"github.com/Order52/truesense/internal/power"
	"github.com/Order52/truesense/internal/powermgr"
	"github.com/Order52/truesense/internal/sensors"
	"github.com/Order52/truesense/internal/smartprofile"
	"github.com/Order52/truesense/internal/state"
	"github.com/Order52/truesense/internal/undervolt"
)

// Hardware is the firmware as the daemon uses it (satisfied by *hw.HW).
type Hardware interface {
	ReadSensors() (hw.Sensors, error)
	GetMode() (string, error)
	SetMode(name string) (string, error)
	GetFanState() (hw.FanState, error)
	FanAuto() error
	FanMax() error
	FanManual(cpu, gpu int) error
	GetBatteryLimit() (bool, error)
	SetBatteryLimit(on bool) (bool, error)
	OnAC() bool
}

// Sampler is what the daemon reads from the machine (satisfied by *sensors.Sampler).
type Sampler interface {
	CPULoad() float64
	PackageTemp() (float64, bool)
	BatteryPct() float64
	GPU() sensors.Reading
	StartGPU(ctx context.Context)
}

// ClockCap writes the CPU's maximum clock (satisfied by thermal.Writer).
type ClockCap interface {
	Apply(khz int)
	Uncap()
}

// PowerLimits is the CPU package power limit (satisfied by *power.Controller).
type PowerLimits interface {
	Info(ctx context.Context) power.Info
	ReadRaw() (power.RawLimits, error)
	WriteRaw(power.RawLimits) error
	SetLimits(pl1, pl2 float64) (power.Applied, error)
}

// CPUUndervolt is the CPU voltage offset (satisfied by *undervolt.Controller).
type CPUUndervolt interface {
	Read() (undervolt.Offsets, error)
	Apply(coreMV, cacheMV int, confirm bool) (undervolt.Offsets, error)
	Reset() (undervolt.Offsets, error)
}

// GPUUndervolt is the GPU curve offset and clock cap (satisfied by *gpuundervolt.Controller).
type GPUUndervolt interface {
	Read() (gpuundervolt.State, error)
	Apply(offset, capMHz int) (gpuundervolt.Applied, error)
	Reset() (gpuundervolt.Applied, error)
}

// PowerManagers is the detection and takeover of conflicting managers (satisfied by *powermgr.Manager).
type PowerManagers interface {
	Status(ctx context.Context) []powermgr.Unit
	Takeover(ctx context.Context) (powermgr.TakeoverReport, error)
	Restore(ctx context.Context) powermgr.RestoreReport
	Recorded() bool
}

// Curves loads the saved fan curves and Profiles the saved Smart auto profiles.
type (
	// Curves lists and loads fan curves.
	Curves interface {
		Load(name string) (curves.Curve, error)
		Names() ([]string, error)
	}
	// Profiles lists and loads Smart auto profiles.
	Profiles interface {
		Load(name string) (smartprofile.Profile, error)
		Names() ([]string, error)
	}
)

// BootLooks reads and applies the lighting chosen for boot (satisfied by *looks.Store).
type BootLooks interface {
	ReadBoot() *looks.Look
	WriteBoot(*looks.Look) error
	Apply(*looks.Look) ([]string, error)
}

// OperatingLight re-sends the keyboard's operating-mode colour (satisfied by *keyboard.Keyboard).
type OperatingLight interface {
	FollowMode(mode string, colors map[string]string) error
}

// Notifier shows an on-screen message (satisfied by *osd.Notifier).
type Notifier interface {
	Show(text, icon string) bool
}

// Touchpad switches the touchpad (satisfied by *touchpad.Switch).
type Touchpad interface {
	Enabled() (on, ok bool)
	Set(on bool) (bool, error)
}

// Clock gives the wall time (it can jump: suspend, NTP) and a monotonic time in seconds.
type Clock struct {
	Wall func() time.Time
	Mono func() float64
}

// RealClock is the machine's clock.
func RealClock() Clock {
	start := time.Now()
	return Clock{Wall: time.Now, Mono: func() float64 { return time.Since(start).Seconds() }}
}

// Deps is everything the daemon touches outside itself. Every field is replaced by a fake in
// the tests; Production fills them for the real machine.
type Deps struct {
	Config   *config.Config
	State    *state.Store
	HW       Hardware
	Sensors  Sampler
	Cap      ClockCap
	Power    PowerLimits
	UV       CPUUndervolt
	GPUUV    GPUUndervolt
	PM       PowerManagers
	Curves   Curves
	Profiles Profiles
	Looks    BootLooks
	Lights   OperatingLight
	OSD      Notifier
	Touchpad Touchpad
	Procs    func() []gamerules.Process
	UVGuard  *crashguard.Guard
	GPUGuard *crashguard.Guard
	Clock    Clock
	Log      *slog.Logger

	RunDir      string // /run/truesense: the live state file health.sh watches
	TmpState    string // /tmp/truesense-state: a copy for status bars whose sandbox cannot see /run
	LegacyFlag  func(name string) bool
	CPURAPLRoot string
}

// FullCharge is the persisted state of "charge to 100 % once" (see package chargefull).
type FullCharge = chargefull.State
