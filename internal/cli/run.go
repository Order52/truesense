package cli

import (
	"flag"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"strings"
	"syscall"

	"github.com/Order52/truesense/internal/assembly"
	"github.com/Order52/truesense/internal/cli/command"
	"github.com/Order52/truesense/internal/tui"
)

// guiPages and tuiPages are the pages `gui --page` and `tui --page` open on.
var (
	guiPages = []string{"home", "lighting", "curves", "smart", "predator", "undervolt", "battery", "audio", "profiles", "health", "settings", "keys"}
	tuiPages = []string{"dashboard", "fans", "power", "lighting", "audio", "profiles", "system"}
)

func pageFlag(pages []string) func(fs *flag.FlagSet) {
	return func(fs *flag.FlagSet) {
		fs.String("page", "", "open on this page ("+strings.Join(pages, ", ")+")")
	}
}

// pageRun checks --page and hands over to a hook.
func (c *CLI) pageRun(pages []string, hook func(cc *command.Ctx, page string) error) func(*command.Ctx) error {
	return func(cc *command.Ctx) error {
		if err := cc.Want(0, 0); err != nil {
			return err
		}
		if err := oneOfFlag(cc, "page", pages...); err != nil {
			return err
		}
		return hook(cc, flagString(cc, "page"))
	}
}

func (c *CLI) gui() *command.Cmd {
	return &command.Cmd{Name: "gui", Short: "the small optional window", Flags: pageFlag(guiPages), Run: c.pageRun(guiPages, func(cc *command.Ctx, page string) error {
		if c.hooks.GUI == nil {
			return fmt.Errorf("the window is not built into this binary")
		}
		if page == "keys" { // the name the Qt window used for the PREDATOR key page
			page = "predator"
		}
		return c.hooks.GUI(cc.Context, page)
	})}
}

func (c *CLI) tui() *command.Cmd {
	return &command.Cmd{Name: "tui", Short: "the full control surface in the terminal (keyboard and mouse)", Flags: pageFlag(tuiPages), Run: c.pageRun(tuiPages, func(cc *command.Ctx, page string) error {
		if c.hooks.TUI == nil {
			return fmt.Errorf("the terminal UI is not built into this binary")
		}
		if _, ok := tui.PageIndex(page); page != "" && !ok {
			return command.Usagef("unknown page %q", page)
		}
		return c.hooks.TUI(cc.Context, page)
	})}
}

// daemon is `truesense daemon`, started by systemd. SIGTERM and SIGINT end it cleanly (the fans
// and the clocks are handed back on every way out); its log goes to the journal through stderr.
func (c *CLI) daemon() *command.Cmd {
	return &command.Cmd{Name: "daemon", Hidden: true, Short: "run the daemon (systemd does this)", Run: func(cc *command.Ctx) error {
		if err := cc.Want(0, 0); err != nil {
			return err
		}
		ctx, stop := signal.NotifyContext(cc.Context, syscall.SIGTERM, os.Interrupt)
		defer stop()
		log := slog.New(slog.NewTextHandler(cc.Err, &slog.HandlerOptions{Level: slog.LevelInfo}))
		return assembly.RunDaemon(ctx, c.api.Services(), log)
	}}
}
