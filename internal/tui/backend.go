package tui

import (
	"context"
	"time"
)

// Opt is a reading that may be missing (shown as an ellipsis, never as zero).
type Opt[T any] struct {
	V  T
	OK bool
}

// Some wraps a value that is known.
func Some[T any](v T) Opt[T] { return Opt[T]{V: v, OK: true} }

// Sensors are the temperatures (degrees Celsius) and fan speeds (rpm) of one reading.
type Sensors struct {
	CPUTemp, SysTemp, GPUTemp int
	CPUFanRPM, GPUFanRPM      int
}

// Fan modes the firmware reports in FanState.Mode.
const (
	FirmwareFanAuto    = "auto"
	FirmwareFanMax     = "max"
	FirmwareFanManual  = "manual"
	FirmwareFanUnknown = "unknown"
)

// FanState is what the firmware says about the fans right now: the behaviour and the duty
// register of each fan in percent (0 means that fan is stopped).
type FanState struct {
	Mode    string
	CPUDuty int
	GPUDuty int
}

// Battery is the charge reported by the power supply: Percent 0..100 and a status such as
// "Charging", "Discharging" or "Full".
type Battery struct {
	Percent int
	Status  string
}

// ServiceState is what the TrueSense service knows. A backend without a running service
// returns a nil *ServiceState: the TUI then reads the hardware itself.
type ServiceState struct {
	// Sensors is the service's own reading of this second; nil when it has none, in
	// which case the TUI asks Backend.Sensors.
	Sensors *Sensors
	// PowerMode is the current power mode ("" when unknown).
	PowerMode string
	// AC reports whether the charger is connected.
	AC Opt[bool]
	// AutoMode is the automatic power mode: native, smart or off.
	AutoMode string
	// SmartProfile is the name of the Smart auto profile in use.
	SmartProfile string
	// FanOff is true while the service keeps both fans stopped on request.
	FanOff bool
	// SmartFan is true when the service's own fan curve is in control; FanCurve names it.
	SmartFan bool
	FanCurve string
	// CPULoad and GPULoad are percentages; GPUWatts the GPU power draw.
	CPULoad, GPULoad, GPUWatts Opt[int]
	// Activity is what Smart auto thinks the laptop is doing; ModeReason why it last
	// changed the power mode.
	Activity, ModeReason string
	// HoldLeft is how long the user's power-mode pick is still held against Smart auto.
	HoldLeft time.Duration
}

// FanRequest asks for a fan mode: Mode is smart, auto, max, manual or off. For manual,
// CPU and GPU are the duties (0, or 20..100); a 0 or Mode off is a stop and the backend
// must refuse it unless Confirm is true (the TUI sets it only after a Yes answer) and the
// service with its temperature watchdog is running.
type FanRequest struct {
	Mode     string
	CPU, GPU int
	Confirm  bool
}

// KeyboardCaps describes what the keyboard backlight supports.
type KeyboardCaps struct {
	// Effects are the effect names in display order; "off" is among them.
	Effects []string
	// MaxBrightness is the top of the keyboard scale (the shown percentage is twice the value).
	MaxBrightness int
	// MinSpeed and MaxSpeed bound the effect speed (higher is faster).
	MinSpeed, MaxSpeed int
}

// KeyboardLook is a keyboard backlight setting. Colour is six hex digits (RRGGBB);
// Direction is 0 left to right, 1 right to left, 2 up, 3 down.
type KeyboardLook struct {
	Name       string
	Color      string
	Speed      int
	Brightness int
	Direction  int
}

// RearLook is a light bar (back of the laptop) setting; Brightness is 0..100.
type RearLook struct {
	Mode       string
	Color      string
	Speed      int
	Brightness int
	Direction  int
}

// SoundStatus is the verdict on the sound card.
type SoundStatus int

// Sound card verdicts.
const (
	SoundMissing SoundStatus = iota // no sound card to judge
	SoundOK                         // the card has an output
	SoundBroken                     // the card has no output
)

// SoundCard is the state of the sound card; Problem explains SoundBroken.
type SoundCard struct {
	Status  SoundStatus
	Problem string
}

