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

const KINDS = new Set(['question', 'info', 'warning', 'error', 'entry', 'text']);
const ANSWERS = new Set(['ok', 'cancel', 'extra', 'close']);

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

function bool(value) {
    return value === true;
}

export function validate(raw) {
    if (typeof raw !== 'object' || raw === null)
        throw new Error('no item');
    if (!KINDS.has(raw.kind))
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
    };
    if (item.kind === 'entry') {
        const entry = raw.entry ?? {};
        item.entry = {text: string(entry.text, 'entry text', 1 << 16), hidden: bool(entry.hidden)};
    }
    if (item.kind === 'text') {
        const info = raw.info ?? {};
        item.info = {
            text: string(info.text, 'info text', 16 << 20),
            more: bool(info.more),
            checkbox: string(info.checkbox, 'checkbox', 1024),
            autoScroll: bool(info.autoScroll),
        };
    }
    return item;
}
