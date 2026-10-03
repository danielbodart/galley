// Package wire is what a galley client and the window say to each other over
// the socket: one JSON object a line, in both directions.
//
// The client opens the conversation with a Hello naming either an Item, which
// the window queues until someone answers it, or Show, which asks the window
// to come forward. An item's conversation then stays open until the window
// writes the Answer; while it waits, the client may send Follows -- text-info
// read from stdin, a list's rows from stdin, a progress item's updates, a
// listening notification's messages -- and at most one Timeout. Closing the
// connection withdraws the item: the window drops it without answering, which
// is what makes a killed client's question disappear. A notification is the
// one exception: once queued it is the person's, and stays until dismissed.
//
// Everything the window renders arrives already in its final form: zenity's
// escapes and mnemonics are undone here, in the client, so the window never
// has to know what zenity would have done with a string, and the code that
// decides it is the code the tests reach. What the window answers is the
// person's choice, not zenity's output: the client prints it as zenity would.
//
// # Versions
//
// Version 1 is the first window's: --question, --info, --warning, --error,
// --entry and --text-info, read-only. Version 2 adds every other dialog,
// the fields they need, and the lines that go with them: Follow's Rows,
// Progress and Notify, and Answer's Partial and Queued. Each version's lines
// are the earlier one's with fields added, never changed. A client sends the
// lowest version its item needs (Needs), so an item that version 1 could
// carry still reaches a window started before version 2 -- the window is
// long-running, and outlives the client package it came with -- and the
// window takes any version from 1 to its own.
package wire

// Version is the newest the window speaks. MinVersion is the oldest it
// still takes: it refuses anything outside them rather than guessing.
const (
	Version    = 2
	MinVersion = 1
)

// Hello is the first line a client sends.
type Hello struct {
	Galley int `json:"galley"`
	// Exactly one of Item and Show.
	Item *Item `json:"item,omitempty"`
	Show bool  `json:"show,omitempty"`
	// The xdg-activation token the client was started with, if any, so a
	// --show from a keyboard shortcut is allowed to take focus.
	Token string `json:"token,omitempty"`
}

// The kinds of item. Each is one of zenity's dialogs.
const (
	KindQuestion = "question"
	KindInfo     = "info"
	KindWarning  = "warning"
	KindError    = "error"
	KindEntry    = "entry"
	KindText     = "text"

	// Version 2.
	KindList         = "list"
	KindForms        = "forms"
	KindCalendar     = "calendar"
	KindScale        = "scale"
	KindPassword     = "password"
	KindColor        = "color"
	KindFile         = "file"
	KindProgress     = "progress"
	KindNotification = "notification"
	KindAbout        = "about"
)

// Item is one dialog's worth of question.
type Item struct {
	Kind  string `json:"kind"`
	Title string `json:"title"`
	// Text is the body, after zenity's processing of --text. It is markup
	// only when Markup is set, and the window still sanitises it then.
	Text   string `json:"text,omitempty"`
	Markup bool   `json:"markup,omitempty"`
	// Icon is a theme icon name or an absolute path.
	Icon      string `json:"icon,omitempty"`
	Width     int    `json:"width,omitempty"`
	Height    int    `json:"height,omitempty"`
	NoWrap    bool   `json:"noWrap,omitempty"`
	Ellipsize bool   `json:"ellipsize,omitempty"`

	Buttons []Button `json:"buttons"`
	// Default is the index in Buttons that Enter presses, or -1 for none.
	Default int `json:"default"`

	Entry *Entry `json:"entry,omitempty"`
	Info  *Info  `json:"info,omitempty"`

	// Version 2.

	// Locale is the caller's LC_TIME, by the variables that set it, so that
	// a date is written as the caller's zenity would have written it.
	Locale   string    `json:"locale,omitempty"`
	List     *List     `json:"list,omitempty"`
	Forms    *Forms    `json:"forms,omitempty"`
	Calendar *Calendar `json:"calendar,omitempty"`
	Scale    *Scale    `json:"scale,omitempty"`
	Password *Password `json:"password,omitempty"`
	Color    *Color    `json:"color,omitempty"`
	File     *File     `json:"file,omitempty"`
	Progress *Progress `json:"progress,omitempty"`
	Note     *Note     `json:"note,omitempty"`
}

