package actions

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/Order52/truesense/internal/client"
	"github.com/Order52/truesense/internal/config"
	"github.com/Order52/truesense/internal/hw"
	"github.com/Order52/truesense/internal/ipc"
	"github.com/Order52/truesense/internal/looks"
)

// world is a whole fake machine: every call is appended to log, so a test asserts the exact
// sequence of effects.
type world struct {
	log []string

	mode      string
	supported []string
	fan       hw.FanState
	rear      hw.Rear
	battery   bool
	overdrive struct{ on, ok bool }
	bright    int

	state map[string]any

	daemonUp  bool
	daemonSet map[string]any // the daemon's settings
	touch     bool
	root      bool

	savedLooks []string
	profiles   []string
	kbProfiles map[string]bool

	micMuted bool
	output   string
	voice    [2]string
	failOn   string // a call prefix that fails
	cfg      *config.Config
}

func newWorld() *world {
	w := &world{
		mode: "balanced", supported: []string{"quiet", "balanced", "performance", "turbo", "eco"},
		fan:   hw.FanState{Mode: "auto"},
		rear:  hw.Rear{Mode: "static", Color: "112233", Speed: 3, Brightness: 100, Direction: 1},
		state: map[string]any{}, daemonUp: true, daemonSet: map[string]any{"auto_mode": "off", "smart_fan": false},
		touch: true, kbProfiles: map[string]bool{}, output: "speakers", voice: [2]string{"headset", "conference"},
		savedLooks: []string{"native", "mine"}, profiles: []string{"gaming", "quiet"},
	}
	w.cfg = config.Default()
	return w
}

func (w *world) rec(format string, a ...any) error {
	line := fmt.Sprintf(format, a...)
	w.log = append(w.log, line)
	if w.failOn != "" && strings.HasPrefix(line, w.failOn) {
		return errors.New("boom: " + line)
	}
	return nil
}

func (w *world) env() Env {
	return Env{
		Config:      func() (*config.Config, error) { return w.cfg, nil },
		Power:       (*fPower)(w),
		HW:          (*fHW)(w),
		Keyboard:    (*fKB)(w),
		KeyProfiles: (*fKP)(w),
		Looks:       (*fLooks)(w),
		Audio:       (*fAudio)(w),
		Touchpad:    (*fTouch)(w),
		Daemon:      (*fDaemon)(w),
		Profiles:    (*fProfiles)(w),
		State:       (*fState)(w),
		Exec:        func(_ context.Context, c string) error { return w.rec("exec %s", c) },
		IsRoot:      func() bool { return w.root },
	}
}

type fPower world

func (p *fPower) Power(mode string, hold float64) (string, error) {
	w := (*world)(p)
	if err := w.rec("power %s hold=%g", mode, hold); err != nil {
		return "", err
	}
	w.mode = mode
	return mode, nil
}

func (p *fPower) Fan(r client.FanRequest) error {
	w := (*world)(p)
	if r.Mode == "manual" {
		return w.rec("fan manual cpu=%d gpu=%d", *r.CPU, *r.GPU)
	}
	return w.rec("fan %s", r.Mode)
}

type fHW world

func (h *fHW) GetMode() (string, error)          { return h.mode, nil }
func (h *fHW) SupportedModes() ([]string, error) { return h.supported, nil }
func (h *fHW) GetFanState() (hw.FanState, error) { return h.fan, nil }
func (h *fHW) RearGet() (hw.Rear, error)         { return h.rear, nil }
func (h *fHW) RearSet(o hw.RearOpts) (hw.Rear, error) {
	w := (*world)(h)
	if err := w.rec("rear %s %s s=%d b=%d d=%d zones=%v verify=%v", o.Mode, o.Color, o.Speed, o.Brightness, o.Direction, o.Zones, o.Verify); err != nil {
		return hw.Rear{}, err
	}
	return hw.Rear{}, nil
}
func (h *fHW) GetBatteryLimit() (bool, error) { return h.battery, nil }
func (h *fHW) SetBatteryLimit(on bool) (bool, error) {
	w := (*world)(h)
	if err := w.rec("battery %v", on); err != nil {
		return false, err
	}
	h.battery = on
	return on, nil
}
func (h *fHW) GetOverdrive() (bool, bool, error) { return h.overdrive.on, h.overdrive.ok, nil }
func (h *fHW) SetOverdrive(on bool) (bool, error) {
	w := (*world)(h)
	if err := w.rec("overdrive %v", on); err != nil {
		return false, err
	}
	return on, nil
}

