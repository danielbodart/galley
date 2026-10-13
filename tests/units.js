// The window's logic that needs no display -- the queue's order, the
// sanitising of markup, the checking of an item -- under plain gjs:
//
//   gjs -m tests/units.js
//
// Exits non-zero, having said which, when any fails.

import GLib from 'gi://GLib';
import System from 'system';

import {Queue, waited, sameness} from '../daemon/queue.js';
import {render, lines, buttonMarkup} from '../daemon/render.js';
import {validate, rows, progressUpdate, notify, MAX_DIFF_SIDE} from '../daemon/validate.js';
import {formatDate} from '../daemon/dates.js';
import {tint, symbolicName, css, levelIcon} from '../daemon/icons.js';
import {admitted, fromService} from '../daemon/services.js';
import {parseConfig, configArg, loadConfig, defaults} from '../daemon/config.js';
import {isDiff, diffSpans, MAX_DIFF, unifiedDiff, editScript} from '../daemon/diff.js';
import {label} from '../daemon/tray.js';

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
    bad('an unknown kind', {kind: 'slider'}, /not one galley shows/);
    bad('a title that is not a string', {title: 3}, /title is not a string/);
    bad('a default past the buttons', {default: 3}, /default/);
    bad('too many buttons', {buttons: Array(17).fill(good.buttons[0])}, /at most 16/);
    bad('a button with no answer', {buttons: [{label: 'x'}]}, /no answer/);
    bad('a key that moves the queue', {buttons: [{answer: 'ok', label: 'j', key: 'j'}]}, /cannot be used/);
    bad('two buttons on one key', {buttons: [good.buttons[0], good.buttons[0]]}, /cannot be used/);
    bad('a key that is not one character', {buttons: [{answer: 'ok', label: 'x', key: 'xy'}]}, /longer/);
    bad('an underline past the label', {buttons: [{answer: 'ok', label: 'ab', underline: 2}]}, /underline/);
    bad('a huge text', {text: 'x'.repeat((1 << 20) + 1)}, /longer/);

    // A title of any length is shown, cut short with an ellipsis, never
    // refused: the text still carries the whole of what is asked.
    const titled = title => validate({...good, title}).title;
    check('a title of the most is kept', titled('x'.repeat(1024)), 'x'.repeat(1024));
    check('a longer title is cut short', titled('x'.repeat(1025)), 'x'.repeat(1023) + '…');
    check('a huge title is cut short', titled('x'.repeat(1 << 20)), 'x'.repeat(1023) + '…');
    check('a title is never cut inside a character', titled('x'.repeat(1022) + '😀😀'), 'x'.repeat(1022) + '…');
    check('a title of the most in characters is kept', titled('x'.repeat(1022) + '😀'), 'x'.repeat(1022) + '😀');
}

// ---- the other dialogs' items ----------------------------------------------

