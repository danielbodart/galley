package services

import (
	"net"
	"syscall"
	"unsafe"
)

// The poll(2) events this needs, from asm-generic/poll.h, the same on every
// architecture galley is built for.
const (
	pollIn    = 0x1
	pollRDHup = 0x2000
)

type pollFd struct {
	fd             int32
	events, revent int16
}

// WatchHangUp calls hungUp, once, if c's peer hangs up before stop is
// called: a service asking for a requester on a unix socket cancels its
// question with it, so the row goes from the window when the requester
// does. stop returns once the watch has ended, hungUp having been called or
// not, and is called once.
func WatchHangUp(c *net.UnixConn, hungUp func()) (stop func(), err error) {
	var p [2]int
	if err := syscall.Pipe2(p[:], syscall.O_CLOEXEC); err != nil {
		return nil, err
	}
	watching := make(chan struct{})
	go func() {
		defer close(watching)
		if pollHangUp(c, p[0]) {
			hungUp()
		}
	}()
	return func() {
		syscall.Close(p[1])
		<-watching
		syscall.Close(p[0])
	}, nil
}

// pollHangUp waits for c's peer to hang up or stop's other end to close,
// and says which. A connection whose descriptor cannot be had is never
// said to have hung up.
func pollHangUp(c *net.UnixConn, stop int) (hungUp bool) {
	rc, err := c.SyscallConn()
	if err != nil {
		return false
	}
	_ = rc.Control(func(fd uintptr) {
		fds := []pollFd{{fd: int32(fd), events: pollRDHup}, {fd: int32(stop), events: pollIn}}
		for {
			_, _, e := syscall.Syscall6(syscall.SYS_PPOLL, uintptr(unsafe.Pointer(&fds[0])), uintptr(len(fds)), 0, 0, 0, 0)
			if e != syscall.EINTR {
				break
			}
		}
		hungUp = fds[0].revent != 0
	})
	return hungUp
}
