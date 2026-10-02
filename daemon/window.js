// The one window: the waiting items on the left, the selected one's detail
// and buttons on the right.
//
// Three rules shape it.
//
// IT NEVER TAKES FOCUS BY ITSELF. An item arriving adds a row, and if the
// window is not the one in front, a notification says something is waiting;
// the window is never presented for it. A question that pops up under the
// cursor gets answered by whatever key was already on its way -- the Enter
// that ends a command in a terminal -- and that is exactly the press these
// questions exist to make deliberate. `galley --show`, the notification, or
// the app's "show" action bring it forward, and only a person does those.
//
// KEYS ANSWER AT ONCE. There is no delay before a key counts and no second
// confirmation: what is selected is what the keys act on, so only a person
// moves the selection. Nothing arriving moves it, and nothing going does
// either: a selected question withdrawn by its asker -- killed, timed out,
// tired of waiting -- leaves nothing selected until the person picks again,
// rather than handing the keys already on their way, an Enter or the rest of
// a password, to whichever question stood next to it. A key held down
// answers once, not once per repeat. A focused button is the exception to
// Enter's default: Enter presses that button, as it would in any dialog.
//
// ANSWERS ARE THIS WINDOW'S ALONE. Buttons answer through their own signal
// handlers, never through a GAction: an application's actions are exported
// on the session bus, so an "answer" action would let any process there
// press Allow. The only action is "show". GTK's accessibility bus is the
// other way in -- it can click any button -- and the units start the window
// with it off (GTK_A11Y=none; see nix/home.nix).

import GLib from 'gi://GLib';
import Gio from 'gi://Gio';
import Gdk from 'gi://Gdk?version=4.0';
import Gtk from 'gi://Gtk?version=4.0';
import Adw from 'gi://Adw?version=1';

import {Queue, waited} from './queue.js';
import {render, lines, buttonMarkup} from './render.js';

const editsText = widget =>
    widget instanceof Gtk.Text || (widget instanceof Gtk.TextView && widget.editable);

export class QueueWindow {
    constructor(app) {
        this.app = app;
        this.queue = new Queue();
        this._next = 1;
        this._down = new Set();
        // Where the selected item was when its asker withdrew it, while
        // nothing has been picked since; -1 otherwise.
        this._parkedAt = -1;
        this._build();
    }

    _build() {
        this.window = new Adw.Window({
            application: this.app,
            title: 'galley',
            default_width: 960,
            default_height: 560,
            width_request: 360,
            height_request: 300,
            hide_on_close: true,
        });

        this.list = new Gtk.ListBox({
            selection_mode: Gtk.SelectionMode.SINGLE,
            css_classes: ['navigation-sidebar'],
        });
        this.list.set_header_func((row, before) => {
            if (before && before._record.group === row._record.group) {
                row.set_header(null);
                return;
            }
            row.set_header(new Gtk.Label({
                label: row._record.item.title || 'Untitled',
                xalign: 0,
                ellipsize: 3,
                css_classes: ['heading'],
                margin_top: before ? 12 : 6,
                margin_bottom: 6,
                margin_start: 12,
                margin_end: 12,
            }));
        });
        this.list.connect('row-selected', (_list, row) => this._selected(row));

        const sidebarView = new Adw.ToolbarView();
        sidebarView.add_top_bar(new Adw.HeaderBar());
        sidebarView.set_content(new Gtk.ScrolledWindow({
            child: this.list,
            hscrollbar_policy: Gtk.PolicyType.NEVER,
            vexpand: true,
        }));

        this.stack = new Gtk.Stack({vexpand: true, hexpand: true});
        this.stack.add_named(new Adw.StatusPage({
            title: 'Nothing waiting',
            icon_name: 'object-select-symbolic',
        }), 'empty');
        // In a focusable box, so that the focus has somewhere to rest that
        // takes no keys: what was being typed into a withdrawn question's
        // field lands here, and nowhere else. (A status page passes the
        // focus on to its children, of which it has none that take it.)
        this.withdrawn = new Gtk.Box({focusable: true});
        this.withdrawn.append(new Adw.StatusPage({
            title: 'That question was withdrawn',
            description: 'Its asker stopped waiting. Pick another with j or k, ↓ or ↑, or a click.',
            icon_name: 'edit-undo-symbolic',
            hexpand: true,
        }));
        this.stack.add_named(this.withdrawn, 'withdrawn');

        const contentView = new Adw.ToolbarView();
        contentView.add_top_bar(new Adw.HeaderBar());
        contentView.set_content(this.stack);
        this.content = new Adw.NavigationPage({title: 'galley', child: contentView});

        this.split = new Adw.NavigationSplitView({
            sidebar: new Adw.NavigationPage({title: 'Waiting', child: sidebarView}),
            content: this.content,
            min_sidebar_width: 220,
            max_sidebar_width: 340,
        });
        this.window.set_content(this.split);

        const keys = new Gtk.EventControllerKey({propagation_phase: Gtk.PropagationPhase.CAPTURE});
        keys.connect('key-pressed', (_c, keyval, _code, state) => this.handleKey(keyval, state));
        keys.connect('key-released', (_c, keyval) => this.release(keyval));
        this.window.add_controller(keys);

        this.window.connect('notify::is-active', () => {
            this._down.clear();
            if (this.window.is_active)
                this.app.withdraw_notification('waiting');
        });
        this.window.connect('notify::visible', () => this._tick());
    }

