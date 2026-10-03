package client

import (
	"net"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/danielbodart/galley/internal/wire"
)

func str(s string) *string { return &s }
func num(n int) *int       { return &n }

// Each dialog's answers, printed and exited with as zenity does.
func TestEachDialogPrintsAsZenity(t *testing.T) {
	for _, c := range []struct {
		name   string
		args   []string
		answer wire.Answer
		code   int
		stdout string
	}{
		{"list", []string{"--list", "--column=A", "--column=B", "a", "1"},
			wire.Answer{Answer: wire.AnswerOK, Rows: [][]string{{"a", "1"}, {"b", "2"}}}, 0, "a|b\n"},
		{"list, nothing chosen", []string{"--list", "--column=A", "a"}, wire.Answer{Answer: wire.AnswerOK}, 0, ""},
		{"list, its timeout", []string{"--list", "--column=A", "--print-column=1,1", `--separator=\n`, "a"},
			wire.Answer{Answer: wire.AnswerTimeout, Rows: [][]string{{"a"}}}, 5, "a\na\n"},
		{"checklist, every column", []string{"--list", "--checklist", "--column=", "--column=A", "--column=B", "--print-column=all"},
			wire.Answer{Answer: wire.AnswerOK, Rows: [][]string{{"TRUE", "a", "1"}}}, 0, "a|1\n"},
		{"list's extra", []string{"--list", "--column=A", "--extra-button=More", "a"},
			wire.Answer{Answer: wire.AnswerExtra}, 1, "More\n"},
		{"forms", []string{"--forms", "--add-entry=A", "--add-list=L", "--column-values=x|y", "--add-combo=C", "--separator=#"},
			wire.Answer{Answer: wire.AnswerOK, Fields: []wire.FieldValue{{Text: str("a")}, {Rows: [][]string{{"1"}}}, {}}}, 0, "a#1,(null),# \n"},
		{"forms, a list last", []string{"--forms", "--add-list=L", "--column-values=x|y"},
			wire.Answer{Answer: wire.AnswerOK, Fields: []wire.FieldValue{{Rows: [][]string{{"1", "2"}}}}}, 0, "12\n"},
		{"calendar", []string{"--calendar"}, wire.Answer{Answer: wire.AnswerOK, Text: str("05/03/24")}, 0, "05/03/24\n"},
		{"calendar, a format GLib cannot write", []string{"--calendar", "--date-format=%Q"}, wire.Answer{Answer: wire.AnswerOK}, 0, "(null)\n"},
		{"calendar's timeout", []string{"--calendar"}, wire.Answer{Answer: wire.AnswerTimeout, Text: str("x")}, 5, "x\n"},
		{"scale", []string{"--scale"}, wire.Answer{Answer: wire.AnswerOK, Value: num(42)}, 0, "42\n"},
		{"password", []string{"--password"}, wire.Answer{Answer: wire.AnswerOK, Entry: str("pw")}, 0, "pw\n"},
		{"password and username", []string{"--password", "--username"},
			wire.Answer{Answer: wire.AnswerOK, Username: str("dan"), Entry: str("pw")}, 0, "dan|pw\n"},
		{"password's timeout prints nothing", []string{"--password"},
			wire.Answer{Answer: wire.AnswerTimeout, Entry: str("pw")}, 5, ""},
		{"colour", []string{"--color-selection"}, wire.Answer{Answer: wire.AnswerOK, Text: str("rgb(1,2,3)")}, 0, "rgb(1,2,3)\n"},
		{"files", []string{"--file-selection", "--multiple", "--separator=,"},
			wire.Answer{Answer: wire.AnswerOK, Files: []string{"/a", "/b"}}, 0, "/a,/b\n"},
		{"editable text", []string{"--text-info", "--editable", "--filename=/dev/null"},
			wire.Answer{Answer: wire.AnswerOK, Text: str("no newline")}, 0, "no newline"},
		{"combo", []string{"--entry", "a", "b"}, wire.Answer{Answer: wire.AnswerOK, Entry: str("b")}, 0, "b\n"},
		{"about", []string{"--about"}, wire.Answer{Answer: wire.AnswerOK}, 0, ""},
	} {
		t.Run(c.name, func(t *testing.T) {
			w := newWindow(t, answering(c.answer))
			got := w.run("", nil, c.args...)
			if got.code != c.code || got.stdout != c.stdout {
				t.Errorf("got %d %q (stderr %q), want %d %q", got.code, got.stdout, got.stderr, c.code, c.stdout)
			}
		})
	}
}

func TestTheNewerDialogsSpeakTheSecondProtocol(t *testing.T) {
	for _, args := range [][]string{
		{"--scale"}, {"--entry", "a", "b"}, {"--text-info", "--editable", "--filename=/dev/null"},
	} {
		w := newWindow(t, answering(wire.Answer{Answer: wire.AnswerCancel}))
		w.run("", nil, args...)
		if hello := <-w.hellos; hello.Galley != 2 {
			t.Errorf("%q: version %d", args, hello.Galley)
		}
	}
}

