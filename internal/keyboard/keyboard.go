// Package keyboard drives the keyboard backlight of the Predator Helios 16: the Chicony
// 04F2:0117 controller's vendor interface (interface 3, usage page 0xFF02) over hidraw
// feature and output reports (docs/PROTOCOL.md).
//
// Packets are 8 bytes; the last is the checksum 0xFF - (sum of bytes 0..6) & 0xFF, sent with
// HIDIOCSFEATURE. The hardware cannot be asked which effect is running, so every look this
// tool applies is remembered in the user state (Last) and brightness, speed and colour
// changes are re-sent on top of it.
//
// The HID device sits behind Transport so the whole controller runs unchanged against the
// real hidraw node, the in-memory simulator (TRUESENSE_SIM) and the recording fake of the tests.
package keyboard

import (
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/Order52/truesense/internal/firmware"
	"github.com/Order52/truesense/internal/keymap"
	"github.com/Order52/truesense/internal/userstate"
)

// Brightness and animation speed limits.
const (
	// MaxBright is the keyboard's brightness range, 0..50.
	MaxBright = 50
	// SpeedMin and SpeedMax are the UI scale: 1 = slowest .. 12 = fastest. The keyboard stores
	// the speed byte 0 (fastest) .. 11 (slowest) and reads it back (byte 3 of the 0x88 report);
	// 12 and above are refused and become 6 (measured on the PH16-71), so the byte is
	// SpeedMax - speed. Before 0.3.10 the scale was 1..11 with byte = 10 - speed: the two
	// slowest steps were unreachable and the top setting sent 0xFF, which the keyboard turns
	// into a medium speed.
	SpeedMin = 1
	SpeedMax = 12
)

// Defaults the Python tool used where a caller gave nothing.
const (
	// DefaultColor, DefaultSpeed and DefaultEffectBright fill in what a caller (or a saved look) left out.
	DefaultColor        = "ffffff"
	DefaultSpeed        = 5
	DefaultEffectBright = 30
	// DefaultBrightness is used instead of 0: never upload into darkness.
	DefaultBrightness = 25
	// OperatingColor and OperatingBrightness are the OPERATINGMODE defaults (the stock cyan, full brightness).
	OperatingColor      = "00aec7"
	OperatingBrightness = 0x32

	factoryBrightness   = 12
	operatingModePseudo = "operatingmode"
	userKeyboard        = "keyboard"
	userRestore         = "kb_restore"
)

// KeepBrightness asks PerKey to keep whatever brightness the keyboard has (the user's Fn+F11/F12 setting).
const KeepBrightness = -1

// effectNames are the hardware effects in the order users see them.
var effectNames = []string{"off", "static", "breathing", "wave", "snake", "ripple", "neon", "raindrop", "lightning", "spot", "star", "fireball", "snow", "heartbeat"}

var effectCodes = map[string]byte{"off": 0, "static": 1, "breathing": 2, "wave": 3, "snake": 5, "ripple": 6, "neon": 8, "raindrop": 10, "lightning": 18,
	"spot": 37, "star": 38, "fireball": 39, "snow": 40, "heartbeat": 41}

// noColor lists the effects that ignore the colour packet.
var noColor = map[string]bool{"off": true, "wave": true, "neon": true}

// EffectNames lists the effects in display order.
func EffectNames() []string { return append([]string(nil), effectNames...) }

func knownEffect(name string) bool { _, ok := effectCodes[name]; return ok }

// EffectCode returns the hardware code of an effect.
func EffectCode(name string) (byte, bool) { c, ok := effectCodes[name]; return c, ok }

// HasColor reports whether the effect takes a colour (wave and neon do not).
func HasColor(name string) bool { return !noColor[name] }

// OperatingColors are the defaults for lighting that follows the power mode. The vendor's
// per-mode colours are not known: these are OURS, overridable in config [keyboard.operating_colors].
func OperatingColors() map[string]string {
	return map[string]string{"eco": "00cc66", "quiet": "3366ff", "balanced": "00aec7", "performance": "ff9900", "turbo": "ff0000"}
}

// Option configures New.
type Option func(*Keyboard)

// WithSleep replaces time.Sleep, which paces the per-key frames; tests pass a recorder.
func WithSleep(f func(time.Duration)) Option { return func(k *Keyboard) { k.sleep = f } }

