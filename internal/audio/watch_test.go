package audio

import (
	"bytes"
	"context"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestAHeadsetPluggedInLaterGetsDTSByItself(t *testing.T) {
	r := newRig(t, false)
	var sinks []string
	run := r.c.Run
	r.c.Run = func(c2 context.Context, name string, args ...string) Result {
		if name == "pactl" && strings.Join(args, " ") == "list short sinks" {
			return Result{Stdout: "1\t" + strings.Join(sinks, "\n2\t") + "\n"}
		}
		return run(c2, name, args...)
	}
	sinks = []string{"alsa_output.pci.speaker"}
	w := &DTSWatcher{c: r.c}
	w.Step(ctx())
	if len(r.sys.modeCalls()) != 0 {
		t.Fatalf("the first look is only a baseline: %v", r.sys.modeCalls())
	}
	w.Step(ctx())
	if len(r.sys.modeCalls()) != 0 {
		t.Fatal("nothing changed")
	}
	sinks = append(sinks, usbHeadset)
	w.Step(ctx())
	if len(r.sys.modeCalls()) == 0 {
		t.Fatal("the new output was covered")
	}
	if len(r.clk.sleeps) != 1 || r.clk.sleeps[0] != 1500*time.Millisecond {
		t.Errorf("AcerUltraX needs 1.5 s to register the output: %v", r.clk.sleeps)
	}
	r.sys.calls = nil
	w.Step(ctx())
	if len(r.sys.modeCalls()) != 0 {
		t.Error("the same output was covered twice")
	}
	// an output that goes away is not a trigger
	sinks = sinks[:1]
	w.Step(ctx())
	if len(r.sys.modeCalls()) != 0 {
		t.Error("an unplug triggered a cover")
	}
}

func TestADTSCoverThatFailsIsRetriedOnTheNextLook(t *testing.T) {
	r := newRig(t, false)
	var buf bytes.Buffer
	r.c.Log = slog.New(slog.NewTextHandler(&buf, nil))
	sinks := []string{"a"}
	run := r.c.Run
	r.c.Run = func(c2 context.Context, name string, args ...string) Result {
		if name == "pactl" && strings.Join(args, " ") == "list short sinks" {
			return Result{Stdout: "1\t" + strings.Join(sinks, "\n2\t") + "\n"}
		}
		return run(c2, name, args...)
	}
	w := &DTSWatcher{c: r.c}
	w.Step(ctx())
	r.sys.dtsFail["devices"] = "helper crashed"
	sinks = []string{"a", "b"}
	w.Step(ctx())
	if !strings.Contains(buf.String(), "DTS cover failed") || !strings.Contains(buf.String(), "helper crashed") {
		t.Errorf("log = %s", buf.String())
	}
	delete(r.sys.dtsFail, "devices")
	w.Step(ctx())
	if len(r.sys.modeCalls()) == 0 {
		t.Error("after a failure every output looks new, so the cover is retried")
	}
}

func TestTheMicWatcherActsOnlyWhenSomethingChanges(t *testing.T) {
	r := newRig(t, false)
	r.cfg.Audio.Notify = true
	w := r.c.NewMicWatcher()
	w.Step(ctx())
	if len(r.osd.shown) != 1 || !strings.Contains(r.osd.shown[0], "Mic array") {
		t.Fatalf("first pass announces the choice: %v", r.osd.shown)
	}
	if r.sys.defaultSource != testSrc1 {
		t.Fatalf("default = %s", r.sys.defaultSource)
	}
	// the user picks another microphone by hand: the watcher must NOT fight it
	r.sys.defaultSource = testSrc2
	r.osd.shown = nil
	for i := 0; i < 4; i++ {
		w.Step(ctx())
	}
	if r.sys.defaultSource != testSrc2 || len(r.osd.shown) != 0 {
		t.Fatalf("the watcher overrode a manual choice: default %s osd %v", r.sys.defaultSource, r.osd.shown)
	}
	// a headset is plugged in: the policy runs and announces
	r.sys.headsetMicJack = "on"
	w.Step(ctx())
	if r.sys.defaultSource != testSrc2 || len(r.osd.shown) != 1 || !strings.Contains(r.osd.shown[0], "Headset") {
		t.Fatalf("plug: default %s osd %v", r.sys.defaultSource, r.osd.shown)
	}
	// a filter appears: re-applied, but only announced when the jack changed
	r.osd.shown = nil
	r.c.VoiceSet(ctx(), "headset", "conference")
	w.Step(ctx())
	if r.sys.defaultSource != "truesense_pv_headset" || len(r.osd.shown) != 0 {
		t.Fatalf("filter: default %s osd %v", r.sys.defaultSource, r.osd.shown)
	}
	// notifications switched off
	r.cfg.Audio.Notify = false
	r.sys.headsetMicJack = "off"
	w.Step(ctx())
	if len(r.osd.shown) != 0 {
		t.Errorf("notify = false but osd %v", r.osd.shown)
	}
	if r.clk.sleeps[0] != 800*time.Millisecond {
		t.Errorf("PipeWire needs 0.8 s to settle: %v", r.clk.sleeps)
	}
}

func TestTheMicWatcherRunsUntilItsContextEndsAndStopsEverythingItStarted(t *testing.T) {
	r := newRig(t, true) // a broken card: the login repair has work to do
	cctx, cancel := context.WithCancel(context.Background())
	ticks := 0
	r.clk.onTick = func(d time.Duration) {
		if d == time.Second {
			if ticks++; ticks == 3 {
				cancel()
			}
		}
	}
	r.c.NewMicWatcher().Run(cctx)
	if ticks != 3 {
		t.Fatalf("ticks = %d", ticks)
	}
	if !r.sys.did("/x/acerultrax-cli", "devices") {
		t.Error("the DTS cover at start-up did not run")
	}
}

func TestTheMicWatcherSurvivesAMissingSoundSystem(t *testing.T) {
	r := newRig(t, false)
	r.c.Run = func(context.Context, string, ...string) Result { return Result{Code: 127, Stderr: "not installed"} }
	w := r.c.NewMicWatcher()
	for i := 0; i < 3; i++ {
		w.Step(ctx()) // no panic, no hang
	}
}

func TestPurifiedVoiceFilesAreWrittenAtomicallyWithPlainPermissions(t *testing.T) {
	r := newRig(t, false)
	r.c.VoiceSet(ctx(), "headset", "conference")
	st, err := os.Stat(filepath.Join(r.dir, "pw", "truesense-pv-headset.conf"))
	if err != nil || st.Mode().Perm() != 0o644 {
		t.Fatalf("conf = %v, %v", st, err)
	}
	entries, _ := os.ReadDir(filepath.Join(r.dir, "pw"))
	if len(entries) != 1 {
		t.Errorf("leftover temp files: %v", entries)
	}
}
