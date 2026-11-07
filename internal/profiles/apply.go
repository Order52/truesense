package profiles

import (
	"context"
	"errors"
	"fmt"
	"math"
	"slices"
	"strconv"
	"strings"

	"github.com/Order52/truesense/internal/audio"
	"github.com/Order52/truesense/internal/client"
	"github.com/Order52/truesense/internal/curves"
	"github.com/Order52/truesense/internal/hw"
	"github.com/Order52/truesense/internal/ipc"
	"github.com/Order52/truesense/internal/keyboard"
)

// ActionRunner runs one action of internal/actions: *actions.Runner. Profiles use it for
// the settings that are also key actions (power held like a tile pick, fans, keyboard and
// rear on/off, lighting profiles), so a profile and a key press cannot drift apart.
type ActionRunner interface {
	Run(ctx context.Context, spec string) (string, error)
}

// Fan sets the fans: *client.Client.
type Fan interface {
	Fan(r client.FanRequest) error
}

// Daemon is the control socket: *ipc.Client.
type Daemon interface {
	CallInto(cmd string, args ipc.Args, out any) error
}

// Hardware is the firmware features a profile reads (Capture) and sets (Apply): *hw.HW.
type Hardware interface {
	GetMode() (string, error)
	GetFanState() (hw.FanState, error)
	RearGet() (hw.Rear, error)
	RearSet(o hw.RearOpts) (hw.Rear, error)
	GetBatteryLimit() (bool, error)
	SetBatteryLimit(on bool) (bool, error)
	GetUSBCharge() (int, error)
	SetUSBCharge(level int) (int, error)
	GetOverdrive() (on, ok bool, err error)
	SetOverdrive(on bool) (bool, error)
	GetBootSound() (bool, error)
	SetBootSound(on bool) (bool, error)
	GetKBTimeout() (bool, error)
	SetKBTimeout(on bool) (bool, error)
}

// Keyboard is the backlight controller: *keyboard.Keyboard.
type Keyboard interface {
	Brightness() (int, error)
	SetEffect(name, color string, speed, brightness, direction int) error
}

// Audio is the sound system: *audio.Controller.
type Audio interface {
	Output(ctx context.Context) (audio.OutputStatus, error)
	VoiceModes() map[string]string
	SetOutput(ctx context.Context, which string) (string, error)
	VoiceSet(ctx context.Context, dev, mode string) (string, error)
	SetMicMute(ctx context.Context, muted bool) error
	DTSMode(ctx context.Context, name string, scope audio.DTSScope) (string, error)
}

// Curves is the saved fan curves: *curves.Store.
type Curves interface {
	Load(name string) (curves.Curve, error)
}

// State is the per-user memory (which profile was applied last): *userstate.Store.
type State interface {
	Get(key string) any
	Set(key string, value any) error
}

// Store is the profile files plus everything Apply and Capture reach. Every field of Env is
// required.
type Store struct {
	Files
	Env
}

// Env is the outside world of Apply and Capture.
type Env struct {
	Actions  ActionRunner
	Fans     Fan
	Daemon   Daemon
	HW       Hardware
	Keyboard Keyboard
	Audio    Audio
	Curves   Curves
	State    State
}

// stateKey remembers the last applied profile, so `profile-next` continues from it.
const stateKey = "profile"

// NextName is the profile after the last one applied (the first when none was).
func (s *Store) NextName() (string, error) {
	names := s.Names()
	if len(names) == 0 {
		return "", errors.New("no profiles saved")
	}
	cur, _ := s.State.Get(stateKey).(string)
	if i := slices.Index(names, cur); i >= 0 {
		return names[(i+1)%len(names)], nil
	}
	return names[0], nil
}

