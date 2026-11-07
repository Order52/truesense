package cli

import (
	"flag"
	"fmt"
	"strings"

	"github.com/Order52/truesense/internal/cli/command"
)

// The verbs below exist for the window (internal/gui/COMMANDS.md): the same use cases, also
// reachable from a terminal and from scripts with --json.

// addSub adds subcommands to the area with that name.
func addSub(cmds []*command.Cmd, area string, subs ...*command.Cmd) {
	for _, c := range cmds {
		if c.Name == area {
			c.Sub = append(c.Sub, subs...)
			return
		}
	}
	panic("cli: no area " + area) // a programming error, caught by every test of the tree
}

func (c *CLI) addWindowVerbs(list []*command.Cmd) {
	addSub(list, "status", &command.Cmd{Name: "get", Short: "everything at a glance, plus the charger, battery level and keyboard look the window shows", Run: func(cc *command.Ctx) error {
		if err := cc.Want(0, 0); err != nil {
			return err
		}
		cc.Emit(c.api.StatusFull(cc.Context), "")
		return nil
	}})
	addSub(list, "version", &command.Cmd{Name: "get", Run: func(cc *command.Ctx) error { return c.versionRun(cc) }})
	c.touchpadVerbs(list)
	addSub(list, "doctor", &command.Cmd{Name: "run", Short: "check the whole stack (same as `doctor`)", Run: c.doctorRun})
	addSub(list, "selftest", &command.Cmd{Name: "run", Short: "set -> read back -> restore every firmware feature (same as `selftest`)", Run: c.selftestRun,
		Flags: func(fs *flag.FlagSet) { fs.Bool("skip-fans", false, "leave the fans out") }})
	addSub(list, "config", &command.Cmd{Name: "reset-all", Short: "put the fan curve, Smart auto profile, key bindings, power limits, undervolts and lighting back to their defaults (saved profiles are kept)", Run: func(cc *command.Ctx) error {
		if err := cc.Want(0, 0); err != nil {
			return err
		}
		steps := c.api.ResetAll(cc.Context)
		lines := make([]string, len(steps))
		failed := false
		for i, s := range steps {
			lines[i] = fmt.Sprintf("  [%s] %s", pick(s.OK, "ok ", "!! "), s.Step)
			if !s.OK {
				lines[i] += ": " + s.Error
				failed = true
			}
		}
		cc.Emit(map[string]any{"steps": steps}, join(lines))
		if failed {
			return command.ExitCode(1)
		}
		return nil
	}})
	addSub(list, "profile", &command.Cmd{Name: "current", Short: "the profile applied last", Run: func(cc *command.Ctx) error {
		if err := cc.Want(0, 0); err != nil {
			return err
		}
		var name any
		text := "no profile applied yet"
		if n := c.api.CurrentProfile(); n != "" {
			name, text = n, n
		}
		cc.Emit(map[string]any{"name": name}, text)
		return nil
	}})
	addSub(list, "audio", &command.Cmd{Name: "sound", Short: "is there a sound output?", Run: func(cc *command.Ctx) error {
		if err := cc.Want(0, 0); err != nil {
			return err
		}
		s, err := c.api.SoundOutput(cc.Context)
		if err != nil {
			return err
		}
		text := "no sound card found in PipeWire: " + s.Problem
		switch {
		case s.OK != nil && *s.OK:
			text = "the sound output is there (" + strings.Join(s.Sinks, ", ") + ")"
		case s.OK != nil:
			text = "NO OUTPUT: " + s.Problem
		}
		cc.Emit(s, text)
		return nil
	}})
	addSub(list, "look", &command.Cmd{Name: "copy", Usage: "NAME [NEW]", Short: "save a copy of a look", Run: func(cc *command.Ctx) error {
		if err := cc.Want(1, 2, "name", "new"); err != nil {
			return err
		}
		n, err := c.api.CopyLook(cc.Arg(0), cc.Arg(1))
		if err != nil {
			return err
		}
		cc.Emit(n, fmt.Sprintf("copied look '%s' to '%s'", cc.Arg(0), n))
		return nil
	}})
	addSub(list, "keyboard",
		&command.Cmd{Name: "cells", Short: "the colours of the per-key picture: 128 values (the last two are not keys)", Run: func(cc *command.Ctx) error {
			if err := cc.Want(0, 0); err != nil {
				return err
			}
			cells := c.api.KeyboardCells()
			cc.Emit(map[string][]string{"cells": cells}, strings.Join(cells, ","))
			return nil
		}},
		&command.Cmd{Name: "set-cells", Usage: "RRGGBB,RRGGBB,... (up to 128)", Short: "upload a per-key picture given as a comma list of colours", Flags: func(fs *flag.FlagSet) { declOptInt(fs, "brightness", "") },
			Run: func(cc *command.Ctx) error {
				if err := cc.Want(1, 1, "cells"); err != nil {
					return err
				}
				if err := c.api.SetKeyboardCells(strings.Split(cc.Arg(0), ","), flagOptInt(cc, "brightness")); err != nil {
					return err
				}
				cc.Emit("sent", "")
				return nil
			}})
	addSub(list, "gui", &command.Cmd{Name: "autostart", Usage: "[on|off|status]", Short: "open the window at login", Run: func(cc *command.Ctx) error {
		return tri(cc, "window opens at login", []string{"on", "off", "status"}, optional(c.api.Autostart), c.api.SetAutostart)
	}})
}

// touchpadVerbs makes `touchpad` a command with verbs (on, off, toggle, status, get, set): the
// 0.3.x command took the word as its argument, and still does through these verbs.
func (c *CLI) touchpadVerbs(list []*command.Cmd) {
	run := func(action string) func(*command.Ctx) error {
		return func(cc *command.Ctx) error {
			if err := cc.Want(0, 0); err != nil {
				return err
			}
			cc.Args = []string{action}
			return tri(cc, "touchpad", nil, c.api.Touchpad, c.api.SetTouchpad)
		}
	}
	subs := []*command.Cmd{{Name: "on", Run: run("on")}, {Name: "off", Run: run("off")}, {Name: "toggle", Run: run("toggle")}, {Name: "status", Run: run("status")},
		{Name: "get", Short: "whether the touchpad is on (true, false or null)", Run: func(cc *command.Ctx) error {
			if err := cc.Want(0, 0); err != nil {
				return err
			}
			v, err := c.api.Touchpad()
			if err != nil {
				return err
			}
			cc.Emit(v, "touchpad: "+onoff(v))
			return nil
		}},
		{Name: "set", Usage: "on|off|toggle", Run: func(cc *command.Ctx) error {
			if err := cc.Want(1, 1, "action"); err != nil {
				return err
			}
			if _, err := cc.Choice(0, "action", "", "on", "off", "toggle"); err != nil {
				return err
			}
			return tri(cc, "touchpad", nil, c.api.Touchpad, c.api.SetTouchpad)
		}}}
	addSub(list, "touchpad", subs...)
}
