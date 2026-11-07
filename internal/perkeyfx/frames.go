// Package perkeyfx draws the software per-key animations. The keyboard's hardware effects
// cover the whole keyboard; a different animation on chosen keys has to be drawn frame by
// frame.
//
// Render is a pure function of time and options that returns the 128 cells of one frame. The
// settings live in the user state ("perkey_fx": mode, speed, keys, base colours); a Runner,
// started inside the key service (which already has the keyboard), draws about 5 frames a
// second while an animation is on, then puts the base colours back when it is stopped. Each
// frame is a full upload (the keyboard takes about 190 ms), so the animations are smooth for
// slow effects, not for fast ones.
package perkeyfx

import (
	"math"

	"github.com/Order52/truesense/internal/keymap"
)

// Mode is one animation: the name the settings carry and the label shown to the user.
type Mode struct {
	Name  string
	Label string
}

// Modes lists the animations in display order.
func Modes() []Mode {
	return []Mode{
		{"pulse", "Pulse"}, {"wave", "Wave"}, {"cycle", "Colour cycle"}, {"twinkle", "Twinkle"},
		{"rainbow", "Rainbow sweep"}, {"breathing", "Breathing (base colours)"}, {"scanner", "Scanner"}, {"comet", "Comet"}, {"ripple", "Ripples"},
		{"rain", "Rain"}, {"matrix", "Matrix rain"}, {"fire", "Fire"}, {"plasma", "Plasma"}, {"aurora", "Aurora"}, {"lightning", "Lightning"},
		{"confetti", "Confetti"}, {"starlight", "Starlight"}, {"mood", "Mood lamp"},
	}
}

// ModeNames lists the animation names in display order.
func ModeNames() []string {
	ms := Modes()
	out := make([]string, len(ms))
	for i, m := range ms {
		out[i] = m.Name
	}
	return out
}

// IsMode reports whether name is an animation (not "off").
func IsMode(name string) bool {
	for _, m := range Modes() {
		if m.Name == name {
			return true
		}
	}
	return false
}

// Speed limits of an animation (1 slowest .. 10 fastest).
const (
	SpeedMin = 1
	SpeedMax = 10
)

const cols = keymap.Columns

var white = keymap.RGB{255, 255, 255}

// hash is a stateless pseudo-random 0..1 from two or three integers (no state to keep between
// frames). The arithmetic wraps modulo 2^32 exactly as the Python version masked it.
func hash(i, k, z int) float64 {
	//nolint:gosec // wrapping is the point: negative inputs map to the same residues Python's mask gave
	x := uint32(uint64(int64(i))*2654435761 + uint64(int64(k))*40503 + uint64(int64(z))*7919 + 97)
	x ^= x >> 15
	x *= 2246822519
	x ^= x >> 13
	return float64(x&0xFFFF) / 65536
}

func clamp01(f float64) float64 { return math.Max(0, math.Min(1, f)) }

func scale(c keymap.RGB, f float64) keymap.RGB {
	f = clamp01(f)
	return keymap.RGB{uint8(float64(c[0]) * f), uint8(float64(c[1]) * f), uint8(float64(c[2]) * f)}
}

func mix(a, b keymap.RGB, f float64) keymap.RGB {
	f = clamp01(f)
	var c keymap.RGB
	for i := range c {
		c[i] = uint8(float64(a[i]) + (float64(b[i])-float64(a[i]))*f)
	}
	return c
}

// heat maps 0..1 to black, red, orange, yellow-white.
func heat(v float64) keymap.RGB {
	v = clamp01(v)
	switch {
	case v < 0.4:
		return keymap.RGB{uint8(255 * v / 0.4), 0, 0}
	case v < 0.75:
		return keymap.RGB{255, uint8(160 * (v - 0.4) / 0.35), 0}
	}
	g := 160 + int(95*(v-0.75)/0.25)                                           // 160..255 for v in 0.75..1
	return keymap.RGB{255, uint8(min(g, 255)), uint8(200 * (v - 0.75) / 0.25)} //nolint:gosec // g is at most 255 (min above)
}

// pyMod is Python's float modulo (the result takes the sign of the divisor).
func pyMod(x, y float64) float64 {
	r := math.Mod(x, y)
	if r != 0 && (r < 0) != (y < 0) {
		r += y
	}
	return r
}

func brightest(c keymap.RGB) uint8 { return max(c[0], c[1], c[2]) }

