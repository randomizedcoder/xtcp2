package goip

import (
	"path/filepath"
	"strings"

	"golang.org/x/sys/unix"
)

// netns_get_fd (lib/namespace.c) for `ip link … netns`: the namespace's
// descriptor, which the request carries as IFLA_NET_NS_FD.

// netnsOpen opens a namespace file read-only, a variable so the tests can
// answer from paths of their own. ip opens without O_NONBLOCK, so a FIFO
// named as the namespace hangs it until a writer comes; goip opens at once,
// and the kernel refuses the descriptor as it does any file that is not a
// namespace.
var netnsOpen = func(path string) (int, error) {
	return unix.Open(path, unix.O_RDONLY|unix.O_CLOEXEC|unix.O_NONBLOCK, 0)
}

// netnsGetFd is netns_get_fd: a word with no slash is a name under
// NETNS_RUN_DIR, otherwise a path; when that doesn't open, a word that
// get_integer reads is a pid, /proc/PID/ns/net. It doesn't check that what
// opened is a namespace; the kernel refuses one that isn't.
func netnsGetFd(s string) (int, bool) {
	path := s
	if !strings.Contains(s, "/") {
		path = filepath.Join(netnsRunDir, s)
	}
	if fd, err := netnsOpen(path); err == nil {
		return fd, true
	}
	if _, ok := getInteger(s, 0); !ok {
		return -1, false
	}
	fd, err := netnsOpen("/proc/" + s + "/ns/net")
	return fd, err == nil
}

// openFds is ip's open_fds: the descriptors a command opened for its
// request, closed once it is written.
type openFds []int

func (o *openFds) add(fd int) {
	*o = append(*o, fd)
}

// close closes them all. A failed close is EBADF, which nothing here can
// act on.
func (o *openFds) close() {
	for _, fd := range *o {
		if err := unix.Close(fd); err != nil {
			continue
		}
	}
	*o = nil
}
