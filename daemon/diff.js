// A unified diff in a --text-info, shown as one: added and removed lines on
// green and red, the part of a changed line that changed stronger still,
// hunk and file headers set apart. The text is the caller's, unchanged --
// what is shown, copied and printed is the same -- only coloured, so a
// zenity caller that sends a diff gets it read as one without asking: chase's
// grant approver sends `diff -u`'s output under a line or two of its own.
//
// Nothing here touches GTK: it finds the spans, and bodies.js tags them.

// A unified diff's file headers and first hunk, one after another: what
// `diff -u` and `git diff` print, and what prose does not.
const HEADER = /^--- [^\n]*\n\+\+\+ [^\n]*\n@@ -\d+(?:,\d+)? \+\d+(?:,\d+)? @@/m;

// Past this, the text is shown as it is: colouring is for reading a change,
// and a diff this long is not read in a dialog.
export const MAX_DIFF = 512 * 1024;

export function isDiff(text) {
    return text.length <= MAX_DIFF && HEADER.test(text);
}

// The spans to colour, in characters (code points, as a GtkTextBuffer counts
// offsets): {kind, start, end}, kind one of file, hunk, add, del, addWord,
// delWord. What comes before the first file header, and anything between
// hunks that is not part of one, is left as it is.
export function diffSpans(text) {
    const spans = [];
    const lines = text.split('\n');
    const starts = [];
    let at = 0;
    for (const line of lines) {
        starts.push(at);
        at += length(line) + 1;
    }
    const span = (kind, i, from = 0, to = length(lines[i])) =>
        spans.push({kind, start: starts[i] + from, end: starts[i] + to});

    let inHunk = false;
    let dels = [], adds = [];
    const pair = () => {
        // Each removed line with the added line in its place, when a run of
        // one is followed by a run of the other: what changed between them
        // is the middle that their common start and end leave.
        const n = Math.min(dels.length, adds.length);
        for (let k = 0; k < n; k++) {
            const [from, toDel, toAdd] = changed(lines[dels[k]], lines[adds[k]]);
            if (toDel > from)
                span('delWord', dels[k], from, toDel);
            if (toAdd > from)
                span('addWord', adds[k], from, toAdd);
        }
        dels = [];
        adds = [];
    };

    for (let i = 0; i < lines.length; i++) {
        const line = lines[i];
        if (line.startsWith('--- ') && lines[i + 1]?.startsWith('+++ ') && /^@@ /.test(lines[i + 2] ?? '')) {
            pair();
            span('file', i);
            span('file', i + 1);
            i++;
            inHunk = false;
        } else if (/^@@ -\d+(?:,\d+)? \+\d+(?:,\d+)? @@/.test(line)) {
            pair();
            span('hunk', i);
            inHunk = true;
        } else if (!inHunk) {
            continue;
        } else if (line.startsWith('-')) {
            if (adds.length)
                pair();
            span('del', i);
            dels.push(i);
        } else if (line.startsWith('+')) {
            span('add', i);
            adds.push(i);
        } else if (line.startsWith(' ') || line.startsWith('\\') || line === '') {
            pair();
        } else {
            // Not a hunk's line: `diff --git`, `index`, or prose after it.
            pair();
            inHunk = false;
        }
    }
    pair();
    return spans;
}

const length = s => {
    let n = 0;
    for (const _ of s)
        n++;
    return n;
};

// Where two lines, their - and + dropped, stop being the same and start
// being the same again: [from, end in a, end in b], in characters of the
// lines as they are, marks included.
function changed(a, b) {
    const x = Array.from(a.slice(1)), y = Array.from(b.slice(1));
    let p = 0;
    while (p < x.length && p < y.length && x[p] === y[p])
        p++;
    let s = 0;
    while (s < x.length - p && s < y.length - p && x[x.length - 1 - s] === y[y.length - 1 - s])
        s++;
    // Nothing in common at all says nothing about which part changed.
    if (p === 0 && s === 0)
        return [0, 0, 0];
    return [p + 1, x.length - s + 1, y.length - s + 1];
}
