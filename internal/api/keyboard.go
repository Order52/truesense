package api

import (
	"context"
	"fmt"
	"sort"
	"strings"

	"github.com/Order52/truesense/internal/designs"
	"github.com/Order52/truesense/internal/keyboard"
	"github.com/Order52/truesense/internal/keymap"
	"github.com/Order52/truesense/internal/perkeyfx"
)

// KeyboardStatus is the backlight at a glance. Look is what this tool last put on the
// keyboard (the hardware cannot be asked), an empty object when unknown.
type KeyboardStatus struct {
	Look             any      `json:"look"`
	Brightness       int      `json:"brightness"`
	Max              int      `json:"max"`
	BacklightTimeout bool     `json:"backlight_timeout"`
	LightingProfiles []string `json:"lighting_profiles"`
}

// KeyboardStatus reads the keyboard backlight.
func (a *API) KeyboardStatus() (KeyboardStatus, error) {
	st := KeyboardStatus{Max: keyboard.MaxBright, Look: map[string]any{}}
	if last := a.s.KB.Last(); !last.IsZero() {
		st.Look = last
	}
	var err error
	if st.Brightness, err = a.s.KB.Brightness(); err != nil {
		return st, err
	}
	if st.BacklightTimeout, err = a.s.HW.GetKBTimeout(); err != nil {
		return st, err
	}
	st.LightingProfiles = nonNil(a.s.KeyProf.Names())
	return st, nil
}

// KeyboardPower switches the backlight on, off or (toggle) the other way and answers the
// message the key would show.
func (a *API) KeyboardPower(ctx context.Context, action string) (string, error) {
	return a.action(ctx, "keyboard:"+action)
}

// RestoreKeyboard puts the stock cyan per-key picture back, at the brightness the keyboard
// has (the user's Fn+F11/F12 level).
func (a *API) RestoreKeyboard() error {
	return a.s.KB.PerKey(keymap.Canvas(keymap.RGB{0x00, 0xAE, 0xC7}), keyboard.KeepBrightness)
}

// KeyNames is the key and group names the lighting commands accept.
type KeyNames struct {
	Keys   []string `json:"keys"`
	Groups []string `json:"groups"`
}

// ListKeys lists every key and group name, sorted.
func (a *API) ListKeys() KeyNames {
	return KeyNames{Keys: sortedKeys(a.km.Names()), Groups: sortedKeys(a.km.Groups())}
}

// KeyCell is one lighting cell of a key.
type KeyCell struct {
	Cell int `json:"cell"`
	Col  int `json:"col"`
	Row  int `json:"row"`
}

// KeyLayout is where each key's lighting cells are, for other tools.
type KeyLayout struct {
	Keys   map[string][]KeyCell `json:"keys"`
	Groups []string             `json:"groups"`
}

// KeyLayout returns the key map.
func (a *API) KeyLayout() KeyLayout {
	l := KeyLayout{Keys: map[string][]KeyCell{}, Groups: sortedKeys(a.km.Groups())}
	for name, cells := range a.km.Names() {
		for _, c := range cells {
			l.Keys[name] = append(l.Keys[name], KeyCell{Cell: c, Col: c / keymap.Rows, Row: c % keymap.Rows})
		}
	}
	return l
}

func sortedKeys[V any](m map[string]V) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

// EffectSpec is a hardware effect as the command line gives it. A nil Brightness keeps the
// keyboard's own level (or 30 when it is dark; 50 for neon).
type EffectSpec struct {
	Name       string
	Color      string
	Speed      int
	Brightness *int
	Direction  int
}

// SetKeyboardEffect puts a hardware effect on the keyboard. The pseudo effect
// "operatingmode" makes the colour follow the power mode.
func (a *API) SetKeyboardEffect(e EffectSpec) error {
	if e.Name == "operatingmode" {
		return a.s.KB.SetOperatingMode(e.Color, keyboard.OperatingBrightness)
	}
	var b int
	switch {
	case e.Brightness != nil:
		b = *e.Brightness
	case e.Name == "neon":
		b = 50
	default:
		b, _ = a.s.KB.Brightness()
		if b == 0 {
			b = keyboard.DefaultEffectBright
		}
	}
	return a.s.KB.SetEffect(e.Name, e.Color, e.Speed, b, e.Direction)
}

