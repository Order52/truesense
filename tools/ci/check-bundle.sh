#!/usr/bin/env bash
# SPDX-License-Identifier: MIT
# Verify two builds of the release bundle (tools/ci/build-bundle.sh): they must be byte-identical (reproducible), and the bundle must hold what an installer needs
# and a program that runs and says the right version.
#   tools/ci/check-bundle.sh X.Y.Z DIR_A DIR_B
set -Eeuo pipefail
VER=${1:?usage: check-bundle.sh X.Y.Z DIR_A DIR_B}; A=${2:?}; B=${3:?}
fail() { echo "::error::$*" >&2; exit 1; }

for f in "truesense-$VER.tar.gz" truesense; do
  cmp -s "$A/$f" "$B/$f" || fail "$f is not reproducible: two builds of the same commit differ ($(sha256sum "$A/$f" | cut -c1-12) vs $(sha256sum "$B/$f" | cut -c1-12))"
  echo "reproducible: $f $(sha256sum "$A/$f" | cut -d' ' -f1)"
done

tmp=$(mktemp -d); trap 'rm -rf "$tmp"' EXIT
tar -tzf "$A/truesense-$VER.tar.gz" > "$tmp/list"
for f in tools/install.sh tools/get.sh tools/uninstall.sh bin/truesense internal/buildinfo/buildinfo.go cmd/truesense/main.go go.mod go.sum \
         kernel/truesense-wmi/truesense_wmi.c kernel/truesense-wmi/dkms.conf packaging/truesense.service packaging/health.sh LICENSE; do
  grep -qx "truesense-$VER/$f" "$tmp/list" || fail "the bundle lacks $f"
done
tar -tvzf "$A/truesense-$VER.tar.gz" "truesense-$VER/bin/truesense" | grep -q '^-rwxr-xr-x 0/0 ' || fail "bin/truesense must be executable and owned by root:root in the bundle"
tar -xzf "$A/truesense-$VER.tar.gz" -C "$tmp"
got=$("$tmp/truesense-$VER/bin/truesense" --version) || fail "the bundled program does not run"
[ "$got" = "truesense $VER" ] || fail "the bundled program says '$got', expected 'truesense $VER'"
if ldd "$tmp/truesense-$VER/bin/truesense" >/dev/null 2>&1; then fail "the program is dynamically linked: it must be static"; fi
cmp -s "$tmp/truesense-$VER/bin/truesense" "$A/truesense" || fail "bin/truesense in the bundle is not the program that was built"
echo "bundle ok: $(wc -l < "$tmp/list") entries, $got, static"
