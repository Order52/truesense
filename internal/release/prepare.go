package release

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"os/exec"
	"strings"
	"time"
)

// Runner runs an external program. It is the only way this package touches the machine besides the
// files under the root, so tests substitute a recorder for git and make.
type Runner interface {
	// Run executes argv in dir with the given output writers and returns the program's exit code;
	// err is only for a program that could not be started at all.
	Run(ctx context.Context, dir string, argv []string, stdout, stderr io.Writer) (exitCode int, err error)
}

// ExecRunner runs real programs.
type ExecRunner struct{}

// Run implements Runner with os/exec.
func (ExecRunner) Run(ctx context.Context, dir string, argv []string, stdout, stderr io.Writer) (int, error) {
	cmd := exec.CommandContext(ctx, argv[0], argv[1:]...)
	cmd.Dir, cmd.Stdout, cmd.Stderr = dir, stdout, stderr
	err := cmd.Run()
	var ee *exec.ExitError
	switch {
	case err == nil:
		return 0, nil
	case errors.As(err, &ee):
		return ee.ExitCode(), nil
	default:
		return -1, fmt.Errorf("run %s: %w", argv[0], err)
	}
}

// GateCommands is what `prepare` runs before it commits: the Go gate of the Makefile (gofmt, vet,
// staticcheck, golangci-lint, the race-detector tests, govulncheck) and the shell checks (bash -n and
// shellcheck on every script we ship). CI runs the same things again on the pushed tag.
func GateCommands() [][]string {
	return [][]string{
		{"make", "go-ci"},
		{"bash", "tools/ci/check-shell.sh"},
	}
}

// Options configures Prepare.
type Options struct {
	Root    string // the repository
	Version string // the new version, X.Y.Z
	Title   string // the short release title (required)
	Gate    bool   // run GateCommands before committing
	Push    bool   // push the commit and the tag (CI then publishes)
	DryRun  bool   // only say what would happen
	Today   time.Time
	Run     Runner
	Out     io.Writer
}

// Prepare bumps every version, retitles the notes, runs the gate, commits and tags. It refuses a
// dirty tree, a version that is not newer, an existing tag, empty "Unreleased" notes and a missing
// title, and it pushes nothing unless asked to.
func Prepare(ctx context.Context, o Options) error {
	old, err := Current(o.Root)
	if err != nil {
		return err
	}
	nv, err := ParseVersion(o.Version)
	if err != nil {
		return err
	}
	ov, err := ParseVersion(old)
	if err != nil {
		return err
	}
	if nv.Compare(ov) <= 0 {
		return fmt.Errorf("%s is not newer than %s", o.Version, old)
	}
	pending, err := Unreleased(o.Root)
	if err != nil {
		return err
	}
	if pending == "" {
		return errors.New(`RELEASE_NOTES.md has no notes under "## Unreleased": write what changed first`)
	}
	if o.Title == "" {
		return errors.New(`give the release a short title: --title "..."`)
	}
	var status bytes.Buffer
	if code, err := o.Run.Run(ctx, o.Root, []string{"git", "status", "--porcelain"}, &status, io.Discard); err != nil || code != 0 {
		return fmt.Errorf("git status failed (is %s a git repository?): %w", o.Root, errOr(err, code))
	}
	if strings.TrimSpace(status.String()) != "" {
		return errors.New("the working tree has uncommitted changes: commit or stash them first")
	}
	tag := "v" + o.Version
	if code, err := o.Run.Run(ctx, o.Root, []string{"git", "rev-parse", "-q", "--verify", "refs/tags/" + tag}, io.Discard, io.Discard); err != nil {
		return err
	} else if code == 0 {
		return fmt.Errorf("tag %s already exists", tag)
	}
	say := func(format string, a ...any) { _, _ = fmt.Fprintf(o.Out, format+"\n", a...) }
	say("release %s -> %s: %s", old, o.Version, o.Title)
	if o.DryRun {
		say("(dry run: nothing changed)")
		return nil
	}
	if _, err := Bump(o.Root, o.Version); err != nil {
		return err
	}
	if err := Retitle(o.Root, o.Version, o.Title, o.Today.Format("2006-01-02")); err != nil {
		return err
	}
	problems, err := Check(o.Root, "")
	if err != nil {
		return err
	}
	if len(problems) > 0 {
		return fmt.Errorf("after bumping:\n  %s", strings.Join(problems, "\n  "))
	}
	step := func(argv ...string) error {
		say("  $ %s", strings.Join(argv, " "))
		code, err := o.Run.Run(ctx, o.Root, argv, o.Out, o.Out)
		if err != nil {
			return err
		}
		if code != 0 {
			return fmt.Errorf("failed: %s", strings.Join(argv, " "))
		}
		return nil
	}
	if o.Gate {
		for _, c := range GateCommands() {
			if err := step(c...); err != nil {
				return err
			}
		}
	}
	for _, c := range [][]string{
		{"git", "add", "-A"},
		{"git", "commit", "-q", "-m", fmt.Sprintf("Release %s: %s", o.Version, o.Title)},
		{"git", "tag", "-a", tag, "-m", "TrueSense " + o.Version},
	} {
		if err := step(c...); err != nil {
			return err
		}
	}
	if !o.Push {
		say("committed and tagged %s. Publish with:  git push origin HEAD %s", tag, tag)
		return nil
	}
	if err := step("git", "push", "origin", "HEAD", tag); err != nil {
		return err
	}
	say("pushed: CI publishes https://github.com/Order52/truesense/releases/tag/%s", tag)
	return nil
}

func errOr(err error, code int) error {
	if err != nil {
		return err
	}
	return fmt.Errorf("exit status %d", code)
}
