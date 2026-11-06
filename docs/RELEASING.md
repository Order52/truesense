# Releasing

One command prepares a release; pushing the tag publishes it. Nothing is built or uploaded by hand.

```
# 1. write what changed under "## Unreleased" in RELEASE_NOTES.md (as you merge changes)
# 2. prepare: bump every version, retitle the notes, run the gate (make check, tests, ruff), commit, tag
python3 tools/release.py prepare 0.3.16 --title "short title of the release"
# 3. publish: the tag push starts .github/workflows/release.yml
git push origin HEAD v0.3.16          # or:  python3 tools/release.py prepare ... --push
```
`prepare` refuses a dirty tree, a version that is not newer, an existing tag, empty "Unreleased" notes, or a release without a title. `--dry-run` shows what it would do; `--no-gate` skips the gate (CI runs it again anyway).

## What the pipeline does (release.yml)
1. **verify**: the tag, `__version__`, the kernel module, DKMS, both PKGBUILDs, `get.sh`, the README badge and the release notes all agree (`tools/release.py check --tag vX.Y.Z`); the tagged commit is on `main`; the full test suite, lint and a module build pass.
2. **build**: `truesense-X.Y.Z.tar.gz` straight from `git archive` (reproducible: it is built twice and compared), the release notes cut from `RELEASE_NOTES.md`, and a check that the tarball has the installer, the TUI, the module source and the units.
3. **package-arch**: the Arch package, built with `makepkg` as an unprivileged user in a clean container.
4. **publish**: `SHA256SUMS`, a signed **build provenance attestation** for every file (Sigstore, no keys to keep), then the GitHub release with the tarball, the package, the sums and the notes. It finally runs `tools/get.sh` against the published release exactly as a user would.
5. **aur** (only if the `AUR_SSH_PRIVATE_KEY` secret is set): pushes the PKGBUILD. Otherwise run `tools/aur-publish.sh X.Y.Z` yourself.

Re-publish an existing tag, or rehearse without publishing: Actions, Release, Run workflow, give the tag, leave "dry run" on.

## Everyday CI (ci.yml)
Every push and pull request: ruff, shellcheck, actionlint, version consistency, systemd unit verification (with a hardening-score ratchet that may only improve), desktop entries, documentation links; the tests on Python 3.11 to 3.14 with a coverage floor; the kernel module built against the headers of Arch (linux and linux-lts), Ubuntu 22.04 and 24.04, Debian and Fedora; the installer's distribution detection on Arch, Ubuntu, Debian, Fedora and openSUSE; the Arch package. `CI passed` is the single check to require in branch protection. **Kernel watch** (kernels.yml) rebuilds the module against the newest kernels every Monday and opens one issue when a kernel breaks it. **CodeQL** scans Python and C weekly and on pull requests. Dependabot keeps the actions current.

## One-time repository settings (Settings in GitHub)
- Branch protection on `main`: require the `CI passed` check and a linear history.
- Actions, General: workflow permissions "Read repository contents" (the workflows ask for more only where they need it).
- For AUR publishing: add the `AUR_SSH_PRIVATE_KEY` secret (an SSH key registered on the AUR account that owns the package).
