// Package smartprofile is the data model of the Smart-auto profiles: the settings that decide
// how Smart auto chooses the power mode (which mode on power and on battery, how long a load
// must last before Turbo, which temperatures stop Turbo, and so on).
//
// Built-in profiles are read-only (copy one to make your own); the user's live in
// ~/.config/truesense/smart/NAME.json. The profile in use is kept by the daemon, so it works
// before anyone logs in. This package only describes and stores profiles; the decisions are
// made elsewhere.
package smartprofile

import (
	"encoding/json"

	"github.com/Order52/truesense/internal/firmware"
)

// Modes are the power modes a profile can choose, from the quietest to the fastest.
func Modes() []string { return []string{"eco", "quiet", "balanced", "performance", "turbo"} }

func validMode(m string) bool {
	for _, x := range Modes() {
		if x == m {
			return true
		}
	}
	return false
}

// Profile is one Smart-auto profile. The JSON field names are the Python [smart] settings
// keys: the daemon exposes this structure on the control socket.
type Profile struct {
	Description string `json:"description,omitempty"`

	ACIdle     string `json:"ac_idle"`
	ACBrowsing string `json:"ac_browsing"`
	ACBusy     string `json:"ac_busy"`
	ACTurbo    bool   `json:"ac_turbo"`

	TurboAfterS   int `json:"turbo_after_s"`
	TurboMaxCPU   int `json:"turbo_max_cpu"`
	TurboMaxGPU   int `json:"turbo_max_gpu"`
	BackoffHoldS  int `json:"backoff_hold_s"`
	IdleAfterS    int `json:"idle_after_s"`
	BatteryLowPct int `json:"battery_low_pct"`

	BatteryIdle     string `json:"battery_idle"`
	BatteryBrowsing string `json:"battery_browsing"`
	BatteryBusy     string `json:"battery_busy"`
	BatteryHot      string `json:"battery_hot"`

	UpDwellS    int `json:"up_dwell_s"`
	DownDwellS  int `json:"down_dwell_s"`
	BusyHoldS   int `json:"busy_hold_s"`
	MinChangeS  int `json:"min_change_s"`
	TurboMargin int `json:"turbo_margin_c"`
	TurboRetryS int `json:"turbo_retry_s"`
}

// Defaults returns the settings a profile gets for everything it does not give (and what the
// "truesense" built-in is made of).
func Defaults() Profile {
	return Profile{
		ACIdle: "balanced", ACBrowsing: "balanced", ACBusy: "performance", ACTurbo: true,
		TurboAfterS: 30, TurboMaxCPU: 85, TurboMaxGPU: 75, BackoffHoldS: 90, IdleAfterS: 60,
		BatteryIdle: "eco", BatteryBrowsing: "eco", BatteryBusy: "balanced", BatteryHot: "quiet", BatteryLowPct: 20,
		UpDwellS: 5, DownDwellS: 60, BusyHoldS: 60, MinChangeS: 45, TurboMargin: 6, TurboRetryS: 180,
	}
}

// ModeSetting describes one setting whose value is a power mode: its JSON key and the label
// a window shows for it.
type ModeSetting struct {
	Key   string
	Label string
}

// ModeSettings lists the mode-valued settings in display order (the Python LABELS).
func ModeSettings() []ModeSetting {
	return []ModeSetting{
		{"ac_idle", "On power, idle"}, {"ac_browsing", "On power, everyday use"}, {"ac_busy", "On power, gaming or heavy work"},
		{"battery_idle", "On battery, idle"}, {"battery_browsing", "On battery, everyday use"},
		{"battery_busy", "On battery, gaming or heavy work"}, {"battery_hot", "On battery, too hot"},
	}
}

// NumSetting describes one numeric setting: its JSON key, the allowed range and the label a
// window shows for it (the Python NUMS).
type NumSetting struct {
	Key      string
	Min, Max int
	Label    string
}

// NumSettings lists the numeric settings with their ranges, in display order.
func NumSettings() []NumSetting {
	return []NumSetting{
		{"turbo_after_s", 5, 300, "Heavy load for this long before Turbo (s)"},
		{"turbo_max_cpu", 60, 95, "No Turbo above this CPU temperature (°C)"},
		{"turbo_max_gpu", 55, 85, "No Turbo above this GPU temperature (°C)"},
		{"backoff_hold_s", 10, 600, "After overheating, wait before Turbo again (s)"},
		{"idle_after_s", 10, 600, "Idle for this long before the idle mode (s)"},
		{"battery_low_pct", 5, 50, "Below this battery level, force Eco (%)"},
		{"up_dwell_s", 2, 120, "Wait before moving to a faster mode (s)"},
		{"down_dwell_s", 2, 300, "Wait before moving to a slower mode (s)"},
		{"busy_hold_s", 0, 600, "Stay in the gaming / heavy mode this long after the load drops (s)"},
		{"min_change_s", 2, 600, "At least this long between automatic mode changes (s)"},
		{"turbo_margin_c", 0, 20, "Enter Turbo only this far below its temperature limits (°C)"},
		{"turbo_retry_s", 10, 900, "After Turbo reaches its limit, wait before trying it again (s)"},
	}
}

