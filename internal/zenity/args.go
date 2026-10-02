// Package zenity reads zenity's command line, the one galley is a drop-in
// for, and turns it into the item the window shows and the way its answer
// is printed and exited with.
//
// The reading follows zenity 4.2's own (src/option.c): GOption's syntax --
// --name=value or --name value, options and arguments in any order, -- to
// end the options -- every option zenity knows accepted with every dialog
// except the few it checks after parsing, and the same words for the same
// mistakes. What galley does not implement is refused by name rather than
// shown as something else: a dialog galley cannot stack, or an option that
// would change what is printed.
package zenity

import (
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"unicode/utf16"
	"unicode/utf8"

	"github.com/danielbodart/galley/internal/wire"
)

// Error is a command line zenity would refuse, or one galley does. Its
// message is printed as it is, and the exit is zenity's for an error.
type Error struct{ Message string }

func (e *Error) Error() string { return e.Message }

// zenity's own words, from option.c.
const (
	errSyntax  = "This option is not available. Please see --help for all possible usages."
	errDialogs = "Two or more dialog options specified"
	errNoMode  = "You must specify a dialog type. See 'zenity --help' for details"
)

func unsupported(name string) error {
	return &Error{fmt.Sprintf("--%s is not supported for this dialog", name)}
}

// Invocation is a command line, read.
type Invocation struct {
	Help    bool
	Version bool
	// Show asks the window to come forward; galley's own option.
	Show bool

	Item wire.Item
	// Extra is each --extra-button as given, which is what zenity prints
	// when one is pressed -- its mnemonic underscores and all.
	Extra []string
	// Timeout is --timeout in seconds, 0 for none.
	Timeout int
	// Filename is text-info's --filename; empty means stdin.
	Filename string
}

type arg int

const (
	none arg = iota
	str
	integer
	strs
)

// Every option zenity 4.2 has, with what it takes. A dialog's flag is here
// too, as a mode.
var options = map[string]arg{
	// General.
	"title": str, "window-icon": str, "icon-name": str, "width": integer,
	"height": integer, "timeout": integer, "ok-label": str, "cancel-label": str,
	"extra-button": strs, "modal": none, "attach": integer,
	// Shared by several dialogs.
	"text": str, "icon": str, "no-wrap": none, "no-markup": none,
	"ellipsize": none, "filename": str, "multiple": none, "separator": str,
	"editable": none, "date-format": str,
	// Calendar.
	"day": integer, "month": integer, "year": integer,
	// Entry.
	"entry-text": str, "hide-text": none,
	// File selection.
	"directory": none, "save": none, "file-filter": strs, "confirm-overwrite": none,
	// List.
	"column": strs, "checklist": none, "radiolist": none, "imagelist": none,
	"print-column": str, "hide-column": str, "hide-header": none, "mid-search": none,
	// Notification.
	"listen": none, "hint": strs,
	// Progress.
	"percentage": integer, "pulsate": none, "auto-close": none, "auto-kill": none,
	"no-cancel": none, "time-remaining": none,
	// Question.
	"default-cancel": none, "switch": none,
	// Text.
	"font": str, "checkbox": str, "html": none, "no-interaction": none, "url": str,
	"auto-scroll": none,
	// Scale.
	"value": integer, "min-value": integer, "max-value": integer, "step": integer,
	"print-partial": none, "hide-value": none,
	// Forms.
	"add-entry": str, "add-password": str, "add-multiline-entry": str,
	"add-calendar": str, "add-list": str, "list-values": strs, "column-values": strs,
	"add-combo": str, "combo-values": strs, "show-header": none,
	// Password.
	"username": none,
	// Colour selection.
	"color": str, "show-palette": none,
}

// The dialogs. Those galley stacks map to the item kind they become.
var modes = map[string]string{
	"question": wire.KindQuestion, "info": wire.KindInfo, "warning": wire.KindWarning,
	"error": wire.KindError, "entry": wire.KindEntry, "text-info": wire.KindText,
	"calendar": "", "file-selection": "", "list": "", "notification": "",
	"progress": "", "scale": "", "forms": "", "password": "", "color-selection": "",
	"about": "",
}

// Options galley knows but does not implement, each because it changes what
// the dialog prints or shows in a way a stacked item does not do.
var refused = map[string]string{
	"editable": "an editable text-info would print its text, and galley's is read-only",
	"html":     "galley shows text as text and never renders HTML",
	"url":      "galley shows text as text and never loads a URL",
}

