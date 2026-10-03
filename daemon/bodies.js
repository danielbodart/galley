// Each kind of item's own part of its page: what sits between its text and
// its buttons, and what it answers.
//
// A body is a plain object:
//
//   widget     what the page shows, or none
//   expands    whether it takes the page's spare height
//   focus      what takes the focus when the item is selected; none leaves
//              it on the queue's row, where letters are keys
//   scroller   what Page Up and Page Down scroll
//   search     a list's search field, which / reaches
//   values(a)  puts what the person chose into answer a, for OK or timeout
//   press(b)   takes a button's press instead of the window answering it,
//              as the file chooser's Open… does; true when it did
//   clear()    forgets what was typed, and stops anything still running
//   follow     {append, rows, progress}: what the client's lines change
//   state()    what the end-to-end test reads
//   fill(v)    what the end-to-end test sets, as a person would
//
// Everything a caller sent is set as plain text, as the window's own text
// is: labels with set_text, cells as labels, nothing as markup but the text
// the window has already sanitised (render.js).

import GLib from 'gi://GLib';
import Gio from 'gi://Gio';
import Gdk from 'gi://Gdk?version=4.0';
import Gtk from 'gi://Gtk?version=4.0';

import {MAX_ROWS} from './validate.js';
import {formatDate} from './dates.js';
import {isDiff, diffSpans} from './diff.js';

export function makeBody(item, ctx) {
    const make = kinds[item.kind];
    return make ? make(item, ctx) : {};
}

const kinds = {
    entry: entryBody,
    text: textBody,
    list: listBody,
    forms: formsBody,
    calendar: calendarBody,
    scale: scaleBody,
    password: passwordBody,
    color: colorBody,
    file: fileBody,
    progress: progressBody,
};

// ---- entry --------------------------------------------------------------

function entryBody(item) {
    let entry, widget;
    if (item.entry.values) {
        // zenity's combo: a text field with the values to pick from, the
        // --entry-text first and picked when there is one.
        widget = Gtk.ComboBoxText.new_with_entry();
        for (const v of item.entry.values)
            widget.append_text(v);
        if (item.entry.text !== '' && item.entry.values[0] === item.entry.text)
            widget.set_active(0);
        entry = widget.get_child();
    } else {
        entry = item.entry.hidden
            ? new Gtk.PasswordEntry({show_peek_icon: true})
            : new Gtk.Entry();
        if (item.entry.text)
            entry.set_text(item.entry.text);
        widget = entry;
    }
    return {
        widget,
        focus: entry,
        values: a => {
            a.entry = entry.get_text();
        },
        // A hidden entry's text is cleared from its widget the moment it is
        // answered or withdrawn, rather than left for the collector.
        clear: () => entry.set_text(''),
        state: () => ({entry: item.entry.hidden ? undefined : entry.get_text(), values: item.entry.values}),
        fill: v => entry.set_text(String(v)),
        entry,
    };
}

// ---- text-info ----------------------------------------------------------

function textBody(item) {
    const editable = item.info.editable;
    const view = new Gtk.TextView({
        editable, cursor_visible: editable, monospace: true,
        wrap_mode: item.noWrap ? Gtk.WrapMode.NONE : Gtk.WrapMode.WORD_CHAR,
        top_margin: 6, bottom_margin: 6, left_margin: 6, right_margin: 6,
    });
    view.buffer.set_text(item.info.text ?? '', -1);
    // A diff is coloured as one (diff.js) -- not one being edited, whose
    // colours the typing would leave behind.
    const colour = editable ? () => {} : diffColours(view.buffer);
    colour();
    const scroller = new Gtk.ScrolledWindow({child: view, vexpand: true, css_classes: ['card']});
    const box = new Gtk.Box({orientation: Gtk.Orientation.VERTICAL, spacing: 12, vexpand: true});
    box.append(scroller);
    let checkbox = null;
    if (item.info.checkbox) {
        checkbox = new Gtk.CheckButton({label: item.info.checkbox, use_underline: false});
        box.append(checkbox);
    }
    const text = () => {
        const buffer = view.buffer;
        return buffer.get_text(buffer.get_start_iter(), buffer.get_end_iter(), false);
    };
    return {
        widget: box,
        expands: true,
        scroller,
        // An editable text is typed into as an entry is; a read-only one
        // leaves the keys to the buttons.
        focus: editable ? view : null,
        checkbox,
        view,
        values: a => {
            if (editable)
                a.text = text();
        },
        clear: () => {
            if (editable)
                view.buffer.set_text('', -1);
        },
        follow: {
            append: s => {
                const buffer = view.buffer;
                buffer.insert(buffer.get_end_iter(), s, -1);
                colour();
                if (item.info.autoScroll)
                    view.scroll_to_mark(buffer.get_insert(), 0, false, 0, 0);
            },
        },
        state: () => ({text: text(), diff: diffShown(view.buffer)}),
        fill: v => view.buffer.set_text(String(v), -1),
    };
}

