// Package profiles is the whole-setup profiles: a saved bundle of settings applied in one go
// (command line, PREDATOR key, window).
//
// A profile is a plain TOML file, in ~/.config/truesense/profiles/ (yours) or in the shipped
// directory (<prefix>/share/truesense/profiles; yours win). Every section is optional, and
// applying a profile changes only what it lists:
//
//	description = "..."
//	[power]    mode = "turbo"                                   quiet balanced performance turbo eco     auto = "off" | "native" | "smart"
//	[fan]      mode = "smart" | "auto" | "max" | "manual"       manual also needs cpu = 60, gpu = 60 (20..100)     curve = "truesense" (smart fan curve name)
//	[keyboard] on = true  effect = "wave" color = "00aec7" speed = 5 brightness = 30   or   profile = "<lighting profile>"   timeout = true
//	[rear]     on = true  effect = "breathing" color = "ff0000" speed = 3 brightness = 100
//	[battery]  limit = true   usb_charge = 30                   (0 10 20 30)
//	[display]  overdrive = true
//	[boot]     sound = false
//	[audio]    output = "speakers" | "headphones"   voice_headset = "conference"   voice_array = "off"   mic_muted = false   dts_mode = "movie"
//
// Applying a profile never changes the GPU mode (that needs a reboot) and never touches
// undervolt.
//
// Profiles are kept as generic TOML tables, not a struct, so a file written by hand or by a
// newer version round-trips without losing keys, and a wrong type in one setting fails that
// setting alone (it is reported) instead of the whole profile.
package profiles

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"unicode"

	toml "github.com/pelletier/go-toml/v2"

	"github.com/Order52/truesense/internal/fsx"
)

// Profile is a decoded profile: sections of settings.
type Profile map[string]any

// DefaultUserDir is ~/.config/truesense/profiles.
func DefaultUserDir() string {
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".config", "truesense", "profiles")
}

// DefaultSysDirs are the shipped profile directories. TRUESENSE_PROFILES_DIR replaces them
// (the tests and a source tree use it).
func DefaultSysDirs(getenv func(string) string) []string {
	if d := getenv("TRUESENSE_PROFILES_DIR"); d != "" {
		return []string{d}
	}
	return []string{"/usr/local/share/truesense/profiles", "/usr/share/truesense/profiles"}
}

// Files is the profile files on disk: the user's directory and the shipped ones.
type Files struct {
	UserDir string
	SysDirs []string
}

// ValidName reports whether name can name a profile: letters, digits, '-' and '_' (never a
// path).
func ValidName(name string) bool {
	if name == "" {
		return false
	}
	for _, r := range name {
		if !unicode.IsLetter(r) && !unicode.IsNumber(r) && r != '-' && r != '_' {
			return false
		}
	}
	return true
}

var errName = errors.New("profile names: letters, digits, - and _")

func (f *Files) dirs() []string { return append([]string{f.UserDir}, f.SysDirs...) }

// path returns where the profile lives (the user's first), or "" when there is none.
func (f *Files) path(name string) (string, error) {
	if !ValidName(name) {
		return "", errName
	}
	for _, d := range f.dirs() {
		p := filepath.Join(d, name+".toml")
		if _, err := os.Stat(p); err == nil {
			return p, nil
		}
	}
	return "", nil
}

// Names lists every profile, shipped and yours, sorted, each once.
func (f *Files) Names() []string {
	seen := map[string]bool{}
	for _, d := range f.dirs() {
		entries, _ := os.ReadDir(d)
		for _, e := range entries {
			if n, ok := strings.CutSuffix(e.Name(), ".toml"); ok {
				seen[n] = true
			}
		}
	}
	out := make([]string, 0, len(seen))
	for n := range seen {
		out = append(out, n)
	}
	sort.Strings(out)
	return out
}

// Load reads a profile.
func (f *Files) Load(name string) (Profile, error) {
	p, err := f.path(name)
	if err != nil {
		return nil, err
	}
	if p == "" {
		return nil, fmt.Errorf("no profile '%s' (truesense profile list)", name)
	}
	data, err := os.ReadFile(p)
	if err != nil {
		return nil, fmt.Errorf("read profile '%s': %w", name, err)
	}
	return Parse(data, p)
}

// Parse decodes TOML text; origin names the file in the error.
func Parse(data []byte, origin string) (Profile, error) {
	var d Profile
	if err := toml.Unmarshal(data, &d); err != nil {
		return nil, fmt.Errorf("%s: %w", origin, err)
	}
	if d == nil {
		d = Profile{}
	}
	return d, nil
}

// Source says where a profile comes from: "yours", "shipped", or "" when there is none.
func (f *Files) Source(name string) string {
	p, err := f.path(name)
	switch {
	case err != nil || p == "":
		return ""
	case filepath.Dir(p) == filepath.Clean(f.UserDir):
		return "yours"
	}
	return "shipped"
}

// Save writes your profile (a shipped one of the same name is overridden, not changed) and
// returns the file's path.
func (f *Files) Save(name string, data Profile) (string, error) {
	if !ValidName(name) {
		return "", errName
	}
	b, err := toml.Marshal(map[string]any(data))
	if err != nil {
		return "", fmt.Errorf("encode profile '%s': %w", name, err)
	}
	p := filepath.Join(f.UserDir, name+".toml")
	if err := fsx.WriteAtomic(p, b, 0o644); err != nil {
		return "", fmt.Errorf("save profile '%s': %w", name, err)
	}
	return p, nil
}

