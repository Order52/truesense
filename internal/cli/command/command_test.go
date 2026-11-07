package command

import (
	"bytes"
	"context"
	"errors"
	"flag"
	"strconv"
	"strings"
	"testing"
)

func tree() (*Root, *[]string) {
	var calls []string
	root := &Root{Name: "ts", Version: "1.2.3", Short: "test program", Cmds: []*Cmd{
		{Name: "power", Short: "power mode", Sub: []*Cmd{
			{Name: "set", Short: "set it", Usage: "MODE", Flags: func(fs *flag.FlagSet) { fs.Int("hold", 30, "minutes to hold"); fs.String("color", "", "a colour") },
				Run: func(c *Ctx) error {
					hold, _ := c.Flags.Lookup("hold").Value.(flag.Getter).Get().(int)
					calls = append(calls, "power set "+c.Arg(0)+" hold="+strconv.Itoa(hold)+" color="+c.Flags.Lookup("color").Value.String())
					c.Emit(map[string]any{"mode": c.Arg(0)}, "mode set")
					return nil
				}},
			{Name: "status", Short: "show it", Run: func(c *Ctx) error { c.Emit(true, ""); return nil }},
		}},
		{Name: "fan", Short: "fans", Run: func(c *Ctx) error { return errors.New("the fans refused") }},
		{Name: "daemon", Short: "run the daemon", Hidden: true, Run: func(c *Ctx) error { return nil }},
		{Name: "bad", Short: "usage error", Run: func(c *Ctx) error { return Usagef("needs two arguments") }},
	}}
	return root, &calls
}

func run(t *testing.T, args ...string) (code int, out, errs string, calls []string) {
	t.Helper()
	root, c := tree()
	var o, e bytes.Buffer
	code = root.Execute(context.Background(), args, &o, &e)
	return code, o.String(), e.String(), *c
}

func TestFlagsMayComeAfterPositionals(t *testing.T) {
	for _, args := range [][]string{{"power", "set", "turbo", "--color", "ff0000", "--hold", "5"}, {"power", "set", "--hold", "5", "turbo", "--color=ff0000"}, {"power", "set", "--color", "ff0000", "--hold=5", "turbo"}} {
		code, _, errs, calls := run(t, args...)
		if code != ExitOK || len(calls) != 1 || calls[0] != "power set turbo hold=5 color=ff0000" {
			t.Fatalf("%v -> %d %q %v", args, code, errs, calls)
		}
	}
}

func TestDashDashEndsFlags(t *testing.T) {
	_, _, _, calls := run(t, "power", "set", "--", "--odd")
	if len(calls) != 1 || !strings.Contains(calls[0], "--odd") {
		t.Fatalf("%v", calls)
	}
}

func TestJSONAndHumanOutput(t *testing.T) {
	if _, out, _, _ := run(t, "power", "set", "turbo"); strings.TrimSpace(out) != "mode set" {
		t.Fatalf("human: %q", out)
	}
	if _, out, _, _ := run(t, "--json", "power", "set", "turbo"); !strings.Contains(out, `"mode": "turbo"`) {
		t.Fatalf("json: %q", out)
	}
	if _, out, _, _ := run(t, "power", "set", "turbo", "--json"); !strings.Contains(out, `"mode"`) {
		t.Fatalf("--json after the command works too: %q", out)
	}
	if _, out, _, _ := run(t, "power", "status"); strings.TrimSpace(out) != "on" {
		t.Fatalf("a boolean prints on/off: %q", out)
	}
}

func TestExitCodes(t *testing.T) {
	if code, _, errs, _ := run(t, "fan"); code != ExitError || !strings.Contains(errs, "ts: the fans refused") {
		t.Fatalf("runtime error: %d %q", code, errs)
	}
	if code, _, errs, _ := run(t, "bad"); code != ExitUsage || !strings.Contains(errs, "needs two arguments") || !strings.Contains(errs, "usage: ts bad") {
		t.Fatalf("usage error: %d %q", code, errs)
	}
	if code, _, errs, _ := run(t, "powr"); code != ExitUsage || !strings.Contains(errs, `did you mean "power"`) {
		t.Fatalf("typo: %d %q", code, errs)
	}
	if code, _, errs, _ := run(t, "power", "stat"); code != ExitUsage || !strings.Contains(errs, `did you mean "status"`) {
		t.Fatalf("typo in a verb: %d %q", code, errs)
	}
	if code, _, errs, _ := run(t, "power", "set", "--nope"); code != ExitUsage || !strings.Contains(errs, "nope") {
		t.Fatalf("unknown flag: %d %q", code, errs)
	}
}

func TestVersionAndHelp(t *testing.T) {
	if _, out, _, _ := run(t, "--version"); strings.TrimSpace(out) != "ts 1.2.3" {
		t.Fatalf("version: %q", out)
	}
	code, out, _, _ := run(t, "--help")
	if code != ExitOK || !strings.Contains(out, "power") || strings.Contains(out, "daemon") {
		t.Fatalf("help must list areas and hide hidden ones: %d %q", code, out)
	}
	if code, _, _, _ := run(t); code != ExitUsage {
		t.Fatal("no arguments: usage exit")
	}
	if _, out, _, _ = run(t, "power", "set", "--help"); !strings.Contains(out, "--hold") || !strings.Contains(out, "minutes to hold") || !strings.Contains(out, "usage: ts power set MODE") {
		t.Fatalf("command help: %q", out)
	}
	if _, out, _, _ = run(t, "power"); !strings.Contains(out, "status") {
		t.Fatalf("a group without a Run lists its commands: %q", out)
	}
}

func TestCompletionIsGeneratedFromTheTree(t *testing.T) {
	root, _ := tree()
	for _, sh := range []string{"bash", "zsh", "fish"} {
		s, err := root.Completion(sh)
		if err != nil || !strings.Contains(s, "power") || strings.Contains(s, "daemon") {
			t.Fatalf("%s: %v\n%s", sh, err, s)
		}
	}
	b, _ := root.Completion("bash")
	if !strings.Contains(b, "--hold") || !strings.Contains(b, "set status") {
		t.Fatalf("bash must offer verbs and their flags:\n%s", b)
	}
	if _, err := root.Completion("powershell"); err == nil {
		t.Fatal("unknown shell")
	}
}

func FuzzExecute(f *testing.F) {
	for _, s := range []string{"power set turbo", "--json power status", "", "-h", "power set --hold", "x y z", "--", "power -- set"} {
		f.Add(s)
	}
	f.Fuzz(func(t *testing.T, line string) {
		root, _ := tree()
		var o, e bytes.Buffer
		root.Execute(context.Background(), strings.Fields(line), &o, &e) // must never panic
	})
}
