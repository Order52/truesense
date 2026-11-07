package api_test

import (
	"slices"
	"testing"

	"github.com/Order52/truesense/internal/api"
	"github.com/Order52/truesense/internal/ipc"
)

func curveEdit(cpu, gpu string) api.CurveEdit { return api.CurveEdit{CPU: cpu, GPU: gpu} }

func contains(list []string, s string) bool { return slices.Contains(list, s) }

func TestFanStatus(t *testing.T) {
	f, a := machine(t)
	st, err := a.FanStatus()
	must(t, err)
	if st.Mode != "auto" || st.Smart != nil {
		t.Fatalf("%+v", st)
	}
	d := f.StartDaemon()
	d.Reply("state", map[string]any{"live": map[string]any{}, "settings": map[string]any{"smart_fan": true}, "hold_left_s": 0})
	st, err = a.FanStatus()
	must(t, err)
	if st.Smart == nil || !*st.Smart {
		t.Fatalf("%+v", st)
	}
}

// A stopped fan is refused unless confirmed and watched by the service: the rules of the safety page.
func TestStoppingAFanNeedsTheServiceAndTheConfirmation(t *testing.T) {
	f, a := machine(t)
	wantErr(t, a.FanOff(true), "needs the TrueSense service")
	wantErr(t, a.FanSet(0, nil, true), "needs the TrueSense service")
	wantErr(t, a.FanSet(10, nil, true), "20..100")
	wantErr(t, a.FanSet(101, nil, true), "20..100")
	if snap := f.Sim.Snapshot(); snap.Fan != "auto" {
		t.Fatalf("a refused request left the fans in %q", snap.Fan)
	}
	d := f.StartDaemon()
	d.Reply("fan", func(args ipc.Args) (any, error) {
		if args["confirm"] != true {
			return nil, &ipc.RemoteError{Msg: "stopping a fan needs explicit confirmation"}
		}
		return "off", nil
	})
	wantErr(t, a.FanOff(false), "needs explicit confirmation")
	must(t, a.FanOff(true))
	must(t, a.FanSet(0, ptr(40), true))
	c, _ := d.Last("fan")
	if c.Args["mode"] != "manual" || c.Args["cpu"] != float64(0) || c.Args["gpu"] != float64(40) {
		t.Fatalf("%+v", c)
	}
}

func TestFanWritesGoToTheHardwareWithoutAService(t *testing.T) {
	f, a := machine(t)
	must(t, a.FanSet(60, nil, false))
	if s := f.Sim.Snapshot(); s.Fan != "manual" || s.Duty != [2]int{60, 60} {
		t.Fatalf("%+v", s)
	}
	must(t, a.FanMax())
	if f.Sim.Snapshot().Fan != "max" {
		t.Fatal("max")
	}
	must(t, a.FanAuto())
	if f.Sim.Snapshot().Fan != "auto" {
		t.Fatal("auto")
	}
	wantErr(t, a.FanSmart(), "smart curve needs the TrueSense daemon")
}

func TestSmartFanSwitch(t *testing.T) {
	f, a := machine(t)
	_, err := a.SmartFan()
	wantErr(t, err, "daemon is not running")
	d := f.StartDaemon()
	d.Reply("set", func(args ipc.Args) (any, error) { return map[string]any{"smart_fan": args["value"]}, nil })
	on, err := a.SetSmartFan(true)
	must(t, err)
	if !on {
		t.Fatal("the value the service reports is returned")
	}
}

func TestFanCurves(t *testing.T) {
	f, a := machine(t)
	l, err := a.FanCurves()
	must(t, err)
	if l.Names[0] != "native" || l.Current != "" || l.Descriptions["quiet"] == "" {
		t.Fatalf("%+v", l)
	}
	good := curveEdit("52:0,60:35,70:55,80:75,89:100", "48:0,60:40,78:100")
	saved, err := a.SaveFanCurve("mine", good)
	must(t, err)
	if len(saved.CPU) != 5 || saved.Hysteresis != 4 {
		t.Fatalf("%+v", saved)
	}
	// too cool for the load: refused, nothing saved
	_, err = a.SaveFanCurve("cool", curveEdit("52:0,60:35", "48:0,60:40"))
	if err == nil {
		t.Fatal("a curve that is not at 100 % by the throttle temperature must be refused")
	}
	if names, _ := a.FanCurves(); contains(names.Names, "cool") {
		t.Fatal("the refused curve was saved")
	}
	_, err = a.SaveFanCurve("empty", curveEdit("", ""))
	wantErr(t, err, "give --cpu and --gpu points")
	_, err = a.SaveFanCurve("", good)
	wantErr(t, err, "save under which name?")

	// editing: start from a built-in and change one thing
	pred := 7.0
	edited := good
	edited.From, edited.CPU, edited.GPU, edited.Predict = "quiet", "", "", &pred
	q, err := a.SaveFanCurve("quieter", edited)
	must(t, err)
	if q.Predict != 7 || len(q.CPU) != 6 {
		t.Fatalf("%+v", q)
	}
	name, err := a.CopyFanCurve("quiet", "")
	must(t, err)
	if name != "quiet-copy" {
		t.Fatal(name)
	}
	wantErr(t, a.DeleteFanCurve("quiet"), "built in")
	must(t, a.DeleteFanCurve("quieter"))

	// using one: a built-in goes by name, a saved one is sent as data
	d := f.StartDaemon()
	d.Reply("fan_curve", func(args ipc.Args) (any, error) {
		if args["action"] == "get" {
			return map[string]any{"name": "mine", "curve": map[string]any{}, "names": []string{"mine"}}, nil
		}
		return map[string]any{"name": args["name"], "curve": map[string]any{}}, nil
	})
	if _, err := a.UseFanCurve("quiet"); err != nil {
		t.Fatal(err)
	}
	if c, _ := d.Last("fan_curve"); c.Args["data"] != nil {
		t.Fatalf("a built-in curve is used by name: %+v", c)
	}
	if _, err := a.UseFanCurve("mine"); err != nil {
		t.Fatal(err)
	}
	if c, _ := d.Last("fan_curve"); c.Args["data"] == nil {
		t.Fatalf("a saved curve is sent as data: %+v", c)
	}
	_, err = a.UseFanCurve("")
	wantErr(t, err, "which curve?")
	l, err = a.FanCurves()
	must(t, err)
	if l.Current != "mine" {
		t.Fatalf("current %q", l.Current)
	}
}
