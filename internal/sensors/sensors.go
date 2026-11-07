// Package sensors reads what the daemon decides from: CPU load (from /proc/stat), the CPU
// package temperature (coretemp), the battery level and the GPU's temperature, load and power
// (streamed from nvidia-smi only when something smart needs it). Every file path and process
// is injectable, so the readings are testable without a laptop.
package sensors

import (
	"bufio"
	"context"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"
)

// EMA is an exponential moving average: the first sample is taken as it is, each later one
// moves the average by Alpha of the difference (smoothing a jittery reading without a delay
// long enough to hide a real rise).
type EMA struct {
	Alpha float64
	v     float64
	set   bool
}

// Update folds in a sample and returns the new average.
func (e *EMA) Update(x float64) float64 {
	if !e.set {
		e.v, e.set = x, true
	} else {
		e.v += e.Alpha * (x - e.v)
	}
	return e.v
}

// Value returns the average and whether any sample has been seen.
func (e *EMA) Value() (float64, bool) { return e.v, e.set }

// Reading is one sample of the GPU.
type Reading struct {
	Temp, Util, Power float64
	Seen              time.Time
}

// Sampler holds the state needed to turn raw counters into readings.
type Sampler struct {
	Root  string           // prefix of /proc and /sys (empty = the real ones)
	Now   func() time.Time // injectable clock
	Spawn func(ctx context.Context, name string, args ...string) (io.ReadCloser, func() error, error)

	mu                  sync.Mutex
	prevTotal, prevIdle uint64
	haveProc            bool
	load                EMA
	util, power         EMA
	gpuTemp             float64
	gpuSeen             time.Time
	gpuStarted          bool
}

// New returns a Sampler for the real machine.
func New() *Sampler {
	return &Sampler{Now: time.Now, Spawn: spawnProcess, load: EMA{Alpha: 0.25}, util: EMA{Alpha: 0.3}, power: EMA{Alpha: 0.3}}
}

func (s *Sampler) path(p string) string { return filepath.Join(s.Root, p) }

func spawnProcess(ctx context.Context, name string, args ...string) (io.ReadCloser, func() error, error) {
	cmd := exec.CommandContext(ctx, name, args...)
	out, err := cmd.StdoutPipe()
	if err != nil {
		return nil, nil, err
	}
	if err := cmd.Start(); err != nil {
		return nil, nil, err
	}
	return out, cmd.Wait, nil
}

// CPULoad samples /proc/stat and returns the smoothed CPU busy percentage (0 until two samples exist).
func (s *Sampler) CPULoad() float64 {
	s.mu.Lock()
	defer s.mu.Unlock()
	b, err := os.ReadFile(s.path("/proc/stat"))
	if err != nil {
		v, _ := s.load.Value()
		return v
	}
	line, _, _ := strings.Cut(string(b), "\n")
	f := strings.Fields(line)
	if len(f) < 9 {
		v, _ := s.load.Value()
		return v
	}
	var total, idle uint64
	for i, w := range f[1:9] {
		n, err := strconv.ParseUint(w, 10, 64)
		if err != nil {
			v, _ := s.load.Value()
			return v
		}
		total += n
		if i == 3 || i == 4 { // idle + iowait
			idle += n
		}
	}
	if s.haveProc && total > s.prevTotal {
		busy := 100 * (1 - float64(idle-s.prevIdle)/float64(total-s.prevTotal))
		s.load.Update(busy)
	}
	s.prevTotal, s.prevIdle, s.haveProc = total, idle, true
	v, _ := s.load.Value()
	return v
}

// PackageTemp is the CPU package temperature in degrees C from the coretemp hwmon, if present.
func (s *Sampler) PackageTemp() (float64, bool) {
	hwmons, _ := filepath.Glob(s.path("/sys/class/hwmon/hwmon*"))
	for _, dir := range hwmons {
		name, err := os.ReadFile(filepath.Join(dir, "name"))
		if err != nil || strings.TrimSpace(string(name)) != "coretemp" {
			continue
		}
		b, err := os.ReadFile(filepath.Join(dir, "temp1_input"))
		if err != nil {
			continue
		}
		if v, err := strconv.Atoi(strings.TrimSpace(string(b))); err == nil {
			return float64(v) / 1000, true
		}
	}
	return 0, false
}

// BatteryPct is the battery charge in percent (100 when there is no battery to ask).
func (s *Sampler) BatteryPct() float64 {
	caps, _ := filepath.Glob(s.path("/sys/class/power_supply/BAT*/capacity"))
	for _, p := range caps {
		if b, err := os.ReadFile(p); err == nil {
			if v, err := strconv.ParseFloat(strings.TrimSpace(string(b)), 64); err == nil {
				return v
			}
		}
	}
	return 100
}

// GPU returns the latest GPU reading (the zero Reading until the stream has produced one).
func (s *Sampler) GPU() Reading {
	s.mu.Lock()
	defer s.mu.Unlock()
	u, _ := s.util.Value()
	p, _ := s.power.Value()
	return Reading{Temp: s.gpuTemp, Util: u, Power: p, Seen: s.gpuSeen}
}

// StartGPU starts the nvidia-smi stream (once). It restarts the process when it ends, after a
// pause, until ctx is cancelled; a machine without nvidia-smi just never produces readings.
func (s *Sampler) StartGPU(ctx context.Context) {
	s.mu.Lock()
	if s.gpuStarted {
		s.mu.Unlock()
		return
	}
	s.gpuStarted = true
	s.mu.Unlock()
	go func() {
		for ctx.Err() == nil {
			s.streamOnce(ctx)
			select {
			case <-ctx.Done():
				return
			case <-time.After(5 * time.Second):
			}
		}
	}()
}

func (s *Sampler) streamOnce(ctx context.Context) {
	out, wait, err := s.Spawn(ctx, "nvidia-smi", "--query-gpu=temperature.gpu,utilization.gpu,power.draw", "--format=csv,noheader,nounits", "-l", "2")
	if err != nil {
		return
	}
	defer func() { _ = out.Close(); _ = wait() }()
	sc := bufio.NewScanner(out)
	for sc.Scan() {
		s.ParseGPULine(sc.Text())
	}
}

// ParseGPULine folds one line of "temp, util, power" into the readings; malformed lines are ignored.
func (s *Sampler) ParseGPULine(line string) bool {
	f := strings.Split(line, ",")
	if len(f) != 3 {
		return false
	}
	var v [3]float64
	for i := range f {
		x, err := strconv.ParseFloat(strings.TrimSpace(f[i]), 64)
		if err != nil {
			return false
		}
		v[i] = x
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.gpuTemp = v[0]
	s.util.Update(v[1])
	s.power.Update(v[2])
	s.gpuSeen = s.Now()
	return true
}
