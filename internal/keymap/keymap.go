// Package keymap maps key names to the lighting cells of the Predator Helios 16 (PH16-71)
// keyboard and builds whole-keyboard colour pictures (fill, gradient, rainbow).
//
// A cell is numbered column*6 + row (22 columns, 6 rows, row 0 on top). The map comes from
// the owner-verified capture embedded below: each cell was lit and the key the owner pressed
// was recorded. Corrections against the community map, verified by key presses: cell 2 is
// Esc, cell 5 is CapsLock, cell 70 is the main-row minus.
package keymap

import (
	_ "embed"
	"encoding/json"
	"fmt"
	"math"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"sync"

	"github.com/Order52/truesense/internal/firmware"
)

const (
	// NumCells is the length of every colour list the keyboard takes (4 bytes per cell, 8 reports of 64 bytes).
	NumCells = 128
	// LitCells is how many cells the vendor's Windows application lights (0..125); the last two stay black.
	LitCells = 126
	// Columns is the number of key columns; Rows the number of rows.
	Columns = 22
	// Rows is the number of key rows.
	Rows = 6
)

// KeyColor pairs a key or group name with a colour. Lists of these keep the order the user gave,
// which matters when two names cover the same cell: the later one wins.
type KeyColor struct {
	Key   string
	Color string // RRGGBB
}

// RGB is one colour, red green blue.
type RGB [3]uint8

// Hex returns the colour as lower-case RRGGBB, the form every saved file and state uses.
func (c RGB) Hex() string { return fmt.Sprintf("%02x%02x%02x", c[0], c[1], c[2]) }

var hex6 = regexp.MustCompile(`^[0-9a-fA-F]{6}$`)

// ParseHex reads RRGGBB (any case, any number of leading '#', as the Python tool accepted).
func ParseHex(s string) (RGB, error) {
	t := strings.TrimLeft(s, "#")
	if !hex6.MatchString(t) {
		return RGB{}, firmware.Errorf("bad colour '%s', use RRGGBB", t)
	}
	var c RGB
	for i := range c {
		v, _ := strconv.ParseUint(t[2*i:2*i+2], 16, 8) // validated by the pattern above
		c[i] = uint8(v)
	}
	return c, nil
}

//go:embed keymap_ph16-71_verified.json
var verifiedJSON []byte

// override fixes the cells the capture labelled wrongly (see the package comment).
var override = map[int]string{2: "esc", 5: "caps", 70: "minus"}

// alias turns the capture's truncated community labels into the names users type.
var alias = map[string]string{
	"oemminus": "minus", "capsl": "caps", "lshif": "lshift", "rshif": "rshift", "windo": "win", "optio": "menu", "retur": "enter", "backs": "backspace",
	"preda": "predator", "forwa": "next", "backw": "prev", "playp": "playpause", "numlk": "numlock", "numen": "numenter", "num.": "num.", "oemplus": "=",
}

// Map is the key-name table: single keys and named groups, each a list of cells.
type Map struct {
	names  map[string][]int
	groups map[string][]int
}

// verified is parsed once; a Map is immutable and its accessors hand out copies.
var verified = sync.OnceValue(func() *Map {
	m, err := parseMap(verifiedJSON)
	if err != nil {
		panic("keymap: embedded map is corrupt: " + err.Error()) // a constant of the binary; a test parses it
	}
	return m
})

// Verified returns the PH16-71 map.
func Verified() *Map { return verified() }

func parseMap(data []byte) (*Map, error) {
	var d struct {
		Cells map[string]struct {
			Label *string `json:"label"`
		} `json:"cells"`
	}
	if err := json.Unmarshal(data, &d); err != nil {
		return nil, fmt.Errorf("parse keymap: %w", err)
	}
	ids := make([]int, 0, len(d.Cells))
	label := map[int]string{}
	for k, v := range d.Cells {
		i, err := strconv.Atoi(k)
		if err != nil || i < 0 || i >= NumCells {
			return nil, fmt.Errorf("parse keymap: bad cell %q", k)
		}
		ids = append(ids, i)
		if v.Label != nil {
			label[i] = *v.Label
		}
	}
	sort.Ints(ids) // the capture lists the cells in ascending order; cell order inside a name follows it
	m := &Map{names: map[string][]int{}}
	for _, i := range ids {
		n, ok := override[i]
		if !ok {
			n = normLabel(label[i])
		}
		if n != "" {
			m.names[n] = append(m.names[n], i)
		}
	}
	for _, p := range [][2]string{{"enter", "return"}, {"win", "windows"}} { // friendlier names
		if cells, ok := m.names[p[1]]; ok {
			if _, have := m.names[p[0]]; !have {
				m.names[p[0]] = cells
			}
		}
	}
	m.groups = m.buildGroups()
	return m, nil
}

func normLabel(l string) string {
	l = strings.ToLower(l)
	if a, ok := alias[l]; ok {
		return a
	}
	return l
}

func (m *Map) join(names []string) []int {
	var out []int
	for _, n := range names {
		out = append(out, m.names[n]...)
	}
	return out
}

