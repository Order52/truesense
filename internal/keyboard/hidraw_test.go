package keyboard

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"golang.org/x/sys/unix"

	"github.com/Order52/truesense/internal/firmware"
)

func TestIoctlRequestNumbers(t *testing.T) {
	// _IOC(_IOC_READ|_IOC_WRITE, 'H', nr, len) as the Python tool computed them.
	if got := hidioc(6, 9); got != 0xC0094806 {
		t.Errorf("HIDIOCSFEATURE(9) = %#x", got)
	}
	if got := hidioc(7, 9); got != 0xC0094807 {
		t.Errorf("HIDIOCGFEATURE(9) = %#x", got)
	}
}

func TestIsLightingInterface(t *testing.T) {
	const uevent = "DRIVER=hid-generic\nHID_ID=0003:000004F2:00000117\nHID_NAME=Chicony\n"
	for _, tc := range []struct {
		name   string
		uevent string
		desc   []byte
		want   bool
	}{
		{"lighting interface", uevent, []byte{0x06, 0x02, 0xff, 0x09, 0x01}, true},
		{"lower case ids", strings.ToLower(uevent), []byte{0x06, 0x02, 0xff}, true},
		{"other interface of the keyboard", uevent, []byte{0x05, 0x01, 0x09, 0x06}, false},
		{"other device", "HID_ID=0003:0000046D:0000C31C\n", []byte{0x06, 0x02, 0xff}, false},
		{"empty descriptor", uevent, nil, false},
		{"short descriptor", uevent, []byte{0x06, 0x02}, false},
		{"no uevent", "", []byte{0x06, 0x02, 0xff}, false},
	} {
		if got := isLightingInterface(tc.uevent, tc.desc); got != tc.want {
			t.Errorf("%s: %v", tc.name, got)
		}
	}
}

func FuzzIsLightingInterface(f *testing.F) {
	f.Add("HID_ID=0003:000004F2:00000117", []byte{0x06, 0x02, 0xff})
	f.Add("", []byte{})
	f.Add("\x00", []byte{0x06})
	f.Fuzz(func(t *testing.T, uevent string, desc []byte) {
		got := isLightingInterface(uevent, desc)
		if got && (len(desc) < 3 || !strings.Contains(strings.ToUpper(uevent), "000004F2:00000117")) {
			t.Fatal("matched without the ids or the usage page")
		}
	})
}

// fakeSys builds a /sys/class/hidraw tree: node name -> (uevent, descriptor).
func fakeSys(t *testing.T, nodes map[string][2]string) string {
	t.Helper()
	root := t.TempDir()
	for name, v := range nodes {
		dir := filepath.Join(root, "sys/class/hidraw", name, "device")
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatal(err)
		}
		if v[0] != "-" {
			must(t, os.WriteFile(filepath.Join(dir, "uevent"), []byte(v[0]), 0o644))
		}
		if v[1] != "-" {
			must(t, os.WriteFile(filepath.Join(dir, "report_descriptor"), []byte(v[1]), 0o644))
		}
	}
	return root
}

func must(t *testing.T, err error) {
	t.Helper()
	if err != nil {
		t.Fatal(err)
	}
}

func TestFindNode(t *testing.T) {
	const kb = "HID_ID=0003:000004F2:00000117\n"
	const lighting = "\x06\x02\xff\x09\x01"
	t.Run("picks the lighting interface among the keyboard's interfaces", func(t *testing.T) {
		root := fakeSys(t, map[string][2]string{
			"hidraw0": {"HID_ID=0003:0000046D:0000C31C\n", lighting}, // other device with the same usage page
			"hidraw1": {kb, "\x05\x01\x09\x06"},                      // keyboard interface 0
			"hidraw2": {kb, lighting},                                // the one
			"hidraw3": {kb, lighting},
		})
		got, err := FindNode(root)
		if err != nil || got != "hidraw2" {
			t.Errorf("FindNode = %q, %v", got, err)
		}
	})
	t.Run("string order like the Python tool", func(t *testing.T) {
		root := fakeSys(t, map[string][2]string{"hidraw2": {kb, lighting}, "hidraw10": {kb, lighting}})
		if got, _ := FindNode(root); got != "hidraw10" {
			t.Errorf("FindNode = %q", got)
		}
	})
	t.Run("unreadable nodes are skipped", func(t *testing.T) {
		root := fakeSys(t, map[string][2]string{"hidraw0": {"-", lighting}, "hidraw1": {kb, "-"}, "hidraw2": {kb, lighting}})
		if got, err := FindNode(root); err != nil || got != "hidraw2" {
			t.Errorf("FindNode = %q, %v", got, err)
		}
	})
	t.Run("none found", func(t *testing.T) {
		_, err := FindNode(t.TempDir())
		var fe *firmware.Error
		if !errors.As(err, &fe) || !strings.Contains(err.Error(), "usage page 0xFF02") {
			t.Errorf("%v", err)
		}
	})
}

func TestHidrawOpenerPermissionAndMissingNode(t *testing.T) {
	root := fakeSys(t, map[string][2]string{"hidraw0": {"HID_ID=0003:000004F2:00000117\n", "\x06\x02\xff"}})
	devRoot := t.TempDir()
	if err := os.MkdirAll(filepath.Join(devRoot, "dev"), 0o755); err != nil {
		t.Fatal(err)
	}
	// node missing: a plain error naming the node
	if _, err := HidrawOpener(root, devRoot)(); err == nil || !strings.Contains(err.Error(), "hidraw0") {
		t.Errorf("missing node: %v", err)
	}
	// node present and readable: it opens, and hidraw output reports reach the file with the report id in front
	node := filepath.Join(devRoot, "dev", "hidraw0")
	must(t, os.WriteFile(node, nil, 0o600))
	tr, err := HidrawOpener(root, devRoot)()
	if err != nil {
		t.Fatal(err)
	}
	data := bytes.Repeat([]byte{0xAB}, OutputLen)
	must(t, tr.WriteOutput(data))
	must(t, tr.Close())
	got, _ := os.ReadFile(node)
	if len(got) != OutputLen+1 || got[0] != 0 || !bytes.Equal(got[1:], data) {
		t.Errorf("file holds %d bytes, first %#x: want report id 0 then the 64 data bytes", len(got), got[:1])
	}
	// no permission
	if os.Getuid() != 0 {
		must(t, os.Chmod(node, 0))
		_, err := HidrawOpener(root, devRoot)()
		if err == nil || !strings.Contains(err.Error(), "no permission") || !strings.Contains(err.Error(), "plugdev") {
			t.Errorf("no permission: %v", err)
		}
	}
}

func TestHidrawFeatureReportsFailCleanlyOnANonHIDFile(t *testing.T) {
	f, err := os.CreateTemp(t.TempDir(), "x")
	must(t, err)
	defer f.Close()
	h := &hidraw{fd: int(f.Fd())}
	if err := h.SetFeature(make([]byte, 8)); !errors.Is(err, unix.ENOTTY) {
		t.Errorf("SetFeature on a regular file: %v", err)
	}
	if _, err := h.GetFeature(8); !errors.Is(err, unix.ENOTTY) {
		t.Errorf("GetFeature on a regular file: %v", err)
	}
}