    // ---- items --------------------------------------------------------

    // Adds an item and returns its handle: append(text) for text-info read
    // from stdin, timeout() to answer it as timed out, withdraw() when its
    // client has gone. respond(answer) is called once, when it is answered.
    add(item, respond) {
        const record = {
            id: `item-${this._next++}`,
            item,
            group: item.title,
            arrived: GLib.get_monotonic_time(),
            respond,
            done: false,
            waitLabels: [],
        };
        record.row = this._row(record);
        record.page = this._page(record);

        const at = this.queue.add(record);
        if (this._parkedAt >= 0 && at <= this._parkedAt)
            this._parkedAt++;
        this.list.insert(record.row, at);
        this.list.invalidate_headers();
        this.stack.add_named(record.page, record.id);
        this._grow(item);

        // Only an empty selection takes the newcomer: one already made is
        // the person's, and moving it is how a key lands on the wrong item.
        // Nor does one left empty by a withdrawal, for the same reason.
        if (!this.list.get_selected_row() && this._parkedAt < 0)
            this.list.select_row(record.row);
        this._title();
        this._notify();

        return {
            append: text => this._append(record, text),
            timeout: () => this._finish(record, {answer: 'timeout'}, false),
            withdraw: () => this._remove(record, false),
        };
    }

    _row(record) {
        const {item} = record;
        const box = new Gtk.Box({spacing: 8, margin_top: 6, margin_bottom: 6, margin_start: 6, margin_end: 6});
        // Its first two lines of text, which for frisket's asker are the
        // workspace and what the request does.
        const shown = item.kind === 'text'
            ? item.info.text ?? ''
            : render(item.text ?? '', item.markup).text;
        const [first, second] = lines(shown, 2);
        box.append(new Gtk.Image({icon_name: symbolic(item), valign: Gtk.Align.START}));
        const words = new Gtk.Box({orientation: Gtk.Orientation.VERTICAL, hexpand: true});
        words.append(new Gtk.Label({
            label: first || item.title || 'Untitled', xalign: 0, ellipsize: 3, single_line_mode: true,
        }));
        if (second) {
            words.append(new Gtk.Label({
                label: second, xalign: 0, ellipsize: 3, single_line_mode: true,
                css_classes: ['dim-label', 'caption'],
            }));
        }
        box.append(words);
        const wait = new Gtk.Label({css_classes: ['dim-label', 'numeric', 'caption'], valign: Gtk.Align.START});
        box.append(wait);
        record.waitLabels.push(wait);
        const row = new Gtk.ListBoxRow({child: box});
        row._record = record;
        return row;
    }

