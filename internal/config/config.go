// Package config is TrueSense's configuration: built-in defaults, overridden by
// /etc/truesense/config.toml (system) and ~/.config/truesense/config.toml (the user's own).
// Everything is optional; a missing file or key falls back to the default.
package config

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	toml "github.com/pelletier/go-toml/v2"

	"github.com/Order52/truesense/internal/fsx"
)

// SystemPath is the machine-wide configuration file.
const SystemPath = "/etc/truesense/config.toml"

// UserPath is the current user's configuration file.
func UserPath() string {
	if p := os.Getenv("TRUESENSE_CONFIG"); p != "" {
		return p
	}
	if home, err := os.UserHomeDir(); err == nil {
		return filepath.Join(home, ".config", "truesense", "config.toml")
	}
	return ""
}

// Modes are the power modes a configuration may name.
var Modes = []string{"eco", "quiet", "balanced", "performance", "turbo"}

// Config is the whole configuration.
type Config struct {
	User     string      `toml:"user"`
	Daemon   DaemonCfg   `toml:"daemon"`
	Fan      FanCfg      `toml:"fan"`
	CPU      CPUCfg      `toml:"cpu"`
	Native   NativeCfg   `toml:"native"`
	Smart    SmartCfg    `toml:"smart"`
	Profile  ProfileCfg  `toml:"profile"`
	Keys     KeysCfg     `toml:"keys"`
	Startup  StartupCfg  `toml:"startup"`
	Keyboard KeyboardCfg `toml:"keyboard"`
	Audio    AudioCfg    `toml:"audio"`
	Update   UpdateCfg   `toml:"update"`
	OSD      OSDCfg      `toml:"osd"`
	Compat   CompatCfg   `toml:"compat"`
}

// DaemonCfg covers what the service does by itself. A first install behaves like PredatorSense:
// firmware fans, native auto mode, nothing of ours in the way.
type DaemonCfg struct {
	SmartFan    bool `toml:"smart_fan"`
	AutoProfile bool `toml:"auto_profile"`
}

// FanCfg is the smart fan curve: the level entered at each temperature, with hysteresis.
type FanCfg struct {
	Levels       []int `toml:"levels"`
	CPUUp        []int `toml:"cpu_up"`
	GPUUp        []int `toml:"gpu_up"`
	Hysteresis   int   `toml:"hysteresis"`
	DownHold     int   `toml:"down_hold"`
	Predict      int   `toml:"predict"`
	BatteryShift int   `toml:"battery_shift"`
	Couple       int   `toml:"couple"`
	HotCPU       int   `toml:"hot_cpu"`
	HotGPU       int   `toml:"hot_gpu"`
	CalmCPU      int   `toml:"calm_cpu"`
	CalmGPU      int   `toml:"calm_gpu"`
	CalmHold     int   `toml:"calm_hold"`
}

// CPUCfg covers power limits and the thermal clock governor.
type CPUCfg struct {
	PLNormalW  int  `toml:"pl_normal_w"`
	PLGamingW  int  `toml:"pl_gaming_w"`
	GovHi      int  `toml:"gov_hi"`
	GovLo      int  `toml:"gov_lo"`
	GovEnabled bool `toml:"gov_enabled"`
	CapMaxKHz  int  `toml:"cap_max_khz"`
	CapMinKHz  int  `toml:"cap_min_khz"`
	CapDownKHz int  `toml:"cap_down_khz"`
	CapUpKHz   int  `toml:"cap_up_khz"`
}

// NativeCfg is "native auto", like PredatorSense on Windows: follows the power source only.
type NativeCfg struct {
	AC      string `toml:"ac"`
	Battery string `toml:"battery"`
}

