// Package doctor is `truesense doctor`: it checks the whole stack and says how to fix what is
// wrong. It is read-only. A check returns a status, a detail line and a fix; the exit code is
// 1 if anything FAILs (WARN does not fail).
//
// Every probe is injected through Env: files are read below a root path, programs run through
// a Runner, the firmware through the hw interfaces, the daemon and the sound system through
// small interfaces, so the whole health check runs in a test against a temporary tree and
// fakes. The titles and wording are the 0.3.x ones, because users and the documentation refer
// to them.
package doctor

import (
	"context"
	"os"
	"os/exec"
	"os/user"
	"path/filepath"
	"strconv"
	"time"

	"golang.org/x/sys/unix"

	"github.com/Order52/truesense/internal/buildinfo"
	"github.com/Order52/truesense/internal/config"
	"github.com/Order52/truesense/internal/hw"
	"github.com/Order52/truesense/internal/services"
)

// Status grades one check.
type Status string

// The four grades. INFO never counts as a problem.
const (
	OK   Status = "OK"
	WARN Status = "WARN"
	FAIL Status = "FAIL"
	INFO Status = "INFO"
)

// Outcome is what one check found: its grade, one line of detail and how to fix it.
type Outcome struct {
	Status Status
	Detail string
	Fix    string
}

func ok(detail string) Outcome        { return Outcome{Status: OK, Detail: detail} }
func info(detail, fix string) Outcome { return Outcome{Status: INFO, Detail: detail, Fix: fix} }
func warn(detail, fix string) Outcome { return Outcome{Status: WARN, Detail: detail, Fix: fix} }
func fail(detail, fix string) Outcome { return Outcome{Status: FAIL, Detail: detail, Fix: fix} }
func graded(good bool, detail, fix string) Outcome {
	if good {
		return ok(detail)
	}
	return warn(detail, fix)
}

// Check is one named probe. The title is what the report shows.
type Check struct {
	Title string
	Run   func(ctx context.Context, e *Env) Outcome
}

// Firmware is the part of hw.HW the checks read. *hw.HW satisfies it.
type Firmware interface {
	Capabilities() (map[string]bool, error)
	GetFanState() (hw.FanState, error)
}

// Settings and Live are the parts of the daemon's state the checks look at. The JSON names are
// the daemon's.
type (
	// Settings are the switches the daemon runs by.
	Settings struct {
		SmartFan    bool `json:"smart_fan"`
		AutoProfile bool `json:"auto_profile"`
	}
	// Live is what the daemon last measured.
	Live struct {
		HWOK     *bool  `json:"hw_ok"` // nil = not reported = fine
		Activity string `json:"activity"`
	}
	// DaemonState is the answer to the daemon's "state" command.
	DaemonState struct {
		Live     Live     `json:"live"`
		Settings Settings `json:"settings"`
	}
)

// Daemon is the control socket as the checks use it; internal/ipc's client satisfies it
// through a thin adapter.
type Daemon interface {
	State(ctx context.Context) (DaemonState, error)
	// Call sends a command and discards the answer: it is how the version probe asks for
	// commands a stale service does not know.
	Call(ctx context.Context, command string, args map[string]any) error
}

// Audio is the sound system as the Audio check sees it. internal/audio's Controller has these
// methods; the interface lives here because doctor sits below audio in the layering.
type Audio interface {
	HasRNNoise() bool
	HasDTS() bool
	// JackStatus reports what the codec sees on the 3.5 mm jack.
	JackStatus(ctx context.Context) (headphones, headsetMic bool, err error)
	// SoundCheck judges the card: known is false when there is no card to judge; problem
	// says what is wrong when known and not ok.
	SoundCheck(ctx context.Context) (known, ok bool, problem string, err error)
	// PlainHeadsets lists the labels of headset-like outputs that AcerUltraX leaves on its
	// "Plain" (no processing) profile.
	PlainHeadsets(ctx context.Context) ([]string, error)
}

// Env is everything the checks look at. Build one with NewEnv for the real machine; tests fill
// the fields they need.
type Env struct {
	Root       string // prefix of every absolute path the checks read ("" = the real filesystem)
	Run        services.Runner
	Kernel     string // uname -r
	Version    string // this build
	EUID       int
	Getenv     func(string) string
	LookPath   func(string) (string, error)
	LookupUID  func(user string) (int, error)
	Now        func() time.Time
	Firmware   Firmware
	Daemon     Daemon
	Audio      Audio
	Config     func() (*config.Config, error)
	FnKeyNode  func() string          // the keyboard's Fn-key interface, "" when not found
	CeilingLow func() []string        // cores whose frequency ceiling is below their maximum
	Access     func(path string) bool // can the caller read AND write path
	Workers    int                    // how many checks run side by side (default 8)
}

// NewEnv is the real machine: the real filesystem, real programs (each given 6 s, as the
// health check always has), the running kernel and the caller's identity. The firmware, the
// daemon and the sound system are the caller's to connect.
func NewEnv(fw Firmware, daemon Daemon, audio Audio) *Env {
	var u unix.Utsname
	_ = unix.Uname(&u)
	return &Env{
		Run: services.ExecRunner(6 * time.Second), Kernel: unix.ByteSliceToString(u.Release[:]), Version: buildinfo.Version, EUID: os.Geteuid(),
		Getenv: os.Getenv, LookPath: exec.LookPath, Now: time.Now, Firmware: fw, Daemon: daemon, Audio: audio, Config: config.LoadDefault, Workers: 8,
		LookupUID: func(name string) (int, error) {
			usr, err := user.Lookup(name)
			if err != nil {
				return 0, err
			}
			return strconv.Atoi(usr.Uid)
		},
		Access: func(path string) bool { return unix.Access(path, unix.R_OK|unix.W_OK) == nil },
	}
}

func (e *Env) path(p string) string { return filepath.Join(e.Root, p) }

func (e *Env) workers() int {
	if e.Workers > 0 {
		return e.Workers
	}
	return 8
}

// Paths the checks read, relative to Env.Root.
const (
	dkmsRoot    = "/var/lib/dkms"
	moduleName  = "truesense_wmi"
	devicePath  = "/dev/truesense"
	statePath   = "/run/truesense/state"
	modprobeCfg = "/etc/modprobe.d/truesense.conf"
)

// Checks returns every check in the order the report shows them.
func Checks() []Check {
	return []Check{
		{"Model", checkModel}, {"Kernel", checkKernel}, {"Kernel headers", checkHeaders}, {"DKMS", checkDKMS},
		{"Kernel module", checkModule}, {"Device", checkDevice}, {"Conflicts", checkConflicts}, {"Power managers", checkPowerManagers},
		{"CPU turbo", checkTurbo}, {"Daemon", checkDaemon}, {"Service version", checkServiceVersion}, {"User services", checkUserServices},
		{"udev / keyboard", checkUdev}, {"Config", checkConfig}, {"Secure Boot", checkSecureBoot}, {"Audio", checkAudio}, {"Fans", checkFans},
		{"Window", checkWindow}, {"Status bars", checkStatusBars},
	}
}
