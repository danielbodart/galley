package services

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"net"
	"os"
	"os/user"
	"path/filepath"
	"reflect"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/danielbodart/galley/wire"
)

// fakeGalley listens where a Client dials, answers one item with replies,
// a line each, and reports the item's line and the client's hang-up.
type fakeGalley struct {
	client Client
	user   string
	items  chan []byte
	closed chan struct{}
}

// startGalley is a window for this user, its cgroup this process's when
// inGalleyCgroup, another's otherwise.
func startGalley(t *testing.T, inGalleyCgroup bool, replies ...string) *fakeGalley {
	t.Helper()
	me, err := user.Current()
	if err != nil {
		t.Fatal(err)
	}
	own, err := CgroupID(os.Getpid())
	if err != nil {
		t.Fatal(err)
	}
	g := &fakeGalley{user: me.Username, items: make(chan []byte, 1), closed: make(chan struct{})}
	g.client = Client{Dir: shortDir(t), Cgroup: func(int) (uint64, error) {
		if inGalleyCgroup {
			return own, nil
		}
		return own + 1, nil
	}}
	l, err := net.Listen("unix", filepath.Join(g.client.Dir, me.Username+".sock"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { l.Close() })
	go func() {
		c, err := l.Accept()
		if err != nil {
			return
		}
		defer c.Close()
		r := bufio.NewReader(c)
		line, err := r.ReadBytes('\n')
		if err == nil {
			g.items <- line
			for _, reply := range replies {
				c.Write([]byte(reply + "\n"))
			}
		}
		for err == nil {
			_, err = r.ReadByte()
		}
		close(g.closed)
	}()
	return g
}

// shortDir is a directory whose socket paths fit a sun_path, which a test's
// own TempDir, named after the test, may not.
func shortDir(t *testing.T) string {
	t.Helper()
	dir, err := os.MkdirTemp("", "services")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.RemoveAll(dir) })
	return dir
}

func golden(t *testing.T, name string) string {
	t.Helper()
	b, err := os.ReadFile(filepath.Join("..", "wire", "testdata", name))
	if err != nil {
		t.Fatal(err)
	}
	return strings.TrimSuffix(string(b), "\n")
}

var question = &wire.Item{Kind: wire.KindQuestion, Title: "ssh to server",
	Buttons: []wire.Button{{Answer: wire.AnswerCancel, Label: "Refuse", Underline: -1}}, Default: -1}

func TestAPeerOutsideGalleysCgroupIsNobodyAndHearsNothing(t *testing.T) {
	g := startGalley(t, false, golden(t, "answer-extra.json"))
	_, _, err := g.client.Ask(context.Background(), g.user, question)
	if !errors.Is(err, ErrNobody) {
		t.Fatalf("got %v, want nobody", err)
	}
	if !strings.Contains(err.Error(), "not galley.service") {
		t.Errorf("%v does not name the cgroup", err)
	}
	<-g.closed
	select {
	case line := <-g.items:
		t.Fatalf("posted %s", line)
	default:
	}
}

func TestAPeerWithAnotherUIDIsNobody(t *testing.T) {
	g := startGalley(t, true, golden(t, "answer-extra.json"))
	other, err := user.Lookup("nobody")
	if err != nil {
		t.Skip("no user nobody")
	}
	if other.Uid == strconv.Itoa(os.Getuid()) {
		t.Skip("running as nobody")
	}
	if err := os.Symlink(filepath.Join(g.client.Dir, g.user+".sock"), filepath.Join(g.client.Dir, "nobody.sock")); err != nil {
		t.Fatal(err)
	}
	_, _, err = g.client.Ask(context.Background(), "nobody", question)
	if !errors.Is(err, ErrNobody) || !strings.Contains(err.Error(), "galley peer uid") {
		t.Fatalf("got %v, want nobody for the peer's uid", err)
	}
}

func TestNoSocketIsNobody(t *testing.T) {
	g := startGalley(t, true)
	g.client.Dir = shortDir(t)
	if _, _, err := g.client.Ask(context.Background(), g.user, question); !errors.Is(err, ErrNobody) {
		t.Fatalf("got %v, want nobody", err)
	}
	if err := g.client.Notify(context.Background(), g.user, "x"); !errors.Is(err, ErrNobody) {
		t.Fatalf("notify got %v, want nobody", err)
	}
}

// The item goes as wire's Hello at the one version, byte for byte as wire
// writes it, and the window's answer comes back as wire reads it, with the
// window that gave it.
func TestTheHelloIsWiresAndTheAnswerComesBackWhole(t *testing.T) {
	var hello wire.Hello
	if err := json.Unmarshal([]byte(golden(t, "signer-always.json")), &hello); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"answer-always.json", "answer-extra.json", "answer-allow-for.json",
		"answer-sudo-dialog.json", "answer-sudo-dialog-once.json", "answer-cancel.json", "answer-close.json"} {
		t.Run(name, func(t *testing.T) {
			g := startGalley(t, true, golden(t, name))
			a, w, err := g.client.Ask(context.Background(), g.user, hello.Item)
			if err != nil {
				t.Fatal(err)
			}
			if got, want := string(<-g.items), golden(t, "signer-always.json")+"\n"; got != want {
				t.Errorf("sent\n%s\nwant\n%s", got, want)
			}
			var want wire.Answer
			if err := json.Unmarshal([]byte(golden(t, name)), &want); err != nil {
				t.Fatal(err)
			}
			if !reflect.DeepEqual(a, want) {
				t.Errorf("answered %+v, want %+v", a, want)
			}
			if w.Pid != os.Getpid() || w.Start == 0 || w.Cgroup == "" {
				t.Errorf("window %+v, want pid %d", w, os.Getpid())
			}
		})
	}
}

