package tui

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"
)

// fakeBackend simulates a laptop the way the Python firmware simulator does (same initial
// state, same refusals), records every call and can be told to fail an operation.
type fakeBackend struct {
	mu sync.Mutex

	service bool
	delay   time.Duration // latency of every write, to exercise ordering and coalescing

	mode      string
	hold      time.Duration
	auto      string
	smartProf string
	curve     string
	fanMode   string
	smartFan  bool
	fanOff    bool
	duty      [2]int
	temps     Sensors
	ac        bool

	limit, calib, od, boot, kbt, full bool
	usb                               int
	gpu                               string

	kb      KeyboardLook
	kbKnown bool
	kbNow   int
	rear    RearLook
	zones   bool
	saved   []string

	sound   SoundCard
	repair  RepairResult
	output  string
	spk     bool
	dts     bool
	devices []DTSDevice
	mic     Microphone
	voice   VoiceModes

	profiles, looks []string
	doctor          []DoctorResult
	module          string

	failures map[string]error
	calls    []string
	stops    []fanStop
	window   int
}

// fanStop records a write that left a fan stopped that was running before.
type fanStop struct {
	req     FanRequest
	service bool
}

func newFake() *fakeBackend {
	return &fakeBackend{
		mode: "eco", auto: "native", smartProf: "balanced", curve: "native", fanMode: FirmwareFanAuto, duty: [2]int{30, 30},
		temps: Sensors{CPUTemp: 50, SysTemp: 52, GPUTemp: 48}, ac: true,
		usb: 30, gpu: "optimus", od: true, kbt: true,
		kbNow: 25, rear: RearLook{Mode: "static", Color: "00aec7", Speed: 3, Brightness: 100, Direction: 1},
		sound: SoundCard{Status: SoundOK}, output: "speakers",
		devices: []DTSDevice{{Label: "Speakers", Profile: "Music", Current: true}},
		mic:     Microphone{Volume: Some(100)}, voice: VoiceModes{Headset: "off", Array: "off"},
		profiles: []string{"balanced", "battery-saver", "gaming", "quiet"}, looks: []string{"native", "ocean"},
		doctor: []DoctorResult{}, module: "sim",
		failures: map[string]error{},
	}
}

// failOn makes the named operation return err from now on.
func (f *fakeBackend) failOn(op string, err error) {
	f.mu.Lock()
	f.failures[op] = err
	f.mu.Unlock()
}

// begin records a call and returns the injected failure, if any.
func (f *fakeBackend) begin(op string) error {
	f.calls = append(f.calls, op)
	name, _, _ := strings.Cut(op, " ")
	return f.failures[name]
}

func (f *fakeBackend) count(op string) int {
	f.mu.Lock()
	defer f.mu.Unlock()
	n := 0
	for _, c := range f.calls {
		if c == op || strings.HasPrefix(c, op+" ") {
			n++
		}
	}
	return n
}

func (f *fakeBackend) lag() {
	if f.delay > 0 {
		time.Sleep(f.delay)
	}
}

// write wraps a state change: latency first (outside the lock), then the change.
func (f *fakeBackend) write(op string, do func() error) error {
	f.lag()
	f.mu.Lock()
	defer f.mu.Unlock()
	if err := f.begin(op); err != nil {
		return err
	}
	return do()
}

// ---- LiveBackend

func (f *fakeBackend) ServiceState(context.Context) (*ServiceState, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if !f.service {
		return nil, nil
	}
	s := f.temps
	return &ServiceState{Sensors: &s, PowerMode: f.mode, AC: Some(f.ac), AutoMode: f.auto, SmartProfile: f.smartProf, FanOff: f.fanOff,
		SmartFan: f.smartFan, FanCurve: f.curve, CPULoad: Some(12), GPULoad: Some(3), GPUWatts: Some(9), Activity: "idle", ModeReason: "quiet desktop", HoldLeft: f.hold}, nil
}

func (f *fakeBackend) Sensors(context.Context) (Sensors, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if err := f.failures["Sensors"]; err != nil {
		return Sensors{}, err
	}
	return f.temps, nil
}

func (f *fakeBackend) FanState(context.Context) (FanState, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return FanState{Mode: f.fanMode, CPUDuty: f.duty[0], GPUDuty: f.duty[1]}, nil
}

