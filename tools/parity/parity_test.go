package parity

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
	"time"
)

// repoRoot finds the directory with go.mod, starting at this package.
func repoRoot(t *testing.T) string {
	t.Helper()
	dir, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	for {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir
		}
		if parent := filepath.Dir(dir); parent != dir {
			dir = parent
			continue
		}
		t.Skip("repository root not found")
	}
}

// harness runs one implementation of the command line with a private environment.
type harness struct {
	t       *testing.T
	name    string
	cmd     []string // program and fixed leading arguments
	dir     string   // the Python tree for python, "" for go
	home    string
	state   string
	emptyOn string // a PATH directory with nothing in it
}

func (h *harness) run(args ...string) (string, int) {
	h.t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, h.cmd[0], append(append([]string{}, h.cmd[1:]...), args...)...)
	cmd.Dir = h.dir
	cmd.Env = []string{
		"HOME=" + h.home, "TRUESENSE_SIM=1", "TRUESENSE_SOCKET=/nonexistent/x", "TRUESENSE_USERSTATE=" + h.state, "PATH=" + h.emptyOn,
		"TRUESENSE_RUN=" + filepath.Join(h.home, "run"), "LANG=C.UTF-8", "PYTHONDONTWRITEBYTECODE=1",
	}
	if h.dir != "" {
		cmd.Env = append(cmd.Env, "PYTHONPATH=src")
	}
	var out bytes.Buffer
	cmd.Stdout, cmd.Stderr = &out, &out
	err := cmd.Run()
	code := 0
	if ee, ok := err.(*exec.ExitError); ok { //nolint:errorlint // Run returns the ExitError itself
		code = ee.ExitCode()
	} else if err != nil {
		h.t.Fatalf("%s %v: %v", h.name, args, err)
	}
	return strings.ReplaceAll(out.String(), h.home, "$HOME"), code
}

func setup(t *testing.T) (py, gobin func() *harness) {
	t.Helper()
	root := repoRoot(t)
	python, err := exec.LookPath("python3")
	if err != nil {
		t.Skip("python3 is not installed")
	}
	if _, err := os.Stat(filepath.Join(root, "src", "truesense", "cli.py")); err != nil {
		t.Skip("the Python tree (src/truesense) is not here")
	}
	goTool, err := exec.LookPath("go")
	if err != nil {
		t.Skip("the go tool is not on PATH")
	}
	bin := filepath.Join(t.TempDir(), "truesense")
	build := exec.CommandContext(context.Background(), goTool, "build", "-o", bin, "./cmd/truesense")
	build.Dir = root
	build.Env = append(os.Environ(), "CGO_ENABLED=0")
	if out, err := build.CombinedOutput(); err != nil {
		t.Fatalf("build the Go command line: %v\n%s", err, out)
	}
	empty := t.TempDir()
	mk := func(name string, cmd []string, dir string) func() *harness {
		return func() *harness {
			return &harness{t: t, name: name, cmd: cmd, dir: dir, home: t.TempDir(), state: filepath.Join(t.TempDir(), "user.json"), emptyOn: empty}
		}
	}
	return mk("python", []string{python, "-m", "truesense"}, root), mk("go", []string{bin}, "")
}

// A case is one or more command lines run in order on the same private state, each with
// --json; only the last one's output is compared (the earlier ones set the scene).
type testCase struct {
	name string
	runs [][]string
	// text compares the plain output instead of --json (commands that print text only).
	text bool
}

func one(name string, args ...string) testCase { return testCase{name: name, runs: [][]string{args}} }

