// Package osd shows short on-screen messages ("Audio output: Headphones") for the daemon, the
// key service and the command line. The style (config [osd] style; TRUESENSE_OSD overrides) is
// native (the compositor's own notification, then the desktop notification daemon), noctalia
// (Noctalia's OSD popup) or none. A missing helper program is never an error.
package osd

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"os/user"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
)

// Runner runs a program (optionally as another user, with a given environment) and returns
// its standard output and exit code (-1 when it could not run at all).
type Runner func(ctx context.Context, env []string, asUser string, name string, args ...string) (stdout string, code int)

// Notifier shows messages.
type Notifier struct {
	Style      string // native | noctalia | none ("" = native); TRUESENSE_OSD overrides
	User       string // whose desktop session to show on when running as root
	Run        Runner
	IsRoot     bool
	RuntimeDir string // base of the per-user runtime directories (default /run/user)
	Lookup     func(name string) (uid int, home string, err error)
}

// New returns a Notifier for the real machine.
func New(style, user string) *Notifier {
	return &Notifier{Style: style, User: user, Run: ExecRunner, IsRoot: os.Geteuid() == 0, RuntimeDir: "/run/user", Lookup: lookupUser}
}

func lookupUser(name string) (int, string, error) {
	u, err := user.Lookup(name)
	if err != nil {
		return 0, "", err
	}
	uid, _ := strconv.Atoi(u.Uid)
	return uid, u.HomeDir, nil
}

// ExecRunner is the real Runner. As root it runs the program as the user with runuser.
func ExecRunner(ctx context.Context, env []string, asUser string, name string, args ...string) (string, int) {
	ctx, cancel := context.WithTimeout(ctx, 4*time.Second)
	defer cancel()
	if asUser != "" && os.Geteuid() == 0 {
		args = append([]string{"-u", asUser, "--", name}, args...)
		name = "runuser"
	}
	cmd := exec.CommandContext(ctx, name, args...)
	if env != nil {
		cmd.Env = env
	}
	out, err := cmd.Output()
	if err != nil {
		var ee *exec.ExitError
		if errors.As(err, &ee) {
			return string(out), ee.ExitCode()
		}
		return "", -1
	}
	return string(out), 0
}

func (n *Notifier) style() string {
	if s := os.Getenv("TRUESENSE_OSD"); s != "" {
		return s
	}
	if n.Style != "" {
		return n.Style
	}
	return "native"
}

var noctaliaIcon = map[string]string{"audio": "headphones", "speaker": "device-speaker", "power": "gauge", "fan": "gauge", "mic": "microphone"}

// sessionEnv is the environment of the user's desktop session, needed when the daemon (root)
// shows a message.
func (n *Notifier) sessionEnv() []string {
	uid, home, err := n.Lookup(n.User)
	if err != nil {
		return nil
	}
	rt := filepath.Join(n.RuntimeDir, strconv.Itoa(uid))
	env := []string{"PATH=/usr/bin:/bin", "HOME=" + home, "XDG_RUNTIME_DIR=" + rt, "DBUS_SESSION_BUS_ADDRESS=unix:path=" + rt + "/bus"}
	if socks := newest(filepath.Join(rt, "wayland-*")); len(socks) > 0 {
		for i := len(socks) - 1; i >= 0; i-- {
			if !strings.HasSuffix(socks[i], ".lock") {
				env = append(env, "WAYLAND_DISPLAY="+filepath.Base(socks[i]))
				break
			}
		}
	}
	if sig := newest(filepath.Join(rt, "hypr", "*")); len(sig) > 0 {
		env = append(env, "HYPRLAND_INSTANCE_SIGNATURE="+filepath.Base(sig[len(sig)-1]))
	}
	return env
}

// newest returns the paths matching pattern, oldest first.
func newest(pattern string) []string {
	m, _ := filepath.Glob(pattern)
	sort.Slice(m, func(i, j int) bool {
		a, errA := os.Stat(m[i])
		b, errB := os.Stat(m[j])
		if errA != nil || errB != nil {
			return m[i] < m[j]
		}
		return a.ModTime().Before(b.ModTime())
	})
	return m
}

// Show displays text with an icon name ("audio", "power", "fan", "battery", "mic", ...).
// It reports whether a helper accepted it.
func (n *Notifier) Show(text, icon string) bool {
	style := n.style()
	if style == "none" {
		return false
	}
	var env []string
	asUser := ""
	if n.IsRoot && n.User != "" {
		if env = n.sessionEnv(); env == nil {
			return false
		}
		asUser = n.User
	}
	ctx := context.Background()
	if style == "noctalia" {
		ic := noctaliaIcon[icon]
		if ic == "" {
			ic = icon
		}
		_, code := n.Run(ctx, env, asUser, "noctalia", "msg", "osd-show", ic, text)
		return code == 0
	}
	if out, _ := n.Run(ctx, env, asUser, "hyprctl", "notify", "1", "1800", "rgb(00aec7)", text); strings.TrimSpace(out) == "ok" {
		return true
	}
	_, code := n.Run(ctx, env, asUser, "notify-send", "-a", "TrueSense", "-t", "1800", "TrueSense", text)
	return code == 0
}

var (
	quietMu    sync.Mutex
	quietDepth int
)

// Quiet silences Noctalia's own popups while a profile flip makes PipeWire recreate devices.
// Call the returned function to end it (Noctalia's popups come back after a short settle time,
// even when several Quiet sections overlap).
func (n *Notifier) Quiet(settle time.Duration) (done func()) {
	quietMu.Lock()
	quietDepth++
	first := quietDepth == 1
	quietMu.Unlock()
	if first {
		n.Run(context.Background(), nil, "", "noctalia", "msg", "osd-disable")
	}
	return func() {
		quietMu.Lock()
		last := quietDepth == 1
		quietMu.Unlock()
		if last {
			time.Sleep(settle)
			n.Run(context.Background(), nil, "", "noctalia", "msg", "osd-enable")
		}
		quietMu.Lock()
		quietDepth--
		quietMu.Unlock()
	}
}
