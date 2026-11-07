package tui

import (
	"fmt"
	"math"
	"strings"
)

const (
	minWidth, minHeight = 44, 12
	wideTabs            = 82
)

// roundEven rounds half to even, as the layout was tuned with.
func roundEven(f float64) int { return int(math.RoundToEven(f)) }

// Draw paints the whole screen.
func (a *App) Draw() {
	a.lock()
	defer a.unlock()
	a.draw()
}

func (a *App) draw() {
	cv := a.cv
	cv.Clear()
	a.hits, a.tabHits = nil, nil
	w, h := cv.Size()
	if w < minWidth || h < minHeight {
		cv.Put(0, 0, "TrueSense needs", StWarn)
		cv.Put(1, 0, "a 44x12 terminal", StWarn)
		cv.Put(2, 0, fmt.Sprintf("(this is %dx%d)", w, h), StDim)
		return
	}
	a.drawHeader()
	a.drawPage(a.page(), 2, h-4, w)
	a.drawFooter()
	if a.modal != nil {
		a.drawModal()
	}
}

func (a *App) drawHeader() {
	cv := a.cv
	w, _ := cv.Size()
	g := cv.Glyphs()
	cv.Put(0, 0, " TrueSense ", StTitle)
	cv.Put(0, 11, a.version, StDim)
	v := a.data.view()
	type item struct {
		text string
		st   Style
	}
	var right []item
	if v.batt.OK {
		pct := v.batt.V.Percent
		text, st := fmt.Sprintf("%d %%", pct), StBad
		if v.ac.OK && v.ac.V {
			text = g.Bolt + text
		}
		if (v.ac.OK && v.ac.V) || pct > 30 {
			st = StGood
		}
		right = append(right, item{text, st})
	}
	if v.svc != nil {
		right = append(right, item{g.Dot + " service", StGood})
	} else {
		right = append(right, item{g.Dot + " direct", StWarn})
	}
	x := w - 1
	for i := len(right) - 1; i >= 0; i-- {
		x -= width(right[i].text)
		cv.Put(0, x, right[i].text, right[i].st)
		x -= 2
	}
	x = 1
	wide := w >= wideTabs
	for i, p := range a.pages {
		label := fmt.Sprintf(" %d ", i+1)
		if wide || i == a.pageI {
			label = fmt.Sprintf(" %d %s ", i+1, p.name)
		}
		if x+width(label) >= w-1 {
			break
		}
		st := StTab
		if i == a.pageI {
			st = StTabSel
		}
		cv.Put(1, x, label, st)
		a.tabHits = append(a.tabHits, tabHit{x, x + width(label), i})
		x += width(label) + 1
	}
}

type keyHint struct{ key, text string }

// keysFor lists the keys that matter for the selected row, most useful first.
func (a *App) keysFor(r *Row) []keyHint {
	ud, lr := a.cv.Glyphs().UpDown, a.cv.Glyphs().LeftRight
	var base []keyHint
	switch {
	case r == nil || !r.Selectable():
		base = []keyHint{{ud, "select"}}
	case r.Kind == KindSlider:
		base = []keyHint{{ud, "select"}, {lr, "adjust"}, {"Enter", "apply now"}}
	case r.Kind == KindChoice:
		base = []keyHint{{ud, "select"}, {lr, "choose"}, {"Enter", "apply now"}}
	case r.Kind == KindToggle:
		base = []keyHint{{ud, "select"}, {"Space", "switch"}}
	default:
		base = []keyHint{{ud, "select"}, {"Enter", "run"}}
	}
	for _, p := range a.page().rows {
		if p.state == StatePending || p.state == StateAsking {
			base = append(base, keyHint{"Esc", "undo"})
			break
		}
	}
	return append(base, keyHint{"Tab", "page"}, keyHint{"?", "help"}, keyHint{"q", "quit"})
}

