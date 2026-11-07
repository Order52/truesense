// Package app is the entry point behind cmd/truesense: it picks what to run from the program
// name (truesense, truesense-fast) and builds the real dependencies. It is the only package that
// knows the terminal, the signals and the process exit code.
package app

import (
	"context"
	"fmt"
	"io"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"

	"github.com/Order52/truesense/internal/adapt"
	"github.com/Order52/truesense/internal/api"
	"github.com/Order52/truesense/internal/assembly"
	"github.com/Order52/truesense/internal/buildinfo"
	"github.com/Order52/truesense/internal/cli"
	"github.com/Order52/truesense/internal/cli/command"
	"github.com/Order52/truesense/internal/client"
	"github.com/Order52/truesense/internal/fast"
	"github.com/Order52/truesense/internal/firmware"
	"github.com/Order52/truesense/internal/hw"
	"github.com/Order52/truesense/internal/tui"
)

// exitInterrupted is the conventional exit code of a program ended by Ctrl-C.
const exitInterrupted = 130

// Run executes the program with the given arguments (args[0] is the program name) and
// returns the process exit code.
func Run(args []string, stdout, stderr io.Writer) int {
	if len(args) == 0 {
		return command.ExitUsage
	}
	if filepath.Base(args[0]) == "truesense-fast" {
		return runFast(args[1:], stdout, stderr)
	}
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGTERM, os.Interrupt)
	defer stop()
	return runCLI(ctx, args[1:], os.Stdin, stdout, stderr)
}

// runFast is the status-bar client: it needs the hardware and the daemon socket and nothing
// else, so it builds nothing more (it starts in a few milliseconds).
func runFast(args []string, stdout, stderr io.Writer) int {
	h := hw.New(firmware.FromEnv(), nil)
	return fast.Run(args, client.New(h), stdout, stderr)
}

// runCLI runs the command line. A signal (Ctrl-C, SIGTERM) cancels ctx, which every command
// gets, so work that must undo itself (the self-test) can; a command that then fails exits 130.
func runCLI(ctx context.Context, args []string, stdin io.Reader, stdout, stderr io.Writer) int {
	if len(args) > 0 && (args[0] == "-V" || args[0] == "--version") { // no need to build the program for that
		_, _ = fmt.Fprintf(stdout, "truesense %s\n", buildinfo.Version)
		return command.ExitOK
	}
	services := assembly.New()
	a := api.New(services)
	hooks := cli.Hooks{
		TUI: func(ctx context.Context, page string) error { return runTUI(ctx, a, services, page, stderr) },
		GUI: func(ctx context.Context, page string) error {
			return realWindow(stderr).showWindow(ctx, a, services, page)
		},
	}
	code := cli.New(a, hooks, stdin).Execute(ctx, args, stdout, stderr)
	if code != command.ExitOK && ctx.Err() != nil {
		return exitInterrupted
	}
	return code
}

// runTUI shows the terminal UI. It needs a terminal on both input and output.
func runTUI(ctx context.Context, a *api.API, s *assembly.Services, page string, stderr io.Writer) error {
	if !cli.IsTerminal(os.Stdin) || !cli.IsTerminal(os.Stdout) {
		_, _ = fmt.Fprintln(stderr, "truesense tui needs a terminal")
		return command.ExitCode(1)
	}
	return tui.Run(ctx, adapt.TUI(a, s), tui.Options{Page: page, Version: buildinfo.Version})
}
