package cli

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"sort"
	"strconv"
	"strings"

	"github.com/Order52/truesense/internal/api"
	"github.com/Order52/truesense/internal/cli/command"
	"github.com/Order52/truesense/internal/hw"
	"github.com/Order52/truesense/internal/powermgr"
)

func (c *CLI) power() *command.Cmd {
	return &command.Cmd{
		Name: "power", Short: "power mode (quiet balanced performance turbo eco) and the automatic profile",
		Run: c.powerStatus,
		Sub: []*command.Cmd{
			{Name: "status", Run: c.powerStatus},
			{Name: "list", Run: c.powerList},
			{Name: "set", Usage: "MODE", Run: c.powerSet},
			{Name: "next", Run: func(cc *command.Ctx) error { return c.powerStep(cc, c.api.NextPower) }},
			{Name: "prev", Run: func(cc *command.Ctx) error { return c.powerStep(cc, c.api.PrevPower) }},
			{Name: "smart", Usage: "[list|current|show|use|copy|save|delete] [NAME] [NEW]", Run: c.powerSmart,
				Short: "Smart auto profiles: list | current | show NAME | use NAME | copy NAME [NEW] | save NAME [--from BASE] --set key=value ... | delete NAME (built-in profiles are read-only: copy them to edit)",
				Flags: func(fs *flag.FlagSet) {
					fs.String("from", "", "start from this profile")
					declStrings(fs, "set", "KEY=VALUE (repeatable)")
				}},
			{Name: "auto", Usage: "[off|native|smart|on|toggle|status]", Run: c.powerAuto,
				Short: "automatic power mode: off | native (PredatorSense-like: power source only) | smart (heat, load and battery aware); on = smart"},
			{Name: "hold", Usage: "[MINUTES]", Run: c.powerHold, Short: "keep the current power mode against the automatic profile"},
			{Name: "release", Short: "end a hold", Run: c.powerRelease},
			{Name: "managers", Usage: "[list|stop|restore]", Run: c.powerManagers,
				Short: "other power managers (auto-cpufreq, TLP, tuned, power-profiles-daemon, system76-power): list | stop (TrueSense manages power: stops them, sets sane CPU defaults, remembers) | restore"},
			{Name: "game", Usage: "[list|add|remove|clear] [NAME] [MODE]", Run: c.powerGame,
				Short: "game rules: a program raises the power mode while it runs: list | add NAME MODE | remove NAME | clear"},
		},
	}
}

func (c *CLI) powerStatus(cc *command.Ctx) error {
	if err := cc.Want(0, 0); err != nil {
		return err
	}
	st, err := c.api.PowerStatus()
	if err != nil {
		return err
	}
	text := "power mode: " + st.Mode
	if st.AutoProfile != nil {
		on := "off"
		if *st.AutoProfile {
			on = "on"
		}
		text += "   (automatic profile " + on
		if st.HoldLeftS != nil && *st.HoldLeftS > 0 {
			text += fmt.Sprintf(", manual choice held for %d more min", *st.HoldLeftS/60)
		}
		text += ")"
	}
	cc.Emit(st, text)
	return nil
}

func (c *CLI) powerList(cc *command.Ctx) error {
	if err := cc.Want(0, 0); err != nil {
		return err
	}
	modes, err := c.api.PowerModes()
	if err != nil {
		return err
	}
	cc.Emit(modes, strings.Join(modes, "  "))
	return nil
}

// holdMinutes is how long a power mode picked on the command line is held against the
// automatic profile.
const holdMinutes = 30

func (c *CLI) powerSet(cc *command.Ctx) error {
	if err := cc.Want(1, 1, "mode"); err != nil {
		return err
	}
	mode, err := cc.Choice(0, "mode", "", hw.ModeNames...)
	if err != nil {
		return err
	}
	m, err := c.api.SetPower(mode, holdMinutes)
	if err != nil {
		return err
	}
	cc.Emit(m, fmt.Sprintf("power mode: %s   (held %d min against the automatic profile; `truesense power release` to end)", m, holdMinutes))
	return nil
}