// Parse reads argv, without the program name. cwd makes an --icon path
// absolute, since the window does not run where the caller does.
func Parse(argv []string, cwd string) (*Invocation, error) {
	inv := &Invocation{}
	var mode string
	given := map[string]string{}
	set := map[string]bool{}
	var positional []string

	for i := 0; i < len(argv); i++ {
		a := argv[i]
		if a == "--" {
			positional = append(positional, argv[i+1:]...)
			break
		}
		if a == "-h" || a == "-?" || strings.HasPrefix(a, "--help") {
			inv.Help = true
			continue
		}
		if !strings.HasPrefix(a, "-") || a == "-" {
			positional = append(positional, a)
			continue
		}
		if !strings.HasPrefix(a, "--") {
			return nil, &Error{errSyntax}
		}
		name, value, hasValue := strings.Cut(a[2:], "=")
		switch name {
		case "version":
			inv.Version = true
			continue
		case "show":
			inv.Show = true
			continue
		}
		if _, ok := modes[name]; ok {
			if hasValue {
				return nil, &Error{errSyntax}
			}
			if mode != "" && mode != name {
				return nil, &Error{errDialogs}
			}
			mode = name
			continue
		}
		kind, ok := options[name]
		if !ok {
			return nil, &Error{errSyntax}
		}
		if kind == none {
			if hasValue {
				return nil, &Error{errSyntax}
			}
			set[name] = true
			continue
		}
		if !hasValue {
			if i+1 >= len(argv) {
				return nil, &Error{fmt.Sprintf("Missing argument for --%s", name)}
			}
			i++
			value = argv[i]
		}
		switch kind {
		case integer:
			n, err := strconv.Atoi(strings.TrimSpace(value))
			if err != nil {
				return nil, &Error{fmt.Sprintf("Cannot parse integer value “%s” for --%s", value, name)}
			}
			given[name] = strconv.Itoa(n)
		case strs:
			if name == "extra-button" {
				inv.Extra = append(inv.Extra, value)
			}
			given[name] = value
		default:
			given[name] = value
		}
		set[name] = true
	}

	if inv.Help || inv.Version || inv.Show {
		return inv, nil
	}
	if mode == "" {
		return nil, &Error{errNoMode}
	}
	kind := modes[mode]
	if err := checks(mode, set, given); err != nil {
		return nil, err
	}
	if kind == "" {
		return nil, &Error{fmt.Sprintf("--%s is a dialog galley does not stack: it shows only --question, --info, --warning, --error, --entry and --text-info", mode)}
	}
	for name, why := range refused {
		if set[name] {
			return nil, &Error{fmt.Sprintf("--%s is not supported by galley: %s", name, why)}
		}
	}
	if len(positional) > 0 {
		if kind == wire.KindEntry {
			return nil, &Error{"--entry with a list of values is not supported by galley: it shows a single text field"}
		}
		// zenity ignores what it does not use, and so does galley.
	}

	item := wire.Item{Kind: kind, Default: -1}
	item.Title = clip(given["title"], maxTitle)
	if !set["title"] {
		item.Title = defaultTitle[kind]
	}
	inv.Timeout = atoi(given["timeout"])
	// zenity takes any size and GTK makes what it can of it; the window
	// takes -1 (none) to 100000, so a size beyond either end is that end.
	item.Width = clamp(atoi(given["width"]), -1, maxSize)
	item.Height = clamp(atoi(given["height"]), -1, maxSize)

	text, hasText := given["text"], set["text"]
	switch kind {
	case wire.KindQuestion, wire.KindInfo, wire.KindWarning, wire.KindError:
		if !hasText {
			// The dialog's own text is a plain label in zenity's .ui file.
			item.Text = defaultText[kind]
		} else if set["no-markup"] {
			// zenity sets it as it is: no escapes, no markup.
			item.Text = text
		} else {
			item.Text = Compress(text)
			item.Markup = true
		}
		item.NoWrap = set["no-wrap"]
		item.Ellipsize = set["ellipsize"]
		item.Icon = icon(given["icon"], kind, cwd)
	case wire.KindEntry:
		if !hasText {
			text = "_Enter new text:"
		} else {
			text = Compress(text)
		}
		item.Text, _, _ = Mnemonic(text)
		item.Icon = defaultIcon[kind]
		item.Entry = &wire.Entry{Text: given["entry-text"], Hidden: set["hide-text"]}
	case wire.KindText:
		// zenity's text-info has no --text: it is read and ignored.
		item.Icon = defaultIcon[kind]
		item.NoWrap = set["no-wrap"]
		item.Info = &wire.Info{Checkbox: clip(given["checkbox"], maxCheckbox), AutoScroll: set["auto-scroll"]}
		inv.Filename = given["filename"]
	}

	var spec []wire.Button
	ok, cancel := given["ok-label"], given["cancel-label"]
	switch kind {
	case wire.KindQuestion:
		if !set["switch"] {
			spec = append(spec,
				wire.Button{Answer: wire.AnswerCancel, Label: or(set["cancel-label"], cancel, "_No")},
				wire.Button{Answer: wire.AnswerOK, Label: or(set["ok-label"], ok, "_Yes")})
			item.Default = 1
			if set["default-cancel"] {
				item.Default = 0
			}
		} else if len(inv.Extra) == 0 {
			// A --switch with no buttons at all is still closed in zenity,
			// by Escape or the window's close button, and exits as Escape.
			// Escape only hides galley's window, so the item gets a button
			// that does what zenity's closing does. Not the default: Enter
			// closes nothing in zenity's either.
			spec = append(spec, wire.Button{Answer: wire.AnswerClose, Label: "_Close"})
		}
	case wire.KindInfo, wire.KindWarning, wire.KindError:
		spec = append(spec, wire.Button{Answer: wire.AnswerOK, Label: or(set["ok-label"], ok, "_OK")})
		item.Default = 0
	case wire.KindEntry, wire.KindText:
		spec = append(spec,
			wire.Button{Answer: wire.AnswerCancel, Label: or(set["cancel-label"], cancel, "_Cancel")},
			wire.Button{Answer: wire.AnswerOK, Label: or(set["ok-label"], ok, "_OK")})
		item.Default = 1
	}
	for i, label := range inv.Extra {
		spec = append(spec, wire.Button{Answer: wire.AnswerExtra, Index: i, Label: label})
	}
	if len(spec) > maxButtons {
		return nil, &Error{fmt.Sprintf("galley shows at most %d buttons, and this dialog would have %d", maxButtons, len(spec))}
	}
	item.Buttons = Buttons(spec)
	for i, b := range item.Buttons {
		item.Buttons[i].Label = clip(b.Label, maxLabel)
		if b.Underline >= utf8.RuneCountInString(item.Buttons[i].Label) {
			item.Buttons[i].Underline = -1
		}
	}
	inv.Item = item
	return inv, nil
}

