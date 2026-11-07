// Package audio drives the laptop's sound through PipeWire/WirePlumber (pactl, wpctl,
// systemctl --user), ALSA's mixer (amixer, read-mostly) and AcerUltraX (acerultrax-cli): output
// switching between speakers and headphones, "there is no sound" detection and repair, the
// microphone, the headset jack, Purified Voice (RNNoise noise reduction on PipeWire, one filter
// chain per input) and the DTS sound.
//
// Acer's Purified Voice runs a proprietary Realtek model that cannot run on Linux. This builds an
// open equivalent: one PipeWire filter chain per input (headset mic, mic array) with RNNoise
// voice-activity gating. Modes: off | conference (gentle) | personal (strict; can clip very soft
// words).
//
// Hard rule: nothing here ever writes the HDA codec (`reconfig`, `hwC*D*` files, hda-verb). On
// this laptop that hangs the audio until a reboot. The Controller refuses any command that
// mentions them, whatever the caller asks for, and the tests prove it.
//
// Every external program goes through the injected Runner, every file through an injected
// path, every pause through an injected clock, so all of it is tested against fakes; parsing of
// the tools' output is in pure functions (parse.go).
package audio

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os"
	"os/exec"
	"regexp"
	"strings"
	"time"

	"github.com/Order52/truesense/internal/config"
)

// Result is what a finished program left behind. Code 127 stands for "could not run it"
// (not installed, killed on a timeout), with the reason in Stderr.
type Result struct {
	Stdout string
	Stderr string
	Code   int
}

// Runner runs one external program to completion. It never returns an error: a program that
// cannot start is a Result with Code 127, which is how every caller treats it.
type Runner func(ctx context.Context, name string, args ...string) Result

// ExecRunner is the production Runner.
func ExecRunner() Runner {
	return func(ctx context.Context, name string, args ...string) Result {
		var stdout, stderr strings.Builder
		cmd := exec.CommandContext(ctx, name, args...) // #nosec G204 -- fixed audio tools with arguments built by this package
		cmd.Stdout, cmd.Stderr = &stdout, &stderr
		err := cmd.Run()
		var exit *exec.ExitError
		switch {
		case err == nil:
			return Result{Stdout: stdout.String(), Stderr: stderr.String()}
		case errors.As(err, &exit) && ctx.Err() == nil:
			return Result{Stdout: stdout.String(), Stderr: stderr.String(), Code: exit.ExitCode()}
		case errors.Is(err, exec.ErrNotFound):
			return Result{Stderr: name + ": not installed", Code: 127}
		}
		return Result{Stdout: stdout.String(), Stderr: err.Error(), Code: 127}
	}
}

// OSD shows on-screen notifications. internal/osd satisfies it; nil means "no notifications".
type OSD interface {
	// Show displays text with an icon name.
	Show(text, icon string)
	// Quiet silences the desktop shell's own popups while a profile flip makes PipeWire
	// recreate devices; the returned function re-enables them.
	Quiet() (end func())
}

// UserState is the small per-user memory; *userstate.Store satisfies it.
type UserState interface {
	GetInto(key string, out any) error
	Set(key string, value any) error
}

// Controller is the sound system, as one value that carries its dependencies. Build it with New
// and override fields in tests.
type Controller struct {
	Run   Runner
	Cfg   func() *config.Config // read on every use, like the 0.3.x config.load(): a change applies without a restart
	State UserState
	OSD   OSD
	Log   *slog.Logger

	Getenv func(string) string
	// Sleep waits for d or until ctx is done.
	Sleep func(ctx context.Context, d time.Duration)
	// Now is a monotonic clock for timeouts.
	Now func() time.Time
	// Lock serialises the output cycle between processes (two quick key presses must not
	// interleave); the returned function releases it.
	Lock func() (unlock func(), err error)

	ProcCards     string        // /proc/asound/cards
	ConfDir       string        // where PipeWire reads its drop-in configuration
	Plugin        string        // the RNNoise LADSPA plugin, "" when it is not installed
	DTSBinary     func() string // where acerultrax-cli is, "" when it is not installed
	DTSConfigPath string        // AcerUltraX's own config (its headphone EQ list)
	CallTimeout   time.Duration // the most one pactl/wpctl/amixer/systemctl call may take
}

// Card constants of this laptop's sound card (Intel SOF HDA DSP) and its UCM profiles.
const (
	cardMatch = "pci-0000_00_1f.3-platform-skl_hda_dsp_generic"
	frontSink = "acerultrax_sink" // AcerUltraX's virtual sink: it stays the default so DTS keeps working
)

