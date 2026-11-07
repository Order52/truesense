package audio

import (
	"context"
	"fmt"
	"os"
	"reflect"
	"strings"
	"testing"
	"time"
)

func (f *fakeSystem) modeCalls() [][]string {
	f.mu.Lock()
	defer f.mu.Unlock()
	var out [][]string
	for _, c := range f.calls {
		if len(c) > 1 && strings.HasSuffix(c[0], "acerultrax-cli") && c[1] == "mode" {
			out = append(out, c[1:])
		}
	}
	return out
}

func TestDTSDevicesAreReadIncludingLabelsThatRunIntoTheKindColumn(t *testing.T) {
	r := newRig(t, false)
	d, err := r.c.DTSDevices(ctx())
	if err != nil {
		t.Fatal(err)
	}
	kinds := make([]string, len(d))
	for i, x := range d {
		kinds[i] = x.Kind
	}
	if !reflect.DeepEqual(kinds, []string{"speakers", "headphones", "other", "other", "bluetooth-speaker"}) || d[0].Label != "Laptop speakers" {
		t.Fatalf("devices = %+v", d)
	}
	if d[2].Label != "DualSense wireless controller (PS5) Direct DualSense Wireless Controller" || !d[3].Current || d[3].Ref != usbHeadset {
		t.Fatalf("devices = %+v", d)
	}
}

func TestAPlainUSBHeadsetGetsTheSpeakersProfileAndAHeadphoneEQOnce(t *testing.T) {
	r := newRig(t, false)
	done, err := r.c.DTSCover(ctx(), false)
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, x := range done {
		found = found || strings.Contains(x, "Headset PC38X: Music + headphone EQ")
	}
	if !found {
		t.Fatalf("done = %v", done)
	}
	if r.sys.dtsProfile[usbHeadset] != "Music" || !r.sys.dtsEQ[usbHeadset] {
		t.Fatalf("profile %v eq %v", r.sys.dtsProfile, r.sys.dtsEQ)
	}
	if _, touched := r.sys.dtsProfile["bluez_output.AA.1"]; touched {
		t.Error("a Bluetooth speaker keeps Plain")
	}
	if r.sys.did("/x/acerultrax-cli", "mode", "Music", "--device", "@speakers") || r.sys.did("/x/acerultrax-cli", "mode", "Music", "--device", "@headphones") {
		t.Error("the laptop's own outputs are AcerUltraX's business")
	}
	r.sys.calls = nil
	if again, _ := r.c.DTSCover(ctx(), false); len(again) != 0 || len(r.sys.modeCalls()) != 0 {
		t.Fatalf("second look changes nothing: %v %v", again, r.sys.modeCalls())
	}
	// the memory is in the user state, as a sorted list
	var seen []string
	if err := r.c.State.GetInto("dts_seen", &seen); err != nil || !reflect.DeepEqual(seen, []string{usbHeadset, dualSense}) {
		t.Errorf("dts_seen = %v, %v", seen, err)
	}
}

