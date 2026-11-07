package cli

import (
	"flag"
	"fmt"
	"strconv"
	"strings"

	"github.com/Order52/truesense/internal/api"
	"github.com/Order52/truesense/internal/cli/command"
	"github.com/Order52/truesense/internal/curves"
)

func (c *CLI) fan() *command.Cmd {
	return &command.Cmd{
		Name: "fan", Short: "fans: firmware auto, max, manual %, smart curve",
		Run: c.fanStatus,
		Sub: []*command.Cmd{
			{Name: "status", Run: c.fanStatus},
			{Name: "auto", Short: "firmware control (0 rpm when cool)", Run: c.fanAuto},
			{Name: "max", Run: c.fanMax},
			{Name: "off", Short: "stop both fans (needs --confirm; only when cool; the service returns them to Auto if it gets warm)", Run: c.fanOff,
				Flags: func(fs *flag.FlagSet) {
					fs.Bool("confirm", false, "I understand the laptop can overheat with the fans off")
				}},
			{Name: "set", Usage: "CPU [GPU]", Short: "manual duty per fan, 20..100 %, or 0 to stop that fan (needs --confirm, only when cool)", Run: c.fanSet,
				Flags: func(fs *flag.FlagSet) {
					fs.Bool("confirm", false, "I understand a stopped fan can let the laptop overheat")
				}},
			{Name: "curve", Usage: "[list|current|show|use|save|delete|copy] [NAME] [NEW]", Run: c.fanCurve,
				Short: "smart-fan curves: list | current | show NAME | use NAME | copy NAME [NEW] | save NAME --cpu PTS --gpu PTS | delete NAME (built-in curves are read-only: copy them to edit)",
				Flags: func(fs *flag.FlagSet) {
					fs.String("cpu", "", "CPU points, e.g. 52:0,60:35,70:55,80:75,89:100 (temperature:percent, 0 = firmware auto)")
					fs.String("gpu", "", "GPU points")
					for _, k := range []string{"hysteresis", "down-rate", "predict", "couple", "battery-shift"} {
						declOptFloat(fs, k, "")
					}
					fs.String("from", "", "start from this curve and change only what you give")
				}},
			{Name: "smart", Usage: "[on|off|toggle|status]", Short: "the daemon's smart curve", Run: func(cc *command.Ctx) error {
				return tri(cc, "smart fan curve", nil, optional(c.api.SmartFan), c.api.SetSmartFan)
			}},
		},
	}
}

func (c *CLI) fanStatus(cc *command.Ctx) error {
	if err := cc.Want(0, 0); err != nil {
		return err
	}
	st, err := c.api.FanStatus()
	if err != nil {
		return err
	}
	cc.Emit(st, fmt.Sprintf("fans: %s   CPU %d rpm   GPU %d rpm   smart curve: %s", st.Mode, st.CPUFanRPM, st.GPUFanRPM, onoff(st.Smart)))
	return nil
}

func (c *CLI) fanAuto(cc *command.Ctx) error {
	if err := cc.Want(0, 0); err != nil {
		return err
	}
	if err := c.api.FanAuto(); err != nil {
		return err
	}
	cc.Emit(api.MsgFanAuto, "")
	return nil
}

func (c *CLI) fanMax(cc *command.Ctx) error {
	if err := cc.Want(0, 0); err != nil {
		return err
	}
	if err := c.api.FanMax(); err != nil {
		return err
	}
	cc.Emit(api.MsgFanMax, "")
	return nil
}

func (c *CLI) fanOff(cc *command.Ctx) error {
	if err := cc.Want(0, 0); err != nil {
		return err
	}
	if err := c.api.FanOff(flagBool(cc, "confirm")); err != nil {
		return err
	}
	cc.Emit(api.MsgFanOff, "")
	return nil
}

