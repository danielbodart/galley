// Who may post on the services socket: the system users named in
// $GALLEY_SERVICES, a JSON object of user name to the label the window
// shows for them, each name's uid read from /etc/passwd once, at start.

import {levelIcon} from './icons.js';

// A map of uid to {name, label}. A name /etc/passwd does not have admits
// no one.
export function admitted(services, passwd) {
    const uids = new Map();
    for (const line of passwd.split('\n')) {
        const [name, , uid] = line.split(':');
        if (name && /^\d+$/.test(uid ?? '') && !uids.has(name))
            uids.set(name, Number(uid));
    }
    const out = new Map();
    if (!services)
        return out;
    const parsed = JSON.parse(services);
    if (typeof parsed !== 'object' || parsed === null || Array.isArray(parsed))
        throw new Error('GALLEY_SERVICES is not an object of user name to label');
    for (const [name, label] of Object.entries(parsed)) {
        if (typeof label !== 'string')
            throw new Error(`GALLEY_SERVICES' label for ${name} is not a string`);
        if (uids.has(name))
            out.set(uids.get(name), {name, label});
    }
    return out;
}

// A service's item, once validated: who posted it, never markup, and the
// icon for its level.
export function fromService(item, service, uid, pid) {
    item.caller = {uid, name: service.name, label: service.label, pid};
    item.markup = false;
    item.icon = levelIcon[item.level];
    return item;
}
