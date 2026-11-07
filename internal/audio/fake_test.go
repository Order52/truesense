package audio

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/Order52/truesense/internal/config"
	"github.com/Order52/truesense/internal/userstate"
)

const (
	testCard = "alsa_card.pci-0000_00_1f.3-platform-skl_hda_dsp_generic"
	testHW   = "alsa_output.pci-0000_00_1f.3-platform-skl_hda_dsp_generic.HiFi__Speaker__sink"
	testHP   = "alsa_output.pci-0000_00_1f.3-platform-skl_hda_dsp_generic.HiFi__Headphones__sink"
	testSrc1 = "alsa_input.pci-0000_00_1f.3-platform-skl_hda_dsp_generic.HiFi__Mic1__source"
	testSrc2 = "alsa_input.pci-0000_00_1f.3-platform-skl_hda_dsp_generic.HiFi__Mic2__source"
	hifiSpk  = "HiFi (Mic1, Mic2, Speaker)"
	hifiHP   = "HiFi (Headphones, Mic1, Mic2)"
)

// fakeSystem is pactl, wpctl, amixer, systemctl and acerultrax-cli as they behaved on the
// PH16-71, with enough state for the logic above them to be driven end to end. It records every
// command (calls) so tests can assert exactly what was sent, and it is the ONLY thing the audio
// tests ever run: no real program is started.
type fakeSystem struct {
	mu    sync.Mutex
	calls [][]string

	// card
	cardPresent  bool
	offered      []string
	profile      string
	sinkUp       bool // a hardware sink exists
	restartFixes bool
	restarted    int
	extraSinks   []string // other outputs (USB, Bluetooth, HDMI)
	defaultSink  string
	sinkInputs   []string
	sinkDescs    map[string]string

	// microphones
	defaultSource string
	muted         map[string]bool // by full source name
	volume        string          // pactl get-source-volume text
	sourceOutputs []string
	extraSources  []string
	confDir       string // PipeWire reads its filter chains from here: a restart creates their nodes

	// ALSA
	headphoneJack, headsetMicJack string
	boost                         int
	boostBroken                   bool // cset is accepted but the value does not change
	noBoostControl                bool

	// AcerUltraX
	dtsDevicesText string
	dtsProfile     map[string]string // ref -> profile
	dtsEQ          map[string]bool
	dtsFail        map[string]string // "mode"/"devices"/... -> stderr
	dtsStdout      map[string]string
}

func newFakeSystem(broken bool) *fakeSystem {
	f := &fakeSystem{cardPresent: true, restartFixes: true, muted: map[string]bool{}, headphoneJack: "off", headsetMicJack: "off", boost: 1, defaultSource: testSrc1,
		volume: "Volume: front-left: 65536 / 100% / 0.00 dB,   front-right: 65536 / 100% / 0.00 dB\n        balance 0.00\n", sinkDescs: map[string]string{},
		dtsProfile: map[string]string{}, dtsEQ: map[string]bool{}, dtsFail: map[string]string{}, dtsStdout: map[string]string{}}
	f.offered = []string{"off", hifiHP, hifiSpk, "pro-audio"}
	if broken {
		f.offered = []string{"off", "pro-audio"}
		f.profile, f.sinkUp = "off", false
	} else {
		f.profile, f.sinkUp = hifiSpk, true
	}
	f.defaultSink = "acerultrax_sink"
	return f
}

func (f *fakeSystem) did(prefix ...string) bool {
	f.mu.Lock()
	defer f.mu.Unlock()
	for _, c := range f.calls {
		if len(c) >= len(prefix) && strings.Join(c[:len(prefix)], "\x00") == strings.Join(prefix, "\x00") {
			return true
		}
	}
	return false
}

func (f *fakeSystem) count(prefix ...string) int {
	f.mu.Lock()
	defer f.mu.Unlock()
	n := 0
	for _, c := range f.calls {
		if len(c) >= len(prefix) && strings.Join(c[:len(prefix)], "\x00") == strings.Join(prefix, "\x00") {
			n++
		}
	}
	return n
}

