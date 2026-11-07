# Architecture

```
                  ┌────────────────────────────────────────────────────────────┐
  firmware        │ ACPI-WMI: gaming (fans, modes, rear light, ...), APGe, battery │
                  └───────────────▲────────────────────────────────────────────┘
                                  │ stable WMI API only
                         truesense_wmi (kernel module, DKMS)   /dev/truesense: text protocol
                                  ▲
        truesense service (root)  │  state in /var/lib/truesense, live state in /run/truesense
        ├ smart fan, smart auto, power limits, undervolt, boot lighting, watchdogs
        └ control socket /run/truesense/truesense.sock  (JSON lines, SO_PEERCRED check)
                 ▲            ▲              ▲                 ▲
              CLI        window (Qt)    key service       status bars (truesense-fast, state file)
                                 (user)  hidraw: PREDATOR/Fn keys, keyboard lighting, key animations
```

## Layers

- **`kernel/truesense-wmi`** is one small module. DMI-guarded (`Acer` / `Predator PH16-71`, `force=1` overrides), a misc device `/dev/truesense`, a text protocol (`get_fan`, `set_mode 5`, ...), a global command mutex, a per-open-file reply buffer (no shared-buffer race), strict validation of every argument, fans back to firmware Auto on unbind. DKMS rebuilds it for every installed kernel.
- **`device.py`** speaks that protocol; `TRUESENSE_SIM=1` swaps in an in-memory simulator that mirrors every kernel validation, so everything above runs without hardware.
- **`backend.py`** turns firmware commands into functions with read-back and retry (`settle`).
- **`daemon.py`** (`truesense.service`, `Type=notify`, watchdog, `Restart=always`) owns what must be privileged or persistent: the smart fan (`curves.py`), Smart auto (`smartprofiles.py`), power limits (RAPL), the thermal clock cap, undervolts (MSR 0x150 and NVML), boot lighting, charge-once, and the watchdogs (fans-off, undervolt-at-boot). Nothing smart runs on a fresh install.
- **`ipc.py`**: one JSON object per line over a UNIX socket; every request is authorised by the caller's uid. `control.py` is the fast path for fans and power (one request, no settle delay).
- **Clients**: `cli.py` (every area, `--json`), `tui.py` (a curses terminal UI: a background reader, one row model for every setting, and a drawing layer the tests read as text), `gui.py` (Qt 6; a thin layer over the same functions, worker threads for slow calls, "newest value wins" for sliders), `fnkeys.py` (the key service: gestures, actions, hot reload, `perkeyfx.py` animations), `truesense-fast` (a 5 ms socket client for bars).
- **`models.py`** knows which laptops are verified (`SUPPORTED`, one entry per model, added only with a hardware report), classifies the machine as *supported*, *unverified* (another Acer Predator/Nitro) or *unsupported*, and runs `truesense probe`: a read-only look at the firmware that sends only the commands in `READ_ONLY` (a test proves nothing else is ever sent). `tools/install.sh` applies the same tiers before it builds anything.
- **`services.py`** is `truesense service check` (read-only: every unit, the kernel module, the control socket) and `service repair` (idempotent: reload units, rebuild and load the module, enable and start what is down, leave what runs alone; it never touches a fan or a setting).
- **`update.py`** downloads a release, checks it against the release's `SHA256SUMS` (required from 0.3.16 on, never skipped when offline), unpacks it with a path-traversal-safe extractor and hands over to `tools/install.sh`. `tools/get.sh` does the same for the one-line install.
- **Profiles** are plain data with one shape everywhere: built-in and read-only (always including `native`), yours in `~/.config/truesense/<kind>/NAME.json`. Kinds: fan curves, Smart auto, PREDATOR key bindings, CPU undervolt, GPU undervolt, lighting (`looks`). "Use" sends or writes the chosen profile; the service keeps what it needs so it works before login.

## Files

| Path | What |
|---|---|
| `/etc/truesense/config.toml`, `~/.config/truesense/config.toml` | settings (system, then yours) |
| `/var/lib/truesense/state.json` | service settings that survive reboots (modes, active curve and profile, your power limits, boot undervolt) |
| `/var/lib/truesense/boot-look.json` | the lighting the service applies at boot and after suspend |
| `/var/lib/truesense/*-pending` | crash-guard flags for undervolt at boot |
| `/run/truesense/state`, `/tmp/truesense-state` | live `key=value` state for status bars |
| `~/.local/state/truesense/user.json` | what you last set on the lights (so brightness and colour keep the current effect) |

## Build, test and release

`.github/workflows/ci.yml` runs on every push (lint, shellcheck, actionlint, unit verification, tests on Python 3.11 to 3.14, the module on five distribution kernels, the installer on five distributions, the Arch package); `release.yml` publishes a tag with `SHA256SUMS` and build provenance; `kernels.yml` watches new kernels weekly; CodeQL and Dependabot run on their own. `tools/release.py` keeps every version number in one table. Details and the one-time repository settings: [RELEASING.md](RELEASING.md).

## Protocols

The kernel text protocol and the socket commands are documented at the top of `truesense_wmi.c` and in `daemon.py` (`handlers`). Replies are `ok key=value ...` or `err message`.

## Testing

`make test` runs the unit tests against the simulator (service logic, every CLI command, the window's logic headlessly, profile rules, safety limits). `truesense selftest` sets, reads back and restores every firmware setting on real hardware; `truesense doctor` checks the installation. `tools/screenshots.py` rebuilds the pictures in `docs/images`.
