# Status: what works, what does not (Acer Predator Helios 16 PH16-71)

Tested on one machine: **Acer Predator Helios 16 PH16-71, BIOS V1.18, i7-13700HX, RTX 4060 Laptop (dGPU/MUX mode), CachyOS, kernels 7.3.0-4 and 6.18 LTS, PipeWire 1.6, WirePlumber 0.5, Secure Boot off.** Other models or BIOS versions are untested.

Labels: **VERIFIED** = set, read back (or measured, or seen by the owner) and restored on this laptop. **SIMULATED** = the logic is covered by tests against the built-in simulator but has **not** been exercised on the real laptop yet. **PARTIAL** = works with a stated limit. **UNTESTED** = written, never run on hardware. **NOT WORKING / NOT BUILT** = listed so nobody wastes time. The raw chronological evidence is in [verification-log.md](verification-log.md).

## The Go program (0.4.0)
| Feature | Status | Notes |
|---|---|---|
| Daemon, control socket, state restored from 0.3.x (smart fan, curve, profile, hold) | VERIFIED | installed over 0.3.17; every setting kept; the stop path hands the fans back to the firmware |
| Self-test (every firmware setting set, read back, restored) | VERIFIED | 17 of 17 with the Go program |
| Terminal UI against the Go daemon (fans, power modes with hold, rear light) | VERIFIED | driven through a real terminal; every change restored |
| PREDATOR key and the key service (hidraw, gestures, `exec:` action) | VERIFIED | tap ran `truesense gui` on the real key |
| Window (web): server, token and Host/Origin refusals, commands, fan-stop refusal | VERIFIED | against the real daemon; the page itself was opened by the PREDATOR key |
| GPU undervolt helper (reads the curve offset through NVML) | VERIFIED (read) | reads +200 MHz and its range; **writes are SIMULATED** (against a fake library), none was run on the real GPU |
| Fan stop (both fans) with the temperature watchdog | SIMULATED | refused without confirm and when warm; the stop itself was not run on the Go daemon |

## Power, cooling, battery
| Feature | Status | Notes |
|---|---|---|
| Power modes eco / quiet / balanced / performance / turbo | VERIFIED | all five set and read back |
| Temperatures and fan RPM | VERIFIED | the RPM field is 13 bits wide |
| Fans: Auto, Max, manual % (one or separate CPU/GPU) | VERIFIED | one write holds; duty 20 to 100 % |
| Smart fan (earlier ladder version) | VERIFIED | under a 24-thread load the fans reached 100/90 % within 10 s and the CPU held 88 to 90 °C |
| Smart fan (current curve version, built-in and custom curves) | SIMULATED | same safety rules; the curve engine itself has not been run under real load |
| Stopping one fan or both (0 % per fan) with watchdog | PARTIAL / NOT EFFECTIVE | tested on the real laptop (0.3.5): the firmware accepts a 0 % command for the GPU fan and the duty register reads back 0, but the fan keeps turning at about 1690 rpm. The EC appears to keep a minimum speed in manual mode, so the fans cannot really be stopped this way; the window now measures the fan speed after a stop and says so |
| Stopping other power managers (`power managers`, Settings switch) | SIMULATED | logic tested with a fake systemctl and a fake sysfs tree; not yet run for real (the service stops the unit through systemd) |
| Game rules (`power game`) | SIMULATED | process matching tested against a fake /proc; the effect on the real laptop is not yet watched |
| Native auto (Balanced on power, Eco on battery) | SIMULATED | modelled on how PredatorSense behaves; not compared side by side with Windows |
| Smart auto and its profiles | SIMULATED | escalation, Turbo timing, thermal back-off, battery rules, resume-by-itself: all in tests |
| CPU power limits (kept by the service) | PARTIAL | write, read-back and restore verified earlier (root); the "service keeps them" logic is SIMULATED |
| 80 % battery limit, USB charging while off, calibration flag, LCD overdrive, boot sound, keyboard timeout | VERIFIED | set, read back, restored. The keyboard's own 30 s timer is a separate controller flag, now handled |
| Charge to 100 % once | SIMULATED | only the firmware's 80 % / 100 % exist; no other threshold is possible |
| GPU mode (MUX) | PARTIAL | read and write verified (restored immediately); the effect after a reboot was not tested; no live switching |
| CPU undervolt (MSR 0x150) | PARTIAL | reading is verified (0 mV); **writing was never tried**; the BIOS may lock it. Guarded: confirmation, safety zones, rollback |
| GPU undervolt (NVML offset and clock cap) | PARTIAL | reading the offset range is verified (the driver supports it); **writing was never tried**; the GPU already had +200 MHz set outside TrueSense when first read |
| Undervolt at boot (crash guard) | SIMULATED | |
| NVIDIA power limit | NOT POSSIBLE | the driver reports it is not supported on this GPU |

