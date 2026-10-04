// Package client is galley's command: it reads zenity's command line, puts
// the item to the window over the socket, waits for its answer, and prints
// and exits as zenity would have.
//
// It is the thin end. It holds nothing the window needs after the answer,
// keeps no state between runs, and its connection is the item: when the
// process goes, for any reason, the kernel closes the socket and the window
// drops the question.
package client

import (
	"bufio"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"path/filepath"
	"sync"
	"syscall"
	"time"
	"unicode/utf8"

	"github.com/danielbodart/galley/internal/wire"
	"github.com/danielbodart/galley/internal/zenity"
)

// Env is everything Main reads from the world, so tests can supply it.
type Env struct {
	Stdin  io.Reader
	Stdout io.Writer
	Stderr io.Writer
	Getenv func(string) string
	Cwd    string
	// Version is galley's own.
	Version string
	// Grace is how long to wait for the window to answer a timeout before
	// exiting as timed out regardless. Zero means two seconds.
	Grace time.Duration
	// HangUpParent is --auto-kill's SIGHUP to the caller; nil sends it.
	HangUpParent func()
	// Now is the time, for a progress item's time remaining; nil is the
	// clock's.
	Now func() time.Time
}

// Main runs one invocation and returns its exit status. args includes the
// program name.
func Main(args []string, env Env) int {
	inv, err := zenity.Parse(args[1:], env.Cwd)
	if err != nil {
		if e, ok := err.(*zenity.Error); ok {
			for _, w := range e.Warnings {
				fmt.Fprintln(env.Stderr, w)
			}
		}
		fmt.Fprintln(env.Stderr, err)
		return 255
	}
	for _, w := range inv.Warnings {
		fmt.Fprintln(env.Stderr, w)
	}
	switch {
	case inv.Help:
		fmt.Fprint(env.Stdout, usage)
		return 0
	case inv.Version:
		fmt.Fprintln(env.Stdout, env.Version)
		return 0
	case inv.Show:
		return show(env)
	case inv.Failure != nil:
		if inv.Failure.Message != "" {
			fmt.Fprintln(env.Stderr, inv.Failure.Message)
		}
		if inv.Failure.Exit < 0 {
			return zenity.Code(zenity.Failed, env.Getenv)
		}
		return inv.Failure.Exit
	}
	return ask(inv, env)
}

const usage = `Usage: galley DIALOG [OPTION…] [VALUE…]

A zenity drop-in whose dialogs stack in one window. It reads zenity's
command line and exits and prints as zenity does: see zenity's own --help
for its options.

  DIALOG is one of --question, --info, --warning, --error, --entry,
  --password, --text-info, --list, --forms, --calendar, --scale,
  --color-selection, --file-selection, --progress, --notification
  or --about.

  --show         bring the window forward
  --version      print the version

The window listens on $XDG_RUNTIME_DIR/galley/sock.
`

// Socket is where the window listens: $GALLEY_SOCKET if set, for tests and
// for running a window by hand, else $XDG_RUNTIME_DIR/galley/sock.
func Socket(getenv func(string) string) (string, error) {
	if s := getenv("GALLEY_SOCKET"); s != "" {
		return s, nil
	}
	dir := getenv("XDG_RUNTIME_DIR")
	if dir == "" {
		return "", errors.New("XDG_RUNTIME_DIR is not set")
	}
	return filepath.Join(dir, "galley", "sock"), nil
}

// Dial connects to the window, having checked it is ours to talk to: the
// socket's directory is ours and no one else's, and the process listening
// is running as us. A password goes down this socket, so a listener anyone
// else could have put there is not one to send it to.
func Dial(path string) (*net.UnixConn, error) {
	dir := filepath.Dir(path)
	fi, err := os.Lstat(dir)
	if err != nil {
		return nil, err
	}
	st, ok := fi.Sys().(*syscall.Stat_t)
	if !fi.IsDir() || !ok || int(st.Uid) != os.Getuid() || fi.Mode().Perm()&0o077 != 0 {
		return nil, fmt.Errorf("%s is not a directory only this user can open", dir)
	}
	c, err := net.DialUnix("unix", nil, &net.UnixAddr{Name: path, Net: "unix"})
	if err != nil {
		return nil, err
	}
	raw, err := c.SyscallConn()
	if err != nil {
		c.Close()
		return nil, err
	}
	var cred *syscall.Ucred
	var credErr error
	if err := raw.Control(func(fd uintptr) {
		cred, credErr = syscall.GetsockoptUcred(int(fd), syscall.SOL_SOCKET, syscall.SO_PEERCRED)
	}); err != nil || credErr != nil {
		c.Close()
		return nil, errors.Join(err, credErr)
	}
	if int(cred.Uid) != os.Getuid() {
		c.Close()
		return nil, fmt.Errorf("the window at %s runs as uid %d, not this user", path, cred.Uid)
	}
	return c, nil
}

// unreachable is what a client says, and exits, when it cannot put its item
// to the window: zenity's exit when GTK cannot open a display.
func unreachable(env Env, err error) int {
	fmt.Fprintf(env.Stderr, "galley: cannot reach the window: %v\n", err)
	return 1
}