func (m *Map) buildGroups() map[string][]int {
	letters := make([]string, 0, 26)
	for c := 'a'; c <= 'z'; c++ {
		letters = append(letters, string(c))
	}
	fkeys := make([]string, 0, 12)
	for i := 1; i <= 12; i++ {
		fkeys = append(fkeys, "f"+strconv.Itoa(i))
	}
	var numpad []string
	for n := range m.names {
		if strings.HasPrefix(n, "num") {
			numpad = append(numpad, n)
		}
	}
	sort.Strings(numpad) // Python walked its dict in insertion order; sorted names are the deterministic equivalent
	all := make([]int, LitCells)
	for i := range all {
		all[i] = i
	}
	return map[string][]int{
		"wasd":    m.join([]string{"w", "a", "s", "d"}),
		"arrows":  m.join([]string{"up", "down", "left", "right"}),
		"fkeys":   m.join(fkeys),
		"numrow":  m.join([]string{"1", "2", "3", "4", "5", "6", "7", "8", "9", "0"}),
		"letters": m.join(letters),
		"numpad":  m.join(numpad),
		"all":     all,
	}
}

// Names returns every single-key name with its cells (a copy).
func (m *Map) Names() map[string][]int {
	out := make(map[string][]int, len(m.names))
	for k, v := range m.names {
		out[k] = append([]int(nil), v...)
	}
	return out
}

// Groups returns the named groups (wasd, arrows, fkeys, numrow, letters, numpad, all) with their cells (a copy).
func (m *Map) Groups() map[string][]int {
	out := make(map[string][]int, len(m.groups))
	for k, v := range m.groups {
		out[k] = append([]int(nil), v...)
	}
	return out
}

// Key returns the cells of one key name (not a group), or nil.
func (m *Map) Key(name string) []int { return append([]int(nil), m.names[strings.ToLower(name)]...) }

var cellName = regexp.MustCompile(`^cell(\d+)$`)

// CellsFor resolves a group, a key name or "cellN" (N < 128) to cells.
func (m *Map) CellsFor(name string) ([]int, error) {
	n := strings.ToLower(name)
	if g, ok := m.groups[n]; ok {
		return append([]int(nil), g...), nil
	}
	if k, ok := m.names[n]; ok {
		return append([]int(nil), k...), nil
	}
	if sub := cellName.FindStringSubmatch(n); sub != nil {
		if c, err := strconv.Atoi(sub[1]); err == nil && c < NumCells {
			return []int{c}, nil
		}
	}
	return nil, firmware.Errorf("unknown key '%s' (see: truesense keyboard list-keys)", name)
}

// pyRound is Python's round(): halves go to the even neighbour, which the saved frames of
// 0.3.x were computed with.
func pyRound(x float64) int { return int(math.RoundToEven(x)) }

// channel rounds like Python's round() and clamps to a colour byte.
func channel(f float64) uint8 {
	r := pyRound(f)
	if r < 0 {
		return 0
	}
	if r > 255 {
		return 255
	}
	return uint8(r)
}

// Lerp blends two colours (t = 0 gives a, t = 1 gives b).
func Lerp(a, b RGB, t float64) RGB {
	var c RGB
	for i := range c {
		c[i] = channel(float64(a[i]) + (float64(b[i])-float64(a[i]))*t)
	}
	return c
}

// pyMod is Python's float modulo: the result takes the sign of the divisor.
func pyMod(x, y float64) float64 {
	r := math.Mod(x, y)
	if r != 0 && (r < 0) != (y < 0) {
		r += y
	}
	return r
}

// HSV returns the fully saturated colour at hue h (0..1, wraps).
func HSV(h float64) RGB {
	h = pyMod(h, 1.0)
	i := int(h * 6)
	f := h*6 - float64(i)
	q := 1 - f
	var r, g, b float64
	switch i % 6 {
	case 0:
		r, g, b = 1, f, 0
	case 1:
		r, g, b = q, 1, 0
	case 2:
		r, g, b = 0, 1, f
	case 3:
		r, g, b = 0, q, 1
	case 4:
		r, g, b = f, 0, 1
	default:
		r, g, b = 1, 0, q
	}
	return RGB{channel(r * 255), channel(g * 255), channel(b * 255)}
}

// Canvas is a full colour list (128 cells) with every lit cell set to base and the two unused cells black.
func Canvas(base RGB) []RGB {
	cv := make([]RGB, NumCells)
	for i := 0; i < LitCells; i++ {
		cv[i] = base
	}
	return cv
}

// Gradient is a left-to-right blend over the 22 columns (cell / 6).
func Gradient(c1, c2 RGB) []RGB {
	cv := make([]RGB, NumCells)
	for i := 0; i < LitCells; i++ {
		cv[i] = Lerp(c1, c2, float64(i/Rows)/float64(Columns-1))
	}
	return cv
}

// Rainbow spreads the hues over the 22 columns.
func Rainbow() []RGB {
	cv := make([]RGB, NumCells)
	for i := 0; i < LitCells; i++ {
		cv[i] = HSV(float64(i/Rows) / float64(Columns))
	}
	return cv
}
