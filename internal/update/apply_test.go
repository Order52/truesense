package update

import (
	"context"
	"os"
	"path/filepath"
	"reflect"
	"strconv"
	"strings"
	"testing"
)

func fakeProc(t *testing.T, cmdlines ...string) string {
	t.Helper()
	root := t.TempDir()
	for i, c := range cmdlines {
		dir := filepath.Join(root, itoa(100+i))
		mustMkdir(t, dir)
		if err := os.WriteFile(filepath.Join(dir, "cmdline"), []byte(strings.ReplaceAll(c, " ", "\x00")+"\x00"), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	mustMkdir(t, filepath.Join(root, "self")) // not a pid
	return root
}

func itoa(n int) string { return strconv.Itoa(n) }

func TestPolkitdAloneIsNotAPasswordPrompt(t *testing.T) {
	if PolkitAgentRunning(fakeProc(t, "/usr/lib/polkit-1/polkitd --no-debug", "/usr/bin/zsh -c source snapshot polkit-gnome")) {
		t.Error("polkitd and a shell that merely mentions polkit-gnome are not an agent")
	}
	for _, agent := range []string{"/usr/bin/hyprpolkitagent", "/usr/lib/polkit-gnome/polkit-gnome-authentication-agent-1", "/usr/bin/gnome-shell", "/usr/lib/xfce-polkit/xfce-polkit"} {
		if !PolkitAgentRunning(fakeProc(t, "/usr/bin/zsh", agent)) {
			t.Errorf("%s not recognised", agent)
		}
	}
	if PolkitAgentRunning(filepath.Join(t.TempDir(), "nowhere")) {
		t.Error("no /proc must mean no agent")
	}
	if PolkitAgentRunning(fakeProc(t)) {
		t.Error("an empty /proc has no agent")
	}
}

func hostWith(have map[string]bool, env map[string]string, euid int) Host {
	return Host{
		EUID:   euid,
		Getenv: func(k string) string { return env[k] },
		LookPath: func(n string) (string, error) {
			if have[n] {
				return "/usr/bin/" + n, nil
			}
			return "", errNotFound
		},
	}
}

func TestTheTerminalIsChosenFromTheEnvironmentThenTheUsualOnes(t *testing.T) {
	h := hostWith(map[string]bool{"foot": true, "xterm": true}, map[string]string{"TERMINAL": "/usr/bin/foot"}, 1000)
	if got := h.TerminalCommand("echo hi"); !reflect.DeepEqual(got, []string{"foot", "sh", "-c", "echo hi"}) {
		t.Errorf("got %v", got)
	}
	h = hostWith(map[string]bool{"foot": true, "xterm": true}, nil, 1000)
	if got := h.TerminalCommand("echo hi"); got[0] != "foot" {
		t.Errorf("default order: %v", got)
	}
	h = hostWith(map[string]bool{"foot": true, "xterm": true}, map[string]string{"TERMINAL": "xterm"}, 1000)
	if got := h.TerminalCommand("x"); got[0] != "xterm" {
		t.Errorf("$TERMINAL first: %v", got)
	}
	h = hostWith(map[string]bool{"foot": true}, map[string]string{"TERMINAL": "unknown-term"}, 1000)
	if got := h.TerminalCommand("x"); got[0] != "foot" {
		t.Errorf("an unknown $TERMINAL falls back: %v", got)
	}
	if got := hostWith(nil, nil, 1000).TerminalCommand("x"); got != nil {
		t.Errorf("no terminal: %v", got)
	}
	h = hostWith(map[string]bool{"wezterm": true}, nil, 1000)
	if got := h.TerminalCommand("cmd"); !reflect.DeepEqual(got, []string{"wezterm", "start", "--always-new-process", "--", "sh", "-c", "cmd"}) {
		t.Errorf("wezterm: %v", got)
	}
}

func TestPkexecOnlyWithAnAgentAndADesktop(t *testing.T) {
	wayland := map[string]string{"WAYLAND_DISPLAY": "wayland-test"}
	both := map[string]bool{"pkexec": true, "sudo": true}
	for _, tc := range []struct {
		name       string
		have       map[string]bool
		env        map[string]string
		euid       int
		gui, agent bool
		want       []string
		fails      bool
	}{
		{"agent and desktop", both, wayland, 1000, true, true, []string{"pkexec", "/x/install.sh"}, false},
		{"no agent: pkexec would fail without a prompt", both, wayland, 1000, true, false, []string{"sudo", "/x/install.sh"}, false},
		{"terminal use never pkexec", both, wayland, 1000, false, true, []string{"sudo", "/x/install.sh"}, false},
		{"no desktop", both, nil, 1000, true, true, []string{"sudo", "/x/install.sh"}, false},
		{"X11 counts as a desktop", both, map[string]string{"DISPLAY": ":0"}, 1000, true, true, []string{"pkexec", "/x/install.sh"}, false},
		{"no sudo, an agent: pkexec", map[bool]map[string]bool{true: {"pkexec": true}}[true], nil, 1000, false, true, []string{"pkexec", "/x/install.sh"}, false},
		{"nothing to elevate with", nil, wayland, 1000, true, true, nil, true},
		{"no sudo, no agent", map[string]bool{"pkexec": true}, wayland, 1000, true, false, nil, true},
		{"already root", nil, nil, 0, true, true, []string{"/x/install.sh"}, false},
	} {
		got, err := hostWith(tc.have, tc.env, tc.euid).ElevateCommand("/x/install.sh", tc.gui, tc.agent)
		if (err != nil) != tc.fails || !reflect.DeepEqual(got, tc.want) {
			t.Errorf("%s: got %v, %v", tc.name, got, err)
		}
	}
}

func TestInstalledVersion(t *testing.T) {
	dir := t.TempDir()
	h := Host{InstalledPath: filepath.Join(dir, "__init__.py")}
	if h.InstalledVersion() != "" {
		t.Error("not installed must be empty")
	}
	if err := os.WriteFile(h.InstalledPath, []byte("# x\n__version__ = \"0.3.17\"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if got := h.InstalledVersion(); got != "0.3.17" {
		t.Errorf("got %q", got)
	}
	if err := os.WriteFile(h.InstalledPath, []byte("nothing"), 0o600); err != nil {
		t.Fatal(err)
	}
	if h.InstalledVersion() != "" {
		t.Error("no version line must be empty")
	}
}

// execRecorder is the installer hand-over, recorded instead of run.
type execRecorder struct {
	ran  [][]string
	code int
	err  error
	// onRun lets a test see the state at the moment the installer would start.
	onRun func(argv []string)
}

func (e *execRecorder) Exec(_ context.Context, argv []string) (int, error) {
	e.ran = append(e.ran, argv)
	if e.onRun != nil {
		e.onRun(argv)
	}
	return e.code, e.err
}

func ptr[T any](v T) *T { return &v }

func TestApplyDownloadsVerifiesAndHandsOverToTheInstaller(t *testing.T) {
	files := merge(release(t, "0.3.17", releaseOpts{sums: "auto"}), map[string]any{"/repos/Order52/truesense/tags": tagList(t, "v0.3.17")})
	g := newFakeGitHub(t, files)
	u := updaterFor(t, g)
	rec := &execRecorder{}
	u.Host.Exec = rec.Exec
	u.Host.LookPath = hostWith(map[string]bool{"sudo": true}, nil, 1000).LookPath
	var installerExisted, installerExecutable bool
	rec.onRun = func(argv []string) {
		st, err := os.Stat(argv[1])
		installerExisted, installerExecutable = err == nil, err == nil && st.Mode().Perm()&0o100 != 0
	}
	res, err := u.Apply(context.Background(), ApplyOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if res.Via != "installer" || res.Version != "0.3.17" || res.ReturnCode != 0 || res.Verified == nil || !*res.Verified || len(res.SHA256) != 64 {
		t.Fatalf("result = %+v", res)
	}
	if len(rec.ran) != 1 || rec.ran[0][0] != "sudo" || !strings.HasSuffix(rec.ran[0][1], "/truesense-0.3.17/tools/install.sh") {
		t.Fatalf("exec = %v", rec.ran)
	}
	if !installerExisted || !installerExecutable {
		t.Error("the installer must exist and be executable when it is started")
	}
	if left := leftovers(t, u); len(left) != 0 {
		t.Errorf("the unpacked release must be removed afterwards, left %v", left)
	}
}

func TestApplyInstallsAnExplicitOlderTagAndPropagatesTheInstallerExitCode(t *testing.T) {
	files := merge(release(t, "0.3.16", releaseOpts{sums: "auto"}), map[string]any{"/repos/Order52/truesense/tags": tagList(t, "v0.3.17")})
	u := updaterFor(t, newFakeGitHub(t, files))
	rec := &execRecorder{code: 3}
	u.Host.Exec = rec.Exec
	u.Host.EUID = 0
	res, err := u.Apply(context.Background(), ApplyOptions{Tag: "v0.3.16"})
	if err != nil || res.Version != "0.3.16" || res.ReturnCode != 3 || !reflect.DeepEqual(rec.ran[0][:1], []string{rec.ran[0][0]}) || len(rec.ran[0]) != 1 {
		t.Fatalf("res = %+v, err = %v, ran = %v (root runs the script directly)", res, err, rec.ran)
	}
}

func TestApplyRefusesWhenTheDownloadCannotBeVerifiedAndNeverRunsAnInstaller(t *testing.T) {
	for name, o := range map[string]releaseOpts{
		"tampered":        {sums: "auto", tamper: true},
		"new, no list":    {sums: nil, archive: true},
		"list lacks file": {sums: strings.Repeat("a", 64) + "  other\n"},
	} {
		t.Run(name, func(t *testing.T) {
			files := merge(release(t, "0.3.17", o), map[string]any{"/repos/Order52/truesense/tags": tagList(t, "v0.3.17")})
			u := updaterFor(t, newFakeGitHub(t, files))
			rec := &execRecorder{}
			u.Host.Exec = rec.Exec
			u.Host.LookPath = hostWith(map[string]bool{"sudo": true}, nil, 1000).LookPath
			if _, err := u.Apply(context.Background(), ApplyOptions{}); err == nil {
				t.Fatal("accepted")
			}
			if len(rec.ran) != 0 {
				t.Fatalf("ran %v after a refusal", rec.ran)
			}
			if left := leftovers(t, u); len(left) != 0 {
				t.Errorf("left %v", left)
			}
		})
	}
}

func TestApplyNeedsAKnownReleaseAndAnElevationMethod(t *testing.T) {
	u := updaterFor(t, newFakeGitHub(t, nil))
	if _, err := u.Apply(context.Background(), ApplyOptions{}); !contains(err, "could not reach GitHub") {
		t.Errorf("unreachable and nothing cached: %v", err)
	}
	files := merge(release(t, "0.3.17", releaseOpts{sums: "auto"}), map[string]any{"/repos/Order52/truesense/tags": tagList(t, "v0.3.17")})
	u = updaterFor(t, newFakeGitHub(t, files)) // no sudo, no pkexec
	if _, err := u.Apply(context.Background(), ApplyOptions{Agent: ptr(false)}); !contains(err, "need sudo") {
		t.Errorf("no way to become root: %v", err)
	}
	if left := leftovers(t, u); len(left) != 0 {
		t.Errorf("left %v", left)
	}
	if _, err := u.Apply(context.Background(), ApplyOptions{Tag: "bogus"}); !contains(err, "not a version tag") {
		t.Errorf("bad tag: %v", err)
	}
}

func TestTheWindowFallsBackToATerminalWhenThereIsNoPasswordPrompt(t *testing.T) {
	files := map[string]any{"/repos/Order52/truesense/tags": tagList(t, "v9.9.9")}
	newU := func() (*Updater, *execRecorder) {
		u := updaterFor(t, newFakeGitHub(t, files))
		rec := &execRecorder{}
		u.Host.Exec = rec.Exec
		u.Host.LookPath = hostWith(map[string]bool{"myterm": true, "kitty": true}, nil, 1000).LookPath
		mustMkdir(t, filepath.Dir(u.Host.InstalledPath))
		return u, rec
	}
	install := func(u *Updater, v string) {
		if err := os.WriteFile(u.Host.InstalledPath, []byte("__version__ = \""+v+"\"\n"), 0o600); err != nil {
			t.Fatal(err)
		}
	}

	u, rec := newU()
	install(u, "9.9.9")
	res, err := u.Apply(context.Background(), ApplyOptions{GUI: true, Agent: ptr(false)})
	if err != nil || res.Via != "terminal" || res.ReturnCode != 0 || res.Installed == nil || *res.Installed != "9.9.9" || res.Verified != nil {
		t.Fatalf("res = %+v, err = %v", res, err)
	}
	if got := rec.ran[0]; got[0] != "kitty" || !strings.Contains(got[len(got)-1], "truesense update apply --yes --version 9.9.9") {
		t.Fatalf("terminal command = %v", got)
	}

	u, _ = newU() // the terminal closed but nothing was installed: not a success
	install(u, "0.3.5")
	if res, err = u.Apply(context.Background(), ApplyOptions{GUI: true, Agent: ptr(false)}); err != nil || res.ReturnCode == 0 || *res.Installed != "0.3.5" {
		t.Fatalf("res = %+v, err = %v", res, err)
	}
	u, rec = newU() // and when the terminal itself failed, its code is kept
	rec.code = 7
	if res, _ = u.Apply(context.Background(), ApplyOptions{GUI: true, Agent: ptr(false)}); res.ReturnCode != 7 || res.Installed != nil {
		t.Fatalf("res = %+v", res)
	}

	u, _ = newU() // no terminal at all
	u.Host.LookPath = hostWith(nil, nil, 1000).LookPath
	if _, err = u.Apply(context.Background(), ApplyOptions{GUI: true, Agent: ptr(false)}); !contains(err, "truesense update apply") {
		t.Fatalf("err = %v", err)
	}
	if len(rec.ran) != 1 {
		t.Errorf("unexpected exec: %v", rec.ran)
	}
}

func TestAnInstallerThatCannotBeStartedIsAnError(t *testing.T) {
	files := merge(release(t, "0.3.17", releaseOpts{sums: "auto"}), map[string]any{"/repos/Order52/truesense/tags": tagList(t, "v0.3.17")})
	u := updaterFor(t, newFakeGitHub(t, files))
	rec := &execRecorder{err: os.ErrPermission}
	u.Host.Exec = rec.Exec
	u.Host.EUID = 0
	if _, err := u.Apply(context.Background(), ApplyOptions{}); !contains(err, "run the installer") {
		t.Fatalf("err = %v", err)
	}
	if left := leftovers(t, u); len(left) != 0 {
		t.Errorf("left %v", left)
	}
}
