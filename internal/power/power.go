// Package power reads and sets the CPU package power limits (Intel RAPL PL1/PL2 through the
// powercap sysfs) and the NVIDIA GPU power limit (through nvidia-smi), and rates a PL1/PL2
// pair as safe, caution or danger.
//
// Limits are range-checked, a write is read back and verified (the previous limits are put
// back if it did not take), and nothing here runs by itself: the daemon decides when to call.
// Every external effect is injected: the sysfs root, the MSR reader and the nvidia-smi runner.
package power

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/Order52/truesense/internal/firmware"
)

// The range of PL1/PL2 this laptop accepts (13700HX, stock 80 W): anything outside is refused.
const (
	MinW = 25
	MaxW = 120
)

const (
	pl1File = "constraint_0_power_limit_uw"
	pl2File = "constraint_1_power_limit_uw"

	// msrPkgPowerLimit is MSR_PKG_POWER_LIMIT; its bit 63 locks the limits until reboot.
	msrPkgPowerLimit = 0x610
	nvidiaTimeout    = 5 * time.Second
)

// MSRReader reads one model-specific register of CPU 0 (the undervolt package's DevMSR is
// one). It is an interface so tests never touch /dev/cpu.
type MSRReader interface {
	Read(offset uint32) (uint64, error)
}

// Result is what a finished external program printed and returned.
type Result struct {
	Stdout   string
	Stderr   string
	ExitCode int
}

// Runner runs an external program (nvidia-smi). The error is for "could not run at all"
// (missing binary, timeout); a program that ran and failed is a Result with ExitCode != 0.
type Runner func(ctx context.Context, name string, args ...string) (Result, error)

// Controller reaches the power-limit hardware through injected dependencies.
type Controller struct {
	// SysRoot is the prefix of /sys (empty means the real "/sys"); tests point it at a temp tree.
	SysRoot string
	// MSR reads MSR_PKG_POWER_LIMIT for the lock bit; nil reports it as unreadable.
	MSR MSRReader
	// Run runs nvidia-smi; nil disables the NVIDIA parts.
	Run Runner
	// WriteFile overwrites a sysfs attribute; nil uses the real file system. Tests replace it
	// to play firmware that clamps or ignores a write.
	WriteFile func(path, value string) error
}

func (c *Controller) raplDir() string {
	root := c.SysRoot
	if root == "" {
		root = "/sys"
	}
	return filepath.Join(root, "class", "powercap", "intel-rapl:0")
}

// NvidiaLimits are the GPU power limits in watts, as nvidia-smi prints them (text, because
// a driver may print "[N/A]").
type NvidiaLimits struct {
	Current string `json:"current"`
	Default string `json:"default"`
	Min     string `json:"min"`
	Max     string `json:"max"`
}

// Info is what the daemon reports about the power limits. The JSON names are the Python dict
// keys. Unreadable values are null; MSRLockBit is true/false, or text explaining why the
// register could not be read.
type Info struct {
	PL1W        *float64      `json:"pl1_w"`
	PL2W        *float64      `json:"pl2_w"`
	RAPLEnabled *string       `json:"rapl_enabled"`
	MSRLockBit  any           `json:"msr_lock_bit"`
	NvidiaLimit *NvidiaLimits `json:"nvidia_power_limit_w"`
}

func readTrim(path string) (string, bool) {
	b, err := os.ReadFile(path)
	if err != nil {
		return "", false
	}
	return strings.TrimSpace(string(b)), true
}

func readMicrowatts(path string) (int64, bool) {
	s, ok := readTrim(path)
	if !ok || s == "" {
		return 0, false
	}
	v, err := strconv.ParseInt(s, 10, 64)
	return v, err == nil
}

func watts(uw int64, ok bool) *float64 {
	if !ok {
		return nil
	}
	w := float64(uw) / 1e6
	return &w
}

// Info reads the current limits. Every part is best effort: a missing RAPL directory, an
// unreadable MSR or an absent nvidia-smi leaves that part null rather than failing.
func (c *Controller) Info(ctx context.Context) Info {
	dir := c.raplDir()
	var out Info
	out.PL1W = watts(readMicrowatts(filepath.Join(dir, pl1File)))
	out.PL2W = watts(readMicrowatts(filepath.Join(dir, pl2File)))
	if s, ok := readTrim(filepath.Join(dir, "enabled")); ok {
		out.RAPLEnabled = &s
	}
	out.MSRLockBit = "unreadable (needs root + msr module)"
	if c.MSR != nil {
		if v, err := c.MSR.Read(msrPkgPowerLimit); err == nil {
			out.MSRLockBit = v>>63 != 0
		}
	}
	out.NvidiaLimit = c.nvidiaLimits(ctx)
	return out
}