var cases = []testCase{
	one("status", "status"), one("sensors", "sensors"),
	one("power status", "power", "status"), one("power list", "power", "list"), one("power set", "power", "set", "turbo"),
	one("power smart list", "power", "smart", "list"), one("power smart show", "power", "smart", "show", "steady"),
	one("fan status", "fan", "status"), one("fan auto", "fan", "auto"), one("fan max", "fan", "max"),
	one("fan curve show", "fan", "curve", "show", "quiet"),
	one("battery status", "battery", "status"), one("battery limit on", "battery", "limit", "on"),
	one("battery usb-charge", "battery", "usb-charge", "10"), one("battery calibration", "battery", "calibration"),
	one("display overdrive", "display", "overdrive"), one("display overdrive off", "display", "overdrive", "off"),
	one("boot sound", "boot", "sound"), one("boot sound toggle", "boot", "sound", "toggle"),
	one("gpu status", "gpu", "status"), one("gpu set", "gpu", "set", "auto"),
	one("gpu undervolt list", "gpu", "undervolt", "list"), one("gpu undervolt show", "gpu", "undervolt", "show", "native"),
	one("rear status", "rear", "status"),
	{name: "rear set wave", runs: [][]string{{"rear", "set", "wave", "--color", "00ff88", "--speed", "2"}}},
	{name: "rear set static zones", runs: [][]string{{"rear", "set", "static", "--zones", "ff0000,00ff00,0000ff"}, {"rear", "status"}}},
	{name: "rear off then on", runs: [][]string{{"rear", "off"}, {"rear", "on"}}},
	one("keyboard status", "keyboard", "status"), one("keyboard list-keys", "keyboard", "list-keys"), one("keyboard layout", "keyboard", "layout"),
	one("keyboard brightness", "keyboard", "brightness"), one("keyboard timeout", "keyboard", "timeout"),
	{name: "keyboard effect", runs: [][]string{{"keyboard", "effect", "wave", "--brightness", "40"}, {"keyboard", "status"}}},
	{name: "keyboard fill", runs: [][]string{{"keyboard", "fill", "ff0000"}, {"keyboard", "status"}}},
	one("keyboard design list", "keyboard", "design", "list"),
	one("look list", "look", "list"), one("look show", "look", "show", "native"),
	{name: "look save then show", runs: [][]string{{"keyboard", "effect", "static", "--color", "ff0000"}, {"look", "save", "mine"}, {"look", "show", "mine"}}},
	one("audio output", "audio", "output"), one("audio mic", "audio", "mic"), one("audio jack", "audio", "jack"), one("audio voice", "audio", "voice"),
	one("audio speaker-filter", "audio", "speaker-filter"),
	one("cpu info", "cpu", "info"), one("cpu undervolt list", "cpu", "undervolt", "list"), one("cpu undervolt show", "cpu", "undervolt", "show", "mild"),
	one("config show", "config", "show"), one("config validate", "config", "validate"), one("config path", "config", "path"),
	one("startup show", "startup", "show"),
	{name: "startup set then show", runs: [][]string{{"startup", "set", "keyboard", "default"}, {"startup", "show"}}},
	{name: "keys set then show", runs: [][]string{{"keys", "set", "double", "output-toggle"}, {"keys", "show"}}},
	{name: "keys profile save use current", runs: [][]string{{"keys", "profile", "save", "mine", "--tap", "power:turbo"}, {"keys", "profile", "use", "mine"}, {"keys", "profile", "current"}}},
	{name: "keys profile list after save", runs: [][]string{{"keys", "profile", "save", "mine", "--tap", "power:turbo"}, {"keys", "profile", "list"}}},
	{name: "keys reset", runs: [][]string{{"keys", "set", "tap", "power-next"}, {"keys", "reset", "tap"}, {"keys", "show"}}},
	{name: "profile save then list", runs: [][]string{{"profile", "save", "mine", "--description", "x"}, {"profile", "list"}}},
	{name: "profile save then show", runs: [][]string{{"profile", "save", "mine"}, {"profile", "show", "mine"}}},
	{name: "cpu undervolt save list", runs: [][]string{{"cpu", "undervolt", "save", "x", "--core", "-30", "--cache", "-20"}, {"cpu", "undervolt", "list"}}},
	{name: "gpu undervolt save list", runs: [][]string{{"gpu", "undervolt", "save", "x", "--offset", "100", "--cap", "2400"}, {"gpu", "undervolt", "list"}}},
	{name: "fan curve save", runs: [][]string{{"fan", "curve", "save", "mine", "--cpu", "52:0,60:35,70:55,80:75,89:100", "--gpu", "48:0,60:40,78:100"}, {"fan", "curve", "show", "mine"}}},
	{name: "fan curve save too cool", runs: [][]string{{"fan", "curve", "save", "mine", "--cpu", "52:0,60:35", "--gpu", "48:0,60:40"}}},
	{name: "power smart save", runs: [][]string{{"power", "smart", "save", "mine", "--set", "ac_busy=turbo", "--set", "ac_turbo=off"}, {"power", "smart", "show", "mine"}}},
	{name: "update auto on", runs: [][]string{{"update", "auto", "on"}, {"update", "auto"}}},
	{name: "audio voice notify", runs: [][]string{{"audio", "voice", "notify", "off"}, {"audio", "voice"}}},
	{name: "keyboard paint", runs: [][]string{{"keyboard", "paint", "w=ff0000", "esc=00ff00"}, {"keyboard", "status"}}},
	{name: "keyboard design apply", runs: [][]string{{"keyboard", "design", "apply", "sunset"}, {"keyboard", "status"}}},
	{name: "keyboard profile save list", runs: [][]string{{"keyboard", "profile", "save", "p1", "fill", "ff0000"}, {"keyboard", "profile", "list"}}},
	{name: "keyboard brightness set", runs: [][]string{{"keyboard", "brightness", "30"}, {"keyboard", "status"}}},
	{name: "rear set breathing", runs: [][]string{{"rear", "set", "breathing", "--color", "ff0000"}}},
	{name: "gpu set", runs: [][]string{{"gpu", "set", "optimus"}, {"gpu", "status"}}},
	{name: "usb charge", runs: [][]string{{"battery", "usb-charge", "20"}, {"battery", "status"}}},
	one("keys show", "keys", "show"), one("keys profile list", "keys", "profile", "list"), one("keys profile show", "keys", "profile", "show", "default"),
	one("keys timing", "keys", "timing"),
	one("profile list", "profile", "list"),
	one("touchpad", "touchpad"),
	one("probe", "probe"),
	one("service check", "service", "check"),
	{name: "completion bash", runs: [][]string{{"completion", "bash"}}, text: true},
	{name: "selftest", runs: [][]string{{"selftest", "--skip-fans"}}, text: true},
}

