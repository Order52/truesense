package keyboard

import (
	"encoding/json"
	"reflect"
	"sync"
	"testing"

	"github.com/Order52/truesense/internal/keymap"
	"github.com/Order52/truesense/internal/userstate"
)

func newSimTest(t *testing.T) (*Keyboard, *SimTransport) {
	t.Helper()
	return NewSim(&userstate.Store{Path: t.TempDir() + "/user.json"})
}

// The simulator is what the command line, the TUI and the window talk to under TRUESENSE_SIM.
func TestSimulatedKeyboardTracksBrightnessLikeTheHardware(t *testing.T) {
	k, sim := newSimTest(t)
	if b, err := k.Brightness(); err != nil || b != 25 {
		t.Fatalf("factory brightness %d, %v", b, err)
	}
	must(t, k.SetEffect("wave", "00aec7", 5, 31, 0))
	if b, _ := k.Brightness(); b != 31 || sim.Brightness() != 31 {
		t.Errorf("effect brightness %d", b)
	}
	must(t, k.SetBrightness(9))
	if b, _ := k.Brightness(); b != 9 {
		t.Errorf("SetBrightness: %d", b)
	}
	must(t, k.PerKey(nil, 44))
	if b, _ := k.Brightness(); b != 44 {
		t.Errorf("PerKey brightness: %d", b)
	}
	must(t, k.SetOperatingMode("112233", 7))
	if b, _ := k.Brightness(); b != 7 {
		t.Errorf("operating brightness: %d", b)
	}
	if len(sim.Packets()) == 0 {
		t.Error("packets are recorded")
	}
}

func TestSimulatedPerKeyUpload(t *testing.T) {
	k, sim := newSimTest(t)
	cells := keymap.Rainbow()
	must(t, k.PerKey(cells, 20))
	if sim.Frames() != 8 {
		t.Errorf("frames = %d, want 8 output reports per upload", sim.Frames())
	}
	if got := sim.LastCells(); !reflect.DeepEqual(got, cells) {
		t.Error("the simulated keyboard holds the uploaded colours")
	}
	must(t, k.PerKey(nil, 20))
	if sim.Frames() != 16 {
		t.Errorf("frames = %d", sim.Frames())
	}
	if (&SimTransport{}).LastCells() != nil {
		t.Error("no upload yet")
	}
}

func TestSimulatedTimeoutFlagStartsOn(t *testing.T) {
	k, sim := newSimTest(t)
	if got, err := k.HIDTimeoutGet(); err != nil || got == nil || !*got {
		t.Fatalf("factory state is on: %v %v", got, err)
	}
	must(t, k.HIDTimeoutSet(false))
	if sim.Timeout() {
		t.Error("flag still on")
	}
	if got, _ := k.HIDTimeoutGet(); got == nil || *got {
		t.Errorf("read back %v", got)
	}
	must(t, k.HIDTimeoutSet(true))
	if got, _ := k.HIDTimeoutGet(); got == nil || !*got {
		t.Errorf("read back %v", got)
	}
}

func TestFromEnvPicksTheSimulatorAndSharesIt(t *testing.T) {
	t.Setenv("TRUESENSE_SIM", "1")
	st := &userstate.Store{Path: t.TempDir() + "/user.json"}
	a, b := FromEnv(st), FromEnv(st)
	must(t, a.SetBrightness(13))
	if got, err := b.Brightness(); err != nil || got != 13 {
		t.Errorf("a second controller must see the same simulated keyboard: %d, %v", got, err)
	}
	must(t, a.SetBrightness(25)) // leave the shared simulator as found
}

func TestFromEnvWithoutSimTouchesOnlyTheRealPathLazily(t *testing.T) {
	t.Setenv("TRUESENSE_SIM", "")
	k := FromEnv(nil)
	if k == nil || k.t != nil {
		t.Fatal("the real device is opened on first use, not at construction")
	}
}

func TestSimulatorIsSafeForConcurrentUse(t *testing.T) {
	k, _ := newSimTest(t)
	var wg sync.WaitGroup
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			_ = k.SetBrightness(i)
			_, _ = k.Brightness()
			_ = k.PerKeyFrame(nil, i)
			_, _ = k.HIDTimeoutGet()
		}(i)
	}
	wg.Wait()
}

func TestNilStateStoreRemembersNothing(t *testing.T) {
	k := New(newFakeDevice().opener(), nil)
	must(t, k.SetEffect("static", "ffffff", 5, 30, 0))
	if !k.Last().IsZero() {
		t.Error("no store, no memory")
	}
	if len(k.CurrentCells()) != 128 {
		t.Error("CurrentCells without a store")
	}
}

func FuzzSetEffectNeverPanics(f *testing.F) {
	f.Add("static", "ff00ff", 5, 30, 0)
	f.Add("operatingmode", "#00aec7", 1, 0, 1)
	f.Add("", "", -1, 99, 300)
	f.Fuzz(func(t *testing.T, name, color string, speed, bri, dir int) {
		k := New(newFakeDevice().opener(), nil)
		_ = k.SetEffect(name, color, speed, bri, dir)
	})
}

// A remembered look is read from a file the user (or an old version) could have damaged:
// replaying whatever decodes must work or fail cleanly.
func FuzzReplayRememberedState(f *testing.F) {
	f.Add([]byte(`{"mode":"effect","name":"wave","color":"00ff00","speed":4,"brightness":20,"direction":0}`))
	f.Add([]byte(`{"mode":"perkey","brightness":12,"cells":["00aec7","zz"]}`))
	f.Add([]byte(`{"mode":"operating","color":"#12","brightness":300}`))
	f.Add([]byte(`{"mode":"off"}`))
	f.Fuzz(func(t *testing.T, b []byte) {
		var s State
		if json.Unmarshal(b, &s) != nil {
			return
		}
		k, _ := NewSim(nil)
		_, _ = k.Replay(s)
	})
}
