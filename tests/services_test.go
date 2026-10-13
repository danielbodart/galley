package tests

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
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

	"github.com/danielbodart/galley/services"
	"github.com/danielbodart/galley/wire"
)

// service is a connection to the services socket that has posted hello.
type service struct {
	conn net.Conn
	r    *bufio.Reader
}

func post(t *testing.T, socket, hello string) *service {
	t.Helper()
	c, err := net.Dial("unix", socket)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { c.Close() })
	fmt.Fprintln(c, hello)
	return &service{c, bufio.NewReader(c)}
}

func postService(t *testing.T, hello string) *service {
	t.Helper()
	return post(t, filepath.Join(runtimeDir, "services"), hello)
}

func (s *service) answer(t *testing.T, want string) {
	t.Helper()
	if line, _ := s.r.ReadString('\n'); strings.TrimSpace(line) != want {
		t.Errorf("answer = %q, want %s", line, want)
	}
}

func golden(t *testing.T, name string) string {
	t.Helper()
	b, err := os.ReadFile(filepath.Join("..", "wire", "testdata", name))
	if err != nil {
		t.Fatal(err)
	}
	return string(bytes.TrimSpace(b))
}

func serviceQuestion(level, text string) string {
	return fmt.Sprintf(`{"galley":5,"item":{"kind":"question","level":%q,"title":"ssh to server","text":%q,"markup":true,`+
		`"buttons":[{"answer":"cancel","label":"Refuse","key":"r"},{"answer":"extra","index":0,"label":"Allow once","key":"a"}],"default":-1}}`,
		level, text)
}

// A service's item shows its label, never its markup, and galley's icon for
// its level, with galley's banner for danger.
func TestAServiceItemIsShownByItsCallerAndLevel(t *testing.T) {
	need(t)
	me, _ := user.Current()
	var posted []*service
	for i, level := range []string{"normal", "warning", "danger"} {
		posted = append(posted, postService(t, serviceQuestion(level, "<b>"+level+"</b>")))
		waitFor(t, level, items(i+1))
	}
	s := current(t)
	icons := map[string]string{"normal": "security-medium", "warning": "dialog-warning", "danger": "security-low"}
	for i, it := range s.Items {
		level := []string{"normal", "warning", "danger"}[i]
		if it.Level != level || it.Icon != icons[level] || it.Text != "<b>"+level+"</b>" || it.Group != "Me" {
			t.Errorf("%s: %+v", level, it)
		}
		if it.Caller == nil || it.Caller.Label != "Me" || it.Caller.Name != me.Username ||
			fmt.Sprint(it.Caller.UID) != me.Uid || it.Caller.PID != os.Getpid() {
			t.Errorf("%s's caller: %+v", level, it.Caller)
		}
		if want := map[bool]string{true: "Danger"}[level == "danger"]; it.Banner != want {
			t.Errorf("%s's banner: %q", level, it.Banner)
		}
	}
	for range posted {
		press(t, "a")
	}
	for _, p := range posted {
		p.answer(t, `{"answer":"extra","index":0}`)
	}
}

func TestAServiceMaySendNoCaller(t *testing.T) {
	need(t)
	p := postService(t, `{"galley":5,"item":{"kind":"question","buttons":[],"caller":{"uid":0,"name":"root","label":"root","pid":1}}}`)
	if line, _ := p.r.ReadString('\n'); !strings.Contains(line, "caller") {
		t.Errorf("reply = %q", line)
	}
}

// Service rows never join one another, nor a client's that is the same.
func TestServiceRowsNeverJoin(t *testing.T) {
	need(t)
	hello := serviceQuestion("normal", "the same")
	first := postService(t, hello)
	waitFor(t, "the first", items(1))
	second := postService(t, hello)
	waitFor(t, "the second", items(2))
	thief := post(t, filepath.Join(runtimeDir, "galley", "sock"), hello)
	s := waitFor(t, "the thief", items(3))
	for _, it := range s.Items {
		if it.Askers != 1 {
			t.Errorf("joined: %+v", it)
		}
	}
	for i := 0; i < 3; i++ {
		press(t, "r")
	}
	for _, p := range []*service{first, second, thief} {
		p.answer(t, `{"answer":"cancel"}`)
	}
}

