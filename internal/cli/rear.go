package cli

import (
	"flag"
	"strings"

	"github.com/Order52/truesense/internal/api"
	"github.com/Order52/truesense/internal/cli/command"
	"github.com/Order52/truesense/internal/hw"
)

func (c *CLI) rear() *command.Cmd {
	power := func(action string) func(*command.Ctx) error {
		return func(cc *command.Ctx) error {
			if err := cc.Want(0, 0); err != nil {
				return err
			}
			msg, err := c.api.RearPower(cc.Context, action)
			if err != nil {
				return err
			}
			cc.Emit(msg, "")
			return nil
		}
	}
	return &command.Cmd{
		Name: "rear", Short: "rear light bar",
		Run: c.rearStatus,
		Sub: []*command.Cmd{
			{Name: "status", Run: c.rearStatus},
			{Name: "on", Run: power("on")},
			{Name: "off", Run: power("off")},
			{Name: "toggle", Run: power("toggle")},
			{Name: "set", Usage: "static|breathing|neon|wave|twinkling", Run: c.rearSet,
				Flags: func(fs *flag.FlagSet) {
					fs.String("color", "ffffff", "colour, RRGGBB")
					fs.Int("speed", 3, "speed")
					fs.Int("brightness", 100, "brightness 0..100")
					fs.Int("direction", 1, "0 or 1")
					fs.String("zones", "", "static only: one colour, or three (left,middle,right), e.g. ff0000,00ff00,0000ff")
				}},
		},
	}
}

func (c *CLI) rearStatus(cc *command.Ctx) error {
	if err := cc.Want(0, 0); err != nil {
		return err
	}
	r, err := c.api.Rear()
	if err != nil {
		return err
	}
	cc.Emit(r, "")
	return nil
}

func (c *CLI) rearSet(cc *command.Ctx) error {
	if err := cc.Want(1, 1, "effect"); err != nil {
		return err
	}
	effect, err := cc.Choice(0, "effect", "", hw.RearModeNames...)
	if err != nil {
		return err
	}
	if err := oneOfFlag(cc, "direction", "0", "1"); err != nil {
		return err
	}
	spec := api.RearSpec{Mode: effect, Color: flagString(cc, "color"), Speed: flagInt(cc, "speed"), Brightness: flagInt(cc, "brightness"), Direction: flagInt(cc, "direction")}
	if z := flagString(cc, "zones"); z != "" {
		spec.Zones = strings.Split(z, ",")
	}
	r, err := c.api.SetRear(spec)
	if err != nil {
		return err
	}
	cc.Emit(r, "")
	return nil
}