// Button is one of an item's answers.
type Button struct {
	// Answer is what pressing it answers: AnswerOK, AnswerCancel,
	// AnswerClose or AnswerExtra, with Index naming which extra button.
	Answer string `json:"answer"`
	Index  int    `json:"index,omitempty"`
	// Label is the text shown, its mnemonic underscores already removed.
	Label string `json:"label"`
	// Key is the single lower-case letter or digit that presses it, or "".
	Key string `json:"key,omitempty"`
	// Underline is the index, in runes of Label, of the character Key names,
	// or -1 when Label has no such character to mark.
	Underline int `json:"underline"`
	// Disabled is a button that cannot be pressed until a Follow enables
	// it: a progress item's OK, before it is done. Version 2.
	Disabled bool `json:"disabled,omitempty"`
}

// Entry is an --entry item's text field.
type Entry struct {
	Text   string `json:"text,omitempty"`
	Hidden bool   `json:"hidden,omitempty"`
	// Values makes it a combo: a text field with these to pick from.
	// Version 2.
	Values []string `json:"values,omitempty"`
}

// Info is a --text-info item's text.
type Info struct {
	Text string `json:"text,omitempty"`
	// More says Appends will follow: the text is being read from stdin.
	More       bool   `json:"more,omitempty"`
	Checkbox   string `json:"checkbox,omitempty"`
	AutoScroll bool   `json:"autoScroll,omitempty"`
	// Editable lets the person change the text, which is then answered.
	// Version 2.
	Editable bool `json:"editable,omitempty"`
}

// List is a --list item's table.
type List struct {
	Columns []string   `json:"columns"`
	Rows    [][]string `json:"rows,omitempty"`
	// More says Rows will follow: they are being read from stdin.
	More bool `json:"more,omitempty"`
	// Type is "", or ListCheck, ListRadio or ListImage, which make the
	// first column a tick, a choice of one, or an image's path.
	Type       string `json:"type,omitempty"`
	Multiple   bool   `json:"multiple,omitempty"`
	Editable   bool   `json:"editable,omitempty"`
	HideHeader bool   `json:"hideHeader,omitempty"`
	// Hidden are the columns not shown, counted from 0.
	Hidden []int `json:"hidden,omitempty"`
}

// A list's types.
const (
	ListCheck = "check"
	ListRadio = "radio"
	ListImage = "image"
)

// Forms is a --forms item's fields, in the order given.
type Forms struct {
	Fields []Field `json:"fields"`
	// DateFormat is a calendar field's, for GLib's g_date_time_format.
	DateFormat string `json:"dateFormat"`
}

// Field is one of a form's.
type Field struct {
	// Kind is one of the Field kinds below.
	Kind  string `json:"kind"`
	Label string `json:"label"`
	// A list's columns, rows and whether its header shows.
	Columns    []string   `json:"columns,omitempty"`
	Rows       [][]string `json:"rows,omitempty"`
	ShowHeader bool       `json:"showHeader,omitempty"`
	// A combo's values; none, and it has nothing to pick.
	Values []string `json:"values,omitempty"`
}

// A form's fields.
const (
	FieldEntry     = "entry"
	FieldPassword  = "password"
	FieldMultiline = "multiline"
	FieldCalendar  = "calendar"
	FieldList      = "list"
	FieldCombo     = "combo"
)

// Calendar is a --calendar item's date, already a real one, and how to
// write the date picked.
type Calendar struct {
	Day    int    `json:"day"`
	Month  int    `json:"month"`
	Year   int    `json:"year"`
	Format string `json:"format"`
}

// Scale is a --scale item's range.
type Scale struct {
	Value int  `json:"value"`
	Min   int  `json:"min"`
	Max   int  `json:"max"`
	Step  int  `json:"step"`
	Hide  bool `json:"hide,omitempty"`
	// Partial asks for an Answer.Partial each time the value moves.
	Partial bool `json:"partial,omitempty"`
}

// Password is a --password item's fields: a hidden password, and with
// Username a username above it.
type Password struct {
	Username bool `json:"username,omitempty"`
}

// Color is a --color-selection item's chooser.
type Color struct {
	// Color is the colour to start from, in any form GDK parses; the window
	// ignores one it cannot read, as zenity does.
	Color string `json:"color,omitempty"`
	// Palette starts with the palette rather than the editor.
	Palette bool `json:"palette,omitempty"`
}

// File is a --file-selection item: what the file chooser it opens is for.
type File struct {
	// Mode is FileOpen, FileSave or FileDirectory.
	Mode     string `json:"mode"`
	Multiple bool   `json:"multiple,omitempty"`
	// Folder is where the chooser starts, Name the name it suggests when
	// saving; both from --filename, Folder absolute.
	Folder  string   `json:"folder,omitempty"`
	Name    string   `json:"name,omitempty"`
	Filters []Filter `json:"filters,omitempty"`
}

// File chooser modes.
const (
	FileOpen      = "open"
	FileSave      = "save"
	FileDirectory = "directory"
)

