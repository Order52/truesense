package release

import (
	"bytes"
	"context"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// repoRoot is the real repository (this package is two levels below it); it is only ever read.
func repoRoot(t *testing.T) string {
	t.Helper()
	root, err := filepath.Abs("../..")
	if err != nil {
		t.Fatal(err)
	}
	return root
}

func readFile(t *testing.T, path string) string {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

func writeFile(t *testing.T, path, text string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(text), 0o644); err != nil {
		t.Fatal(err)
	}
}

// gitIn runs git in a throw-away repository with no signing and a fixed identity.
func gitIn(t *testing.T, dir string, args ...string) string {
	t.Helper()
	cmd := exec.CommandContext(context.Background(), "git", args...)
	cmd.Dir = dir
	cmd.Env = append(os.Environ(), "GIT_CONFIG_GLOBAL=/dev/null", "GIT_CONFIG_SYSTEM=/dev/null")
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, out)
	}
	return strings.TrimSpace(string(out))
}

// tree is a minimal copy of the repository: every versioned file, release notes and a git history.
// Everything runs on it, never on the real repository.
func tree(t *testing.T, unreleased string) string {
	t.Helper()
	root, d := repoRoot(t), t.TempDir()
	for _, e := range Table() {
		writeFile(t, filepath.Join(d, e.File), readFile(t, filepath.Join(root, e.File)))
	}
	cur, err := Current(d)
	if err != nil {
		t.Fatal(err)
	}
	writeFile(t, filepath.Join(d, "RELEASE_NOTES.md"),
		"# Release notes\n\n## Unreleased\n\n"+unreleased+"\n## "+cur+" (2026-01-01): the old one\n\n- old stuff, see also 0.1.0\n\n## 0.0.1 (2025-01-01)\n\n- first\n")
	gitIn(t, d, "init", "-q", "-b", "main")
	for _, kv := range [][2]string{{"user.name", "t"}, {"user.email", "t@t"}, {"commit.gpgsign", "false"}, {"tag.gpgsign", "false"}} {
		gitIn(t, d, "config", kv[0], kv[1])
	}
	gitIn(t, d, "add", "-A")
	gitIn(t, d, "commit", "-q", "-m", "start")
	return d
}

const someNotes = "**New thing**\n- it works\n"

func TestTheRealRepositoryIsConsistent(t *testing.T) {
	problems, err := Check(repoRoot(t), "")
	if err != nil || len(problems) > 0 {
		t.Fatalf("Check = %v, %v", problems, err)
	}
}

func TestAFileThatIsOutOfStepIsNamed(t *testing.T) {
	d := tree(t, someNotes)
	cur, _ := Current(d)
	p := filepath.Join(d, "tools/get.sh")
	writeFile(t, p, strings.Replace(readFile(t, p), cur, "0.0.9", 1))
	problems, err := Check(d, "")
	if err != nil || len(problems) != 1 {
		t.Fatalf("Check = %v, %v", problems, err)
	}
	if !strings.Contains(problems[0], "tools/get.sh") || !strings.Contains(problems[0], "0.0.9") {
		t.Errorf("problem does not name the file and the wrong version: %q", problems[0])
	}
}

func TestATagMustMatchTheVersion(t *testing.T) {
	d := tree(t, someNotes)
	cur, _ := Current(d)
	if problems, err := Check(d, "v"+cur); err != nil || len(problems) != 0 {
		t.Fatalf("matching tag: %v, %v", problems, err)
	}
	problems, _ := Check(d, "v9.9.9")
	if len(problems) != 1 || !strings.Contains(problems[0], "does not match") {
		t.Errorf("wrong tag: %v", problems)
	}
	if _, err := Check(d, "banana"); err == nil {
		t.Error("a tag that is not a version must stop the check")
	}
}

func TestAMissingMentionIsAProblemToo(t *testing.T) {
	d := tree(t, someNotes)
	cur, _ := Current(d)
	p := filepath.Join(d, "packaging/arch/PKGBUILD")
	writeFile(t, p, strings.Replace(readFile(t, p), "pkgver="+cur, "pkgver=banana", 1))
	found := false
	problems, _ := Check(d, "")
	for _, x := range problems {
		found = found || (strings.Contains(x, "PKGBUILD") && strings.Contains(x, "expected 1"))
	}
	if !found {
		t.Errorf("no 'expected 1' problem for the PKGBUILD in %v", problems)
	}
}

