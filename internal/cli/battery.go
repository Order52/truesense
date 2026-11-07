package cli

import (
	"fmt"

	"github.com/Order52/truesense/internal/cli/command"
)

func (c *CLI) battery() *command.Cmd {
	return &command.Cmd{
		Name: "battery", Short: "80 % charge limit, charge to full once, calibration, USB power-off charging",
		Run: c.batteryStatus,
		Sub: []*command.Cmd{
			{Name: "full", Usage: "[start|cancel|status]", Run: c.batteryFull,
				Short: "charge to 100 % once: the 80 % limit comes back by itself (unplug, full for 5 minutes, or 24 hours)"},
			{Name: "limit", Usage: "[on|off|toggle|status]", Run: func(cc *command.Ctx) error {
				return tri(cc, "80 % battery limit", nil, optional(c.api.BatteryLimit), c.api.SetBatteryLimit)
			}},
			{Name: "calibration", Usage: "[on|off|toggle|status]", Run: func(cc *command.Ctx) error {
				return tri(cc, "battery calibration (needs AC, takes hours)", nil, optional(c.api.Calibration), c.api.SetCalibration)
			}},
			{Name: "usb-charge", Usage: "[status|0|10|20|30]", Run: c.batteryUSB,
				Short: "USB charging while powered off: stop level 0 (off) 10 20 30"},
			{Name: "status", Run: c.batteryStatus},
		},
	}
}

func (c *CLI) batteryStatus(cc *command.Ctx) error {
	if err := cc.Want(0, 0); err != nil {
		return err
	}
	st, err := c.api.BatteryStatus()
	if err != nil {
		return err
	}
	cc.Emit(st, "")
	return nil
}

func (c *CLI) batteryFull(cc *command.Ctx) error {
	if err := cc.Want(0, 1, "action"); err != nil {
		return err
	}
	action, err := cc.Choice(0, "action", "status", "start", "cancel", "status")
	if err != nil {
		return err
	}
	switch action {
	case "start":
		r, err := c.api.StartChargeFull()
		if err != nil {
			return err
		}
		cc.Emit(r, "charging to 100 % once; the 80 % limit comes back when you unplug, when it has been full for 5 minutes, or after 24 hours")
	case "cancel":
		r, err := c.api.CancelChargeFull()
		if err != nil {
			return err
		}
		cc.Emit(r, "80 % limit back on")
	default:
		r, err := c.api.ChargeFullStatus()
		if err != nil {
			return err
		}
		limit := r.Limit != nil && *r.Limit
		capacity := "None"
		if r.Capacity != nil {
			capacity = pyValue(*r.Capacity)
		}
		cc.Emit(r, fmt.Sprintf("80 %% limit %s, battery %s %%, charging to full once: %s", pick(limit, "on", "off"), capacity, pick(r.Active, "yes", "no")))
	}
	return nil
}

func (c *CLI) batteryUSB(cc *command.Ctx) error {
	if err := cc.Want(0, 1, "level"); err != nil {
		return err
	}
	level, err := cc.Choice(0, "level", "status", "status", "0", "10", "20", "30")
	if err != nil {
		return err
	}
	var v int
	if level == "status" {
		v, err = c.api.USBCharge()
	} else {
		n, _ := intArg("level", level)
		v, err = c.api.SetUSBCharge(n)
	}
	if err != nil {
		return err
	}
	cc.Emit(v, fmt.Sprintf("USB charging while off, stop level: %d", v))
	return nil
}