func TestARefusedItemIsNobody(t *testing.T) {
	g := startGalley(t, true, golden(t, "answer-error.json"))
	_, _, err := g.client.Ask(context.Background(), g.user, question)
	if !errors.Is(err, ErrNobody) || !strings.Contains(err.Error(), "galley says") {
		t.Fatalf("got %v, want nobody, saying why", err)
	}
}

// A notification asked is answered once it is dismissed, not when it is
// queued; a scale's values as it moves are passed over too.
func TestTheLinesBeforeTheAnswerArePassedOver(t *testing.T) {
	g := startGalley(t, true, `{"queued":true}`, `{"partial":3}`, `{"answer":"ok"}`)
	a, _, err := g.client.Ask(context.Background(), g.user, question)
	if err != nil || a.Answer != wire.AnswerOK || a.Queued || a.Partial != nil {
		t.Fatalf("got %+v, %v", a, err)
	}
}

// A notification is held until it is dismissed, not let go once queued,
// since the window drops a service's item with its connection.
func TestNotifyReturnsOnceDismissed(t *testing.T) {
	g := startGalley(t, true, `{"queued":true}`, `{"answer":"ok"}`)
	if err := g.client.Notify(context.Background(), g.user, "Always was not saved"); err != nil {
		t.Fatal(err)
	}
	var hello wire.Hello
	if err := json.Unmarshal(<-g.items, &hello); err != nil {
		t.Fatal(err)
	}
	if it := hello.Item; hello.Galley != wire.Version || it.Kind != wire.KindNotification ||
		it.Text != "Always was not saved" || it.Note == nil || it.Note.Listen || len(it.Buttons) != 1 {
		t.Errorf("sent %+v", hello)
	}
	<-g.closed

	g = startGalley(t, true, `{"queued":true}`)
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error)
	go func() { done <- g.client.Notify(ctx, g.user, "x") }()
	<-g.items
	cancel()
	<-g.closed
	if err := <-done; !errors.Is(err, context.Canceled) {
		t.Errorf("cancelled notify got %v", err)
	}

	g = startGalley(t, true, golden(t, "answer-error.json"))
	if err := g.client.Notify(context.Background(), g.user, "x"); !errors.Is(err, ErrNobody) {
		t.Errorf("refused notify got %v, want nobody", err)
	}
}

func TestGalleyGoingAwayBeforeAnAnswerIsNobody(t *testing.T) {
	g := startGalley(t, true)
	l, err := net.Listen("unix", filepath.Join(shortDir(t), g.user+".sock"))
	if err != nil {
		t.Fatal(err)
	}
	defer l.Close()
	g.client.Dir = filepath.Dir(l.Addr().String())
	go func() {
		c, err := l.Accept()
		if err == nil {
			bufio.NewReader(c).ReadBytes('\n')
			c.Close()
		}
	}()
	if _, _, err := g.client.Ask(context.Background(), g.user, question); !errors.Is(err, ErrNobody) {
		t.Fatalf("got %v, want nobody", err)
	}
}

// Cancelling the context hangs up, which withdraws the row.
func TestCancellingWithdrawsTheItem(t *testing.T) {
	g := startGalley(t, true)
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error)
	go func() {
		_, _, err := g.client.Ask(ctx, g.user, question)
		done <- err
	}()
	<-g.items
	cancel()
	<-g.closed
	if err := <-done; !errors.Is(err, context.Canceled) {
		t.Fatalf("got %v, want cancelled", err)
	}
}

func pair(t *testing.T) (*net.UnixConn, *net.UnixConn) {
	t.Helper()
	l, err := net.ListenUnix("unix", &net.UnixAddr{Name: filepath.Join(shortDir(t), "s"), Net: "unix"})
	if err != nil {
		t.Fatal(err)
	}
	defer l.Close()
	near, err := net.DialUnix("unix", nil, l.Addr().(*net.UnixAddr))
	if err != nil {
		t.Fatal(err)
	}
	far, err := l.AcceptUnix()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { near.Close(); far.Close() })
	return near, far
}

func TestWatchHangUpSaysWhenThePeerGoes(t *testing.T) {
	near, far := pair(t)
	hung := make(chan struct{})
	stop, err := WatchHangUp(near, func() { close(hung) })
	if err != nil {
		t.Fatal(err)
	}
	far.Close()
	<-hung
	stop()
}

func TestWatchHangUpStopsWithoutCalling(t *testing.T) {
	near, far := pair(t)
	called := make(chan struct{}, 1)
	stop, err := WatchHangUp(near, func() { called <- struct{}{} })
	if err != nil {
		t.Fatal(err)
	}
	// What the peer writes is not a hang-up.
	far.Write([]byte("x\n"))
	time.Sleep(50 * time.Millisecond)
	stop()
	select {
	case <-called:
		t.Fatal("called with the peer still there")
	default:
	}
	// And the connection is as usable after the watch as before it.
	if _, err := near.Write([]byte("y\n")); err != nil {
		t.Fatal(err)
	}
}
