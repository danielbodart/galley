// Package zenity reads zenity's command line, the one galley is a drop-in
// for, and turns it into the item the window shows and the way its answer
// is printed and exited with.
//
// The reading follows zenity 4.2's own (src/option.c): GOption's syntax --
// --name=value or --name value, options and arguments in any order, -- to
// end the options, --GROUP-NAME for an option of a group -- the same checks
// after parsing, in the same order, and the same words for the same
// mistakes. What zenity decides once its dialog is up -- a list with no
// columns, a scale whose value is out of its range -- is decided here too,
// and said and exited as zenity does.
package zenity

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
	"unicode/utf16"
	"unicode/utf8"

	"github.com/danielbodart/galley/internal/wire"
)

// Error is a command line zenity would refuse, or one galley does. Its
// message is printed as it is, after the warnings zenity printed on its way
// to finding it, and the exit is zenity's for an error.
type Error struct {
	Message  string
	Warnings []string
}

func (e *Error) Error() string { return e.Message }

// zenity's own words, from option.c. GOption's own errors -- an unknown
// option, a missing argument, a number that is not one -- all reach
// zenity's error hook, which says only the first.
const (
	errSyntax  = "This option is not available. Please see --help for all possible usages."
	errDialogs = "Two or more dialog options specified"
	errNoMode  = "You must specify a dialog type. See 'zenity --help' for details"
)

// Invocation is a command line, read.
type Invocation struct {
	Help    bool
	Version bool
	// Show asks the window to come forward; galley's own option.
	Show bool

	// Warnings are what zenity prints to stderr on its way to the dialog.
	Warnings []string
	// Failure is what zenity says and exits with once its dialog has
	// started, when it finds it cannot show it; there is no item then.
	Failure *Failure

	Item wire.Item
	// Extra is each --extra-button as given, which is what zenity prints
	// when one is pressed -- its mnemonic underscores and all.
	Extra []string
	// Timeout is --timeout in seconds, 0 for none.
	Timeout int
	// Filename is text-info's --filename; empty means stdin.
	Filename string

	// Separator is what joins a list's, a form's or a file chooser's
	// values, as zenity uses it for each.
	Separator string
	// List is how a list's answer is printed, and its stdin read.
	List *ListOutput
	// Progress is how a progress item's stdin is read.
	Progress *ProgressOptions
	// Listen is a --notification --listen, whose stdin is commands.
	Listen bool
}

// Failure is a dialog zenity gives up on after starting it.
type Failure struct {
	Message string
	// Exit is the status zenity ends with, or -1 for its error code, which
	// ZENITY_ERROR overrides.
	Exit int
}

// ListOutput is what a list prints of the rows it is answered with.
type ListOutput struct {
	// Columns is how many cells make a row.
	Columns int
	// Print are the columns printed, counted from 1; All prints every
	// column but a check or radio list's first.
	Print  []int
	All    bool
	Toggle bool
	// Images makes the first cell of each row read from stdin a path,
	// made absolute from Cwd.
	Images bool
	Cwd    string
}

// ProgressOptions are a progress item's options that its stdin acts on.
type ProgressOptions struct {
	AutoClose     bool
	AutoKill      bool
	NoCancel      bool
	TimeRemaining bool
	Percentage    float64
}

// Parse reads argv, without the program name. cwd makes a path absolute,
// since the window does not run where the caller does; now is today, for a
// calendar given only part of a date.
func Parse(argv []string, cwd string) (*Invocation, error) {
	return parseAt(argv, cwd, time.Now())
}

