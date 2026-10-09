// End to end: the real client against the real window, drawn by GTK's
// broadway backend so no display is needed, with keys pressed through the
// window's test control (daemon/test-control.js, which the package does not
// install).
//
// It runs when $GALLEY_E2E_DAEMON names the command that starts the window
// and $GALLEY_E2E_BROADWAYD names gtk4-broadwayd, as the flake's end-to-end
// check sets them; otherwise it skips. By hand, from the dev shell:
//
//	GALLEY_E2E_DAEMON="gjs -m $PWD/daemon/main.js" \
//	GALLEY_E2E_BROADWAYD=$(command -v gtk4-broadwayd) go test -v ./tests/
package tests

import (
	"bufio"
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"os"
	"os/exec"
	"os/user"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"
)

var (
	runtimeDir string
	client     string
	ready      bool
	// How the window was started, for a test that starts another.
	daemonArgs []string
	daemonEnv  []string
)

func TestMain(m *testing.M) {
	daemon, broadwayd := os.Getenv("GALLEY_E2E_DAEMON"), os.Getenv("GALLEY_E2E_BROADWAYD")
	if daemon == "" || broadwayd == "" {
		os.Exit(m.Run())
	}
	code, err := setUp(daemon, broadwayd, m)
	if err != nil {
		fmt.Fprintln(os.Stderr, "end-to-end set-up:", err)
		os.Exit(1)
	}
	os.Exit(code)
}

func setUp(daemon, broadwayd string, m *testing.M) (int, error) {
	// Short, so every socket's path fits in sun_path.
	dir, err := os.MkdirTemp("", "e")
	if err != nil {
		return 0, err
	}
	defer os.RemoveAll(dir)
	os.Chmod(dir, 0o700)
	runtimeDir = dir

	client = filepath.Join(dir, "client")
	build := exec.Command("go", "build", "-o", client, "../cmd/galley")
	build.Stderr = os.Stderr
	if err := build.Run(); err != nil {
		return 0, fmt.Errorf("building the client: %w", err)
	}

	display := fmt.Sprintf(":%d", 40+os.Getpid()%50)
	env := append(clean(os.Environ()), "XDG_RUNTIME_DIR="+dir, "GDK_BACKEND=broadway", "BROADWAY_DISPLAY="+display)

	bw := exec.Command(broadwayd, display)
	bw.Env = env
	bw.Stderr = os.Stderr
	if err := bw.Start(); err != nil {
		return 0, err
	}
	defer bw.Process.Kill()
	time.Sleep(500 * time.Millisecond)

	me, err := user.Current()
	if err != nil {
		return 0, err
	}
	daemonArgs, daemonEnv = strings.Fields(daemon), env
	d := exec.Command(daemonArgs[0], daemonArgs[1:]...)
	d.Env = append(env, "GALLEY_TEST_CONTROL="+filepath.Join(dir, "ctl"),
		"GALLEY_SERVICES_SOCKET="+filepath.Join(dir, "services"),
		fmt.Sprintf(`GALLEY_SERVICES={%q:"Me"}`, me.Username))
	d.Stdout, d.Stderr = os.Stderr, os.Stderr
	if err := d.Start(); err != nil {
		return 0, err
	}
	defer d.Process.Kill()
	for i := 0; ; i++ {
		_, a := os.Stat(filepath.Join(dir, "galley", "sock"))
		_, b := os.Stat(filepath.Join(dir, "ctl"))
		_, c := os.Stat(filepath.Join(dir, "services"))
		if a == nil && b == nil && c == nil {
			break
		}
		if i > 300 {
			return 0, fmt.Errorf("the window did not start listening")
		}
		time.Sleep(50 * time.Millisecond)
	}
	ready = true
	return m.Run(), nil
}