func (a *App) drawFooter() {
	cv := a.cv
	w, h := cv.Size()
	pg := a.page()
	var row *Row
	if len(pg.rows) > 0 {
		row = pg.rows[pg.sel]
	}
	line := func(t string, st Style) {
		if width(t) > w-2 {
			t = clip(t, w-3) + "…"
		}
		cv.Put(h-2, 1, t, st)
	}
	switch {
	case a.msg.text != "" && a.now().Before(a.msg.until):
		line(a.msg.text, a.msg.style)
	case a.data.view().err != "":
		line("No device: "+a.data.view().err, StWarn)
	case row != nil && row.Hint != "":
		line(row.Hint, StDim)
	}
	x := 1
	for _, k := range a.keysFor(row) {
		if x+width(k.key)+width(k.text)+3 > w {
			break
		}
		cv.Put(h-1, x, k.key, StKey)
		cv.Put(h-1, x+width(k.key)+1, k.text, StDim)
		x += width(k.key) + width(k.text) + 4
	}
}

type pageLine struct {
	row  int // -1 for a blank line
	part int
	text string
}

func (a *App) drawPage(page *Page, y0, hh, w int) {
	cv := a.cv
	var lines []pageLine
	labelW := func() int {
		m := 12
		first := true
		for _, q := range page.rows {
			if q.Kind != KindHeader {
				if first || width(q.Label) > m {
					m = max(m, width(q.Label))
				}
				first = false
			}
		}
		return min(34, w/2, m+2)
	}
	lw := labelW()
	for i, r := range page.rows {
		if r.Hide != nil && r.Hide() {
			continue
		}
		if r.Kind == KindHeader && len(lines) > 0 {
			lines = append(lines, pageLine{row: -1})
		}
		if r.Kind == KindInfo && r.Wrap {
			parts := wrap(a.infoText(r), max(10, w-lw-8))
			if len(parts) == 0 {
				parts = []string{""}
			}
			for k, p := range parts {
				lines = append(lines, pageLine{i, k, p})
			}
		} else {
			lines = append(lines, pageLine{row: i})
		}
	}
	selLine := 0
	for n, l := range lines {
		if l.row == page.sel && l.part == 0 {
			selLine = n
			break
		}
	}
	if selLine < page.scroll {
		page.scroll = selLine
	}
	if selLine >= page.scroll+hh {
		page.scroll = selLine - hh + 1
	}
	page.scroll = max(0, min(page.scroll, max(0, len(lines)-hh)))
	y := y0
	for n := page.scroll; n < min(len(lines), page.scroll+hh); n++ {
		if l := lines[n]; l.row >= 0 {
			a.drawRow(page.rows[l.row], l.row == page.sel, y, w, lw, l.part, l.text)
		}
		y++
	}
	if len(lines) > hh { // scroll indicator
		g := cv.Glyphs()
		if page.scroll > 0 {
			cv.Put(y0, w-2, g.Up, StDim)
		}
		if page.scroll+hh < len(lines) {
			cv.Put(y0+hh-1, w-2, g.Down, StDim)
		}
	}
	if page.extra != nil {
		if ey := y0 + min(len(lines), hh) + 1; y0+hh-ey >= 5 {
			page.extra(ey, y0+hh-ey, w)
		}
	}
}

func (a *App) infoText(r *Row) string {
	if v := r.Value(); v != nil {
		return r.Show(v)
	}
	return "…"
}

func (a *App) drawRow(r *Row, sel bool, y, w, lw, k int, part string) {
	a.drawRowBody(r, sel, y, w, lw, k, part)
	if r.state != StateIdle && r.Kind != KindInfo && r.Kind != KindHeader && k == 0 {
		// last, so nothing overwrites it: a change waiting for Enter, or being written
		g := a.cv.Glyphs()
		if waiting := r.state == StatePending || r.state == StateAsking; waiting {
			a.cv.Put(y, w-3, g.Waiting, StWarn)
		} else {
			a.cv.Put(y, w-3, g.Writing, StDim)
		}
	}
}