func TestAnUnreadableFileIsReportedNotFatal(t *testing.T) {
	d := tree(t, someNotes)
	if err := os.Remove(filepath.Join(d, "docs/INSTALL.md")); err != nil {
		t.Fatal(err)
	}
	problems, err := Check(d, "")
	if err != nil || len(problems) != 1 || !strings.Contains(problems[0], "docs/INSTALL.md: cannot read") {
		t.Errorf("Check = %v, %v", problems, err)
	}
}

func TestMissingNotesSection(t *testing.T) {
	d := tree(t, someNotes)
	cur, _ := Current(d)
	writeFile(t, filepath.Join(d, "RELEASE_NOTES.md"), "# Release notes\n\n## 0.0.1 (2025-01-01)\n\n- first\n")
	problems, _ := Check(d, "")
	if len(problems) != 1 || !strings.Contains(problems[0], "no section for "+cur) {
		t.Errorf("Check = %v", problems)
	}
}

func TestNoVersionInTheSourceOfTruthStopsTheCheck(t *testing.T) {
	d := t.TempDir()
	if _, err := Check(d, ""); err == nil || !strings.Contains(err.Error(), "internal/buildinfo/buildinfo.go") {
		t.Errorf("err = %v", err)
	}
	writeFile(t, filepath.Join(d, "internal/buildinfo/buildinfo.go"), "package buildinfo\n")
	if _, err := Current(d); err == nil {
		t.Error("a buildinfo without a version must be an error")
	}
}

func TestBumpChangesEveryListedPlaceAndNothingElse(t *testing.T) {
	d := tree(t, someNotes)
	old, _ := Current(d)
	readme := filepath.Join(d, "README.md")
	writeFile(t, readme, readFile(t, readme)+"\nSee release "+old+" for details.\n")
	if got, err := Bump(d, "9.8.7"); err != nil || got != old {
		t.Fatalf("Bump = %q, %v", got, err)
	}
	if cur, _ := Current(d); cur != "9.8.7" {
		t.Errorf("Current = %q", cur)
	}
	if problems, _ := Check(d, ""); len(problems) > 0 {
		t.Errorf("after the bump: %v", problems)
	}
	if !strings.Contains(readFile(t, readme), "See release "+old+" for details.") {
		t.Error("a mention outside the version table must be left alone")
	}
	for _, f := range []string{"kernel/truesense-wmi/dkms.conf", "kernel/truesense-wmi/truesense_wmi.c", "packaging/arch/PKGBUILD", "packaging/aur/PKGBUILD.template", "tools/get.sh", "tools/aur-publish.sh", "internal/buildinfo/buildinfo.go"} {
		if !strings.Contains(readFile(t, filepath.Join(d, f)), "9.8.7") {
			t.Errorf("%s was not bumped", f)
		}
	}
}

func TestNotesOfAVersionAreCutOutWithTheInstallFooter(t *testing.T) {
	d := tree(t, someNotes)
	cur, _ := Current(d)
	n, err := Notes(d, cur)
	if err != nil {
		t.Fatal(err)
	}
	if want := "TrueSense " + cur + " for the Acer Predator Helios 16 (PH16-71): the old one."; !strings.HasPrefix(n, want) {
		t.Errorf("headline = %q", strings.SplitN(n, "\n", 2)[0])
	}
	for _, in := range []string{"old stuff", "get.sh | sudo bash", "SHA256SUMS", "SBOM"} {
		if !strings.Contains(n, in) {
			t.Errorf("notes lack %q", in)
		}
	}
	for _, out := range []string{"first", "New thing"} {
		if strings.Contains(n, out) {
			t.Errorf("notes of %s leak %q", cur, out)
		}
	}
	if strings.HasSuffix(n, "\n") {
		t.Error("the notes end with a newline: gh would add a blank line")
	}
	if _, err := Notes(d, "0.0.2"); err == nil {
		t.Error("notes of a version that has none must be an error")
	}
}

func TestTheNotesOfTheRealCurrentReleaseExistAndAreNotEmpty(t *testing.T) {
	root := repoRoot(t)
	cur, err := Current(root)
	if err != nil {
		t.Fatal(err)
	}
	if n, err := Notes(root, cur); err != nil || len(n) <= 200 {
		t.Errorf("Notes = %d bytes, %v", len(n), err)
	}
}

