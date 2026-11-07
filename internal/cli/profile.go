package cli

import (
	"fmt"

	"github.com/Order52/truesense/internal/cli/command"
)

func (c *CLI) profile() *command.Cmd {
	named := func(name string, run func(cc *command.Ctx, n string) error) *command.Cmd {
		return &command.Cmd{Name: name, Usage: "NAME", Run: func(cc *command.Ctx) error {
			if err := cc.Want(1, 1, "name"); err != nil {
				return err
			}
			return run(cc, cc.Arg(0))
		}}
	}
	save := &command.Cmd{Name: "save", Usage: "NAME", Short: "save the CURRENT state as a profile", Flags: stringFlag("description", "what the profile is for"), Run: func(cc *command.Ctx) error {
		if err := cc.Want(1, 1, "name"); err != nil {
			return err
		}
		path, err := c.api.SaveProfile(cc.Context, cc.Arg(0), flagString(cc, "description"))
		if err != nil {
			return err
		}
		cc.Emit(path, fmt.Sprintf("saved the current state as profile '%s'", cc.Arg(0)))
		return nil
	}}
	return &command.Cmd{
		Name: "profile", Short: "named bundles of settings (gaming, quiet, battery-saver, yours)",
		Run: c.profileList,
		Sub: []*command.Cmd{
			{Name: "list", Run: c.profileList},
			{Name: "next", Run: func(cc *command.Ctx) error {
				if err := cc.Want(0, 0); err != nil {
					return err
				}
				msg, err := c.api.NextProfile(cc.Context)
				if err != nil {
					return err
				}
				cc.Emit(msg, "")
				return nil
			}},
			named("show", func(cc *command.Ctx, n string) error {
				p, err := c.api.Profile(n)
				if err != nil {
					return err
				}
				text, err := c.api.ProfileTOML(p)
				if err != nil {
					return err
				}
				cc.Emit(p, text)
				return nil
			}),
			named("apply", func(cc *command.Ctx, n string) error {
				msg, err := c.api.ApplyProfile(cc.Context, n)
				if err != nil {
					return err
				}
				cc.Emit(msg, "")
				return nil
			}),
			save,
			named("delete", func(cc *command.Ctx, n string) error {
				if err := c.api.DeleteProfile(n); err != nil {
					return err
				}
				cc.Emit("deleted "+n, "")
				return nil
			}),
		},
	}
}

func (c *CLI) profileList(cc *command.Ctx) error {
	if err := cc.Want(0, 0); err != nil {
		return err
	}
	list := c.api.Profiles()
	lines := make([]string, len(list))
	for i, p := range list {
		lines[i] = fmt.Sprintf("%-16s [%7s] %s", p.Name, p.Source, p.Description)
	}
	cc.Emit(list, orElse(join(lines), "(no profiles)"))
	return nil
}
