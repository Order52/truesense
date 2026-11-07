// Package command is a small command-tree framework for the truesense command line: nested
// commands with flags, help generated from the tree, a global --json switch, "did you mean"
// suggestions, exit codes, and shell completions generated from the same tree (so they can
// never go stale). It has no knowledge of TrueSense: the areas and verbs live in package cli.
package command

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"sort"
	"strconv"
	"strings"
)

// Exit codes.
const (
	ExitOK    = 0
	ExitError = 1 // the command ran and failed
	ExitUsage = 2 // the command line was wrong
)

// Cmd is one node of the tree: a command that runs, a group of subcommands, or both.
type Cmd struct {
	Name    string
	Short   string // one line, shown in the parent's help
	Long    string // optional longer description
	Usage   string // positional arguments, e.g. "NAME [VALUE]"
	Example string
	Flags   func(fs *flag.FlagSet) // declare this command's flags
	Run     func(c *Ctx) error
	Sub     []*Cmd
	Hidden  bool // not listed in help or completions (for example the daemon, started by systemd)
	// Complete are the words the command's positional arguments take (modes, on/off/toggle, ...),
	// offered by the shell completions next to its flags.
	Complete []string
}

// Ctx is what a running command sees.
type Ctx struct {
	Context context.Context
	Out     io.Writer
	Err     io.Writer
	JSON    bool          // the global --json switch
	Args    []string      // positional arguments left after the flags were parsed
	Flags   *flag.FlagSet // this command's flags (already parsed)
}

// UsageError reports a wrong command line (exit code 2). The message is shown with the usage.
type UsageError struct{ Msg string }

func (e *UsageError) Error() string { return e.Msg }

// ExitCode is an error that only carries the process exit code: the command has already said
// everything it had to say (a failed repair, a health check with failures). Nothing more is printed.
type ExitCode int

func (e ExitCode) Error() string { return "exit code " + strconv.Itoa(int(e)) }

// Usagef builds a UsageError.
func Usagef(format string, args ...any) error { return &UsageError{Msg: fmt.Sprintf(format, args...)} }

// Printf writes to the command's standard output.
func (c *Ctx) Printf(format string, args ...any) { _, _ = fmt.Fprintf(c.Out, format, args...) }

// Println writes a line to the command's standard output.
func (c *Ctx) Println(args ...any) { _, _ = fmt.Fprintln(c.Out, args...) }

// Emit prints a result: JSON for scripts (--json); otherwise the human text when given, else
// a bare value ("on"/"off" for booleans) or indented JSON for structures.
func (c *Ctx) Emit(value any, human string) {
	switch {
	case c.JSON:
		c.writeJSON(value)
	case human != "":
		c.Println(human)
	default:
		switch v := value.(type) {
		case nil:
		case bool:
			if v {
				c.Println("on")
			} else {
				c.Println("off")
			}
		case string:
			c.Println(v)
		case int, int64, float64:
			c.Println(v)
		default:
			c.writeJSON(value)
		}
	}
}

// writeJSON prints v as indented JSON. Text is not HTML-escaped: "&" stays "&", which keeps
// descriptions readable.
func (c *Ctx) writeJSON(v any) {
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false)
	enc.SetIndent("", "  ")
	if err := enc.Encode(v); err != nil {
		buf.Reset()
		_ = enc.Encode(map[string]string{"error": err.Error()})
	}
	_, _ = c.Out.Write(buf.Bytes())
}

// Arg returns positional argument i, or "" when absent.
func (c *Ctx) Arg(i int) string {
	if i < len(c.Args) {
		return c.Args[i]
	}
	return ""
}