// SmartCfg is the Smart auto engine's thresholds.
type SmartCfg struct {
	ACIdle          string `toml:"ac_idle"`
	ACBrowsing      string `toml:"ac_browsing"`
	ACBusy          string `toml:"ac_busy"`
	ACTurbo         bool   `toml:"ac_turbo"`
	TurboAfterS     int    `toml:"turbo_after_s"`
	TurboMaxCPU     int    `toml:"turbo_max_cpu"`
	TurboMaxGPU     int    `toml:"turbo_max_gpu"`
	BackoffHoldS    int    `toml:"backoff_hold_s"`
	IdleAfterS      int    `toml:"idle_after_s"`
	BatteryIdle     string `toml:"battery_idle"`
	BatteryBrowsing string `toml:"battery_browsing"`
	BatteryBusy     string `toml:"battery_busy"`
	BatteryHot      string `toml:"battery_hot"`
	BatteryLowPct   int    `toml:"battery_low_pct"`
	UpDwellS        int    `toml:"up_dwell_s"`
	DownDwellS      int    `toml:"down_dwell_s"`
}

// ProfileCfg covers how a picked power mode is held against the automatic one, and the OSD.
type ProfileCfg struct {
	OSD         bool        `toml:"osd"`
	HoldMinutes int         `toml:"hold_minutes"`
	Auto        ProfileAuto `toml:"auto"`
}

// ProfileAuto maps situations to power modes for the automatic profile.
type ProfileAuto struct {
	ACBusy      string `toml:"ac_busy"`
	ACNormal    string `toml:"ac_normal"`
	BatteryBusy string `toml:"battery_busy"`
	BatteryIdle string `toml:"battery_idle"`
}

// KeysCfg covers the PREDATOR key gestures and their actions.
type KeysCfg struct {
	Profile    string   `toml:"profile"`
	Tap        string   `toml:"tap"`
	Double     string   `toml:"double"`
	Triple     string   `toml:"triple"`
	Long       string   `toml:"long"`
	FnEsc      string   `toml:"fn_esc"`
	LongMS     int      `toml:"long_ms"`
	MultiMS    int      `toml:"multi_ms"`
	PowerCycle []string `toml:"power_cycle"`
}

// StartupCfg covers what is applied at login (last | default | none | profile:NAME | look:NAME | off).
type StartupCfg struct {
	Profile  string `toml:"profile"`
	Keyboard string `toml:"keyboard"`
	Rear     string `toml:"rear"`
}

// KeyboardCfg covers keyboard lighting behaviour.
type KeyboardCfg struct {
	OperatingFollow bool           `toml:"operating_follow"`
	OperatingColors map[string]any `toml:"operating_colors"`
}

// AudioCfg covers jack handling, DTS and microphone noise reduction defaults.
type AudioCfg struct {
	Jack        string `toml:"jack"`
	Notify      bool   `toml:"notify"`
	DTSAll      bool   `toml:"dts_all"`
	HeadsetMode string `toml:"headset_mode"`
	ArrayMode   string `toml:"array_mode"`
}

// UpdateCfg covers update checks (off until switched on) and where releases come from.
type UpdateCfg struct {
	AutoCheck  bool   `toml:"auto_check"`
	CheckHours int    `toml:"check_hours"`
	Repo       string `toml:"repo"`
}

// OSDCfg covers how on-screen notifications are shown (native | noctalia | none).
type OSDCfg struct {
	Style string `toml:"style"`
}

// CompatCfg covers compatibility shims for old status-bar widgets.
type CompatCfg struct {
	PredatorWidgets bool `toml:"predator_widgets"`
}