func parseAt(argv []string, cwd string, now time.Time) (*Invocation, error) {
	r, err := parse(argv)
	if err != nil {
		return nil, err
	}
	inv := &Invocation{}
	if r.help || r.show {
		inv.Help, inv.Show = r.help, r.show
		return inv, nil
	}
	mode, warnings, err := settle(r)
	if err != nil {
		return nil, err
	}
	inv.Warnings = warnings
	if mode == "version" {
		inv.Version = true
		return inv, nil
	}

	kind := kinds[mode]
	item := wire.Item{Kind: kind, Default: -1}
	item.Title = clip(r.str["title"], maxTitle)
	if !r.set["title"] {
		item.Title = defaultTitle[kind]
	}
	if t, ok := r.ints["timeout"]; ok && t > 0 {
		inv.Timeout = t
	}
	// zenity takes any size and GTK makes what it can of it; the window
	// takes -1 (none) to 100000, so a size beyond either end is that end.
	if w, ok := r.ints["width"]; ok {
		item.Width = clamp(w, -1, maxSize)
	}
	if h, ok := r.ints["height"]; ok {
		item.Height = clamp(h, -1, maxSize)
	}
	inv.Extra = r.lists["extra-button"]
	inv.Separator = "|"
	if r.set["separator"] {
		inv.Separator = r.str["separator"]
	}
	text, hasText := r.str["text"], r.set["text"]
	// --text as a dialog's body is markup with its escapes undone, but for
	// the message dialogs' --no-markup; each dialog's own text is plain.
	body := func(fallback string) {
		if hasText {
			item.Text, item.Markup = Compress(text), true
		} else {
			item.Text = fallback
		}
	}
	// The deprecated names for --icon are --icon, later ones winning.
	iconName := r.str["icon"]
	if r.set["window-icon"] {
		iconName = r.str["window-icon"]
	}
	if r.set["icon-name"] {
		iconName = r.str["icon-name"]
	}
	item.Icon = defaultIcon[kind]

	// What a dialog's buttons are made of: zenity's labels, unless given.
	ok := func(fallback string) string { return or(r.set["ok-label"], r.str["ok-label"], fallback) }
	cancel := func(fallback string) string { return or(r.set["cancel-label"], r.str["cancel-label"], fallback) }
	var spec []wire.Button
	okCancel := func(okLabel string) {
		spec = append(spec,
			wire.Button{Answer: wire.AnswerCancel, Label: cancel("_Cancel")},
			wire.Button{Answer: wire.AnswerOK, Label: ok(okLabel)})
		item.Default = 1
	}
	extras := true

	switch kind {
	case wire.KindQuestion, wire.KindInfo, wire.KindWarning, wire.KindError:
		if !hasText {
			// The dialog's own text is a plain label in zenity's .ui file.
			item.Text = defaultText[kind]
		} else if r.set["no-markup"] {
			// zenity sets it as it is: no escapes, no markup.
			item.Text = text
		} else {
			item.Text, item.Markup = Compress(text), true
		}
		item.NoWrap = r.set["no-wrap"]
		item.Ellipsize = r.set["ellipsize"]
		item.Icon = icon(iconName, kind, cwd)
		if kind == wire.KindQuestion {
			if !r.set["switch"] {
				spec = append(spec,
					wire.Button{Answer: wire.AnswerCancel, Label: cancel("_No")},
					wire.Button{Answer: wire.AnswerOK, Label: ok("_Yes")})
				item.Default = 1
				if r.set["default-cancel"] {
					item.Default = 0
				}
			}
		} else {
			spec = append(spec, wire.Button{Answer: wire.AnswerOK, Label: ok("_OK")})
			item.Default = 0
		}

	case wire.KindEntry:
		if hasText {
			text = Compress(text)
		} else {
			text = "_Enter new text:"
		}
		item.Text, _, _ = Mnemonic(text)
		item.Entry = &wire.Entry{Text: r.str["entry-text"], Hidden: r.set["hide-text"]}
		// zenity counts the values after the options, and --entry-text as
		// one more: more than one, and the entry is a combo of them, the
		// --entry-text first and chosen. One alone is a plain entry, and a
		// lone value after the options is not used at all.
		n := len(r.positional)
		if r.set["entry-text"] {
			n++
		}
		if n > 1 {
			values := []string{}
			if r.set["entry-text"] {
				values = append(values, r.str["entry-text"])
			}
			item.Entry.Values = append(values, r.positional...)
			item.Entry.Hidden = false
		}
		okCancel("_OK")

	case wire.KindText:
		// zenity's text-info has no --text: it is read and ignored, as is
		// --font, which galley leaves to the theme (see the README).
		item.NoWrap = r.set["no-wrap"]
		item.Info = &wire.Info{Checkbox: clip(r.str["checkbox"], maxCheckbox),
			AutoScroll: r.set["auto-scroll"], Editable: r.set["editable"]}
		inv.Filename = r.str["filename"]
		okCancel("_OK")

	case wire.KindList:
		if r.set["mid-search"] {
			inv.Warnings = append(inv.Warnings, "Warning: --mid-search is deprecated and will be removed in a future version of zenity. Ignoring.")
		}
		columns := r.lists["column"]
		toggle := r.set["checklist"] || r.set["radiolist"]
		types := 0
		for _, t := range []string{"checklist", "radiolist", "imagelist"} {
			if r.set[t] {
				types++
			}
		}
		// zenity finds these with its dialog half made, and returns from
		// it having set an exit code nothing reads: it exits 0.
		switch {
		case len(columns) == 0:
			inv.Failure = &Failure{"No column titles specified for List dialog.", 0}
		case toggle && len(columns) < 2:
			inv.Failure = &Failure{"Insufficient columns specified for List dialog (at least 2 are required for --checklist or --radiolist).", 0}
		case types > 1:
			inv.Failure = &Failure{"You should use only one List dialog type.", 0}
		}
		if inv.Failure != nil {
			return inv, nil
		}
		body("Select items from the list below.")
		inv.Separator = Compress(inv.Separator)
		list := &wire.List{Columns: columns, Multiple: r.set["multiple"],
			Editable: r.set["editable"], HideHeader: r.set["hide-header"]}
		switch {
		case r.set["radiolist"]:
			list.Type = wire.ListRadio
		case r.set["checklist"]:
			list.Type = wire.ListCheck
		case r.set["imagelist"]:
			list.Type = wire.ListImage
		}
		out := &ListOutput{Columns: len(columns), Toggle: toggle,
			Images: list.Type == wire.ListImage, Cwd: cwd}
		if r.set["print-column"] && strings.EqualFold(r.str["print-column"], "all") {
			out.All = true
		} else if r.set["print-column"] {
			out.Print = columnIndexes(r.str["print-column"], len(columns))
		} else if toggle {
			out.Print = []int{2}
		} else {
			out.Print = []int{1}
		}
		if r.set["hide-column"] {
			for _, c := range columnIndexes(r.str["hide-column"], len(columns)) {
				list.Hidden = append(list.Hidden, c-1)
			}
		}
		// The values after the options are the rows, a column's worth to
		// each; a short last row is dropped. With none, stdin has them.
		if len(r.positional) > 0 {
			for i := 0; i+len(columns) <= len(r.positional); i += len(columns) {
				list.Rows = append(list.Rows, out.row(r.positional[i:i+len(columns)]))
			}
		} else {
			list.More = true
		}
		item.List = list
		inv.List = out
		okCancel("_OK")

	case wire.KindForms:
		body("Forms dialog")
		forms := &wire.Forms{Fields: []wire.Field{}, DateFormat: "%x"}
		if r.set["forms-date-format"] {
			forms.DateFormat = r.str["forms-date-format"]
		}
		columnValues := r.lists["column-values"]
		if len(columnValues) == 0 {
			columnValues = []string{"column"}
		}
		lists, combos := 0, 0
		for _, f := range r.fields {
			fd := wire.Field{Label: f.label}
			switch f.option {
			case "add-entry":
				fd.Kind = wire.FieldEntry
			case "add-password":
				fd.Kind = wire.FieldPassword
			case "add-multiline-entry":
				fd.Kind = wire.FieldMultiline
			case "add-calendar":
				fd.Kind = wire.FieldCalendar
			case "add-list":
				// Each list takes the --column-values of its own place, or
				// the first; and the --list-values of its own place, filled
				// a row at a time.
				fd.Kind = wire.FieldList
				fd.ShowHeader = r.set["show-header"]
				cv := columnValues[0]
				if lists < len(columnValues) {
					cv = columnValues[lists]
				}
				fd.Columns = strings.Split(cv, "|")
				if lv := r.lists["list-values"]; lists < len(lv) {
					values := strings.Split(lv[lists], "|")
					for i := 0; i < len(values); i += len(fd.Columns) {
						fd.Rows = append(fd.Rows, values[i:min(i+len(fd.Columns), len(values))])
					}
				}
				lists++
			case "add-combo":
				fd.Kind = wire.FieldCombo
				if cv := r.lists["combo-values"]; combos < len(cv) {
					fd.Values = strings.Split(cv[combos], "|")
				}
				combos++
			}
			forms.Fields = append(forms.Fields, fd)
		}
		item.Forms = forms
		okCancel("_OK")

	case wire.KindCalendar:
		body("Select a date from below.")
		// What is not given is today's; a date that is not one is said so
		// and today's instead.
		y, m, d := now.Date()
		day, month, year := int(d), int(m), y
		if v, ok := r.ints["day"]; ok && v >= 0 {
			day = v
		}
		if v, ok := r.ints["month"]; ok && v >= 0 {
			month = v
		}
		if v, ok := r.ints["year"]; ok && v >= 0 {
			year = v
		}
		if (month > 0 || year > 0 || day > 0) && !realDate(year, month, day) {
			inv.Warnings = append(inv.Warnings, "Invalid date provided. Falling back to today's date.")
			day, month, year = int(d), int(m), y
		} else if month <= 0 && year <= 0 && day <= 0 {
			day, month, year = int(d), int(m), y
		}
		format := "%x"
		if r.set["date-format"] {
			format = r.str["date-format"]
		}
		item.Calendar = &wire.Calendar{Day: day, Month: month, Year: year, Format: format}
		okCancel("_OK")

	case wire.KindScale:
		body("Adjust the scale value")
		s := &wire.Scale{Value: 0, Min: 0, Max: 100, Step: 1,
			Hide: r.set["hide-value"], Partial: r.set["print-partial"]}
		if v, ok := r.ints["value"]; ok {
			s.Value = v
		}
		if v, ok := r.ints["min-value"]; ok {
			s.Min = v
		}
		if v, ok := r.ints["max-value"]; ok {
			s.Max = v
		}
		if v, ok := r.ints["step"]; ok {
			s.Step = v
		}
		// As with a list, zenity returns from these with an exit code
		// nothing reads.
		if s.Min >= s.Max {
			inv.Failure = &Failure{"Maximum value must be greater than minimum value.", 0}
			return inv, nil
		}
		if s.Value < s.Min || s.Value > s.Max {
			inv.Failure = &Failure{"Value out of range.", 0}
			return inv, nil
		}
		item.Scale = s
		okCancel("_OK")

	case wire.KindPassword:
		// zenity's password dialog has no --text of its own.
		item.Password = &wire.Password{Username: r.set["username"]}
		item.Text = "Type your password"
		if r.set["username"] {
			item.Text = "Type your username and password"
		}
		okCancel("_OK")

	case wire.KindColor:
		item.Color = &wire.Color{Color: r.str["color"], Palette: r.set["show-palette"]}
		item.Text = ""
		okCancel("_Select")

	case wire.KindFile:
		if len(inv.Extra) > 0 {
			inv.Warnings = append(inv.Warnings, "Warning: the --extra-button option for --file-selection is deprecated and will be removed in a future version of zenity. Ignoring.")
		}
		extras = false
		file := &wire.File{Mode: wire.FileOpen, Multiple: r.set["multiple"]}
		if r.set["directory"] {
			file.Mode = wire.FileDirectory
		} else if r.set["save"] {
			file.Mode = wire.FileSave
		}
		if name := r.str["filename"]; name != "" {
			path := name
			if !filepath.IsAbs(path) {
				path = filepath.Join(cwd, path)
			}
			base := filepath.Base(path)
			dir := strings.HasSuffix(name, "/")
			// A name with a directory in it starts the chooser there: in
			// it, when it ends with a slash, else in its parent.
			if base != name {
				if dir {
					file.Folder = filepath.Clean(path)
				} else {
					file.Folder = filepath.Dir(path)
				}
			}
			if file.Mode == wire.FileSave && !dir {
				file.Name = base
			}
		}
		for _, f := range r.lists["file-filter"] {
			file.Filters = append(file.Filters, fileFilter(f))
		}
		item.File = file
		body(fileText(file))
		item.Title = clip(r.str["title"], maxTitle)
		if !r.set["title"] {
			item.Title = fileTitle(file)
		}
		item.Icon = fileIcon[file.Mode]
		spec = append(spec,
			wire.Button{Answer: wire.AnswerCancel, Label: "_Cancel"},
			wire.Button{Answer: wire.AnswerOK, Label: fileButton[file.Mode]})
		item.Default = 1

	case wire.KindProgress:
		body("Running...")
		p := &ProgressOptions{AutoClose: r.set["auto-close"], AutoKill: r.set["auto-kill"],
			NoCancel: r.set["no-cancel"], TimeRemaining: r.set["time-remaining"],
			Percentage: float64(r.ints["percentage"])}
		if p.AutoClose && r.ints["percentage"] == 100 {
			inv.Failure = &Failure{"Combining the options --auto-close and --percentage=100 is not supported.", -1}
			return inv, nil
		}
		inv.Progress = p
		item.Progress = &wire.Progress{Percentage: min(max(p.Percentage, 0), 100),
			Pulsate: r.set["pulsate"], TimeRemaining: p.TimeRemaining}
		// Cancel unless --no-cancel; OK unless --auto-close, and only once
		// it is done.
		if !p.NoCancel {
			spec = append(spec, wire.Button{Answer: wire.AnswerCancel, Label: cancel("_Cancel")})
		}
		if !p.AutoClose {
			done := r.ints["percentage"] == 100
			spec = append(spec, wire.Button{Answer: wire.AnswerOK, Label: ok("_OK"), Disabled: !done})
			if done {
				item.Default = len(spec) - 1
			}
		}

	case wire.KindNotification:
		inv.Listen = r.set["listen"]
		item.Note = &wire.Note{Listen: inv.Listen}
		extras = false
		if !inv.Listen {
			// zenity exits 1 with no text to show; with an empty one, it
			// says it cannot parse it first.
			if !hasText {
				inv.Failure = &Failure{"", 1}
				return inv, nil
			}
			if Compress(text) == "" {
				inv.Failure = &Failure{"Could not parse message", 1}
				return inv, nil
			}
			item.Text = Compress(text)
			item.Icon = icon(iconName, kind, cwd)
		}
		spec = append(spec, wire.Button{Answer: wire.AnswerOK, Label: "_Dismiss"})
		item.Default = 0

	case wire.KindAbout:
		// zenity's about window has no timeout, nor extra buttons.
		inv.Timeout = 0
		extras = false
		spec = append(spec, wire.Button{Answer: wire.AnswerOK, Label: "_Close"})
		item.Default = 0
	}

	if extras {
		for i, label := range inv.Extra {
			spec = append(spec, wire.Button{Answer: wire.AnswerExtra, Index: i, Label: label})
		}
	} else {
		inv.Extra = nil
	}
	if len(spec) > maxButtons {
		return nil, &Error{Message: fmt.Sprintf("galley shows at most %d buttons, and this dialog would have %d", maxButtons, len(spec))}
	}
	item.Buttons = Buttons(spec)
	for i, b := range item.Buttons {
		item.Buttons[i].Disabled = spec[i].Disabled
		item.Buttons[i].Label = clip(b.Label, maxLabel)
		if b.Underline >= utf8.RuneCountInString(item.Buttons[i].Label) {
			item.Buttons[i].Underline = -1
		}
	}
	inv.Item = item
	return inv, nil
}

