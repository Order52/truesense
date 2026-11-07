package tui

import (
	"context"
	"fmt"
	"strings"
	"time"
)

// UI constants: the orders and names the pages offer.
var (
	modeOrder  = []string{"eco", "quiet", "balanced", "performance", "turbo"}
	modeLabel  = map[string]string{"eco": "Eco", "quiet": "Quiet", "balanced": "Balanced", "performance": "Performance", "turbo": "Turbo"}
	modeStyle  = map[string]Style{"eco": "m_eco", "quiet": "m_quiet", "balanced": "m_balanced", "performance": "m_performance", "turbo": "m_turbo"}
	dtsModes   = []string{"Music", "Movie", "Voice", "Game1", "Game2", "Game3", "Plain"}
	colorList  = []Option{{"ffffff", "White"}, {"ff0000", "Red"}, {"ff7a00", "Orange"}, {"ffd000", "Yellow"}, {"00ff40", "Green"}, {"00aec7", "Teal"}, {"0050ff", "Blue"}, {"8000ff", "Purple"}, {"ff3fa0", "Pink"}}
	directions = []Option{{0, "Left to right"}, {1, "Right to left"}, {2, "Up"}, {3, "Down"}}
)

// lightFloor is the least brightness a light comes back at when an effect is chosen while
// it is off (keyboard scale 0-50, light bar 0-100): a barely visible 10 % would look broken.
var lightFloor = map[string]int{"kb": 10, "rear": 40}

func validMode(m string) bool {
	for _, x := range modeOrder {
		if x == m {
			return true
		}
	}
	return false
}

// ---- small value helpers

func anyString(v any) string { return fmt.Sprint(v) }

func strOrNil(s string) any {
	if s == "" {
		return nil
	}
	return s
}

func asBool(v any) bool { b, _ := v.(bool); return b }

func asString(v any) string { s, _ := v.(string); return s }

func onOffText(v any) string {
	if asBool(v) {
		return "on"
	}
	return "off"
}

func capitalize(s string) string {
	if s == "" {
		return s
	}
	return strings.ToUpper(s[:1]) + strings.ToLower(s[1:])
}

func namedOptions(names []string) []Option {
	out := make([]Option, len(names))
	for i, n := range names {
		out[i] = Option{n, n}
	}
	return out
}

func capitalized(names []string) []Option {
	out := make([]Option, len(names))
	for i, n := range names {
		out[i] = Option{n, capitalize(n)}
	}
	return out
}

func intRange(lo, hi, step int) []int {
	var out []int
	for v := lo; v <= hi; v += step {
		out = append(out, v)
	}
	return out
}

// reader makes a slow read from a typed backend read.
func reader[T any](key string, fn func(context.Context) (T, error)) Read {
	return Read{Key: key, Fn: func(ctx context.Context) (any, error) { return fn(ctx) }}
}

// xValue returns a slow reading as a row value (nil when it was not read).
func xValue[T any](d *Data, key string) any {
	if v, ok := getAs[T](d, key); ok {
		return v
	}
	return nil
}

// ---- the pages