func (c *CLI) powerStep(cc *command.Ctx, step func(ctx context.Context) (string, error)) error {
	if err := cc.Want(0, 0); err != nil {
		return err
	}
	msg, err := step(cc.Context)
	if err != nil {
		return err
	}
	cc.Emit(msg, "")
	return nil
}

func (c *CLI) powerAuto(cc *command.Ctx) error {
	if err := cc.Want(0, 1, "action"); err != nil {
		return err
	}
	action, err := cc.Choice(0, "action", "status", "off", "native", "smart", "on", "toggle", "status")
	if err != nil {
		return err
	}
	want, err := c.api.ApplyAutoMode(action)
	if err != nil {
		return err
	}
	text := map[string]string{
		api.AutoOff:    "automatic power mode: off (you choose)",
		api.AutoNative: "automatic power mode: native (power source only, like PredatorSense: AC balanced, battery eco)",
		api.AutoSmart:  "automatic power mode: smart (heat, load and battery aware)",
	}[want]
	cc.Emit(want, text)
	return nil
}

func (c *CLI) powerHold(cc *command.Ctx) error {
	if err := cc.Want(0, 1, "minutes"); err != nil {
		return err
	}
	minutes := 30.0
	if cc.Arg(0) != "" {
		var err error
		if minutes, err = floatArg("minutes", cc.Arg(0)); err != nil {
			return err
		}
	}
	if err := c.api.HoldPower(minutes); err != nil {
		return err
	}
	cc.Emit(fmt.Sprintf("holding the current power mode for %s min", strconv.FormatFloat(minutes, 'g', -1, 64)), "")
	return nil
}

func (c *CLI) powerRelease(cc *command.Ctx) error {
	if err := cc.Want(0, 0); err != nil {
		return err
	}
	if err := c.api.ReleaseHold(); err != nil {
		return err
	}
	cc.Emit(api.MsgHoldReleased, "")
	return nil
}

func (c *CLI) powerManagers(cc *command.Ctx) error {
	if err := cc.Want(0, 1, "action"); err != nil {
		return err
	}
	action, err := cc.Choice(0, "action", "list", "list", "stop", "restore")
	if err != nil {
		return err
	}
	switch action {
	case "list":
		r, err := c.api.PowerManagersStatus()
		if err != nil {
			return err
		}
		state := "off"
		if r.On {
			state = "ON"
		}
		var lines []string
		for _, m := range r.Found {
			lines = append(lines, fmt.Sprintf("  %-24s %s, %s", m.Unit, pick(m.Active, "running", "not running"), pick(m.Enabled, "enabled", "disabled")))
		}
		if len(lines) == 0 {
			lines = []string{"  no other power manager is running or enabled"}
		}
		cc.Emit(r, "TrueSense manages power: "+state+"\n"+join(lines))
	case "stop":
		r, err := c.api.StopPowerManagers()
		if err != nil {
			return err
		}
		text := "stopped: " + orElse(strings.Join(r.Stopped, ", "), "nothing (no other power manager was running)")
		if len(r.Failed) > 0 {
			text += "\nCOULD NOT STOP: " + strings.Join(r.Failed, ", ")
		}
		text += fmt.Sprintf("\nCPU now: governor %s, energy preference %s, turbo %s\n(undo: truesense power managers restore)", textOrUnknown(r.CPU.Governor), textOrUnknown(r.CPU.EPP), turboText(r.CPU))
		cc.Emit(r, text)
	default:
		r, err := c.api.RestorePowerManagers()
		if err != nil {
			return err
		}
		cc.Emit(r, "restored: "+orElse(strings.Join(r.Restored, ", "), "nothing (TrueSense had not stopped anything)"))
	}
	return nil
}

func pick(b bool, yes, no string) string {
	if b {
		return yes
	}
	return no
}

func orElse(s, def string) string {
	if s == "" {
		return def
	}
	return s
}

func textOrUnknown(s *string) string {
	if s == nil {
		return "unknown"
	}
	return *s
}

func turboText(s powermgr.CPUState) string {
	switch {
	case s.NoTurbo != nil && *s.NoTurbo == "0":
		return "on"
	case s.NoTurbo != nil && *s.NoTurbo != "":
		return "OFF"
	}
	return "n/a"
}

