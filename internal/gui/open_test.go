package gui

import (
	"bytes"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

type launch struct {
	path string
	args []string
}

func launcher(installed []string, env map[string]string, startErr error) (Launcher, *launch, *bytes.Buffer) {
	return launcherIn("", installed, env, startErr, false)
}

// launcherIn is launcher with the data directory (where Firefox's application profile goes) and
// whether a Firefox is already running on that profile.
func launcherIn(data string, installed []string, env map[string]string, startErr error, running bool) (Launcher, *launch, *bytes.Buffer) {
	var got launch
	var out bytes.Buffer
	have := map[string]bool{}
	for _, n := range installed {
		have[n] = true
	}
	return Launcher{
		LookPath: func(name string) (string, error) {
			if have[name] {
				return "/usr/bin/" + name, nil
			}
			return "", exec.ErrNotFound
		},
		Start:          func(path string, args ...string) error { got = launch{path, args}; return startErr },
		Getenv:         func(k string) string { return env[k] },
		Out:            &out,
		DataDir:        func() string { return data },
		FirefoxRunning: func(string) bool { return running },
	}, &got, &out
}

func TestOpenPrefersAChromiumAppWindow(t *testing.T) {
	const u = "http://127.0.0.1:1234/#token=abc"
	tests := []struct {
		name      string
		installed []string
		env       map[string]string
		wantPath  string
		wantArg   string
		printed   bool
	}{
		{"chromium first", []string{"firefox", "xdg-open", "chromium"}, map[string]string{"WAYLAND_DISPLAY": "wayland-1"}, "/usr/bin/chromium", "--app=" + u, false},
		{"another chromium-family browser", []string{"firefox", "brave"}, map[string]string{"DISPLAY": ":0"}, "/usr/bin/brave", "--app=" + u, false},
		{"firefox when there is no chromium", []string{"firefox", "xdg-open"}, map[string]string{"DISPLAY": ":0"}, "/usr/bin/firefox", u, false},
		{"xdg-open when there is no browser of the families", []string{"xdg-open"}, map[string]string{"DISPLAY": ":0"}, "/usr/bin/xdg-open", u, false},
		{"just print the URL when nothing can open it", nil, map[string]string{"DISPLAY": ":0"}, "", "", true},
		{"just print the URL without a desktop session", []string{"chromium"}, nil, "", "", true},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			l, got, out := launcherIn(t.TempDir(), tc.installed, tc.env, nil, false)
			if err := l.Open(u); err != nil {
				t.Fatal(err)
			}
			if got.path != tc.wantPath {
				t.Fatalf("started %q, want %q", got.path, tc.wantPath)
			}
			if tc.wantArg != "" && !containsArg(got.args, tc.wantArg) {
				t.Fatalf("args %v lack %q", got.args, tc.wantArg)
			}
			if printed := strings.Contains(out.String(), u); printed != tc.printed {
				t.Fatalf("URL printed = %v (%q), want %v", printed, out.String(), tc.printed)
			}
		})
	}
}

func TestOpenFallsBackToPrintingWhenTheBrowserWillNotStart(t *testing.T) {
	l, _, out := launcher([]string{"chromium"}, map[string]string{"DISPLAY": ":0"}, errors.New("exec format error"))
	if err := l.Open("http://127.0.0.1:1/#token=x"); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.String(), "http://127.0.0.1:1/#token=x") || !strings.Contains(out.String(), "could not start the browser") {
		t.Fatalf("output %q", out.String())
	}
}

func containsArg(args []string, want string) bool {
	for _, a := range args {
		if a == want {
			return true
		}
	}
	return false
}

