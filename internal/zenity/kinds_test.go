package zenity

import (
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/danielbodart/galley/internal/wire"
)

// What zenity 4.2.2 says to each of these before it opens a window -- its
// warnings, then its error and exit 255, or "OK" when it goes on to show the
// dialog -- taken from zenity itself, run with no display to reach. A
// sample of thousands of command lines compared the same way.
func TestTheCommandLineIsZenitys(t *testing.T) {
	for _, c := range []struct {
		args []string
		want string
	}{
		{[]string{"--icon=i", "--forms-date-format=%d", "--ellipsize", "--add-combo=c", "--forms"}, "--(null) is not supported for this dialog | exit 255"},
		{[]string{"--color-selection", "--ellipsize"}, "--(null) is not supported for this dialog | exit 255"},
		{[]string{"--calendar", "--month=1", "--auto-close", "--separator=,"}, "--auto-close is not supported for this dialog | exit 255"},
		{[]string{"--forms", "--pro-auto-close"}, "--auto-close is not supported for this dialog | exit 255"},
		{[]string{"--info", "--pro-auto-kill"}, "--auto-kill is not supported for this dialog | exit 255"},
		{[]string{"--warning", "--auto-kill"}, "--auto-kill is not supported for this dialog | exit 255"},
		{[]string{"--error", "--cancel-label=c"}, "--cancel-label is not supported for this dialog | exit 255"},
		{[]string{"--percentage=0", "--cancel-label=c", "--file-selection", "--icon=i", "--print-partial"}, "--cancel-label is not supported for this dialog | exit 255"},
		{[]string{"--text-info", "--list-checklist"}, "--checklist is not supported for this dialog | exit 255"},
		{[]string{"--filename=f", "--checklist", "--auto-scroll", "--scale", "--percentage=5", "--password"}, "--checklist is not supported for this dialog | exit 255"},
		{[]string{"--about", "--color=red", "--show-palette"}, "--color is not supported for this dialog | exit 255"},
		{[]string{"--about", "--color=red"}, "--color is not supported for this dialog | exit 255"},
		{[]string{"--width=5", "--column=c", "--question"}, "--column is not supported for this dialog | exit 255"},
		{[]string{"--forms", "--list-column=1"}, "--column is not supported for this dialog | exit 255"},
		{[]string{"--list", "--column-values=c", "--radiolist"}, "--column-values is not supported for this dialog | exit 255"},
		{[]string{"--text-info", "--for-column-values=1"}, "--column-values is not supported for this dialog | exit 255"},
		{[]string{"--filename=f", "--hide-value", "--combo-values=v", "--attach=0", "--about"}, "--combo-values is not supported for this dialog | exit 255"},
		{[]string{"--combo-values=v", "--progress"}, "--combo-values is not supported for this dialog | exit 255"},
		{[]string{"--text-info", "--date-format=%Y", "--general-title=t"}, "--date-format is not supported for this dialog | exit 255"},
		{[]string{"--date-format=%Y", "--separator=|", "--question", "--add-password=p", "--print-partial"}, "--date-format is not supported for this dialog | exit 255"},
		{[]string{"--calendar-day=4", "--directory", "--warning"}, "--day is not supported for this dialog | exit 255"},
		{[]string{"--day=3", "--percentage=5", "--attach=3", "--year=2000", "--about"}, "Warning: --attach is deprecated and will be removed in a future version of zenity. Ignoring. | --day is not supported for this dialog | exit 255"},
		{[]string{"--directory", "--progress", "--icon=i", "--add-list=l", "--switch"}, "--directory is not supported for this dialog | exit 255"},
		{[]string{"--forms", "--directory"}, "--directory is not supported for this dialog | exit 255"},
		{[]string{"--calendar", "--editable"}, "--editable is not supported for this dialog | exit 255"},
		{[]string{"--file-selection", "--editable"}, "--editable is not supported for this dialog | exit 255"},
		{[]string{"--add-list=l", "--text-info", "--default-cancel", "--entry-text="}, "--entry-text is not supported for this dialog | exit 255"},
		{[]string{"--value=1", "--step=2", "--file-selection", "--entry-text=", "--general-title=t"}, "--entry-text is not supported for this dialog | exit 255"},
		{[]string{"--notification", "--file-filter=a"}, "--file-filter is not supported for this dialog | exit 255"},
		{[]string{"--confirm-overwrite", "--warning", "--timeout=010", "--file-filter=a", "--hint=h", "--notification"}, "--file-filter is not supported for this dialog | exit 255"},
		{[]string{"--entry", "--filename=f"}, "--filename is not supported for this dialog | exit 255"},
		{[]string{"--filename=f", "--entry"}, "--filename is not supported for this dialog | exit 255"},
		{[]string{"--add-entry=a", "--text-font=x", "--about", "--separator=,", "--checkbox=c"}, "--font is not supported for this dialog | exit 255"},
		{[]string{"--text-font=x", "--list", "--hide-value"}, "--font is not supported for this dialog | exit 255"},
		{[]string{"--question", "--switch", "--hide-column=1"}, "--hide-column is not supported for this dialog | exit 255"},
		{[]string{"--file-selection", "--hide-column=1"}, "--hide-column is not supported for this dialog | exit 255"},
		{[]string{"--hide-header", "--info", "--auto-scroll", "--print-partial"}, "--hide-header is not supported for this dialog | exit 255"},
		{[]string{"--hide-header", "--ellipsize", "--info"}, "--hide-header is not supported for this dialog | exit 255"},
		{[]string{"--list", "--entry-hide-text"}, "--hide-text is not supported for this dialog | exit 255"},
		{[]string{"--progress", "--hide-text"}, "--hide-text is not supported for this dialog | exit 255"},
		{[]string{"--imagelist", "--password", "--cancel-label=c"}, "--imagelist is not supported for this dialog | exit 255"},
		{[]string{"--password", "--imagelist", "--icon-name=i", "--color=red", "--add-calendar=c"}, "Warning: --icon-name is deprecated and will be removed in a future version of zenity; Treating as --icon. | --imagelist is not supported for this dialog | exit 255"},
		{[]string{"--notification", "--list-values=v"}, "--list-values is not supported for this dialog | exit 255"},
		{[]string{"--error", "--list-values=v"}, "--list-values is not supported for this dialog | exit 255"},
		{[]string{"--warning", "--listen"}, "--listen is not supported for this dialog | exit 255"},
		{[]string{"--switch", "--version", "--listen"}, "--listen is not supported for this dialog | exit 255"},
		{[]string{"--forms", "--mid-search"}, "--mid-search is not supported for this dialog | exit 255"},
		{[]string{"--hide-value", "--mid-search", "--forms", "--separator=|"}, "--mid-search is not supported for this dialog | exit 255"},
		{[]string{"--month=1", "--combo-values=v", "--add-password=p", "--percentage=0", "--password"}, "--month is not supported for this dialog | exit 255"},
		{[]string{"--file-selection", "--cal-month=1"}, "--month is not supported for this dialog | exit 255"},
		{[]string{"--progress", "--filename=f", "--editable", "--width=+7", "--multiple"}, "--multiple is not supported for this dialog | exit 255"},
		{[]string{"--step=2", "--multiple", "--question"}, "--multiple is not supported for this dialog | exit 255"},
		{[]string{"--time-remaining", "--no-cancel", "--about"}, "--no-cancel is not supported for this dialog | exit 255"},
		{[]string{"--forms", "--progress-no-cancel"}, "--no-cancel is not supported for this dialog | exit 255"},
		{[]string{"--ok-label=o", "--hint=h", "--file-selection", "--filename=f"}, "Warning: --hint is deprecated and will be removed in a future version of zenity. Ignoring. | --ok-label is not supported for this dialog | exit 255"},
		{[]string{"--add-password=p", "--ok-label=o", "--file-selection"}, "--ok-label is not supported for this dialog | exit 255"},
		{[]string{"--percentage=5", "--step=2", "--auto-close", "--entry"}, "--percentage is not supported for this dialog | exit 255"},
		{[]string{"--password", "--percentage=5"}, "--percentage is not supported for this dialog | exit 255"},
		{[]string{"--print-column=1", "--modal", "--notification", "--no-markup", "--min-value=0"}, "--print-column is not supported for this dialog | exit 255"},
		{[]string{"--separator=,", "--print-column=1", "--file-selection", "--column-values=c", "--show-header"}, "--print-column is not supported for this dialog | exit 255"},
		{[]string{"--list", "--auto-close", "--pulsate", "--add-list=l", "--add-multiline-entry=m"}, "--pulsate is not supported for this dialog | exit 255"},
		{[]string{"--info", "--pulsate"}, "--pulsate is not supported for this dialog | exit 255"},
		{[]string{"--pulsate", "--radiolist", "--forms"}, "--radiolist is not supported for this dialog | exit 255"},
		{[]string{"--info", "--lis-radiolist"}, "--radiolist is not supported for this dialog | exit 255"},
		{[]string{"--calendar", "--file-save"}, "--save is not supported for this dialog | exit 255"},
		{[]string{"--forms", "--fil-save"}, "--save is not supported for this dialog | exit 255"},
		{[]string{"--color-selection", "--separator=,"}, "--separator is not supported for this dialog | exit 255"},
		{[]string{"--add-entry=a", "--separator=,", "--text-info"}, "--separator is not supported for this dialog | exit 255"},
		{[]string{"--hide-value", "--show-header", "--error", "--separator=|"}, "--show-header is not supported for this dialog | exit 255"},
		{[]string{"--print-partial", "--show-header", "--multiple", "--editable", "--about"}, "--show-header is not supported for this dialog | exit 255"},
		{[]string{"--info", "--separator=,", "--day=-2", "--show-palette", "--show-header"}, "--show-palette is not supported for this dialog | exit 255"},
		{[]string{"--notification", "--show-palette"}, "--show-palette is not supported for this dialog | exit 255"},
		{[]string{"--about", "--text=t", "--default-cancel"}, "--text is not supported for this dialog | exit 255"},
		{[]string{"--about", "--add-password=p", "--text=t", "--height=0x10", "--min-value=0"}, "--text is not supported for this dialog | exit 255"},
		{[]string{"--forms", "--time-remaining"}, "--time-remaining is not supported for this dialog | exit 255"},
		{[]string{"--question", "--time-remaining"}, "--time-remaining is not supported for this dialog | exit 255"},
		{[]string{"--calendar", "--password-username"}, "--username is not supported for this dialog | exit 255"},
		{[]string{"--timeout=010", "--list", "--text=t", "--username"}, "--username is not supported for this dialog | exit 255"},
		{[]string{"--attach=0", "--icon=i", "--year=2000", "--add-password=p", "--version"}, "--year is not supported for this dialog | exit 255"},
		{[]string{"--year=2000", "--text-info", "--time-remaining"}, "--year is not supported for this dialog | exit 255"},
		{[]string{"--notification", "--auto-scroll"}, "OK"},
		{[]string{"--info", "--separator=|"}, "OK"},
		{[]string{"--list", "--general-cancel-label=1"}, "This option is not available. Please see --help for all possible usages. | exit 255"},
		{[]string{"--info", "--text-filename=1"}, "This option is not available. Please see --help for all possible usages. | exit 255"},
		{[]string{"--info", "--misc-about"}, "Two or more dialog options specified | exit 255"},
		{[]string{"--file-selection", "--war-warning"}, "Two or more dialog options specified | exit 255"},
		{[]string{"--version", "--ok-label=o"}, "VERSION"},
		{[]string{"--version", "--print-partial"}, "VERSION"},
		{[]string{"--file-selection", "--attach=3"}, "Warning: --attach is deprecated and will be removed in a future version of zenity. Ignoring. | OK"},
		{[]string{"--question", "--attach=3"}, "Warning: --attach is deprecated and will be removed in a future version of zenity. Ignoring. | OK"},
		{[]string{"--warning", "--confirm-overwrite"}, "Warning: --confirm-overwrite is deprecated and will be removed in a future version of zenity. Ignoring. | OK"},
		{[]string{"--entry", "--confirm-overwrite"}, "Warning: --confirm-overwrite is deprecated and will be removed in a future version of zenity. Ignoring. | OK"},
		{[]string{"--hint=h", "--add-password=p", "--color-selection", "--modal"}, "Warning: --hint is deprecated and will be removed in a future version of zenity. Ignoring. | OK"},
		{[]string{"--file-selection", "--file-filter", "--add-calendar=c", "--hint=h"}, "Warning: --hint is deprecated and will be removed in a future version of zenity. Ignoring. | OK"},
		{[]string{"--icon-name=i", "--default-cancel", "--calendar", "--day=-2", "--text=t"}, "Warning: --icon-name is deprecated and will be removed in a future version of zenity; Treating as --icon. | OK"},
		{[]string{"--progress", "--icon-name=i"}, "Warning: --icon-name is deprecated and will be removed in a future version of zenity; Treating as --icon. | OK"},
		{[]string{"--error", "--window-icon=w"}, "Warning: --window-icon is deprecated and will be removed in a future version of zenity; Treating as --icon. | OK"},
		{[]string{"--confirm-overwrite", "--window-icon=w", "--add-multiline-entry=m", "--notification", "--timeout=010"}, "Warning: --window-icon is deprecated and will be removed in a future version of zenity; Treating as --icon. | Warning: --confirm-overwrite is deprecated and will be removed in a future version of zenity. Ignoring. | OK"},
	} {
		inv, err := Parse(c.args, "/")
		var said []string
		if err != nil {
			said = append(said, err.(*Error).Warnings...)
			said = append(said, err.Error(), "exit 255")
		} else {
			// Warnings zenity prints only once its dialog is up are not in
			// what it says without a display.
			for _, w := range inv.Warnings {
				if !strings.Contains(w, "--mid-search") && !strings.Contains(w, "--file-selection is deprecated") {
					said = append(said, w)
				}
			}
			if inv.Version {
				said = append(said, "VERSION")
			} else {
				said = append(said, "OK")
			}
		}
		if got := strings.Join(said, " | "); got != c.want {
			t.Errorf("%q:\n got %s\nwant %s", c.args, got, c.want)
		}
	}
}