func (f *fakeBackend) PowerMode(context.Context) (string, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.mode, nil
}

func (f *fakeBackend) OnAC(context.Context) (bool, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.ac, nil
}

func (f *fakeBackend) Battery() (Battery, bool) {
	return Battery{Percent: 80, Status: "Charging"}, true
}

// ---- PowerBackend

func (f *fakeBackend) SetPowerMode(_ context.Context, mode string, hold time.Duration) error {
	return f.write("SetPowerMode "+mode, func() error {
		f.mode, f.hold = mode, hold
		return nil
	})
}

func (f *fakeBackend) SetAutoMode(_ context.Context, mode string) error {
	return f.write("SetAutoMode", func() error {
		if !f.service {
			return errors.New("needs the TrueSense service")
		}
		f.auto = mode
		return nil
	})
}

func (f *fakeBackend) ReleaseHold(context.Context) error {
	return f.write("ReleaseHold", func() error { f.hold = 0; return nil })
}

func (f *fakeBackend) SmartProfileNames() []string { return []string{"native", "balanced", "steady"} }

func (f *fakeBackend) SmartProfile(context.Context) (string, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.smartProf, f.begin("read:SmartProfile")
}

func (f *fakeBackend) UseSmartProfile(_ context.Context, name string) error {
	return f.write("UseSmartProfile", func() error { f.smartProf = name; return nil })
}

// ---- FanBackend

func (f *fakeBackend) SetFan(_ context.Context, req FanRequest) error {
	return f.write(fmt.Sprintf("SetFan %s %d %d confirm=%v", req.Mode, req.CPU, req.GPU, req.Confirm), func() error {
		wasStopped := f.duty[0] == 0 || f.duty[1] == 0
		switch req.Mode {
		case "off":
			if !f.service {
				return errors.New("turning the fans off needs the TrueSense service (it brings them back when it gets warm)")
			}
			if !req.Confirm {
				return errors.New("stopping the fans needs a confirmation")
			}
			f.fanOff, f.duty = true, [2]int{}
			f.fanMode = FirmwareFanManual
		case "manual":
			for _, v := range []int{req.CPU, req.GPU} {
				if v != 0 && (v < 20 || v > 100) {
					return errors.New("manual fan duty is 20..100 % per fan, or 0 to stop that fan")
				}
			}
			if req.CPU == 0 || req.GPU == 0 {
				if !f.service {
					return errors.New("stopping a fan needs the TrueSense service (it brings the fans back when it gets warm)")
				}
				if !req.Confirm {
					return errors.New("stopping a fan needs a confirmation")
				}
			}
			f.fanMode, f.fanOff, f.smartFan, f.duty = FirmwareFanManual, false, false, [2]int{req.CPU, req.GPU}
		case "smart":
			f.fanMode, f.fanOff, f.smartFan = FirmwareFanManual, false, true
		case "auto":
			f.fanMode, f.fanOff, f.smartFan = FirmwareFanAuto, false, false
		case "max":
			f.fanMode, f.fanOff, f.smartFan, f.duty = FirmwareFanMax, false, false, [2]int{100, 100}
		default:
			return fmt.Errorf("unknown fan mode %q", req.Mode)
		}
		if !wasStopped && (f.duty[0] == 0 || f.duty[1] == 0) {
			f.stops = append(f.stops, fanStop{req, f.service})
		}
		return nil
	})
}

func (f *fakeBackend) FanCurveNames() []string { return []string{"native", "truesense", "quiet"} }

func (f *fakeBackend) FanCurve(context.Context) (string, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.curve, f.begin("read:FanCurve")
}

func (f *fakeBackend) UseFanCurve(_ context.Context, name string) error {
	return f.write("UseFanCurve", func() error { f.curve = name; return nil })
}

// ---- BatteryBackend, DisplayBackend

func (f *fakeBackend) boolRead(op string, p *bool) (bool, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return *p, f.begin("read:" + op)
}

func (f *fakeBackend) boolWrite(op string, p *bool, on bool) error {
	return f.write(op, func() error { *p = on; return nil })
}