// A client whose title is a service's label is not in that service's group.
func TestAClientTitledAsAServiceIsNotInItsGroup(t *testing.T) {
	need(t)
	sock := filepath.Join(runtimeDir, "galley", "sock")
	question := func(title string) string {
		return fmt.Sprintf(`{"galley":5,"item":{"kind":"question","title":%q,"buttons":[{"answer":"cancel","label":"Refuse","key":"r"}],"default":-1}}`, title)
	}
	posted := []*service{postService(t, serviceQuestion("normal", "service"))}
	waitFor(t, "the service", items(1))
	posted = append(posted, post(t, sock, question("Other")))
	waitFor(t, "the other", items(2))
	posted = append(posted, post(t, sock, question("Me")))
	s := waitFor(t, "the one titled Me", items(3))
	if s.Items[0].Caller == nil || s.Items[1].Title != "Other" || s.Items[2].Title != "Me" {
		t.Errorf("order: %+v", s.Items)
	}
	for range posted {
		press(t, "r")
	}
	for _, p := range posted {
		p.answer(t, `{"answer":"cancel"}`)
	}
}

// A service's title of any length is shown cut short with an ellipsis,
// never refused, as zenity's --title is.
func TestAServicesLongTitleIsCutShort(t *testing.T) {
	need(t)
	p := postService(t, fmt.Sprintf(`{"galley":5,"item":{"kind":"question","title":%q,"text":"x",`+
		`"buttons":[{"answer":"cancel","label":"Refuse","key":"r"}],"default":-1}}`, strings.Repeat("é", 5000)))
	s := waitFor(t, "the long title", items(1))
	if s.Items[0].Title != strings.Repeat("é", 1023)+"…" {
		t.Errorf("title is %d runes", len([]rune(s.Items[0].Title)))
	}
	press(t, "r")
	p.answer(t, `{"answer":"cancel"}`)
}

// A service's follow lines keep its markup off and its icon galley's.
func TestAServiceFollowLineHasNoMarkupNorItsOwnIcon(t *testing.T) {
	need(t)
	p := postService(t, `{"galley":5,"item":{"kind":"progress","level":"warning","title":"t","text":"x","buttons":[],"progress":{}}}`)
	waitFor(t, "the progress", items(1))
	fmt.Fprintln(p.conn, `{"progress":{"text":"<b>bold</b>"}}`)
	s := waitFor(t, "the progress text", func(s state) bool { return len(s.Items) == 1 && s.Items[0].Text != "x" })
	if it := s.Items[0]; it.Text != "<b>bold</b>" || it.Icon != "dialog-warning" {
		t.Errorf("progress: %+v", it)
	}
	p.conn.Close()
	waitFor(t, "the progress to go", items(0))

	n := postService(t, `{"galley":5,"item":{"kind":"notification","level":"danger","title":"t","buttons":[{"answer":"ok","label":"OK"}],"default":0,"note":{"listen":true}}}`)
	for i, text := range []string{"<b>first</b>", "<b>second</b>"} {
		fmt.Fprintf(n.conn, `{"notify":{"text":%q,"icon":"emblem-ok"}}`+"\n", text)
		s := waitFor(t, text, func(s state) bool { return len(s.Items) == 1 && s.Items[0].Text == text })
		if it := s.Items[0]; it.Icon != "security-low" || it.Banner != "Danger" {
			t.Errorf("notification %d: %+v", i, it)
		}
	}
	n.conn.Close()
	waitFor(t, "the notification to go", items(0))
}

// The services socket only posts items.
func TestAServiceCannotShowTheWindow(t *testing.T) {
	need(t)
	p := postService(t, `{"galley":5,"show":true}`)
	if line, _ := p.r.ReadString('\n'); !strings.Contains(line, "error") {
		t.Errorf("reply = %q", line)
	}
}

// The sudo dialog shows its prefilled time, and Enter after the password
// presses Allow.
func TestEnterInTheSudoFormPressesAllow(t *testing.T) {
	need(t)
	p := postService(t, golden(t, "sudo-dialog.json"))
	s := waitFor(t, "the dialog", items(1))
	fields := body[struct {
		Fields []struct {
			Text *string `json:"text"`
		} `json:"fields"`
	}](t, s.Items[0].Body).Fields
	if len(fields) != 3 || fields[0].Text != nil || fields[1].Text == nil || *fields[1].Text != "15" {
		t.Errorf("fields = %s", s.Items[0].Body)
	}
	if s.Focus != "entry" {
		t.Errorf("focus = %q", s.Focus)
	}
	fill(t, map[string]any{"0": "hunter2"})
	press(t, "Return")
	p.answer(t, golden(t, "answer-sudo-dialog.json"))
}