// clean drops what would reach the desktop the tests run on: its display
// and its session bus.
func clean(env []string) []string {
	var out []string
	for _, e := range env {
		name, _, _ := strings.Cut(e, "=")
		switch name {
		case "WAYLAND_DISPLAY", "DISPLAY", "DBUS_SESSION_BUS_ADDRESS", "XDG_RUNTIME_DIR", "GALLEY_SOCKET":
			continue
		}
		out = append(out, e)
	}
	return out
}

func need(t *testing.T) {
	t.Helper()
	if !ready {
		t.Skip("GALLEY_E2E_DAEMON and GALLEY_E2E_BROADWAYD are not set")
	}
	t.Cleanup(func() { waitFor(t, "the queue to empty", func(s state) bool { return len(s.Items) == 0 }) })
}

// ---- the window's test control ----------------------------------------

type state struct {
	Visible   bool    `json:"visible"`
	Selected  *string `json:"selected"`
	Withdrawn bool    `json:"withdrawn"`
	Focus     string  `json:"focus"`
	Items     []struct {
		ID     string `json:"id"`
		Kind   string `json:"kind"`
		Title  string `json:"title"`
		Group  string `json:"group"`
		Caller *struct {
			UID   int    `json:"uid"`
			Name  string `json:"name"`
			Label string `json:"label"`
			PID   int    `json:"pid"`
		} `json:"caller"`
		Level   string `json:"level"`
		Banner  string `json:"banner"`
		Text    string `json:"text"`
		Info    string `json:"info"`
		Icon    string `json:"icon"`
		Buttons []struct {
			Label   string `json:"label"`
			Key     string `json:"key"`
			Enabled bool   `json:"enabled"`
		} `json:"buttons"`
		Default   int             `json:"default"`
		Connected bool            `json:"connected"`
		Askers    int             `json:"askers"`
		Badge     string          `json:"badge"`
		Body      json.RawMessage `json:"body"`
	} `json:"items"`
}

// body reads an item's own state, as its body (daemon/bodies.js) gives it.
func body[T any](t *testing.T, raw json.RawMessage) T {
	t.Helper()
	var v T
	if err := json.Unmarshal(raw, &v); err != nil {
		t.Fatalf("body %s: %v", raw, err)
	}
	return v
}

func control(t *testing.T, command any) []byte {
	t.Helper()
	c, err := net.Dial("unix", filepath.Join(runtimeDir, "ctl"))
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	json.NewEncoder(c).Encode(command)
	line, err := bufio.NewReader(c).ReadBytes('\n')
	if err != nil {
		t.Fatal(err)
	}
	return line
}

func current(t *testing.T) state {
	t.Helper()
	var s state
	if err := json.Unmarshal(control(t, map[string]any{"state": true}), &s); err != nil {
		t.Fatal(err)
	}
	return s
}

// pressed is what became of a key: taken by the window, or else passed to
// the focused widget ("button", "entry") or to nothing ("").
type pressed struct {
	Handled bool   `json:"handled"`
	To      string `json:"to"`
	Error   string `json:"error"`
}

func key(t *testing.T, key string, extra ...map[string]any) pressed {
	t.Helper()
	command := map[string]any{"key": key}
	for _, e := range extra {
		for k, v := range e {
			command[k] = v
		}
	}
	var r pressed
	json.Unmarshal(control(t, command), &r)
	if r.Error != "" {
		t.Fatal(r.Error)
	}
	return r
}

func press(t *testing.T, k string, extra ...map[string]any) bool {
	t.Helper()
	return key(t, k, extra...).Handled
}

func focus(t *testing.T, label string) {
	t.Helper()
	var r pressed
	json.Unmarshal(control(t, map[string]any{"focus": label}), &r)
	if r.Error != "" {
		t.Fatal(r.Error)
	}
}

func waitFor(t *testing.T, what string, ok func(state) bool) state {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for {
		s := current(t)
		if ok(s) {
			return s
		}
		if time.Now().After(deadline) {
			t.Fatalf("waiting for %s; the window is %+v", what, s)
		}
		time.Sleep(20 * time.Millisecond)
	}
}

func items(n int) func(state) bool {
	return func(s state) bool { return len(s.Items) == n }
}

