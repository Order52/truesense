package api

import (
	"bytes"
	"context"
	"encoding/json"

	"github.com/Order52/truesense/internal/hw"
)

// StatusEntry is one section of the overview. Value is the reading, or the text
// "ERROR: ..." when that one reading failed (a broken part never hides the others).
type StatusEntry struct {
	Key   string
	Value any
}

// Status is `truesense status`: every reading at a glance. It marshals as one JSON object in
// the order the sections were read, the way the 0.3.x output was ordered.
type Status struct{ Entries []StatusEntry }

// Get returns the value of a section (nil when there is none).
func (s Status) Get(key string) any {
	for _, e := range s.Entries {
		if e.Key == key {
			return e.Value
		}
	}
	return nil
}

// MarshalJSON writes the sections as an object, in order.
func (s Status) MarshalJSON() ([]byte, error) {
	var b bytes.Buffer
	b.WriteByte('{')
	for i, e := range s.Entries {
		if i > 0 {
			b.WriteByte(',')
		}
		k, _ := json.Marshal(e.Key)
		v, err := json.Marshal(e.Value)
		if err != nil {
			v, _ = json.Marshal("ERROR: " + err.Error())
		}
		b.Write(k)
		b.WriteByte(':')
		b.Write(v)
	}
	b.WriteByte('}')
	return b.Bytes(), nil
}

// Status reads everything. Every reading is independent: one that fails becomes an
// "ERROR: ..." entry and the rest are still shown.
func (a *API) Status(ctx context.Context) Status {
	a.s.KB.Prewarm() // the keyboard takes ~50 ms to open: do it while the firmware reads below run
	var out Status
	put := func(key string, read func() (any, error)) {
		v, err := read()
		if err != nil {
			v = "ERROR: " + err.Error()
		}
		out.Entries = append(out.Entries, StatusEntry{key, v})
	}
	put("power_mode", func() (any, error) { return a.s.HW.GetMode() })
	put("sensors", func() (any, error) { return a.s.HW.ReadSensors() })
	put("fans", func() (any, error) { return a.s.HW.GetFanState() })
	put("daemon", func() (any, error) {
		var st any
		err := a.s.IPC.CallInto("state", nil, &st)
		return st, err
	})
	put("battery_limit", func() (any, error) { return a.s.HW.GetBatteryLimit() })
	put("usb_charge_stop", func() (any, error) { return a.s.HW.GetUSBCharge() })
	put("overdrive", func() (any, error) { return optionalBool(a.s.HW.GetOverdrive()) })
	put("boot_sound", func() (any, error) { return a.s.HW.GetBootSound() })
	put("kb_backlight_timeout", func() (any, error) { return a.s.HW.GetKBTimeout() })
	put("gpu_mode", func() (any, error) { return a.s.HW.GetGPUMode() })
	put("rear", func() (any, error) { return a.s.HW.RearGet() })
	put("touchpad", func() (any, error) {
		on, ok := a.s.Touchpad.Enabled()
		if !ok {
			return nil, nil
		}
		return on, nil
	})
	put("audio_output", func() (any, error) { return a.s.Audio.Output(ctx) })
	put("jack", func() (any, error) { return a.s.Audio.Jack(ctx).Raw, nil })
	put("voice", func() (any, error) { return a.s.Audio.VoiceModes(), nil })
	return out
}

// Sensors reads every temperature and both fan speeds in one firmware call.
func (a *API) Sensors() (hw.Sensors, error) { return a.s.HW.ReadSensors() }

// optionalBool turns a reading that may be unknown (the firmware answered -1) into a pointer.
func optionalBool(on, ok bool, err error) (*bool, error) {
	if err != nil || !ok {
		return nil, err
	}
	return &on, nil
}
