package cli

import (
	"encoding/json"
	"flag"
	"fmt"
	"os"

	"github.com/Order52/truesense/internal/api"
	"github.com/Order52/truesense/internal/cli/command"
	"github.com/Order52/truesense/internal/doctor"
	"github.com/Order52/truesense/internal/models"
	"golang.org/x/sys/unix"
)

// IsTerminal reports whether w is a terminal (a file that answers the terminal ioctl).
func IsTerminal(w any) bool {
	f, ok := w.(*os.File)
	if !ok {
		return false
	}
	_, err := unix.IoctlGetTermios(int(f.Fd()), unix.TCGETS)
	return err == nil
}

func (c *CLI) touchpad() *command.Cmd {
	return &command.Cmd{Name: "touchpad", Short: "touchpad on/off (Fn+F10): on | off | toggle | status", Run: func(cc *command.Ctx) error {
		if err := cc.Want(0, 0); err != nil {
			return err
		}
		return tri(cc, "touchpad", nil, c.api.Touchpad, c.api.SetTouchpad)
	}}
}

func (c *CLI) service() *command.Cmd {
	stream := func(run func(cc *command.Ctx) (int, error)) func(*command.Ctx) error {
		return func(cc *command.Ctx) error {
			if err := cc.Want(0, 0); err != nil {
				return err
			}
			_, err := run(cc) // systemctl's own exit code is its message: the 0.3.x command did not pass it on
			return err
		}
	}
	return &command.Cmd{
		Name: "service", Short: "the daemon",
		Run: stream(func(cc *command.Ctx) (int, error) { return c.api.ServiceStatus(cc.Context, cc.Out, cc.Err) }),
		Sub: []*command.Cmd{
			{Name: "status", Run: stream(func(cc *command.Ctx) (int, error) { return c.api.ServiceStatus(cc.Context, cc.Out, cc.Err) })},
			{Name: "restart", Run: stream(func(cc *command.Ctx) (int, error) { return c.api.RestartService(cc.Context, cc.Out, cc.Err) })},
			{Name: "logs", Run: stream(func(cc *command.Ctx) (int, error) { return c.api.ServiceLogs(cc.Context, cc.Out, cc.Err) })},
			{Name: "check", Short: "every unit, the kernel module and the control socket (read-only)", Run: c.serviceCheck},
			{Name: "repair", Short: "get the daemon, its health timer, the module and your user services running again (safe to repeat; touches no setting)", Run: c.serviceRepair,
				Flags: func(fs *flag.FlagSet) { fs.Bool("system", false, "the part that needs root (run through sudo)") }},
		},
	}
}

func (c *CLI) serviceCheck(cc *command.Ctx) error {
	if err := cc.Want(0, 0); err != nil {
		return err
	}
	rows := c.api.CheckServices(cc.Context)
	ok := api.ServicesOK(rows)
	cc.Emit(rows, api.RenderServices(rows)+pick(ok, "\n\nall good", "\n\nsomething is not running: truesense service repair"))
	if !ok {
		return command.ExitCode(1)
	}
	return nil
}

func (c *CLI) serviceRepair(cc *command.Ctx) error {
	if err := cc.Want(0, 0); err != nil {
		return err
	}
	if flagBool(cc, "system") { // the part sudo runs for the part below: one JSON line for the caller to read
		steps, err := c.api.RepairSystemServices(cc.Context)
		if err != nil {
			return err
		}
		b, _ := json.Marshal(steps)
		cc.Println(string(b))
		if !api.StepsOK(steps) {
			return command.ExitCode(1)
		}
		return nil
	}
	steps, err := c.api.RepairServices(cc.Context)
	if err != nil {
		return err
	}
	type row struct {
		Step string `json:"step"`
		OK   bool   `json:"ok"`
	}
	rows := make([]row, len(steps))
	lines := make([]string, len(steps))
	for i, s := range steps {
		rows[i] = row{s.What, s.OK}
		lines[i] = fmt.Sprintf("  [%s] %s", pick(s.OK, "ok ", "!! "), s.What)
	}
	cc.Emit(rows, join(lines))
	if !api.StepsOK(steps) {
		return command.ExitCode(1)
	}
	return nil
}

