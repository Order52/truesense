// Package touchpad switches the touchpad on and off through the kernel's per-device
// "inhibited" file, which works on any compositor. Writing needs root: the daemon does it for users.
package touchpad

import (
	"errors"
	"fmt"
	"os"
	"regexp"
	"strings"
)

// ErrNotFound means no touchpad was found.
var ErrNotFound = errors.New("touchpad not found")

// Switch is the touchpad of this machine.
type Switch struct {
	DevicesFile string // /proc/bus/input/devices
	SysInput    string // /sys/class/input
}

// New returns the Switch for the real machine.
func New() *Switch {
	return &Switch{DevicesFile: "/proc/bus/input/devices", SysInput: "/sys/class/input"}
}

var event = regexp.MustCompile(`event\d+`)

// inhibitFile finds the touchpad's inhibit file: the SYNA touchpad block of the input device list.
func (s *Switch) inhibitFile() (string, error) {
	b, err := os.ReadFile(s.DevicesFile)
	if err != nil {
		return "", ErrNotFound
	}
	for _, block := range strings.Split(string(b), "\n\n") {
		if !strings.Contains(block, "Touchpad") || !strings.Contains(strings.ToUpper(block), "SYNA") {
			continue
		}
		var handlers []string
		for _, l := range strings.Split(block, "\n") {
			if strings.HasPrefix(l, "H: Handlers=") {
				handlers = append(handlers, l)
			}
		}
		if m := event.FindString(strings.Join(handlers, "")); m != "" {
			return fmt.Sprintf("%s/%s/device/inhibited", s.SysInput, m), nil
		}
	}
	return "", ErrNotFound
}

// Enabled reports whether the touchpad is on; ok is false when it cannot be told.
func (s *Switch) Enabled() (on, ok bool) {
	f, err := s.inhibitFile()
	if err != nil {
		return false, false
	}
	b, err := os.ReadFile(f)
	if err != nil {
		return false, false
	}
	return strings.TrimSpace(string(b)) == "0", true
}

// Set switches the touchpad and verifies it.
func (s *Switch) Set(on bool) (bool, error) {
	f, err := s.inhibitFile()
	if err != nil {
		return false, err
	}
	v := "1"
	if on {
		v = "0"
	}
	if err := os.WriteFile(f, []byte(v), 0o644); err != nil {
		return false, fmt.Errorf("cannot switch the touchpad (%w); this needs root: go through the daemon", err)
	}
	if got, ok := s.Enabled(); !ok || got != on {
		return false, errors.New("touchpad readback mismatch")
	}
	return on, nil
}
