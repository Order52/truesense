package daemon

import (
	"encoding/json"
	"math"
	"strconv"
	"strings"

	"github.com/Order52/truesense/internal/fsx"
)

// liveKeys fixes the order of the live state file (status bars and health.sh read it).
var liveKeys = []string{"mode", "activity", "fan", "tcpu", "tgpu", "chassis", "cpu", "gpu", "gpuw", "rpm1", "rpm2", "cap", "power_mode", "ac", "auto", "auto_mode", "fan_curve", "smart_profile", "mode_reason", "fan_off", "hw_ok"}

func round(v float64) int { return int(math.Round(v)) }

// publish records the live readings for the "state" request and writes them as key=value lines
// for status bars. health.sh restarts the service when the file goes stale, so it is written on
// every step.
func (dm *Daemon) publish(smart bool, gpuUtil, gpuPower, load, tc, tg, chassis float64) {
	mode := "manual"
	if smart {
		mode = "smart"
	}
	cur := dm.curMode
	if cur == "" {
		cur = "?"
	}
	ac := dm.lastAC != nil && *dm.lastAC
	kind := dm.autoKind()
	live := map[string]any{
		"mode": mode, "activity": string(dm.activity), "fan": dm.writer.Written(), "tcpu": round(tc), "tgpu": round(tg), "chassis": round(chassis), "cpu": round(load),
		"gpu": round(gpuUtil), "gpuw": round(gpuPower), "rpm1": dm.rpm[0], "rpm2": dm.rpm[1], "cap": dm.gov.Cap / 1000, "power_mode": cur, "ac": ac, "auto": kind != "off",
		"auto_mode": kind, "fan_curve": dm.d.State.String("fan_curve", "native"), "smart_profile": dm.d.State.String("smart_profile", "native"), "mode_reason": dm.reason,
		"fan_off": dm.fanOff, "hw_ok": dm.hwOK,
	}
	dm.live = live
	var b strings.Builder
	for _, k := range liveKeys {
		b.WriteString(k + "=" + liveText(live[k]) + "\n")
	}
	paths := []string{}
	if dm.d.RunDir != "" {
		paths = append(paths, dm.d.RunDir+"/state")
	}
	if dm.d.TmpState != "" {
		paths = append(paths, dm.d.TmpState)
	}
	if dm.d.Config.Compat.PredatorWidgets {
		paths = append(paths, "/tmp/predator-state")
	}
	for _, p := range paths {
		_ = fsx.WriteAtomic(p, []byte(b.String()), 0o644) // a read-only /tmp or run directory is not worth stopping for
	}
}

func liveText(v any) string {
	switch x := v.(type) {
	case bool:
		if x {
			return "1"
		}
		return "0"
	case int:
		return strconv.Itoa(x)
	case string:
		return x
	}
	b, _ := json.Marshal(v)
	return string(b)
}