// sent returns every recorded command as one string each.
func (f *fakeSystem) sent() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	out := make([]string, len(f.calls))
	for i, c := range f.calls {
		out[i] = strings.Join(c, " ")
	}
	return out
}

func (f *fakeSystem) sinks() []string {
	out := []string{"acerultrax_sink"}
	if f.sinkUp {
		if f.profile == hifiHP {
			out = append(out, testHP)
		} else {
			out = append(out, testHW)
		}
	}
	return append(out, f.extraSinks...)
}

func (f *fakeSystem) sources() []string {
	out := []string{testSrc1, testSrc2, testHW + ".monitor"}
	out = append(out, f.extraSources...)
	if f.confDir != "" {
		for _, d := range voiceDevices {
			if exists(filepath.Join(f.confDir, "truesense-pv-"+d.name+".conf")) {
				out = append(out, d.node())
			}
		}
	}
	sort.Strings(out)
	return out
}

func (f *fakeSystem) run(_ context.Context, name string, args ...string) Result {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls = append(f.calls, append([]string{name}, args...))
	ok := func(out string) Result { return Result{Stdout: out} }
	a := strings.Join(args, " ")
	switch name {
	case "pactl":
		switch {
		case a == "list short cards":
			if !f.cardPresent {
				return ok("")
			}
			return ok(fmt.Sprintf("70\talsa_card.usb-Other\talsa\n71\t%s\talsa\n", testCard))
		case a == "list cards":
			var profs strings.Builder
			for _, p := range f.offered {
				fmt.Fprintf(&profs, "\t\t%s: %s (sinks: 1, sources: 2, priority: 1, available: yes)\n", p, p)
			}
			return ok("Card #70\n\tName: alsa_card.usb-Other\n\tProfiles:\n\t\toff: Off (sinks: 0, sources: 0, priority: 0, available: yes)\n\tActive Profile: off\n\tPorts:\n\n" +
				"Card #71\n\tName: " + testCard + "\n\tDriver: alsa\n\tProfiles:\n" + profs.String() + "\tActive Profile: " + f.profile + "\n\tPorts:\n\t\tanalog-output-speaker: Speaker (type: Speaker)\n")
		case a == "list short sinks":
			var b strings.Builder
			for i, s := range f.sinks() {
				fmt.Fprintf(&b, "%d\t%s\tPipeWire\ts32le 2ch 48000Hz\tSUSPENDED\n", 30+i, s)
			}
			return ok(b.String())
		case a == "list sinks":
			var b strings.Builder
			for i, s := range f.sinks() {
				desc := f.sinkDescs[s]
				if desc == "" {
					desc = "Sink " + s
				}
				fmt.Fprintf(&b, "Sink #%d\n\tState: SUSPENDED\n\tName: %s\n\tDescription: %s\n\tDriver: PipeWire\n", 30+i, s, desc)
			}
			return ok(b.String())
		case a == "list short sources":
			var b strings.Builder
			for i, s := range f.sources() {
				fmt.Fprintf(&b, "%d\t%s\tPipeWire\ts32le 2ch 48000Hz\tSUSPENDED\n", 40+i, s)
			}
			return ok(b.String())
		case a == "list short sink-inputs":
			return ok(strings.Join(f.sinkInputs, "\n"))
		case a == "list short source-outputs":
			return ok(strings.Join(f.sourceOutputs, "\n"))
		case a == "get-default-sink":
			return ok(f.defaultSink + "\n")
		case a == "get-default-source":
			return ok(f.defaultSource + "\n")
		case len(args) == 2 && args[0] == "set-default-sink":
			f.defaultSink = args[1]
		case len(args) == 2 && args[0] == "set-default-source":
			f.defaultSource = args[1]
		case len(args) == 3 && args[0] == "set-card-profile":
			if !contains(f.offered, args[2]) {
				return Result{Stderr: "Failure: No such profile", Code: 1}
			}
			f.profile = args[2]
			f.sinkUp = args[2] == hifiSpk || args[2] == hifiHP
		case len(args) == 2 && args[0] == "get-source-mute":
			if f.muted[args[1]] {
				return ok("Mute: yes\n")
			}
			return ok("Mute: no\n")
		case len(args) == 3 && args[0] == "set-source-mute":
			f.muted[args[1]] = args[2] == "1"
		case len(args) == 2 && args[0] == "get-source-volume":
			return ok(f.volume)
		}
	case "wpctl":
		if len(args) == 3 && args[0] == "set-volume" && strings.HasSuffix(args[2], "%") {
			var pct int
			_, _ = fmt.Sscanf(args[2], "%d%%", &pct)
			f.volume = fmt.Sprintf("Volume: front-left: %d / %d%% / -3.00 dB,   front-right: %d / %d%% / -3.00 dB\n", pct*655, pct, pct*655, pct)
		}
	case "systemctl":
		switch a {
		case "--user restart wireplumber.service":
			f.restarted++
			if f.restartFixes {
				f.offered = []string{"off", hifiHP, hifiSpk, "pro-audio"}
				f.profile, f.sinkUp = hifiSpk, true
			}
		case "--user restart pipewire.service":
		}
	case "amixer":
		return f.amixer(args)
	}
	if strings.HasSuffix(name, "acerultrax-cli") {
		return f.acerultrax(args)
	}
	return ok("")
}

