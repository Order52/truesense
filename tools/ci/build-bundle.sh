#!/usr/bin/env bash
# SPDX-License-Identifier: MIT
# Build the release bundle deterministically: truesense-X.Y.Z.tar.gz = `git archive` of the checked-out commit plus the prebuilt static program bin/truesense.
# The same commit always gives the same bytes (sorted names, fixed mtime, root owner, gzip -n), so anyone can rebuild the bundle and compare checksums.
#   tools/ci/build-bundle.sh X.Y.Z OUTDIR      writes OUTDIR/truesense-X.Y.Z.tar.gz and OUTDIR/truesense (the program on its own, for the SBOM and the smoke test)
# Environment: SOURCE_DATE_EPOCH (default: the commit time), GOOS/GOARCH (default linux/amd64: the only platform the bundle supports).
set -Eeuo pipefail
VER=${1:?usage: build-bundle.sh X.Y.Z OUTDIR}; OUT=${2:?usage: build-bundle.sh X.Y.Z OUTDIR}
[[ "$VER" =~ ^[0-9]+\.[0-9]+\.[0-9]+$ ]] || { echo "!! not a version: $VER" >&2; exit 1; }
ROOT=$(realpath "$(dirname "$0")/../..")
cd "$ROOT"
export SOURCE_DATE_EPOCH=${SOURCE_DATE_EPOCH:-$(git log -1 --format=%ct HEAD)}
export TZ=UTC GOOS=${GOOS:-linux} GOARCH=${GOARCH:-amd64} CGO_ENABLED=0 GOFLAGS=-mod=readonly
mkdir -p "$OUT"; OUT=$(realpath "$OUT")

# 1. the program: the Makefile's recipe (static, -trimpath, commit and date from git), from a clean dist/
rm -f dist/truesense dist/truesense-fast
make --no-print-directory build
cp dist/truesense "$OUT/truesense"

# 2. the bundle: the tree as git archives it, with bin/truesense added, repacked in a fixed order
stage=$(mktemp -d); trap 'rm -rf "$stage"' EXIT
git archive --format=tar --prefix="truesense-$VER/" HEAD | tar -x -C "$stage"
install -D -m755 "$OUT/truesense" "$stage/truesense-$VER/bin/truesense"
( cd "$stage" && umask 022 && tar --sort=name --mtime="@$SOURCE_DATE_EPOCH" --owner=0 --group=0 --numeric-owner --format=gnu -cf - "truesense-$VER" ) | gzip -n -9 > "$OUT/truesense-$VER.tar.gz"
sha256sum "$OUT/truesense" "$OUT/truesense-$VER.tar.gz"
