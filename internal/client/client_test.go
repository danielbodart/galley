package client

import (
	"bufio"
	"bytes"
	"encoding/json"
	"net"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/danielbodart/galley/internal/wire"
)

// window is a stand-in for the real one: it accepts one client, hands the
// test what it said, and answers as the test tells it.
type window struct {
	t       *testing.T
	dir     string
	ln      *net.UnixListener
	hellos  chan wire.Hello
	follows chan wire.Follow
	closed  chan struct{}
	answer  func(conn net.Conn, hello wire.Hello, follows <-chan wire.Follow)
}

func newWindow(t *testing.T, answer func(net.Conn, wire.Hello, <-chan wire.Follow)) *window {
	// Short, so the socket's path fits in sun_path wherever TMPDIR is.
	dir, err := os.MkdirTemp("", "g")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.RemoveAll(dir) })
	os.Chmod(dir, 0o700)
	os.Mkdir(filepath.Join(dir, "galley"), 0o700)
	ln, err := net.ListenUnix("unix", &net.UnixAddr{Name: filepath.Join(dir, "galley", "sock"), Net: "unix"})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { ln.Close() })
	w := &window{t: t, dir: dir, ln: ln, hellos: make(chan wire.Hello, 1),
		follows: make(chan wire.Follow, 100), closed: make(chan struct{}), answer: answer}
	go w.serve()
	return w
}

func (w *window) serve() {
	conn, err := w.ln.Accept()
	if err != nil {
		return
	}
	defer conn.Close()
	r := bufio.NewReader(conn)
	line, err := r.ReadBytes('\n')
	if err != nil {
		return
	}
	var hello wire.Hello
	json.Unmarshal(line, &hello)
	w.hellos <- hello
	follows := make(chan wire.Follow, 100)
	go func() {
		defer close(w.closed)
		defer close(follows)
		for {
			line, err := r.ReadBytes('\n')
			if err != nil {
				return
			}
			var f wire.Follow
			json.Unmarshal(line, &f)
			w.follows <- f
			follows <- f
		}
	}()
	w.answer(conn, hello, follows)
}

func reply(conn net.Conn, a wire.Answer) {
	json.NewEncoder(conn).Encode(a)
}

func answering(a wire.Answer) func(net.Conn, wire.Hello, <-chan wire.Follow) {
	return func(conn net.Conn, _ wire.Hello, _ <-chan wire.Follow) { reply(conn, a) }
}

type result struct {
	code           int
	stdout, stderr string
}

func (w *window) run(stdin string, env map[string]string, args ...string) result {
	var out, errs bytes.Buffer
	vars := map[string]string{"XDG_RUNTIME_DIR": w.dir}
	for k, v := range env {
		vars[k] = v
	}
	code := Main(append([]string{"galley"}, args...), Env{
		Stdin:   strings.NewReader(stdin),
		Stdout:  &out,
		Stderr:  &errs,
		Getenv:  func(k string) string { return vars[k] },
		Cwd:     "/",
		Version: "test",
		Grace:   200 * time.Millisecond,
	})
	return result{code, out.String(), errs.String()}
}

var asker = []string{"--question", "--no-markup", "--title=Allow this request?",
	"--ok-label=Allow", "--cancel-label=Refuse", "--extra-button=Ask", "--width=640", "--text=GET /"}

