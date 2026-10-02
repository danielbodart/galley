// The window follows the desktop's appearance -- dark or light, the accent
// colour, high contrast -- as it is when the window starts and as it changes
// while the window is open. libadwaita does the following; this checks that
// nothing in galley stops it: a forced colour scheme, a stylesheet of its
// own, a plain Gtk.Application.
//
//   gjs -m tests/appearance.js
//
// The real window (daemon/main.js) runs on a private session bus and GTK's
// broadway backend, never on the desktop this runs on:
//
// - with the portal disabled (ADW_DISABLE_PORTAL=1), when libadwaita reads
//   GSettings -- org.gnome.desktop.interface's color-scheme and
//   accent-color, org.gnome.desktop.a11y.interface's high-contrast, which
//   it falls back to for contrast with or without a portal -- kept in a
//   keyfile here, so this process can change them under the window, and
//   once more to see them read at start;
// - with a stand-in for xdg-desktop-portal's Settings interface, which is
//   how GNOME tells an application: org.freedesktop.appearance's
//   color-scheme, accent-color and contrast, read at start and then
//   changed by its SettingChanged signal. The portal is libadwaita's only
//   source of the colour scheme and the accent: GSettings is not consulted
//   for those while the portal is enabled, even when it is missing, so a
//   window that cannot reach the session bus's portal stays light and blue.
//
// It reads what the window made of them through the test control
// (daemon/test-control.js). $GALLEY_APPEARANCE_DAEMON names the command that
// starts the window, gjs on this checkout's daemon/main.js otherwise;
// gtk4-broadwayd and dbus-run-session are found on PATH.
//
// Exits non-zero, having said which, when any check fails.

import GLib from 'gi://GLib';
import Gio from 'gi://Gio';
import System from 'system';

const here = GLib.path_get_dirname(GLib.filename_from_uri(import.meta.url)[0]);
const repo = GLib.path_get_dirname(here);

const sleep = ms => new Promise(r => GLib.timeout_add(GLib.PRIORITY_DEFAULT, ms, () => {
    r();
    return GLib.SOURCE_REMOVE;
}));

function run(main) {
    const loop = new GLib.MainLoop(null, false);
    let code = 1;
    main().then(c => {
        code = c;
    }, e => {
        printerr(`FAIL ${e.message}`);
        if (e.stack)
            printerr(e.stack);
    }).finally(() => loop.quit());
    loop.run();
    return code;
}

Gio._promisify(Gio.Subprocess.prototype, 'wait_check_async', 'wait_check_finish');
Gio._promisify(Gio.Subprocess.prototype, 'wait_async', 'wait_finish');

// ---- outside: a display and a session bus of its own --------------------

async function outside() {
    const need = name => {
        const path = GLib.find_program_in_path(name);
        if (!path)
            throw new Error(`${name} is not on PATH`);
        return path;
    };
    const broadwayd = need('gtk4-broadwayd');
    const dbus = need('dbus-run-session');

    // Short, so the sockets' paths fit in sun_path.
    const dir = GLib.dir_make_tmp('ga-XXXXXX');
    GLib.mkdir_with_parents(`${dir}/config`, 0o700);
    const display = `:${50 + (new Date().getTime() % 40)}`;

    // A bus with no service directories, so nothing on it is started on
    // demand: no xdg-desktop-portal from the desktop's own data directories
    // answering for the stand-in.
    GLib.file_set_contents(`${dir}/bus.conf`, `<!DOCTYPE busconfig PUBLIC
 "-//freedesktop//DTD D-Bus Bus Configuration 1.0//EN"
 "http://www.freedesktop.org/standards/dbus/1.0/busconfig.dtd">
<busconfig>
  <type>session</type>
  <listen>unix:dir=${dir}</listen>
  <auth>EXTERNAL</auth>
  <policy context="default">
    <allow send_destination="*" eavesdrop="true"/>
    <allow eavesdrop="true"/>
    <allow own="*"/>
  </policy>
</busconfig>
`);

    // Nothing of the desktop's: not its display, its bus, its settings, nor
    // a theme or a debugging override of libadwaita's that would decide
    // the appearance before the settings could.
    let env = GLib.get_environ();
    for (const name of ['WAYLAND_DISPLAY', 'DISPLAY', 'DBUS_SESSION_BUS_ADDRESS', 'XDG_RUNTIME_DIR',
        'GALLEY_SOCKET', 'GTK_THEME', 'ADW_DISABLE_PORTAL', 'ADW_DEBUG_COLOR_SCHEME',
        'ADW_DEBUG_ACCENT_COLOR', 'ADW_DEBUG_HIGH_CONTRAST', 'GSETTINGS_BACKEND'])
        env = GLib.environ_unsetenv(env, name);
    const set = {
        XDG_RUNTIME_DIR: dir,
        XDG_CONFIG_HOME: `${dir}/config`,
        GALLEY_SOCKET: `${dir}/sock`,
        GDK_BACKEND: 'broadway',
        BROADWAY_DISPLAY: display,
        GSETTINGS_BACKEND: 'keyfile',
        // As the units start it (nix/home.nix): the appearance does not
        // come over the accessibility bus.
        GTK_A11Y: 'none',
        GALLEY_APPEARANCE_INSIDE: dir,
    };
    for (const [k, v] of Object.entries(set))
        env = GLib.environ_setenv(env, k, v, true);

    const launcher = new Gio.SubprocessLauncher({flags: Gio.SubprocessFlags.NONE});
    launcher.set_environ(env);
    const bw = launcher.spawnv([broadwayd, display]);
    try {
        await sleep(500);
        const inner = launcher.spawnv([dbus, `--config-file=${dir}/bus.conf`, '--',
            'gjs', '-m', `${here}/appearance.js`]);
        await inner.wait_async(null);
        return inner.get_if_exited() ? inner.get_exit_status() : 1;
    } finally {
        bw.force_exit();
        GLib.spawn_command_line_sync(`rm -rf ${GLib.shell_quote(dir)}`);
    }
}