    _page(record) {
        const {item} = record;
        const page = new Gtk.Box({
            orientation: Gtk.Orientation.VERTICAL,
            spacing: 12,
            margin_top: 18, margin_bottom: 18, margin_start: 18, margin_end: 18,
        });

        const head = new Gtk.Box({spacing: 12});
        head.append(icon(item.icon, item.kind));
        const heading = new Gtk.Box({orientation: Gtk.Orientation.VERTICAL, valign: Gtk.Align.CENTER});
        heading.append(new Gtk.Label({
            label: item.title || 'Untitled', xalign: 0, wrap: true, wrap_mode: 2, css_classes: ['title-3'],
        }));
        const wait = new Gtk.Label({xalign: 0, css_classes: ['dim-label', 'caption']});
        record.waitLabels.push(wait);
        heading.append(wait);
        head.append(heading);
        page.append(head);

        let scroller = null;
        if (item.text) {
            const {text, attributes} = render(item.text, item.markup);
            const label = new Gtk.Label({
                xalign: 0, yalign: 0, selectable: false, use_markup: false, use_underline: false,
                wrap: !item.noWrap && !item.ellipsize, wrap_mode: 2,
                ellipsize: item.ellipsize ? 3 : 0,
            });
            label.set_text(text);
            if (attributes)
                label.set_attributes(attributes);
            scroller = new Gtk.ScrolledWindow({
                child: label,
                vexpand: item.kind !== 'text' && item.kind !== 'entry',
                propagate_natural_height: true,
                hscrollbar_policy: item.noWrap ? Gtk.PolicyType.AUTOMATIC : Gtk.PolicyType.NEVER,
            });
            page.append(scroller);
        }

        if (item.kind === 'entry') {
            const entry = item.entry.hidden
                ? new Gtk.PasswordEntry({show_peek_icon: true})
                : new Gtk.Entry();
            if (item.entry.text)
                entry.set_text(item.entry.text);
            record.entry = entry;
            page.append(entry);
            page.append(new Gtk.Box({vexpand: true}));
        }

        if (item.kind === 'text') {
            const view = new Gtk.TextView({
                editable: false, cursor_visible: false, monospace: true,
                wrap_mode: item.noWrap ? Gtk.WrapMode.NONE : Gtk.WrapMode.WORD_CHAR,
                top_margin: 6, bottom_margin: 6, left_margin: 6, right_margin: 6,
            });
            view.buffer.set_text(item.info.text ?? '', -1);
            record.view = view;
            scroller = new Gtk.ScrolledWindow({child: view, vexpand: true, css_classes: ['card']});
            page.append(scroller);
            if (item.info.checkbox) {
                record.checkbox = new Gtk.CheckButton({label: item.info.checkbox, use_underline: false});
                page.append(record.checkbox);
            }
        }
        record.scroller = scroller;

        const buttons = new Gtk.Box({spacing: 8, halign: Gtk.Align.END});
        record.buttons = item.buttons.map((spec, i) => {
            const label = new Gtk.Label();
            label.set_markup(buttonMarkup(GLib, spec.label, spec.underline));
            const button = new Gtk.Button({child: label, focus_on_click: false});
            if (i === item.default)
                button.add_css_class('suggested-action');
            button.connect('clicked', () => this._press(record, spec));
            buttons.append(button);
            if (spec.answer === 'ok' && record.checkbox) {
                button.sensitive = false;
                record.checkbox.connect('toggled', () => {
                    button.sensitive = record.checkbox.active;
                });
            }
            return button;
        });
        page.append(buttons);
        return page;
    }

    _append(record, text) {
        if (record.done || !record.view)
            return;
        const buffer = record.view.buffer;
        buffer.insert(buffer.get_end_iter(), text, -1);
        if (record.item.info.autoScroll)
            record.view.scroll_to_mark(buffer.get_insert(), 0, false, 0, 0);
    }

    // A button pressed, by click or key. An OK held back by an unticked
    // --checkbox stays held back whichever way it is pressed.
    _press(record, spec) {
        if (record.checkbox && spec.answer === 'ok' && !record.checkbox.active)
            return;
        const answer = {answer: spec.answer};
        if (spec.answer === 'extra')
            answer.index = spec.index;
        this._finish(record, answer, true);
    }

    // An item answered: by the person, or else by its timeout.
    _finish(record, answer, byPerson) {
        if (record.done)
            return;
        if (record.entry && (answer.answer === 'ok' || answer.answer === 'timeout'))
            answer.entry = record.entry.get_text();
        record.respond(answer);
        this._remove(record, byPerson);
    }

    // An item gone. When it was the selected one and the person answered
    // it, its neighbour takes the selection: the next press is meant for
    // the next question. When it went any other way, nothing is selected
    // until the person picks, while the window is up for keys to reach.
    _remove(record, byPerson) {
        if (record.done)
            return;
        record.done = true;
        // A hidden entry's text is cleared from its widget the moment it is
        // answered or withdrawn, rather than left for the collector.
        if (record.entry) {
            record.entry.set_text('');
            record.entry = null;
        }
        const wasSelected = this.list.get_selected_row() === record.row;
        const at = this.queue.remove(record);
        this.list.remove(record.row);
        this.list.invalidate_headers();
        this.stack.remove(record.page);
        record.respond = null;

        if (this.queue.length === 0) {
            this._parkedAt = -1;
            this.app.withdraw_notification('waiting');
            this.window.set_visible(false);
        } else if (wasSelected && !byPerson && this.window.visible) {
            this._parkedAt = at;
            this.list.unselect_all();
            this._selected(null);
        } else if (wasSelected) {
            this.list.select_row(this.queue.at(this.queue.afterRemoval(at)).row);
        } else if (this._parkedAt > at) {
            this._parkedAt--;
        }
        this._title();
    }