// A row waits as long as its service's connection: nothing ends it but an
// answer or the service hanging up.
func TestAServiceRowStaysUntilWithdrawn(t *testing.T) {
	need(t)
	p := postService(t, serviceQuestion("normal", "waits"))
	waitFor(t, "the question", items(1))
	time.Sleep(2 * time.Second)
	if n := len(current(t).Items); n != 1 {
		t.Fatalf("%d rows", n)
	}
	p.conn.Close()
	waitFor(t, "the row to go", items(0))
}

// second starts another window with the settings config, its own socket,
// settings and log in a directory of its own, and waits for path, when it
// is not empty, to be a socket.
func second(t *testing.T, path, config string, env ...string) string {
	t.Helper()
	dir, err := os.MkdirTemp(runtimeDir, "w")
	if err != nil {
		t.Fatal(err)
	}
	sock := filepath.Join(dir, "sock")
	args, err := configured(dir, config)
	if err != nil {
		t.Fatal(err)
	}
	d := exec.Command(args[0], args[1:]...)
	d.Env = append(append(append([]string{}, daemonEnv...), env...), "GALLEY_SOCKET="+sock)
	log, err := os.Create(filepath.Join(dir, "log"))
	if err != nil {
		t.Fatal(err)
	}
	defer log.Close()
	d.Stdout, d.Stderr = log, log
	if err := d.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		d.Process.Kill()
		d.Wait()
		if t.Failed() {
			b, _ := os.ReadFile(log.Name())
			t.Logf("the window's log:\n%s", b)
		}
	})
	waitForSocket(t, sock)
	if path != "" {
		waitForSocket(t, path)
	}
	return sock
}

func waitForSocket(t *testing.T, path string) {
	t.Helper()
	for i := 0; ; i++ {
		if info, err := os.Stat(path); err == nil && info.Mode()&os.ModeSocket != 0 {
			return
		}
		if i > 300 {
			t.Fatalf("nothing listening on %s", path)
		}
		time.Sleep(50 * time.Millisecond)
	}
}

func hungUp(t *testing.T, p *service) {
	t.Helper()
	if line, err := p.r.ReadString('\n'); line != "" || (err != io.EOF && !errors.Is(err, syscall.ECONNRESET)) {
		t.Errorf("read %q, %v", line, err)
	}
}

// logged checks the log of the window second started on sock, once it has
// served.
func logged(t *testing.T, sock, want string) {
	t.Helper()
	b, _ := os.ReadFile(filepath.Join(filepath.Dir(sock), "log"))
	if !strings.Contains(string(b), want) {
		t.Errorf("log %q, want %q", b, want)
	}
}

func peerPid(t *testing.T, path string) int {
	t.Helper()
	c, err := net.Dial("unix", path)
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	raw, err := c.(*net.UnixConn).SyscallConn()
	if err != nil {
		t.Fatal(err)
	}
	var cred *syscall.Ucred
	raw.Control(func(fd uintptr) {
		cred, err = syscall.GetsockoptUcred(int(fd), syscall.SOL_SOCKET, syscall.SO_PEERCRED)
	})
	if err != nil {
		t.Fatal(err)
	}
	return int(cred.Pid)
}

// The user's socket answers --show.
func serves(t *testing.T, sock string) {
	t.Helper()
	p := post(t, sock, `{"galley":5,"show":true}`)
	p.answer(t, `{"shown":true}`)
}

// A service connecting finds the window itself at the other end, as frisket
// checks, not whatever made the socket.
func TestAServicesPeerIsTheWindow(t *testing.T) {
	need(t)
	if pid := peerPid(t, filepath.Join(runtimeDir, "services")); pid != daemonPid {
		t.Errorf("peer pid %d, the window's %d", pid, daemonPid)
	}
}

