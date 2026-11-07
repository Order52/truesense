package models

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"testing"

	"github.com/Order52/truesense/internal/firmware"
)

// dmiTree writes a fake /sys/class/dmi/id.
func dmiTree(t testing.TB, product, vendor, bios string) string {
	t.Helper()
	dir := t.TempDir()
	for name, v := range map[string]string{"product_name": product, "sys_vendor": vendor, "bios_version": bios, "bios_date": "06/09/2026"} {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(v+"\n"), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	return dir
}

func TestVerifiedModelIsSupportedAndANewBIOSIsOnlyANote(t *testing.T) {
	m := Identify(dmiTree(t, "Predator PH16-71", "Acer", "V1.18"))
	if m.Tier != TierSupported || m.BIOSVerified == nil || !*m.BIOSVerified {
		t.Fatalf("PH16-71 on V1.18 = %+v", m)
	}
	if m.Family != "Predator Helios 16" || m.Message != "Predator PH16-71, BIOS V1.18" {
		t.Fatalf("family/message = %q / %q", m.Family, m.Message)
	}
	m = Identify(dmiTree(t, "Predator PH16-71", "Acer", "V1.25"))
	if m.Tier != TierSupported || m.BIOSVerified == nil || *m.BIOSVerified || !strings.Contains(m.Message, "V1.18") {
		t.Fatalf("PH16-71 on V1.25 = %+v", m)
	}
}

func TestSimilarAcerGamingLaptopsAreUnverifiedNotSupported(t *testing.T) {
	for _, p := range []string{"Predator PH16-72", "Predator PH18-71", "Predator PHN16-71", "Predator PT16-51", "Nitro AN16-41", "Predator PHN18-71", "predator ph16-72"} {
		m := Identify(dmiTree(t, p, "Acer", "V1.18"))
		if m.Tier != TierUnverified || m.BIOSVerified != nil {
			t.Errorf("%s: %+v", p, m)
		}
		if !strings.Contains(m.Message, "truesense probe") || !strings.Contains(m.Message, "new_model_report") {
			t.Errorf("%s: message %q", p, m.Message)
		}
	}
}

func TestOtherMachinesDoNotApply(t *testing.T) {
	for _, c := range []struct{ product, vendor string }{
		{"Inspiron 15", "Dell Inc."}, {"Aspire 5", "Acer"}, {"", ""}, {"Predator PH16-71", "Not Acer Ltd"},
	} {
		if m := Identify(dmiTree(t, c.product, c.vendor, "V1")); m.Tier != TierUnsupported {
			t.Errorf("%+v graded %s", c, m.Tier)
		}
	}
	m := Identify("/nonexistent")
	if m.Tier != TierUnsupported || m.Product != "unknown" || m.BIOS != "?" {
		t.Errorf("no DMI at all: %+v", m)
	}
	if want := "'unknown' (unknown vendor) is not an Acer Predator: TrueSense does not apply"; m.Message != want {
		t.Errorf("message = %q, want %q", m.Message, want)
	}
}

func TestOnlyAVerifiedReportCanAddAModel(t *testing.T) {
	keys := make([]string, 0)
	for k, v := range Supported {
		keys = append(keys, k)
		if len(v.VerifiedBIOS) == 0 {
			t.Errorf("%s has no verified BIOS", k)
		}
	}
	if !slices.Equal(keys, []string{"Predator PH16-71"}) {
		t.Fatalf("supported models = %v", keys)
	}
}

// recorder is a firmware.Device that remembers every command it is sent.
type recorder struct {
	mu    sync.Mutex
	sent  []string
	reply func(string) (string, error)
}

func (r *recorder) Run(command string) (string, error) {
	r.mu.Lock()
	r.sent = append(r.sent, command)
	r.mu.Unlock()
	if r.reply != nil {
		return r.reply(command)
	}
	return "ok v=1\n", nil
}

// sysTree builds a fake /sys with the given WMI devices and, optionally, the loaded module.
func sysTree(t *testing.T, loaded bool, guids ...string) string {
	t.Helper()
	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, "bus", "wmi", "devices"), 0o755); err != nil {
		t.Fatal(err)
	}
	for _, g := range guids {
		if err := os.Mkdir(filepath.Join(dir, "bus", "wmi", "devices", g), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	if loaded {
		if err := os.MkdirAll(filepath.Join(dir, "module", "truesense_wmi"), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	return dir
}

// boundDevice is a device node that exists, so the probe believes the module is bound.
func boundDevice(t *testing.T) string {
	t.Helper()
	p := filepath.Join(t.TempDir(), "truesense")
	if err := os.WriteFile(p, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	return p
}

var wmiDevices = []string{"7A4DDFE7-5B5D-40B4-8595-4408E0CC7F56", "05901221-D566-11D1-B2F0-00A0C9062910-1"}

func TestProbeOnlyEverSendsReads(t *testing.T) {
	rec := &recorder{}
	r := Probe(rec, ProbeOptions{DMIRoot: dmiTree(t, "Predator PH18-71", "Acer", "V1"), SysRoot: sysTree(t, true, wmiDevices...), DevicePath: boundDevice(t), Kernel: "7.3.0"})
	if !slices.Equal(rec.sent, ReadOnly) {
		t.Fatalf("sent %v, want exactly %v", rec.sent, ReadOnly)
	}
	if r.Writes != 0 {
		t.Fatalf("writes = %d", r.Writes)
	}
	for _, c := range rec.sent {
		isRead := c == "version" || c == "caps" || c == "sensors" || strings.HasPrefix(c, "get_")
		if !isRead || strings.Contains(c, "set") || strings.Contains(c, "force") {
			t.Errorf("%q is not a read", c)
		}
	}
	if r.Answered != "14/14" || !r.Module.Loaded || !r.Module.Device {
		t.Errorf("answered=%s module=%+v", r.Answered, r.Module)
	}
}

func TestProbeReadOnlyListHasNoWrites(t *testing.T) {
	// The list itself is the contract: the kernel's set_*, force and rear commands must never appear.
	for _, c := range ReadOnly {
		if strings.HasPrefix(c, "set_") || strings.Contains(c, "force") || strings.HasPrefix(c, "rear") {
			t.Errorf("%q in ReadOnly", c)
		}
	}
}

func TestGamingInterfaceIsDetectedFromTheWMIDevices(t *testing.T) {
	dmi := dmiTree(t, "Predator PH18-71", "Acer", "V1")
	r := Probe(nil, ProbeOptions{DMIRoot: dmi, SysRoot: sysTree(t, false, wmiDevices...), DevicePath: "/nonexistent/x"})
	if !r.GamingInterfacePresent || !slices.Contains(r.WMIGUIDs, GamingWMIGUID) || len(r.WMIGUIDs) != 2 {
		t.Fatalf("guids = %v present=%v", r.WMIGUIDs, r.GamingInterfacePresent)
	}
	r = Probe(nil, ProbeOptions{DMIRoot: dmi, SysRoot: sysTree(t, false, "05901221-D566-11D1-B2F0-00A0C9062910", "not-a-guid"), DevicePath: "/nonexistent/x"})
	if r.GamingInterfacePresent || len(r.WMIGUIDs) != 1 {
		t.Fatalf("guids = %v present=%v", r.WMIGUIDs, r.GamingInterfacePresent)
	}
}

func TestWithoutTheDeviceItExplainsHowToReadSafelyAndNeverTouchesTheFirmware(t *testing.T) {
	rec := &recorder{}
	r := Probe(rec, ProbeOptions{DMIRoot: dmiTree(t, "Predator PH18-71", "Acer", "V1"), SysRoot: sysTree(t, false), DevicePath: "/nonexistent/truesense"})
	if len(rec.sent) != 0 {
		t.Fatalf("sent %v to an unbound module", rec.sent)
	}
	if !strings.Contains(r.Note, "force=1") || r.Answered != "0/0" || len(r.Firmware) != 0 {
		t.Fatalf("report = %+v", r)
	}
}

func TestAFirmwareThatDoesNotAnswerIsReportedNotFatal(t *testing.T) {
	rec := &recorder{reply: func(string) (string, error) { return "", errors.New(strings.Repeat("no such command ", 20)) }}
	r := Probe(rec, ProbeOptions{DMIRoot: dmiTree(t, "Predator PH18-71", "Acer", "V1"), SysRoot: sysTree(t, true), DevicePath: boundDevice(t)})
	if r.Answered != "0/14" || r.Firmware["get_fan"].OK {
		t.Fatalf("answered=%s fan=%+v", r.Answered, r.Firmware["get_fan"])
	}
	if n := len([]rune(r.Firmware["get_fan"].Error)); n != 120 {
		t.Errorf("error is %d runes, want it cut to 120", n)
	}
	// a firmware "err ..." reply counts as no answer too
	rec = &recorder{reply: func(c string) (string, error) {
		if c == "get_gpu" {
			return "err unknown command\n", nil
		}
		return "ok a=1\n", nil
	}}
	r = Probe(rec, ProbeOptions{DMIRoot: dmiTree(t, "Predator PH18-71", "Acer", "V1"), SysRoot: sysTree(t, true), DevicePath: boundDevice(t)})
	if r.Answered != "13/14" || r.Firmware["get_gpu"].Error != "unknown command" {
		t.Fatalf("answered=%s gpu=%+v", r.Answered, r.Firmware["get_gpu"])
	}
}

func TestReportIsJSONAndTextAndCarriesNoSerialNumbers(t *testing.T) {
	rec := &recorder{reply: func(string) (string, error) { return "ok v=1\n", nil }}
	r := Probe(rec, ProbeOptions{DMIRoot: dmiTree(t, "Predator PH18-71", "Acer", "V1"), SysRoot: sysTree(t, true, wmiDevices...), DevicePath: boundDevice(t), Kernel: "7.3.0-test", Version: "9.9.9"})
	text := Render(r)
	js, err := JSON(r)
	if err != nil {
		t.Fatal(err)
	}
	var data map[string]any
	if err := json.Unmarshal([]byte(js), &data); err != nil {
		t.Fatal(err)
	}
	if data["model"].(map[string]any)["product"] != "Predator PH18-71" || data["truesense"] != "9.9.9" || data["writes"] != float64(0) {
		t.Errorf("json = %s", js)
	}
	for _, key := range []string{"truesense", "model", "kernel", "writes", "module", "wmi_guids", "gaming_interface_present", "firmware", "answered", "verdict"} {
		if _, ok := data[key]; !ok {
			t.Errorf("JSON lacks %q (the 0.3.x probe has it)", key)
		}
	}
	if !strings.Contains(text, "Share it") || !strings.Contains(text, "0 writes") || !strings.Contains(text, "Verdict   unverified: share this report") {
		t.Errorf("text = %s", text)
	}
	for _, banned := range []string{"serial", "uuid"} {
		if strings.Contains(strings.ToLower(js), banned) {
			t.Errorf("report mentions %q", banned)
		}
	}
	fw := data["firmware"].(map[string]any)["version"].(map[string]any)
	if fw["ok"] != true || fw["value"].(map[string]any)["v"] != "1" {
		t.Errorf("firmware.version = %v", fw)
	}
}

func TestRenderForSupportedAndFailedAnswers(t *testing.T) {
	rec := &recorder{reply: func(c string) (string, error) {
		if c == "get_fan" {
			return "", errors.New("boom")
		}
		return "ok b=2 a=1\n", nil
	}}
	r := Probe(rec, ProbeOptions{DMIRoot: dmiTree(t, "Predator PH16-71", "Acer", "V1.18"), SysRoot: sysTree(t, true, wmiDevices...), DevicePath: boundDevice(t), Kernel: "7.3.0"})
	text := Render(r)
	for _, want := range []string{
		"Model     Predator PH16-71 (Acer), BIOS V1.18 06/09/2026", "Verdict   supported: verified on this model", "Kernel    7.3.0   module bound: yes",
		"WMI       Acer gaming interface PRESENT (2 WMI devices)", "  version        a=1 b=2", "  get_fan        (no answer: boom)", "Nothing was written (0 writes). ",
	} {
		if !strings.Contains(text, want) {
			t.Errorf("text lacks %q:\n%s", want, text)
		}
	}
	if strings.Contains(text, "Share it") {
		t.Error("a supported model is not asked to share a report")
	}
	if strings.Index(text, "  version ") > strings.Index(text, "  get_gpu ") {
		t.Error("answers must follow the ReadOnly order")
	}
}

func TestProbeAgainstTheSimulator(t *testing.T) {
	r := Probe(firmware.NewSimulator(), ProbeOptions{DMIRoot: dmiTree(t, "Predator PH16-71", "Acer", "V1.18"), SysRoot: sysTree(t, true), DevicePath: boundDevice(t)})
	var bad []string
	for name, a := range r.Firmware {
		if !a.OK {
			bad = append(bad, name+": "+a.Error)
		}
	}
	slices.Sort(bad)
	if len(bad) != 0 {
		t.Fatalf("the simulator does not answer %v", bad)
	}
}

func FuzzIdentify(f *testing.F) {
	f.Add("Predator PH16-71", "Acer", "V1.18")
	f.Add("Nitro AN16-41", "Acer Inc.", "")
	f.Add("", "", "")
	f.Add("Predator\x00PH16-71", "ACER\n", "\xff\xfe")
	f.Fuzz(func(t *testing.T, product, vendor, bios string) {
		m := Identify(dmiTree(t, product, vendor, bios))
		switch m.Tier {
		case TierSupported:
			if m.BIOSVerified == nil || !isAcer(m.Vendor) {
				t.Fatalf("supported without Acer/verification: %+v", m)
			}
		case TierUnverified, TierUnsupported:
			if m.BIOSVerified != nil {
				t.Fatalf("bios_verified set on %s", m.Tier)
			}
		default:
			t.Fatalf("tier %q", m.Tier)
		}
		if m.Message == "" {
			t.Fatal("no message")
		}
	})
}
