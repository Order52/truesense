# Architecture

TrueSense is one static Go program (`truesense`: the daemon, the command line, the terminal UI, the window and the user services; `truesense-fast` is the same binary under another name) on top of one small C kernel module. This page is the map; the reasoning behind the design is in [GO-MIGRATION.md](GO-MIGRATION.md).

```
                  ┌────────────────────────────────────────────────────────────┐
  firmware        │ ACPI-WMI: gaming (fans, modes, rear light, ...), APGe, battery │
                  └───────────────▲────────────────────────────────────────────┘
                                  │ stable WMI API only
                         truesense_wmi (kernel module, C, DKMS)   /dev/truesense: text protocol
                                  ▲
        truesense daemon (root)   │  state in /var/lib/truesense, live state in /run/truesense
        ├ smart fan, automatic power mode, power limits, undervolt, boot lighting, watchdogs
        └ control socket /run/truesense/truesense.sock  (JSON lines, SO_PEERCRED check)
                 ▲            ▲              ▲                 ▲
              CLI        terminal UI     window (web)      key service           status bars
                                   (all as the user)       hidraw: PREDATOR/Fn   (truesense-fast, state file)
```

## Layers

Dependencies point **down** this list; a package may import packages below it and never above. Every layer is one concern, every external effect (files, processes, sockets, the clock) is injected, and decisions are separate from doing.

| Layer | Packages | What |
|---|---|---|
| Entry | `cmd/truesense`, `internal/app` | `main` only wires: it picks daemon, CLI, TUI, window or a user service from the arguments and the program name |
| Clients | `internal/cli`, `internal/tui`, `internal/gui`, `internal/fnkeys`, `internal/startup`, `internal/api` | the command tree, the curses-style terminal UI (tcell), the local web window, the key service; `api` is the application layer they share |
| Assembly | `internal/assembly` | builds every concrete component once (`Services`), adapts packages that are deliberately independent, starts the daemon with its real dependencies |
| Daemon | `internal/daemon`, `internal/ipc` | the control loop, the request handlers and the socket protocol; the daemon is wiring only: it decides nothing itself |
| Decisions | `internal/autoprofile`, `internal/fanctl`, `internal/curves`, `internal/smartprofile`, `internal/thermal`, `internal/chargefull`, `internal/crashguard`, `internal/gamerules` | pure functions and state machines of readings: testable at any speed without a laptop |
| Domain | `internal/keyboard`, `internal/looks`, `internal/keyprofiles`, `internal/designs`, `internal/perkeyfx`, `internal/keymap`, `internal/audio`, `internal/power`, `internal/powermgr`, `internal/undervolt`, `internal/gpuundervolt`, `internal/profiles`, `internal/keybinds`, `internal/actions` | one feature each, behind small interfaces |
| System | `internal/models`, `internal/doctor`, `internal/selftest`, `internal/services`, `internal/update`, `internal/osd`, `internal/touchpad`, `internal/sensors` | health, updates, model support, the machine's own readings |
| Platform | `internal/hw`, `internal/firmware` | typed firmware features with read-back; the kernel module's protocol and its simulator |
| Foundation | `internal/config`, `internal/state`, `internal/userstate`, `internal/fsx`, `internal/sdnotify`, `internal/buildinfo` | settings, persistence, atomic files, systemd notification |

