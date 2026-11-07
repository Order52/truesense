//go:build linux

package gpuundervolt

import (
	"strings"
	"testing"
)

// The real loader is only ever pointed at a library name that cannot exist: this test must
// pass the same on a machine with and without an NVIDIA driver, and must never load the
// real libnvidia-ml.
func TestOpenNVMLFailsCleanlyWhenTheLibraryIsAbsent(t *testing.T) {
	n, err := openNVML([]string{"libtruesense-test-no-such-library.so.0"})
	if err == nil || n != nil {
		t.Fatalf("openNVML = %v, %v; want an error", n, err)
	}
	if !strings.Contains(err.Error(), "NVIDIA driver library (libnvidia-ml) not found") {
		t.Errorf("message = %q", err)
	}
	if n, err := openNVML(nil); err == nil || n != nil {
		t.Errorf("no candidate names must fail: %v, %v", n, err)
	}
}

func TestDriverLibraryNames(t *testing.T) {
	if len(driverLibraries) == 0 || driverLibraries[0] != "libnvidia-ml.so.1" {
		t.Errorf("driverLibraries = %v", driverLibraries)
	}
}

func TestStatusMapsReturnCodes(t *testing.T) {
	if status(nvmlSuccess) != nil {
		t.Error("success is not an error")
	}
	if err := status(4); err == nil || err.Error() != "needs root (the TrueSense service)" {
		t.Errorf("status(4) = %v", err)
	}
}