// The dialogs, by zenity's flag, as the kind of item each becomes.
var kinds = map[string]string{
	"question": wire.KindQuestion, "info": wire.KindInfo, "warning": wire.KindWarning,
	"error": wire.KindError, "entry": wire.KindEntry, "text-info": wire.KindText,
	"list": wire.KindList, "forms": wire.KindForms, "calendar": wire.KindCalendar,
	"scale": wire.KindScale, "password": wire.KindPassword, "color-selection": wire.KindColor,
	"file-selection": wire.KindFile, "progress": wire.KindProgress,
	"notification": wire.KindNotification, "about": wire.KindAbout,
}

// Each dialog's heading and text when none is given, and its icon, from
// zenity's .ui file and source. Where zenity's dialog has no heading -- the
// form, the password and the file chooser's, whose title is the portal's --
// galley names it, since the queue groups by it.
var defaultTitle = map[string]string{
	wire.KindQuestion: "Question", wire.KindInfo: "Information", wire.KindWarning: "Warning",
	wire.KindError: "Error", wire.KindEntry: "Add a new entry", wire.KindText: "Text View",
	wire.KindList: "Select items from the list", wire.KindForms: "Forms dialog",
	wire.KindCalendar: "Calendar selection", wire.KindScale: "Adjust the scale value",
	wire.KindPassword: "Password", wire.KindColor: "Select a Color",
	wire.KindProgress: "Progress", wire.KindNotification: "Notification",
	wire.KindAbout: "About galley",
}