// Keyboard is the keyboard backlight controller. It is safe for concurrent use: every
// sequence of transfers runs under one lock, so a brightness read can never land in the
// middle of a frame upload.
type Keyboard struct {
	open  Opener
	state *userstate.Store // may be nil: then nothing is remembered
	sleep func(time.Duration)

	mu sync.Mutex
	t  Transport
}

// New returns a controller that opens its device through open. The device is opened
// lazily and kept: opening the interface costs about 50 ms on the PH16-71 (the USB transfers
// themselves take 0.3 ms), and a stale handle (the keyboard re-enumerates after suspend)
// is dropped and reopened once.
func New(open Opener, state *userstate.Store, opts ...Option) *Keyboard {
	k := &Keyboard{open: open, state: state, sleep: time.Sleep}
	for _, o := range opts {
		o(k)
	}
	return k
}

// call runs fn on the open transport under the lock; one reopen-and-retry if the handle has
// gone stale. fn must not call call.
func (k *Keyboard) call(fn func(Transport) error) error {
	k.mu.Lock()
	defer k.mu.Unlock()
	for attempt := 0; ; attempt++ {
		if k.t == nil {
			t, err := k.open()
			if err != nil {
				return err
			}
			k.t = t
		}
		err := fn(k.t)
		if err == nil {
			return nil
		}
		k.dropLocked()
		if attempt == 1 {
			return err
		}
	}
}

func (k *Keyboard) dropLocked() {
	if k.t != nil {
		_ = k.t.Close() // already failing: a close error adds nothing
		k.t = nil
	}
}

// Close releases the device. The controller reopens it if used again.
func (k *Keyboard) Close() error {
	k.mu.Lock()
	defer k.mu.Unlock()
	if k.t == nil {
		return nil
	}
	err := k.t.Close()
	k.t = nil
	return err
}

// Prewarm opens the device in the background (about 50 ms): call it early in a process that
// will talk to the keyboard soon, so that time overlaps other work. The channel is closed
// when the attempt is over; failures are left for the first real call to report.
func (k *Keyboard) Prewarm() <-chan struct{} {
	done := make(chan struct{})
	go func() {
		defer close(done)
		_ = k.call(func(Transport) error { return nil })
	}()
	return done
}

func checksum(p []byte) byte {
	var sum byte
	for _, b := range p {
		sum += b
	}
	return 0xFF - sum
}

// pkt builds an 8-byte packet from its seven data bytes.
func pkt(b ...byte) []byte { return append(b, checksum(b)) }

func (k *Keyboard) send(pkts ...[]byte) error {
	return k.call(func(t Transport) error {
		for _, p := range pkts {
			if err := t.SetFeature(p); err != nil {
				return fmt.Errorf("send feature report: %w", err)
			}
		}
		return nil
	})
}

func (k *Keyboard) remember(key string, v any) {
	if k.state != nil {
		_ = k.state.Set(key, v) // a convenience file: failing to write it never fails the light
	}
}

func hexc(c string) (keymap.RGB, error) {
	rgb, err := keymap.ParseHex(c)
	if err != nil {
		return keymap.RGB{}, firmware.Errorf("colour must be RRGGBB")
	}
	return rgb, nil
}

// SetEffect puts a hardware effect on the keyboard. The pseudo effect "operatingmode" hands
// over to SetOperatingMode (color is then the only argument used).
func (k *Keyboard) SetEffect(name, color string, speed, brightness, direction int) error {
	if name == operatingModePseudo {
		return k.SetOperatingMode(color, OperatingBrightness)
	}
	code, ok := effectCodes[name]
	if !ok {
		return firmware.Errorf("effect: %s", strings.Join(effectNames, ", "))
	}
	if speed < SpeedMin || speed > SpeedMax || brightness < 0 || brightness > MaxBright {
		return firmware.Errorf("speed %d..%d, brightness 0..%d", SpeedMin, SpeedMax, MaxBright)
	}
	if direction < 0 || direction > 254 {
		return firmware.Errorf("direction 0..254")
	}
	var rgb keymap.RGB
	if !noColor[name] {
		var err error
		if rgb, err = hexc(color); err != nil {
			return err
		}
	}
	prelude := []byte{0xB1, 0, 0, 0, 0, 0, 0, 0x4E}
	if name == "off" {
		if err := k.send(prelude, pkt(0x08, 0x02, 0, 0, 0, 0, 0), pkt(0x14, 0, 0, 0, 0, 0, 0)); err != nil {
			return err
		}
		k.remember(userKeyboard, State{Mode: ModeOff})
		return nil
	}
	err := k.send(prelude, pkt(0x14, 0, 0, rgb[0], rgb[1], rgb[2], 0),
		pkt(0x08, 0x02, code, byte(SpeedMax-speed), byte(brightness), 0x01, byte(direction+1)))
	if err != nil {
		return err
	}
	k.remember(userKeyboard, State{Mode: ModeEffect, Name: name, Color: normColor(color), Speed: &speed, Brightness: &brightness, Direction: &direction})
	return nil
}

