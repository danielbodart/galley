package zenity

import (
	"reflect"
	"strings"
	"testing"

	"github.com/danielbodart/galley/internal/wire"
)

func TestCompressIsGStrcompress(t *testing.T) {
	for _, c := range []struct{ in, want string }{
		{`plain`, "plain"},
		{`a\nb`, "a\nb"},
		{`tab\there`, "tab\there"},
		{`\b\f\r\v`, "\b\f\r\v"},
		{`back\\slash`, `back\slash`},
		{`quote\"d`, `quote"d`},
		// Any other escaped character is itself, its backslash dropped.
		{`\q\y\z`, "qyz"},
		// Up to three octal digits; a fourth is a character of its own.
		{`\101\1010`, "AA0"},
		{`\7x`, "\ax"},
		// A trailing backslash ends the string, as GLib warns and stops.
		{`end\`, "end"},
		// An octal NUL ends the C string.
		{`cut\000here`, "cut"},
		// Bytes that are not UTF-8 are shown as replacement characters.
		{`\377ok`, "�ok"},
		{"ünï\\ncode", "ünï\ncode"},
	} {
		if got := Compress(c.in); got != c.want {
			t.Errorf("Compress(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}

func TestMnemonicIsGTKs(t *testing.T) {
	for _, c := range []struct {
		in   string
		text string
		mark rune
		at   int
	}{
		{"_Allow", "Allow", 'A', 0},
		{"Re_fuse", "Refuse", 'f', 2},
		{"foo__bar", "foo_bar", 0, -1},
		// Only the first single underscore marks; a later one is dropped.
		{"_a_b", "ab", 'a', 0},
		{"trailing_", "trailing", 0, -1},
		{"__init__", "_init_", 0, -1},
		{"_Ünï", "Ünï", 'Ü', 0},
		{"no mark", "no mark", 0, -1},
	} {
		text, mark, at := Mnemonic(c.in)
		if text != c.text || mark != c.mark || at != c.at {
			t.Errorf("Mnemonic(%q) = %q %q %d, want %q %q %d", c.in, text, mark, at, c.text, c.mark, c.at)
		}
	}
}

func labels(bs ...string) []wire.Button {
	out := make([]wire.Button, len(bs))
	for i, b := range bs {
		out[i] = wire.Button{Answer: wire.AnswerExtra, Index: i, Label: b}
	}
	return out
}

func keys(bs []wire.Button) string {
	var s []string
	for _, b := range bs {
		k := b.Key
		if k == "" {
			k = "-"
		}
		s = append(s, k)
	}
	return strings.Join(s, ",")
}

func TestButtonsGetAKeyEach(t *testing.T) {
	for _, c := range []struct {
		labels []string
		want   string
	}{
		// frisket's asker, as it calls zenity: cancel, ok, then the extra.
		{[]string{"Refuse", "Allow", "Ask"}, "r,a,s"},
		// Mnemonics come first, whatever their order.
		{[]string{"Ask", "_Allow"}, "s,a"},
		{[]string{"_Yes", "_No"}, "y,n"},
		// A mnemonic already taken falls back to the first free letter.
		{[]string{"_Allow", "_Ask"}, "a,s"},
		// j and k move through the queue and are never a button's.
		{[]string{"_Keep", "Junk"}, "e,u"},
		// Case does not matter; digits count; no letter left is no key.
		{[]string{"A", "a", "1"}, "a,-,1"},
		{[]string{"→", "OK"}, "-,o"},
	} {
		got := Buttons(labels(c.labels...))
		if keys(got) != c.want {
			t.Errorf("Buttons(%q) keys = %s, want %s", c.labels, keys(got), c.want)
		}
	}
	got := Buttons(labels("Re_fuse", "Allow"))
	if got[0].Label != "Refuse" || got[0].Underline != 2 || got[1].Underline != 0 {
		t.Errorf("labels and underlines = %+v", got)
	}
}

// The three command lines on the machine galley was written for, exactly as
// each calls zenity.
func TestTheFrisketAsker(t *testing.T) {
	text := "my-project\n\nPOST api.github.com/x\\\\y\n\nsession s, policy p"
	inv, err := Parse([]string{"--question", "--no-markup",
		"--title=Run this command?", "--ok-label=Allow", "--cancel-label=Refuse",
		"--extra-button=Ask", "--width=640", "--text=" + text}, "/")
	if err != nil {
		t.Fatal(err)
	}
	item := inv.Item
	// --no-markup: zenity sets the text as it is, with no escapes undone.
	if item.Kind != wire.KindQuestion || item.Title != "Run this command?" || item.Text != text || item.Markup {
		t.Errorf("item = %+v", item)
	}
	if item.Width != 640 || item.Default != 1 || keys(item.Buttons) != "r,a,s" {
		t.Errorf("item = %+v", item)
	}
	want := []wire.Button{
		{Answer: wire.AnswerCancel, Label: "Refuse", Key: "r", Underline: 0},
		{Answer: wire.AnswerOK, Label: "Allow", Key: "a", Underline: 0},
		{Answer: wire.AnswerExtra, Index: 0, Label: "Ask", Key: "s", Underline: 1},
	}
	if !reflect.DeepEqual(item.Buttons, want) {
		t.Errorf("buttons = %+v", item.Buttons)
	}
	if !reflect.DeepEqual(inv.Extra, []string{"Ask"}) {
		t.Errorf("extra = %q", inv.Extra)
	}
}

func TestTheSudoAskpass(t *testing.T) {
	inv, err := Parse([]string{"--entry", "--hide-text", "--title=Authentication Required",
		"--text=Authenticate to run as root:\n\n    sudo nix-collect__garbage \\\\\n        -d\n\nPassword for dan:"}, "/")
	if err != nil {
		t.Fatal(err)
	}
	item := inv.Item
	want := "Authenticate to run as root:\n\n    sudo nix-collect_garbage \\\n        -d\n\nPassword for dan:"
	if item.Kind != wire.KindEntry || item.Text != want || !item.Entry.Hidden || item.Entry.Text != "" {
		t.Errorf("item = %+v %+v", item, item.Entry)
	}
	if keys(item.Buttons) != "c,o" || item.Default != 1 {
		t.Errorf("buttons = %+v", item.Buttons)
	}
}

// Every prompt for a password shows the key, whichever dialog asks; what
// asks for none does not.
func TestEveryPasswordShowsTheKey(t *testing.T) {
	for _, c := range []struct {
		args []string
		want string
	}{
		{[]string{"--password"}, "dialog-password"},
		{[]string{"--password", "--username"}, "dialog-password"},
		{[]string{"--entry", "--hide-text"}, "dialog-password"},
		{[]string{"--forms", "--add-entry=User", "--add-password=Password"}, "dialog-password"},
		{[]string{"--forms", "--add-password=PIN"}, "dialog-password"},
		{[]string{"--entry"}, "text-editor"},
		// Values make the entry a combo, which zenity shows, hidden or not.
		{[]string{"--entry", "--hide-text", "--entry-text=a", "b"}, "text-editor"},
		{[]string{"--forms", "--add-entry=Name"}, "document-edit"},
	} {
		inv, err := Parse(c.args, "/")
		if err != nil {
			t.Fatalf("%q: %v", c.args, err)
		}
		if inv.Item.Icon != c.want {
			t.Errorf("%q: icon = %q, want %q", c.args, inv.Item.Icon, c.want)
		}
	}
}

func TestTheChaseApprover(t *testing.T) {
	inv, err := Parse([]string{"--text-info", "--title=Approve this change?",
		"--ok-label=Approve", "--cancel-label=Refuse", "--width=720", "--height=520",
		"--filename=/run/user/1000/chase-approve.abc"}, "/")
	if err != nil {
		t.Fatal(err)
	}
	item := inv.Item
	if item.Kind != wire.KindText || inv.Filename != "/run/user/1000/chase-approve.abc" ||
		item.Width != 720 || item.Height != 520 || keys(item.Buttons) != "r,a" {
		t.Errorf("item = %+v, file %q", item, inv.Filename)
	}
	if item.Buttons[0].Answer != wire.AnswerCancel || item.Buttons[1].Answer != wire.AnswerOK || item.Default != 1 {
		t.Errorf("buttons = %+v", item.Buttons)
	}
}

func TestDefaultsAreZenitys(t *testing.T) {
	for _, c := range []struct {
		args            []string
		title, text     string
		labels, deflt   string
		markup, entries bool
	}{
		{[]string{"--question"}, "Question", "Are you sure you want to proceed?", "No,Yes", "Yes", false, false},
		{[]string{"--question", "--default-cancel"}, "Question", "Are you sure you want to proceed?", "No,Yes", "No", false, false},
		{[]string{"--info"}, "Information", "All updates are complete.", "OK", "OK", false, false},
		{[]string{"--warning"}, "Warning", "Are you sure you want to proceed?", "OK", "OK", false, false},
		{[]string{"--error"}, "Error", "An error has occurred.", "OK", "OK", false, false},
		{[]string{"--entry"}, "Add a new entry", "Enter new text:", "Cancel,OK", "OK", false, true},
		{[]string{"--text-info"}, "Text View", "", "Cancel,OK", "OK", false, false},
		// With markup, --text is compressed first.
		{[]string{"--info", "--text", `<b>a</b>\tb`}, "Information", "<b>a</b>\tb", "OK", "OK", true, false},
		// --switch has only the extra buttons, and so no default.
		{[]string{"--question", "--switch", "--extra-button", "One", "--extra-button", "Two"}, "Question", "Are you sure you want to proceed?", "One,Two", "", false, false},
		{[]string{"--list", "--column=a"}, "Select items from the list", "Select items from the list below.", "Cancel,OK", "OK", false, false},
		{[]string{"--forms"}, "Forms dialog", "Forms dialog", "Cancel,OK", "OK", false, false},
		{[]string{"--calendar"}, "Calendar selection", "Select a date from below.", "Cancel,OK", "OK", false, false},
		{[]string{"--scale"}, "Adjust the scale value", "Adjust the scale value", "Cancel,OK", "OK", false, false},
		{[]string{"--password"}, "Password", "Type your password", "Cancel,OK", "OK", false, false},
		{[]string{"--color-selection"}, "Select a Color", "", "Cancel,Select", "Select", false, false},
		{[]string{"--file-selection"}, "Open File", "Choose a file.", "Cancel,Open…", "Open…", false, false},
		{[]string{"--file-selection", "--directory", "--multiple"}, "Select Folders", "Choose one or more folders.", "Cancel,Select…", "Select…", false, false},
		// A progress bar's OK waits for it to be done, and so is no default.
		{[]string{"--progress"}, "Progress", "Running...", "Cancel,OK", "", false, false},
		{[]string{"--progress", "--percentage=100"}, "Progress", "Running...", "Cancel,OK", "OK", false, false},
		{[]string{"--notification", "--text=a"}, "Notification", "a", "Dismiss", "Dismiss", false, false},
		{[]string{"--about"}, "About galley", "", "Close", "Close", false, false},
		// A dialog's --text is markup with its escapes undone.
		{[]string{"--list", "--column=a", `--text=<i>x</i>\n`}, "Select items from the list", "<i>x</i>\n", "Cancel,OK", "OK", true, false},
	} {
		inv, err := Parse(c.args, "/")
		if err != nil {
			t.Errorf("%q: %v", c.args, err)
			continue
		}
		item := inv.Item
		var ls []string
		for _, b := range item.Buttons {
			ls = append(ls, b.Label)
		}
		deflt := ""
		if item.Default >= 0 {
			deflt = item.Buttons[item.Default].Label
		}
		if item.Title != c.title || item.Text != c.text || strings.Join(ls, ",") != c.labels ||
			deflt != c.deflt || item.Markup != c.markup || (item.Entry != nil) != c.entries {
			t.Errorf("%q: got %+v", c.args, item)
		}
	}
}

func TestSyntaxIsGOptions(t *testing.T) {
	// --name value and --name=value, options after arguments, -- ending them.
	inv, err := Parse([]string{"--title", "-x-", "--question", "--text=a=b", "--", "--info"}, "/")
	if err != nil {
		t.Fatal(err)
	}
	if inv.Item.Title != "-x-" || inv.Item.Kind != wire.KindQuestion {
		t.Errorf("item = %+v", inv.Item)
	}
	// The last of an option given twice wins; extra buttons accumulate.
	inv, _ = Parse([]string{"--question", "--ok-label=A", "--ok-label=B", "--extra-button=X", "--extra-button=Y"}, "/")
	if inv.Item.Buttons[1].Label != "B" || !reflect.DeepEqual(inv.Extra, []string{"X", "Y"}) {
		t.Errorf("buttons = %+v, extra = %q", inv.Item.Buttons, inv.Extra)
	}
	// Options zenity accepts for any dialog and galley has no use for: the
	// scale's, which zenity never checks, and the forms' fields. A flag
	// given a value is the flag.
	if _, err := Parse([]string{"--question", "--modal=no", "--value=3", "--add-entry=x", "--hide-value"}, "/"); err != nil {
		t.Errorf("harmless options refused: %v", err)
	}
	// --GROUP-OPTION is the group's option; integers are C's, in any base.
	inv, err = Parse([]string{"--forms", "--forms-date-format=%d", "--general-width", "0x10", "--height= 010"}, "/")
	if err != nil || inv.Item.Forms.DateFormat != "%d" || inv.Item.Width != 16 || inv.Item.Height != 8 {
		t.Errorf("got %+v, %v", inv, err)
	}
	inv, _ = Parse([]string{"--question", "--timeout", "7"}, "/")
	if inv.Timeout != 7 {
		t.Errorf("timeout = %d", inv.Timeout)
	}
}

func TestRefusals(t *testing.T) {
	for _, c := range []struct {
		args []string
		want string
	}{
		{[]string{"--question", "--frobnicate"}, "This option is not available. Please see --help for all possible usages."},
		{[]string{"-q"}, "This option is not available."},
		{[]string{"--question", "--hide-text=yes"}, "--hide-text is not supported for this dialog"},
		{[]string{}, "You must specify a dialog type."},
		{[]string{"--question", "--entry"}, "Two or more dialog options specified"},
		// GOption's own errors, which zenity's error hook says as the first.
		{[]string{"--question", "--width=wide"}, "This option is not available."},
		{[]string{"--question", "--width=7x"}, "This option is not available."},
		{[]string{"--question", "--width=3000000000"}, "This option is not available."},
		{[]string{"--question", "--text"}, "This option is not available."},
		{[]string{"--info", "--info-text=x"}, "This option is not available."},
		// A --switch with no buttons.
		{[]string{"--question", "--switch"}, "This option is not available."},
		// zenity's own checks after parsing.
		{[]string{"--info", "--cancel-label=No"}, "--cancel-label is not supported for this dialog"},
		// zenity names --no-wrap and --ellipsize from a table without them.
		{[]string{"--entry", "--ellipsize"}, "--(null) is not supported for this dialog"},
		{[]string{"--question", "--filename=x"}, "--filename is not supported for this dialog"},
		{[]string{"--question", "--editable"}, "--editable is not supported for this dialog"},
		{[]string{"--question", "--percentage=3"}, "--percentage is not supported for this dialog"},
		{[]string{"--info", "--font=x"}, "--font is not supported for this dialog"},
		// --date-format is the calendar's; the form's is reached by its group.
		{[]string{"--forms", "--date-format=%Y"}, "--date-format is not supported for this dialog"},
		{[]string{"--about", "--version"}, "Two or more dialog options specified"},
		// What galley never does.
		{[]string{"--text-info", "--html"}, "--html is not supported by galley"},
		{[]string{"--text-info", "--url=https://example.com"}, "--url is not supported by galley"},
		{[]string{"--info", "--no-interaction"}, "This option is not available."},
		{append([]string{"--question"}, repeat("--extra-button=x", 15)...), "galley shows at most 16 buttons, and this dialog would have 17"},
	} {
		_, err := Parse(c.args, "/")
		if err == nil || !strings.HasPrefix(err.Error(), c.want) {
			t.Errorf("%q: got %v, want %q", c.args, err, c.want)
		}
	}
}

func repeat(s string, n int) []string {
	out := make([]string, n)
	for i := range out {
		out[i] = s
	}
	return out
}

// What zenity takes and the window would not is made to fit, not refused.
func TestWhatTheWindowWouldRefuseIsFitted(t *testing.T) {
	long := strings.Repeat("é", 2000)
	inv, err := Parse([]string{"--question", "--width=-5", "--height=200000", "--title=" + long,
		"--ok-label=" + long, "--extra-button=" + long + "😀"}, "/")
	if err != nil {
		t.Fatal(err)
	}
	item := inv.Item
	if item.Width != -1 || item.Height != 100000 {
		t.Errorf("size = %d x %d", item.Width, item.Height)
	}
	if item.Title != strings.Repeat("é", 1023)+"…" {
		t.Errorf("title is %d runes", len([]rune(item.Title)))
	}
	for _, b := range item.Buttons[1:] {
		if b.Label != strings.Repeat("é", 255)+"…" {
			t.Errorf("label is %d runes", len([]rune(b.Label)))
		}
	}
	// What is printed is what was given.
	if inv.Extra[0] != long+"😀" {
		t.Errorf("extra is %d runes", len([]rune(inv.Extra[0])))
	}
	// A character of two UTF-16 units is not cut in half.
	if got := clip("ab😀", 3); got != "ab…" {
		t.Errorf("clip = %q", got)
	}
	if got := clip("ab😀", 4); got != "ab😀" {
		t.Errorf("clip = %q", got)
	}
	// A mark beyond the cut underlines nothing, but its key still presses.
	inv, _ = Parse([]string{"--info", "--ok-label=" + strings.Repeat("-", 300) + "_x"}, "/")
	if b := inv.Item.Buttons[0]; b.Key != "x" || b.Underline != -1 {
		t.Errorf("button = %+v", b)
	}
}

func TestExitCodes(t *testing.T) {
	env := map[string]string{}
	getenv := func(k string) string { return env[k] }
	for o, want := range map[Outcome]int{OK: 0, Cancel: 1, Esc: 1, Failed: 255, Extra: 1, Timeout: 5} {
		if got := Code(o, getenv); got != want {
			t.Errorf("Code(%d) = %d, want %d", o, got, want)
		}
	}
	env["DIALOG_CANCEL"] = "3"
	env["ZENITY_TIMEOUT"] = " 9x"
	env["ZENITY_OK"] = "nope"
	if Code(Cancel, getenv) != 3 || Code(Timeout, getenv) != 9 || Code(OK, getenv) != 0 {
		t.Errorf("overrides: %d %d %d", Code(Cancel, getenv), Code(Timeout, getenv), Code(OK, getenv))
	}
	env["ZENITY_CANCEL"] = "4"
	if Code(Cancel, getenv) != 4 {
		t.Errorf("ZENITY_ wins over DIALOG_: %d", Code(Cancel, getenv))
	}
}
