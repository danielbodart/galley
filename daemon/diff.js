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

// ---- a diff worked out from a file's two versions -------------------------

// The lines of context a hunk keeps around what changed, as `diff -u`.
const CONTEXT = 3;

// How much work the search for the fewest changes may do, in steps along
// the two files, before it settles for every remaining line removed and
// then added: still the change, only not the shortest way to show it.
const BUDGET = 4e6;

// A unified diff of old to new, both whole files' text, headed by path:
// {text, spans, added, removed}, spans as diffSpans gives them, found from
// the lines as they were made rather than read back from the text, so a
// line that happens to look like a header is never taken for one. A last
// line with no newline is marked as `diff -u` marks it.
export function unifiedDiff(path, old, now) {
    const a = splitLines(old), b = splitLines(now);
    const edits = editScript(a, b);
    const out = [];
    const push = (kind, line) => out.push({kind, line});
    push('file', `--- ${path}`);
    push('file', `+++ ${path}`);
    let added = 0, removed = 0;
    for (const hunk of hunks(edits)) {
        const [oldStart, oldCount, newStart, newCount] = hunk.range;
        push('hunk', `@@ -${spot(oldStart, oldCount)} +${spot(newStart, newCount)} @@`);
        for (const e of hunk.edits) {
            const token = e.op === '+' ? b[e.j] : a[e.i];
            const kind = {'+': 'add', '-': 'del', ' ': 'same'}[e.op];
            if (e.op === '+')
                added++;
            else if (e.op === '-')
                removed++;
            push(kind, e.op + token.replace(/\n$/, ''));
            if (!token.endsWith('\n'))
                push('note', '\\ No newline at end of file');
        }
    }
    if (added === 0 && removed === 0)
        push('note', 'No change.');

    const lines = out.map(o => o.line);
    const text = lines.join('\n');
    const spans = [];
    let at = 0;
    const starts = lines.map(line => {
        const s = at;
        at += length(line) + 1;
        return s;
    });
    const span = (kind, k, from = 0, to = length(lines[k])) =>
        spans.push({kind, start: starts[k] + from, end: starts[k] + to});
    let dels = [], adds = [];
    const pair = () => {
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
    out.forEach(({kind}, k) => {
        if (kind === 'del') {
            if (adds.length)
                pair();
            dels.push(k);
        } else if (kind === 'add') {
            adds.push(k);
        } else if (kind !== 'note') {
            pair();
        }
        if (kind !== 'same' && kind !== 'note')
            span(kind, k);
    });
    pair();
    return {text, spans, added, removed};
}

// A file's lines, each with its newline; the last without, when the file
// does not end in one, so that a newline added or taken away is a change.
function splitLines(s) {
    return s.match(/[^\n]*\n|[^\n]+$/g) ?? [];
}

// A hunk's range, as `diff -u` writes it, from the lines before it and how
// many it has: its first line's number and the count, the count left out
// when it is one, and an empty range numbered by the line before it.
function spot(start, count) {
    if (count === 0)
        return `${start},0`;
    return count === 1 ? `${start + 1}` : `${start + 1},${count}`;
}

// The edits that make a into b, in order: {op, i, j}, op '-' removes a[i],
// '+' adds b[j] and ' ' keeps a[i], which is b[j]. Myers' search for the
// fewest, after the lines the two share at their start and end are set
// aside; past its budget, what is left of the middle is removed and added.
export function editScript(a, b) {
    let start = 0;
    while (start < a.length && start < b.length && a[start] === b[start])
        start++;
    let endA = a.length, endB = b.length;
    while (endA > start && endB > start && a[endA - 1] === b[endB - 1]) {
        endA--;
        endB--;
    }
    const edits = [];
    for (let k = 0; k < start; k++)
        edits.push({op: ' ', i: k, j: k});
    edits.push(...middle(a, b, start, endA, start, endB));
    for (let i = endA, j = endB; i < a.length; i++, j++)
        edits.push({op: ' ', i, j});
    return edits;
}

function middle(a, b, a0, a1, b0, b1) {
    const n = a1 - a0, m = b1 - b0;
    const replace = () => [
        ...Array.from({length: n}, (_, k) => ({op: '-', i: a0 + k})),
        ...Array.from({length: m}, (_, k) => ({op: '+', j: b0 + k})),
    ];
    if (n === 0 || m === 0)
        return replace();
    const max = n + m;
    const offset = max;
    const v = new Int32Array(2 * max + 2);
    const trace = [];
    let work = 0;
    for (let d = 0; d <= max; d++) {
        trace.push(v.slice(offset - d, offset + d + 2));
        for (let k = -d; k <= d; k += 2) {
            let x = k === -d || (k !== d && v[offset + k - 1] < v[offset + k + 1])
                ? v[offset + k + 1]
                : v[offset + k - 1] + 1;
            let y = x - k;
            while (x < n && y < m && a[a0 + x] === b[b0 + y]) {
                x++;
                y++;
            }
            v[offset + k] = x;
            if (x >= n && y >= m)
                return backtrack(trace, d, n, m, a0, b0);
        }
        work += 2 * d + 1 + (n + m);
        if (work > BUDGET)
            return replace();
    }
    return replace();
}

// Walks the search's rounds back from (n, m) to the start.
function backtrack(trace, dEnd, n, m, a0, b0) {
    const edits = [];
    let x = n, y = m;
    for (let d = dEnd; d > 0; d--) {
        const v = trace[d];
        // trace[d] holds v[offset - d .. offset + d + 1], taken before round
        // d ran: what round d - 1 left.
        const at = k => v[k + d];
        const k = x - y;
        const prevK = k === -d || (k !== d && at(k - 1) < at(k + 1)) ? k + 1 : k - 1;
        const prevX = at(prevK);
        const prevY = prevX - prevK;
        while (x > prevX && y > prevY) {
            x--;
            y--;
            edits.push({op: ' ', i: a0 + x, j: b0 + y});
        }
        if (x === prevX)
            edits.push({op: '+', j: b0 + --y});
        else
            edits.push({op: '-', i: a0 + --x});
    }
    while (x > 0 && y > 0) {
        x--;
        y--;
        edits.push({op: ' ', i: a0 + x, j: b0 + y});
    }
    return edits.reverse();
}

// The edits grouped into hunks: each change with CONTEXT kept lines either
// side, hunks whose context would meet made one. range is [old start, old
// count, new start, new count], starts counted from 0.
function hunks(edits) {
    const out = [];
    let i = 0;
    while (i < edits.length) {
        while (i < edits.length && edits[i].op === ' ')
            i++;
        if (i >= edits.length)
            break;
        // Hunks never meet: the next change is more than twice CONTEXT on.
        const from = Math.max(0, i - CONTEXT);
        let to = i;
        // Out to the last change whose gap to the next is short enough.
        for (;;) {
            while (to < edits.length && edits[to].op !== ' ')
                to++;
            let gap = to;
            while (gap < edits.length && edits[gap].op === ' ')
                gap++;
            if (gap < edits.length && gap - to <= 2 * CONTEXT) {
                to = gap;
                continue;
            }
            to = Math.min(edits.length, to + CONTEXT);
            break;
        }
        const part = edits.slice(from, to);
        // The lines of each file before the hunk.
        let oldStart = 0, newStart = 0;
        for (let k = 0; k < from; k++) {
            if (edits[k].op !== '+')
                oldStart++;
            if (edits[k].op !== '-')
                newStart++;
        }
        const oldCount = part.filter(e => e.op !== '+').length;
        const newCount = part.filter(e => e.op !== '-').length;
        out.push({
            range: [oldStart, oldCount, newStart, newCount],
            edits: part,
        });
        i = to;
    }
    return out;
}
