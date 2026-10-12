// The window's settings file: $XDG_CONFIG_HOME/galley/config.json, or the
// FILE of `galley-daemon -config FILE`. Its keys are the home-manager
// module's option names, and it is read strictly, to the rules of
// internal/config, which `galley check` applies when the module is built:
// an unknown key, a value of the wrong type or a null is an error, since
// each would be a setting that silently does not apply. Both readers are
// held to the examples in internal/config/testdata.

const KEYS = ['accessibility', 'services'];

// No colon, slash or white space, as Go's config.UserName.
const USER_NAME = /^[^:/ \t\r\n\v\f]+$/;

// The settings when there is no file: GTK's accessibility bus off, and no
// service admitted.
export function defaults() {
    return {accessibility: false, services: {}};
}

// The settings in text, or throws saying what is wrong with them.
export function parseConfig(text) {
    const c = JSON.parse(text);
    if (typeof c !== 'object' || c === null || Array.isArray(c))
        throw new Error('not a JSON object');
    for (const key of Object.keys(c)) {
        if (!KEYS.includes(key))
            throw new Error(`unknown field "${key}"`);
    }
    const out = defaults();
    if ('accessibility' in c) {
        if (typeof c.accessibility !== 'boolean')
            throw new Error('accessibility is not true or false');
        out.accessibility = c.accessibility;
    }
    if ('services' in c) {
        const s = c.services;
        if (typeof s !== 'object' || s === null || Array.isArray(s))
            throw new Error('services is not an object of user name to label');
        for (const [name, label] of Object.entries(s)) {
            if (!USER_NAME.test(name))
                throw new Error(`services: ${JSON.stringify(name)} is not a user name /etc/passwd could list`);
            if (typeof label !== 'string')
                throw new Error(`services.${name} is not a string`);
        }
        out.services = {...s};
    }
    return out;
}

// The settings file named on the command line, or null when none is, or
// throws on any other argument: -config FILE, --config FILE, -config=FILE.
export function configArg(args) {
    let path = null;
    for (let i = 0; i < args.length; i++) {
        const m = /^--?config(?:=(.*))?$/.exec(args[i]);
        if (!m)
            throw new Error(`unexpected argument ${JSON.stringify(args[i])}; the only one is -config FILE`);
        const value = m[1] ?? args[++i];
        if (!value)
            throw new Error('-config needs a FILE');
        path = value;
    }
    return path;
}

// The settings the window runs with, and what went wrong reading them, if
// anything: the file named, else the default path, where no file is the
// defaults. A file that cannot be read or is wrong is the defaults too,
// with the error, so the window still serves its user, with no service
// admitted and the accessibility bus off.
export function loadConfig(GLib, named) {
    const path = named ??
        GLib.build_filenamev([GLib.get_user_config_dir(), 'galley', 'config.json']);
    let bytes;
    try {
        bytes = GLib.file_get_contents(path)[1];
    } catch (e) {
        if (named === null && e.matches(GLib.FileError, GLib.FileError.NOENT))
            return {config: defaults(), path, error: null};
        return {config: defaults(), path, error: `${path}: ${e.message}`};
    }
    try {
        return {config: parseConfig(new TextDecoder().decode(bytes)), path, error: null};
    } catch (e) {
        return {config: defaults(), path, error: `${path}: ${e.message}`};
    }
}