// SetOperatingMode selects OPERATINGMODE (0x22): the keyboard colour reflects the power mode
// (docs/PROTOCOL.md: no prelude; colour packet 14 00 01 R G B 00 chk; effect 08 03 22 05 32 02 01 chk).
func (k *Keyboard) SetOperatingMode(color string, brightness int) error {
	rgb, err := hexc(color)
	if err != nil {
		return err
	}
	if brightness < 0 || brightness > 255 {
		return firmware.Errorf("brightness 0..255")
	}
	if err := k.send(pkt(0x14, 0, 0x01, rgb[0], rgb[1], rgb[2], 0), pkt(0x08, 0x03, 0x22, 0x05, byte(brightness), 0x02, 0x01)); err != nil {
		return err
	}
	k.remember(userKeyboard, State{Mode: ModeOperating, Color: normColor(color), Brightness: &brightness})
	return nil
}

// FollowMode makes operating-mode lighting track the power mode: the keyboard does not do it
// by itself (verified), so the colour is re-sent on each change. colors overrides OperatingColors.
func (k *Keyboard) FollowMode(mode string, colors map[string]string) error {
	merged := OperatingColors()
	for m, c := range colors {
		merged[m] = c
	}
	c, ok := merged[mode]
	if !ok {
		c = OperatingColor
	}
	return k.SetOperatingMode(c, OperatingBrightness)
}

// Last returns what this tool last put on the keyboard (zero State if unknown).
func (k *Keyboard) Last() State { return k.getState(userKeyboard) }

func (k *Keyboard) getState(key string) State {
	var s State
	if k.state == nil || k.state.GetInto(key, &s) != nil {
		return State{}
	}
	return s
}

// Brightness is the keyboard's current brightness (0..50) as the firmware reports it: byte 4 of
// the GET_REPORT after the 0x88 packet (verified: it tracks writes, including Fn+F11/F12).
func (k *Keyboard) Brightness() (int, error) {
	var level int
	err := k.call(func(t Transport) error {
		r, err := queryReport(t, 0x88, 0x77)
		if err != nil {
			return err
		}
		level = int(r[4])
		return nil
	})
	if err != nil {
		return 0, fmt.Errorf("read keyboard brightness: %w", err)
	}
	return level, nil
}

// queryReport sends a request packet (command, checksum) and reads the answer.
func queryReport(t Transport, cmd, chk byte) ([]byte, error) {
	if err := t.SetFeature([]byte{cmd, 0, 0, 0, 0, 0, 0, chk}); err != nil {
		return nil, fmt.Errorf("send feature report: %w", err)
	}
	r, err := t.GetFeature(PacketLen)
	if err != nil {
		return nil, fmt.Errorf("get feature report: %w", err)
	}
	if len(r) < PacketLen {
		return nil, fmt.Errorf("short feature report (%d bytes)", len(r))
	}
	return r, nil
}

func (k *Keyboard) brightnessOrDefault() (int, error) {
	b, err := k.Brightness()
	if err != nil {
		return 0, err
	}
	if b == 0 {
		return DefaultBrightness, nil
	}
	return b, nil
}

// PerKey uploads a direct per-key picture (docs/PROTOCOL.md): 128 cells of 4 bytes (00 R G B)
// as 8 output reports of 64 bytes; cell = column*6 + row. Missing cells are black. brightness
// KeepBrightness keeps what the user set with Fn+F11/F12 (never uploading into darkness: 0 becomes 25).
// The picture is remembered as the current look.
func (k *Keyboard) PerKey(cells []keymap.RGB, brightness int) error {
	return k.perKey(cells, brightness, true)
}

// PerKeyFrame is PerKey for animation frames: the picture is not remembered, so the look the
// user chose survives the animation.
func (k *Keyboard) PerKeyFrame(cells []keymap.RGB, brightness int) error {
	return k.perKey(cells, brightness, false)
}

