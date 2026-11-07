package api

import (
	"github.com/Order52/truesense/internal/hw"
	"github.com/Order52/truesense/internal/keyboard"
)

// userKeyboardOn is the user-state key the key actions keep: false while the light is off.
const userKeyboardOn = "keyboard_on"

// KeyboardLook is a keyboard backlight setting as a window or terminal edits it. The zero
// values of Color and Speed mean "unknown".
type KeyboardLook struct {
	Name       string
	Color      string
	Speed      int
	Brightness int
	Direction  int
}

// KeyboardLast is what this tool last put on the keyboard (the hardware cannot be asked).
// Name "off" means the light was switched off (by a key, a command or a window); an empty
// Name means nothing is remembered.
func (a *API) KeyboardLast() KeyboardLook {
	st := a.s.KB.Last()
	look := KeyboardLook{Name: st.Name, Color: st.Color, Brightness: st.BrightnessOr(0), Direction: st.DirectionOr(0)}
	if st.Speed != nil {
		look.Speed = *st.Speed
	}
	var on bool
	if a.s.User.GetInto(userKeyboardOn, &on) == nil && !on {
		look.Name = "off"
	}
	return look
}

// ApplyKeyboardLook puts a look on the keyboard; Name "off" switches the light off keeping the
// look it had, anything else is a hardware effect and brings the light back on.
func (a *API) ApplyKeyboardLook(l KeyboardLook) error {
	if l.Name == "off" {
		if err := a.s.KB.Off(); err != nil {
			return err
		}
		return a.rememberKeyboardOn(false)
	}
	if err := a.s.KB.SetEffect(l.Name, l.Color, l.Speed, l.Brightness, l.Direction); err != nil {
		return err
	}
	return a.rememberKeyboardOn(true)
}

func (a *API) rememberKeyboardOn(on bool) error { return a.s.User.Set(userKeyboardOn, on) }

// KeyboardEffects lists the hardware effects in display order ("off" first).
func KeyboardEffects() []string { return keyboard.EffectNames() }

// LightingProfileNames lists the saved per-key lightings.
func (a *API) LightingProfileNames() []string { return nonNil(a.s.KeyProf.Names()) }

// ApplyLightingProfile puts a saved per-key lighting on the keyboard, at the brightness the
// keyboard has.
func (a *API) ApplyLightingProfile(name string) error {
	return a.s.KeyProf.Apply(name, keyboard.KeepBrightness)
}

// SetRearLook writes the light bar without waiting for the read-back (a slider or a colour
// drag follows with the next write at once). With keepZones the three part colours of the
// static effect that the window saved are written again, so changing the brightness does not
// flatten them.
func (a *API) SetRearLook(spec RearSpec, keepZones bool) error {
	if keepZones && spec.Mode == "static" && len(spec.Zones) == 0 {
		var zones []string
		if a.s.User.GetInto("rear_zones", &zones) == nil && len(zones) == 3 {
			spec.Zones = zones
		}
	}
	_, err := a.s.HW.RearSet(hw.RearOpts{Mode: spec.Mode, Color: spec.Color, Speed: spec.Speed, Brightness: spec.Brightness, Direction: spec.Direction, Zones: spec.Zones})
	return err
}
