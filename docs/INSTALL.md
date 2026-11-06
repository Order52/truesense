# Installing TrueSense

Requires: an Acer Predator Helios 16 **PH16-71**, systemd, Linux with kernel headers available, Python 3.11+.

## One line
```
curl -fsSL https://raw.githubusercontent.com/Order52/truesense/main/tools/get.sh | sudo bash
curl -fsSL https://raw.githubusercontent.com/Order52/truesense/main/tools/get.sh | sudo bash -s -- --no-gui   # extra options
```
`get.sh` finds the newest release, downloads it, **checks it against the release's `SHA256SUMS`** (a mismatch, or a missing list on a new release, stops the install), and runs `tools/install.sh`. Pin a version with `TRUESENSE_VERSION=0.3.15`, also require an exact hash with `TRUESENSE_SHA256=<hash>`, and with the GitHub CLI installed add `TRUESENSE_VERIFY_ATTESTATION=1` to check the signed build provenance too. Read the script first if you like: it is ~20 lines.

## From a clone
```
tools/install.sh --check     # read-only: distribution, model, what is missing (no root)
sudo tools/install.sh        # installs missing prerequisites, then TrueSense
```

## Options
| option | meaning |
|---|---|
| `--check` | report only, change nothing |
| `--no-deps` | never run the package manager, only report what is missing |
| `--no-gui` | do not install the window's Qt dependency |
| `--with-audio` | also install the RNNoise plugin where packaged (Arch) |
| `--no-services` | install files, do not enable/start services |
| `--module-only` | only the kernel module |
| `--user=NAME` | desktop user to set up (default: who ran sudo) |
| `--prefix=DIR` | install prefix (default `/usr/local`) |
| `--force` | allow a model other than PH16-71 (unsupported) |
| `--uninstall` | remove (same as `tools/uninstall.sh [--purge]`) |

## What it does for each distribution
| family | prerequisites installed with | headers package |
|---|---|---|
| Arch, CachyOS, Manjaro, EndeavourOS | `pacman -S --needed` | `<pkgbase>-headers` of the running kernel |
| Debian, Ubuntu, Mint, Pop | `apt-get install` | `linux-headers-$(uname -r)` |
| Fedora, Nobara | `dnf install` | `kernel-devel-$(uname -r)` |
| openSUSE | `zypper install` | `kernel-default-devel` |
Anything else: it tells you exactly what to install by hand.

Safety: the module is test-built in a scratch folder first (nothing changes if it does not compile); the program files are installed beside the old ones and swapped; if `truesense.service` does not come up within 15 s the previous version is restored. Everything is logged to `/var/log/truesense-install.log`. The installer is safe to re-run.

Secure Boot: DKMS modules must be signed; the installer warns if the DKMS key is not enrolled (`sudo mokutil --import /var/lib/dkms/mok.pub`, reboot).

## Updating
Updating is the same one click, and it is safe to repeat:
- **From the window**: Settings, Update (it checks once a day if you allow it; one request, no identifiers). **From a terminal**: `truesense update check`, then `sudo truesense update apply`. Or run the one-line install again.
- The updater downloads the release's published file and **verifies its SHA256 against `SHA256SUMS`** before anything runs as root. Releases from 0.3.16 on are built by CI (`.github/workflows/release.yml`) from the tag, byte-for-byte reproducibly, with a signed provenance attestation: `gh attestation verify truesense-X.Y.Z.tar.gz --repo Order52/truesense`.
- The installer test-builds the kernel module **before** it changes anything, swaps the program files atomically, restarts the service, and **rolls back to the previous version** if the service does not come up. The fans are handed back to the firmware whenever the service stops.
- After a kernel update, DKMS rebuilds the module by itself; if it ever does not, the health timer rebuilds it within minutes (`truesense service repair` does it now).

## Services
| unit | what | scope |
|---|---|---|
| `truesense.service` | the daemon: fans, power modes, Smart auto (watchdog, restarts itself, gives the fans back to the firmware however it ends) | system |
| `truesense-health.timer` | every 30 s: restart a stalled daemon, rebuild a missing module once per 10 min | system |
| `truesense-keys.service`, `truesense-audio.service`, `truesense-startup.service` | Fn/PREDATOR keys, microphone and sound handling, look at login | user |
| `truesense-update.timer` | daily update check (only if you turned it on) | user |

`truesense service check` lists every unit, the kernel module and the control socket (read-only, exit code 1 if something is down). `truesense service repair` fixes what it finds: re-reads the unit files, rebuilds and loads the module, enables and starts what is stopped, restarts a daemon that does not answer. It is idempotent (a second run changes nothing), never touches a setting or a fan, and asks for `sudo` only for the system part. `truesense service logs` shows the journal.

## Other models
TrueSense talks to firmware, so it only writes to laptops it has been **verified** on (today: **Predator PH16-71**, BIOS V1.18; other BIOS versions of the same model are expected to work and `doctor` says when yours is not the verified one).
- **Same model, any distribution or kernel**: supported; CI builds the module weekly against current Arch, Fedora and Ubuntu kernels and opens an issue before an update can break it.
- **A similar Acer Predator/Nitro** (for example PH16-72, PH18-71, PHN16-71): **unverified**. The kernel module does not bind and the installer refuses without `--force`; nothing is written to the firmware. The safe way to help: `sudo modprobe truesense_wmi force=1` (this only lets the module load), then `truesense probe --json`. The probe **only reads** (a test proves it can send nothing but a fixed list of read commands), prints the model, BIOS, which firmware read-outs answer and whether the Acer gaming WMI interface is present, and contains no serial number. Post it with the "Another Predator model" issue form. Once it matches the verified model a release adds the model to the supported list. Undo with `sudo modprobe -r truesense_wmi`.
- **`--force`** installs anyway and can misbehave (wrong fan registers, wrong modes). Use it only after reading `docs/SAFETY.md`.

## Arch package (AUR)
`packaging/aur/PKGBUILD.template` builds from the tagged release tarball; the maintainer publishes with `tools/aur-publish.sh X.Y.Z` (needs an AUR account and SSH key). After that: `yay -S truesense`. Official Arch repositories require a Trusted User/package maintainer to adopt the package; it cannot be pushed there directly.

## Status
Tested end to end on CachyOS (Arch family) only. For Debian, Ubuntu, Fedora and openSUSE, CI checks on every push that the installer recognises the distribution and names the right packages, and that the kernel module builds against each one's kernel headers; a full install on those systems is **untested on real hardware**. Reports and fixes welcome.
