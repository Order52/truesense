package api

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/Order52/truesense/internal/audio"
	"github.com/Order52/truesense/internal/buildinfo"
	"github.com/Order52/truesense/internal/designs"
	"github.com/Order52/truesense/internal/keymap"
	"github.com/Order52/truesense/internal/looks"
	"github.com/Order52/truesense/internal/services"
)

// Version is this build's version.
func (a *API) Version() string { return buildinfo.Version }

// Supply is the battery as the power supply class reports it.
type Supply struct {
	Percent int
	Status  string // Charging, Discharging, Full, ...
}

// Supply reads the first battery; ok is false on a machine without one.
func (a *API) Supply() (Supply, bool) {
	dirs, _ := filepath.Glob(filepath.Join(a.s.HW.SysRoot, "/sys/class/power_supply/BAT*"))
	for _, d := range dirs {
		capacity, err1 := os.ReadFile(filepath.Join(d, "capacity"))
		status, err2 := os.ReadFile(filepath.Join(d, "status"))
		pct, err3 := strconv.Atoi(strings.TrimSpace(string(capacity)))
		if err1 == nil && err2 == nil && err3 == nil {
			return Supply{Percent: pct, Status: strings.TrimSpace(string(status))}, true
		}
	}
	return Supply{}, false
}

// StatusFull is Status plus what the window shows at a glance: whether the charger is in (ac),
// the battery level (battery_pct, null without a battery), the keyboard's own brightness
// (kb_brightness) and the look this tool last put on the keyboard (kb_look).
func (a *API) StatusFull(ctx context.Context) Status {
	st := a.Status(ctx)
	st.Entries = append(st.Entries, StatusEntry{"ac", a.s.HW.OnAC()})
	var pct any
	if b, ok := a.Supply(); ok {
		pct = b.Percent
	}
	st.Entries = append(st.Entries, StatusEntry{"battery_pct", pct})
	var bright any
	if b, err := a.s.KB.Brightness(); err == nil {
		bright = b
	}
	st.Entries = append(st.Entries, StatusEntry{"kb_brightness", bright})
	look := any(map[string]any{})
	if last := a.s.KB.Last(); !last.IsZero() {
		look = last
	}
	st.Entries = append(st.Entries, StatusEntry{"kb_look", look})
	return st
}

// RearStatus is the light bar with the three part colours of its static effect (null when
// the bar has one colour).
type RearStatus struct {
	Raw        string   `json:"raw"`
	Mode       string   `json:"mode"`
	Speed      int      `json:"speed"`
	Brightness int      `json:"brightness"`
	Direction  int      `json:"direction"`
	Color      string   `json:"color"`
	Zones      []string `json:"zones"`
}

// RearStatus reads the light bar and the part colours remembered with it.
func (a *API) RearStatus() (RearStatus, error) {
	r, err := a.s.HW.RearGet()
	if err != nil {
		return RearStatus{}, err
	}
	st := RearStatus{Raw: r.Raw, Mode: r.Mode, Speed: r.Speed, Brightness: r.Brightness, Direction: r.Direction, Color: r.Color}
	var zones []string
	if a.s.User.GetInto("rear_zones", &zones) == nil && len(zones) == 3 {
		st.Zones = zones
	}
	return st, nil
}

// SetRearNoVerify is SetRear without the read-back (a slider drag is followed by the next
// write at once).
func (a *API) SetRearNoVerify(spec RearSpec) (RearStatus, error) {
	if err := a.SetRearLook(spec, false); err != nil {
		return RearStatus{}, err
	}
	return a.RearStatus()
}

// ---- per-key colours as lists of 128 "rrggbb"

func parseCells(cells []string) ([]keymap.RGB, error) {
	if len(cells) > keymap.NumCells {
		return nil, fmt.Errorf("a picture has at most %d cells, not %d", keymap.NumCells, len(cells))
	}
	out := make([]keymap.RGB, keymap.NumCells)
	for i, c := range cells {
		rgb, err := keymap.ParseHex(c)
		if err != nil {
			return nil, fmt.Errorf("cell %d: %w", i, err)
		}
		out[i] = rgb
	}
	return out, nil
}

func hexCells(cells []keymap.RGB) []string {
	out := make([]string, len(cells))
	for i, c := range cells {
		out[i] = c.Hex()
	}
	return out
}

// KeyboardCells is the colours of the per-key picture now (128 of them; the last two are not keys).
func (a *API) KeyboardCells() []string { return hexCells(a.s.KB.CurrentCells()) }

