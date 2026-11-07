package actions

import (
	"context"
	"errors"
	"fmt"
	"os/exec"
	"slices"
	"strconv"
	"syscall"

	"github.com/Order52/truesense/internal/audio"
	"github.com/Order52/truesense/internal/client"
	"github.com/Order52/truesense/internal/config"
	"github.com/Order52/truesense/internal/hw"
	"github.com/Order52/truesense/internal/ipc"
	"github.com/Order52/truesense/internal/keyboard"
)

// The keyboard and rear light effects the "next effect" actions step through, in this order
// (the position is remembered in the user state as kb_effect / rear_effect).
var (
	kbEffects   = []string{"static", "breathing", "wave", "snake", "ripple", "neon", "raindrop", "lightning", "spot", "star", "fireball", "snow", "heartbeat"}
	rearEffects = []string{"static", "breathing", "neon", "wave", "twinkling"}
)

// defaultCycle is what power-next/prev step through when no configured mode is supported.
var defaultCycle = []string{"eco", "balanced", "turbo"}

// Runner executes actions against an Env.
type Runner struct {
	env Env
	v   Validator
}

// New returns a Runner over env.
func New(env Env) *Runner {
	r := &Runner{env: env}
	if env.Looks != nil {
		r.v.LookNames = env.Looks.Names
	}
	return r
}

// Validate checks an action the way Run will (saved looks included). Configuration code
// uses it so that a bad action is refused when it is set.
func (r *Runner) Validate(spec string) error { return r.v.Validate(spec) }

// Run executes one action and returns the message to show ("" for none). The action is
// validated first, so a spec that went stale (a look deleted since) fails with a clear
// message instead of half running.
func (r *Runner) Run(ctx context.Context, spec string) (string, error) {
	name, arg := Split(spec)
	if err := r.v.Validate(spec); err != nil {
		return "", err
	}
	cfg, err := r.env.Config()
	if err != nil {
		return "", fmt.Errorf("read configuration: %w", err)
	}
	hold := float64(cfg.Profile.HoldMinutes)
	switch name {
	case "none":
		return "", nil
	case "power-next", "power-prev", "power-cycle":
		return r.powerStep(name, arg, cfg, hold)
	case "power":
		return r.setPower(arg, hold)
	case "fan":
		return r.fan(arg)
	case "fan-max-toggle":
		st, err := r.env.HW.GetFanState()
		if err != nil {
			return "", err
		}
		if st.Mode == "max" {
			return r.fan("smart")
		}
		return r.fan("max")
	case "smart-fan-toggle":
		return r.smartFanToggle()
	case "look":
		return r.look(arg)
	case "auto":
		return r.setAuto(arg)
	case "auto-next":
		return r.autoNext()
	case "profile":
		return r.env.Profiles.Apply(ctx, arg)
	case "profile-next":
		next, err := r.env.Profiles.NextName()
		if err != nil {
			return "", err
		}
		return r.env.Profiles.Apply(ctx, next)
	case "output-toggle":
		_, err := r.env.Audio.OutputToggle(ctx)
		return "", err
	case "output-cycle":
		_, err := r.env.Audio.OutputCycle(ctx)
		return "", err
	case "output":
		if _, err := r.env.Audio.SetOutput(ctx, arg); err != nil {
			return "", err
		}
		return "Audio output: " + audio.Label(arg), nil
	case "mic-toggle":
		muted, err := r.env.Audio.ToggleMicMute(ctx, false)
		if err != nil {
			return "", err
		}
		return micText(muted), nil
	case "mic":
		if err := r.env.Audio.SetMicMute(ctx, arg == "mute"); err != nil {
			return "", err
		}
		return micText(arg == "mute"), nil
	case "voice-next":
		dev, mode, err := r.env.Audio.VoiceNext(ctx, "")
		if err != nil {
			return "", err
		}
		return fmt.Sprintf("Purified Voice (%s): %s", dev, audio.ModeLabel(mode)), nil
	case "touchpad-toggle":
		return r.touchpad(ctx, "toggle")
	case "touchpad":
		return r.touchpad(ctx, arg)
	case "keyboard":
		return r.keyboard(arg)
	case "keyboard-effect-next":
		return r.keyboardEffectNext()
	case "keyboard-profile":
		if err := r.env.KeyProfiles.Apply(arg, keyboard.KeepBrightness); err != nil {
			return "", err
		}
		r.remember("keyboard_profile", arg)
		r.remember("keyboard_on", true)
		return "Keyboard profile: " + arg, nil
	case "rear":
		return r.rear(arg)
	case "rear-effect-next":
		return r.rearEffectNext()
	case "battery-limit-toggle":
		cur, err := r.env.HW.GetBatteryLimit()
		if err != nil {
			return "", err
		}
		v, err := r.env.HW.SetBatteryLimit(!cur)
		if err != nil {
			return "", err
		}
		return "Battery 80 % limit: " + onOff(v), nil
	case "overdrive-toggle":
		cur, ok, err := r.env.HW.GetOverdrive()
		if err != nil {
			return "", err
		}
		v, err := r.env.HW.SetOverdrive(!cur || !ok) // unknown counts as off, so the press turns it on
		if err != nil {
			return "", err
		}
		return "LCD overdrive: " + onOff(v), nil
	case "notify":
		return arg, nil
	case "exec":
		if r.env.Exec == nil {
			return "", errors.New("exec: actions are not available here")
		}
		return "", r.env.Exec(ctx, arg)
	}
	return "", fmt.Errorf("unhandled action %s", name) // unreachable: Validate covers every name
}

