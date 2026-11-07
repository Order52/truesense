package tui

import "math"

// Key is a key press: a printable character is its own code, the rest are the constants below.
type Key int32

// Keys the screen reacts to. Enter arrives as KeyEnter (a carriage return is the same key).
const (
	KeyCtrlL     Key = 12
	KeyTab       Key = 9
	KeyEnter     Key = 10
	KeyReturn    Key = 13
	KeyEsc       Key = 27
	KeyBackspace Key = 127
	KeyBS        Key = 8
)

// Special keys live above the Unicode range so they never collide with a character.
const (
	KeyUp Key = 0x110000 + iota
	KeyDown
	KeyLeft
	KeyRight
	KeyBacktab
	KeyHome
	KeyEnd
	KeyPageUp
	KeyPageDown
)

// Key handles a key press. It reports nothing: the screen is redrawn when Dirty.
func (a *App) Key(k Key) {
	a.lock()
	defer a.unlock()
	a.key(k)
}

func (a *App) key(k Key) {
	a.touch()
	a.drag = nil
	if a.modal != nil {
		a.modalKey(k)
		return
	}
	pg := a.page()
	before := a.ctxPos()
	switch {
	case k == 'q' || k == 'Q':
		a.quit = true
	case k == KeyEsc:
		a.undoAll()
	case k == '?':
		a.help()
	case k == KeyTab:
		a.gotoPage(a.pageI + 1)
	case k == KeyBacktab:
		a.gotoPage(a.pageI - 1)
	case k >= '1' && int(k-'1') < len(a.pages):
		a.gotoPage(int(k - '1'))
	case k == KeyUp || k == 'k':
		pg.move(-1)
	case k == KeyDown || k == 'j':
		pg.move(1)
	case k == KeyHome:
		pg.sel = 0
		if !pg.rows[0].Selectable() {
			pg.move(1)
		}
	case k == KeyEnd:
		pg.sel = len(pg.rows) - 1
		if !pg.rows[pg.sel].Selectable() {
			pg.move(-1)
		}
	case k == KeyPageUp:
		for range 5 {
			pg.move(-1)
		}
	case k == KeyPageDown:
		for range 5 {
			pg.move(1)
		}
	case k == KeyLeft || k == 'h':
		a.adjust(-1)
	case k == KeyRight || k == 'l':
		a.adjust(1)
	case k == KeyEnter || k == KeyReturn || k == ' ':
		a.activate()
	case k == KeyCtrlL:
		a.repaint = true // redraw the whole screen (after another program wrote over it)
	case k == 'r':
		if a.worker.Idle() && len(a.pending) == 0 {
			clear(a.kb)
			clear(a.rear)
		}
		a.say("Reading everything again…", StDim, 2, false)
		a.refresh()
	case k == 'm':
		a.cycleMode()
	}
	if a.ctxPos() != before {
		a.moved()
	}
}

// cycleMode steps to the next power mode from the value just chosen (not the last
// reading), so two quick presses go two steps.
func (a *App) cycleMode() {
	row := a.pages[0].rows[0]
	m := asString(row.Value())
	i := -1
	for j, x := range modeOrder {
		if x == m {
			i = j
		}
	}
	if i < 0 {
		return
	}
	a.editAfter(row, modeOrder[(i+1)%len(modeOrder)], false, delayCycleKey)
}

// cur is the selected row if it can be acted on.
func (a *App) cur() *Row {
	pg := a.page()
	if len(pg.rows) == 0 {
		return nil
	}
	if r := pg.rows[pg.sel]; r.Selectable() {
		return r
	}
	return nil
}

// adjust moves a choice or slider one step, or flips a toggle.
func (a *App) adjust(d int) {
	r := a.cur()
	if r == nil {
		return
	}
	switch r.Kind {
	case KindToggle:
		a.edit(r, !asBool(r.Value()), true)
	case KindChoice:
		opts := r.options()
		if len(opts) == 0 {
			return
		}
		i, known := r.Index()
		j := i + d
		if !known {
			j = 0
			if d < 0 {
				j = len(opts) - 1
			}
		}
		j = max(0, min(len(opts)-1, j))
		if !known || j != i {
			a.edit(r, opts[j].Value, false)
		}
	case KindSlider:
		if len(r.Values) == 0 {
			return
		}
		i, known := r.Index()
		base := i
		if !known {
			base = r.StartIndex()
		}
		j := max(0, min(len(r.Values)-1, base+d))
		if !known || j != i {
			a.edit(r, r.Values[j], false)
		}
	}
}

