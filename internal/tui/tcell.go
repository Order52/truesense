package tui

import (
	"strings"

	"github.com/gdamore/tcell/v2"
)

// tcellCanvas draws on a tcell screen.
type tcellCanvas struct {
	s      tcell.Screen
	g      *Glyphs
	styles map[Style]tcell.Style
}

func newTCellCanvas(s tcell.Screen, unicode, color bool) *tcellCanvas {
	return &tcellCanvas{s: s, g: NewGlyphs(unicode), styles: palette(color)}
}

// Size implements Canvas.
func (c *tcellCanvas) Size() (int, int) { return c.s.Size() }

// Glyphs implements Canvas.
func (c *tcellCanvas) Glyphs() *Glyphs { return c.g }

// Clear implements Canvas.
func (c *tcellCanvas) Clear() { c.s.Clear() }

// Put implements Canvas; tcell clips to the screen.
func (c *tcellCanvas) Put(y, x int, text string, st Style) {
	if y < 0 {
		return
	}
	ts, ok := c.styles[st]
	if !ok {
		ts = tcell.StyleDefault
	}
	i := 0
	for _, r := range text {
		if x+i >= 0 {
			c.s.SetContent(x+i, y, r, nil, ts)
		}
		i++
	}
}

// palette maps style names to terminal looks: colours when the terminal has them, bold,
// dim and reverse when it does not (or NO_COLOR is set).
func palette(color bool) map[Style]tcell.Style {
	d := tcell.StyleDefault
	m := map[Style]tcell.Style{}
	if !color {
		m[StText], m[StDim], m[StAccent], m[StGood] = d, d.Dim(true), d.Bold(true), d
		m[StWarn], m[StBad], m[StSel], m[StPill] = d.Bold(true), d.Bold(true).Underline(true), d.Reverse(true), d.Reverse(true).Bold(true)
		m[StTab], m[StTabSel], m[StTitle], m[StKey] = d, d.Reverse(true).Bold(true), d.Bold(true), d.Bold(true)
		m[StHead], m[StOn], m[StOff], m[StBar], m[StBarOff] = d.Bold(true), d.Reverse(true), d.Dim(true), d.Bold(true), d.Dim(true)
		for _, st := range modeStyle {
			m[st], m[st+selSuffix] = d.Bold(true), d.Reverse(true).Bold(true)
		}
		return m
	}
	const black, red, green, yellow, blue, magenta, cyan = 0, 1, 2, 3, 4, 5, 6
	fg := func(n int) tcell.Style { return d.Foreground(tcell.PaletteColor(n)) }
	on := func(f, b int) tcell.Style {
		return d.Foreground(tcell.PaletteColor(f)).Background(tcell.PaletteColor(b))
	}
	m[StText], m[StDim], m[StAccent], m[StGood] = d, d.Dim(true), fg(cyan).Bold(true), fg(green)
	m[StWarn], m[StBad], m[StSel], m[StPill] = fg(yellow), fg(red).Bold(true), on(black, cyan), on(black, cyan).Bold(true)
	m[StTab], m[StTabSel], m[StTitle], m[StKey] = d.Dim(true), on(black, cyan).Bold(true), fg(cyan).Bold(true), fg(cyan)
	m[StHead], m[StOn], m[StOff], m[StBar], m[StBarOff] = fg(magenta).Bold(true), on(black, green).Bold(true), d.Dim(true), fg(cyan), d.Dim(true)
	for name, col := range map[string]int{"eco": green, "quiet": blue, "balanced": cyan, "performance": yellow, "turbo": red} {
		st := modeStyle[name]
		m[st], m[st+selSuffix] = fg(col).Bold(true), on(black, col).Bold(true)
	}
	return m
}

// unicodeTerminal reports whether the locale says UTF-8 and the terminal is not the bare
// Linux console (whose font lacks the box and block characters).
func unicodeTerminal(env func(string) string) bool {
	loc := ""
	for _, k := range []string{"LC_ALL", "LC_CTYPE", "LANG"} {
		if v := env(k); v != "" {
			loc = v
			break
		}
	}
	l := strings.ToLower(loc)
	return (strings.Contains(l, "utf-8") || strings.Contains(l, "utf8")) && env("TERM") != "linux"
}

// translateKey maps a tcell key event to the screen's keys; quit is true for Ctrl-C.
func translateKey(ev *tcell.EventKey) (k Key, quit, ok bool) {
	switch ev.Key() {
	case tcell.KeyRune:
		return Key(ev.Rune()), false, true
	case tcell.KeyCtrlC:
		return 0, true, true
	case tcell.KeyEnter:
		return KeyEnter, false, true
	case tcell.KeyTab:
		return KeyTab, false, true
	case tcell.KeyBacktab:
		return KeyBacktab, false, true
	case tcell.KeyEscape:
		return KeyEsc, false, true
	case tcell.KeyBackspace, tcell.KeyBackspace2:
		return KeyBackspace, false, true
	case tcell.KeyUp:
		return KeyUp, false, true
	case tcell.KeyDown:
		return KeyDown, false, true
	case tcell.KeyLeft:
		return KeyLeft, false, true
	case tcell.KeyRight:
		return KeyRight, false, true
	case tcell.KeyHome:
		return KeyHome, false, true
	case tcell.KeyEnd:
		return KeyEnd, false, true
	case tcell.KeyPgUp:
		return KeyPageUp, false, true
	case tcell.KeyPgDn:
		return KeyPageDown, false, true
	case tcell.KeyCtrlL:
		return KeyCtrlL, false, true
	}
	return 0, false, false
}

// mouseState turns tcell's button masks into press, drag and release.
type mouseState struct{ held bool }

func (m *mouseState) handle(a *App, ev *tcell.EventMouse) {
	x, y := ev.Position()
	b := ev.Buttons()
	switch {
	case b&tcell.WheelUp != 0:
		a.Mouse(x, y, -1, false)
		return
	case b&tcell.WheelDown != 0:
		a.Mouse(x, y, 1, false)
		return
	case b&tcell.Button1 != 0 && !m.held:
		a.Mouse(x, y, 0, true)
	case b&tcell.Button1 != 0:
		a.MouseMove(x, y)
	case m.held:
		a.MouseRelease()
	}
	m.held = b&tcell.Button1 != 0
}
