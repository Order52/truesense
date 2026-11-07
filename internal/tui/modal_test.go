package tui

import (
	"reflect"
	"strings"
	"testing"
)

func TestConfirmDialogDefaultsToNoAndEnterAloneNeverSaysYes(t *testing.T) {
	for _, k := range []Key{KeyEnter, KeyReturn, ' ', 'n', 'N', KeyEsc, 'q'} {
		h := newHarness(t, 100, 30)
		yes, no := 0, 0
		h.app.confirm("Stop?", func() { yes++ }, func() { no++ })
		if h.app.modal.focus != 1 {
			t.Fatal("focus must start on No")
		}
		h.app.Key(k)
		if yes != 0 || no != 1 || h.app.modal != nil {
			t.Errorf("key %v: yes %d no %d modal %v", k, yes, no, h.app.modal)
		}
		if !strings.Contains(h.app.msg.text, "Cancelled") {
			t.Errorf("key %v: message %q", k, h.app.msg.text)
		}
	}
}

func TestConfirmDialogYesNeedsAnExplicitChoice(t *testing.T) {
	for name, keys := range map[string][]Key{"y": {'y'}, "Y": {'Y'}, "arrow then Enter": {KeyLeft, KeyEnter}, "h then space": {'h', ' '}, "tab then Enter": {KeyTab, KeyReturn}, "backtab": {KeyBacktab, KeyEnter}} {
		h := newHarness(t, 100, 30)
		yes, no := 0, 0
		h.app.confirm("Stop?", func() { yes++ }, func() { no++ })
		for _, k := range keys {
			h.app.Key(k)
		}
		if yes != 1 || no != 0 || h.app.modal != nil || h.app.yes != 1 {
			t.Errorf("%s: yes %d no %d", name, yes, no)
		}
	}
}

func TestConfirmDialogFocusToggles(t *testing.T) {
	h := newHarness(t, 100, 30)
	h.app.confirm("Stop?", func() {}, nil)
	var got []int
	for _, k := range []Key{KeyLeft, KeyRight, 'h', 'l', KeyTab, KeyBacktab} {
		h.app.Key(k)
		got = append(got, h.app.modal.focus)
	}
	if !reflect.DeepEqual(got, []int{0, 1, 0, 1, 0, 1}) {
		t.Fatalf("%v", got)
	}
	h.app.Key('x') // other keys do nothing, and never close it
	if h.app.modal == nil {
		t.Fatal("an unrelated key closed the question")
	}
}

func TestConfirmDialogIsDrawnWithBothButtonsAndTheMarkedOne(t *testing.T) {
	h := newHarness(t, 100, 30)
	h.app.confirm("Stop BOTH fans? A rather long question that has to wrap onto several lines to fit the box.", func() {}, nil)
	txt := h.text()
	for _, w := range []string{"Confirm", "[ Yes ]", "[ No ]", "Enter picks the marked one"} {
		if !strings.Contains(txt, w) {
			t.Errorf("missing %q", w)
		}
	}
	for y, line := range strings.Split(txt, "\n") {
		if i := strings.Index(line, "[ No ]"); i >= 0 {
			if st := h.cv.StyleAt(y, len([]rune(line[:i]))); st != StPill {
				t.Errorf("No is drawn %s, want the marked pill", st)
			}
		}
		if i := strings.Index(line, "[ Yes ]"); i >= 0 {
			if st := h.cv.StyleAt(y, len([]rune(line[:i]))); st == StPill {
				t.Error("Yes must not be the marked one")
			}
		}
	}
}

func TestConfirmDialogMouseAnswersOnPress(t *testing.T) {
	h := newHarness(t, 100, 30)
	yes := 0
	h.app.confirm("Go?", func() { yes++ }, nil)
	h.app.Draw()
	h.app.Mouse(0, 0, 0, true) // elsewhere: nothing
	if h.app.modal == nil {
		t.Fatal("a click outside the buttons closed the question")
	}
	h.app.Mouse(0, 0, 1, false) // the wheel does nothing either
	if !h.click("[ Yes ]", -1) || yes != 1 {
		t.Fatal("clicking Yes")
	}
}

func TestPromptFirstKeyReplacesTheSuggestionAndBackspaceEditsIt(t *testing.T) {
	h := newHarness(t, 100, 30)
	var got []string
	h.app.prompt("Colour", "00aec7", func(s string) { got = append(got, s) })
	h.keys("ab")
	h.app.Key(KeyEnter)
	h.app.prompt("Colour", "00aec7", func(s string) { got = append(got, s) })
	h.app.Key(KeyBackspace)
	h.keys("1")
	h.app.Key(KeyEnter)
	h.app.prompt("Colour", "ff", func(s string) { got = append(got, s) })
	h.app.Key(KeyEsc)
	if h.app.modal != nil {
		t.Fatal("Esc closes the prompt")
	}
	if !reflect.DeepEqual(got, []string{"ab", "00aec1"}) {
		t.Fatalf("%v", got)
	}
}

func TestPromptIsLimitedAndTakesPrintableAsciiOnly(t *testing.T) {
	h := newHarness(t, 100, 30)
	var got string
	h.app.prompt("T", "", func(s string) { got = s })
	h.keys(strings.Repeat("x", 40))
	h.app.Key(KeyUp)
	h.app.Key(Key(0x2603)) // a snowman
	h.app.Key(KeyEnter)
	if got != strings.Repeat("x", promptMax) {
		t.Fatalf("%q", got)
	}
	h.app.prompt("T", "", func(string) {})
	h.app.Draw()
	if !strings.Contains(h.cv.Text(), "> _") {
		t.Fatalf("prompt not drawn:\n%s", h.cv.Text())
	}
	h.app.Key(KeyBackspace) // on an empty buffer: no panic
}

func TestHelpClosesOnAnyKeyOrClick(t *testing.T) {
	h := newHarness(t, 100, 30)
	h.app.help()
	h.app.Mouse(3, 3, 0, true)
	if h.app.modal != nil {
		t.Fatal("a click did not close the help")
	}
	h.app.help()
	h.app.Key('x')
	if h.app.modal != nil {
		t.Fatal("a key did not close the help")
	}
}

func TestEveryDialogFitsTheSmallestScreen(t *testing.T) {
	h := newHarness(t, 44, 12)
	h.app.help()
	if widest(h.text()) > 44 {
		t.Fatal("help too wide")
	}
	h.app.modal = nil
	h.app.confirm("Stop BOTH fans? The service brings them back at CPU 70 °C / GPU 65 °C.", func() {}, nil)
	if widest(h.text()) > 44 {
		t.Fatal("question too wide")
	}
}

func TestWrap(t *testing.T) {
	for _, tc := range []struct {
		in   string
		n    int
		want []string
	}{
		{"", 10, nil},
		{"hello world", 20, []string{"hello world"}},
		{"hello world", 5, []string{"hello", "world"}},
		{"a   b", 10, []string{"a   b"}},
		{"abcdefghij", 4, []string{"abcd", "efgh", "ij"}},
		{"one two three four", 9, []string{"one two", "three", "four"}},
		{"  lead", 10, []string{"lead"}},
		{"x", 0, []string{"x"}},
	} {
		if got := wrap(tc.in, tc.n); !reflect.DeepEqual(got, tc.want) {
			t.Errorf("wrap(%q,%d) = %q, want %q", tc.in, tc.n, got, tc.want)
		}
	}
}
