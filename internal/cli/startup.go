package cli

import (
	"fmt"

	"github.com/Order52/truesense/internal/cli/command"
)

func (c *CLI) startup() *command.Cmd {
	return &command.Cmd{
		Name: "startup", Short: "what the lights and profile do at login",
		Run: c.startupShow,
		Sub: []*command.Cmd{
			{Name: "show", Run: c.startupShow},
			{Name: "apply", Short: "do it now (the user service does this at login)", Run: func(cc *command.Ctx) error {
				if err := cc.Want(0, 0); err != nil {
					return err
				}
				done, err := c.api.ApplyStartup(cc.Context)
				if err != nil {
					return err
				}
				cc.Emit(done, join(done))
				return nil
			}},
			{Name: "boot", Usage: "[NAME|current|clear]", Run: c.startupBoot,
				Short: "the look applied at BOOT (before login) and after suspend: NAME | current | clear (no argument: show)"},
			{Name: "set", Usage: "keyboard|rear|profile VALUE", Run: func(cc *command.Ctx) error {
				if err := cc.Want(2, 2, "target", "value"); err != nil {
					return err
				}
				target, err := cc.Choice(0, "target", "", "keyboard", "rear", "profile")
				if err != nil {
					return err
				}
				val, err := c.api.SetStartup(target, cc.Arg(1))
				if err != nil {
					return err
				}
				cc.Emit(val, fmt.Sprintf("at login, %s: %s", target, orElse(val, "none")))
				return nil
			}},
		},
	}
}

func (c *CLI) startupShow(cc *command.Ctx) error {
	if err := cc.Want(0, 0); err != nil {
		return err
	}
	s, err := c.api.Startup()
	if err != nil {
		return err
	}
	cc.Emit(s, "")
	return nil
}

func (c *CLI) startupBoot(cc *command.Ctx) error {
	if err := cc.Want(0, 1, "what"); err != nil {
		return err
	}
	cur, err := c.api.BootLook()
	if err != nil {
		return err
	}
	switch what := cc.Arg(0); what {
	case "":
		text := "no look is applied at boot"
		if cur != nil {
			text = "at boot and after suspend: " + c.api.DescribeLook(cur)
		}
		cc.Emit(cur, text)
	case "clear":
		if err := c.api.ClearBootLook(); err != nil {
			return err
		}
		cc.Emit(nil, "nothing is applied at boot any more")
	default:
		look, err := c.api.SetBootLook(what)
		if err != nil {
			return err
		}
		cc.Emit(look, "at boot and after suspend: "+c.api.DescribeLook(look))
	}
	return nil
}
