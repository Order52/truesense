package touchpad

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
)

const devices = `I: Bus=0011 Vendor=0001 Product=0001 Version=ab41
N: Name="AT Translated Set 2 keyboard"
H: Handlers=sysrq kbd event3 leds

I: Bus=0018 Vendor=06cb Product=cd7e Version=0100
N: Name="SYNA2BA6:00 06CB:CD7E Touchpad"
H: Handlers=mouse1 event7
B: PROP=5
`

func fake(t *testing.T, list string) *Switch {
	t.Helper()
	root := t.TempDir()
	dev := filepath.Join(root, "devices")
	if err := os.WriteFile(dev, []byte(list), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(root, "input/event7/device"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "input/event7/device/inhibited"), []byte("0\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	return &Switch{DevicesFile: dev, SysInput: filepath.Join(root, "input")}
}

func TestSwitchOffAndOn(t *testing.T) {
	s := fake(t, devices)
	if on, ok := s.Enabled(); !ok || !on {
		t.Fatalf("starts on: %v %v", on, ok)
	}
	if got, err := s.Set(false); err != nil || got {
		t.Fatalf("off: %v %v", got, err)
	}
	if on, _ := s.Enabled(); on {
		t.Fatal("must read back as off")
	}
	if got, err := s.Set(true); err != nil || !got {
		t.Fatalf("on: %v %v", got, err)
	}
}

func TestNoTouchpadIsAnErrorNotACrash(t *testing.T) {
	s := fake(t, "N: Name=\"keyboard\"\nH: Handlers=kbd event3\n")
	if _, ok := s.Enabled(); ok {
		t.Fatal("no touchpad: cannot tell")
	}
	if _, err := s.Set(false); !errors.Is(err, ErrNotFound) {
		t.Fatalf("err = %v", err)
	}
	if _, err := (&Switch{DevicesFile: "/nonexistent"}).Set(true); !errors.Is(err, ErrNotFound) {
		t.Fatalf("err = %v", err)
	}
}
