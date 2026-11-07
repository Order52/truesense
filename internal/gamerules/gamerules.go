// Package gamerules maps a program name to the power mode to use while it runs
// ("hl2_linux": "performance", "eldenring.exe": "turbo").
//
// The service looks at the running programs every few seconds. A matching program raises the
// automatic power mode (Native and Smart auto both honour it); when the program ends the
// automatic profile takes over again by itself. Names match the process name (as `ps -e`
// shows it, 15 characters), the program file's name, or, for Wine/Proton games, any `.exe` on
// the command line. Case does not matter. Turbo still backs off to Performance if the laptop
// overheats, and Eco is forced when the battery is low.
//
// Matching is pure: Running works on a list of processes passed in. Reading that list from
// /proc is the separate, small ListProcesses.
package gamerules

import (
	"encoding/json"
	"regexp"
	"sort"
	"strings"

	"github.com/Order52/truesense/internal/firmware"
)

// Modes are the power modes a rule can ask for, from the lowest to the highest rank. When
// several running programs match, the highest mode wins.
func Modes() []string { return []string{"eco", "quiet", "balanced", "performance", "turbo"} }

// MaxRules bounds how many rules there can be.
const MaxRules = 64

// commLen is how many characters the kernel keeps of a process name (TASK_COMM_LEN - 1).
const commLen = 15

// maxArgs is how many leading command-line arguments are looked at for Wine/Proton games.
const maxArgs = 12

// Rules maps a lower-case program name to a power mode. It marshals as a plain JSON object,
// the form the daemon keeps and the clients show.
type Rules map[string]string

var nameRE = regexp.MustCompile(`^[a-z0-9._+ -]{1,80}$`)

func modeRank(m string) int {
	for i, x := range Modes() {
		if x == m {
			return i
		}
	}
	return -1
}

// CleanName normalises a program name (trimmed, lower case) and checks it: letters, digits,
// . _ + - and spaces, 1 to 80 characters. Anything else, a path separator above all, is refused.
func CleanName(name string) (string, error) {
	n := strings.ToLower(strings.TrimSpace(name))
	if !nameRE.MatchString(n) {
		return "", firmware.Errorf("program name: letters, digits, . _ + - and spaces (max 80)")
	}
	return n, nil
}

func modeError(key string) error {
	return firmware.Errorf("%s: mode must be one of %s", key, strings.Join(Modes(), ", "))
}

// Validate checks rules that arrived as decoded JSON (a name -> mode map) and returns them
// with the names cleaned. When two names clean to the same one, the last in sorted order wins.
func Validate(v any) (Rules, error) {
	data, err := json.Marshal(v)
	if err != nil {
		return nil, firmware.Errorf("game rules are a name -> mode map")
	}
	return Parse(data)
}

// Parse is Validate for JSON text.
func Parse(data []byte) (Rules, error) {
	var obj map[string]json.RawMessage
	if err := json.Unmarshal(data, &obj); err != nil || obj == nil {
		return nil, firmware.Errorf("game rules are a name -> mode map")
	}
	keys := make([]string, 0, len(obj))
	for k := range obj {
		keys = append(keys, k)
	}
	sort.Strings(keys) // a deterministic first error
	out := Rules{}
	for _, k := range keys {
		var mode *string
		if json.Unmarshal(obj[k], &mode) != nil || mode == nil || modeRank(*mode) < 0 {
			return nil, modeError(k)
		}
		clean, err := CleanName(k)
		if err != nil {
			return nil, err
		}
		out[clean] = *mode
	}
	if len(out) > MaxRules {
		return nil, firmware.Errorf("at most %d rules", MaxRules)
	}
	return out, nil
}

// Add returns the rules with name (cleaned) set to mode, leaving the receiver unchanged.
func (r Rules) Add(name, mode string) (Rules, error) {
	k, err := CleanName(name)
	if err != nil {
		return nil, err
	}
	if modeRank(mode) < 0 {
		return nil, firmware.Errorf("mode: %s", strings.Join(Modes(), ", "))
	}
	out := r.clone()
	out[k] = mode
	if len(out) > MaxRules {
		return nil, firmware.Errorf("at most %d rules", MaxRules)
	}
	return out, nil
}

// Remove returns the rules without name (cleaned), leaving the receiver unchanged. A name
// with no rule is an error, so a typo is noticed.
func (r Rules) Remove(name string) (Rules, error) {
	k, err := CleanName(name)
	if err != nil {
		return nil, err
	}
	if _, ok := r[k]; !ok {
		return nil, firmware.Errorf("no rule for '%s'", name)
	}
	out := r.clone()
	delete(out, k)
	return out, nil
}

func (r Rules) clone() Rules {
	out := make(Rules, len(r)+1)
	for k, v := range r {
		out[k] = v
	}
	return out
}

// Process is what the matcher needs of one running program: the kernel's name for it and the
// start of its command line (NUL-separated arguments).
type Process struct {
	Comm    string
	Cmdline []byte
}

// maxCmdline is how much of a command line is considered.
const maxCmdline = 4096

// Names returns the lower-case names one process answers to: its comm, the file name of its
// first argument, and the file name of any of the first twelve arguments ending in .exe (a
// Wine/Proton game runs as wine64-preloader with the game's .exe on the command line). File
// names are also given cut to 15 characters, because the kernel truncates comm to that; Running
// does the same cut when it looks a name up.
func (p Process) Names() map[string]bool {
	res := map[string]bool{}
	if comm := strings.ToLower(strings.TrimSpace(decode(p.Comm))); comm != "" {
		res[comm] = true
	}
	cmd := p.Cmdline
	if len(cmd) > maxCmdline {
		cmd = cmd[:maxCmdline]
	}
	i := 0
	for _, arg := range strings.Split(string(cmd), "\x00") {
		raw := decode(arg)
		if raw == "" {
			continue
		}
		if i >= maxArgs {
			break
		}
		base := raw
		base = strings.ReplaceAll(base, "\\", "/")
		if j := strings.LastIndex(base, "/"); j >= 0 {
			base = base[j+1:]
		}
		base = strings.ToLower(base)
		if i == 0 || strings.HasSuffix(base, ".exe") {
			res[base] = true
			res[truncateRunes(base, commLen)] = true
		}
		i++
	}
	return res
}

// decode makes text valid UTF-8, replacing bad bytes the way the Python decode did, so that
// cutting to 15 characters never splits a character.
func decode(s string) string { return string([]rune(s)) }

func truncateRunes(s string, n int) string {
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	return string(r[:n])
}

// Running returns the mode of the highest-ranked rule whose program is in procs, or "" and
// false when none is. procs is passed in (see ListProcesses) so this is a pure function.
func Running(rules Rules, procs []Process) (string, bool) {
	if len(rules) == 0 {
		return "", false
	}
	best, bestRank := "", -1
	for _, p := range procs {
		for n := range p.Names() {
			m, ok := rules[n]
			if !ok {
				m, ok = rules[truncateRunes(n, commLen)]
			}
			if ok {
				if r := modeRank(m); r > bestRank {
					best, bestRank = m, r
				}
			}
		}
	}
	return best, bestRank >= 0
}
