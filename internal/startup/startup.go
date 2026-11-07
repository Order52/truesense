// Package startup is what the lights and the profile do at login (`truesense startup apply`,
// run by the truesense-startup user service) and the validated setter behind `truesense
// startup set` and the window.
//
// The [startup] section of the configuration chooses:
//
//	keyboard, rear   last     put back what was set last (saved automatically on every change)
//	                 default  the stock look
//	                 none     leave it as the firmware has it
//	                 off      dark
//	                 keyboard also: profile:NAME (a saved lighting profile); both: look:NAME
//	profile          a full profile (power, fans, lighting, battery, audio) applied first, or empty
package startup

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/Order52/truesense/internal/config"
	"github.com/Order52/truesense/internal/hw"
	"github.com/Order52/truesense/internal/keyboard"
	"github.com/Order52/truesense/internal/looks"
)

// ProfileSet is the whole-setup profiles: *profiles.Store.
type ProfileSet interface {
	Names() []string
	Apply(ctx context.Context, name string) (string, error)
}

// KeyProfileSet is the saved per-key lighting profiles: *keyprofiles.Store.
type KeyProfileSet interface {
	Names() []string
	Exists(name string) bool
	Apply(name string, brightness int) error
}

// LookSet is the saved lighting looks: *looks.Store.
type LookSet interface {
	Names() []string
	Load(name string) (*looks.Look, error)
	Apply(look *looks.Look) ([]string, error)
}

// Keyboard is the backlight controller: *keyboard.Keyboard.
type Keyboard interface {
	Last() keyboard.State
	Replay(s keyboard.State) (string, error)
	RestoreDefault() error
	Off() error
}

// Rear is the rear light bar: *hw.HW.
type Rear interface {
	RearSet(o hw.RearOpts) (hw.Rear, error)
}

// State is the per-user memory (the rear light's last setting): *userstate.Store.
type State interface {
	GetInto(key string, out any) error
}

// Env is everything startup reaches; every field except the retry settings is required.
type Env struct {
	// Config reads the effective configuration (system file, then the user's).
	Config func() (*config.Config, error)
	// ConfigPath is the user config file SetOption writes (default config.UserPath()).
	ConfigPath  string
	Profiles    ProfileSet
	KeyProfiles KeyProfileSet
	Looks       LookSet
	Keyboard    Keyboard
	Rear        Rear
	State       State

	// Retries is how many more times a failed keyboard or rear step is tried, Delay apart,
	// for a keyboard that has not enumerated yet at login. Zero (the default) tries once, as
	// 0.3.x did.
	Retries int
	Delay   time.Duration
	// Sleep waits Delay between attempts; nil uses the real clock. It returns early when ctx
	// ends.
	Sleep func(ctx context.Context, d time.Duration)
}

// Startup applies and changes the login settings.
type Startup struct{ env Env }

// New returns a Startup over env.
func New(env Env) *Startup {
	if env.Sleep == nil {
		env.Sleep = realSleep
	}
	if env.ConfigPath == "" {
		env.ConfigPath = config.UserPath()
	}
	return &Startup{env: env}
}

func realSleep(ctx context.Context, d time.Duration) {
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-t.C:
	case <-ctx.Done():
	}
}

// SetOption sets what happens at login: target keyboard | rear | profile ("none" clears the
// profile). It is validated the same way for the command line and the window: the profile,
// lighting profile or look must exist, and the resulting configuration must be valid. It
// returns the stored value.
func (s *Startup) SetOption(target, value string) (string, error) {
	if target != "keyboard" && target != "rear" && target != "profile" {
		return "", errors.New("target: keyboard | rear | profile")
	}
	val := value
	if target == "profile" && value == "none" {
		val = ""
	}
	if target == "profile" && val != "" && !contains(s.env.Profiles.Names(), val) {
		return "", fmt.Errorf("no profile %s: %s", pyRepr(val), strings.Join(s.env.Profiles.Names(), ", "))
	}
	if name, ok := strings.CutPrefix(val, "profile:"); target == "keyboard" && ok && !s.env.KeyProfiles.Exists(name) {
		return "", fmt.Errorf("no lighting profile %s: %s", pyRepr(name), strings.Join(s.env.KeyProfiles.Names(), ", "))
	}
	if name, ok := strings.CutPrefix(val, "look:"); ok && !contains(s.env.Looks.Names(), name) {
		return "", fmt.Errorf("no look %s: %s", pyRepr(name), strings.Join(s.env.Looks.Names(), ", "))
	}
	cfg, err := s.env.Config()
	if err != nil {
		return "", fmt.Errorf("read configuration: %w", err)
	}
	switch target {
	case "keyboard":
		cfg.Startup.Keyboard = val
	case "rear":
		cfg.Startup.Rear = val
	case "profile":
		cfg.Startup.Profile = val
	}
	for _, problem := range cfg.Validate() {
		if strings.Contains(problem, "[startup]") {
			return "", errors.New(problem)
		}
	}
	if err := config.WriteUser(map[string]any{"startup": map[string]any{target: val}}, s.env.ConfigPath); err != nil {
		return "", fmt.Errorf("save startup %s: %w", target, err)
	}
	return val, nil
}