// A window whose services are someone else: a stale file where its socket
// goes is replaced, the socket is open to all, and this user is hung up on.
func TestAnUnlistedUidIsResetAndAStaleSocketIsReplaced(t *testing.T) {
	need(t)
	dir, err := os.MkdirTemp(runtimeDir, "s")
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, "services")
	if err := os.WriteFile(path, []byte("stale"), 0o600); err != nil {
		t.Fatal(err)
	}
	serves(t, second(t, path, `{"services": {"root": "Root"}}`, "GALLEY_SERVICES_SOCKET="+path))
	if info, err := os.Stat(path); err != nil || info.Mode().Perm() != 0o666 {
		t.Errorf("services socket %v, %v", info, err)
	}
	hungUp(t, post(t, path, serviceQuestion("normal", "x")))
}

// Services that cannot be set up are logged, and the user is served.
func TestBrokenServicesLeaveTheUsersSocket(t *testing.T) {
	need(t)
	me, _ := user.Current()
	mine := fmt.Sprintf(`{"services": {%q: "Me"}}`, me.Username)
	t.Run("a bad config admits no one", func(t *testing.T) {
		path := filepath.Join(runtimeDir, "bad")
		sock := second(t, "", fmt.Sprintf(`{"services": {%q: 1}}`, me.Username), "GALLEY_SERVICES_SOCKET="+path)
		serves(t, sock)
		logged(t, sock, "using the default settings")
		if _, err := os.Lstat(path); !os.IsNotExist(err) {
			t.Errorf("services socket bound: %v", err)
		}
	})
	t.Run("a services socket that cannot be bound", func(t *testing.T) {
		sock := second(t, "", mine, "GALLEY_SERVICES_SOCKET="+filepath.Join(runtimeDir, "nowhere", "services"))
		serves(t, sock)
		logged(t, sock, "cannot listen on")
	})
	t.Run("a services socket too long to bind", func(t *testing.T) {
		dir, err := os.MkdirTemp(runtimeDir, "l")
		if err != nil {
			t.Fatal(err)
		}
		sock := second(t, "", mine, "GALLEY_SERVICES_SOCKET="+filepath.Join(dir, strings.Repeat("x", 120)))
		serves(t, sock)
		logged(t, sock, "longer than a unix socket address holds")
		if entries, _ := os.ReadDir(dir); len(entries) != 0 {
			t.Errorf("bound %v", entries)
		}
	})
	t.Run("a services socket another window listens on", func(t *testing.T) {
		path := filepath.Join(runtimeDir, "services")
		sock := second(t, "", mine, "GALLEY_SERVICES_SOCKET="+path)
		serves(t, sock)
		logged(t, sock, "another window is already listening")
		if pid := peerPid(t, path); pid != daemonPid {
			t.Errorf("peer pid %d, the window's %d", pid, daemonPid)
		}
	})
}

// No service named, no services socket: the window binds it only for the
// services its settings name.
func TestNoServicesNoServicesSocket(t *testing.T) {
	need(t)
	path := filepath.Join(runtimeDir, "unnamed")
	serves(t, second(t, "", "{}", "GALLEY_SERVICES_SOCKET="+path))
	if _, err := os.Lstat(path); !os.IsNotExist(err) {
		t.Errorf("services socket bound: %v", err)
	}
}

// The accessibility bus is off unless the settings turn it on, set before
// GTK makes the window's first widget; a -config the window cannot read
// leaves it off.
func TestTheAccessibilityBusIsOffUnlessAskedFor(t *testing.T) {
	need(t)
	a11y := func(t *testing.T, ctl string) string {
		t.Helper()
		waitForSocket(t, ctl)
		c, err := net.Dial("unix", ctl)
		if err != nil {
			t.Fatal(err)
		}
		defer c.Close()
		fmt.Fprintln(c, `{"a11y": true}`)
		var reply struct {
			A11y *string `json:"a11y"`
		}
		if err := json.NewDecoder(c).Decode(&reply); err != nil {
			t.Fatal(err)
		}
		if reply.A11y == nil {
			return "(unset)"
		}
		return *reply.A11y
	}
	if got := a11y(t, filepath.Join(runtimeDir, "ctl")); got != "none" {
		t.Errorf("the window with services and no accessibility: GTK_A11Y %s", got)
	}
	for _, c := range []struct{ name, config, want string }{
		{"on", `{"accessibility": true}`, "(unset)"},
		{"off", `{"accessibility": false}`, "none"},
		{"unreadable", `{"accessibility": "yes"}`, "none"},
	} {
		t.Run(c.name, func(t *testing.T) {
			dir, err := os.MkdirTemp(runtimeDir, "y")
			if err != nil {
				t.Fatal(err)
			}
			ctl := filepath.Join(dir, "ctl")
			second(t, "", c.config, "GALLEY_TEST_CONTROL="+ctl)
			if got := a11y(t, ctl); got != c.want {
				t.Errorf("GTK_A11Y %s, want %s", got, c.want)
			}
		})
	}
}