// What colours a diff, by kind of span: a line's background across the
// view's width, a changed part's behind its characters, a header's letters.
// Translucent, and a mid blue for a hunk's header, so that each reads on the
// light style's view and on the dark's alike.
const diffTags = {
    add: {paragraph_background_rgba: rgba('rgba(46, 194, 126, 0.16)')},
    del: {paragraph_background_rgba: rgba('rgba(224, 27, 36, 0.13)')},
    addWord: {background_rgba: rgba('rgba(46, 194, 126, 0.40)')},
    delWord: {background_rgba: rgba('rgba(224, 27, 36, 0.32)')},
    hunk: {weight: 700, foreground_rgba: rgba('#3584e4')},
    file: {weight: 700},
};

function rgba(spec) {
    const c = new Gdk.RGBA();
    c.parse(spec);
    return c;
}

// Colours the buffer's text when it is a diff, again each time it is called
// -- once more is added -- at most once a main loop's turn.
function diffColours(buffer) {
    const tags = {};
    for (const [kind, props] of Object.entries(diffTags)) {
        tags[kind] = new Gtk.TextTag({name: `diff-${kind}`, ...props});
        buffer.tag_table.add(tags[kind]);
    }
    let pending = 0;
    const apply = () => {
        pending = 0;
        const start = buffer.get_start_iter(), end = buffer.get_end_iter();
        for (const tag of Object.values(tags))
            buffer.remove_tag(tag, start, end);
        const text = buffer.get_text(start, end, false);
        buffer._diff = isDiff(text);
        if (!buffer._diff)
            return GLib.SOURCE_REMOVE;
        for (const {kind, start: from, end: to} of diffSpans(text))
            buffer.apply_tag(tags[kind], buffer.get_iter_at_offset(from), buffer.get_iter_at_offset(to));
        return GLib.SOURCE_REMOVE;
    };
    return () => {
        if (!pending)
            pending = GLib.idle_add(GLib.PRIORITY_DEFAULT_IDLE, apply);
    };
}

const diffShown = buffer => Boolean(buffer._diff);

// ---- a table, for --list and a form's list -------------------------------

