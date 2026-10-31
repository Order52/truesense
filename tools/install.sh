#!/usr/bin/env bash
# rpredatorsense installer.  sudo tools/install.sh            install or upgrade (kernel helper, daemon, user services, udev rule)
#                            sudo tools/install.sh --with-audio  also install noise-suppression-for-voice (RNNoise plugin)
set -eu
[ "$(id -u)" -eq 0 ] || { echo "run with sudo"; exit 1; }
ROOT=$(realpath "$(dirname "$0")/.."); U=${SUDO_USER:-}; [ -n "$U" ] || { echo "run with sudo from your normal user (SUDO_USER is empty)"; exit 1; }
if [ "${1:-}" = "--with-audio" ]; then command -v pacman >/dev/null && pacman -S --needed --noconfirm noise-suppression-for-voice || echo "(--with-audio only automates Arch; install the RNNoise LADSPA plugin, noise-suppression-for-voice, with your package manager)"; fi
"$ROOT/tools/install_kernel_module.sh"
install -d /usr/local/lib/rpredatorsense /etc/rpredatorsense
rm -rf /usr/local/lib/rpredatorsense/rpredatorsense; cp -r "$ROOT/src/rpredatorsense" /usr/local/lib/rpredatorsense/
find /usr/local/lib/rpredatorsense -name __pycache__ -prune -exec rm -rf {} +
install -m755 "$ROOT/packaging/health.sh" /usr/local/lib/rpredatorsense/health.sh
printf '#!/bin/sh\nexec env PYTHONPATH=/usr/local/lib/rpredatorsense python3 -m rpredatorsense "$@"\n' > /usr/local/bin/rpredatorsense; chmod 755 /usr/local/bin/rpredatorsense
[ -e /etc/rpredatorsense/config.toml ] || printf '# overrides for built-in defaults, see src/rpredatorsense/config.py\nuser = "%s"\n' "$U" > /etc/rpredatorsense/config.toml
install -m644 "$ROOT"/packaging/rpredatorsense.service "$ROOT"/packaging/rpredatorsense-health.service "$ROOT"/packaging/rpredatorsense-health.timer /etc/systemd/system/
install -m644 "$ROOT/packaging/70-rpredatorsense.rules" /etc/udev/rules.d/; udevadm control --reload; udevadm trigger --subsystem-match=misc; udevadm trigger --subsystem-match=input --action=change
install -m644 "$ROOT/packaging/rpredatorsense-keys.service" "$ROOT/packaging/rpredatorsense-audio.service" /etc/systemd/user/
systemctl daemon-reload
UID_=$(id -u "$U"); systemctl --global enable rpredatorsense-keys.service rpredatorsense-audio.service
runuser -u "$U" -- env XDG_RUNTIME_DIR=/run/user/$UID_ systemctl --user daemon-reload || true
runuser -u "$U" -- env XDG_RUNTIME_DIR=/run/user/$UID_ systemctl --user restart rpredatorsense-keys.service rpredatorsense-audio.service || echo "(keys service starts at next login)"
# another fan/profile daemon would fight over the fans: tell the user, never disable it silently
for u in predator.service tuned.service; do systemctl is-active --quiet "$u" 2>/dev/null && echo "NOTE: $u is active and may conflict with rpredatorsense (fans/profile); disable it yourself if you want rpredatorsense to control them."; done
systemctl enable rpredatorsense.service rpredatorsense-health.timer
systemctl restart rpredatorsense.service          # `enable --now` does NOT restart a running service, so upgrades would keep running the old code
systemctl start rpredatorsense-health.timer
sleep 3; systemctl --no-pager --lines=5 status rpredatorsense.service | head -12; cat /tmp/predator-state 2>/dev/null
