// Command truesense is the whole of TrueSense in one binary: the daemon, the command line,
// the terminal UI, the window and the user services. See internal/app.
package main

import (
	"os"

	"github.com/Order52/truesense/internal/app"
)

func main() { os.Exit(app.Run(os.Args, os.Stdout, os.Stderr)) }