{
    const list = validate({kind: 'list', buttons: [], list: {columns: ['a', 'b'], rows: [['1', '2'], ['3']],
        type: 'check', hidden: [1], more: true}});
    check('a list', [list.list.columns, list.list.rows, list.list.type, list.list.hidden, list.list.more],
        [['a', 'b'], [['1', '2'], ['3']], 'check', [1], true]);
    check('a combo is never hidden', validate({kind: 'entry', buttons: [], entry: {hidden: true, values: ['a']}}).entry,
        {text: '', hidden: false, values: ['a']});
    check('a form', validate({kind: 'forms', buttons: [], forms: {fields: [{kind: 'list', label: 'L'}, {kind: 'combo'}]}}).forms,
        {dateFormat: '%x', fields: [
            {kind: 'list', label: 'L', columns: ['column'], rows: [], showHeader: false, values: null, text: ''},
            {kind: 'combo', label: '', columns: ['column'], rows: [], showHeader: false, values: null, text: ''}]});
    check('a disabled button', validate({kind: 'progress', buttons: [{answer: 'ok', label: 'OK', disabled: true}]}).buttons[0].disabled,
        true);

    const bad = (name, raw, pattern) => throws(name, () => validate(raw), pattern);
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

// ---- levels, and fields' text ----------------------------------------------

{
    check('no level is normal', validate({kind: 'question', buttons: []}).level, 'normal');
    check('a level', validate({kind: 'question', buttons: [], level: 'danger'}).level, 'danger');
    const fields = validate({kind: 'forms', buttons: [], forms: {fields: [
        {kind: 'entry', text: '15'}, {kind: 'multiline', text: 'a\nb'}, {kind: 'combo', text: 'x'}]}}).forms.fields;
    check('a field\'s text', fields.map(f => f.text), ['15', 'a\nb', '']);

    const bad = (name, raw, pattern) => throws(name, () => validate(raw), pattern);
    bad('a level galley does not know', {kind: 'question', buttons: [], level: 'red'}, /level/);
    bad('a password with text', {kind: 'forms', buttons: [], forms: {fields: [{kind: 'password', text: 'x'}]}},
        /password/);
    bad('a caller', {kind: 'question', buttons: [], caller: {uid: 0, name: 'root', label: 'root', pid: 1}}, /caller/);
    throws('a null caller', () => validate({kind: 'question', buttons: [], caller: null}), /caller/);

    // The golden files the wire package reads, as the window reads them.
    const golden = name => JSON.parse(new TextDecoder().decode(GLib.file_get_contents(`wire/testdata/${name}`)[1]));
    for (const [name, level] of [['signer-form.json', 'normal'], ['signer-question.json', 'warning'],
        ['sudo-dialog.json', 'normal'], ['sudo-dialog-warning.json', 'warning'],
        ['sudo-dialog-danger.json', 'danger'], ['sudo-question.json', 'danger']]) {
        const hello = golden(name);
        const item = validate(hello.item);
        check(`${name}'s level`, item.level, level);
        check(`${name}'s fields`, item.forms?.fields.map(f => [f.kind, f.text]),
            hello.item.forms?.fields.map(f => [f.kind, f.text ?? '']));
    }
}

// ---- tables ------------------------------------------------------------------

{
    const t = {columns: ['pid', 'process'], rows: [['5113', 'claude'], ['', 'bash -c\n    make']], monospace: [1],
        summary: 'claude ← bash -c'};
    check('a table', validate({kind: 'question', buttons: [], table: t}).table, t);
    check('no table', validate({kind: 'question', buttons: []}).table, undefined);
    check('a table with no monospace', validate({kind: 'question', buttons: [], table: {columns: ['a'], rows: []}}).table,
        {columns: ['a'], rows: [], monospace: [], summary: ''});
    const bad = (name, table, pattern) => throws(name, () => validate({kind: 'question', buttons: [], table}), pattern);
    bad('a table that is a list', [], /table/);
    bad('a table with no columns', {columns: [], rows: []}, /no columns/);
    bad('a row wider than the columns', {columns: ['a'], rows: [['x', 'y']]}, /table rows/);
    bad('a summary that is not a string', {columns: ['a'], rows: [], summary: 1}, /summary/);
    bad('a monospace column that is not there', {columns: ['a'], rows: [], monospace: [1]}, /monospace/);
    bad('too many rows', {columns: ['a'], rows: Array.from({length: 65}, () => ['x'])}, /table rows/);
}

// ---- the same question -------------------------------------------------------

{
    const ask = {
        kind: 'question', title: 'Allow this request?', text: 'GET /', width: 640, icon: 'security-medium',
        buttons: [{answer: 'cancel', label: 'Refuse', key: 'r'}, {answer: 'ok', label: 'Allow', key: 'a'}],
    };
    const key = raw => sameness(validate(raw));
    // The whole item, as validate() makes it: fields it does not keep make
    // no difference, any it keeps does.
    check('the same question', key(ask), key({...ask}));
    check('what validate drops', key({...ask, unknown: 'x', timeout: 5}), key(ask));
    check('fields in another order', key({buttons: ask.buttons, ...ask, kind: 'question'}), key(ask));
    const differs = (name, change) => check(name, key({...ask, ...change}) === key(ask), false);
    differs('another text', {text: 'GET /x'});
    differs('another title', {title: 'Approve this change?'});
    differs('another icon', {icon: 'media-tape'});
    differs('another width', {width: 720});
    differs('another button', {buttons: [ask.buttons[0], {answer: 'ok', label: 'Approve', key: 'a'}]});

    // Nothing that streams is ever the same as anything.
    const never = (name, raw) => check(name, key(raw), null);
    never('a text read from stdin', {kind: 'text', buttons: [], info: {more: true}});
    never('a list read from stdin', {kind: 'list', buttons: [], list: {columns: ['a'], more: true}});
    never('a progress bar', {kind: 'progress', buttons: []});
    never('a listening notification', {kind: 'notification', buttons: [], note: {listen: true}});
    never('a scale printing as it moves', {kind: 'scale', buttons: [], scale: {partial: true}});

    // Everything else is.
    const shared = (name, raw) => check(name, typeof key(raw), 'string');
    shared('a text from a file', {kind: 'text', buttons: [], info: {text: 'x'}});
    shared('an editable text', {kind: 'text', buttons: [], info: {text: 'x', editable: true}});
    shared('a list', {kind: 'list', buttons: [], list: {columns: ['a'], rows: [['1']]}});
    shared('a scale', {kind: 'scale', buttons: [], scale: {value: 3}});
    shared('a notification', {kind: 'notification', buttons: [], text: 'done'});
    shared('a password', {kind: 'password', buttons: []});
    shared('a hidden entry', {kind: 'entry', buttons: [], entry: {hidden: true}});
    shared('a question', ask);
}

// ---- services ----------------------------------------------------------------

{
    const passwd = 'root:x:0:0::/root:/bin/sh\nssh-signer:x:951:951::/:/sbin/nologin\n' +
        'sudo-gate:x:952:952::/:/sbin/nologin\nbroken:x:abc:1::/:/\n';
    const map = admitted({'ssh-signer': 'SSH signer', 'sudo-gate': 'sudo', ghost: 'Ghost', broken: 'B'}, passwd);
    check('admitted', [...map.entries()], [[951, {name: 'ssh-signer', label: 'SSH signer'}],
        [952, {name: 'sudo-gate', label: 'sudo'}]]);
    check('none set', admitted({}, passwd).size, 0);

    const ask = {kind: 'question', title: 'ssh to server', text: '<b>x</b>', markup: true, icon: 'media-tape',
        buttons: [{answer: 'cancel', label: 'Refuse'}]};
    const signer = {name: 'ssh-signer', label: 'SSH signer'};
    const item = level => fromService(validate({...ask, level}), signer, 951, 42);
    check('the caller', item('normal').caller, {uid: 951, name: 'ssh-signer', label: 'SSH signer', pid: 42});
    check('no markup', item('normal').markup, false);
    check('icons by level', ['normal', 'warning', 'danger'].map(l => item(l).icon),
        ['security-medium', 'dialog-warning', 'security-low']);
    check('each level its own colour', new Set(Object.values(levelIcon).map(tint)).size, 3);
    check('a service\'s item is never the same as another', sameness(item('normal')), null);
}

// ---- the settings file -------------------------------------------------------
//
// The examples `galley check` is tested against (internal/config), so the
// window and the check agree on what is a config.

{
    const dir = 'internal/config/testdata';
    const examples = kind => {
        const names = [];
        const d = GLib.Dir.open(`${dir}/${kind}`, 0);
        for (let n; (n = d.read_name()) !== null;)
            names.push(n);
        d.close();
        return names.sort();
    };
    const read = path => new TextDecoder().decode(GLib.file_get_contents(path)[1]);
    check('there are good examples', examples('good').length > 0, true);
    check('there are bad examples', examples('bad').length > 0, true);
    for (const name of examples('good')) {
        try {
            parseConfig(read(`${dir}/good/${name}`));
        } catch (e) {
            printerr(`FAIL good/${name}: ${e.message}`);
            failed++;
        }
    }
    for (const name of examples('bad'))
        throws(`bad/${name}`, () => parseConfig(read(`${dir}/bad/${name}`)), /./);

    check('every setting', parseConfig(read(`${dir}/good/every-setting.json`)),
        {accessibility: true, services: {'ssh-signer': 'SSH signer', 'sudo-gate': '50% "sudo"'}});
    check('an empty file is the defaults', parseConfig('{}'), {accessibility: false, services: {}});
    throws('an unknown key is named', () => parseConfig('{"accesibility": true}'), /unknown field "accesibility"/);

    check('no -config', configArg([]), null);
    check('-config FILE', configArg(['-config', '/x']), '/x');
    check('--config=FILE', configArg(['--config=/y']), '/y');
    throws('-config with no FILE', () => configArg(['-config']), /FILE/);
    throws('another argument', () => configArg(['--gapplication-service']), /unexpected/);

    const missing = loadConfig(GLib, null);
    check('no file at the default path is the defaults, unremarked',
        missing.error === null || !missing.error.includes('No such file'), true);
    const named = loadConfig(GLib, `${dir}/nowhere.json`);
    check('a named file that is missing is the defaults, with the error',
        [named.config, named.error !== null], [defaults(), true]);
    const bad = loadConfig(GLib, `${dir}/bad/misspelt-key.json`);
    check('a bad file is the defaults, with the error', [bad.config, /accesibility/.test(bad.error)], [defaults(), true]);
    check('a good file', loadConfig(GLib, `${dir}/good/every-setting.json`).config.accessibility, true);
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

// ---- icons -----------------------------------------------------------------

{
    // Each kind of question its colour, the same for an icon and its
    // symbolic variant; a file none.
    check('sudo', tint('dialog-password'), 'tint-yellow');
    check('symbolic', tint('dialog-password-symbolic'), 'tint-yellow');
    check('approval', tint('accessories-text-editor'), 'tint-purple');
    check('request', tint('security-medium'), 'tint-blue');
    check('question', tint('dialog-question'), 'tint-teal');
    check('entry', tint('text-editor'), 'tint-green');
    check('recording', tint('media-tape'), 'tint-red');
    check('forms', tint('document-edit'), 'tint-green');
    check('file', tint('document-open'), 'tint-blue');
    check('a file', tint('/a/b.png'), null);
    check('none', tint(''), null);
    // Any other name one of the same colours, always the same one.
    check('hashed', tint('some-caller-icon'), tint('some-caller-icon-symbolic'));
    check('hashed in palette', /^tint-[a-z]+$/.test(tint('some-caller-icon')) && css(false).includes(`image.${tint('some-caller-icon')} `), true);

    // The queue shows the symbolic variant when the theme has one.
    const has = n => n === 'media-floppy-symbolic';
    check('has symbolic', symbolicName('media-floppy', has), 'media-floppy-symbolic');
    check('already symbolic', symbolicName('media-floppy-symbolic', has), 'media-floppy-symbolic');
    check('no symbolic', symbolicName('dialog-question', has), null);
    check('file symbolic', symbolicName('/a/media-floppy', () => true), null);
    // A shade of each colour for either style.
    check('light shades', css(false).includes('image.tint-blue { color: var(--blue-4); }'), true);
    check('dark shades', css(true).includes('image.tint-blue { color: var(--blue-2); }'), true);
}

// ---- diffs -------------------------------------------------------------------

{
    // chase's approver: its own lines, then `diff -u`'s.
    const approval = [
        'What this checkout\'s chase.jsonc asks for has changed.',
        '',
        '--- approved',
        '+++ proposed',
        '@@ -1,3 +1,4 @@',
        ' {',
        '-  "images": ["postgres:18"],',
        '+  "images": ["postgres:18", "redis:8"],',
        '+  "lan": true',
        ' }',
    ].join('\n');
    check('a diff', isDiff(approval), true);
    check('prose', isDiff('--- a line of dashes\n+++ and pluses\nbut no hunk'), false);
    check('a list', isDiff('- one\n+ two\n@@ three'), false);
    check('too long', isDiff(approval + ' '.repeat(MAX_DIFF)), false);

    const spans = diffSpans(approval);
    const at = (kind, n) => spans.filter(s => s.kind === kind).map(s => approval.slice(s.start, s.end))[n];
    check('nothing before the headers', spans.some(s => s.start < approval.indexOf('---')), false);
    check('file', at('file', 0), '--- approved');
    check('file +', at('file', 1), '+++ proposed');
    check('hunk', at('hunk', 0), '@@ -1,3 +1,4 @@');
    check('del', at('del', 0), '-  "images": ["postgres:18"],');
    check('add', at('add', 1), '+  "lan": true');
    check('context', spans.some(s => approval.slice(s.start, s.end) === ' {'), false);
    // What changed in the line put in place of another.
    check('del word', at('delWord', 0), undefined);
    check('add word', at('addWord', 0), ', "redis:8"');
    check('one pair', spans.filter(s => s.kind === 'addWord').length, 1);

    // Offsets count characters, as GTK's do, not UTF-16 units.
    const wide = '--- a\n+++ b\n@@ -1 +1 @@\n-🙂 x\n+🙂 y';
    const w = diffSpans(wide).find(s => s.kind === 'addWord');
    check('code points', [w.start, w.end], [Array.from(wide).length - 1, Array.from(wide).length]);
    // Lines with nothing in common are not marked within.
    check('unrelated', diffSpans('--- a\n+++ b\n@@ -1 +1 @@\n-abc\n+xyz').some(s => s.kind.endsWith('Word')), false);
}

// ---- diffs and Always --------------------------------------------------------

{
    const buttons = [{answer: 'cancel', label: 'Refuse'}, {answer: 'extra', index: 0, label: 'Allow once'}];
    const d = {path: '~/Projects/foo/chase.json', old: '{}\n', new: '{"a": 1}\n'};
    check('a diff', validate({kind: 'question', buttons, diff: d}).diff, d);
    check('a new file', validate({kind: 'question', buttons, diff: {path: 'x', new: 'a\n'}}).diff,
        {path: 'x', old: '', new: 'a\n'});
    check('no diff', validate({kind: 'question', buttons}).diff, undefined);
    const bad = (name, diff, pattern) => throws(name, () => validate({kind: 'question', buttons, diff}), pattern);
    bad('a diff that is a list', [], /diff/);
    bad('a diff with no path', {old: '', new: ''}, /no path/);
    bad('a path over lines', {path: 'a\nb', new: ''}, /line break/);
    bad('old not a string', {path: 'a', old: 1}, /diff old/);
    bad('new too long', {path: 'a', new: 'x'.repeat(MAX_DIFF_SIDE + 1)}, /diff new is longer/);
    check('a side at the most', validate({kind: 'question', buttons, diff: {path: 'a', old: 'x'.repeat(MAX_DIFF_SIDE)}})
        .diff.old.length, MAX_DIFF_SIDE);

    // Always is the window's button, after the item's, never the default.
    const always = validate({kind: 'question', buttons, default: 1, always: true});
    check('always', always.always, true);
    check('always button', always.buttons[2], {answer: 'always', index: 0, label: 'Always', key: '', underline: -1,
        disabled: false});
    check('always is not the default', always.default, 1);
    check('no always', validate({kind: 'question', buttons}).buttons.length, 2);
    check('always must be true', validate({kind: 'question', buttons, always: 'yes'}).buttons.length, 2);
    throws('a default on always', () => validate({kind: 'question', buttons, default: 2, always: true}), /default/);
    throws('a poster\'s always button', () => validate({kind: 'question', buttons: [{answer: 'always', label: 'Sure'}]}),
        /no answer galley knows/);
    // Two items alike but for always, or their diffs, are not the same question.
    check('always is part of sameness', sameness(always) === sameness(validate({kind: 'question', buttons, default: 1})),
        false);
}

// ---- diffs worked out -----------------------------------------------------------

{
    const diff = (old, now) => unifiedDiff('chase.json', old, now).text;
    check('a change', diff('{\n  "a": 1,\n  "b": 2\n}\n', '{\n  "a": 1,\n  "b": 3\n}\n'), [
        '--- chase.json', '+++ chase.json', '@@ -1,4 +1,4 @@', ' {', '   "a": 1,', '-  "b": 2', '+  "b": 3', ' }',
    ].join('\n'));
    check('a new file', diff('', 'a\nb\n'), ['--- chase.json', '+++ chase.json', '@@ -0,0 +1,2 @@', '+a', '+b'].join('\n'));
    check('a file emptied', diff('a\n', ''), ['--- chase.json', '+++ chase.json', '@@ -1 +0,0 @@', '-a'].join('\n'));
    check('no change', diff('a\n', 'a\n'), ['--- chase.json', '+++ chase.json', 'No change.'].join('\n'));
    check('a newline taken away', diff('a\n', 'a'), [
        '--- chase.json', '+++ chase.json', '@@ -1 +1 @@', '-a', '+a', '\\ No newline at end of file',
    ].join('\n'));

    // Three lines of context; changes further apart than twice that are
    // hunks of their own, as `diff -u` makes them.
    const n = Array.from({length: 30}, (_, i) => `${i + 1}\n`);
    const changed = n.map((l, i) => (i === 1 || i === 9 || i === 25 ? `x${l}` : l));
    const heads = lines => diff(n.join(''), lines.join('')).split('\n').filter(l => l.startsWith('@@'));
    check('hunks', heads(changed), ['@@ -1,5 +1,5 @@', '@@ -7,7 +7,7 @@', '@@ -23,7 +23,7 @@']);
    check('hunks joined', heads(n.map((l, i) => (i === 1 || i === 8 ? `x${l}` : l))), ['@@ -1,12 +1,12 @@']);
    const added = [...n.slice(0, 15), 'new\n', ...n.slice(15)];
    check('an added line', diff(n.join(''), added.join('')).split('\n').slice(2),
        ['@@ -13,6 +13,7 @@', ' 13', ' 14', ' 15', '+new', ' 16', ' 17', ' 18']);

    // What is coloured: headers, the hunk's line, the lines added and
    // removed, and within a line put in place of another, what changed.
    const shown = unifiedDiff('p', 'keep\n"b": 2\n', 'keep\n"b": 3\n');
    const at = kind => shown.spans.filter(s => s.kind === kind).map(s => shown.text.slice(s.start, s.end));
    check('spans file', at('file'), ['--- p', '+++ p']);
    check('spans hunk', at('hunk'), ['@@ -1,2 +1,2 @@']);
    check('spans del', at('del'), ['-"b": 2']);
    check('spans add', at('add'), ['+"b": 3']);
    check('spans words', [at('delWord'), at('addWord')], [['2'], ['3']]);
    check('counts', [shown.added, shown.removed], [1, 1]);
    // A line that looks like a header is coloured as the line it is.
    const tricky = unifiedDiff('p', '-- a\n', '++ b\n@@ -1 +1 @@\n');
    check('header-like lines', tricky.spans.filter(s => s.kind === 'file').length, 2);

    // Any edit script, applied, makes the new lines from the old.
    let seed = 7;
    const random = k => {
        seed = (seed * 1103515245 + 12345) % 2147483648;
        return seed % k;
    };
    let wrong = 0;
    for (let round = 0; round < 300; round++) {
        const a = Array.from({length: random(20)}, () => 'abc'[random(3)]);
        const b = Array.from({length: random(20)}, () => 'abc'[random(3)]);
        const edits = editScript(a, b);
        const made = edits.filter(e => e.op !== '-').map(e => (e.op === '+' ? b[e.j] : a[e.i]));
        const kept = edits.filter(e => e.op !== '+').map(e => a[e.i]);
        if (JSON.stringify(made) !== JSON.stringify(b) || JSON.stringify(kept) !== JSON.stringify(a) ||
            edits.some(e => e.op === ' ' && a[e.i] !== b[e.j]))
            wrong++;
    }
    check('edit scripts', wrong, 0);
    // The fewest changes: one line changed in a thousand is one removed and one added.
    const big = Array.from({length: 1000}, (_, i) => `${i}`);
    const one = big.map((l, i) => (i === 500 ? 'x' : l));
    check('fewest', editScript(big, one).filter(e => e.op !== ' ').length, 2);
    // Past the budget, still the change, if not the shortest showing of it.
    const many = Array.from({length: 20000}, (_, i) => `${i % 7}\n`).join('');
    const other = Array.from({length: 20000}, (_, i) => `${(i * 3) % 11}\n`).join('');
    const huge = unifiedDiff('p', many, other);
    check('over budget', [huge.removed, huge.added], [19999, 19999]);
}

// ---- the tray ------------------------------------------------------------

check('tray label, nothing waiting', label(0), '');
check('tray label, one', label(1), '1');
check('tray label, many', label(12), '12');

if (failed) {
    printerr(`${failed} failed`);
    System.exit(1);
}
print('ok');