// knownDifferences are differences that were looked at and are on purpose, or are the Python
// tool's bugs; each says why. A case may only differ at the paths listed here.
var knownDifferences = map[string]string{}

func TestParity(t *testing.T) {
	py, gobin := setup(t)
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			p, g := py(), gobin()
			p.t, g.t = t, t
			var pyOut, goOut string
			var pyCode, goCode int
			for i, run := range tc.runs {
				args := run
				if !tc.text {
					args = append([]string{"--json"}, run...)
				}
				pyOut, pyCode = p.run(args...)
				goOut, goCode = g.run(args...)
				if i < len(tc.runs)-1 && pyCode != goCode {
					t.Fatalf("setup step %v: exit python %d, go %d\npython: %s\ngo: %s", run, pyCode, goCode, pyOut, goOut)
				}
			}
			if pyCode != goCode {
				t.Errorf("exit code: python %d, go %d", pyCode, goCode)
			}
			if tc.text {
				compareText(t, tc.name, pyOut, goOut)
				return
			}
			var pj, gj any
			errP, errG := json.Unmarshal([]byte(pyOut), &pj), json.Unmarshal([]byte(goOut), &gj)
			if errP != nil || errG != nil {
				if strings.TrimSpace(pyOut) != strings.TrimSpace(goOut) {
					t.Errorf("not JSON on both sides (python: %v, go: %v)\npython: %s\ngo: %s", errP, errG, pyOut, goOut)
				}
				return
			}
			for _, d := range diff("$", pj, gj) {
				if why, ok := knownDifferences[tc.name+" "+strings.SplitN(d, ":", 2)[0]]; ok {
					t.Logf("known difference: %s (%s)", d, why)
					continue
				}
				t.Errorf("%s", d)
			}
		})
	}
}

