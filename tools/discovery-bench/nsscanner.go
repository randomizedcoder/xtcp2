package main

// nsScanner is the near-zero-allocation answer to "can a long-running daemon do
// better than 3 allocs/pid on the /proc scan?" — the reference implementation a
// productionized Method B would use.
//
// Why a struct and not sync.Pool: discovery runs on ONE goroutine, ONCE per poll
// (possibly minutes apart). A sync.Pool's buffers would be GC'd between such
// widely-spaced calls, so it would re-allocate every poll anyway. A long-lived
// struct that OWNS its scratch — the getdents buffer, path/readlink buffers, the
// dedup map, AND the open /proc directory fd — reuses all of it for the daemon's
// entire life. sync.Pool is for transient objects shared across many goroutines
// (which is exactly how the record/envelope hot path in pkg/xtcp uses it).
//
// Each scan:
//   - rewinds the persistent /proc fd (lseek) — no per-poll open/close,
//   - reads it with getdents into a reused buffer and parses dirents in place
//     (no per-name string, none of os.ReadDir's sort / DirEntry overhead),
//   - builds each "<pid>/ns/net\0" into a reused buffer and calls readlinkat
//     relative to the /proc fd via a raw syscall (no BytePtrFromString cstring),
//   - parses the inode straight from the reused readlink buffer,
//   - dedups into a reused map (cleared, not reallocated) keyed by inode with the
//     representative pid as the value (an int — no per-namespace string either).
//
// Result: zero steady-state allocation, independent of process count. Deliberately
// unsafe/raw and x86_64-oriented (the project's only arch); the plain scanProc*
// funcs remain for the A/B comparison and readability. Call Close at shutdown.

import (
	"unsafe"

	"golang.org/x/sys/unix"
)

type nsScanner struct {
	fd   int            // persistent /proc directory fd, rewound each scan
	dbuf []byte         // getdents buffer
	pbuf []byte         // NUL-terminated relative-path scratch ("<pid>/ns/net\0")
	lbuf []byte         // readlink target buffer
	seen map[uint64]int // inode → representative pid (reused; cleared each scan)
}

// newNsScanner opens procRoot once. If the open fails the scanner is still
// usable — scan just returns (0,0) — so callers need not special-case it.
func newNsScanner(procRoot string) *nsScanner {
	fd, err := unix.Open(procRoot, unix.O_RDONLY|unix.O_DIRECTORY|unix.O_CLOEXEC, 0)
	if err != nil {
		fd = -1
	}
	return &nsScanner{
		fd:   fd,
		dbuf: make([]byte, 64<<10),
		pbuf: make([]byte, 0, 32),
		lbuf: make([]byte, 64),
		seen: make(map[uint64]int, procMapHint),
	}
}

// Close releases the /proc directory fd. Idempotent.
func (s *nsScanner) Close() error {
	if s.fd < 0 {
		return nil
	}
	err := unix.Close(s.fd)
	s.fd = -1
	return err
}

// scan rewinds the /proc fd and repopulates s.seen with the distinct
// network-namespace inodes, returning (found, skipped). Zero steady-state allocs.
func (s *nsScanner) scan() (found, skipped int) {
	clear(s.seen)
	if s.fd < 0 {
		return 0, 0
	}
	if _, err := unix.Seek(s.fd, 0, unix.SEEK_SET); err != nil {
		return 0, 0
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

			// Relative path "<pid>/ns/net\0" resolved against the /proc fd.
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
				s.seen[ino] = pid
			}
		}
	}
	return len(s.seen), skipped
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
