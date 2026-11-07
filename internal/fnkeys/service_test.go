package fnkeys

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/Order52/truesense/internal/config"
)

// ---- decoding and node discovery

func TestDecode(t *testing.T) {
	for _, tc := range []struct {
		report string
		want   Event
	}{
		{"04 80 ff", EventFnF10}, {"04 8a ff", EventFnEsc}, {"04 81 ff", EventPredatorDown}, {"04 00 00", EventRelease},
		{"04 80 ff 00 00 00 00 00", EventFnF10}, {"04 81 ff 00 00 00 00 00", EventPredatorDown},
		{"04 82 ff", EventUnknown}, {"04 8b ff", EventUnknown}, {"04 80 fe", EventUnknown}, {"04 00 01", EventUnknown},
		{"05 81 ff", EventNone}, {"04 81", EventNone}, {"04", EventNone}, {"", EventNone}, {"00 81 ff", EventNone},
	} {
		var b []byte
		for _, f := range strings.Fields(tc.report) {
			var v byte
			_, _ = fmt.Sscanf(f, "%x", &v)
			b = append(b, v)
		}
		if got := Decode(b); got != tc.want {
			t.Errorf("%q: %d, want %d", tc.report, got, tc.want)
		}
	}
}

func FuzzDecode(f *testing.F) {
	for _, s := range [][]byte{{4, 0x81, 0xFF}, {4, 0, 0}, {}, {4, 0x8A, 0xFF, 1, 2, 3, 4, 5}, {9, 9, 9, 9}} {
		f.Add(s)
	}
	f.Fuzz(func(t *testing.T, r []byte) {
		e := Decode(r)
		if (len(r) < 3 || r[0] != 4) != (e == EventNone) {
			t.Fatalf("%x -> %d", r, e)
		}
	})
}

// fakeSys builds /sys/class/hidraw/<name>/device/{uevent,report_descriptor}.
func fakeSys(t *testing.T, nodes map[string][2]string) string {
	t.Helper()
	root := t.TempDir()
	for name, v := range nodes {
		d := filepath.Join(root, "sys/class/hidraw", name, "device")
		if err := os.MkdirAll(d, 0o755); err != nil {
			t.Fatal(err)
		}
		if v[0] != "" {
			_ = os.WriteFile(filepath.Join(d, "uevent"), []byte(v[0]), 0o644)
		}
		if v[1] != "" {
			_ = os.WriteFile(filepath.Join(d, "report_descriptor"), []byte(v[1]), 0o644)
		}
	}
	return root
}

func TestFindNode(t *testing.T) {
	const kbd = "DRIVER=hid-generic\nHID_ID=0003:000004f2:00000117\nHID_NAME=Chicony\n"
	vendorDesc := "\x05\x01\x09\x06\x06\x88\x00\x09\x01\xa1\x01"
	shortDesc := "\x05\x88\x09\x01"
	for _, tc := range []struct {
		name  string
		nodes map[string][2]string
		want  string
		err   error
	}{
		{"the vendor interface (long form usage page)", map[string][2]string{"hidraw3": {kbd, vendorDesc}}, "/dev/hidraw3", nil},
		{"the short form of usage page 0x88", map[string][2]string{"hidraw1": {kbd, shortDesc}}, "/dev/hidraw1", nil},
		{"other interfaces of the same keyboard are skipped", map[string][2]string{"hidraw0": {kbd, "\x05\x01\x09\x06"}, "hidraw2": {kbd, vendorDesc}}, "/dev/hidraw2", nil},
		{"another vendor is skipped", map[string][2]string{"hidraw0": {"HID_ID=0003:0000046D:0000C52B\n", vendorDesc}, "hidraw1": {kbd, vendorDesc}}, "/dev/hidraw1", nil},
		{"string order, as 0.3.x: hidraw10 before hidraw2", map[string][2]string{"hidraw2": {kbd, vendorDesc}, "hidraw10": {kbd, vendorDesc}}, "/dev/hidraw10", nil},
		{"no descriptor readable", map[string][2]string{"hidraw0": {kbd, ""}}, "", ErrNoNode},
		{"no uevent readable", map[string][2]string{"hidraw0": {"", vendorDesc}}, "", ErrNoNode},
		{"no keyboard", map[string][2]string{"hidraw0": {"HID_ID=0003:0000046D:0000C52B\n", vendorDesc}}, "", ErrNoNode},
		{"no hidraw at all", map[string][2]string{}, "", ErrNoNode},
	} {
		t.Run(tc.name, func(t *testing.T) {
			root := fakeSys(t, tc.nodes)
			got, err := FindNode(root, "")
			if !errors.Is(err, tc.err) || got != tc.want {
				t.Errorf("%q, %v; want %q, %v", got, err, tc.want, tc.err)
			}
			if tc.err == nil {
				if got2, _ := FindNode(root, "/fake/dev"); got2 != filepath.Join("/fake/dev", filepath.Base(tc.want)) {
					t.Errorf("devRoot ignored: %q", got2)
				}
			}
		})
	}
	if _, err := FindNode(t.TempDir()+"/missing", ""); !errors.Is(err, ErrNoNode) {
		t.Errorf("missing sys: %v", err)
	}
}

