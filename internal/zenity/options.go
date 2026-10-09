package zenity

import (
	"fmt"
	"strings"
)

// zenity's command line is GLib's GOption, with every option in one of
// seventeen groups (src/option.c). This file is that table and the reading
// GOption does of it; args.go makes an item of what it reads.

type arg int

const (
	none      arg = iota
	str           // G_OPTION_ARG_STRING and _FILENAME
	integer       // G_OPTION_ARG_INT
	strs          // G_OPTION_ARG_STRING_ARRAY: each one given, in order
	field         // a --forms field, G_OPTION_ARG_CALLBACK
	fieldText     // galley's --value for the --forms field before it
)

// entry is one GOptionEntry. key is what it sets, shared where zenity's
// entries share a variable -- every dialog's --text is the one string --
// and "dialog" for a dialog's own flag. noAlias is G_OPTION_FLAG_NOALIAS.
type entry struct {
	name    string
	arg     arg
	noAlias bool
	key     string
}

type group struct {
	name    string
	entries []entry
}

// zenity 4.2.2's groups, in the order it adds them, which is the order
// GOption looks an option up in and the order zenity checks them after
// parsing. --html, --url and --no-interaction are left out, as a zenity
// built without WebKit leaves them out (nixpkgs' is); galley refuses the
// first two by name instead.
var groups = []group{
	{"general", []entry{
		{"title", str, false, "title"},
		{"width", integer, false, "width"},
		{"height", integer, false, "height"},
		{"timeout", integer, false, "timeout"},
		{"ok-label", str, true, "ok-label"},
		{"cancel-label", str, true, "cancel-label"},
		{"extra-button", strs, false, "extra-button"},
		{"modal", none, true, "modal"},
		{"attach", integer, true, "attach"},
		{"icon-name", str, false, "icon-name"},
		{"window-icon", str, false, "window-icon"},
	}},
	{"calendar", []entry{
		{"calendar", none, false, "dialog"},
		{"text", str, true, "text"},
		{"day", integer, false, "day"},
		{"month", integer, false, "month"},
		{"year", integer, false, "year"},
		{"date-format", str, false, "date-format"},
	}},
	{"entry", []entry{
		{"entry", none, false, "dialog"},
		{"text", str, true, "text"},
		{"entry-text", str, false, "entry-text"},
		{"hide-text", none, false, "hide-text"},
	}},
	{"error", messageEntries("error")},
	{"info", messageEntries("info")},
	{"file-selection", []entry{
		{"file-selection", none, false, "dialog"},
		{"filename", str, true, "filename"},
		{"multiple", none, true, "multiple"},
		{"directory", none, false, "directory"},
		{"save", none, false, "save"},
		{"separator", str, true, "separator"},
		{"file-filter", strs, false, "file-filter"},
		{"confirm-overwrite", none, false, "confirm-overwrite"},
	}},
	{"list", []entry{
		{"list", none, false, "dialog"},
		{"text", str, true, "text"},
		{"column", strs, false, "column"},
		{"checklist", none, false, "checklist"},
		{"radiolist", none, false, "radiolist"},
		{"imagelist", none, false, "imagelist"},
		{"separator", str, true, "separator"},
		{"multiple", none, true, "multiple"},
		{"editable", none, true, "editable"},
		{"print-column", str, false, "print-column"},
		{"hide-column", str, false, "hide-column"},
		{"hide-header", none, true, "hide-header"},
		{"mid-search", none, true, "mid-search"},
	}},
	{"notification", []entry{
		{"notification", none, false, "dialog"},
		{"text", str, true, "text"},
		{"icon", str, true, "icon"},
		{"listen", none, false, "listen"},
		{"hint", strs, true, "hint"},
	}},
	{"progress", []entry{
		{"progress", none, false, "dialog"},
		{"text", str, true, "text"},
		{"percentage", integer, false, "percentage"},
		{"pulsate", none, false, "pulsate"},
		{"auto-close", none, false, "auto-close"},
		{"auto-kill", none, false, "auto-kill"},
		{"no-cancel", none, false, "no-cancel"},
		{"time-remaining", none, false, "time-remaining"},
	}},
	{"question", []entry{
		{"question", none, false, "dialog"},
		{"text", str, true, "text"},
		{"icon", str, true, "icon"},
		{"no-wrap", none, true, "no-wrap"},
		{"no-markup", none, true, "no-markup"},
		{"default-cancel", none, true, "default-cancel"},
		{"ellipsize", none, true, "ellipsize"},
		{"switch", none, true, "switch"},
	}},
	{"warning", messageEntries("warning")},
	{"scale", []entry{
		{"scale", none, false, "dialog"},
		{"text", str, true, "text"},
		{"value", integer, false, "value"},
		{"min-value", integer, false, "min-value"},
		{"max-value", integer, false, "max-value"},
		{"step", integer, false, "step"},
		{"print-partial", none, false, "print-partial"},
		{"hide-value", none, false, "hide-value"},
	}},
	{"text-info", []entry{
		{"text-info", none, false, "dialog"},
		{"filename", str, true, "filename"},
		{"editable", none, true, "editable"},
		{"font", str, false, "font"},
		{"checkbox", str, true, "checkbox"},
		{"auto-scroll", none, true, "auto-scroll"},
	}},
	{"color-selection", []entry{
		{"color-selection", none, false, "dialog"},
		{"color", str, false, "color"},
		{"show-palette", none, false, "show-palette"},
	}},
	{"password", []entry{
		{"password", none, false, "dialog"},
		{"username", none, false, "username"},
	}},
	{"forms", []entry{
		{"forms", none, false, "dialog"},
		{"add-entry", field, false, "add-entry"},
		{"add-password", field, false, "add-password"},
		{"add-multiline-entry", field, false, "add-multiline-entry"},
		{"add-calendar", field, false, "add-calendar"},
		{"add-list", field, false, "add-list"},
		{"list-values", strs, false, "list-values"},
		{"column-values", strs, false, "column-values"},
		{"add-combo", field, false, "add-combo"},
		// Reached as --value only straight after an --add-entry or
		// --add-multiline-entry; anywhere else --value is the scale's.
		{"value", fieldText, false, "field-text"},
		{"combo-values", strs, false, "combo-values"},
		{"show-header", none, false, "show-header"},
		{"text", str, true, "text"},
		{"separator", str, true, "separator"},
		// Not --date-format, which GOption finds in the calendar's group
		// first: this one is reached only as --forms-date-format.
		{"date-format", str, false, "forms-date-format"},
	}},
	{"misc", []entry{
		{"about", none, false, "dialog"},
		{"version", none, false, "dialog"},
	}},
}