var profileOf = map[string]string{"speakers": "HiFi (Mic1, Mic2, Speaker)", "headphones": "HiFi (Headphones, Mic1, Mic2)"}

// Label is the human name of an output ("speakers" or "headphones").
func Label(which string) string {
	if which == "headphones" {
		return "Headphones (3.5 mm jack)"
	}
	return "Laptop speakers"
}

// Icon is the OSD icon of an output.
func Icon(which string) string {
	if which == "headphones" {
		return "audio"
	}
	return "speaker"
}

// ModeLabel is the name Acer's Purified Voice modes have in the OSD.
func ModeLabel(mode string) string {
	switch mode {
	case "off":
		return "All Off"
	case "personal":
		return "Personal Call"
	case "conference":
		return "Conference Call"
	}
	return mode
}

// New returns a Controller for the real machine: real programs, /proc, the user's PipeWire
// configuration directory, and the config files. state may be nil (no memory of seen DTS
// outputs); osd may be nil.
func New(state UserState, osd OSD) *Controller {
	home, _ := os.UserHomeDir()
	c := &Controller{
		Run: ExecRunner(), State: state, OSD: osd, Log: slog.Default(), Getenv: os.Getenv, Now: time.Now,
		Sleep: func(ctx context.Context, d time.Duration) {
			t := time.NewTimer(d)
			defer t.Stop()
			select {
			case <-t.C:
			case <-ctx.Done():
			}
		},
		Cfg: func() *config.Config {
			if cfg, err := config.LoadDefault(); err == nil {
				return cfg
			}
			return config.Default()
		},
		ProcCards: "/proc/asound/cards", ConfDir: home + "/.config/pipewire/pipewire.conf.d", DTSConfigPath: home + "/.config/acerultrax/config.toml",
		CallTimeout: 30 * time.Second,
	}
	c.Plugin = firstExisting("/usr/lib/ladspa/librnnoise_ladspa.so", "/usr/lib64/ladspa/librnnoise_ladspa.so", "/usr/lib/x86_64-linux-gnu/ladspa/librnnoise_ladspa.so")
	c.DTSBinary = HostDTSLocator(exec.LookPath, os.Getenv)
	c.Lock = func() (func(), error) { return FileLock(fmt.Sprintf("/tmp/truesense-output-%d.lock", os.Getuid())) }
	return c
}

func firstExisting(paths ...string) string {
	for _, p := range paths {
		if _, err := os.Stat(p); err == nil {
			return p
		}
	}
	return ""
}

func (c *Controller) log() *slog.Logger {
	if c.Log != nil {
		return c.Log
	}
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}

func (c *Controller) say(text, icon string) {
	if c.OSD != nil {
		c.OSD.Show(text, icon)
	}
}

func (c *Controller) quiet() (end func()) {
	if c.OSD == nil {
		return func() {}
	}
	return c.OSD.Quiet()
}

func (c *Controller) sleep(ctx context.Context, d time.Duration) { c.Sleep(ctx, d) }

// noctaliaStyle reports whether notifications go to the Noctalia shell, which wants its popup
// AFTER the (slow) switch instead of before it.
func (c *Controller) noctaliaStyle() bool {
	style := c.Getenv("TRUESENSE_OSD")
	if style == "" {
		style = c.Cfg().OSD.Style
	}
	return style == "noctalia"
}

var codecNodeRE = regexp.MustCompile(`(?i)hwC\d+D\d+`)

// forbidden reports why a command must never run: it would touch the HDA codec (its
// `reconfig` file, a hwC*D* node, hda-verb).
func forbidden(name string, args []string) string {
	switch name {
	case "hda-verb", "hda-jack-retask":
		return name + " writes codec verbs"
	}
	for _, a := range append([]string{name}, args...) {
		if strings.Contains(strings.ToLower(a), "reconfig") || codecNodeRE.MatchString(a) {
			return "the HDA codec is never written (reconfig / hwC*D* hang the audio until reboot)"
		}
	}
	return ""
}

// run runs a program under CallTimeout, after refusing anything that would touch the codec.
func (c *Controller) run(ctx context.Context, name string, args ...string) Result {
	if why := forbidden(name, args); why != "" {
		c.log().Error("refused to run a codec command", "command", name, "why", why)
		return Result{Stderr: "refused: " + why, Code: 126}
	}
	if c.CallTimeout > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, c.CallTimeout)
		defer cancel()
	}
	return c.Run(ctx, name, args...)
}

// out runs a program and returns its standard output (empty when it failed).
func (c *Controller) out(ctx context.Context, name string, args ...string) string {
	return c.run(ctx, name, args...).Stdout
}