func (k *Keyboard) perKey(cells []keymap.RGB, brightness int, remember bool) error {
	if brightness == KeepBrightness {
		b, err := k.brightnessOrDefault()
		if err != nil {
			return err
		}
		brightness = b
	}
	if brightness < 0 || brightness > MaxBright {
		return firmware.Errorf("brightness 0..%d", MaxBright)
	}
	full := make([]keymap.RGB, keymap.NumCells)
	copy(full, cells)
	data := make([]byte, 0, keymap.NumCells*4)
	for _, c := range full {
		data = append(data, 0, c[0], c[1], c[2])
	}
	err := k.call(func(t Transport) error {
		if _, err := queryReport(t, 0x88, 0x77); err != nil {
			return err
		}
		if err := t.SetFeature([]byte{0x12, 0, 0, 0x08, 0, 0, 0, 0xE5}); err != nil {
			return fmt.Errorf("start per-key upload: %w", err)
		}
		for i := 0; i < keymap.NumCells*4/OutputLen; i++ {
			if err := t.WriteOutput(data[i*OutputLen : (i+1)*OutputLen]); err != nil {
				return fmt.Errorf("write per-key report %d: %w", i, err)
			}
			k.sleep(frameSpacing)
		}
		k.sleep(frameSettle)
		if err := t.SetFeature(pkt(0x08, 0x02, 0x33, 0x05, byte(brightness), 0x08, 0x01)); err != nil { //nolint:gosec // brightness was checked to be 0..50 above
			return fmt.Errorf("commit per-key upload: %w", err)
		}
		return nil
	})
	if err != nil {
		return err
	}
	if remember {
		k.remember(userKeyboard, State{Mode: ModePerKey, Brightness: &brightness, Cells: cellsHex(full)})
	}
	return nil
}

// RestoreDefault puts the factory look back: cells 0..125 = #00aec7, brightness 12.
func (k *Keyboard) RestoreDefault() error {
	return k.PerKey(factoryCells(), factoryBrightness)
}

func factoryCells() []keymap.RGB {
	return keymap.Canvas(keymap.RGB{0x00, 0xAE, 0xC7})
}

// SetBrightness changes brightness WITHOUT leaving the current look. The per-key brightness
// packet (mode 0x33) would drop a running effect back to static, so the effect is re-sent with
// the new level. If this tool did not set the current look (unknown), the per-key packet is used.
func (k *Keyboard) SetBrightness(level int) error {
	if level < 0 || level > MaxBright {
		return firmware.Errorf("brightness 0..%d", MaxBright)
	}
	s := k.Last()
	if s.Mode == ModeEffect && knownEffect(s.Name) && s.Name != "off" {
		// the keyboard's own brightness level first (some effects, e.g. spot, follow it) ...
		if err := k.send(pkt(0x08, 0x02, 0x33, 0x05, byte(level), 0x08, 0x01)); err != nil {
			return err
		}
		return k.SetEffect(s.Name, s.ColorOr(DefaultColor), s.SpeedOr(DefaultSpeed), level, s.DirectionOr(0))
	}
	if s.Mode == ModeOperating {
		return k.SetOperatingMode(s.ColorOr(OperatingColor), level)
	}
	if err := k.send(pkt(0x08, 0x02, 0x33, 0x05, byte(level), 0x08, 0x01)); err != nil {
		return err
	}
	if s.Mode == ModePerKey {
		s.Brightness = &level
		k.remember(userKeyboard, s)
	}
	return nil
}

// Off turns the backlight off, keeping the look: brightness 0 in whatever mode is running
// (the effect "off" packet did not blank this keyboard).
func (k *Keyboard) Off() error {
	s := k.Last()
	if s.Mode != ModeOff {
		b, err := k.Brightness()
		if err != nil {
			return err
		}
		if b > 0 {
			s.Brightness = &b
			k.remember(userRestore, s)
		}
	}
	return k.SetBrightness(0)
}

// On brings the backlight back in the look it had before Off.
func (k *Keyboard) On() error {
	s := k.getState(userRestore)
	lvl := s.BrightnessOr(0)
	if lvl == 0 {
		lvl = DefaultBrightness
	}
	if s.Mode != "" {
		k.remember(userKeyboard, s)
	}
	return k.SetBrightness(lvl)
}