func (c *Controller) nvidiaLimits(ctx context.Context) *NvidiaLimits {
	if c.Run == nil {
		return nil
	}
	ctx, cancel := context.WithTimeout(ctx, nvidiaTimeout)
	defer cancel()
	r, err := c.Run(ctx, "nvidia-smi", "--query-gpu=power.limit,power.default_limit,power.min_limit,power.max_limit", "--format=csv,noheader,nounits")
	if err != nil || r.ExitCode != 0 {
		return nil
	}
	line, _, _ := strings.Cut(strings.TrimSpace(r.Stdout), "\n") // the first GPU
	f := strings.Split(line, ",")
	if len(f) < 4 {
		return nil
	}
	return &NvidiaLimits{Current: strings.TrimSpace(f[0]), Default: strings.TrimSpace(f[1]), Min: strings.TrimSpace(f[2]), Max: strings.TrimSpace(f[3])}
}

// RawLimits are PL1 and PL2 in microwatts, exactly as RAPL stores them. The daemon uses them
// to remember what the firmware set and to put it back.
type RawLimits struct {
	PL1 int64
	PL2 int64
}

// ReadRaw returns the current limits in microwatts, or an error when RAPL cannot be read.
func (c *Controller) ReadRaw() (RawLimits, error) {
	dir := c.raplDir()
	pl1, ok1 := readMicrowatts(filepath.Join(dir, pl1File))
	pl2, ok2 := readMicrowatts(filepath.Join(dir, pl2File))
	if !ok1 || !ok2 {
		return RawLimits{}, fmt.Errorf("read RAPL limits: %s not readable", dir)
	}
	return RawLimits{PL1: pl1, PL2: pl2}, nil
}

// WriteRaw writes both limits (microwatts) without a range check or read-back. It is for
// putting back values the firmware itself chose; use SetLimits for anything a user asked for.
func (c *Controller) WriteRaw(l RawLimits) error {
	dir := c.raplDir()
	write := c.WriteFile
	if write == nil {
		write = overwrite
	}
	if err := write(filepath.Join(dir, pl1File), strconv.FormatInt(l.PL1, 10)); err != nil {
		return fmt.Errorf("write RAPL PL1: %w", err)
	}
	if err := write(filepath.Join(dir, pl2File), strconv.FormatInt(l.PL2, 10)); err != nil {
		return fmt.Errorf("write RAPL PL2: %w", err)
	}
	return nil
}

// overwrite overwrites an existing (sysfs) file; it never creates one.
func overwrite(path, value string) error {
	f, err := os.OpenFile(path, os.O_WRONLY|os.O_TRUNC, 0)
	if err != nil {
		return err
	}
	if _, err := f.WriteString(value); err != nil {
		_ = f.Close()
		return err
	}
	return f.Close()
}

// Applied is the verified result of SetLimits.
type Applied struct {
	PL1W float64 `json:"pl1_w"`
	PL2W float64 `json:"pl2_w"`
}

// SetLimits sets PL1 (sustained) and PL2 (burst) in watts. Both must be MinW..MaxW with
// PL2 >= PL1. The values are read back; if the hardware did not keep them, or a write failed
// half way, the previous limits are put back and an error is returned.
func (c *Controller) SetLimits(pl1, pl2 float64) (Applied, error) {
	if !(MinW <= pl1 && pl1 <= MaxW && MinW <= pl2 && pl2 <= MaxW && pl2 >= pl1) {
		return Applied{}, firmware.Errorf("PL1/PL2 must be %d..%d W with PL2 >= PL1", MinW, MaxW)
	}
	// Without the current limits there would be nothing to go back to, so do not start.
	old, err := c.ReadRaw()
	if err != nil {
		return Applied{}, fmt.Errorf("cannot write RAPL (needs root, or the daemon): %w", err)
	}
	if err := c.WriteRaw(RawLimits{PL1: int64(pl1 * 1e6), PL2: int64(pl2 * 1e6)}); err != nil {
		_ = c.WriteRaw(old) // a half-written pair would leave PL1 and PL2 inconsistent
		return Applied{}, fmt.Errorf("cannot write RAPL (needs root, or the daemon): %w", err)
	}
	got, err := c.ReadRaw()
	if err != nil {
		_ = c.WriteRaw(old)
		return Applied{}, fmt.Errorf("verify the power limits: %w", err)
	}
	g1, g2 := float64(got.PL1)/1e6, float64(got.PL2)/1e6
	if g1 != pl1 || g2 != pl2 {
		_ = c.WriteRaw(old)
		return Applied{}, firmware.Errorf("readback (%g, %g) != requested (%g, %g); restored previous limits", g1, g2, pl1, pl2)
	}
	return Applied{PL1W: g1, PL2W: g2}, nil
}

// SetNvidiaLimit sets the GPU power limit in watts through nvidia-smi (the driver enforces
// its own min and max). It returns what nvidia-smi printed.
func (c *Controller) SetNvidiaLimit(ctx context.Context, w float64) (string, error) {
	if c.Run == nil {
		return "", errors.New("nvidia-smi is not available")
	}
	if !(w >= 1 && w <= 1000) {
		return "", firmware.Errorf("GPU power limit must be 1..1000 W")
	}
	r, err := c.Run(ctx, "nvidia-smi", "-pl", strconv.Itoa(int(w)))
	if err != nil {
		return "", fmt.Errorf("run nvidia-smi: %w", err)
	}
	if r.ExitCode != 0 {
		return "", firmware.Errorf("%s", strings.TrimSpace(r.Stdout+r.Stderr))
	}
	return strings.TrimSpace(r.Stdout), nil
}