// compareText compares plain output line by line, ignoring times, versions and the order of
// the self-test's parallel doctor lines.
func compareText(t *testing.T, name, py, goOut string) {
	t.Helper()
	norm := func(s string) []string {
		var out []string
		for _, l := range strings.Split(strings.TrimRight(s, "\n"), "\n") {
			out = append(out, strings.TrimRight(l, " "))
		}
		return out
	}
	p, g := norm(py), norm(goOut)
	if name == "selftest" {
		compareSelftest(t, py, goOut)
		return
	}
	if name == "completion bash" {
		// the completion scripts list the same areas and verbs; their text is generated by
		// each tool, so only the words are compared
		pw, gw := words(py), words(goOut)
		hidden := map[string]bool{"version": true, "auto-run": true, "daemon": true} // not offered by the Go tool on purpose
		for w := range pw {
			if !gw[w] && !hidden[w] && !strings.HasPrefix(w, "-") && !strings.ContainsAny(w, "\"$()[]{}=;|") {
				t.Errorf("python completes %q, go does not", w)
			}
		}
		return
	}
	for i := 0; i < max(len(p), len(g)); i++ {
		var pl, gl string
		if i < len(p) {
			pl = p[i]
		}
		if i < len(g) {
			gl = g[i]
		}
		if pl != gl {
			t.Errorf("line %d:\n python: %s\n go:     %s", i+1, pl, gl)
		}
	}
}

// compareSelftest compares what the self-test decided (PASS, FAIL or SKIP and the item) and the
// total. The details are worded by each tool, and the health check at the end is the doctor's
// own report, which has its own comparison in the doctor's tests.
func compareSelftest(t *testing.T, py, goOut string) {
	t.Helper()
	items := func(s string) []string {
		var out []string
		for _, l := range strings.Split(s, "\n") {
			if f := strings.Fields(l); len(f) > 1 && (f[0] == "PASS" || f[0] == "FAIL" || f[0] == "SKIP") {
				name, _, _ := strings.Cut(strings.Join(f[1:], " "), "  (")
				name, _, _ = strings.Cut(name, " (")
				name, _, _ = strings.Cut(name, ":")
				out = append(out, f[0]+" "+name)
			} else if strings.HasSuffix(l, "passed") {
				out = append(out, l)
			}
		}
		return out
	}
	p, g := items(py), items(goOut)
	if strings.Join(p, "\n") != strings.Join(g, "\n") {
		t.Errorf("self-test items differ\npython:\n%s\ngo:\n%s", strings.Join(p, "\n"), strings.Join(g, "\n"))
	}
}

var wordRE = regexp.MustCompile(`-?-?[A-Za-z0-9_][A-Za-z0-9_-]*`)

func words(s string) map[string]bool {
	out := map[string]bool{}
	for _, w := range wordRE.FindAllString(s, -1) {
		out[w] = true
	}
	return out
}

