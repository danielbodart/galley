// Who may post on the services socket: the system users named in the
// settings' services, an object of user name to the label the window shows
// for them (config.js), each name's uid read from /etc/passwd once, at
// start.

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
    for (const [name, label] of Object.entries(services)) {
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
