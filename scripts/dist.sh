#!/usr/bin/env bash
# The release tarball for one architecture, for a system without Nix:
# dist.sh VERSION x86_64|aarch64 [OUTDIR]
#
# galley-linux-ARCH.tar.gz holds galley-VERSION/, laid out as it installs --
# bin/, share/ -- with install.sh beside it. The name carries no version so
# that releases/latest/download/ always finds it. Only the client differs
# between architectures: the window is JavaScript, run by the system's gjs.
set -euo pipefail

version=$1
arch=$2
out=$(mkdir -p "${3:-dist}" && CDPATH='' cd "${3:-dist}" && pwd)

case $arch in
    x86_64) goarch=amd64 ;;
    aarch64) goarch=arm64 ;;
    *) echo "dist: no architecture '$arch'; x86_64 or aarch64" >&2; exit 2 ;;
esac

CDPATH='' cd "$(dirname "$0")/.."
stage=$(mktemp -d)
trap 'rm -rf "$stage"' EXIT
root=$stage/galley-$version

# Static, as the flake builds it.
CGO_ENABLED=0 GOOS=linux GOARCH=$goarch \
    go build -trimpath -ldflags "-s -w -X main.version=$version" \
    -o "$root/bin/galley" ./cmd/galley
install -m755 packaging/galley-daemon "$root/bin/galley-daemon"

# The window, without the test control (see flake.nix).
mkdir -p "$root/share/galley"
for f in daemon/*.js; do
    [ "$(basename "$f")" = test-control.js ] && continue
    install -m644 "$f" "$root/share/galley/"
done

# The units and the desktop file with their paths still to fill in:
# install.sh knows where they go.
install -Dm644 nix/galley.socket "$root/share/systemd/user/galley.socket"
install -Dm644 nix/galley.service "$root/share/systemd/user/galley.service"
install -Dm644 nix/io.github.danielbodart.Galley.desktop \
    "$root/share/applications/io.github.danielbodart.Galley.desktop"

install -m755 packaging/install.sh "$root/install.sh"
install -m644 LICENSE README.md "$root/"

tar -C "$stage" --sort=name --owner=0 --group=0 --numeric-owner \
    -czf "$out/galley-linux-$arch.tar.gz" "galley-$version"
echo "$out/galley-linux-$arch.tar.gz"
