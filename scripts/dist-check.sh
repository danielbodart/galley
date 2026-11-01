#!/usr/bin/env bash
# The release tarball for this machine's architecture, as `mise run dist`
# made it, installed as a system without Nix installs it: into a prefix,
# where the client says the version it was built as, the units and the
# desktop file name where the programs went, systemd-analyze verifies the
# units against the installed programs, and the test control stays behind;
# then taken out again, leaving nothing. install.sh first checks the system
# has gjs, GTK 4.12 and libadwaita 1.4, so this needs them too.
set -euo pipefail

CDPATH='' cd "$(dirname "$0")/.."
tarball=dist/galley-linux-$(uname -m).tar.gz
work=$(mktemp -d)
trap 'rm -rf "$work"' EXIT

tar -C "$work" -xzf "$tarball"
src=$(echo "$work"/galley-*)
version=${src##*/galley-}
p=$work/prefix
units=$p/share/systemd/user

sh "$src/install.sh" --prefix="$p" --no-systemd
[ "$("$p/bin/galley" --version)" = "$version" ]
grep -qx "ExecStart=$p/bin/galley-daemon" "$units/galley.service"
grep -qx "Exec=$p/bin/galley --show" "$p/share/applications/io.github.danielbodart.Galley.desktop"
[ -f "$p/share/galley/main.js" ] && [ ! -e "$p/share/galley/test-control.js" ]
# --user wants a runtime directory, which a CI runner, with no session of
# its own, does not have.
XDG_RUNTIME_DIR=${XDG_RUNTIME_DIR:-$work} \
    systemd-analyze --user verify "$units/galley.socket" "$units/galley.service"

sh "$src/install.sh" --prefix="$p" --no-systemd --uninstall
[ -z "$(find "$p" -type f)" ]
echo "$tarball installs, verifies and uninstalls."
