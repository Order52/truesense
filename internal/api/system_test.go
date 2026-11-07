package api_test

import (
	"bytes"
	"encoding/json"
	"os"
	"reflect"
	"strings"
	"testing"

	"github.com/Order52/truesense/internal/api"
)

func TestStatusHasEverySectionInOrder(t *testing.T) {
	_, a := machine(t)
	st := a.Status(t.Context())
	var keys []string
	for _, e := range st.Entries {
		keys = append(keys, e.Key)
	}
	want := "power_mode sensors fans daemon battery_limit usb_charge_stop overdrive boot_sound kb_backlight_timeout gpu_mode rear touchpad audio_output jack voice"
	if strings.Join(keys, " ") != want {
		t.Fatalf("%v", keys)
	}
	b, err := json.Marshal(st)
	must(t, err)
	var m map[string]any
	must(t, json.Unmarshal(b, &m))
	if m["power_mode"] != "eco" || !strings.HasPrefix(m["daemon"].(string), "ERROR: the TrueSense daemon is not running") {
		t.Fatalf("%s", b)
	}
	if !bytes.HasPrefix(b, []byte(`{"power_mode":"eco","sensors":`)) {
		t.Fatalf("the sections keep their order in JSON: %s", b)
	}
	if st.Get("touchpad") != nil || st.Get("nothing") != nil {
		t.Fatal("a machine without a touchpad reports null")
	}
}

func TestStatusShowsAFailingReadingAsText(t *testing.T) {
	f, a := machine(t)
	f.S.HW.Dev = failing{}
	st := a.Status(t.Context())
	if s, _ := st.Get("power_mode").(string); !strings.HasPrefix(s, "ERROR: ") {
		t.Fatalf("%v", st.Get("power_mode"))
	}
	if st.Get("sensors") == nil {
		t.Fatal("the other sections are still there")
	}
}

type failing struct{}

func (failing) Run(string) (string, error) { return "", os.ErrPermission }

func TestDoctorAndSelfTestRunOnTheTestMachine(t *testing.T) {
	f, a := machine(t)
	before := f.Sim.Snapshot()
	rep := a.RunDoctor(t.Context())
	if len(rep.Checks) != len(a.DoctorChecks()) || rep.Checks[0].Check != a.DoctorChecks()[0] {
		t.Fatalf("%d checks", len(rep.Checks))
	}
	var out bytes.Buffer
	r := a.SelfTest(t.Context(), &out, true)
	if !strings.Contains(out.String(), "SKIP  fans") && !strings.Contains(out.String(), "SKIP") {
		t.Fatalf("the fans are skipped:\n%s", out.String())
	}
	if r.Total == 0 {
		t.Fatal("nothing ran")
	}
	if after := f.Sim.Snapshot(); !reflect.DeepEqual(before, after) {
		t.Fatalf("the self-test left the firmware changed:\nbefore %+v\nafter  %+v", before, after)
	}
}

func TestProbeIsReadOnlyAndNamesTheModel(t *testing.T) {
	f, a := machine(t)
	before := f.Sim.Snapshot()
	r := a.Probe()
	if r.Writes != 0 {
		t.Fatalf("%d writes", r.Writes)
	}
	after := f.Sim.Snapshot()
	if before.Mode != after.Mode || before.Fan != after.Fan {
		t.Fatal("the probe changed the firmware")
	}
}

func TestConfig(t *testing.T) {
	f, a := machine(t)
	if p := a.ConfigPaths(); len(p) != 2 || !strings.HasSuffix(p[1], "config.toml") {
		t.Fatal(p)
	}
	if problems := a.ValidateConfig(); len(problems) != 0 {
		t.Fatal(problems)
	}
	v, err := a.ConfigValues()
	must(t, err)
	if v["daemon"].(map[string]any)["auto_profile"] != true {
		t.Fatalf("%v", v["daemon"])
	}
	text, err := a.ConfigTOML()
	must(t, err)
	if !strings.Contains(text, "[daemon]") || strings.Contains(text, "'") {
		t.Fatalf("TOML with double-quoted strings:\n%s", text)
	}
	must(t, os.MkdirAll(f.Dir+"/config", 0o755))
	must(t, os.WriteFile(a.UserConfig, []byte("[profile]\nhold_minutes = 0\n[audio]\njack = \"loud\"\n"), 0o644))
	if problems := a.ValidateConfig(); len(problems) == 0 {
		t.Fatal("a bad value is reported")
	}
	must(t, os.WriteFile(a.UserConfig, []byte("this is not toml ="), 0o644))
	if problems := a.ValidateConfig(); len(problems) != 1 || !strings.Contains(problems[0], "config.toml") {
		t.Fatalf("a file that does not parse is one problem naming the file: %v", problems)
	}
}

func TestServiceCommands(t *testing.T) {
	f, a := machine(t)
	var out, errw bytes.Buffer
	if _, err := a.ServiceStatus(t.Context(), &out, &errw); err != nil {
		t.Fatal(err)
	}
	if _, err := a.RestartService(t.Context(), &out, &errw); err != nil {
		t.Fatal(err)
	}
	if _, err := a.ServiceLogs(t.Context(), &out, &errw); err != nil {
		t.Fatal(err)
	}
	got := f.Exec()
	want := []string{"systemctl --no-pager status truesense.service", "sudo systemctl restart truesense.service", "journalctl -u truesense.service -b --no-pager -n 60"}
	for i, w := range want {
		if strings.Join(got[i], " ") != w {
			t.Fatalf("%d: %v", i, got[i])
		}
	}
	rows := a.CheckServices(t.Context())
	if len(rows) == 0 || api.ServicesOK(rows) {
		t.Fatalf("on a machine with nothing installed the check fails: %+v", rows)
	}
}

func TestRepairNeedsRootForTheSystemPart(t *testing.T) {
	_, a := machine(t)
	_, err := a.RepairSystemServices(t.Context())
	wantErr(t, err, "the system part needs root")
	a.EUID = 0
	steps, err := a.RepairSystemServices(t.Context())
	must(t, err)
	if len(steps) == 0 {
		t.Fatal("steps")
	}
}

func TestRepairAsAUserCallsSudoForTheSystemPart(t *testing.T) {
	f, a := machine(t)
	steps, err := a.RepairServices(t.Context())
	must(t, err)
	var sudo []string
	for _, c := range f.Exec() {
		if c[0] == "sudo" {
			sudo = c
		}
	}
	if strings.Join(sudo, " ") != "sudo /usr/bin/truesense service repair --system" {
		t.Fatalf("%v", f.Exec())
	}
	last := steps[len(steps)-1]
	if last.OK || !strings.Contains(last.What, "system part did not run") {
		t.Fatalf("an answer sudo did not give is a failed step: %+v", last)
	}
	a.EUID = 0
	steps, err = a.RepairServices(t.Context())
	must(t, err)
	if !strings.Contains(steps[0].What, "your user services were not touched") || !steps[0].OK {
		t.Fatalf("%+v", steps[0])
	}
}

func TestOpenWindowStartsTheGUIDetached(t *testing.T) {
	f, a := machine(t)
	must(t, a.OpenWindow())
	if got := f.Spawned(); len(got) != 1 || strings.Join(got[0], " ") != "/opt/truesense gui" {
		t.Fatalf("%v", got)
	}
}