// cellFx is the colour of one cell. Cells are numbered column*6 + row (22 columns, 6 rows);
// row 0 is the top row.
func cellFx(mode string, c int, base keymap.RGB, t, rate float64) keymap.RGB {
	col, row := c/keymap.Rows, c%keymap.Rows
	x := float64(col) / float64(cols-1)
	lit := white // a dark key still shows the effect
	if brightest(base) > 60 {
		lit = base
	}
	switch mode {
	case "pulse":
		return scale(lit, 0.15+0.85*(0.5+0.5*math.Sin(2*math.Pi*rate*t)))
	case "breathing":
		return scale(lit, 0.5-0.5*math.Cos(2*math.Pi*rate*0.5*t))
	case "wave":
		return keymap.HSV(x - t*rate)
	case "rainbow":
		return keymap.HSV(x*0.8 + t*rate*0.3 + float64(row)*0.02)
	case "cycle":
		return keymap.HSV(t * rate * 0.5)
	case "mood":
		return mix(white, keymap.HSV(t/60.0*math.Max(1, rate*10)/5), 0.55)
	case "twinkle":
		s := math.Pow(math.Max(0, math.Sin(2*math.Pi*(t*rate*0.6+hash(c, 1, 0)))), 12)
		return mix(base, white, s)
	case "starlight":
		u := t*rate*0.35 + hash(c, 2, 0)
		ph := pyMod(u, 1.0)
		on := hash(c, 3, int(u)) < 0.25 // each key gets a new chance every cycle
		f := 0.0
		if on {
			f = 1 - math.Abs(ph*2-1)
		}
		return mix(scale(base, 0.12), white, f)
	case "scanner":
		pos := float64(cols-1) * (1 - math.Abs(pyMod(t*rate*0.5, 1.0)*2-1))
		return mix(scale(base, 0.08), lit, 1-math.Abs(float64(col)-pos)/3.0)
	case "comet":
		head := pyMod(t*rate*8, float64(cols+8))
		d := head - float64(col)
		f := 0.0
		if 0 <= d && d <= 8 {
			tail := 1 - d/8.0
			f = tail * tail
		}
		return mix(scale(base, 0.05), lit, f)
	case "ripple":
		dx := (float64(col) - float64(cols-1)/2) / 3.0
		dy := float64(row) - 2.5
		d := math.Hypot(dx, dy) * 1.5
		rad := pyMod(t*rate*6, 12)
		return mix(scale(base, 0.06), lit, 1-math.Abs(d-rad)/1.6)
	case "rain":
		top := float64(5 - row)
		head := pyMod(hash(col, 7, 0)*6+t*rate*4, 9)
		d := head - top
		f := 0.0
		if 0 <= d && d < 3 {
			f = 1 - d/3.0
		}
		return mix(scale(base, 0.05), lit, f)
	case "matrix":
		top := float64(5 - row)
		head := pyMod(t*rate*5.5+hash(col, 8, 0)*12, 12)
		d := head - top
		switch {
		case d < 0 || d > 4:
			return keymap.RGB{0, 6, 0}
		case d < 1:
			return keymap.RGB{180, 255, 180}
		}
		return keymap.RGB{0, uint8(255 * (1 - d/4.0)), 0}
	case "fire":
		top := float64(5 - row)
		k := int(t * rate * 4)
		f := pyMod(t*rate*4, 1.0)
		n := hash(c, k, 0)*(1-f) + hash(c, k+1, 0)*f
		return heat(n*0.55 + (0.5 - top*0.09))
	case "plasma":
		v := math.Sin(x*9+t*rate*2) + math.Sin(float64(row)*1.7+t*rate*1.3) + math.Sin((x*6+float64(row))+t*rate)
		return keymap.HSV(v/6.0 + 0.5 + t*rate*0.05)
	case "aurora":
		v := 0.5 + 0.5*math.Sin(x*6+t*rate*0.8+float64(row)*0.7)*math.Sin(x*2.5-t*rate*0.4)
		if v > 0.2 {
			return mix(keymap.RGB{0, 200, 90}, keymap.RGB{110, 40, 210}, v)
		}
		return keymap.RGB{0, 10, 25}
	case "lightning":
		k := int(t * rate * 6)
		on := hash(k, 9, 0) < 0.18
		sc := int(hash(k, 10, 0) * cols)
		w := 2 + int(hash(k, 11, 0)*4)
		if on && abs(col-sc) <= w {
			return keymap.RGB{220, 230, 255}
		}
		return keymap.RGB{5, 5, 18}
	case "confetti":
		k := int(t * rate * 4)
		f := pyMod(t*rate*4, 1.0)
		for j := 0; j < 2; j++ {
			if hash(c, k-j, 12) < 0.07 {
				return scale(keymap.HSV(hash(c, k-j, 13)), 1-(float64(j)+f)/2)
			}
		}
		return scale(base, 0.1)
	}
	return base
}

func abs(n int) int {
	if n < 0 {
		return -n
	}
	return n
}

// Render is one frame: 128 colours. keys are the cells the animation applies to (none = every
// lit key); the rest keep their base colour. A mode that is not an animation ("off") returns the base.
func Render(mode string, t float64, base []keymap.RGB, keys []int, speed int) []keymap.RGB {
	rate := float64(max(SpeedMin, min(SpeedMax, speed))) * 0.1 // cycles per second
	cells := make([]keymap.RGB, keymap.NumCells)
	copy(cells, base)
	sel := keys
	if len(sel) == 0 {
		sel = make([]int, keymap.LitCells)
		for i := range sel {
			sel[i] = i
		}
	}
	for _, c := range sel {
		if c >= 0 && c < keymap.NumCells {
			cells[c] = cellFx(mode, c, cells[c], t, rate)
		}
	}
	return cells
}
