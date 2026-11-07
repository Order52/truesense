package api

import (
	"context"
	"fmt"
	"log/slog"
	"slices"

	"github.com/Order52/truesense/internal/actions"
	"github.com/Order52/truesense/internal/config"
	"github.com/Order52/truesense/internal/fnkeys"
	"github.com/Order52/truesense/internal/keybinds"
	"github.com/Order52/truesense/internal/perkeyfx"
)

// KeyBindings is what each PREDATOR key gesture does now and the press timing: the [keys]
// section of the configuration.
type KeyBindings struct {
	Profile    string   `json:"profile"`
	Tap        string   `json:"tap"`
	Double     string   `json:"double"`
	Triple     string   `json:"triple"`
	Long       string   `json:"long"`
	FnEsc      string   `json:"fn_esc"`
	LongMS     int      `json:"long_ms"`
	MultiMS    int      `json:"multi_ms"`
	PowerCycle []string `json:"power_cycle"`
}

// Action returns the action bound to a gesture.
func (k KeyBindings) Action(gesture string) string {
	return map[string]string{"tap": k.Tap, "double": k.Double, "triple": k.Triple, "long": k.Long, "fn_esc": k.FnEsc}[gesture]
}

// KeyBindings reads the bindings.
func (a *API) KeyBindings() (KeyBindings, error) {
	c, err := a.LoadConfig()
	if err != nil {
		return KeyBindings{}, err
	}
	k := c.Keys
	return KeyBindings{Profile: k.Profile, Tap: k.Tap, Double: k.Double, Triple: k.Triple, Long: k.Long, FnEsc: k.FnEsc, LongMS: k.LongMS, MultiMS: k.MultiMS, PowerCycle: nonNil(k.PowerCycle)}, nil
}

// KeyActions is the catalogue of actions a gesture can be bound to.
func (a *API) KeyActions() string { return actions.Catalogue() }

// KeyGestures are the bindable gestures.
func KeyGestures() []string { return keybinds.Gestures() }

// SetKeyAction binds an action to a gesture. The action is checked now, never at key-press
// time; the key service reloads by itself.
func (a *API) SetKeyAction(gesture, action string) error {
	if err := a.runner.Validate(action); err != nil {
		return fmt.Errorf("%w   (truesense keys actions)", err)
	}
	return config.WriteUser(map[string]any{"keys": map[string]any{gesture: action, "profile": "custom"}}, a.UserConfig)
}

// ResetKeys puts the default action back on one gesture, or on all of them, and answers
// which.
func (a *API) ResetKeys(gesture string) ([]string, error) {
	gestures := KeyGestures()
	if gesture != "" {
		gestures = []string{gesture}
	}
	d := config.Default().Keys
	defaults := KeyBindings{Tap: d.Tap, Double: d.Double, Triple: d.Triple, Long: d.Long, FnEsc: d.FnEsc}
	update := map[string]any{}
	for _, g := range gestures {
		update[g] = defaults.Action(g)
	}
	return gestures, config.WriteUser(map[string]any{"keys": update}, a.UserConfig)
}

// TestAction runs an action now, as the key would, and answers its message.
func (a *API) TestAction(ctx context.Context, action string) (string, error) {
	return a.runner.Run(ctx, action)
}

// KeyTiming is the long-press and multi-tap timing in milliseconds.
type KeyTiming struct {
	LongMS  int `json:"long_ms"`
	MultiMS int `json:"multi_ms"`
}

// KeyTiming reads the timing.
func (a *API) KeyTiming() (KeyTiming, error) {
	k, err := a.KeyBindings()
	return KeyTiming{LongMS: k.LongMS, MultiMS: k.MultiMS}, err
}

