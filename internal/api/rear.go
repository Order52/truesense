package api

import (
	"context"

	"github.com/Order52/truesense/internal/hw"
)

// Rear reads the rear light bar.
func (a *API) Rear() (hw.Rear, error) { return a.s.HW.RearGet() }

// RearSpec is a complete light bar setting as the command line gives it.
type RearSpec struct {
	Mode       string
	Color      string // RRGGBB
	Speed      int    // 1..5
	Brightness int    // 0..100
	Direction  int    // 0 or 1
	Zones      []string
}

// DefaultRearSpec is what `rear set EFFECT` uses for what is not given.
func DefaultRearSpec(mode string) RearSpec {
	return RearSpec{Mode: mode, Color: "ffffff", Speed: 3, Brightness: 100, Direction: 1}
}

// SetRear writes the light bar and answers what the firmware reports back. Zones (one colour,
// or three: left, middle, right) apply to the static effect only.
func (a *API) SetRear(spec RearSpec) (hw.Rear, error) {
	return a.s.HW.RearSet(hw.RearOpts{Mode: spec.Mode, Color: spec.Color, Speed: spec.Speed, Brightness: spec.Brightness, Direction: spec.Direction, Zones: spec.Zones, Verify: true})
}

// RearPower switches the light bar on, off or (toggle) the other way, keeping its look and
// the three part colours of the static effect, and answers the message the key would show.
func (a *API) RearPower(ctx context.Context, action string) (string, error) {
	return a.action(ctx, "rear:"+action)
}
