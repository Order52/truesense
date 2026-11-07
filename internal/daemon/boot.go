package daemon

import (
	"encoding/json"
	"strings"
	"time"

	"github.com/Order52/truesense/internal/chargefull"
)

func joinComma(s []string) string { return strings.Join(s, ", ") }

func jsonUnmarshalPL(b []byte, u *struct{ PL1, PL2 float64 }) error {
	var raw struct {
		PL1 float64 `json:"pl1"`
		PL2 float64 `json:"pl2"`
	}
	if err := json.Unmarshal(b, &raw); err != nil {
		return err
	}
	u.PL1, u.PL2 = raw.PL1, raw.PL2
	return nil
}

// decode converts a stored setting (decoded JSON, or a struct set this run) into out.
func decode(v any, out any) bool {
	if v == nil {
		return false
	}
	b, err := json.Marshal(v)
	return err == nil && json.Unmarshal(b, out) == nil
}

// applyBootLook puts the lighting chosen for boot on the lights. The keyboard may enumerate a
// little after the service starts, so it retries for about 20 seconds.
func (dm *Daemon) applyBootLook(why string) {
	if dm.d.Looks == nil {
		return
	}
	go func() {
		look := dm.d.Looks.ReadBoot()
		if look.IsEmpty() {
			return
		}
		for attempt := 0; attempt < 20; attempt++ {
			dm.mu.Lock()
			done, err := dm.d.Looks.Apply(look)
			dm.mu.Unlock()
			if err == nil {
				dm.log.Info("boot look applied", "why", why, "done", strings.Join(done, "; "))
				return
			}
			if attempt == 19 {
				dm.log.Warn("boot look failed", "why", why, "err", err)
				return
			}
			time.Sleep(time.Second)
		}
	}()
}

type uvBoot struct {
	Name  string `json:"name"`
	Core  int    `json:"core"`
	Cache int    `json:"cache"`
}

// applyUVBoot applies the opt-in CPU undervolt at boot, with a crash guard: the flag file
// exists from applying until ten minutes of uptime. If it is still there at the next start the
// previous boot did not survive, so the undervolt is switched off instead of applied again.
func (dm *Daemon) applyUVBoot() {
	var b uvBoot
	v, _ := dm.d.State.Get("uv_boot")
	if !decode(v, &b) || dm.d.UV == nil || dm.d.UVGuard == nil {
		return
	}
	if dm.d.UVGuard.Tripped() {
		dm.d.State.Set("uv_boot", nil)
		dm.log.Warn("undervolt at boot disabled: the previous boot did not stay up for 10 minutes after applying it")
		return
	}
	if err := dm.d.UVGuard.Arm(); err != nil {
		dm.log.Warn("undervolt at boot not applied: no crash-guard flag could be written", "err", err)
		return
	}
	if _, err := dm.d.UV.Apply(b.Core, b.Cache, true); err != nil {
		dm.log.Warn("undervolt at boot failed", "err", err)
		dm.d.UVGuard.Disarm()
		return
	}
	dm.log.Info("undervolt at boot", "core_mv", b.Core, "cache_mv", b.Cache)
}

type gpuBoot struct {
	Name   string `json:"name"`
	Offset int    `json:"offset"`
	Cap    int    `json:"cap"`
}

// applyGPUUVBoot is the same guard for the GPU undervolt.
func (dm *Daemon) applyGPUUVBoot() {
	var b gpuBoot
	v, _ := dm.d.State.Get("gpu_uv_boot")
	if !decode(v, &b) || dm.d.GPUUV == nil || dm.d.GPUGuard == nil {
		return
	}
	if dm.d.GPUGuard.Tripped() {
		dm.d.State.Set("gpu_uv_boot", nil)
		dm.log.Warn("GPU undervolt at boot disabled: the previous boot did not stay up for 10 minutes after applying it")
		return
	}
	if err := dm.d.GPUGuard.Arm(); err != nil {
		dm.log.Warn("GPU undervolt at boot not applied: no crash-guard flag could be written", "err", err)
		return
	}
	if _, err := dm.d.GPUUV.Apply(b.Offset, b.Cap); err != nil {
		dm.log.Warn("GPU undervolt at boot failed", "err", err)
		dm.d.GPUGuard.Disarm()
		return
	}
	dm.log.Info("GPU undervolt at boot", "offset_mhz", b.Offset, "cap_mhz", b.Cap)
}

// chargeFullCheck ends "charge to 100 % once" when it is over: the 80 % limit comes back.
func (dm *Daemon) chargeFullCheck() {
	dm.mu.Lock()
	defer dm.mu.Unlock()
	var cf chargefull.State
	v, _ := dm.d.State.Get("charge_full")
	if !decode(v, &cf) {
		return
	}
	ac := dm.lastAC != nil && *dm.lastAC
	done, changed := chargefull.Check(&cf, ac, dm.d.Sensors.BatteryPct(), dm.d.Clock.Wall())
	if changed {
		dm.d.State.Set("charge_full", cf)
	}
	if done == "" {
		return
	}
	if _, err := dm.d.HW.SetBatteryLimit(true); err != nil {
		dm.log.Warn("could not bring the 80 % limit back", "err", err)
		return
	}
	dm.d.State.Set("charge_full", nil)
	dm.log.Info("80 % charge limit is back", "why", done)
	dm.say("80 % charge limit is back ("+done+")", "battery")
}

// setHold makes the user's own power-mode pick override the automatic one for minutes.
func (dm *Daemon) setHold(minutes float64) float64 {
	ac := dm.lastAC != nil && *dm.lastAC
	got := dm.engine.SetHold(float64(dm.d.Clock.Wall().UnixNano())/1e9, minutes, ac, dm.activity)
	dm.d.State.Set("profile_hold_until", dm.engine.HoldUntil())
	return got
}