## Lighting
| Feature | Status | Notes |
|---|---|---|
| Rear light bar: 5 effects, three parts (static) | VERIFIED / PARTIAL | effects seen working; the three-part static colours are accepted by the firmware (the order left, middle, right is confirmed for the left part only) |
| Keyboard: 13 effects, colour, speed, brightness | VERIFIED | brightness and speed read back or seen; speed range may be wider than the UI offers |
| Per-key RGB (select, palette, gradient, rainbow, undo) | VERIFIED | all cells correct; the picture follows the real layout |
| Per-key animations (software) | SIMULATED | drawn by the key service at about 5 frames a second |
| 14 more per-key animations, key designs, image painting | SIMULATED | pure drawing code, tested for valid frames; not yet watched on the keyboard. Same ~5 frames a second limit |
| Lighting profiles, boot and resume lighting | SIMULATED | applied by the service at start and after suspend |
| Fn+F11 / Fn+F12 brightness | VERIFIED | done by the keyboard; the key service ignores these keys |

## Keys, audio
| Feature | Status | Notes |
|---|---|---|
| PREDATOR key tap / double / long, Fn+F10 | VERIFIED | |
| Key bindings editor, profiles, scripts, live reload | SIMULATED | the editing logic is tested; reload by the running service not yet watched live |
| F8 mic-mute LED | NOT WORKING | not a codec GPIO; the mechanism is unknown |
| Speakers / headphones switching, jack mode, headset mic boost, mic mute | VERIFIED | |
| Headset microphone | PARTIAL | detected after a replug; a headset already plugged in at boot is not detected until you replug it (kernel quirk). **Never write the codec `reconfig` file** |
| Purified Voice style noise reduction (RNNoise) | VERIFIED | measured |
| Acer DTS through AcerUltraX | VERIFIED | measured |

## System
| Item | Status | Notes |
|---|---|---|
| Installer, uninstaller, DKMS for two kernels, systemd units, udev rule, doctor, self-test | VERIFIED | survives reboots; kernel updates rebuild the module |
| One-line install (`get.sh`) | PARTIAL | downloads the tagged release |
| Window (Qt 6), Noctalia widgets | PARTIAL | every page rendered and the logic tested headlessly; not every control was clicked on the real laptop |
| Arch package (PKGBUILD) | WRITTEN | not built with `makepkg` during testing |
| Secure Boot module signing | UNTESTED | DKMS signs with its own key; Secure Boot was off |
| Update check (GitHub), download and verification | VERIFIED | against the real repository (tag `v0.3.0` downloaded and checked, nothing installed) |
| Update install through the window (password prompt) and the daily notification | SIMULATED | the flow is tested with a fake download and installer; a real update through `pkexec` has not been run yet |
| `.deb`, OpenRGB plugin, tray icon, D-Bus API | NOT BUILT | |

When a SIMULATED or UNTESTED item gets verified on hardware, this table is updated; please report what you see (an issue with `truesense doctor --json`).
