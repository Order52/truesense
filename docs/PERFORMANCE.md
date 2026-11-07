# Performance: what costs what

Measured on the Acer Predator Helios 16 (PH16-71, BIOS V1.18, CachyOS) with TrueSense 0.4.0 (Go) and compared with 0.3.17 (Python, the last release before the port). The point of this page: where the time really goes, so effort goes where it pays.

## The short version

TrueSense is **not CPU-bound**. Almost everything it waits for is the laptop's firmware or a USB device, so a faster language does not make those waits shorter. What the Go port changed is everything around them: start-up, the number of runtimes and the size of the install.

| What | Cost | Who controls it |
|---|---|---|
| One command through the kernel module (`truesense_wmi`, C) with no firmware call | **about 2 microseconds** | already native speed |
| One firmware (ACPI/WMI) call, e.g. read the power mode | **5.0 ms** (each, always) | the BIOS: the method itself runs for 5 ms |
| Reading the fans (three firmware calls) | 15 ms | the BIOS |
| Opening the keyboard's HID interface | **50 ms** (each open) | the kernel + keyboard controller |
| A USB feature report to the keyboard (once open) | 0.3 ms | already fast |
| Sending one per-key frame (8 packets, paced) | about 188 ms | deliberate 16 ms pacing: unpaced writes lose the last row |
| Start-up of the program itself (`truesense --version`) | **4 ms** (Python: 20 ms at best, 44 ms before tuning) | a static binary |

## Measured results: Python 0.3.17 and Go 0.4.0 on the same laptop

Commands (median of 20 to 40 runs, real hardware, service running):

| | Python 0.3.17 | Go 0.4.0 |
|---|---|---|
| `truesense --version` | 20 ms | **4.3 ms** |
| `truesense fan status` | 56 ms | **29.8 ms** |
| `truesense keyboard status` | 108 ms | **8.2 ms** |
| `truesense status` | 137 ms | **107 ms** |
| `truesense doctor` | 272 ms | **106 ms** |

The long-running processes (`truesense daemon`, the key service, the audio watcher), as installed, with the units' `GOMAXPROCS=2 GOGC=25`:

| | Python 0.3.17 | Go 0.4.0 |
|---|---|---|
| Daemon, resident memory | 20 MB | **14.4 MB** (5.6 MB private, the rest is the shared program file) |
| Key service | 19 MB | **16.3 MB** |
| Audio watcher | 20 MB | **14.0 MB** |
| The three together | 58.7 MB | **44.6 MB**, and the three share one program file in memory |
| Daemon, idle CPU | about 0.4 to 0.6 % of one core | about 0.5 % of one core (it wakes once a second and the firmware calls are most of that) |
| Installed size | a Python tree plus Python itself | one 11 MB static file, a 16 KB helper, no Python, no Qt |

The honest reading: always-on CPU use is about the same (the daemon's work is mostly waiting for the firmware, and that is not CPU), memory is a quarter to a third lower, and a command that starts is several times faster. The Go runtime's defaults suit big servers, so the units set two workers and a tight heap; the same settings measured on an unmanaged run cut the private memory from 8.8 MB to 5.7 MB.

## What was worth fixing, and was fixed

- **Opening the keyboard device on every operation (50 ms each).** A process opens it once and keeps the handle (0.5 ms per read afterwards), serialised with a lock, reopened once if it goes stale.
- **Polling the firmware more than needed.** The window reads only what changes by itself; slow settings are re-read on focus, on opening a page or after a change.
- **Checks that waited for each other.** `truesense doctor` runs its checks side by side (a worker pool), and its DKMS checks read DKMS's own directories instead of running `dkms status`.
- **A run of key presses or a drag was many writes.** Writes go through one ordered queue and a newer value for the same setting replaces an older one that has not started.
- **The GPU undervolt's NVIDIA call could hang the service.** It runs in a small helper with a timeout (which also keeps the main binary static).

## What is left, and why

- **The 5 ms per firmware call** is inside the BIOS. The only lever is making fewer calls, which the tiered refresh does.
- **The 50 ms first open of the keyboard** cannot be avoided in a one-shot command.
- **The 188 ms per animation frame** is the pacing the keyboard needs. Shorter spacing is worth testing, but only by someone watching the keys: there is no read-back for LED colours.
- **`truesense status` (107 ms)** asks the sound system and several firmware features; each question is a separate firmware or process call.

## How to measure again

`go test ./...` pins the behaviours above: one open per process, the ordered write queue, the helper's timeout. For timings, run the command a few times and compare medians; for memory, read `VmRSS` and `RssAnon` from `/proc/<pid>/status` of the running processes.
