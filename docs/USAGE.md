# TrueSense usage and troubleshooting

`truesense` is the installed command (from a checkout: `./truesense`). `--json` works anywhere and prints machine-readable output. `truesense <area> --help` lists the sub-commands.

## Install, check, remove
```
sudo tools/install.sh                  # module (DKMS), daemon, user services, udev rule, health timer
sudo tools/install.sh --with-audio     # also the RNNoise plugin (Arch: noise-suppression-for-voice)
sudo tools/install.sh --module-only    # only the kernel module
truesense doctor                       # 15 checks; exit 1 if anything fails; --json available
truesense selftest                     # set -> read back -> restore for every firmware feature
sudo tools/uninstall.sh [--purge]      # --purge also removes settings and saved state
```
Flags: `--no-services`, `--force` (unsupported model, at your own risk), `--prefix=DIR`. The install log is `/var/log/truesense-install.log`. Secure Boot: DKMS signs the module with its own key; enrol it with `mokutil --import /var/lib/dkms/mok.pub` before enabling Secure Boot (untested here, Secure Boot was off).

## Commands
Areas that are switches accept `on | off | toggle | status`.

| Command | What it does |
|---|---|
| `status`, `sensors` | everything / temperatures and fan RPM |
| `power set MODE`, `next`, `prev`, `list`, `status` | eco, quiet, balanced, performance, turbo |
| `power auto off\|native\|smart` | who picks the power mode: you (`off`), **native** (like PredatorSense on Windows: Balanced on power, Eco on battery), or **smart** (heat, load and battery aware: Performance when you game or work hard, Turbo when that is sustained and cool enough, a back-off if it overheats, Eco on battery); `on` = smart |
| `power hold [MIN]`, `power release` | keep your manual choice against the automatic mode (30 min by default) |
| `fan auto \| max \| set CPU [GPU] \| status` | fans; manual duty 20-100 % |
| `fan smart on\|off\|toggle` | the daemon's smart curve |
| `fan curve list\|current\|show NAME\|use NAME` | fan curves (temperature to duty, CPU and GPU separately): built in `truesense` (tuned for this laptop), `quiet`, `performance`, `max-cooling`, plus yours |
| `fan curve save NAME --cpu 52:0,60:35,70:55,89:100 --gpu 48:0,60:35,78:100 [--from BASE] [--predict 5 --hysteresis 4 --down-rate 1.5 --couple 15]`, `fan curve delete NAME` | your own curve; 0 % = firmware auto, the duty may only rise with temperature, it must reach 100 % by 95 C (CPU) / 85 C (GPU) |
| `battery limit`, `battery calibration`, `battery usb-charge [0\|10\|20\|30]` | 80 % limit, calibration flag, USB charging while off |
| `display overdrive`, `boot sound` | LCD overdrive, boot animation/sound |
| `gpu status`, `gpu set optimus\|nvidia\|auto` | MUX mode (reboot to apply) |
| `rear on\|off\|toggle`, `rear set EFFECT --color RRGGBB --speed 1..5 --brightness 0..100` | static breathing neon wave twinkling |
| `rear set static --zones L,M,R` | the light bar has three parts; static can colour each (one colour = all three) |
| `look list\|save NAME\|show NAME\|apply NAME\|delete NAME` | saved lighting looks: the keyboard effect, speed, brightness (or per-key colours) and the rear light, e.g. `look save wave-dim` |
| `startup boot [NAME\|current\|clear]` | the look applied at BOOT (before login) and after every resume from suspend, by the root service; no argument shows it |
| `startup show`, `startup set keyboard\|rear\|profile VALUE`, `startup apply` | what comes back at login: `last` (default, saved automatically), `default`, `off`, `none`; keyboard also `profile:NAME`; profile = a full profile applied first |
| `keyboard on\|off\|toggle`, `restore`, `brightness N` (0-50), `timeout on\|off` | backlight |
| `keyboard effect NAME --color --speed --brightness` | static breathing wave snake ripple neon raindrop lightning spot star fireball snow heartbeat (neon needs brightness 50) |
| `keyboard color RRGGBB` | change the colour of the running effect without leaving it |
| `keyboard paint w=ff0000 arrows=00ff00 esc=ffffff` | colour chosen keys or groups and keep every other key as it is |
| `keyboard fill C`, `gradient C1 C2`, `rainbow`, `keys "wasd=ff0000" --base 101010`, `list-keys`, `layout` | per-key RGB |
| `keyboard operating on\|off` | keyboard colour follows the power mode (re-sent on each change) |
| `keyboard profile list\|save\|load\|delete` | saved lighting, e.g. `profile save work gradient ff3300 6600ff` |
| `audio output speakers\|headphones\|toggle\|cycle\|status` | output switch (the two share one PCM) |
| `audio jack [auto\|headphones\|headset]` | how the 3.5 mm jack is used: auto = what the codec detects, headphones = never use the headset mic, headset = always use it (try this when the codec misses a headset plugged in at boot) |
| `audio mic status\|mute\|unmute\|toggle\|boost`, `audio jack` | microphones, headset-mic boost, what the codec sees on the jack |
| `audio voice status\|headset MODE\|array MODE\|apply\|watch` | AI noise reduction; MODE = off, conference, personal |
| `audio speaker-filter`, `audio dts` | noise filter on the output (off by default); Acer DTS via AcerUltraX |
| `keys show\|set GESTURE ACTION\|reset\|test ACTION\|actions\|timing` | the PREDATOR key (see below) |
| `profile list\|show\|save\|apply\|delete\|next` | bundles of power, fans, lighting, battery, audio |
| `cpu info`, `cpu limits PL1 PL2`, `cpu undervolt ...` | RAPL limits (root); undervolt needs `--confirm` |
| `touchpad on\|off\|toggle` | touchpad inhibit |
| `service status\|restart\|logs`, `config show\|validate\|path`, `completion bash\|zsh\|fish`, `gui` | housekeeping |

