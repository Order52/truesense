package tui

import "strings"

type modalKind int

const (
	modalConfirm modalKind = iota
	modalPrompt
	modalHelp
)

// modal is a dialog that takes every key and click until it is closed.
type modal struct {
	kind   modalKind
	text   string
	buf    string
	fresh  bool // prompt: the first key replaces the suggested text
	focus  int  // confirm: 0 = Yes, 1 = No: Enter never says yes by itself
	then   func()
	submit func(string)
	cancel func()
}

type modalHit struct {
	y, x0, x1 int
	yes       bool
}

// promptMax is the longest text a prompt takes.
const promptMax = 24

// confirm asks a question with [ Yes ] [ No ]; the focus starts on No.
func (a *App) confirm(text string, then, cancel func()) {
	a.modal = &modal{kind: modalConfirm, text: text, then: then, cancel: cancel, focus: 1}
	a.touch()
}

// prompt asks for a line of text; the suggested text is replaced by the first key typed.
func (a *App) prompt(title, initial string, submit func(string)) {
	a.modal = &modal{kind: modalPrompt, text: title, buf: initial, submit: submit, fresh: true}
	a.touch()
}

func (a *App) help() { a.modal = &modal{kind: modalHelp}; a.touch() }

func (a *App) modalKey(k Key) {
	m := a.modal
	switch m.kind {
	case modalHelp:
		a.modal = nil
	case modalConfirm:
		switch k {
		case 'y', 'Y':
			a.answer(true)
		case 'n', 'N', KeyEsc, 'q':
			a.answer(false)
		case KeyLeft, KeyRight, 'h', 'l', KeyTab, KeyBacktab:
			m.focus = 1 - m.focus
		case KeyEnter, KeyReturn, ' ':
			a.answer(m.focus == 0)
		}
	case modalPrompt:
		switch {
		case k == KeyEsc:
			a.modal = nil
		case k == KeyEnter || k == KeyReturn:
			a.modal = nil
			m.submit(m.buf)
		case k == KeyBackspace || k == KeyBS:
			if r := []rune(m.buf); len(r) > 0 {
				m.buf = string(r[:len(r)-1])
			}
			m.fresh = false
		case k >= 32 && k < 127:
			if m.fresh {
				m.buf = ""
			}
			m.fresh = false
			m.buf = clip(m.buf+string(rune(k)), promptMax) // the first key replaces the suggested text; Backspace edits it instead
		}
	}
}

func (a *App) modalMouse(x, y int, click bool) {
	if !click {
		return
	}
	switch a.modal.kind {
	case modalHelp:
		a.modal = nil
	case modalConfirm:
		for _, h := range a.modalHits {
			if h.y == y && h.x0 <= x && x < h.x1 {
				a.answer(h.yes)
				return
			}
		}
	}
}

// answer closes a question: Yes runs it, No cancels it and says nothing was changed.
func (a *App) answer(yes bool) {
	m := a.modal
	a.modal = nil
	if yes {
		a.yes++
		m.then()
		return
	}
	if m.cancel != nil {
		m.cancel()
	}
	a.say("Cancelled: nothing was changed", StDim, 3, false)
}

