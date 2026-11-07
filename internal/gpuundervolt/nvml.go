package gpuundervolt

import "fmt"

// NVML is the small slice of NVIDIA's management library this package needs, for the GPU at
// index 0. It is an interface so everything above it is tested with a fake: the real
// implementation (nvml_linux.go) loads the driver's library at run time and is the only code
// that does.
//
// Every method returns an error; a failure reported by the driver is a Code, whose message
// names what went wrong in words the user can act on.
type NVML interface {
	// Offset returns the current graphics-clock curve offset in MHz.
	Offset() (int, error)
	// OffsetRange returns the offsets the driver allows (min, max MHz).
	OffsetRange() (lo, hi int, err error)
	// SetOffset shifts the clock/voltage curve by mhz.
	SetOffset(mhz int) error
	// LockClocks limits the graphics clock to minMHz..maxMHz.
	LockClocks(minMHz, maxMHz uint) error
	// ResetClocks lifts the clock limit.
	ResetClocks() error
}

// Opener connects to the driver. It is called lazily, when the first operation needs the GPU,
// and may be called again after a failure.
type Opener func() (NVML, error)

// Code is an NVML return code (nvmlReturn_t) other than success.
type Code int

// The codes this package gives a name to.
const (
	CodeInvalidArgument Code = 2
	CodeNotSupported    Code = 3
	CodeNoPermission    Code = 4
	CodeDriverNotLoaded Code = 9
	CodeGPUIsLost       Code = 15
	CodeUnknown         Code = 999
)

var codeText = map[Code]string{
	CodeInvalidArgument: "invalid argument",
	CodeNotSupported:    "not supported by this GPU or driver",
	CodeNoPermission:    "needs root (the TrueSense service)",
	CodeDriverNotLoaded: "driver not loaded",
	CodeGPUIsLost:       "GPU is lost",
	CodeUnknown:         "unknown error",
}

// Error describes the code in words, or "NVML error N" for one that has no entry.
func (c Code) Error() string {
	if s, ok := codeText[c]; ok {
		return s
	}
	return fmt.Sprintf("NVML error %d", int(c))
}