## The window
`truesense gui` (or the TrueSense launcher): a short home page with power mode, GPU mode and fans (in the style of PreySense and G-Helper), and a button grid to the other pages: Lighting (keyboard and rear light bar), Per-key colours (a picture of the keyboard to select keys from), Battery and Screen, Audio (output, 3.5 mm jack, microphones, AI noise reduction, DTS) and Settings (PREDATOR key, what comes back at login, CPU power limits). `truesense gui --page lighting|keys|battery|audio|settings` opens a page directly; a second launch brings the open window forward. Colours and sliders apply live. Needs `python-pyqt6`.

`truesense-fast fan smart|auto|max|<20-100>`, `truesense-fast power <mode>` and `truesense-fast auto on|off` talk to the daemon directly (about 5 ms) for status-bar buttons; they fall back to the CLI.

## The PREDATOR key and Fn keys
`truesense keys set GESTURE ACTION` with GESTURE = `tap`, `double`, `triple`, `long`, `fn_esc`. `truesense keys actions` lists every action: `power-next`, `power:turbo`, `power-cycle:eco,balanced,turbo`, `fan:smart`, `fan-max-toggle`, `profile:gaming`, `output-toggle`, `mic-toggle`, `voice-next`, `touchpad-toggle`, `keyboard-effect-next`, `rear:toggle`, `battery-limit-toggle`, `overdrive-toggle`, `exec:<command>`, `notify:<text>`, `none`. Actions are checked when you set them, never at key-press time. `truesense keys test ACTION` runs one now. Fn+F11/F12 brightness is done by the keyboard itself and is left alone. Defaults: tap = next power mode, double = output toggle, long = fans to max (toggle).

## Services
* `truesense.service` (root, `Type=notify`, watchdog, always restarted): smart fan curve, thermal governor, power limits, automatic profile, IPC socket `/run/truesense/truesense.sock` (root and the configured user only). On stop, crash or exit the fans return to firmware Auto and clock caps are lifted.
* `truesense-keys.service` and `truesense-audio.service` (user): PREDATOR/Fn keys; switches the right microphone filter on plug events.
* `truesense-startup.service` (user, oneshot): restores the saved lighting (and optionally a profile) at login; see `truesense startup`.
* `truesense-health.timer`: restarts a stalled daemon and rebuilds the module after a kernel update.

## Configuration
`/etc/truesense/config.toml` (system: `user`, `[daemon]`, `[fan]`, `[cpu]`, `[profile]`, `[keyboard]`) and `~/.config/truesense/config.toml` (yours: `[keys]`, `[audio]`, `[osd]`). `truesense config show` prints the merged result and `config validate` checks it. Profiles live in `~/.config/truesense/profiles/*.toml` (four ship in `<prefix>/share/truesense/profiles`), lighting in `~/.config/truesense/lighting/*.json`. Live state for status bars: `/run/truesense/state` and `/tmp/truesense-state` (`key=value` lines: power_mode, fan, rpm1, rpm2, tcpu, tgpu, auto, ac, ...).

## Purified Voice
Acer's Purified Voice uses a proprietary Realtek model that cannot run on Linux. TrueSense builds an open equivalent: a PipeWire filter chain with RNNoise voice-activity gating, one filter per input (headset mic, built-in array). `conference` is gentle, `personal` is strict (can clip very soft words). The default microphone follows the active input and the filters survive plug/unplug events.

## Desktop integration
`contrib/noctalia/` holds Noctalia bar widgets (power mode, fan, RGB with panels) that read the state file and call the CLI. Copy them into a plugin that declares `profile`, `fan`, `rgb` widgets and panels. Any status bar can read `/tmp/truesense-state` or run `truesense --json status`.

## Troubleshooting
* Run `truesense doctor` first.
* `/dev/truesense` missing: `sudo modprobe truesense_wmi`; after a kernel update the health timer rebuilds within minutes (or `sudo dkms autoinstall`).
* Fans stuck: `truesense fan auto`; stopping the daemon also hands them back.
* Headset mic missing: unplug and replug (the codec classifies it on the plug event). **Never write `/sys/class/sound/hwC*D*/reconfig`**: it deadlocked the kernel here and needed a hard reboot.
* A firmware value reads -1 right after a write: normal; the tool retries for 1 s.
* Another fan or profile daemon (older scripts, `tuned`, `auto-cpufreq` also caps frequency) will fight TrueSense: stop it.

## Safety rules
Read the current value, write, read back, keep a restore path. Fans return to firmware Auto on any exit. No firmware flashing, no boot-logo writes, no EC access outside the whitelisted WMI calls. Undervolt needs `--confirm`, steps are limited, and `undervolt reset` writes 0.

## Porting to another model
`sudo tools/collect_hardware_data.sh` (read-only) collects ACPI/codec facts. **Never publish the raw ACPI dump**: it contains firmware tables, including one that holds the OEM Windows product key.
