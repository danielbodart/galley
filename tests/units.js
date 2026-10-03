// The window's logic that needs no display -- the queue's order, the
// sanitising of markup, the checking of an item -- under plain gjs:
//
//   gjs -m tests/units.js
//
// Exits non-zero, having said which, when any fails.

import GLib from 'gi://GLib';
import System from 'system';

import {Queue, waited} from '../daemon/queue.js';
import {render, lines, buttonMarkup} from '../daemon/render.js';
import {validate, rows, progressUpdate, notify} from '../daemon/validate.js';
import {formatDate} from '../daemon/dates.js';

let failed = 0;
function check(name, got, want) {
    const g = JSON.stringify(got), w = JSON.stringify(want);
    if (g !== w) {
        printerr(`FAIL ${name}: got ${g}, want ${w}`);
        failed++;
    }
}
function throws(name, f, pattern) {
    try {
        f();
    } catch (e) {
        if (!pattern.test(e.message)) {
            printerr(`FAIL ${name}: threw ${e.message}`);
            failed++;
        }
        return;
    }
    printerr(`FAIL ${name}: did not throw`);
    failed++;
}

// ---- the queue -----------------------------------------------------------

{
    const q = new Queue();
    const item = (name, group) => ({name, group});
    const names = () => q.items.map(i => i.name);
    check('first', q.add(item('a1', 'A')), 0);
    check('another group goes last', q.add(item('b1', 'B')), 1);
    check('a group keeps together', q.add(item('a2', 'A')), 1);
    q.add(item('c1', 'C'));
    q.add(item('b2', 'B'));
    check('order', names(), ['a1', 'a2', 'b1', 'b2', 'c1']);

    check('remove', q.remove(q.at(1)), 1);
    check('after removal, the one that took its place', q.afterRemoval(1), 1);
    check('removing what is not there', q.remove({}), -1);
    q.remove(q.at(3));
    check('after removing the last, the one above', q.afterRemoval(3), 2);

    // A group whose items are all gone starts again at the end.
    q.remove(q.at(0));
    q.add(item('a3', 'A'));
    check('a returning group goes last', names(), ['b1', 'b2', 'a3']);

    check('step down', q.step(0, 1), 1);
    check('step clamps at the end', q.step(2, 1), 2);
    check('step clamps at the top', q.step(0, -1), 0);
    check('step from nothing, down', q.step(-1, 1), 0);
    check('step from nothing, up', q.step(-1, -1), 2);
    check('empty', new Queue().step(-1, 1), -1);
    check('empty after removal', new Queue().afterRemoval(0), -1);
}

check('waited seconds', waited(12.5e6), '12s');
check('waited minutes', waited(125e6), '2m');
check('waited hours', waited(3725e6), '1h 2m');

// ---- markup ----------------------------------------------------------------

{
    const plain = render('<b>not markup</b>', false);
    check('plain text is text', [plain.text, plain.attributes], ['<b>not markup</b>', null]);

    const kept = render('<b>bold</b> <i>it</i> <u>u</u> <s>s</s> <tt>tt</tt>', true);
    check('emphasis is kept', [kept.text, kept.attributes.to_string()], [
        'bold it u s tt',
        '0 4 weight bold\n5 7 style italic\n8 9 underline single\n10 11 strikethrough true\n12 14 family "Monospace"',
    ]);

    const dropped = render(
        '<span foreground="white" background="white" size="1" font_family="Wingdings" rise="9999" fgalpha="1">x</span>', true);
    check('colour, size, font and the rest are dropped', [dropped.text, dropped.attributes], ['x', null]);

    const link = render('<a href="https://example.com">click</a>', true);
    check('a link is not Pango markup, so shown as text', link.text, '<a href="https://example.com">click</a>');

    const broken = render('a & b <b>', true);
    check('broken markup is shown as text', [broken.text, broken.attributes], ['a & b <b>', null]);
}

check('lines', lines('\n  one \n\n two\nthree', 2), ['one', 'two']);
check('button markup underlines its key', buttonMarkup(GLib, 'A<sk>', 3), 'A&lt;s<u>k</u>&gt;');
check('button markup without a key', buttonMarkup(GLib, 'a&b', -1), 'a&amp;b');

// ---- items -----------------------------------------------------------------

{
    const good = {
        kind: 'question', title: 'Allow this request?', text: 'GET /', default: 1,
        buttons: [
            {answer: 'cancel', label: 'Refuse', key: 'r', underline: 0},
            {answer: 'ok', label: 'Allow', key: 'a', underline: 0},
            {answer: 'extra', index: 0, label: 'Ask', key: 's', underline: 1},
        ],
        unknown: 'dropped',
    };
    const item = validate(good);
    check('a good item', [item.kind, item.title, item.default, item.buttons.length, item.unknown],
        ['question', 'Allow this request?', 1, 3, undefined]);
    check('an entry has its field', validate({kind: 'entry', buttons: [], entry: {hidden: true}}).entry,
        {text: '', hidden: true});
    check('a switch closes', validate({kind: 'question', buttons: [{answer: 'close', label: 'Close'}]}).buttons[0].answer,
        'close');

    const bad = (name, change, pattern) =>
        throws(name, () => validate({...good, ...change}), pattern);
    throws('no item', () => validate(null), /no item/);
    bad('an unknown kind', {kind: 'list'}, /not one galley shows/);
    bad('a title that is not a string', {title: 3}, /title is not a string/);
    bad('a default past the buttons', {default: 3}, /default/);
    bad('too many buttons', {buttons: Array(17).fill(good.buttons[0])}, /at most 16/);
    bad('a button with no answer', {buttons: [{label: 'x'}]}, /no answer/);
    bad('a key that moves the queue', {buttons: [{answer: 'ok', label: 'j', key: 'j'}]}, /cannot be used/);
    bad('two buttons on one key', {buttons: [good.buttons[0], good.buttons[0]]}, /cannot be used/);
    bad('a key that is not one character', {buttons: [{answer: 'ok', label: 'x', key: 'xy'}]}, /longer/);
    bad('an underline past the label', {buttons: [{answer: 'ok', label: 'ab', underline: 2}]}, /underline/);
    bad('a huge text', {text: 'x'.repeat((1 << 20) + 1)}, /longer/);
}

