package tui

import (
	"strings"
	"unicode/utf8"
)

// Style names a look ("good", "warn", "pill" ...). The drawing code speaks in names and each
// canvas decides what they mean, so the tests can assert that a value is drawn as good.
type Style string

// The styles the screen uses.
const (
	StText    Style = "text"
	StDim     Style = "dim"
	StAccent  Style = "accent"
	StGood    Style = "good"
	StWarn    Style = "warn"
	StBad     Style = "bad"
	StSel     Style = "sel"
	StPill    Style = "pill"
	StTab     Style = "tab"
	StTabSel  Style = "tabsel"
	StTitle   Style = "title"
	StKey     Style = "key"
	StHead    Style = "head"
	StOn      Style = "on"
	StOff     Style = "off"
	StBar     Style = "bar"
	StBarOff  Style = "bar_off"
	selSuffix       = "_sel"
)

// Glyphs are the characters of the frame, rails and sparklines; plain ASCII keeps a bare
// console (and any terminal without Unicode) usable.
type Glyphs struct {
	Unicode bool
	// Frame and bars.
	H, V, TL, TR, BL, BR     string
	Full, Empty, Knob, Rail  string
	Thin, Dot, Up, Down      string
	Bolt                     string
	Spark                    []rune
	Cursor, Waiting, Writing string
	// ChoiceOpen and ChoiceClose frame a choice that is shown as one value.
	ChoiceOpen, ChoiceClose string
	// UpDown and LeftRight name the arrow keys in the footer; HelpUpDown and HelpLeftRight in the help.
	UpDown, LeftRight, HelpUpDown, HelpLeftRight string
}

// NewGlyphs returns the Unicode or the plain ASCII set.
func NewGlyphs(unicode bool) *Glyphs {
	if unicode {
		return &Glyphs{Unicode: true, H: "─", V: "│", TL: "╭", TR: "╮", BL: "╰", BR: "╯", Full: "█", Empty: "░", Knob: "●", Rail: "━", Thin: "─",
			Spark: []rune(" ▁▂▃▄▅▆▇█"), Dot: "●", Up: "▲", Down: "▼", Bolt: "↯", Cursor: "▸", Waiting: "↵", Writing: "…",
			ChoiceOpen: "‹ ", ChoiceClose: " ›", UpDown: "↑↓", LeftRight: "←→", HelpUpDown: "↑ ↓", HelpLeftRight: "← →"}
	}
	return &Glyphs{H: "-", V: "|", TL: "+", TR: "+", BL: "+", BR: "+", Full: "#", Empty: ".", Knob: "o", Rail: "=", Thin: "-",
		Spark: []rune(" .:-=+*#%"), Dot: "*", Up: "^", Down: "v", Bolt: "AC", Cursor: ">", Waiting: ">", Writing: "..",
		ChoiceOpen: "< ", ChoiceClose: " >", UpDown: "Up/Dn", LeftRight: "L/R", HelpUpDown: "Up Down", HelpLeftRight: "Left Right"}
}

// Canvas is something text can be put on. Put clips to the screen, so callers never worry
// about the edges.
type Canvas interface {
	Size() (w, h int)
	Glyphs() *Glyphs
	Clear()
	Put(y, x int, text string, st Style)
}

// width counts cells of a string (every glyph the screen uses is one cell wide).
func width(s string) int { return utf8.RuneCountInString(s) }

// clip cuts a string to n cells.
func clip(s string, n int) string {
	if n <= 0 {
		return ""
	}
	if utf8.RuneCountInString(s) <= n {
		return s
	}
	return string([]rune(s)[:n])
}

// fillRect paints w blanks (or ch) from (y, x).
func fillRect(c Canvas, y, x, w int, st Style, ch string) {
	if w > 0 {
		c.Put(y, x, strings.Repeat(ch, w), st)
	}
}

// drawBox draws a rounded frame with an optional title.
func drawBox(c Canvas, y, x, h, w int, title string, st, tst Style) {
	g := c.Glyphs()
	if h < 2 || w < 4 {
		return
	}
	c.Put(y, x, g.TL+strings.Repeat(g.H, w-2)+g.TR, st)
	c.Put(y+h-1, x, g.BL+strings.Repeat(g.H, w-2)+g.BR, st)
	for i := 1; i < h-1; i++ {
		c.Put(y+i, x, g.V, st)
		c.Put(y+i, x+w-1, g.V, st)
	}
	if title != "" {
		c.Put(y, x+2, " "+title+" ", tst)
	}
}

type cell struct {
	r  rune
	st Style
}

// GridCanvas is a text screen in memory: what the tests (and the golden snapshots) read.
type GridCanvas struct {
	w, h   int
	cells  [][]cell
	glyphs *Glyphs
}

// NewGridCanvas makes a blank w x h screen.
func NewGridCanvas(w, h int, unicode bool) *GridCanvas {
	g := &GridCanvas{glyphs: NewGlyphs(unicode)}
	g.Resize(w, h)
	return g
}

// Resize changes the size and blanks the screen.
func (g *GridCanvas) Resize(w, h int) { g.w, g.h = w, h; g.Clear() }

// Size implements Canvas.
func (g *GridCanvas) Size() (int, int) { return g.w, g.h }

// Glyphs implements Canvas.
func (g *GridCanvas) Glyphs() *Glyphs { return g.glyphs }

// Clear implements Canvas.
func (g *GridCanvas) Clear() {
	g.cells = make([][]cell, g.h)
	for y := range g.cells {
		row := make([]cell, g.w)
		for x := range row {
			row[x] = cell{' ', StText}
		}
		g.cells[y] = row
	}
}

// Put implements Canvas.
func (g *GridCanvas) Put(y, x int, text string, st Style) {
	if y < 0 || y >= g.h {
		return
	}
	i := 0
	for _, r := range text {
		if cx := x + i; cx >= 0 && cx < g.w {
			g.cells[y][cx] = cell{r, st}
		}
		i++
	}
}

// Text is the whole screen as lines without trailing blanks.
func (g *GridCanvas) Text() string {
	lines := make([]string, g.h)
	for y, row := range g.cells {
		var b strings.Builder
		for _, c := range row {
			b.WriteRune(c.r)
		}
		lines[y] = strings.TrimRight(b.String(), " ")
	}
	return strings.Join(lines, "\n")
}

// Lines is Text split into lines.
func (g *GridCanvas) Lines() []string { return strings.Split(g.Text(), "\n") }

// StyleAt is the style of one cell.
func (g *GridCanvas) StyleAt(y, x int) Style { return g.cells[y][x].st }