// RepairResult reports a sound repair: Fixed is true when an output exists afterwards,
// Changed when something had to be done, Steps what was done.
type RepairResult struct {
	Fixed, Changed bool
	Steps          []string
}

// Microphone is the state of the microphones: Muted is true when every input is muted.
type Microphone struct {
	Muted  bool
	Volume Opt[int]
}

// Voice inputs for SetVoiceMode.
const (
	VoiceHeadset = "headset"
	VoiceArray   = "array"
)

// VoiceModes are the noise-reduction modes (off, conference or personal) of each input.
type VoiceModes struct {
	Headset, Array string
}

// DTSDevice is an output the DTS sound processor knows and the profile it uses.
type DTSDevice struct {
	Label, Profile string
	Current        bool
}

// DoctorStatus is the grade of one health check.
type DoctorStatus string

// Health check grades.
const (
	DoctorOK   DoctorStatus = "OK"
	DoctorWarn DoctorStatus = "WARN"
	DoctorFail DoctorStatus = "FAIL"
)

// DoctorResult is the outcome of one health check; Fix is advice shown for WARN and FAIL.
type DoctorResult struct {
	Check  string
	Status DoctorStatus
	Detail string
	Fix    string
}

// PowerBackend is power mode, automatic mode and the hold on them.
type PowerBackend interface {
	// SetPowerMode switches the power mode and holds it against Smart auto for hold.
	SetPowerMode(ctx context.Context, mode string, hold time.Duration) error
	// SetAutoMode sets the automatic power mode: native, smart or off (needs the service).
	SetAutoMode(ctx context.Context, mode string) error
	// ReleaseHold hands control back to the automatic power mode now.
	ReleaseHold(ctx context.Context) error
	// SmartProfileNames lists the Smart auto profiles; SmartProfile names the one in use.
	SmartProfileNames() []string
	SmartProfile(ctx context.Context) (string, error)
	// UseSmartProfile makes a Smart auto profile current (the service needs it).
	UseSmartProfile(ctx context.Context, name string) error
}

// FanBackend is the fans and their curves.
type FanBackend interface {
	// SetFan applies a request (see FanRequest for the stop rules).
	SetFan(ctx context.Context, req FanRequest) error
	// FanCurveNames lists the fan curves; FanCurve names the one in use.
	FanCurveNames() []string
	FanCurve(ctx context.Context) (string, error)
	// UseFanCurve makes a curve current.
	UseFanCurve(ctx context.Context, name string) error
}

// BatteryBackend is charging and the firmware's on/off settings.
type BatteryBackend interface {
	BatteryLimit(ctx context.Context) (bool, error)
	SetBatteryLimit(ctx context.Context, on bool) error
	Calibration(ctx context.Context) (bool, error)
	SetCalibration(ctx context.Context, on bool) error
	// USBCharge is the battery percentage down to which USB ports charge while the laptop is off (0 = off).
	USBCharge(ctx context.Context) (int, error)
	SetUSBCharge(ctx context.Context, percent int) error
	// ChargeToFullActive reports whether "charge to 100 % once" is running; ChargeToFull
	// starts (true) or cancels (false) it.
	ChargeToFullActive(ctx context.Context) (bool, error)
	ChargeToFull(ctx context.Context, start bool) error
}

// DisplayBackend is graphics and other firmware switches.
type DisplayBackend interface {
	GPUMode(ctx context.Context) (string, error) // optimus, nvidia or auto
	SetGPUMode(ctx context.Context, mode string) error
	Overdrive(ctx context.Context) (bool, error)
	SetOverdrive(ctx context.Context, on bool) error
	BootSound(ctx context.Context) (bool, error)
	SetBootSound(ctx context.Context, on bool) error
	KeyboardTimeout(ctx context.Context) (bool, error)
	SetKeyboardTimeout(ctx context.Context, on bool) error
}