func (c *CLI) config() *command.Cmd {
	return &command.Cmd{
		Name: "config", Short: "configuration",
		Run: c.configShow,
		Sub: []*command.Cmd{
			{Name: "show", Run: c.configShow},
			{Name: "validate", Run: func(cc *command.Ctx) error {
				if err := cc.Want(0, 0); err != nil {
					return err
				}
				problems := c.api.ValidateConfig()
				if problems == nil {
					problems = []string{}
				}
				cc.Emit(problems, orElse(join(problems), "configuration valid"))
				if len(problems) > 0 {
					return command.ExitCode(1)
				}
				return nil
			}},
			{Name: "path", Run: func(cc *command.Ctx) error {
				if err := cc.Want(0, 0); err != nil {
					return err
				}
				cc.Emit(c.api.ConfigPaths(), "")
				return nil
			}},
		},
	}
}

func (c *CLI) configShow(cc *command.Ctx) error {
	if err := cc.Want(0, 0); err != nil {
		return err
	}
	values, err := c.api.ConfigValues()
	if err != nil {
		return err
	}
	text, err := c.api.ConfigTOML()
	if err != nil {
		return err
	}
	cc.Emit(values, text)
	return nil
}

func (c *CLI) doctor() *command.Cmd {
	return &command.Cmd{Name: "doctor", Short: "check the whole stack and say how to fix problems", Run: c.doctorRun}
}

func (c *CLI) doctorRun(cc *command.Ctx) error {
	if err := cc.Want(0, 0); err != nil {
		return err
	}
	r := c.api.RunDoctor(cc.Context)
	if cc.JSON {
		if err := doctor.RenderJSON(cc.Out, r); err != nil {
			return err
		}
	} else {
		doctor.RenderText(cc.Out, r, IsTerminal(cc.Out) && os.Getenv("NO_COLOR") == "")
	}
	if code := r.ExitCode(); code != 0 {
		return command.ExitCode(code)
	}
	return nil
}

func (c *CLI) selftest() *command.Cmd {
	return &command.Cmd{Name: "selftest", Short: "set -> read back -> restore every firmware feature", Run: c.selftestRun, Flags: func(fs *flag.FlagSet) {
		fs.Bool("skip-fans", false, "leave the fans out")
	}}
}

func (c *CLI) selftestRun(cc *command.Ctx) error {
	if err := cc.Want(0, 0); err != nil {
		return err
	}
	if r := c.api.SelfTest(cc.Context, cc.Out, flagBool(cc, "skip-fans")); !r.OK() {
		return command.ExitCode(1)
	}
	return nil
}

func (c *CLI) probe() *command.Cmd {
	return &command.Cmd{Name: "probe", Short: "read-only report of this laptop for model support (never writes a setting)", Run: func(cc *command.Ctx) error {
		if err := cc.Want(0, 0); err != nil {
			return err
		}
		r := c.api.Probe()
		if cc.JSON {
			text, err := models.JSON(r)
			if err != nil {
				return err
			}
			cc.Println(text)
			return nil
		}
		cc.Println(models.Render(r))
		return nil
	}}
}

func (c *CLI) completion() *command.Cmd {
	return &command.Cmd{Name: "completion", Usage: "bash|zsh|fish", Short: "shell completion script", Run: func(cc *command.Ctx) error {
		if err := cc.Want(1, 1, "shell"); err != nil {
			return err
		}
		shell, err := cc.Choice(0, "shell", "", "bash", "zsh", "fish")
		if err != nil {
			return err
		}
		script, err := c.root.Completion(shell)
		if err != nil {
			return err
		}
		cc.Println(script)
		return nil
	}}
}

func (c *CLI) version() *command.Cmd {
	return &command.Cmd{Name: "version", Hidden: true, Run: c.versionRun}
}

func (c *CLI) versionRun(cc *command.Ctx) error {
	if err := cc.Want(0, 0); err != nil {
		return err
	}
	cc.Emit(map[string]string{"truesense": c.root.Version}, "truesense "+c.root.Version)
	return nil
}
