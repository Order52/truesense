package keyboard

import (
	"strings"

	"github.com/Order52/truesense/internal/keymap"
)

// Look modes stored in State.Mode.
const (
	ModeOff       = "off"
	ModeEffect    = "effect"
	ModeOperating = "operating"
	ModePerKey    = "perkey"
)

// State is what this tool last put on the keyboard. The hardware cannot be asked which effect
// is running, so the tool remembers it (in the user state, and inside saved looks), and the
// JSON field names are the ones 0.3.x wrote: old files must keep loading.
//
// Numbers are pointers because "absent" and "0" differ: a missing speed means the default of
// 5, while a brightness of 0 is a real (dark) setting. The Or methods apply the defaults.
type State struct {
	Mode       string   `json:"mode"`
	Name       string   `json:"name,omitempty"`
	Color      string   `json:"color,omitempty"`
	Speed      *int     `json:"speed,omitempty"`
	Brightness *int     `json:"brightness,omitempty"`
	Direction  *int     `json:"direction,omitempty"`
	Cells      []string `json:"cells,omitempty"`
}

// IsZero reports an unknown look (nothing remembered).
func (s State) IsZero() bool {
	return s.Mode == "" && s.Name == "" && s.Color == "" && s.Speed == nil && s.Brightness == nil && s.Direction == nil && len(s.Cells) == 0
}

// SpeedOr returns the speed, or def if none was stored.
func (s State) SpeedOr(def int) int { return orInt(s.Speed, def) }

// BrightnessOr returns the brightness, or def if none was stored.
func (s State) BrightnessOr(def int) int { return orInt(s.Brightness, def) }

// DirectionOr returns the direction, or def if none was stored.
func (s State) DirectionOr(def int) int { return orInt(s.Direction, def) }

// ColorOr returns the stored colour, or def if none was stored.
func (s State) ColorOr(def string) string {
	if s.Color == "" {
		return def
	}
	return s.Color
}

func orInt(p *int, def int) int {
	if p == nil {
		return def
	}
	return *p
}

// Int returns a pointer to n, for building a State.
func Int(n int) *int { return &n }

// cellsHex turns the first 128 colours into the saved form, padding with black.
func cellsHex(cells []keymap.RGB) []string {
	out := make([]string, keymap.NumCells)
	for i := range out {
		if i < len(cells) {
			out[i] = cells[i].Hex()
		} else {
			out[i] = "000000"
		}
	}
	return out
}

// parseCells reads saved per-key colours; a malformed entry is an error rather than a
// silently wrong colour.
func parseCells(hex []string) ([]keymap.RGB, error) {
	out := make([]keymap.RGB, len(hex))
	for i, h := range hex {
		c, err := keymap.ParseHex(h)
		if err != nil {
			return nil, err
		}
		out[i] = c
	}
	return out, nil
}

func normColor(c string) string { return strings.ToLower(strings.TrimLeft(c, "#")) }
