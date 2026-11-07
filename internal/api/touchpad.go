package api

import (
	"github.com/Order52/truesense/internal/ipc"
)

// Touchpad reports whether the touchpad is on; nil when it cannot be told.
func (a *API) Touchpad() (*bool, error) {
	on, ok := a.s.Touchpad.Enabled()
	if !ok {
		return nil, nil
	}
	return &on, nil
}

// SetTouchpad switches the touchpad. Writing the kernel's switch needs root, so the service
// does it for users; root with no service writes it directly.
func (a *API) SetTouchpad(on bool) (bool, error) {
	action := "off"
	if on {
		action = "on"
	}
	var got bool
	err := a.s.IPC.CallInto("touchpad", ipc.Args{"action": action}, &got)
	if err == nil {
		return got, nil
	}
	if a.EUID == 0 {
		return a.s.Touchpad.Set(on)
	}
	return false, err
}
