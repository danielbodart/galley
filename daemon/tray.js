// The tray icon: galley's icon in the top bar, with how many questions are
// waiting beside it, and a menu to bring the window forward or quit. It is a
// StatusNotifierItem on the session bus, which GNOME shows with the
// AppIndicator extension and other desktops on their own; with neither,
// nothing shows it and nothing is lost.
//
// It is there from the window's start, which is the first question's
// arrival (nix/galley.socket), and goes with the window. Nothing in it
// answers a question: Open does what `galley --show` does, and Quit ends the
// window, which every client waiting reads as a hang-up, as when it is
// killed.

import GLib from 'gi://GLib';
import Gio from 'gi://Gio';

const WATCHER = 'org.kde.StatusNotifierWatcher';
const PATH = '/StatusNotifierItem';
const MENU = '/StatusNotifierItem/Menu';

const ITEM = `
<node>
  <interface name="org.kde.StatusNotifierItem">
    <property name="Category" type="s" access="read"/>
    <property name="Id" type="s" access="read"/>
    <property name="Title" type="s" access="read"/>
    <property name="Status" type="s" access="read"/>
    <property name="IconName" type="s" access="read"/>
    <property name="ItemIsMenu" type="b" access="read"/>
    <property name="Menu" type="o" access="read"/>
    <property name="XAyatanaLabel" type="s" access="read"/>
    <property name="XAyatanaLabelGuide" type="s" access="read"/>
    <method name="Activate"><arg name="x" type="i" direction="in"/><arg name="y" type="i" direction="in"/></method>
    <method name="SecondaryActivate"><arg name="x" type="i" direction="in"/><arg name="y" type="i" direction="in"/></method>
    <method name="Scroll"><arg name="delta" type="i" direction="in"/><arg name="orientation" type="s" direction="in"/></method>
    <method name="ProvideXdgActivationToken"><arg name="token" type="s" direction="in"/></method>
    <signal name="XAyatanaNewLabel"><arg name="label" type="s"/><arg name="guide" type="s"/></signal>
  </interface>
</node>`;

// The menu, in the form the tray asks for it (com.canonical.dbusmenu). It
// never changes, so its revision is always the first.
const DBUSMENU = `
<node>
  <interface name="com.canonical.dbusmenu">
    <property name="Version" type="u" access="read"/>
    <property name="TextDirection" type="s" access="read"/>
    <property name="Status" type="s" access="read"/>
    <property name="IconThemePath" type="as" access="read"/>
    <method name="GetLayout">
      <arg type="i" name="parentId" direction="in"/><arg type="i" name="recursionDepth" direction="in"/>
      <arg type="as" name="propertyNames" direction="in"/>
      <arg type="u" name="revision" direction="out"/><arg type="(ia{sv}av)" name="layout" direction="out"/>
    </method>
    <method name="GetGroupProperties">
      <arg type="ai" name="ids" direction="in"/><arg type="as" name="propertyNames" direction="in"/>
      <arg type="a(ia{sv})" name="properties" direction="out"/>
    </method>
    <method name="GetProperty">
      <arg type="i" name="id" direction="in"/><arg type="s" name="name" direction="in"/>
      <arg type="v" name="value" direction="out"/>
    </method>
    <method name="Event">
      <arg type="i" name="id" direction="in"/><arg type="s" name="eventId" direction="in"/>
      <arg type="v" name="data" direction="in"/><arg type="u" name="timestamp" direction="in"/>
    </method>
    <method name="EventGroup">
      <arg type="a(isvu)" name="events" direction="in"/><arg type="ai" name="idErrors" direction="out"/>
    </method>
    <method name="AboutToShow">
      <arg type="i" name="id" direction="in"/><arg type="b" name="needUpdate" direction="out"/>
    </method>
    <method name="AboutToShowGroup">
      <arg type="ai" name="ids" direction="in"/>
      <arg type="ai" name="updatesNeeded" direction="out"/><arg type="ai" name="idErrors" direction="out"/>
    </method>
    <signal name="LayoutUpdated"><arg type="u" name="revision"/><arg type="i" name="parent"/></signal>
  </interface>
</node>`;