func TestACalendarSendsTheCallersLocale(t *testing.T) {
	w := newWindow(t, answering(wire.Answer{Answer: wire.AnswerCancel}))
	w.run("", map[string]string{"LC_TIME": "de_DE.UTF-8", "LANG": "en_GB.UTF-8"}, "--calendar", "--day=2", "--month=1", "--year=2020")
	hello := <-w.hellos
	if hello.Item.Locale != "de_DE.UTF-8" || !reflect.DeepEqual(*hello.Item.Calendar,
		wire.Calendar{Day: 2, Month: 1, Year: 2020, Format: "%x"}) {
		t.Errorf("item = %+v %+v", hello.Item, hello.Item.Calendar)
	}
}

func TestAScalePrintsEachValueAsItMoves(t *testing.T) {
	w := newWindow(t, func(conn net.Conn, _ wire.Hello, _ <-chan wire.Follow) {
		reply(conn, wire.Answer{Partial: num(3)})
		reply(conn, wire.Answer{Partial: num(4)})
		reply(conn, wire.Answer{Answer: wire.AnswerOK, Value: num(4)})
	})
	if got := w.run("", nil, "--scale", "--print-partial"); got.code != 0 || got.stdout != "3\n4\n4\n" {
		t.Errorf("got %+v", got)
	}
}

func TestAListReadsItsRows(t *testing.T) {
	w := newWindow(t, func(conn net.Conn, _ wire.Hello, follows <-chan wire.Follow) {
		var rows [][]string
		for f := range follows {
			rows = append(rows, f.Rows...)
			if len(rows) == 3 {
				break
			}
		}
		reply(conn, wire.Answer{Answer: wire.AnswerOK, Rows: rows})
	})
	// The stand-in answers with every row it was sent, the last short.
	got := w.run("a\t\n1\nb\n2\nc", nil, "--list", "--column=A", "--column=B", "--print-column=ALL", "--separator=,")
	if got.stdout != "a,1,b,2,c\n" {
		t.Errorf("got %+v", got)
	}
	if hello := <-w.hellos; !hello.Item.List.More {
		t.Errorf("list = %+v", hello.Item.List)
	}
}

func TestAnImageListsPathsAreMadeAbsolute(t *testing.T) {
	w := newWindow(t, answering(wire.Answer{Answer: wire.AnswerCancel}))
	w.run("", nil, "--list", "--imagelist", "--column=I", "--column=N", "icon.png", "one", "/abs.png", "two")
	if rows := (<-w.hellos).Item.List.Rows; !reflect.DeepEqual(rows, [][]string{{"/icon.png", "one"}, {"/abs.png", "two"}}) {
		t.Errorf("rows = %q", rows)
	}
}

func TestAProgressBarsStdin(t *testing.T) {
	w := newWindow(t, func(conn net.Conn, _ wire.Hello, follows <-chan wire.Follow) {
		for f := range follows {
			if f.Progress != nil && f.Progress.Closed {
				reply(conn, wire.Answer{Answer: wire.AnswerOK})
				return
			}
		}
	})
	if got := w.run("10\n# half\n", nil, "--progress"); got.code != 0 {
		t.Errorf("got %+v", got)
	}
	<-w.closed
	var updates []wire.ProgressUpdate
	for len(w.follows) > 0 {
		updates = append(updates, *(<-w.follows).Progress)
	}
	if len(updates) != 3 || *updates[0].Percentage != 10 || *updates[1].Text != "half" ||
		!updates[2].Closed || !updates[2].Finished {
		t.Errorf("updates = %+v", updates)
	}
}

// --auto-close: zenity closes itself at 100%, OK.
func TestAProgressBarClosesItself(t *testing.T) {
	w := newWindow(t, func(_ net.Conn, _ wire.Hello, follows <-chan wire.Follow) {
		for range follows {
		}
	})
	if got := w.run("50\n100\n", map[string]string{"ZENITY_OK": "9"}, "--progress", "--auto-close"); got.code != 9 {
		t.Errorf("got %+v", got)
	}
}

func TestAutoKillHangsUpOnTheCaller(t *testing.T) {
	hung := false
	w := newWindow(t, answering(wire.Answer{Answer: wire.AnswerCancel}))
	got := runWith(w, func(e *Env) { e.HangUpParent = func() { hung = true } }, "--progress", "--auto-kill")
	if got.code != 1 || !hung {
		t.Errorf("got %+v, hung up %v", got, hung)
	}
	hung = false
	w = newWindow(t, answering(wire.Answer{Answer: wire.AnswerCancel}))
	runWith(w, func(e *Env) { e.HangUpParent = func() { hung = true } }, "--progress")
	if hung {
		t.Errorf("hung up without --auto-kill")
	}
}

func TestANotificationReturnsAtOnce(t *testing.T) {
	answered := make(chan struct{})
	w := newWindow(t, func(conn net.Conn, _ wire.Hello, _ <-chan wire.Follow) {
		reply(conn, wire.Answer{Queued: true})
		// Never answered: the client has gone.
		<-answered
	})
	defer close(answered)
	start := time.Now()
	got := w.run("", map[string]string{"ZENITY_OK": "9"}, "--notification", `--text=Done\nbody`)
	// zenity returns 0 itself, not its OK.
	if got.code != 0 || time.Since(start) > time.Second {
		t.Errorf("got %+v", got)
	}
	if item := (<-w.hellos).Item; item.Text != "Done\nbody" || item.Note.Listen {
		t.Errorf("item = %+v", item)
	}
}