// ---- inside: the window, the settings and the portal --------------------

let failed = 0;
const dir = GLib.getenv('GALLEY_APPEARANCE_INSIDE');

// The window's test control: one command, one JSON reply.
function control(command) {
    const client = new Gio.SocketClient();
    const conn = client.connect(new Gio.UnixSocketAddress({path: `${dir}/ctl`}), null);
    conn.get_output_stream().write_all(new TextEncoder().encode(`${JSON.stringify(command)}\n`), null);
    const input = new Gio.DataInputStream({base_stream: conn.get_input_stream()});
    const [line] = input.read_line_utf8(null);
    conn.close(null);
    return JSON.parse(line);
}

async function start(name, env = {}) {
    const command = GLib.getenv('GALLEY_APPEARANCE_DAEMON') || `gjs -m ${repo}/daemon/main.js`;
    const [, argv] = GLib.shell_parse_argv(command);
    const launcher = new Gio.SubprocessLauncher({flags: Gio.SubprocessFlags.NONE});
    launcher.setenv('GALLEY_TEST_CONTROL', `${dir}/ctl`, true);
    for (const [k, v] of Object.entries(env))
        launcher.setenv(k, v, true);
    GLib.unlink(`${dir}/ctl`);
    const daemon = launcher.spawnv(argv);
    for (let i = 0; !GLib.file_test(`${dir}/ctl`, GLib.FileTest.EXISTS); i++) {
        if (i > 200)
            throw new Error(`${name}: the window did not start`);
        await sleep(50);
    }
    // Open, as it is when the appearance changes under a person.
    control({show: true});
    return daemon;
}

async function stop(daemon) {
    daemon.send_signal(15);
    await daemon.wait_async(null);
}

// Waits for the window's appearance to have `want`'s values, and fails the
// check named if it has not within a few seconds.
async function expect(name, want) {
    let got;
    for (let i = 0; i < 100; i++) {
        got = control({appearance: true});
        if (Object.entries(want).every(([k, v]) => got[k] === v))
            return;
        await sleep(50);
    }
    printerr(`FAIL ${name}: got ${JSON.stringify(got)}, want ${JSON.stringify(want)}`);
    failed++;
}

async function viaGSettings(iface, a11y, accents) {
    const noPortal = {ADW_DISABLE_PORTAL: '1'};
    const daemon = await start('GSettings', noPortal);
    try {
        await expect('GSettings: light by default', {dark: false, highContrast: false});

        iface.set_string('color-scheme', 'prefer-dark');
        await expect('GSettings: prefer-dark, live', {dark: true});
        iface.set_string('color-scheme', 'prefer-light');
        await expect('GSettings: prefer-light, live', {dark: false});

        if (accents) {
            iface.set_string('accent-color', 'purple');
            await expect('GSettings: accent purple, live', {systemAccents: true, accent: 'purple'});
            iface.set_string('accent-color', 'teal');
            await expect('GSettings: accent teal, live', {accent: 'teal'});
        }

        a11y.set_boolean('high-contrast', true);
        await expect('GSettings: high contrast, live', {highContrast: true});
        a11y.set_boolean('high-contrast', false);
        await expect('GSettings: high contrast off, live', {highContrast: false});
    } finally {
        await stop(daemon);
    }

    // As it is when the window starts.
    iface.set_string('color-scheme', 'prefer-dark');
    if (accents)
        iface.set_string('accent-color', 'orange');
    a11y.set_boolean('high-contrast', true);
    Gio.Settings.sync();
    const again = await start('GSettings at start', noPortal);
    try {
        await expect('GSettings: as set at start', {
            dark: true, highContrast: true, ...accents ? {accent: 'orange'} : {},
        });
    } finally {
        await stop(again);
    }
}

// xdg-desktop-portal's Settings interface, as much of it as libadwaita reads.
const PORTAL = `<node>
  <interface name="org.freedesktop.portal.Settings">
    <method name="ReadAll"><arg type="as" direction="in"/><arg type="a{sa{sv}}" direction="out"/></method>
    <method name="Read"><arg type="s" direction="in"/><arg type="s" direction="in"/><arg type="v" direction="out"/></method>
    <method name="ReadOne"><arg type="s" direction="in"/><arg type="s" direction="in"/><arg type="v" direction="out"/></method>
    <signal name="SettingChanged"><arg type="s"/><arg type="s"/><arg type="v"/></signal>
    <property name="version" type="u" access="read"/>
  </interface>
</node>`;

