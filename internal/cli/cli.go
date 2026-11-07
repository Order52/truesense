// Package cli is the truesense command line: the areas (power, fan, rear, ...) and their verbs,
// exactly as the 0.3.x tool had them, wired to the application layer in internal/api. A command
// here only parses its arguments, calls one api method and prints the answer, as text for a
// person or as JSON (--json) for a script; what an operation does is decided in api.
package cli

import (
	"context"
	"flag"
	"fmt"
	"io"
	"strconv"
	"strings"

	"github.com/Order52/truesense/internal/api"
	"github.com/Order52/truesense/internal/buildinfo"
	"github.com/Order52/truesense/internal/cli/command"
)

// Hooks are the commands only the application can run: they need a terminal, a window or
// services that sit above this package.
type Hooks struct {
	// TUI runs the terminal UI, opened on page ("" for the dashboard).
	TUI func(ctx context.Context, page string) error
	// GUI runs the window, opened on page ("" for home).
	GUI func(ctx context.Context, page string) error
}

// CLI is the command tree over one API.
type CLI struct {
	api   *api.API
	hooks Hooks
	stdin io.Reader
	root  *command.Root
}

// New builds the command tree. stdin answers the one question the command line asks (the
// confirmation of `update apply`).
func New(a *api.API, hooks Hooks, stdin io.Reader) *command.Root {
	c := &CLI{api: a, hooks: hooks, stdin: stdin}
	c.root = &command.Root{
		Name:    "truesense",
		Version: buildinfo.Version,
		Short:   "truesense: command line for the Acer Predator Helios 16 (PH16-71).   truesense <area> <verb> ...   (every setting has on / off / toggle / status; --json for scripts)",
		Example: "  truesense status\n  truesense power set turbo\n  truesense fan set 60\n  truesense rear set breathing --color 00ff88\n  truesense keyboard effect wave --brightness 50\n  truesense keys set double output-toggle\n  truesense profile apply gaming\n  truesense doctor",
		Cmds:    c.areas(),
	}
	return c.root
}

// areas lists the areas in the order the help shows them.
func (c *CLI) areas() []*command.Cmd {
	list := []*command.Cmd{
		c.status(), c.sensors(), c.power(), c.fan(), c.battery(), c.display(), c.boot(), c.gpu(), c.rear(), c.update(), c.look(),
		c.startup(), c.keyboard(), c.audio(), c.keys(), c.profile(), c.cpu(), c.touchpad(), c.service(), c.config(), c.doctor(),
		c.selftest(), c.completion(), c.gui(), c.probe(), c.tui(), c.daemon(), c.version(),
	}
	c.addWindowVerbs(list)
	applyCompletions(list, "", completeWords())
	return list
}

// ---- argument and flag helpers ----

// the on / off / toggle / status words every boolean setting takes.
var triWords = []string{"on", "off", "toggle", "status"}

func onoff(v *bool) string {
	switch {
	case v == nil:
		return "unknown"
	case *v:
		return "on"
	}
	return "off"
}

func optional(get func() (bool, error)) func() (*bool, error) {
	return func() (*bool, error) {
		v, err := get()
		if err != nil {
			return nil, err
		}
		return &v, nil
	}
}

// tri is the on / off / toggle / status pattern shared by every boolean setting: the first
// word of the command's arguments is the action ("status" when absent).
func tri(c *command.Ctx, label string, words []string, get func() (*bool, error), set func(bool) (bool, error)) error {
	if err := c.Want(0, 1, "action"); err != nil {
		return err
	}
	if words == nil {
		words = triWords
	}
	action, err := c.Choice(0, "action", "status", words...)
	if err != nil {
		return err
	}
	cur, err := get()
	if err != nil {
		return err
	}
	next := cur
	switch action {
	case "status":
	case "toggle":
		on, err := set(cur == nil || !*cur)
		if err != nil {
			return err
		}
		next = &on
	default:
		on, err := set(action == "on")
		if err != nil {
			return err
		}
		next = &on
	}
	c.Emit(next, label+": "+onoff(next))
	return nil
}

// optIntValue is an integer flag that tells "not given" from 0.
type optIntValue struct {
	v   int
	set bool
}