func TestANotificationWithATimeoutWaitsForIt(t *testing.T) {
	w := newWindow(t, func(conn net.Conn, _ wire.Hello, follows <-chan wire.Follow) {
		reply(conn, wire.Answer{Queued: true})
		for range follows {
		}
	})
	if got := w.run("", nil, "--notification", "--text=x", "--timeout=1"); got.code != 5 || got.stdout != "" {
		t.Errorf("got %+v", got)
	}
	w = newWindow(t, func(conn net.Conn, _ wire.Hello, _ <-chan wire.Follow) {
		reply(conn, wire.Answer{Queued: true})
		reply(conn, wire.Answer{Answer: wire.AnswerOK})
	})
	if got := w.run("", nil, "--notification", "--text=x", "--timeout=10"); got.code != 0 {
		t.Errorf("got %+v", got)
	}
}

func TestAListeningNotification(t *testing.T) {
	w := newWindow(t, func(_ net.Conn, _ wire.Hello, follows <-chan wire.Follow) {
		for range follows {
		}
	})
	got := w.run("icon: /i.png\nmessage: one\\ttab\nnonsense\nvisible:true\nwhat: x\ntooltip:two\n", nil, "--notification", "--listen")
	if got.code != 0 || got.stderr != "Could not parse command from stdin\nUnknown command 'what'\n" {
		t.Errorf("got %+v", got)
	}
	<-w.closed
	var notes []wire.Notify
	for len(w.follows) > 0 {
		notes = append(notes, *(<-w.follows).Notify)
	}
	if !reflect.DeepEqual(notes, []wire.Notify{{Text: "one\ttab", Icon: "/i.png"}, {Text: "two", Icon: "/i.png"}}) {
		t.Errorf("notes = %+v", notes)
	}
}

// What zenity finds wrong once its dialog has started is said, and exited
// with, as zenity does -- 0, from a dialog that returns without quitting.
func TestWhatZenityRefusesOnceStarted(t *testing.T) {
	w := &window{dir: "/nonexistent"}
	for _, c := range []struct {
		args   []string
		code   int
		stderr string
	}{
		{[]string{"--list"}, 0, "No column titles specified for List dialog.\n"},
		{[]string{"--list", "--mid-search", "--checklist", "--column=a"}, 0,
			"Warning: --mid-search is deprecated and will be removed in a future version of zenity. Ignoring.\n" +
				"Insufficient columns specified for List dialog (at least 2 are required for --checklist or --radiolist).\n"},
		{[]string{"--list", "--column=a", "--column=b", "--checklist", "--imagelist"}, 0, "You should use only one List dialog type.\n"},
		{[]string{"--scale", "--min-value=5", "--max-value=5"}, 0, "Maximum value must be greater than minimum value.\n"},
		{[]string{"--scale", "--value=101"}, 0, "Value out of range.\n"},
		{[]string{"--progress", "--auto-close", "--percentage=100"}, 255, "Combining the options --auto-close and --percentage=100 is not supported.\n"},
		{[]string{"--notification"}, 1, ""},
		{[]string{"--notification", "--text="}, 1, "Could not parse message\n"},
		{[]string{"--question", "--window-icon=x", "--day=2"}, 255,
			"Warning: --window-icon is deprecated and will be removed in a future version of zenity; Treating as --icon.\n" +
				"--day is not supported for this dialog\n"},
	} {
		got := w.run("", nil, c.args...)
		if got.code != c.code || got.stderr != c.stderr || got.stdout != "" {
			t.Errorf("%q: got %+v", c.args, got)
		}
	}
}

func TestATimeoutZenityTakesWithoutItsDialog(t *testing.T) {
	// The colour and file choosers are not zenity's own dialogs, and its
	// timeout exits from under them having printed nothing.
	for _, args := range [][]string{{"--color-selection"}, {"--file-selection"}} {
		w := newWindow(t, func(_ net.Conn, _ wire.Hello, follows <-chan wire.Follow) {
			for f := range follows {
				if f.Timeout {
					t.Errorf("%q asked the window", args)
				}
			}
		})
		if got := w.run("", nil, append(args, "--timeout=1")...); got.code != 5 || got.stdout != "" {
			t.Errorf("%q: got %+v", args, got)
		}
	}
}

func runWith(w *window, change func(*Env), args ...string) result {
	var out, errs strings.Builder
	vars := map[string]string{"XDG_RUNTIME_DIR": w.dir}
	env := Env{
		Stdin:   strings.NewReader(""),
		Stdout:  &out,
		Stderr:  &errs,
		Getenv:  func(k string) string { return vars[k] },
		Cwd:     "/",
		Version: "test",
		Grace:   200 * time.Millisecond,
	}
	change(&env)
	code := Main(append([]string{"galley"}, args...), env)
	return result{code, out.String(), errs.String()}
}
