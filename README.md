# rpredatorsense

**PredatorSense for Linux, for one laptop: the Acer Predator Helios 16 (PH16-71).**
Fans and power modes, battery and charging settings, the rear light bar, per-key keyboard RGB and effects, the Fn / PREDATOR keys, and the audio extras (headset mic, Purified Voice equivalent, Acer DTS sound), as a small CLI plus a few systemd services. Written in Python with one tiny kernel helper.

> **Read this first.** This was built and tested on a single PH16-71 (BIOS V1.18). It talks to the firmware through ACPI-WMI and the keyboard controller. That is risky on any other model or BIOS. Do not run it on other hardware unless you accept that. Nothing here is affiliated with, or endorsed by, Acer. See [Safety](#safety) and [Known issues](#known-issues).

## What works

Every row below was exercised on the real laptop with a read-back, a measurement, or the owner's own eyes. The full table, with notes and the things that do **not** work, is in [docs/STATUS.md](docs/STATUS.md).

| Area | Status |
|---|---|
| Power modes (quiet, balanced, performance, turbo, eco) | verified |
| Fans: Auto, Max, manual % per fan; temperatures and RPM | verified |
| Smart-fan daemon: fan ladder, thermal governor, automatic power profile | verified, idle and under heavy load |
| LCD overdrive, boot sound, battery 80 % limit, USB power-off charging, keyboard backlight timeout | verified |
| Rear light bar (5 effects) | verified |
| Keyboard per-key RGB, groups, gradients, saved profiles, 13 built-in effects | verified |
| Fn+F10 touchpad; PREDATOR key (tap / hold / double tap, configurable) | verified |
| Fn+Esc | captured (vendor report `04 8a ff`), no default action; configurable |
| Speakers <-> headphones switching with the aux jack plugged in | verified |
| Purified Voice equivalent (RNNoise via PipeWire), automatic headset/array switching | verified |
| Acer DTS sound through [AcerUltraX](https://github.com/fawaz7/AcerUltraX) | verified (measured) |
| CPU PL1/PL2 limits (root) | verified |
| GPU mode (MUX) | read and write verified; effect after reboot not tested |
| Undervolt | read only; writing was **not tested** |

Not working or not built: the F8 mic-mute LED, headset mic detection when the headset is already plugged in at boot (needs a replug), NVIDIA power limit (the driver refuses it), `.deb`, a daemon API, a GUI, an OpenRGB plugin, live GPU switching.

## Install

Requirements: Linux with the Acer WMI gaming interface, `dkms` and kernel headers, Python 3.11+, systemd, PipeWire (for audio). For battery limit/calibration, USB charging and the backlight timeout you also need the **Linuwu-Sense** driver (not bundled). The headset mic needs a kernel with the ALC245 quirk for subsystem 1025:166c.

```
git clone <this repository> rpredatorsense && cd rpredatorsense
sudo tools/install.sh              # add --with-audio on Arch to also install the RNNoise plugin
rpredatorsense selftest            # every firmware feature: set -> read back -> restore -> PASS/FAIL
```

The installer builds and signs the DKMS module, installs the daemon and user services, a udev rule and a health timer. Re-running it upgrades and restarts the daemon. On Arch you can also `cd packaging/arch && makepkg -si`. Secure Boot signing is untested (see [docs/USAGE.md](docs/USAGE.md)).

## Quick start

```
rpredatorsense status                       # everything at a glance
rpredatorsense mode turbo                   # power mode
rpredatorsense fan set 60                   # both fans 60 %; `fan auto` hands them back to the firmware
rpredatorsense battery on                   # 80 % charge limit
rpredatorsense keyboard rainbow             # per-key RGB; also fill, gradient, keys "wasd=ff0000"
rpredatorsense keyboard effect wave --brightness 50
rpredatorsense rear breathing --color 00ff88
rpredatorsense output toggle                # speakers <-> headphones while the jack is plugged in
rpredatorsense voice headset personal       # Purified Voice mode for the headset mic
```

All commands, configuration and troubleshooting: [docs/USAGE.md](docs/USAGE.md).

## How it works

```
 CLI (rpredatorsense) ----+----> /dev/acer-rear  (kernel/acer-rear-wmi, DKMS)
 rpredatorsense.service --+        whitelisted ACPI-WMI calls: power mode, fans, sensors,
   (root: fan ladder,              GPU mode, boot sound, overdrive, rear light.
    governor, profile)    +----> Linuwu-Sense sysfs (external): battery, USB charge, backlight timeout
 rpredatorsense-keys -----+----> keyboard HID (hidraw): lighting, Fn / PREDATOR key reports
 rpredatorsense-audio ----+----> PipeWire: Purified Voice filters, output switching
```

The kernel helper is deliberately **not** a generic "call any WMI method" interface: each command is validated and only the methods needed here are reachable. On unload, or when the daemon stops, the fans go back to firmware Auto.

## Safety

* Every write is: read the current value, write, read back, keep a restore path. Setters retry the read-back for one second because the firmware can briefly answer -1.
* Fans return to firmware Auto on any exit. The daemon also forces 100 % if its loop breaks while the machine is hot.
* No firmware or BIOS flashing, no custom boot-logo writes, no EC access outside the whitelisted WMI calls.
* The undervolt tool refuses without `--confirm`, is limited to -120 mV, and `uv-reset` writes 0.
* **Do not write the codec's `reconfig` file** (`/sys/class/sound/hwC*D*/reconfig`): it deadlocked the kernel on the test laptop and needed a hard reboot.
* **Do not publish raw ACPI dumps.** They contain firmware tables, including the OEM Windows product key. `.gitignore` already excludes them.

## Known issues

* A headset that is already plugged in when the machine boots is not detected until you unplug and replug it.
* The F8 mic-mute LED does not follow the mic state (not a codec GPIO; the real mechanism is unknown).
* Purified Voice `personal` mode can clip very soft speech; `conference` is the default.
* If `auto-cpufreq` is installed it also writes the clock caps, so the governor's cap is not a hard guarantee.
* The keyboard's OPERATING MODE effect does not follow the power mode by itself; the optional daemon hook re-sends the colour on each mode change. Colours are this project's own defaults.
* Untested: Secure Boot signing, a full battery calibration cycle, GPU mode taking effect after a reboot, other models and BIOS versions.

## Repository layout

```
src/rpredatorsense/   the Python package (CLI, backend, daemon, keys, lighting, audio, cpu, selftest)
kernel/acer-rear-wmi/ the DKMS kernel helper (/dev/acer-rear)
packaging/            systemd units, udev rule, health script, Arch PKGBUILD
tools/                install scripts and read-only diagnostics
docs/                 STATUS.md, USAGE.md, verification-log.md, research/, hardware-data/
```

`docs/research/` holds the protocol notes from the Windows reverse-engineering phase; `docs/hardware-data/` holds small, non-sensitive Linux-side findings (codec dump, pin configs, RAPL/MSR facts).

## Credits and licences

* [Linuwu-Sense](https://github.com/0x7375646F/Linuwu-Sense): the Acer Predator sysfs driver used for battery, USB charging and the backlight timeout, and the reference for several WMI calls.
* Acer PredatorSense (observed, not copied), [PreySense](https://github.com/hammadzaigham/PreySense), Venator and TensorRaya's Helios RGB notes (community keyboard-lighting references): protocol knowledge.
* [AcerUltraX](https://github.com/fawaz7/AcerUltraX): runs Acer's own DTS audio engine on Linux. This project only integrates with it; it downloads Acer's files from Acer on your machine.
* [RNNoise](https://gitlab.xiph.org/xiph/rnnoise) (via the `noise-suppression-for-voice` LADSPA plugin): the noise-reduction model behind Purified Voice.

This repository's own code is licensed **GPL-2.0-only** (see [LICENSE](LICENSE)); the kernel module declares `MODULE_LICENSE("GPL")`. **No Acer, DTS or Realtek files are included or redistributed.** "Acer", "Predator", "PredatorSense" and "DTS" are trademarks of their owners.

## Contributing

Reports from other models are welcome, but run the read-only tools first (`tools/collect_hardware_data.sh`, `tools/headset_diag.sh`) and please do not paste raw ACPI dumps anywhere public. Include your BIOS version, kernel and the `rpredatorsense selftest` output.
