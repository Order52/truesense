# Release notes

## 0.1.0 (2026-10-31): first release

First working version for the **Acer Predator Helios 16 PH16-71** (BIOS V1.18; tested on CachyOS, kernel 7.3.0-4, PipeWire 1.6, WirePlumber 0.5.18, Secure Boot off). Everything below was checked on that one machine. See [docs/STATUS.md](docs/STATUS.md) for the evidence and for what does not work.

### Added
* **CLI** `rpredatorsense` (and `./rps` from a checkout): status, sensors, power mode, fans, firmware settings, GPU mode, rear light, keyboard lighting, audio output, Purified Voice, CPU limits, selftest.
* **Kernel helper** `acer_rear_wmi` (DKMS, `/dev/acer-rear`): whitelisted, validated ACPI-WMI commands for power mode, fans, sensors, GPU mode, boot sound, overdrive and the rear light. Returns the fans to firmware Auto when unloaded.
* **Daemon** `rpredatorsense.service`: smart-fan ladder with hysteresis and prediction, thermal governor, power limits, automatic power profile (AC/battery/load), health timer that restarts it and rebuilds DKMS after kernel updates.
* **Keys** `rpredatorsense-keys.service`: Fn+F10 touchpad toggle (no root, via udev rule), PREDATOR key tap / hold / double tap with configurable actions, Fn+Esc hook. Finds the keyboard's HID interface by itself.
* **Lighting**: rear light bar (5 effects); keyboard per-key RGB (fill, gradient, rainbow, per-key, named groups, saved profiles), 13 built-in effects, brightness read-back, and an opt-in operating-mode colour that follows the power mode.
* **Audio**: speakers <-> headphones switching while the aux jack is plugged in (instant notification); Purified Voice equivalent using RNNoise on PipeWire (conference and personal modes, per-input filters, automatic headset/array switching with a notification); integration notes for AcerUltraX (Acer DTS sound).
* **Safety**: read -> write -> read back -> restore on every firmware setting; manual fan duty limited to 20-100 %; undervolt refuses without `--confirm`; no firmware flashing.
* **Selftest**: `rpredatorsense selftest` (15 checks).
* **Packaging**: `tools/install.sh`, an Arch `PKGBUILD`, systemd units, a udev rule.

### Verified highlights
* Fans: one write holds; 60 % ~ 4070 rpm, 100 % ~ 6050 rpm. Under a 24-thread AVX load the daemon had both fans at 100/90 % within 10 s, the governor lowered the clock cap in steps, and the CPU held 88-90 C.
* Per-key lighting: all 126 cells correct; 13 built-in keyboard effects and all 5 rear-bar effects seen working.
* Purified Voice personal mode: speech level -1.9 dB, quiet floor -24.7 dB. Headset <-> array filter switching on plug events.
* Survives reboots: modules load, services start, device renumbering (`hidraw2` -> `hidraw3`, `event14` -> `event21`) handled.

### Bugs found and fixed while building this
* RPM was read with a 12-bit mask; the field is 13 bits (readings above 4095 wrapped).
* Per-key lighting upload was shifted by 7 cells: `hidraw` drops the first byte of a write on a device without report IDs, so frames must be written as 65 bytes.
* Setters false-failed about 1 time in 15 because the firmware briefly answers -1 right after a write; read-backs now retry for one second.
* Purified Voice filters were destroyed on every headset plug event (`node.dont-fallback`); they now survive and re-link.
* A removed "sticky output" option never did anything (WirePlumber already had that property off).

### Known issues
* A headset already plugged in at boot is not detected until you unplug and replug it. Do **not** try to fix this through the codec `reconfig` file: it deadlocked the kernel on the test laptop.
* The F8 mic-mute LED does not follow the mic state (not a codec GPIO).
* Purified Voice `personal` mode can clip very soft speech.
* `auto-cpufreq`, if installed, also writes clock caps, so the governor's cap is not a hard guarantee.
* NVIDIA power limit cannot be changed on this GPU (driver refuses).

### Not included / not tested
* Not built: `.deb`, a daemon socket / D-Bus API, a tray or GUI, an OpenRGB plugin, live GPU switching.
* Not tested: Secure Boot signing, an undervolt write (read-only checked), GPU mode taking effect after a reboot, a full battery-calibration cycle, any other model or BIOS version.

### Upgrade notes
First release. Re-running `sudo tools/install.sh` upgrades in place and restarts the daemon. The user services (`rpredatorsense-keys`, `rpredatorsense-audio`) restart for the invoking user; other logged-in users restart theirs on next login.