// ---- the service, driven synchronously

type recorder struct {
	mu    sync.Mutex
	calls []string
	reply map[string]struct {
		msg string
		err error
	}
	block chan struct{} // when set, Run blocks on it (until ctx ends)
	panic string
}

func (r *recorder) Run(ctx context.Context, spec string) (string, error) {
	r.mu.Lock()
	r.calls = append(r.calls, spec)
	rep := r.reply[spec]
	blk := r.block
	pn := r.panic
	r.mu.Unlock()
	if pn == spec {
		panic("kaboom")
	}
	if blk != nil {
		select {
		case <-blk:
		case <-ctx.Done():
			return "", ctx.Err()
		}
	}
	return rep.msg, rep.err
}

func (r *recorder) got() []string {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]string(nil), r.calls...)
}

type osdLog struct {
	mu   sync.Mutex
	logs []string
}

func (o *osdLog) Show(text, icon string) bool {
	o.mu.Lock()
	defer o.mu.Unlock()
	o.logs = append(o.logs, icon+": "+text)
	return true
}
func (o *osdLog) got() []string {
	o.mu.Lock()
	defer o.mu.Unlock()
	return append([]string(nil), o.logs...)
}

func cfgWith(f func(k *config.KeysCfg)) func() (*config.Config, error) {
	c := config.Default()
	c.Keys = config.KeysCfg{Profile: "p", Tap: "power-next", Double: "output-toggle", Triple: "none", Long: "fan-max-toggle", FnEsc: "mic-toggle", LongMS: 800, MultiMS: 450}
	if f != nil {
		f(&c.Keys)
	}
	return func() (*config.Config, error) { return c, nil }
}

// syncService builds a service whose actions run inline, so a test can assert right after an event.
func syncService(t *testing.T, rec *recorder, o *osdLog, cfg func() (*config.Config, error)) *service {
	t.Helper()
	env := Env{Config: cfg, Actions: rec, OSD: o, ConfigPath: filepath.Join(t.TempDir(), "config.toml")}.withDefaults()
	c, err := cfg()
	if err != nil {
		t.Fatal(err)
	}
	s := newService(context.Background(), env, c)
	s.spawn = func(f func()) { f() }
	return s
}

var (
	down    = []byte{4, 0x81, 0xFF}
	release = []byte{4, 0, 0}
	fnEsc   = []byte{4, 0x8A, 0xFF}
	fnF10   = []byte{4, 0x80, 0xFF}
)

