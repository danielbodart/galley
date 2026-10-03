package tests

// End to end, each of zenity's dialogs after the first six: the item each
// becomes, what the person does to it through the test control, and what
// the client prints and exits with.

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"
)

type table struct {
	Rows     [][]string `json:"rows"`
	Checked  []bool     `json:"checked"`
	Selected []int      `json:"selected"`
}

func fill(t *testing.T, v any) {
	t.Helper()
	var r pressed
	if err := json.Unmarshal(control(t, map[string]any{"fill": v}), &r); err != nil || r.Error != "" {
		t.Fatalf("fill %v: %v %s", v, err, r.Error)
	}
}

func TestAList(t *testing.T) {
	need(t)
	r := start(t, "", "--list", "--title=Pick one", "--column=Name", "--column=Size",
		"apple", "3", "pear", "5", "fig", "1", "short")
	s := waitFor(t, "the list", items(1))
	it := s.Items[0]
	if it.Kind != "list" || it.Text != "Select items from the list below." {
		t.Errorf("item = %+v", it)
	}
	// A short last row is dropped, and the first row is chosen at the start.
	got := body[table](t, it.Body)
	if !reflect.DeepEqual(got.Rows, [][]string{{"apple", "3"}, {"pear", "5"}, {"fig", "1"}}) ||
		!reflect.DeepEqual(got.Selected, []int{0}) {
		t.Errorf("table = %+v", got)
	}
	fill(t, map[string]any{"select": []int{1}})
	press(t, "Return")
	r.expect(t, 0, "pear\n")

	// Every column, with the separator's escapes undone.
	r = start(t, "", "--list", "--column=Name", "--column=Size", "--print-column=ALL",
		`--separator=\t`, "apple", "3", "pear", "5")
	waitFor(t, "the list", items(1))
	press(t, "Return")
	r.expect(t, 0, "apple\t3\n")
}

func TestAListReadsItsRowsFromStdin(t *testing.T) {
	need(t)
	r, in := startPiped(t, "--list", "--column=Name", "--column=Size", "--print-column=2")
	io.WriteString(in, "apple\n3\npear  \n")
	waitFor(t, "the first row", func(s state) bool {
		return len(s.Items) == 1 && len(body[table](t, s.Items[0].Body).Rows) == 1
	})
	io.WriteString(in, "5\nfig\n")
	in.Close()
	s := waitFor(t, "the rest", func(s state) bool {
		return len(s.Items) == 1 && len(body[table](t, s.Items[0].Body).Rows) == 3
	})
	// Trailing space is trimmed from each line; a short last row is shown.
	if got := body[table](t, s.Items[0].Body).Rows; !reflect.DeepEqual(got, [][]string{{"apple", "3"}, {"pear", "5"}, {"fig"}}) {
		t.Errorf("rows = %q", got)
	}
	fill(t, map[string]any{"select": []int{1}})
	press(t, "Return")
	r.expect(t, 0, "5\n")
}

func TestAChecklistAndARadiolist(t *testing.T) {
	need(t)
	r := start(t, "", "--list", "--checklist", "--column=Pick", "--column=Fruit",
		"TRUE", "apple", "FALSE", "pear", "true", "fig")
	s := waitFor(t, "the list", items(1))
	if got := body[table](t, s.Items[0].Body).Checked; !reflect.DeepEqual(got, []bool{true, false, true}) {
		t.Errorf("checked = %v", got)
	}
	fill(t, map[string]any{"tick": 1})
	fill(t, map[string]any{"tick": 0})
	press(t, "Return")
	r.expect(t, 0, "pear|fig\n")

	// A radio list's last TRUE is its choice; ticking another moves it.
	r = start(t, "", "--list", "--radiolist", "--column=Pick", "--column=Fruit", "--column=Colour",
		"--print-column=ALL", "TRUE", "apple", "red", "TRUE", "pear", "green", "FALSE", "fig", "purple")
	s = waitFor(t, "the list", items(1))
	if got := body[table](t, s.Items[0].Body).Checked; !reflect.DeepEqual(got, []bool{false, true, false}) {
		t.Errorf("checked = %v", got)
	}
	fill(t, map[string]any{"tick": 2})
	press(t, "Return")
	// ALL is every column but the tick.
	r.expect(t, 0, "fig|purple\n")
}