// Capture records the current setup as a profile: what can be read back. Keyboard colours
// cannot, so only the keyboard's "off after 30 s" setting is captured. Daemon-held settings
// (smart fan, fan curve, mode control) are recorded when the daemon runs and left out when it
// does not.
func (s *Store) Capture(ctx context.Context, description string) (Profile, error) {
	if description == "" {
		description = "captured"
	}
	d := Profile{"description": description}
	mode, err := s.HW.GetMode()
	if err != nil {
		return nil, err
	}
	power := map[string]any{"mode": mode}
	fan, err := s.HW.GetFanState()
	if err != nil {
		return nil, err
	}
	var st struct {
		Settings map[string]any `json:"settings"`
	}
	stateErr := s.Daemon.CallInto("state", nil, &st)
	smart := stateErr == nil && truthy(st.Settings["smart_fan"])
	fanSection := map[string]any{"mode": fan.Mode}
	if smart {
		fanSection["mode"] = "smart"
	}
	var curve struct {
		Name string `json:"name"`
	}
	if err := s.Daemon.CallInto("fan_curve", ipc.Args{"action": "get"}, &curve); err == nil {
		fanSection["curve"] = curve.Name
	}
	if stateErr == nil {
		auto, ok := st.Settings["auto_mode"].(string)
		if !ok {
			auto = "off"
		}
		power["auto"] = auto
	}
	if fanSection["mode"] == "manual" {
		fanSection["cpu"], fanSection["gpu"] = fan.Duty.CPU, fan.Duty.GPU
	}
	d["power"], d["fan"] = power, fanSection

	r, err := s.HW.RearGet()
	if err != nil {
		return nil, err
	}
	rear := map[string]any{"on": r.Brightness > 0}
	if r.Brightness > 0 && slices.Contains(hw.RearModeNames, r.Mode) {
		rear["effect"], rear["color"], rear["speed"], rear["brightness"] = r.Mode, r.Color, r.Speed, r.Brightness
	}
	d["rear"] = rear

	limit, err := s.HW.GetBatteryLimit()
	if err != nil {
		return nil, err
	}
	usb, err := s.HW.GetUSBCharge()
	if err != nil {
		return nil, err
	}
	d["battery"] = map[string]any{"limit": limit, "usb_charge": usb}
	od, odKnown, err := s.HW.GetOverdrive()
	if err != nil {
		return nil, err
	}
	d["display"] = map[string]any{"overdrive": od && odKnown}
	sound, err := s.HW.GetBootSound()
	if err != nil {
		return nil, err
	}
	d["boot"] = map[string]any{"sound": sound}
	kbt, err := s.HW.GetKBTimeout()
	if err != nil {
		return nil, err
	}
	d["keyboard"] = map[string]any{"timeout": kbt}
	if out, err := s.Audio.Output(ctx); err == nil {
		modes := s.Audio.VoiceModes()
		d["audio"] = map[string]any{"output": out.Active, "voice_headset": modes["headset"], "voice_array": modes["array"]}
	}
	return d, nil
}

// Apply applies what the profile LISTS and nothing else, and returns the summary line, e.g.
// "Profile 'gaming' applied (4 settings)". A setting that fails is reported in that line and
// does not stop the others: a half-applied profile is better than a laptop left in an
// unknown state by an early exit. The power mode is held like a tile pick.
func (s *Store) Apply(ctx context.Context, name string) (string, error) {
	d, err := s.Load(name)
	if err != nil {
		return "", err
	}
	done, failed := 0, []string{}
	step := func(label string, fn func() error) {
		if err := guard(fn); err != nil {
			failed = append(failed, label+": "+err.Error())
			return
		}
		done++
	}
	run := func(spec string) func() error {
		return func() error { _, err := s.Actions.Run(ctx, spec); return err }
	}

	p := table(d, "power")
	if has(p, "auto") {
		step("power auto", func() error {
			return s.Daemon.CallInto("set", ipc.Args{"key": "auto_mode", "value": p["auto"]}, nil)
		})
	}
	if has(p, "mode") {
		step("power", run("power:"+text(p["mode"])))
	}
	f := table(d, "fan")
	if has(f, "curve") {
		step("fan curve", func() error { return s.fanCurve(text(f["curve"])) })
	}
	if f["mode"] == "manual" {
		step("fan", func() error { return s.fanManual(f) })
	} else if has(f, "mode") {
		step("fan", run("fan:"+text(f["mode"])))
	}
	k := table(d, "keyboard")
	switch {
	case has(k, "profile"):
		step("keyboard profile", run("keyboard-profile:"+text(k["profile"])))
	case has(k, "effect"):
		step("keyboard effect", func() error { return s.keyboardEffect(k) })
	case has(k, "on"):
		step("keyboard", run(map[bool]string{true: "keyboard:on", false: "keyboard:off"}[truthy(k["on"])]))
	}
	if has(k, "timeout") {
		step("keyboard timeout", func() error { _, err := s.HW.SetKBTimeout(truthy(k["timeout"])); return err })
	}
	r := table(d, "rear")
	switch {
	case has(r, "effect"):
		step("rear light", func() error { return s.rear(r) })
	case has(r, "on"):
		step("rear light", run(map[bool]string{true: "rear:on", false: "rear:off"}[truthy(r["on"])]))
	}
	b := table(d, "battery")
	if has(b, "limit") {
		step("battery limit", func() error { _, err := s.HW.SetBatteryLimit(truthy(b["limit"])); return err })
	}
	if has(b, "usb_charge") {
		step("usb charge", func() error {
			n, err := toInt(b["usb_charge"])
			if err != nil {
				return err
			}
			_, err = s.HW.SetUSBCharge(n)
			return err
		})
	}
	if disp := table(d, "display"); has(disp, "overdrive") {
		step("overdrive", func() error { _, err := s.HW.SetOverdrive(truthy(disp["overdrive"])); return err })
	}
	if boot := table(d, "boot"); has(boot, "sound") {
		step("boot sound", func() error { _, err := s.HW.SetBootSound(truthy(boot["sound"])); return err })
	}
	a := table(d, "audio")
	if has(a, "output") {
		step("audio output", func() error { _, err := s.Audio.SetOutput(ctx, text(a["output"])); return err })
	}
	if has(a, "voice_headset") {
		step("voice (headset)", func() error { _, err := s.Audio.VoiceSet(ctx, "headset", text(a["voice_headset"])); return err })
	}
	if has(a, "voice_array") {
		step("voice (array)", func() error { _, err := s.Audio.VoiceSet(ctx, "array", text(a["voice_array"])); return err })
	}
	if has(a, "mic_muted") {
		step("microphone", func() error { return s.Audio.SetMicMute(ctx, truthy(a["mic_muted"])) })
	}
	if has(a, "dts_mode") {
		step("DTS mode", func() error { _, err := s.Audio.DTSMode(ctx, text(a["dts_mode"]), audio.DTSConfigured); return err })
	}
	_ = s.State.Set(stateKey, name) // a convenience, never a requirement
	msg := fmt.Sprintf("Profile '%s' applied (%d settings", name, done)
	if len(failed) > 0 {
		msg += fmt.Sprintf(", %d FAILED: %s", len(failed), strings.Join(failed, "; "))
	}
	return msg + ")", nil
}