func TestServiceMapsReportsToConfiguredActions(t *testing.T) {
	rec := &recorder{reply: map[string]struct {
		msg string
		err error
	}{"power-next": {msg: "Power mode: turbo"}, "mic-toggle": {msg: "Microphones: MUTED"}}}
	o := &osdLog{}
	s := syncService(t, rec, o, cfgWith(nil))
	now := t0
	step := func(r []byte, after time.Duration) { now = now.Add(after); s.handleReport(r, now) }

	step(down, 0)
	step(release, 30*time.Millisecond)
	if len(rec.got()) != 0 {
		t.Fatal("a tap is not final until the window closes")
	}
	s.advance(now.Add(450 * time.Millisecond))
	step(fnEsc, time.Second)
	step(release, 10*time.Millisecond) // the release that ends Fn+Esc is not a tap
	s.advance(now.Add(time.Second))
	step(fnF10, time.Second)
	step([]byte{4, 0x82, 0xFF}, time.Second) // Fn+F11: ignored
	step([]byte{1, 2, 3}, time.Second)       // not a vendor report
	if got, want := rec.got(), []string{"power-next", "mic-toggle", "touchpad-toggle"}; !reflect.DeepEqual(got, want) {
		t.Errorf("actions %v, want %v", got, want)
	}
	if got, want := o.got(), []string{"power: Power mode: turbo", "power: Microphones: MUTED"}; !reflect.DeepEqual(got, want) {
		t.Errorf("osd %v, want %v (an action with no message shows nothing)", got, want)
	}
}

func TestEveryGestureRunsItsOwnBinding(t *testing.T) {
	rec := &recorder{}
	s := syncService(t, rec, nil, cfgWith(func(k *config.KeysCfg) {
		k.Tap, k.Double, k.Triple, k.Long = "none", "notify:d", "notify:t", "notify:l"
	}))
	at := func(ms int) time.Time { return t0.Add(time.Duration(ms) * time.Millisecond) }
	// double
	s.handleReport(down, at(0))
	s.handleReport(release, at(10))
	s.handleReport(down, at(100))
	s.handleReport(release, at(110))
	s.advance(at(600))
	// triple
	for i, base := range []int{1000, 1100, 1200} {
		s.handleReport(down, at(base))
		s.handleReport(release, at(base+10))
		_ = i
	}
	// long
	s.handleReport(down, at(3000))
	s.advance(at(3800))
	s.handleReport(release, at(4000))
	s.advance(at(9000))
	if got, want := rec.got(), []string{"notify:d", "notify:t", "notify:l"}; !reflect.DeepEqual(got, want) {
		t.Errorf("%v, want %v", got, want)
	}
}

func TestFailedActionIsShownAndDoesNotStopTheService(t *testing.T) {
	rec := &recorder{reply: map[string]struct {
		msg string
		err error
	}{"power-next": {err: errors.New("no permission")}}}
	o := &osdLog{}
	s := syncService(t, rec, o, cfgWith(nil))
	s.handleReport(down, t0)
	s.handleReport(release, t0.Add(10*time.Millisecond))
	s.advance(t0.Add(time.Second))
	s.handleReport(fnEsc, t0.Add(2*time.Second)) // the service goes on
	if got, want := o.got(), []string{"power: TrueSense: no permission"}; !reflect.DeepEqual(got, want) {
		t.Errorf("osd %v", got)
	}
	if len(rec.got()) != 2 {
		t.Errorf("calls %v", rec.got())
	}
}

func TestInvalidBindingsBecomeNoneAtStart(t *testing.T) {
	rec := &recorder{}
	s := syncService(t, rec, nil, cfgWith(func(k *config.KeysCfg) { k.Tap = "frobnicate"; k.FnEsc = "power" }))
	s.handleReport(down, t0)
	s.handleReport(release, t0.Add(10*time.Millisecond))
	s.advance(t0.Add(time.Second))
	s.handleReport(fnEsc, t0.Add(2*time.Second))
	if got, want := rec.got(), []string{"none", "none"}; !reflect.DeepEqual(got, want) {
		t.Errorf("%v: a typo in the config must never run something else", got)
	}
}

func TestNonsenseTimingsFallBackToTheDefaults(t *testing.T) {
	s := syncService(t, &recorder{}, nil, cfgWith(func(k *config.KeysCfg) { k.LongMS, k.MultiMS = 0, -5 }))
	if s.bindings.LongMS != 800 || s.bindings.MultiMS != 450 {
		t.Errorf("%+v", s.bindings)
	}
}

