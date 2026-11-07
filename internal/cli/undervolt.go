package cli

import (
	"flag"
	"fmt"
	"strconv"
	"strings"

	"github.com/Order52/truesense/internal/api"
	"github.com/Order52/truesense/internal/cli/command"
	"github.com/Order52/truesense/internal/undervolt"
)

func (c *CLI) gpuUndervolt(cc *command.Ctx) error {
	if err := cc.Want(1, 3, "action", "name", "new"); err != nil {
		return err
	}
	action, err := cc.Choice(0, "action", "", "get", "list", "show", "use", "reset", "copy", "save", "delete", "boot")
	if err != nil {
		return err
	}
	name, req := cc.Arg(1), api.UndervoltRequest{Confirm: flagBool(cc, "confirm"), Force: flagBool(cc, "force")}
	capText := func(mhz int) string {
		if mhz == 0 {
			return "none"
		}
		return strconv.Itoa(mhz)
	}
	switch action {
	case "get":
		r, err := c.api.GPUUndervoltState()
		if err != nil {
			return err
		}
		text := "not readable: " + r.Error
		if r.OffsetMHz != nil {
			rng := [2]int{}
			if r.RangeMHz != nil {
				rng = *r.RangeMHz
			}
			text = fmt.Sprintf("offset %+d MHz (driver range %d..%d)", *r.OffsetMHz, rng[0], rng[1])
		}
		if r.Applied != nil {
			text += fmt.Sprintf("   (applied: %s, cap %d MHz)", r.Applied.Name, r.Applied.Cap)
		}
		if r.Boot != nil {
			text += fmt.Sprintf("   (at boot: %s)", r.Boot.Name)
		}
		cc.Emit(r, text)
	case "list":
		l, err := c.api.GPUUndervolts()
		if err != nil {
			return err
		}
		lines := make([]string, 0, len(l.Names))
		for _, n := range l.Names {
			p, err := c.api.GPUUndervoltProfile(n)
			if err != nil {
				return err
			}
			line := fmt.Sprintf("  %-11s offset %+4d MHz  cap %5s", n, p.Offset, capText(p.Cap))
			if d, ok := l.Descriptions[n]; ok {
				line += "  - " + d
			}
			lines = append(lines, line)
		}
		cc.Emit(l.Names, join(lines))
	case "show":
		n := orElse(name, "truesense")
		p, err := c.api.GPUUndervoltProfile(n)
		if err != nil {
			return err
		}
		cc.Emit(p, fmt.Sprintf("%s: offset %+d MHz, cap %s MHz   %s", n, p.Offset, capText(p.Cap), p.Description))
	case "reset":
		r, err := c.api.ResetGPUUndervolt()
		if err != nil {
			return err
		}
		cc.Emit(r, "GPU undervolt reset: offset 0, no clock cap")
	case "use":
		r, err := c.api.ApplyGPUUndervolt(name, req)
		if err != nil {
			return err
		}
		cc.Emit(r, fmt.Sprintf("applied '%s': offset %+d MHz, cap %s MHz   (a reboot returns to stock; `truesense gpu undervolt reset` does it now)", name, r.OffsetMHz, capText(r.CapMHz)))
	case "copy":
		n, err := c.api.CopyGPUUndervolt(name, cc.Arg(2))
		if err != nil {
			return err
		}
		cc.Emit(n, fmt.Sprintf("copied '%s' to '%s'", name, n))
	case "save":
		p, err := c.api.SaveGPUUndervolt(name, flagOptInt(cc, "offset"), flagInt(cc, "cap"))
		if err != nil {
			return err
		}
		cc.Emit(p, fmt.Sprintf("saved GPU undervolt profile '%s' (use it: truesense gpu undervolt use %s --confirm)", name, name))
	case "delete":
		if err := c.api.DeleteGPUUndervolt(name); err != nil {
			return err
		}
		cc.Emit(name, fmt.Sprintf("deleted GPU undervolt profile '%s'", name))
	case "boot":
		boot, err := c.api.SetGPUUndervoltBoot(name, req)
		if err != nil {
			return err
		}
		if boot == nil {
			cc.Emit(nil, "no GPU undervolt is applied at boot")
			return nil
		}
		cc.Emit(boot, fmt.Sprintf("'%s' will be applied at boot (guarded, like the CPU undervolt)", name))
	}
	return nil
}

