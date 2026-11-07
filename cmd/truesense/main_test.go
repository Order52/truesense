package main

import (
	"runtime"
	"runtime/debug"
	"testing"
)

func TestTuneSizesTheRuntimeForASmallProgram(t *testing.T) {
	t.Setenv("GOMAXPROCS", "")
	t.Setenv("GOGC", "")
	prev := runtime.GOMAXPROCS(0)
	oldGC := debug.SetGCPercent(100)
	defer func() { runtime.GOMAXPROCS(prev); debug.SetGCPercent(oldGC) }()
	tune()
	if got := runtime.GOMAXPROCS(0); got > 2 {
		t.Fatalf("GOMAXPROCS = %d, want at most 2", got)
	}
	if got := debug.SetGCPercent(25); got != 25 {
		t.Fatalf("GC percent = %d, want 25", got)
	}
}

func TestTuneRespectsTheEnvironment(t *testing.T) {
	t.Setenv("GOMAXPROCS", "4")
	t.Setenv("GOGC", "200")
	prev := runtime.GOMAXPROCS(7)
	oldGC := debug.SetGCPercent(150)
	defer func() { runtime.GOMAXPROCS(prev); debug.SetGCPercent(oldGC) }()
	tune()
	if got := runtime.GOMAXPROCS(0); got != 7 {
		t.Fatalf("an explicit GOMAXPROCS must be left alone, runtime now %d", got)
	}
	if got := debug.SetGCPercent(150); got != 150 {
		t.Fatalf("an explicit GOGC must be left alone, GC percent now %d", got)
	}
}