func onOff(v bool) string {
	if v {
		return "on"
	}
	return "off"
}

func micText(muted bool) string {
	if muted {
		return "Microphones: MUTED"
	}
	return "Microphones: live"
}

// powerStep moves to the next or previous mode of a list: the configured cycle (limited to
// the modes this laptop supports) or the one the action names. A mode that is not in the
// list starts the walk at its beginning (next) or one before its end (prev), as 0.3.x did.
func (r *Runner) powerStep(name, arg string, cfg *config.Config, hold float64) (string, error) {
	var order []string
	if name == "power-cycle" {
		order = splitList(arg)
	} else {
		supported, err := r.env.HW.SupportedModes()
		if err != nil {
			return "", err
		}
		for _, m := range cfg.Keys.PowerCycle {
			if slices.Contains(supported, m) {
				order = append(order, m)
			}
		}
		if len(order) == 0 {
			order = defaultCycle
		}
	}
	cur, err := r.env.HW.GetMode()
	if err != nil {
		return "", err
	}
	step := 1
	if name == "power-prev" {
		step = -1
	}
	i := slices.Index(order, cur)
	n := len(order)
	next := order[((i+step)%n+n)%n]
	return r.setPower(next, hold)
}

func splitList(arg string) []string {
	var out []string
	start := 0
	for i := 0; i <= len(arg); i++ {
		if i == len(arg) || arg[i] == ',' {
			out = append(out, arg[start:i])
			start = i + 1
		}
	}
	return out
}

func (r *Runner) setPower(mode string, hold float64) (string, error) {
	got, err := r.env.Power.Power(mode, hold)
	if err != nil {
		return "", err
	}
	return "Power mode: " + got, nil
}

func (r *Runner) fan(arg string) (string, error) {
	texts := map[string]string{"smart": "Fans: smart curve", "auto": "Fans: firmware auto", "max": "Fans: MAX"}
	if text, ok := texts[arg]; ok {
		if err := r.env.Power.Fan(client.FanRequest{Mode: arg}); err != nil {
			return "", err
		}
		return text, nil
	}
	n, _ := strconv.Atoi(arg) // validated: 20..100
	if err := r.env.Power.Fan(client.FanRequest{Mode: "manual", CPU: &n, GPU: &n}); err != nil {
		return "", err
	}
	return fmt.Sprintf("Fans: %d %%", n), nil
}

// daemonState is the part of the daemon's state the actions read.
type daemonState struct {
	Settings map[string]any `json:"settings"`
}

func (r *Runner) daemonSettings() (map[string]any, error) {
	var st daemonState
	if err := r.env.Daemon.CallInto("state", nil, &st); err != nil {
		return nil, err
	}
	return st.Settings, nil
}

func (r *Runner) smartFanToggle() (string, error) {
	on := true // no daemon: unknown counts as off, and the set below then reports the daemon missing
	if s, err := r.daemonSettings(); err == nil {
		on = !truthy(s["smart_fan"])
	}
	if err := r.env.Daemon.CallInto("set", ipc.Args{"key": "smart_fan", "value": on}, nil); err != nil {
		return "", errors.New("the TrueSense daemon is not running")
	}
	if on {
		return "Fans: smart curve", nil
	}
	return "Fans: smart curve OFF", nil
}

