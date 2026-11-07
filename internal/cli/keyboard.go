package cli

import (
	"flag"
	"fmt"
	"path/filepath"
	"sort"
	"strings"

	"github.com/Order52/truesense/internal/api"
	"github.com/Order52/truesense/internal/cli/command"
	"github.com/Order52/truesense/internal/keyboard"
	"github.com/Order52/truesense/internal/perkeyfx"
)

func (c *CLI) keyboard() *command.Cmd {
	brightness := func(fs *flag.FlagSet) { declOptInt(fs, "brightness", "") }
	sent := func(cc *command.Ctx, err error) error {
		if err != nil {
			return err
		}
		cc.Emit("sent", "")
		return nil
	}
	power := func(action string) func(*command.Ctx) error {
		return func(cc *command.Ctx) error {
			if err := cc.Want(0, 0); err != nil {
				return err
			}
			msg, err := c.api.KeyboardPower(cc.Context, action)
			if err != nil {
				return err
			}
			cc.Emit(msg, "")
			return nil
		}
	}
	return &command.Cmd{
		Name: "keyboard", Short: "keyboard lighting",
		Run: c.keyboardStatus,
		Sub: []*command.Cmd{
			{Name: "status", Run: c.keyboardStatus},
			{Name: "on", Run: power("on")},
			{Name: "off", Run: power("off")},
			{Name: "toggle", Run: power("toggle")},
			{Name: "restore", Short: "back to the stock cyan per-key state", Run: func(cc *command.Ctx) error {
				if err := cc.Want(0, 0); err != nil {
					return err
				}
				return sent(cc, c.api.RestoreKeyboard())
			}},
			{Name: "list-keys", Run: func(cc *command.Ctx) error {
				if err := cc.Want(0, 0); err != nil {
					return err
				}
				cc.Emit(c.api.ListKeys(), "")
				return nil
			}},
			{Name: "effect", Usage: "NAME", Run: c.keyboardEffect, Flags: func(fs *flag.FlagSet) {
				fs.String("color", "00aec7", "colour, RRGGBB")
				fs.Int("speed", 5, "speed")
				brightness(fs)
				fs.Int("direction", 0, "direction")
			}},
			{Name: "fx", Usage: "MODE", Run: c.keyboardFX, Flags: func(fs *flag.FlagSet) {
				fs.Int("speed", 5, "1..10")
				fs.String("keys", "", "comma list of keys or groups, e.g. wasd,arrows,f5 (default: all keys)")
			}, Short: "per-key animation on chosen keys (drawn by the key service, about 5 frames a second): off | " + strings.Join(perkeyfx.ModeNames(), " | ")},
			{Name: "speed", Usage: "LEVEL", Short: "animation speed of the running effect, 1 (slowest) .. 12 (fastest)", Run: func(cc *command.Ctx) error {
				if err := cc.Want(1, 1, "level"); err != nil {
					return err
				}
				level, err := intArg("level", cc.Arg(0))
				if err != nil {
					return err
				}
				if err := c.api.SetKeyboardSpeed(level); err != nil {
					return err
				}
				cc.Emit(fmt.Sprintf("keyboard effect speed: %d / %d", level, keyboard.SpeedMax), "")
				return nil
			}},
			{Name: "color", Usage: "COLOR", Short: "change the colour of the current effect (stays in the same mode)", Run: func(cc *command.Ctx) error {
				if err := cc.Want(1, 1, "color"); err != nil {
					return err
				}
				return sent(cc, c.api.SetKeyboardColor(cc.Arg(0)))
			}},
			{Name: "paint", Usage: "KEY=COLOR ...", Short: "colour some keys and keep the rest, e.g. paint w=ff0000 arrows=00ff00 esc=ffffff", Flags: brightness, Run: func(cc *command.Ctx) error {
				if err := cc.Want(1, len(cc.Args)+1, "items"); err != nil {
					return err
				}
				n, err := c.api.PaintKeys(cc.Args, flagOptInt(cc, "brightness"))
				if err != nil {
					return err
				}
				cc.Emit(fmt.Sprintf("painted %d key(s)", n), "")
				return nil
			}},
			{Name: "layout", Short: "key names and positions (for other tools)", Run: func(cc *command.Ctx) error {
				if err := cc.Want(0, 0); err != nil {
					return err
				}
				cc.Emit(c.api.KeyLayout(), "")
				return nil
			}},
			{Name: "design", Usage: "list|apply|save [NAME]", Run: c.keyboardDesign, Flags: brightness,
				Short: "still designs: list | apply NAME | save NAME (yours go in ~/.config/truesense/designs/NAME.json)"},
			{Name: "image", Usage: "FILE", Short: "paint a picture across the keys (PNG, JPEG or GIF)", Flags: brightness, Run: func(cc *command.Ctx) error {
				if err := cc.Want(1, 1, "file"); err != nil {
					return err
				}
				if err := c.api.ImageKeyboard(cc.Arg(0), flagOptInt(cc, "brightness")); err != nil {
					return err
				}
				cc.Emit(fmt.Sprintf("painted %s across the keys (scaled to 22 x 6 keys); look at the keyboard, and `truesense keyboard status` shows the stored colours", filepath.Base(cc.Arg(0))), "")
				return nil
			}},
			{Name: "fill", Usage: "COLOR", Flags: brightness, Run: func(cc *command.Ctx) error {
				if err := cc.Want(1, 1, "color"); err != nil {
					return err
				}
				return sent(cc, c.api.FillKeyboard(cc.Arg(0), flagOptInt(cc, "brightness")))
			}},
			{Name: "gradient", Usage: "FROM_COLOR TO_COLOR", Flags: brightness, Run: func(cc *command.Ctx) error {
				if err := cc.Want(2, 2, "from_color", "to_color"); err != nil {
					return err
				}
				return sent(cc, c.api.GradientKeyboard(cc.Arg(0), cc.Arg(1), flagOptInt(cc, "brightness")))
			}},
			{Name: "rainbow", Flags: brightness, Run: func(cc *command.Ctx) error {
				if err := cc.Want(0, 0); err != nil {
					return err
				}
				return sent(cc, c.api.RainbowKeyboard(flagOptInt(cc, "brightness")))
			}},
			{Name: "keys", Usage: "KEY=COLOR ...", Short: `per-key colours, e.g. keys "wasd=ff0000" "esc=00ff00" --base 101010`, Flags: func(fs *flag.FlagSet) {
				fs.String("base", "000000", "colour of every other key")
				declOptInt(fs, "brightness", "")
			}, Run: func(cc *command.Ctx) error {
				if err := cc.Want(1, len(cc.Args)+1, "items"); err != nil {
					return err
				}
				return sent(cc, c.api.SetKeyColors(cc.Args, flagString(cc, "base"), flagOptInt(cc, "brightness")))
			}},
			{Name: "brightness", Usage: "[LEVEL]", Short: "0..50 (also Fn+F11/F12)", Run: c.keyboardBrightness},
			{Name: "timeout", Usage: "[on|off|toggle|status]", Short: "backlight off after 30 s of no typing", Run: func(cc *command.Ctx) error {
				return tri(cc, "backlight timeout (off after 30 s)", nil, optional(c.api.KeyboardTimeout), c.api.SetKeyboardTimeout)
			}},
			{Name: "operating", Usage: "[on|off|status]", Short: "colour follows the power mode", Run: func(cc *command.Ctx) error {
				return tri(cc, "operating-mode colour follows the power mode", []string{"on", "off", "status"}, optional(c.api.OperatingFollow), c.api.SetOperatingFollow)
			}},
			{Name: "profile", Usage: "list|load|save|delete [NAME] [SPEC ...]", Short: "saved lighting profiles; save: fill COLOR | gradient C1 C2 | rainbow", Run: c.keyboardProfile},
		},
	}
}