// pyRepr quotes a name the way the 0.3.x messages did (single quotes).
func pyRepr(s string) string {
	if strings.Contains(s, "'") && !strings.Contains(s, `"`) {
		return `"` + s + `"`
	}
	return "'" + strings.ReplaceAll(s, "'", `\'`) + "'"
}

func contains(list []string, v string) bool {
	for _, x := range list {
		if x == v {
			return true
		}
	}
	return false
}

// Apply applies the login settings: the profile first, then the lighting. It returns one
// line per step ("keyboard: stock look", "rear: FAILED (...)"); a step that fails never stops
// the others, because the login service must leave the machine as usable as it can.
func (s *Startup) Apply(ctx context.Context) ([]string, error) {
	cfg, err := s.env.Config()
	if err != nil {
		return nil, fmt.Errorf("read configuration: %w", err)
	}
	st := cfg.Startup
	var done []string
	step := func(label string, retry bool, fn func() (string, error)) {
		attempts := 1
		if retry {
			attempts += max(0, s.env.Retries)
		}
		var res string
		var err error
		for i := 0; i < attempts; i++ {
			if i > 0 {
				s.env.Sleep(ctx, s.env.Delay)
				if ctx.Err() != nil {
					break
				}
			}
			if res, err = fn(); err == nil {
				break
			}
		}
		switch {
		case err != nil:
			done = append(done, fmt.Sprintf("%s: FAILED (%v)", label, err))
		case res == "":
			done = append(done, label+": nothing saved")
		default:
			done = append(done, label+": "+res)
		}
	}

	if st.Profile != "" {
		step("profile", false, func() (string, error) { return s.env.Profiles.Apply(ctx, st.Profile) })
	}
	k, r := st.Keyboard, st.Rear
	kLook, rLook := strings.HasPrefix(k, "look:"), strings.HasPrefix(r, "look:")
	if kLook || rLook { // a saved look sets keyboard and rear together
		// 0.3.x loads one look, the keyboard's when both are looks, and takes both parts from it.
		name := strings.TrimPrefix(r, "look:")
		if kLook {
			name = strings.TrimPrefix(k, "look:")
		}
		look, err := s.env.Looks.Load(name)
		if err != nil {
			if kLook {
				done = append(done, fmt.Sprintf("keyboard: FAILED (%v)", err))
			}
			if rLook {
				done = append(done, fmt.Sprintf("rear: FAILED (%v)", err))
			}
		} else {
			if kLook {
				step("keyboard", true, func() (string, error) { return s.lookPart(&looks.Look{Keyboard: look.Keyboard}) })
			}
			if rLook {
				step("rear", true, func() (string, error) { return s.lookPart(&looks.Look{Rear: look.Rear}) })
			}
		}
		if kLook {
			k = "none"
		}
		if rLook {
			r = "none"
		}
	}

	switch {
	case k == "last":
		step("keyboard", true, func() (string, error) { return s.env.Keyboard.Replay(s.env.Keyboard.Last()) })
	case k == "default":
		step("keyboard", true, func() (string, error) { return "stock look", s.env.Keyboard.RestoreDefault() })
	case k == "off":
		step("keyboard", true, func() (string, error) { return "off", s.env.Keyboard.Off() })
	case strings.HasPrefix(k, "profile:"):
		step("keyboard", true, func() (string, error) {
			return k, s.env.KeyProfiles.Apply(k[len("profile:"):], keyboard.KeepBrightness)
		})
	}

	switch r {
	case "last":
		step("rear", true, s.rearLast)
	case "default":
		step("rear", true, func() (string, error) { return "stock look", s.rearSet("breathing", "3723c7", 3, 100, 1) })
	case "off":
		step("rear", true, func() (string, error) { return "off", s.rearSet("static", "00aec7", 3, 0, 1) })
	}
	return done, nil
}

// lookPart applies one half of a look; a look without that half leaves the lights alone.
func (s *Startup) lookPart(part *looks.Look) (string, error) {
	if part.IsEmpty() {
		return "unchanged", nil
	}
	items, err := s.env.Looks.Apply(part)
	if err != nil {
		return "", err
	}
	if len(items) == 0 {
		return "unchanged", nil
	}
	return items[0], nil
}

func (s *Startup) rearSet(mode, color string, speed, brightness, direction int) error {
	o := hw.DefaultRear()
	o.Mode, o.Color, o.Speed, o.Brightness, o.Direction, o.Verify = mode, color, speed, brightness, direction, true
	_, err := s.env.Rear.RearSet(o)
	return err
}

// rearLast puts back the rear light as hw remembered it (rear_state in the user state).
func (s *Startup) rearLast() (string, error) {
	var st looks.Rear
	if s.env.State.GetInto("rear_state", &st) != nil {
		return "", nil // absent, or not a setting we wrote: nothing saved
	}
	if st.Mode == "" || st.Color == "" || st.Speed == nil || st.Brightness == nil {
		return "", errors.New("the saved rear light setting is incomplete")
	}
	o := hw.DefaultRear()
	o.Mode, o.Color, o.Speed, o.Brightness, o.Zones, o.Verify = st.Mode, st.Color, *st.Speed, *st.Brightness, st.Zones, true
	if st.Direction != nil {
		o.Direction = *st.Direction
	}
	if _, err := s.env.Rear.RearSet(o); err != nil {
		return "", err
	}
	return "restored", nil
}