func TestReloadOnConfigChange(t *testing.T) {
	cur := config.Default()
	cur.Keys.Tap, cur.Keys.LongMS, cur.Keys.MultiMS = "power-next", 800, 450
	var mu sync.Mutex
	var loadErr error
	cfg := func() (*config.Config, error) {
		mu.Lock()
		defer mu.Unlock()
		if loadErr != nil {
			return nil, loadErr
		}
		c := *cur
		return &c, nil
	}
	rec := &recorder{}
	path := filepath.Join(t.TempDir(), "config.toml")
	env := Env{Config: cfg, Actions: rec, ConfigPath: path}.withDefaults()
	s := newService(context.Background(), env, cur)
	s.spawn = func(f func()) { f() }

	touch := func(mtime time.Time) {
		if err := os.WriteFile(path, []byte("x"), 0o644); err != nil {
			t.Fatal(err)
		}
		if err := os.Chtimes(path, mtime, mtime); err != nil {
			t.Fatal(err)
		}
	}
	tap := func(base time.Time) {
		s.handleReport(down, base)
		s.handleReport(release, base.Add(10*time.Millisecond))
		s.advance(base.Add(5 * time.Second))
	}

	s.reload() // no change: nothing happens
	tap(t0)
	touch(time.Unix(1000, 0))
	mu.Lock()
	cur.Keys.Tap, cur.Keys.Double, cur.Keys.LongMS, cur.Keys.MultiMS = "power:turbo", "bogus", 500, 300
	mu.Unlock()
	s.reload()
	tap(t0.Add(10 * time.Second))
	if got, want := rec.got(), []string{"power-next", "power:turbo"}; !reflect.DeepEqual(got, want) {
		t.Errorf("%v, want %v", got, want)
	}
	if s.bindings.Double != "output-toggle" {
		t.Errorf("an invalid new binding keeps the old action, got %q", s.bindings.Double)
	}
	if s.bindings.LongMS != 500 || s.bindings.MultiMS != 300 {
		t.Errorf("timing not reloaded: %+v", s.bindings)
	}
	s.handleReport(down, t0.Add(20*time.Second))
	if d, _ := s.m.Deadline(); !d.Equal(t0.Add(20*time.Second + 500*time.Millisecond)) {
		t.Errorf("the machine did not take the new timing: %v", d)
	}

	// a file that cannot be read keeps everything
	touch(time.Unix(2000, 0))
	mu.Lock()
	loadErr = errors.New("syntax error")
	mu.Unlock()
	s.reload()
	if s.bindings.Tap != "power:turbo" {
		t.Errorf("%+v", s.bindings)
	}
	// the same mtime is not looked at again
	mu.Lock()
	loadErr = nil
	cur.Keys.Tap = "power:eco"
	mu.Unlock()
	s.reload()
	if s.bindings.Tap != "power:turbo" {
		t.Error("an unchanged file must not be re-read")
	}
	// the file disappearing is a change too (back to the system/default config)
	if err := os.Remove(path); err != nil {
		t.Fatal(err)
	}
	s.reload()
	if s.bindings.Tap != "power:eco" {
		t.Errorf("%+v", s.bindings)
	}
}

// ---- Run, with a fake hidraw and the real clock (smallest legal timings, so about a second in all)

type fakeReports struct {
	in     chan []byte
	failed chan error
	closed chan struct{}
	once   sync.Once
}

func newFakeReports() *fakeReports {
	return &fakeReports{in: make(chan []byte, 64), failed: make(chan error, 1), closed: make(chan struct{})}
}

func (f *fakeReports) Read() ([]byte, error) {
	select {
	case r := <-f.in:
		return r, nil
	case err := <-f.failed:
		return nil, err
	case <-f.closed:
		return nil, io.ErrClosedPipe
	}
}
func (f *fakeReports) Close() error { f.once.Do(func() { close(f.closed) }); return nil }

