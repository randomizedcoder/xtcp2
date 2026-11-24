package gotc

import (
	"errors"
	"runtime"

	"golang.org/x/sys/unix"
)

// netnsRunDir is NETNS_RUN_DIR; the tests point it elsewhere.
var netnsRunDir = "/var/run/netns"

// netnsSwitch is lib/namespace.c netns_switch, which tc -netns calls
// before it opens its socket: open NETNS_RUN_DIR/name, then setns.
// Either failure is printed and main exits 255.
//
// netns_switch then unshares a mount namespace and remounts /sys (and
// bind-mounts /etc/netns/name) for the commands that read them; gotc
// reads neither, and a Go process can't unshare CLONE_NEWNS (its threads
// share their fs state), so it stops at setns. The goroutine is locked to
// its thread first and stays locked: that thread opens the netlink socket,
// which belongs to the namespace it was opened in.
func (r *runner) netnsSwitch(name string) error {
	fd, err := unix.Open(netnsRunDir+"/"+name, unix.O_RDONLY|unix.O_CLOEXEC, 0)
	if err != nil {
		r.errf("Cannot open network namespace \"%s\": %s\n", name, strerror(errnoOf(err)))
		return exitCode(255)
	}
	runtime.LockOSThread()
	serr := unix.Setns(fd, unix.CLONE_NEWNET)
	if cerr := unix.Close(fd); cerr != nil && r.errOut == nil {
		r.errOut = cerr
	}
	if serr != nil {
		r.errf("setting the network namespace \"%s\" failed: %s\n", name, strerror(errnoOf(serr)))
		return exitCode(255)
	}
	return nil
}

// errnoOf is err's errno, EIO when it carries none.
func errnoOf(err error) unix.Errno {
	var errno unix.Errno
	if errors.As(err, &errno) {
		return errno
	}
	return unix.EIO
}
