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
	waitFor(t, "the notification to go", items(0))
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

// second starts another window, its own socket in a directory of its own,
// with services as its services socket, or with none when it is nil.
func second(t *testing.T, services *os.File, env ...string) string {
	t.Helper()
	dir, err := os.MkdirTemp(runtimeDir, "w")
	if err != nil {
		t.Fatal(err)
	}
	sock := filepath.Join(dir, "sock")
	env = append(env, "GALLEY_SOCKET="+sock)
	var d *exec.Cmd
	if services == nil {
		d = exec.Command(daemonArgs[0], daemonArgs[1:]...)
		d.Env = append(append([]string{}, daemonEnv...), env...)
		d.Stdout, d.Stderr = os.Stderr, os.Stderr
	} else {
		d = withServices(services, env...)
	}
	if err := d.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		d.Process.Kill()
		d.Wait()
	})
	for i := 0; ; i++ {
		if _, err := os.Stat(sock); err == nil {
			break
		}
		if i > 300 {
			t.Fatal("the second window did not start listening")
		}
		time.Sleep(50 * time.Millisecond)
	}
	return sock
}

func servicesSocket(t *testing.T) (*os.File, string) {
	t.Helper()
	dir, err := os.MkdirTemp(runtimeDir, "s")
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, "services")
	f, err := listening(path)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { f.Close() })
	return f, path
}

func hungUp(t *testing.T, p *service) {
	t.Helper()
	if line, err := p.r.ReadString('\n'); line != "" || (err != io.EOF && !errors.Is(err, syscall.ECONNRESET)) {
		t.Errorf("read %q, %v", line, err)
	}
}

// The user's socket answers --show.
func serves(t *testing.T, sock string) {
	t.Helper()
	p := post(t, sock, `{"galley":3,"show":true}`)
	p.answer(t, `{"shown":true}`)
}

// A window whose services are someone else hangs up on this user.
func TestAnUnlistedUidIsReset(t *testing.T) {
	need(t)
	f, path := servicesSocket(t)
	serves(t, second(t, f, `GALLEY_SERVICES={"root":"Root"}`))
	hungUp(t, post(t, path, serviceQuestion("normal", "x")))
}

// Services that cannot be set up are logged, and the user is served.
func TestBrokenServicesLeaveTheUsersSocket(t *testing.T) {
	need(t)
	me, _ := user.Current()
	t.Run("a bad GALLEY_SERVICES admits no one", func(t *testing.T) {
		f, path := servicesSocket(t)
		serves(t, second(t, f, fmt.Sprintf(`GALLEY_SERVICES={%q:1}`, me.Username)))
		hungUp(t, post(t, path, serviceQuestion("normal", "x")))
	})
	t.Run("a services fd that is not a socket", func(t *testing.T) {
		f, err := os.Open(os.DevNull)
		if err != nil {
			t.Fatal(err)
		}
		defer f.Close()
		serves(t, second(t, f, fmt.Sprintf(`GALLEY_SERVICES={%q:"Me"}`, me.Username)))
	})
	t.Run("GALLEY_SERVICES with no services socket", func(t *testing.T) {
		serves(t, second(t, nil, fmt.Sprintf(`GALLEY_SERVICES={%q:"Me"}`, me.Username)))
	})
}

// A service's notification goes when its connection does, plain or
// listening.
func TestAServiceNotificationGoesWithItsConnection(t *testing.T) {
	need(t)
	p := postService(t, `{"galley":3,"item":{"kind":"notification","title":"t","text":"plain","buttons":[{"answer":"ok","label":"OK"}],"default":0}}`)
	p.answer(t, `{"queued":true}`)
	waitFor(t, "the notification", items(1))
	p.conn.Close()
	waitFor(t, "the notification to go", items(0))

	n := postService(t, `{"galley":3,"item":{"kind":"notification","title":"t","buttons":[{"answer":"ok","label":"OK"}],"default":0,"note":{"listen":true}}}`)
	fmt.Fprintln(n.conn, `{"notify":{"text":"listening"}}`)
	waitFor(t, "the listening notification", items(1))
	n.conn.Close()
	waitFor(t, "the listening notification to go", items(0))
}

// The window takes the user's socket from systemd by its name, in either
// order beside the services socket, or as the one fd passed whatever its
// name, rather than binding the path systemd holds.
func TestTheUsersSocketFromSystemd(t *testing.T) {
	need(t)
	me, _ := user.Current()
	for _, names := range []string{"user:services", "services:user", "", "galley.socket"} {
		t.Run(names, func(t *testing.T) {
			dir, err := os.MkdirTemp(runtimeDir, "a")
			if err != nil {
				t.Fatal(err)
			}
			paths := map[string]string{}
			var files []*os.File
			for _, name := range strings.Split(names, ":") {
				path := filepath.Join(dir, fmt.Sprint(len(files)))
				f, err := listening(path)
				if err != nil {
					t.Fatal(err)
				}
				defer f.Close()
				if name != "services" {
					name = "user"
				}
				paths[name] = path
				files = append(files, f)
			}
			d := activated(names, files, "GALLEY_SOCKET="+paths["user"], fmt.Sprintf(`GALLEY_SERVICES={%q:"Me"}`, me.Username))
			if err := d.Start(); err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() {
				d.Process.Kill()
				d.Wait()
			})
			for _, f := range files {
				f.Close()
			}
			serves(t, paths["user"])
			if path, ok := paths["services"]; ok {
				p := post(t, path, `{"galley":3,"show":true}`)
				if line, _ := p.r.ReadString('\n'); !strings.Contains(line, "error") {
					t.Errorf("services reply = %q", line)
				}
			}
		})
	}
}
