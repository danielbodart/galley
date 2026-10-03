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
// Enter's default: Enter presses that button, as it would in any dialog;
// and so are a field of several lines, where Enter is a new line, and a
// list's cell being edited, where it ends the edit.
//
// ANSWERS ARE THIS WINDOW'S ALONE. Buttons answer through their own signal
// handlers, never through a GAction: an application's actions are exported
// on the session bus, so an "answer" action would let any process there
// press Allow. The only action is "show". GTK's accessibility bus is the
// other way in -- it can click any button -- and the units start the window
// with it off (GTK_A11Y=none; see nix/home.nix).
//
// Every one of zenity's dialogs is an item here, including those that ask
// nothing: a progress bar is a live item until it is done and answered, and
// a notification is an entry that stays, once its client has gone, until
// the person dismisses it.

import GLib from 'gi://GLib';
import Gio from 'gi://Gio';
import Gdk from 'gi://Gdk?version=4.0';
import Gtk from 'gi://Gtk?version=4.0';
import Adw from 'gi://Adw?version=1';

import {Queue, waited} from './queue.js';
import {render, lines, buttonMarkup} from './render.js';
import {makeBody, chooseFiles} from './bodies.js';

// What takes typed text, where letters are not keys.
const editsText = widget =>
    widget instanceof Gtk.Text || (widget instanceof Gtk.TextView && widget.editable);

// Where Enter is the widget's: a new line in a field of several, the end of
// an edit in a list's cell.
const ownsEnter = widget =>
    (widget instanceof Gtk.TextView && widget.editable) ||
    (widget instanceof Gtk.Text && ancestor(widget, Gtk.EditableLabel));

// Widgets that move within themselves by the arrows: a list's rows, a
// calendar's days, a scale's value, a text's lines, a colour's swatches.
// The arrows are theirs while they have the focus; j and k still move the
// queue.
const ARROWS = [Gtk.ColumnView, Gtk.ListView, Gtk.Calendar, Gtk.Range, Gtk.TextView,
    Gtk.ComboBox, Gtk.DropDown, Gtk.ColorChooserWidget];
const usesArrows = widget => ARROWS.some(type => ancestor(widget, type));

function ancestor(widget, type) {
    for (let w = widget; w; w = w.get_parent()) {
        if (w instanceof type)
            return w;
        if (w instanceof Gtk.Window)
            return null;
    }
    return null;
}

// Items that wait for an answer, which is what the notification that
// something is waiting counts: not a progress bar, which is still going,
// nor a notification, which says itself.
const asks = item => item.kind !== 'progress' && item.kind !== 'notification';