func show(env Env) int {
	path, err := Socket(env.Getenv)
	if err != nil {
		return unreachable(env, err)
	}
	c, err := Dial(path)
	if err != nil {
		return unreachable(env, err)
	}
	defer c.Close()
	token := env.Getenv("XDG_ACTIVATION_TOKEN")
	if token == "" {
		token = env.Getenv("DESKTOP_STARTUP_ID")
	}
	if err := json.NewEncoder(c).Encode(wire.Hello{Galley: wire.Version, Show: true, Token: token}); err != nil {
		return unreachable(env, err)
	}
	var a wire.Answer
	if err := json.NewDecoder(bufio.NewReader(c)).Decode(&a); err != nil || !a.Shown {
		if a.Error != "" {
			err = errors.New(a.Error)
		}
		return unreachable(env, fmt.Errorf("the window did not come forward: %v", err))
	}
	return 0
}

func ask(inv *zenity.Invocation, env Env) int {
	item := inv.Item
	now := env.Now
	if now == nil {
		now = time.Now
	}
	switch item.Kind {
	case wire.KindText:
		if inv.Filename != "" {
			text, err := readFile(inv.Filename)
			if err != nil {
				// zenity warns and shows the dialog empty.
				fmt.Fprintf(env.Stderr, "galley: Cannot open file '%s': %v\n", inv.Filename, err)
			}
			item.Info.Text = text
		} else {
			item.Info.More = true
		}
	case wire.KindCalendar, wire.KindForms:
		item.Locale = locale(env.Getenv)
	case wire.KindAbout:
		item.Text = fmt.Sprintf("galley %s\n\nA zenity drop-in whose dialogs stack in one window."+
			"\n\nhttps://github.com/danielbodart/galley\nMIT licence", env.Version)
	}

	path, err := Socket(env.Getenv)
	if err != nil {
		return unreachable(env, err)
	}
	c, err := Dial(path)
	if err != nil {
		return unreachable(env, err)
	}
	defer c.Close()

	var mu sync.Mutex
	enc := json.NewEncoder(c)
	send := func(v any) error {
		mu.Lock()
		defer mu.Unlock()
		return enc.Encode(v)
	}
	if err := send(wire.Hello{Galley: wire.Needs(&item), Item: &item}); err != nil {
		return unreachable(env, err)
	}

	// Every line the window sends: partial values and a notification's
	// being queued come before the answer, which is the last.
	lines := make(chan wire.Answer, 16)
	failed := make(chan error, 1)
	go func() {
		dec := json.NewDecoder(bufio.NewReader(c))
		for {
			var a wire.Answer
			if err := dec.Decode(&a); err != nil {
				failed <- err
				return
			}
			lines <- a
			if a.Partial == nil && !a.Queued {
				return
			}
		}
	}()

	// What stdin feeds, for the dialogs that read it. ended is closed when
	// it is done with; closing is a progress item's --auto-close.
	ended := make(chan struct{})
	closing := make(chan struct{}, 1)
	feed := func(f func()) {
		go func() {
			defer close(ended)
			f()
		}()
	}
	switch {
	case item.Kind == wire.KindText && item.Info.More:
		feed(func() { pump(env.Stdin, func(s string) error { return send(wire.Follow{Append: s}) }) })
	case item.Kind == wire.KindList && item.List.More:
		feed(func() { rows(env.Stdin, inv.List.Rows(), send) })
	case item.Kind == wire.KindProgress:
		feed(func() {
			if progress(env.Stdin, inv.Progress.Reader(), now, send) {
				closing <- struct{}{}
			}
		})
	case inv.Listen:
		feed(func() { listen(env.Stdin, zenity.NewListener(env.Cwd), send, env.Stderr) })
	}

	// The dialogs that are no window of zenity's own -- a notification,
	// the colour and file choosers -- exit on their timeout at once, having
	// printed nothing. The rest ask the window, which answers with what is
	// chosen so far.
	direct := item.Kind == wire.KindNotification || item.Kind == wire.KindColor || item.Kind == wire.KindFile
	var expired <-chan time.Time
	if inv.Timeout > 0 {
		expired = time.After(time.Duration(inv.Timeout) * time.Second)
	}
	var grace <-chan time.Time
	var listening <-chan struct{}
	if inv.Listen {
		listening = ended
	}
	for {
		select {
		case a := <-lines:
			switch {
			case a.Partial != nil:
				fmt.Fprintf(env.Stdout, "%d\n", *a.Partial)
				continue
			case a.Queued:
				// A notification is the person's once it is queued, and
				// zenity's returns at once unless it has a timeout to wait.
				if expired == nil && !inv.Listen {
					return 0
				}
				continue
			}
			return answered(inv, a, env)
		case err := <-failed:
			if errors.Is(err, io.EOF) {
				err = errors.New("it closed without answering")
			}
			fmt.Fprintf(env.Stderr, "galley: the window went away: %v\n", err)
			return 1
		case <-listening:
			// zenity listens on after stdin ends; galley's notifications
			// outlive their client, so it has nothing left to wait for.
			return 0
		case <-closing:
			return zenity.Code(zenity.OK, env.Getenv)
		case <-expired:
			expired = nil
			if direct {
				return zenity.Code(zenity.Timeout, env.Getenv)
			}
			// Asked of the window rather than decided here, since what is
			// chosen at the time is printed with the timeout.
			send(wire.Follow{Timeout: true})
			wait := env.Grace
			if wait == 0 {
				wait = 2 * time.Second
			}
			grace = time.After(wait)
		case <-grace:
			return zenity.Code(zenity.Timeout, env.Getenv)
		}
	}
}

