# Usage and troubleshooting

`rpredatorsense` is the installed command. From a checkout use `./rps` (same arguments).

## Install
```
sudo tools/install.sh              # kernel helper (DKMS), daemon, user services, udev rule
sudo tools/install.sh --with-audio # also installs the RNNoise LADSPA plugin (Arch: noise-suppression-for-voice)
rpredatorsense selftest            # checks every firmware feature and prints PASS/FAIL
```
`install.sh` is safe to re-run; it restarts the daemon so upgrades take effect. On Arch you can instead build the package: `cd packaging/arch && makepkg -si`, then set `user = "<your login>"` in `/etc/rpredatorsense/config.toml`.

Requirements: Linux with ACPI-WMI for the Acer gaming interface, `dkms` + kernel headers, Python 3.11+, systemd, PipeWire (audio features). For battery limit/calibration, USB charging and the keyboard-backlight timeout the **Linuwu-Sense** driver must also be installed (it provides `/sys/devices/platform/acer-wmi/predator_sense/`; not bundled). A kernel that carries the ALC245 quirk for subsystem 1025:166c is needed for the headset microphone.

Secure Boot: DKMS signs the module with its own key (`/var/lib/dkms/mok.pub`); enrol it once with `mokutil --import` before enabling Secure Boot. This path was not tested (Secure Boot was off on the test machine).

## Commands
| Command | What it does |
|---|---|
| `status`, `sensors` | everything / firmware temperatures and fan RPM |
| `mode [quiet\|balanced\|performance\|turbo\|eco]` | power mode (a manual choice holds for 30 min against the automatic profile) |
| `fan auto\|max\|set CPU% [GPU%]\|status\|curve` | fans; manual duty 20-100 %; one write holds |
| `overdrive`, `battery`, `boot-sound`, `kb-timeout [on\|off]`, `usb-charge [0\|10\|20\|30]` | firmware settings, each read back after writing |
| `gpu [optimus\|nvidia\|auto]` | GPU mode (reboot needed to take effect) |
| `rear static\|breathing\|neon\|wave\|twinkling --color RRGGBB [--speed 1..5 --brightness 0..100]`, `rear off`, `rear get` | rear light bar |
| `keyboard fill COLOR`, `gradient C1 C2`, `rainbow`, `keys "wasd=ff0000" "esc=00ff00" --base 101010`, `list-keys` | per-key RGB |
| `keyboard effect NAME --color RRGGBB --speed 1..11 --brightness 0..50` | built-in effects (static breathing wave snake ripple neon raindrop lightning spot star fireball snow heartbeat operatingmode; neon needs brightness 50) |
| `keyboard profile save NAME fill COLOR\|gradient C1 C2\|rainbow`, `profile load NAME`, `profile list` | saved per-key profiles |
| `keyboard get`, `keyboard brightness N`, `keyboard off`, `keyboard restore` | brightness read-back (0-50) / set / off / the stock cyan per-key state |
| `output speakers\|headphones\|toggle\|cycle\|status` | switch speakers and headphones while the aux jack is plugged in |
| `mic toggle`, `mic status` | mute / state |
| `voice status`, `voice headset MODE`, `voice array MODE`, `voice apply`, `voice watch` | Purified Voice per input; MODE = off, conference, personal |
| `speaker on\|off` | Purified Voice filter in front of the output (off by default; it also gates music) |
| `cpu info`, `cpu limits PL1 PL2`, `cpu uv-get`, `cpu uv-set MV --confirm`, `cpu uv-reset` | power limits (root) and a guarded undervolt |
| `daemon`, `keys` | the services |
| `selftest [--skip-fans]` | set, read back and restore every firmware feature, plus service checks |