func TestFirefoxOpensAsAnApplicationWindowInItsOwnProfile(t *testing.T) {
	const u = "http://127.0.0.1:1234/#code=abc"
	data := t.TempDir()
	l, got, _ := launcherIn(data, []string{"firefox"}, map[string]string{"WAYLAND_DISPLAY": "wayland-1"}, nil, false)
	if err := l.Open(u); err != nil {
		t.Fatal(err)
	}
	profile := filepath.Join(data, "truesense", "firefox-app")
	for _, want := range []string{"--no-remote", "--profile", profile, "--name", "truesense", "--new-window", u} {
		if !containsArg(got.args, want) {
			t.Errorf("first launch args %v lack %q", got.args, want)
		}
	}
	css, err := os.ReadFile(filepath.Join(profile, "chrome", "userChrome.css"))
	if err != nil || !strings.Contains(string(css), "#nav-bar") || !strings.Contains(string(css), "#tabbrowser-tabs") {
		t.Fatalf("the application profile must hide the address bar and the tabs: %v %q", err, css)
	}
	js, err := os.ReadFile(filepath.Join(profile, "user.js"))
	if err != nil || !strings.Contains(string(js), "legacyUserProfileCustomizations") {
		t.Fatalf("user.js must enable the stylesheet: %v", err)
	}
}

func TestASecondLaunchJoinsTheRunningApplicationNotTheUsersBrowser(t *testing.T) {
	const u = "http://127.0.0.1:1234/#code=abc"
	l, got, _ := launcherIn(t.TempDir(), []string{"firefox"}, map[string]string{"DISPLAY": ":0"}, nil, true)
	if err := l.Open(u); err != nil {
		t.Fatal(err)
	}
	if containsArg(got.args, "--no-remote") {
		t.Fatalf("while the application's own Firefox runs, a launch must reach it (no --no-remote): %v", got.args)
	}
	if !containsArg(got.args, "--profile") || !containsArg(got.args, "--new-window") {
		t.Fatalf("args %v", got.args)
	}
	// the very first launch must NOT reach an already running browser of the user's
	l2, got2, _ := launcherIn(t.TempDir(), []string{"firefox"}, map[string]string{"DISPLAY": ":0"}, nil, false)
	if err := l2.Open(u); err != nil || !containsArg(got2.args, "--no-remote") {
		t.Fatalf("a first launch must be its own process: %v %v", err, got2.args)
	}
}

func TestIfTheProfileCannotBeWrittenFirefoxStillOpensTheWindow(t *testing.T) {
	const u = "http://127.0.0.1:1234/#code=abc"
	blocked := filepath.Join(t.TempDir(), "file")
	if err := os.WriteFile(blocked, nil, 0o644); err != nil {
		t.Fatal(err)
	}
	l, got, _ := launcherIn(blocked, []string{"firefox"}, map[string]string{"DISPLAY": ":0"}, nil, false) // a file where the directory should be
	if err := l.Open(u); err != nil {
		t.Fatal(err)
	}
	if got.path != "/usr/bin/firefox" || !containsArg(got.args, u) {
		t.Fatalf("the page must still open: %+v", got)
	}
}

func TestProfileFilesAreRewrittenOnlyWhenTheyDiffer(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "p")
	if err := writeFirefoxProfile(dir); err != nil {
		t.Fatal(err)
	}
	css := filepath.Join(dir, "chrome", "userChrome.css")
	before, _ := os.Stat(css)
	if err := writeFirefoxProfile(dir); err != nil {
		t.Fatal(err)
	}
	after, _ := os.Stat(css)
	if !after.ModTime().Equal(before.ModTime()) {
		t.Fatal("an up-to-date profile must not be rewritten")
	}
	if err := os.WriteFile(css, []byte("old"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := writeFirefoxProfile(dir); err != nil {
		t.Fatal(err)
	}
	if b, _ := os.ReadFile(css); string(b) != firefoxUserChrome {
		t.Fatal("a changed file must be restored")
	}
}

func TestFirefoxProfileRunningReadsTheLockSymlink(t *testing.T) {
	dir := t.TempDir()
	if firefoxProfileRunning(dir) {
		t.Fatal("no lock: not running")
	}
	if err := os.Symlink("127.0.0.1:+"+strconv.Itoa(os.Getpid()), filepath.Join(dir, "lock")); err != nil {
		t.Fatal(err)
	}
	if !firefoxProfileRunning(dir) {
		t.Fatal("a lock naming a live process means running")
	}
	_ = os.Remove(filepath.Join(dir, "lock"))
	if err := os.Symlink("127.0.0.1:+999999999", filepath.Join(dir, "lock")); err != nil {
		t.Fatal(err)
	}
	if firefoxProfileRunning(dir) {
		t.Fatal("a stale lock (no such process) is not running")
	}
}
