// galley's window: one long-running process that queues every client's
// question in one window. systemd starts it on the first connection to the
// socket (see nix/galley.socket); it can also be started by hand, when it
// binds the socket itself.
//
//   galley-daemon
//
// $GALLEY_SOCKET overrides where it listens, for tests and for a second
// window beside the first.

import GLib from 'gi://GLib';
import Gio from 'gi://Gio';
import Adw from 'gi://Adw?version=1';
import System from 'system';

import {QueueWindow} from './window.js';
import {Server} from './server.js';

const APP_ID = 'io.github.danielbodart.Galley';

const socketPath = GLib.getenv('GALLEY_SOCKET') ||
    GLib.build_filenamev([GLib.get_user_runtime_dir(), 'galley', 'sock']);

// HANDLES_COMMAND_LINE, so that starting does not emit activate and show the
// window: a socket-activated window is started by an item arriving, and
// must not take focus for it. A second galley-daemon hands its command line
// to the first and exits, leaving the socket with the first.
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
    // action, so nothing on the session bus can give one.
    const show = new Gio.SimpleAction({name: 'show'});
    show.connect('activate', () => window.show(null));
    app.add_action(show);

    server = new Server(window);
    try {
        server.listen(socketPath);
    } catch (e) {
        printerr(`galley: cannot listen on ${socketPath}: ${e.message}`);
        status = 1;
        app.release();
        app.quit();
        return;
    }

    const control = GLib.getenv('GALLEY_TEST_CONTROL');
    if (control) {
        import('./test-control.js')
            .then(m => m.start(window, control))
            .catch(() => printerr('galley: GALLEY_TEST_CONTROL is set, but this build has no test control'));
    }
});

app.connect('shutdown', () => server?.close());
app.connect('command-line', () => 0);
app.connect('activate', () => window?.show(null));

const code = app.run([System.programInvocationName, ...System.programArgs]);
System.exit(status || code);