// Default returns the built-in defaults.
func Default() *Config {
	return &Config{
		Daemon: DaemonCfg{SmartFan: false, AutoProfile: true},
		Fan: FanCfg{
			Levels: []int{30, 40, 50, 60, 70, 85, 100}, CPUUp: []int{52, 60, 68, 75, 80, 85, 89}, GPUUp: []int{48, 55, 61, 66, 70, 74, 79},
			Hysteresis: 4, DownHold: 8, Predict: 5, BatteryShift: 3, Couple: 20, HotCPU: 92, HotGPU: 82, CalmCPU: 58, CalmGPU: 55, CalmHold: 20,
		},
		CPU:    CPUCfg{PLNormalW: 80, PLGamingW: 65, GovHi: 88, GovLo: 83, GovEnabled: true, CapMaxKHz: 5_000_000, CapMinKHz: 2_800_000, CapDownKHz: 200_000, CapUpKHz: 100_000},
		Native: NativeCfg{AC: "balanced", Battery: "eco"},
		Smart: SmartCfg{ACIdle: "balanced", ACBrowsing: "balanced", ACBusy: "performance", ACTurbo: true, TurboAfterS: 30, TurboMaxCPU: 85, TurboMaxGPU: 75, BackoffHoldS: 90, IdleAfterS: 60,
			BatteryIdle: "eco", BatteryBrowsing: "eco", BatteryBusy: "balanced", BatteryHot: "quiet", BatteryLowPct: 20, UpDwellS: 5, DownDwellS: 30},
		Profile: ProfileCfg{OSD: true, HoldMinutes: 10, Auto: ProfileAuto{ACBusy: "turbo", ACNormal: "balanced", BatteryBusy: "balanced", BatteryIdle: "eco"}},
		Keys: KeysCfg{Profile: "default", Tap: "power-next", Double: "output-toggle", Triple: "none", Long: "fan-max-toggle", FnEsc: "none", LongMS: 800, MultiMS: 450,
			PowerCycle: []string{"eco", "quiet", "balanced", "performance", "turbo"}},
		Startup:  StartupCfg{Keyboard: "last", Rear: "last"},
		Keyboard: KeyboardCfg{OperatingColors: map[string]any{}},
		Audio:    AudioCfg{Jack: "auto", Notify: true, DTSAll: true, HeadsetMode: "conference", ArrayMode: "conference"},
		Update:   UpdateCfg{AutoCheck: false, CheckHours: 24, Repo: "Order52/truesense"},
		OSD:      OSDCfg{Style: "native"},
	}
}

// Load reads the system file then the user's file over the defaults. A path of "" is skipped.
// A file with a syntax error is an error that names the file.
func Load(system, user string) (*Config, error) {
	cfg := Default()
	for _, p := range []string{system, user} {
		if p == "" {
			continue
		}
		b, err := os.ReadFile(p)
		if os.IsNotExist(err) {
			continue
		}
		if err != nil {
			return nil, fmt.Errorf("%s: %w", p, err)
		}
		if err := toml.Unmarshal(b, cfg); err != nil {
			return nil, fmt.Errorf("%s: %w", p, err)
		}
	}
	if cfg.User == "" {
		for _, k := range []string{"SUDO_USER", "USER"} {
			if v := os.Getenv(k); v != "" {
				cfg.User = v
				break
			}
		}
	}
	return cfg, nil
}

// LoadDefault loads the system and user files at their standard places.
func LoadDefault() (*Config, error) { return Load(SystemPath, UserPath()) }

func isMode(s string) bool {
	for _, m := range Modes {
		if m == s {
			return true
		}
	}
	return false
}

func sorted(a []int) bool {
	for i := 1; i < len(a); i++ {
		if a[i] < a[i-1] {
			return false
		}
	}
	return true
}