func (f *fakeBackend) BatteryLimit(context.Context) (bool, error) {
	return f.boolRead("BatteryLimit", &f.limit)
}
func (f *fakeBackend) SetBatteryLimit(_ context.Context, on bool) error {
	return f.boolWrite("SetBatteryLimit", &f.limit, on)
}
func (f *fakeBackend) Calibration(context.Context) (bool, error) {
	return f.boolRead("Calibration", &f.calib)
}
func (f *fakeBackend) SetCalibration(_ context.Context, on bool) error {
	return f.boolWrite("SetCalibration", &f.calib, on)
}
func (f *fakeBackend) Overdrive(context.Context) (bool, error) { return f.boolRead("Overdrive", &f.od) }
func (f *fakeBackend) SetOverdrive(_ context.Context, on bool) error {
	return f.boolWrite("SetOverdrive", &f.od, on)
}
func (f *fakeBackend) BootSound(context.Context) (bool, error) {
	return f.boolRead("BootSound", &f.boot)
}
func (f *fakeBackend) SetBootSound(_ context.Context, on bool) error {
	return f.boolWrite("SetBootSound", &f.boot, on)
}
func (f *fakeBackend) KeyboardTimeout(context.Context) (bool, error) {
	return f.boolRead("KeyboardTimeout", &f.kbt)
}
func (f *fakeBackend) SetKeyboardTimeout(_ context.Context, on bool) error {
	return f.boolWrite("SetKeyboardTimeout", &f.kbt, on)
}
func (f *fakeBackend) ChargeToFullActive(context.Context) (bool, error) {
	return f.boolRead("ChargeToFullActive", &f.full)
}
func (f *fakeBackend) ChargeToFull(_ context.Context, start bool) error {
	return f.boolWrite("ChargeToFull", &f.full, start)
}

func (f *fakeBackend) USBCharge(context.Context) (int, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.usb, f.begin("read:USBCharge")
}

func (f *fakeBackend) SetUSBCharge(_ context.Context, p int) error {
	return f.write("SetUSBCharge", func() error { f.usb = p; return nil })
}

func (f *fakeBackend) GPUMode(context.Context) (string, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.gpu, f.begin("read:GPUMode")
}

func (f *fakeBackend) SetGPUMode(_ context.Context, m string) error {
	return f.write("SetGPUMode", func() error { f.gpu = m; return nil })
}

// ---- LightBackend

func (f *fakeBackend) KeyboardCaps() KeyboardCaps {
	return KeyboardCaps{Effects: []string{"off", "static", "breathing", "wave", "snake", "ripple", "neon", "raindrop", "lightning", "spot", "star", "fireball", "snow", "heartbeat"},
		MaxBrightness: 50, MinSpeed: 1, MaxSpeed: 12}
}

func (f *fakeBackend) KeyboardLast(context.Context) (KeyboardLook, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if !f.kbKnown {
		return KeyboardLook{}, f.begin("read:KeyboardLast")
	}
	return f.kb, f.begin("read:KeyboardLast")
}

func (f *fakeBackend) KeyboardBrightness(context.Context) (int, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.kbNow, f.begin("read:KeyboardBrightness")
}

func (f *fakeBackend) SetKeyboard(_ context.Context, look KeyboardLook) error {
	return f.write(fmt.Sprintf("SetKeyboard %s b=%d", look.Name, look.Brightness), func() error {
		if look.Name == "off" {
			f.kb, f.kbKnown, f.kbNow = KeyboardLook{Name: "off"}, true, 0
			return nil
		}
		f.kb, f.kbKnown, f.kbNow = look, true, look.Brightness
		return nil
	})
}

func (f *fakeBackend) SavedLightings() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]string(nil), f.saved...)
}

func (f *fakeBackend) ApplySavedLighting(_ context.Context, name string) error {
	return f.write("ApplySavedLighting "+name, func() error { return nil })
}

func (f *fakeBackend) RearModes() []string {
	return []string{"static", "breathing", "neon", "wave", "twinkling"}
}

func (f *fakeBackend) Rear(context.Context) (RearLook, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.rear, f.begin("read:Rear")
}

func (f *fakeBackend) SetRear(_ context.Context, look RearLook, keepZones bool) error {
	return f.write(fmt.Sprintf("SetRear %s b=%d", look.Mode, look.Brightness), func() error {
		f.rear, f.zones = look, keepZones
		return nil
	})
}

// ---- AudioBackend