// Want checks the number of positional arguments the way argparse does: too few names the
// missing ones (names are the argument names, in order), too many lists the extra ones.
func (c *Ctx) Want(minArgs, maxArgs int, names ...string) error {
	if len(c.Args) < minArgs {
		missing := names
		if len(names) > len(c.Args) {
			missing = names[len(c.Args):minArgs]
		}
		return Usagef("the following arguments are required: %s", strings.Join(missing, ", "))
	}
	if len(c.Args) > maxArgs {
		return Usagef("unrecognized arguments: %s", strings.Join(c.Args[maxArgs:], " "))
	}
	return nil
}

// Choice returns positional argument i checked against the allowed values; an absent argument
// is def (which may be "" for an optional argument that has no default).
func (c *Ctx) Choice(i int, name, def string, choices ...string) (string, error) {
	if i >= len(c.Args) {
		return def, nil
	}
	return OneOf(name, c.Args[i], choices...)
}

// OneOf checks a value against the allowed ones, with argparse's wording.
func OneOf(name, value string, choices ...string) (string, error) {
	for _, ch := range choices {
		if value == ch {
			return value, nil
		}
	}
	quoted := make([]string, len(choices))
	for i, ch := range choices {
		quoted[i] = "'" + ch + "'"
	}
	return "", Usagef("argument %s: invalid choice: '%s' (choose from %s)", name, value, strings.Join(quoted, ", "))
}

// Root is the program.
type Root struct {
	Name    string
	Version string
	Short   string
	Example string
	Cmds    []*Cmd
}

// Execute runs the program with args (without the program name) and returns the exit code.
func (r *Root) Execute(ctx context.Context, args []string, out, errw io.Writer) int {
	asJSON := false
	var rest []string
	for i, a := range args {
		switch a {
		case "--":
			rest = append(rest, args[i:]...)
			goto done
		case "--json", "-json":
			asJSON = true
		default:
			rest = append(rest, a)
		}
	}
done:
	if len(rest) > 0 && (rest[0] == "-V" || rest[0] == "--version") {
		_, _ = fmt.Fprintf(out, "%s %s\n", r.Name, r.Version)
		return ExitOK
	}
	if len(rest) == 0 || rest[0] == "-h" || rest[0] == "--help" || rest[0] == "help" {
		r.rootHelp(out)
		if len(rest) == 0 {
			return ExitUsage
		}
		return ExitOK
	}
	cmd, path, left, err := r.resolve(rest)
	if err == nil {
		err = r.run(ctx, cmd, path, left, asJSON, out, errw)
	}
	return r.finish(err, path, cmd, out, errw)
}

func (r *Root) finish(err error, path []string, cmd *Cmd, out, errw io.Writer) int {
	if err == nil {
		return ExitOK
	}
	var code ExitCode
	if errors.As(err, &code) {
		return int(code)
	}
	var ue *UsageError
	if errors.As(err, &ue) {
		_, _ = fmt.Fprintf(errw, "%s: %s\n", r.Name, ue.Msg)
		if cmd != nil {
			_, _ = fmt.Fprintf(errw, "usage: %s\n", usageLine(r.Name, path, cmd))
		} else {
			_, _ = fmt.Fprintf(errw, "run '%s --help' for the list of commands\n", r.Name)
		}
		return ExitUsage
	}
	if errors.Is(err, flag.ErrHelp) {
		if cmd != nil {
			help(out, r.Name, path, cmd)
		}
		return ExitOK
	}
	_, _ = fmt.Fprintf(errw, "error: %v\n", err) // the 0.3.x wording: scripts and users grep for it
	return ExitError
}

// resolve walks the tree as far as the words name commands.
func (r *Root) resolve(args []string) (cmd *Cmd, path, left []string, err error) {
	level := r.Cmds
	for len(args) > 0 {
		next := find(level, args[0])
		if next == nil {
			if cmd == nil {
				return nil, nil, nil, &UsageError{Msg: unknown(args[0], level)}
			}
			break
		}
		cmd = next
		path = append(path, next.Name)
		args = args[1:]
		level = next.Sub
		if len(level) == 0 {
			break
		}
	}
	return cmd, path, args, nil
}