// modeField returns the mode-valued setting with that JSON key, or nil.
func (p *Profile) modeField(key string) *string {
	switch key {
	case "ac_idle":
		return &p.ACIdle
	case "ac_browsing":
		return &p.ACBrowsing
	case "ac_busy":
		return &p.ACBusy
	case "battery_idle":
		return &p.BatteryIdle
	case "battery_browsing":
		return &p.BatteryBrowsing
	case "battery_busy":
		return &p.BatteryBusy
	case "battery_hot":
		return &p.BatteryHot
	}
	return nil
}

// numField returns the numeric setting with that JSON key, or nil.
func (p *Profile) numField(key string) *int {
	switch key {
	case "turbo_after_s":
		return &p.TurboAfterS
	case "turbo_max_cpu":
		return &p.TurboMaxCPU
	case "turbo_max_gpu":
		return &p.TurboMaxGPU
	case "backoff_hold_s":
		return &p.BackoffHoldS
	case "idle_after_s":
		return &p.IdleAfterS
	case "battery_low_pct":
		return &p.BatteryLowPct
	case "up_dwell_s":
		return &p.UpDwellS
	case "down_dwell_s":
		return &p.DownDwellS
	case "busy_hold_s":
		return &p.BusyHoldS
	case "min_change_s":
		return &p.MinChangeS
	case "turbo_margin_c":
		return &p.TurboMargin
	case "turbo_retry_s":
		return &p.TurboRetryS
	}
	return nil
}

// Check verifies a typed profile: every mode is a known one and every number is inside its
// range.
func (p Profile) Check() error {
	for _, ms := range ModeSettings() {
		if !validMode(*p.modeField(ms.Key)) {
			return modeError(ms.Key)
		}
	}
	for _, ns := range NumSettings() {
		if v := *p.numField(ns.Key); v < ns.Min || v > ns.Max {
			return rangeError(ns)
		}
	}
	return nil
}

func modeError(key string) error {
	return firmware.Errorf("%s must be one of eco, quiet, balanced, performance, turbo", key)
}

func rangeError(ns NumSetting) error { return firmware.Errorf("%s: %d..%d", ns.Key, ns.Min, ns.Max) }

// Validate checks a profile that arrived as decoded JSON (a control-socket request, a file)
// and returns it with the defaults filled in for every setting it does not give. Unknown keys
// are ignored. Numbers are truncated to whole numbers, as the Python implementation did.
func Validate(v any) (Profile, error) {
	data, err := json.Marshal(v)
	if err != nil {
		return Profile{}, firmware.Errorf("a smart profile is an object of settings")
	}
	return Parse(data)
}

// Parse is Validate for JSON text.
func Parse(data []byte) (Profile, error) {
	var obj map[string]json.RawMessage
	if err := json.Unmarshal(data, &obj); err != nil || obj == nil {
		return Profile{}, firmware.Errorf("a smart profile is an object of settings")
	}
	p := Defaults()
	if s, ok := rawString(obj["description"]); ok {
		p.Description = s
	}
	for _, ms := range ModeSettings() {
		raw, present := obj[ms.Key]
		if !present {
			continue
		}
		s, ok := rawString(raw)
		if !ok || !validMode(s) {
			return Profile{}, modeError(ms.Key)
		}
		*p.modeField(ms.Key) = s
	}
	if raw, present := obj["ac_turbo"]; present {
		b, ok := rawBool(raw)
		if !ok {
			return Profile{}, firmware.Errorf("ac_turbo is true or false")
		}
		p.ACTurbo = b
	}
	for _, ns := range NumSettings() {
		raw, present := obj[ns.Key]
		if !present {
			continue
		}
		f, ok := rawNumber(raw)
		if !ok || f < float64(ns.Min) || f > float64(ns.Max) {
			return Profile{}, rangeError(ns)
		}
		*p.numField(ns.Key) = int(f)
	}
	return p, p.Check()
}

func rawNumber(raw json.RawMessage) (float64, bool) {
	var f *float64
	if json.Unmarshal(raw, &f) != nil || f == nil {
		return 0, false
	}
	return *f, true
}

func rawString(raw json.RawMessage) (string, bool) {
	var s *string
	if json.Unmarshal(raw, &s) != nil || s == nil {
		return "", false
	}
	return *s, true
}

func rawBool(raw json.RawMessage) (bool, bool) {
	var b *bool
	if json.Unmarshal(raw, &b) != nil || b == nil {
		return false, false
	}
	return *b, true
}