// SetKeyboardCells uploads a per-key picture; a nil brightness keeps the keyboard's own level.
func (a *API) SetKeyboardCells(cells []string, brightness *int) error {
	rgb, err := parseCells(cells)
	if err != nil {
		return err
	}
	return a.s.KB.PerKey(rgb, brightnessArg(brightness))
}

// DesignShown is a design as the window edits it.
type DesignShown struct {
	Info   string   `json:"info"`
	Source string   `json:"source"`
	Cells  []string `json:"cells"`
}

// ShowDesign returns a design's colours.
func (a *API) ShowDesign(name string) (DesignShown, error) {
	d, err := a.s.Designs.Get(name)
	if err != nil {
		return DesignShown{}, err
	}
	cells, err := designs.CellsOf(a.km, d)
	if err != nil {
		return DesignShown{}, err
	}
	return DesignShown{Info: d.Info, Source: d.Source, Cells: hexCells(cells)}, nil
}

// SaveDesignCells saves given colours as one of your designs and answers its file; nil cells
// are the picture now on the keyboard.
func (a *API) SaveDesignCells(name string, cells []string) (string, error) {
	if name == "" {
		return "", fmt.Errorf("give a design name")
	}
	if cells == nil {
		return a.SaveDesign(name)
	}
	rgb, err := parseCells(cells)
	if err != nil {
		return "", err
	}
	return a.s.Designs.Save(name, rgb)
}

// SaveLightingProfileCells saves given colours as a lighting profile (nil: the picture now on
// the keyboard) and answers the message the command prints.
func (a *API) SaveLightingProfileCells(name string, cells []string) (string, error) {
	if cells == nil {
		cells = a.KeyboardCells()
	}
	rgb, err := parseCells(cells)
	if err != nil {
		return "", err
	}
	if err := a.s.KeyProf.Save(name, rgb); err != nil {
		return "", err
	}
	return fmt.Sprintf("saved lighting profile '%s'", name), nil
}

// ---- looks

// SaveLookValue saves a look as the window holds it (nil: what is on the lights now).
func (a *API) SaveLookValue(name string, l *looks.Look) (*looks.Look, error) {
	return a.s.Looks.Save(name, l)
}

// CopyLook saves a look under a new name (NAME-copy, numbered until free) and answers it.
func (a *API) CopyLook(name, newName string) (string, error) { return a.s.Looks.Copy(name, newName) }

// LookIsBuiltin reports whether a look is read-only.
func (a *API) LookIsBuiltin(name string) bool { return slicesContains(looks.BuiltinNames(), name) }

func slicesContains(list []string, s string) bool {
	for _, x := range list {
		if x == s {
			return true
		}
	}
	return false
}

// CurrentProfile is the whole-setup profile applied last ("" when none).
func (a *API) CurrentProfile() string {
	var name string
	_ = a.s.User.GetInto("profile", &name)
	return name
}

// ---- audio

// MicReport is the microphone state plus the headset microphone boost (nil without the control).
type MicReport struct {
	audio.MicState
	Boost *int `json:"boost"`
}

// MicStatus reads the microphones and the boost.
func (a *API) MicStatus(ctx context.Context) (MicReport, error) {
	boost, err := a.HeadsetBoost(ctx)
	return MicReport{MicState: a.Mic(ctx), Boost: boost}, err
}

// DTSInfo is the DTS sound at a glance.
type DTSInfo struct {
	On      bool        `json:"on"`
	Mode    *string     `json:"mode"`
	Devices []DTSDevice `json:"devices"`
}

// DTSDevice is one output and its DTS profile.
type DTSDevice = audio.DTSDevice

// DTSInfo reads whether DTS is on, the profile of the output playing and every output.
func (a *API) DTSInfo(ctx context.Context) (DTSInfo, error) {
	status, err := a.DTSStatus(ctx)
	if err != nil {
		return DTSInfo{}, err
	}
	devs, err := a.DTSDevices(ctx)
	if err != nil {
		return DTSInfo{}, err
	}
	info := DTSInfo{On: DTSOn(status), Devices: devs}
	if info.Devices == nil {
		info.Devices = []DTSDevice{}
	}
	for _, d := range devs {
		if d.Current {
			p := d.Profile
			info.Mode = &p
		}
	}
	return info, nil
}

// SoundOutput is whether the sound card has an output: OK is null when there is no card.
type SoundOutput struct {
	OK      *bool    `json:"ok"`
	Problem string   `json:"problem"`
	Sinks   []string `json:"sinks"`
}

