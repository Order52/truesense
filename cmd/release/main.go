// Command release is the release tooling: check that every file agrees on one version, print the
// release notes of a version, and prepare a release (bump, gate, commit, tag). See internal/release.
package main

import (
	"context"
	"os"
	"os/signal"
	"time"

	"github.com/Order52/truesense/internal/release"
)

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	code := release.Main(ctx, os.Args[1:], os.Stdout, os.Stderr, release.ExecRunner{}, time.Now)
	stop()
	os.Exit(code)
}
