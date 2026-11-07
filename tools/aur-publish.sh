#!/usr/bin/env bash
# SPDX-License-Identifier: MIT
# Publish/update the AUR package "truesense" for a released tag (the PKGBUILD builds the Go program from the tag archive). Run by the maintainer or by the release workflow (needs the AUR SSH key).
#   tools/aur-publish.sh 0.4.0
set -Eeuo pipefail
VER=${1:?usage: aur-publish.sh X.Y.Z}; ROOT=$(realpath "$(dirname "$0")/.."); W=$(mktemp -d)
curl -fsSL -o "$W/src.tgz" "https://github.com/Order52/truesense/archive/refs/tags/v$VER.tar.gz"; SHA=$(sha256sum "$W/src.tgz" | cut -d' ' -f1)
# the package builds the program from this archive with `go build`: it must be the source of this version (and have a go.mod), or the AUR would ship another release
tar -xOzf "$W/src.tgz" "truesense-$VER/internal/buildinfo/buildinfo.go" | grep -qx "const Version = \"$VER\"" || { echo "!! the v$VER archive does not declare version $VER in internal/buildinfo/buildinfo.go: refusing" >&2; exit 1; }
tar -tzf "$W/src.tgz" "truesense-$VER/go.mod" >/dev/null 2>&1 || { echo "!! the v$VER archive has no go.mod: the package could not be built: refusing" >&2; exit 1; }
git clone ssh://aur@aur.archlinux.org/truesense.git "$W/aur"
sed -e "s/^pkgver=.*/pkgver=$VER/" -e "s/^pkgrel=.*/pkgrel=1/" -e "s/@SHA@/$SHA/" -e "s/truesense-wmi-[0-9.]*/truesense-wmi-$VER/g" "$ROOT/packaging/aur/PKGBUILD.template" > "$W/aur/PKGBUILD"
cp "$ROOT/packaging/aur/truesense.install" "$W/aur/"
cd "$W/aur" && makepkg --printsrcinfo > .SRCINFO && git add PKGBUILD truesense.install .SRCINFO
git -c user.name="${GIT_AUTHOR_NAME:-$(git config user.name)}" commit -m "truesense $VER" && git push origin HEAD:master
