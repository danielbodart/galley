// The queue's order, with nothing of GTK in it, so it can be tested on its
// own.
//
// Items are grouped by their caller -- an item's group is its title, which
// is the nearest thing to a caller zenity's command line has: frisket's asker
// titles every request "Allow this request?", sudo every password
// "Authentication Required". Groups stay in the order their first waiting
// item arrived, and within a group the newest is at the bottom. A group whose
// items are all answered is gone; its next item starts it again at the end.

export class Queue {
    constructor() {
        this.items = [];
    }

    get length() {
        return this.items.length;
    }

    at(i) {
        return this.items[i];
    }

    indexOf(item) {
        return this.items.indexOf(item);
    }

    // Adds item, whose group is item.group, and returns where it went.
    add(item) {
        let at = this.items.length;
        for (let i = this.items.length - 1; i >= 0; i--) {
            if (this.items[i].group === item.group) {
                at = i + 1;
                break;
            }
        }
        this.items.splice(at, 0, item);
        return at;
    }

    // Removes item and returns where it was, or -1.
    remove(item) {
        const i = this.items.indexOf(item);
        if (i >= 0)
            this.items.splice(i, 1);
        return i;
    }

    // Which item to select once the selected one, at index i, is gone: the
    // one that took its place, else the one above, else none. Never one
    // further away, so the next Enter answers what is in front of the eye.
    afterRemoval(i) {
        if (this.items.length === 0)
            return -1;
        return Math.min(i, this.items.length - 1);
    }

    // The index one step from i, clamped to the ends.
    step(i, by) {
        if (this.items.length === 0)
            return -1;
        if (i < 0)
            return by > 0 ? 0 : this.items.length - 1;
        return Math.max(0, Math.min(this.items.length - 1, i + by));
    }
}

// How long something has waited, said briefly.
export function waited(microseconds) {
    const s = Math.max(0, Math.floor(microseconds / 1e6));
    if (s < 60)
        return `${s}s`;
    const m = Math.floor(s / 60);
    if (m < 60)
        return `${m}m`;
    return `${Math.floor(m / 60)}h ${m % 60}m`;
}
