#!/usr/bin/env bash
# SPDX-License-Identifier: MIT
# Publish/update the AUR package "truesense" for a released tag. Run by the maintainer (needs an AUR account + SSH key).
#   tools/aur-publish.sh 0.3.16
set -Eeuo pipefail
VER=${1:?usage: aur-publish.sh X.Y.Z}; ROOT=$(realpath "$(dirname "$0")/.."); W=$(mktemp -d)
curl -fsSL -o "$W/src.tgz" "https://github.com/Order52/truesense/archive/refs/tags/v$VER.tar.gz"; SHA=$(sha256sum "$W/src.tgz" | cut -d' ' -f1)
git clone ssh://aur@aur.archlinux.org/truesense.git "$W/aur"
sed -e "s/^pkgver=.*/pkgver=$VER/" -e "s/^pkgrel=.*/pkgrel=1/" -e "s/@SHA@/$SHA/" -e "s/truesense-wmi-[0-9.]*/truesense-wmi-$VER/g" "$ROOT/packaging/aur/PKGBUILD.template" > "$W/aur/PKGBUILD"
cp "$ROOT/packaging/aur/truesense.install" "$W/aur/"
cd "$W/aur" && makepkg --printsrcinfo > .SRCINFO && git add PKGBUILD truesense.install .SRCINFO
git -c user.name="${GIT_AUTHOR_NAME:-$(git config user.name)}" commit -m "truesense $VER" && git push origin HEAD:master
