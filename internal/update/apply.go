package update

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
)

// Host is everything about the machine the hand-over depends on, behind small functions so
// tests never look at the real desktop and never run an installer.
type Host struct {
	EUID     int
	Getenv   func(string) string
	LookPath func(string) (string, error)
	ProcRoot string // /proc
	// InstalledPath is the installed package's version file (the one tools/install.sh writes).
	InstalledPath string
	// Exec runs a command to completion with the terminal attached and returns its exit code.
	// It is how the installer is started: the one place an update runs code as root, so tests
	// replace it with a recorder.
	Exec func(ctx context.Context, argv []string) (int, error)
}

// DefaultHost is the real machine.
func DefaultHost() Host {
	return Host{
		EUID: os.Geteuid(), Getenv: os.Getenv, LookPath: exec.LookPath, ProcRoot: "/proc",
		InstalledPath: "/usr/local/lib/truesense/truesense/__init__.py", Exec: execAttached,
	}
}

func execAttached(ctx context.Context, argv []string) (int, error) {
	if len(argv) == 0 {
		return 0, errors.New("empty command")
	}
	cmd := exec.CommandContext(ctx, argv[0], argv[1:]...) // #nosec G204 -- the installer of a verified release, or a terminal emulator
	cmd.Stdin, cmd.Stdout, cmd.Stderr = os.Stdin, os.Stdout, os.Stderr
	err := cmd.Run()
	var exit *exec.ExitError
	switch {
	case err == nil:
		return 0, nil
	case errors.As(err, &exit):
		return exit.ExitCode(), nil
	}
	return 0, err
}

// agents are the programs that show pkexec's password prompt. polkitd alone is not enough:
// pkexec needs an AGENT to ask for the password.
var agents = []string{"polkit-gnome-authentication-agent", "polkit-kde-authentication-agent", "polkit-mate-authentication-agent", "lxqt-policykit-agent", "xfce-polkit",
	"hyprpolkitagent", "soteria", "lxpolkit", "pantheon-agent-polkit", "ukui-polkit", "cosmic-osd", "polkit-ukui-authentication-agent", "gnome-shell"}

// terminals maps each terminal emulator to how it runs "sh -c COMMAND" and waits until it ends.
var terminals = []struct {
	name string
	argv []string
}{
	{"kitty", []string{"kitty", "sh", "-c"}}, {"foot", []string{"foot", "sh", "-c"}}, {"alacritty", []string{"alacritty", "-e", "sh", "-c"}},
	{"ghostty", []string{"ghostty", "-e", "sh", "-c"}}, {"wezterm", []string{"wezterm", "start", "--always-new-process", "--", "sh", "-c"}},
	{"konsole", []string{"konsole", "-e", "sh", "-c"}}, {"gnome-terminal", []string{"gnome-terminal", "--wait", "--", "sh", "-c"}},
	{"xfce4-terminal", []string{"xfce4-terminal", "--disable-server", "-x", "sh", "-c"}}, {"xterm", []string{"xterm", "-e", "sh", "-c"}},
}

// PolkitAgentRunning reports whether some graphical password prompt for pkexec exists, by
// looking at the command line of every process under procRoot.
func PolkitAgentRunning(procRoot string) bool {
	entries, err := os.ReadDir(procRoot)
	if err != nil {
		return false
	}
	for _, e := range entries {
		if !isDigits(e.Name()) {
			continue
		}
		b, err := readPrefix(filepath.Join(procRoot, e.Name(), "cmdline"), 512)
		if err != nil {
			continue
		}
		cmd := strings.ToLower(strings.ReplaceAll(string(b), "\x00", " "))
		program, _, _ := strings.Cut(cmd, " ")
		for _, a := range agents {
			if strings.Contains(program, a) {
				return true
			}
		}
	}
	return false
}

func isDigits(s string) bool {
	return s != "" && strings.Trim(s, "0123456789") == ""
}

func readPrefix(path string, n int) ([]byte, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer func() { _ = f.Close() }()
	buf := make([]byte, n)
	k, err := f.Read(buf)
	if k > 0 {
		return buf[:k], nil
	}
	return nil, err
}

// TerminalCommand is a command that opens a terminal window, runs the shell command inner in
// it and waits: $TERMINAL first, then the usual ones. nil if there is no terminal.
func (h Host) TerminalCommand(inner string) []string {
	var names []string
	if t := h.Getenv("TERMINAL"); t != "" {
		names = append(names, filepath.Base(t))
	}
	for _, t := range terminals {
		names = append(names, t.name)
	}
	for _, n := range names {
		for _, t := range terminals {
			if t.name != n {
				continue
			}
			if _, err := h.LookPath(n); err == nil {
				return append(append([]string(nil), t.argv...), inner)
			}
		}
	}
	return nil
}

