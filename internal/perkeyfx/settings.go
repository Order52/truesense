package perkeyfx

import (
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/Order52/truesense/internal/firmware"
	"github.com/Order52/truesense/internal/keymap"
	"github.com/Order52/truesense/internal/userstate"
)

// stateKey is where the settings live in the user state; the key service and the clients
// (command line, TUI, window) meet there.
const stateKey = "perkey_fx"

// Settings is the animation switched on (or "off"). The JSON names are those 0.3.x wrote.
type Settings struct {
	Mode  string   `json:"mode"`
	Speed int      `json:"speed,omitempty"`
	Keys  []int    `json:"keys,omitempty"` // empty: every lit key
	Base  []string `json:"base,omitempty"` // RRGGBB of each cell: what the keys look like without the animation
	T0    float64  `json:"t0,omitempty"`   // start time, seconds since the epoch: the animation's clock runs from here
}

// CellSource gives the colours currently on the keyboard (the default base of an animation).
type CellSource interface {
	CurrentCells() []keymap.RGB
}

// Controller reads and writes the settings in the user state.
type Controller struct {
	State *userstate.Store
	Cells CellSource
	Now   func() time.Time
}

// NewController returns a Controller using the wall clock.
func NewController(state *userstate.Store, cells CellSource) *Controller {
	return &Controller{State: state, Cells: cells, Now: time.Now}
}

// Get returns the current settings ({mode: off} if none).
func (c *Controller) Get() Settings {
	var s Settings
	if c.State == nil || c.State.GetInto(stateKey, &s) != nil || s.Mode == "" {
		return Settings{Mode: "off"}
	}
	return s
}

// Seconds converts a time to the float seconds the animation clock uses.
func Seconds(t time.Time) float64 { return float64(t.UnixNano()) / 1e9 }

// Set switches an animation on, or "off". speed is 1..10; keys are the cells it applies to (none:
// every lit key); base is the colour of each cell without the animation (default: the
// colours now on the keyboard).
func (c *Controller) Set(mode string, speed int, keys []int, base []keymap.RGB) (Settings, error) {
	if mode != "off" && !IsMode(mode) {
		return Settings{}, firmware.Errorf("animation: off, %s", strings.Join(ModeNames(), ", "))
	}
	if speed < SpeedMin || speed > SpeedMax {
		return Settings{}, firmware.Errorf("speed %d..%d", SpeedMin, SpeedMax)
	}
	for _, k := range keys {
		if k < 0 || k >= keymap.NumCells {
			return Settings{}, firmware.Errorf("cell %d: 0..%d", k, keymap.NumCells-1)
		}
	}
	if len(base) == 0 && c.Cells != nil {
		base = c.Cells.CurrentCells()
	}
	hexes := make([]string, len(base))
	for i, b := range base {
		hexes[i] = b.Hex()
	}
	s := Settings{Mode: mode, Speed: speed, Keys: uniqueSorted(keys), Base: hexes, T0: Seconds(c.Now())}
	if c.State != nil {
		if err := c.State.Set(stateKey, s); err != nil {
			return Settings{}, fmt.Errorf("save animation settings: %w", err)
		}
	}
	return c.Get(), nil
}

func uniqueSorted(keys []int) []int {
	if len(keys) == 0 {
		return nil
	}
	seen := map[int]bool{}
	var out []int
	for _, k := range keys {
		if !seen[k] {
			seen[k] = true
			out = append(out, k)
		}
	}
	sort.Ints(out)
	return out
}

// parseBase reads the stored base colours; a corrupt entry makes the whole list unusable.
func parseBase(hex []string) ([]keymap.RGB, bool) {
	out := make([]keymap.RGB, len(hex))
	for i, h := range hex {
		c, err := keymap.ParseHex(h)
		if err != nil {
			return nil, false
		}
		out[i] = c
	}
	return out, true
}