// LightBackend is the keyboard backlight and the rear light bar.
type LightBackend interface {
	KeyboardCaps() KeyboardCaps
	// KeyboardLast is what this tool last put on the keyboard (the hardware cannot say
	// which effect runs); an empty Name means unknown, Name "off" means the light was switched off.
	KeyboardLast(ctx context.Context) (KeyboardLook, error)
	// KeyboardBrightness is the keyboard's own current brightness (Fn+F11/F12 change it too).
	KeyboardBrightness(ctx context.Context) (int, error)
	// SetKeyboard applies a look; Name "off" switches the light off.
	SetKeyboard(ctx context.Context, look KeyboardLook) error
	// SavedLightings lists the per-key lightings saved in the window; ApplySavedLighting applies one.
	SavedLightings() []string
	ApplySavedLighting(ctx context.Context, name string) error

	RearModes() []string
	Rear(ctx context.Context) (RearLook, error)
	// SetRear applies a light bar look. KeepZones asks to keep the three part colours set
	// in the window (true while the user has not picked a colour here).
	SetRear(ctx context.Context, look RearLook, keepZones bool) error
}

// AudioBackend is the sound card, outputs, DTS and microphones.
type AudioBackend interface {
	SoundCard(ctx context.Context) (SoundCard, error)
	RepairSound(ctx context.Context) (RepairResult, error)
	Output(ctx context.Context) (string, error) // speakers or headphones
	SetOutput(ctx context.Context, output string) error
	SpeakerFilter(ctx context.Context) (bool, error)
	SetSpeakerFilter(ctx context.Context, on bool) error
	DTSOn(ctx context.Context) (bool, error)
	SetDTS(ctx context.Context, on bool) error
	DTSDevices(ctx context.Context) ([]DTSDevice, error)
	// SetDTSMode sets the DTS sound mode on every output.
	SetDTSMode(ctx context.Context, mode string) error
	// DTSEverywhere applies DTS to every output now and says what it did ("" = nothing to do).
	DTSEverywhere(ctx context.Context) (string, error)
	Microphone(ctx context.Context) (Microphone, error)
	SetMicMuted(ctx context.Context, muted bool) error
	SetMicVolume(ctx context.Context, percent int) error
	VoiceModes(ctx context.Context) (VoiceModes, error)
	// SetVoiceMode sets the noise reduction of input (VoiceHeadset or VoiceArray) and applies the microphone policy.
	SetVoiceMode(ctx context.Context, input, mode string) error
}

// ProfileBackend is whole-setup profiles and boot looks.
type ProfileBackend interface {
	ProfileNames() []string
	// DescribeProfile says in one line what a profile changes ("" when it cannot be read).
	DescribeProfile(name string) string
	ApplyProfile(ctx context.Context, name string) error
	LookNames() []string
	ApplyLook(ctx context.Context, name string) error
}

// SystemBackend is versions, the health check and the window.
type SystemBackend interface {
	ModuleVersion(ctx context.Context) (string, error)
	// DoctorChecks lists the titles of the health checks in display order; RunDoctor runs them all (read-only).
	DoctorChecks() []string
	RunDoctor(ctx context.Context) ([]DoctorResult, error)
	// OpenWindow starts the graphical window as a detached process.
	OpenWindow(ctx context.Context) error
}

// LiveBackend is what the dashboard reads every second. All of it is cheap and called from
// a background goroutine.
type LiveBackend interface {
	// ServiceState returns nil, nil when no service runs; an error is treated the same way.
	ServiceState(ctx context.Context) (*ServiceState, error)
	// Sensors, FanState, PowerMode and OnAC read the hardware directly.
	Sensors(ctx context.Context) (Sensors, error)
	FanState(ctx context.Context) (FanState, error)
	PowerMode(ctx context.Context) (string, error)
	OnAC(ctx context.Context) (bool, error)
	// Battery reads the power supply; ok is false on a machine without a battery.
	Battery() (b Battery, ok bool)
}

// Backend is everything the terminal UI reads and writes. Operations that can fail return
// an error whose text is shown to the user at the bottom of the screen; reads are cheap
// and may be called from several goroutines at once.
type Backend interface {
	LiveBackend
	PowerBackend
	FanBackend
	BatteryBackend
	DisplayBackend
	LightBackend
	AudioBackend
	ProfileBackend
	SystemBackend
}