func (r *Runner) look(name string) (string, error) {
	l, err := r.env.Looks.Load(name)
	if err != nil {
		return "", err
	}
	if _, err := r.env.Looks.Apply(l); err != nil {
		return "", err
	}
	return "Look: " + name, nil
}

func (r *Runner) setAuto(value string) (string, error) {
	if err := r.env.Daemon.CallInto("set", ipc.Args{"key": "auto_mode", "value": value}, nil); err != nil {
		return "", err
	}
	return "Mode control: " + value, nil
}

func (r *Runner) autoNext() (string, error) {
	s, err := r.daemonSettings()
	if err != nil {
		return "", err
	}
	cur, ok := s["auto_mode"].(string)
	if !ok {
		cur = "off"
	}
	next := map[string]string{"off": "native", "native": "smart", "smart": "off"}[cur]
	if next == "" {
		next = "smart" // an unknown value (a hand-edited state) goes to Smart, as in 0.3.x
	}
	return r.setAuto(next)
}

// touchpad asks the daemon (the switch needs root); root without a daemon switches it itself.
func (r *Runner) touchpad(ctx context.Context, action string) (string, error) {
	var on bool
	err := r.env.Daemon.CallInto("touchpad", ipc.Args{"action": action}, &on)
	if err != nil {
		if r.env.IsRoot == nil || !r.env.IsRoot() || r.env.Touchpad == nil {
			return "", err
		}
		want := action == "on"
		if action == "toggle" {
			cur, _ := r.env.Touchpad.Enabled()
			want = !cur
		}
		if on, err = r.env.Touchpad.Set(want); err != nil {
			return "", err
		}
	}
	return "Touchpad: " + onOff(on), nil
}

func (r *Runner) keyboard(arg string) (string, error) {
	cur := r.stateBool("keyboard_on", true)
	next := arg == "on"
	if arg == "toggle" {
		next = !cur
	}
	switch {
	case !next:
		if err := r.env.Keyboard.Off(); err != nil {
			return "", err
		}
	case truthy(r.env.State.Get("kb_restore")):
		if err := r.env.Keyboard.On(); err != nil {
			return "", err
		}
	default:
		profile := r.stateString("keyboard_profile", "default")
		var err error
		if r.env.KeyProfiles.Exists(profile) {
			err = r.env.KeyProfiles.Apply(profile, keyboard.KeepBrightness)
		} else {
			err = r.env.Keyboard.RestoreDefault()
		}
		if err != nil {
			return "", err
		}
	}
	r.remember("keyboard_on", next)
	return "Keyboard light: " + onOff(next), nil
}

func (r *Runner) keyboardEffectNext() (string, error) {
	i := mod(r.stateInt("kb_effect", -1)+1, len(kbEffects))
	r.remember("kb_effect", i)
	b, err := r.env.Keyboard.Brightness()
	if err != nil {
		return "", err
	}
	if b == 0 {
		b = keyboard.DefaultEffectBright
	}
	effect := kbEffects[i]
	level := b
	if effect == "neon" {
		level = max(b, keyboard.DefaultEffectBright) // the neon effect is invisible when dim
	}
	if err := r.env.Keyboard.SetEffect(effect, keyboard.OperatingColor, keyboard.DefaultSpeed, level, 0); err != nil {
		return "", err
	}
	r.remember("keyboard_on", true)
	return "Keyboard effect: " + effect, nil
}

// rearLast is what the light bar was before it was switched off, so switching it on restores it.
type rearLast struct {
	mode, color                  string
	speed, brightness, direction int
}

func defaultRearLast() rearLast {
	return rearLast{mode: "static", color: "00aec7", speed: 3, brightness: 100, direction: 1}
}

func (r *Runner) rearLast() rearLast {
	last := defaultRearLast()
	m, ok := r.env.State.Get("rear_last").(map[string]any)
	if !ok || len(m) == 0 {
		return last
	}
	if v, ok := m["mode"].(string); ok {
		last.mode = v
	}
	if v, ok := m["color"].(string); ok {
		last.color = v
	}
	last.speed = intOf(m["speed"], last.speed)
	last.brightness = intOf(m["brightness"], last.brightness)
	last.direction = intOf(m["direction"], last.direction)
	return last
}