    _selected(row) {
        if (!row) {
            const parked = this._parkedAt >= 0;
            this.stack.set_visible_child_name(parked ? 'withdrawn' : 'empty');
            this.content.title = 'galley';
            if (parked)
                this.withdrawn.grab_focus();
            return;
        }
        this._parkedAt = -1;
        const record = row._record;
        this.stack.set_visible_child_name(record.id);
        this.content.title = record.item.title || 'Untitled';
        // An entry is ready to type into; anything else leaves the focus on
        // the list, where letters are keys.
        if (record.entry)
            record.entry.grab_focus();
        else if (this.window.get_focus() !== row)
            row.grab_focus();
    }

    current() {
        return this.list.get_selected_row()?._record ?? null;
    }

    // ---- keys ---------------------------------------------------------

    // The window's keys, ahead of any widget's. Returns true when the key
    // was the window's.
    handleKey(keyval, state) {
        const repeat = this._down.has(keyval);
        this._down.add(keyval);
        const name = Gdk.keyval_name(keyval);

        if (name === 'Escape') {
            this.hide();
            return true;
        }
        if (state & (Gdk.ModifierType.CONTROL_MASK | Gdk.ModifierType.SUPER_MASK))
            return false;
        const alt = (state & Gdk.ModifierType.ALT_MASK) !== 0;
        const editing = editsText(this.window.get_focus());
        const record = this.current();

        switch (name) {
        case 'Up': case 'KP_Up':
            this.move(-1);
            return true;
        case 'Down': case 'KP_Down':
            this.move(1);
            return true;
        case 'Return': case 'KP_Enter': case 'ISO_Enter':
            // A button Tab has reached is pressed by Enter itself, as GTK
            // does: Enter on a focused Refuse must not Allow.
            if (isButton(this.window.get_focus()))
                return false;
            if (record && !repeat && record.item.default >= 0)
                this._press(record, record.item.buttons[record.item.default]);
            return true;
        case 'Page_Up': case 'Page_Down':
            if (record?.scroller) {
                const adj = record.scroller.vadjustment;
                const by = name === 'Page_Up' ? -adj.page_increment : adj.page_increment;
                adj.value = Math.max(adj.lower, Math.min(adj.upper - adj.page_size, adj.value + by));
            }
            return true;
        }

        if (!editing && !alt) {
            if (name === 'j') {
                this.move(1);
                return true;
            }
            if (name === 'k') {
                this.move(-1);
                return true;
            }
        }
        if (record && (alt || !editing)) {
            const code = Gdk.keyval_to_unicode(Gdk.keyval_to_lower(keyval));
            const key = code ? String.fromCodePoint(code).toLowerCase() : '';
            const spec = key && record.item.buttons.find(b => b.key === key);
            if (spec) {
                if (!repeat)
                    this._press(record, spec);
                return true;
            }
        }
        return false;
    }

    release(keyval) {
        this._down.delete(keyval);
    }

    // One step through the queue. From a withdrawn question's place, down
    // is the one that took its place and up the one above it.
    move(by) {
        let at;
        if (!this.current() && this._parkedAt >= 0)
            at = by > 0 ? this.queue.afterRemoval(this._parkedAt) : Math.max(0, this._parkedAt - 1);
        else
            at = this.queue.step(this.queue.indexOf(this.current()), by);
        if (at >= 0)
            this.list.select_row(this.queue.at(at).row);
    }

    // ---- the window ---------------------------------------------------

    // Brings the window forward, with the activation token of whatever asked
    // when there is one: under Wayland only that lets it take focus.
    show(token) {
        if (token)
            this.window.set_startup_id(token);
        this.window.present();
        this.app.withdraw_notification('waiting');
    }

    hide() {
        this.window.set_visible(false);
    }

    _title() {
        const n = this.queue.length;
        this.window.title = n ? `galley (${n} waiting)` : 'galley';
    }

