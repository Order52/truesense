package api_test

import (
	"strings"
	"testing"

	"github.com/Order52/truesense/internal/api"
	"github.com/Order52/truesense/internal/ipc"
)

func TestSmartProfiles(t *testing.T) {
	f, a := machine(t)
	l, err := a.SmartProfiles()
	must(t, err)
	if l.Names[0] != "native" || l.Descriptions["silent"] == "" || l.Current != "" {
		t.Fatalf("%+v", l)
	}
	p, err := a.SaveSmartProfile("mine", "silent", []string{"ac_busy=turbo", "ac_turbo=on", "turbo_after_s=40"})
	must(t, err)
	if p.ACBusy != "turbo" || !p.ACTurbo || p.TurboAfterS != 40 || p.ACIdle != "quiet" {
		t.Fatalf("started from silent and changed three things: %+v", p)
	}
	for _, bad := range []string{"nope=1", "ac_busy=", "ac_busy", "turbo_after_s=x"} {
		if _, err := a.SaveSmartProfile("mine", "", []string{bad}); err == nil {
			t.Fatalf("%q must be refused", bad)
		}
	}
	_, err = a.SaveSmartProfile("mine", "", []string{"ac_busy=ludicrous"})
	if err == nil {
		t.Fatal("a mode that does not exist")
	}
	_, err = a.SaveSmartProfile("", "", nil)
	wantErr(t, err, "save under which name?")
	// editing your own profile keeps what it had
	p, err = a.SaveSmartProfile("mine", "", []string{"idle_after_s=100"})
	must(t, err)
	if p.ACBusy != "turbo" || p.IdleAfterS != 100 {
		t.Fatalf("%+v", p)
	}
	if n, err := a.CopySmartProfile("steady", ""); err != nil || n != "steady-copy" {
		t.Fatalf("%q %v", n, err)
	}
	wantErr(t, a.DeleteSmartProfile("steady"), "built in")

	d := f.StartDaemon()
	d.Reply("smart_profile", func(args ipc.Args) (any, error) {
		if args["action"] == "get" {
			return map[string]any{"name": "mine", "profile": map[string]any{}, "names": []string{}}, nil
		}
		return map[string]any{"name": args["name"], "profile": map[string]any{}}, nil
	})
	if _, err := a.UseSmartProfile("steady"); err != nil {
		t.Fatal(err)
	}
	if c, _ := d.Last("smart_profile"); c.Args["data"] != nil {
		t.Fatal("built-in profiles go by name")
	}
	if _, err := a.UseSmartProfile("mine"); err != nil {
		t.Fatal(err)
	}
	if c, _ := d.Last("smart_profile"); c.Args["data"] == nil {
		t.Fatal("yours is sent as data")
	}
	if l, _ := a.SmartProfiles(); l.Current != "mine" {
		t.Fatalf("%+v", l)
	}
}

func TestLooks(t *testing.T) {
	_, a := machine(t)
	if names := a.Looks(); len(names) == 0 || names[0] != "native" {
		t.Fatal(names)
	}
	must(t, a.SetKeyboardEffect(api.EffectSpec{Name: "static", Color: "ff0000", Speed: 5}))
	l, err := a.SaveLook("mine")
	must(t, err)
	if l.Keyboard == nil || l.Keyboard.Color != "ff0000" {
		t.Fatalf("%+v", l)
	}
	if !strings.Contains(a.DescribeLook(l), "static") {
		t.Fatal(a.DescribeLook(l))
	}
	must(t, a.SetKeyboardEffect(api.EffectSpec{Name: "static", Color: "00ff00", Speed: 5}))
	done, err := a.ApplyLook("mine")
	must(t, err)
	if len(done) == 0 || a.Looks()[len(a.Looks())-1] != "mine" {
		t.Fatalf("%v %v", done, a.Looks())
	}
	if _, err := a.ApplyLook("nope"); err == nil {
		t.Fatal("unknown look")
	}
	must(t, a.DeleteLook("mine"))
	if err := a.DeleteLook("native"); err == nil {
		t.Fatal("a built-in look cannot be deleted")
	}
}

func TestBootLook(t *testing.T) {
	f, a := machine(t)
	_, err := a.BootLook()
	wantErr(t, err, "daemon is not running")
	d := f.StartDaemon()
	d.Reply("boot_look", nil)
	if l, err := a.BootLook(); err != nil || l != nil {
		t.Fatalf("%v %v", l, err)
	}
	d.Reply("boot_look", func(args ipc.Args) (any, error) { return args["look"], nil })
	l, err := a.SetBootLook("native")
	must(t, err)
	if l == nil {
		t.Fatal("look")
	}
	if c, _ := d.Last("boot_look"); c.Args["action"] != "set" || c.Args["look"] == nil {
		t.Fatalf("%+v", c)
	}
	must(t, a.ClearBootLook())
	if _, err := a.SetBootLook("nope"); err == nil {
		t.Fatal("unknown look")
	}
}
