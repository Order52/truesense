package keyboard

import "fmt"

// The keyboard controller's OWN "backlight off after 30 s" flag. It is separate from the
// firmware (WMI) flag: SET is `30 01 <0|1> 00 00 00 00 chk`, GET is `b0 00 .. 4f` then read
// byte 2 (docs/PROTOCOL.md). Its factory state is ON, which is why the lights went dark after
// 30 s while the firmware flag read "off".

// HIDTimeoutGet reads the controller's flag, or (nil, nil) when the controller's answer is not
// the expected report (it cannot say).
func (k *Keyboard) HIDTimeoutGet() (*bool, error) {
	var on *bool
	err := k.call(func(t Transport) error {
		r, err := queryReport(t, 0xB0, 0x4F)
		if err != nil {
			return err
		}
		on = nil
		if r[0] == 0xB0 {
			v := r[2] != 0
			on = &v
		}
		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("read keyboard timeout flag: %w", err)
	}
	return on, nil
}

// HIDTimeoutSet writes the controller's flag.
func (k *Keyboard) HIDTimeoutSet(on bool) error {
	var v byte
	if on {
		v = 1
	}
	if err := k.send(pkt(0x30, 0x01, v, 0, 0, 0, 0)); err != nil {
		return fmt.Errorf("set keyboard timeout flag: %w", err)
	}
	return nil
}

// TimeoutFlag adapts the controller to the hw package's KBTimeout interface (hw sits below
// this package, so it only declares the interface).
type TimeoutFlag struct{ K *Keyboard }

// Get returns the flag, or (nil, nil) when the controller cannot say.
func (f TimeoutFlag) Get() (*bool, error) { return f.K.HIDTimeoutGet() }

// Set writes the flag.
func (f TimeoutFlag) Set(on bool) error { return f.K.HIDTimeoutSet(on) }

// Timeout returns the controller's flag as a value for hw.HW.KBT.
func (k *Keyboard) Timeout() TimeoutFlag { return TimeoutFlag{K: k} }