func (c *CLI) cpu() *command.Cmd {
	return &command.Cmd{
		Name: "cpu", Short: "CPU power limits and the guarded undervolt",
		Run: c.cpuInfo,
		Sub: []*command.Cmd{
			{Name: "info", Run: c.cpuInfo},
			{Name: "limits", Usage: "[PL1] [PL2]", Run: c.cpuLimits,
				Short: "CPU power limits PL1 (sustained) and PL2 (burst) in watts; no argument shows them; --auto goes back to automatic",
				Flags: func(fs *flag.FlagSet) {
					fs.Bool("auto", false, "go back to the automatic limits")
					fs.Bool("force", false, "allow values beyond the danger line")
				}},
			{Name: "undervolt", Usage: "get|list|show|use|reset|copy|save|delete|boot|set [NAME] [NEW]", Run: c.cpuUndervolt,
				Short: "CPU undervolt profiles (reset on reboot; applying needs --confirm): get | list | show NAME | use NAME --confirm | reset | copy NAME [NEW] | save NAME --core MV --cache MV | delete NAME | boot NAME|off --confirm",
				Flags: func(fs *flag.FlagSet) {
					declOptInt(fs, "core", "")
					declOptInt(fs, "cache", "")
					fs.Bool("confirm", false, "I understand this can freeze or reboot the laptop")
					fs.Bool("force", false, "allow values beyond the danger line")
					fs.Int("plane", 0, "set: 0 core, 2 cache")
				}},
		},
	}
}

func (c *CLI) cpuInfo(cc *command.Ctx) error {
	if err := cc.Want(0, 0); err != nil {
		return err
	}
	cc.Emit(c.api.CPUInfo(cc.Context), "")
	return nil
}

// pyFloat prints a number the way Python's repr does: 80.0, 37.5.
func pyFloat(f float64) string {
	s := strconv.FormatFloat(f, 'f', -1, 64)
	if !strings.Contains(s, ".") {
		s += ".0"
	}
	return s
}

func limitsText(d api.PowerLimits) string {
	num := func(p *float64) string {
		if p == nil {
			return "None"
		}
		return pyFloat(*p)
	}
	text := fmt.Sprintf("PL1 %s W, PL2 %s W   ", num(d.PL1W), num(d.PL2W))
	if d.User != nil {
		return text + fmt.Sprintf("(yours: %s/%s W)", strconv.FormatFloat(d.User.PL1, 'g', -1, 64), strconv.FormatFloat(d.User.PL2, 'g', -1, 64))
	}
	return text + fmt.Sprintf("(automatic: %d W, %d W when gaming)", d.Auto.NormalW, d.Auto.GamingW)
}

func (c *CLI) cpuLimits(cc *command.Ctx) error {
	if err := cc.Want(0, 2, "pl1", "pl2"); err != nil {
		return err
	}
	switch {
	case flagBool(cc, "auto"):
		d, err := c.api.ResetCPULimits()
		if err != nil {
			return err
		}
		cc.Emit(d, "automatic power limits: "+limitsText(d))
	case cc.Arg(0) == "":
		d, err := c.api.CPULimits()
		if err != nil {
			return err
		}
		cc.Emit(d, limitsText(d))
	default:
		pl1, err := floatArg("pl1", cc.Arg(0))
		if err != nil {
			return err
		}
		pl2 := pl1
		if cc.Arg(1) != "" {
			if pl2, err = floatArg("pl2", cc.Arg(1)); err != nil {
				return err
			}
		}
		r, err := c.api.SetCPULimits(pl1, pl2, flagBool(cc, "force"))
		if err != nil {
			return err
		}
		g := func(v float64) string { return strconv.FormatFloat(v, 'g', -1, 64) }
		cc.Emit(r, fmt.Sprintf("power limits set: PL1 %s W, PL2 %s W   (the service keeps them; `truesense cpu limits --auto` goes back)", g(r.PL1W), g(r.PL2W)))
	}
	return nil
}

func offsetsText(core, cache *int, errText string) string {
	if core == nil {
		return "not readable: " + errText
	}
	cacheText := "None"
	if cache != nil {
		cacheText = strconv.Itoa(*cache)
	}
	return fmt.Sprintf("core %d mV, cache %s mV", *core, cacheText)
}

