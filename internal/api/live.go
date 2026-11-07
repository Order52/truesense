package api

import (
	"context"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/Order52/truesense/internal/hw"
)

// Live is one reading of everything the window shows live: cheap reads only, because it runs
// about once a second. A member that cannot be read is null.
type Live struct {
	T          int64        `json:"t"`
	Sensors    *hw.Sensors  `json:"sensors"`
	PowerMode  *string      `json:"power_mode"`
	AC         *bool        `json:"ac"`
	BatteryPct *int         `json:"battery_pct"`
	Fans       *hw.FanState `json:"fans"`
	Load       LiveLoad     `json:"load"`
	Power      LivePower    `json:"power"`
	Daemon     *DaemonState `json:"daemon"`
}

// LiveLoad is the CPU and GPU load in percent, as the service measures it.
type LiveLoad struct {
	CPUPct *float64 `json:"cpu_pct"`
	GPUPct *float64 `json:"gpu_pct"`
}

// LivePower is the CPU package power (from RAPL, when it can be read) and the GPU power (the
// service's reading), in watts.
type LivePower struct {
	CPUW *float64 `json:"cpu_w"`
	GPUW *float64 `json:"gpu_w"`
}

// raplMeter turns the cumulative RAPL energy counter into watts between two readings.
type raplMeter struct {
	mu     sync.Mutex
	last   float64 // microjoules
	lastAt time.Time
	have   bool
}

func (a *API) cpuPower() *float64 {
	b, err := os.ReadFile(filepath.Join(a.s.Power.SysRoot, "/sys/class/powercap/intel-rapl:0/energy_uj"))
	if err != nil {
		return nil
	}
	uj, err := strconv.ParseFloat(strings.TrimSpace(string(b)), 64)
	if err != nil {
		return nil
	}
	m, now := &a.rapl, a.Now()
	m.mu.Lock()
	defer m.mu.Unlock()
	prevUJ, prevAt, had := m.last, m.lastAt, m.have
	m.last, m.lastAt, m.have = uj, now, true
	dt := now.Sub(prevAt).Seconds()
	if !had || dt <= 0 || uj < prevUJ { // the first reading, or the counter wrapped
		return nil
	}
	w := (uj - prevUJ) / 1e6 / dt
	return &w
}

func liveNumber(m map[string]any, key string) *float64 {
	if f, ok := m[key].(float64); ok {
		return &f
	}
	return nil
}

// Live reads the window's live values. The service's own readings are used when it runs (it
// has them this second already); the hardware is read otherwise.
func (a *API) Live(ctx context.Context) Live {
	l := Live{T: a.Now().Unix()}
	var live map[string]any
	if a.DaemonAvailable() {
		if d, err := a.DaemonState(); err == nil {
			l.Daemon, live = &d, d.Live
		}
	}
	_, hasRPM := live["rpm1"]
	_, hasT := live["tcpu"]
	if hasRPM && hasT {
		tcpu, tgpu, chassis := int(*liveNumber(live, "tcpu")), 0, 0
		if v := liveNumber(live, "tgpu"); v != nil {
			tgpu = int(*v)
		}
		if v := liveNumber(live, "chassis"); v != nil {
			chassis = int(*v)
		}
		rpm1, rpm2 := 0, 0
		if v := liveNumber(live, "rpm1"); v != nil {
			rpm1 = int(*v)
		}
		if v := liveNumber(live, "rpm2"); v != nil {
			rpm2 = int(*v)
		}
		l.Sensors = &hw.Sensors{CPUTemp: tcpu, SysTemp: chassis, GPUTemp: tgpu, CPUFanRPM: rpm1, GPUFanRPM: rpm2}
	} else if s, err := a.s.HW.ReadSensors(); err == nil {
		l.Sensors = &s
	}
	if m, _ := live["power_mode"].(string); m != "" && m != "?" {
		l.PowerMode = &m
	} else if m, err := a.s.HW.GetMode(); err == nil {
		l.PowerMode = &m
	}
	if ac, ok := live["ac"].(bool); ok {
		l.AC = &ac
	} else {
		ac := a.s.HW.OnAC()
		l.AC = &ac
	}
	if s, ok := a.Supply(); ok {
		l.BatteryPct = &s.Percent
	}
	if f, err := a.s.HW.GetFanState(); err == nil {
		l.Fans = &f
	}
	l.Load = LiveLoad{CPUPct: liveNumber(live, "cpu"), GPUPct: liveNumber(live, "gpu")}
	l.Power = LivePower{CPUW: a.cpuPower(), GPUW: liveNumber(live, "gpuw")}
	return l
}