func find(level []*Cmd, name string) *Cmd {
	for _, c := range level {
		if c.Name == name {
			return c
		}
	}
	return nil
}

func (r *Root) run(ctx context.Context, cmd *Cmd, path, args []string, asJSON bool, out, errw io.Writer) error {
	fs := flag.NewFlagSet(strings.Join(append([]string{r.Name}, path...), " "), flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	if cmd.Flags != nil {
		cmd.Flags(fs)
	}
	pos, err := parseInterspersed(fs, args)
	if err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return err
		}
		return &UsageError{Msg: err.Error()}
	}
	if len(pos) > 0 && len(cmd.Sub) > 0 { // resolve consumed every word that named a subcommand
		return &UsageError{Msg: unknown(pos[0], cmd.Sub)}
	}
	if cmd.Run == nil {
		help(out, r.Name, path, cmd)
		return nil
	}
	return cmd.Run(&Ctx{Context: ctx, Out: out, Err: errw, JSON: asJSON, Args: pos, Flags: fs})
}

// parseInterspersed parses flags that may come before, between or after positional arguments
// (the way argparse does), so "rear set wave --color 00ff88" works. "--" ends the flags. A
// negative number is a value, not a flag ("cpu undervolt set -10"), as in argparse while no
// option looks like a number.
func parseInterspersed(fs *flag.FlagSet, args []string) ([]string, error) {
	args = maskNegatives(fs, args)
	var pos []string
	for {
		if err := fs.Parse(args); err != nil {
			return nil, err
		}
		args = fs.Args()
		if len(args) == 0 {
			return unmask(pos), nil
		}
		if args[0] == "--" {
			return unmask(append(pos, args[1:]...)), nil
		}
		pos = append(pos, args[0])
		args = args[1:]
	}
}

// negMark prefixes a negative number that stands for a positional argument while the flag
// package parses the line (it would take "-10" for a flag).
const negMark = "\x00"

func isNegativeNumber(s string) bool {
	if len(s) < 2 || s[0] != '-' {
		return false
	}
	digits, dot := 0, false
	for _, c := range s[1:] {
		switch {
		case c >= '0' && c <= '9':
			digits++
		case c == '.' && !dot:
			dot = true
		default:
			return false
		}
	}
	return digits > 0
}

// maskNegatives marks the negative numbers that are positional: every one except the value
// of a flag that takes one ("--core -50").
func maskNegatives(fs *flag.FlagSet, args []string) []string {
	out := make([]string, len(args))
	copy(out, args)
	for i := 0; i < len(out); i++ {
		a := out[i]
		switch {
		case a == "--":
			return out
		case isNegativeNumber(a):
			out[i] = negMark + a
		case strings.HasPrefix(a, "-") && len(a) > 1 && !strings.Contains(a, "="):
			if f := fs.Lookup(strings.TrimLeft(a, "-")); f != nil && !isBoolFlag(f) {
				i++ // the next word is this flag's value, whatever it looks like
			}
		}
	}
	return out
}

func isBoolFlag(f *flag.Flag) bool {
	b, ok := f.Value.(interface{ IsBoolFlag() bool })
	return ok && b.IsBoolFlag()
}

func unmask(pos []string) []string {
	for i, p := range pos {
		pos[i] = strings.TrimPrefix(p, negMark)
	}
	return pos
}

// ---- help ----

func usageLine(root string, path []string, c *Cmd) string {
	parts := append([]string{root}, path...)
	if len(c.Sub) > 0 && c.Run == nil {
		parts = append(parts, "<command>")
	}
	if c.Usage != "" {
		parts = append(parts, c.Usage)
	}
	return strings.Join(parts, " ")
}