// A service's notification goes when its connection does, plain or
// listening.
func TestAServiceNotificationGoesWithItsConnection(t *testing.T) {
	need(t)
	p := postService(t, `{"galley":5,"item":{"kind":"notification","title":"t","text":"plain","buttons":[{"answer":"ok","label":"OK"}],"default":0}}`)
	p.answer(t, `{"queued":true}`)
	waitFor(t, "the notification", items(1))
	p.conn.Close()
	waitFor(t, "the notification to go", items(0))

	n := postService(t, `{"galley":5,"item":{"kind":"notification","title":"t","buttons":[{"answer":"ok","label":"OK"}],"default":0,"note":{"listen":true}}}`)
	fmt.Fprintln(n.conn, `{"notify":{"text":"listening"}}`)
	waitFor(t, "the listening notification", items(1))
	n.conn.Close()
	waitFor(t, "the listening notification to go", items(0))
}

// The window takes the one socket systemd passes, whatever its name, rather
// than binding the path systemd holds.
func TestTheUsersSocketFromSystemd(t *testing.T) {
	need(t)
	for _, name := range []string{"user", "", "galley.socket"} {
		t.Run(name, func(t *testing.T) {
			dir, err := os.MkdirTemp(runtimeDir, "a")
			if err != nil {
				t.Fatal(err)
			}
			path := filepath.Join(dir, "sock")
			f, err := listening(path)
			if err != nil {
				t.Fatal(err)
			}
			d := activated(dir, name, f, "GALLEY_SOCKET="+path)
			err = d.Start()
			f.Close()
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() {
				d.Process.Kill()
				d.Wait()
			})
			serves(t, path)
		})
	}
}

// tableDialog is the sudo dialog with a table of n rows: a process chain
// a row, each a command of a few lines.
func tableDialog(n int) string {
	rows := make([][]string, n)
	for i := range rows {
		rows[i] = []string{fmt.Sprint(1000 + i), fmt.Sprintf("bash -c\n    cd /home/dan/%d\n    && make", i)}
	}
	b, _ := json.Marshal(map[string]any{"galley": 5, "item": map[string]any{
		"kind": "forms", "level": "normal", "title": "sudo make install", "text": "/run/current-system/sw/bin/make install",
		"buttons": []map[string]any{{"answer": "cancel", "label": "Cancel", "underline": -1}, {"answer": "ok", "label": "Allow", "underline": -1}},
		"default": 1,
		"forms": map[string]any{"fields": []map[string]any{
			{"kind": "password", "label": "Password for dan"},
			{"kind": "entry", "label": "Remember the password for this claude for", "text": "15"},
			{"kind": "combo", "label": "", "values": []string{"minutes", "hours"}}}},
		"table": map[string]any{"columns": []string{"PID", "Process"}, "rows": rows, "monospace": []int{1},
			"summary": "claude ← make install"},
	}})
	return string(b)
}

// A long table scrolls in what height the window has left, never pushing
// the fields or the buttons out of it; a short one is only as tall as its
// rows; and the row in the queue shows the title and the table's summary.
func TestATableScrollsAndLeavesTheFieldsInView(t *testing.T) {
	need(t)
	long := postService(t, tableDialog(60))
	waitFor(t, "the long table", items(1))
	start(t, "", "--show").expect(t, 0, "")
	s := waitFor(t, "the long table", func(s state) bool {
		return s.Visible && len(s.Items) == 1 && s.Items[0].Table != nil && s.Items[0].Table.Height > 0 && s.Items[0].InView != nil
	})
	it := s.Items[0]
	if it.Table.Rows != 60 || !it.Table.Scrolls || !*it.InView {
		t.Errorf("long table: %+v, in view %v", *it.Table, *it.InView)
	}
	if len(it.Row) != 2 || it.Row[0] != "sudo make install" || it.Row[1] != "claude ← make install" {
		t.Errorf("row = %q", it.Row)
	}
	long.conn.Close()
	waitFor(t, "the long table to go", items(0))

	short := postService(t, tableDialog(1))
	waitFor(t, "the short table", items(1))
	start(t, "", "--show").expect(t, 0, "")
	s = waitFor(t, "the short table", func(s state) bool {
		return s.Visible && len(s.Items) == 1 && s.Items[0].Table != nil && s.Items[0].Table.Height > 0 && s.Items[0].InView != nil
	})
	it = s.Items[0]
	if it.Table.Scrolls || it.Table.Height > 120 || !*it.InView {
		t.Errorf("short table: %+v, in view %v", *it.Table, *it.InView)
	}
	short.conn.Close()
	waitFor(t, "the short table to go", items(0))
}