func offsets(o undervolt.Offsets) string { return offsetsText(&o.CoreMV, &o.CacheMV, "") }

func (c *CLI) cpuUndervolt(cc *command.Ctx) error {
	if err := cc.Want(1, 3, "action", "name", "new"); err != nil {
		return err
	}
	action, err := cc.Choice(0, "action", "", "get", "list", "show", "use", "reset", "copy", "save", "delete", "boot", "set")
	if err != nil {
		return err
	}
	if err := oneOfFlag(cc, "plane", "0", "2"); err != nil {
		return err
	}
	name, req := cc.Arg(1), api.UndervoltRequest{Confirm: flagBool(cc, "confirm"), Force: flagBool(cc, "force")}
	switch action {
	case "get":
		r, err := c.api.CPUUndervoltState()
		if err != nil {
			return err
		}
		text := offsetsText(r.CoreMV, r.CacheMV, r.Error)
		if r.Applied != nil {
			text += fmt.Sprintf("   (applied: %s)", r.Applied.Name)
		}
		if r.Boot != nil {
			text += fmt.Sprintf("   (at boot: %s)", r.Boot.Name)
		}
		cc.Emit(r, text)
	case "list":
		l, err := c.api.CPUUndervolts()
		if err != nil {
			return err
		}
		lines := make([]string, 0, len(l.Names))
		for _, n := range l.Names {
			p, err := c.api.CPUUndervoltProfile(n)
			if err != nil {
				return err
			}
			line := fmt.Sprintf("  %-12s core %4d mV  cache %4d mV", n, p.Core, p.Cache)
			if d, ok := l.Descriptions[n]; ok {
				line += "  - " + d
			}
			lines = append(lines, line)
		}
		cc.Emit(l.Names, join(lines))
	case "show":
		n := orElse(name, "truesense")
		p, err := c.api.CPUUndervoltProfile(n)
		if err != nil {
			return err
		}
		cc.Emit(p, fmt.Sprintf("%s: core %d mV, cache %d mV   %s", n, p.Core, p.Cache, p.Description))
	case "reset":
		r, err := c.api.ResetCPUUndervolt()
		if err != nil {
			return err
		}
		cc.Emit(r, "undervolt reset: "+offsets(r))
	case "use":
		r, err := c.api.ApplyCPUUndervolt(name, req)
		if err != nil {
			return err
		}
		cc.Emit(r, fmt.Sprintf("applied '%s': %s   (a reboot returns to stock; `truesense cpu undervolt reset` does it now)", name, offsets(r)))
	case "set":
		if name == "" {
			return fmt.Errorf("usage: truesense cpu undervolt set -10 --confirm   (or use a profile)")
		}
		mv, err := intArg("mv", name)
		if err != nil {
			return err
		}
		r, err := c.api.SetCPUUndervolt(flagInt(cc, "plane"), mv, req)
		if err != nil {
			return err
		}
		cc.Emit(r, offsets(r))
	case "copy":
		n, err := c.api.CopyCPUUndervolt(name, cc.Arg(2))
		if err != nil {
			return err
		}
		cc.Emit(n, fmt.Sprintf("copied '%s' to '%s'", name, n))
	case "save":
		p, err := c.api.SaveCPUUndervolt(name, flagOptInt(cc, "core"), flagOptInt(cc, "cache"))
		if err != nil {
			return err
		}
		cc.Emit(p, fmt.Sprintf("saved undervolt profile '%s' (use it: truesense cpu undervolt use %s --confirm)", name, name))
	case "delete":
		if err := c.api.DeleteCPUUndervolt(name); err != nil {
			return err
		}
		cc.Emit(name, fmt.Sprintf("deleted undervolt profile '%s'", name))
	case "boot":
		boot, err := c.api.SetCPUUndervoltBoot(name, req)
		if err != nil {
			return err
		}
		if boot == nil {
			cc.Emit(nil, "no undervolt is applied at boot")
			return nil
		}
		cc.Emit(boot, fmt.Sprintf("'%s' will be applied at boot (guarded: skipped and switched off if the previous boot did not stay up 10 minutes)", name))
	}
	return nil
}
