package gamerules

import (
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"strconv"
	"strings"
	"testing"
)

func TestCleanName(t *testing.T) {
	tests := []struct {
		in      string
		want    string
		wantErr bool
	}{
		{"Game.EXE", "game.exe", false},
		{"  hl2_linux  ", "hl2_linux", false},
		{"my game", "my game", false},
		{"c++", "c++", false},
		{"a-b_c.d+e", "a-b_c.d+e", false},
		{strings.Repeat("a", 80), strings.Repeat("a", 80), false},
		{strings.Repeat("a", 81), "", true},
		{"", "", true},
		{"   ", "", true},
		{"../x", "", true}, // Python test
		{"a/b", "", true},
		{"a\\b", "", true},
		{"a\x00b", "", true},
		{"naïve", "", true},
		{"a;b", "", true},
		{"a\tb", "", true},
	}
	for _, tt := range tests {
		got, err := CleanName(tt.in)
		if (err != nil) != tt.wantErr || got != tt.want {
			t.Errorf("CleanName(%q) = %q, %v; want %q (err %v)", tt.in, got, err, tt.want, tt.wantErr)
		}
		if err != nil && !strings.Contains(err.Error(), "program name: letters, digits") {
			t.Errorf("message: %v", err)
		}
	}
}

func TestValidate(t *testing.T) {
	many := map[string]any{}
	for i := 0; i < 65; i++ {
		many["game"+strconv.Itoa(i)] = "eco"
	}
	atMax := map[string]any{}
	for i := 0; i < 64; i++ {
		atMax["game"+strconv.Itoa(i)] = "turbo"
	}
	tests := []struct {
		name    string
		in      any
		want    Rules
		wantErr string
	}{
		{"empty", map[string]any{}, Rules{}, ""},
		{"valid", map[string]any{"hl2_linux": "performance", "EldenRing.exe": "turbo"}, Rules{"hl2_linux": "performance", "eldenring.exe": "turbo"}, ""},
		{"every mode", map[string]any{"a": "eco", "b": "quiet", "c": "balanced", "d": "performance", "e": "turbo"}, Rules{"a": "eco", "b": "quiet", "c": "balanced", "d": "performance", "e": "turbo"}, ""},
		{"names that clean alike collapse", map[string]any{"Game": "eco", "game": "turbo"}, Rules{"game": "turbo"}, ""},
		{"bad mode (Python test)", map[string]any{"x": "warp"}, nil, "x: mode must be one of eco, quiet, balanced, performance, turbo"},
		{"mode is case sensitive", map[string]any{"x": "Turbo"}, nil, "mode must be one of"},
		{"mode as number", map[string]any{"x": 3}, nil, "mode must be one of"},
		{"mode null", map[string]any{"x": nil}, nil, "mode must be one of"},
		{"bad name", map[string]any{"../x": "turbo"}, nil, "program name: letters, digits"},
		{"empty name", map[string]any{"": "turbo"}, nil, "program name: letters, digits"},
		{"64 rules are fine", atMax, nil, ""},
		{"65 rules are too many", many, nil, "at most 64 rules"},
		{"not a map", []any{"a"}, nil, "game rules are a name -> mode map"},
		{"null", nil, nil, "game rules are a name -> mode map"},
		{"text", "x", nil, "game rules are a name -> mode map"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := Validate(tt.in)
			if tt.wantErr != "" {
				if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
					t.Fatalf("err = %v, want %q", err, tt.wantErr)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if tt.want != nil && !reflect.DeepEqual(got, tt.want) {
				t.Errorf("got %v, want %v", got, tt.want)
			}
		})
	}
}

// Python: test_game_rules_raise_the_mode_while_the_game_runs (the rule management part).
func TestAddRemove(t *testing.T) {
	r := Rules{}
	r2, err := r.Add("Game.EXE", "performance")
	if err != nil || r2["game.exe"] != "performance" {
		t.Fatalf("Add = %v, %v", r2, err)
	}
	if len(r) != 0 {
		t.Error("Add changed the receiver")
	}
	for _, bad := range []struct{ name, mode string }{{"x", "warp"}, {"../x", "turbo"}, {"", "turbo"}, {"x", ""}} {
		if _, err := r2.Add(bad.name, bad.mode); err == nil {
			t.Errorf("Add(%q, %q) accepted", bad.name, bad.mode)
		}
	}
	if _, err := r2.Remove("nothing"); err == nil || !strings.Contains(err.Error(), "no rule for 'nothing'") {
		t.Errorf("Remove(nothing): %v", err)
	}
	if _, err := r2.Remove("../x"); err == nil {
		t.Error("Remove of a bad name accepted")
	}
	r3, err := r2.Remove(" GAME.exe ")
	if err != nil || len(r3) != 0 || len(r2) != 1 {
		t.Errorf("Remove = %v, %v (receiver %v)", r3, err, r2)
	}
	full := Rules{}
	for i := 0; i < MaxRules; i++ {
		full["g"+strconv.Itoa(i)] = "eco"
	}
	if _, err := full.Add("one-more", "eco"); err == nil || !strings.Contains(err.Error(), "at most 64 rules") {
		t.Errorf("Add past the limit: %v", err)
	}
	if got, err := full.Add("g1", "turbo"); err != nil || got["g1"] != "turbo" {
		t.Errorf("replacing a rule at the limit: %v, %v", got, err)
	}
}

