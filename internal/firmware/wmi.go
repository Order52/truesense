package firmware

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"strings"
	"syscall"
)

// WMI is the real device: /dev/truesense. Every call opens the device, writes one command,
// reads the reply and closes it; the kernel keeps a reply buffer per open file, so the
// daemon, the CLI and the windows can all use the device at the same time.
type WMI struct{ Path string }

// Run implements Device.
func (w *WMI) Run(command string) (string, error) {
	f, err := os.OpenFile(w.Path, os.O_RDWR, 0)
	if err != nil {
		switch {
		case errors.Is(err, fs.ErrNotExist):
			return "", fmt.Errorf("%w: %s not found (sudo modprobe truesense_wmi, or run: truesense doctor)", ErrNotLoaded, w.Path)
		case errors.Is(err, fs.ErrPermission):
			return "", fmt.Errorf("%w: %s (log in on the laptop's seat, or use sudo; see: truesense doctor)", ErrPermission, w.Path)
		}
		return "", err
	}
	defer f.Close()
	if _, err := f.WriteString(command); err != nil {
		// The module rejected the command; its reason is waiting in the reply buffer.
		buf := make([]byte, 256)
		n, _ := f.Read(buf)
		reason := strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(string(buf[:n])), "err"))
		if reason == "" {
			var errno syscall.Errno
			if errors.As(err, &errno) {
				reason = errno.Error()
			} else {
				reason = err.Error()
			}
			reason = fmt.Sprintf("'%s' rejected: %s", command, reason)
		}
		return "", &Error{Msg: reason}
	}
	buf := make([]byte, 512)
	n, err := f.Read(buf)
	if err != nil && n == 0 {
		return "", fmt.Errorf("read reply: %w", err)
	}
	return string(buf[:n]), nil
}