func TestTheHeadphoneEQIsOnlyAskedForWhenAcerUltraXHasNone(t *testing.T) {
	r := newRig(t, false)
	if err := os.WriteFile(r.c.DTSConfigPath, []byte("[headphones.\"alsa_output.usb-GeneralPlus_USB_Audio_Device-00.analog-stereo\"]\nbands = [1,2]\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	r.c.DTSCover(ctx(), false)
	for _, c := range r.sys.sent() {
		if strings.Contains(c, "headphone") && strings.Contains(c, "GeneralPlus") {
			t.Errorf("an EQ it already has was asked for again: %s", c)
		}
	}
	if !r.sys.did("/x/acerultrax-cli", "headphone", "auto", "--device", dualSense) {
		t.Errorf("a controller without an EQ gets AcerUltraX's own pick first: %v", r.sys.sent())
	}
}

func TestAProfileChosenOnPurposeLaterIsLeftAlone(t *testing.T) {
	r := newRig(t, false)
	r.c.DTSCover(ctx(), false)
	r.sys.dtsProfile[usbHeadset] = "Plain"
	r.sys.calls = nil
	r.c.DTSCover(ctx(), false)
	for _, c := range r.sys.sent() {
		if strings.Contains(c, " mode ") || strings.Contains(c, " headphone ") {
			t.Errorf("changed something on a second look: %s", c)
		}
	}
}

func TestSpeakersOnPlainMeansNoDTSSoNothingIsChanged(t *testing.T) {
	r := newRig(t, false)
	r.sys.dtsDevicesText = strings.Replace(realDevices, "speakers           Music", "speakers           Plain", 1)
	done, err := r.c.DTSCover(ctx(), false)
	if err != nil || len(done) != 0 || len(r.sys.modeCalls()) != 0 {
		t.Fatalf("done %v calls %v err %v", done, r.sys.modeCalls(), err)
	}
}

func TestForceRedoesEveryOutput(t *testing.T) {
	r := newRig(t, false)
	r.c.DTSCover(ctx(), false)
	r.sys.dtsProfile[usbHeadset] = "Plain"
	done, _ := r.c.DTSCover(ctx(), true)
	if len(done) == 0 || r.sys.dtsProfile[usbHeadset] != "Music" {
		t.Fatalf("done = %v profile %v", done, r.sys.dtsProfile)
	}
	// forced, the Bluetooth speaker is included (without a headphone EQ)
	if r.sys.dtsProfile["bluez_output.AA.1"] != "Music" || r.sys.dtsEQ["bluez_output.AA.1"] {
		t.Errorf("bluetooth speaker: %v %v", r.sys.dtsProfile, r.sys.dtsEQ)
	}
	// a forced look with nothing to do says so
	r2 := newRig(t, false)
	r2.sys.dtsDevicesText = "  Laptop speakers   speakers  Movie\n    @speakers\n"
	got, _ := r2.c.DTSCover(ctx(), true)
	if !reflect.DeepEqual(got, []string{"every output is already on Movie"}) {
		t.Errorf("got %v", got)
	}
	r3 := newRig(t, false)
	r3.sys.dtsDevicesText = "  Dev   other  Plain\n    r\n" // no speakers row at all, forced: Music
	if _, _ = r3.c.DTSCover(ctx(), true); r3.sys.dtsProfile["r"] != "Music" {
		t.Errorf("forced with no speakers row: %v", r3.sys.dtsProfile)
	}
}

func TestAModeChoiceGoesToEveryOutput(t *testing.T) {
	r := newRig(t, false)
	out, err := r.c.DTSMode(ctx(), "game1", DTSEverywhere)
	if err != nil || out != "ok" {
		t.Fatalf("out = %q, %v", out, err)
	}
	modes := r.sys.modeCalls()
	if !reflect.DeepEqual(modes[0], []string{"mode", "game1"}) {
		t.Fatalf("first call = %v", modes[0])
	}
	withDevice := 0
	for _, m := range modes {
		if indexOf(m, "--device") >= 0 {
			withDevice++
			if m[len(m)-1] == usbHeadset {
				t.Error("the current output must not be set twice")
			}
		}
	}
	if withDevice != 4 {
		t.Errorf("%d per-device calls, want every other output (4): %v", withDevice, modes)
	}
	// an output that already has the profile is skipped (compared without case)
	r2 := newRig(t, false)
	r2.sys.dtsProfile[dualSense] = "Game1"
	r2.c.DTSMode(ctx(), "game1", DTSEverywhere)
	if r2.sys.did("/x/acerultrax-cli", "mode", "game1", "--device", dualSense) {
		t.Error("an output already on the profile was set again")
	}
}

func TestTheScopeFollowsTheSettingUnlessTheCallerSaysOtherwise(t *testing.T) {
	r := newRig(t, false)
	r.cfg.Audio.DTSAll = false
	r.c.DTSMode(ctx(), "music", DTSConfigured)
	if n := len(r.sys.modeCalls()); n != 1 {
		t.Errorf("dts_all = false: %d mode calls, want only the one playing", n)
	}
	r.sys.calls = nil
	r.c.DTSMode(ctx(), "music", DTSEverywhere)
	if len(r.sys.modeCalls()) < 2 {
		t.Error("explicit everywhere ignored the setting")
	}
	r.cfg.Audio.DTSAll = true
	r.sys.calls = nil
	r.c.DTSMode(ctx(), "music", DTSCurrentOnly)
	if len(r.sys.modeCalls()) != 1 {
		t.Error("explicit current-only ignored")
	}
	r.sys.calls = nil
	r.c.DTSMode(ctx(), "movie", DTSConfigured)
	if len(r.sys.modeCalls()) < 2 {
		t.Error("the default is every output")
	}
}

func TestDTSErrors(t *testing.T) {
	r := newRig(t, false)
	r.sys.dtsFail["status"] = "warning\nAcerUltraX daemon is not running\n"
	if _, err := r.c.DTSStatus(ctx()); err == nil || err.Error() != "AcerUltraX daemon is not running" {
		t.Errorf("err = %v (the last line of what it said)", err)
	}
	r.sys.dtsFail["power"] = " "
	r.sys.dtsFail["mode"] = ""
	r.c.Run = func(_ context.Context, name string, args ...string) Result { return Result{Code: 2} }
	if _, err := r.c.DTSPower(ctx(), true); err == nil || err.Error() != "acerultrax-cli failed" {
		t.Errorf("silent failure: %v", err)
	}
	r.c.DTSBinary = func() string { return "" }
	if _, err := r.c.DTSStatus(ctx()); err == nil || !strings.Contains(err.Error(), "AcerUltraX is not installed") {
		t.Errorf("not installed: %v", err)
	}
	if _, err := r.c.DTSMode(ctx(), "music", DTSConfigured); err == nil {
		t.Error("mode without AcerUltraX")
	}
	if _, err := r.c.DTSCover(ctx(), false); err == nil {
		t.Error("cover without AcerUltraX")
	}
	r.c.DTSBinary = nil
	if r.c.HasDTS() {
		t.Error("no locator = not installed")
	}
}

func TestDTSPowerAndStatus(t *testing.T) {
	r := newRig(t, false)
	r.sys.dtsStdout["status"] = "DTS:            on\n"
	if out, _ := r.c.DTSStatus(ctx()); !strings.HasPrefix(out, "DTS:            on") {
		t.Errorf("status = %q", out)
	}
	r.c.DTSPower(ctx(), true)
	r.c.DTSPower(ctx(), false)
	if !r.sys.did("/x/acerultrax-cli", "power", "on") || !r.sys.did("/x/acerultrax-cli", "power", "off") {
		t.Errorf("sent %v", r.sys.sent())
	}
}

func TestDTSTimeoutIsPerCall(t *testing.T) {
	r := newRig(t, false)
	var got time.Duration
	r.c.Run = func(c context.Context, name string, args ...string) Result {
		dl, ok := c.Deadline()
		if !ok {
			t.Error("no deadline: a hung helper would hang the audio service")
		}
		got = time.Until(dl)
		return Result{}
	}
	r.c.DTS(ctx(), 1800*time.Second, "setup")
	if got < 1700*time.Second {
		t.Errorf("setup timeout = %v", got)
	}
	r.c.DTSStatus(ctx())
	if got > 31*time.Second {
		t.Errorf("default timeout = %v, want 30 s", got)
	}
}

func TestPlainHeadsetsIsWhatTheDoctorWarnsAbout(t *testing.T) {
	r := newRig(t, false)
	got, err := r.c.PlainHeadsets(ctx())
	want := []string{"DualSense wireless controller (PS5) Direct DualSense Wireless Controller", "Headset PC38X"}
	if err != nil || !reflect.DeepEqual(got, want) {
		t.Fatalf("got %v, %v", got, err)
	}
	r.c.DTSCover(ctx(), false)
	if got, _ = r.c.PlainHeadsets(ctx()); len(got) != 0 {
		t.Errorf("after the cover: %v", got)
	}
}

func TestFindDTS(t *testing.T) {
	dir := t.TempDir()
	home := dir + "/home"
	sudoHome := dir + "/sudo"
	mk := func(p string) {
		if err := os.MkdirAll(p[:strings.LastIndex(p, "/")], 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, nil, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	notFound := func(string) (string, error) { return "", fmt.Errorf("not found") }
	if got := FindDTS(notFound, home, sudoHome); got != "" && !strings.HasPrefix(got, "/usr") {
		t.Errorf("nothing installed in the temp homes, got %q", got)
	}
	mk(sudoHome + "/.local/bin/acerultrax-cli")
	if got := FindDTS(notFound, home, sudoHome); got != sudoHome+"/.local/bin/acerultrax-cli" {
		t.Errorf("sudo user's home: %q", got)
	}
	mk(home + "/.local/bin/acerultrax-cli")
	if got := FindDTS(notFound, home, sudoHome); got != home+"/.local/bin/acerultrax-cli" {
		t.Errorf("own home first: %q", got)
	}
	if got := FindDTS(func(string) (string, error) { return "/opt/bin/acerultrax-cli", nil }, home); got != "/opt/bin/acerultrax-cli" {
		t.Errorf("PATH first: %q", got)
	}
	if got := FindDTS(notFound, "", ""); strings.Contains(got, ".local") {
		t.Errorf("empty homes must not become /.local/bin: %q", got)
	}
}