func messageEntries(name string) []entry {
	return []entry{
		{name, none, false, "dialog"},
		{"text", str, true, "text"},
		{"icon", str, true, "icon"},
		{"no-wrap", none, true, "no-wrap"},
		{"no-markup", none, true, "no-markup"},
		{"ellipsize", none, true, "ellipsize"},
	}
}

// lookup finds an option as GOption does: by its name in the first group
// that has it, else as --GROUP-NAME, for a group whose name starts with
// GROUP, among the entries that allow an alias.
func lookup(name string) (entry, bool) {
	for _, g := range groups {
		for _, e := range g.entries {
			if e.name == name {
				return e, true
			}
		}
	}
	dash := strings.IndexByte(name, '-')
	if dash <= 0 {
		return entry{}, false
	}
	prefix, rest := name[:dash], name[dash+1:]
	for _, g := range groups {
		if !strings.HasPrefix(g.name, prefix) {
			continue
		}
		for _, e := range g.entries {
			if e.name == rest && !e.noAlias {
				return e, true
			}
		}
	}
	return entry{}, false
}

func lookupIn(groupName, name string) (entry, bool) {
	for _, g := range groups {
		if g.name != groupName {
			continue
		}
		for _, e := range g.entries {
			if e.name == name {
				return e, true
			}
		}
	}
	return entry{}, false
}

// read is what a command line set.
type read struct {
	dialogs map[string]bool
	set     map[string]bool
	str     map[string]string
	ints    map[string]int
	lists   map[string][]string
	// The forms' fields, in the order given.
	fields     []formField
	positional []string
	help       bool
	show       bool
}

type formField struct{ option, label, text string }

