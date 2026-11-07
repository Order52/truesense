package gui

import (
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
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
	// DataDir is the user's data directory (XDG_DATA_HOME); Firefox's application profile lives under it.
	DataDir func() string
	// FirefoxRunning reports whether a Firefox is already running on the profile directory.
	FirefoxRunning func(dir string) bool
}

// DefaultLauncher uses the real PATH, process table and terminal.
func DefaultLauncher() Launcher {
	return Launcher{LookPath: exec.LookPath, Start: startDetached, Getenv: os.Getenv, Out: os.Stdout, DataDir: defaultDataDir, FirefoxRunning: firefoxProfileRunning}
}

// Open shows url as an app window of a Chromium-family browser, else as an application-style
// Firefox window (its own profile without tabs or address bar), else
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
			return l.started(url, l.Start(path, "--app="+url, "--class="+WindowClass, "--window-size=1180,820", "--no-first-run"))
		}
	}
	if path, err := l.LookPath("firefox"); err == nil {
		return l.openFirefox(path, url)
	}
	if path, err := l.LookPath("xdg-open"); err == nil {
		return l.started(url, l.Start(path, url))
	}
	return l.print(url, "no browser found")
}

// openFirefox shows url in Firefox as an application window: a profile of its own with the tabs
// and the address bar hidden (see firefoxapp.go). If that profile cannot be prepared the page still
// opens, in an ordinary Firefox window.
func (l Launcher) openFirefox(path, url string) error {
	dir := filepath.Join(l.DataDir(), "truesense", firefoxProfileName)
	if err := writeFirefoxProfile(dir); err != nil {
		return l.started(url, l.Start(path, "--new-window", url))
	}
	return l.started(url, l.Start(path, firefoxArgs(dir, url, l.FirefoxRunning(dir))...))
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
