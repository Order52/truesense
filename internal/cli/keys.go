package cli

import (
	"flag"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"strings"
	"syscall"

	"github.com/Order52/truesense/internal/api"
	"github.com/Order52/truesense/internal/cli/command"
	"github.com/Order52/truesense/internal/keybinds"
)

func (c *CLI) keys() *command.Cmd {
	gesture := func(cc *command.Ctx, i int) (string, error) {
		return cc.Choice(i, "gesture", "", keybinds.Gestures()...)
	}
	return &command.Cmd{
		Name: "keys", Short: "PREDATOR key and Fn+Esc: what each gesture does",
		Run: c.keysShow,
		Sub: []*command.Cmd{
			{Name: "show", Run: c.keysShow},
			{Name: "actions", Run: func(cc *command.Ctx) error {
				if err := cc.Want(0, 0); err != nil {
					return err
				}
				cc.Println(c.api.KeyActions())
				return nil
			}},
			{Name: "run", Short: "run the key service in the foreground (what the systemd user service does)", Run: c.keysRun},
			{Name: "set", Usage: "GESTURE ACTION", Run: func(cc *command.Ctx) error {
				if err := cc.Want(2, 2, "gesture", "action"); err != nil {
					return err
				}
				g, err := gesture(cc, 0)
				if err != nil {
					return err
				}
				if err := c.api.SetKeyAction(g, cc.Arg(1)); err != nil {
					return err
				}
				cc.Emit(fmt.Sprintf("%s -> %s   (the key service reloads by itself; save it as a profile: truesense keys profile save NAME)", g, cc.Arg(1)), "")
				return nil
			}},
			{Name: "reset", Usage: "[GESTURE]", Run: func(cc *command.Ctx) error {
				if err := cc.Want(0, 1, "gesture"); err != nil {
					return err
				}
				g, err := gesture(cc, 0)
				if err != nil {
					return err
				}
				done, err := c.api.ResetKeys(g)
				if err != nil {
					return err
				}
				cc.Emit("reset: "+strings.Join(done, ", "), "")
				return nil
			}},
			{Name: "test", Usage: "ACTION", Short: "run an action now, as the key would", Run: func(cc *command.Ctx) error {
				if err := cc.Want(1, 1, "action"); err != nil {
					return err
				}
				msg, err := c.api.TestAction(cc.Context, cc.Arg(0))
				if err != nil {
					return err
				}
				cc.Emit(orElse(msg, "ok"), "")
				return nil
			}},
			{Name: "profile", Usage: "[list|current|show|use|copy|save|delete] [NAME] [NEW]", Run: c.keysProfile,
				Short: "key binding profiles: list | current | show NAME | use NAME | copy NAME [NEW] | save NAME [--from BASE] [--tap A --double A ...] | delete NAME (built-in profiles are read-only: copy to edit)",
				Flags: func(fs *flag.FlagSet) {
					fs.String("from", "", "start from this profile")
					for _, g := range []string{"tap", "double", "triple", "long", "fn-esc"} {
						fs.String(g, "", "action for this gesture")
					}
					declOptInt(fs, "long-ms", "")
					declOptInt(fs, "multi-ms", "")
				}},
			{Name: "timing", Run: c.keysTiming, Flags: func(fs *flag.FlagSet) {
				declOptInt(fs, "long-ms", "")
				declOptInt(fs, "multi-ms", "")
			}},
		},
	}
}

func (c *CLI) keysShow(cc *command.Ctx) error {
	if err := cc.Want(0, 0); err != nil {
		return err
	}
	k, err := c.api.KeyBindings()
	if err != nil {
		return err
	}
	lines := make([]string, 0, 5)
	for _, g := range keybinds.Gestures() {
		lines = append(lines, fmt.Sprintf("%-8s %s", g, k.Action(g)))
	}
	cc.Emit(k, join(lines)+fmt.Sprintf("\n(long press after %d ms; taps within %d ms count together)", k.LongMS, k.MultiMS))
	return nil
}