var defaultText = map[string]string{
	wire.KindQuestion: "Are you sure you want to proceed?", wire.KindInfo: "All updates are complete.",
	wire.KindWarning: "Are you sure you want to proceed?", wire.KindError: "An error has occurred.",
}

var defaultIcon = map[string]string{
	wire.KindQuestion: "dialog-question", wire.KindInfo: "dialog-information",
	wire.KindWarning: "dialog-warning", wire.KindError: "dialog-error",
	wire.KindEntry: "insert-text", wire.KindText: "accessories-text-editor",
	wire.KindList: "view-list", wire.KindForms: "document-edit",
	wire.KindCalendar: "x-office-calendar", wire.KindScale: "dialog-question",
	wire.KindPassword: "dialog-password", wire.KindColor: "applications-graphics",
	wire.KindProgress: "appointment-soon", wire.KindNotification: "dialog-information",
	wire.KindAbout: "help-about",
}

var fileIcon = map[string]string{
	wire.FileOpen: "document-open", wire.FileSave: "document-save-as", wire.FileDirectory: "folder-open",
}

var fileButton = map[string]string{
	wire.FileOpen: "_Open…", wire.FileSave: "_Save…", wire.FileDirectory: "_Select…",
}

func fileTitle(f *wire.File) string {
	switch {
	case f.Mode == wire.FileSave:
		return "Save File"
	case f.Mode == wire.FileDirectory && f.Multiple:
		return "Select Folders"
	case f.Mode == wire.FileDirectory:
		return "Select Folder"
	case f.Multiple:
		return "Open Files"
	}
	return "Open File"
}