// parse reads argv as GOption does. Any mistake is GOption's error, which
// zenity's error hook turns into its one message for all of them.
func parse(argv []string) (*read, error) {
	r := &read{dialogs: map[string]bool{}, set: map[string]bool{}, str: map[string]string{},
		ints: map[string]int{}, lists: map[string][]string{}}
	syntax := &Error{Message: errSyntax}
	// The field an --add-entry or --add-multiline-entry just added, or -1.
	entryField := -1
	// A bare --value given to a field, which is the scale's after all
	// unless the dialog is --forms.
	var fieldValue *string
	for i := 0; i < len(argv); i++ {
		a := argv[i]
		prev := entryField
		entryField = -1
		if a == "--" {
			r.positional = append(r.positional, argv[i+1:]...)
			break
		}
		if a == "-h" || a == "-?" || strings.HasPrefix(a, "--help") {
			r.help = true
			continue
		}
		if !strings.HasPrefix(a, "-") || a == "-" {
			r.positional = append(r.positional, a)
			continue
		}
		if !strings.HasPrefix(a, "--") {
			return nil, syntax
		}
		name, value, hasValue := strings.Cut(a[2:], "=")
		switch name {
		case "show":
			// galley's own: bring the window forward.
			r.show = true
			continue
		case "html", "url":
			return nil, &Error{Message: fmt.Sprintf("--%s is not supported by galley: %s", name, refused[name])}
		}
		e, ok := lookup(name)
		if !ok {
			return nil, syntax
		}
		if name == "value" && prev >= 0 {
			e, _ = lookupIn("forms", "value")
		}
		if e.arg == none {
			// GOption takes --flag=anything as the flag.
			if e.key == "dialog" {
				r.dialogs[e.name] = true
			} else {
				r.set[e.key] = true
			}
			continue
		}
		if !hasValue {
			if i+1 >= len(argv) {
				return nil, syntax
			}
			i++
			value = argv[i]
		}
		switch e.arg {
		case integer:
			if e.key == "value" {
				fieldValue = nil
			}
			n, ok := strtol(value)
			if !ok {
				return nil, syntax
			}
			r.ints[e.key] = n
		case strs:
			r.lists[e.key] = append(r.lists[e.key], value)
		case field:
			r.fields = append(r.fields, formField{option: e.name, label: value})
			if e.name == "add-entry" || e.name == "add-multiline-entry" {
				entryField = len(r.fields) - 1
			}
		case fieldText:
			if prev < 0 {
				return nil, syntax
			}
			r.fields[prev].text = value
			if name == "value" {
				fieldValue = &value
			}
			continue
		default:
			r.str[e.key] = value
		}
		r.set[e.key] = true
	}
	if fieldValue != nil && !r.dialogs["forms"] {
		n, ok := strtol(*fieldValue)
		if !ok {
			return nil, syntax
		}
		r.ints["value"] = n
		r.set["value"] = true
	}
	return r, nil
}

// refused are what galley knows of zenity and never does.
var refused = map[string]string{
	"html": "galley shows text as text and never renders HTML",
	"url":  "galley shows text as text and never loads a URL",
}

// strtol is GOption's reading of an integer: C's strtol in base 0 --
// leading space, a sign, 0x for hex and 0 for octal -- with nothing after
// it, and within an int.
func strtol(s string) (int, bool) {
	i := 0
	for i < len(s) && (s[i] == ' ' || (s[i] >= '\t' && s[i] <= '\r')) {
		i++
	}
	neg := false
	if i < len(s) && (s[i] == '+' || s[i] == '-') {
		neg = s[i] == '-'
		i++
	}
	base := 10
	if i+1 < len(s) && s[i] == '0' && (s[i+1] == 'x' || s[i+1] == 'X') && i+2 < len(s) && hexDigit(s[i+2]) >= 0 {
		base = 16
		i += 2
	} else if i < len(s) && s[i] == '0' {
		base = 8
	}
	start := i
	var n int64
	for ; i < len(s); i++ {
		d := hexDigit(s[i])
		if d < 0 || d >= base {
			break
		}
		n = n*int64(base) + int64(d)
		if n > 1<<40 {
			return 0, false
		}
	}
	if i == start || i != len(s) {
		return 0, false
	}
	if neg {
		n = -n
	}
	if n > 1<<31-1 || n < -1<<31 {
		return 0, false
	}
	return int(n), true
}

func hexDigit(c byte) int {
	switch {
	case c >= '0' && c <= '9':
		return int(c - '0')
	case c >= 'a' && c <= 'f':
		return int(c-'a') + 10
	case c >= 'A' && c <= 'F':
		return int(c-'A') + 10
	}
	return -1
}

func unsupported(key string) error {
	return &Error{Message: fmt.Sprintf("--%s is not supported for this dialog", printed(key))}
}

// printed is the name zenity's message gives an option: the entry's own
// name, found by its variable in one group's table -- which for --no-wrap
// and --ellipsize is a table without them, so C's "(null)".
func printed(key string) string {
	switch key {
	case "forms-date-format":
		return "date-format"
	case "no-wrap", "ellipsize":
		return "(null)"
	}
	return key
}