// keysRun is the key service, started by systemd's user manager: a signal ends it cleanly.
func (c *CLI) keysRun(cc *command.Ctx) error {
	if err := cc.Want(0, 0); err != nil {
		return err
	}
	ctx, stop := signal.NotifyContext(cc.Context, syscall.SIGTERM, os.Interrupt)
	defer stop()
	return c.api.RunKeyService(ctx, slog.New(slog.NewTextHandler(cc.Err, nil)))
}

func (c *CLI) keysTiming(cc *command.Ctx) error {
	if err := cc.Want(0, 0); err != nil {
		return err
	}
	var long, multi int
	if v := flagOptInt(cc, "long-ms"); v != nil {
		long = *v
	}
	if v := flagOptInt(cc, "multi-ms"); v != nil {
		multi = *v
	}
	if long == 0 && multi == 0 {
		t, err := c.api.KeyTiming()
		if err != nil {
			return err
		}
		cc.Emit(t, "")
		return nil
	}
	if _, err := c.api.SetKeyTiming(long, multi); err != nil {
		return err
	}
	var parts []string
	if long != 0 {
		parts = append(parts, fmt.Sprintf("'long_ms': %d", long))
	}
	if multi != 0 {
		parts = append(parts, fmt.Sprintf("'multi_ms': %d", multi))
	}
	cc.Emit("timing: {"+strings.Join(parts, ", ")+"}", "")
	return nil
}

func (c *CLI) keyProfileText(b keybinds.Bindings) string {
	lines := make([]string, 0, 6)
	for _, g := range keybinds.Gestures() {
		lines = append(lines, fmt.Sprintf("  %-11s %s", keybinds.Label(g), b.Action(g)))
	}
	return join(lines) + fmt.Sprintf("\n  long press after %d ms; taps within %d ms count together", b.LongMS, b.MultiMS)
}

func (c *CLI) keysProfile(cc *command.Ctx) error {
	if err := cc.Want(0, 3, "action", "name", "new"); err != nil {
		return err
	}
	action, err := cc.Choice(0, "action", "current", "list", "current", "show", "use", "copy", "save", "delete")
	if err != nil {
		return err
	}
	name := cc.Arg(1)
	switch action {
	case "list":
		l, err := c.api.KeyProfiles()
		if err != nil {
			return err
		}
		cc.Emit(l.Names, listLines(l))
	case "current":
		cur, err := c.api.KeyProfileInUse()
		if err != nil {
			return err
		}
		cc.Emit(cur, fmt.Sprintf("key profile in use: %s\n%s", cur.Profile, c.keyProfileText(cur.Bindings)))
	case "show":
		n := orElse(name, "default")
		p, err := c.api.KeyProfile(n)
		if err != nil {
			return err
		}
		cc.Emit(p, n+"\n"+c.keyProfileText(p.Bindings))
	case "use":
		if err := c.api.UseKeyProfile(name); err != nil {
			return err
		}
		cc.Emit(name, fmt.Sprintf("key profile in use: %s   (the key service reloads by itself)", name))
	case "copy":
		n, err := c.api.CopyKeyProfile(name, cc.Arg(2))
		if err != nil {
			return err
		}
		cc.Emit(n, fmt.Sprintf("copied '%s' to '%s' (edit: truesense keys profile save %s --tap power:turbo)", name, n, n))
	case "save":
		edit := api.KeyProfileEdit{From: flagString(cc, "from"), Actions: map[string]string{}, LongMS: flagOptInt(cc, "long-ms"), MultiMS: flagOptInt(cc, "multi-ms")}
		for _, g := range keybinds.Gestures() {
			if v := flagString(cc, strings.ReplaceAll(g, "_", "-")); v != "" {
				edit.Actions[g] = v
			}
		}
		p, err := c.api.SaveKeyProfile(name, edit)
		if err != nil {
			return err
		}
		cc.Emit(p, fmt.Sprintf("saved key profile '%s' (use it: truesense keys profile use %s)", name, name))
	case "delete":
		if err := c.api.DeleteKeyProfile(name); err != nil {
			return err
		}
		cc.Emit(name, fmt.Sprintf("deleted key profile '%s'", name))
	}
	return nil
}