func TestSection(t *testing.T) {
	notes := "# T\n\n## Unreleased\n\nnext\n\n## 1.2.3 (2026-01-01): big\n\nline one\n\n```\n### not a section\n```\n\n## 1.2.2 (2025-12-01)\n\nolder\n\n## 1.2.30 (2025-11-01)\n\nthirty\n"
	for _, tc := range []struct {
		version, want string
		ok            bool
	}{
		{"1.2.3", "line one\n\n```\n### not a section\n```", true},
		{"1.2.2", "older", true},
		{"1.2.30", "thirty", true},
		{"1.2", "", false},
		{"9.9.9", "", false},
	} {
		got, ok := Section(notes, tc.version)
		if got != tc.want || ok != tc.ok {
			t.Errorf("Section(%s) = %q, %v; want %q, %v", tc.version, got, ok, tc.want, tc.ok)
		}
	}
	if got := Title(notes, "1.2.3"); got != "big" {
		t.Errorf("Title = %q", got)
	}
	if got := Title(notes, "1.2.2"); got != "" {
		t.Errorf("Title without one = %q", got)
	}
}

func TestParseVersion(t *testing.T) {
	for _, tc := range []struct {
		in   string
		want Version
		ok   bool
	}{
		{"1.2.3", Version{1, 2, 3}, true},
		{"v0.3.17", Version{0, 3, 17}, true},
		{"  v10.0.1\n", Version{10, 0, 1}, true},
		{"banana", Version{}, false},
		{"1.2", Version{}, false},
		{"1.2.3.4", Version{}, false},
		{"vv1.2.3", Version{}, false},
		{"1.2.3-rc1", Version{}, false},
		{"99999999999999999999.0.0", Version{}, false},
		{"", Version{}, false},
	} {
		got, err := ParseVersion(tc.in)
		if (err == nil) != tc.ok || got != tc.want {
			t.Errorf("ParseVersion(%q) = %v, %v", tc.in, got, err)
		}
	}
	a, b := Version{0, 3, 9}, Version{0, 3, 17}
	if a.Compare(b) != -1 || b.Compare(a) != 1 || a.Compare(a) != 0 {
		t.Error("versions must compare numerically (0.3.9 is older than 0.3.17)")
	}
}

// ---- prepare ----------------------------------------------------------------------------------------------------------------------------

func prepare(t *testing.T, d, version, title string, mutate func(*Options)) error {
	t.Helper()
	o := Options{Root: d, Version: version, Title: title, Today: time.Date(2026, 3, 4, 0, 0, 0, 0, time.UTC), Run: ExecRunner{}, Out: io.Discard}
	if mutate != nil {
		mutate(&o)
	}
	return Prepare(context.Background(), o)
}

func TestPrepareBumpsRetitlesCommitsAndTags(t *testing.T) {
	d := tree(t, someNotes)
	if err := prepare(t, d, "9.8.7", "shiny", nil); err != nil {
		t.Fatal(err)
	}
	if cur, _ := Current(d); cur != "9.8.7" {
		t.Errorf("Current = %q", cur)
	}
	if problems, err := Check(d, "v9.8.7"); err != nil || len(problems) > 0 {
		t.Errorf("Check = %v, %v", problems, err)
	}
	notes := readFile(t, filepath.Join(d, "RELEASE_NOTES.md"))
	if !strings.Contains(notes, "## 9.8.7 (2026-03-04): shiny") || strings.Contains(notes, "## Unreleased") {
		t.Errorf("notes not retitled:\n%s", notes)
	}
	if tags := gitIn(t, d, "tag"); tags != "v9.8.7" {
		t.Errorf("tags = %q", tags)
	}
	if subject := gitIn(t, d, "log", "-1", "--format=%s"); subject != "Release 9.8.7: shiny" {
		t.Errorf("commit subject = %q", subject)
	}
	if st := gitIn(t, d, "status", "--porcelain"); st != "" {
		t.Errorf("tree not clean after prepare: %q", st)
	}
}

func TestPrepareRefusesWhatWouldMakeABadRelease(t *testing.T) {
	d := tree(t, someNotes)
	cur, _ := Current(d)
	for _, tc := range []struct{ name, version, title, want string }{
		{"same version", cur, "t", "not newer"},
		{"older version", "0.0.1", "t", "not newer"},
		{"no title", "9.8.7", "", "title"},
		{"not a version", "banana", "t", "not a version"},
	} {
		if err := prepare(t, d, tc.version, tc.title, nil); err == nil || !strings.Contains(err.Error(), tc.want) {
			t.Errorf("%s: err = %v, want it to mention %q", tc.name, err, tc.want)
		}
	}
	writeFile(t, filepath.Join(d, "dirty.txt"), "x")
	if err := prepare(t, d, "9.8.7", "t", nil); err == nil || !strings.Contains(err.Error(), "uncommitted") {
		t.Errorf("dirty tree: err = %v", err)
	}
	if err := prepare(t, tree(t, ""), "9.8.7", "t", nil); err == nil || !strings.Contains(err.Error(), "Unreleased") {
		t.Errorf("empty notes: err = %v", err)
	}
	if err := prepare(t, t.TempDir(), "9.8.7", "t", nil); err == nil {
		t.Error("a directory that is not the repository must be refused")
	}
}