func parsed(t *testing.T, args ...string) *Invocation {
	t.Helper()
	inv, err := parseAt(args, "/work", time.Date(2026, 10, 3, 12, 0, 0, 0, time.Local))
	if err != nil {
		t.Fatalf("%q: %v", args, err)
	}
	return inv
}

func TestAList(t *testing.T) {
	inv := parsed(t, "--list", "--checklist", "--column=Pick", "--column=Name", "--column=Size",
		"--hide-column=3,9,x", "--print-column=3,1", `--separator=\t`, "--multiple", "--editable", "--hide-header",
		"TRUE", "a", "1", "FALSE", "b")
	l := inv.Item.List
	if l.Type != wire.ListCheck || !l.Multiple || !l.Editable || !l.HideHeader || l.More ||
		!reflect.DeepEqual(l.Hidden, []int{2}) || !reflect.DeepEqual(l.Rows, [][]string{{"TRUE", "a", "1"}}) {
		t.Errorf("list = %+v", l)
	}
	if o := inv.List; !reflect.DeepEqual(o.Print, []int{3, 1}) || o.All || !o.Toggle || inv.Separator != "\t" {
		t.Errorf("output = %+v, separator %q", o, inv.Separator)
	}
	// With no values after the options, the rows come on stdin; a check or
	// radio list prints its second column, any other its first.
	inv = parsed(t, "--list", "--radiolist", "--column=a", "--column=b")
	if !inv.Item.List.More || inv.Item.List.Type != wire.ListRadio || !reflect.DeepEqual(inv.List.Print, []int{2}) {
		t.Errorf("list = %+v %+v", inv.Item.List, inv.List)
	}
	inv = parsed(t, "--list", "--imagelist", "--column=a", "--column=b", "--print-column=ALL", "i.png", "x")
	if !inv.List.All || inv.Item.List.Rows[0][0] != "/work/i.png" || !reflect.DeepEqual(inv.List.Print, []int(nil)) {
		t.Errorf("list = %+v %+v", inv.Item.List, inv.List)
	}
}

