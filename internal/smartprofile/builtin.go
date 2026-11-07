package smartprofile

var builtinOrder = []string{"native", "truesense", "steady", "battery-first", "performance-first", "silent"}

func builtinProfile(name string) (Profile, bool) {
	p := Defaults()
	switch name {
	case "native":
		p.Description = "PredatorSense default: Balanced on power, Eco on battery, nothing else (no load or heat logic, like Native auto)"
		p.ACIdle, p.ACBrowsing, p.ACBusy, p.ACTurbo = "balanced", "balanced", "balanced", false
		p.BatteryIdle, p.BatteryBrowsing, p.BatteryBusy, p.BatteryHot = "eco", "eco", "eco", "eco"
		p.UpDwellS, p.DownDwellS, p.BusyHoldS, p.MinChangeS, p.TurboMargin, p.TurboRetryS = 2, 2, 0, 2, 0, 10
	case "truesense":
		p.Description = "Our profile for this laptop: Balanced day to day, Performance when you game, Turbo only when sustained and cool, Eco on battery"
	case "steady":
		p.Description = "For long games and cooling that is not the laptop's own (external fans): Performance when busy, never Turbo, and it changes mode only after minutes of quiet"
		p.ACTurbo, p.BusyHoldS, p.DownDwellS, p.MinChangeS, p.UpDwellS = false, 180, 180, 180, 8
	case "battery-first":
		p.Description = "Longest battery and quietest: no Turbo, light modes everywhere"
		p.ACIdle, p.ACBrowsing, p.ACTurbo = "quiet", "quiet", false
		p.BatteryIdle, p.BatteryBrowsing, p.BatteryBusy, p.BatteryLowPct, p.DownDwellS = "eco", "eco", "quiet", 30, 20
	case "performance-first":
		p.Description = "Fastest: Performance for everyday use on power, Turbo after 10 s of heavy load"
		p.ACIdle, p.ACBrowsing = "balanced", "performance"
		p.TurboAfterS, p.TurboMaxCPU, p.TurboMaxGPU, p.BackoffHoldS = 10, 90, 80, 45
		p.BatteryIdle, p.BatteryBrowsing, p.BatteryBusy, p.BatteryLowPct, p.UpDwellS = "quiet", "balanced", "performance", 15, 3
	case "silent":
		p.Description = "As quiet as possible: Quiet and Balanced only, no Turbo"
		p.ACIdle, p.ACBrowsing, p.ACBusy, p.ACTurbo = "quiet", "quiet", "balanced", false
		p.BatteryIdle, p.BatteryBrowsing, p.BatteryBusy, p.BatteryHot = "eco", "eco", "quiet", "eco"
	default:
		return Profile{}, false
	}
	return p, true
}

// BuiltinNames lists the built-in profile names in display order.
func BuiltinNames() []string { return append([]string(nil), builtinOrder...) }

// Builtin returns a built-in profile, or false when there is none of that name.
func Builtin(name string) (Profile, bool) { return builtinProfile(name) }

// IsBuiltin reports whether name is a built-in (read-only) profile.
func IsBuiltin(name string) bool {
	_, ok := builtinProfile(name)
	return ok
}