func (a *App) drawRowBody(r *Row, sel bool, y, w, lw, k int, part string) {
	cv := a.cv
	g := cv.Glyphs()
	if r.Kind == KindHeader {
		cv.Put(y, 1, r.Label, StHead)
		cv.Put(y, 2+width(r.Label), " "+strings.Repeat(g.H, max(0, w-width(r.Label)-5)), StDim)
		return
	}
	dis := r.Kind != KindInfo && !r.Selectable()
	if k == 0 {
		if sel {
			cv.Put(y, 0, g.Cursor, StAccent)
		}
		st := StText
		switch {
		case dis:
			st = StDim
		case sel:
			st = StAccent
		}
		cv.Put(y, 2, clip(r.Label, lw-1), st)
	}
	cx := 2 + lw + 1
	if r.Kind == KindInfo {
		v := r.Value()
		st := StText
		if r.InfoStyle != nil && v != nil {
			st = r.InfoStyle(v)
		}
		text := part
		if !r.Wrap {
			text = r.Show(v)
		}
		if v == nil {
			st = StDim
		}
		cv.Put(y, cx, clip(text, max(0, w-cx-1)), st)
		return
	}
	if dis {
		if r.Kind == KindButton {
			cv.Put(y, cx, "[ "+r.Text+" ]", StDim)
		} else {
			cv.Put(y, cx, "(not available)", StDim)
		}
		return
	}
	v := r.Value()
	a.hits = append(a.hits, hit{y, 0, w, r, zoneRow, 0})
	switch r.Kind {
	case KindToggle:
		s, st := "[ OFF ]", StOff
		switch {
		case v == nil:
			s, st = "[  …  ]", StDim
		case asBool(v):
			s, st = "[ ON  ]", StOn
		}
		cv.Put(y, cx, s, st)
		a.hits = append(a.hits, hit{y, cx, cx + width(s), r, zoneToggle, 0})
	case KindButton:
		s := "[ " + r.Text + " ]"
		st := StAccent
		if sel {
			st = StPill
		}
		cv.Put(y, cx, s, st)
		a.hits = append(a.hits, hit{y, cx, cx + width(s), r, zoneButton, 0})
	case KindChoice:
		a.drawChoice(r, sel, y, w, cx)
	case KindSlider:
		a.drawSlider(r, sel, y, w, cx)
	}
}

func (a *App) drawChoice(r *Row, sel bool, y, w, cx int) {
	cv := a.cv
	g := cv.Glyphs()
	opts := r.options()
	idx, known := r.Index()
	total := len(opts) - 1
	for _, o := range opts {
		total += width(o.Label) + 2
	}
	if total <= w-cx-2 && len(opts) > 0 {
		x := cx
		for i, o := range opts {
			cur := known && i == idx
			st := StDim
			if cur {
				st = StPill
				if r.Colors != nil {
					base := StAccent
					if c, ok := r.Colors[asString(o.Value)]; ok {
						base = c
					}
					st = base + selSuffix
				}
			}
			cv.Put(y, x, " "+o.Label+" ", st)
			a.hits = append(a.hits, hit{y, x, x + width(o.Label) + 2, r, zoneOpt, i})
			x += width(o.Label) + 3
		}
		return
	}
	lab := "…"
	if known && idx < len(opts) {
		lab = opts[idx].Label
	}
	s := g.ChoiceOpen + lab + g.ChoiceClose
	st := StAccent
	if sel {
		st = StPill
	}
	cv.Put(y, cx, s, st)
	a.hits = append(a.hits, hit{y, cx, cx + 2, r, zonePrev, 0}, hit{y, cx + width(s) - 2, cx + width(s), r, zoneNext, 0})
}

func (a *App) drawSlider(r *Row, sel bool, y, w, cx int) {
	cv := a.cv
	g := cv.Glyphs()
	vals := r.Values
	idx, known := r.Index()
	bw := max(8, min(36, w-cx-14))
	text := "…"
	if known {
		text = r.Show(vals[idx])
	}
	pos := 0
	if known {
		pos = roundEven(float64(idx) / float64(max(1, len(vals)-1)) * float64(bw-1))
	}
	for i := range bw {
		if known && i <= pos {
			cv.Put(y, cx+i, g.Rail, StBar)
		} else {
			cv.Put(y, cx+i, g.Thin, StBarOff)
		}
	}
	if known {
		st := StText
		if sel {
			st = StAccent
		}
		cv.Put(y, cx+pos, g.Knob, st)
	}
	st := StDim
	if known {
		st = StText
	}
	cv.Put(y, cx+bw+2, text, st)
	a.hits = append(a.hits, hit{y, cx, cx + bw, r, zoneBar, bw})
}