// SetKeyboardColor changes the colour of the current look without changing the effect.
func (a *API) SetKeyboardColor(color string) error { return a.s.KB.SetColor(color) }

// SetKeyboardSpeed changes the animation speed (1 slowest .. 12 fastest) of the running effect.
func (a *API) SetKeyboardSpeed(level int) error { return a.s.KB.SetSpeed(level) }

// KeyboardBrightness is the keyboard's own brightness (0..50).
func (a *API) KeyboardBrightness() (int, error) { return a.s.KB.Brightness() }

// SetKeyboardBrightness changes the brightness without leaving the current look.
func (a *API) SetKeyboardBrightness(level int) error { return a.s.KB.SetBrightness(level) }

// brightnessArg turns an optional brightness into what the keyboard takes.
func brightnessArg(b *int) int {
	if b == nil {
		return keyboard.KeepBrightness
	}
	return *b
}

// KeyColors parses "key=RRGGBB" items (a key or a group name, then a colour).
func KeyColors(items []string) ([]keymap.KeyColor, error) {
	out := make([]keymap.KeyColor, 0, len(items))
	for _, item := range items {
		k, col, _ := strings.Cut(item, "=")
		if col == "" {
			return nil, fmt.Errorf("'%s': use key=RRGGBB", item)
		}
		out = append(out, keymap.KeyColor{Key: k, Color: col})
	}
	return out, nil
}

// PaintKeys colours the named keys and groups and keeps every other key as it is. It answers
// how many names were painted.
func (a *API) PaintKeys(items []string, brightness *int) (int, error) {
	kc, err := KeyColors(items)
	if err != nil {
		return 0, err
	}
	return a.s.KB.Paint(a.km, kc, brightnessArg(brightness))
}

// SetKeyColors paints the named keys and groups over a base colour (everything else).
func (a *API) SetKeyColors(items []string, base string, brightness *int) error {
	kc, err := KeyColors(items)
	if err != nil {
		return err
	}
	b, err := keymap.ParseHex(base)
	if err != nil {
		return err
	}
	cv := keymap.Canvas(b)
	for _, it := range kc {
		cells, err := a.km.CellsFor(it.Key)
		if err != nil {
			return err
		}
		rgb, err := keymap.ParseHex(it.Color)
		if err != nil {
			return err
		}
		for _, c := range cells {
			cv[c] = rgb
		}
	}
	return a.s.KB.PerKey(cv, brightnessArg(brightness))
}

// FillKeyboard paints every key one colour.
func (a *API) FillKeyboard(color string, brightness *int) error {
	c, err := keymap.ParseHex(color)
	if err != nil {
		return err
	}
	return a.s.KB.PerKey(keymap.Canvas(c), brightnessArg(brightness))
}

// GradientKeyboard paints a left-to-right blend of two colours.
func (a *API) GradientKeyboard(from, to string, brightness *int) error {
	c1, err := keymap.ParseHex(from)
	if err != nil {
		return err
	}
	c2, err := keymap.ParseHex(to)
	if err != nil {
		return err
	}
	return a.s.KB.PerKey(keymap.Gradient(c1, c2), brightnessArg(brightness))
}

// RainbowKeyboard paints a rainbow across the keys.
func (a *API) RainbowKeyboard(brightness *int) error {
	return a.s.KB.PerKey(keymap.Rainbow(), brightnessArg(brightness))
}

// ImageKeyboard paints a picture file across the keys (scaled to the 22 x 6 key grid).
func (a *API) ImageKeyboard(path string, brightness *int) error {
	cells, err := designs.ImageCellsFile(path)
	if err != nil {
		return err
	}
	return a.s.KB.PerKey(cells, brightnessArg(brightness))
}

// DesignInfo is one still design of the lists.
type DesignInfo struct {
	Info   string `json:"info"`
	Source string `json:"source"`
}