    // Says something is waiting, when the window is not already in front.
    // Only with a session bus: a notification goes over it.
    _notify() {
        if (this.window.is_active || !this.app.get_dbus_connection())
            return;
        const n = this.queue.length;
        const notification = new Gio.Notification();
        notification.set_title(n === 1 ? 'A question is waiting' : `${n} questions are waiting`);
        notification.set_body('Open galley to answer.');
        notification.set_default_action('app.show');
        // Urgent, so that Do Not Disturb still shows it and it stays until
        // dismissed: a caller is blocked until it is answered, and nothing
        // else on screen says so.
        notification.set_priority(Gio.NotificationPriority.URGENT);
        this.app.send_notification('waiting', notification);
    }

    // The first item to ask for more room than the window has gets it, if
    // the window has not been shown yet.
    _grow(item) {
        if (this.window.visible)
            return;
        const [w, h] = this.window.get_default_size();
        const width = item.width > 0 ? Math.min(item.width + 300, 2400) : 0;
        const height = item.height > 0 ? Math.min(item.height + 120, 1600) : 0;
        this.window.set_default_size(Math.max(w, width), Math.max(h, height));
    }

    // How long each item has waited, refreshed each second while it shows.
    _tick() {
        if (this._ticking || !this.window.visible)
            return;
        const update = () => {
            const now = GLib.get_monotonic_time();
            for (const record of this.queue.items) {
                const text = waited(now - record.arrived);
                record.waitLabels[0].label = text;
                record.waitLabels[1].label = `waiting ${text}`;
            }
        };
        update();
        this._ticking = GLib.timeout_add_seconds(GLib.PRIORITY_DEFAULT, 1, () => {
            if (!this.window.visible) {
                this._ticking = 0;
                return GLib.SOURCE_REMOVE;
            }
            update();
            return GLib.SOURCE_CONTINUE;
        });
    }

    // What the end-to-end check reads: never sent anywhere else.
    state() {
        const current = this.current();
        return {
            visible: this.window.visible,
            selected: current?.id ?? null,
            withdrawn: this._parkedAt >= 0,
            focus: describe(this.window.get_focus(), current, this.withdrawn),
            items: this.queue.items.map(r => ({
                id: r.id,
                kind: r.item.kind,
                title: r.item.title,
                group: r.group,
                text: render(r.item.text ?? '', r.item.markup).text,
                info: r.view ? r.view.buffer.text : undefined,
                buttons: r.item.buttons.map(b => ({label: b.label, key: b.key ?? ''})),
            })),
        };
    }
}

const isButton = widget => widget instanceof Gtk.Button || widget instanceof Gtk.CheckButton;

// What has the focus, for the end-to-end check: "entry", "row", "button:"
// and its label, "page" for the withdrawn page, else the widget's type.
function describe(widget, record, withdrawn) {
    if (!widget)
        return '';
    if (widget instanceof Gtk.ListBoxRow)
        return 'row';
    if (widget === withdrawn)
        return 'page';
    if (editsText(widget))
        return 'entry';
    const i = record?.buttons?.indexOf(widget) ?? -1;
    if (i >= 0)
        return `button:${record.item.buttons[i].label}`;
    return widget.constructor.name;
}

// An item's icon: a file when the name is a path, else a theme icon.
function icon(name, kind) {
    const image = new Gtk.Image({pixel_size: 48, valign: Gtk.Align.START});
    if (name && name.startsWith('/'))
        image.set_from_gicon(Gio.FileIcon.new(Gio.File.new_for_path(name)));
    else
        image.set_from_gicon(Gio.ThemedIcon.new_with_default_fallbacks(name || fallbackIcon[kind]));
    return image;
}

const fallbackIcon = {
    question: 'dialog-question', info: 'dialog-information', warning: 'dialog-warning',
    error: 'dialog-error', entry: 'insert-text', text: 'accessories-text-editor',
};

const symbolicIcon = {
    question: 'dialog-question-symbolic', info: 'dialog-information-symbolic',
    warning: 'dialog-warning-symbolic', error: 'dialog-error-symbolic',
    entry: 'document-edit-symbolic', text: 'text-x-generic-symbolic',
};

function symbolic(item) {
    if (item.kind === 'entry' && item.entry.hidden)
        return 'dialog-password-symbolic';
    return symbolicIcon[item.kind] ?? 'dialog-question-symbolic';
}