// ---- clients ----------------------------------------------------------

type run struct {
	cmd    *exec.Cmd
	stdout bytes.Buffer
	stderr bytes.Buffer
	done   chan struct{}
	code   int
}

func start(t *testing.T, stdin string, args ...string) *run {
	t.Helper()
	r := &run{cmd: exec.Command(client, args...), done: make(chan struct{})}
	r.cmd.Env = append(clean(os.Environ()), "XDG_RUNTIME_DIR="+runtimeDir)
	r.cmd.Stdout, r.cmd.Stderr = &r.stdout, &r.stderr
	if stdin != "" {
		r.cmd.Stdin = strings.NewReader(stdin)
	}
	if err := r.cmd.Start(); err != nil {
		t.Fatal(err)
	}
	go func() {
		r.cmd.Wait()
		r.code = r.cmd.ProcessState.ExitCode()
		close(r.done)
	}()
	t.Cleanup(func() { r.cmd.Process.Kill() })
	return r
}

// startPiped is start with a stdin the test writes to as it goes.
func startPiped(t *testing.T, args ...string) (*run, io.WriteCloser) {
	t.Helper()
	r := &run{cmd: exec.Command(client, args...), done: make(chan struct{})}
	r.cmd.Env = append(clean(os.Environ()), "XDG_RUNTIME_DIR="+runtimeDir)
	r.cmd.Stdout, r.cmd.Stderr = &r.stdout, &r.stderr
	in, err := r.cmd.StdinPipe()
	if err != nil {
		t.Fatal(err)
	}
	if err := r.cmd.Start(); err != nil {
		t.Fatal(err)
	}
	go func() {
		r.cmd.Wait()
		r.code = r.cmd.ProcessState.ExitCode()
		close(r.done)
	}()
	t.Cleanup(func() {
		in.Close()
		r.cmd.Process.Kill()
	})
	return r, in
}

func (r *run) expect(t *testing.T, code int, stdout string) {
	t.Helper()
	select {
	case <-r.done:
	case <-time.After(10 * time.Second):
		t.Fatalf("%v still waiting", r.cmd.Args)
	}
	if r.code != code || r.stdout.String() != stdout {
		t.Errorf("%v: exit %d stdout %q (stderr %q), want %d %q",
			r.cmd.Args[1:], r.code, r.stdout.String(), r.stderr.String(), code, stdout)
	}
}

func (r *run) waiting(t *testing.T) {
	t.Helper()
	select {
	case <-r.done:
		t.Fatalf("%v answered already: %d %q", r.cmd.Args[1:], r.code, r.stdout.String())
	default:
	}
}

// ---- the tests --------------------------------------------------------

func asker(text string) []string {
	return []string{"--question", "--no-markup", "--title=Allow this request?",
		"--ok-label=Allow", "--cancel-label=Refuse", "--extra-button=Ask", "--width=640", "--text=" + text}
}

func TestTheAskersThreeAnswersStack(t *testing.T) {
	need(t)
	first := start(t, "", asker("one")...)
	waitFor(t, "the first", items(1))
	second := start(t, "", asker("two")...)
	waitFor(t, "the second", items(2))
	third := start(t, "", asker("three")...)
	s := waitFor(t, "all three", items(3))

	if s.Selected == nil || *s.Selected != s.Items[0].ID || s.Items[0].Text != "one" {
		t.Errorf("the first to arrive is not the one selected: %+v", s)
	}
	for _, it := range s.Items {
		if it.Group != "Allow this request?" {
			t.Errorf("group = %q", it.Group)
		}
	}
	if keys := fmt.Sprint(s.Items[0].Buttons); keys != "[{Refuse r true} {Allow a true} {Ask s true}]" {
		t.Errorf("buttons = %s", keys)
	}

	press(t, "a")
	first.expect(t, 0, "")
	second.waiting(t)
	press(t, "r")
	second.expect(t, 1, "")
	press(t, "s")
	third.expect(t, 1, "Ask\n")
}

