// Package firmware is the only door to the laptop's firmware: the text protocol of the
// truesense_wmi kernel module (/dev/truesense), and an in-memory simulator of it
// (TRUESENSE_SIM=1) that mirrors the kernel's argument validation.
//
// One command per call; replies are "ok key=value ..." or "err message". Nothing above this
// package knows whether it is talking to hardware.
package firmware

import (
	"errors"
	"fmt"
	"os"
	"strconv"
	"strings"
)

// DevicePath is where the kernel module's character device lives (TRUESENSE_DEVICE overrides it).
const DevicePath = "/dev/truesense"

// Device runs one command of the module's protocol and returns the raw reply line.
// Implementations are safe for concurrent use.
type Device interface {
	Run(command string) (string, error)
}

// Error is a failure reported by (or on the way to) the firmware.
type Error struct{ Msg string }

func (e *Error) Error() string { return e.Msg }

// Errorf builds an *Error.
func Errorf(format string, args ...any) *Error { return &Error{Msg: fmt.Sprintf(format, args...)} }

// ErrNotLoaded and ErrPermission classify the two common ways opening the device fails,
// so callers can show the right fix.
var (
	ErrNotLoaded  = errors.New("the truesense_wmi kernel module is not loaded")
	ErrPermission = errors.New("no permission on the TrueSense device")
)

// Reply is a parsed "ok key=value ..." line.
type Reply map[string]string

// Parse turns a reply line into a Reply, or the module's error message into an *Error.
func Parse(line string) (Reply, error) {
	line = strings.TrimSpace(line)
	switch {
	case strings.HasPrefix(line, "err"):
		msg := strings.TrimSpace(line[3:])
		if msg == "" {
			msg = "error"
		}
		return nil, &Error{Msg: msg}
	case !strings.HasPrefix(line, "ok"):
		return nil, Errorf("unexpected reply %q", line)
	}
	r := Reply{}
	for _, kv := range strings.Fields(line[2:]) {
		if k, v, ok := strings.Cut(kv, "="); ok {
			r[k] = v
		}
	}
	return r, nil
}

// Int returns a numeric field (decimal, or 0x-prefixed hexadecimal).
func (r Reply) Int(key string) (int, error) {
	v, ok := r[key]
	if !ok {
		return 0, Errorf("missing %q in reply %v", key, map[string]string(r))
	}
	base := 10
	if strings.HasPrefix(v, "0x") {
		base, v = 16, v[2:]
	}
	n, err := strconv.ParseInt(v, base, 64)
	if err != nil {
		return 0, Errorf("invalid %q in reply %v", key, map[string]string(r))
	}
	return int(n), nil
}

// Do runs a command on dev and parses the reply.
func Do(dev Device, command string) (Reply, error) {
	line, err := dev.Run(command)
	if err != nil {
		return nil, err
	}
	return Parse(line)
}

// FromEnv returns the simulator when TRUESENSE_SIM is set, otherwise the kernel device
// (at TRUESENSE_DEVICE if set).
func FromEnv() Device {
	if os.Getenv("TRUESENSE_SIM") != "" {
		return Sim()
	}
	path := os.Getenv("TRUESENSE_DEVICE")
	if path == "" {
		path = DevicePath
	}
	return &WMI{Path: path}
}