// SetKeyTiming changes the timing (zero leaves a value alone) within the allowed range.
func (a *API) SetKeyTiming(longMS, multiMS int) (KeyTiming, error) {
	upd := map[string]any{}
	if longMS != 0 {
		if longMS < keybinds.LongMSMin || longMS > keybinds.LongMSMax {
			return KeyTiming{}, fmt.Errorf("long_ms is %d..%d", keybinds.LongMSMin, keybinds.LongMSMax)
		}
		upd["long_ms"] = longMS
	}
	if multiMS != 0 {
		if multiMS < keybinds.MultiMSMin || multiMS > keybinds.MultiMSMax {
			return KeyTiming{}, fmt.Errorf("multi_ms is %d..%d", keybinds.MultiMSMin, keybinds.MultiMSMax)
		}
		upd["multi_ms"] = multiMS
	}
	return KeyTiming{LongMS: longMS, MultiMS: multiMS}, config.WriteUser(map[string]any{"keys": upd}, a.UserConfig)
}

// KeyProfiles lists the key binding profiles with the one in use starred.
func (a *API) KeyProfiles() (NamedList, error) {
	cur, err := a.binds.Current()
	if err != nil {
		return NamedList{}, err
	}
	l := NamedList{Names: a.binds.Names(), Current: cur.Profile, Descriptions: map[string]string{}}
	for _, n := range l.Names {
		if p, ok := keybinds.Builtin(n); ok {
			l.Descriptions[n] = p.Description
		}
	}
	return l, nil
}

// KeyProfileInUse is the bindings in force and the profile they came from.
func (a *API) KeyProfileInUse() (keybinds.Active, error) { return a.binds.Current() }

// KeyProfile reads one profile, built-in or yours.
func (a *API) KeyProfile(name string) (keybinds.Profile, error) { return a.binds.Load(name) }

// UseKeyProfile makes a profile the active one (the key service reloads by itself).
func (a *API) UseKeyProfile(name string) error {
	if name == "" {
		return fmt.Errorf("which profile? (truesense keys profile list)")
	}
	_, err := a.binds.Use(name)
	return err
}

// CopyKeyProfile saves a copy under newName (NAME-copy when empty) and answers the name.
func (a *API) CopyKeyProfile(name, newName string) (string, error) {
	if name == "" {
		return "", fmt.Errorf("copy which profile?")
	}
	return a.binds.Copy(name, newName)
}

// KeyProfileEdit is what `keys profile save` changes: the action of each gesture and the
// timing, when given, over the profile to start from.
type KeyProfileEdit struct {
	From            string
	Actions         map[string]string // gesture -> action
	LongMS, MultiMS *int
}

// SaveKeyProfile saves a profile: start from From (or from your own profile of that name,
// else the defaults) and change what the edit gives.
func (a *API) SaveKeyProfile(name string, e KeyProfileEdit) (keybinds.Profile, error) {
	if name == "" {
		return keybinds.Profile{}, fmt.Errorf("save under which name?")
	}
	p := keybinds.Profile{Bindings: keybinds.Default()}
	switch {
	case e.From != "":
		b, err := a.binds.Load(e.From)
		if err != nil {
			return p, err
		}
		p = b
	case !slices.Contains(keybinds.BuiltinNames(), name) && slices.Contains(a.binds.Names(), name):
		b, err := a.binds.Load(name)
		if err != nil {
			return p, err
		}
		p = b
	}
	for g, act := range e.Actions {
		p.SetAction(g, act)
	}
	if e.LongMS != nil {
		p.LongMS = *e.LongMS
	}
	if e.MultiMS != nil {
		p.MultiMS = *e.MultiMS
	}
	return a.binds.Save(name, p)
}

// DeleteKeyProfile removes one of your profiles.
func (a *API) DeleteKeyProfile(name string) error { return a.binds.Delete(name) }

// RunKeyService runs the key service in the foreground until ctx ends: the PREDATOR key,
// Fn+Esc and Fn+F10, and the per-key lighting animations. It is what the systemd user service
// runs.
func (a *API) RunKeyService(ctx context.Context, log *slog.Logger) error {
	fx := perkeyfx.NewRunner(perkeyfx.NewController(a.s.User, a.s.KB).Get, a.s.KB)
	return fnkeys.Run(ctx, fnkeys.Env{
		SysRoot: a.SysRoot, DevRoot: a.DevRoot,
		Config: a.LoadConfig, ConfigPath: a.UserConfig, Actions: a.runner, Validate: a.runner.Validate, OSD: a.s.OSD, Log: log,
		Background: []func(context.Context){fx.Run},
	})
}
