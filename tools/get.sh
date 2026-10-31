#!/usr/bin/env bash
# One-line installer:  curl -fsSL https://raw.githubusercontent.com/Order52/truesense/main/tools/get.sh | sudo bash
# Downloads a tagged release, optionally checks a pinned SHA-256, and runs tools/install.sh.
#   TRUESENSE_VERSION=0.2.0   which release (default: the one this script was published with)
#   TRUESENSE_SHA256=...      refuse to install unless the tarball matches
# Prefer to read first: git clone https://github.com/Order52/truesense && less truesense/tools/install.sh
set -euo pipefail
VERSION="${TRUESENSE_VERSION:-0.2.0}"
[ "$(id -u)" -eq 0 ] || { echo "run with sudo: curl -fsSL <url> | sudo bash" >&2; exit 1; }
for c in curl tar sha256sum; do command -v "$c" >/dev/null || { echo "missing: $c" >&2; exit 1; }; done
tmp="$(mktemp -d)"; trap 'rm -rf "$tmp"' EXIT
url="https://github.com/Order52/truesense/archive/refs/tags/v${VERSION}.tar.gz"
echo "downloading $url"
curl -fsSL --retry 3 -o "$tmp/ts.tar.gz" "$url"
sum="$(sha256sum "$tmp/ts.tar.gz" | cut -d' ' -f1)"; echo "sha256: $sum"
if [ -n "${TRUESENSE_SHA256:-}" ] && [ "$sum" != "$TRUESENSE_SHA256" ]; then echo "checksum mismatch, refusing to install" >&2; exit 1; fi
mkdir "$tmp/src"; tar -xzf "$tmp/ts.tar.gz" -C "$tmp/src" --strip-components=1
[ -x "$tmp/src/tools/install.sh" ] || { echo "unexpected archive layout" >&2; exit 1; }
"$tmp/src/tools/install.sh" "$@"
