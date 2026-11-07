package gui

import (
	"bytes"
	"errors"
	"os/exec"
	"strings"
	"testing"
)

type launch struct {
	path string
	args []string
}

func launcher(installed []string, env map[string]string, startErr error) (Launcher, *launch, *bytes.Buffer) {
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
		Start:  func(path string, args ...string) error { got = launch{path, args}; return startErr },
		Getenv: func(k string) string { return env[k] },
		Out:    &out,
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
			l, got, out := launcher(tc.installed, tc.env, nil)
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
