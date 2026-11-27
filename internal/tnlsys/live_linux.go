package tnlsys

import (
	"errors"
	"io"
	"os"
	"path"
	"runtime"
	"strconv"
	"unsafe"

	"golang.org/x/sys/unix"
)

// SpillFill is what Live fills the bytes past an argument with, so a
// kernel write there shows.
const SpillFill = 0xA5

// tunDev is TUNDEV, the path a tun descriptor's /proc link reads.
const tunDev = "/dev/net/tun"

// Live asks the kernel. SysRoot and ProcRoot default to /sys and /proc;
// a test points them at a fake tree.
type Live struct {
	SysRoot  string
	ProcRoot string
}

// ifreqData is struct ifreq with the ifr_data member, as
// pkg/linkmonitor's ethtoolIfreq: the pointer stays visible to the GC, and
// the union is 24 bytes on 64-bit Linux, 16 on 32-bit.
type ifreqData struct {
	name    [IfNameSize]byte
	data    unsafe.Pointer
	padding [8 + unsafe.Sizeof(uintptr(0))]byte
}

// Ioctl makes req on a fresh socket, as tnl_get_ioctl and tnl_gen_ioctl
// do. The kernel writes into a copy of arg SpillWindow bytes longer, so a
// reply larger than arg lands in Go memory Live owns and comes back as
// spill instead of corrupting the heap.
func (l Live) Ioctl(family int, req uint32, name string, arg []byte) (spill []byte, err error) {
	if !KnownReq(req) || len(arg) == 0 || len(arg) > MaxArg {
		return nil, ErrRequest
	}
	fd, err := unix.Socket(family, unix.SOCK_DGRAM|unix.SOCK_CLOEXEC, 0)
	if err != nil {
		return nil, &SocketError{Err: err}
	}
	defer func() {
		if cerr := unix.Close(fd); cerr != nil && err == nil {
			err = cerr
		}
	}()
	buf := make([]byte, len(arg)+SpillWindow)
	copy(buf, arg)
	for i := len(arg); i < len(buf); i++ {
		buf[i] = SpillFill
	}
	var ifr ifreqData
	copy(ifr.name[:], IfName(name))
	ifr.data = unsafe.Pointer(&buf[0])
	_, _, errno := unix.Syscall(unix.SYS_IOCTL, uintptr(fd), uintptr(req), uintptr(unsafe.Pointer(&ifr)))
	runtime.KeepAlive(buf)
	if errno != 0 {
		return nil, errno
	}
	copy(arg, buf)
	return SpillOf(buf[len(arg):]), nil
}

// SpillOf is what the kernel wrote into a window past an argument that was
// filled with SpillFill: the window up to its last byte that is not
// SpillFill, nil when there is none. The kernel copies its reply from the
// argument's start, so its write is a prefix of the window; a reply ending
// in SpillFill bytes is cut short by them.
func SpillOf(w []byte) []byte {
	for i := len(w) - 1; i >= 0; i-- {
		if w[i] != SpillFill {
			return append([]byte(nil), w[:i+1]...)
		}
	}
	return nil
}

func (l Live) sysRoot() string {
	if l.SysRoot != "" {
		return l.SysRoot
	}
	return "/sys"
}

func (l Live) procRoot() string {
	if l.ProcRoot != "" {
		return l.ProcRoot
	}
	return "/proc"
}

// ReadProp reads /sys/class/net/DEV/PROP under an os.Root at SysRoot: the
// class/net entries are symlinks into devices/, which stay inside it. A
// name with a slash, or "." or "..", is ENOENT, as no device has one.
func (l Live) ReadProp(dev, prop string) (string, error) {
	if !plainName(dev) || !plainName(prop) {
		return "", unix.ENOENT
	}
	b, err := l.readSys(path.Join("class/net", dev, prop))
	if err != nil {
		return "", errnoOf(err)
	}
	return string(b), nil
}

func (l Live) readSys(name string) (b []byte, err error) {
	r, err := os.OpenRoot(l.sysRoot())
	if err != nil {
		return nil, err
	}
	defer closeInto(r, &err)
	return r.ReadFile(name)
}

// plainName is a single path element.
func plainName(s string) bool {
	return s != "" && s != "." && s != ".." && path.Base(s) == s && s[0] != '/'
}

// TunFDs is show_processes' walk: /proc/[0-9]*/fd/[0-9]* in directory
// order (glob with GLOB_NOSORT), the caller's own pid skipped, the
// descriptors whose link is /dev/net/tun kept with their fdinfo and their
// process's comm. A process or descriptor that goes away mid-walk is
// skipped; `ip` prints perror for those, which no recording reproduces.
func (l Live) TunFDs() (_ []TunFD, err error) {
	r, err := os.OpenRoot(l.procRoot())
	if err != nil {
		return nil, err
	}
	defer closeInto(r, &err)
	pids, err := readDirNames(r, ".")
	if err != nil {
		return nil, err
	}
	self := os.Getpid()
	out := []TunFD{}
	for _, p := range pids {
		pid, ok := leadingInt(p)
		if !ok || pid == self {
			continue
		}
		fds, err := readDirNames(r, p+"/fd")
		if err != nil {
			continue
		}
		for _, f := range fds {
			fd, ok := leadingInt(f)
			if !ok {
				continue
			}
			link, err := r.Readlink(p + "/fd/" + f)
			if err != nil || link != tunDev {
				continue
			}
			info, err := r.ReadFile(p + "/fdinfo/" + strconv.Itoa(fd))
			if err != nil {
				continue
			}
			t := TunFD{Pid: pid, FD: fd, FdInfo: string(info)}
			if comm, err := r.ReadFile(p + "/comm"); err == nil {
				t.Comm = string(comm)
			} else {
				t.NoComm = true
			}
			out = append(out, t)
		}
	}
	return out, nil
}

// readDirNames is a directory's names unsorted, in the order readdir
// returns them, as glob's GLOB_NOSORT leaves them.
func readDirNames(r *os.Root, dir string) (_ []string, err error) {
	f, err := r.Open(dir)
	if err != nil {
		return nil, err
	}
	defer closeInto(f, &err)
	names, err := f.Readdirnames(-1)
	if err != nil && !errors.Is(err, io.EOF) {
		return nil, err
	}
	return names, nil
}

// leadingInt is sscanf's %d on a name glob's [0-9]* matched: the leading
// digits, which must be there.
func leadingInt(s string) (int, bool) {
	n := 0
	for n < len(s) && s[n] >= '0' && s[n] <= '9' {
		n++
	}
	if n == 0 {
		return 0, false
	}
	v, err := strconv.Atoi(s[:n])
	return v, err == nil
}

// closeInto closes c, reporting its error through *err unless an earlier
// one is already there.
func closeInto(c io.Closer, err *error) {
	if cerr := c.Close(); cerr != nil && *err == nil {
		*err = cerr
	}
}