func (o *optIntValue) String() string {
	if o == nil || !o.set {
		return ""
	}
	return strconv.Itoa(o.v)
}
func (o *optIntValue) Set(s string) error {
	n, err := strconv.Atoi(s)
	if err != nil {
		return fmt.Errorf("parse error")
	}
	o.v, o.set = n, true
	return nil
}
func (o *optIntValue) Get() any {
	if !o.set {
		return (*int)(nil)
	}
	v := o.v
	return &v
}

// optFloatValue is a number flag that tells "not given" from 0.
type optFloatValue struct {
	v   float64
	set bool
}

func (o *optFloatValue) String() string {
	if o == nil || !o.set {
		return ""
	}
	return strconv.FormatFloat(o.v, 'g', -1, 64)
}
func (o *optFloatValue) Set(s string) error {
	n, err := strconv.ParseFloat(s, 64)
	if err != nil {
		return fmt.Errorf("parse error")
	}
	o.v, o.set = n, true
	return nil
}
func (o *optFloatValue) Get() any {
	if !o.set {
		return (*float64)(nil)
	}
	v := o.v
	return &v
}

func declOptInt(fs *flag.FlagSet, name, usage string)   { fs.Var(&optIntValue{}, name, usage) }
func declOptFloat(fs *flag.FlagSet, name, usage string) { fs.Var(&optFloatValue{}, name, usage) }

func flagValue(c *command.Ctx, name string) any {
	f := c.Flags.Lookup(name)
	if f == nil {
		panic("cli: flag --" + name + " is not declared") // a programming error, caught by every test of the command
	}
	return f.Value.(flag.Getter).Get()
}

func flagString(c *command.Ctx, name string) string { return flagValue(c, name).(string) }
func flagBool(c *command.Ctx, name string) bool     { return flagValue(c, name).(bool) }
func flagInt(c *command.Ctx, name string) int       { return flagValue(c, name).(int) }
func flagOptInt(c *command.Ctx, name string) *int   { return flagValue(c, name).(*int) }
func flagOptFloat(c *command.Ctx, name string) *float64 {
	return flagValue(c, name).(*float64)
}

// flagGiven reports whether the user set a flag.
func flagGiven(c *command.Ctx, name string) bool {
	given := false
	c.Flags.Visit(func(f *flag.Flag) { given = given || f.Name == name })
	return given
}

// intArg parses a positional integer.
func intArg(name, s string) (int, error) {
	n, err := strconv.Atoi(s)
	if err != nil {
		return 0, command.Usagef("argument %s: invalid int value: '%s'", name, s)
	}
	return n, nil
}

// floatArg parses a positional number.
func floatArg(name, s string) (float64, error) {
	n, err := strconv.ParseFloat(s, 64)
	if err != nil {
		return 0, command.Usagef("argument %s: invalid float value: '%s'", name, s)
	}
	return n, nil
}

// oneOfFlag checks a flag's value against the allowed ones.
func oneOfFlag(c *command.Ctx, name string, choices ...string) error {
	if !flagGiven(c, name) {
		return nil
	}
	_, err := command.OneOf("--"+name, fmt.Sprint(flagValue(c, name)), choices...)
	return err
}

// join renders a list one entry per line.
func join(lines []string) string { return strings.Join(lines, "\n") }

// pyBool is how the 0.3.x tool printed a boolean inside a line of text.
func pyBool(b bool) string {
	if b {
		return "True"
	}
	return "False"
}

func fmtf(format string, args ...any) string { return fmt.Sprintf(format, args...) }

// stringsValue is a flag that may be repeated (--set a=1 --set b=2).
type stringsValue []string

func (s *stringsValue) String() string { return strings.Join(*s, ",") }
func (s *stringsValue) Set(v string) error {
	*s = append(*s, v)
	return nil
}
func (s *stringsValue) Get() any { return []string(*s) }

func declStrings(fs *flag.FlagSet, name, usage string) { fs.Var(&stringsValue{}, name, usage) }
func flagStrings(c *command.Ctx, name string) []string { return flagValue(c, name).([]string) }

// pyValue prints a setting the way the 0.3.x tool printed it inside a line of text.
func pyValue(v any) string {
	switch x := v.(type) {
	case bool:
		return pyBool(x)
	case float64:
		return strconv.FormatFloat(x, 'f', -1, 64)
	case nil:
		return "None"
	}
	return fmt.Sprint(v)
}

func stringFlag(name, usage string) func(fs *flag.FlagSet) {
	return func(fs *flag.FlagSet) { fs.String(name, "", usage) }
}