func TestAListsOutput(t *testing.T) {
	o := &ListOutput{Columns: 3, Print: []int{2}, Toggle: true}
	rows := [][]string{{"TRUE", "a", "1"}, {"TRUE", "b"}}
	if got := o.Text(rows, "|"); got != "a|b\n" {
		t.Errorf("got %q", got)
	}
	o.Print = []int{3}
	// A short row has nothing in the column to print.
	if got := o.Text(rows, "|"); got != "1\n" {
		t.Errorf("got %q", got)
	}
	o.All = true
	if got := o.Text(rows, ", "); got != "a, 1, b\n" {
		t.Errorf("got %q", got)
	}
	if got := o.Text(nil, "|"); got != "" {
		t.Errorf("nothing chosen printed %q", got)
	}
}

func TestAForm(t *testing.T) {
	inv := parsed(t, "--forms", "--text=<b>Who</b>", "--add-entry=Name", "--add-list=First",
		"--add-password=PIN", "--add-list=Second", "--add-combo=Size", "--add-combo=Bare",
		"--add-calendar=When", "--add-multiline-entry=Notes",
		"--list-values=a|b|c", "--list-values=d", "--column-values=x|y", "--column-values=z",
		"--combo-values=S|M", "--show-header", "--separator=,")
	f := inv.Item.Forms
	if inv.Item.Text != "<b>Who</b>" || !inv.Item.Markup || f.DateFormat != "%x" || inv.Separator != "," {
		t.Errorf("item = %+v", inv.Item)
	}
	want := []wire.Field{
		{Kind: wire.FieldEntry, Label: "Name"},
		{Kind: wire.FieldList, Label: "First", Columns: []string{"x", "y"}, Rows: [][]string{{"a", "b"}, {"c"}}, ShowHeader: true},
		{Kind: wire.FieldPassword, Label: "PIN"},
		{Kind: wire.FieldList, Label: "Second", Columns: []string{"z"}, Rows: [][]string{{"d"}}, ShowHeader: true},
		{Kind: wire.FieldCombo, Label: "Size", Values: []string{"S", "M"}},
		{Kind: wire.FieldCombo, Label: "Bare"},
		{Kind: wire.FieldCalendar, Label: "When"},
		{Kind: wire.FieldMultiline, Label: "Notes"},
	}
	if !reflect.DeepEqual(f.Fields, want) {
		t.Errorf("fields =\n%+v\nwant\n%+v", f.Fields, want)
	}
	// With no --column-values, a list has one column, "column".
	if c := parsed(t, "--forms", "--add-list=L").Item.Forms.Fields[0].Columns; !reflect.DeepEqual(c, []string{"column"}) {
		t.Errorf("columns = %q", c)
	}
}

