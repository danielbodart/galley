#!/bin/sh
# Installs galley from the release tarball this sits in, for a system
# without Nix: the client, the window, its user units and desktop file.
#
#   ./install.sh                     into ~/.local, for you
#   sudo ./install.sh --prefix=/usr/local    for everyone
#   ./install.sh --uninstall         (with the same --prefix)
#
# The window needs gjs, GTK 4.12 or later and libadwaita 1.4 or later, with
# their GObject introspection data; this checks for them before it copies
# anything. systemd then starts the window on the first question.
set -eu

usage() {
    cat <<'END'
Usage: install.sh [--prefix=DIR] [--no-systemd] [--uninstall]

  --prefix=DIR   where to install (default ~/.local; /usr/local for everyone)
  --no-systemd   copy the units but leave systemctl alone
  --uninstall    remove what install.sh installed under DIR
END
}

prefix=$HOME/.local
systemd=yes
uninstall=no
for arg in "$@"; do
    case $arg in
        --prefix=*) prefix=${arg#--prefix=} ;;
        --no-systemd) systemd=no ;;
        --uninstall) uninstall=yes ;;
        -h | --help) usage; exit 0 ;;
        *) usage >&2; exit 2 ;;
    esac
done
case $prefix in
    /*) ;;
    *) prefix=$(pwd)/$prefix ;;
esac

here=$(CDPATH='' cd "$(dirname "$0")" && pwd)
desktop=io.github.danielbodart.Galley.desktop
units=$prefix/share/systemd/user

# systemctl --user for yourself; for everyone, --global enables the socket
# in every user's session from their next login.
systemctl_() {
    [ "$systemd" = yes ] || return 0
    command -v systemctl >/dev/null || { systemd=no; return 0; }
    if [ "$(id -u)" = 0 ]; then
        case $1 in
            enable | disable) systemctl --global "$1" galley.socket ;;
        esac
    else
        case $1 in
            enable) systemctl --user daemon-reload &&
                    systemctl --user enable --now galley.socket ||
                    systemd=no ;;
            disable) systemctl --user disable --now galley.socket galley.service 2>/dev/null || true
                     systemctl --user daemon-reload ;;
        esac
    fi
}

if [ "$uninstall" = yes ]; then
    systemctl_ disable
    rm -f "$prefix/bin/galley" "$prefix/bin/galley-daemon" \
        "$units/galley.socket" "$units/galley.service" \
        "$prefix/share/applications/$desktop"
    rm -rf "$prefix/share/galley"
    echo "galley removed from $prefix."
    exit 0
fi

# What the window is built of. The client needs nothing.
if ! command -v gjs >/dev/null; then
    echo "install.sh: no gjs. galley's window needs gjs, GTK 4.12 or later and" >&2
    echo "            libadwaita 1.4 or later, with their introspection data." >&2
    exit 1
fi
if ! gjs -c "
    imports.gi.versions.Gtk = '4.0';
    imports.gi.versions.Adw = '1';
    const { Gtk, Adw } = imports.gi;
    if (Gtk.get_minor_version() < 12)
        throw new Error('GTK 4.' + Gtk.get_minor_version() + ' is older than 4.12');
    if (Adw.get_minor_version() < 4)
        throw new Error('libadwaita 1.' + Adw.get_minor_version() + ' is older than 1.4');
" 2>/dev/null; then
    echo "install.sh: gjs cannot load GTK 4.12 or later and libadwaita 1.4 or later." >&2
    echo "            Install them, with their GObject introspection data (typelibs)." >&2
    exit 1
fi

mkdir -p "$prefix/bin" "$prefix/share/galley" "$units" "$prefix/share/applications"
install -m755 "$here/bin/galley" "$here/bin/galley-daemon" "$prefix/bin/"
rm -rf "$prefix/share/galley"
cp -R "$here/share/galley" "$prefix/share/galley"
install -m644 "$here/share/systemd/user/galley.socket" "$units/"
sed "s|@daemon@|$prefix/bin/galley-daemon|" \
    "$here/share/systemd/user/galley.service" > "$units/galley.service"
sed "s|@galley@|$prefix/bin/galley|" \
    "$here/share/applications/$desktop" > "$prefix/share/applications/$desktop"
chmod 644 "$units/galley.service" "$prefix/share/applications/$desktop"

systemctl_ enable

echo "galley $("$prefix/bin/galley" --version) installed in $prefix."
case ":$PATH:" in
    *":$prefix/bin:"*) ;;
    *) echo "$prefix/bin is not on your PATH." ;;
esac
case $prefix in
    "$HOME/.local" | /usr/local | /usr) ;;
    *) echo "systemd looks for user units in ~/.local/share/systemd/user and" \
        "/usr/local/share/systemd/user, not $units: link them from there." ;;
esac
if [ "$systemd" = no ]; then
    echo "Start the window's socket with: systemctl --user enable --now galley.socket"
fi
