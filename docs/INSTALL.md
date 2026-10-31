# Installing TrueSense

Requires: an Acer Predator Helios 16 **PH16-71**, systemd, Linux with kernel headers available, Python 3.11+.

## One line
```
curl -fsSL https://raw.githubusercontent.com/Order52/truesense/main/tools/get.sh | sudo bash
curl -fsSL https://raw.githubusercontent.com/Order52/truesense/main/tools/get.sh | sudo bash -s -- --no-gui   # extra options
```
`get.sh` finds the newest tag, downloads it, and runs `tools/install.sh`. Pin a version with `TRUESENSE_VERSION=0.3.11`, verify it with `TRUESENSE_SHA256=<hash>`. Read the script first if you like: it is ~20 lines.

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

## Arch package (AUR)
`packaging/aur/PKGBUILD.template` builds from the tagged release tarball; the maintainer publishes with `tools/aur-publish.sh X.Y.Z` (needs an AUR account and SSH key). After that: `yay -S truesense`. Official Arch repositories require a Trusted User/package maintainer to adopt the package; it cannot be pushed there directly.

## Status
Tested end to end on CachyOS (Arch family) only. Debian, Fedora and openSUSE paths are **untested on real systems**: only the detection and package naming are unit-tested. Reports and fixes welcome.
