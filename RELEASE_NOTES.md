# Release notes

## 0.2.0 (2026-10-31): TrueSense

The project is now **TrueSense**: one kernel module, one daemon, one CLI, an optional window. Tested only on the **Acer Predator Helios 16 PH16-71** (BIOS V1.18, CachyOS, kernel 7.3.0-4 and 6.18 LTS, PipeWire 1.6). See [docs/STATUS.md](docs/STATUS.md).

### Added in this release (later)
* **Native auto and Smart auto** power modes (`truesense power auto native|smart`), and curve-based **smart fan** with built-in curves, your own curves (`truesense fan curve ...`) and a draggable curve editor in the window.
* **Lighting looks** (`truesense look`) and a **boot look**: the daemon applies a chosen look when the laptop starts, before login, and again after resume from suspend. Window: Lighting, Profiles.
* Keyboard 30-second timeout now also clears the keyboard controller's own flag; per-key paint and picker; rear light three-zone colours; effect speed.

### Changed
* **One kernel module** `truesense_wmi` replaces `acer_rear_wmi` and the separate Linuwu-Sense dependency. DMI-guarded, stable WMI API only, per-open reply buffers (fixes a shared-buffer race), a global command mutex, fans back to firmware Auto when unloaded. Builds warning-free on two kernels; DKMS rebuilds on every kernel update.
* **Installer** removes the old drivers and files (backed up to `/var/backups/truesense`), blocks conflicting modules, keeps your Purified Voice modes and lighting profiles, runs `doctor` at the end. `tools/uninstall.sh [--purge]` reverses it.
* **CLI redesign**: 23 areas, `on|off|toggle|status` wherever a switch exists, `--json` anywhere, shell completion for bash, zsh and fish.
* **Daemon** talks over a UNIX socket (peer-credential checked), keeps its settings across reboots, survives a missing device, is `Type=notify` with a watchdog and `Restart=always`, and publishes live state for status bars (now including fan RPM and the automatic-profile flag).

### Added
* `truesense doctor` (15 checks, exit code for scripts) and a health timer.
* **Customisable PREDATOR key**: tap, double, triple and long press, plus Fn+Esc, each bound to any action (power modes, fans, profiles, output switch, mic, lighting, `exec:`...). Validated when set.
* **Profiles**: save and apply bundles of power, fans, lighting, battery and audio. Four ship by default.
* Fn+F10 touchpad toggle through the same action system.
* Audio: default microphone follows the active Purified Voice input; per-input modes; headset-mic boost; jack status.
* **Optional window** (`truesense gui`, Qt 6 via PyQt6) and Noctalia bar widgets in `contrib/noctalia`.
* A hardware simulator (`TRUESENSE_SIM=1`) and 28 unit tests including a smoke run of 119 CLI commands.

### Fixed
* The simulator was more lenient than the kernel (accepted values the firmware refuses); it now mirrors every validation.
* The microphone watcher only reacted to jack events; it now reacts to filter and mode changes.
* `keyboard profile` crashed in one code path; the doctor mis-reported services when run under `sudo`.
* A forced-reboot lesson recorded: never write the codec `reconfig` file on this laptop.

### Still not working or untested
F8 mic-mute LED, headset detection when plugged in at boot (replug), NVIDIA power limit, undervolt write, battery calibration cycle, GPU mode effect after reboot, Secure Boot signing, `.deb`.

## 0.1.0 (2026-10-31): first release

First working version for the **Acer Predator Helios 16 PH16-71** (BIOS V1.18; tested on CachyOS, kernel 7.3.0-4, PipeWire 1.6, WirePlumber 0.5.18, Secure Boot off). Everything below was checked on that one machine. See [docs/STATUS.md](docs/STATUS.md) for the evidence and for what does not work.

### Added
* **CLI** `truesense` (and `./rps` from a checkout): status, sensors, power mode, fans, firmware settings, GPU mode, rear light, keyboard lighting, audio output, Purified Voice, CPU limits, selftest.
* **Kernel helper** `acer_rear_wmi` (DKMS, `/dev/acer-rear`): whitelisted, validated ACPI-WMI commands for power mode, fans, sensors, GPU mode, boot sound, overdrive and the rear light. Returns the fans to firmware Auto when unloaded.
* **Daemon** `truesense.service`: smart-fan ladder with hysteresis and prediction, thermal governor, power limits, automatic power profile (AC/battery/load), health timer that restarts it and rebuilds DKMS after kernel updates.
* **Keys** `truesense-keys.service`: Fn+F10 touchpad toggle (no root, via udev rule), PREDATOR key tap / hold / double tap with configurable actions, Fn+Esc hook. Finds the keyboard's HID interface by itself.
* **Lighting**: rear light bar (5 effects); keyboard per-key RGB (fill, gradient, rainbow, per-key, named groups, saved profiles), 13 built-in effects, brightness read-back, and an opt-in operating-mode colour that follows the power mode.
* **Audio**: speakers <-> headphones switching while the aux jack is plugged in (instant notification); Purified Voice equivalent using RNNoise on PipeWire (conference and personal modes, per-input filters, automatic headset/array switching with a notification); integration notes for AcerUltraX (Acer DTS sound).
* **Safety**: read -> write -> read back -> restore on every firmware setting; manual fan duty limited to 20-100 %; undervolt refuses without `--confirm`; no firmware flashing.
* **Selftest**: `truesense selftest` (15 checks).
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
First release. Re-running `sudo tools/install.sh` upgrades in place and restarts the daemon. The user services (`truesense-keys`, `truesense-audio`) restart for the invoking user; other logged-in users restart theirs on next login.