func TestProcessNames(t *testing.T) {
	tests := []struct {
		name string
		p    Process
		want []string
	}{
		{"plain program", Process{"bash\n", []byte("/bin/bash\x00")}, []string{"bash"}},
		{"native game", Process{"hl2_linux\n", []byte("/home/u/hl2_linux\x00-game\x00hl2\x00")}, []string{"hl2_linux"}},
		{"wine game", Process{"wine64-preloader\n", []byte("Z:\\games\\EldenRing.exe\x00")}, []string{"wine64-preloader", "eldenring.exe"}},
		{"wine game, exe not first", Process{"wine64-preloader\n", []byte("/usr/bin/wine64-preloader\x00C:\\Games\\Foo\\bar.EXE\x00-flag\x00")}, []string{"wine64-preloader", "wine64-preloade", "bar.exe"}},
		{"long exe name is also cut to 15", Process{"x\n", []byte("a.exe-this-is-long-name.exe\x00")}, []string{"x", "a.exe-this-is-long-name.exe", "a.exe-this-is-l"}},
		{"comm only", Process{"kworker\n", nil}, []string{"kworker"}},
		{"empty everything", Process{"", nil}, nil},
		{"an .exe beyond the 12th argument is ignored", Process{"w\n", []byte(strings.Repeat("a\x00", 12) + "late.exe\x00")}, []string{"w", "a"}},
		{"argument that is not .exe and not first is ignored", Process{"p\n", []byte("/bin/p\x00data.txt\x00")}, []string{"p"}},
		{"empty arguments do not count", Process{"p\n", []byte("\x00\x00/bin/p\x00\x00")}, []string{"p"}},
		{"bad UTF-8 does not panic", Process{"p\xff\n", []byte("\xff\xfe.exe\x00")}, []string{"p\ufffd", "\ufffd\ufffd.exe"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := tt.p.Names()
			want := map[string]bool{}
			for _, n := range tt.want {
				want[n] = true
			}
			if !reflect.DeepEqual(got, want) {
				t.Errorf("Names = %v, want %v", got, want)
			}
		})
	}
}

// Python: test_game_rule_matching_reads_process_names.
func TestRunning(t *testing.T) {
	procs := []Process{
		{"bash\n", []byte("/bin/bash\x00")},
		{"hl2_linux\n", []byte("/home/u/hl2_linux\x00-game\x00hl2\x00")},
		{"wine64-preloader\n", []byte("Z:\\games\\EldenRing.exe\x00")},
	}
	tests := []struct {
		name  string
		rules Rules
		procs []Process
		want  string
		found bool
	}{
		{"native name", Rules{"hl2_linux": "performance"}, procs, "performance", true},
		{"the highest mode wins", Rules{"eldenring.exe": "turbo", "hl2_linux": "performance"}, procs, "turbo", true},
		{"the highest wins whatever the order", Rules{"hl2_linux": "turbo", "eldenring.exe": "eco"}, procs, "turbo", true},
		{"no match", Rules{"notepad.exe": "turbo"}, procs, "", false},
		{"no rules", Rules{}, procs, "", false},
		{"nil rules", nil, procs, "", false},
		{"no processes", Rules{"hl2_linux": "turbo"}, nil, "", false},
		{"the file name of argument zero", Rules{"bash": "eco"}, procs, "eco", true},
		{"a 15-character comm matches the full rule name", Rules{"verylongprogramname": "turbo"}, []Process{{"verylongprogram\n", []byte("/opt/verylongprogramname\x00")}}, "turbo", true},
		{"rule written long, comm is cut", Rules{"averyveryverylongname": "quiet"}, []Process{{"averyveryverylo\n", nil}}, "", false},
		{"rule of exactly the cut length matches the cut comm", Rules{"averyveryverylo": "quiet"}, []Process{{"averyveryverylo\n", nil}}, "quiet", true},
		{"rules are looked up in lower case names", Rules{"eldenring.exe": "turbo"}, []Process{{"x\n", []byte("C:\\ELDENRING.EXE\x00")}}, "turbo", true},
		{"an unknown mode in a rule is ignored", Rules{"bash": "warp"}, procs, "", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, found := Running(tt.rules, tt.procs)
			if got != tt.want || found != tt.found {
				t.Errorf("Running = %q, %v; want %q, %v", got, found, tt.want, tt.found)
			}
		})
	}
}

func fakeProc(t *testing.T, root, pid, comm string, args ...string) {
	t.Helper()
	dir := filepath.Join(root, pid)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "comm"), []byte(comm+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	cmd := []byte{}
	for _, a := range args {
		cmd = append(cmd, a...)
		cmd = append(cmd, 0)
	}
	if err := os.WriteFile(filepath.Join(dir, "cmdline"), cmd, 0o644); err != nil {
		t.Fatal(err)
	}
}

