#!/usr/bin/env bash
# TrueSense installer. One command:   sudo tools/install.sh          (safe to re-run: it upgrades in place)
#   --module-only   only migrate + install the kernel module (used while testing)
#   --with-audio    also install the RNNoise plugin for Purified Voice (Arch: noise-suppression-for-voice)
#   --no-services   install files but do not enable/start services
#   --force         install on a model other than the Acer Predator PH16-71 (unsupported, can misbehave)
#   --prefix=DIR    install prefix (default /usr/local)
set -Eeuo pipefail
PREFIX=/usr/local; MODULE_ONLY=0; AUDIO=0; SERVICES=1; FORCE=0
for a in "$@"; do case "$a" in
  --module-only) MODULE_ONLY=1;; --with-audio) AUDIO=1;; --no-services) SERVICES=0;; --force) FORCE=1;; --prefix=*) PREFIX=${a#*=};;
  -h|--help) sed -n '2,8p' "$0"; exit 0;; *) echo "unknown option: $a" >&2; exit 2;; esac; done
[ "$(id -u)" -eq 0 ] || { echo "run with sudo:  sudo $0 $*"; exit 1; }
ROOT=$(realpath "$(dirname "$0")/.."); U=${SUDO_USER:-}; [ -n "$U" ] && [ "$U" != root ] || { echo "run with sudo from your normal user (SUDO_USER is empty)"; exit 1; }
UID_=$(id -u "$U"); HOME_U=$(getent passwd "$U" | cut -d: -f6)
mkdir -p /var/log; exec > >(tee -a /var/log/truesense-install.log) 2>&1
trap 'echo "!! install failed at line $LINENO (log: /var/log/truesense-install.log). Nothing is half-enabled: fix the problem and re-run." >&2' ERR
say() { printf '  %s\n' "$*"; }; step() { printf '\n== %s\n' "$*"; }
ur() { runuser -u "$U" -- env XDG_RUNTIME_DIR=/run/user/$UID_ DBUS_SESSION_BUS_ADDRESS=unix:path=/run/user/$UID_/bus "$@"; }
echo "TrueSense installer $(date '+%F %T')  prefix=$PREFIX  user=$U"

step "pre-flight checks"
model=$(cat /sys/class/dmi/id/product_name 2>/dev/null || echo unknown)
if [ "$model" != "Predator PH16-71" ]; then
  [ "$FORCE" = 1 ] || { echo "  this machine is '$model', not an Acer Predator PH16-71. TrueSense talks to firmware and is only tested on the PH16-71. Re-run with --force to override."; exit 1; }
  say "WARNING: '$model' is unsupported; continuing because of --force"; printf 'options truesense_wmi force=1\n' > /etc/modprobe.d/truesense-force.conf
else say "model: $model, BIOS $(cat /sys/class/dmi/id/bios_version 2>/dev/null)"; rm -f /etc/modprobe.d/truesense-force.conf; fi
command -v dkms >/dev/null || { echo "  dkms is missing (Arch: sudo pacman -S dkms linux-headers)"; exit 1; }
[ -e "/lib/modules/$(uname -r)/build" ] || { echo "  kernel headers for the running kernel ($(uname -r)) are missing (Arch: install your kernel's -headers package)"; exit 1; }
python3 -c 'import sys; sys.exit(0 if sys.version_info >= (3,11) else 1)' || { echo "  Python 3.11+ is required"; exit 1; }
command -v systemctl >/dev/null || { echo "  systemd is required"; exit 1; }
say "dkms, kernel headers, python $(python3 -c 'import sys; print("%d.%d"%sys.version_info[:2])'), systemd: ok"
if command -v mokutil >/dev/null && mokutil --sb-state 2>/dev/null | grep -qi "enabled"; then
  if [ -e /var/lib/dkms/mok.pub ] && mokutil --test-key /var/lib/dkms/mok.pub 2>&1 | grep -qi "already enrolled"; then say "Secure Boot: on, DKMS signing key enrolled: ok"
  else say "WARNING: Secure Boot is on and the DKMS signing key is not enrolled: enrol it (sudo mokutil --import /var/lib/dkms/mok.pub), reboot, then re-run"; fi
else say "Secure Boot: off (or unknown): no key enrolment needed"; fi

step "removing legacy helpers (backed up first)"
"$ROOT/tools/migrate-legacy.sh"

step "kernel module"
install -d /etc/modprobe.d /etc/modules-load.d /etc/udev/rules.d
install -m644 "$ROOT/packaging/truesense.modprobe.conf" /etc/modprobe.d/truesense.conf
install -m644 "$ROOT/packaging/truesense.modules-load.conf" /etc/modules-load.d/truesense.conf
install -m644 "$ROOT/packaging/70-truesense.rules" /etc/udev/rules.d/70-truesense.rules
udevadm control --reload
"$ROOT/tools/install_kernel_module.sh"
udevadm trigger --subsystem-match=misc --subsystem-match=hidraw 2>/dev/null || true; sleep 1
[ "$MODULE_ONLY" = 1 ] && { echo; echo "module-only install done."; exit 0; }

