// Dates as zenity writes them: by a GLib format (g_date_time_format), in
// the caller's LC_TIME.
//
// The window has one locale, the session's, and a caller may have its own;
// zenity, run by the caller, writes month names and %x in the caller's. So
// the caller's LC_TIME is taken for the moment a date is written, when this
// process's system has that locale, and the window's own put back.

import Gettext from 'gettext';

// Returns the date written, or null when GLib cannot write the format.
export function formatDate(date, format, locale) {
    const C = Gettext.LocaleCategory.TIME;
    const before = locale ? Gettext.setlocale(C, null) : null;
    const switched = locale ? Gettext.setlocale(C, locale) !== null : false;
    try {
        return date.format(format);
    } finally {
        if (switched)
            Gettext.setlocale(C, before);
    }
}