func TestEnterPressesTheDefault(t *testing.T) {
	need(t)
	yes := start(t, "", "--question", "--text=proceed?")
	waitFor(t, "the question", items(1))
	press(t, "Return")
	yes.expect(t, 0, "")

	no := start(t, "", "--question", "--default-cancel")
	waitFor(t, "the question", items(1))
	press(t, "KP_Enter")
	no.expect(t, 1, "")
}

// Tab to Refuse and Enter refuses: the window leaves Enter to a focused
// button, as any dialog does.
func TestEnterOnAFocusedButtonPressesIt(t *testing.T) {
	need(t)
	start(t, "", "--show").expect(t, 0, "")
	r := start(t, "", asker("focused")...)
	waitFor(t, "the question", items(1))
	focus(t, "Refuse")
	if s := current(t); s.Focus != "button:Refuse" {
		t.Fatalf("focus = %q", s.Focus)
	}
	if k := key(t, "Return"); k.Handled || k.To != "button" {
		t.Errorf("Return = %+v", k)
	}
	r.expect(t, 1, "")
}

// The selected question withdrawn by its asker hands nothing on: the keys
// that were on their way do not answer the question beside it.
func TestAWithdrawnSelectionIsNotHandedOn(t *testing.T) {
	need(t)
	start(t, "", "--show").expect(t, 0, "")
	first := start(t, "", asker("first")...)
	waitFor(t, "the first", items(1))
	second := start(t, "", asker("second")...)
	s := waitFor(t, "the second", items(2))
	if *s.Selected != s.Items[0].ID {
		t.Fatalf("selected %s", *s.Selected)
	}

	first.cmd.Process.Signal(syscall.SIGTERM)
	s = waitFor(t, "the first to go", items(1))
	if s.Selected != nil || !s.Withdrawn || s.Focus != "page" {
		t.Errorf("after the withdrawal: selected %v, withdrawn %v, focus %q", s.Selected, s.Withdrawn, s.Focus)
	}
	press(t, "Return")
	press(t, "a")
	time.Sleep(200 * time.Millisecond)
	second.waiting(t)

	// Down is the question that took its place, chosen.
	press(t, "Down")
	if s := current(t); s.Selected == nil || *s.Selected != s.Items[0].ID || s.Withdrawn {
		t.Errorf("after Down: %+v", s)
	}
	press(t, "a")
	second.expect(t, 0, "")
}

// A password prompt withdrawn while it is typed into: the rest of the
// password goes nowhere, not to the buttons of the question below.
func TestAWithdrawnPasswordKeepsItsKeys(t *testing.T) {
	need(t)
	start(t, "", "--show").expect(t, 0, "")
	sudo := start(t, "", "--entry", "--hide-text", "--title=Authentication Required")
	waitFor(t, "the prompt", items(1))
	r := start(t, "", asker("below")...)
	waitFor(t, "the request", items(2))
	if s := current(t); s.Focus != "entry" {
		t.Fatalf("focus = %q", s.Focus)
	}
	if k := key(t, "h"); k.Handled || k.To != "entry" {
		t.Errorf("h = %+v", k)
	}

	sudo.cmd.Process.Signal(syscall.SIGTERM)
	if s := waitFor(t, "the prompt to go", items(1)); s.Focus != "page" {
		t.Errorf("focus = %q", s.Focus)
	}
	for _, k := range []string{"a", "s", "r", "Return", "space"} {
		if p := key(t, k); p.To != "" {
			t.Errorf("%s went to %q", k, p.To)
		}
	}
	time.Sleep(200 * time.Millisecond)
	r.waiting(t)

	press(t, "k")
	press(t, "r")
	r.expect(t, 1, "")
}

