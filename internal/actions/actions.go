// Package actions is the one vocabulary shared by the PREDATOR key, saved key profiles,
// `truesense keys test` and the window: an action is `name` or `name:argument`, runs once and
// answers with a short message the caller shows on screen.
//
// Unknown or malformed actions are rejected when they are configured (Validate), never at
// key-press time, so a typo can never make the key do something unexpected. Everything an
// action touches (the daemon, the firmware, the keyboard, the sound system, the user state,
// the shell) is reached through the small interfaces of Env, so each action is tested with
// fakes and no process, device or socket.
package actions

import (
	"fmt"
	"slices"
	"sort"
	"strconv"
	"strings"

	"github.com/Order52/truesense/internal/hw"
)

// catalogue is the reference text `truesense keys actions` prints and the window shows.
const catalogue = "An action is `name` or `name:argument`. Each returns a short message (shown on screen by the caller). Unknown names are rejected when configuring, never at key-press time.\n" +
	"\n" +
	"  none\n" +
	"  power-next | power-prev | power:<mode> | power-cycle:<mode,mode,...>     eco quiet balanced performance turbo (a manual choice holds for [profile] hold_minutes)\n" +
	"  fan:smart | fan:auto | fan:max | fan:<percent> | fan-max-toggle | smart-fan-toggle\n" +
	"  profile:<name> | profile-next                                              saved profiles (see `truesense profile`)\n" +
	"  output-toggle | output-cycle | output:speakers | output:headphones\n" +
	"  mic-toggle | mic:mute | mic:unmute | voice-next\n" +
	"  touchpad-toggle | touchpad:on | touchpad:off\n" +
	"  keyboard:on | keyboard:off | keyboard:toggle | keyboard-effect-next | keyboard-profile:<name>\n" +
	"  rear:on | rear:off | rear:toggle | rear-effect-next\n" +
	"  battery-limit-toggle | overdrive-toggle\n" +
	"  look:<name> | auto:<off|native|smart> | auto-next                           lighting looks (truesense look), the mode control\n" +
	"  exec:<shell command>      runs as you, detached      |      notify:<text>"

// Catalogue returns the documented list of every action, one family per line.
func Catalogue() string { return catalogue }

// fixedNames take no argument; argNames need one.
var (
	fixedNames = []string{"auto-next", "none", "power-next", "power-prev", "fan-max-toggle", "smart-fan-toggle", "profile-next", "output-toggle", "output-cycle", "mic-toggle", "voice-next",
		"touchpad-toggle", "keyboard-effect-next", "rear-effect-next", "battery-limit-toggle", "overdrive-toggle"}
	argNames = []string{"look", "auto", "power", "power-cycle", "fan", "profile", "output", "mic", "touchpad", "keyboard", "keyboard-profile", "rear", "exec", "notify"}
)

// Names lists every action name (with and without argument), sorted, so a test or a UI can
// check that each one is documented in Catalogue.
func Names() []string {
	out := append(slices.Clone(fixedNames), argNames...)
	sort.Strings(out)
	return out
}

// Split separates `name:argument` at the first colon and trims both parts (the argument of
// exec: may itself contain colons).
func Split(spec string) (name, arg string) {
	name, arg, _ = strings.Cut(strings.TrimSpace(spec), ":")
	return strings.TrimSpace(name), strings.TrimSpace(arg)
}

// Validator checks actions. LookNames, when set, lets `look:NAME` be checked against the
// saved looks; without it only the syntax of a look action is checked.
type Validator struct {
	LookNames func() []string
}

// Validate is the syntax-only check (no saved looks consulted), for callers that have no
// look store at hand.
func Validate(spec string) error { return Validator{}.Validate(spec) }

// Validate returns nil if the action is valid and otherwise the reason, in the words shown to
// the user. It is used when configuring keys and profiles, so it must be strict.
func (v Validator) Validate(spec string) error {
	name, arg := Split(spec)
	if slices.Contains(fixedNames, name) {
		if arg != "" {
			return fmt.Errorf("'%s' takes no argument", name)
		}
		return nil
	}
	if !slices.Contains(argNames, name) {
		return fmt.Errorf("unknown action '%s'", name)
	}
	if arg == "" {
		return fmt.Errorf("'%s' needs an argument (e.g. %s:...)", name, name)
	}
	switch name {
	case "auto":
		if arg != "off" && arg != "native" && arg != "smart" {
			return fmt.Errorf("auto: off | native | smart")
		}
	case "look":
		if v.LookNames != nil && !slices.Contains(v.LookNames(), arg) {
			return fmt.Errorf("no saved look '%s' (truesense look list)", arg)
		}
	case "power":
		if !slices.Contains(hw.ModeNames, arg) {
			return fmt.Errorf("power mode must be one of %s", strings.Join(hw.ModeNames, ", "))
		}
	case "power-cycle":
		for _, m := range strings.Split(arg, ",") {
			if !slices.Contains(hw.ModeNames, m) {
				return fmt.Errorf("power-cycle needs a comma list of power modes")
			}
		}
	case "fan":
		if _, ok := fanPercent(arg); arg != "smart" && arg != "auto" && arg != "max" && !ok {
			return fmt.Errorf("fan: smart | auto | max | 20..100")
		}
	case "output":
		if arg != "speakers" && arg != "headphones" {
			return fmt.Errorf("output: speakers | headphones")
		}
	case "mic":
		if arg != "mute" && arg != "unmute" {
			return fmt.Errorf("mic: mute | unmute")
		}
	case "touchpad":
		if arg != "on" && arg != "off" {
			return fmt.Errorf("touchpad: on | off")
		}
	case "keyboard", "rear":
		if arg != "on" && arg != "off" && arg != "toggle" {
			return fmt.Errorf("%s: on | off | toggle", name)
		}
	}
	return nil
}

// fanPercent parses a manual duty: digits only, 20..100 (a fan is never stopped by a key).
func fanPercent(arg string) (int, bool) {
	for _, r := range arg {
		if r < '0' || r > '9' {
			return 0, false
		}
	}
	n, err := strconv.Atoi(arg)
	if err != nil || n < 20 || n > 100 {
		return 0, false
	}
	return n, true
}
