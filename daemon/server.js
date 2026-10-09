// The socket, and each client's conversation over it.
//
// The socket is $XDG_RUNTIME_DIR/galley/sock, in a directory only this user
// can open. systemd makes it and hands it over when the user units are
// installed (LISTEN_FDS); a window started by hand binds it itself, having
// checked the directory is private and that no other window is listening.
//
// A conversation is the item: the window answers it on the connection that
// asked, and a connection that closes first takes its item away unanswered.
// See wire for the lines in each direction.
//
// $GALLEY_SERVICES_SOCKET, when set, is a second socket, for system users'
// services: bound here, open to every user, and served only to those
// $GALLEY_SERVICES names (services.js).

import GLib from 'gi://GLib';
import Gio from 'gi://Gio';

import {validate, rows, progressUpdate, notify} from './validate.js';
import {fromService} from './services.js';

Gio._promisify(Gio.DataInputStream.prototype, 'read_line_async', 'read_line_finish_utf8');

// The protocol versions this window takes (wire): any from the
// first to its own, so a client older than the window still reaches it.
const MIN_VERSION = 1;
const VERSION = 3;

// The longest line a client may send: a text-info's whole file arrives in
// its first, and the client caps that at 16 MiB of text, which JSON's
// escaping can at most make six times as long.
const MAX_LINE = 100 * 1024 * 1024;

export class Server {
    constructor(window) {
        this.window = window;
        this.uid = new Gio.Credentials().get_unix_user();
        this.service = new Gio.SocketService();
        this.service.connect('incoming', (_service, connection) => {
            this._serve(connection).catch(e => logError(e, 'galley: a client'));
            return true;
        });
        this.bound = null;
        this.services = null;
        this.servicesBound = null;
    }

    // Binds path for the services in admitted, a map of uid to {name,
    // label}, replacing whatever file is there.
    listenServices(path, admitted) {
        try {
            Gio.File.new_for_path(path).delete(null);
        } catch (e) {
            // Nothing there.
        }
        this.services = new Gio.SocketService();
        this.services.connect('incoming', (_service, connection) => {
            this._serve(connection, admitted).catch(e => logError(e, 'galley: a service'));
            return true;
        });
        this.services.add_address(new Gio.UnixSocketAddress({path}),
            Gio.SocketType.STREAM, Gio.SocketProtocol.DEFAULT, null);
        this.servicesBound = path;
        Gio.File.new_for_path(path).set_attribute_uint32('unix::mode', 0o666,
            Gio.FileQueryInfoFlags.NOFOLLOW_SYMLINKS, null);
        this.services.start();
    }

    // Takes systemd's socket when there is one, else binds path. Throws when
    // it can do neither.
    listen(path) {
        const pid = new Gio.Credentials().get_unix_pid();
        if (GLib.getenv('LISTEN_PID') === String(pid) && GLib.getenv('LISTEN_FDS') === '1') {
            for (const name of ['LISTEN_PID', 'LISTEN_FDS', 'LISTEN_FDNAMES'])
                GLib.unsetenv(name);
            this.service.add_socket(Gio.Socket.new_from_fd(3), null);
        } else {
            this._bind(path);
        }
        this.service.start();
    }

    _bind(path) {
        // sun_path holds 107 bytes and a NUL; GLib would cut a longer path
        // short and bind somewhere no client looks.
        if (new TextEncoder().encode(path).length > 107)
            throw new Error('the path is longer than a unix socket address holds');
        const dir = GLib.path_get_dirname(path);
        GLib.mkdir_with_parents(dir, 0o700);
        const info = Gio.File.new_for_path(dir).query_info(
            'standard::type,unix::mode,unix::uid', Gio.FileQueryInfoFlags.NOFOLLOW_SYMLINKS, null);
        if (info.get_file_type() !== Gio.FileType.DIRECTORY ||
            info.get_attribute_uint32('unix::uid') !== this.uid ||
            (info.get_attribute_uint32('unix::mode') & 0o077) !== 0)
            throw new Error(`${dir} is not a directory only this user can open (type ${info.get_file_type()}, uid ${info.get_attribute_uint32("unix::uid")}, mode ${info.get_attribute_uint32("unix::mode").toString(8)})`);

        if (GLib.file_test(path, GLib.FileTest.EXISTS)) {
            let live = false;
            try {
                new Gio.SocketClient().connect(new Gio.UnixSocketAddress({path}), null).close(null);
                live = true;
            } catch (e) {
                // Nothing listening: a window that went without cleaning up.
            }
            if (live)
                throw new Error(`another window is already listening on ${path}`);
            Gio.File.new_for_path(path).delete(null);
        }
        this.service.add_address(new Gio.UnixSocketAddress({path}),
            Gio.SocketType.STREAM, Gio.SocketProtocol.DEFAULT, null);
        Gio.File.new_for_path(path).set_attribute_uint32('unix::mode', 0o600,
            Gio.FileQueryInfoFlags.NOFOLLOW_SYMLINKS, null);
        this.bound = path;
    }