func (c *CLI) fanSet(cc *command.Ctx) error {
	if err := cc.Want(1, 2, "cpu", "gpu"); err != nil {
		return err
	}
	cpu, err := intArg("cpu", cc.Arg(0))
	if err != nil {
		return err
	}
	var gpu *int
	if cc.Arg(1) != "" {
		g, err := intArg("gpu", cc.Arg(1))
		if err != nil {
			return err
		}
		gpu = &g
	}
	if err := c.api.FanSet(cpu, gpu, flagBool(cc, "confirm")); err != nil {
		return err
	}
	g := cpu
	if gpu != nil {
		g = *gpu
	}
	text := fmt.Sprintf("fans: manual %d %% / %d %%", cpu, g)
	if cpu == 0 || g == 0 {
		text += api.FanStopNote
	}
	cc.Emit(text, "")
	return nil
}

// points prints curve points the way the 0.3.x tool did: [[52, 0], [60, 35]].
func points(p []curves.Point) string {
	parts := make([]string, len(p))
	for i, pt := range p {
		parts[i] = "[" + strconv.FormatFloat(pt[0], 'f', -1, 64) + ", " + strconv.FormatFloat(pt[1], 'f', -1, 64) + "]"
	}
	return "[" + strings.Join(parts, ", ") + "]"
}

func (c *CLI) fanCurve(cc *command.Ctx) error {
	if err := cc.Want(0, 3, "action", "name", "new"); err != nil {
		return err
	}
	action, err := cc.Choice(0, "action", "current", "list", "current", "show", "use", "save", "delete", "copy")
	if err != nil {
		return err
	}
	name := cc.Arg(1)
	switch action {
	case "list":
		l, err := c.api.FanCurves()
		if err != nil {
			return err
		}
		cc.Emit(l.Names, listLines(l))
	case "current":
		d, err := c.api.FanCurveInUse()
		if err != nil {
			return err
		}
		cc.Emit(d, fmt.Sprintf("fan curve in use: %s\n  cpu %s\n  gpu %s", d.Name, points(d.Curve.CPU), points(d.Curve.GPU)))
	case "show":
		n := orElse(name, "truesense")
		cv, err := c.api.FanCurve(n)
		if err != nil {
			return err
		}
		cc.Emit(cv, fmt.Sprintf("%s\n  cpu %s\n  gpu %s\n  hysteresis=%s  down_rate=%s  predict=%s  couple=%s  battery_shift=%s", n, points(cv.CPU), points(cv.GPU),
			pyFloat(cv.Hysteresis), pyFloat(cv.DownRate), pyFloat(cv.Predict), strconv.FormatFloat(cv.Couple, 'f', -1, 64), pyFloat(cv.BatteryShift)))
	case "use":
		used, err := c.api.UseFanCurve(name)
		if err != nil {
			return err
		}
		cc.Emit(used, "fan curve in use: "+used)
	case "save":
		saved, err := c.api.SaveFanCurve(name, api.CurveEdit{
			CPU: flagString(cc, "cpu"), GPU: flagString(cc, "gpu"), From: flagString(cc, "from"),
			Hysteresis: flagOptFloat(cc, "hysteresis"), DownRate: flagOptFloat(cc, "down-rate"), Predict: flagOptFloat(cc, "predict"),
			Couple: flagOptFloat(cc, "couple"), BatteryShift: flagOptFloat(cc, "battery-shift"),
		})
		if err != nil {
			return err
		}
		cc.Emit(saved, fmt.Sprintf("saved fan curve '%s' (use it: truesense fan curve use %s)", name, name))
	case "copy":
		n, err := c.api.CopyFanCurve(name, cc.Arg(2))
		if err != nil {
			return err
		}
		cc.Emit(n, fmt.Sprintf("copied '%s' to '%s' (edit it: truesense fan curve save %s --cpu ...)", name, n, n))
	case "delete":
		if err := c.api.DeleteFanCurve(name); err != nil {
			return err
		}
		cc.Emit(name, fmt.Sprintf("deleted fan curve '%s'", name))
	}
	return nil
}