// Designs lists every design (built-in, and yours which win), by name.
func (a *API) Designs() map[string]DesignInfo {
	out := map[string]DesignInfo{}
	for n, d := range a.s.Designs.LoadAll() {
		out[n] = DesignInfo{Info: d.Info, Source: d.Source}
	}
	return out
}

// ApplyDesign paints a design on the keyboard.
func (a *API) ApplyDesign(name string, brightness *int) error {
	if name == "" {
		return fmt.Errorf("give a design name")
	}
	d, err := a.s.Designs.Get(name)
	if err != nil {
		return err
	}
	cells, err := designs.CellsOf(a.km, d)
	if err != nil {
		return err
	}
	return a.s.KB.PerKey(cells, brightnessArg(brightness))
}

// SaveDesign stores the picture now on the keyboard as one of your designs and answers its file.
func (a *API) SaveDesign(name string) (string, error) {
	if name == "" {
		return "", fmt.Errorf("give a design name")
	}
	return a.s.Designs.Save(name, a.s.KB.CurrentCells())
}

// KeyboardFXSpec is a per-key animation: the mode ("off" stops it), its speed (1..10) and the
// comma list of keys or groups it covers (empty: every key).
type KeyboardFXSpec struct {
	Mode  string
	Speed int
	Keys  string
}

// SetKeyboardFX starts or stops a per-key animation. The key service draws it (about 5
// frames a second); this only records the choice, after uploading the picture it starts from.
func (a *API) SetKeyboardFX(spec KeyboardFXSpec) error {
	var cells []int
	if spec.Keys != "" {
		set := map[int]bool{}
		for _, k := range strings.Split(spec.Keys, ",") {
			cs, err := a.km.CellsFor(strings.TrimSpace(k))
			if err != nil {
				return err
			}
			for _, c := range cs {
				set[c] = true
			}
		}
		for c := range set {
			cells = append(cells, c)
		}
		sort.Ints(cells)
	}
	_, err := a.SetKeyboardFXCells(spec.Mode, spec.Speed, cells, nil)
	return err
}

// SetKeyboardFXCells is SetKeyboardFX with the keys as cell numbers (none: every key) and the
// colours under the animation given (nil: the picture now on the keyboard, uploaded first). It
// answers the settings stored.
func (a *API) SetKeyboardFXCells(mode string, speed int, cells []int, base []string) (perkeyfx.Settings, error) {
	var under []keymap.RGB
	if base != nil {
		var err error
		if under, err = parseCells(base); err != nil {
			return perkeyfx.Settings{}, err
		}
	} else if mode != "off" {
		if err := a.s.KB.PerKey(a.s.KB.CurrentCells(), keyboard.KeepBrightness); err != nil {
			return perkeyfx.Settings{}, err
		}
	}
	if mode == "off" && speed == 0 {
		speed = perkeyfx.SpeedMin // stopping needs no speed, but the settings keep a valid one
	}
	return perkeyfx.NewController(a.s.User, a.s.KB).Set(mode, speed, cells, under)
}

// OperatingFollow reports whether the keyboard colour follows the power mode (the service
// re-sends it on each change).
func (a *API) OperatingFollow() (bool, error) { return a.daemonBool("operating_follow") }

// SetOperatingFollow switches that and answers the value the service holds.
func (a *API) SetOperatingFollow(on bool) (bool, error) {
	return a.daemonSetBool("operating_follow", on)
}

// LightingProfileResult is the answer of a saved-lighting command: the names for "list", a
// message for the others.
type LightingProfileResult struct {
	Names   []string
	Message string
}

// LightingProfile runs `keyboard profile list|load|save|delete NAME [spec ...]`; spec is
// "fill COLOR", "gradient C1 C2" or "rainbow" for save.
func (a *API) LightingProfile(action, name string, spec []string) (LightingProfileResult, error) {
	names, msg, err := a.s.KeyProf.Command(action, name, spec)
	return LightingProfileResult{Names: nonNil(names), Message: msg}, err
}