func TestAFormsOutput(t *testing.T) {
	s := func(v string) *string { return &v }
	forms := &wire.Forms{Fields: []wire.Field{
		{Kind: wire.FieldEntry}, {Kind: wire.FieldList, Columns: []string{"a", "b"}},
		{Kind: wire.FieldCombo}, {Kind: wire.FieldList, Columns: []string{"c"}},
	}}
	got := FormsText(forms, []wire.FieldValue{{Text: s("x")}, {Rows: [][]string{{"1"}}}, {}, {Rows: [][]string{{"2"}}}}, "|")
	// A list not last: each cell then a comma, a missing one C's (null); a
	// combo with nothing picked a space; the last list's cells run on.
	if got != "x|1,(null),| |2\n" {
		t.Errorf("got %q", got)
	}
}

func TestACalendar(t *testing.T) {
	inv := parsed(t, "--calendar", "--day=9", "--date-format=%A")
	if c := inv.Item.Calendar; *c != (wire.Calendar{Day: 9, Month: 10, Year: 2026, Format: "%A"}) || len(inv.Warnings) != 0 {
		t.Errorf("calendar = %+v, warnings %q", c, inv.Warnings)
	}
	// A date that is none is said so, and today's instead.
	inv = parsed(t, "--calendar", "--day=31", "--month=2")
	if c := inv.Item.Calendar; *c != (wire.Calendar{Day: 3, Month: 10, Year: 2026, Format: "%x"}) ||
		!reflect.DeepEqual(inv.Warnings, []string{"Invalid date provided. Falling back to today's date."}) {
		t.Errorf("calendar = %+v, warnings %q", c, inv.Warnings)
	}
	inv = parsed(t, "--calendar", "--year=2024", "--month=2", "--day=29")
	if c := inv.Item.Calendar; c.Day != 29 || c.Month != 2 || c.Year != 2024 || len(inv.Warnings) != 0 {
		t.Errorf("calendar = %+v", c)
	}
}