// checks are zenity's, made after it has parsed: an option it knows, given
// with a dialog it means nothing to, in the few cases zenity says so.
func checks(mode string, set map[string]bool, given map[string]string) error {
	in := func(ms ...string) bool {
		for _, m := range ms {
			if m == mode {
				return true
			}
		}
		return false
	}
	switch {
	case set["separator"] && given["separator"] != "|" && !in("list", "file-selection", "forms"):
		return unsupported("separator")
	case set["multiple"] && !in("file-selection", "list"):
		return unsupported("multiple")
	case set["editable"] && !in("text-info", "list"):
		return unsupported("editable")
	case set["filename"] && !in("file-selection", "text-info"):
		return unsupported("filename")
	case set["ok-label"] && in("file-selection"):
		return unsupported("ok-label")
	case set["cancel-label"] && in("file-selection", "error", "warning", "info"):
		return unsupported("cancel-label")
	case set["no-wrap"] && !in("info", "error", "question", "warning", "text-info"):
		return unsupported("no-wrap")
	case set["ellipsize"] && !in("info", "error", "question", "warning"):
		return unsupported("ellipsize")
	}
	return nil
}

// Each dialog's heading and text when none is given, and its icon, from
// zenity's .ui file and source.
var defaultTitle = map[string]string{
	wire.KindQuestion: "Question", wire.KindInfo: "Information", wire.KindWarning: "Warning",
	wire.KindError: "Error", wire.KindEntry: "Add a new entry", wire.KindText: "Text View",
}

var defaultText = map[string]string{
	wire.KindQuestion: "Are you sure you want to proceed?", wire.KindInfo: "All updates are complete.",
	wire.KindWarning: "Are you sure you want to proceed?", wire.KindError: "An error has occurred.",
}

var defaultIcon = map[string]string{
	wire.KindQuestion: "dialog-question", wire.KindInfo: "dialog-information",
	wire.KindWarning: "dialog-warning", wire.KindError: "dialog-error",
	wire.KindEntry: "insert-text", wire.KindText: "accessories-text-editor",
}

// icon is --icon as zenity reads it -- a file if one exists at that path,
// else a theme icon's name -- made absolute, since the window has its own
// working directory.
func icon(name, kind, cwd string) string {
	if name == "" {
		return defaultIcon[kind]
	}
	if _, err := os.Stat(name); err == nil && !filepath.IsAbs(name) {
		return filepath.Join(cwd, name)
	}
	return name
}

// What the window takes of an item (daemon/validate.js), in UTF-16 code
// units for text, as JavaScript counts a string's length. zenity has no
// such limits; what is longer is cut short here, with an ellipsis, rather
// than refused there. An extra button's label is printed as it was given,
// whatever is shown.
const (
	maxTitle    = 1024
	maxLabel    = 256
	maxCheckbox = 1024
	maxButtons  = 16
	maxSize     = 100000
)

// clip cuts s to at most max UTF-16 code units, the last of them an
// ellipsis when anything was cut.
func clip(s string, max int) string {
	if len(utf16.Encode([]rune(s))) <= max {
		return s
	}
	n := 0
	for i, r := range s {
		if n+utf16.RuneLen(r) > max-1 {
			return s[:i] + "…"
		}
		n += utf16.RuneLen(r)
	}
	return s
}

func clamp(n, lo, hi int) int {
	return min(max(n, lo), hi)
}

func or(given bool, value, fallback string) string {
	if given {
		return value
	}
	return fallback
}

func atoi(s string) int {
	n, _ := strconv.Atoi(s)
	return n
}
