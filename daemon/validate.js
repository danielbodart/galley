// What an item may hold, checked before anything of it is shown.
//
// The client is this user's own program, but whatever runs as this user can
// write to the socket, so an item is read the way any input is: each field
// its expected type and within a size, everything else dropped. What comes
// back is a fresh object holding only the fields the window reads.
//
// The client fits zenity's command line to these limits before it sends --
// sizes clamped, titles and labels cut short, more than 16 buttons refused
// (internal/zenity/args.go) -- so only an item written by hand meets them.

// The kinds each protocol version knows (internal/wire).
const KINDS_1 = ['question', 'info', 'warning', 'error', 'entry', 'text'];
const KINDS_2 = [...KINDS_1, 'list', 'forms', 'calendar', 'scale', 'password', 'color',
    'file', 'progress', 'notification', 'about'];
const KINDS = {1: new Set(KINDS_1), 2: new Set(KINDS_2)};
const ANSWERS = new Set(['ok', 'cancel', 'extra', 'close']);
const FIELDS = new Set(['entry', 'password', 'multiline', 'calendar', 'list', 'combo']);

// How much a list may hold: zenity has no limit, and a window is not the
// place for more.
export const MAX_ROWS = 100000;
const MAX_COLUMNS = 64;
const MAX_CELL = 1 << 16;

function string(value, name, max, fallback = '') {
    if (value === undefined || value === null)
        return fallback;
    if (typeof value !== 'string')
        throw new Error(`${name} is not a string`);
    if (value.length > max)
        throw new Error(`${name} is longer than ${max}`);
    return value;
}

function int(value, name, min, max, fallback = 0) {
    if (value === undefined || value === null)
        return fallback;
    if (!Number.isInteger(value) || value < min || value > max)
        throw new Error(`${name} is not a whole number from ${min} to ${max}`);
    return value;
}

function number(value, name, min, max, fallback = 0) {
    if (value === undefined || value === null)
        return fallback;
    if (typeof value !== 'number' || !Number.isFinite(value) || value < min || value > max)
        throw new Error(`${name} is not a number from ${min} to ${max}`);
    return value;
}

function bool(value) {
    return value === true;
}

function list(value, name, max, each) {
    if (value === undefined || value === null)
        return [];
    if (!Array.isArray(value) || value.length > max)
        throw new Error(`${name} is not a list of at most ${max}`);
    return value.map((v, i) => each(v, `${name}[${i}]`));
}

const strings = (value, name, max, length) => list(value, name, max, (v, n) => string(v, n, length));

// Rows of cells, each row at most columns long.
export function rows(value, name, columns, max = MAX_ROWS) {
    return list(value, name, max, (row, n) => strings(row, n, Math.max(columns, 1), MAX_CELL));
}