// Replay puts a remembered look back on the keyboard and returns what was done, or "" if there
// was nothing to restore.
func (k *Keyboard) Replay(s State) (string, error) {
	switch {
	case s.Mode == ModeEffect && knownEffect(s.Name):
		if err := k.SetEffect(s.Name, s.ColorOr(DefaultColor), s.SpeedOr(DefaultSpeed), s.BrightnessOr(DefaultBrightness), s.DirectionOr(0)); err != nil {
			return "", err
		}
		return "effect " + s.Name, nil
	case s.Mode == ModeOperating:
		if err := k.SetOperatingMode(s.ColorOr(OperatingColor), s.BrightnessOr(OperatingBrightness)); err != nil {
			return "", err
		}
		return "operating mode", nil
	case s.Mode == ModePerKey && len(s.Cells) > 0:
		cells, err := parseCells(s.Cells)
		if err != nil {
			return "", fmt.Errorf("remembered per-key colours: %w", err)
		}
		b := s.BrightnessOr(0)
		if b == 0 {
			b = DefaultBrightness
		}
		if err := k.PerKey(cells, b); err != nil {
			return "", err
		}
		return "per-key colours", nil
	}
	return "", nil
}

// SetSpeed changes the animation speed (1 slowest .. 12 fastest) of the running effect, keeping its colour and brightness.
func (k *Keyboard) SetSpeed(speed int) error {
	if speed < SpeedMin || speed > SpeedMax {
		return firmware.Errorf("speed %d..%d", SpeedMin, SpeedMax)
	}
	s := k.Last()
	if s.Mode != ModeEffect || s.Name == "" || s.Name == "off" || s.Name == "static" {
		return firmware.Errorf("the current look has no animation speed; pick an effect first")
	}
	b := s.BrightnessOr(-1)
	if b < 0 {
		var err error
		if b, err = k.brightnessOrDefault(); err != nil {
			return err
		}
	}
	return k.SetEffect(s.Name, s.ColorOr(DefaultColor), speed, b, s.DirectionOr(0))
}

// SetColor changes the colour of the current look without changing the effect. Effects
// without a colour (wave, neon) cannot take one.
func (k *Keyboard) SetColor(color string) error {
	rgb, err := hexc(color)
	if err != nil {
		return err
	}
	color = normColor(color)
	s := k.Last()
	b, err := k.brightnessOrDefault()
	if err != nil {
		return err
	}
	if s.Mode == ModeEffect && knownEffect(s.Name) {
		if noColor[s.Name] {
			return firmware.Errorf("the %s effect has no colour; pick another effect (or use `keyboard fill`)", s.Name)
		}
		return k.SetEffect(s.Name, color, s.SpeedOr(DefaultSpeed), b, s.DirectionOr(0))
	}
	if s.Mode == ModeOperating {
		return k.SetOperatingMode(color, b)
	}
	return k.PerKey(keymap.Canvas(rgb), b)
}

// CurrentCells returns the per-key colours this tool last uploaded (128 cells); the stock
// cyan if none are remembered.
func (k *Keyboard) CurrentCells() []keymap.RGB {
	s := k.Last()
	if s.Mode == ModePerKey && len(s.Cells) > 0 {
		if cells, err := parseCells(s.Cells); err == nil {
			out := make([]keymap.RGB, keymap.NumCells)
			copy(out, cells)
			return out
		}
	}
	return factoryCells()
}

// Paint colours named keys or groups and leaves every other key as it is. It returns how many names were painted.
func (k *Keyboard) Paint(m *keymap.Map, items []keymap.KeyColor, brightness int) (int, error) {
	cells := k.CurrentCells()
	for _, it := range items {
		rgb, err := keymap.ParseHex(it.Color)
		if err != nil {
			return 0, err
		}
		list, err := m.CellsFor(it.Key)
		if err != nil {
			return 0, err
		}
		for _, c := range list {
			cells[c] = rgb
		}
	}
	if err := k.PerKey(cells, brightness); err != nil {
		return 0, err
	}
	return len(items), nil
}

// PaintCells colours individual cells and keeps every other key as it is.
func (k *Keyboard) PaintCells(changes map[int]string, brightness int) error {
	cells := k.CurrentCells()
	for c, col := range changes {
		if c < 0 || c >= keymap.NumCells {
			return firmware.Errorf("cell '%d': 0..%d", c, keymap.NumCells-1)
		}
		rgb, err := hexc(col)
		if err != nil {
			return err
		}
		cells[c] = rgb
	}
	return k.PerKey(cells, brightness)
}