// alwaysDialog is the signer's form with Always and a diff, its default
// the button at index def, or -1 for none.
func alwaysDialog(t *testing.T, def int, diff map[string]string) string {
	t.Helper()
	var hello map[string]any
	if err := json.Unmarshal([]byte(golden(t, "signer-always.json")), &hello); err != nil {
		t.Fatal(err)
	}
	item := hello["item"].(map[string]any)
	item["default"] = def
	if diff != nil {
		item["diff"] = diff
	}
	b, _ := json.Marshal(hello)
	return string(b)
}

// The signer's Always: the window works the diff out from the file's two
// versions and shows it, and adds its own Always button after the item's,
// which Enter never presses unless Tab has reached it, and which answers
// "always".
func TestAlwaysShowsTheDiffAndIsNeverEnter(t *testing.T) {
	need(t)
	start(t, "", "--show").expect(t, 0, "")
	p := postService(t, golden(t, "signer-always.json"))
	s := waitFor(t, "the dialog", items(1))
	it := s.Items[0]
	want := strings.Join([]string{
		"--- ~/Projects/foo/chase.json", "+++ ~/Projects/foo/chase.json", "@@ -1,3 +1,7 @@", " {",
		`-  "signer": {}`, `+  "signer": {`, `+    "ssh": {`, `+      "github.com": "allow"`, "+    }", "+  }", " }",
	}, "\n")
	if it.Diff == nil || it.Diff.Text != want || it.Diff.Added != 5 || it.Diff.Removed != 1 {
		t.Errorf("diff = %+v", it.Diff)
	}
	if n := len(it.Buttons); n != 4 || it.Buttons[3].Label != "Always" || !it.Buttons[3].Enabled || it.Default != -1 {
		t.Errorf("buttons = %+v, default %d", it.Buttons, it.Default)
	}
	press(t, "Return")
	if s := current(t); len(s.Items) != 1 {
		t.Fatalf("Enter answered: %+v", s.Items)
	}
	focus(t, "Always")
	press(t, "Return")
	p.answer(t, golden(t, "answer-always.json"))
	waitFor(t, "the dialog to go", items(0))

	// With a default, Enter presses it, never Always.
	p = postService(t, alwaysDialog(t, 1, nil))
	waitFor(t, "the dialog with a default", items(1))
	press(t, "Return")
	p.answer(t, golden(t, "answer-extra.json"))
}

// A long diff scrolls in the height the window has left, the fields and
// buttons still in sight; a new file's is every line added.
func TestALongDiffScrollsAndLeavesTheFieldsInView(t *testing.T) {
	need(t)
	var lines []string
	for i := range 300 {
		lines = append(lines, fmt.Sprintf(`  "rule-%d": "allow",`, i))
	}
	p := postService(t, alwaysDialog(t, -1, map[string]string{
		"path": "chase.json", "new": "{\n" + strings.Join(lines, "\n") + "\n}\n"}))
	waitFor(t, "the long diff", items(1))
	start(t, "", "--show").expect(t, 0, "")
	s := waitFor(t, "the long diff", func(s state) bool {
		return s.Visible && len(s.Items) == 1 && s.Items[0].Diff != nil && s.Items[0].Diff.Height > 0 && s.Items[0].InView != nil
	})
	it := s.Items[0]
	if it.Diff.Added != 302 || it.Diff.Removed != 0 || !strings.Contains(it.Diff.Text, "@@ -0,0 +1,302 @@") {
		t.Errorf("new file's diff: %d added, %d removed", it.Diff.Added, it.Diff.Removed)
	}
	if !it.Diff.Scrolls || !*it.InView {
		t.Errorf("long diff: scrolls %v, height %d, in view %v", it.Diff.Scrolls, it.Diff.Height, *it.InView)
	}
	focus(t, "Always")
	press(t, "Return")
	p.answer(t, golden(t, "answer-always.json"))
}

