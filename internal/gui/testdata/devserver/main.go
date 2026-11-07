// Command devserver serves the window against a simulated laptop, for looking at the
// front-end in a browser or taking screenshots without hardware:
//
//	go run ./internal/gui/testdata/devserver [-no-daemon] [-update] [-ac=false]
//
// It prints the window URL (with its access token) and runs until interrupted.
package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"os"
	"os/signal"
	"time"

	"github.com/Order52/truesense/internal/gui"
	"github.com/Order52/truesense/internal/gui/guitest"
)

func main() {
	noDaemon := flag.Bool("no-daemon", false, "simulate a stopped daemon")
	update := flag.Bool("update", false, "simulate a newer release")
	onBattery := flag.Bool("battery", false, "simulate running on battery")
	idle := flag.Duration("idle", 10*time.Minute, "stop after this long without a page")
	page := flag.String("page", "", "page to open first")
	flag.Parse()

	dir, err := os.MkdirTemp("", "guidev-")
	if err != nil {
		log.Fatal(err)
	}
	defer os.RemoveAll(dir) //nolint:errcheck // a temporary lock directory

	laptop := guitest.New()
	laptop.NoDaemon = *noDaemon
	laptop.SetUpdateAvailable(*update)
	laptop.SetAC(!*onBattery)

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()
	err = gui.Run(ctx, gui.Options{
		Commands: laptop, RuntimeDir: dir, Page: *page, IdleTimeout: *idle, Version: "dev",
		Open: func(url string) error { fmt.Println(url); return nil },
	})
	if err != nil {
		log.Fatal(err)
	}
}
