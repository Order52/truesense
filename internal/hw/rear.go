package hw

import (
	"encoding/hex"
	"fmt"
	"regexp"
	"strings"

	"github.com/Order52/truesense/internal/firmware"
)

// RearModes are the light bar's effects and their firmware codes.
var (
	RearModeNames = []string{"static", "breathing", "neon", "wave", "twinkling"}
	rearCode      = map[string]byte{"static": 0, "breathing": 1, "neon": 2, "wave": 3, "twinkling": 7}
	// RearZones are the light bar's three parts as firmware zone bits, left to right seen from
	// behind the laptop (left confirmed by the owner; middle and right assumed).
	RearZones = []int{1, 2, 4}
)

var hex6 = regexp.MustCompile(`^[0-9a-fA-F]{6}$`)

// Rear is the light bar as the firmware reports it.
type Rear struct {
	Raw        string   `json:"raw"`
	Mode       string   `json:"mode"`
	Speed      int      `json:"speed"`
	Brightness int      `json:"brightness"`
	Direction  int      `json:"direction"`
	Color      string   `json:"color"`
	Zones      []string `json:"zones,omitempty"`
}

// RearGet reads the light bar. Byte 0 of the reply is a status byte, then the 16-byte layout of set_rear.
func (h *HW) RearGet() (Rear, error) {
	r, err := h.do("get_rear")
	if err != nil {
		return Rear{}, err
	}
	raw, err := hex.DecodeString(r["raw"])
	if err != nil || len(raw) < 9 {
		return Rear{}, firmware.Errorf("unexpected rear reply %q", r["raw"])
	}
	b := raw[1:]
	mode := fmt.Sprintf("unknown(%d)", b[0])
	for name, code := range rearCode {
		if code == b[0] {
			mode = name
		}
	}
	return Rear{Raw: hex.EncodeToString(raw), Mode: mode, Speed: int(b[1]), Brightness: int(b[2]), Direction: int(b[4]), Color: hex.EncodeToString(b[5:8])}, nil
}

// RearOpts is a complete light bar setting. Zero values are meaningful (brightness 0 is "off"),
// so start from DefaultRear and change what you need.
type RearOpts struct {
	Mode       string
	Color      string   // RRGGBB
	Speed      int      // 1..5
	Brightness int      // 0..100
	Direction  int      // 0 or 1
	Zones      []string // optional: one colour, or three (left, middle, right); only the static effect can colour the parts differently
	Verify     bool     // read back and confirm; false for live sliders and colour drags, where the next write follows at once
}

// DefaultRear is the starting point for RearSet.
func DefaultRear() RearOpts {
	return RearOpts{Mode: "static", Color: "ffffff", Speed: 3, Brightness: 100, Direction: 1, Verify: true}
}

func normHex(c string) (string, error) {
	if !hex6.MatchString(c) {
		return "", firmware.Errorf("color must be RRGGBB hex")
	}
	return strings.ToLower(c), nil
}

// RearSet writes the light bar. In the static effect the colour is a separate firmware call per
// zone (the effect packet ignores its colour bytes), so the zones are written after the packet.
func (h *HW) RearSet(o RearOpts) (Rear, error) {
	code, ok := rearCode[o.Mode]
	if !ok {
		return Rear{}, firmware.Errorf("rear mode: %s", strings.Join(RearModeNames, ", "))
	}
	color, err := normHex(o.Color)
	if err != nil {
		return Rear{}, err
	}
	zones := o.Zones
	if zones != nil {
		for i := range zones {
			if zones[i], err = normHex(zones[i]); err != nil {
				return Rear{}, err
			}
		}
		if len(zones) == 1 {
			zones = []string{zones[0], zones[0], zones[0]}
		}
		if len(zones) != 3 {
			return Rear{}, firmware.Errorf("zones: give one colour, or three (left middle right)")
		}
		if o.Mode != "static" {
			return Rear{}, firmware.Errorf("different colours per part only work with the static effect")
		}
		color = zones[0]
	}
	if o.Speed < 1 || o.Speed > 5 || o.Brightness < 0 || o.Brightness > 100 || (o.Direction != 0 && o.Direction != 1) {
		return Rear{}, firmware.Errorf("speed 1..5, brightness 0..100, direction 0|1")
	}
	pkt := append([]byte{code, byte(o.Speed), byte(o.Brightness), 0, byte(o.Direction)}, mustHex(color)...)
	pkt = append(pkt, 3, 2, 0, 0, 0, 0, 0, 0)
	if _, err := h.do("set_rear " + hex.EncodeToString(pkt)); err != nil {
		return Rear{}, err
	}
	var zs []string
	if o.Mode == "static" {
		zs = zones
		if zs == nil {
			zs = []string{color, color, color}
		}
		for i, z := range RearZones {
			if _, err := h.do(fmt.Sprintf("set_color %s %d", zs[i], z)); err != nil {
				return Rear{}, err
			}
		}
		h.remember("rear_zones", zs)
	}
	var remembered any
	if o.Mode == "static" {
		remembered = zones
	}
	h.remember("rear_state", map[string]any{"mode": o.Mode, "color": color, "speed": o.Speed, "brightness": o.Brightness, "direction": o.Direction, "zones": remembered})
	if !o.Verify {
		return Rear{Mode: o.Mode, Brightness: o.Brightness, Speed: o.Speed, Color: color, Zones: zs}, nil
	}
	type pair struct {
		mode   string
		bright int
	}
	read := func() (pair, error) { r, err := h.RearGet(); return pair{r.Mode, r.Brightness}, err }
	if _, err := settle(h, read, pair{o.Mode, o.Brightness}, "rear light"); err != nil {
		return Rear{}, err
	}
	r, err := h.RearGet()
	r.Zones = zs
	return r, err
}

func mustHex(s string) []byte { b, _ := hex.DecodeString(s); return b }

func (h *HW) remember(key string, v any) {
	if h.State != nil {
		_ = h.State.Set(key, v) // a convenience file: failing to write it never fails the light
	}
}