func (c *CLI) keyboardStatus(cc *command.Ctx) error {
	if err := cc.Want(0, 0); err != nil {
		return err
	}
	st, err := c.api.KeyboardStatus()
	if err != nil {
		return err
	}
	cc.Emit(st, "")
	return nil
}

func (c *CLI) keyboardEffect(cc *command.Ctx) error {
	if err := cc.Want(1, 1, "name"); err != nil {
		return err
	}
	err := c.api.SetKeyboardEffect(api.EffectSpec{Name: cc.Arg(0), Color: flagString(cc, "color"), Speed: flagInt(cc, "speed"), Brightness: flagOptInt(cc, "brightness"), Direction: flagInt(cc, "direction")})
	if err != nil {
		return err
	}
	cc.Emit("sent", "")
	return nil
}

func (c *CLI) keyboardFX(cc *command.Ctx) error {
	if err := cc.Want(1, 1, "mode"); err != nil {
		return err
	}
	mode, err := cc.Choice(0, "mode", "", append([]string{"off"}, perkeyfx.ModeNames()...)...)
	if err != nil {
		return err
	}
	keys := flagString(cc, "keys")
	if err := c.api.SetKeyboardFX(api.KeyboardFXSpec{Mode: mode, Speed: flagInt(cc, "speed"), Keys: keys}); err != nil {
		return err
	}
	text := "per-key animation: " + mode
	if mode != "off" {
		text += fmt.Sprintf(" on %s (needs the key service: systemctl --user status truesense-keys)", orElse(keys, "all keys"))
	}
	cc.Emit(mode, text)
	return nil
}