// A table of rows, as zenity's list shows them: a column each, sortable by
// its header, filtered by a search, with the first column a tick, a choice
// or an image for those lists. Cells are plain text; an editable list's are
// editable labels.
function makeTable({columns, type = '', multiple = false, editable = false,
    header = true, hidden = [], autoselect = true}, activated) {
    const rows = [];
    const checked = [];
    const toggles = type === 'check' || type === 'radio';
    const model = new Gtk.StringList();
    const view = new Gtk.ColumnView({
        reorderable: false,
        single_click_activate: toggles,
        hexpand: true,
        vexpand: true,
    });
    const sorted = new Gtk.SortListModel({model, sorter: view.get_sorter()});
    let search = '';
    const filter = new Gtk.CustomFilter();
    filter.set_filter_func(o => !search || rows[Number(o.string)].join(' ').toLowerCase().includes(search));
    const filtered = new Gtk.FilterListModel({model: sorted, filter});
    const selection = multiple
        ? new Gtk.MultiSelection({model: filtered})
        : new Gtk.SingleSelection({model: filtered, autoselect, can_unselect: !autoselect});
    view.set_model(selection);

    // The ticks bound to rows on screen, so a change to one row's tick can
    // be shown on the others; and, for a radio list, the group they share,
    // anchored by one never shown.
    const bound = new Set();
    const anchor = type === 'radio' ? new Gtk.CheckButton() : null;
    const show = () => {
        for (const w of bound) {
            w._binding = true;
            w.active = checked[w._row] ?? false;
            w._binding = false;
        }
    };
    const tick = (row, on) => {
        if (type === 'radio') {
            if (!on)
                return;
            checked.fill(false);
        }
        checked[row] = on;
        show();
    };

    columns.forEach((title, c) => {
        const toggle = c === 0 && toggles;
        const image = c === 0 && type === 'image';
        const factory = new Gtk.SignalListItemFactory();
        factory.connect('setup', (_f, li) => {
            let w;
            if (toggle) {
                w = new Gtk.CheckButton({focus_on_click: false});
                if (anchor)
                    w.set_group(anchor);
                w.connect('toggled', () => {
                    if (!w._binding && w._row !== undefined && (w.active || type === 'check'))
                        tick(w._row, w.active);
                });
            } else if (image) {
                w = new Gtk.Image({pixel_size: 24});
            } else if (editable) {
                w = new Gtk.EditableLabel({hexpand: true});
            } else {
                w = new Gtk.Label({xalign: 0, use_markup: false, use_underline: false});
            }
            w.halign = toggle || image ? Gtk.Align.START : Gtk.Align.FILL;
            li.set_child(w);
        });
        factory.connect('bind', (_f, li) => {
            const row = Number(li.item.string);
            const w = li.child;
            w._row = row;
            const cell = rows[row][c] ?? '';
            if (toggle) {
                bound.add(w);
                w._binding = true;
                w.active = checked[row] ?? false;
                w._binding = false;
            } else if (image) {
                if (cell.startsWith('/'))
                    w.set_from_file(cell);
                else
                    w.clear();
            } else if (editable) {
                w.text = cell;
                w._edit = w.connect('notify::text', () => {
                    if (rows[row].length > c)
                        rows[row][c] = w.text;
                });
            } else {
                w.set_text(cell);
            }
        });
        factory.connect('unbind', (_f, li) => {
            const w = li.child;
            bound.delete(w);
            if (w._edit) {
                w.disconnect(w._edit);
                w._edit = 0;
            }
            w._row = undefined;
        });
        const column = new Gtk.ColumnViewColumn({
            title: header ? title : '',
            factory,
            expand: !toggle && !image,
            resizable: !toggle,
            visible: !hidden.includes(c),
        });
        if (!toggle) {
            const sorter = new Gtk.CustomSorter();
            sorter.set_sort_func((a, b) => {
                const x = rows[Number(a.string)][c] ?? '', y = rows[Number(b.string)][c] ?? '';
                return x < y ? -1 : x > y ? 1 : 0;
            });
            column.set_sorter(sorter);
        }
        view.append_column(column);
    });
    if (!header) {
        for (let w = view.get_first_child(); w; w = w.get_next_sibling()) {
            if (w.get_css_name() === 'header')
                w.visible = false;
        }
    }

    // Enter is the window's -- it answers OK -- so a row is activated by a
    // double click, or a single one in a tick list, where Space ticks too.
    view.connect('activate', (_v, position) => {
        const row = Number(selection.get_item(position)?.string);
        if (Number.isNaN(row))
            return;
        if (toggles)
            tick(row, type === 'radio' ? true : !checked[row]);
        else
            activated?.();
    });
    if (toggles) {
        const space = new Gtk.EventControllerKey({propagation_phase: Gtk.PropagationPhase.CAPTURE});
        space.connect('key-pressed', (_c, keyval, _code, state) => {
            if (keyval !== Gdk.KEY_space || state & Gdk.ModifierType.CONTROL_MASK)
                return false;
            for (const row of selectedRows()) {
                tick(row, type === 'radio' ? true : !checked[row]);
                if (type === 'radio')
                    break;
            }
            return true;
        });
        view.add_controller(space);
    }

    function selectedRows() {
        const out = [];
        for (let i = 0; i < selection.get_n_items(); i++) {
            if (selection.is_selected(i))
                out.push(Number(selection.get_item(i).string));
        }
        return out;
    }

    return {
        view,
        // Rows arriving, a column's worth each, the last perhaps short. A
        // radio list's last TRUE is its choice, as GTK's group makes it.
        add(more) {
            const from = rows.length;
            const room = MAX_ROWS - from;
            if (room <= 0)
                return;
            more = more.slice(0, room);
            for (const cells of more) {
                const row = rows.length;
                rows.push([...cells]);
                const on = toggles && (cells[0] ?? '').toLowerCase() === 'true';
                if (on && type === 'radio')
                    checked.fill(false);
                checked.push(on);
            }
            model.splice(from, 0, more.map((_, i) => String(from + i)));
            show();
        },
        // What zenity prints from: a tick list's ticked rows, in the order
        // they came; any other's selected rows, in the order shown.
        chosen() {
            const which = toggles
                ? rows.map((_, i) => i).filter(i => checked[i])
                : selectedRows();
            return which.map(i => [...rows[i]]);
        },
        search(text) {
            search = text.toLowerCase();
            filter.changed(Gtk.FilterChange.DIFFERENT);
        },
        clear() {
            rows.length = 0;
            checked.length = 0;
            model.splice(0, model.get_n_items(), []);
        },
        state: () => ({rows, checked, selected: selectedRows()}),
        fill(v) {
            if (Array.isArray(v.select)) {
                selection.unselect_all();
                for (let i = 0; i < selection.get_n_items(); i++) {
                    if (v.select.includes(Number(selection.get_item(i).string)))
                        selection.select_item(i, false);
                }
            }
            if (Number.isInteger(v.tick))
                tick(v.tick, type === 'radio' ? true : !checked[v.tick]);
            if (Array.isArray(v.edit)) {
                const [row, column, text] = v.edit;
                rows[row][column] = text;
                model.items_changed(0, 0, 0);
            }
            if (typeof v.search === 'string')
                this.search(v.search);
            if (v.activate)
                activated?.();
        },
    };
}

