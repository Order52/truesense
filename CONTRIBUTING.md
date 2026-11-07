# Contributing

Thanks for helping. TrueSense drives real hardware, so the rules are about safety and honesty.

## Ground rules
- **Licence**: contributions are under the project's MIT licence (the kernel module is dual MIT / GPL-2.0-only). Only submit code you wrote or have the right to publish; never copy code from GPL projects or from vendor software. See [docs/LEGAL.md](docs/LEGAL.md).
- **Never claim something works without a read-back or a real test.** Mark new features VERIFIED, SIMULATED or UNTESTED in [docs/STATUS.md](docs/STATUS.md).
- **Safety first**: every write is read, written and read back; anything that can hurt (voltages, fans, power limits) needs a confirmation, safe limits and a way back. No firmware flashing, no embedded-controller access outside documented WMI calls, never write the codec `reconfig` file.
- **No proprietary files**: nothing from Acer, DTS or Realtek, and no raw ACPI dumps (they contain the OEM Windows product key).
- Other models: the module is DMI-guarded on purpose. Porting is welcome; send the output of `tools/collect_hardware_data.sh` **after reading it** (it is read-only) and never the raw tables.

## Set up
```
git clone https://github.com/Order52/truesense && cd truesense
make go-test                    # the whole suite, against the built-in simulator (no hardware needed)
make go-ci                      # what CI runs: gofmt, vet, staticcheck, golangci-lint, race-detector tests, govulncheck
make build                      # dist/truesense, the static binary
TRUESENSE_SIM=1 dist/truesense gui   # run the window from the checkout on the simulator
```
Go 1.26 or newer (see `go.mod`). `TRUESENSE_SIM=1` runs everything on the simulator. The tests never touch your real service, config or lighting memory.

## Changing code
- Go, and as few dependencies as possible (every one is supply-chain surface in a root program; see the table in [docs/GO-MIGRATION.md](docs/GO-MIGRATION.md)). The layers and their dependency rules are in [docs/ARCHITECTURE.md](docs/ARCHITECTURE.md): decisions are pure, effects are injected, the CLI, terminal UI and window are thin and share `internal/api`.
- New setting: `internal/firmware` / `internal/hw` (typed, read back), a daemon handler in `internal/daemon` if it needs privilege or persistence, an `internal/api` method, a CLI verb, a terminal UI row and a window control, a table-driven test (the simulator mirrors the kernel's validation), a line in USAGE and STATUS.
- New profile kind: follow `internal/curves` / `internal/smartprofile` (built-ins read-only incl. `native`, copy to edit, validate everything).
- Anything that parses input from outside (socket requests, firmware replies, archives, files) gets a fuzz target.
- Kernel module: `make module-test` builds it for every installed kernel with warnings on; keep it warning-free.
- Run `make go-ci` before you push (`gofmt`, `go vet`, `staticcheck`, `golangci-lint` with the repo's `.golangci.yml`, the race-detector tests and `govulncheck`; tools that are not installed are skipped locally, CI always runs them), and `make ci` for the shell, workflow and documentation checks.

## Releasing and CI
See docs/RELEASING.md: `go run ./cmd/release prepare X.Y.Z --title "..."` then push the tag. CI runs on every push (gofmt, vet, staticcheck, golangci-lint, govulncheck, the race-detector tests and a short fuzz pass, a reproducible build check, the module on several distributions' kernels, the installer on five distribution families, the Arch package).

## Reporting
Use the issue templates. Include `truesense doctor --json`.
