// TEST ONLY, AND NOT INSTALLED. The end-to-end check presses the window's
// keys and reads its state through this: a second socket, named by
// $GALLEY_TEST_CONTROL, taking one JSON command a connection.
//
//   {"state": true}          -> the window's state (QueueWindow.state)
//   {"key": "Return"}        -> a key pressed and released, by keyval name;
//                               "alt": true holds Alt, "hold": true leaves
//                               it down, so the next press is a repeat
//   {"entry": "text"}        -> the selected entry's text set
//   {"focus": "Refuse"}      -> the selected item's button of that label
//                               focused, as Tab would
//   {"show": true}           -> the window brought forward
//
// A key goes to the window's own handler first, as GTK's capture phase
// sends it. One the window leaves is passed on the way GTK would pass it to
// the focused widget, for the two widgets the window leaves keys to: a
// button, which Enter and Space press, and a text field, which a character
// is typed into. That passing on is this file's stand-in for GTK's own:
// GTK 4 has no way to synthesise a key event, so what GTK does with a key
// the window leaves is assumed here, not tested.
//
// The package leaves this file out (flake.nix), so a window a person uses
// has no way to be answered but by that person.

import GLib from 'gi://GLib';
import Gio from 'gi://Gio';
import Gdk from 'gi://Gdk?version=4.0';
import Gtk from 'gi://Gtk?version=4.0';

Gio._promisify(Gio.DataInputStream.prototype, 'read_line_async', 'read_line_finish_utf8');

export function start(window, path) {
    const service = new Gio.SocketService();
    service.add_address(new Gio.UnixSocketAddress({path}),
        Gio.SocketType.STREAM, Gio.SocketProtocol.DEFAULT, null);
    service.connect('incoming', (_s, connection) => {
        serve(window, connection).catch(e => logError(e, 'galley test control'));
        return true;
    });
    service.start();
    printerr(`galley: TEST CONTROL listening on ${path}`);
}

// What GTK does with a key the window has left: Enter or Space on a
// button presses it, and a character goes into a text field. Returns what
// took it, or "" for nothing.
function passOn(focus, keyval, name) {
    if ((focus instanceof Gtk.Button || focus instanceof Gtk.CheckButton) &&
        ['Return', 'KP_Enter', 'ISO_Enter', 'space', 'KP_Space'].includes(name)) {
        focus.activate();
        return 'button';
    }
    const code = Gdk.keyval_to_unicode(keyval);
    if (focus instanceof Gtk.Text && code >= 0x20) {
        focus.set_text(focus.get_text() + String.fromCodePoint(code));
        return 'entry';
    }
    return '';
}

async function serve(window, connection) {
    const input = new Gio.DataInputStream({base_stream: connection.get_input_stream()});
    const [line] = await input.read_line_async(GLib.PRIORITY_DEFAULT, null);
    let reply = {ok: true};
    try {
        const command = JSON.parse(line);
        if (command.state) {
            reply = window.state();
        } else if (command.key) {
            const keyval = Gdk.keyval_from_name(command.key);
            const state = command.alt ? Gdk.ModifierType.ALT_MASK : 0;
            reply.handled = window.handleKey(keyval, state);
            if (!reply.handled)
                reply.to = passOn(window.window.get_focus(), keyval, command.key);
            if (!command.hold)
                window.release(keyval);
        } else if (typeof command.entry === 'string') {
            const current = window.current();
            if (!current?.entry)
                throw new Error('the selected item is not an entry');
            current.entry.set_text(command.entry);
        } else if (typeof command.focus === 'string') {
            const current = window.current();
            const i = current?.item.buttons.findIndex(b => b.label === command.focus) ?? -1;
            if (i < 0)
                throw new Error(`the selected item has no button ${JSON.stringify(command.focus)}`);
            if (!current.buttons[i].grab_focus())
                throw new Error(`the button ${JSON.stringify(command.focus)} cannot take the focus`);
        } else if (command.show) {
            window.show(null);
        }
    } catch (e) {
        reply = {error: e.message};
    }
    connection.get_output_stream().write_all(new TextEncoder().encode(`${JSON.stringify(reply)}\n`), null);
    connection.close(null);
}