// A --switch has only its extra buttons, and so no default: Enter answers
// nothing.
func TestASwitchHasOnlyItsButtons(t *testing.T) {
	need(t)
	r := start(t, "", "--question", "--switch", "--extra-button=One", "--extra-button=Two")
	s := waitFor(t, "the question", items(1))
	if fmt.Sprint(s.Items[0].Buttons) != "[{One o true} {Two t true}]" {
		t.Errorf("buttons = %v", s.Items[0].Buttons)
	}
	press(t, "Return")
	time.Sleep(200 * time.Millisecond)
	r.waiting(t)
	press(t, "t")
	r.expect(t, 1, "Two\n")
}

func TestAHeldKeyAnswersOnce(t *testing.T) {
	need(t)
	first := start(t, "", "--question", "--text=first")
	waitFor(t, "the first", items(1))
	second := start(t, "", "--question", "--text=second")
	waitFor(t, "the second", items(2))

	press(t, "Return", map[string]any{"hold": true})
	first.expect(t, 0, "")
	// The key is still down: this press is its repeat.
	press(t, "Return", map[string]any{"hold": true})
	time.Sleep(200 * time.Millisecond)
	second.waiting(t)
	press(t, "Return") // and up
	press(t, "Return")
	second.expect(t, 0, "")
}

func TestThePassword(t *testing.T) {
	need(t)
	sudo := start(t, "", "--entry", "--hide-text", "--title=Authentication Required",
		"--text=Authenticate to run as root:\n\n    sudo nix-collect__garbage\n\nPassword for dan:")
	s := waitFor(t, "the prompt", items(1))
	if s.Items[0].Text != "Authenticate to run as root:\n\n    sudo nix-collect_garbage\n\nPassword for dan:" {
		t.Errorf("text = %q", s.Items[0].Text)
	}
	control(t, map[string]any{"entry": "correct horse"})
	press(t, "Return")
	sudo.expect(t, 0, "correct horse\n")

	// With the window in front and the entry focused, a letter is typed,
	// not a button pressed; Alt and the letter press it.
	start(t, "", "--show").expect(t, 0, "")
	cancelled := start(t, "", "--entry", "--hide-text")
	waitFor(t, "the prompt", items(1))
	if press(t, "c") {
		t.Errorf("c was taken from the password field")
	}
	cancelled.waiting(t)
	press(t, "c", map[string]any{"alt": true})
	cancelled.expect(t, 1, "")
	press(t, "Escape")
}

func TestTextInfo(t *testing.T) {
	need(t)
	file := filepath.Join(t.TempDir(), "diff")
	os.WriteFile(file, []byte("--- a/chase.jsonc\n+++ b/chase.jsonc\n"), 0o600)
	approve := start(t, "", "--text-info", "--title=Approve this change?",
		"--ok-label=Approve", "--cancel-label=Refuse", "--width=720", "--height=520", "--filename="+file)
	s := waitFor(t, "the change", items(1))
	if s.Items[0].Info != "--- a/chase.jsonc\n+++ b/chase.jsonc\n" {
		t.Errorf("info = %q", s.Items[0].Info)
	}
	press(t, "Return")
	approve.expect(t, 0, "")

	refuse := start(t, "streamed\nfrom stdin\n", "--text-info", "--cancel-label=Refuse")
	waitFor(t, "the stream", func(s state) bool {
		return len(s.Items) == 1 && s.Items[0].Info == "streamed\nfrom stdin\n"
	})
	press(t, "r")
	refuse.expect(t, 1, "")
}

func TestTheCheckboxHoldsBackOK(t *testing.T) {
	need(t)
	r := start(t, "terms", "--text-info", "--checkbox=I have read them")
	waitFor(t, "the terms", items(1))
	press(t, "Return")
	time.Sleep(200 * time.Millisecond)
	r.waiting(t)
	press(t, "c")
	r.expect(t, 1, "")
}

func TestAKilledClientTakesItsItem(t *testing.T) {
	need(t)
	r := start(t, "", asker("going")...)
	waitFor(t, "the item", items(1))
	r.cmd.Process.Signal(syscall.SIGTERM)
	waitFor(t, "the item to go", items(0))
}