const OPEN = 1, LINE = 2, QUIT = 3;

// Each entry's properties, by id: the menu itself, then Open, a line, Quit.
const ENTRIES = {
    0: {'children-display': new GLib.Variant('s', 'submenu')},
    [OPEN]: {label: new GLib.Variant('s', 'Open galley')},
    [LINE]: {type: new GLib.Variant('s', 'separator')},
    [QUIT]: {label: new GLib.Variant('s', 'Quit')},
};

const LAYOUT = new GLib.Variant('(u(ia{sv}av))', [1, [0, ENTRIES[0],
    [OPEN, LINE, QUIT].map(id => new GLib.Variant('(ia{sv}av)', [id, ENTRIES[id], []]))]]);

// The label beside the icon: the count while something waits, else nothing.
export const label = n => (n > 0 ? String(n) : '');

// The widest the label is likely to be, so the bar does not shift as the
// count changes.
const GUIDE = '99';

export class Tray {
    // show(token) brings the window forward, with the activation token the
    // tray gave for the click when it gave one: under Wayland only that lets
    // the window take focus. quit() ends the window.
    constructor(connection, show, quit) {
        this._waiting = 0;
        this._token = null;
        const open = () => {
            const token = this._token;
            this._token = null;
            show(token);
        };
        const clicked = (id, eventId) => {
            if (eventId !== 'clicked')
                return;
            if (id === OPEN)
                open();
            else if (id === QUIT)
                quit();
        };

        const tray = this;
        this._item = Gio.DBusExportedObject.wrapJSObject(ITEM, {
            Category: 'ApplicationStatus',
            Id: 'galley',
            Title: 'galley',
            Status: 'Active',
            IconName: 'dialog-question-symbolic',
            ItemIsMenu: false,
            Menu: MENU,
            get XAyatanaLabel() {
                return label(tray._waiting);
            },
            XAyatanaLabelGuide: GUIDE,
            Activate: open,
            SecondaryActivate: open,
            Scroll: () => {},
            ProvideXdgActivationToken: token => {
                this._token = token;
            },
        });
        this._item.export(connection, PATH);

        this._menu = Gio.DBusExportedObject.wrapJSObject(DBUSMENU, {
            Version: 3,
            TextDirection: 'ltr',
            Status: 'normal',
            IconThemePath: [],
            GetLayoutAsync: (_params, invocation) => invocation.return_value(LAYOUT),
            GetGroupPropertiesAsync: ([ids], invocation) => invocation.return_value(
                new GLib.Variant('(a(ia{sv}))', [ids.filter(id => id in ENTRIES).map(id => [id, ENTRIES[id]])])),
            GetPropertyAsync: ([id, name], invocation) => {
                const value = ENTRIES[id]?.[name];
                if (value)
                    invocation.return_value(new GLib.Variant('(v)', [value]));
                else
                    invocation.return_dbus_error('com.canonical.dbusmenu.Error', `no ${name} on ${id}`);
            },
            Event: clicked,
            EventGroup: events => {
                for (const [id, eventId] of events)
                    clicked(id, eventId);
                return [];
            },
            AboutToShow: () => false,
            AboutToShowGroup: () => [[], []],
        });
        this._menu.export(connection, MENU);

        // The tray may come after the window, or go and come back with the
        // shell: the item is offered to it each time it appears.
        Gio.bus_watch_name_on_connection(connection, WATCHER,
            Gio.BusNameWatcherFlags.NONE,
            () => connection.call(WATCHER, '/StatusNotifierWatcher', WATCHER,
                'RegisterStatusNotifierItem', new GLib.Variant('(s)', [PATH]),
                null, Gio.DBusCallFlags.NONE, -1, null, null),
            null);
    }

    // How many questions are waiting, for the label.
    count(n) {
        if (n === this._waiting)
            return;
        this._waiting = n;
        this._item.emit_signal('XAyatanaNewLabel', new GLib.Variant('(ss)', [label(n), GUIDE]));
    }
}
