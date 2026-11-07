package release

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"
)

const usage = `Release tooling: one place that knows where the version number lives.

  release check [--tag vX.Y.Z]     every file agrees on one version (and it matches the tag, when given); CI runs this on every push and on release
  release version                  the current version (what the pipeline builds when it rehearses from a branch)
  release notes X.Y.Z              the release notes of that version (from RELEASE_NOTES.md) as GitHub release text
  release prepare X.Y.Z --title T  bump everything, turn "## Unreleased" into this version, run the gate, commit and tag  (--push also pushes: CI then publishes)
                                   (--dry-run says what it would do, --no-gate skips the gate: CI runs it again anyway)

Run it from anywhere inside the repository (or give -C DIR): go run ./cmd/release check
prepare refuses a dirty tree, a version that is not newer, an existing tag, empty "Unreleased" notes or a missing title. Nothing is pushed unless you say --push.
`

// FindRoot returns the repository root: the nearest directory at or above start that holds RELEASE_NOTES.md.
func FindRoot(start string) (string, error) {
	dir, err := filepath.Abs(start)
	if err != nil {
		return "", err
	}
	for {
		if _, err := os.Stat(filepath.Join(dir, notesFile)); err == nil {
			return dir, nil
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return "", fmt.Errorf("no %s in %s or any directory above it: run from inside the repository or use -C DIR", notesFile, start)
		}
		dir = parent
	}
}

// parseInterspersed parses flags that may come before or after the positional arguments
// (`prepare 0.3.16 --title T`), which the standard flag package does not do by itself.
func parseInterspersed(fs *flag.FlagSet, args []string) ([]string, error) {
	var pos []string
	for {
		if err := fs.Parse(args); err != nil {
			return nil, err
		}
		args = fs.Args()
		if len(args) == 0 {
			return pos, nil
		}
		pos = append(pos, args[0])
		args = args[1:]
	}
}

// Main is the command line: it returns the process exit code (0 ok, 1 a failed check or refusal, 2 a usage error).
func Main(ctx context.Context, args []string, stdout, stderr io.Writer, run Runner, now func() time.Time) int {
	fail := func(err error) int {
		_, _ = fmt.Fprintln(stderr, err)
		return 1
	}
	usageErr := func(msg string) int {
		_, _ = fmt.Fprintf(stderr, "release: %s\n\n%s", msg, usage)
		return 2
	}
	dir := "."
	if len(args) >= 2 && args[0] == "-C" {
		dir, args = args[1], args[2:]
	}
	if len(args) == 0 {
		return usageErr("a command is required")
	}
	if args[0] == "-h" || args[0] == "--help" {
		_, _ = fmt.Fprint(stdout, usage)
		return 0
	}
	cmd := args[0]
	fs := flag.NewFlagSet(cmd, flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	var tag, title string
	var noGate, push, dry bool
	switch cmd {
	case "check":
		fs.StringVar(&tag, "tag", "", "")
	case "prepare":
		fs.StringVar(&title, "title", "", "")
		fs.BoolVar(&noGate, "no-gate", false, "")
		fs.BoolVar(&push, "push", false, "")
		fs.BoolVar(&dry, "dry-run", false, "")
	case "version", "notes":
	default:
		return usageErr(fmt.Sprintf("unknown command %q", cmd))
	}
	pos, err := parseInterspersed(fs, args[1:])
	if err != nil {
		if errors.Is(err, flag.ErrHelp) {
			_, _ = fmt.Fprint(stdout, usage)
			return 0
		}
		return usageErr(err.Error())
	}
	wantPos := map[string]int{"check": 0, "version": 0, "notes": 1, "prepare": 1}[cmd]
	if len(pos) != wantPos {
		return usageErr(fmt.Sprintf("%s takes %d argument(s), got %d", cmd, wantPos, len(pos)))
	}
	root, err := FindRoot(dir)
	if err != nil {
		return fail(err)
	}
	switch cmd {
	case "check":
		problems, err := Check(root, tag)
		if err != nil {
			return fail(err)
		}
		if len(problems) > 0 {
			_, _ = fmt.Fprintf(stderr, "version problems:\n  %s\n", strings.Join(problems, "\n  "))
			return 1
		}
		v, _ := Current(root)
		msg := fmt.Sprintf("version %s: every file agrees", v)
		if tag != "" {
			msg += fmt.Sprintf(" (tag %s)", tag)
		}
		_, _ = fmt.Fprintln(stdout, msg)
	case "version":
		v, err := Current(root)
		if err != nil {
			return fail(err)
		}
		_, _ = fmt.Fprintln(stdout, v)
	case "notes":
		n, err := Notes(root, strings.TrimLeft(pos[0], "v"))
		if err != nil {
			return fail(err)
		}
		_, _ = fmt.Fprintln(stdout, n)
	case "prepare":
		err := Prepare(ctx, Options{
			Root: root, Version: strings.TrimLeft(pos[0], "v"), Title: title,
			Gate: !noGate, Push: push, DryRun: dry, Today: now(), Run: run, Out: stdout,
		})
		if err != nil {
			return fail(err)
		}
	}
	return 0
}
