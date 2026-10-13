package services

import (
	"errors"
	"fmt"
	"net"
	"os"
	"strconv"
	"strings"
	"syscall"
	"unsafe"
)

// The kernel's, from linux/socket.h and linux/pidfd.h, which the standard
// library's syscall package does not have: galley's client takes no
// dependency beyond it, and these are the same on every architecture galley
// is built for.
const (
	sysPidfdOpen = 434        // pidfd_open(2), Linux 5.3, one number on every architecture
	soPeerPidfd  = 77         // SO_PEERPIDFD, Linux 6.5
	pidfdGetInfo = 0xc048ff0b // PIDFD_GET_INFO, _IOWR(0xff, 11, struct pidfd_info), Linux 6.13

	pidfdInfoPid      = 0x1
	pidfdInfoCreds    = 0x2
	pidfdInfoCgroupID = 0x4
)

// pidfdInfo is struct pidfd_info as Linux 6.15 has it.
type pidfdInfo struct {
	Mask, CgroupID                                                    uint64
	Pid, Tgid, Ppid, Ruid, Rgid, Euid, Egid, Suid, Sgid, Fsuid, Fsgid uint32
	ExitCode                                                          int32
	CoredumpMask                                                      uint32
	_                                                                 uint32
}

const pidfdWant = pidfdInfoPid | pidfdInfoCreds | pidfdInfoCgroupID

// info reads pidfd's process, checking the mask before any field: a pidfd
// whose process has gone reads as pid 0 and uid 0.
func info(pidfd int) (pidfdInfo, error) {
	in := pidfdInfo{Mask: pidfdWant}
	if _, _, e := syscall.Syscall(syscall.SYS_IOCTL, uintptr(pidfd), pidfdGetInfo, uintptr(unsafe.Pointer(&in))); e != 0 {
		return pidfdInfo{}, fmt.Errorf("PIDFD_GET_INFO: %w", e)
	}
	if in.Mask&pidfdWant != pidfdWant {
		return pidfdInfo{}, errors.New("PIDFD_GET_INFO: the process has gone")
	}
	return in, nil
}

// CgroupID is the cgroup pid runs in, by its directory's inode, read
// through a pidfd: what Client.Cgroup returns, for a test whose stand-in
// window is its own process.
func CgroupID(pid int) (uint64, error) {
	pidfd, err := pidfdOpen(pid)
	if err != nil {
		return 0, err
	}
	defer syscall.Close(pidfd)
	in, err := info(pidfd)
	if err != nil {
		return 0, err
	}
	return in.CgroupID, nil
}

func pidfdOpen(pid int) (int, error) {
	fd, _, e := syscall.Syscall(sysPidfdOpen, uintptr(pid), 0, 0)
	if e != 0 {
		return -1, fmt.Errorf("pidfd_open %d: %w", pid, e)
	}
	return int(fd), nil
}

// peer is who is on the other end of c: its uid, as SO_PEERCRED has it,
// and its process, through SO_PEERPIDFD, the process the kernel recorded at
// connect and never one that has since taken its pid.
func peer(c *net.UnixConn) (uid int, in pidfdInfo, pidfd int, err error) {
	rc, err := c.SyscallConn()
	if err != nil {
		return 0, pidfdInfo{}, -1, err
	}
	var cred *syscall.Ucred
	var credErr, fdErr error
	pidfd = -1
	if err := rc.Control(func(fd uintptr) {
		cred, credErr = syscall.GetsockoptUcred(int(fd), syscall.SOL_SOCKET, syscall.SO_PEERCRED)
		var v int32
		n := uint32(unsafe.Sizeof(v))
		if _, _, e := syscall.Syscall6(syscall.SYS_GETSOCKOPT, fd, syscall.SOL_SOCKET, soPeerPidfd,
			uintptr(unsafe.Pointer(&v)), uintptr(unsafe.Pointer(&n)), 0); e != 0 {
			fdErr = fmt.Errorf("SO_PEERPIDFD: %w", e)
			return
		}
		pidfd = int(v)
	}); err != nil {
		return 0, pidfdInfo{}, -1, err
	}
	if credErr != nil {
		if pidfd >= 0 {
			syscall.Close(pidfd)
		}
		return 0, pidfdInfo{}, -1, fmt.Errorf("SO_PEERCRED: %w", credErr)
	}
	if fdErr != nil {
		return 0, pidfdInfo{}, -1, fdErr
	}
	if in, err = info(pidfd); err != nil {
		syscall.Close(pidfd)
		return 0, pidfdInfo{}, -1, err
	}
	return int(cred.Uid), in, pidfd, nil
}

// checkPeer is the window answering for uid: a process of uid's, running in
// the cgroup want, by inode, at connect. It reads the process's start time
// and cgroup path from /proc for the Window it returns, then asks the pidfd
// again, so what was read is that process's and not a later one's.
func checkPeer(c *net.UnixConn, uid int, want func(int) (uint64, error)) (Window, error) {
	got, in, pidfd, err := peer(c)
	if err != nil {
		return Window{}, fmt.Errorf("galley peer: %w", err)
	}
	defer syscall.Close(pidfd)
	if got != uid {
		return Window{}, fmt.Errorf("galley peer uid %d is not %d", got, uid)
	}
	w := Window{Pid: int(in.Pid)}
	dir := "/proc/" + strconv.Itoa(w.Pid)
	if w.Start, err = startTime(dir); err != nil {
		return Window{}, fmt.Errorf("galley peer cannot be read: %w", err)
	}
	if w.Cgroup, err = cgroupPath(dir); err != nil {
		return Window{}, fmt.Errorf("galley peer cannot be read: %w", err)
	}
	if _, err := info(pidfd); err != nil {
		return Window{}, fmt.Errorf("galley peer: %w", err)
	}
	cgroup, err := want(uid)
	if err != nil {
		return Window{}, fmt.Errorf("galley.service: %w", err)
	}
	if in.CgroupID != cgroup {
		return Window{}, fmt.Errorf("galley peer pid %d is in %s, not galley.service", w.Pid, w.Cgroup)
	}
	return w, nil
}

// startTime is /proc/<pid>/stat's starttime, field 22, read after the
// comm, which may itself hold spaces and brackets.
func startTime(dir string) (uint64, error) {
	b, err := os.ReadFile(dir + "/stat")
	if err != nil {
		return 0, err
	}
	s := string(b)
	rp := strings.LastIndexByte(s, ')')
	if rp < 0 {
		return 0, fmt.Errorf("%s/stat: no comm", dir)
	}
	f := strings.Fields(s[rp+1:])
	if len(f) < 20 {
		return 0, fmt.Errorf("%s/stat: %d fields", dir, len(f))
	}
	return strconv.ParseUint(f[19], 10, 64)
}

// cgroupPath is the process's cgroup v2 path, from /proc/<pid>/cgroup's
// "0::" line, as the log names it.
func cgroupPath(dir string) (string, error) {
	b, err := os.ReadFile(dir + "/cgroup")
	if err != nil {
		return "", err
	}
	for _, line := range strings.Split(string(b), "\n") {
		if path, ok := strings.CutPrefix(line, "0::"); ok {
			return path, nil
		}
	}
	return "", fmt.Errorf("%s/cgroup: no cgroup v2 path", dir)
}
