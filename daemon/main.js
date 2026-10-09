// galley's window: one long-running process that queues every client's
// question in one window. systemd starts it on the first connection to the
// socket (see nix/galley.socket); it can also be started by hand, when it
// binds the socket itself.
//
//   galley-daemon
//
// $GALLEY_SERVICES_SOCKET and $GALLEY_SERVICES let system users' services
// post (server.js, services.js). Neither going wrong stops the window
// serving its user.
//
// $GALLEY_SOCKET overrides where it listens, for tests. It does not make a
// second window: the window is one application on the session bus, and
// there is one of it there (see below).

import GLib from 'gi://GLib';
import Gio from 'gi://Gio';
import Adw from 'gi://Adw?version=1';
import System from 'system';

import {QueueWindow} from './window.js';
import {Server, passed} from './server.js';
import {admitted} from './services.js';
import {Tray} from './tray.js';

const APP_ID = 'io.github.danielbodart.Galley';

// The process's name is gjs's otherwise, and GTK gives the accessibility bus
// that name for the application: a tool finding the window there, as
// WayDriver does, would have to look for "gjs".
GLib.set_prgname('galley');
GLib.set_application_name('galley');

const socketPath = GLib.getenv('GALLEY_SOCKET') ||
    GLib.build_filenamev([GLib.get_user_runtime_dir(), 'galley', 'sock']);
const servicesPath = GLib.getenv('GALLEY_SERVICES_SOCKET');

// HANDLES_COMMAND_LINE, so that starting does not emit activate and show the
// window: a socket-activated window is started by an item arriving, and
// must not take focus for it.
//
// Unique, since the notification's "show" reaches the window by its name on
// the session bus. So a second galley-daemon in the same session would only
// be a remote of the first, never listening; it says so and fails instead,
// rather than exiting 0 with a socket systemd handed it unserved, whose
// clients would wait and then read the hang-up as Cancel.
const app = new Adw.Application({
    application_id: APP_ID,
    flags: Gio.ApplicationFlags.HANDLES_COMMAND_LINE,
});

let window = null;
let server = null;
let status = 0;

app.connect('startup', () => {
    // Nothing is shown until something waits, and the process lives on
    // between questions: held, rather than kept alive by a window.
    app.hold();
    window = new QueueWindow(app);

    // The one exported action: bringing the window forward. Answers have no
    // action, so no action on the session bus can give one; GTK's
    // accessibility bus could, and the units turn it off (nix/home.nix).
    const show = new Gio.SimpleAction({name: 'show'});
    show.connect('activate', () => window.show(null));
    app.add_action(show);

    // The tray icon, whose menu brings the window forward, as the action
    // does.
    const bus = app.get_dbus_connection();
    if (bus)
        window.tray = new Tray(bus, token => window.show(token));

    server = new Server(window);
    try {
        server.listen(socketPath, passed());
    } catch (e) {
        printerr(`galley: cannot listen on ${socketPath}: ${e.message}`);
        status = 1;
        app.release();
        app.quit();
        return;
    }
    listenServices();

    const control = GLib.getenv('GALLEY_TEST_CONTROL');
    if (control) {
        import('./test-control.js')
            .then(m => m.start(window, control))
            .catch(() => printerr('galley: GALLEY_TEST_CONTROL is set, but this build has no test control'));
    }
});

function listenServices() {
    const names = GLib.getenv('GALLEY_SERVICES');
    if (!servicesPath) {
        if (names)
            printerr('galley: GALLEY_SERVICES is set, but GALLEY_SERVICES_SOCKET is not');
        return;
    }
    if (!names)
        printerr('galley: GALLEY_SERVICES_SOCKET is set, but GALLEY_SERVICES is not, so it admits no one');
    let admit = new Map();
    try {
        const passwd = new TextDecoder().decode(GLib.file_get_contents('/etc/passwd')[1]);
        admit = admitted(names, passwd);
    } catch (e) {
        printerr(`galley: admitting no services: ${e.message}`);
    }
    try {
        server.listenServices(servicesPath, admit);
    } catch (e) {
        printerr(`galley: cannot listen on ${servicesPath}: ${e.message}`);
    }
}

app.connect('shutdown', () => server?.close());
app.connect('command-line', () => 0);
app.connect('activate', () => window?.show(null));

try {
    app.register(null);
} catch (e) {
    printerr(`galley: cannot register on the session bus: ${e.message}`);
    System.exit(1);
}
if (app.get_is_remote()) {
    printerr(`galley: another galley window already runs in this session (${APP_ID} on the session bus); this one would not listen on ${socketPath}`);
    System.exit(1);
}

const code = app.run([System.programInvocationName, ...System.programArgs]);
System.exit(status || code);
