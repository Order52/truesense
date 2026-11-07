// Package fast is the instant client for status-bar buttons: `truesense-fast fan smart|auto|max|<20-100>`,
// `power <mode> [hold-minutes]` and `auto on|off|native|smart`. It makes one request to the
// daemon (a few milliseconds, no settle delay) and, when there is no daemon, falls back to what
// the hardware allows without one. It is the same binary as `truesense`, started under another name.
package fast

import (
	"fmt"
	"io"
	"strconv"

	"github.com/Order52/truesense/internal/client"
	"github.com/Order52/truesense/internal/ipc"
)

const usage = "usage: truesense-fast fan smart|auto|max|<20-100>   |   power <mode> [hold-minutes]   |   auto on|off|native|smart"

// Run executes one command and returns the process exit code.
func Run(args []string, c *client.Client, stdout, stderr io.Writer) int {
	fail := func(err error) int { _, _ = fmt.Fprintln(stderr, "truesense-fast:", err); return 1 }
	bad := func(msg string) int { _, _ = fmt.Fprintln(stderr, msg); return 2 }
	if len(args) < 2 {
		return bad(usage)
	}
	switch args[0] {
	case "fan":
		req := client.FanRequest{Mode: args[1]}
		switch args[1] {
		case "smart", "auto", "max":
		default:
			n, err := strconv.Atoi(args[1])
			if err != nil {
				return bad("usage: truesense-fast fan smart|auto|max|<20-100>")
			}
			req = client.FanRequest{Mode: "manual", CPU: &n}
		}
		if err := c.Fan(req); err != nil {
			return fail(err)
		}
	case "power":
		hold := 30.0
		if len(args) > 2 {
			h, err := strconv.ParseFloat(args[2], 64)
			if err != nil || h < 0 {
				return bad("usage: truesense-fast power <mode> [hold-minutes]")
			}
			hold = h
		}
		if _, err := c.Power(args[1], hold); err != nil {
			return fail(err)
		}
	case "auto":
		v := args[1]
		switch v {
		case "on":
			v = "smart"
		case "off", "native", "smart":
		default:
			return bad("usage: truesense-fast auto off|native|smart")
		}
		if !c.DaemonAvailable() {
			return fail(fmt.Errorf("the automatic power mode needs the TrueSense service"))
		}
		if err := c.IPC.CallInto("set", ipc.Args{"key": "auto_mode", "value": v}, nil); err != nil {
			return fail(err)
		}
	default:
		return bad(usage)
	}
	return 0
}