func TestAnExistingTagIsRefusedAndADryRunChangesNothing(t *testing.T) {
	d := tree(t, someNotes)
	gitIn(t, d, "tag", "v9.8.7")
	if err := prepare(t, d, "9.8.7", "t", nil); err == nil || !strings.Contains(err.Error(), "exists") {
		t.Errorf("err = %v", err)
	}
	e := tree(t, someNotes)
	before, _ := Current(e)
	var out bytes.Buffer
	if err := prepare(t, e, "9.8.8", "t", func(o *Options) { o.DryRun, o.Out = true, &out }); err != nil {
		t.Fatal(err)
	}
	if after, _ := Current(e); after != before {
		t.Errorf("a dry run changed the version: %s -> %s", before, after)
	}
	if !strings.Contains(out.String(), "dry run") || gitIn(t, e, "status", "--porcelain") != "" {
		t.Errorf("dry run output %q or tree dirty", out.String())
	}
}

// recorder is a Runner that remembers every command and answers from a script.
type recorder struct {
	calls    []string
	exitCode map[string]int
}

func (r *recorder) Run(_ context.Context, _ string, argv []string, _, _ io.Writer) (int, error) {
	cmd := strings.Join(argv, " ")
	r.calls = append(r.calls, cmd)
	if strings.HasPrefix(cmd, "git rev-parse") {
		return 1, nil // no such tag
	}
	return r.exitCode[cmd], nil
}

func TestThePrepareGateRunsBeforeTheCommitAndAFailureStopsEverything(t *testing.T) {
	d := tree(t, someNotes)
	rec := &recorder{}
	if err := prepare(t, d, "9.8.7", "t", func(o *Options) { o.Run, o.Gate, o.Push = rec, true, true }); err != nil {
		t.Fatal(err)
	}
	want := []string{
		"git status --porcelain", "git rev-parse -q --verify refs/tags/v9.8.7",
		"make go-ci", "bash tools/ci/check-shell.sh",
		"git add -A", "git commit -q -m Release 9.8.7: t", "git tag -a v9.8.7 -m TrueSense 9.8.7", "git push origin HEAD v9.8.7",
	}
	if strings.Join(rec.calls, "|") != strings.Join(want, "|") {
		t.Errorf("commands:\n got  %q\n want %q", rec.calls, want)
	}

	d = tree(t, someNotes)
	rec = &recorder{exitCode: map[string]int{"make go-ci": 2}}
	err := prepare(t, d, "9.8.7", "t", func(o *Options) { o.Run, o.Gate = rec, true })
	if err == nil || !strings.Contains(err.Error(), "failed: make go-ci") {
		t.Fatalf("err = %v", err)
	}
	for _, c := range rec.calls {
		if strings.HasPrefix(c, "git commit") || strings.HasPrefix(c, "git tag") || strings.HasPrefix(c, "git push") {
			t.Errorf("%q ran although the gate failed", c)
		}
	}

	rec = &recorder{}
	if err := prepare(t, tree(t, someNotes), "9.8.7", "t", func(o *Options) { o.Run, o.Gate = rec, false }); err != nil {
		t.Fatal(err)
	}
	for _, c := range rec.calls {
		if c == "make go-ci" {
			t.Error("--no-gate still ran the gate")
		}
	}
}

func TestAPushOnlyHappensWhenAsked(t *testing.T) {
	rec := &recorder{}
	if err := prepare(t, tree(t, someNotes), "9.8.7", "t", func(o *Options) { o.Run = rec }); err != nil {
		t.Fatal(err)
	}
	for _, c := range rec.calls {
		if strings.HasPrefix(c, "git push") {
			t.Errorf("pushed without --push: %q", c)
		}
	}
}

// ---- command line --------------------------------------------------------------------------------------------------------------------

func runMain(t *testing.T, args ...string) (code int, stdout, stderr string) {
	t.Helper()
	var out, errb bytes.Buffer
	code = Main(context.Background(), args, &out, &errb, &recorder{}, func() time.Time { return time.Date(2026, 3, 4, 0, 0, 0, 0, time.UTC) })
	return code, out.String(), errb.String()
}