// ---- the second protocol's items -------------------------------------------

{
    const v2 = raw => validate(raw, 2);
    const list = v2({kind: 'list', buttons: [], list: {columns: ['a', 'b'], rows: [['1', '2'], ['3']],
        type: 'check', hidden: [1], more: true}});
    check('a list', [list.list.columns, list.list.rows, list.list.type, list.list.hidden, list.list.more],
        [['a', 'b'], [['1', '2'], ['3']], 'check', [1], true]);
    check('a combo is never hidden', v2({kind: 'entry', buttons: [], entry: {hidden: true, values: ['a']}}).entry,
        {text: '', hidden: false, values: ['a']});
    check('a first-protocol entry has no values', validate({kind: 'entry', buttons: [], entry: {values: ['a']}}).entry,
        {text: '', hidden: false});
    check('a form', v2({kind: 'forms', buttons: [], forms: {fields: [{kind: 'list', label: 'L'}, {kind: 'combo'}]}}).forms,
        {dateFormat: '%x', fields: [
            {kind: 'list', label: 'L', columns: ['column'], rows: [], showHeader: false, values: null},
            {kind: 'combo', label: '', columns: ['column'], rows: [], showHeader: false, values: null}]});
    check('a disabled button', v2({kind: 'progress', buttons: [{answer: 'ok', label: 'OK', disabled: true}]}).buttons[0].disabled,
        true);
    check('a first-protocol button is never disabled',
        validate({kind: 'info', buttons: [{answer: 'ok', label: 'OK', disabled: true}]}).buttons[0].disabled, false);

    const bad = (name, raw, pattern) => throws(name, () => v2(raw), pattern);
    throws('a second-protocol kind in the first', () => validate({kind: 'scale', buttons: []}), /not one galley shows/);
    bad('a list with no columns', {kind: 'list', buttons: [], list: {columns: []}}, /no columns/);
    bad('a tick list of one column', {kind: 'list', buttons: [], list: {columns: ['a'], type: 'radio'}}, /two columns/);
    bad('a row longer than the columns', {kind: 'list', buttons: [], list: {columns: ['a'], rows: [['1', '2']]}}, /at most 1/);
    bad('a hidden column that is not one', {kind: 'list', buttons: [], list: {columns: ['a'], hidden: [1]}}, /hidden/);
    bad('a field galley does not show', {kind: 'forms', buttons: [], forms: {fields: [{kind: 'slider'}]}}, /not a field/);
    bad('a scale out of its range', {kind: 'scale', buttons: [], scale: {min: 0, max: 10, value: 11}}, /range/);
    bad('a chooser starting somewhere relative', {kind: 'file', buttons: [], file: {mode: 'open', folder: 'tmp'}}, /absolute/);
    bad('a chooser of an unknown mode', {kind: 'file', buttons: [], file: {mode: 'delete'}}, /mode/);
    bad('a locale that is not one', {kind: 'calendar', buttons: [], locale: 'C; rm -rf /'}, /locale/);
    bad('a percentage past 100', {kind: 'progress', buttons: [], progress: {percentage: 101}}, /percentage/);

    check('rows', rows([['a'], []], 'rows', 1), [['a'], []]);
    throws('rows too wide', () => rows([['a', 'b']], 'rows', 1), /at most 1/);
    check('a progress update', progressUpdate({percentage: 50, text: '<b>x</b>', closed: true}),
        {finished: false, closed: true, percentage: 50, text: '<b>x</b>'});
    throws('a progress update past 100', () => progressUpdate({percentage: 200}), /percentage/);
    check('a notification', notify({text: 'hi', icon: 'x', extra: 1}), {text: 'hi', icon: 'x'});
}

// ---- dates -------------------------------------------------------------------

{
    const date = GLib.DateTime.new_local(2024, 3, 5, 0, 0, 0);
    check('a date', formatDate(date, '%Y-%m-%d %A', ''), '2024-03-05 Tuesday');
    check('a date in C', formatDate(date, '%x', 'C'), '03/05/24');
    // A locale the system does not have leaves the window's.
    check('an unknown locale', formatDate(date, '%Y', 'xx_XX.UTF-8'), '2024');
    check('a format GLib cannot write', formatDate(date, '%Q', ''), null);
}

if (failed) {
    printerr(`${failed} failed`);
    System.exit(1);
}
print('ok');
