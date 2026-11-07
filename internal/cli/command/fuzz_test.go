package command

import (
	"flag"
	"strings"
	"testing"
)

// FuzzMaskNegatives checks the negative-number rule never loses or invents an argument.
func FuzzMaskNegatives(f *testing.F) {
	for _, seed := range []string{"-10", "--core -50 x", "x -2.5 --confirm", "-", "--", "-1 -- -2", "--core=-5"} {
		f.Add(seed)
	}
	f.Fuzz(func(t *testing.T, line string) {
		if strings.ContainsRune(line, 0) {
			t.Skip("a NUL cannot be in a command line")
		}
		fs := flag.NewFlagSet("t", flag.ContinueOnError)
		fs.Int("core", 0, "")
		fs.Bool("confirm", false, "")
		args := strings.Fields(line)
		masked := maskNegatives(fs, args)
		if len(masked) != len(args) {
			t.Fatalf("%d arguments became %d", len(args), len(masked))
		}
		for i := range args {
			if strings.TrimPrefix(masked[i], negMark) != args[i] {
				t.Fatalf("%q became %q", args[i], masked[i])
			}
		}
	})
}
