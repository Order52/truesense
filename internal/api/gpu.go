package api

import (
	"errors"
	"fmt"

	"github.com/Order52/truesense/internal/gpuundervolt"
	"github.com/Order52/truesense/internal/ipc"
)

// AppliedGPUUndervolt is a GPU undervolt the service applied or keeps for boot.
type AppliedGPUUndervolt struct {
	Name   string `json:"name"`
	Offset int    `json:"offset"`
	Cap    int    `json:"cap"`
}

// GPUUndervolt is the service's view of the GPU undervolt: the curve offset and the range the
// driver allows (or why they cannot be read), what was applied, what is kept for boot.
type GPUUndervolt struct {
	OffsetMHz *int                 `json:"offset_mhz,omitempty"`
	RangeMHz  *[2]int              `json:"range_mhz,omitempty"`
	Error     string               `json:"error,omitempty"`
	Applied   *AppliedGPUUndervolt `json:"applied"`
	Boot      *AppliedGPUUndervolt `json:"boot"`
	Pending   bool                 `json:"pending"`
}

// GPUUndervoltState asks the service for the current curve offset.
func (a *API) GPUUndervoltState() (GPUUndervolt, error) {
	var r GPUUndervolt
	err := a.s.IPC.CallInto("gpu_undervolt", ipc.Args{"action": "get"}, &r)
	return r, err
}

// GPUUndervolts lists the GPU undervolt profiles (built-in first).
func (a *API) GPUUndervolts() (NamedList, error) {
	names, err := a.s.GPUStore.Names()
	if err != nil {
		return NamedList{}, err
	}
	l := NamedList{Names: names, Descriptions: map[string]string{}}
	for _, n := range names {
		if p, ok := gpuundervolt.Builtin(n); ok {
			l.Descriptions[n] = p.Description
		}
	}
	return l, nil
}

// GPUUndervoltProfile reads one profile.
func (a *API) GPUUndervoltProfile(name string) (gpuundervolt.Profile, error) {
	return a.s.GPUStore.Load(name)
}

// ResetGPUUndervolt puts the curve offset back to 0 and lifts the clock cap.
func (a *API) ResetGPUUndervolt() (gpuundervolt.Applied, error) {
	var r gpuundervolt.Applied
	err := a.s.IPC.CallInto("gpu_undervolt", ipc.Args{"action": "reset"}, &r)
	return r, err
}

// ApplyGPUUndervolt applies a profile through the service (until the next reboot).
func (a *API) ApplyGPUUndervolt(name string, r UndervoltRequest) (gpuundervolt.Applied, error) {
	p, err := a.s.GPUStore.Load(name)
	if err != nil {
		return gpuundervolt.Applied{}, err
	}
	return a.ApplyGPUUndervoltValues(name, p.Offset, p.Cap, r)
}

func (a *API) checkGPUUndervolt(offset, capMHz int, r UndervoltRequest) error {
	if !a.DaemonAvailable() { // nothing can be applied without the service; its own message says so
		return nil
	}
	if !r.Confirm {
		return errors.New("a GPU undervolt needs explicit confirmation (it can crash games or the driver if too aggressive)")
	}
	if lvl, why := gpuundervolt.Risk(offset, capMHz); lvl == gpuundervolt.Danger && !r.Force {
		return fmt.Errorf("%s Add the explicit force flag if you really mean it (CLI: --force)", why)
	}
	return nil
}

// ApplyGPUUndervoltValues applies an offset and cap given as numbers (the window's editor),
// under a name that labels them. It needs the confirmation, and force beyond the danger line.
func (a *API) ApplyGPUUndervoltValues(name string, offset, capMHz int, r UndervoltRequest) (gpuundervolt.Applied, error) {
	var out gpuundervolt.Applied
	if err := a.checkGPUUndervolt(offset, capMHz, r); err != nil {
		return out, err
	}
	err := a.s.IPC.CallInto("gpu_undervolt", ipc.Args{"action": "apply", "offset": offset, "cap": capMHz, "confirm": r.Confirm, "name": name, "force": r.Force}, &out)
	return out, err
}

// CopyGPUUndervolt saves a copy under newName (NAME-copy when empty) and answers the name.
func (a *API) CopyGPUUndervolt(name, newName string) (string, error) {
	return a.s.GPUStore.Copy(name, newName)
}

// SaveGPUUndervolt saves a profile (it applies nothing); capMHz 0 means no cap.
func (a *API) SaveGPUUndervolt(name string, offset *int, capMHz int) (gpuundervolt.Profile, error) {
	if name == "" || offset == nil {
		return gpuundervolt.Profile{}, fmt.Errorf("usage: truesense gpu undervolt save NAME --offset 100 --cap 2400")
	}
	return a.s.GPUStore.Save(name, gpuundervolt.Profile{Offset: *offset, Cap: capMHz})
}

// DeleteGPUUndervolt removes one of your profiles.
func (a *API) DeleteGPUUndervolt(name string) error { return a.s.GPUStore.Delete(name) }

// SetGPUUndervoltBoot makes the service apply a profile at boot. An empty name or "off" clears it.
func (a *API) SetGPUUndervoltBoot(name string, r UndervoltRequest) (*AppliedGPUUndervolt, error) {
	if name == "" || name == "off" {
		return nil, a.s.IPC.CallInto("gpu_undervolt", ipc.Args{"action": "boot_clear"}, nil)
	}
	p, err := a.s.GPUStore.Load(name)
	if err != nil {
		return nil, err
	}
	return a.SetGPUUndervoltBootValues(name, p.Offset, p.Cap, r)
}

// SetGPUUndervoltBootValues makes the service apply an offset and cap given as numbers at boot.
func (a *API) SetGPUUndervoltBootValues(name string, offset, capMHz int, r UndervoltRequest) (*AppliedGPUUndervolt, error) {
	if err := a.checkGPUUndervolt(offset, capMHz, r); err != nil {
		return nil, err
	}
	var boot AppliedGPUUndervolt
	err := a.s.IPC.CallInto("gpu_undervolt", ipc.Args{"action": "boot", "offset": offset, "cap": capMHz, "confirm": r.Confirm, "name": name, "force": r.Force}, &boot)
	return &boot, err
}

// GPUUndervoltIsBuiltin reports whether a profile is read-only.
func GPUUndervoltIsBuiltin(name string) bool { return gpuundervolt.IsBuiltin(name) }
