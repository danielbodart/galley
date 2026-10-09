package tests

import (
	"bufio"
	"bytes"
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
	return fmt.Sprintf(`{"galley":3,"item":{"kind":"question","level":%q,"title":"ssh to server","text":%q,"markup":true,`+
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
	p := postService(t, `{"galley":3,"item":{"kind":"question","buttons":[],"caller":{"uid":0,"name":"root","label":"root","pid":1}}}`)
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
		return fmt.Sprintf(`{"galley":3,"item":{"kind":"question","title":%q,"buttons":[{"answer":"cancel","label":"Refuse","key":"r"}],"default":-1}}`, title)
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

// A service's follow lines keep its markup off and its icon galley's.
func TestAServiceFollowLineHasNoMarkupNorItsOwnIcon(t *testing.T) {
	need(t)
	p := postService(t, `{"galley":3,"item":{"kind":"progress","level":"warning","title":"t","text":"x","buttons":[],"progress":{}}}`)
	waitFor(t, "the progress", items(1))
	fmt.Fprintln(p.conn, `{"progress":{"text":"<b>bold</b>"}}`)
	s := waitFor(t, "the progress text", func(s state) bool { return len(s.Items) == 1 && s.Items[0].Text != "x" })
	if it := s.Items[0]; it.Text != "<b>bold</b>" || it.Icon != "dialog-warning" {
		t.Errorf("progress: %+v", it)
	}
	p.conn.Close()
	waitFor(t, "the progress to go", items(0))

	n := postService(t, `{"galley":3,"item":{"kind":"notification","level":"danger","title":"t","buttons":[{"answer":"ok","label":"OK"}],"default":0,"note":{"listen":true}}}`)
	for i, text := range []string{"<b>first</b>", "<b>second</b>"} {
		fmt.Fprintf(n.conn, `{"notify":{"text":%q,"icon":"emblem-ok"}}`+"\n", text)
		s := waitFor(t, text, func(s state) bool { return len(s.Items) == 1 && s.Items[0].Text == text })
		if it := s.Items[0]; it.Icon != "security-low" || it.Banner != "Danger" {
			t.Errorf("notification %d: %+v", i, it)
		}
	}
	n.conn.Close()
	press(t, "Return")
}

// The services socket only posts items.
func TestAServiceCannotShowTheWindow(t *testing.T) {
	need(t)
	p := postService(t, `{"galley":3,"show":true}`)
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

// A window whose services are someone else: a stale file where its socket
// goes is replaced, the socket is open to all, and this user is hung up on.
func TestAnUnlistedUidIsResetAndAStaleSocketIsReplaced(t *testing.T) {
	need(t)
	dir := filepath.Join(runtimeDir, "w2")
	os.Mkdir(dir, 0o700)
	socket := filepath.Join(dir, "services")
	if err := os.WriteFile(socket, []byte("stale"), 0o600); err != nil {
		t.Fatal(err)
	}
	d := exec.Command(daemonArgs[0], daemonArgs[1:]...)
	d.Env = append(daemonEnv, "GALLEY_SOCKET="+filepath.Join(dir, "sock"),
		"GALLEY_SERVICES_SOCKET="+socket, `GALLEY_SERVICES={"root":"Root"}`)
	d.Stdout, d.Stderr = os.Stderr, os.Stderr
	if err := d.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		d.Process.Kill()
		d.Wait()
	})
	for i := 0; ; i++ {
		info, err := os.Stat(socket)
		if err == nil && info.Mode()&os.ModeSocket != 0 {
			if perm := info.Mode().Perm(); perm != 0o666 {
				t.Errorf("mode %o", perm)
			}
			break
		}
		if i > 300 {
			t.Fatalf("the stale file was not replaced: %v %v", info, err)
		}
		time.Sleep(50 * time.Millisecond)
	}
	p := post(t, socket, serviceQuestion("normal", "x"))
	if line, err := p.r.ReadString('\n'); line != "" || (err != io.EOF && !errors.Is(err, syscall.ECONNRESET)) {
		t.Errorf("read %q, %v", line, err)
	}
}
