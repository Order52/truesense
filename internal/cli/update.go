package cli

import (
	"bufio"
	"flag"
	"fmt"
	"strings"

	"github.com/Order52/truesense/internal/cli/command"
	"github.com/Order52/truesense/internal/update"
)

func (c *CLI) update() *command.Cmd {
	return &command.Cmd{
		Name: "update", Short: "check for a newer TrueSense and install it",
		Run: c.updateCheck,
		Flags: func(fs *flag.FlagSet) {
			fs.Bool("force", false, "ignore the daily cache")
		},
		Sub: []*command.Cmd{
			{Name: "check", Short: "ask GitHub for the newest release", Run: c.updateCheck,
				Flags: func(fs *flag.FlagSet) { fs.Bool("force", false, "ignore the daily cache") }},
			{Name: "apply", Short: "download the newest release and run its installer (asks for administrator rights)", Run: c.updateApply,
				Flags: func(fs *flag.FlagSet) {
					fs.Bool("yes", false, "do not ask first")
					fs.String("version", "", "install this version instead of the newest, e.g. 0.3.1")
				}},
			{Name: "auto", Usage: "[on|off|toggle|status]", Short: "check for updates by itself once a day (a desktop notification, nothing is installed)", Run: func(cc *command.Ctx) error {
				return tri(cc, "check for updates once a day", nil, optional(c.api.AutoUpdateCheck), c.api.SetAutoUpdateCheck)
			}},
			{Name: "auto-run", Hidden: true, Run: c.updateAutoRun},
		},
	}
}

func updateLine(r update.Status) string {
	var line string
	switch {
	case r.Available && r.Latest != nil:
		line = fmt.Sprintf("TrueSense %s: update %s is available   (truesense update apply)", r.Current, *r.Latest)
	default:
		line = fmt.Sprintf("TrueSense %s is up to date", r.Current)
		if r.Latest != nil {
			line += fmt.Sprintf(" (newest %s)", *r.Latest)
		}
	}
	if r.Error != nil {
		line += fmt.Sprintf("   [%s]", *r.Error)
	}
	return line
}

func (c *CLI) updateCheck(cc *command.Ctx) error {
	if err := cc.Want(0, 0); err != nil {
		return err
	}
	r, err := c.api.CheckUpdate(cc.Context, flagBool(cc, "force"))
	if err != nil {
		return err
	}
	cc.Emit(r, updateLine(r))
	return nil
}

func (c *CLI) updateApply(cc *command.Ctx) error {
	if err := cc.Want(0, 0); err != nil {
		return err
	}
	plan, err := c.api.PlanUpdate(cc.Context, flagString(cc, "version"))
	if err != nil {
		return err
	}
	if !plan.Needed {
		cc.Emit(plan.Status, fmt.Sprintf("TrueSense %s is already the newest version", plan.Status.Current))
		return nil
	}
	if !flagBool(cc, "yes") {
		repo, err := c.api.UpdateRepo()
		if err != nil {
			return err
		}
		cc.Printf("Install TrueSense %s (you have %s)?\nThis downloads the release from github.com/%s and runs its installer as root (the service restarts; fans return to the firmware meanwhile).\n",
			strings.TrimPrefix(plan.Tag, "v"), plan.Status.Current, repo)
		cc.Printf("Continue? [y/N] ")
		answer, _ := bufio.NewReader(c.stdin).ReadString('\n')
		if a := strings.ToLower(strings.TrimSpace(answer)); a != "y" && a != "yes" {
			return fmt.Errorf("cancelled")
		}
	}
	res, err := c.api.ApplyUpdate(cc.Context, plan.Tag)
	if res.Version == "" && err != nil {
		return err
	}
	text := fmt.Sprintf("the installer exited with code %d (see /var/log/truesense-install.log)", res.ReturnCode)
	if res.ReturnCode == 0 {
		sum := res.SHA256
		if len(sum) > 16 {
			sum = sum[:16]
		}
		verified := "this release predates published checksums"
		if res.Verified != nil && *res.Verified {
			verified = "checksum matches the release"
		}
		text = fmt.Sprintf("updated to %s (download sha256 %s…, %s); restart the window and the key service if they are open", res.Version, sum, verified)
	}
	cc.Emit(res, text)
	return err
}

func (c *CLI) updateAutoRun(cc *command.Ctx) error {
	if err := cc.Want(0, 0); err != nil {
		return err
	}
	_, err := c.api.NotifyNewUpdate(cc.Context)
	return err
}