func TestAMultipleEditableList(t *testing.T) {
	need(t)
	r := start(t, "", "--list", "--multiple", "--editable", "--column=Name", "--hide-column=1",
		"--separator=,", "a", "b", "c")
	waitFor(t, "the list", items(1))
	fill(t, map[string]any{"select": []int{0, 2}, "edit": []any{2, 0, "c, edited"}})
	press(t, "Return")
	r.expect(t, 0, "a,c, edited\n")

	// Nothing chosen prints nothing at all.
	r = start(t, "", "--list", "--multiple", "--column=Name", "a")
	waitFor(t, "the list", items(1))
	press(t, "Return")
	r.expect(t, 0, "")
}

func TestAListPrintsWhatIsChosenWhenItTimesOut(t *testing.T) {
	need(t)
	r := start(t, "", "--list", "--column=Name", "--timeout=1", "first", "second")
	r.expect(t, 5, "first\n")
}

// The arrows move through a list while it has the focus; j and k, and Alt
// with an arrow, still move through the queue.
func TestAListKeepsTheArrows(t *testing.T) {
	need(t)
	start(t, "", "--show").expect(t, 0, "")
	l := start(t, "", "--list", "--column=Name", "a", "b")
	waitFor(t, "the list", items(1))
	q := start(t, "", "--question", "--text=next")
	s := waitFor(t, "the question", items(2))
	if s.Focus != "ColumnView" {
		t.Fatalf("focus = %q", s.Focus)
	}
	if k := key(t, "Down"); k.Handled {
		t.Errorf("Down was taken from the list")
	}
	if s := current(t); *s.Selected != s.Items[0].ID {
		t.Errorf("Down moved the queue")
	}
	press(t, "j")
	if s := current(t); *s.Selected != s.Items[1].ID {
		t.Errorf("j did not move the queue")
	}
	press(t, "y")
	q.expect(t, 0, "")
	press(t, "c")
	l.expect(t, 1, "")
	press(t, "Escape")
}

func TestAForm(t *testing.T) {
	need(t)
	r := start(t, "", "--forms", "--title=Sign up", "--text=Who <b>are</b> you?",
		"--add-entry=Name", "--add-password=PIN", "--add-combo=Size", "--combo-values=S|M|L",
		"--add-list=Pick", "--list-values=a|b", "--add-multiline-entry=Notes",
		"--add-calendar=When", "--forms-date-format=%Y-%m-%d", "--separator=;")
	s := waitFor(t, "the form", items(1))
	if s.Items[0].Kind != "forms" || s.Items[0].Text != "Who are you?" {
		t.Errorf("item = %+v", s.Items[0])
	}
	if s.Focus != "entry" {
		t.Errorf("focus = %q", s.Focus)
	}
	fill(t, map[string]any{"0": "Dan", "1": "1234", "2": 1, "3": map[string]any{"select": []int{1}},
		"4": "line one\nline two", "5": "2024-03-05"})
	press(t, "Return")
	// A list not last has a comma after each cell, then the separator.
	r.expect(t, 0, "Dan;1234;M;b,;line one\nline two;2024-03-05\n")

	// Nothing filled in: an empty entry, a combo's first value, a list with
	// nothing selected -- run into what follows, being last.
	r = start(t, "", "--forms", "--add-combo=Size", "--add-entry=Name", "--add-list=Pick", "--list-values=x")
	waitFor(t, "the form", items(1))
	press(t, "Return")
	r.expect(t, 0, " ||\n")
}

func TestACalendar(t *testing.T) {
	need(t)
	r := start(t, "", "--calendar", "--day=5", "--month=3", "--year=2024", "--date-format=%d.%m.%Y (%A)")
	s := waitFor(t, "the calendar", items(1))
	if got := body[map[string]string](t, s.Items[0].Body)["date"]; got != "2024-03-05" {
		t.Errorf("date = %q", got)
	}
	press(t, "Return")
	r.expect(t, 0, "05.03.2024 (Tuesday)\n")

	r = start(t, "", "--calendar", "--day=5", "--month=3", "--year=2024", "--date-format=%Y")
	waitFor(t, "the calendar", items(1))
	fill(t, "1999-12-31")
	press(t, "c")
	r.expect(t, 1, "")
}

