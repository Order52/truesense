//go:build linux

package gpuundervolt

import (
	"errors"
	"fmt"
	"math"

	"github.com/ebitengine/purego"
)

// driverLibraries are the names the driver's management library goes by, tried in order.
// dlopen searches the system's library path, as ctypes.find_library did in the Python version.
var driverLibraries = []string{"libnvidia-ml.so.1", "libnvidia-ml.so"}

// errLibraryNotFound is what a machine without the NVIDIA driver gets. The wording is the
// Python implementation's, which users see.
var errLibraryNotFound = errors.New("NVIDIA driver library (libnvidia-ml) not found")

// OpenNVML loads the driver's NVML library (no cgo: purego calls it through dlopen),
// initialises it and takes the GPU at index 0. It fails cleanly, with a message that says the
// NVIDIA library was not found, on a machine without the driver.
func OpenNVML() (NVML, error) { return openNVML(driverLibraries) }

// nvmlSuccess is NVML_SUCCESS.
const nvmlSuccess = 0

// realNVML holds the resolved NVML entry points. The device handle is an opaque pointer that
// only the driver interprets.
type realNVML struct {
	dev uintptr

	getOffset   func(dev uintptr, offset *int32) int32
	getRange    func(dev uintptr, lo, hi *int32) int32 // nil on drivers without it
	setOffset   func(dev uintptr, offset int32) int32
	lockClocks  func(dev uintptr, minMHz, maxMHz uint32) int32
	resetClocks func(dev uintptr) int32 // nil on drivers without it
}

// openNVML is OpenNVML over the given library names; tests pass a name that does not exist to
// exercise the failure path without loading the real library.
func openNVML(libraries []string) (n NVML, err error) {
	var lib uintptr
	for _, name := range libraries {
		if lib, err = purego.Dlopen(name, purego.RTLD_NOW|purego.RTLD_GLOBAL); err == nil {
			break
		}
	}
	if err != nil {
		return nil, errLibraryNotFound
	}
	// purego panics when a symbol is missing; turn that into an error.
	defer func() {
		if r := recover(); r != nil {
			n, err = nil, fmt.Errorf("NVIDIA driver library has an unexpected interface: %v", r)
		}
	}()

	var initFn func() int32
	var handleByIndex func(index uint32, dev *uintptr) int32
	purego.RegisterLibFunc(&initFn, lib, "nvmlInit_v2")
	purego.RegisterLibFunc(&handleByIndex, lib, "nvmlDeviceGetHandleByIndex_v2")
	r := &realNVML{}
	purego.RegisterLibFunc(&r.getOffset, lib, "nvmlDeviceGetGpcClkVfOffset")
	purego.RegisterLibFunc(&r.setOffset, lib, "nvmlDeviceSetGpcClkVfOffset")
	purego.RegisterLibFunc(&r.lockClocks, lib, "nvmlDeviceSetGpuLockedClocks")
	// Older drivers lack these two; the package copes without them.
	if sym, err := purego.Dlsym(lib, "nvmlDeviceGetGpcClkMinMaxVfOffset"); err == nil {
		purego.RegisterFunc(&r.getRange, sym)
	}
	if sym, err := purego.Dlsym(lib, "nvmlDeviceResetGpuLockedClocks"); err == nil {
		purego.RegisterFunc(&r.resetClocks, sym)
	}

	if initFn() != nvmlSuccess {
		return nil, errors.New("could not start NVML (is the NVIDIA driver loaded?)")
	}
	if handleByIndex(0, &r.dev) != nvmlSuccess {
		return nil, errors.New("no NVIDIA GPU found")
	}
	return r, nil
}

func status(rc int32) error {
	if rc == nvmlSuccess {
		return nil
	}
	return Code(rc)
}

func (r *realNVML) Offset() (int, error) {
	var off int32
	if err := status(r.getOffset(r.dev, &off)); err != nil {
		return 0, err
	}
	return int(off), nil
}

func (r *realNVML) OffsetRange() (int, int, error) {
	if r.getRange == nil {
		return 0, 0, CodeNotSupported
	}
	var lo, hi int32
	if err := status(r.getRange(r.dev, &lo, &hi)); err != nil {
		return 0, 0, err
	}
	return int(lo), int(hi), nil
}

func (r *realNVML) SetOffset(mhz int) error {
	if mhz < math.MinInt32 || mhz > math.MaxInt32 {
		return CodeInvalidArgument
	}
	return status(r.setOffset(r.dev, int32(mhz)))
}

func (r *realNVML) LockClocks(minMHz, maxMHz uint) error {
	if uint64(minMHz) > math.MaxUint32 || uint64(maxMHz) > math.MaxUint32 {
		return CodeInvalidArgument
	}
	return status(r.lockClocks(r.dev, uint32(minMHz), uint32(maxMHz)))
}

func (r *realNVML) ResetClocks() error {
	if r.resetClocks == nil {
		return CodeNotSupported
	}
	return status(r.resetClocks(r.dev))
}