func (a *App) drawModal() {
	cv := a.cv
	w, h := cv.Size()
	g := cv.Glyphs()
	a.modalHits = nil
	m := a.modal
	if m.kind == modalHelp {
		keys := [][2]string{
			{g.HelpUpDown + "  j k", "move between settings"}, {g.HelpLeftRight + "  h l", "change the value (sent by itself after a short pause)"},
			{"Enter", "send the change now / run a button"}, {"Space", "switch a toggle, or send the change"},
			{"Esc", "take back a change that was not sent yet"}, {"Tab  1-7", "next page, or a page by number"}, {"q", "quit"}, {"m", "next power mode"},
			{"r", "read everything again"}, {"Ctrl-L", "redraw the screen"}}
		notes := []string{"Mouse: click tabs, rows and options; drag a slider; the wheel moves the selection.",
			"Stopping a fan, or a setting that needs a restart, always asks first. The answer is No unless you choose Yes.",
			"The window has the rest: per-key colours, images, undervolting."}
		bw := min(w-4, 78)
		inner := bw - 4
		kw := 0
		for _, k := range keys {
			kw = max(kw, width(k[0]))
		}
		kw = min(kw+2, inner/2)
		type line struct {
			key, text string
			col       int
		}
		var lines []line
		for _, k := range keys {
			for i, part := range wrap(k[1], max(10, inner-kw)) {
				key := ""
				if i == 0 {
					key = k[0]
				}
				lines = append(lines, line{key, part, kw})
			}
		}
		lines = append(lines, line{})
		for _, n := range notes {
			for _, part := range wrap(n, inner) {
				lines = append(lines, line{"", part, 0})
			}
			lines = append(lines, line{})
		}
		lines = lines[:len(lines)-1]
		bh := min(len(lines)+4, h-2)
		y, x := max(1, (h-bh)/2), (w-bw)/2
		for i := range bh {
			fillRect(cv, y+i, x, bw, StText, " ")
		}
		drawBox(cv, y, x, bh, bw, "Keys", StAccent, StAccent)
		for i, l := range lines {
			if i >= bh-4 {
				break
			}
			if l.key != "" {
				cv.Put(y+2+i, x+2, l.key, StKey)
			}
			st := StDim
			if l.col != 0 {
				st = StText
			}
			cv.Put(y+2+i, x+2+l.col, l.text, st)
		}
		return
	}
	body := wrap(m.text, min(60, w-10))
	if len(body) == 0 {
		body = []string{""}
	}
	longest := 0
	for _, b := range body {
		longest = max(longest, width(b))
	}
	bw := min(w-4, min(max(longest+6, 24), 66))
	bh := len(body) + 5
	y, x := max(1, (h-bh)/2), (w-bw)/2
	for i := range bh {
		fillRect(cv, y+i, x, bw, StText, " ")
	}
	title, bst := "Type", StAccent
	if m.kind == modalConfirm {
		title, bst = "Confirm", StWarn
	}
	drawBox(cv, y, x, bh, bw, title, bst, StAccent)
	for i, b := range body {
		cv.Put(y+1+i, x+3, b, StText)
	}
	if m.kind != modalConfirm {
		cv.Put(y+bh-2, x+3, "> "+m.buf+"_", StAccent)
		return
	}
	by, bx := y+bh-2, x+3
	for i, label := range []string{"Yes", "No"} {
		capt := "[ " + label + " ]"
		st := StDim
		if m.focus == i {
			st = StPill
		}
		cv.Put(by, bx, capt, st)
		a.modalHits = append(a.modalHits, modalHit{by, bx, bx + width(capt), i == 0})
		bx += width(capt) + 2
	}
	cv.Put(by, bx+1, "y / n  ·  Enter picks the marked one", StDim)
}

// wrap breaks text into lines of at most n cells at spaces; a word longer than a line is
// cut. Runs of spaces inside a line are kept, spaces at the ends of lines are dropped.
func wrap(text string, n int) []string {
	if n < 1 {
		n = 1
	}
	var lines []string
	var cur []rune
	flush := func() {
		if s := strings.TrimRight(string(cur), " "); s != "" {
			lines = append(lines, s)
		}
		cur = cur[:0]
	}
	for _, chunk := range chunks(text) {
		cr := []rune(chunk)
		isSpace := cr[0] == ' '
		if isSpace {
			if len(cur) > 0 {
				cur = append(cur, cr...)
			}
			continue
		}
		for len(cur)+len(cr) > n {
			if len(cur) > 0 {
				flush()
				continue
			}
			cur = append(cur, cr[:n]...)
			cr = cr[n:]
			flush()
		}
		cur = append(cur, cr...)
	}
	flush()
	return lines
}

// chunks splits text into alternating runs of spaces and non-spaces.
func chunks(text string) []string {
	var out []string
	var b strings.Builder
	space := false
	for i, r := range text {
		isSp := r == ' ' || r == '\t' || r == '\n'
		if i > 0 && isSp != space {
			out = append(out, b.String())
			b.Reset()
		}
		space = isSp
		if isSp {
			r = ' '
		}
		b.WriteRune(r)
	}
	if b.Len() > 0 {
		out = append(out, b.String())
	}
	return out
}
