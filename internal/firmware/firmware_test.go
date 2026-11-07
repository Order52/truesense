package firmware

import (
	"errors"
	"strings"
	"testing"
)

func TestParse(t *testing.T) {
	tests := []struct {
		name, in string
		want     Reply
		err      string
	}{
		{"fields", "ok a=1 b=0x2\n", Reply{"a": "1", "b": "0x2"}, ""},
		{"bare ok", "ok\n", Reply{}, ""},
		{"error text", "err invalid argument\n", nil, "invalid argument"},
		{"bare err", "err\n", nil, "error"},
		{"garbage", "hello", nil, `unexpected reply "hello"`},
		{"empty", "", nil, `unexpected reply ""`},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, err := Parse(tc.in)
			if tc.err != "" {
				var fe *Error
				if !errors.As(err, &fe) || fe.Msg != tc.err {
					t.Fatalf("err = %v, want %q", err, tc.err)
				}
				return
			}
			if err != nil || len(got) != len(tc.want) {
				t.Fatalf("got %v, %v", got, err)
			}
			for k, v := range tc.want {
				if got[k] != v {
					t.Fatalf("%s = %q, want %q", k, got[k], v)
				}
			}
		})
	}
}

func TestInt(t *testing.T) {
	r := Reply{"d": "42", "h": "0xC3", "bad": "x"}
	if n, err := r.Int("d"); err != nil || n != 42 {
		t.Fatalf("d = %d, %v", n, err)
	}
	if n, err := r.Int("h"); err != nil || n != 0xC3 {
		t.Fatalf("h = %d, %v", n, err)
	}
	for _, k := range []string{"bad", "missing"} {
		if _, err := r.Int(k); err == nil {
			t.Fatalf("%s: want an error", k)
		}
	}
}

func FuzzParse(f *testing.F) {
	for _, s := range []string{"ok a=1", "err x", "", "ok =", "ok a=b=c", "\x00\xff", "ok " + strings.Repeat("k=v ", 100)} {
		f.Add(s)
	}
	f.Fuzz(func(t *testing.T, s string) {
		r, err := Parse(s)
		if (r == nil) == (err == nil) {
			t.Fatalf("exactly one of reply and error must be set: %v %v", r, err)
		}
		for k := range r {
			_, _ = r.Int(k) // must never panic
		}
	})
}

func run(t *testing.T, m *Simulator, cmd string) (Reply, error) {
	t.Helper()
	return Do(m, cmd)
}

func TestSimulatorMirrorsKernelValidation(t *testing.T) {
	m := NewSimulator()
	bad := []string{
		"set_mode 2", "set_mode x", "set_mode", "set_gpu 4", "set_boot 2", "set_overdrive 2", "get_misc 0", "get_misc 17", "set_fan_manual 10 30", "set_fan_manual 30",
		"set_fan_manual 101 30", "set_usb 5", "set_kbtimeout 2", "get_battery 3", "set_battery 1 2", "set_battery 3 1", "set_color 12345", "set_color zzzzzz", "set_color ffffff 3",
		"set_rear 00", "get_mode 1", "nonsense",
	}
	for _, c := range bad {
		if _, err := run(t, m, c); err == nil {
			t.Errorf("%q must be rejected", c)
		}
	}
	good := []string{"version", "caps", "sensors", "get_mode", "set_mode 5", "set_fan_manual 0 100", "set_fan_manual 20 100", "set_usb 20", "set_color ffffff 4", "get_battery 2", "set_battery 1 1"}
	for _, c := range good {
		if _, err := run(t, m, c); err != nil {
			t.Errorf("%q: %v", c, err)
		}
	}
}

func TestSimulatorKeepsState(t *testing.T) {
	m := NewSimulator()
	if _, err := run(t, m, "set_mode 5"); err != nil {
		t.Fatal(err)
	}
	r, _ := run(t, m, "get_mode")
	if n, _ := r.Int("mode"); n != 5 {
		t.Fatalf("mode = %d", n)
	}
	_, _ = run(t, m, "set_fan_manual 40 55")
	r, _ = run(t, m, "get_fan")
	if b, _ := r.Int("behavior"); b != 0xC3 {
		t.Fatalf("behavior = %#x", b)
	}
	if c, _ := r.Int("cpu"); c != 40 {
		t.Fatalf("cpu duty = %d", c)
	}
	m.Reset()
	if s := m.Snapshot(); s.Mode != 6 || s.Fan != "auto" {
		t.Fatalf("reset: %+v", s)
	}
}

func TestWMIReportsMissingDevice(t *testing.T) {
	_, err := (&WMI{Path: "/nonexistent/truesense"}).Run("version")
	if !errors.Is(err, ErrNotLoaded) {
		t.Fatalf("err = %v", err)
	}
}

func TestFromEnv(t *testing.T) {
	t.Setenv("TRUESENSE_SIM", "1")
	if _, ok := FromEnv().(*Simulator); !ok {
		t.Fatal("TRUESENSE_SIM must select the simulator")
	}
	t.Setenv("TRUESENSE_SIM", "")
	t.Setenv("TRUESENSE_DEVICE", "/x/y")
	if w, ok := FromEnv().(*WMI); !ok || w.Path != "/x/y" {
		t.Fatal("TRUESENSE_DEVICE must select the kernel device at that path")
	}
}