func runEnv(t *testing.T, rec ActionRunner, o OSD, f func(k *config.KeysCfg)) (Env, *fakeReports) {
	t.Helper()
	sys := fakeSys(t, map[string][2]string{"hidraw4": {"HID_ID=0003:000004F2:00000117\n", "\x06\x88\x00"}})
	rep := newFakeReports()
	cfg := cfgWith(func(k *config.KeysCfg) {
		k.LongMS, k.MultiMS = 300, 200
		if f != nil {
			f(k)
		}
	})
	return Env{SysRoot: sys, DevRoot: "/dev", Config: cfg, Actions: rec, OSD: o, ConfigPath: filepath.Join(t.TempDir(), "c.toml"), PollEvery: 20 * time.Millisecond,
		Open: func(p string) (Reports, error) {
			if p != "/dev/hidraw4" {
				return nil, fmt.Errorf("unexpected node %s", p)
			}
			return rep, nil
		}}, rep
}

func startRun(t *testing.T, env Env) (stop func() error) {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	errc := make(chan error, 1)
	go func() { errc <- Run(ctx, env) }()
	return func() error {
		cancel()
		select {
		case err := <-errc:
			return err
		case <-time.After(5 * time.Second):
			t.Fatal("Run did not stop")
			return nil
		}
	}
}

func waitFor(t *testing.T, what string, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for !cond() {
		if time.Now().After(deadline) {
			t.Fatalf("timed out waiting for %s", what)
		}
		time.Sleep(5 * time.Millisecond)
	}
}

func TestRunTapLongAndFnKeysEndToEnd(t *testing.T) {
	rec := &recorder{reply: map[string]struct {
		msg string
		err error
	}{"power-next": {msg: "Power mode: eco"}}}
	o := &osdLog{}
	env, rep := runEnv(t, rec, o, nil)
	stop := startRun(t, env)

	rep.in <- down
	rep.in <- release
	waitFor(t, "the tap", func() bool { return len(rec.got()) == 1 })
	rep.in <- fnEsc
	rep.in <- release
	waitFor(t, "fn+esc", func() bool { return len(rec.got()) == 2 })
	rep.in <- down // held: long fires at 300 ms while still down
	waitFor(t, "the long press", func() bool { return len(rec.got()) == 3 })
	rep.in <- release
	rep.in <- fnF10
	waitFor(t, "fn+f10", func() bool { return len(rec.got()) == 4 })
	time.Sleep(250 * time.Millisecond) // a release after a long press must not become a tap
	if err := stop(); err != nil {
		t.Fatal(err)
	}
	if got, want := rec.got(), []string{"power-next", "mic-toggle", "fan-max-toggle", "touchpad-toggle"}; !reflect.DeepEqual(got, want) {
		t.Errorf("%v, want %v", got, want)
	}
	if got := o.got(); len(got) != 1 || got[0] != "power: Power mode: eco" {
		t.Errorf("osd %v", got)
	}
}

func TestRunNeverBlocksOnASlowActionAndTimesItOut(t *testing.T) {
	rec := &recorder{block: make(chan struct{})}
	o := &osdLog{}
	env, rep := runEnv(t, rec, o, func(k *config.KeysCfg) { k.Tap, k.FnEsc = "power-next", "mic-toggle" })
	env.ActionTimeout = 150 * time.Millisecond
	stop := startRun(t, env)
	rep.in <- fnEsc // hangs ...
	rep.in <- fnF10 // ... yet the next key is served at once
	waitFor(t, "both actions started", func() bool { return len(rec.got()) == 2 })
	waitFor(t, "the timeout to be reported", func() bool { return len(o.got()) == 2 })
	if err := stop(); err != nil {
		t.Fatal(err)
	}
	for _, l := range o.got() {
		if !strings.Contains(l, "timed out") {
			t.Errorf("osd %q", l)
		}
	}
}