// activate is Enter or Space: run a button, flip a toggle, or send a change that was made.
// On a choice or slider that was not changed it does nothing but say how to change it:
// Enter never writes a value the user did not choose.
func (a *App) activate() {
	r := a.cur()
	if r == nil {
		return
	}
	switch r.Kind {
	case KindButton:
		a.press(r)
	case KindToggle:
		a.edit(r, !asBool(r.Value()), true)
	case KindChoice, KindSlider:
		switch {
		case r.state == StatePending:
			a.commit(r)
		case a.cv.Glyphs().Unicode:
			a.say("Use ← → to change this; it is applied by itself", StDim, 3, true)
		default:
			a.say("Use Left/Right to change this; it is applied by itself", StDim, 3, true)
		}
	}
}

// ---- mouse

// Mouse handles a press at column x, row y (acting on the press, no click delay), or a
// wheel step (wheel -1 up, +1 down). click false with no wheel does nothing.
func (a *App) Mouse(x, y, wheel int, click bool) {
	a.lock()
	defer a.unlock()
	a.mouse(x, y, wheel, click)
}

func (a *App) mouse(x, y, wheel int, click bool) {
	a.touch()
	if a.modal != nil {
		a.modalMouse(x, y, click)
		return
	}
	before := a.ctxPos()
	pg := a.page()
	if wheel != 0 {
		pg.move(wheel)
		a.moved()
		return
	}
	if !click {
		return
	}
	if y == 1 {
		for _, t := range a.tabHits {
			if t.x0 <= x && x < t.x1 {
				a.gotoPage(t.page)
				return
			}
		}
		return
	}
	for i := len(a.hits) - 1; i >= 0; i-- {
		h := a.hits[i]
		if h.y != y || x < h.x0 || x >= h.x1 {
			continue
		}
		if r := h.row; r.Selectable() {
			for idx, pr := range pg.rows {
				if pr == r {
					pg.sel = idx
				}
			}
		}
		switch h.zone {
		case zoneRow:
			continue
		case zoneToggle:
			a.edit(h.row, !asBool(h.row.Value()), true)
		case zoneButton:
			a.press(h.row)
		case zoneOpt:
			if opts := h.row.options(); h.extra < len(opts) { // the list may have changed since the frame was drawn
				a.edit(h.row, opts[h.extra].Value, true)
			}
		case zonePrev:
			a.adjust(-1)
		case zoneNext:
			a.adjust(1)
		case zoneBar:
			a.drag = &dragState{h.row, h.x0, h.extra}
			a.dragTo(x, true)
		}
		break
	}
	if a.ctxPos() != before {
		a.moved()
	}
}

func (a *App) dragTo(x int, now bool) {
	if a.drag == nil {
		return
	}
	r, n := a.drag.row, len(a.drag.row.Values)
	i := int(math.RoundToEven(float64(x-a.drag.x0) / float64(max(1, a.drag.width-1)) * float64(n-1)))
	i = max(0, min(n-1, i))
	if cur, ok := asInt(r.Value()); ok && cur == r.Values[i] && !r.Busy() {
		return
	}
	a.edit(r, r.Values[i], now)
}

// MouseMove is the pointer moving with the button held: keep dragging the slider that was pressed.
func (a *App) MouseMove(x, y int) {
	a.lock()
	defer a.unlock()
	if a.drag != nil && a.modal == nil {
		a.dragTo(x, false)
		a.touch()
	}
}

// MouseRelease ends a drag and sends the position it ended on.
func (a *App) MouseRelease() {
	a.lock()
	defer a.unlock()
	d := a.drag
	a.drag = nil
	if d != nil && d.row.state == StatePending {
		a.commit(d.row)
		a.touch()
	}
}
