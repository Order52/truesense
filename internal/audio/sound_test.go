package audio

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"
)

func TestACardThatIsOffWithNoOutputSinkIsDetectedAndExplained(t *testing.T) {
	r := newRig(t, true)
	st, err := r.c.SoundState(ctx())
	if err != nil {
		t.Fatal(err)
	}
	if !st.IsBroken() || st.Profile != "off" || st.HifiOffered || !strings.Contains(st.Problem, "switched off") || !strings.Contains(st.Problem, "profiles are missing") {
		t.Fatalf("state = %+v", st)
	}
	r.sys.offered = []string{"off", hifiHP, hifiSpk, "pro-audio"} // profiles offered but none selected
	st, _ = r.c.SoundState(ctx())
	if !st.HifiOffered || strings.Contains(st.Problem, "missing") {
		t.Fatalf("hifi offered: %+v", st)
	}
	r.sys.profile = "pro-audio"
	if st, _ = r.c.SoundState(ctx()); st.Problem != "the sound card has no output (profile: pro-audio)" {
		t.Fatalf("problem = %q", st.Problem)
	}
}

func TestAHealthyCardAndAMissingCardAreNotTouched(t *testing.T) {
	r := newRig(t, false)
	st, _ := r.c.SoundState(ctx())
	if !st.IsOK() || len(st.Sinks) != 1 || st.Sinks[0] != testHW {
		t.Fatalf("state = %+v", st)
	}
	res, err := r.c.Repair(ctx(), RepairOptions{})
	if err != nil || !res.Fixed || res.Changed || len(res.Steps) != 0 {
		t.Fatalf("repair = %+v, %v", res, err)
	}
	if r.sys.did("systemctl") || r.sys.did("pactl", "set-card-profile") {
		t.Fatalf("nothing is restarted or switched: %v", r.sys.sent())
	}
	r = newRig(t, false)
	r.sys.cardPresent = false
	st, _ = r.c.SoundState(ctx())
	if st.OK != nil || st.Problem != "no sound card known to PipeWire" {
		t.Fatalf("no card: %+v", st)
	}
	res, _ = r.c.Repair(ctx(), RepairOptions{})
	if res.Fixed || res.Changed {
		t.Fatalf("repair without a card = %+v", res)
	}
}

func TestRepairRestartsWirePlumberWhenTheProfilesAreMissing(t *testing.T) {
	r := newRig(t, true)
	res, err := r.c.Repair(ctx(), RepairOptions{Timeout: time.Second, Poll: 10 * time.Millisecond})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Fixed || !res.Changed || !strings.Contains(res.Steps[0], "restarted WirePlumber") || !res.State.IsOK() {
		t.Fatalf("result = %+v", res)
	}
	if !r.sys.did("systemctl", "--user", "restart", "wireplumber.service") || r.sys.profile != hifiSpk {
		t.Fatalf("sent %v", r.sys.sent())
	}
}

func TestRepairJustPicksTheProfileWhenTheCardStillOffersIt(t *testing.T) {
	r := newRig(t, true)
	r.sys.offered = []string{"off", hifiHP, hifiSpk, "pro-audio"}
	res, _ := r.c.Repair(ctx(), RepairOptions{Timeout: time.Second, Poll: 10 * time.Millisecond})
	if !res.Fixed || len(res.Steps) != 1 || res.Steps[0] != "selected the speaker profile" {
		t.Fatalf("result = %+v", res)
	}
	if r.sys.did("systemctl") {
		t.Fatal("no restart needed")
	}
}

func TestRepairReportsHonestlyWhenARestartDoesNotHelp(t *testing.T) {
	r := newRig(t, true)
	r.sys.restartFixes = false
	res, _ := r.c.Repair(ctx(), RepairOptions{Timeout: 300 * time.Millisecond, Poll: 10 * time.Millisecond})
	if res.Fixed || !res.State.IsBroken() || len(res.Steps) == 0 {
		t.Fatalf("result = %+v", res)
	}
	for _, cmd := range r.sys.sent() {
		if strings.HasPrefix(cmd, "pactl set-card-profile") && strings.Contains(cmd, "HiFi") {
			t.Fatalf("it forced a profile the card does not offer: %s", cmd)
		}
	}
}

func TestRepairThatRestartsAndThenFindsTheProfilesPicksOne(t *testing.T) {
	r := newRig(t, true)
	// the restart brings the profiles back but not the sink: the second look must select one
	r.sys.restartFixes = false
	restart := r.c.Run
	r.c.Run = func(c2 context.Context, name string, args ...string) Result {
		res := restart(c2, name, args...)
		if name == "systemctl" && strings.Join(args, " ") == "--user restart wireplumber.service" {
			r.sys.offered = []string{"off", hifiHP, hifiSpk, "pro-audio"}
		}
		return res
	}
	res, _ := r.c.Repair(ctx(), RepairOptions{Timeout: 200 * time.Millisecond, Poll: 10 * time.Millisecond})
	if !res.Fixed || len(res.Steps) != 2 || res.Steps[1] != "selected the speaker profile" {
		t.Fatalf("result = %+v", res)
	}
}

func TestTheRepairNeverTouchesTheCodec(t *testing.T) {
	r := newRig(t, true)
	r.c.Repair(ctx(), RepairOptions{Timeout: time.Second, Poll: 10 * time.Millisecond})
	for _, cmd := range r.sys.sent() {
		if strings.Contains(cmd, "reconfig") || strings.HasPrefix(cmd, "amixer") || strings.HasPrefix(cmd, "hda-verb") || strings.HasPrefix(cmd, "tee") {
			t.Fatalf("the repair ran %q", cmd)
		}
	}
}