func TestRunSurvivesAPanickingAction(t *testing.T) {
	rec := &recorder{panic: "mic-toggle"}
	o := &osdLog{}
	env, rep := runEnv(t, rec, o, nil)
	stop := startRun(t, env)
	rep.in <- fnEsc
	waitFor(t, "the panic to be reported", func() bool { return len(o.got()) == 1 })
	rep.in <- fnF10
	waitFor(t, "the next key", func() bool { return len(rec.got()) == 2 })
	if err := stop(); err != nil {
		t.Fatal(err)
	}
	if got := o.got()[0]; !strings.Contains(got, "action panicked: kaboom") {
		t.Errorf("%q", got)
	}
}

func TestRunHotReloadsTheBindings(t *testing.T) {
	rec := &recorder{}
	env, rep := runEnv(t, rec, nil, nil)
	var mu sync.Mutex
	fnEscAction := "mic-toggle"
	env.Config = cfgWithDyn(func(k *config.KeysCfg) {
		mu.Lock()
		defer mu.Unlock()
		k.LongMS, k.MultiMS, k.FnEsc = 300, 200, fnEscAction
	})
	stop := startRun(t, env)
	rep.in <- fnEsc
	waitFor(t, "first binding", func() bool { return len(rec.got()) == 1 })
	mu.Lock()
	fnEscAction = "touchpad:off"
	mu.Unlock()
	if err := os.WriteFile(env.ConfigPath, []byte("[keys]\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	waitFor(t, "the reload", func() bool {
		rep.in <- fnEsc
		time.Sleep(30 * time.Millisecond)
		g := rec.got()
		return g[len(g)-1] == "touchpad:off"
	})
	if err := stop(); err != nil {
		t.Fatal(err)
	}
}

func cfgWithDyn(f func(k *config.KeysCfg)) func() (*config.Config, error) {
	return func() (*config.Config, error) {
		c := config.Default()
		f(&c.Keys)
		return c, nil
	}
}

func TestRunStartupFailures(t *testing.T) {
	env := Env{SysRoot: t.TempDir(), Config: cfgWith(nil), Actions: &recorder{}}
	if err := Run(context.Background(), env); !errors.Is(err, ErrNoNode) {
		t.Errorf("no keyboard: %v", err)
	}
	env, _ = runEnv(t, &recorder{}, nil, nil)
	env.Config = func() (*config.Config, error) { return nil, errors.New("bad toml") }
	if err := Run(context.Background(), env); err == nil || !strings.Contains(err.Error(), "bad toml") {
		t.Errorf("bad config: %v", err)
	}
	env, _ = runEnv(t, &recorder{}, nil, nil)
	env.Open = func(string) (Reports, error) { return nil, os.ErrPermission }
	if err := Run(context.Background(), env); !errors.Is(err, os.ErrPermission) {
		t.Errorf("open: %v", err)
	}
}

func TestRunReturnsWhenTheNodeFails(t *testing.T) {
	env, rep := runEnv(t, &recorder{}, nil, nil)
	stop := startRun(t, env)
	rep.failed <- errors.New("device gone")
	time.Sleep(50 * time.Millisecond)
	err := stop()
	if err == nil || !strings.Contains(err.Error(), "device gone") {
		t.Errorf("a failed node must end the service so it is restarted: %v", err)
	}
}

func TestRunStartsAndStopsBackgroundLoops(t *testing.T) {
	env, _ := runEnv(t, &recorder{}, nil, nil)
	started, stopped := make(chan struct{}), make(chan struct{})
	env.Background = []func(context.Context){func(ctx context.Context) {
		close(started)
		<-ctx.Done()
		close(stopped)
	}}
	stop := startRun(t, env)
	<-started
	if err := stop(); err != nil {
		t.Fatal(err)
	}
	select {
	case <-stopped:
	default:
		t.Error("Run returned before its background loop ended")
	}
}

func TestRealClock(t *testing.T) {
	c := RealClock{}
	ch, stop := c.After(10 * time.Millisecond)
	defer stop()
	select {
	case <-ch:
	case <-time.After(2 * time.Second):
		t.Fatal("timer never fired")
	}
	if time.Since(c.Now()) > time.Second {
		t.Error("Now")
	}
}
