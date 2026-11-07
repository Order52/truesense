package tui

import (
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

var update = flag.Bool("update", false, "rewrite the golden snapshots in testdata/golden")

// goldenState is a fixed laptop: the same readings every run, so a page is the same text.
func goldenState(b *fakeBackend) {
	b.service = true
	b.mode, b.auto, b.smartProf = "performance", "smart", "steady"
	b.saved = []string{"aurora", "ember"}
	b.devices = []DTSDevice{{Label: "Speakers", Profile: "Music", Current: true}, {Label: "Headset PC38X", Profile: "Game1"}}
	b.limit, b.hold = true, 0
}

func checkGolden(t *testing.T, name, got string) {
	t.Helper()
	path := filepath.Join("testdata", "golden", name+".txt")
	got += "\n"
	if *update {
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(got), 0o644); err != nil {
			t.Fatal(err)
		}
		return
	}
	want, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("%v (run go test -update ./internal/tui)", err)
	}
	if string(want) != got {
		t.Errorf("%s differs from its golden snapshot (run go test -update to accept):\n--- want\n%s\n--- got\n%s", name, want, got)
	}
}

// TestGoldenPages snapshots every page at four sizes, with the service running and with
// the direct hardware fallback.
func TestGoldenPages(t *testing.T) {
	sizes := [][2]int{{100, 30}, {80, 24}, {60, 20}, {44, 12}}
	for _, svc := range []bool{true, false} {
		for _, sz := range sizes {
			for i := range pageCount {
				mode := "service"
				if !svc {
					mode = "direct"
				}
				name := fmt.Sprintf("%s_%s_%dx%d", strings.ToLower(pageNames[i]), mode, sz[0], sz[1])
				t.Run(name, func(t *testing.T) {
					h := newHarness(t, sz[0], sz[1], goldenState, func(b *fakeBackend) { b.service = svc })
					h.app.Goto(i)
					checkGolden(t, name, h.text())
				})
			}
		}
	}
}

// TestGoldenDialogsAndStates snapshots the states a page passes through: a change waiting,
// a question, the help, the activity list and a terminal that is too small.
func TestGoldenDialogsAndStates(t *testing.T) {
	cases := map[string]func(h *harness){
		"pending_change_100x30": func(h *harness) { h.app.Goto(1); h.keys("jlll") },
		"question_100x30": func(h *harness) {
			h.app.Goto(1)
			h.keys("j")
			h.keys("hhhhhhhh")
			h.app.Key(KeyEnter)
		},
		"help_100x30": func(h *harness) { h.keys("?") },
		"activity_100x40": func(h *harness) {
			h.keys("l")
			h.app.Key(KeyEnter)
			h.app.Goto(2)
			h.sel("Boot sound")
			h.keys(" ")
			h.app.Goto(0)
		},
		"prompt_100x30":  func(h *harness) { h.app.Goto(3); h.sel("Custom colour"); h.app.Key(KeyEnter) },
		"too_small_30x8": func(h *harness) {},
	}
	for name, setup := range cases {
		t.Run(name, func(t *testing.T) {
			var w, hh int
			var base string
			if _, err := fmt.Sscanf(name[strings.LastIndex(name, "_")+1:], "%dx%d", &w, &hh); err != nil {
				t.Fatal(err)
			}
			base = name
			h := newHarness(t, w, hh, goldenState)
			setup(h)
			checkGolden(t, base, h.text())
		})
	}
}