// ElevateCommand is how to run the installer as root: pkexec (graphical prompt) only when
// there is a desktop AND a polkit agent to ask for the password, else sudo.
func (h Host) ElevateCommand(script string, gui, agent bool) ([]string, error) {
	if h.EUID == 0 {
		return []string{script}, nil
	}
	has := func(name string) bool { _, err := h.LookPath(name); return err == nil }
	desktop := h.Getenv("WAYLAND_DISPLAY") != "" || h.Getenv("DISPLAY") != ""
	switch {
	case gui && agent && has("pkexec") && desktop:
		return []string{"pkexec", script}, nil
	case has("sudo"):
		return []string{"sudo", script}, nil
	case agent && has("pkexec"):
		return []string{"pkexec", script}, nil
	}
	return nil, errors.New("need sudo, or pkexec with a polkit agent, to install the update")
}

var installedVersionRE = regexp.MustCompile(`__version__\s*=\s*"([^"]+)"`)

// InstalledVersion reads the version of the installed package ("" if it is not installed).
func (h Host) InstalledVersion() string {
	b, err := os.ReadFile(h.InstalledPath)
	if err != nil {
		return ""
	}
	if m := installedVersionRE.FindSubmatch(b); m != nil {
		return string(m[1])
	}
	return ""
}

// ApplyOptions say what to install and from where it is asked.
type ApplyOptions struct {
	Tag string // "" = the newest release
	GUI bool   // asked from the window: a missing password prompt falls back to a terminal
	// Agent overrides the polkit agent detection (tests); nil means look at the processes.
	Agent *bool
	// PinSHA256 additionally pins the archive's hash.
	PinSHA256 string
}

// ApplyResult is what Apply reports. The JSON names are the 0.3.x ones; the terminal path has
// no checksum of its own (the terminal runs `truesense update apply`, which verifies).
type ApplyResult struct {
	Version    string  `json:"version"`
	SHA256     string  `json:"sha256"`
	Verified   *bool   `json:"verified,omitempty"`
	ReturnCode int     `json:"returncode"`
	Via        string  `json:"via"` // "installer" or "terminal"
	Installed  *string `json:"installed,omitempty"`
}

// Apply downloads the release and runs its installer as root. From the window (GUI) with no
// polkit agent there is no way to ask for a password, so a terminal window runs
// `truesense update apply` instead and the result is judged by what got installed.
func (u *Updater) Apply(ctx context.Context, o ApplyOptions) (ApplyResult, error) {
	st := u.Check(ctx, true, 0)
	if st.Error != nil && st.Info == nil {
		return ApplyResult{}, errors.New(*st.Error)
	}
	tag := o.Tag
	if tag == "" {
		if st.Info == nil {
			return ApplyResult{}, errors.New("no release known to install")
		}
		tag = st.Info.Tag
	}
	ver, ok := ParseVersion(tag)
	if !ok {
		return ApplyResult{}, fmt.Errorf("not a version tag: %q", tag)
	}
	version := ver.String()
	host := u.Host
	agent := false
	switch {
	case o.Agent != nil:
		agent = *o.Agent
	case o.GUI || host.EUID != 0:
		agent = PolkitAgentRunning(host.ProcRoot)
	}
	if o.GUI && host.EUID != 0 && !agent {
		return u.applyInTerminal(ctx, version)
	}
	rel, err := u.Download(ctx, tag, DownloadOptions{PinSHA256: o.PinSHA256})
	if err != nil {
		return ApplyResult{}, err
	}
	defer rel.Cleanup()
	argv, err := host.ElevateCommand(rel.Installer(), o.GUI, agent)
	if err != nil {
		return ApplyResult{}, err
	}
	code, err := host.Exec(ctx, argv)
	if err != nil {
		return ApplyResult{}, fmt.Errorf("run the installer: %w", err)
	}
	verified := rel.Verified
	return ApplyResult{Version: version, SHA256: rel.SHA256, Verified: &verified, ReturnCode: code, Via: "installer"}, nil
}

func (u *Updater) applyInTerminal(ctx context.Context, version string) (ApplyResult, error) {
	inner := fmt.Sprintf(`truesense update apply --yes --version %s; echo; printf "Press Enter to close this window... "; read _`, version)
	argv := u.Host.TerminalCommand(inner)
	if argv == nil {
		return ApplyResult{}, errors.New("this desktop has no password prompt for the window (no polkit agent) and no terminal was found. Run this in a terminal instead: truesense update apply")
	}
	code, err := u.Host.Exec(ctx, argv)
	if err != nil {
		return ApplyResult{}, fmt.Errorf("open a terminal: %w", err)
	}
	installed := u.Host.InstalledVersion()
	if installed == version {
		code = 0
	} else if code == 0 {
		code = 1 // the terminal closed but nothing was installed: not a success
	}
	res := ApplyResult{Version: version, ReturnCode: code, Via: "terminal"}
	if installed != "" {
		res.Installed = &installed
	}
	return res, nil
}