// SoundOutput judges the sound card for the window.
func (a *API) SoundOutput(ctx context.Context) (SoundOutput, error) {
	st, err := a.SoundState(ctx)
	out := SoundOutput{OK: st.OK, Problem: st.Problem, Sinks: st.Sinks}
	if out.Sinks == nil {
		out.Sinks = []string{}
	}
	return out, err
}

// ---- window at login

func (a *API) autostartPath() (string, error) {
	dir, err := os.UserConfigDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "autostart", "truesense.desktop"), nil
}

// Autostart reports whether the window opens at login.
func (a *API) Autostart() (bool, error) {
	p, err := a.autostartPath()
	if err != nil {
		return false, err
	}
	_, err = os.Stat(p)
	return err == nil, nil
}

// SetAutostart makes the window open at login (an autostart entry in the user's configuration)
// or not.
func (a *API) SetAutostart(on bool) (bool, error) {
	p, err := a.autostartPath()
	if err != nil {
		return false, err
	}
	if !on {
		if err := os.Remove(p); err != nil && !errors.Is(err, os.ErrNotExist) {
			return true, err
		}
		return false, nil
	}
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		return false, err
	}
	entry := "[Desktop Entry]\nType=Application\nName=TrueSense\nExec=truesense gui\nX-GNOME-Autostart-enabled=true\n"
	return true, os.WriteFile(p, []byte(entry), 0o644)
}

// ---- services

// RepairUserServices is the part of the repair a window can do: your own services. It adds a
// step that says so when the system part is left (a window cannot answer a sudo prompt).
func (a *API) RepairUserServices(ctx context.Context) []services.Step {
	steps := a.serviceManager().RepairUser(ctx)
	if a.EUID != 0 {
		steps = append(steps, services.Step{What: "the system part (daemon, health timer, module) needs `sudo truesense service repair`", OK: true})
	}
	return steps
}

// ServiceLogText is the daemon's last 60 log lines of this boot.
func (a *API) ServiceLogText(ctx context.Context) (string, error) {
	var out, errw strings.Builder
	if _, err := a.ServiceLogs(ctx, &out, &errw); err != nil {
		return "", err
	}
	return out.String() + errw.String(), nil
}

// SelfTestText runs the self-test and answers whether it passed with everything it printed.
func (a *API) SelfTestText(ctx context.Context, skipFans bool) (ok bool, output string) {
	var out strings.Builder
	r := a.SelfTest(ctx, &out, skipFans)
	return r.OK(), out.String()
}

// ---- reset to defaults

// ResetStep is one thing `config reset-all` did.
type ResetStep struct {
	Step  string `json:"step"`
	OK    bool   `json:"ok"`
	Error string `json:"error,omitempty"`
}

// ResetAll puts the fan curve, the Smart auto profile, the fans, the mode control, the key
// bindings, the power limits, both undervolts, the keyboard and the light bar back to their
// defaults. Every step runs even if one fails; saved profiles are kept.
func (a *API) ResetAll(ctx context.Context) []ResetStep {
	var steps []ResetStep
	do := func(name string, f func() error) {
		err := f()
		s := ResetStep{Step: name, OK: err == nil}
		if err != nil {
			s.Error = err.Error()
		}
		steps = append(steps, s)
	}
	do("fan curve: native", func() error { _, err := a.UseFanCurve("native"); return err })
	do("Smart auto profile: native", func() error { _, err := a.UseSmartProfile("native"); return err })
	do("fans: firmware auto", a.FanAuto)
	do("mode control: native", func() error { return a.SetAutoMode(AutoNative) })
	do("key bindings: native", func() error { return a.UseKeyProfile("native") })
	do("power limits: automatic", func() error { _, err := a.ResetCPULimits(); return err })
	do("CPU undervolt: no boot profile", func() error { _, err := a.SetCPUUndervoltBoot("off", UndervoltRequest{}); return err })
	do("CPU undervolt: stock", func() error { _, err := a.ResetCPUUndervolt(); return err })
	do("GPU undervolt: no boot profile", func() error { _, err := a.SetGPUUndervoltBoot("off", UndervoltRequest{}); return err })
	do("GPU undervolt: stock", func() error { _, err := a.ResetGPUUndervolt(); return err })
	do("keyboard: stock look", a.RestoreKeyboard)
	do("light bar: stock", func() error {
		_, err := a.SetRear(RearSpec{Mode: "breathing", Color: "3723c7", Speed: 3, Brightness: 100, Direction: 1})
		return err
	})
	return steps
}