## Services
* `rpredatorsense.service` (root): fan ladder, thermal governor, power limits, automatic power profile. Always hands the fans back to firmware Auto and uncaps clocks on stop, crash or exit.
* `rpredatorsense-keys.service` (user): Fn+F10, Fn+Esc, PREDATOR key (finds the keyboard's HID interface by itself).
* `rpredatorsense-audio.service` (user): picks the right Purified Voice filter when a headset is plugged or unplugged.
* `rpredatorsense-health.timer`: restarts the daemon if it stalls and rebuilds the DKMS module after a kernel update.

## Configuration
* `/etc/rpredatorsense/config.toml`: `user`, fan ladder (`[fan]`), power limits and governor (`[cpu]`), `[profile] auto/osd`, `[keyboard] operating_follow` and `operating_colors`.
* `~/.config/rpredatorsense/config.toml`: `[buttons]` with `tap`, `long`, `double`, `fn_esc` = `mode-cycle | fan-max-toggle | touchpad | mic-toggle | output-toggle | none`; `[audio]` with `notify`.
* State files: `~/.cache/predator-fan-mode` (`smart` = daemon controls fans, `manual` = leave them alone), `~/.cache/predator-auto-off` (exists = no automatic power profile), `/tmp/predator-state` (live state for status bars).
* `RPS_OSD=native|noctalia|none` chooses how the output switch is announced (native desktop notification by default).

## Purified Voice
Acer's Purified Voice runs a proprietary Realtek model that cannot run on Linux. This project builds an open equivalent: a PipeWire filter chain with RNNoise (voice-activity gating). `conference` is gentle, `personal` is strict (sometimes clips very soft words). Modes map to Acer's labels: All Off, Personal Call, Conference Call. The filters survive plug/unplug events; only the default source is switched.

## Audio output switching
Speakers and the 3.5 mm headphones use the same analog playback PCM, so ALSA/UCM exposes them as two mutually exclusive card profiles and a volume applet can only show the active one. `rpredatorsense output ...` flips the card profile (instant notification, then a silent switch). Flipping only the codec playback switches does not work: the codec feeds the two pins from different DACs and the single stream is bound to one.

## Optional desktop integration (examples, not installed)
A Hyprland key bind: `bind = , F5, exec, rpredatorsense output cycle` (speakers -> headphones -> other devices). When the shell has Noctalia, popups it generates while the profile flips are silenced with `noctalia msg osd-disable` / `osd-enable`. Without Noctalia, `hyprctl notify` or `notify-send` is used.

## Troubleshooting
* `/dev/acer-rear` missing: `sudo modprobe acer_rear_wmi`; after a kernel update the health timer rebuilds DKMS within 10 minutes (or `sudo dkms autoinstall`).
* Fans stuck: `rpredatorsense fan auto`. Stopping the service also returns them to firmware Auto.
* Headset mic missing: unplug and replug the headset (the codec classifies it on the plug event); `amixer -c1 cget iface=CARD,name='Headset Mic Jack'` must read `on`. `sudo tools/headset_diag.sh` collects data. **Never write the codec's `reconfig` file**: it deadlocked the kernel on the test laptop and needed a hard reboot.
* Per-key colours shifted by a few keys: hidraw writes without report IDs drop the first byte; writes must be 65 bytes (leading 0 + 64).
* A firmware setting reads -1 right after a write: the firmware needs a moment; the tool retries for 1 s.
* Another fan/profile daemon (an older script, `tuned`, ...) will fight over the fans: stop it.

## Safety rules the tools follow
Read the current value, write, read back, keep a restore path. Fans return to firmware Auto on any exit. No firmware flashing, no boot-logo writes, no EC access outside the whitelisted WMI calls. The undervolt interface needs `--confirm`, steps are limited to -120 mV and `uv-reset` writes 0.

## Collecting data for another model
`sudo tools/collect_hardware_data.sh` (read-only: ACPI tables, codec, RAPL/MSR facts) and `tools/headset_diag.sh` help port the project. **Do not publish the raw ACPI dump**: it contains firmware tables, including one with the OEM Windows product key.
