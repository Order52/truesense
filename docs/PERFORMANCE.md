# Performance: what costs what

Measured on the Acer Predator Helios 16 (PH16-71, BIOS V1.18, CachyOS, Python 3.14, PyQt6). The point of this page: where the time really goes, so effort goes where it pays.

## The short version

TrueSense is **not CPU-bound**. Almost everything it waits for is the laptop's firmware or a USB device, so a faster language would not make those waits shorter.

| What | Cost | Who controls it |
|---|---|---|
| One command through the kernel module (`truesense_wmi`, C) with no firmware call | **about 2 microseconds** | already native speed |
| One firmware (ACPI/WMI) call, e.g. read the power mode | **5.0 ms** (each, always) | the BIOS: the method itself runs for 5 ms |
| Reading the fans (three firmware calls) | 15 ms | the BIOS |
| Opening the keyboard's HID interface | **50 ms** (each open) | the kernel + keyboard controller |
| A USB feature report to the keyboard (once open) | 0.3 ms | already fast |
| Sending one per-key frame (8 packets, paced) | about 188 ms | deliberate 16 ms pacing: unpaced writes lose the last row |
| The service (`truesense daemon`) while idle | **0.35 % of one core, 20 MB** | already small |
| Python start-up itself (`python3 -c pass`) | 17 ms | the interpreter |

So the module is already as fast as native code (about 2 microseconds of its own), and the remaining time is the firmware and the keyboard controller answering. Rewriting the Python in C++ or Rust would save microseconds out of milliseconds.

## What was worth fixing (and was fixed)

Things that were *ours*, found by measuring:

- **Opening the keyboard device on every operation (50 ms each).** A process now opens it once and keeps the handle (0.5 ms per read afterwards, 95 times faster), serialised with a lock, reopened once if it goes stale. The first open can run in the background (`keyboard.prewarm()`) while other work happens.
- **No bytecode cache in the installed copy.** The install folder is root-owned, so a normal user's Python could not write `.pyc` files and recompiled `gui.py` and 14 other modules on every launch. The installer (and the Arch packages) now compile as root; `truesense doctor` checks it.
- **Building every page before showing the window.** The window now builds Home, shows itself, and builds the other nine pages right after the first paint: 266 ms to a visible window became 92 ms.
- **Polling the firmware more than needed.** While idle the window reads only what changes by itself (the fans, and power mode and power source from the service); slow settings are re-read every 20 s, on focus, on opening a page or after a change. 55 firmware/USB calls per 10 s became 15.
- **Checks that waited for each other.** `truesense doctor` runs its checks side by side (691 ms to 272 ms warm).
- **Importing things nobody needs yet** (the HTTP stack for update checks) and **building the whole argument parser for one command**.

## Measured results (before, then now)

| | Before | Now |
|---|---|---|
| Launch: process start to visible window | 266 ms | 92 ms |
| `truesense --version` | 44 ms | 20 ms |
| `truesense fan status` | 65 ms | 56 ms |
| `truesense keyboard status` | 170 ms | 108 ms |
| `truesense status` | 241 ms | 137 ms |
| `truesense doctor` | 691 ms | 272 ms |
| Window: full hardware refresh | 212 ms | 115 ms |
| Window: idle firmware/USB calls per 10 s | 55 (594 ms) | 15 (134 ms) |
| Keyboard brightness read | 52 ms | 0.55 ms |
| Per-key animation frame | about 235 ms | about 188 ms |

## What is left, and why

- **The 5 ms per firmware call** is inside the BIOS. The only lever is making fewer calls, which is what the tiered refresh does.
- **The 50 ms first open of the keyboard** cannot be avoided in a one-shot command; it is hidden behind other work where there is any (`status`, window start).
- **The 188 ms per animation frame** is the pacing the keyboard needs. Shorter spacing is worth testing, but only by someone watching the keys: there is no read-back for LED colours.

## How to measure again

`make test` and the tests in `tests/` pin the behaviours above (one open per process, stale-handle reopen, no interleaving between threads, parallel `doctor`, lazy window build, the installer's bytecode step). For timings, run the command a few times and compare medians; the first call after a long idle includes cold caches.
