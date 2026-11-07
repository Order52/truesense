package gpuundervolt

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

// HelperName is the helper program that talks to NVIDIA's management library.
//
// The daemon is a static program and cannot load the driver's library itself, and it must not
// try: a call that hangs inside the NVIDIA driver would block the root service. The helper
// (tools/nvml/truesense-nvml.c, built by the installer) does one NVML call per run for the GPU
// at index 0 and prints one line; the daemon runs it with a timeout.
const HelperName = "truesense-nvml"

// CallTimeout bounds one helper run. NVML answers in milliseconds; a driver that does not
// answer in this time is reported as an error instead of hanging the service.
const CallTimeout = 10 * time.Second

// errLibraryNotFound is what a machine without the NVIDIA driver gets. The wording is the
// Python implementation's, which users see.
var errLibraryNotFound = errors.New("NVIDIA driver library (libnvidia-ml) not found")

// ErrHelperMissing means the helper program is not installed.
var ErrHelperMissing = errors.New("the GPU helper (" + HelperName + ") is not installed: run the TrueSense installer again (GPU undervolt needs it)")

// RunFunc runs the helper with arguments and returns its standard output.
type RunFunc func(ctx context.Context, path string, args ...string) (string, error)

// ExecRun is the real RunFunc.
func ExecRun(ctx context.Context, path string, args ...string) (string, error) {
	out, err := exec.CommandContext(ctx, path, args...).Output()
	return string(out), err
}

// HelperPath finds the helper: TRUESENSE_NVML, next to the program under ../lib/truesense, then
// the installed places.
func HelperPath() (string, error) {
	var candidates []string
	if p := os.Getenv("TRUESENSE_NVML"); p != "" {
		candidates = append(candidates, p)
	}
	if exe, err := os.Executable(); err == nil {
		if real, err := filepath.EvalSymlinks(exe); err == nil {
			exe = real
		}
		candidates = append(candidates, filepath.Join(filepath.Dir(exe), "..", "lib", "truesense", HelperName))
	}
	candidates = append(candidates, "/usr/local/lib/truesense/"+HelperName, "/usr/lib/truesense/"+HelperName)
	for _, c := range candidates {
		if st, err := os.Stat(c); err == nil && !st.IsDir() && st.Mode()&0o111 != 0 { //nolint:gosec // candidates are fixed install locations and an explicit override, never request input
			return c, nil
		}
	}
	return "", ErrHelperMissing
}

// helperNVML implements NVML by running the helper once per call.
type helperNVML struct {
	path    string
	run     RunFunc
	timeout time.Duration
}

// OpenNVML returns the helper-backed NVML for the GPU at index 0. It fails when the helper is not
// installed; whether a driver exists is found out by the first call (the helper says "library").
func OpenNVML() (NVML, error) {
	p, err := HelperPath()
	if err != nil {
		return nil, err
	}
	return &helperNVML{path: p, run: ExecRun, timeout: CallTimeout}, nil
}

// newHelper is the constructor tests use, with a fake runner.
func newHelper(path string, run RunFunc, timeout time.Duration) *helperNVML {
	return &helperNVML{path: path, run: run, timeout: timeout}
}

func (h *helperNVML) call(args ...string) ([]int, error) {
	ctx, cancel := context.WithTimeout(context.Background(), h.timeout)
	defer cancel()
	out, err := h.run(ctx, h.path, args...)
	if ctx.Err() != nil {
		return nil, fmt.Errorf("the NVIDIA driver did not answer within %v", h.timeout)
	}
	if err != nil {
		return nil, fmt.Errorf("the GPU helper failed: %w", err)
	}
	return parseReply(out)
}

// parseReply reads the helper's one-line answer: "ok [numbers]" or "err <what>". The input comes
// from another program, so every shape is checked.
func parseReply(out string) ([]int, error) {
	line := strings.TrimSpace(out)
	f := strings.Fields(line)
	if len(f) == 0 {
		return nil, errors.New("the GPU helper gave no answer")
	}
	switch f[0] {
	case "ok":
		nums := make([]int, 0, len(f)-1)
		for _, w := range f[1:] {
			n, err := strconv.Atoi(w)
			if err != nil {
				return nil, fmt.Errorf("the GPU helper gave an unreadable answer: %q", line)
			}
			nums = append(nums, n)
		}
		return nums, nil
	case "err":
		return nil, helperError(f[1:], line)
	}
	return nil, fmt.Errorf("the GPU helper gave an unreadable answer: %q", line)
}

func helperError(f []string, line string) error {
	if len(f) == 0 {
		return fmt.Errorf("the GPU helper gave an unreadable answer: %q", line)
	}
	switch f[0] {
	case "code":
		if len(f) == 2 {
			if n, err := strconv.Atoi(f[1]); err == nil {
				return Code(n)
			}
		}
	case "library":
		return errLibraryNotFound
	case "init":
		return errors.New("could not start NVML (is the NVIDIA driver loaded?)")
	case "nodevice":
		return errors.New("no NVIDIA GPU found")
	case "usage":
		return CodeInvalidArgument
	}
	return fmt.Errorf("the GPU helper gave an unreadable answer: %q", line)
}

func one(nums []int, err error) (int, error) {
	if err != nil {
		return 0, err
	}
	if len(nums) != 1 {
		return 0, fmt.Errorf("the GPU helper gave an unexpected answer (%d numbers)", len(nums))
	}
	return nums[0], nil
}

// Offset implements NVML.
func (h *helperNVML) Offset() (int, error) { return one(h.call("offset")) }

// OffsetRange implements NVML.
func (h *helperNVML) OffsetRange() (int, int, error) {
	nums, err := h.call("range")
	if err != nil {
		return 0, 0, err
	}
	if len(nums) != 2 {
		return 0, 0, fmt.Errorf("the GPU helper gave an unexpected answer (%d numbers)", len(nums))
	}
	return nums[0], nums[1], nil
}

// SetOffset implements NVML.
func (h *helperNVML) SetOffset(mhz int) error {
	_, err := h.call("set-offset", strconv.Itoa(mhz))
	return err
}

// LockClocks implements NVML.
func (h *helperNVML) LockClocks(minMHz, maxMHz uint) error {
	_, err := h.call("lock", strconv.FormatUint(uint64(minMHz), 10), strconv.FormatUint(uint64(maxMHz), 10))
	return err
}

// ResetClocks implements NVML.
func (h *helperNVML) ResetClocks() error {
	_, err := h.call("reset-clocks")
	return err
}