// Delete removes your profile; a shipped one cannot be deleted.
func (f *Files) Delete(name string) error {
	p, err := f.path(name)
	if err != nil {
		return err
	}
	if p == "" {
		return fmt.Errorf("no profile '%s'", name)
	}
	if filepath.Dir(p) != filepath.Clean(f.UserDir) {
		return fmt.Errorf("'%s' is a shipped profile; save your own copy with the same name to override it", name)
	}
	if err := os.Remove(p); err != nil {
		return fmt.Errorf("delete profile '%s': %w", name, err)
	}
	return nil
}

// Describe says what a profile will change, in plain words, one line per area.
func Describe(d Profile) []string {
	var out []string
	p, f, k, r, b, a := table(d, "power"), table(d, "fan"), table(d, "keyboard"), table(d, "rear"), table(d, "battery"), table(d, "audio")
	if has(p, "mode") || has(p, "auto") {
		var bits []string
		if has(p, "mode") {
			bits = append(bits, capitalize(text(p["mode"]))+" mode (held for a while)")
		}
		if has(p, "auto") {
			bits = append(bits, "mode control "+text(p["auto"]))
		}
		out = append(out, "Power: "+strings.Join(bits, ", "))
	}
	if has(f, "mode") || has(f, "curve") {
		var bits []string
		if has(f, "mode") {
			bits = append(bits, fanText(f))
		}
		if has(f, "curve") {
			bits = append(bits, "curve "+text(f["curve"]))
		}
		out = append(out, "Fans: "+strings.Join(nonEmpty(bits), ", "))
	}
	if len(k) > 0 {
		var bits []string
		if has(k, "effect") {
			bits = append(bits, text(k["effect"])+" effect")
		}
		if has(k, "profile") {
			bits = append(bits, "key set "+text(k["profile"]))
		}
		if has(k, "on") {
			bits = append(bits, "backlight "+onOff(k["on"]))
		}
		if has(k, "timeout") {
			bits = append(bits, "light off after 30 s "+onOff(k["timeout"]))
		}
		out = append(out, "Keyboard: "+strings.Join(bits, ", "))
	}
	if len(r) > 0 {
		line := "off"
		switch {
		case truthy(r["effect"]):
			line = strings.TrimSpace(text(r["effect"]) + " " + textOr(r["color"], ""))
		case truthy(r["on"]):
			line = "on"
		}
		out = append(out, "Rear light: "+line)
	}
	if len(b) > 0 {
		var bits []string
		if has(b, "limit") {
			bits = append(bits, "80 % limit "+onOff(b["limit"]))
		}
		if has(b, "usb_charge") {
			bits = append(bits, "USB charging while off "+text(b["usb_charge"]))
		}
		out = append(out, "Battery: "+strings.Join(bits, ", "))
	}
	if disp := table(d, "display"); len(disp) > 0 {
		out = append(out, "Screen: overdrive "+onOff(disp["overdrive"]))
	}
	if boot := table(d, "boot"); len(boot) > 0 {
		out = append(out, "Boot sound "+onOff(boot["sound"]))
	}
	if len(a) > 0 {
		var bits []string
		if has(a, "output") {
			bits = append(bits, text(a["output"]))
		}
		if has(a, "voice_headset") {
			bits = append(bits, "headset noise reduction "+text(a["voice_headset"]))
		}
		if has(a, "voice_array") {
			bits = append(bits, "built-in noise reduction "+text(a["voice_array"]))
		}
		out = append(out, "Audio: "+strings.Join(nonEmpty(bits), ", "))
	}
	if len(out) == 0 {
		return []string{"(nothing)"}
	}
	return out
}

func fanText(f map[string]any) string {
	switch m := text(f["mode"]); m {
	case "smart":
		return "smart curve"
	case "auto":
		return "firmware Auto"
	case "max":
		return "maximum"
	case "manual":
		return fmt.Sprintf("manual %s %% / %s %%", textOr(f["cpu"], "?"), textOr(f["gpu"], "?"))
	default:
		return m
	}
}

func nonEmpty(in []string) []string {
	var out []string
	for _, s := range in {
		if s != "" {
			out = append(out, s)
		}
	}
	return out
}

func capitalize(s string) string {
	if s == "" {
		return s
	}
	r := []rune(strings.ToLower(s))
	r[0] = unicode.ToUpper(r[0])
	return string(r)
}

// table returns a section of the profile, or nil when it is absent or not a table.
func table(d Profile, name string) map[string]any {
	m, _ := d[name].(map[string]any)
	return m
}

func has(m map[string]any, key string) bool { _, ok := m[key]; return ok }

// text formats a TOML value the way a Python f-string did.
func text(v any) string {
	switch x := v.(type) {
	case bool:
		if x {
			return "True"
		}
		return "False"
	case nil:
		return "None"
	}
	return fmt.Sprint(v)
}

func textOr(v any, def string) string {
	if v == nil {
		return def
	}
	return text(v)
}

func onOff(v any) string {
	if truthy(v) {
		return "on"
	}
	return "off"
}

// truthy is Python's idea of true, which the 0.3.x profiles were written against.
func truthy(v any) bool {
	switch x := v.(type) {
	case nil:
		return false
	case bool:
		return x
	case int64:
		return x != 0
	case int:
		return x != 0
	case float64:
		return x != 0
	case string:
		return x != ""
	case []any:
		return len(x) > 0
	case map[string]any:
		return len(x) > 0
	}
	return true
}
