package api_test

import (
	"context"
	"io"
	"log/slog"
	"os"
	"testing"
	"time"

	"github.com/Order52/truesense/internal/api"
)

func TestKeyBindingsStartAtTheDefaults(t *testing.T) {
	_, a := machine(t)
	k, err := a.KeyBindings()
	must(t, err)
	if k.Tap == "" || k.LongMS != 800 || k.Action("fn_esc") != k.FnEsc || k.PowerCycle == nil {
		t.Fatalf("%+v", k)
	}
	if len(api.KeyGestures()) != 5 {
		t.Fatal(api.KeyGestures())
	}
	if c := a.KeyActions(); len(c) < 100 {
		t.Fatal("the catalogue")
	}
}

func TestSetKeyActionChecksTheActionNow(t *testing.T) {
	_, a := machine(t)
	err := a.SetKeyAction("tap", "bogus-action")
	wantErr(t, err, "(truesense keys actions)")
	must(t, a.SetKeyAction("double", "fan:max"))
	k, _ := a.KeyBindings()
	if k.Double != "fan:max" || k.Profile != "custom" {
		t.Fatalf("%+v", k)
	}
	wantErr(t, a.SetKeyAction("tap", "look:nothing"), "no saved look")
	done, err := a.ResetKeys("double")
	must(t, err)
	if len(done) != 1 || done[0] != "double" {
		t.Fatal(done)
	}
	k, _ = a.KeyBindings()
	if k.Double != "output-toggle" {
		t.Fatal("reset")
	}
	if done, _ := a.ResetKeys(""); len(done) != 5 {
		t.Fatal(done)
	}
}

func TestKeyTimingIsRangeChecked(t *testing.T) {
	_, a := machine(t)
	_, err := a.SetKeyTiming(100, 0)
	wantErr(t, err, "long_ms is 300..2000")
	_, err = a.SetKeyTiming(0, 5000)
	wantErr(t, err, "multi_ms is 200..1000")
	_, err = a.SetKeyTiming(600, 0)
	must(t, err)
	tm, _ := a.KeyTiming()
	if tm.LongMS != 600 || tm.MultiMS != 450 {
		t.Fatalf("%+v", tm)
	}
}

func TestKeyProfiles(t *testing.T) {
	_, a := machine(t)
	l, err := a.KeyProfiles()
	must(t, err)
	if l.Current != "default" || l.Names[0] != "native" {
		t.Fatalf("%+v", l)
	}
	p, err := a.SaveKeyProfile("mine", api.KeyProfileEdit{Actions: map[string]string{"tap": "power:turbo"}, LongMS: ptr(700)})
	must(t, err)
	if p.Tap != "power:turbo" || p.LongMS != 700 || p.Double == "" {
		t.Fatalf("%+v", p)
	}
	_, err = a.SaveKeyProfile("bad", api.KeyProfileEdit{Actions: map[string]string{"tap": "nonsense"}})
	if err == nil {
		t.Fatal("a bad action is refused when saved")
	}
	must(t, a.UseKeyProfile("mine"))
	cur, _ := a.KeyProfileInUse()
	if cur.Profile != "mine" || cur.Tap != "power:turbo" {
		t.Fatalf("%+v", cur)
	}
	wantErr(t, a.UseKeyProfile(""), "which profile?")
	if n, err := a.CopyKeyProfile("mine", ""); err != nil || n != "mine-copy" {
		t.Fatal(n, err)
	}
	wantErr(t, a.DeleteKeyProfile("default"), "")
	must(t, a.DeleteKeyProfile("mine-copy"))
	// saving again edits your own profile, keeping what is not mentioned
	p, err = a.SaveKeyProfile("mine", api.KeyProfileEdit{Actions: map[string]string{"double": "fan:max"}})
	must(t, err)
	if p.Tap != "power:turbo" || p.Double != "fan:max" {
		t.Fatalf("%+v", p)
	}
}

