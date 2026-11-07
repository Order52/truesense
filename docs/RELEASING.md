# Releasing

One command prepares a release; pushing the tag publishes it. Nothing is built or uploaded by hand, and nothing in the process needs Python: the release tool, the link checker and the installer tests are Go, the program is one static binary.

```
# 1. write what changed under "## Unreleased" in RELEASE_NOTES.md (as you merge changes)
# 2. prepare: bump every version, retitle the notes, run the gate (make go-ci and the shell checks), commit, tag
go run ./cmd/release prepare 0.4.0 --title "short title of the release"
# 3. publish: the tag push starts .github/workflows/release.yml
git push origin HEAD v0.4.0          # or:  go run ./cmd/release prepare ... --push
```
`prepare` refuses a dirty tree, a version that is not newer, an existing tag, empty "Unreleased" notes, or a release without a title. `--dry-run` shows what it would do; `--no-gate` skips the gate (CI runs it again anyway). The other commands: `go run ./cmd/release check [--tag vX.Y.Z]` (do the version numbers, notes and tag agree?; `make release-check` runs it), `version`, and `notes X.Y.Z` (the GitHub release text).

The version lives in `internal/buildinfo/buildinfo.go` (`const Version = "X.Y.Z"`, compiled into the binary) and the release tool knows every other place that must say the same: the README badge, `docs/INSTALL.md`, the kernel module (`dkms.conf` and `truesense_wmi.c`), `tools/aur-publish.sh`, `tools/get.sh` and both PKGBUILDs. The table is in `internal/release/table.go`; `prepare` rewrites only those places.

## What the pipeline does (release.yml)
1. **verify**: the tag, `buildinfo.Version`, the kernel module, DKMS, both PKGBUILDs, `get.sh`, the README badge and the release notes all agree (`cmd/release check --tag vX.Y.Z`); the tagged commit is on `main`; `go.mod` and `go.sum` are committed; `go vet`, the tests with the race detector, `govulncheck` and a module build pass.
2. **build**: the static `linux/amd64` program (`CGO_ENABLED=0`, `-trimpath`, commit and date from git) and the **bundle** `truesense-X.Y.Z.tar.gz`, built by `tools/ci/build-bundle.sh` **twice and compared byte for byte**, then checked by `tools/ci/check-bundle.sh` (complete, the program inside runs and says `truesense X.Y.Z`, static). The release notes are cut from `RELEASE_NOTES.md`, and a CycloneDX **SBOM** of the program that ships is generated with a pinned `cyclonedx-gomod`.
3. **package-arch**: the Arch package, built with `makepkg` as an unprivileged user in a clean container (it builds the program with `go build`, makedepends `go`).
4. **publish**: `SHA256SUMS` over the bundle, the package and the SBOM, a signed **build provenance attestation** for every file including `SHA256SUMS` (Sigstore, no keys to keep), then the GitHub release with all of it and the notes. It finally runs `tools/get.sh` against the published release exactly as a user would, with the checksum **and** the attestation check on.
5. **aur** (only if the `AUR_SSH_PRIVATE_KEY` secret is set): pushes the PKGBUILD (it refuses a tag whose source does not declare the version). Otherwise run `tools/aur-publish.sh X.Y.Z` yourself.

Re-publish an existing tag, or rehearse without publishing: Actions, Release, Run workflow, give the tag, leave "dry run" on.

## What a release contains
| File | What it is |
|---|---|
| `truesense-X.Y.Z.tar.gz` | the **bundle**: everything `git archive` of the tag has, plus the prebuilt static program `bin/truesense`. Top directory `truesense-X.Y.Z/`. `tools/get.sh` downloads it and runs its `tools/install.sh`, which installs `bin/truesense` (and refuses one whose version differs from the source tree) |
| `truesense-X.Y.Z-1-x86_64.pkg.tar.zst` | the Arch package |
| `truesense-X.Y.Z.sbom.cdx.json` | the SBOM: the Go modules and versions compiled into the program |
| `SHA256SUMS` | checksums of the three files above |

The bundle is reproducible: sorted names, the commit's timestamp, owner `root`, `gzip -n`. Check out the tag, run `tools/ci/build-bundle.sh X.Y.Z /tmp/out` with the same Go version, and `sha256sum` gives the same hash as the release (the Go version is recorded in the SBOM and in `go version -m bin/truesense`).

## Verifying a download
```
sha256sum -c --ignore-missing SHA256SUMS                      # the file you downloaded is the one that was published
gh attestation verify truesense-X.Y.Z.tar.gz --repo Order52/truesense   # and CI built it from this repository (any file, SHA256SUMS included)
go version -m bin/truesense                                   # after unpacking: Go version and every module in the program
```
The one-line installer does the first check always and the second when you set `TRUESENSE_VERIFY_ATTESTATION=1` (it needs the GitHub CLI). The SBOM is a standard CycloneDX file: feed it to any vulnerability scanner (`grype sbom:truesense-X.Y.Z.sbom.cdx.json`).

## Everyday CI (ci.yml)
Every push and pull request:
- **Static checks**: `go.mod` tidy and tracked, `gofmt`, `go vet`, `staticcheck`, `golangci-lint` (the repo's `.golangci.yml`), `govulncheck`, shellcheck, actionlint, the version consistency check (`cmd/release`), systemd unit verification (with a hardening-score ratchet that may only improve), desktop entries, documentation links (`cmd/checklinks`).
- **Tests**: `go test -race -shuffle=on` with a coverage summary, then a few seconds of `go test -fuzz` on every `Fuzz` target (`make go-fuzz`). The installer and `get.sh` tests (`tools/shelltest`) run the scripts with bash against fake os-release files, fake DMI data, fake programs and a local fake GitHub.
- **Build**: the static binary built twice and compared, `ldd` says static, `truesense --version` matches `buildinfo`, and the release bundle is built twice and checked exactly as the release does.
- **Kernel module** against the headers of Arch (linux and linux-lts), Ubuntu 22.04 and 24.04, Debian and Fedora; **installer** (`tools/install.sh --check`) on Arch, Ubuntu, Debian, Fedora and openSUSE (these containers have no Python, and the check fails if the installer asks for it); the **Arch package**.

`CI passed` is the single check to require in branch protection. **Kernel watch** (kernels.yml) rebuilds the module against the newest kernels every Monday and opens one issue when a kernel breaks it. **CodeQL** scans Go and C weekly and on pull requests. Dependabot keeps the actions and the Go modules current. Every action is pinned to a full commit SHA.

Run the same gates locally: `make go-ci` (lint, race tests, govulncheck), `tools/ci/check-shell.sh`, `go run ./cmd/release check`, `go run ./cmd/checklinks`.

## One-time repository settings (Settings in GitHub)
- Branch protection on `main`: require the `CI passed` check and a linear history.
- Actions, General: workflow permissions "Read repository contents" (the workflows ask for more only where they need it).
- For AUR publishing: add the `AUR_SSH_PRIVATE_KEY` secret (an SSH key registered on the AUR account that owns the package).
