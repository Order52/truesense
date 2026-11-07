// Command truesense is the whole of TrueSense in one binary: the daemon, the command line,
// the terminal UI, the window and the user services. See internal/app.
package main

import (
	"os"
	"runtime"
	"runtime/debug"

	"github.com/Order52/truesense/internal/app"
)

func main() {
	tune()
	os.Exit(app.Run(os.Args, os.Stdout, os.Stderr))
}

// tune sizes the Go runtime for what this program is: a small daemon, a command line and two
// user services, which do almost no parallel work. The defaults suit big servers: one worker
// per core (24 here) and a heap allowed to double before it is collected. Two workers and a
// quarter of headroom cut the private memory of each process by a third. Anything the user set
// in the environment (GOMAXPROCS, GOGC) wins.
func tune() {
	if os.Getenv("GOMAXPROCS") == "" {
		runtime.GOMAXPROCS(min(2, runtime.NumCPU()))
	}
	if os.Getenv("GOGC") == "" {
		debug.SetGCPercent(25)
	}
}