// The same scenario as the Python test, through a fake /proc tree.
func TestListProcessesAndRunning(t *testing.T) {
	root := t.TempDir()
	fakeProc(t, root, "10", "bash", "/bin/bash")
	fakeProc(t, root, "11", "hl2_linux", "/home/u/hl2_linux", "-game", "hl2")
	fakeProc(t, root, "12", "wine64-preloader", "Z:\\games\\EldenRing.exe")
	procs := ListProcesses(root)
	if len(procs) != 3 {
		t.Fatalf("ListProcesses = %d processes, want 3", len(procs))
	}
	for _, tt := range []struct {
		rules Rules
		want  string
		found bool
	}{
		{Rules{"hl2_linux": "performance"}, "performance", true},
		{Rules{"eldenring.exe": "turbo", "hl2_linux": "performance"}, "turbo", true},
		{Rules{"notepad.exe": "turbo"}, "", false},
		{Rules{}, "", false},
	} {
		if got, found := Running(tt.rules, procs); got != tt.want || found != tt.found {
			t.Errorf("Running(%v) = %q, %v", tt.rules, got, found)
		}
	}
}

func TestListProcessesSkipsWhatIsNotAProcess(t *testing.T) {
	root := t.TempDir()
	fakeProc(t, root, "5", "ok", "/bin/ok")
	// Not numeric, a plain file named like a pid, a process without comm, one without cmdline.
	if err := os.MkdirAll(filepath.Join(root, "self"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "99"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(root, "7"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(root, "8"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "8", "comm"), []byte("nocmd\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	procs := ListProcesses(root)
	if len(procs) != 2 {
		t.Fatalf("got %d processes: %+v", len(procs), procs)
	}
	if got, found := Running(Rules{"nocmd": "eco"}, procs); !found || got != "eco" {
		t.Error("a process without a command line is still matched by name")
	}
	if got := ListProcesses(filepath.Join(root, "missing")); len(got) != 0 {
		t.Errorf("a missing /proc = %v", got)
	}
}

func TestListProcessesBoundsTheCommandLine(t *testing.T) {
	root := t.TempDir()
	fakeProc(t, root, "1", "big", strings.Repeat("a", 10000))
	procs := ListProcesses(root)
	if len(procs) != 1 || len(procs[0].Cmdline) != maxCmdline {
		t.Fatalf("cmdline length = %d, want %d", len(procs[0].Cmdline), maxCmdline)
	}
}

func TestRulesJSON(t *testing.T) {
	b, err := json.Marshal(Rules{"a": "eco"})
	if err != nil || string(b) != `{"a":"eco"}` {
		t.Errorf("= %s, %v", b, err)
	}
}

func FuzzValidate(f *testing.F) {
	for _, s := range []string{`{}`, `{"hl2_linux":"performance"}`, `{"X":"warp"}`, `null`, `[]`, `{"../x":"eco"}`, `{"a":1}`, `{"A b":"turbo","a b":"eco"}`} {
		f.Add([]byte(s))
	}
	f.Fuzz(func(t *testing.T, data []byte) {
		r, err := Parse(data)
		if err != nil {
			return
		}
		if len(r) > MaxRules {
			t.Fatalf("%d rules accepted", len(r))
		}
		for name, mode := range r {
			if c, err := CleanName(name); err != nil || c != name {
				t.Fatalf("accepted name %q (clean %q, %v)", name, c, err)
			}
			if modeRank(mode) < 0 {
				t.Fatalf("accepted mode %q", mode)
			}
		}
		b, _ := json.Marshal(r)
		again, err := Parse(b)
		if err != nil || !reflect.DeepEqual(again, r) {
			t.Fatalf("round trip %v -> %s -> %v (%v)", r, b, again, err)
		}
	})
}

func FuzzCleanName(f *testing.F) {
	for _, s := range []string{"Game.EXE", "", "../x", "a b", "\x00", "ÄÖ"} {
		f.Add(s)
	}
	f.Fuzz(func(t *testing.T, s string) {
		c, err := CleanName(s)
		if err != nil {
			return
		}
		if again, err := CleanName(c); err != nil || again != c {
			t.Fatalf("CleanName(%q) = %q is not stable", s, c)
		}
		if strings.ContainsAny(c, "/\\\x00") {
			t.Fatalf("path character in %q", c)
		}
	})
}

// FuzzProcessMatching: whatever bytes a process shows in /proc, matching must not panic and
// must only report modes that exist.
func FuzzProcessMatching(f *testing.F) {
	f.Add("bash\n", []byte("/bin/bash\x00"), "bash")
	f.Add("\xff", []byte("\xff\x00\\\\.exe"), "x")
	f.Fuzz(func(t *testing.T, comm string, cmdline []byte, name string) {
		p := Process{Comm: comm, Cmdline: cmdline}
		for n := range p.Names() {
			if n != strings.ToLower(n) {
				t.Fatalf("name %q is not lower case", n)
			}
		}
		if m, ok := Running(Rules{name: "turbo"}, []Process{p}); ok && m != "turbo" {
			t.Fatalf("mode %q", m)
		}
	})
}
