#!/usr/bin/env bash
# Install/upgrade the rpredatorsense WMI helper (acer_rear_wmi v1.1, DKMS) and reload it.  sudo tools/install_kernel_module.sh
set -eu
[ "$(id -u)" -eq 0 ] || { echo "run with sudo"; exit 1; }
SRC=$(realpath "$(dirname "$0")/../kernel/acer-rear-wmi"); V=1.2; N=acer-rear-wmi
fans_auto() { [ -w /dev/acer-rear ] && echo set_fan_auto > /dev/acer-rear 2>/dev/null || true; }
fans_auto
for v in $(dkms status "$N" 2>/dev/null | sed -E 's|^[^/]*/([^,: ]+).*|\1|' | sort -u); do dkms remove "$N/$v" --all >/dev/null 2>&1 || true; done
dkms remove "$N/$V" --all >/dev/null 2>&1 || true; dkms remove "$N/$V" >/dev/null 2>&1 || true
rm -rf "/usr/src/$N-$V"; mkdir -p "/usr/src/$N-$V"; cp "$SRC"/{acer_rear_wmi.c,Makefile,dkms.conf} "/usr/src/$N-$V/"
dkms add "$N/$V"; dkms build "$N/$V"; dkms install "$N/$V" --force
rmmod acer_rear_wmi 2>/dev/null || true; modprobe acer_rear_wmi
sleep 1; ls -l /dev/acer-rear; chgrp plugdev /dev/acer-rear 2>/dev/null || true
echo version-check: ; echo get_gpu > /dev/acer-rear && cat /dev/acer-rear