func (c *CLI) powerGame(cc *command.Ctx) error {
	if err := cc.Want(0, 3, "action", "name", "mode"); err != nil {
		return err
	}
	action, err := cc.Choice(0, "action", "list", "list", "add", "remove", "clear")
	if err != nil {
		return err
	}
	mode, err := cc.Choice(2, "mode", "", hw.ModeNames...)
	if err != nil {
		return err
	}
	r, err := c.api.GameRules(action, cc.Arg(1), mode)
	if err != nil {
		return err
	}
	names := make([]string, 0, len(r.Rules))
	for k := range r.Rules {
		names = append(names, k)
	}
	sort.Strings(names)
	var lines []string
	for _, k := range names {
		lines = append(lines, fmt.Sprintf("  %-28s %s", k, r.Rules[k]))
	}
	text := orElse(join(lines), "  (no game rules)")
	if r.Running != nil && *r.Running != "" {
		text += fmt.Sprintf("\n  running now: %s mode", *r.Running)
	}
	cc.Emit(r, text)
	return nil
}

// profileLines prints the settings of a profile one per line, in the order of keys, the way the
// 0.3.x tool did ("  ac_idle = balanced").
func profileLines(v any, keys []string) string {
	b, _ := json.Marshal(v)
	var m map[string]any
	_ = json.Unmarshal(b, &m)
	lines := make([]string, 0, len(keys))
	for _, k := range keys {
		lines = append(lines, fmt.Sprintf("  %s = %s", k, pyValue(m[k])))
	}
	return join(lines)
}

// listLines prints a list of profiles with a star on the one in use and the description of the
// built-in ones.
func listLines(l api.NamedList) string {
	lines := make([]string, 0, len(l.Names))
	for _, n := range l.Names {
		line := "  " + n
		if n == l.Current {
			line = "* " + n
		}
		if d, ok := l.Descriptions[n]; ok {
			line += "  - " + d
		}
		lines = append(lines, line)
	}
	return join(lines)
}

func (c *CLI) powerSmart(cc *command.Ctx) error {
	if err := cc.Want(0, 3, "action", "name", "new"); err != nil {
		return err
	}
	action, err := cc.Choice(0, "action", "current", "list", "current", "show", "use", "copy", "save", "delete")
	if err != nil {
		return err
	}
	name := cc.Arg(1)
	switch action {
	case "list":
		l, err := c.api.SmartProfiles()
		if err != nil {
			return err
		}
		cc.Emit(l.Names, listLines(l))
	case "current":
		d, err := c.api.SmartProfileInUse()
		if err != nil {
			return err
		}
		cc.Emit(d, fmt.Sprintf("Smart auto profile in use: %s\n%s", d.Name, profileLines(d.Profile, api.SmartSettingKeys)))
	case "show":
		n := orElse(name, "truesense")
		p, err := c.api.SmartProfile(n)
		if err != nil {
			return err
		}
		cc.Emit(p, n+"\n"+profileLines(p, api.SmartSettingKeys))
	case "use":
		used, err := c.api.UseSmartProfile(name)
		if err != nil {
			return err
		}
		cc.Emit(used, "Smart auto profile in use: "+used)
	case "copy":
		n, err := c.api.CopySmartProfile(name, cc.Arg(2))
		if err != nil {
			return err
		}
		cc.Emit(n, fmt.Sprintf("copied '%s' to '%s' (edit: truesense power smart save %s --set ac_busy=turbo)", name, n, n))
	case "save":
		p, err := c.api.SaveSmartProfile(name, flagString(cc, "from"), flagStrings(cc, "set"))
		if err != nil {
			return err
		}
		cc.Emit(p, fmt.Sprintf("saved smart profile '%s' (use it: truesense power smart use %s)", name, name))
	case "delete":
		if err := c.api.DeleteSmartProfile(name); err != nil {
			return err
		}
		cc.Emit(name, fmt.Sprintf("deleted smart profile '%s'", name))
	}
	return nil
}