// rearZones returns the three part colours of the static effect that were set last, kept
// through on/off so a multi-coloured bar comes back as it was.
func (r *Runner) rearZones(mode string) []string {
	if mode != "static" {
		return nil
	}
	list, ok := r.env.State.Get("rear_zones").([]any)
	if !ok || len(list) != 3 {
		return nil
	}
	out := make([]string, 3)
	for i, z := range list {
		s, ok := z.(string)
		if !ok {
			return nil
		}
		out[i] = s
	}
	return out
}

func (r *Runner) rear(arg string) (string, error) {
	cur, err := r.env.HW.RearGet()
	if err != nil {
		return "", err
	}
	on := cur.Brightness > 0
	next := arg == "on"
	if arg == "toggle" {
		next = !on
	}
	if on && !next {
		r.remember("rear_last", map[string]any{"mode": cur.Mode, "color": cur.Color, "speed": cur.Speed, "brightness": cur.Brightness, "direction": cur.Direction})
	}
	o := hw.DefaultRear()
	o.Verify = true
	if next {
		last := r.rearLast()
		o.Mode = last.mode
		if !slices.Contains(hw.RearModeNames, o.Mode) {
			o.Mode = "static"
		}
		o.Color, o.Speed, o.Direction = last.color, last.speed, last.direction
		o.Brightness = last.brightness
		if o.Brightness == 0 {
			o.Brightness = 100
		}
	} else {
		o.Mode = cur.Mode
		if !slices.Contains(hw.RearModeNames, o.Mode) {
			o.Mode = "static"
		}
		o.Color, o.Speed, o.Direction, o.Brightness = cur.Color, cur.Speed, cur.Direction, 0
	}
	o.Zones = r.rearZones(o.Mode)
	if _, err := r.env.HW.RearSet(o); err != nil {
		return "", err
	}
	return "Rear light: " + onOff(next), nil
}

func (r *Runner) rearEffectNext() (string, error) {
	i := mod(r.stateInt("rear_effect", -1)+1, len(rearEffects))
	r.remember("rear_effect", i)
	cur, err := r.env.HW.RearGet()
	if err != nil {
		return "", err
	}
	o := hw.DefaultRear()
	o.Verify = true
	o.Mode, o.Color, o.Speed, o.Direction = rearEffects[i], cur.Color, cur.Speed, cur.Direction
	o.Brightness = cur.Brightness
	if o.Brightness == 0 {
		o.Brightness = 100
	}
	if _, err := r.env.HW.RearSet(o); err != nil {
		return "", err
	}
	return "Rear effect: " + rearEffects[i], nil
}

// ShellExec is the production Env.Exec: it starts the command through /bin/sh -c in its own
// session with no input and no output, as the calling user, and returns once it is started.
// It is deliberately not bound to ctx's deadline: `exec:truesense gui` starts a window that
// must outlive the key press; the process is reaped in the background.
func ShellExec(ctx context.Context, command string) error {
	cmd := exec.CommandContext(context.WithoutCancel(ctx), "/bin/sh", "-c", command) //nolint:gosec // the command is the user's own configured action, run as the user
	cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
	if err := cmd.Start(); err != nil {
		return fmt.Errorf("exec %q: %w", command, err)
	}
	go func() { _ = cmd.Wait() }()
	return nil
}

// State helpers. The user state is JSON, so numbers come back as float64.

// remember saves a convenience value; a read-only disk is not an error for an action.
func (r *Runner) remember(key string, value any) { _ = r.env.State.Set(key, value) }

func (r *Runner) stateBool(key string, def bool) bool {
	v := r.env.State.Get(key)
	if v == nil {
		return def
	}
	return truthy(v)
}

func (r *Runner) stateString(key, def string) string {
	if s, ok := r.env.State.Get(key).(string); ok {
		return s
	}
	return def
}

func (r *Runner) stateInt(key string, def int) int { return intOf(r.env.State.Get(key), def) }

func intOf(v any, def int) int {
	switch n := v.(type) {
	case float64:
		return int(n)
	case int:
		return n
	case int64:
		return int(n)
	}
	return def
}

// truthy is Python's idea of true, which the 0.3.x state files were written with.
func truthy(v any) bool {
	switch x := v.(type) {
	case nil:
		return false
	case bool:
		return x
	case float64:
		return x != 0
	case int:
		return x != 0
	case string:
		return x != ""
	case []any:
		return len(x) > 0
	case map[string]any:
		return len(x) > 0
	}
	return true
}

func mod(a, n int) int { return ((a % n) + n) % n }
