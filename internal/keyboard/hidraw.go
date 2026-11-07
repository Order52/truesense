package keyboard

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"unsafe"

	"golang.org/x/sys/unix"

	"github.com/Order52/truesense/internal/firmware"
)

// The keyboard is the Chicony controller 04F2:0117; its lighting interface is the hidraw node
// whose report descriptor starts with "usage page 0xFF02" (06 02 FF). The keyboard's other
// interfaces share the vendor and product ids.
const (
	hidID           = "000004F2:00000117"
	lightingUsageHi = "\x06\x02\xff"
)

// isLightingInterface decides from a hidraw node's uevent text and report descriptor whether
// it is the keyboard's lighting interface. Both come from the kernel but are treated as
// untrusted input: nothing here may panic on short or odd data.
func isLightingInterface(uevent string, descriptor []byte) bool {
	return strings.Contains(strings.ToUpper(uevent), hidID) && bytes.HasPrefix(descriptor, []byte(lightingUsageHi))
}

// FindNode returns the name (hidrawN) of the lighting interface under sysRoot ("" = the real
// /sys). Nodes are examined in string order, as the Python tool did.
func FindNode(sysRoot string) (string, error) {
	nodes, _ := filepath.Glob(filepath.Join(sysRoot, "/sys/class/hidraw/hidraw*"))
	sort.Strings(nodes)
	for _, h := range nodes {
		uevent, err := os.ReadFile(filepath.Join(h, "device/uevent"))
		if err != nil {
			continue
		}
		desc, err := os.ReadFile(filepath.Join(h, "device/report_descriptor"))
		if err != nil {
			continue
		}
		if isLightingInterface(string(uevent), desc) {
			return filepath.Base(h), nil
		}
	}
	return "", firmware.Errorf("keyboard lighting interface (usage page 0xFF02) not found")
}

// HidrawOpener returns an Opener for the real keyboard. sysRoot and devRoot are prefixes of
// /sys and /dev ("" = the real ones) so tests can point them at a temporary tree.
func HidrawOpener(sysRoot, devRoot string) Opener {
	return func() (Transport, error) {
		name, err := FindNode(sysRoot)
		if err != nil {
			return nil, err
		}
		node := filepath.Join(devRoot, "/dev", name)
		fd, err := unix.Open(node, unix.O_RDWR|unix.O_CLOEXEC, 0)
		if err != nil {
			if errors.Is(err, unix.EACCES) || errors.Is(err, unix.EPERM) {
				return nil, firmware.Errorf("no permission on %s: add yourself to group plugdev and install the udev rule", node)
			}
			return nil, fmt.Errorf("open %s: %w", node, err)
		}
		return &hidraw{fd: fd}, nil
	}
}

// hidraw is the real Transport: a file descriptor on /dev/hidrawN.
type hidraw struct{ fd int }

// ioctl request numbers: _IOC(read|write, 'H', nr, len). HIDIOCSFEATURE is nr 6, HIDIOCGFEATURE nr 7;
// the length is that of the buffer including the leading report id.
func hidioc(nr, length uintptr) uintptr { return 0xC0000000 | length<<16 | uintptr('H')<<8 | nr }

func (h *hidraw) ioctl(nr uintptr, buf []byte) error {
	for {
		//nolint:gosec // G103: the kernel reads/writes exactly len(buf) bytes of this slice (the length is encoded in the request)
		_, _, errno := unix.Syscall(unix.SYS_IOCTL, uintptr(h.fd), hidioc(nr, uintptr(len(buf))), uintptr(unsafe.Pointer(&buf[0])))
		if errno == unix.EINTR {
			continue
		}
		if errno != 0 {
			return errno
		}
		return nil
	}
}

// SetFeature sends report id 0 plus the 8 data bytes.
func (h *hidraw) SetFeature(payload []byte) error {
	return h.ioctl(6, append([]byte{0}, payload...))
}

// GetFeature reads report id 0 plus n data bytes and returns the data.
func (h *hidraw) GetFeature(n int) ([]byte, error) {
	buf := make([]byte, 1+n)
	if err := h.ioctl(7, buf); err != nil {
		return nil, err
	}
	return buf[1:], nil
}

// WriteOutput writes the 64 data bytes behind a leading 0: that is the report id, which
// hidraw strips, so the device gets exactly 64 data bytes.
func (h *hidraw) WriteOutput(data []byte) error {
	buf := append([]byte{0}, data...)
	for {
		n, err := unix.Write(h.fd, buf)
		if errors.Is(err, unix.EINTR) {
			continue
		}
		if err != nil {
			return err
		}
		if n != len(buf) {
			return fmt.Errorf("short write: %d of %d bytes", n, len(buf))
		}
		return nil
	}
}

func (h *hidraw) Close() error { return unix.Close(h.fd) }
