package api

import (
	"context"
	"errors"
	"fmt"

	"github.com/Order52/truesense/internal/ipc"
	"github.com/Order52/truesense/internal/power"
	"github.com/Order52/truesense/internal/undervolt"
)

// CPUInfo reads the CPU package power limits, the MSR lock and the GPU power limit straight
// from the hardware (every part is best effort: what cannot be read is null).
func (a *API) CPUInfo(ctx context.Context) power.Info { return a.s.Power.Info(ctx) }

// PowerLimits is the service's view of the CPU power limits: the live values, the ones you
// set (nil when automatic) and the automatic ones from the configuration.
type PowerLimits struct {
	power.Info
	User *struct {
		PL1 float64 `json:"pl1"`
		PL2 float64 `json:"pl2"`
	} `json:"user"`
	Auto struct {
		NormalW int `json:"normal_w"`
		GamingW int `json:"gaming_w"`
	} `json:"auto"`
}

// CPULimits asks the service for the power limits.
func (a *API) CPULimits() (PowerLimits, error) {
	var r PowerLimits
	err := a.s.IPC.CallInto("cpu_info", nil, &r)
	return r, err
}

// SetCPULimits sets PL1 (sustained) and PL2 (burst) in watts; the service keeps them. A pair
// beyond the danger line is refused unless force.
func (a *API) SetCPULimits(pl1, pl2 float64, force bool) (power.Applied, error) {
	var r power.Applied
	if lvl, why := power.Risk(pl1, pl2); lvl == power.Danger && !force && a.DaemonAvailable() {
		return r, fmt.Errorf("%s Add the explicit force flag if you really mean it", why)
	}
	err := a.s.IPC.CallInto("cpu_limits", ipc.Args{"pl1": pl1, "pl2": pl2, "force": force}, &r)
	return r, err
}

// ResetCPULimits goes back to the automatic limits and answers what the service reports then.
func (a *API) ResetCPULimits() (PowerLimits, error) {
	if err := a.s.IPC.CallInto("cpu_limits", ipc.Args{"reset": true}, nil); err != nil {
		return PowerLimits{}, err
	}
	return a.CPULimits()
}

// AppliedUndervolt is a CPU undervolt the service applied or keeps for boot.
type AppliedUndervolt struct {
	Name  string `json:"name"`
	Core  int    `json:"core"`
	Cache int    `json:"cache"`
}

// CPUUndervolt is the service's view of the CPU undervolt: the offsets (or why they cannot be
// read), what was applied, what is kept for boot, and whether the boot crash guard is armed.
type CPUUndervolt struct {
	CoreMV  *int              `json:"core_mv,omitempty"`
	CacheMV *int              `json:"cache_mv,omitempty"`
	Error   string            `json:"error,omitempty"`
	Applied *AppliedUndervolt `json:"applied"`
	Boot    *AppliedUndervolt `json:"boot"`
	Pending bool              `json:"pending"`
}

// CPUUndervoltState asks the service for the current offsets.
func (a *API) CPUUndervoltState() (CPUUndervolt, error) {
	var r CPUUndervolt
	err := a.s.IPC.CallInto("undervolt", ipc.Args{"action": "get"}, &r)
	return r, err
}

// CPUUndervolts lists the undervolt profiles (built-in first).
func (a *API) CPUUndervolts() (NamedList, error) {
	names, err := a.s.UVStore.Names()
	if err != nil {
		return NamedList{}, err
	}
	l := NamedList{Names: names, Descriptions: map[string]string{}}
	for _, n := range names {
		if p, ok := undervolt.Builtin(n); ok {
			l.Descriptions[n] = p.Description
		}
	}
	return l, nil
}

// CPUUndervoltProfile reads one profile.
func (a *API) CPUUndervoltProfile(name string) (undervolt.Profile, error) {
	return a.s.UVStore.Load(name)
}

// ResetCPUUndervolt writes stock voltage (never needs a confirmation).
func (a *API) ResetCPUUndervolt() (undervolt.Offsets, error) {
	var r undervolt.Offsets
	err := a.s.IPC.CallInto("undervolt", ipc.Args{"action": "reset"}, &r)
	return r, err
}

// UndervoltRequest is how an undervolt is asked for: it needs the explicit confirmation, and
// a value beyond the danger line needs force as well.
type UndervoltRequest struct {
	Confirm, Force bool
}

// ApplyCPUUndervolt applies a profile through the service (until the next reboot).
func (a *API) ApplyCPUUndervolt(name string, r UndervoltRequest) (undervolt.Offsets, error) {
	p, err := a.s.UVStore.Load(name)
	if err != nil {
		return undervolt.Offsets{}, err
	}
	return a.applyCPUUndervolt(name, p.Core, p.Cache, r)
}

