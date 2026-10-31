# TrueSense

**PredatorSense for Linux, for one laptop: the Acer Predator Helios 16 (PH16-71).**
Power modes, fans, battery and charging, GPU mode, the rear light bar, per-key keyboard RGB, the PREDATOR key and Fn keys, and the audio extras (headset mic, AI noise reduction, Acer DTS sound). It is a root daemon, one small kernel module, a command line that does everything, and an optional window.

> **Read this first.** Built and tested on a single PH16-71 (BIOS V1.18). It talks to the firmware through ACPI-WMI and the keyboard controller. The module refuses to load on other models unless you force it, and forcing it on other hardware is at your own risk. Not affiliated with or endorsed by Acer.

## Install (one command)

```
git clone https://github.com/Order52/truesense && cd truesense && sudo tools/install.sh
truesense doctor        # checks everything and says what to fix
```

Or, without cloning: `curl -fsSL https://raw.githubusercontent.com/Order52/truesense/main/tools/get.sh | sudo bash` (read it first; set `TRUESENSE_SHA256` to pin the download).

The installer is safe to re-run (it upgrades). It removes older helper drivers from earlier versions of this project (backing them up first), builds the DKMS module for every installed kernel (so kernel updates keep working), installs the daemon and user services, a udev rule, and a health timer that repairs a stalled daemon or missing module. `sudo tools/uninstall.sh [--purge]` removes it all. On Arch: `cd packaging/arch && makepkg -si`.

Needs: Linux 6.6+, `dkms` and matching kernel headers, Python 3.11+, systemd. For audio features: PipeWire and the RNNoise plugin (`--with-audio` installs it on Arch). The window needs `python-pyqt6` (Qt 6; native on Wayland and X11).

## What it does

```
truesense status                         everything at a glance
truesense power set turbo                eco quiet balanced performance turbo (also: next, prev, auto on|off, hold, release)
truesense fan smart on                   smart curve; also: fan auto | fan max | fan set 60  (manual 20-100 %)
truesense battery limit on               80 % charge limit; also: calibration, usb-charge 0|10|20|30
truesense display overdrive toggle       LCD overdrive;  boot sound toggle;  gpu set nvidia  (reboot to apply)
truesense rear set wave --color 00ffcc   rear light bar; also on|off|toggle
truesense keyboard effect wave           13 built-in effects; fill, gradient, rainbow, keys "wasd=ff0000", profile save|load
truesense audio output toggle            speakers <-> headphones while the jack is plugged in
truesense audio voice headset personal   Purified Voice style noise reduction per microphone
truesense keys set double output-toggle  make the PREDATOR key do what you want (tap / double / triple / long)
truesense profile apply gaming           a saved bundle of power, fans, lighting, battery, audio
truesense gui                            the optional window
```

Every area takes `on|off|toggle|status` where that makes sense, and `--json` anywhere for scripts. Shell completion for bash, zsh and fish is installed. Full reference: [docs/USAGE.md](docs/USAGE.md).

## How it is built

```
firmware (ACPI-WMI) <- truesense_wmi (DKMS kernel module, /dev/truesense)
                         ^
        truesense daemon (root, systemd, watchdog) <- JSON socket -> CLI, window, key service, status bars
        smart fan curve, thermal governor, automatic power profile, saved state
        user services: truesense-keys (PREDATOR/Fn keys), truesense-audio (headset/mic switching)
```

* The kernel module is a single, small, DMI-guarded driver using only the stable WMI API. Fans go back to firmware Auto whenever it unloads.
* The daemon never leaves the fans stuck: on stop, crash or exit the firmware takes them back, and systemd restarts it. A health timer rebuilds the module after a kernel update.
* Everything is testable without hardware: `TRUESENSE_SIM=1` swaps in a simulator, and `make test` runs the suite.

## Status

Every claim below was checked on the real laptop with a read-back, a measurement, or the owner's own eyes. Details and the things that do **not** work are in [docs/STATUS.md](docs/STATUS.md).

| Area | Status |
|---|---|
| Power modes, fans (Auto/Max/manual, RPM, temperatures), smart fan daemon, automatic profile | verified |
| Overdrive, boot sound, 80 % limit, USB charging while off, backlight timeout | verified |
| Rear light bar (5 effects); keyboard per-key RGB and 13 effects | verified |
| PREDATOR key tap / double / long, Fn+F10 | verified |
| Speakers <-> headphones switching; headset mic; AI noise reduction; Acer DTS via AcerUltraX | verified |
| GPU mode (MUX) | write and read-back verified; effect after reboot untested |
| Undervolt | read only; writing was never attempted |
| Battery calibration, Secure Boot signing | untested |
| F8 mic-mute LED, NVIDIA power limit, live GPU switching | not working / not built |

Known limits: a headset already plugged in at boot is not detected until you replug it (kernel quirk). **Never write the codec's `reconfig` file** on this laptop; it deadlocked the kernel.

## Credits and licence

GPL-2.0-only. Firmware interface knowledge comes from reading the public [Linuwu-Sense](https://github.com/0x7375646F/Linuwu-Sense) driver and observing this laptop's own behaviour; Acer sound uses [AcerUltraX](https://github.com/fawaz7/AcerUltraX); noise reduction uses [RNNoise](https://github.com/xiph/rnnoise). No Acer, DTS or Realtek files are included.