class Portal {
    constructor(values) {
        this.values = values; // 'namespace key' -> GLib.Variant
        this.version = 2;
        this.exported = Gio.DBusExportedObject.wrapJSObject(PORTAL, this);
    }

    ReadAll() {
        const out = {};
        for (const [k, v] of Object.entries(this.values)) {
            const [ns, key] = k.split(' ');
            (out[ns] ??= {})[key] = v;
        }
        return out;
    }

    ReadOneAsync([ns, key], invocation) {
        const v = this.values[`${ns} ${key}`];
        if (!v)
            invocation.return_dbus_error('org.freedesktop.portal.Error.NotFound', `${ns} ${key}`);
        else
            invocation.return_value(new GLib.Variant('(v)', [v]));
    }

    // The deprecated Read, which libadwaita calls, wraps the value once more.
    ReadAsync([ns, key], invocation) {
        const v = this.values[`${ns} ${key}`];
        if (!v)
            invocation.return_dbus_error('org.freedesktop.portal.Error.NotFound', `${ns} ${key}`);
        else
            invocation.return_value(new GLib.Variant('(v)', [new GLib.Variant('v', v)]));
    }

    change(ns, key, value) {
        this.values[`${ns} ${key}`] = value;
        this.exported.emit_signal('SettingChanged', new GLib.Variant('(ssv)', [ns, key, value]));
    }
}

async function viaPortal(iface, a11y, accents) {
    const fdo = 'org.freedesktop.appearance';
    const portal = new Portal({
        // Light, though GSettings still says dark from before: the portal
        // is what is followed when there is one, as under GNOME.
        [`${fdo} color-scheme`]: new GLib.Variant('u', 2),
        [`${fdo} contrast`]: new GLib.Variant('u', 0),
        [`${fdo} accent-color`]: new GLib.Variant('(ddd)', [0.2, 0.82, 0.48]), // green
    });
    const bus = Gio.DBus.session;
    portal.exported.export(bus, '/org/freedesktop/portal/desktop');
    const owned = await new Promise((resolve, reject) => {
        const id = Gio.bus_own_name_on_connection(bus, 'org.freedesktop.portal.Desktop',
            Gio.BusNameOwnerFlags.NONE, () => resolve(id), () => reject(new Error('cannot own the portal\'s name')));
    });

    const daemon = await start('portal');
    try {
        await expect('portal: as read at start', {
            dark: false, highContrast: false, ...accents ? {systemAccents: true, accent: 'green'} : {},
        });

        portal.change(fdo, 'color-scheme', new GLib.Variant('u', 1));
        await expect('portal: prefer-dark, live', {dark: true});
        portal.change(fdo, 'color-scheme', new GLib.Variant('u', 0));
        await expect('portal: no preference, live', {dark: false});

        if (accents) {
            portal.change(fdo, 'accent-color', new GLib.Variant('(ddd)', [0.88, 0.11, 0.14])); // red
            await expect('portal: accent red, live', {accent: 'red'});
        }

        portal.change(fdo, 'contrast', new GLib.Variant('u', 1));
        await expect('portal: high contrast, live', {highContrast: true});
        portal.change(fdo, 'contrast', new GLib.Variant('u', 0));
        await expect('portal: high contrast off, live', {highContrast: false});
    } finally {
        await stop(daemon);
        Gio.bus_unown_name(owned);
        portal.exported.unexport();
    }
}

async function inside() {
    // Never the desktop's own bus or display.
    const address = GLib.getenv('DBUS_SESSION_BUS_ADDRESS') ?? '';
    if (!address.includes(dir) || GLib.getenv('WAYLAND_DISPLAY') || GLib.getenv('DISPLAY') ||
        GLib.getenv('GDK_BACKEND') !== 'broadway' || GLib.getenv('GSETTINGS_BACKEND') !== 'keyfile')
        throw new Error('not on a private bus and display; run it as gjs -m tests/appearance.js');

    const source = Gio.SettingsSchemaSource.get_default();
    if (!source?.lookup('org.gnome.desktop.interface', true)?.has_key('color-scheme'))
        throw new Error('no org.gnome.desktop.interface schema with color-scheme (gsettings-desktop-schemas)');
    const accents = source.lookup('org.gnome.desktop.interface', true).has_key('accent-color');
    if (!accents)
        print('SKIP accent colours: this org.gnome.desktop.interface has no accent-color');
    const iface = new Gio.Settings({schema_id: 'org.gnome.desktop.interface'});
    const a11y = new Gio.Settings({schema_id: 'org.gnome.desktop.a11y.interface'});

    await viaGSettings(iface, a11y, accents);
    await viaPortal(iface, a11y, accents);

    if (failed)
        printerr(`${failed} failed`);
    else
        print('ok: the window follows the dark style, the accent colour and high contrast, at start and live, by the portal and by GSettings');
    return failed ? 1 : 0;
}

System.exit(run(dir ? inside : outside));
