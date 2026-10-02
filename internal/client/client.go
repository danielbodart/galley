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

// ZenityVersion is the zenity whose command line galley reads, which is what
// `zenity --version` prints when galley is installed as zenity: a script
// that checks it is asking which zenity's options it may use.
const ZenityVersion = "4.2.2"

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
}

// Main runs one invocation and returns its exit status. args includes the
// program name, which decides what --version prints.
func Main(args []string, env Env) int {
	inv, err := zenity.Parse(args[1:], env.Cwd)
	if err != nil {
		fmt.Fprintln(env.Stderr, err)
		return 255
	}
	switch {
	case inv.Help:
		fmt.Fprint(env.Stdout, usage)
		return 0
	case inv.Version:
		if filepath.Base(args[0]) == "zenity" {
			fmt.Fprintln(env.Stdout, ZenityVersion)
		} else {
			fmt.Fprintln(env.Stdout, env.Version)
		}
		return 0
	case inv.Show:
		return show(env)
	}
	return ask(inv, env)
}

const usage = `Usage: galley [--question|--info|--warning|--error|--entry|--text-info] [OPTION…]

A zenity drop-in whose dialogs stack in one window. It reads zenity's
command line and exits and prints as zenity does.

  --title=TITLE  --text=TEXT  --width=W  --height=H  --timeout=SECONDS
  --ok-label=LABEL  --cancel-label=LABEL  --extra-button=LABEL (repeatable)
  --icon=ICON  --no-markup  --no-wrap  --ellipsize
  --question: --default-cancel  --switch
  --entry: --entry-text=TEXT  --hide-text
  --text-info: --filename=FILE (else stdin)  --checkbox=TEXT  --auto-scroll

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
	stream := false
	if item.Kind == wire.KindText {
		if inv.Filename != "" {
			text, err := readFile(inv.Filename)
			if err != nil {
				// zenity warns and shows the dialog empty.
				fmt.Fprintf(env.Stderr, "galley: Cannot open file '%s': %v\n", inv.Filename, err)
			}
			item.Info.Text = text
		} else {
			stream = true
			item.Info.More = true
		}
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
	if err := send(wire.Hello{Galley: wire.Version, Item: &item}); err != nil {
		return unreachable(env, err)
	}

	answers := make(chan wire.Answer, 1)
	failed := make(chan error, 1)
	go func() {
		var a wire.Answer
		if err := json.NewDecoder(bufio.NewReader(c)).Decode(&a); err != nil {
			failed <- err
			return
		}
		answers <- a
	}()

	if stream {
		go pump(env.Stdin, func(s string) error { return send(wire.Follow{Append: s}) })
	}

	var expired <-chan time.Time
	if inv.Timeout > 0 {
		expired = time.After(time.Duration(inv.Timeout) * time.Second)
	}
	var grace <-chan time.Time
	for {
		select {
		case a := <-answers:
			return answered(inv, a, env)
		case err := <-failed:
			if errors.Is(err, io.EOF) {
				err = errors.New("it closed without answering")
			}
			fmt.Fprintf(env.Stderr, "galley: the window went away: %v\n", err)
			return 1
		case <-expired:
			expired = nil
			// Asked of the window rather than decided here, since an
			// entry's text at the time is printed with the timeout.
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
	entry := func() {
		if inv.Item.Kind == wire.KindEntry {
			text := ""
			if a.Entry != nil {
				text = *a.Entry
			}
			fmt.Fprintln(env.Stdout, text)
		}
	}
	switch a.Answer {
	case wire.AnswerOK:
		entry()
		return zenity.Code(zenity.OK, env.Getenv)
	case wire.AnswerCancel:
		return zenity.Code(zenity.Cancel, env.Getenv)
	case wire.AnswerTimeout:
		entry()
		return zenity.Code(zenity.Timeout, env.Getenv)
	case wire.AnswerExtra:
		if a.Index >= 0 && a.Index < len(inv.Extra) {
			fmt.Fprintln(env.Stdout, inv.Extra[a.Index])
			return zenity.Code(zenity.Extra, env.Getenv)
		}
	}
	if a.Error != "" {
		fmt.Fprintf(env.Stderr, "galley: the window refused the item: %s\n", a.Error)
	} else {
		fmt.Fprintf(env.Stderr, "galley: the window gave an answer galley does not know: %q\n", a.Answer)
	}
	return 255
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