func TestTimeout(t *testing.T) {
	need(t)
	q := start(t, "", "--question", "--timeout=1")
	e := start(t, "", "--entry", "--entry-text=typed", "--timeout=1")
	q.expect(t, 5, "")
	e.expect(t, 5, "typed\n")
}

func TestASelectionStaysWhereItWas(t *testing.T) {
	need(t)
	a := start(t, "", "--question", "--title=A", "--text=a")
	waitFor(t, "a", items(1))
	b := start(t, "", "--question", "--title=B", "--text=b")
	waitFor(t, "b", items(2))
	press(t, "j")
	c := start(t, "", "--question", "--title=A", "--text=a again")
	s := waitFor(t, "c", items(3))
	// Grouped: the second A sits with the first, above B.
	if s.Items[0].Text != "a" || s.Items[1].Text != "a again" || s.Items[2].Text != "b" {
		t.Errorf("order = %+v", s.Items)
	}
	if *s.Selected != s.Items[2].ID {
		t.Errorf("the selection moved to %s", *s.Selected)
	}
	press(t, "y")
	b.expect(t, 0, "")
	// The selection was the last row; it falls to the one above.
	press(t, "Down")
	press(t, "n")
	c.expect(t, 1, "")
	press(t, "k")
	press(t, "y")
	a.expect(t, 0, "")
}

func TestEscapeHidesAndShowBrings(t *testing.T) {
	need(t)
	r := start(t, "", "--question")
	s := waitFor(t, "the question", items(1))
	if s.Visible {
		t.Errorf("an arrival showed the window")
	}
	show := start(t, "", "--show")
	show.expect(t, 0, "")
	waitFor(t, "the window", func(s state) bool { return s.Visible })
	press(t, "Escape")
	waitFor(t, "the window to hide", func(s state) bool { return !s.Visible && len(s.Items) == 1 })
	r.waiting(t)
	press(t, "y")
	r.expect(t, 0, "")
}

func TestMarkupIsSanitised(t *testing.T) {
	need(t)
	r := start(t, "", "--question", `--text=<b>bold</b> <span foreground="white">hidden</span>\tand <a href="x">link</a>`)
	s := waitFor(t, "the question", items(1))
	// <a> is not Pango markup: the text is shown as it came.
	if s.Items[0].Text != "<b>bold</b> <span foreground=\"white\">hidden</span>\tand <a href=\"x\">link</a>" {
		t.Errorf("text = %q", s.Items[0].Text)
	}
	press(t, "n")
	r.expect(t, 1, "")

	r = start(t, "", "--question", `--text=<b>bold</b> <span foreground="white">shown</span>`)
	s = waitFor(t, "the question", items(1))
	if s.Items[0].Text != "bold shown" {
		t.Errorf("text = %q", s.Items[0].Text)
	}
	press(t, "n")
	r.expect(t, 1, "")
}

func TestTheWindowRefusesABadItem(t *testing.T) {
	need(t)
	c, err := net.Dial("unix", filepath.Join(runtimeDir, "galley", "sock"))
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	fmt.Fprintln(c, `{"galley":1,"item":{"kind":"list","buttons":[]}}`)
	line, _ := bufio.NewReader(c).ReadString('\n')
	if !strings.Contains(line, `"error"`) || !strings.Contains(line, "not one galley shows") {
		t.Errorf("reply = %q", line)
	}
	if len(current(t).Items) != 0 {
		t.Errorf("a refused item was queued")
	}
}

// ---- the same question ------------------------------------------------

// asking is n rows, the first with this many asking, and its icon showing
// the count when more than one is.
func asking(n, askers int) func(state) bool {
	badge := ""
	if askers > 1 {
		badge = fmt.Sprint(askers)
	}
	return func(s state) bool {
		return len(s.Items) == n && s.Items[0].Askers == askers && s.Items[0].Badge == badge
	}
}