step "TrueSense files"
if [ "$AUDIO" = 1 ]; then command -v pacman >/dev/null && pacman -S --needed --noconfirm noise-suppression-for-voice || say "(--with-audio only automates Arch; install the RNNoise LADSPA plugin with your package manager)"; fi
install -d "$PREFIX/lib/truesense" "$PREFIX/bin" "$PREFIX/share/truesense/profiles" /etc/truesense
rm -rf "$PREFIX/lib/truesense/truesense"; cp -r "$ROOT/src/truesense" "$PREFIX/lib/truesense/"; find "$PREFIX/lib/truesense" -name __pycache__ -prune -exec rm -rf {} +
install -m755 "$ROOT/packaging/health.sh" "$PREFIX/lib/truesense/health.sh"
printf '#!/bin/sh\nexec env PYTHONPATH=%s/lib/truesense python3 -m truesense "$@"\n' "$PREFIX" > "$PREFIX/bin/truesense"; chmod 755 "$PREFIX/bin/truesense"
[ -d "$ROOT/packaging/profiles" ] && install -m644 "$ROOT"/packaging/profiles/*.toml "$PREFIX/share/truesense/profiles/"
if [ ! -e /etc/truesense/config.toml ]; then
  printf '# TrueSense system configuration (see docs/USAGE.md). Built-in defaults apply to everything not set here.\nuser = "%s"\n\n# First install: the PREDATOR key behaves like PredatorSense on Windows (it opens the app). Change it in the app: PREDATOR key.\n[keys]\nprofile = "native"\ntap = "exec:truesense gui"\ndouble = "none"\nlong = "none"\n' "$U" > /etc/truesense/config.toml
fi
for sh in bash zsh fish; do
  case $sh in bash) d=/usr/share/bash-completion/completions/truesense;; zsh) d=/usr/share/zsh/site-functions/_truesense;; fish) d=/usr/share/fish/vendor_completions.d/truesense.fish;; esac
  install -d "$(dirname "$d")"; "$PREFIX/bin/truesense" completion $sh > "$d" 2>/dev/null || rm -f "$d"
done
say "installed $PREFIX/bin/truesense and $PREFIX/lib/truesense"

step "services"
for u in truesense.service truesense-health.service truesense-health.timer; do sed -e "s|@PREFIX@|$PREFIX|g" -e "s|@MODPROBE@|$(command -v modprobe)|g" "$ROOT/packaging/$u" > /etc/systemd/system/$u; done
install -m755 "$ROOT/tools/truesense-fast" "$PREFIX/bin/truesense-fast"
install -d /usr/share/applications; sed "s|@PREFIX@|$PREFIX|g" "$ROOT/packaging/truesense.desktop" > /usr/share/applications/truesense.desktop
for u in truesense-keys.service truesense-audio.service truesense-startup.service; do sed -e "s|@PREFIX@|$PREFIX|g" "$ROOT/packaging/$u" > /etc/systemd/user/$u; done
systemctl daemon-reload; ur systemctl --user daemon-reload || true
if [ "$SERVICES" = 1 ]; then
  systemctl enable truesense.service truesense-health.timer >/dev/null 2>&1
  systemctl restart truesense.service            # `enable --now` would NOT restart a running service, so upgrades would keep the old code
  systemctl start truesense-health.timer
  systemctl --global enable truesense-keys.service truesense-audio.service truesense-startup.service >/dev/null 2>&1
  ur systemctl --user restart truesense-keys.service truesense-audio.service truesense-startup.service || say "(user services start at your next login)"
  sleep 2
  if [ -r /var/lib/truesense/migrated_voice ]; then      # the user already had Purified Voice filters: recreate them under the new names, same modes
    hs=$(sed -n 's/^headset=//p' /var/lib/truesense/migrated_voice); ar=$(sed -n 's/^array=//p' /var/lib/truesense/migrated_voice)
    ur "$PREFIX/bin/truesense" audio voice set headset "${hs:-off}" >/dev/null 2>&1 || true
    ur "$PREFIX/bin/truesense" audio voice set array "${ar:-off}" >/dev/null 2>&1 || true
    rm -f /var/lib/truesense/migrated_voice; say "Purified Voice filters recreated (headset: ${hs:-off}, array: ${ar:-off})"
  fi
else say "services installed but not enabled (--no-services)"; fi

step "health check"
"$PREFIX/bin/truesense" doctor || say "the checker found problems: see above (re-run it any time: truesense doctor)"
echo; echo "TrueSense is installed. Try:  truesense status   |   truesense --help"
