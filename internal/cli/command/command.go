// Package command is a small command-tree framework for the truesense command line: nested
// commands with flags, help generated from the tree, a global --json switch, "did you mean"
// suggestions, exit codes, and shell completions generated from the same tree (so they can
// never go stale). It has no knowledge of TrueSense: the areas and verbs live in package cli.
package command

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"sort"
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

func (c *Ctx) writeJSON(v any) {
	b, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		b, _ = json.Marshal(map[string]string{"error": err.Error()})
	}
	_, _ = c.Out.Write(append(b, '\n'))
}

// Arg returns positional argument i, or "" when absent.
func (c *Ctx) Arg(i int) string {
	if i < len(c.Args) {
		return c.Args[i]
	}
	return ""
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
	_, _ = fmt.Fprintf(errw, "%s: %v\n", r.Name, err)
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
	if cmd.Run == nil {
		if len(pos) > 0 && len(cmd.Sub) > 0 {
			return &UsageError{Msg: unknown(pos[0], cmd.Sub)}
		}
		help(out, r.Name, path, cmd)
		return nil
	}
	return cmd.Run(&Ctx{Context: ctx, Out: out, Err: errw, JSON: asJSON, Args: pos, Flags: fs})
}

// parseInterspersed parses flags that may come before, between or after positional arguments
// (the way argparse does), so "rear set wave --color 00ff88" works. "--" ends the flags.
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
		if args[0] == "--" {
			return append(pos, args[1:]...), nil
		}
		pos = append(pos, args[0])
		args = args[1:]
	}
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
