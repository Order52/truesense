package cli

import (
	"strings"

	"github.com/Order52/truesense/internal/cli/command"
	"github.com/Order52/truesense/internal/hw"
	"github.com/Order52/truesense/internal/keybinds"
	"github.com/Order52/truesense/internal/perkeyfx"
)

// completeWords are the words the positional arguments of each command take, by command path.
// The shell completions offer them next to the flags, so `truesense power set <TAB>` lists the
// power modes.
func completeWords() map[string][]string {
	profileVerbs := []string{"list", "current", "show", "use", "copy", "save", "delete"}
	return map[string][]string{
		"power set":            hw.ModeNames,
		"power smart":          profileVerbs,
		"power auto":           {"off", "native", "smart", "on", "toggle", "status"},
		"power managers":       {"list", "stop", "restore"},
		"power game":           append([]string{"list", "add", "remove", "clear"}, hw.ModeNames...),
		"fan smart":            triWords,
		"fan curve":            {"list", "current", "show", "use", "save", "delete", "copy"},
		"battery full":         {"start", "cancel", "status"},
		"battery limit":        triWords,
		"battery calibration":  triWords,
		"battery usb-charge":   {"status", "0", "10", "20", "30"},
		"display overdrive":    triWords,
		"boot sound":           triWords,
		"gpu set":              hw.GPUModeNames,
		"gpu undervolt":        {"get", "list", "show", "use", "reset", "copy", "save", "delete", "boot"},
		"rear set":             hw.RearModeNames,
		"update auto":          triWords,
		"startup set":          {"keyboard", "rear", "profile"},
		"keyboard fx":          append([]string{"off"}, perkeyfx.ModeNames()...),
		"keyboard design":      {"list", "apply", "save"},
		"keyboard timeout":     triWords,
		"keyboard operating":   {"on", "off", "status"},
		"keyboard profile":     {"list", "load", "save", "delete"},
		"audio output":         {"status", "speakers", "headphones", "toggle", "cycle"},
		"audio mic":            {"status", "mute", "unmute", "toggle", "volume", "boost"},
		"audio jack":           {"auto", "headphones", "headset"},
		"audio voice":          {"status", "set", "enable", "disable", "next", "apply", "watch", "notify"},
		"audio speaker-filter": {"on", "off", "status"},
		"audio dts":            {"status", "on", "off", "mode", "profiles", "devices", "all", "setup"},
		"keys set":             keybinds.Gestures(),
		"keys reset":           keybinds.Gestures(),
		"keys profile":         profileVerbs,
		"cpu undervolt":        {"get", "list", "show", "use", "reset", "copy", "save", "delete", "boot", "set"},
		"completion":           {"bash", "zsh", "fish"},
		"gui":                  guiPages,
		"tui":                  tuiPages,
	}
}

// applyCompletions sets Complete on every command that has an entry.
func applyCompletions(cmds []*command.Cmd, prefix string, words map[string][]string) {
	for _, c := range cmds {
		path := strings.TrimSpace(prefix + " " + c.Name)
		if w, ok := words[path]; ok {
			c.Complete = w
		}
		applyCompletions(c.Sub, path, words)
	}
}