// humanCases are compared as plain text, line for line: the messages users read.
var humanCases = [][]string{
	{"sensors"}, {"power", "status"}, {"power", "list"}, {"power", "set", "turbo"}, {"power", "smart", "list"}, {"power", "smart", "show", "silent"},
	{"power", "smart", "copy", "silent", "mine"}, {"power", "auto"}, {"power", "hold"}, {"power", "managers"}, {"power", "game"},
	{"fan", "status"}, {"fan", "auto"}, {"fan", "max"}, {"fan", "off"}, {"fan", "set", "50"}, {"fan", "set", "0", "--confirm"}, {"fan", "set", "10"},
	{"fan", "curve", "show", "quiet"}, {"fan", "curve", "list"}, {"fan", "curve", "copy", "quiet", "q2"}, {"fan", "smart"},
	{"battery", "limit"}, {"battery", "limit", "on"}, {"battery", "limit", "toggle"}, {"battery", "calibration"}, {"battery", "usb-charge"}, {"battery", "usb-charge", "20"}, {"battery", "full"},
	{"display", "overdrive"}, {"display", "overdrive", "toggle"}, {"boot", "sound", "on"},
	{"gpu", "status"}, {"gpu", "set", "optimus"}, {"gpu", "undervolt", "list"}, {"gpu", "undervolt", "show", "mild"}, {"gpu", "undervolt", "get"}, {"gpu", "undervolt", "save", "x", "--offset", "100"},
	{"cpu", "undervolt", "list"}, {"cpu", "undervolt", "show", "mild"}, {"cpu", "undervolt", "save", "x", "--core", "-30", "--cache", "-20"}, {"cpu", "undervolt", "use", "mild"}, {"cpu", "limits"},
	{"keyboard", "brightness"}, {"keyboard", "brightness", "30"}, {"keyboard", "timeout", "off"}, {"keyboard", "speed", "3"}, {"keyboard", "color", "ff0000"}, {"keyboard", "paint", "w=ff0000", "esc=00ff00"},
	{"keyboard", "gradient", "ff0000", "0000ff"}, {"keyboard", "rainbow"}, {"keyboard", "keys", "wasd=ff0000", "--base", "101010"}, {"keyboard", "design", "list"}, {"keyboard", "design", "apply", "sunset"},
	{"keyboard", "fx", "wave"}, {"keyboard", "fx", "off"}, {"keyboard", "profile", "list"}, {"keyboard", "profile", "save", "p1", "fill", "ff0000"}, {"keyboard", "restore"}, {"keyboard", "on"}, {"keyboard", "off"}, {"keyboard", "toggle"},
	{"rear", "on"}, {"rear", "off"}, {"rear", "toggle"},
	{"look", "list"}, {"look", "show", "native"}, {"look", "apply", "native"}, {"look", "save", "mine"}, {"look", "delete", "nope"},
	{"startup", "show"}, {"startup", "set", "keyboard", "default"}, {"startup", "set", "rear", "off"}, {"startup", "set", "profile", "none"}, {"startup", "apply"},
	{"keys", "show"}, {"keys", "set", "tap", "power-next"}, {"keys", "set", "tap", "bogus"}, {"keys", "reset"}, {"keys", "timing", "--long-ms", "500"}, {"keys", "profile"}, {"keys", "profile", "list"},
	{"keys", "profile", "show", "default"}, {"keys", "profile", "copy", "default", "mine"}, {"keys", "test", "notify:hi"},
	{"profile", "list"}, {"profile", "show", "gaming"}, {"profile", "apply", "gaming"}, {"profile", "save", "mine"}, {"profile", "next"},
	{"audio", "output"}, {"audio", "mic"}, {"audio", "speaker-filter"}, {"audio", "dts", "devices"},
	{"config", "validate"}, {"config", "path"}, {"touchpad"}, {"update", "auto"}, {"version"},
	{"power", "set", "ludicrous"}, {"power", "bogus"}, {"nosuch"}, {"fan", "set"}, {"rear", "set", "wave", "--direction", "7"},
}

// TestParityHuman compares the plain-text output and exit codes. Differences that are on
// purpose are listed in humanKnown with the reason.
func TestParityHuman(t *testing.T) {
	py, gobin := setup(t)
	for _, args := range humanCases {
		t.Run(strings.Join(args, " "), func(t *testing.T) {
			p, g := py(), gobin()
			p.t, g.t = t, t
			pyOut, pyCode := p.run(args...)
			goOut, goCode := g.run(args...)
			if pyCode != goCode {
				t.Errorf("exit code: python %d, go %d", pyCode, goCode)
			}
			if why, ok := humanKnown[strings.Join(args, " ")]; ok {
				t.Logf("known difference (%s)\n python: %s\n go:     %s", why, pyOut, goOut)
				return
			}
			if strings.TrimSpace(pyOut) != strings.TrimSpace(goOut) {
				t.Errorf("output differs\n python: %s\n go:     %s", pyOut, goOut)
			}
		})
	}
}

// humanKnown are the plain-text outputs that differ on purpose, by command line.
var humanKnown = map[string]string{
	"power set ludicrous":         usageNote,
	"power bogus":                 usageNote,
	"nosuch":                      usageNote,
	"fan set":                     usageNote,
	"rear set wave --direction 7": usageNote,
}

// usageNote: a wrong command line exits 2 on both sides with the same complaint (the argument
// and the allowed values); argparse and the Go parser lay the usage line out differently.
const usageNote = "usage errors are laid out differently by each parser; same message, same exit code"
