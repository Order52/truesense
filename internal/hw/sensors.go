package hw

import (
	"context"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

func (h *HW) sys(p string) string { return filepath.Join(h.SysRoot, p) }

// CPUTemp is the CPU package temperature in degrees C from the coretemp hwmon (the hottest
// core sensor), or ok=false when there is none.
func (h *HW) CPUTemp() (temp float64, ok bool) {
	hwmons, _ := filepath.Glob(h.sys("/sys/class/hwmon/hwmon*"))
	for _, dir := range hwmons {
		name, err := os.ReadFile(filepath.Join(dir, "name"))
		if err != nil || strings.TrimSpace(string(name)) != "coretemp" {
			continue
		}
		inputs, _ := filepath.Glob(filepath.Join(dir, "temp*_input"))
		for _, in := range inputs {
			b, err := os.ReadFile(in)
			if err != nil {
				continue
			}
			v, err := strconv.Atoi(strings.TrimSpace(string(b)))
			if err != nil {
				continue
			}
			if t := float64(v) / 1000; !ok || t > temp {
				temp, ok = t, true
			}
		}
	}
	return temp, ok
}

// GPUTemp is the NVIDIA GPU temperature from nvidia-smi, or ok=false when it cannot be read.
func (h *HW) GPUTemp() (temp float64, ok bool) {
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	out, err := h.Exec(ctx, "nvidia-smi", "--query-gpu=temperature.gpu", "--format=csv,noheader,nounits")
	if err != nil {
		return 0, false
	}
	f := strings.Fields(string(out))
	if len(f) == 0 {
		return 0, false
	}
	v, err := strconv.ParseFloat(f[0], 64)
	return v, err == nil
}

// OnAC reports whether the laptop is on mains power (true when no supply says otherwise).
func (h *HW) OnAC() bool {
	for _, n := range []string{"ACAD", "AC", "AC0", "ADP0", "ADP1"} {
		b, err := os.ReadFile(h.sys("/sys/class/power_supply/" + n + "/online"))
		if err == nil {
			return strings.TrimSpace(string(b)) == "1"
		}
	}
	return true
}
