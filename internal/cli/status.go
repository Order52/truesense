package cli

import (
	"github.com/Order52/truesense/internal/cli/command"
)

func (c *CLI) status() *command.Cmd {
	return &command.Cmd{Name: "status", Short: "everything at a glance", Run: func(cc *command.Ctx) error {
		if err := cc.Want(0, 0); err != nil {
			return err
		}
		cc.Emit(c.api.Status(cc.Context), "")
		return nil
	}}
}

func (c *CLI) sensors() *command.Cmd {
	return &command.Cmd{Name: "sensors", Short: "temperatures and fan RPM", Run: func(cc *command.Ctx) error {
		if err := cc.Want(0, 0); err != nil {
			return err
		}
		s, err := c.api.Sensors()
		if err != nil {
			return err
		}
		cc.Emit(s, fmtf("CPU %d C   GPU %d C   board %d C   fans %d / %d rpm", s.CPUTemp, s.GPUTemp, s.SysTemp, s.CPUFanRPM, s.GPUFanRPM))
		return nil
	}}
}