func (f *fakeSystem) amixer(args []string) Result {
	if len(args) > 0 && strings.HasPrefix(args[0], "-c") {
		args = args[1:]
	}
	if len(args) < 2 {
		return Result{Code: 1, Stderr: "amixer: bad usage"}
	}
	ctl := args[1]
	switch args[0] {
	case "cget":
		switch {
		case strings.Contains(ctl, "name=Headphone Jack"):
			return Result{Stdout: fmt.Sprintf("numid=33,iface=CARD,name='Headphone Jack'\n  ; type=BOOLEAN,access=r-------,values=1\n  : values=%s\n", f.headphoneJack)}
		case strings.Contains(ctl, "name=Headset Mic Jack"):
			return Result{Stdout: fmt.Sprintf("numid=34,iface=CARD,name='Headset Mic Jack'\n  ; type=BOOLEAN,access=r-------,values=1\n  : values=%s\n", f.headsetMicJack)}
		case strings.Contains(ctl, "name=Headset Mic Boost Volume"):
			if f.noBoostControl {
				return Result{Code: 1, Stderr: "amixer: Cannot find the given element"}
			}
			return Result{Stdout: fmt.Sprintf("numid=20,iface=MIXER,name='Headset Mic Boost Volume'\n  ; type=INTEGER,access=rw---R--,values=2,min=0,max=3,step=0\n  : values=%d,%d\n  | dBscale-min=0.00dB,step=10.00dB,mute=0\n", f.boost, f.boost)}
		}
	case "cset":
		if strings.Contains(ctl, "Headset Mic Boost Volume") && len(args) == 3 && !f.boostBroken {
			_, _ = fmt.Sscanf(args[2], "%d,", &f.boost)
		}
		return Result{Stdout: "numid=20\n"}
	}
	return Result{}
}

func contains(list []string, s string) bool {
	for _, x := range list {
		if x == s {
			return true
		}
	}
	return false
}

// realDevices is `acerultrax-cli devices` as the real helper prints it (after its output is stripped).
const realDevices = `  Laptop speakers                                    speakers           Music
    @speakers
  Headphones (3.5 mm jack)                           headphones         Music
    @headphones
  DualSense wireless controller (PS5) Direct DualSense Wireless Controller other              Plain
    alsa_output.usb-Sony_DualSense-00.Direct__Direct__sink
* Headset PC38X                                      other              Plain
    alsa_output.usb-GeneralPlus_USB_Audio_Device-00.analog-stereo
  Some Speaker                                       bluetooth-speaker  Plain
    bluez_output.AA.1
`

const (
	usbHeadset = "alsa_output.usb-GeneralPlus_USB_Audio_Device-00.analog-stereo"
	dualSense  = "alsa_output.usb-Sony_DualSense-00.Direct__Direct__sink"
)

