package undervolt

import (
	"encoding/binary"
	"fmt"
	"os"
)

// MSR reads and writes model-specific registers of CPU 0. It is an interface so the mailbox
// logic is tested against a fake and never touches /dev/cpu.
type MSR interface {
	Read(offset uint32) (uint64, error)
	Write(offset uint32, value uint64) error
}

// DefaultMSRPath is CPU 0's MSR device (needs root and the msr kernel module).
const DefaultMSRPath = "/dev/cpu/0/msr"

// DevMSR is the real MSR: it opens the device for every access, like the Python
// implementation, so nothing stays open between calls. The kernel's msr driver takes the
// register number as the file offset and moves 8 bytes.
type DevMSR struct {
	// Path is the device; empty means DefaultMSRPath.
	Path string
}

func (d DevMSR) path() string {
	if d.Path == "" {
		return DefaultMSRPath
	}
	return d.Path
}

// Read returns the register's value.
func (d DevMSR) Read(offset uint32) (uint64, error) {
	f, err := os.Open(d.path())
	if err != nil {
		return 0, err
	}
	defer func() { _ = f.Close() }() // a read-only file: nothing to flush
	var b [8]byte
	if _, err := f.ReadAt(b[:], int64(offset)); err != nil {
		return 0, fmt.Errorf("read MSR %#x: %w", offset, err)
	}
	return binary.LittleEndian.Uint64(b[:]), nil
}

// Write stores a value in the register.
func (d DevMSR) Write(offset uint32, value uint64) error {
	f, err := os.OpenFile(d.path(), os.O_WRONLY, 0)
	if err != nil {
		return err
	}
	var b [8]byte
	binary.LittleEndian.PutUint64(b[:], value)
	if _, err := f.WriteAt(b[:], int64(offset)); err != nil {
		_ = f.Close()
		return fmt.Errorf("write MSR %#x: %w", offset, err)
	}
	return f.Close()
}
