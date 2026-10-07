// Package netlink owns the monitor's nonblocking Linux netlink sockets.
// It provides transport only; transaction matching and event policy belong to
// callers. Socket and receive-buffer ownership never cross the public API.
package netlink

import (
	"context"
	"errors"
	"fmt"
	"math"
	"os"
	"sync"
	"sync/atomic"
	"syscall"
	"time"

	"golang.org/x/sys/unix"
)

const (
	initialBuffer = 64 * 1024
	maxBuffer     = 4 * 1024 * 1024
	maxBatch      = 64
)

var (
	ErrConfig     = errors.New("netlink: invalid socket configuration")
	ErrConcurrent = errors.New("netlink: socket already has an I/O owner")
	ErrSender     = errors.New("netlink: sender is not the kernel")
	ErrTruncated  = errors.New("netlink: datagram truncated; next buffer enlarged")
	ErrOversize   = errors.New("netlink: datagram exceeds buffer cap")
	ErrAncillary  = errors.New("netlink: ancillary metadata truncated")
	ErrLength     = errors.New("netlink: invalid datagram length")
)

type pollFile interface {
	SetReadDeadline(time.Time) error
	SetWriteDeadline(time.Time) error
	Close() error
}

type recvFunc func(int, []byte, []byte, int) (int, int, int, unix.Sockaddr, error)
type sendFunc func(int, []byte, []byte, unix.Sockaddr, int) (int, error)

// Conn has a single owner for Send/Receive. Concurrent operations are rejected;
// Close is safe concurrently, idempotent, and wakes a parked operation. The
// caller must Close on every exit path. No descriptor integer escapes Conn.
type Conn struct {
	file      pollFile
	raw       syscall.RawConn
	recv      recvFunc
	send      sendFunc
	buffer    []byte
	busy      atomic.Bool
	closed    atomic.Bool
	closeOnce sync.Once
	closeErr  error
}

type socketOps struct {
	socket func(int, int, int) (int, error)
	bind   func(int, unix.Sockaddr) error
	join   func(int, int, int, int) error
	close  func(int) error
	wrap   func(int) (pollFile, syscall.RawConn, error)
}

// Open creates a CLOEXEC, nonblocking socket in the calling thread's current
// network namespace. groups are numeric multicast group IDs, not bit masks.
// Route, generic and RDMA protocols use separate Conn instances. ctx bounds
// acquisition; each subsequent operation supplies its own cancellation context.
func Open(ctx context.Context, protocol int, groups []uint32) (*Conn, error) {
	return openSocket(ctx, protocol, groups, socketOps{
		socket: unix.Socket, bind: unix.Bind, join: unix.SetsockoptInt,
		close: unix.Close, wrap: wrapSocket,
	})
}

func openSocket(ctx context.Context, protocol int, groups []uint32, ops socketOps) (*Conn, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if protocol != unix.NETLINK_ROUTE && protocol != unix.NETLINK_GENERIC && protocol != unix.NETLINK_RDMA {
		return nil, ErrConfig
	}
	for _, group := range groups {
		if group == 0 || group > math.MaxInt32 {
			return nil, ErrConfig
		}
	}
	fd, err := ops.socket(unix.AF_NETLINK, unix.SOCK_RAW|unix.SOCK_NONBLOCK|unix.SOCK_CLOEXEC, protocol)
	if err != nil {
		return nil, fmt.Errorf("netlink socket: %w", err)
	}
	if err = configureSocket(fd, groups, ops); err != nil {
		return nil, errors.Join(err, ops.close(fd))
	}
	// wrap takes ownership even on error; never raw-close fd after this point.
	file, raw, err := ops.wrap(fd)
	if err != nil {
		return nil, err
	}
	c := &Conn{file: file, raw: raw, recv: unix.Recvmsg, send: unix.SendmsgN, buffer: make([]byte, initialBuffer)}
	if err := ctx.Err(); err != nil {
		return nil, errors.Join(err, c.Close())
	}
	return c, nil
}

func configureSocket(fd int, groups []uint32, ops socketOps) error {
	if err := ops.bind(fd, &unix.SockaddrNetlink{Family: unix.AF_NETLINK}); err != nil {
		return fmt.Errorf("netlink bind: %w", err)
	}
	for _, group := range groups {
		if err := ops.join(fd, unix.SOL_NETLINK, unix.NETLINK_ADD_MEMBERSHIP, int(group)); err != nil {
			return fmt.Errorf("netlink membership %d: %w", group, err)
		}
	}
	return nil
}

func wrapSocket(fd int) (pollFile, syscall.RawConn, error) {
	f := os.NewFile(uintptr(fd), "go-link-monitor-netlink")
	if f == nil {
		return nil, nil, errors.Join(os.ErrInvalid, unix.Close(fd))
	}
	raw, err := f.SyscallConn()
	if err == nil {
		// Fail immediately if this descriptor cannot use Go's poller/deadlines.
		err = f.SetDeadline(time.Time{})
	}
	if err != nil {
		return nil, nil, errors.Join(err, f.Close())
	}
	return f, raw, nil
}

// Close releases the owned os.File once. It never raw-closes a possibly reused
// descriptor, including when a cancellation callback races with owner shutdown.
func (c *Conn) Close() error {
	c.closeOnce.Do(func() {
		c.closed.Store(true)
		c.closeErr = c.file.Close()
	})
	return c.closeErr
}

func (c *Conn) operation(ctx context.Context, deadline func(time.Time) error, run func() error) (err error) {
	if !c.busy.CompareAndSwap(false, true) {
		return ErrConcurrent
	}
	defer c.busy.Store(false)
	if c.closed.Load() {
		return errors.Join(os.ErrClosed, c.Close())
	}
	if err := ctx.Err(); err != nil {
		return errors.Join(err, c.Close())
	}
	// AfterFunc creates no waiting goroutine. On cancellation it closes the
	// pollable file. Wait for an already-started callback before releasing the
	// operation so a callback can never close a later operation's descriptor.
	done := make(chan error, 1)
	stop := context.AfterFunc(ctx, func() {
		done <- c.Close()
	})
	defer func() {
		if !stop() {
			err = errors.Join(err, <-done)
		}
		if ctx.Err() != nil {
			err = errors.Join(ctx.Err(), err, c.Close())
		} else if errors.Is(err, os.ErrDeadlineExceeded) {
			// Never let remaining multipart replies leak across a timeout.
			err = errors.Join(err, c.Close())
		}
		// RawConn exposes internal/poll closing errors rather than os.ErrClosed.
		// Use owned state, never error text, to provide a stable classification.
		if c.closed.Load() {
			err = errors.Join(err, os.ErrClosed, c.Close())
		}
	}()
	due, _ := ctx.Deadline()
	if err = deadline(due); err != nil {
		return err
	}
	return run()
}
