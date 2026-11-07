// Package fnkeys is the key service (`truesense keys run`, a user service): it reads the
// keyboard's vendor HID interface, recognises the PREDATOR key and Fn+Esc/Fn+F10, resolves the
// PREDATOR key's press patterns into tap, double, triple and long, and runs the action bound
// to each (internal/actions).
//
// The keyboard's vendor interface (usage page 0x88, report id 4) sends:
//
//	04 80 FF  Fn+F10        04 8A FF  Fn+Esc
//	04 81 FF  PREDATOR key pressed        04 00 00  released
//
// Fn+F11/F12 brightness is done by the keyboard firmware and is deliberately ignored.
package fnkeys

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"strings"
)

// vendorID is the keyboard controller (Chicony 04F2:0117) as hid uevents print it.
const vendorID = "000004F2:00000117"

// ErrNoNode means the keyboard's Fn-key HID interface was not found.
var ErrNoNode = errors.New("the Fn-key HID interface (usage page 0x88) was not found")

// FindNode returns the hidraw device of the Fn-key interface: the node of the keyboard
// controller whose report descriptor declares usage page 0x88. sysRoot and devRoot are the
// prefixes of /sys and /dev ("" = the real ones); nodes are examined in name order, as 0.3.x
// did (hidraw10 before hidraw2).
func FindNode(sysRoot, devRoot string) (string, error) {
	if devRoot == "" {
		devRoot = "/dev"
	}
	dir := filepath.Join(sysRoot, "/sys/class/hidraw")
	entries, err := os.ReadDir(dir)
	if err != nil {
		return "", ErrNoNode
	}
	for _, e := range entries {
		if !strings.HasPrefix(e.Name(), "hidraw") {
			continue
		}
		base := filepath.Join(dir, e.Name(), "device")
		uevent, err := os.ReadFile(filepath.Join(base, "uevent"))
		if err != nil || !strings.Contains(strings.ToUpper(string(uevent)), vendorID) {
			continue
		}
		desc, err := os.ReadFile(filepath.Join(base, "report_descriptor"))
		if err != nil {
			continue
		}
		if bytes.Contains(desc, []byte{0x06, 0x88, 0x00}) || bytes.Contains(desc, []byte{0x05, 0x88}) {
			return filepath.Join(devRoot, e.Name()), nil
		}
	}
	return "", ErrNoNode
}