export class QueueWindow {
    constructor(app) {
        this.app = app;
        this.queue = new Queue();
        this._next = 1;
        this._down = new Set();
        // Where the selected item was when its asker withdrew it, while
        // nothing has been picked since; -1 otherwise.
        this._parkedAt = -1;
        // Opens GTK's file chooser for a file-selection item; the test
        // control stands in for it.
        this.chooseFiles = chooseFiles;
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
                label: row._record.group || 'Untitled',
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
                this._withdrawNotifications();
        });
        this.window.connect('notify::visible', () => this._tick());
    }

    // ---- items --------------------------------------------------------

    // Adds an item and returns its handle: what the client's lines change
    // (append, rows, progress), timeout() to answer it as timed out, and
    // withdraw() when its client has gone. client.answer(answer) is called
    // once, when it is answered; client.tell(line) for a line before that.
    add(item, client) {
        const record = this._add(item, client);
        const follow = record.body.follow ?? {};
        return {
            append: text => !record.done && follow.append?.(text),
            rows: rows => !record.done && follow.rows?.(rows),
            progress: update => !record.done && follow.progress?.(update),
            timeout: () => this._finish(record, {answer: 'timeout'}, false),
            // A notification outlives its client, as zenity's does: it is
            // the person's to dismiss.
            withdraw: () => {
                if (record.item.kind === 'notification')
                    record.client = null;
                else
                    this._remove(record, false);
            },
        };
    }

    // A --notification --listen: nothing until its first message, then one
    // entry whose text each message replaces, as each replaces zenity's
    // one notification -- or a new entry, once the person has dismissed it.
    // Its entries stay when its client goes.
    listen(item) {
        let current = null;
        return {
            notify: message => {
                const icon = message.icon || 'dialog-information';
                if (current && !current.done) {
                    current.item.text = message.text;
                    current.item.icon = icon;
                    this._setText(current, message.text, false);
                    current.icon.set_from_gicon(gicon(icon));
                    this._notify(current);
                } else {
                    current = this._add({...item, text: message.text, icon, markup: false}, null);
                }
            },
            withdraw: () => {},
        };
    }

    _add(item, client) {
        const record = {
            id: `item-${this._next++}`,
            item,
            group: item.title,
            arrived: GLib.get_monotonic_time(),
            client,
            done: false,
            waitLabels: [],
            default: item.default,
        };
        record.body = makeBody(item, {
            window: this,
            record,
            press: answer => {
                const spec = item.buttons.find(b => b.answer === answer);
                if (spec)
                    this._press(record, spec);
                else if (answer === 'ok')
                    this._finish(record, {answer: 'ok'}, true);
            },
            finish: answer => this._finish(record, answer, true),
            tell: line => {
                if (!record.done)
                    record.client?.tell(line);
            },
            setText: (text, markup) => this._setText(record, text, markup),
            setButton: (answer, how) => this._setButton(record, answer, how),
        });
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
        this._notify(record);
        return record;
    }

    _row(record) {
        const {item} = record;
        const box = new Gtk.Box({spacing: 8, margin_top: 6, margin_bottom: 6, margin_start: 6, margin_end: 6});
        box.append(new Gtk.Image({icon_name: symbolic(item), valign: Gtk.Align.START}));
        const words = new Gtk.Box({orientation: Gtk.Orientation.VERTICAL, hexpand: true});
        record.rowFirst = new Gtk.Label({xalign: 0, ellipsize: 3, single_line_mode: true});
        record.rowSecond = new Gtk.Label({
            xalign: 0, ellipsize: 3, single_line_mode: true, css_classes: ['dim-label', 'caption'],
        });
        words.append(record.rowFirst);
        words.append(record.rowSecond);
        if (record.body.rowWidget)
            words.append(record.body.rowWidget);
        box.append(words);
        const wait = new Gtk.Label({css_classes: ['dim-label', 'numeric', 'caption'], valign: Gtk.Align.START});
        box.append(wait);
        record.waitLabels.push(wait);
        this._rowText(record);
        const row = new Gtk.ListBoxRow({child: box});
        row._record = record;
        return row;
    }

    // A row's first two lines of text, which for frisket's asker are the
    // workspace and what the request does.
    _rowText(record) {
        const {item} = record;
        const shown = item.kind === 'text'
            ? item.info.text ?? ''
            : render(item.text ?? '', item.markup).text;
        const [first, second] = lines(shown, 2);
        record.rowFirst.set_text(first || item.title || 'Untitled');
        record.rowSecond.set_text(second ?? '');
        record.rowSecond.visible = Boolean(second);
    }

    _page(record) {
        const {item, body} = record;
        const page = new Gtk.Box({
            orientation: Gtk.Orientation.VERTICAL,
            spacing: 12,
            margin_top: 18, margin_bottom: 18, margin_start: 18, margin_end: 18,
        });

        const head = new Gtk.Box({spacing: 12});
        record.icon = icon(item.icon, item.kind);
        head.append(record.icon);
        const heading = new Gtk.Box({orientation: Gtk.Orientation.VERTICAL, valign: Gtk.Align.CENTER});
        heading.append(new Gtk.Label({
            label: item.title || 'Untitled', xalign: 0, wrap: true, wrap_mode: 2, css_classes: ['title-3'],
        }));
        const wait = new Gtk.Label({xalign: 0, css_classes: ['dim-label', 'caption']});
        record.waitLabels.push(wait);
        heading.append(wait);
        head.append(heading);
        page.append(head);

        record.label = new Gtk.Label({
            xalign: 0, yalign: 0, selectable: false, use_markup: false, use_underline: false,
            wrap: !item.noWrap && !item.ellipsize, wrap_mode: 2,
            ellipsize: item.ellipsize ? 3 : 0,
        });
        const textScroller = new Gtk.ScrolledWindow({
            child: record.label,
            vexpand: !body.expands && !body.widget,
            propagate_natural_height: true,
            hscrollbar_policy: item.noWrap ? Gtk.PolicyType.AUTOMATIC : Gtk.PolicyType.NEVER,
        });
        page.append(textScroller);
        record.textScroller = textScroller;
        this._setText(record, item.text ?? '', item.markup);

        if (body.widget) {
            page.append(body.widget);
            if (!body.expands)
                page.append(new Gtk.Box({vexpand: true}));
        }
        record.scroller = body.scroller ?? textScroller;

        const buttons = new Gtk.Box({spacing: 8, halign: Gtk.Align.END});
        record.buttons = item.buttons.map((spec, i) => {
            const label = new Gtk.Label();
            label.set_markup(buttonMarkup(GLib, spec.label, spec.underline));
            const button = new Gtk.Button({child: label, focus_on_click: false, sensitive: !spec.disabled});
            if (i === record.default)
                button.add_css_class('suggested-action');
            button.connect('clicked', () => this._press(record, spec));
            buttons.append(button);
            if (spec.answer === 'ok' && body.checkbox) {
                button.sensitive = false;
                body.checkbox.connect('toggled', () => {
                    button.sensitive = body.checkbox.active;
                });
            }
            return button;
        });
        page.append(buttons);
        return page;
    }

    // An item's text, replaced: a progress item's "#" lines, a listening
    // notification's messages.
    _setText(record, text, markup) {
        record.item.text = text;
        record.item.markup = markup;
        const rendered = render(text, markup);
        record.label.set_text(rendered.text);
        record.label.set_attributes(rendered.attributes);
        record.textScroller.visible = rendered.text !== '';
        this._rowText(record);
    }

    // A button enabled or disabled, and made the default: a progress
    // item's OK once it is done, its Cancel once its stdin is.
    _setButton(record, answer, {enabled, isDefault}) {
        const i = record.item.buttons.findIndex(b => b.answer === answer);
        if (i < 0)
            return;
        record.buttons[i].sensitive = enabled;
        if (isDefault) {
            record.buttons[record.default]?.remove_css_class('suggested-action');
            record.default = i;
            record.buttons[i].add_css_class('suggested-action');
        }
    }

    // A button pressed, by click or key. One that is disabled -- an OK held
    // back by an unticked --checkbox, a progress bar's OK before it is done
    // -- stays so whichever way it is pressed. A body may take the press
    // itself: the file chooser's Open… opens the chooser.
    _press(record, spec) {
        if (record.done)
            return;
        const i = record.item.buttons.indexOf(spec);
        if (i >= 0 && !record.buttons[i].sensitive)
            return;
        if (record.body.press?.(spec))
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
        if (answer.answer === 'ok' || answer.answer === 'timeout')
            record.body.values?.(answer);
        record.client?.answer(answer);
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
        // What was typed -- a password above all -- is cleared from its
        // widget the moment it is answered or withdrawn, rather than left
        // for the collector; and a chooser still open is closed.
        record.body.clear?.();
        if (record.noted)
            this.app.withdraw_notification(record.id);
        const wasSelected = this.list.get_selected_row() === record.row;
        const at = this.queue.remove(record);
        this.list.remove(record.row);
        this.list.invalidate_headers();
        this.stack.remove(record.page);
        record.client = null;

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
        // A field is ready to type into, a list or a calendar to move
        // through; anything else leaves the focus on the queue's row,
        // where letters are keys.
        if (record.body.focus)
            record.body.focus.grab_focus();
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
        const focus = this.window.get_focus();
        const editing = editsText(focus);
        const record = this.current();

        switch (name) {
        case 'Up': case 'KP_Up': case 'Down': case 'KP_Down':
            // A list, a calendar or a scale with the focus moves itself.
            if (!alt && usesArrows(focus))
                return false;
            this.move(name.endsWith('Up') ? -1 : 1);
            return true;
        case 'Return': case 'KP_Enter': case 'ISO_Enter':
            // A button Tab has reached is pressed by Enter itself, as GTK
            // does: Enter on a focused Refuse must not Allow. A field of
            // several lines takes it as a new line.
            if (isButton(focus) || ownsEnter(focus))
                return false;
            if (record && !repeat && record.default >= 0)
                this._press(record, record.item.buttons[record.default]);
            return true;
        case 'Page_Up': case 'Page_Down':
            if (record?.scroller) {
                const adj = record.scroller.vadjustment;
                const by = name === 'Page_Up' ? -adj.page_increment : adj.page_increment;
                adj.value = Math.max(adj.lower, Math.min(adj.upper - adj.page_size, adj.value + by));
            }
            return true;
        case 'slash':
            // A list's search.
            if (!editing && record?.body.search) {
                record.body.search.grab_focus();
                return true;
            }
            break;
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
        this._withdrawNotifications();
    }

    hide() {
        this.window.set_visible(false);
    }

    _title() {
        const n = this.queue.length;
        this.window.title = n ? `galley (${n} waiting)` : 'galley';
    }

    // Says something has arrived, when the window is not already in front.
    // Only with a session bus: a notification goes over it.
    //
    // For a question, the one notification that questions are waiting, with
    // no word of theirs in it, urgent: a caller is blocked until it is
    // answered, and nothing else on screen says so. For a notification, its
    // own text, at normal priority, as zenity's would have been; it is
    // withdrawn when the entry is dismissed, or the window comes forward.
    // A progress bar says nothing: it is still going.
    _notify(record) {
        if (this.window.is_active || !this.app.get_dbus_connection())
            return;
        const {item} = record;
        if (item.kind === 'notification') {
            const [title, ...body] = (item.text ?? '').split('\n');
            const notification = new Gio.Notification();
            notification.set_title(title);
            if (body.length)
                notification.set_body(body.join('\n'));
            notification.set_icon(gicon(item.icon || 'dialog-information'));
            notification.set_default_action('app.show');
            this.app.send_notification(record.id, notification);
            record.noted = true;
            return;
        }
        if (!asks(item))
            return;
        const n = this.queue.items.filter(r => asks(r.item)).length;
        const notification = new Gio.Notification();
        notification.set_title(n === 1 ? 'A question is waiting' : `${n} questions are waiting`);
        notification.set_body('Open galley to answer.');
        notification.set_default_action('app.show');
        notification.set_priority(Gio.NotificationPriority.URGENT);
        this.app.send_notification('waiting', notification);
    }

    _withdrawNotifications() {
        this.app.withdraw_notification('waiting');
        for (const record of this.queue.items) {
            if (record.noted) {
                this.app.withdraw_notification(record.id);
                record.noted = false;
            }
        }
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
                info: r.item.kind === 'text' ? r.body.state().text : undefined,
                icon: r.item.icon,
                buttons: r.item.buttons.map((b, i) => ({
                    label: b.label, key: b.key ?? '', enabled: r.buttons[i].sensitive,
                })),
                default: r.default,
                connected: r.client !== null,
                body: r.body.state?.(),
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
    for (const type of ARROWS) {
        const w = ancestor(widget, type);
        if (w)
            return w.constructor.name.replace(/^Gtk_?/, '');
    }
    return widget.constructor.name;
}

// A theme icon's name or a file's path, as a GIcon.
function gicon(name) {
    if (name.startsWith('/'))
        return Gio.FileIcon.new(Gio.File.new_for_path(name));
    return Gio.ThemedIcon.new_with_default_fallbacks(name);
}

// An item's icon: a file when the name is a path, else a theme icon.
function icon(name, kind) {
    const image = new Gtk.Image({pixel_size: 48, valign: Gtk.Align.START});
    image.set_from_gicon(gicon(name || fallbackIcon[kind] || 'dialog-question'));
    return image;
}

const fallbackIcon = {
    question: 'dialog-question', info: 'dialog-information', warning: 'dialog-warning',
    error: 'dialog-error', entry: 'insert-text', text: 'accessories-text-editor',
    list: 'view-list', forms: 'document-edit', calendar: 'x-office-calendar',
    scale: 'dialog-question', password: 'dialog-password', color: 'applications-graphics',
    file: 'document-open', progress: 'appointment-soon', notification: 'dialog-information',
    about: 'help-about',
};

const symbolicIcon = {
    question: 'dialog-question-symbolic', info: 'dialog-information-symbolic',
    warning: 'dialog-warning-symbolic', error: 'dialog-error-symbolic',
    entry: 'document-edit-symbolic', text: 'text-x-generic-symbolic',
    list: 'view-list-symbolic', forms: 'document-edit-symbolic',
    calendar: 'x-office-calendar-symbolic', scale: 'view-continuous-symbolic',
    password: 'dialog-password-symbolic', color: 'color-select-symbolic',
    file: 'document-open-symbolic', progress: 'content-loading-symbolic',
    notification: 'preferences-system-notifications-symbolic', about: 'help-about-symbolic',
};

function symbolic(item) {
    if (item.kind === 'entry' && item.entry.hidden)
        return 'dialog-password-symbolic';
    return symbolicIcon[item.kind] ?? 'dialog-question-symbolic';
}