func (f *fakeBackend) SoundCard(context.Context) (SoundCard, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.sound, f.begin("read:SoundCard")
}

func (f *fakeBackend) RepairSound(context.Context) (RepairResult, error) {
	var res RepairResult
	err := f.write("RepairSound", func() error { res = f.repair; return nil })
	return res, err
}

func (f *fakeBackend) Output(context.Context) (string, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.output, f.begin("read:Output")
}

func (f *fakeBackend) SetOutput(_ context.Context, o string) error {
	return f.write("SetOutput", func() error { f.output = o; return nil })
}

func (f *fakeBackend) SpeakerFilter(context.Context) (bool, error) {
	return f.boolRead("SpeakerFilter", &f.spk)
}
func (f *fakeBackend) SetSpeakerFilter(_ context.Context, on bool) error {
	return f.boolWrite("SetSpeakerFilter", &f.spk, on)
}
func (f *fakeBackend) DTSOn(context.Context) (bool, error) { return f.boolRead("DTSOn", &f.dts) }
func (f *fakeBackend) SetDTS(_ context.Context, on bool) error {
	return f.boolWrite("SetDTS", &f.dts, on)
}

func (f *fakeBackend) DTSDevices(context.Context) ([]DTSDevice, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]DTSDevice(nil), f.devices...), f.begin("read:DTSDevices")
}

func (f *fakeBackend) SetDTSMode(_ context.Context, m string) error {
	return f.write("SetDTSMode", func() error {
		for i := range f.devices {
			f.devices[i].Profile = m
		}
		return nil
	})
}

func (f *fakeBackend) DTSEverywhere(context.Context) (string, error) {
	return "", f.write("DTSEverywhere", func() error { return nil })
}

func (f *fakeBackend) Microphone(context.Context) (Microphone, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.mic, f.begin("read:Microphone")
}

func (f *fakeBackend) SetMicMuted(_ context.Context, m bool) error {
	return f.write("SetMicMuted", func() error { f.mic.Muted = m; return nil })
}

func (f *fakeBackend) SetMicVolume(_ context.Context, p int) error {
	return f.write("SetMicVolume", func() error { f.mic.Volume = Some(p); return nil })
}

func (f *fakeBackend) VoiceModes(context.Context) (VoiceModes, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.voice, f.begin("read:VoiceModes")
}

func (f *fakeBackend) SetVoiceMode(_ context.Context, input, mode string) error {
	return f.write("SetVoiceMode "+input, func() error {
		if input == VoiceHeadset {
			f.voice.Headset = mode
		} else {
			f.voice.Array = mode
		}
		return nil
	})
}

// ---- ProfileBackend, SystemBackend

func (f *fakeBackend) ProfileNames() []string { return append([]string(nil), f.profiles...) }
func (f *fakeBackend) DescribeProfile(name string) string {
	return "Power: " + name + " mode (held for a while)  ·  Fans: smart curve"
}
func (f *fakeBackend) ApplyProfile(_ context.Context, name string) error {
	return f.write("ApplyProfile "+name, func() error { return nil })
}
func (f *fakeBackend) LookNames() []string { return append([]string(nil), f.looks...) }
func (f *fakeBackend) ApplyLook(_ context.Context, name string) error {
	return f.write("ApplyLook "+name, func() error { return nil })
}

func (f *fakeBackend) ModuleVersion(context.Context) (string, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.module, f.begin("read:ModuleVersion")
}

func (f *fakeBackend) DoctorChecks() []string { return []string{"Kernel", "Kernel headers", "Audio"} }

func (f *fakeBackend) RunDoctor(context.Context) ([]DoctorResult, error) {
	var res []DoctorResult
	err := f.write("RunDoctor", func() error { res = append(res, f.doctor...); return nil })
	return res, err
}

func (f *fakeBackend) OpenWindow(context.Context) error {
	return f.write("OpenWindow", func() error { f.window++; return nil })
}

// callList returns a copy of the recorded calls.
func (f *fakeBackend) callList() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]string(nil), f.calls...)
}

func (f *fakeBackend) writeCalls() []string {
	var out []string
	for _, c := range f.callList() {
		if !strings.HasPrefix(c, "read:") {
			out = append(out, c)
		}
	}
	return out
}

var _ Backend = (*fakeBackend)(nil)
