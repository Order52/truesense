package adapt

import (
	"context"

	"github.com/Order52/truesense/internal/api"
	"github.com/Order52/truesense/internal/hw"
	"github.com/Order52/truesense/internal/keyboard"
	"github.com/Order52/truesense/internal/tui"
)

// ---- LightBackend

func (b *backend) KeyboardCaps() tui.KeyboardCaps {
	return tui.KeyboardCaps{Effects: api.KeyboardEffects(), MaxBrightness: keyboard.MaxBright, MinSpeed: keyboard.SpeedMin, MaxSpeed: keyboard.SpeedMax}
}

func (b *backend) KeyboardLast(context.Context) (tui.KeyboardLook, error) {
	l := b.a.KeyboardLast()
	return tui.KeyboardLook{Name: l.Name, Color: l.Color, Speed: l.Speed, Brightness: l.Brightness, Direction: l.Direction}, nil
}

func (b *backend) KeyboardBrightness(context.Context) (int, error) { return b.a.KeyboardBrightness() }

func (b *backend) SetKeyboard(_ context.Context, l tui.KeyboardLook) error {
	return b.a.ApplyKeyboardLook(api.KeyboardLook{Name: l.Name, Color: l.Color, Speed: l.Speed, Brightness: l.Brightness, Direction: l.Direction})
}

func (b *backend) SavedLightings() []string { return b.a.LightingProfileNames() }

func (b *backend) ApplySavedLighting(_ context.Context, name string) error {
	return b.a.ApplyLightingProfile(name)
}

func (b *backend) RearModes() []string { return append([]string(nil), hw.RearModeNames...) }

func (b *backend) Rear(context.Context) (tui.RearLook, error) {
	r, err := b.a.Rear()
	return tui.RearLook{Mode: r.Mode, Color: r.Color, Speed: r.Speed, Brightness: r.Brightness, Direction: r.Direction}, err
}

func (b *backend) SetRear(_ context.Context, l tui.RearLook, keepZones bool) error {
	return b.a.SetRearLook(api.RearSpec{Mode: l.Mode, Color: l.Color, Speed: l.Speed, Brightness: l.Brightness, Direction: l.Direction}, keepZones)
}