// The same question asked twice is one row, and one press answers both.
func TestTheSameQuestionIsOneRow(t *testing.T) {
	need(t)
	first := start(t, "", asker("same")...)
	waitFor(t, "the first", items(1))
	second := start(t, "", asker("same")...)
	waitFor(t, "the second to join it", asking(1, 2))
	other := start(t, "", asker("other")...)
	s := waitFor(t, "the other", items(2))
	if s.Items[1].Askers != 1 || s.Items[1].Badge != "" {
		t.Errorf("the other has %d asking, badged %q", s.Items[1].Askers, s.Items[1].Badge)
	}

	press(t, "a")
	first.expect(t, 0, "")
	second.expect(t, 0, "")
	other.waiting(t)
	press(t, "r")
	other.expect(t, 1, "")
}

// An asker that times out or is killed takes only itself; the row stays
// for the rest. One timing out is answered by the window, with what is
// typed so far, as a sole asker is.
func TestAnAskerLeavingLeavesTheOthers(t *testing.T) {
	need(t)
	name := []string{"--entry", "--title=Who?", "--text=Name:"}
	killed := start(t, "", name...)
	waitFor(t, "the first", items(1))
	last := start(t, "", name...)
	waitFor(t, "the second", asking(1, 2))
	timed := start(t, "", append(name, "--timeout=3")...)
	waitFor(t, "the timed one to join", asking(1, 3))
	control(t, map[string]any{"entry": "typed"})
	timed.expect(t, 5, "typed\n")
	waitFor(t, "the timed out one to go", asking(1, 2))

	killed.cmd.Process.Signal(syscall.SIGTERM)
	waitFor(t, "the killed one to go", asking(1, 1))
	last.waiting(t)
	press(t, "Return")
	last.expect(t, 0, "typed\n")
}

// Nothing is remembered: the same question asked once the last was
// answered is a new row.
func TestAnAnsweredQuestionIsAskedAgain(t *testing.T) {
	need(t)
	r := start(t, "", asker("again")...)
	s := waitFor(t, "the question", items(1))
	before := s.Items[0].ID
	press(t, "a")
	r.expect(t, 0, "")

	r = start(t, "", asker("again")...)
	s = waitFor(t, "the question again", items(1))
	if s.Items[0].ID == before || s.Items[0].Askers != 1 {
		t.Errorf("asked again: %+v", s.Items[0])
	}
	press(t, "r")
	r.expect(t, 1, "")
}

// The same password prompt twice, as two sudos at once ask it: one
// password answers both.
func TestTheSamePasswordAnswersAll(t *testing.T) {
	need(t)
	sudo := []string{"--entry", "--hide-text", "--title=Authentication Required", "--text=Password for dan:"}
	first := start(t, "", sudo...)
	waitFor(t, "the first", items(1))
	second := start(t, "", sudo...)
	waitFor(t, "the second to join it", asking(1, 2))
	control(t, map[string]any{"entry": "pw"})
	press(t, "Return")
	first.expect(t, 0, "pw\n")
	second.expect(t, 0, "pw\n")
}

// Whatever streams is a row of its own, however alike.
func TestStreamsAreNeverShared(t *testing.T) {
	need(t)
	var bars []io.WriteCloser
	var runs []*run
	for i := 0; i < 2; i++ {
		r, in := startPiped(t, "--progress", "--title=Copying")
		waitFor(t, "the bar", items(i+1))
		runs, bars = append(runs, r), append(bars, in)
	}
	for i, in := range bars {
		in.Close()
		waitFor(t, "the bar to end", func(s state) bool {
			return len(s.Items) == 2-i && s.Items[0].Buttons[1].Enabled
		})
		press(t, "Return")
		runs[i].expect(t, 0, "")
		waitFor(t, "the bar to go", items(1-i))
	}

	a := start(t, "the same\n", "--text-info")
	waitFor(t, "the first text", items(1))
	b := start(t, "the same\n", "--text-info")
	waitFor(t, "the second text", items(2))
	press(t, "Return")
	a.expect(t, 0, "")
	press(t, "Return")
	b.expect(t, 0, "")

	scale := []string{"--scale", "--value=3", "--print-partial"}
	a = start(t, "", scale...)
	waitFor(t, "the first scale", items(1))
	b = start(t, "", scale...)
	waitFor(t, "the second scale", items(2))
	press(t, "Return")
	a.expect(t, 0, "3\n")
	press(t, "Return")
	b.expect(t, 0, "3\n")
}