func TestTestActionRunsTheAction(t *testing.T) {
	_, a := machine(t)
	if msg, err := a.TestAction(t.Context(), "notify:hello"); err != nil || msg != "hello" {
		t.Fatalf("%q %v", msg, err)
	}
	if _, err := a.TestAction(t.Context(), "bogus"); err == nil {
		t.Fatal("unknown action")
	}
	if msg, err := a.TestAction(t.Context(), "power:turbo"); err != nil || msg != "Power mode: turbo" {
		t.Fatalf("%q %v", msg, err)
	}
	if msg, err := a.NextPower(t.Context()); err != nil || msg == "" {
		t.Fatalf("%q %v", msg, err)
	}
}

// With no keyboard Fn-key interface the key service says so and returns (the supervisor restarts it).
func TestKeyServiceWithoutAKeyboardNode(t *testing.T) {
	_, a := machine(t)
	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()
	err := a.RunKeyService(ctx, slog.New(slog.NewTextHandler(io.Discard, nil)))
	if err == nil {
		t.Fatal("the node is missing on the test machine")
	}
}

func TestProfiles(t *testing.T) {
	f, a := machine(t)
	must(t, os.MkdirAll(f.Dir+"/shipped", 0o755))
	must(t, os.WriteFile(f.Dir+"/shipped/gaming.toml", []byte("description = \"Fast\"\n[power]\nmode = \"turbo\"\n"), 0o644))
	a.UseConfigFiles(a.SystemConfig, a.UserConfig) // the shipped directory is read when the API is wired
	list := a.Profiles()
	if len(list) != 1 || list[0].Name != "gaming" || list[0].Source != "shipped" || list[0].Description != "Fast" {
		t.Fatalf("%+v", list)
	}
	if d := a.DescribeProfile("gaming"); d == "" {
		t.Fatal("description")
	}
	if a.DescribeProfile("nope") != "" {
		t.Fatal("unknown profile")
	}
	msg, err := a.ApplyProfile(t.Context(), "gaming")
	must(t, err)
	if msg == "" || f.Sim.Snapshot().Mode != 5 {
		t.Fatalf("%q mode %d", msg, f.Sim.Snapshot().Mode)
	}
	p, _ := a.Profile("gaming")
	text, err := a.ProfileTOML(p)
	must(t, err)
	if text != "description = \"Fast\"\n\n[power]\nmode = \"turbo\"\n" {
		t.Fatalf("%q", text)
	}
	path, err := a.SaveProfile(t.Context(), "mine", "my setup")
	must(t, err)
	if _, err := os.Stat(path); err != nil {
		t.Fatal(err)
	}
	if len(a.ProfileNames()) != 2 {
		t.Fatal(a.ProfileNames())
	}
	next, err := a.NextProfile(t.Context())
	must(t, err)
	if next == "" {
		t.Fatal("next")
	}
	_, err = a.SaveProfile(t.Context(), "", "")
	wantErr(t, err, "profile names")
	must(t, a.DeleteProfile("mine"))
	if err := a.DeleteProfile("gaming"); err == nil {
		t.Fatal("a shipped profile cannot be deleted")
	}
}

func TestStartup(t *testing.T) {
	_, a := machine(t)
	s, err := a.Startup()
	must(t, err)
	if s.Keyboard == "" {
		t.Fatalf("%+v", s)
	}
	v, err := a.SetStartup("keyboard", "default")
	must(t, err)
	if v != "default" {
		t.Fatal(v)
	}
	if _, err := a.SetStartup("keyboard", "profile:nope"); err == nil {
		t.Fatal("a lighting profile that does not exist")
	}
	if _, err := a.SetStartup("profile", "nope"); err == nil {
		t.Fatal("a profile that does not exist")
	}
	if _, err := a.SetStartup("rear", "last"); err != nil {
		t.Fatal(err)
	}
	lines, err := a.ApplyStartup(t.Context())
	must(t, err)
	if len(lines) == 0 {
		t.Fatal("one line per step")
	}
}