// servicesClient is package services' Client for the window under test: its
// services socket under this user's name, and the window's own cgroup,
// which in the test is this process's, standing in for galley.service.
func servicesClient(t *testing.T) (services.Client, string) {
	t.Helper()
	me, err := user.Current()
	if err != nil {
		t.Fatal(err)
	}
	dir, err := os.MkdirTemp(runtimeDir, "s")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.RemoveAll(dir) })
	if err := os.Symlink(filepath.Join(runtimeDir, "services"), filepath.Join(dir, me.Username+".sock")); err != nil {
		t.Fatal(err)
	}
	cgroup, err := services.CgroupID(daemonPid)
	if err != nil {
		t.Fatal(err)
	}
	return services.Client{Dir: dir, Cgroup: func(int) (uint64, error) { return cgroup, nil }}, me.Username
}

// Package services asks the real window: the item is shown as a service's,
// the answer comes back from the window it checked, and cancelling takes
// the row away.
func TestPackageServicesAsksTheWindow(t *testing.T) {
	need(t)
	cl, name := servicesClient(t)
	item := &wire.Item{Kind: wire.KindQuestion, Level: wire.LevelWarning, Title: "ssh to server", Text: "a question",
		Buttons: []wire.Button{{Answer: wire.AnswerCancel, Label: "Refuse", Key: "r", Underline: 0},
			{Answer: wire.AnswerExtra, Label: "Allow once", Key: "a", Underline: 0}},
		Default: -1, Always: true}
	type result struct {
		a   wire.Answer
		w   services.Window
		err error
	}
	done := make(chan result)
	go func() {
		a, w, err := cl.Ask(context.Background(), name, item)
		done <- result{a, w, err}
	}()
	s := waitFor(t, "the question", items(1))
	if it := s.Items[0]; it.Caller == nil || it.Caller.Label != "Me" || it.Level != "warning" || it.Text != "a question" {
		t.Errorf("item = %+v", it)
	}
	press(t, "a")
	r := <-done
	if r.err != nil || r.a.Answer != wire.AnswerExtra || r.a.Index != 0 {
		t.Fatalf("got %+v, %v", r.a, r.err)
	}
	if r.w.Pid != daemonPid || r.w.Start == 0 || r.w.Cgroup == "" {
		t.Errorf("window = %+v, want pid %d", r.w, daemonPid)
	}

	ctx, cancel := context.WithCancel(context.Background())
	go func() {
		a, w, err := cl.Ask(ctx, name, item)
		done <- result{a, w, err}
	}()
	waitFor(t, "the second question", items(1))
	cancel()
	if r := <-done; !errors.Is(r.err, context.Canceled) {
		t.Errorf("cancelled: %v", r.err)
	}
	waitFor(t, "the row to go", items(0))
}

// A notification from package services stays until it is dismissed, and
// Notify returns then.
func TestPackageServicesNotificationStaysUntilDismissed(t *testing.T) {
	need(t)
	cl, name := servicesClient(t)
	done := make(chan error)
	go func() { done <- cl.Notify(context.Background(), name, "The rule was not saved") }()
	waitFor(t, "the notification", func(s state) bool {
		return len(s.Items) == 1 && s.Items[0].Text == "The rule was not saved"
	})
	select {
	case err := <-done:
		t.Fatalf("returned before it was dismissed: %v", err)
	default:
	}
	press(t, "Return")
	if err := <-done; err != nil {
		t.Fatal(err)
	}
	waitFor(t, "the notification to go", items(0))
}

// The window's services socket is refused to anyone checking for a window
// in another cgroup: what galley.service would be, were the window not in
// it.
func TestPackageServicesRefusesAWindowOutsideItsCgroup(t *testing.T) {
	need(t)
	cl, name := servicesClient(t)
	cgroup, _ := cl.Cgroup(0)
	cl.Cgroup = func(int) (uint64, error) { return cgroup + 1, nil }
	_, _, err := cl.Ask(context.Background(), name, &wire.Item{Kind: wire.KindQuestion, Buttons: []wire.Button{}, Default: -1})
	if !errors.Is(err, services.ErrNobody) {
		t.Fatalf("got %v, want nobody", err)
	}
	if len(current(t).Items) != 0 {
		t.Errorf("an item was posted")
	}
}