function listBody(item, ctx) {
    const l = item.list;
    const table = makeTable({
        columns: l.columns, type: l.type, multiple: l.multiple, editable: l.editable,
        header: !l.hideHeader, hidden: l.hidden,
    }, () => ctx.press('ok'));
    table.add(l.rows);
    const search = new Gtk.SearchEntry({placeholder_text: 'Search (/)'});
    search.connect('search-changed', () => table.search(search.get_text()));
    const scroller = new Gtk.ScrolledWindow({child: table.view, vexpand: true, css_classes: ['card']});
    const box = new Gtk.Box({orientation: Gtk.Orientation.VERTICAL, spacing: 6, vexpand: true});
    box.append(search);
    box.append(scroller);
    return {
        widget: box,
        expands: true,
        focus: table.view,
        scroller,
        search,
        values: a => {
            a.rows = table.chosen();
        },
        clear: () => table.clear(),
        follow: {rows: more => table.add(more)},
        state: table.state,
        fill: v => table.fill(v),
    };
}

// ---- dates --------------------------------------------------------------

function calendarDate(calendar) {
    const d = calendar.get_date();
    return GLib.DateTime.new_local(d.get_year(), d.get_month(), d.get_day_of_month(), 0, 0, 0);
}

function calendarBody(item) {
    const c = item.calendar;
    const calendar = new Gtk.Calendar({halign: Gtk.Align.START});
    const date = GLib.DateTime.new_local(c.year, c.month, c.day, 0, 0, 0);
    if (date)
        calendar.select_day(date);
    return {
        widget: calendar,
        focus: calendar,
        values: a => {
            const text = formatDate(calendarDate(calendar), c.format, item.locale);
            if (text !== null)
                a.text = text;
        },
        state: () => ({date: calendarDate(calendar).format('%Y-%m-%d')}),
        fill: v => {
            const [y, m, d] = String(v).split('-').map(Number);
            calendar.select_day(GLib.DateTime.new_local(y, m, d, 0, 0, 0));
        },
    };
}

// ---- forms --------------------------------------------------------------

