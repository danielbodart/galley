// Package wire is what a galley client and the window say to each other over
// the socket: one JSON object a line, in both directions.
//
// The client opens the conversation with a Hello naming either an Item, which
// the window queues until someone answers it, or Show, which asks the window
// to come forward. An item's conversation then stays open until the window
// writes the Answer; while it waits, the client may send Appends (text-info
// read from stdin, as it arrives) and at most one Timeout. Closing the
// connection withdraws the item: the window drops it without answering, which
// is what makes a killed client's question disappear.
//
// Everything the window renders arrives already in its final form: zenity's
// escapes and mnemonics are undone here, in the client, so the window never
// has to know what zenity would have done with a string, and the code that
// decides it is the code the tests reach.
package wire

// Version is the protocol's, sent in every Hello. The window refuses one it
// does not know rather than guessing at its fields.
const Version = 1

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
}

// Entry is an --entry item's text field.
type Entry struct {
	Text   string `json:"text,omitempty"`
	Hidden bool   `json:"hidden,omitempty"`
}

// Info is a --text-info item's text.
type Info struct {
	Text string `json:"text,omitempty"`
	// More says Appends will follow: the text is being read from stdin.
	More       bool   `json:"more,omitempty"`
	Checkbox   string `json:"checkbox,omitempty"`
	AutoScroll bool   `json:"autoScroll,omitempty"`
}

// Follow is any line a client sends after its Hello.
type Follow struct {
	// Append adds text to a text-info item.
	Append string `json:"append,omitempty"`
	// Timeout asks the window to answer AnswerTimeout now.
	Timeout bool `json:"timeout,omitempty"`
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

// Answer is the window's only line to an item's client. Entry carries an
// entry item's text for AnswerOK and AnswerTimeout.
type Answer struct {
	Answer string  `json:"answer,omitempty"`
	Index  int     `json:"index,omitempty"`
	Entry  *string `json:"entry,omitempty"`
	// Shown answers a Show.
	Shown bool `json:"shown,omitempty"`
	// Error is the window refusing the Hello, said for the client to print.
	Error string `json:"error,omitempty"`
}