func TestAScale(t *testing.T) {
	inv := parsed(t, "--scale", "--value=-5", "--min-value=-10", "--max-value=10", "--step=2", "--print-partial", "--hide-value")
	if s := inv.Item.Scale; *s != (wire.Scale{Value: -5, Min: -10, Max: 10, Step: 2, Hide: true, Partial: true}) {
		t.Errorf("scale = %+v", s)
	}
}

func TestAFileSelection(t *testing.T) {
	inv := parsed(t, "--file-selection", "--save", "--filename=out/report.txt", "--multiple",
		"--file-filter=Text files |  *.txt *.md", "--file-filter=*.png *.jpg", "--separator=:", "--extra-button=X")
	f := inv.Item.File
	want := wire.File{Mode: wire.FileSave, Multiple: true, Folder: "/work/out", Name: "report.txt", Filters: []wire.Filter{
		{Name: "Text files", Patterns: []string{"*.txt", "*.md"}}, {Name: "*.png *.jpg", Patterns: []string{"*.png", "*.jpg"}}}}
	if !reflect.DeepEqual(*f, want) || inv.Separator != ":" || inv.Extra != nil || len(inv.Item.Buttons) != 2 {
		t.Errorf("file = %+v, item %+v", f, inv.Item)
	}
	// --extra-button is warned of and dropped, as zenity does.
	if len(inv.Warnings) != 1 || !strings.Contains(inv.Warnings[0], "--extra-button option for --file-selection") {
		t.Errorf("warnings = %q", inv.Warnings)
	}
	// A directory ending in a slash is where to start; a bare name starts
	// nowhere in particular, and is the name only when saving.
	if f := parsed(t, "--file-selection", "--filename=/tmp/").Item.File; f.Folder != "/tmp" || f.Name != "" {
		t.Errorf("file = %+v", f)
	}
	if f := parsed(t, "--file-selection", "--save", "--filename=x.txt").Item.File; f.Folder != "" || f.Name != "x.txt" {
		t.Errorf("file = %+v", f)
	}
}