export function validate(raw, version = 1) {
    if (typeof raw !== 'object' || raw === null)
        throw new Error('no item');
    if (!KINDS[version]?.has(raw.kind))
        throw new Error(`kind ${JSON.stringify(raw.kind)} is not one galley shows`);
    if (!Array.isArray(raw.buttons) || raw.buttons.length > 16)
        throw new Error('buttons is not a list of at most 16');

    const keys = new Set();
    const buttons = raw.buttons.map((b, i) => {
        if (typeof b !== 'object' || b === null || !ANSWERS.has(b.answer))
            throw new Error(`button ${i} has no answer galley knows`);
        const key = string(b.key, 'key', 1);
        if (key && (!/^[a-z0-9]$/.test(key) || key === 'j' || key === 'k' || keys.has(key)))
            throw new Error(`button ${i}'s key ${JSON.stringify(key)} cannot be used`);
        if (key)
            keys.add(key);
        const label = string(b.label, 'label', 256);
        return {
            answer: b.answer,
            index: b.answer === 'extra' ? int(b.index, 'index', 0, 15) : 0,
            label,
            key,
            underline: int(b.underline, 'underline', -1, [...label].length - 1, -1),
            disabled: version >= 2 && bool(b.disabled),
        };
    });

    const item = {
        kind: raw.kind,
        title: string(raw.title, 'title', 1024),
        text: string(raw.text, 'text', 1 << 20),
        markup: bool(raw.markup),
        icon: string(raw.icon, 'icon', 4096),
        width: int(raw.width, 'width', -1, 100000),
        height: int(raw.height, 'height', -1, 100000),
        noWrap: bool(raw.noWrap),
        ellipsize: bool(raw.ellipsize),
        buttons,
        default: int(raw.default, 'default', -1, buttons.length - 1, -1),
        locale: version >= 2 ? string(raw.locale, 'locale', 256) : '',
    };
    if (item.locale && !/^[A-Za-z0-9_.@=-]*$/.test(item.locale))
        throw new Error('locale is not a locale name');
    const part = name => raw[name] ?? {};

    switch (item.kind) {
    case 'entry': {
        const entry = part('entry');
        item.entry = {
            text: string(entry.text, 'entry text', 1 << 16),
            hidden: bool(entry.hidden),
        };
        // A combo's values: a combo's text is never hidden.
        if (version >= 2 && entry.values !== undefined && entry.values !== null) {
            item.entry.values = strings(entry.values, 'entry values', MAX_ROWS, MAX_CELL);
            item.entry.hidden = false;
        }
        break;
    }
    case 'text': {
        const info = part('info');
        item.info = {
            text: string(info.text, 'info text', 16 << 20),
            more: bool(info.more),
            checkbox: string(info.checkbox, 'checkbox', 1024),
            autoScroll: bool(info.autoScroll),
            editable: version >= 2 && bool(info.editable),
        };
        break;
    }
    case 'list': {
        const l = part('list');
        const columns = strings(l.columns, 'columns', MAX_COLUMNS, 1024);
        if (columns.length === 0)
            throw new Error('a list has no columns');
        const type = string(l.type, 'list type', 8);
        if (!['', 'check', 'radio', 'image'].includes(type))
            throw new Error(`list type ${JSON.stringify(type)} is not one galley shows`);
        if ((type === 'check' || type === 'radio') && columns.length < 2)
            throw new Error('a check or radio list needs two columns');
        item.list = {
            columns,
            rows: rows(l.rows, 'rows', columns.length),
            more: bool(l.more),
            type,
            multiple: bool(l.multiple),
            editable: bool(l.editable),
            hideHeader: bool(l.hideHeader),
            hidden: list(l.hidden, 'hidden', MAX_COLUMNS, (v, n) => int(v, n, 0, columns.length - 1)),
        };
        break;
    }
    case 'forms': {
        const f = part('forms');
        item.forms = {
            dateFormat: string(f.dateFormat, 'date format', 1024, '%x'),
            fields: list(f.fields, 'fields', 64, (field, n) => {
                if (typeof field !== 'object' || field === null || !FIELDS.has(field.kind))
                    throw new Error(`${n} is not a field galley shows`);
                const columns = strings(field.columns, `${n} columns`, MAX_COLUMNS, 1024);
                return {
                    kind: field.kind,
                    label: string(field.label, `${n} label`, 1024),
                    columns: columns.length ? columns : ['column'],
                    rows: rows(field.rows, `${n} rows`, Math.max(columns.length, 1)),
                    showHeader: bool(field.showHeader),
                    values: field.values === undefined || field.values === null
                        ? null : strings(field.values, `${n} values`, MAX_ROWS, MAX_CELL),
                };
            }),
        };
        break;
    }
    case 'calendar': {
        const c = part('calendar');
        item.calendar = {
            year: int(c.year, 'year', 1, 9999, 1),
            month: int(c.month, 'month', 1, 12, 1),
            day: int(c.day, 'day', 1, 31, 1),
            format: string(c.format, 'date format', 1024, '%x'),
        };
        break;
    }
    case 'scale': {
        const s = part('scale');
        const range = 2 ** 31;
        item.scale = {
            min: int(s.min, 'min', -range, range - 1),
            max: int(s.max, 'max', -range, range - 1, 100),
            value: int(s.value, 'value', -range, range - 1),
            step: int(s.step, 'step', -range, range - 1, 1),
            hide: bool(s.hide),
            partial: bool(s.partial),
        };
        if (item.scale.min >= item.scale.max ||
            item.scale.value < item.scale.min || item.scale.value > item.scale.max)
            throw new Error('the scale\'s value is not within its range');
        break;
    }
    case 'password':
        item.password = {username: bool(part('password').username)};
        break;
    case 'color': {
        const c = part('color');
        item.color = {color: string(c.color, 'color', 256), palette: bool(c.palette)};
        break;
    }
    case 'file': {
        const f = part('file');
        const mode = string(f.mode, 'mode', 16, 'open');
        if (!['open', 'save', 'directory'].includes(mode))
            throw new Error(`file mode ${JSON.stringify(mode)} is not one galley shows`);
        const folder = string(f.folder, 'folder', 4096);
        if (folder && !folder.startsWith('/'))
            throw new Error('folder is not an absolute path');
        item.file = {
            mode,
            multiple: bool(f.multiple),
            folder,
            name: string(f.name, 'name', 4096),
            filters: list(f.filters, 'filters', 64, (filter, n) => ({
                name: string(filter?.name, `${n} name`, 1024),
                patterns: strings(filter?.patterns, `${n} patterns`, 256, 1024),
            })),
        };
        break;
    }
    case 'progress': {
        const p = part('progress');
        item.progress = {
            percentage: number(p.percentage, 'percentage', 0, 100),
            pulsate: bool(p.pulsate),
            timeRemaining: bool(p.timeRemaining),
        };
        break;
    }
    case 'notification':
        item.note = {listen: bool(part('note').listen)};
        break;
    }
    return item;
}

// A progress item's update, read as an item is.
export function progressUpdate(raw) {
    if (typeof raw !== 'object' || raw === null)
        return null;
    const update = {finished: bool(raw.finished), closed: bool(raw.closed)};
    if (raw.percentage !== undefined)
        update.percentage = number(raw.percentage, 'percentage', 0, 100);
    if (raw.text !== undefined)
        update.text = string(raw.text, 'progress text', 1 << 16);
    if (raw.pulsate !== undefined)
        update.pulsate = bool(raw.pulsate);
    if (raw.remaining !== undefined)
        update.remaining = string(raw.remaining, 'remaining', 256);
    return update;
}

// A listening notification's message, read as an item is.
export function notify(raw) {
    if (typeof raw !== 'object' || raw === null)
        return null;
    return {text: string(raw.text, 'message', 1 << 16), icon: string(raw.icon, 'icon', 4096)};
}
