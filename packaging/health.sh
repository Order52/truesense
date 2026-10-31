#!/bin/bash
# Restart the daemon if it died or its state file is stale; rebuild the DKMS modules once per 10 min if a kernel update left them missing.
STAMP=/run/rpredatorsense-dkms-tried
if [ ! -e /dev/acer-rear ]; then
  modprobe acer_rear_wmi 2>/dev/null
  if [ ! -e /dev/acer-rear ] && { [ ! -e "$STAMP" ] || [ $(( $(date +%s) - $(stat -c %Y "$STAMP") )) -gt 600 ]; }; then
    touch "$STAMP"; logger -t rpredatorsense-health "acer_rear_wmi missing for $(uname -r): dkms autoinstall"
    dkms autoinstall -k "$(uname -r)" >/dev/null 2>&1; modprobe acer_rear_wmi 2>/dev/null; modprobe linuwu_sense 2>/dev/null
    [ -e /dev/acer-rear ] && systemctl restart rpredatorsense.service
  fi
fi
systemctl is-active --quiet rpredatorsense.service || { systemctl restart rpredatorsense.service; exit 0; }
age=$(( $(date +%s) - $(stat -c %Y /tmp/predator-state 2>/dev/null || echo 0) ))
[ "$age" -gt 20 ] && { logger -t rpredatorsense-health "state file stale (${age}s), restarting"; systemctl restart rpredatorsense.service; }
exit 0