func TestAProgressBar(t *testing.T) {
	inv := parsed(t, "--progress", "--percentage=30", "--pulsate", "--time-remaining", "--ok-label=Done")
	if p := inv.Item.Progress; *p != (wire.Progress{Percentage: 30, Pulsate: true, TimeRemaining: true}) {
		t.Errorf("progress = %+v", p)
	}
	b := inv.Item.Buttons
	if len(b) != 2 || b[0].Label != "Cancel" || b[0].Disabled || b[1].Label != "Done" || !b[1].Disabled || inv.Item.Default != -1 {
		t.Errorf("buttons = %+v", b)
	}
	if b := parsed(t, "--progress", "--no-cancel", "--auto-close").Item.Buttons; len(b) != 0 {
		t.Errorf("buttons = %+v", b)
	}
}

func TestAProgressBarsStdin(t *testing.T) {
	start := time.Unix(1000, 0)
	p := (&ProgressOptions{TimeRemaining: true}).Reader()
	u, closed := p.Line("# Copying \\t<b>x</b>  \n", start)
	if *u.Text != "Copying \t<b>x</b>" || closed {
		t.Errorf("text = %+v", u)
	}
	if u, _ := p.Line("pulsate\n", start); u != nil {
		t.Errorf("pulsate with no colon = %+v", u)
	}
	if u, _ := p.Line("pulsate: TRUE\n", start); !*u.Pulsate {
		t.Errorf("pulsate = %+v", u)
	}
	if u, _ := p.Line("words\n", start); u != nil {
		t.Errorf("words = %+v", u)
	}
	// The first percentage starts the clock.
	if u, _ := p.Line("20\n", start); *u.Percentage != 20 || *u.Remaining != "" || u.Finished {
		t.Errorf("20 = %+v", u)
	}
	// 25% in 30 seconds: 90 more.
	if u, _ := p.Line("25\n", start.Add(30*time.Second)); *u.Remaining != "Time remaining: 0:01:30" {
		t.Errorf("25 = %q", *u.Remaining)
	}
	// Stopping the pulse shows the last percentage again.
	if u, _ := p.Line("pulsate:false\n", start); *u.Pulsate || *u.Percentage != 25 {
		t.Errorf("pulsate:false = %+v", u)
	}
	if u, _ := p.Line("100\n", start); !u.Finished {
		t.Errorf("100 = %+v", u)
	}
	if u, closed := p.End(); !u.Closed || !u.Finished || *u.Pulsate || *u.Percentage != 100 || closed {
		t.Errorf("end = %+v %v", u, closed)
	}
	auto := (&ProgressOptions{AutoClose: true}).Reader()
	if _, closed := auto.Line("100\n", start); !closed {
		t.Errorf("--auto-close did not close at 100")
	}
	if u, closed := auto.End(); u.Finished || !closed {
		t.Errorf("--auto-close end = %+v %v", u, closed)
	}
}

