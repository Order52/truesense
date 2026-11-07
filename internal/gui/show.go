package gui

import (
	"context"
	"encoding/json"
	"io"
	"net"
	"net/http"
	"os"
	"os/exec"
	"regexp"
	"strings"
	"time"
)

// WindowClass is the window class (Wayland app id) of the TrueSense window: Firefox is started
// with it by name, Chromium-family browsers with --class. A desktop can group the window under the
// TrueSense icon and apply window rules to it, and Raise finds it by it.
const WindowClass = "truesense"

// Show asks the window this user already has open to go to page, and reports whether a page
// heard it. When it did, no second window is needed: bring the existing one forward (Raise).
func Show(page string) bool {
	dir, err := DefaultRuntimeDir()
	if err != nil {
		return false
	}
	return ShowIn(dir, page)
}

// ShowIn is Show for a given runtime directory.
func ShowIn(runtimeDir, page string) bool {
	info, ok := readLive(runtimeDir)
	if !ok {
		return false
	}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	body, _ := json.Marshal(map[string]string{"page": page})
	base := "http://" + net.JoinHostPort("127.0.0.1", itoa(info.Port))
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, base+"/api/gui/show", strings.NewReader(string(body)))
	if err != nil {
		return false
	}
	req.Header.Set("Authorization", "Bearer "+info.Token)
	req.Header.Set("Content-Type", "application/json")
	res, err := (&http.Client{}).Do(req)
	if err != nil {
		return false
	}
	defer res.Body.Close() //nolint:errcheck // read-only
	var out struct {
		OK     bool `json:"ok"`
		Result struct {
			Shown bool `json:"shown"`
		} `json:"result"`
	}
	if res.StatusCode != http.StatusOK || json.NewDecoder(io.LimitReader(res.Body, 4096)).Decode(&out) != nil {
		return false
	}
	return out.OK && out.Result.Shown
}

func itoa(n int) string { b, _ := json.Marshal(n); return string(b) }

// Raiser brings the TrueSense window to the front. Desktops differ, and there is no portable way,
// so it tries what the running desktop has (Hyprland, Sway, an X11 window manager) and says
// whether one of them found the window. Every effect is a field for the tests.
type Raiser struct {
	LookPath func(name string) (string, error)
	// Run runs a program and returns its standard output.
	Run    func(ctx context.Context, name string, args ...string) (string, error)
	Getenv func(key string) string
}

// DefaultRaiser uses the real PATH and environment.
func DefaultRaiser() Raiser {
	return Raiser{LookPath: exec.LookPath, Getenv: os.Getenv, Run: func(ctx context.Context, name string, args ...string) (string, error) {
		out, err := exec.CommandContext(ctx, name, args...).Output() //nolint:gosec // fixed program names and arguments
		return string(out), err
	}}
}

// Raise brings the window forward with the default tools.
func Raise() bool { return DefaultRaiser().Raise() }

var hyprWindow = regexp.MustCompile(`"class":\s*"` + WindowClass + `"`)

// Raise reports whether a desktop tool found a TrueSense window and focused it.
func (r Raiser) Raise() bool {
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	have := func(name string) bool { _, err := r.LookPath(name); return err == nil }
	if r.Getenv("HYPRLAND_INSTANCE_SIGNATURE") != "" && have("hyprctl") {
		if out, err := r.Run(ctx, "hyprctl", "clients", "-j"); err == nil && hyprWindow.MatchString(out) {
			// Hyprland 0.5x and newer take a Lua dispatcher; older ones the classic "focuswindow class:..." form.
			if _, err := r.Run(ctx, "hyprctl", "dispatch", `hl.dsp.focus({ window = "class:^`+WindowClass+`$" })`); err == nil {
				return true
			}
			_, err := r.Run(ctx, "hyprctl", "dispatch", "focuswindow", "class:^"+WindowClass+"$")
			return err == nil
		}
		return false
	}
	if r.Getenv("SWAYSOCK") != "" && have("swaymsg") {
		out, err := r.Run(ctx, "swaymsg", `[app_id="`+WindowClass+`"] focus`)
		return err == nil && strings.Contains(out, `"success": true`)
	}
	if r.Getenv("DISPLAY") != "" && have("wmctrl") {
		_, err := r.Run(ctx, "wmctrl", "-x", "-a", WindowClass)
		return err == nil
	}
	if r.Getenv("DISPLAY") != "" && have("xdotool") {
		out, err := r.Run(ctx, "xdotool", "search", "--class", WindowClass)
		if err != nil || strings.TrimSpace(out) == "" {
			return false
		}
		id := strings.Fields(out)[0]
		_, err = r.Run(ctx, "xdotool", "windowactivate", id)
		return err == nil
	}
	return false
}
