# Release notes

## 0.3.0 (2026-10-31): production release

Tested on one machine, the **Acer Predator Helios 16 PH16-71** (BIOS V1.18, CachyOS, kernels 7.3.0-4 and 6.18 LTS). [docs/STATUS.md](docs/STATUS.md) says exactly what was verified on hardware and what is only covered by simulator tests. **The kernel module changed (fans-off), so the DKMS version is 0.3.0: re-run `sudo tools/install.sh`.**

### Highlights
* **A fresh install behaves like PredatorSense on Windows.** Fans on the firmware's own control, Native auto power mode, nothing of ours touching clocks or power limits until you choose a smart feature. Every profile kind has a read-only `native` entry that is that default.
* **One window** (`truesense gui`, Qt 6): a compact home page and clean pages for Lighting (keyboard, per-key on the real layout, rear bar, profiles), fan curves, Smart auto profiles, the PREDATOR key, undervolt (CPU and GPU), battery, audio and settings. Live updates, follows changes made elsewhere, the wheel never changes a setting by accident.
* **Smart auto** (heat, load and battery aware, with profiles) and **Native auto**; a mode you pick ends by itself when the situation changes.
* **Fans**: Smart curves (built-in, your own, drag-to-edit), manual with separate CPU/GPU sliders, and Off with a temperature watchdog.
* **Undervolt** for CPU and GPU with profiles, safety zones and typed confirmation beyond the danger line, power limits the service keeps, and a crash-guarded apply at boot.
* **PREDATOR key editor** with profiles, any action, and your own scripts through a file picker. Lighting profiles applied by the service at boot and after suspend. Per-key animations.

### Added since 0.2.0
Everything above, plus: `truesense doctor` checks for the service version, the window and status-bar support; `truesense battery full`; `truesense look`; `truesense keys profile`; `truesense power smart`; `truesense fan curve`; `truesense cpu undervolt` / `gpu undervolt`; the system check and report in Settings; `tools/screenshots.py`; CI; SAFETY, ARCHITECTURE and TROUBLESHOOTING documents.

### Changed
* The service no longer rewrites your power limits every few seconds; it keeps yours until you go back to automatic, and hands the firmware's values back when you switch smart features off.
* The GPU sampler (`nvidia-smi`) starts only when a smart feature needs it; the service paces itself at 2 s when idle.
* The keyboard's own 30-second timer (a separate controller flag, on by default) is now controlled; the old setting only changed the firmware flag.
* Defaults: smart fan off, Native auto on, hold time for a manual pick 10 minutes.

### Fixed
* Headset mic boost read the channel count instead of the value (every change looked like 2 and failed).
* Slider drags applied an older value instead of the newest; refreshes could move a slider while dragged; the window could crash when closed during a refresh; an uncaught error in a window slot aborted the program (now it is logged and shown).
* Tests used to talk to the real service and write the real user state; they are now fully isolated. Several tests depended on the real charger, CPU temperature or `/tmp`; they no longer do.
* Releases after a mode pick, per-key brightness dropping an effect to static, rear-effect changes turning the bar off, Fn+F10/F11/F12 counted as PREDATOR taps, doctor false warnings under `sudo`.

### Known limits
Not yet verified on the real laptop: fans-off, undervolt writes (CPU and GPU), Smart auto in real use, charge-to-100 %-once, key animations. F8 mic LED, headset-at-boot detection, NVIDIA power limit: see STATUS.

## 0.2.0 and 0.1.0

Earlier versions were development releases of this project (first the `rpredatorsense` tool, then the rename to TrueSense with one kernel module). Their contents are all part of 0.3.0 above; see the git history for the details.
