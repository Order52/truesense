package cli

import (
	"fmt"

	"github.com/Order52/truesense/internal/cli/command"
)

func (c *CLI) look() *command.Cmd {
	named := func(name string, run func(cc *command.Ctx, look string) error) *command.Cmd {
		return &command.Cmd{Name: name, Usage: "NAME", Run: func(cc *command.Ctx) error {
			if err := cc.Want(1, 1, "name"); err != nil {
				return err
			}
			return run(cc, cc.Arg(0))
		}}
	}
	save := named("save", func(cc *command.Ctx, name string) error {
		l, err := c.api.SaveLook(name)
		if err != nil {
			return err
		}
		cc.Emit(l, fmt.Sprintf("saved look '%s': %s", name, c.api.DescribeLook(l)))
		return nil
	})
	save.Short = "save what is on the lights now"
	return &command.Cmd{
		Name: "look", Short: "saved lighting looks (keyboard effect/brightness/colours + rear light)",
		Run: c.lookList,
		Sub: []*command.Cmd{
			{Name: "list", Run: c.lookList},
			save,
			named("apply", func(cc *command.Ctx, name string) error {
				done, err := c.api.ApplyLook(name)
				if err != nil {
					return err
				}
				cc.Emit(done, join(done))
				return nil
			}),
			named("show", func(cc *command.Ctx, name string) error {
				l, err := c.api.Look(name)
				if err != nil {
					return err
				}
				cc.Emit(l, fmt.Sprintf("%s: %s", name, c.api.DescribeLook(l)))
				return nil
			}),
			named("delete", func(cc *command.Ctx, name string) error {
				if err := c.api.DeleteLook(name); err != nil {
					return err
				}
				cc.Emit(name, fmt.Sprintf("deleted look '%s'", name))
				return nil
			}),
		},
	}
}

func (c *CLI) lookList(cc *command.Ctx) error {
	if err := cc.Want(0, 0); err != nil {
		return err
	}
	names := c.api.Looks()
	cc.Emit(names, orElse(join(names), "(no saved looks; `truesense look save NAME`)"))
	return nil
}
