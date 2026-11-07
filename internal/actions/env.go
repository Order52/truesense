package actions

import (
	"context"

	"github.com/Order52/truesense/internal/client"
	"github.com/Order52/truesense/internal/config"
	"github.com/Order52/truesense/internal/hw"
	"github.com/Order52/truesense/internal/ipc"
	"github.com/Order52/truesense/internal/looks"
)

// Power is how power modes and fans are set: *client.Client (through the daemon when it runs,
// on the hardware otherwise).
type Power interface {
	Power(mode string, holdMinutes float64) (string, error)
	Fan(r client.FanRequest) error
}

// Hardware is the firmware features the actions read and toggle: *hw.HW.
type Hardware interface {
	GetMode() (string, error)
	SupportedModes() ([]string, error)
	GetFanState() (hw.FanState, error)
	RearGet() (hw.Rear, error)
	RearSet(o hw.RearOpts) (hw.Rear, error)
	GetBatteryLimit() (bool, error)
	SetBatteryLimit(on bool) (bool, error)
	GetOverdrive() (on, ok bool, err error)
	SetOverdrive(on bool) (bool, error)
}

// Keyboard is the backlight controller: *keyboard.Keyboard.
type Keyboard interface {
	On() error
	Off() error
	RestoreDefault() error
	Brightness() (int, error)
	SetEffect(name, color string, speed, brightness, direction int) error
}

// KeyProfiles are the saved per-key lighting profiles: *keyprofiles.Store.
type KeyProfiles interface {
	Exists(name string) bool
	Apply(name string, brightness int) error
}

// Looks are the saved lighting looks: *looks.Store.
type Looks interface {
	Names() []string
	Load(name string) (*looks.Look, error)
	Apply(look *looks.Look) ([]string, error)
}

// Audio is the sound system: *audio.Controller. Its output actions show their own on-screen
// message (the switch takes about a second, so the message comes first), which is why those
// actions return no text.
type Audio interface {
	OutputToggle(ctx context.Context) (string, error)
	OutputCycle(ctx context.Context) (string, error)
	SetOutput(ctx context.Context, which string) (string, error)
	ToggleMicMute(ctx context.Context, defaultOnly bool) (bool, error)
	SetMicMute(ctx context.Context, muted bool) error
	VoiceNext(ctx context.Context, dev string) (device, mode string, err error)
}

// Touchpad is the kernel's per-device switch: *touchpad.Switch. Only root can write it, so a
// user's key press goes through the daemon and this is the fallback for root without one.
type Touchpad interface {
	Enabled() (on, ok bool)
	Set(on bool) (bool, error)
}

// Daemon is the control socket: *ipc.Client.
type Daemon interface {
	CallInto(cmd string, args ipc.Args, out any) error
}

// Profiles are the whole-setup profiles: *profiles.Store through an adapter that knows the
// action runner (profiles apply their power, fan and lighting settings by running actions,
// so the dependency is wired in the application, not imported here).
type Profiles interface {
	Apply(ctx context.Context, name string) (string, error)
	NextName() (string, error)
}

// State is the small per-user memory: *userstate.Store.
type State interface {
	Get(key string) any
	Set(key string, value any) error
}

// Env is everything the actions reach. Every field is required except Touchpad (nil means
// "not available"), Exec (nil refuses exec: actions) and IsRoot (nil means "not root").
type Env struct {
	// Config is read on every action, so an edit of the configuration applies without
	// restarting the key service.
	Config      func() (*config.Config, error)
	Power       Power
	HW          Hardware
	Keyboard    Keyboard
	KeyProfiles KeyProfiles
	Looks       Looks
	Audio       Audio
	Touchpad    Touchpad
	Daemon      Daemon
	Profiles    Profiles
	State       State
	// Exec starts an exec: command. It must run as the calling user and return as soon as the
	// command is started (ShellExec does).
	Exec   func(ctx context.Context, command string) error
	IsRoot func() bool
}
