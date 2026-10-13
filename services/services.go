// Package services is a system service's way to put an item to a user's
// galley: through the services socket the window binds for them, at
// /run/galley-ask/<user>.sock, when its settings name the service's system
// user (see the README's Security section).
//
// An answer is trusted only from the window itself: a process of the
// user's, running in their galley.service's cgroup when it connected, which
// the package checks by the peer's pidfd before it sends anything. Anyone
// else on the socket, or no one, is ErrNobody: the caller then has no one
// to ask, and decides what that means.
//
// The conversation is wire's: a Hello naming the item, at wire.Version,
// and the window's Answer once someone answers it. Closing the connection
// withdraws the item, so a caller whose own requester goes away cancels
// the context it asked with, and the row goes from the window; WatchHangUp
// says when a unix socket's peer has gone, for a requester on one. A
// notification goes with its connection too, so Notify holds it until the
// person dismisses it.
//
// What the item says -- its text, its buttons, its fields -- is the
// caller's, as are what an answer means and how a typed password is kept:
// this package carries the item and the answer and decides neither.
package services

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"os/user"
	"path/filepath"
	"strconv"
	"syscall"

	"github.com/danielbodart/galley/wire"
)

// Dir is where the window binds each user's services socket,
// Dir/<user>.sock, a directory the system makes for them.
const Dir = "/run/galley-ask"

// ErrNobody is there being no window to answer: no socket, a peer that is
// not the user's galley.service, a window that went before it answered, or
// one that refused the item. The error wrapping it says which.
var ErrNobody = errors.New("no one to ask")

// Window is the galley process that answered, as checked when the
// connection was made: its pid and start time, together naming the
// process, and its cgroup path, for a log.
type Window struct {
	Pid    int
	Start  uint64
	Cgroup string
}

// Client reaches users' windows. The zero value is the real one; a test
// moves the sockets and stands its own process in for galley.service.
type Client struct {
	// Dir is where the sockets are; Dir when empty.
	Dir string
	// Cgroup is the cgroup, by its directory's inode, that uid's window
	// must run in; ServiceCgroup when nil. CgroupID gives a process's.
	Cgroup func(uid int) (uint64, error)
}

// ServiceCgroup is uid's galley.service's cgroup, by inode, stat'ed now:
// the user unit the home-manager module runs the window as.
func ServiceCgroup(uid int) (uint64, error) {
	var st syscall.Stat_t
	path := fmt.Sprintf("/sys/fs/cgroup/user.slice/user-%d.slice/user@%d.service/app.slice/galley.service", uid, uid)
	if err := syscall.Stat(path, &st); err != nil {
		return 0, err
	}
	return st.Ino, nil
}

// Ask puts item to name's window with the zero Client.
func Ask(ctx context.Context, name string, item *wire.Item) (wire.Answer, Window, error) {
	return Client{}.Ask(ctx, name, item)
}

// Notify shows a notification with the zero Client.
func Notify(ctx context.Context, name, text string) error {
	return Client{}.Notify(ctx, name, text)
}

// Ask puts item to name's window and returns its answer, once someone has
// given one, and the window that gave it. The window's lines before its
// answer, a notification's Queued or a scale's Partial, are passed over.
// Cancelling ctx withdraws the item and returns ctx's error; anything that
// leaves no one to answer is ErrNobody, a window refusing the item
// included.
func (cl Client) Ask(ctx context.Context, name string, item *wire.Item) (wire.Answer, Window, error) {
	conn, w, err := cl.dial(ctx, name)
	if err != nil {
		return wire.Answer{}, Window{}, err
	}
	defer conn.Close()
	defer context.AfterFunc(ctx, func() { conn.Close() })()

	a, err := converse(conn, item)
	if ctx.Err() != nil {
		return wire.Answer{}, Window{}, ctx.Err()
	}
	if err != nil {
		return wire.Answer{}, Window{}, fmt.Errorf("%w: %v", ErrNobody, err)
	}
	return a, w, nil
}

// Notify shows text in name's window as a notification, with an OK to
// dismiss it, and returns once the person has: a service's item goes from
// the window when its connection does, a notification as much as a
// question, so Notify holds the connection until then. Cancelling ctx takes
// the notification away and returns ctx's error.
func (cl Client) Notify(ctx context.Context, name, text string) error {
	_, _, err := cl.Ask(ctx, name, &wire.Item{Kind: wire.KindNotification, Text: text, Note: &wire.Note{},
		Buttons: []wire.Button{{Answer: wire.AnswerOK, Label: "OK", Underline: -1}}, Default: 0})
	return err
}

// dial connects to name's services socket and checks the window is at the
// other end, before anything is said to it.
func (cl Client) dial(ctx context.Context, name string) (*net.UnixConn, Window, error) {
	u, err := user.Lookup(name)
	if err != nil {
		return nil, Window{}, err
	}
	uid, err := strconv.Atoi(u.Uid)
	if err != nil {
		return nil, Window{}, err
	}
	dir, cgroup := cl.Dir, cl.Cgroup
	if dir == "" {
		dir = Dir
	}
	if cgroup == nil {
		cgroup = ServiceCgroup
	}

	var d net.Dialer
	c, err := d.DialContext(ctx, "unix", filepath.Join(dir, name+".sock"))
	if err != nil {
		if ctx.Err() != nil {
			return nil, Window{}, ctx.Err()
		}
		return nil, Window{}, fmt.Errorf("%w: %v", ErrNobody, err)
	}
	conn := c.(*net.UnixConn)
	w, err := checkPeer(conn, uid, cgroup)
	if err != nil {
		conn.Close()
		return nil, Window{}, fmt.Errorf("%w: %v", ErrNobody, err)
	}
	return conn, w, nil
}

// converse sends item's Hello and reads the window's lines until its
// answer, passing over a Queued or a Partial, or until it refuses the item.
func converse(conn *net.UnixConn, item *wire.Item) (wire.Answer, error) {
	line, err := json.Marshal(wire.Hello{Galley: wire.Version, Item: item})
	if err != nil {
		return wire.Answer{}, err
	}
	if _, err := conn.Write(append(line, '\n')); err != nil {
		return wire.Answer{}, err
	}
	d := json.NewDecoder(bufio.NewReader(conn))
	for {
		var a wire.Answer
		if err := d.Decode(&a); err != nil {
			return wire.Answer{}, err
		}
		if a.Error != "" {
			return wire.Answer{}, fmt.Errorf("galley says %q", a.Error)
		}
		if !a.Queued && a.Partial == nil {
			return a, nil
		}
	}
}