function formsBody(item) {
    const grid = new Gtk.Grid({column_spacing: 12, row_spacing: 8, hexpand: true});
    let expands = false;
    const fields = item.forms.fields.map((f, i) => {
        const label = new Gtk.Label({xalign: 0, yalign: 0, use_markup: false, use_underline: false});
        label.set_text(f.label);
        grid.attach(label, 0, i, 1, 1);
        let widget, focus, value, clear = () => {}, state, fill;
        switch (f.kind) {
        case 'entry':
        case 'password': {
            const entry = f.kind === 'password'
                ? new Gtk.PasswordEntry({show_peek_icon: true, hexpand: true})
                : new Gtk.Entry({hexpand: true});
            widget = focus = entry;
            value = () => ({text: entry.get_text()});
            clear = () => entry.set_text('');
            state = () => (f.kind === 'password' ? {} : {text: entry.get_text()});
            fill = v => entry.set_text(String(v));
            break;
        }
        case 'multiline': {
            const view = new Gtk.TextView({wrap_mode: Gtk.WrapMode.WORD, accepts_tab: false,
                top_margin: 6, bottom_margin: 6, left_margin: 6, right_margin: 6});
            widget = new Gtk.ScrolledWindow({child: view, vexpand: true, hexpand: true,
                min_content_height: 80, css_classes: ['card']});
            expands = true;
            focus = view;
            const text = () => view.buffer.get_text(view.buffer.get_start_iter(), view.buffer.get_end_iter(), false);
            value = () => ({text: text()});
            clear = () => view.buffer.set_text('', -1);
            state = () => ({text: text()});
            fill = v => view.buffer.set_text(String(v), -1);
            break;
        }
        case 'calendar': {
            const calendar = new Gtk.Calendar({halign: Gtk.Align.START});
            widget = focus = calendar;
            value = () => {
                const text = formatDate(calendarDate(calendar), item.forms.dateFormat, item.locale);
                return text === null ? {} : {text};
            };
            state = () => ({date: calendarDate(calendar).format('%Y-%m-%d')});
            fill = v => {
                const [y, m, d] = String(v).split('-').map(Number);
                calendar.select_day(GLib.DateTime.new_local(y, m, d, 0, 0, 0));
            };
            break;
        }
        case 'list': {
            const table = makeTable({columns: f.columns, header: f.showHeader, autoselect: false});
            table.add(f.rows);
            widget = new Gtk.ScrolledWindow({child: table.view, hexpand: true, vexpand: true,
                min_content_height: 100, css_classes: ['card']});
            expands = true;
            focus = table.view;
            value = () => ({rows: table.chosen()});
            clear = () => table.clear();
            state = table.state;
            fill = v => table.fill(v);
            break;
        }
        case 'combo': {
            const values = f.values ?? [];
            const dropdown = Gtk.DropDown.new_from_strings(values);
            dropdown.sensitive = values.length > 0;
            dropdown.hexpand = true;
            widget = focus = dropdown;
            value = () => {
                const picked = dropdown.get_selected_item();
                return picked ? {text: picked.get_string()} : {};
            };
            state = () => ({text: dropdown.get_selected_item()?.get_string() ?? null});
            fill = v => dropdown.set_selected(Number(v));
            break;
        }
        }
        grid.attach(widget, 1, i, 1, 1);
        // The label names its field, for the accessibility bus when it is on.
        label.set_mnemonic_widget(widget);
        return {focus, value, clear, state, fill};
    });
    const scroller = new Gtk.ScrolledWindow({child: grid, vexpand: true,
        hscrollbar_policy: Gtk.PolicyType.NEVER, propagate_natural_height: true});
    return {
        widget: scroller,
        expands: true,
        scroller,
        focus: fields[0]?.focus ?? null,
        values: a => {
            a.fields = fields.map(f => f.value());
        },
        clear: () => fields.forEach(f => f.clear()),
        state: () => ({fields: fields.map(f => f.state())}),
        fill: v => {
            for (const [i, value] of Object.entries(v))
                fields[Number(i)].fill(value);
        },
    };
}

// ---- scale --------------------------------------------------------------

