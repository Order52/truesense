/* A fake NVIDIA management library for testing tools/nvml/truesense-nvml.c: no GPU is touched.
 * FAKE_NVML_MODE: unset = a healthy GPU; "noinit" = nvmlInit_v2 fails; "nodevice" = no GPU;
 * "set4" = every write answers NVML_ERROR_NO_PERMISSION (4). The curve offset reads as 123456
 * so a test can tell this library from a real one before it writes anything. The offset is kept
 * in the file named by FAKE_NVML_STATE, because every helper run is a new process. */
#include <stdio.h>
#include <stdlib.h>
#include <string.h>

static int mode_is(const char *m) { const char *v = getenv("FAKE_NVML_MODE"); return v && !strcmp(v, m); }
static int read_offset(void)
{
	const char *p = getenv("FAKE_NVML_STATE");
	FILE *f = p ? fopen(p, "r") : NULL;
	int v = 123456;

	if (f) {
		if (fscanf(f, "%d", &v) != 1)
			v = 123456;
		fclose(f);
	}
	return v;
}

static void write_offset(int v)
{
	const char *p = getenv("FAKE_NVML_STATE");
	FILE *f = p ? fopen(p, "w") : NULL;

	if (f) {
		fprintf(f, "%d\n", v);
		fclose(f);
	}
}

int nvmlInit_v2(void) { return mode_is("noinit") ? 9 : 0; }
int nvmlShutdown(void) { return 0; }
int nvmlDeviceGetHandleByIndex_v2(unsigned int i, void **dev) { if (mode_is("nodevice") || i != 0) return 6; *dev = (void *)0x1; return 0; }
int nvmlDeviceGetGpcClkVfOffset(void *d, int *off) { (void)d; *off = read_offset(); return 0; }
int nvmlDeviceGetGpcClkMinMaxVfOffset(void *d, int *lo, int *hi) { (void)d; *lo = -300; *hi = 250; return 0; }
int nvmlDeviceSetGpcClkVfOffset(void *d, int off) { (void)d; if (mode_is("set4")) return 4; write_offset(off); return 0; }
int nvmlDeviceSetGpuLockedClocks(void *d, unsigned int lo, unsigned int hi) { (void)d; return (mode_is("set4") || lo > hi) ? (mode_is("set4") ? 4 : 2) : 0; }
int nvmlDeviceResetGpuLockedClocks(void *d) { (void)d; return mode_is("set4") ? 4 : 0; }
