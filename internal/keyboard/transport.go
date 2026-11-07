package keyboard

import "time"

// Report sizes of the lighting interface (docs/PROTOCOL.md).
const (
	// PacketLen is the length of every feature report: seven data bytes and a checksum.
	PacketLen = 8
	// OutputLen is the length of one per-key output report (16 cells of 4 bytes).
	OutputLen = 64
)

// Transport is the keyboard's vendor HID interface as the controller sees it. The report id
// (always 0 on this device) is the transport's business: payloads here are data only.
// Implementations need not be safe for concurrent use: the Keyboard serialises every sequence.
type Transport interface {
	// SetFeature sends one feature report of PacketLen data bytes (HIDIOCSFEATURE).
	SetFeature(payload []byte) error
	// GetFeature reads a feature report of n data bytes (HIDIOCGFEATURE), report id stripped.
	GetFeature(n int) ([]byte, error)
	// WriteOutput sends one output report of OutputLen data bytes.
	WriteOutput(data []byte) error
	// Close releases the device.
	Close() error
}

// Opener opens the transport. The Keyboard calls it lazily, once, and again after a failure.
type Opener func() (Transport, error)

// Pacing of a per-key upload, measured on the keyboard: Windows spaces the frames about 15 ms
// apart, and unpaced writes lose the last frame; the final pause lets the controller finish
// before the brightness packet that commits the upload.
const (
	frameSpacing = 16 * time.Millisecond
	frameSettle  = 50 * time.Millisecond
)