    close() {
        for (const service of [this.service, this.services]) {
            service?.stop();
            service?.close();
        }
        for (const path of [this.bound, this.servicesBound]) {
            if (!path)
                continue;
            try {
                Gio.File.new_for_path(path).delete(null);
            } catch (e) {
                // Already gone.
            }
        }
    }

    // admitted is the services socket's map of uid to service; null on the
    // user's own socket.
    async _serve(connection, admitted = null) {
        // On the user's socket, the directory already keeps everyone else
        // out; this says so again for a socket systemd made, whose directory
        // this code did not check.
        const peer = connection.get_socket().get_credentials();
        const uid = peer.get_unix_user();
        const service = admitted ? admitted.get(uid) : null;
        if (admitted ? !service : uid !== this.uid) {
            connection.close(null);
            return;
        }

        const input = new Gio.DataInputStream({
            base_stream: connection.get_input_stream(),
            newline_type: Gio.DataStreamNewlineType.LF,
        });
        const output = connection.get_output_stream();
        const send = message => {
            try {
                output.write_all(new TextEncoder().encode(`${JSON.stringify(message)}\n`), null);
            } catch (e) {
                // The client has gone; its item goes with it below.
            }
        };
        const read = async () => {
            const [line, length] = await input.read_line_async(GLib.PRIORITY_DEFAULT, null);
            if (length > MAX_LINE)
                throw new Error('line too long');
            return line;
        };
        const hangUp = () => {
            try {
                connection.close(null);
            } catch (e) {
                // Already closed.
            }
        };

        let handle = null;
        try {
            const first = await read();
            if (first === null) {
                hangUp();
                return;
            }
            const hello = JSON.parse(first);
            const version = hello?.galley;
            if (!Number.isInteger(version) || version < MIN_VERSION || version > VERSION)
                throw new Error(`protocol version ${version} is not one from ${MIN_VERSION} to ${VERSION}`);

            if (!service && hello.show === true) {
                this.window.show(typeof hello.token === 'string' ? hello.token : null);
                send({shown: true});
                hangUp();
                return;
            }

            const item = validate(hello.item, version);
            if (service)
                fromService(item, service, uid, peer.get_unix_pid());
            const client = {
                // The last line: the answer, and the conversation is over.
                answer: answer => {
                    send(answer);
                    hangUp();
                },
                // A line before it: a scale's value as it moves.
                tell: send,
            };
            if (item.kind === 'notification' && item.note.listen) {
                handle = this.window.listen(item);
            } else {
                handle = this.window.add(item, client);
                // A notification is the person's from here: its client may
                // go, as zenity's does, and leave it in the queue.
                if (item.kind === 'notification')
                    send({queued: true});
            }

            for (;;) {
                const line = await read();
                if (line === null)
                    break;
                const follow = JSON.parse(line);
                if (typeof follow?.append === 'string')
                    handle.append?.(follow.append);
                if (follow?.timeout === true)
                    handle.timeout?.();
                if (version < 2)
                    continue;
                if (follow?.rows !== undefined && item.kind === 'list')
                    handle.rows(rows(follow.rows, 'rows', item.list.columns.length));
                const update = progressUpdate(follow?.progress);
                if (update && item.kind === 'progress')
                    handle.progress(update);
                const message = notify(follow?.notify);
                if (message && handle.notify)
                    handle.notify(message);
            }
        } catch (e) {
            // A closed connection rejects the pending read; anything else is
            // a client that said something it should not have, and it is
            // told so before it is hung up on.
            if (!handle && !(e instanceof GLib.Error))
                send({error: e.message});
        }
        handle?.withdraw();
        hangUp();
    }
}
