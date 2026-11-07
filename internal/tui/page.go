package tui

// Page is one screen: its rows, the slow reads that fill them, and an optional read-only
// panel drawn under the rows (the dashboard).
type Page struct {
	name   string
	rows   []*Row
	reads  []Read
	extra  func(y, h, w int)
	sel    int
	scroll int
}

// pageNames are the tabs in order.
var pageNames = [pageCount]string{"Dashboard", "Fans", "Power", "Lighting", "Audio", "Profiles", "System"}

// PageNames lists the pages in tab order.
func PageNames() []string { return append([]string(nil), pageNames[:]...) }

func newPage(name string, rows []*Row, reads []Read, extra func(y, h, w int)) *Page {
	p := &Page{name: name, rows: rows, reads: reads, extra: extra}
	for i, r := range rows {
		if r.Selectable() {
			p.sel = i
			break
		}
	}
	return p
}

// init gives every row the app's clock.
func (p *Page) init(now Clock) {
	for _, r := range p.rows {
		r.now = now
	}
}

// move selects the next selectable row in direction d, wrapping.
func (p *Page) move(d int) {
	n := len(p.rows)
	i := p.sel
	for range n {
		i = ((i+d)%n + n) % n
		if p.rows[i].Selectable() {
			p.sel = i
			return
		}
	}
}

// ---- row constructors

func choiceRow(label string, options []Option, get func() any, put func(any), r Row) *Row {
	r.Kind, r.Label, r.Get, r.Put, r.Options = KindChoice, label, get, put, options
	return &r
}

func toggleRow(label string, get func() any, put func(any), r Row) *Row {
	r.Kind, r.Label, r.Get, r.Put = KindToggle, label, get, put
	return &r
}

func sliderRow(label string, values []int, get func() any, put func(any), r Row) *Row {
	r.Kind, r.Label, r.Get, r.Put, r.Values = KindSlider, label, get, put, values
	return &r
}

func buttonRow(label, text string, press func(), r Row) *Row {
	r.Kind, r.Label, r.Press, r.Text = KindButton, label, press, text
	return &r
}

func infoRow(label string, get func() any, r Row) *Row {
	r.Kind, r.Label, r.Get = KindInfo, label, get
	return &r
}

func headerRow(text string) *Row { return &Row{Kind: KindHeader, Label: text} }