function scaleBody(item, ctx) {
    const s = item.scale;
    const adjustment = new Gtk.Adjustment({lower: s.min, upper: s.max, value: s.value,
        step_increment: s.step, page_increment: s.step});
    const scale = new Gtk.Scale({orientation: Gtk.Orientation.HORIZONTAL, adjustment,
        digits: 0, draw_value: !s.hide, hexpand: true});
    scale.set_round_digits(0);
    const value = () => Math.round(adjustment.value);
    if (s.partial) {
        // --print-partial: each value as it moves, printed by the client
        // as zenity prints it, before the answer.
        adjustment.connect('value-changed', () => ctx.tell({partial: value()}));
    }
    return {
        widget: scale,
        focus: scale,
        values: a => {
            a.value = value();
        },
        state: () => ({value: value()}),
        fill: v => {
            adjustment.value = Number(v);
        },
    };
}

// ---- password -----------------------------------------------------------

function passwordBody(item) {
    const grid = new Gtk.Grid({column_spacing: 12, row_spacing: 8, hexpand: true});
    let username = null;
    let row = 0;
    if (item.password.username) {
        grid.attach(new Gtk.Label({label: 'Username:', xalign: 0}), 0, row, 1, 1);
        username = new Gtk.Entry({hexpand: true});
        grid.attach(username, 1, row++, 1, 1);
    }
    grid.attach(new Gtk.Label({label: 'Password:', xalign: 0}), 0, row, 1, 1);
    const password = new Gtk.PasswordEntry({show_peek_icon: true, hexpand: true});
    grid.attach(password, 1, row, 1, 1);
    return {
        widget: grid,
        focus: username ?? password,
        values: a => {
            if (username)
                a.username = username.get_text();
            a.entry = password.get_text();
        },
        clear: () => {
            username?.set_text('');
            password.set_text('');
        },
        state: () => ({username: username?.get_text()}),
        fill: v => {
            if (typeof v.username === 'string')
                username?.set_text(v.username);
            if (typeof v.password === 'string')
                password.set_text(v.password);
        },
    };
}

// ---- colour -------------------------------------------------------------

function colorBody(item, ctx) {
    const chooser = new Gtk.ColorChooserWidget({show_editor: !item.color.palette, use_alpha: true});
    if (item.color.color) {
        const rgba = new Gdk.RGBA();
        if (rgba.parse(item.color.color))
            chooser.set_rgba(rgba);
    }
    // A colour activated -- double-clicked -- is chosen, as in zenity's.
    chooser.connect('color-activated', () => ctx.press('ok'));
    return {
        widget: chooser,
        expands: true,
        // Not the chooser: its first focusable child is the editor's text
        // field, which would take the letters that are the buttons' keys.
        // Tab or a click reaches it.
        values: a => {
            a.text = chooser.get_rgba().to_string();
        },
        state: () => ({color: chooser.get_rgba().to_string()}),
        fill: v => {
            const rgba = new Gdk.RGBA();
            if (rgba.parse(String(v)))
                chooser.set_rgba(rgba);
        },
    };
}

// ---- file chooser -------------------------------------------------------

// GTK's own file chooser, as the person asks for it: Gtk.FileDialog, which
// goes through the desktop's portal where there is one. Resolves to the
// chosen paths, or null when the chooser was dismissed.
export function chooseFiles(spec, parent, cancellable) {
    const dialog = new Gtk.FileDialog({title: spec.title, modal: true});
    if (spec.folder)
        dialog.initial_folder = Gio.File.new_for_path(spec.folder);
    if (spec.name)
        dialog.initial_name = spec.name;
    if (spec.filters.length) {
        const filters = new Gio.ListStore({item_type: Gtk.FileFilter.$gtype});
        for (const f of spec.filters) {
            const filter = new Gtk.FileFilter({name: f.name});
            for (const p of f.patterns)
                filter.add_pattern(p);
            filters.append(filter);
        }
        dialog.filters = filters;
        dialog.default_filter = filters.get_item(0);
    }
    const [start, finish] = {
        open: spec.multiple ? ['open_multiple', 'open_multiple_finish'] : ['open', 'open_finish'],
        save: ['save', 'save_finish'],
        directory: spec.multiple
            ? ['select_multiple_folders', 'select_multiple_folders_finish']
            : ['select_folder', 'select_folder_finish'],
    }[spec.mode];
    return new Promise(resolve => {
        dialog[start](parent, cancellable, (_d, result) => {
            let chosen;
            try {
                chosen = dialog[finish](result);
            } catch (e) {
                resolve(null);
                return;
            }
            const files = chosen instanceof Gio.File
                ? [chosen]
                : Array.from({length: chosen.get_n_items()}, (_, i) => chosen.get_item(i));
            // A file with no path -- one the chooser found somewhere other
            // than a disk -- is printed as C prints a NULL, as zenity does.
            resolve(files.map(f => f.get_path() ?? '(null)'));
        });
    });
}