type fKB world

func (k *fKB) On() error             { return (*world)(k).rec("kb on") }
func (k *fKB) Off() error            { return (*world)(k).rec("kb off") }
func (k *fKB) RestoreDefault() error { return (*world)(k).rec("kb restore-default") }
func (k *fKB) Brightness() (int, error) {
	return k.bright, nil
}
func (k *fKB) SetEffect(name, color string, speed, brightness, direction int) error {
	return (*world)(k).rec("kb effect %s %s s=%d b=%d d=%d", name, color, speed, brightness, direction)
}

type fKP world

func (k *fKP) Exists(name string) bool { return k.kbProfiles[name] }
func (k *fKP) Apply(name string, b int) error {
	return (*world)(k).rec("kbprofile %s b=%d", name, b)
}

type fLooks world

func (l *fLooks) Names() []string { return l.savedLooks }
func (l *fLooks) Load(name string) (*looks.Look, error) {
	if err := (*world)(l).rec("look load %s", name); err != nil {
		return nil, err
	}
	return &looks.Look{}, nil
}
func (l *fLooks) Apply(*looks.Look) ([]string, error) {
	return nil, (*world)(l).rec("look apply")
}

type fAudio world

func (a *fAudio) OutputToggle(context.Context) (string, error) {
	return "", (*world)(a).rec("audio output-toggle")
}
func (a *fAudio) OutputCycle(context.Context) (string, error) {
	return "", (*world)(a).rec("audio output-cycle")
}
func (a *fAudio) SetOutput(_ context.Context, which string) (string, error) {
	return which, (*world)(a).rec("audio output %s", which)
}
func (a *fAudio) ToggleMicMute(_ context.Context, defaultOnly bool) (bool, error) {
	w := (*world)(a)
	if err := w.rec("audio mic-toggle defaultOnly=%v", defaultOnly); err != nil {
		return false, err
	}
	a.micMuted = !a.micMuted
	return a.micMuted, nil
}
func (a *fAudio) SetMicMute(_ context.Context, muted bool) error {
	return (*world)(a).rec("audio mic %v", muted)
}
func (a *fAudio) VoiceNext(_ context.Context, dev string) (string, string, error) {
	return a.voice[0], a.voice[1], (*world)(a).rec("audio voice-next dev=%q", dev)
}

type fTouch world

func (t *fTouch) Enabled() (bool, bool) { return t.touch, true }
func (t *fTouch) Set(on bool) (bool, error) {
	w := (*world)(t)
	if err := w.rec("touchpad direct %v", on); err != nil {
		return false, err
	}
	t.touch = on
	return on, nil
}

type fDaemon world

func (d *fDaemon) CallInto(cmd string, args ipc.Args, out any) error {
	w := (*world)(d)
	if !d.daemonUp {
		return ipc.ErrNotRunning
	}
	switch cmd {
	case "state":
		*(out.(*daemonState)) = daemonState{Settings: d.daemonSet}
		return w.rec("daemon state")
	case "set":
		if err := w.rec("daemon set %v=%v", args["key"], args["value"]); err != nil {
			return err
		}
		d.daemonSet[args["key"].(string)] = args["value"]
		return nil
	case "touchpad":
		if err := w.rec("daemon touchpad %v", args["action"]); err != nil {
			return err
		}
		switch args["action"] {
		case "on":
			d.touch = true
		case "off":
			d.touch = false
		case "toggle":
			d.touch = !d.touch
		}
		*(out.(*bool)) = d.touch
		return nil
	}
	return fmt.Errorf("unexpected daemon command %q", cmd)
}

type fProfiles world

func (p *fProfiles) Apply(_ context.Context, name string) (string, error) {
	if err := (*world)(p).rec("profile %s", name); err != nil {
		return "", err
	}
	return "applied " + name, nil
}
func (p *fProfiles) NextName() (string, error) { return p.profiles[1], nil }

type fState world

func (s *fState) Get(key string) any { return s.state[key] }
func (s *fState) Set(key string, v any) error {
	s.state[key] = v
	return (*world)(s).rec("state %s=%v", key, v)
}