- **`kernel/truesense-wmi`** is one small C module. DMI-guarded (`Acer` / `Predator PH16-71`, `force=1` overrides), a misc device `/dev/truesense`, a text protocol (`get_fan`, `set_mode 5`, ...), a global command mutex, a per-open-file reply buffer (no shared-buffer race), strict validation of every argument, fans back to firmware Auto on unbind. DKMS rebuilds it for every installed kernel.
- **`firmware`** speaks that protocol; `TRUESENSE_SIM=1` swaps in an in-memory simulator that mirrors every kernel validation, so everything above runs unchanged without hardware. **`hw`** turns firmware commands into typed functions that write, read back (retrying for about a second) and return the verified value or an error.
- **`daemon`** (`truesense.service`, `Type=notify`, watchdog ping at half the interval, `Restart=always`) owns what must be privileged or persistent: the smart fan, the automatic power mode, power limits (RAPL), the thermal clock cap, undervolts (MSR 0x150 and NVML), boot lighting, charge-once and the watchdogs (a stopped fan, the boot-undervolt crash guard). Nothing smart runs on a fresh install. On stop, crash, panic or signal the fans go back to firmware Auto and the clock cap is lifted; a step that fails while the machine is hot drives the fans to 100 %.
- **`ipc`**: one JSON object per line over a UNIX socket; every request is authorised by the caller's uid (`SO_PEERCRED`). Requests are parsed defensively and fuzzed; a panicking handler becomes an error reply, never a dead daemon. `client` is the fast path for fans and power (one request, no settle delay) with a direct-hardware fallback for what is safe without the service.
- **`models`** knows which laptops are verified (one entry per model, added only with a hardware report), classifies the machine as *supported*, *unverified* (another Acer Predator or Nitro) or *unsupported*, and runs `truesense probe`: a read-only look at the firmware that sends only the commands in `ReadOnly` (a test proves nothing else is ever sent). `tools/install.sh` applies the same tiers before it builds anything.
- **`services`** is `truesense service check` (read-only) and `service repair` (idempotent: reload units, rebuild and load the module, enable and start what is down; it never touches a fan or a setting). **`update`** downloads a release, checks it against the release's `SHA256SUMS` (required from 0.3.16 on, never skipped when offline), unpacks it with an extractor that refuses links, special files and path traversal by construction, and hands over to `tools/install.sh`.
- **Profiles** are plain data with one shape everywhere: built-in and read-only (always including `native`), yours in `~/.config/truesense/<kind>/NAME.json`. Kinds: fan curves, Smart auto, PREDATOR key bindings, CPU undervolt, GPU undervolt, lighting (`looks`). "Use" sends or writes the chosen profile; the service keeps what it needs so it works before login.
- **The window** is a local web UI embedded in the binary. `truesense gui` starts a per-user loopback server (random port, a one-time token in the URL fragment, Host and cross-site checks, a strict Content-Security-Policy) that forwards commands to the daemon socket as that user, so the daemon's uid check still applies, and opens it as an app window. No Qt, no cgo.

## Files

| Path | What |
|---|---|
| `/etc/truesense/config.toml`, `~/.config/truesense/config.toml` | settings (system, then yours) |
| `/var/lib/truesense/state.json` | service settings that survive reboots (modes, active curve and profile, your power limits, boot undervolt) |
| `/var/lib/truesense/boot-look.json` | the lighting the service applies at boot and after suspend |
| `/var/lib/truesense/*-pending` | crash-guard flags for undervolt at boot |
| `/run/truesense/state`, `/tmp/truesense-state` | live `key=value` state for status bars; `health.sh` restarts a service whose state file goes stale |
| `~/.local/state/truesense/user.json` | what you last set on the lights (so brightness and colour keep the current effect) |

## Build, test and release

`make build` makes the static, reproducible binary (`CGO_ENABLED=0 -trimpath`); `make go-ci` is what CI runs: `gofmt`, `go vet`, `staticcheck`, `golangci-lint`, the race-detector tests (`-race -shuffle=on`) and `govulncheck`; `make go-fuzz` runs every fuzz target (the parsers of untrusted input: socket requests, firmware replies, archives, checksum lists, profiles). `.github/workflows/ci.yml` also builds the module on five distribution kernels and runs the installer's checks on five distributions; `release.yml` publishes a tag with `SHA256SUMS`, an SBOM and build provenance; `kernels.yml` watches new kernels weekly; CodeQL and Dependabot run on their own. `go run ./cmd/release` keeps every version number in one table. Details and the one-time repository settings: [RELEASING.md](RELEASING.md).

## Protocols

The kernel text protocol is documented at the top of `truesense_wmi.c` and in [PROTOCOL.md](PROTOCOL.md); the socket commands are the table in `internal/daemon/handlers.go`. Replies are `ok key=value ...` or `err message` (kernel) and `{"ok":true,"result":...}` or `{"ok":false,"error":"..."}` (socket).

## Testing

`go test ./...` runs everything against the firmware simulator: the daemon's behaviour tick by tick with a fake laptop, every handler through the real wire path, the decision engines at any speed, the terminal UI on a simulated screen (golden snapshots, a seeded stress run), and fuzz seeds. `truesense selftest` sets, reads back and restores every firmware setting on real hardware; `truesense doctor` checks the installation.
