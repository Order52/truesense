package tui

import (
	"strings"
	"testing"
	"unicode/utf8"
)

// FuzzApp turns arbitrary bytes into key presses, clicks, drags, ticks and resizes: the
// screen must never panic, never draw wider than the terminal and never stop a fan.
func FuzzApp(f *testing.F) {
	f.Add([]byte("jjhhhh\n\n"))
	f.Add([]byte("2jhhhhhhhh\ny"))
	f.Add([]byte("4jjjjllll\x1b?q"))
	f.Add([]byte{3, 10, 20, 4, 30, 40, 5, 6, 7, 8, 9})
	f.Fuzz(func(t *testing.T, data []byte) {
		h := newHarness(t, 100, 30)
		w, hh := 100, 30
		sizes := [][2]int{{100, 30}, {60, 20}, {44, 12}, {30, 8}}
		for i := 0; i < len(data); i++ {
			b := data[i]
			arg := func() int {
				if i+1 < len(data) {
					i++
					return int(data[i])
				}
				return 0
			}
			switch b % 8 {
			case 0, 1, 2:
				h.app.Key(fuzzKeys[int(b)%len(fuzzKeys)])
			case 3:
				h.app.Mouse(arg()%(w+1), arg()%(hh+1), 0, true)
			case 4:
				h.app.MouseMove(arg()%(w+1), arg()%(hh+1))
			case 5:
				h.app.MouseRelease()
			case 6:
				h.elapse()
			case 7:
				sz := sizes[arg()%len(sizes)]
				w, hh = sz[0], sz[1]
				h.cv.Resize(w, hh)
			}
			h.app.quit = false
			h.app.Draw()
			if widest(h.cv.Text()) > w {
				t.Fatalf("a line wider than %d cells", w)
			}
		}
		h.app.modal = nil
		h.app.Flush()
		if h.b.duty[0] == 0 || h.b.duty[1] == 0 || len(h.b.stops) != 0 {
			t.Fatal("a fan was stopped without the service")
		}
	})
}

// FuzzWrap checks the word wrapper's contract: no line is wider than asked and no word
// character is lost.
func FuzzWrap(f *testing.F) {
	f.Add("Stop BOTH fans? The service brings them back at CPU 70 °C / GPU 65 °C.", 20)
	f.Add("aaaaaaaaaaaaaaaaaaaaaaaaaaaaa", 4)
	f.Add("  \t\n x  y ", 3)
	f.Fuzz(func(t *testing.T, text string, n int) {
		if n < 1 || n > 200 || !utf8.ValidString(text) {
			return
		}
		lines := wrap(text, n)
		var got strings.Builder
		for _, l := range lines {
			if utf8.RuneCountInString(l) > n {
				t.Fatalf("line %q is wider than %d", l, n)
			}
			got.WriteString(strings.Join(strings.Fields(l), ""))
		}
		if want := strings.Join(strings.Fields(text), ""); got.String() != want {
			t.Fatalf("lost text: %q -> %q", want, got.String())
		}
	})
}