func TestAScale(t *testing.T) {
	need(t)
	r := start(t, "", "--scale", "--value=30", "--min-value=0", "--max-value=50", "--step=5", "--print-partial")
	s := waitFor(t, "the scale", items(1))
	if got := body[map[string]int](t, s.Items[0].Body)["value"]; got != 30 {
		t.Errorf("value = %d", got)
	}
	fill(t, 40)
	fill(t, 45)
	press(t, "Return")
	// Each value as it moved, and then the one answered.
	r.expect(t, 0, "40\n45\n45\n")

	r = start(t, "", "--scale", "--timeout=1", "--value=7")
	r.expect(t, 5, "7\n")
}

func TestAPassword(t *testing.T) {
	need(t)
	r := start(t, "", "--password", "--username", "--title=Log in")
	s := waitFor(t, "the prompt", items(1))
	if s.Items[0].Text != "Type your username and password" || s.Focus != "entry" {
		t.Errorf("item = %+v, focus %q", s.Items[0], s.Focus)
	}
	fill(t, map[string]any{"username": "dan", "password": "correct horse"})
	if strings.Contains(string(current(t).Items[0].Body), "horse") {
		t.Errorf("the password is in the window's state: %s", current(t).Items[0].Body)
	}
	press(t, "Return")
	r.expect(t, 0, "dan|correct horse\n")

	// A password's timeout prints nothing.
	r = start(t, "", "--password", "--timeout=1")
	waitFor(t, "the prompt", items(1))
	fill(t, map[string]any{"password": "half"})
	r.expect(t, 5, "")
}

func TestAColour(t *testing.T) {
	need(t)
	r := start(t, "", "--color-selection", "--color=#ff0000")
	s := waitFor(t, "the chooser", items(1))
	if fmt.Sprint(s.Items[0].Buttons) != "[{Cancel c true} {Select s true}]" {
		t.Errorf("buttons = %v", s.Items[0].Buttons)
	}
	press(t, "Return")
	r.expect(t, 0, "rgb(255,0,0)\n")

	r = start(t, "", "--color-selection", "--show-palette")
	waitFor(t, "the chooser", items(1))
	fill(t, "rgba(0,0,255,0.5)")
	press(t, "s")
	r.expect(t, 0, "rgba(0,0,255,0.5)\n")
}

type chooser struct {
	Choosing bool `json:"choosing"`
	Request  *struct {
		Title    string `json:"title"`
		Mode     string `json:"mode"`
		Multiple bool   `json:"multiple"`
		Folder   string `json:"folder"`
		Name     string `json:"name"`
		Filters  []struct {
			Name     string   `json:"name"`
			Patterns []string `json:"patterns"`
		} `json:"filters"`
	} `json:"request"`
}

func TestAFileSelection(t *testing.T) {
	need(t)
	dir := t.TempDir()
	r := start(t, "", "--file-selection", "--save", "--filename="+filepath.Join(dir, "report.txt"),
		"--file-filter=Text | *.txt *.md", "--file-filter=*.pdf")
	s := waitFor(t, "the item", items(1))
	it := s.Items[0]
	if it.Title != "Save File" || it.Text != "Choose where to save a file." ||
		fmt.Sprint(it.Buttons) != "[{Cancel c true} {Save… s true}]" {
		t.Errorf("item = %+v", it)
	}
	// Dismissing the chooser leaves the item waiting.
	control(t, map[string]any{"choose": nil})
	press(t, "Return")
	time.Sleep(200 * time.Millisecond)
	r.waiting(t)
	got := body[chooser](t, current(t).Items[0].Body)
	if got.Request == nil || got.Request.Mode != "save" || got.Request.Folder != dir ||
		got.Request.Name != "report.txt" || len(got.Request.Filters) != 2 ||
		got.Request.Filters[0].Name != "Text" || strings.Join(got.Request.Filters[0].Patterns, " ") != "*.txt *.md" ||
		got.Request.Filters[1].Name != "*.pdf" {
		t.Errorf("chooser = %+v", got.Request)
	}
	control(t, map[string]any{"choose": []string{filepath.Join(dir, "out.txt")}})
	press(t, "s")
	r.expect(t, 0, filepath.Join(dir, "out.txt")+"\n")

	r = start(t, "", "--file-selection", "--multiple", "--separator=:")
	waitFor(t, "the item", items(1))
	control(t, map[string]any{"choose": []string{"/a", "/b c"}})
	press(t, "o")
	r.expect(t, 0, "/a:/b c\n")

	// Its timeout is zenity's: an exit, with nothing printed.
	r = start(t, "", "--file-selection", "--timeout=1")
	r.expect(t, 5, "")
}

