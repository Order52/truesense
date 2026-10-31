# Status: what works, what does not (Acer Predator Helios 16 PH16-71)

Tested on one machine: **Acer Predator Helios 16 PH16-71, BIOS V1.18, i7-13700HX, RTX 4060 Laptop (dGPU/MUX mode), CachyOS, kernel 7.3.0-4, PipeWire 1.6, WirePlumber 0.5.18, Secure Boot disabled.**
Other models or BIOS versions are untested; see the warnings in the README before trying them.

Meaning of the labels: **VERIFIED** = set, read back (or measured / seen by the owner) and restored on this laptop. **PARTIAL** = works with a stated limit. **NOT WORKING / NOT BUILT** = listed so nobody wastes time. The raw chronological evidence is in [verification-log.md](verification-log.md).

## Firmware, power and cooling
| Feature | Status | Notes |
|---|---|---|
| Power modes quiet / balanced / performance / turbo / eco | VERIFIED | all five set and read back; platform_profile agrees |
| Temperatures and fan RPM (firmware) | VERIFIED | the RPM field is 13 bits wide |
| Fans: Auto, Max, manual % per fan | VERIFIED | a single write holds; 60 % about 4070 rpm, 70 % about 4580, 100 % about 6050, Max about 6050; manual duty limited to 20-100 % |
| Smart-fan daemon (fan ladder, thermal governor, auto power profile) | VERIFIED | idle -> firmware Auto; 24-thread AVX load -> fans 100/90 within 10 s, governor lowers the clock cap in steps, CPU held 88-90 C; returns to idle afterwards. `auto-cpufreq`, if installed, also writes the clock caps, so the cap is not a hard guarantee |
| LCD overdrive, boot animation/sound, battery 80 % limit, USB power-off charge level, keyboard backlight timeout | VERIFIED | set, read back, restored. Read-backs retry for 1 s because the firmware can answer -1 right after a write |
| Battery calibration | PARTIAL | the flag toggles and reads back; a real calibration cycle (hours) was not run |
| GPU mode (MUX: optimus / nvidia / auto) | PARTIAL | read and write verified (restored immediately); the effect after a reboot was not tested; live switching (supergfxctl / envycontrol) not implemented |
| CPU PL1/PL2 (RAPL) | VERIFIED | write, read back, restore (needs root); out-of-range values refused; lock bit clear on this unit |
| Undervolt (MSR 0x150) | PARTIAL | read only: core and cache planes report 0 mV. Writing an offset was **not attempted**; the tool refuses without `--confirm` and limits to -120 mV |
| NVIDIA power limit (`nvidia-smi -pl`) | NOT POSSIBLE | the driver reports it is not supported on this GPU |
| Custom boot logo | NOT SUPPORTED | firmware write; refused on purpose |

## Lighting
| Feature | Status | Notes |
|---|---|---|
| Rear light bar: static, breathing, neon, wave, twinkling | VERIFIED | all five seen working; colour, speed, brightness read back |
| Keyboard per-key RGB (fill, gradient, rainbow, per-key, named groups, saved profiles) | VERIFIED | all 126 cells correct after fixing the hidraw report-id framing (writes must be 65 bytes: a leading 0 plus 64) |
| Keyboard built-in effects | VERIFIED | static, breathing, wave, snake, ripple, raindrop, star, heartbeat, neon (needs brightness 50), lightning, spot, fireball, snow |
| Keyboard OPERATING MODE (colour follows the power mode) | PARTIAL | the keyboard shows one static colour by itself and does **not** track the power mode; re-sending the colour on each mode change works (green / blue / cyan confirmed). Colours are this project's defaults, not Acer's. Daemon hook is opt-in (`[keyboard] operating_follow`) and was not exercised live |
| Fn+F11 / Fn+F12 keyboard brightness | VERIFIED (firmware) | the keyboard firmware does it cleanly by itself; the key service deliberately ignores these keys |

## Keys and buttons
| Feature | Status | Notes |
|---|---|---|
| Fn+F10 touchpad toggle | VERIFIED | works without root (udev rule); reads back the real state |
| PREDATOR key: tap / hold / double tap | VERIFIED | defaults: step power mode (held 30 min against the auto profile), toggle fans Max, switch speakers/headphones; configurable |
| Fn+Esc | PARTIAL | sends a vendor report `04 8a ff` (plus KEY_KPPLUSMINUS); no default action; configurable |
| Fn+arrow keys | CAPTURED | each sends only KEY_KPPLUSMINUS, indistinguishable from each other; nothing else to bind |
| Mic mute toggle | VERIFIED | via PipeWire |
| F8 mic-mute LED | NOT WORKING | not a codec GPIO (GPIO0-2 tested); the kernel's LED hook never fires on this SOF setup. Probably driven by the keyboard controller or EC; the mechanism is unknown |

## Audio
| Feature | Status | Notes |
|---|---|---|
| Headset microphone on the 3.5 mm jack | PARTIAL | works once the headset is plugged in while running (mainline kernel quirk for 1025:166c). **A headset already plugged in at boot is not detected until you unplug and replug it.** Do not try to fix this with the codec `reconfig` interface: it deadlocked the kernel on this laptop |
| Purified Voice equivalent (RNNoise via PipeWire) | VERIFIED | conference and personal modes measured: speech level -1.9 dB, quiet floor -24 dB. Personal mode sometimes clips very soft words |
| Purified Voice automatic switching + notification | VERIFIED | headset filter when a headset is plugged in, mic-array filter otherwise |
| Purified Voice for speakers/headphones output | PARTIAL | wiring confirmed; attenuation not measured; off by default (it also gates music) |
| Acer DTS sound via AcerUltraX | VERIFIED (measured) | external project; offline render of one test signal through all modes on both speakers and headphones shows clear, mode-dependent processing. Whether it sounds better is subjective |
| Speakers <-> headphones while the aux jack is plugged in | VERIFIED | speakers and headphones share one playback PCM, so only one exists at a time; the tool flips the card profile and shows an instant notification. Flipping only the codec switches does not work |

## System
| Item | Status | Notes |
|---|---|---|
| Install / uninstall script, DKMS module, systemd units, udev rule | VERIFIED | survives reboots: modules load, services start, devices renumbering handled |
| `rpredatorsense selftest` | 15 / 15 pass | firmware settings, daemon, key service, keyboard read-back, audio profile, Purified Voice config |
| Arch package (PKGBUILD) | BUILT | builds cleanly; not installed during testing |
| Secure Boot module signing | UNTESTED | DKMS signs with its own key; Secure Boot was off on the test machine |

## Not built
`.deb` package, a socket / D-Bus API for the daemon, a tray icon or GUI, an OpenRGB plugin, live GPU switching, automatic restore of per-key lighting after a firmware reset.
