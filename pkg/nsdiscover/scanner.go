// Package nsdiscover discovers the set of Linux network namespaces on a host by
// scanning /proc/<pid>/ns/net inodes — i.e. every namespace that has at least
// one live process, including anonymous container/pod namespaces that have no
// /run/netns bind mount. This is the "Method B" discovery xtcp2 uses; see
// docs/design-namespace-discovery-and-reconcile.md for why it replaced the
// /run/netns directory + inotify model.
//
// Scanner is a reusable, zero-allocation scanner meant to be created once and
// reused for a long-running daemon's whole life (a struct that owns its scratch,
// not sync.Pool — see the design doc). Resolver maps a namespace inode to a
// best-effort human name for record labeling.
package nsdiscover

import (
	"bytes"
	"unsafe"

	"golang.org/x/sys/unix"
)

// Namespace is one discovered network namespace.
type Namespace struct {
	Inode uint64 // nsfs inode — the stable identity
	Pid   int    // a representative live pid; enter it via /proc/<Pid>/ns/net
}

// scannerMapHint pre-sizes the dedup set. Most hosts have well under this many
// distinct network namespaces; overshoot just avoids a couple of early grows.
const scannerMapHint = 64

// Scanner scans a procfs for distinct network namespaces. It is NOT safe for
// concurrent use: it is intended for a single owner goroutine that reuses it
// across polls. Each Scan rewinds the persistent procfs fd and reuses every
// buffer + the dedup map, so steady-state allocation is zero regardless of the
// process count. Call Close at shutdown.
type Scanner struct {
	fd   int                 // persistent procfs dir fd, rewound each scan
	dbuf []byte              // getdents buffer
	pbuf []byte              // NUL-terminated relative-path scratch ("<pid>/ns/net\0")
	lbuf []byte              // readlink target buffer
	seen map[uint64]struct{} // per-scan dedup set (cleared, not reallocated)
}

// NewScanner opens procRoot (typically "/proc"). If the open fails the scanner
// still constructs and Scan returns 0 — callers need not special-case it.
func NewScanner(procRoot string) *Scanner {
	fd, err := unix.Open(procRoot, unix.O_RDONLY|unix.O_DIRECTORY|unix.O_CLOEXEC, 0)
	if err != nil {
		fd = -1
	}
	return &Scanner{
		fd:   fd,
		dbuf: make([]byte, 64<<10),
		pbuf: make([]byte, 0, 32),
		lbuf: make([]byte, 64),
		seen: make(map[uint64]struct{}, scannerMapHint),
	}
}

// Close releases the procfs fd. Idempotent.
func (s *Scanner) Close() error {
	if s.fd < 0 {
		return nil
	}
	err := unix.Close(s.fd)
	s.fd = -1
	return err
}

// Scan rewinds procfs and calls fn exactly once per distinct network namespace.
// It returns the number of pids skipped (the pid exited mid-scan, or readlinkat
// returned EACCES when unprivileged). Zero steady-state allocation. fn receives
// a Namespace by value; it may copy it but must not assume any ordering.
func (s *Scanner) Scan(fn func(Namespace)) (skipped int) {
	clear(s.seen)
	if s.fd < 0 {
		return 0
	}
	if _, err := unix.Seek(s.fd, 0, unix.SEEK_SET); err != nil {
		return 0
	}
	for {
		n, gerr := unix.Getdents(s.fd, s.dbuf)
		if gerr != nil || n <= 0 {
			break
		}
		for off := 0; off < n; {
			d := (*unix.Dirent)(unsafe.Pointer(&s.dbuf[off]))
			reclen := int(d.Reclen)
			if reclen <= 0 || off+reclen > n {
				break // malformed record; stop this batch defensively
			}
			off += reclen

			name := direntName(d)
			if len(name) == 0 || name[0] < '0' || name[0] > '9' {
				continue
			}
			pid, ok := atoiBytes(name)
			if !ok {
				continue
			}

			// Relative path "<pid>/ns/net\0" resolved against the procfs fd.
			s.pbuf = append(s.pbuf[:0], name...)
			s.pbuf = append(s.pbuf, "/ns/net\x00"...)

			m, ok := rawReadlinkat(s.fd, s.pbuf, s.lbuf)
			if !ok {
				skipped++
				continue
			}
			ino, ok := parseNetInode(s.lbuf[:m])
			if !ok {
				skipped++
				continue
			}
			if _, dup := s.seen[ino]; !dup {
				s.seen[ino] = struct{}{}
				fn(Namespace{Inode: ino, Pid: pid})
			}
		}
	}
	return skipped
}

// direntName returns the NUL-terminated name of a getdents64 record as a byte
// slice aliasing the getdents buffer (no allocation). getdents guarantees the
// name is NUL-terminated within the record.
func direntName(d *unix.Dirent) []byte {
	name := (*[256]byte)(unsafe.Pointer(&d.Name[0]))
	i := 0
	for i < len(name) && name[i] != 0 {
		i++
	}
	return name[:i]
}

// atoiBytes parses a positive decimal from bytes without allocating.
func atoiBytes(b []byte) (int, bool) {
	if len(b) == 0 {
		return 0, false
	}
	n := 0
	for _, c := range b {
		if c < '0' || c > '9' {
			return 0, false
		}
		n = n*10 + int(c-'0')
	}
	return n, true
}

// parseNetInode extracts the inode from a "net:[4026531840]" symlink target,
// operating on bytes so the hot path needs no result-string alloc.
func parseNetInode(target []byte) (uint64, bool) {
	l := bytes.IndexByte(target, '[')
	r := bytes.IndexByte(target, ']')
	if l < 0 || r <= l+1 { // r<=l+1 also rejects empty brackets "[]"
		return 0, false
	}
	var ino uint64
	for _, c := range target[l+1 : r] {
		if c < '0' || c > '9' {
			return 0, false
		}
		ino = ino*10 + uint64(c-'0')
	}
	return ino, true
}

// rawReadlinkat calls readlinkat(dirfd, nulPath, buf) via a raw syscall so the
// path — already NUL-terminated in a reused buffer — is passed without the
// BytePtrFromString cstring copy the unix.Readlinkat wrapper would make. Returns
// the number of bytes written to buf.
func rawReadlinkat(dirfd int, nulPath, buf []byte) (int, bool) {
	r, _, errno := unix.Syscall6(
		unix.SYS_READLINKAT,
		uintptr(dirfd),
		uintptr(unsafe.Pointer(&nulPath[0])),
		uintptr(unsafe.Pointer(&buf[0])),
		uintptr(len(buf)),
		0, 0,
	)
	if errno != 0 {
		return 0, false
	}
	n := int(r)
	if n <= 0 {
		return 0, false
	}
	return n, true
}
