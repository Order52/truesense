package api_test

import (
	"testing"

	"github.com/Order52/truesense/internal/ipc"
)

func TestPowerStatusWithAndWithoutService(t *testing.T) {
	f, a := machine(t)
	st, err := a.PowerStatus()
	must(t, err)
	if st.Mode != "eco" || len(st.Supported) != 5 || st.AutoProfile != nil || st.HoldLeftS != nil {
		t.Fatalf("without a service: %+v", st)
	}
	d := f.StartDaemon()
	d.Reply("state", map[string]any{"live": map[string]any{}, "settings": map[string]any{"auto_profile": true}, "hold_left_s": 125})
	st, err = a.PowerStatus()
	must(t, err)
	if st.AutoProfile == nil || !*st.AutoProfile || st.HoldLeftS == nil || *st.HoldLeftS != 125 {
		t.Fatalf("with a service: %+v", st)
	}
}

func TestSetPowerGoesThroughTheServiceWhenItRuns(t *testing.T) {
	f, a := machine(t)
	got, err := a.SetPower("turbo", 30)
	must(t, err)
	if got != "turbo" || f.Sim.Snapshot().Mode != 5 {
		t.Fatalf("direct write: %q, firmware mode %d", got, f.Sim.Snapshot().Mode)
	}
	d := f.StartDaemon()
	if _, err := a.SetPower("quiet", 30); err != nil {
		t.Fatal(err)
	}
	call, ok := d.Last("power")
	if !ok || call.Args["mode"] != "quiet" || call.Args["hold_minutes"] != float64(30) {
		t.Fatalf("service request: %+v", call)
	}
}

func TestAutoModeIsDerivedFromTheServiceState(t *testing.T) {
	f, a := machine(t)
	if _, err := a.AutoMode(); err == nil {
		t.Fatal("no service: the question needs one")
	}
	d := f.StartDaemon()
	for _, tc := range []struct {
		settings map[string]any
		want     string
	}{
		{map[string]any{"auto_mode": "smart"}, "smart"},
		{map[string]any{"auto_profile": true}, "smart"},
		{map[string]any{"auto_profile": false}, "off"},
		{map[string]any{}, "off"},
	} {
		d.Reply("state", map[string]any{"live": map[string]any{}, "settings": tc.settings, "hold_left_s": 0})
		if got, err := a.AutoMode(); err != nil || got != tc.want {
			t.Fatalf("%v: %q %v, want %q", tc.settings, got, err, tc.want)
		}
	}
	d.Reply("set", map[string]any{"auto_mode": "native"})
	must(t, a.SetAutoMode("native"))
	if c, _ := d.Last("set"); c.Args["key"] != "auto_mode" || c.Args["value"] != "native" {
		t.Fatalf("%+v", c)
	}
}

func TestHoldAndRelease(t *testing.T) {
	f, a := machine(t)
	d := f.StartDaemon()
	must(t, a.HoldPower(12.5))
	if c, _ := d.Last("hold"); c.Args["minutes"] != 12.5 {
		t.Fatalf("%+v", c)
	}
	must(t, a.ReleaseHold())
	if c, _ := d.Last("hold"); c.Args["minutes"] != nil {
		t.Fatalf("release sends no minutes: %+v", c)
	}
}

func TestGameRulesUsageAndRequests(t *testing.T) {
	f, a := machine(t)
	_, err := a.GameRules("add", "game.exe", "")
	wantErr(t, err, "usage: truesense power game add PROGRAM MODE")
	_, err = a.GameRules("remove", "", "")
	wantErr(t, err, "usage: truesense power game remove PROGRAM")
	d := f.StartDaemon()
	d.Reply("game_rules", func(args ipc.Args) (any, error) {
		return map[string]any{"rules": map[string]any{"game.exe": args["mode"]}, "running": "game.exe"}, nil
	})
	r, err := a.GameRules("add", "game.exe", "turbo")
	must(t, err)
	if r.Rules["game.exe"] != "turbo" || r.Running == nil || *r.Running != "game.exe" {
		t.Fatalf("%+v", r)
	}
}

func TestPowerManagersDecode(t *testing.T) {
	f, a := machine(t)
	d := f.StartDaemon()
	d.Reply("power_managers", func(args ipc.Args) (any, error) {
		switch args["action"] {
		case "status":
			return map[string]any{"on": true, "found": []any{map[string]any{"unit": "tlp.service", "active": true, "enabled": false}}, "recorded": true}, nil
		case "takeover":
			return map[string]any{"found": []any{"tlp.service"}, "stopped": []any{"tlp.service"}, "failed": []any{}, "cpu_not_set": []any{}, "cpu": map[string]any{"governor": "powersave", "no_turbo": "0", "epp": nil}, "ceiling_fixed": true, "on": true}, nil
		}
		return map[string]any{"restored": []any{"tlp.service"}, "cpu": map[string]any{}, "on": false}, nil
	})
	st, err := a.PowerManagersStatus()
	must(t, err)
	if !st.On || len(st.Found) != 1 || st.Found[0].Unit != "tlp.service" || !st.Found[0].Active {
		t.Fatalf("%+v", st)
	}
	stop, err := a.StopPowerManagers()
	must(t, err)
	if len(stop.Stopped) != 1 || stop.CPU.Governor == nil || *stop.CPU.Governor != "powersave" || !stop.On {
		t.Fatalf("%+v", stop)
	}
	rest, err := a.RestorePowerManagers()
	must(t, err)
	if len(rest.Restored) != 1 || rest.On {
		t.Fatalf("%+v", rest)
	}
}

func TestNextPowerNeedsTheActionRunner(t *testing.T) {
	_, a := machine(t)
	a.Actions = nil
	_, err := a.NextPower(t.Context())
	wantErr(t, err, "key actions are not available")
}
