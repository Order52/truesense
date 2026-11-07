package curves

// builtinOrder is the order the built-in curves are listed in.
var builtinOrder = []string{"native", "truesense", "quiet", "performance", "max-cooling"}

// Tuned for the PH16-71 (i7-13700HX + RTX 4060, two fans on shared heatpipes): the CPU reaches
// 90 C in seconds under load while the GPU side is calmer, so the CPU curve ramps from 56 C
// and tops out at 89 C, the GPU curve from 54 C to 78 C. Firmware Auto below the first point
// (silent when cool).
func builtinCurve(name string) (Curve, bool) {
	c := Defaults()
	switch name {
	case "native":
		c.Description = "PredatorSense default: the firmware controls the fans exactly as it does on Windows (they stop when cool). Nothing of ours runs"
		c.Native = true
		c.CPU = []Point{{52, 0}, {89, 100}}
		c.GPU = []Point{{48, 0}, {78, 100}}
	case "truesense":
		c.Description = "Our curve for this laptop: silent when cool, early and smooth ramp, full speed before throttling"
		c.CPU = []Point{{52, 0}, {56, 30}, {62, 38}, {68, 48}, {74, 58}, {80, 70}, {85, 85}, {89, 100}}
		c.GPU = []Point{{48, 0}, {54, 30}, {60, 38}, {65, 48}, {70, 60}, {74, 78}, {78, 100}}
	case "quiet":
		c.Description = "Lowest noise: the fans stay off longer and ramp later"
		c.CPU = []Point{{58, 0}, {66, 30}, {72, 40}, {78, 55}, {84, 75}, {89, 100}}
		c.GPU = []Point{{52, 0}, {60, 30}, {66, 40}, {72, 58}, {77, 80}, {80, 100}}
		c.Predict, c.DownRate = 3.0, 1.0
	case "performance":
		c.Description = "Cooler and louder: starts early and ramps hard"
		c.CPU = []Point{{45, 30}, {55, 40}, {62, 55}, {70, 70}, {78, 85}, {84, 100}}
		c.GPU = []Point{{42, 30}, {52, 40}, {60, 55}, {66, 70}, {72, 85}, {76, 100}}
		c.Predict, c.DownRate = 6.0, 2.5
	case "max-cooling":
		c.Description = "Fans at full speed above 60 C"
		c.CPU = []Point{{40, 40}, {60, 100}}
		c.GPU = []Point{{40, 40}, {58, 100}}
		c.Predict, c.DownRate = 8.0, 4.0
	default:
		return Curve{}, false
	}
	return c, true
}

// BuiltinNames lists the built-in curve names in display order.
func BuiltinNames() []string { return append([]string(nil), builtinOrder...) }

// IsBuiltin reports whether name is a built-in (read-only) curve.
func IsBuiltin(name string) bool {
	_, ok := builtinCurve(name)
	return ok
}

// Builtin returns a copy of a built-in curve, or false when there is none of that name.
func Builtin(name string) (Curve, bool) { return builtinCurve(name) }
