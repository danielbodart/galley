// What an item's icon looks like in the queue and over its detail: the same
// icon on both sides, and a colour per icon, so that kinds of question are
// told apart at a glance -- sudo's key from a grant's diff from a request.
//
// The colour follows the icon's name, which is the caller's to choose with
// --icon: a caller that wants its questions told apart gives each kind its
// own icon, and the colour comes with it. Names galley's callers use have a
// colour picked for them; any other name is given one of the same colours by
// a hash of the name, so it is at least always the same one.

// The colours, from libadwaita's palette: a darker shade on a light
// background and a lighter one on a dark background, so each keeps its
// contrast with the sidebar and the page.
const shades = {
    //        light         dark
    blue: ['--blue-4', '--blue-2'],
    teal: ['#1c8a96', '#5bc8af'],
    green: ['--green-5', '--green-2'],
    yellow: ['#a17204', '--yellow-2'],
    orange: ['--orange-5', '--orange-2'],
    red: ['--red-4', '--red-1'],
    purple: ['--purple-3', '--purple-1'],
    slate: ['#5c6f80', '#a5b6c6'],
};

const named = {
    // Every password prompt: --password, --entry --hide-text (sudo's
    // askpass), a --forms with an --add-password.
    'dialog-password': 'yellow',
    // A grant's diff: chase's approver, a --text-info.
    'accessories-text-editor': 'purple',
    // A request, a command or a connection: frisket's asker.
    'security-medium': 'blue',
    // A recording's question: frisket's asker under `chase record`.
    'media-tape': 'red',
    // An entry whose text is shown.
    'text-editor': 'green',
    // Any other question, and zenity's messages.
    'dialog-question': 'teal',
    'dialog-warning': 'orange',
    'dialog-error': 'red',
    'dialog-information': 'slate',
    // zenity's other dialogs, by galley's icon for each (args.go).
    'document-edit': 'green',
    'view-list': 'teal',
    'x-office-calendar': 'orange',
    'applications-graphics': 'purple',
    'document-open': 'blue',
    'document-save-as': 'blue',
    'folder-open': 'blue',
    'appointment-soon': 'slate',
    'help-about': 'slate',
};

const order = Object.keys(shades);

const base = name => name.replace(/-symbolic$/, '');

// The CSS class that colours an icon of this name, or null for one that is a
// file: a picture has colours of its own.
export function tint(name) {
    if (!name || name.startsWith('/'))
        return null;
    const b = base(name);
    let colour = named[b];
    if (!colour) {
        let h = 0;
        for (const c of b)
            h = (h * 31 + c.codePointAt(0)) >>> 0;
        colour = order[h % order.length];
    }
    return `tint-${colour}`;
}

// The symbolic icon to show for this name in the queue, when the theme has
// one -- `has` asks it -- else null.
export function symbolicName(name, has) {
    if (!name || name.startsWith('/'))
        return null;
    const want = `${base(name)}-symbolic`;
    return has(want) ? want : null;
}

const colour = v => (v.startsWith('--') ? `var(${v})` : v);

// The rules that colour the icons, in the shades for a dark style or a light
// one: the window loads them again when the style changes (window.js).
export const css = dark => Object.entries(shades)
    .map(([name, pair]) => `image.tint-${name} { color: ${colour(pair[dark ? 1 : 0])}; }`)
    .join('\n');
