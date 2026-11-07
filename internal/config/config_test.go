package config

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func write(t *testing.T, dir, name, body string) string {
	t.Helper()
	p := filepath.Join(dir, name)
	if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	return p
}

func TestDefaultsAreValid(t *testing.T) {
	if p := Default().Validate(); len(p) != 0 {
		t.Fatalf("defaults must validate: %v", p)
	}
}

func TestUserOverridesSystemOverridesDefaults(t *testing.T) {
	d := t.TempDir()
	sys := write(t, d, "sys.toml", "[cpu]\npl_normal_w = 70\npl_gaming_w = 60\n[fan]\nlevels = [30, 50]\ncpu_up = [50, 70]\ngpu_up = [45, 65]\n")
	usr := write(t, d, "usr.toml", "[cpu]\npl_gaming_w = 55\n")
	c, err := Load(sys, usr)
	if err != nil {
		t.Fatal(err)
	}
	if c.CPU.PLNormalW != 70 || c.CPU.PLGamingW != 55 {
		t.Fatalf("cpu = %+v", c.CPU)
	}
	if !reflect.DeepEqual(c.Fan.Levels, []int{30, 50}) {
		t.Fatalf("a list must replace the default, not extend it: %v", c.Fan.Levels)
	}
	if c.Fan.Hysteresis != 4 {
		t.Fatalf("untouched keys keep the default: %d", c.Fan.Hysteresis)
	}
}

func TestMissingFilesAreFineButASyntaxErrorNamesTheFile(t *testing.T) {
	if _, err := Load("/nonexistent/a.toml", "/nonexistent/b.toml"); err != nil {
		t.Fatal(err)
	}
	bad := write(t, t.TempDir(), "bad.toml", "[cpu\n")
	if _, err := Load("", bad); err == nil || !strings.Contains(err.Error(), "bad.toml") {
		t.Fatalf("err = %v", err)
	}
}

func TestValidateCatchesMistakes(t *testing.T) {
	c := Default()
	c.Fan.Levels = []int{30, 120}
	c.Fan.CPUUp = []int{70, 50}
	c.CPU.PLGamingW = 500
	c.Audio.Jack = "bluetooth"
	c.Smart.ACBusy = "ludicrous"
	c.Startup.Rear = "profile:x"
	c.Update.CheckHours = 0
	c.OSD.Style = "fancy"
	p := strings.Join(c.Validate(), "\n")
	for _, want := range []string{"levels must be 20..100", "ascending", "25..120", "[audio] jack", "[smart] ac_busy", "[startup] rear", "check_hours", "[osd] style", "same length"} {
		if !strings.Contains(p, want) {
			t.Errorf("missing complaint %q in:\n%s", want, p)
		}
	}
}

func TestUnknownKeysFindsTypos(t *testing.T) {
	p := write(t, t.TempDir(), "c.toml", "[cpu]\npl_normal_w = 70\npl_normall_w = 1\n[nonsense]\nx = 1\n[keyboard.operating_colors]\nfree = \"form\"\n")
	got := strings.Join(UnknownKeys(p), ",")
	if !strings.Contains(got, "cpu.pl_normall_w") || !strings.Contains(got, "nonsense") || strings.Contains(got, "free") {
		t.Fatalf("unknown = %q", got)
	}
}

func TestWriteUserMergesAndKeepsOtherKeys(t *testing.T) {
	p := filepath.Join(t.TempDir(), "sub", "config.toml")
	if err := WriteUser(map[string]any{"keys": map[string]any{"tap": "none"}}, p); err != nil {
		t.Fatal(err)
	}
	if err := WriteUser(map[string]any{"keys": map[string]any{"double": "mic-toggle"}, "cpu": map[string]any{"pl_gaming_w": int64(50)}}, p); err != nil {
		t.Fatal(err)
	}
	c, err := Load("", p)
	if err != nil {
		t.Fatal(err)
	}
	if c.Keys.Tap != "none" || c.Keys.Double != "mic-toggle" || c.CPU.PLGamingW != 50 {
		t.Fatalf("got %+v %+v", c.Keys, c.CPU)
	}
}