function fileBody(item, ctx) {
    const f = item.file;
    const box = new Gtk.Box({orientation: Gtk.Orientation.VERTICAL, spacing: 6});
    const line = (heading, text) => {
        const label = new Gtk.Label({xalign: 0, wrap: true, wrap_mode: 2, selectable: false,
            css_classes: ['dim-label']});
        label.set_text(`${heading}: ${text}`);
        box.append(label);
    };
    if (f.folder)
        line('Starting in', f.folder);
    if (f.name)
        line('Suggested name', f.name);
    for (const filter of f.filters)
        line('Filter', `${filter.name} (${filter.patterns.join(' ')})`);
    let cancellable = null;
    let request = null;
    return {
        widget: box,
        // The action button opens the chooser rather than answering; what
        // is chosen there answers, and a chooser dismissed leaves the item
        // waiting, to be opened again.
        press: spec => {
            if (spec.answer !== 'ok')
                return false;
            if (cancellable)
                return true;
            cancellable = new Gio.Cancellable();
            request = {title: item.title, mode: f.mode, multiple: f.multiple,
                folder: f.folder, name: f.name, filters: f.filters};
            ctx.window.chooseFiles(request, ctx.window.window, cancellable).then(files => {
                cancellable = null;
                if (files && files.length)
                    ctx.finish({answer: 'ok', files});
            });
            return true;
        },
        values: () => {},
        clear: () => cancellable?.cancel(),
        state: () => ({choosing: cancellable !== null, request}),
    };
}

// ---- progress -----------------------------------------------------------

function progressBody(item, ctx) {
    const p = item.progress;
    const bar = new Gtk.ProgressBar({fraction: p.percentage / 100, hexpand: true});
    const remaining = new Gtk.Label({xalign: 0, css_classes: ['dim-label', 'caption']});
    const box = new Gtk.Box({orientation: Gtk.Orientation.VERTICAL, spacing: 6});
    box.append(bar);
    box.append(remaining);
    // The queue's row shows the bar too, small.
    const rowBar = new Gtk.ProgressBar({fraction: bar.fraction, css_classes: ['osd'], margin_top: 2});
    let percentage = p.percentage;
    let pulsing = 0;
    const pulse = on => {
        if (on && !pulsing) {
            pulsing = GLib.timeout_add(GLib.PRIORITY_DEFAULT, 100, () => {
                bar.pulse();
                rowBar.pulse();
                return GLib.SOURCE_CONTINUE;
            });
        } else if (!on && pulsing) {
            GLib.source_remove(pulsing);
            pulsing = 0;
        }
    };
    const fraction = f => {
        bar.fraction = f;
        rowBar.fraction = f;
    };
    pulse(p.pulsate);
    return {
        widget: box,
        rowWidget: rowBar,
        values: () => {},
        clear: () => pulse(false),
        follow: {
            progress: u => {
                if (u.percentage !== undefined) {
                    percentage = u.percentage;
                    fraction(percentage / 100);
                }
                if (u.text !== undefined)
                    ctx.setText(u.text, true);
                if (u.pulsate !== undefined)
                    pulse(u.pulsate);
                if (u.remaining !== undefined)
                    remaining.set_text(u.remaining);
                // stdin closed: Cancel no longer offered, the bar full.
                if (u.closed)
                    ctx.setButton('cancel', {enabled: false});
                if (u.finished)
                    ctx.setButton('ok', {enabled: true, isDefault: true});
            },
        },
        state: () => ({percentage, fraction: bar.fraction, pulsing: pulsing !== 0, remaining: remaining.get_text()}),
    };
}
