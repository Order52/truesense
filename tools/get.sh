#!/usr/bin/env bash
# SPDX-License-Identifier: MIT
# One-line installer. Downloads the newest TrueSense release from GitHub and runs tools/install.sh.
#   curl -fsSL https://raw.githubusercontent.com/Order52/truesense/main/tools/get.sh | sudo bash
#   ... | sudo bash -s -- --no-gui          (extra options go to install.sh)
# Environment: TRUESENSE_VERSION=0.3.14 (pin a version), TRUESENSE_SHA256=<hash> (verify the download)
set -Eeuo pipefail
REPO=Order52/truesense
[ "$(id -u)" -eq 0 ] || { echo "run as root:  curl -fsSL https://raw.githubusercontent.com/$REPO/main/tools/get.sh | sudo bash" >&2; exit 1; }
for c in curl tar; do command -v "$c" >/dev/null || { echo "$c is required (install it with your package manager)" >&2; exit 1; }; done
VERSION="${TRUESENSE_VERSION:-}"
if [ -z "$VERSION" ]; then                                   # newest tag by version order (no jq needed)
  VERSION=$(curl -fsSL "https://api.github.com/repos/$REPO/tags?per_page=100" | sed -n 's/.*"name": *"v\([0-9][0-9.]*\)".*/\1/p' | sort -V | tail -n1)
  [ -n "$VERSION" ] || { echo "could not find the latest version (GitHub unreachable or rate-limited): set TRUESENSE_VERSION=x.y.z" >&2; exit 1; }
fi
TMP=$(mktemp -d); trap 'rm -rf "$TMP"' EXIT
echo "TrueSense $VERSION: downloading"
curl -fsSL -o "$TMP/ts.tar.gz" "https://github.com/$REPO/archive/refs/tags/v$VERSION.tar.gz"
[ -z "${TRUESENSE_SHA256:-}" ] || echo "$TRUESENSE_SHA256  $TMP/ts.tar.gz" | sha256sum -c -
tar -xzf "$TMP/ts.tar.gz" -C "$TMP"
cd "$TMP"/truesense-*/ && exec bash tools/install.sh "$@"