func TestTheLoginServiceRepairsAMissingOutputOnceAndLeavesAHealthyOneAlone(t *testing.T) {
	r := newRig(t, true)
	r.c.AutoRepair(ctx(), []time.Duration{time.Second, 2 * time.Second})
	if r.sys.profile != hifiSpk || len(r.osd.shown) != 1 || r.osd.shown[0] != "audio: Sound output repaired" {
		t.Fatalf("profile %q, osd %v", r.sys.profile, r.osd.shown)
	}
	if got := r.clk.sleeps[0]; got != time.Second {
		t.Errorf("first look after %v", got)
	}
	r = newRig(t, false)
	r.c.AutoRepair(ctx(), []time.Duration{time.Second, 2 * time.Second})
	if len(r.osd.shown) != 0 || r.sys.did("systemctl") {
		t.Fatalf("healthy card was touched: %v %v", r.osd.shown, r.sys.sent())
	}
	if len(r.clk.sleeps) != 1 {
		t.Errorf("a healthy card needs one look, slept %v", r.clk.sleeps)
	}
}

func TestTheLoginServiceLooksAgainAtTheSecondDelayMeasuredFromTheStart(t *testing.T) {
	r := newRig(t, true)
	r.sys.restartFixes = false // still broken after the first repair: the second look repairs again
	r.c.AutoRepair(ctx(), []time.Duration{20 * time.Second, 90 * time.Second})
	// a repair that ends "still missing" is the end of that check (at most once per check)
	if r.sys.restarted != 1 {
		t.Fatalf("restarts = %d, want 1 per check that found a problem", r.sys.restarted)
	}
	r = newRig(t, false)
	r.sys.cardPresent = true
	calls := 0
	run := r.c.Run
	r.c.Run = func(c2 context.Context, name string, args ...string) Result {
		if name == "pactl" && strings.Join(args, " ") == "list cards" && calls == 0 {
			calls++
			return Result{Stdout: "garbage with no card"} // the first look fails to parse
		}
		return run(c2, name, args...)
	}
	r.c.AutoRepair(ctx(), []time.Duration{20 * time.Second, 90 * time.Second})
	if len(r.clk.sleeps) != 2 || r.clk.sleeps[0] != 20*time.Second || r.clk.sleeps[1] != 70*time.Second {
		t.Fatalf("sleeps = %v: the second delay is counted from the start", r.clk.sleeps)
	}
}

func TestSoundStateJSONHasTheOldShapes(t *testing.T) {
	r := newRig(t, true)
	broken, _ := r.c.SoundState(ctx())
	healthy, _ := newRig(t, false).c.SoundState(ctx())
	none := SoundState{Problem: "no sound card known to PipeWire"}
	for name, tc := range map[string]struct {
		st   SoundState
		keys []string
	}{
		"broken":  {broken, []string{"ok", "card", "profile", "offered", "hifi_offered", "problem"}},
		"healthy": {healthy, []string{"ok", "card", "profile", "sinks"}},
		"no card": {none, []string{"ok", "problem"}},
	} {
		b, err := json.Marshal(tc.st)
		if err != nil {
			t.Fatal(err)
		}
		var m map[string]any
		_ = json.Unmarshal(b, &m)
		if len(m) != len(tc.keys) {
			t.Errorf("%s: %s", name, b)
		}
		for _, k := range tc.keys {
			if _, ok := m[k]; !ok {
				t.Errorf("%s lacks %q: %s", name, k, b)
			}
		}
	}
	if b, _ := json.Marshal(broken); !strings.Contains(string(b), `"hifi_offered":false`) || !strings.Contains(string(b), `"ok":false`) {
		t.Errorf("broken = %s", b)
	}
	if b, _ := json.Marshal(none); !strings.Contains(string(b), `"ok":null`) {
		t.Errorf("none = %s", b)
	}
	res, _ := r.c.Repair(ctx(), RepairOptions{Timeout: time.Second, Poll: time.Millisecond})
	b, _ := json.Marshal(res)
	for _, k := range []string{`"fixed":true`, `"changed":true`, `"steps":[`, `"state":{`} {
		if !strings.Contains(string(b), k) {
			t.Errorf("repair JSON lacks %s: %s", k, b)
		}
	}
	if b, _ := json.Marshal(RepairResult{Steps: []string{}, State: none}); !strings.Contains(string(b), `"steps":[]`) {
		t.Errorf("empty steps = %s", b)
	}
}

func TestDoctorsView(t *testing.T) {
	r := newRig(t, true)
	known, ok, problem, err := r.c.SoundCheck(ctx())
	if err != nil || !known || ok || !strings.Contains(problem, "switched off") {
		t.Fatalf("broken: %v %v %q %v", known, ok, problem, err)
	}
	r = newRig(t, false)
	if known, ok, _, _ = r.c.SoundCheck(ctx()); !known || !ok {
		t.Fatalf("healthy: %v %v", known, ok)
	}
	r.sys.cardPresent = false
	if known, _, _, _ = r.c.SoundCheck(ctx()); known {
		t.Fatal("no card is not known")
	}
	if !r.c.HasRNNoise() || !r.c.HasDTS() {
		t.Error("plugin and AcerUltraX are configured in the rig")
	}
	r.c.Plugin, r.c.DTSBinary = "", nil
	if r.c.HasRNNoise() || r.c.HasDTS() {
		t.Error("nothing installed")
	}
}