type progressState struct {
	Percentage float64 `json:"percentage"`
	Pulsing    bool    `json:"pulsing"`
	Remaining  string  `json:"remaining"`
}

func TestAProgressBar(t *testing.T) {
	need(t)
	r, in := startPiped(t, "--progress", "--title=Copying", "--text=Starting")
	s := waitFor(t, "the bar", items(1))
	if fmt.Sprint(s.Items[0].Buttons) != "[{Cancel c true} {OK o false}]" || s.Items[0].Default != -1 {
		t.Errorf("buttons = %v, default %d", s.Items[0].Buttons, s.Items[0].Default)
	}
	io.WriteString(in, "10\n# Copying <b>a</b>\\tfile\n")
	s = waitFor(t, "the update", func(s state) bool {
		return len(s.Items) == 1 && s.Items[0].Text == "Copying a\tfile"
	})
	if got := body[progressState](t, s.Items[0].Body); got.Percentage != 10 {
		t.Errorf("progress = %+v", got)
	}
	// OK is not there to press until it is done.
	press(t, "Return")
	press(t, "o")
	time.Sleep(200 * time.Millisecond)
	r.waiting(t)
	io.WriteString(in, "100\n")
	waitFor(t, "OK", func(s state) bool { return len(s.Items) == 1 && s.Items[0].Buttons[1].Enabled })
	press(t, "Return")
	r.expect(t, 0, "")

	// stdin closed: done, and Cancel no longer offered.
	r, in = startPiped(t, "--progress", "--pulsate")
	s = waitFor(t, "the bar", items(1))
	if !body[progressState](t, s.Items[0].Body).Pulsing {
		t.Errorf("not pulsing")
	}
	in.Close()
	s = waitFor(t, "the end", func(s state) bool {
		return len(s.Items) == 1 && s.Items[0].Buttons[1].Enabled && !s.Items[0].Buttons[0].Enabled
	})
	if got := body[progressState](t, s.Items[0].Body); got.Pulsing || got.Percentage != 100 {
		t.Errorf("progress = %+v", got)
	}
	press(t, "Return")
	r.expect(t, 0, "")
}

func TestAProgressBarClosesItself(t *testing.T) {
	need(t)
	r, in := startPiped(t, "--progress", "--auto-close", "--no-cancel")
	s := waitFor(t, "the bar", items(1))
	if len(s.Items[0].Buttons) != 0 {
		t.Errorf("buttons = %v", s.Items[0].Buttons)
	}
	io.WriteString(in, "50\n100\n")
	r.expect(t, 0, "")
	waitFor(t, "the bar to go", items(0))

	r, _ = startPiped(t, "--progress")
	waitFor(t, "the bar", items(1))
	press(t, "c")
	r.expect(t, 1, "")
}

func TestANotification(t *testing.T) {
	need(t)
	// It returns at once, and its entry stays, its client gone.
	start(t, "", "--notification", `--text=Build done\nAll green`, "--icon=emblem-ok").expect(t, 0, "")
	s := waitFor(t, "the entry", items(1))
	it := s.Items[0]
	if it.Kind != "notification" || it.Text != "Build done\nAll green" || it.Icon != "emblem-ok" ||
		it.Connected || fmt.Sprint(it.Buttons) != "[{Dismiss d true}]" {
		t.Errorf("item = %+v", it)
	}
	press(t, "Return")
	waitFor(t, "it to be dismissed", items(0))

	// With a timeout zenity waits for it, or for the notification to be
	// clicked.
	r := start(t, "", "--notification", "--text=wait", "--timeout=10")
	waitFor(t, "the entry", items(1))
	press(t, "d")
	r.expect(t, 0, "")
}