func TestEachAnswerExitsAndPrintsAsZenity(t *testing.T) {
	entry := "hunter2"
	empty := ""
	for _, c := range []struct {
		name   string
		args   []string
		answer wire.Answer
		code   int
		stdout string
	}{
		{"allow", asker, wire.Answer{Answer: wire.AnswerOK}, 0, ""},
		{"refuse", asker, wire.Answer{Answer: wire.AnswerCancel}, 1, ""},
		// The extra button's label, as given, on stdout, and exit 1.
		{"ask", asker, wire.Answer{Answer: wire.AnswerExtra, Index: 0}, 1, "Ask\n"},
		{"mnemonic extra", []string{"--question", "--extra-button=_Later"}, wire.Answer{Answer: wire.AnswerExtra}, 1, "_Later\n"},
		{"password", []string{"--entry", "--hide-text"}, wire.Answer{Answer: wire.AnswerOK, Entry: &entry}, 0, "hunter2\n"},
		{"empty password", []string{"--entry", "--hide-text"}, wire.Answer{Answer: wire.AnswerOK, Entry: &empty}, 0, "\n"},
		{"cancelled password", []string{"--entry", "--hide-text"}, wire.Answer{Answer: wire.AnswerCancel}, 1, ""},
		// zenity prints an entry's text when it times out, too.
		{"entry timeout", []string{"--entry"}, wire.Answer{Answer: wire.AnswerTimeout, Entry: &entry}, 5, "hunter2\n"},
		{"question timeout", asker, wire.Answer{Answer: wire.AnswerTimeout}, 5, ""},
		{"approve", []string{"--text-info", "--filename=/dev/null"}, wire.Answer{Answer: wire.AnswerOK}, 0, ""},
		{"info", []string{"--info"}, wire.Answer{Answer: wire.AnswerOK}, 0, ""},
		{"unknown answer", asker, wire.Answer{Answer: "maybe"}, 255, ""},
		{"extra out of range", asker, wire.Answer{Answer: wire.AnswerExtra, Index: 3}, 255, ""},
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

func TestTheItemIsSentWhole(t *testing.T) {
	w := newWindow(t, answering(wire.Answer{Answer: wire.AnswerOK}))
	w.run("", nil, asker...)
	hello := <-w.hellos
	if hello.Galley != wire.Version || hello.Item == nil {
		t.Fatalf("hello = %+v", hello)
	}
	item := hello.Item
	if item.Kind != wire.KindQuestion || item.Title != "Allow this request?" || item.Text != "GET /" ||
		item.Width != 640 || len(item.Buttons) != 3 || item.Default != 1 {
		t.Errorf("item = %+v", item)
	}
}

func TestEnvironmentOverridesExitCodes(t *testing.T) {
	w := newWindow(t, answering(wire.Answer{Answer: wire.AnswerCancel}))
	if got := w.run("", map[string]string{"ZENITY_CANCEL": "7"}, asker...); got.code != 7 {
		t.Errorf("code = %d", got.code)
	}
}

func TestTextInfoReadsItsFile(t *testing.T) {
	file := filepath.Join(t.TempDir(), "diff")
	os.WriteFile(file, []byte("--- a\n+++ b\n"), 0o600)
	w := newWindow(t, answering(wire.Answer{Answer: wire.AnswerOK}))
	w.run("", nil, "--text-info", "--filename="+file)
	hello := <-w.hellos
	if hello.Item.Info.Text != "--- a\n+++ b\n" || hello.Item.Info.More {
		t.Errorf("info = %+v", hello.Item.Info)
	}
}

func TestTextInfoWithoutAFileStillAsks(t *testing.T) {
	w := newWindow(t, answering(wire.Answer{Answer: wire.AnswerOK}))
	got := w.run("", nil, "--text-info", "--filename=/nonexistent/x")
	if got.code != 0 || !strings.Contains(got.stderr, "Cannot open file '/nonexistent/x'") {
		t.Errorf("got %+v", got)
	}
}

func TestTextInfoStreamsStdin(t *testing.T) {
	w := newWindow(t, func(conn net.Conn, _ wire.Hello, follows <-chan wire.Follow) {
		var text string
		for f := range follows {
			text += f.Append
			if text == "line one\nlïne two\n" {
				break
			}
		}
		reply(conn, wire.Answer{Answer: wire.AnswerOK})
	})
	got := w.run("line one\nlïne two\n", nil, "--text-info")
	if got.code != 0 {
		t.Errorf("got %+v", got)
	}
	if hello := <-w.hellos; !hello.Item.Info.More {
		t.Errorf("info = %+v", hello.Item.Info)
	}
}

func TestPumpNeverSplitsACharacter(t *testing.T) {
	// One byte at a time: every multi-byte character arrives in pieces.
	var sent []string
	pump(&oneByte{s: "aé€😀z"}, func(s string) error {
		sent = append(sent, s)
		return nil
	})
	if strings.Join(sent, "") != "aé€😀z" {
		t.Fatalf("sent %q", sent)
	}
	for _, s := range sent {
		if !strings.HasPrefix(strings.ToValidUTF8(s, "!"), s) || strings.ContainsRune(s, '�') {
			t.Errorf("a chunk splits a character: %q", s)
		}
	}
}

type oneByte struct{ s string }

func (r *oneByte) Read(p []byte) (int, error) {
	if r.s == "" {
		return 0, os.ErrClosed
	}
	p[0] = r.s[0]
	r.s = r.s[1:]
	return 1, nil
}

func TestTimeoutAsksTheWindow(t *testing.T) {
	entry := "half typed"
	w := newWindow(t, func(conn net.Conn, _ wire.Hello, follows <-chan wire.Follow) {
		for f := range follows {
			if f.Timeout {
				reply(conn, wire.Answer{Answer: wire.AnswerTimeout, Entry: &entry})
				return
			}
		}
	})
	start := time.Now()
	got := w.run("", nil, "--entry", "--timeout=1")
	if got.code != 5 || got.stdout != "half typed\n" {
		t.Errorf("got %+v", got)
	}
	if d := time.Since(start); d < time.Second {
		t.Errorf("timed out after %v", d)
	}
}

func TestTimeoutWithASilentWindowStillExits(t *testing.T) {
	w := newWindow(t, func(_ net.Conn, _ wire.Hello, follows <-chan wire.Follow) {
		for range follows {
		}
	})
	if got := w.run("", nil, "--question", "--timeout=1"); got.code != 5 {
		t.Errorf("got %+v", got)
	}
}

func TestAWindowThatGoesIsAnError(t *testing.T) {
	w := newWindow(t, func(conn net.Conn, _ wire.Hello, _ <-chan wire.Follow) { conn.Close() })
	got := w.run("", nil, asker...)
	if got.code != 1 || !strings.Contains(got.stderr, "the window went away") {
		t.Errorf("got %+v", got)
	}
}

func TestARefusedItemIsAnError(t *testing.T) {
	w := newWindow(t, answering(wire.Answer{Error: "kind is not one galley shows"}))
	got := w.run("", nil, asker...)
	if got.code != 255 || !strings.Contains(got.stderr, "kind is not one galley shows") {
		t.Errorf("got %+v", got)
	}
}

func TestNoWindowIsZenitysNoDisplay(t *testing.T) {
	dir, _ := os.MkdirTemp("", "g")
	defer os.RemoveAll(dir)
	os.Chmod(dir, 0o700)
	os.Mkdir(filepath.Join(dir, "galley"), 0o700)
	w := &window{dir: dir}
	got := w.run("", nil, asker...)
	if got.code != 1 || !strings.Contains(got.stderr, "cannot reach the window") {
		t.Errorf("got %+v", got)
	}
	got = w.run("", map[string]string{"XDG_RUNTIME_DIR": ""}, asker...)
	if got.code != 1 || !strings.Contains(got.stderr, "XDG_RUNTIME_DIR is not set") {
		t.Errorf("got %+v", got)
	}
}

func TestASharedDirectoryIsNotTrusted(t *testing.T) {
	w := newWindow(t, answering(wire.Answer{Answer: wire.AnswerOK}))
	os.Chmod(filepath.Join(w.dir, "galley"), 0o755)
	got := w.run("", nil, "--entry", "--hide-text")
	if got.code != 1 || !strings.Contains(got.stderr, "not a directory only this user can open") {
		t.Errorf("got %+v", got)
	}
}

func TestShow(t *testing.T) {
	w := newWindow(t, answering(wire.Answer{Shown: true}))
	got := w.run("", map[string]string{"XDG_ACTIVATION_TOKEN": "tok"}, "--show")
	if got.code != 0 {
		t.Errorf("got %+v", got)
	}
	if hello := <-w.hellos; !hello.Show || hello.Token != "tok" || hello.Item != nil {
		t.Errorf("hello = %+v", hello)
	}
}

func TestCommandLineErrorsExit255(t *testing.T) {
	w := &window{dir: "/nonexistent"}
	got := w.run("", nil, "--question", "--frobnicate")
	if got.code != 255 || got.stderr != "This option is not available. Please see --help for all possible usages.\n" {
		t.Errorf("got %+v", got)
	}
}

func TestVersion(t *testing.T) {
	var out bytes.Buffer
	env := Env{Stdout: &out, Stderr: &out, Getenv: func(string) string { return "" }, Version: "0.1"}
	Main([]string{"/bin/galley", "--version"}, env)
	Main([]string{"/run/current-system/sw/bin/zenity", "--version"}, env)
	if out.String() != "0.1\n"+ZenityVersion+"\n" {
		t.Errorf("got %q", out.String())
	}
}
