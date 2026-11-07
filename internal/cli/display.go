package cli

import (
	"flag"
	"fmt"

	"github.com/Order52/truesense/internal/cli/command"
	"github.com/Order52/truesense/internal/hw"
)

func (c *CLI) display() *command.Cmd {
	return &command.Cmd{Name: "display", Short: "LCD overdrive", Sub: []*command.Cmd{
		{Name: "overdrive", Usage: "[on|off|toggle|status]", Run: func(cc *command.Ctx) error {
			return tri(cc, "LCD overdrive", nil, c.api.Overdrive, c.api.SetOverdrive)
		}},
	}}
}

func (c *CLI) boot() *command.Cmd {
	return &command.Cmd{Name: "boot", Short: "boot animation and sound", Sub: []*command.Cmd{
		{Name: "sound", Usage: "[on|off|toggle|status]", Run: func(cc *command.Ctx) error {
			return tri(cc, "boot sound", nil, optional(c.api.BootSound), c.api.SetBootSound)
		}},
	}}
}

func (c *CLI) gpu() *command.Cmd {
	return &command.Cmd{
		Name: "gpu", Short: "GPU mode (MUX); a change needs a reboot",
		Run: c.gpuStatus,
		Sub: []*command.Cmd{
			{Name: "status", Run: c.gpuStatus},
			{Name: "set", Usage: "optimus|nvidia|auto", Run: c.gpuSet},
			{Name: "undervolt", Usage: "get|list|show|use|reset|copy|save|delete|boot [NAME] [NEW]", Run: c.gpuUndervolt,
				Short: "GPU undervolt profiles (clock-curve offset + clock cap; reset on reboot; applying needs --confirm): get | list | show NAME | use NAME --confirm | reset | copy NAME [NEW] | save NAME --offset MHZ [--cap MHZ] | delete NAME | boot NAME|off --confirm",
				Flags: func(fs *flag.FlagSet) {
					declOptInt(fs, "offset", "")
					fs.Int("cap", 0, "")
					fs.Bool("confirm", false, "I understand this can crash games or the driver")
					fs.Bool("force", false, "allow values beyond the danger line")
				}},
		},
	}
}

func (c *CLI) gpuStatus(cc *command.Ctx) error {
	if err := cc.Want(0, 0); err != nil {
		return err
	}
	m, err := c.api.GPUMode()
	if err != nil {
		return err
	}
	cc.Emit(m, "GPU mode: "+m)
	return nil
}

func (c *CLI) gpuSet(cc *command.Ctx) error {
	if err := cc.Want(1, 1, "mode"); err != nil {
		return err
	}
	mode, err := cc.Choice(0, "mode", "", hw.GPUModeNames...)
	if err != nil {
		return err
	}
	m, err := c.api.SetGPUMode(mode)
	if err != nil {
		return err
	}
	cc.Emit(m, fmt.Sprintf("GPU mode: %s   (takes effect after a reboot)", m))
	return nil
}
