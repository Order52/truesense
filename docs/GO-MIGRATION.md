# Migrating TrueSense to Go: the plan

Status: in progress on the `go-migration` branch. This document is the contract every part of the port follows.

## Why, and what does not change

TrueSense controls firmware from a root daemon. Go gives it: one static binary (no Python version to track, nothing to `pip`), start-up in milliseconds, a smaller memory footprint
(one process instead of three interpreters), memory safety, a standard library that fits a socket daemon, and first-class tooling (`go test -race`, fuzzing, `govulncheck`, reproducible builds).

What does **not** change:
- the **kernel module** (`kernel/truesense-wmi`, C, DKMS) and its text protocol: it is the firmware interface and it is verified on hardware;
- the **wire protocol** of the control socket (one JSON object per line, `{"cmd":..,"args":{..}}` -> `{"ok":true,"result":..}`), so the existing clients can be pointed at the new daemon while the port is verified;
- every **safety rule** in `docs/SAFETY.md` (see "Safety invariants" below);
- the user-visible commands, file locations and settings, so an update from 0.3.x keeps working.

## Decisions (with the research behind them)

| Question | Decision | Why |
|---|---|---|
| Layout | `cmd/<binary>/` thin mains, all logic in `internal/`, no `pkg/` | Go team's "Organizing a Go module": the compiler enforces `internal/`; `pkg/` is only for deliberately public APIs; mains only parse flags, wire dependencies and call `Run()` |
| Binaries | one multi-call binary `truesense` (`truesense-fast` is a link that selects the fast path by `argv[0]`) | one thing to install, sign and update; start-up cost does not depend on binary size |
| Dependencies | the standard library, `golang.org/x/sys`, `github.com/pelletier/go-toml/v2` (existing `config.toml` files must keep working), `github.com/gdamore/tcell/v2` (TUI), `github.com/ebitengine/purego` (dlopen without cgo, used only by the GPU undervolt to call NVML: `nvidia-smi` cannot set the curve offset) | every dependency is supply-chain surface in a root program; tcell is the terminal layer under most Go TUIs and the existing TUI's `Canvas` design ports onto it unchanged |
| systemd | `Type=notify`, `WatchdogSec`, ping at half the interval, `STOPPING=1` on shutdown; a ~40-line `sdnotify` package instead of a library | matches go-systemd/mdlayher guidance, no dependency; `NOTIFY_SOCKET` unset means a silent no-op so the same binary runs in a terminal and in tests |
| Logging | `log/slog` (text to the journal) | standard, structured, leveled |
| Concurrency | `context.Context` for cancellation, one goroutine per loop, state behind a mutex-guarded store, no global mutable state outside the simulator | testable, race-detector clean |
| Errors | wrapped with `%w`, typed `HwError` for firmware errors, never `panic` in library code; the daemon recovers per request and per loop tick and returns the fans to firmware Auto on any fatal path | the daemon must never leave fans stopped |
| Build | `CGO_ENABLED=0 go build -trimpath -buildvcs=false -ldflags "-s -w -X ...buildinfo.Version=..."`, `SOURCE_DATE_EPOCH` = commit time | reproducible static binary (GoReleaser's recipe, without GoReleaser: the release job is already ours) |
| Tests | table-driven, `-race -shuffle=on`, fuzz targets for every parser of untrusted input (socket requests, firmware replies, tar, SHA256SUMS), golden parity tests against the Python implementation, a simulated firmware for end-to-end tests | the 244 Python tests are the specification: each behaviour is ported, not re-invented |
| Lint/security | `go vet`, `staticcheck`, `golangci-lint` (errcheck, govet, staticcheck, revive, gosec, errorlint, bodyclose-free), `govulncheck` in CI, CodeQL for Go | official Go security guidance |
| TUI | `tcell` + the existing Canvas/Row/Data/Worker design | already hardware-tested; `tcell.SimulationScreen` replaces the text grid in tests |
| Window | a local web UI embedded in the binary (`go:embed`): `truesense gui` starts a per-user loopback server (random port, one-time token, serves the app and forwards commands to the daemon socket as that user) and opens it as an app window | Qt has no maintained Go binding; a cgo-free web UI keeps the binary static and works in any browser; the daemon's uid check still applies because the forwarder connects as the user |

## Architecture

Dependencies only point **down** this list; a package may import packages below it, never above. Each layer is one concern.

```
cmd/truesense                      thin main: app.Run(os.Args)
internal/app                       argv[0] / subcommand dispatch
------------------------------------------------------------------ clients
internal/cli  internal/tui  internal/gui  internal/keys(service)  internal/audioservice
internal/client                    typed API to the daemon, falls back to direct hardware when there is no daemon
------------------------------------------------------------------ daemon
internal/daemon                    loops, state, watchdogs, request handlers (wiring only)
internal/ipc                       wire protocol, server (SO_PEERCRED auth), client
------------------------------------------------------------------ domain
internal/autoprofile  internal/smartprofile  internal/curves     decisions (pure functions of readings)
internal/power  internal/undervolt  internal/powermgr           CPU/GPU limits, MSR, conflicting power managers
internal/keyboard  internal/lighting(looks, perkey effects)      keyboard HID, rear bar looks
internal/audio                                                 PipeWire/DTS/microphone
internal/profiles  internal/keybinds  internal/actions         user-level bundles and key actions
internal/models  internal/doctor  internal/selftest  internal/services  internal/update
------------------------------------------------------------------ platform
internal/hw                        typed firmware features (modes, fans, battery, rear, sensors) with read-back
internal/firmware                  Device interface: kernel module (real) and simulator (TRUESENSE_SIM=1)
internal/config  internal/userstate  internal/fsx  internal/sdnotify  internal/buildinfo
```

Rules: no package-level mutable state except `firmware`'s simulator; every external effect (files, processes, time, sockets) goes through a small interface or an injected function so the logic is testable without root or hardware; a function either decides or does, never both.

## Safety invariants (each has a test)

1. Firmware calls go only through the kernel module's whitelisted commands; the kernel validates every argument and the Go side validates again before sending.
2. Stopping a fan needs the daemon, an explicit confirm and the temperature watchdog (CPU 70 degC / GPU 65 degC returns the fans); a fan duty is 0 or 20..100 and nothing else.
3. The daemon returns the fans to firmware Auto and lifts clock caps on stop, crash, panic and signal; the watchdog ping stops if the control loop stalls.
4. Undervolts keep their crash guard (a pending flag file removed only after a clean boot), and are rejected outside the safety zones.
5. Every control request is authorised by the caller's uid (`SO_PEERCRED`): root or the configured desktop user.
6. Downloads are verified (SHA256SUMS) before anything runs as root; tar extraction rejects traversal and links.
7. A write is read back and verified; a value that cannot be verified is an error, not a success.
8. `truesense probe` and unverified models send read-only commands only.

## Phases and ownership

0. Plan, branch, toolchain, CI skeleton (this document).
1. Foundation: `buildinfo`, `fsx`, `sdnotify`, `firmware` (+ simulator), `hw`, `ipc`, `config`, `userstate`, `models`.
2. Parallel: **daemon** (curves, smartprofile, autoprofile, power, undervolt, powermgr, daemon, state); **lighting** (keyboard HID, per-key, effects, looks, keyprofiles, designs); **system** (audio, doctor, services, update, selftest).
3. Parallel: **keys** (fnkeys, keybinds, actions, osd, profiles, startup); **cli**; **tui**; **gui** (web).
4. Integration: installer, units, packaging (PKGBUILD from source with `go build`), CI/CD (Go jobs, govulncheck, fuzz smoke, release builds the binary), docs.
5. Verification: Go test suite with `-race`; golden parity against the Python CLI in simulation; Go daemon with the unchanged Python clients; live run on the PH16-71 (restoring every setting); install, update-from-0.3.17, rollback.
6. Cut over: remove the Python implementation, release 0.4.0.

## Verification strategy

- **Behaviour parity:** the Python tests are ported case by case into Go tests (same inputs, same expected outputs).
- **Golden parity:** `tools/parity/` runs the same commands in simulation with the Python and the Go CLI and diffs the normalised `--json` output.
- **Protocol parity:** the Go daemon is exercised by the existing Python clients before they are removed.
- **Live:** the real daemon on the real laptop, every setting changed and restored (fans never stopped on hardware without an explicit confirm and the watchdog).
- **Performance:** start-up, idle CPU and resident memory are measured and recorded against the Python numbers (daemon 20 MB / 0.6 % CPU; three processes about 59 MB).

## Conventions for all Go code

- `gofmt`/`goimports` clean; `go vet`, `staticcheck` and the repo's `golangci-lint` config pass; `go test -race -shuffle=on ./...` passes.
- Package doc comment on every package; doc comment on every exported identifier; comments explain *why*.
- Small packages with one purpose, constructors take their dependencies; `context.Context` first parameter for anything that blocks or does I/O.
- No `init()` side effects, no panics for expected failures, errors carry the operation (`fmt.Errorf("set fan manual: %w", err)`).
- Tests next to the code (`foo_test.go`), table-driven, hermetic (temp dirs, injected clocks and runners); a test never touches the real daemon, sound system, firmware or network.
- Fuzz targets (`FuzzXxx`) for parsers; seed corpus in `testdata/fuzz`.
- User-visible strings and file formats stay compatible with 0.3.x unless this document says otherwise.