func TestAListeningNotification(t *testing.T) {
	need(t)
	r, in := startPiped(t, "--notification", "--listen", "--title=Builds")
	io.WriteString(in, "icon: emblem-ok\nmessage: first\n")
	s := waitFor(t, "the first", items(1))
	if s.Items[0].Text != "first" || s.Items[0].Group != "Builds" || s.Items[0].Icon != "emblem-ok" {
		t.Errorf("item = %+v", s.Items[0])
	}
	// Each message replaces the last, as each replaces zenity's one
	// notification.
	io.WriteString(in, "tooltip:second\\tline\nvisible: false\nbogus\n")
	waitFor(t, "the second", func(s state) bool { return len(s.Items) == 1 && s.Items[0].Text == "second\tline" })
	// Once dismissed, the next is a new entry.
	press(t, "Return")
	waitFor(t, "it to be dismissed", items(0))
	io.WriteString(in, "message:third\n")
	waitFor(t, "the third", func(s state) bool { return len(s.Items) == 1 && s.Items[0].Text == "third" })
	in.Close()
	r.expect(t, 0, "")
	if !strings.Contains(r.stderr.String(), "Could not parse command from stdin") {
		t.Errorf("stderr = %q", r.stderr.String())
	}
	// It outlives its client.
	if s := current(t); len(s.Items) != 1 || s.Items[0].Connected {
		t.Errorf("after the client: %+v", s.Items)
	}
	press(t, "d")
}

func TestAnEntryWithValuesIsACombo(t *testing.T) {
	need(t)
	r := start(t, "", "--entry", "--entry-text=pear", "apple", "fig")
	s := waitFor(t, "the combo", items(1))
	if got := body[map[string]any](t, s.Items[0].Body); fmt.Sprint(got["values"]) != "[pear apple fig]" || got["entry"] != "pear" {
		t.Errorf("combo = %v", got)
	}
	press(t, "Return")
	r.expect(t, 0, "pear\n")

	r = start(t, "", "--entry", "apple", "fig")
	waitFor(t, "the combo", items(1))
	control(t, map[string]any{"entry": "typed"})
	press(t, "Return")
	r.expect(t, 0, "typed\n")
}

func TestAnEditableTextInfo(t *testing.T) {
	need(t)
	start(t, "", "--show").expect(t, 0, "")
	r := start(t, "draft\n", "--text-info", "--editable")
	s := waitFor(t, "the text", func(s state) bool { return len(s.Items) == 1 && s.Items[0].Info == "draft\n" })
	if s.Focus != "entry" {
		t.Errorf("focus = %q", s.Focus)
	}
	// Enter is a new line in it, not OK.
	if k := key(t, "Return"); k.Handled || k.To != "text" {
		t.Errorf("Return = %+v", k)
	}
	fill(t, "final text")
	press(t, "o", map[string]any{"alt": true})
	// What it holds, printed with no newline added.
	r.expect(t, 0, "final text")
	press(t, "Escape")
}

func TestAbout(t *testing.T) {
	need(t)
	r := start(t, "", "--about")
	s := waitFor(t, "the item", items(1))
	if !strings.HasPrefix(s.Items[0].Text, "galley ") || fmt.Sprint(s.Items[0].Buttons) != "[{Close c true}]" {
		t.Errorf("item = %+v", s.Items[0])
	}
	press(t, "Return")
	r.expect(t, 0, "")
}

// A client of the first protocol still reaches the window, and is refused
// what only the second has.
func TestAVersionOneClientStillAsks(t *testing.T) {
	need(t)
	c, err := net.Dial("unix", filepath.Join(runtimeDir, "galley", "sock"))
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	fmt.Fprintln(c, `{"galley":1,"item":{"kind":"question","title":"Old","buttons":[{"answer":"ok","label":"Yes","key":"y","underline":0}],"default":0}}`)
	waitFor(t, "the question", items(1))
	press(t, "y")
	line, _ := bufio.NewReader(c).ReadString('\n')
	if strings.TrimSpace(line) != `{"answer":"ok"}` {
		t.Errorf("answer = %q", line)
	}

	c2, err := net.Dial("unix", filepath.Join(runtimeDir, "galley", "sock"))
	if err != nil {
		t.Fatal(err)
	}
	defer c2.Close()
	fmt.Fprintln(c2, `{"galley":1,"item":{"kind":"scale","buttons":[]}}`)
	line, _ = bufio.NewReader(c2).ReadString('\n')
	if !strings.Contains(line, "not one galley shows") {
		t.Errorf("reply = %q", line)
	}
}