// build creates the seven pages. Rows read from Data and write through App.run; nothing
// here touches the hardware directly.
func (a *App) build() []*Page {
	d, b, x := a.data, a.b, a.data

	modeRow := choiceRow("Power mode", func() []Option {
		out := make([]Option, len(modeOrder))
		for i, m := range modeOrder {
			out[i] = Option{m, modeLabel[m]}
		}
		return out
	}(), func() any { return strOrNil(d.powerMode()) }, func(v any) {
		m := asString(v)
		a.run(fmt.Sprintf("Power mode: %s (held %d min)", modeLabel[m], holdMinutes), func() (any, error) {
			return nil, b.SetPowerMode(a.ctx, m, holdMinutes*time.Minute)
		}, runOpts{key: "power"})
	}, Row{Colors: modeStyle, Hint: "Eco / Quiet / Balanced / Performance / Turbo. Your pick is held for 30 minutes against Smart auto."})

	autoRow := func() *Row {
		return choiceRow("Automatic power mode", []Option{{"native", "Native"}, {"smart", "Smart"}, {"off", "Off"}},
			func() any { return strOrNil(d.service().AutoMode) },
			func(v any) {
				m := asString(v)
				a.run("Automatic power mode: "+m, func() (any, error) { return nil, b.SetAutoMode(a.ctx, m) }, runOpts{key: "auto"})
			},
			Row{Hint: "Native: like PredatorSense (changes with the charger). Smart: also watches heat, load and games. Off: hands off. Needs the TrueSense service.", Enabled: d.daemon})
	}

	// ---- fans
	fanModeGet := func() any {
		fan := d.fanState()
		if !d.daemon() && fan == nil {
			return nil
		}
		svc := d.service()
		if svc.FanOff {
			return "off"
		}
		mode := ""
		if fan != nil {
			mode = fan.Mode
		}
		custom := svc.FanCurve != "" && svc.FanCurve != "native"
		switch {
		case mode == FirmwareFanManual && svc.SmartFan, mode == FirmwareFanAuto && svc.SmartFan && custom:
			return "smart"
		case mode == FirmwareFanAuto, mode == FirmwareFanMax, mode == FirmwareFanManual:
			return mode
		}
		return nil
	}
	fanPut := func(v any) {
		m := asString(v)
		switch m {
		case "off":
			a.run("Fans off", func() (any, error) { return nil, b.SetFan(a.ctx, FanRequest{Mode: "off", Confirm: true}) }, runOpts{key: "fan"})
		case "manual":
			cpu, gpu := a.fanHand(Opt[int]{}, Opt[int]{})
			a.run(fmt.Sprintf("Fans: manual, CPU %d %% GPU %d %%", cpu, gpu), func() (any, error) {
				return nil, b.SetFan(a.ctx, FanRequest{Mode: "manual", CPU: cpu, GPU: gpu, Confirm: cpu == 0 || gpu == 0})
			}, runOpts{key: "fan"})
		default:
			a.run("Fans: "+m, func() (any, error) { return nil, b.SetFan(a.ctx, FanRequest{Mode: m}) }, runOpts{key: "fan"})
		}
	}
	fanGuard := func(v any) string {
		if asString(v) == "off" {
			return "Stop BOTH fans? The service brings them back at CPU 70 °C / GPU 65 °C."
		}
		return ""
	}
	dutyGet := func(cpu bool) func() any {
		return func() any {
			f := d.fanState()
			switch {
			case f == nil:
				return nil
			case cpu:
				return f.CPUDuty
			}
			return f.GPUDuty
		}
	}
	dutyPut := func(which string) func(any) {
		return func(v any) {
			n, _ := asInt(v)
			var cpu, gpu int
			if which == "cpu" {
				cpu, gpu = a.fanHand(Some(n), Opt[int]{})
			} else {
				cpu, gpu = a.fanHand(Opt[int]{}, Some(n))
			}
			what := fmt.Sprintf("%d %%", n)
			if n == 0 {
				what = "stopped"
			}
			a.run(fmt.Sprintf("%s fan %s", strings.ToUpper(which), what), func() (any, error) {
				return nil, b.SetFan(a.ctx, FanRequest{Mode: "manual", CPU: cpu, GPU: gpu, Confirm: n == 0})
			}, runOpts{key: "fan"})
		}
	}
	dutyGuard := func(which string) func(any) string {
		return func(v any) string {
			if n, _ := asInt(v); n == 0 {
				return fmt.Sprintf("Stop the %s fan? The service brings it back when the laptop gets warm.", strings.ToUpper(which))
			}
			return ""
		}
	}
	duties := append([]int{0}, intRange(20, 100, 5)...)
	pct := func(v any) string {
		if n, _ := asInt(v); n == 0 {
			return "OFF"
		}
		return fmt.Sprintf("%v %%", v)
	}
	rpm := func(fan func(Sensors) int) func() any {
		return func() any {
			if s, ok := d.sensors(); ok {
				return fmt.Sprintf("%d rpm", fan(s))
			}
			return nil
		}
	}
	curveNames := b.FanCurveNames()
	smartNames := b.SmartProfileNames()
	namesOf := func(key string, initial []string) func() []Option {
		return func() []Option {
			if n, ok := getAs[[]string](d, key); ok {
				return namedOptions(n)
			}
			return namedOptions(initial)
		}
	}

	dashMode := choiceRow("Fans", []Option{{"smart", "Smart"}, {"auto", "Auto"}, {"max", "Max"}}, fanModeGet, fanPut,
		Row{Hint: "Smart: TrueSense's own curve. Auto: the firmware. Max: full speed. More on the Fans page."})
	dash := newPage("Dashboard", []*Row{modeRow, autoRow(), dashMode}, nil, a.drawDashboard)

	curveRow := choiceRow("Curve", nil, func() any {
		if n, ok := getAs[string](d, "curve"); ok && n != "" {
			return n
		}
		return strOrNil(d.service().FanCurve)
	}, func(v any) {
		n := asString(v)
		a.run("Fan curve: "+n, func() (any, error) { return nil, b.UseFanCurve(a.ctx, n) }, runOpts{key: "curve"})
	}, Row{Hint: "Used by the Smart mode. native = the firmware's own curve.", Enabled: d.daemon, OptionsFn: namesOf("curvenames", curveNames)})
	fans := newPage("Fans", []*Row{
		headerRow("Fan control"),
		choiceRow("Mode", []Option{{"smart", "Smart curve"}, {"auto", "Auto"}, {"max", "Max"}, {"manual", "Manual"}, {"off", "Off"}}, fanModeGet, fanPut,
			Row{Guard: fanGuard, Hint: "Smart: TrueSense's curve. Auto: firmware. Max: 100 %. Manual: the sliders below. Off: both fans stop (asks first)."}),
		sliderRow("CPU fan", duties, dutyGet(true), dutyPut("cpu"), Row{Fmt: pct, Guard: dutyGuard("cpu"), Exact0: true, Start: Some(40),
			Hint: "Left/Right in steps of 5; the lowest position is OFF and asks first. Moving a slider switches to Manual."}),
		sliderRow("GPU fan", duties, dutyGet(false), dutyPut("gpu"), Row{Fmt: pct, Guard: dutyGuard("gpu"), Exact0: true, Start: Some(40),
			Hint: "Stop one fan and keep the other, for example when external fans cool the laptop (asks first)."}),
		infoRow("CPU fan speed", rpm(func(s Sensors) int { return s.CPUFanRPM }), Row{}),
		infoRow("GPU fan speed", rpm(func(s Sensors) int { return s.GPUFanRPM }), Row{}),
		headerRow("Fan curve"),
		curveRow,
	}, []Read{reader("curve", b.FanCurve), {Key: "curvenames", Fn: func(context.Context) (any, error) { return b.FanCurveNames(), nil }}}, nil)

	// ---- power
	onoff := func(label string, set func(context.Context, bool) error) func(any) {
		return func(v any) {
			on := asBool(v)
			a.run(fmt.Sprintf("%s: %s", label, onOffText(v)), func() (any, error) { return nil, set(a.ctx, on) }, runOpts{})
		}
	}
	holdLeft := func() time.Duration { return d.service().HoldLeft }
	power := newPage("Power", []*Row{
		headerRow("Mode"), modeRow, autoRow(),
		choiceRow("Smart profile", nil, func() any {
			if n, ok := getAs[string](d, "smartp"); ok && n != "" {
				return n
			}
			return strOrNil(d.service().SmartProfile)
		}, func(v any) {
			n := asString(v)
			a.run("Smart profile: "+n, func() (any, error) { return nil, b.UseSmartProfile(a.ctx, n) }, runOpts{key: "smartp"})
		}, Row{Hint: "How Smart auto reacts: when to go up, how steady to stay. 'steady' for long games and external cooling.", Enabled: d.daemon, OptionsFn: namesOf("smartnames", smartNames)}),
		infoRow("Hold", func() any {
			if h := int(holdLeft() / time.Second); h > 0 {
				return fmt.Sprintf("your pick is held for %d min %d s", h/60, h%60)
			}
			return "none: automatic power mode is in charge"
		}, Row{}),
		buttonRow("Release the hold", "Release", func() {
			a.run("Hold released: automatic power mode is back", func() (any, error) { return nil, b.ReleaseHold(a.ctx) }, runOpts{})
		}, Row{Hint: "Let the automatic power mode take over now.", Enabled: func() bool { return holdLeft() > 0 }}),
		headerRow("Battery and charging"),
		toggleRow("Battery limit (80 %)", func() any { return xValue[bool](x, "limit") }, onoff("Battery limit", b.SetBatteryLimit), Row{Hint: "Stops charging at 80 % to keep the battery healthy."}),
		buttonRow("Charge to 100 % once", "Start / stop", func() {
			active, _ := getAs[bool](d, "full") // read now, under the screen's lock: the write runs later on the worker
			a.run("Charging to 100 % once", func() (any, error) { return nil, b.ChargeToFull(a.ctx, !active) }, runOpts{})
		}, Row{Hint: "Needs the charger and the 80 % limit on; it goes back to the limit by itself.", Enabled: func() bool {
			lim, _ := getAs[bool](d, "limit")
			return d.daemon() && lim
		}}),
		toggleRow("Battery calibration", func() any { return xValue[bool](x, "calib") }, onoff("Battery calibration", b.SetCalibration),
			Row{Hint: "A slow full charge and discharge cycle that re-learns the capacity."}),
		choiceRow("USB charging when off", []Option{{0, "Off"}, {10, "10 %"}, {20, "20 %"}, {30, "30 %"}}, func() any { return xValue[int](x, "usb") }, func(v any) {
			n, _ := asInt(v)
			a.run(fmt.Sprintf("USB charging: %d", n), func() (any, error) { return nil, b.SetUSBCharge(a.ctx, n) }, runOpts{})
		}, Row{Hint: "Charge phones from the USB port while the laptop is off, down to this battery level."}),
		headerRow("Graphics and display"),
		choiceRow("GPU mode", []Option{{"optimus", "Hybrid"}, {"nvidia", "NVIDIA only"}, {"auto", "Auto"}}, func() any { return xValue[string](x, "gpu") }, func(v any) {
			m := asString(v)
			a.run(fmt.Sprintf("GPU mode: %s (reboot to apply)", m), func() (any, error) { return nil, b.SetGPUMode(a.ctx, m) }, runOpts{key: "gpu"})
		}, Row{Hint: "Applies after a reboot.", Guard: func(v any) string {
			return fmt.Sprintf("Switch the GPU mode to %s? It takes effect after a reboot.", asString(v))
		}}),
		toggleRow("Panel overdrive", func() any { return xValue[bool](x, "od") }, onoff("Panel overdrive", b.SetOverdrive), Row{Hint: "Faster pixel response; can show slight overshoot."}),
		toggleRow("Boot sound", func() any { return xValue[bool](x, "boot") }, onoff("Boot sound", b.SetBootSound), Row{}),
		toggleRow("Keyboard light timeout", func() any { return xValue[bool](x, "kbt") }, onoff("Keyboard timeout", b.SetKeyboardTimeout), Row{Hint: "Turn the keyboard light off after a while without typing."}),
	}, []Read{reader("limit", b.BatteryLimit), reader("calib", b.Calibration), reader("usb", b.USBCharge), reader("gpu", b.GPUMode), reader("od", b.Overdrive),
		reader("boot", b.BootSound), reader("kbt", b.KeyboardTimeout), reader("smartp", b.SmartProfile), reader("full", b.ChargeToFullActive),
		{Key: "smartnames", Fn: func(context.Context) (any, error) { return b.SmartProfileNames(), nil }}}, nil)

	// ---- lighting
	light := a.lightingPage()

	// ---- audio
	audioPage := a.audioPage()

	// ---- profiles
	prof := a.profilesPage()

	// ---- system
	system := a.systemPage()

	return []*Page{dash, fans, power, light, audioPage, prof, system}
}

// fanHand returns manual duties for both fans. A fan you did not touch keeps what it is
// doing now: never 0 unless it really is stopped and the mode is already Manual, so
// moving one slider can never stop the other fan.
func (a *App) fanHand(cpu, gpu Opt[int]) (int, int) {
	f := a.data.fanState()
	var c0, g0 Opt[int]
	manual := false
	if f != nil {
		c0, g0, manual = Some(f.CPUDuty), Some(f.GPUDuty), f.Mode == FirmwareFanManual
	}
	keep := func(v, now Opt[int]) int {
		if v.OK {
			return v.V
		}
		if manual && now.OK && now.V == 0 {
			return 0
		}
		n := now.V
		if n == 0 {
			n = 40
		}
		return max(20, min(100, n))
	}
	return keep(cpu, c0), keep(gpu, g0)
}