// answered prints what zenity prints for an answer and returns its exit.
func answered(inv *zenity.Invocation, a wire.Answer, env Env) int {
	out, outcome, ok := inv.Output(a)
	if !ok {
		if a.Error != "" {
			fmt.Fprintf(env.Stderr, "galley: the window refused the item: %s\n", a.Error)
		} else {
			fmt.Fprintf(env.Stderr, "galley: the window gave an answer galley does not know: %q\n", a.Answer)
		}
		return 255
	}
	fmt.Fprint(env.Stdout, out)
	if outcome == zenity.Cancel && inv.Progress != nil && inv.Progress.AutoKill {
		// --auto-kill: zenity hangs up on whatever started it.
		if env.HangUpParent != nil {
			env.HangUpParent()
		} else {
			syscall.Kill(os.Getppid(), syscall.SIGHUP)
		}
	}
	return zenity.Code(outcome, env.Getenv)
}

// locale is the caller's LC_TIME, as setlocale finds it.
func locale(getenv func(string) string) string {
	for _, name := range []string{"LC_ALL", "LC_TIME", "LANG"} {
		if v := getenv(name); v != "" {
			return v
		}
	}
	return ""
}

// lines calls f with each line of r, newline and all, the last without one
// if it has none. Lines are read as GLib reads them, whatever their bytes.
func eachLine(r io.Reader, f func(line string) bool) {
	br := bufio.NewReaderSize(r, 64<<10)
	for {
		line, err := br.ReadString('\n')
		if line != "" && !f(line) {
			return
		}
		if err != nil {
			return
		}
	}
}

// rows sends a list's rows as stdin completes them, a row at a time, and
// the part of a row it ends with.
func rows(r io.Reader, rs *zenity.Rows, send func(any) error) {
	eachLine(r, func(line string) bool {
		if row := rs.Line(line); row != nil {
			return send(wire.Follow{Rows: [][]string{row}}) == nil
		}
		return true
	})
	if row := rs.End(); row != nil {
		send(wire.Follow{Rows: [][]string{row}})
	}
}

// progress sends a progress item's updates as stdin gives them. It returns
// true when the dialog closes itself, as --auto-close does.
func progress(r io.Reader, p *zenity.Progress, now func() time.Time, send func(any) error) bool {
	closed := false
	eachLine(r, func(line string) bool {
		u, close := p.Line(line, now())
		if u != nil && send(wire.Follow{Progress: u}) != nil {
			return false
		}
		closed = close
		return !close
	})
	if closed {
		return true
	}
	u, close := p.End()
	send(wire.Follow{Progress: u})
	return close
}

// listen sends a listening notification's messages as stdin gives them,
// and says what zenity would of a line it cannot use.
func listen(r io.Reader, l *zenity.Listener, send func(any) error, stderr io.Writer) {
	eachLine(r, func(line string) bool {
		c := l.Line(line)
		if c.Warning != "" {
			fmt.Fprintln(stderr, c.Warning)
		}
		if c.Notify != nil {
			return send(wire.Follow{Notify: c.Notify}) == nil
		}
		return true
	})
}

// Text-info's file is read whole, as zenity does, and capped where a window
// would no longer be the place to read it.
const maxText = 16 << 20

func readFile(name string) (string, error) {
	f, err := os.Open(name)
	if err != nil {
		return "", err
	}
	defer f.Close()
	b, err := io.ReadAll(io.LimitReader(f, maxText))
	return string(b), err
}

// pump sends stdin to the window as it arrives, a whole number of UTF-8
// characters at a time so that none is split across two appends and shown
// as two replacement characters. Bytes that are not UTF-8 become U+FFFD on
// the wire, which is JSON's rule.
func pump(r io.Reader, send func(string) error) {
	buf := make([]byte, 32<<10)
	var held []byte
	total := 0
	for total < maxText {
		n, err := r.Read(buf)
		if n > 0 {
			total += n
			chunk := append(held, buf[:n]...)
			cut := len(chunk)
			// Hold back an incomplete final character, at most three bytes.
			for i := len(chunk) - 1; i >= 0 && i >= len(chunk)-3; i-- {
				if utf8.RuneStart(chunk[i]) {
					if !utf8.FullRune(chunk[i:]) {
						cut = i
					}
					break
				}
			}
			held = append([]byte(nil), chunk[cut:]...)
			if cut > 0 {
				if send(string(chunk[:cut])) != nil {
					return
				}
			}
		}
		if err != nil {
			break
		}
	}
	if len(held) > 0 {
		send(string(held))
	}
}