func (c *CLI) keyboardDesign(cc *command.Ctx) error {
	if err := cc.Want(1, 2, "action", "name"); err != nil {
		return err
	}
	action, err := cc.Choice(0, "action", "", "list", "apply", "save")
	if err != nil {
		return err
	}
	switch action {
	case "list":
		ds := c.api.Designs()
		names := make([]string, 0, len(ds))
		for n := range ds {
			names = append(names, n)
		}
		sort.Strings(names)
		lines := make([]string, 0, len(names))
		for _, n := range names {
			lines = append(lines, fmt.Sprintf("  %-12s %-9s %s", n, ds[n].Source, ds[n].Info))
		}
		cc.Emit(ds, join(lines))
	case "apply":
		if err := c.api.ApplyDesign(cc.Arg(1), flagOptInt(cc, "brightness")); err != nil {
			return err
		}
		cc.Emit(fmt.Sprintf("design %s applied", cc.Arg(1)), "")
	default:
		path, err := c.api.SaveDesign(cc.Arg(1))
		if err != nil {
			return err
		}
		cc.Emit("saved to "+path, "")
	}
	return nil
}

func (c *CLI) keyboardBrightness(cc *command.Ctx) error {
	if err := cc.Want(0, 1, "level"); err != nil {
		return err
	}
	if cc.Arg(0) == "" {
		b, err := c.api.KeyboardBrightness()
		if err != nil {
			return err
		}
		cc.Emit(b, fmt.Sprintf("keyboard brightness: %d / %d", b, keyboard.MaxBright))
		return nil
	}
	level, err := intArg("level", cc.Arg(0))
	if err != nil {
		return err
	}
	if err := c.api.SetKeyboardBrightness(level); err != nil {
		return err
	}
	cc.Emit(level, fmt.Sprintf("keyboard brightness: %d / %d", level, keyboard.MaxBright))
	return nil
}

func (c *CLI) keyboardProfile(cc *command.Ctx) error {
	if err := cc.Want(1, len(cc.Args)+1, "verb2"); err != nil {
		return err
	}
	action, err := cc.Choice(0, "verb2", "", "list", "load", "save", "delete")
	if err != nil {
		return err
	}
	r, err := c.api.LightingProfile(action, cc.Arg(1), cc.Args[min(2, len(cc.Args)):])
	if err != nil {
		return err
	}
	if action == "list" {
		cc.Emit(r.Names, orElse(join(r.Names), "(no lighting profiles)"))
		return nil
	}
	cc.Emit(r.Message, "")
	return nil
}
