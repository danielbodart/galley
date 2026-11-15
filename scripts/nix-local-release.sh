#!/usr/bin/env bash
# Makes this checkout's flake a release of tarballs built here, without
# publishing anything: writes nix/release.json for them, stages it (a flake
# in a git checkout sees only what git tracks), and adds each tarball to the
# Nix store under the very path fetchurl would fetch it to, so the package
# is built from these files and nothing is downloaded.
#
# CI's nix job does this with the tarballs its own dist job made, so the
# package's checks (end-to-end, the home-manager module) run the client this
# run will release, through the same release.json, fetchurl and install as
# a pinned tag. The weekly update does it with the tarballs it built, and
# mise run nix-check and nix-build with this machine's. The path is sound
# because it is content-addressed: fetchurl's output is fixed by its name
# (the tarball's) and its hash (release.json's), and nix-store --add-fixed
# computes the hash from the file, so only the file whose hash release.json
# names can stand in for the download.
#
# usage: scripts/nix-local-release.sh VERSION DIR
#
# DIR is what `mise run dist` wrote: galley-linux-<arch>.tar.gz and their
# SHA256SUMS, built as VERSION. The release.json it leaves staged must never
# be committed to trunk (ci.yml's release job says why).
set -euo pipefail
unset CDPATH

if [ $# -ne 2 ]; then
    echo "usage: scripts/nix-local-release.sh VERSION DIR" >&2
    exit 2
fi
version=$1
dir=$(cd "$2" && pwd)
scripts=$(cd "$(dirname "$0")" && pwd)

cd "$(git rev-parse --show-toplevel)"
mkdir -p nix
"$scripts/release-json.sh" "$version" "$dir/SHA256SUMS" >nix/release.json
git add nix/release.json
for f in "$dir"/galley-linux-*.tar.gz; do
    nix-store --add-fixed sha256 "$f" >/dev/null
done
cat nix/release.json