func (f *fakeSystem) acerultrax(args []string) Result {
	if len(args) == 0 {
		return Result{Code: 2, Stderr: "usage"}
	}
	if msg := f.dtsFail[args[0]]; msg != "" {
		return Result{Stderr: msg, Code: 1}
	}
	switch args[0] {
	case "devices":
		text := f.dtsDevicesText
		if text == "" {
			text = realDevices
		}
		for ref, prof := range f.dtsProfile {
			text = strings.Replace(text, "Plain\n    "+ref, prof+"\n    "+ref, 1)
		}
		return Result{Stdout: strings.TrimSpace(text) + "\n"}
	case "mode":
		if i := indexOf(args, "--device"); i >= 0 && i+1 < len(args) {
			f.dtsProfile[args[i+1]] = strings.ToUpper(args[1][:1]) + args[1][1:]
		}
	case "headphone":
		if i := indexOf(args, "--device"); i >= 0 && i+1 < len(args) && args[1] == "default" {
			f.dtsEQ[args[i+1]] = true
		}
	}
	return Result{Stdout: f.dtsStdout[args[0]] + "ok\n"}
}

func indexOf(list []string, s string) int {
	for i, x := range list {
		if x == s {
			return i
		}
	}
	return -1
}

// fakeClock advances only when something sleeps, so timeouts and waits are deterministic.
type fakeClock struct {
	mu     sync.Mutex
	now    time.Time
	sleeps []time.Duration
	onTick func(d time.Duration) // optional hook, called after each sleep
}

func (c *fakeClock) Now() time.Time { c.mu.Lock(); defer c.mu.Unlock(); return c.now }
func (c *fakeClock) Sleep(ctx context.Context, d time.Duration) {
	c.mu.Lock()
	c.now = c.now.Add(d)
	c.sleeps = append(c.sleeps, d)
	hook := c.onTick
	c.mu.Unlock()
	if hook != nil {
		hook(d)
	}
}

type fakeOSD struct {
	mu    sync.Mutex
	shown []string
	depth int
	max   int
}

func (o *fakeOSD) Show(text, icon string) {
	o.mu.Lock()
	o.shown = append(o.shown, icon+": "+text)
	o.mu.Unlock()
}
func (o *fakeOSD) Quiet() func() {
	o.mu.Lock()
	o.depth++
	o.max = max(o.max, o.depth)
	o.mu.Unlock()
	return func() { o.mu.Lock(); o.depth--; o.mu.Unlock() }
}

type rig struct {
	t     *testing.T
	sys   *fakeSystem
	c     *Controller
	clk   *fakeClock
	osd   *fakeOSD
	cfg   *config.Config
	dir   string
	env   map[string]string
	locks int
}

func newRig(t *testing.T, broken bool) *rig {
	t.Helper()
	dir := t.TempDir()
	r := &rig{t: t, sys: newFakeSystem(broken), clk: &fakeClock{now: time.Unix(1_800_000_000, 0)}, osd: &fakeOSD{}, cfg: config.Default(), dir: dir, env: map[string]string{}}
	r.sys.confDir = filepath.Join(dir, "pw")
	if err := os.WriteFile(filepath.Join(dir, "cards"), []byte(" 0 [sofhdadsp     ]: sof-hda-dsp - sof-hda-dsp\n                      Intel Corp.-Predator PH16-71\n 1 [Other          ]: USB-Audio - Other\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	r.c = &Controller{
		Run: r.sys.run, Cfg: func() *config.Config { return r.cfg }, State: &userstate.Store{Path: filepath.Join(dir, "user.json")}, OSD: r.osd, Log: nil,
		Getenv: func(k string) string { return r.env[k] }, Sleep: r.clk.Sleep, Now: r.clk.Now,
		Lock:      func() (func(), error) { r.locks++; return func() { r.locks-- }, nil },
		ProcCards: filepath.Join(dir, "cards"), ConfDir: filepath.Join(dir, "pw"), Plugin: "/usr/lib/ladspa/librnnoise_ladspa.so",
		DTSBinary: func() string { return "/x/acerultrax-cli" }, DTSConfigPath: filepath.Join(dir, "acerultrax.toml"),
	}
	return r
}

func ctx() context.Context { return context.Background() }
