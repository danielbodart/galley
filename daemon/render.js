// How caller text becomes what a label shows.
//
// Everything a caller sends is plain text unless its item says markup, which
// zenity's message dialogs do unless given --no-markup. Even then the markup
// is never handed to a label as markup: Pango parses it into text and
// attributes, and only the attributes that change emphasis are kept. A
// colour could paint part of a command in the background's colour, a size
// could push it out of sight, a font could draw one character as another,
// and a GtkLabel's <a href> would make a link; none of them survives. Markup
// that does not parse is shown as the text it is.

import Pango from 'gi://Pango';

// Pango's own serialisation of an attribute list is a line each, "start end
// type value", and the one way GJS can tell an attribute's type; these are
// the lines kept. <tt>'s monospace is the only family that survives.
const kept = /^\d+ \d+ (?:(?:style|weight|variant|underline|strikethrough) [\w-]+|family "Monospace")$/;

// Returns {text, attributes} for a label: attributes is a Pango.AttrList or
// null.
export function render(text, markup) {
    if (!markup)
        return {text, attributes: null};
    let parsed;
    try {
        parsed = Pango.parse_markup(text, -1, '\0');
    } catch (e) {
        return {text, attributes: null};
    }
    const [, list, plain] = parsed;
    const lines = (list?.to_string() ?? '').split('\n').filter(l => kept.test(l));
    const attributes = lines.length ? Pango.AttrList.from_string(lines.join('\n')) : null;
    return {text: plain, attributes};
}

// The first n lines of a text that are not blank, trimmed, for the queue's
// list.
export function lines(text, n) {
    return text.split('\n').map(l => l.trim()).filter(l => l !== '').slice(0, n);
}

// A button's label with its key's character underlined, as markup built
// here from escaped text, so the label is the caller's only as text.
export function buttonMarkup(GLib, label, underline) {
    const chars = [...label];
    const escape = s => GLib.markup_escape_text(s, -1);
    if (underline < 0 || underline >= chars.length)
        return escape(label);
    return escape(chars.slice(0, underline).join('')) +
        `<u>${escape(chars[underline])}</u>` +
        escape(chars.slice(underline + 1).join(''));
}