// zenity's stof: every digit counts, wherever it is.
func TestStofIsZenitys(t *testing.T) {
	for in, want := range map[string]float32{
		"50\n": 50, "12.5": 12.5, "12,5": 12.5, "-3": -3, "1a2": 12, "7%": 7, "1.2.3": 1.23,
	} {
		if got := stof(in); got != want {
			t.Errorf("stof(%q) = %v, want %v", in, got, want)
		}
	}
	p := (&ProgressOptions{}).Reader()
	if u, _ := p.Line("999\n", time.Now()); *u.Percentage != 100 || !u.Finished {
		t.Errorf("999 = %+v", u)
	}
}

func TestAListsRowsFromStdin(t *testing.T) {
	rows := (&ListOutput{Columns: 2, Images: true, Cwd: "/w"}).Rows()
	var got [][]string
	for _, line := range []string{"a.png\n", "one \t\r\n", "/b.png", "two\n", "c"} {
		if row := rows.Line(line); row != nil {
			got = append(got, row)
		}
	}
	got = append(got, rows.End())
	if !reflect.DeepEqual(got, [][]string{{"/w/a.png", "one"}, {"/b.png", "two"}, {"/w/c"}}) {
		t.Errorf("rows = %q", got)
	}
	if rows.End() != nil {
		t.Errorf("a second end")
	}
}

func TestANotificationsCommands(t *testing.T) {
	l := NewListener("/w")
	for _, c := range []struct {
		line string
		want Command
	}{
		{"message:hi\\nthere\n", Command{Notify: &wire.Notify{Text: "hi\nthere"}}},
		{"  ICON :  dialog-warning\n", Command{Icon: ptr("dialog-warning")}},
		{"tooltip:\tx\n", Command{Notify: &wire.Notify{Text: "x", Icon: "dialog-warning"}}},
		{"visible: false\n", Command{}},
		{"message:\n", Command{Warning: "Could not parse message"}},
		{"message:\xff\n", Command{Warning: "Invalid UTF-8 in input!"}},
		{"no colon\n", Command{Warning: "Could not parse command from stdin"}},
		{"hint: x\n", Command{Warning: "Unknown command 'hint'"}},
	} {
		if got := l.Line(c.line); !reflect.DeepEqual(got, c.want) {
			t.Errorf("%q = %+v, want %+v", c.line, got, c.want)
		}
	}
}

