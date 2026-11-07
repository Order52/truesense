//go:build !linux

package gpuundervolt

import "errors"

// OpenNVML is only implemented on Linux, where the NVIDIA driver library is loaded without cgo.
func OpenNVML() (NVML, error) {
	return nil, errors.New("NVIDIA driver library (libnvidia-ml) not found: GPU undervolt is only supported on Linux")
}
