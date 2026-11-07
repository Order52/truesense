// Package app is the entry point behind cmd/truesense: it picks what to run from the
// program name (truesense, truesense-fast) and the first argument.
package app

import (
	"fmt"
	"io"

	"github.com/Order52/truesense/internal/buildinfo"
)

// Run executes the program with the given arguments (args[0] is the program name) and
// returns the process exit code.
func Run(args []string, stdout, stderr io.Writer) int {
	if len(args) > 1 && (args[1] == "--version" || args[1] == "version") {
		fmt.Fprintf(stdout, "truesense %s\n", buildinfo.Version)
		return 0
	}
	fmt.Fprintln(stderr, "truesense: the Go port is under construction")
	return 2
}