// guard turns a panic in one setting into that setting's failure.
func guard(fn func() error) (err error) {
	defer func() {
		if p := recover(); p != nil {
			err = fmt.Errorf("internal error: %v", p)
		}
	}()
	return fn()
}

// fanCurve selects a smart fan curve in the daemon. A saved curve travels with the request
// (the daemon is root and cannot read the user's curves directory); built-ins go by name.
func (s *Store) fanCurve(name string) error {
	var data any
	if !curves.IsBuiltin(name) {
		c, err := s.Curves.Load(name)
		if err != nil {
			return err
		}
		data = c
	}
	return s.Daemon.CallInto("fan_curve", ipc.Args{"action": "use", "name": name, "data": data}, nil)
}

func (s *Store) fanManual(f map[string]any) error {
	if !has(f, "cpu") || !has(f, "gpu") {
		return errors.New("manual fans need cpu and gpu")
	}
	cpu, err := toInt(f["cpu"])
	if err != nil {
		return err
	}
	gpu, err := toInt(f["gpu"])
	if err != nil {
		return err
	}
	return s.Fans.Fan(client.FanRequest{Mode: "manual", CPU: &cpu, GPU: &gpu})
}

func (s *Store) keyboardEffect(k map[string]any) error {
	color, err := str(k, "color", "00aec7")
	if err != nil {
		return err
	}
	speed, err := intOf(k, "speed", keyboard.DefaultSpeed)
	if err != nil {
		return err
	}
	var brightness int
	if has(k, "brightness") {
		if brightness, err = intOf(k, "brightness", 0); err != nil {
			return err
		}
	} else {
		if brightness, err = s.Keyboard.Brightness(); err != nil {
			return err
		}
		if brightness == 0 {
			brightness = keyboard.DefaultEffectBright
		}
	}
	return s.Keyboard.SetEffect(text(k["effect"]), color, speed, brightness, 0)
}

func (s *Store) rear(r map[string]any) error {
	color, err := str(r, "color", "ffffff")
	if err != nil {
		return err
	}
	speed, err := intOf(r, "speed", 3)
	if err != nil {
		return err
	}
	brightness, err := intOf(r, "brightness", 100)
	if err != nil {
		return err
	}
	direction, err := intOf(r, "direction", 1)
	if err != nil {
		return err
	}
	o := hw.DefaultRear()
	o.Mode, o.Color, o.Speed, o.Brightness, o.Direction, o.Verify = text(r["effect"]), color, speed, brightness, direction, true
	_, err = s.HW.RearSet(o)
	return err
}

func str(m map[string]any, key, def string) (string, error) {
	v, ok := m[key]
	if !ok {
		return def, nil
	}
	s, isStr := v.(string)
	if !isStr {
		return "", fmt.Errorf("%s must be text", key)
	}
	return s, nil
}

func intOf(m map[string]any, key string, def int) (int, error) {
	v, ok := m[key]
	if !ok {
		return def, nil
	}
	return toInt(v)
}

// toInt converts a TOML value the way Python's int() did: integers, floats (truncated),
// booleans and numeric text.
func toInt(v any) (int, error) {
	switch x := v.(type) {
	case int64:
		return int(x), nil
	case int:
		return x, nil
	case float64:
		if math.IsNaN(x) || math.IsInf(x, 0) || math.Abs(x) > 1<<53 {
			return 0, fmt.Errorf("%v is not a whole number", v)
		}
		return int(x), nil
	case bool:
		if x {
			return 1, nil
		}
		return 0, nil
	case string:
		n, err := strconv.Atoi(strings.TrimSpace(x))
		if err != nil {
			return 0, fmt.Errorf("%q is not a number", x)
		}
		return n, nil
	}
	return 0, fmt.Errorf("%v is not a number", v)
}