// A notification waited on is shared by those waiting on it, as any
// question is. One whose clients have gone, as they do as soon as it is
// queued, asks nothing: the same one sent again is a new entry, and tells
// the person again.
func TestTheSameNotificationCountsWhoIsThere(t *testing.T) {
	need(t)
	// With a timeout, zenity's waits.
	a := start(t, "", "--notification", "--text=done", "--timeout=10")
	waitFor(t, "one waiting on it", asking(1, 1))
	b := start(t, "", "--notification", "--text=done", "--timeout=10")
	waitFor(t, "two waiting on it", asking(1, 2))
	press(t, "d")
	a.expect(t, 0, "")
	b.expect(t, 0, "")
	waitFor(t, "it to go", items(0))

	for i := 0; i < 3; i++ {
		start(t, "", "--notification", "--text=done").expect(t, 0, "")
		waitFor(t, "an entry of its own", func(s state) bool {
			if len(s.Items) != i+1 {
				return false
			}
			for _, it := range s.Items {
				if it.Askers != 0 || it.Badge != "" || it.Connected {
					return false
				}
			}
			return true
		})
	}
	for i := 2; i >= 0; i-- {
		press(t, "d")
		waitFor(t, "one to be dismissed", items(i))
	}
}

// A shared row takes no lines from its askers: one that joins cannot
// change the question another is waiting on.
func TestASharedRowTakesNoLines(t *testing.T) {
	need(t)
	dial := func(hello string) (net.Conn, *bufio.Reader) {
		c, err := net.Dial("unix", filepath.Join(runtimeDir, "galley", "sock"))
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { c.Close() })
		fmt.Fprintln(c, hello)
		return c, bufio.NewReader(c)
	}
	answer := func(r *bufio.Reader, want string) {
		t.Helper()
		if line, _ := r.ReadString('\n'); strings.TrimSpace(line) != want {
			t.Errorf("answer = %q, want %s", line, want)
		}
	}
	buttons := `"buttons":[{"answer":"cancel","label":"Refuse","key":"r"},{"answer":"ok","label":"Approve","key":"a"}]`

	text := `{"galley":2,"item":{"kind":"text","title":"Approve this change?",` + buttons + `,"info":{"text":"- a\n+ b\n"}}}`
	_, asked := dial(text)
	waitFor(t, "the text", items(1))
	joined, _ := dial(text)
	waitFor(t, "the second to join it", asking(1, 2))
	fmt.Fprintln(joined, `{"append":"+ harmless\n"}`)
	joined.Close()
	s := waitFor(t, "the joined one to go", asking(1, 1))
	if s.Items[0].Info != "- a\n+ b\n" {
		t.Errorf("the text became %q", s.Items[0].Info)
	}
	press(t, "a")
	answer(asked, `{"answer":"ok"}`)

	list := `{"galley":2,"item":{"kind":"list","title":"Pick one",` + buttons + `,"list":{"columns":["x"],"rows":[["a"],["b"]]}}}`
	_, asked = dial(list)
	waitFor(t, "the list", items(1))
	joined, _ = dial(list)
	waitFor(t, "the second to join it", asking(1, 2))
	fmt.Fprintln(joined, `{"rows":[["evil"]]}`)
	joined.Close()
	s = waitFor(t, "the joined one to go", asking(1, 1))
	if rows := body[table](t, s.Items[0].Body).Rows; len(rows) != 2 {
		t.Errorf("the rows became %v", rows)
	}
	press(t, "r")
	answer(asked, `{"answer":"cancel"}`)
}