// settle is zenity's checking after parsing: each group's post-parse hook in
// turn, which takes its dialog as the mode -- two are an error -- and
// refuses its options when the mode is not its dialog; then the checks of
// options several dialogs share. It returns the dialog, and the warnings
// zenity prints on the way.
func settle(r *read) (mode string, warnings []string, err error) {
	// What is said before a mistake is found is said all the same.
	defer func() {
		if e, ok := err.(*Error); ok {
			e.Warnings = warnings
		}
	}()
	setMode := func(name string) error {
		if r.dialogs[name] {
			if mode != "" {
				return &Error{Message: errDialogs}
			}
			mode = name
		}
		return nil
	}
	// refuse names the first option set of those given, when the mode is
	// not the group's own.
	refuse := func(own string, names ...string) error {
		if mode == own {
			return nil
		}
		for _, n := range names {
			if r.set[n] {
				return unsupported(n)
			}
		}
		return nil
	}
	for _, g := range groups {
		switch g.name {
		case "general":
			if r.set["window-icon"] {
				warnings = append(warnings, "Warning: --window-icon is deprecated and will be removed in a future version of zenity; Treating as --icon.")
			}
			if r.set["icon-name"] {
				warnings = append(warnings, "Warning: --icon-name is deprecated and will be removed in a future version of zenity; Treating as --icon.")
			}
			if r.ints["attach"] != 0 {
				warnings = append(warnings, "Warning: --attach is deprecated and will be removed in a future version of zenity. Ignoring.")
			}
			continue
		case "misc":
			if err := setMode("about"); err != nil {
				return "", warnings, err
			}
			if err := setMode("version"); err != nil {
				return "", warnings, err
			}
			continue
		}
		if err := setMode(g.name); err != nil {
			return "", warnings, err
		}
		switch g.name {
		case "calendar":
			// Days, months and years are refused only above -1, zenity's
			// "not given".
			if mode != "calendar" {
				for _, n := range []string{"day", "month", "year"} {
					if r.set[n] && r.ints[n] > -1 {
						return "", warnings, unsupported(n)
					}
				}
			}
			err = refuse("calendar", "date-format")
		case "entry":
			err = refuse("entry", "entry-text", "hide-text")
		case "file-selection":
			err = refuse("file-selection", "directory", "save", "file-filter")
			if err == nil && r.set["confirm-overwrite"] {
				warnings = append(warnings, "Warning: --confirm-overwrite is deprecated and will be removed in a future version of zenity. Ignoring.")
			}
		case "list":
			err = refuse("list", "column", "checklist", "radiolist", "imagelist",
				"print-column", "hide-column", "hide-header", "mid-search")
		case "notification":
			err = refuse("notification", "listen")
			if err == nil && r.set["hint"] {
				warnings = append(warnings, "Warning: --hint is deprecated and will be removed in a future version of zenity. Ignoring.")
			}
		case "progress":
			if mode != "progress" {
				for _, n := range []string{"pulsate", "percentage", "auto-close", "auto-kill", "no-cancel", "time-remaining"} {
					// A percentage of 0 is zenity's "not given".
					if r.set[n] && (n != "percentage" || r.ints[n] != 0) {
						return "", warnings, unsupported(n)
					}
				}
			}
		case "question":
			if r.set["switch"] && len(r.lists["extra-button"]) == 0 {
				err = &Error{Message: errSyntax}
			}
		case "text-info":
			err = refuse("text-info", "font")
		case "color-selection":
			err = refuse("color-selection", "color", "show-palette")
		case "password":
			err = refuse("password", "username")
		case "forms":
			err = refuse("forms", "forms-date-format", "list-values", "column-values", "combo-values", "show-header")
		}
		if err != nil {
			return "", warnings, err
		}
	}

	in := func(modes ...string) bool {
		for _, m := range modes {
			if m == mode {
				return true
			}
		}
		return false
	}
	switch {
	case r.set["text"] && in("about", "version"):
		return "", warnings, unsupported("text")
	case r.set["separator"] && r.str["separator"] != "|" && !in("list", "file-selection", "forms"):
		return "", warnings, unsupported("separator")
	case r.set["multiple"] && !in("file-selection", "list"):
		return "", warnings, unsupported("multiple")
	case r.set["editable"] && !in("text-info", "list"):
		return "", warnings, unsupported("editable")
	case r.set["filename"] && !in("file-selection", "text-info"):
		return "", warnings, unsupported("filename")
	case r.set["ok-label"] && in("file-selection"):
		return "", warnings, unsupported("ok-label")
	case r.set["cancel-label"] && in("file-selection", "error", "warning", "info"):
		return "", warnings, unsupported("cancel-label")
	case r.set["no-wrap"] && !in("info", "error", "question", "warning", "text-info"):
		return "", warnings, unsupported("no-wrap")
	case r.set["ellipsize"] && !in("info", "error", "question", "warning"):
		return "", warnings, unsupported("ellipsize")
	}
	if mode == "" {
		return "", warnings, &Error{Message: errNoMode}
	}
	return mode, warnings, nil
}