func TestCommandLine(t *testing.T) {
	d := tree(t, someNotes)
	cur, _ := Current(d)

	if code, out, _ := runMain(t, "-C", d, "version"); code != 0 || out != cur+"\n" {
		t.Errorf("version: %d %q", code, out)
	}
	if code, out, _ := runMain(t, "-C", d, "check", "--tag", "v"+cur); code != 0 || !strings.Contains(out, "every file agrees (tag v"+cur+")") {
		t.Errorf("check: %d %q", code, out)
	}
	if code, _, errs := runMain(t, "-C", d, "check", "--tag", "v9.9.9"); code != 1 || !strings.Contains(errs, "version problems:") || !strings.Contains(errs, "does not match") {
		t.Errorf("check with a wrong tag: %d %q", code, errs)
	}
	if code, out, _ := runMain(t, "-C", d, "notes", "v"+cur); code != 0 || !strings.HasPrefix(out, "TrueSense "+cur) {
		t.Errorf("notes: %d %q", code, out[:min(len(out), 60)])
	}
	if code, _, errs := runMain(t, "-C", d, "notes", "0.0.2"); code != 1 || !strings.Contains(errs, "no notes for 0.0.2") {
		t.Errorf("notes of a missing version: %d %q", code, errs)
	}
	// flags after the positional argument, as in `prepare 0.3.16 --title "..."`
	code, out, errs := runMain(t, "-C", d, "prepare", "v9.8.7", "--title", "shiny", "--dry-run", "--no-gate")
	if code != 0 || !strings.Contains(out, "release "+cur+" -> 9.8.7: shiny") {
		t.Errorf("prepare --dry-run: %d %q %q", code, out, errs)
	}
	if code, _, errs := runMain(t, "-C", d, "prepare", "9.8.7", "--title=x", "--dry-run"); code != 0 {
		t.Errorf("--title=x: %d %q", code, errs)
	}
	for _, args := range [][]string{{}, {"-C", d}, {"frobnicate"}, {"-C", d, "prepare"}, {"-C", d, "check", "extra"}, {"-C", d, "prepare", "1.0.0", "--bogus"}} {
		if code, _, errs := runMain(t, args...); code != 2 || !strings.Contains(errs, "release prepare X.Y.Z") {
			t.Errorf("%v: code %d, usage missing in %q", args, code, errs)
		}
	}
	if code, out, _ := runMain(t, "--help"); code != 0 || !strings.Contains(out, "release check") {
		t.Errorf("--help: %d", code)
	}
	if code, _, errs := runMain(t, "-C", t.TempDir(), "version"); code != 1 || !strings.Contains(errs, "RELEASE_NOTES.md") {
		t.Errorf("outside a repository: %d %q", code, errs)
	}
}

func TestFindRootWalksUp(t *testing.T) {
	d := tree(t, someNotes)
	sub := filepath.Join(d, "a", "b")
	if err := os.MkdirAll(sub, 0o755); err != nil {
		t.Fatal(err)
	}
	got, err := FindRoot(sub)
	if err != nil || got != d {
		t.Errorf("FindRoot = %q, %v; want %q", got, err, d)
	}
}

// ---- fuzzing: the notes file is edited by hand, so the cutters must survive anything ---------------------------------------------------

func FuzzParseVersion(f *testing.F) {
	for _, s := range []string{"1.2.3", "v0.3.17", "", "banana", "1.2.3.4", " 1.2.3 ", "99999999999999999999.1.1", "\xff.1.2"} {
		f.Add(s)
	}
	f.Fuzz(func(t *testing.T, s string) {
		v, err := ParseVersion(s)
		if err == nil && v.Compare(v) != 0 {
			t.Fatalf("%q parsed to %v that differs from itself", s, v)
		}
	})
}

func FuzzSection(f *testing.F) {
	f.Add("## Unreleased\n\nx\n## 1.2.3 (2026-01-01): t\n\nbody\n", "1.2.3")
	f.Add("## 1.2.3 (", "1.2.3")
	f.Add("", "")
	f.Add("## (\n## ()\n", "")
	f.Fuzz(func(t *testing.T, notes, version string) {
		body, ok := Section(notes, version)
		if !ok && body != "" {
			t.Fatalf("no section but a body: %q", body)
		}
		if ok && strings.HasPrefix(body, "## ") {
			t.Fatalf("a section's body starts with the next heading: %q", body)
		}
		_ = Title(notes, version)
	})
}