func (r *Root) rootHelp(w io.Writer) {
	_, _ = fmt.Fprintf(w, "usage: %s [--json] <area> [<verb>] [arguments]\n\n%s\n\nareas:\n", r.Name, r.Short)
	writeList(w, r.Cmds)
	_, _ = fmt.Fprintf(w, "\noptions:\n  -h, --help     show this help\n  --json         machine-readable output\n  -V, --version  show the version\n")
	if r.Example != "" {
		_, _ = fmt.Fprintf(w, "\nexamples:\n%s\n", r.Example)
	}
}

func help(w io.Writer, root string, path []string, c *Cmd) {
	_, _ = fmt.Fprintf(w, "usage: %s\n", usageLine(root, path, c))
	if c.Short != "" {
		_, _ = fmt.Fprintf(w, "\n%s\n", c.Short)
	}
	if c.Long != "" {
		_, _ = fmt.Fprintf(w, "\n%s\n", c.Long)
	}
	if len(c.Sub) > 0 {
		_, _ = fmt.Fprintf(w, "\ncommands:\n")
		writeList(w, c.Sub)
	}
	if c.Flags != nil {
		fs := flag.NewFlagSet("", flag.ContinueOnError)
		fs.SetOutput(io.Discard)
		c.Flags(fs)
		var lines []string
		fs.VisitAll(func(f *flag.Flag) {
			def := ""
			if f.DefValue != "" && f.DefValue != "false" {
				def = fmt.Sprintf(" (default %s)", f.DefValue)
			}
			lines = append(lines, fmt.Sprintf("  --%-14s %s%s", f.Name, f.Usage, def))
		})
		if len(lines) > 0 {
			_, _ = fmt.Fprintf(w, "\noptions:\n%s\n", strings.Join(lines, "\n"))
		}
	}
	if c.Example != "" {
		_, _ = fmt.Fprintf(w, "\nexamples:\n%s\n", c.Example)
	}
}

func writeList(w io.Writer, cmds []*Cmd) {
	width := 0
	for _, c := range cmds {
		if !c.Hidden && len(c.Name) > width {
			width = len(c.Name)
		}
	}
	for _, c := range cmds {
		if !c.Hidden {
			_, _ = fmt.Fprintf(w, "  %-*s  %s\n", width, c.Name, c.Short)
		}
	}
}

// unknown builds the "no such command" message with the closest name, if one is close.
func unknown(word string, level []*Cmd) string {
	best, bestD := "", 3
	for _, c := range level {
		if c.Hidden {
			continue
		}
		d := distance(word, c.Name)
		if strings.HasPrefix(c.Name, word) { // "stat" means "status" before it means "set"
			d = 0
		}
		if d < bestD || (d == bestD && best != "" && len(c.Name) < len(best)) {
			best, bestD = c.Name, d
		}
	}
	if best != "" {
		return fmt.Sprintf("unknown command %q (did you mean %q?)", word, best)
	}
	return fmt.Sprintf("unknown command %q", word)
}

// distance is the Levenshtein distance between two short words.
func distance(a, b string) int {
	prev := make([]int, len(b)+1)
	for j := range prev {
		prev[j] = j
	}
	for i := 1; i <= len(a); i++ {
		cur := make([]int, len(b)+1)
		cur[0] = i
		for j := 1; j <= len(b); j++ {
			cost := 1
			if a[i-1] == b[j-1] {
				cost = 0
			}
			cur[j] = min(prev[j]+1, cur[j-1]+1, prev[j-1]+cost)
		}
		prev = cur
	}
	return prev[len(b)]
}

// visible returns the non-hidden commands of a level, sorted as declared.
func visible(level []*Cmd) []*Cmd {
	var out []*Cmd
	for _, c := range level {
		if !c.Hidden {
			out = append(out, c)
		}
	}
	return out
}

// flagNames lists a command's long flags.
func flagNames(c *Cmd) []string {
	if c.Flags == nil {
		return nil
	}
	fs := flag.NewFlagSet("", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	c.Flags(fs)
	var out []string
	fs.VisitAll(func(f *flag.Flag) { out = append(out, "--"+f.Name) })
	sort.Strings(out)
	return out
}
