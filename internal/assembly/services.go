// Package assembly builds the running program: it constructs every concrete component once
// (Services), adapts packages that are deliberately independent of each other, and starts the
// daemon with its real dependencies. The command line, the terminal UI and the window take what
// they need from Services; nothing below this package knows about it.
package assembly

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/Order52/truesense/internal/audio"
	"github.com/Order52/truesense/internal/client"
	"github.com/Order52/truesense/internal/config"
	"github.com/Order52/truesense/internal/curves"
	"github.com/Order52/truesense/internal/designs"
	"github.com/Order52/truesense/internal/firmware"
	"github.com/Order52/truesense/internal/gpuundervolt"
	"github.com/Order52/truesense/internal/hw"
	"github.com/Order52/truesense/internal/ipc"
	"github.com/Order52/truesense/internal/keyboard"
	"github.com/Order52/truesense/internal/keyprofiles"
	"github.com/Order52/truesense/internal/looks"
	"github.com/Order52/truesense/internal/osd"
	"github.com/Order52/truesense/internal/power"
	"github.com/Order52/truesense/internal/powermgr"
	"github.com/Order52/truesense/internal/sensors"
	"github.com/Order52/truesense/internal/smartprofile"
	"github.com/Order52/truesense/internal/touchpad"
	"github.com/Order52/truesense/internal/undervolt"
	"github.com/Order52/truesense/internal/userstate"
)

// Services is every concrete component of the running program, built once from the
// configuration and the environment. The command line, the terminal UI, the window and the
// daemon all take what they need from it; the packages below this one know nothing of each other.
type Services struct {
	Cfg    *config.Config
	Issues []string // problems found in the configuration (the defaults were used instead)

	User     *userstate.Store
	Dev      firmware.Device
	HW       *hw.HW
	KB       *keyboard.Keyboard
	Looks    *looks.Store
	KeyProf  *keyprofiles.Store
	Designs  *designs.Store
	OSD      *osd.Notifier
	Audio    *audio.Controller
	IPC      *ipc.Client
	Client   *client.Client
	Curves   *curves.Store
	Smart    *smartprofile.Store
	UVStore  *undervolt.Store
	GPUStore *gpuundervolt.Store
	PM       *powermgr.Manager
	Power    *power.Controller
	UV       *undervolt.Controller
	GPUUV    *gpuundervolt.Controller
	Touchpad *touchpad.Switch
	Sampler  *sensors.Sampler
}

// New builds everything. A configuration with problems is reported in Issues and
// replaced, as a whole, by the built-in defaults (keeping the configured user), so the program
// still starts.
func New() *Services {
	s := &Services{}
	cfg, err := config.LoadDefault()
	if err != nil {
		s.Issues = append(s.Issues, err.Error())
		cfg = config.Default()
		cfg.User = envUser()
	}
	if p := cfg.Validate(); len(p) > 0 {
		s.Issues = append(s.Issues, p...)
		def := config.Default()
		def.User = cfg.User
		cfg = def
	}
	s.Cfg = cfg
	s.User = userstate.New()
	s.Dev = firmware.FromEnv()
	s.HW = hw.New(s.Dev, s.User)
	s.KB = keyboard.FromEnv(s.User)
	s.HW.KBT = s.KB.Timeout()
	s.Looks = looks.New(s.User, s.KB, s.HW)
	s.KeyProf = keyprofiles.New(keyprofiles.DefaultDir(), s.KB)
	s.Designs = designs.New(designs.DefaultDir())
	s.OSD = osd.New(cfg.OSD.Style, cfg.User)
	s.Audio = audio.New(s.User, audioOSD{s.OSD})
	s.IPC = &ipc.Client{}
	s.Client = client.New(s.HW)
	s.Curves = curves.NewStore()
	s.Smart = smartprofile.NewStore()
	s.UVStore = undervolt.NewStore()
	s.GPUStore = gpuundervolt.NewStore()
	s.PM = powermgr.New()
	s.UV = undervolt.New("")
	s.GPUUV = gpuundervolt.New(gpuundervolt.OpenNVML)
	s.Power = &power.Controller{MSR: undervolt.DevMSR{}, Run: execPower}
	s.Touchpad = touchpad.New()
	s.Sampler = sensors.New()
	return s
}

func envUser() string {
	for _, k := range []string{"SUDO_USER", "USER"} {
		if v := os.Getenv(k); v != "" {
			return v
		}
	}
	return ""
}

// audioOSD gives the audio controller the two things it asks of the on-screen display.
type audioOSD struct{ n *osd.Notifier }

func (a audioOSD) Show(text, icon string) { a.n.Show(text, icon) }
func (a audioOSD) Quiet() func()          { return a.n.Quiet(800 * time.Millisecond) }

// execPower runs nvidia-smi for the power-limit controller.
func execPower(ctx context.Context, name string, args ...string) (power.Result, error) {
	var out, errb strings.Builder
	cmd := exec.CommandContext(ctx, name, args...)
	cmd.Stdout, cmd.Stderr = &out, &errb
	err := cmd.Run()
	code := 0
	if ee, ok := err.(*exec.ExitError); ok {
		code, err = ee.ExitCode(), nil
	}
	return power.Result{Stdout: out.String(), Stderr: errb.String(), ExitCode: code}, err
}

// legacyFlag reports whether an old status-bar widget's flag file exists in the user's cache.
func legacyFlag(home string) func(string) bool {
	return func(name string) bool {
		_, err := os.Stat(filepath.Join(home, ".cache", name))
		return err == nil
	}
}