// Filter is one --file-filter: a name and its glob patterns.
type Filter struct {
	Name     string   `json:"name"`
	Patterns []string `json:"patterns"`
}

// Progress is a --progress item's start.
type Progress struct {
	Percentage    float64 `json:"percentage"`
	Pulsate       bool    `json:"pulsate,omitempty"`
	TimeRemaining bool    `json:"timeRemaining,omitempty"`
}

// Note is a --notification item. Without Listen it is the one entry, its
// text the item's; with it there is nothing to show until a Notify.
type Note struct {
	Listen bool `json:"listen,omitempty"`
}

// Follow is any line a client sends after its Hello.
type Follow struct {
	// Append adds text to a text-info item.
	Append string `json:"append,omitempty"`
	// Timeout asks the window to answer AnswerTimeout now.
	Timeout bool `json:"timeout,omitempty"`

	// Version 2.

	// Rows adds rows to a list item.
	Rows [][]string `json:"rows,omitempty"`
	// Progress updates a progress item.
	Progress *ProgressUpdate `json:"progress,omitempty"`
	// Notify shows a listening notification's message: its entry's text
	// replaced, or a new entry if the person has dismissed the last.
	Notify *Notify `json:"notify,omitempty"`
}

// ProgressUpdate is a line of a progress item's stdin, read.
type ProgressUpdate struct {
	// Percentage moves the bar, 0 to 100.
	Percentage *float64 `json:"percentage,omitempty"`
	// Text replaces the text above it, as markup.
	Text *string `json:"text,omitempty"`
	// Pulsate starts or stops the bar moving on its own.
	Pulsate *bool `json:"pulsate,omitempty"`
	// Remaining is the time-remaining line, "" to clear it.
	Remaining *string `json:"remaining,omitempty"`
	// Finished enables OK and makes it the default: 100%, or stdin closed.
	Finished bool `json:"finished,omitempty"`
	// Closed is stdin closed: the bar full and still, and Cancel disabled.
	Closed bool `json:"closed,omitempty"`
}

// Notify is one message for a listening notification.
type Notify struct {
	Text string `json:"text"`
	Icon string `json:"icon,omitempty"`
}

// What an item can be answered.
const (
	AnswerOK      = "ok"
	AnswerCancel  = "cancel"
	AnswerExtra   = "extra"
	AnswerTimeout = "timeout"
	// AnswerClose is zenity's Escape: the dialog closed with no button.
	AnswerClose = "close"
)

// Answer is the window's last line to an item's client, and in version 2
// may come after Partial or Queued lines. With AnswerOK and AnswerTimeout it
// carries what the person chose, in the field for the item's kind.
type Answer struct {
	Answer string `json:"answer,omitempty"`
	Index  int    `json:"index,omitempty"`
	// Entry is an entry's or a combo's text, or a password item's password.
	Entry *string `json:"entry,omitempty"`
	// Shown answers a Show.
	Shown bool `json:"shown,omitempty"`
	// Error is the window refusing the Hello, said for the client to print.
	Error string `json:"error,omitempty"`

	// Version 2.

	// Username is a password item's username.
	Username *string `json:"username,omitempty"`
	// Text is an editable text-info's text, a calendar's date as written by
	// its format (absent when the format could not write it), or a colour.
	Text *string `json:"text,omitempty"`
	// Value is a scale's.
	Value *int `json:"value,omitempty"`
	// Rows are a list's chosen rows, each as it now reads, in the order
	// zenity prints them.
	Rows [][]string `json:"rows,omitempty"`
	// Fields are a form's values, one for each of its fields.
	Fields []FieldValue `json:"fields,omitempty"`
	// Files are a file chooser's chosen paths.
	Files []string `json:"files,omitempty"`
	// Partial is a scale's value as it moves, before any answer.
	Partial *int `json:"partial,omitempty"`
	// Queued says a notification is in the queue, before any answer.
	Queued bool `json:"queued,omitempty"`
}

// FieldValue is one form field's value: Text for every kind but a list's,
// absent for a combo with nothing picked; Rows, its selected row, for a
// list's.
type FieldValue struct {
	Text *string    `json:"text,omitempty"`
	Rows [][]string `json:"rows,omitempty"`
}

// Needs is the lowest version that carries item: 1 for what the first
// window showed, 2 for anything after.
func Needs(item *Item) int {
	switch item.Kind {
	case KindQuestion, KindInfo, KindWarning, KindError:
	case KindEntry:
		if item.Entry != nil && item.Entry.Values != nil {
			return 2
		}
	case KindText:
		if item.Info != nil && item.Info.Editable {
			return 2
		}
	default:
		return 2
	}
	return 1
}