func ptr(s string) *string { return &s }

func TestANotification(t *testing.T) {
	inv := parsed(t, "--notification", `--text=Done\nAll fine`, "--icon=emblem-ok", "--extra-button=x", "--ok-label=y")
	if inv.Item.Text != "Done\nAll fine" || inv.Item.Icon != "emblem-ok" || inv.Item.Note.Listen ||
		inv.Extra != nil || len(inv.Item.Buttons) != 1 || inv.Item.Buttons[0].Label != "Dismiss" {
		t.Errorf("item = %+v", inv.Item)
	}
	// Listening, its --text and --icon are not used.
	inv = parsed(t, "--notification", "--listen", "--text=x", "--icon=y")
	if !inv.Listen || inv.Item.Text != "" || inv.Item.Icon != "dialog-information" {
		t.Errorf("item = %+v", inv.Item)
	}
}

func TestAnEntryWithValues(t *testing.T) {
	inv := parsed(t, "--entry", "--hide-text", "--entry-text=b", "a", "c")
	if e := inv.Item.Entry; !reflect.DeepEqual(e.Values, []string{"b", "a", "c"}) || e.Hidden || e.Text != "b" {
		t.Errorf("entry = %+v", e)
	}
	// One value alone, and zenity shows a plain entry, the value unused.
	if e := parsed(t, "--entry", "a").Item.Entry; e.Values != nil || e.Text != "" {
		t.Errorf("entry = %+v", e)
	}
	if e := parsed(t, "--entry", "--entry-text=b").Item.Entry; e.Values != nil || e.Text != "b" {
		t.Errorf("entry = %+v", e)
	}
}

func TestTheDeprecatedIconsAreIcons(t *testing.T) {
	inv := parsed(t, "--info", "--icon=a", "--window-icon=b")
	if inv.Item.Icon != "b" || len(inv.Warnings) != 1 {
		t.Errorf("icon %q, warnings %q", inv.Item.Icon, inv.Warnings)
	}
	inv = parsed(t, "--info", "--icon-name=c", "--window-icon=b", "--attach=9")
	if inv.Item.Icon != "c" || len(inv.Warnings) != 3 {
		t.Errorf("icon %q, warnings %q", inv.Item.Icon, inv.Warnings)
	}
}

func TestOutputs(t *testing.T) {
	s := func(v string) *string { return &v }
	n := 7
	for _, c := range []struct {
		args    []string
		answer  wire.Answer
		stdout  string
		outcome Outcome
	}{
		{[]string{"--scale"}, wire.Answer{Answer: wire.AnswerTimeout, Value: &n}, "7\n", Timeout},
		{[]string{"--color-selection"}, wire.Answer{Answer: wire.AnswerOK, Text: s("rgb(0,0,0)")}, "rgb(0,0,0)\n", OK},
		{[]string{"--file-selection"}, wire.Answer{Answer: wire.AnswerOK, Files: []string{"/a"}}, "/a\n", OK},
		{[]string{"--text-info", "--editable"}, wire.Answer{Answer: wire.AnswerTimeout, Text: s("t")}, "t", Timeout},
		{[]string{"--text-info"}, wire.Answer{Answer: wire.AnswerOK, Text: s("t")}, "", OK},
		{[]string{"--progress"}, wire.Answer{Answer: wire.AnswerOK}, "", OK},
		{[]string{"--progress", "--extra-button=Skip"}, wire.Answer{Answer: wire.AnswerExtra}, "Skip\n", Extra},
		{[]string{"--password", "--username"}, wire.Answer{Answer: wire.AnswerOK, Entry: s("p")}, "|p\n", OK},
	} {
		out, outcome, ok := parsed(t, c.args...).Output(c.answer)
		if !ok || out != c.stdout || outcome != c.outcome {
			t.Errorf("%q: %q %v %v", c.args, out, outcome, ok)
		}
	}
}
