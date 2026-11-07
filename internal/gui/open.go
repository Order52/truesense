package gui

import (
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
	"syscall"
)

// chromiumFamily are the browsers that can show a page as an application window without
// tabs or an address bar (--app), which is what the Qt window looked like.
var chromiumFamily = []string{
	"chromium", "chromium-browser", "google-chrome-stable", "google-chrome", "brave", "brave-browser",
	"microsoft-edge-stable", "microsoft-edge", "vivaldi", "vivaldi-stable", "helium-browser",
}

// Launcher shows a URL as a window. Every external effect is a field so the choice of
// browser is testable without starting one.
type Launcher struct {
	// LookPath finds a program on PATH (exec.LookPath).
	LookPath func(name string) (string, error)
	// Start launches a program detached from this one and returns without waiting.
	Start func(path string, args ...string) error
	// Getenv reads the environment (a desktop is there when DISPLAY or WAYLAND_DISPLAY is set).
	Getenv func(key string) string
	// Out is where the URL is printed when no browser can be started.
	Out io.Writer
}

// DefaultLauncher uses the real PATH, process table and terminal.
func DefaultLauncher() Launcher {
	return Launcher{LookPath: exec.LookPath, Start: startDetached, Getenv: os.Getenv, Out: os.Stdout}
}

// Open shows url as an app window of a Chromium-family browser, else in Firefox, else
// through xdg-open, else it only prints the URL for the user to open (the URL carries the
// access token, so it is not printed when a browser was started).
func Open(url string) error { return DefaultLauncher().Open(url) }

// Open implements the preference order described at the package-level Open.
func (l Launcher) Open(url string) error {
	if l.Getenv("DISPLAY") == "" && l.Getenv("WAYLAND_DISPLAY") == "" {
		return l.print(url, "no desktop session found")
	}
	for _, name := range chromiumFamily {
		if path, err := l.LookPath(name); err == nil {
			return l.started(url, l.Start(path, "--app="+url, "--window-size=1180,820", "--no-first-run"))
		}
	}
	if path, err := l.LookPath("firefox"); err == nil {
		return l.started(url, l.Start(path, "--new-window", url))
	}
	if path, err := l.LookPath("xdg-open"); err == nil {
		return l.started(url, l.Start(path, url))
	}
	return l.print(url, "no browser found")
}

func (l Launcher) started(url string, err error) error {
	if err != nil {
		return l.print(url, fmt.Sprintf("could not start the browser: %v", err))
	}
	return nil
}

func (l Launcher) print(url, why string) error {
	_, _ = fmt.Fprintf(l.Out, "%s: open this address in a browser:\n%s\n", why, url)
	return nil
}

func startDetached(path string, args ...string) error {
	cmd := exec.CommandContext(context.Background(), path, args...) //nolint:gosec // path comes from LookPath, args are fixed flags and our own URL
	cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
	if err := cmd.Start(); err != nil {
		return err
	}
	go func() { _ = cmd.Wait() }() // reap it; the browser outlives this call
	return nil
}