// Validate returns a list of problems (empty = fine). It catches typos that would otherwise
// silently fall back to a default.
func (c *Config) Validate() []string {
	var out []string
	f := c.Fan
	if len(f.Levels) != len(f.CPUUp) || len(f.Levels) != len(f.GPUUp) {
		out = append(out, "[fan] levels, cpu_up and gpu_up must have the same length")
	}
	for _, l := range f.Levels {
		if l < 20 || l > 100 {
			out = append(out, "[fan] levels must be 20..100")
			break
		}
	}
	if !sorted(f.CPUUp) || !sorted(f.GPUUp) {
		out = append(out, "[fan] cpu_up / gpu_up must be ascending")
	}
	if c.CPU.PLNormalW < 25 || c.CPU.PLNormalW > 120 || c.CPU.PLGamingW < 25 || c.CPU.PLGamingW > 120 {
		out = append(out, "[cpu] power limits must be 25..120 W")
	}
	switch c.Audio.Jack {
	case "auto", "headphones", "headset":
	default:
		out = append(out, "[audio] jack must be auto, headphones or headset")
	}
	modes := strings.Join(Modes, ", ")
	for k, v := range map[string]string{"ac_idle": c.Smart.ACIdle, "ac_browsing": c.Smart.ACBrowsing, "ac_busy": c.Smart.ACBusy, "battery_idle": c.Smart.BatteryIdle,
		"battery_browsing": c.Smart.BatteryBrowsing, "battery_busy": c.Smart.BatteryBusy, "battery_hot": c.Smart.BatteryHot} {
		if !isMode(v) {
			out = append(out, fmt.Sprintf("[smart] %s must be one of %s", k, modes))
		}
	}
	for k, v := range map[string]string{"ac": c.Native.AC, "battery": c.Native.Battery} {
		if !isMode(v) {
			out = append(out, fmt.Sprintf("[native] %s must be one of %s", k, modes))
		}
	}
	if !validStartup(c.Startup.Keyboard, "profile:", "look:") {
		out = append(out, "[startup] keyboard must be last, default, none, off, profile:NAME or look:NAME")
	}
	if !validStartup(c.Startup.Rear, "look:") {
		out = append(out, "[startup] rear must be last, default, none, off or look:NAME")
	}
	if c.Update.CheckHours < 1 || c.Update.CheckHours > 168 {
		out = append(out, "[update] check_hours must be 1..168")
	}
	switch c.OSD.Style {
	case "native", "noctalia", "none":
	default:
		out = append(out, "[osd] style must be native, noctalia or none")
	}
	return out
}

func validStartup(v string, prefixes ...string) bool {
	switch v {
	case "last", "default", "none", "off":
		return true
	}
	for _, p := range prefixes {
		if strings.HasPrefix(v, p) {
			return true
		}
	}
	return false
}

// UnknownKeys lists keys in the file at path that this version does not know (usually a
// typo): "section.key". A missing or unreadable file has none.
func UnknownKeys(path string) []string {
	b, err := os.ReadFile(path)
	if err != nil {
		return nil
	}
	var got, known map[string]any
	if toml.Unmarshal(b, &got) != nil {
		return nil
	}
	kb, err := toml.Marshal(Default())
	if err != nil || toml.Unmarshal(kb, &known) != nil {
		return nil
	}
	var out []string
	var walk func(prefix string, g, k map[string]any)
	walk = func(prefix string, g, k map[string]any) {
		for key, v := range g {
			kv, ok := k[key]
			if !ok {
				out = append(out, prefix+key)
				continue
			}
			gm, gok := v.(map[string]any)
			km, kok := kv.(map[string]any)
			if gok && kok && prefix != "keyboard." { // keyboard.operating_colors is a free-form table
				walk(prefix+key+".", gm, km)
			}
		}
	}
	walk("", got, known)
	return out
}

// WriteUser merges updates (a nested map) into the user's file and writes it back atomically.
func WriteUser(updates map[string]any, path string) error {
	cur := map[string]any{}
	if b, err := os.ReadFile(path); err == nil {
		if err := toml.Unmarshal(b, &cur); err != nil {
			return fmt.Errorf("%s: %w", path, err)
		}
	}
	merge(cur, updates)
	b, err := toml.Marshal(cur)
	if err != nil {
		return err
	}
	return fsx.WriteAtomic(path, b, 0o644)
}

func merge(dst, src map[string]any) {
	for k, v := range src {
		if sm, ok := v.(map[string]any); ok {
			if dm, ok := dst[k].(map[string]any); ok {
				merge(dm, sm)
				continue
			}
		}
		dst[k] = v
	}
}