// SetCPUUndervolt changes one plane (0 core, 2 cache) and keeps the other at what the
// service reports now.
func (a *API) SetCPUUndervolt(plane, mv int, r UndervoltRequest) (undervolt.Offsets, error) {
	cur, err := a.CPUUndervoltState()
	if err != nil {
		return undervolt.Offsets{}, err
	}
	core, cache := 0, 0
	if cur.CoreMV != nil {
		core = *cur.CoreMV
	}
	if cur.CacheMV != nil {
		cache = *cur.CacheMV
	}
	if plane == 0 {
		core = mv
	} else {
		cache = mv
	}
	return a.applyCPUUndervolt("manual", core, cache, r)
}

// ApplyCPUUndervoltValues applies offsets given as numbers (the window's editor), under a name
// that labels them. Like every undervolt it needs the confirmation, and a value beyond the
// danger line needs force as well: refused here even if the caller already asked the user.
func (a *API) ApplyCPUUndervoltValues(name string, core, cache int, r UndervoltRequest) (undervolt.Offsets, error) {
	return a.applyCPUUndervolt(name, core, cache, r)
}

func (a *API) checkCPUUndervolt(core, cache int, r UndervoltRequest) error {
	if !a.DaemonAvailable() { // nothing can be applied without the service; its own message says so
		return nil
	}
	if !r.Confirm {
		return errors.New("an undervolt needs explicit confirmation (it can freeze or reboot the laptop if too aggressive)")
	}
	if lvl, why := undervolt.Risk(core, cache); lvl == undervolt.Danger && !r.Force {
		return fmt.Errorf("%s Add the explicit force flag if you really mean it (CLI: --force)", why)
	}
	return nil
}

func (a *API) applyCPUUndervolt(name string, core, cache int, r UndervoltRequest) (undervolt.Offsets, error) {
	var off undervolt.Offsets
	if err := a.checkCPUUndervolt(core, cache, r); err != nil {
		return off, err
	}
	err := a.s.IPC.CallInto("undervolt", ipc.Args{"action": "apply", "core": core, "cache": cache, "confirm": r.Confirm, "name": name, "force": r.Force}, &off)
	return off, err
}

// CopyCPUUndervolt saves a copy under newName (NAME-copy when empty) and answers the name.
func (a *API) CopyCPUUndervolt(name, newName string) (string, error) {
	return a.s.UVStore.Copy(name, newName)
}

// SaveCPUUndervolt saves a profile (it applies nothing).
func (a *API) SaveCPUUndervolt(name string, core, cache *int) (undervolt.Profile, error) {
	if name == "" || core == nil || cache == nil {
		return undervolt.Profile{}, fmt.Errorf("usage: truesense cpu undervolt save NAME --core -50 --cache -40")
	}
	return a.s.UVStore.Save(name, undervolt.Profile{Core: *core, Cache: *cache})
}

// DeleteCPUUndervolt removes one of your profiles.
func (a *API) DeleteCPUUndervolt(name string) error { return a.s.UVStore.Delete(name) }

// SetCPUUndervoltBoot makes the service apply a profile at boot, guarded: it is skipped and
// switched off when the previous boot did not stay up. An empty name or "off" clears it.
func (a *API) SetCPUUndervoltBoot(name string, r UndervoltRequest) (*AppliedUndervolt, error) {
	if name == "" || name == "off" {
		return nil, a.s.IPC.CallInto("undervolt", ipc.Args{"action": "boot_clear"}, nil)
	}
	p, err := a.s.UVStore.Load(name)
	if err != nil {
		return nil, err
	}
	return a.SetCPUUndervoltBootValues(name, p.Core, p.Cache, r)
}

// SetCPUUndervoltBootValues makes the service apply offsets given as numbers at boot (guarded).
func (a *API) SetCPUUndervoltBootValues(name string, core, cache int, r UndervoltRequest) (*AppliedUndervolt, error) {
	if err := a.checkCPUUndervolt(core, cache, r); err != nil {
		return nil, err
	}
	var boot AppliedUndervolt
	err := a.s.IPC.CallInto("undervolt", ipc.Args{"action": "boot", "core": core, "cache": cache, "confirm": r.Confirm, "name": name, "force": r.Force}, &boot)
	return &boot, err
}

// CPUUndervoltIsBuiltin reports whether a profile is read-only.
func CPUUndervoltIsBuiltin(name string) bool { return undervolt.IsBuiltin(name) }