// fileText says what a file chooser is asked for, where the dialog has no
// --text: zenity's chooser says so by being one.
func fileText(f *wire.File) string {
	switch {
	case f.Mode == wire.FileSave:
		return "Choose where to save a file."
	case f.Mode == wire.FileDirectory && f.Multiple:
		return "Choose one or more folders."
	case f.Mode == wire.FileDirectory:
		return "Choose a folder."
	case f.Multiple:
		return "Choose one or more files."
	}
	return "Choose a file."
}

// fileFilter reads a --file-filter as zenity does: "NAME | PATTERN ...", or
// patterns alone, named by themselves.
func fileFilter(s string) wire.Filter {
	name, patterns, found := strings.Cut(s, "|")
	if found {
		name = strings.TrimSpace(name)
	} else {
		name, patterns = s, s
	}
	f := wire.Filter{Name: name, Patterns: []string{}}
	for _, p := range strings.Split(strings.TrimLeft(patterns, " "), " ") {
		if p != "" {
			f.Patterns = append(f.Patterns, p)
		}
	}
	return f
}

// columnIndexes reads --print-column or --hide-column: numbers separated by
// commas, each read by atoi, those outside 1 to n dropped.
func columnIndexes(s string, n int) []int {
	out := []int{}
	for _, part := range strings.Split(s, ",") {
		if i := atoiC(part); i > 0 && i <= n {
			out = append(out, i)
		}
	}
	return out
}

// row is one of a list's rows as the window gets it: an image list's first
// cell a path made absolute, since the window has its own directory.
func (o *ListOutput) row(cells []string) []string {
	row := append([]string(nil), cells...)
	if o.Images && len(row) > 0 && row[0] != "" && !filepath.IsAbs(row[0]) {
		row[0] = filepath.Join(o.Cwd, row[0])
	}
	return row
}

// realDate is whether GLib would make a date of these.
func realDate(year, month, day int) bool {
	if year < 1 || year > 9999 || month < 1 || month > 12 || day < 1 {
		return false
	}
	return day <= time.Date(year, time.Month(month)+1, 0, 0, 0, 0, 0, time.UTC).Day()
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

// Icon is icon, for a listening notification's icon: commands.
func Icon(name, cwd string) string {
	if name == "" {
		return ""
	}
	return icon(name, wire.KindNotification, cwd)
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

// Version is the zenity whose command line galley reads.
const Version = "4.2.2"
