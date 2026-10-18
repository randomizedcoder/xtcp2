// Package nlconn owns nonblocking Linux netlink sockets: linkmonitor's
// subscriptions and request clients, and pkg/rtnlmon's listener.
// It provides transport only; transaction matching and event policy belong to
// callers. Socket and receive-buffer ownership never cross the public API.
package nlconn

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
	ErrConfig     = errors.New("nlconn: invalid socket configuration")
	ErrConcurrent = errors.New("nlconn: socket already has an I/O owner")
	ErrSender     = errors.New("nlconn: sender is not the kernel")
	ErrTruncated  = errors.New("nlconn: datagram truncated; next buffer enlarged")
	ErrOversize   = errors.New("nlconn: datagram exceeds buffer cap")
	ErrAncillary  = errors.New("nlconn: ancillary metadata truncated")
	ErrLength     = errors.New("nlconn: invalid datagram length")
)

type pollFile interface {
	SetReadDeadline(time.Time) error
	SetWriteDeadline(time.Time) error
	Close() error
}

// recvFunc receives one datagram into the buffer and the sender into the
// address: (fd, buffer, sender, flags) to (length, message flags, error).
type recvFunc func(int, []byte, *unix.RawSockaddrNetlink, int) (int, int, error)
type sendFunc func(int, []byte, []byte, unix.Sockaddr, int) (int, error)

// Conn has a single owner for Send/Receive. Concurrent operations are rejected;
// Close is safe concurrently, idempotent, and wakes a parked operation. The
// caller must Close on every exit path. No descriptor integer escapes Conn.
type Conn struct {
	file   pollFile
	raw    syscall.RawConn
	recv   recvFunc
	send   sendFunc
	buffer []byte
	rx     receiveState
	// cancel closes the Conn when an operation's context is canceled and
	// reports Close's error on cancelDone; operation registers it with
	// context.AfterFunc. Both are built on first use and reused.
	cancel     func()
	cancelDone chan error
	busy       atomic.Bool
	closed     atomic.Bool
	closeOnce  sync.Once
	closeErr   error
}

type socketOps struct {
	socket func(int, int, int) (int, error)
	bind   func(int, unix.Sockaddr) error
	setInt func(int, int, int, int) error
	close  func(int) error
	wrap   func(int) (pollFile, syscall.RawConn, error)
}

// Open creates a CLOEXEC, nonblocking socket in the calling thread's current
// network namespace. groups are numeric multicast group IDs, not bit masks.
// Route, generic and RDMA protocols use separate Conn instances. ctx bounds
// acquisition; each subsequent operation supplies its own cancellation context.
func Open(ctx context.Context, protocol int, groups []uint32) (*Conn, error) {
	return OpenWithOptions(ctx, protocol, groups, Options{})
}

// Options are the socket settings Open leaves at the kernel's defaults.
type Options struct {
	// RcvBuf, when positive, sizes the receive buffer before bind:
	// SO_RCVBUFFORCE, which may exceed net.core.rmem_max, and SO_RCVBUF when
	// the caller lacks CAP_NET_ADMIN. A multicast listener's buffer decides how
	// large a burst it absorbs before the kernel reports ENOBUFS.
	RcvBuf int
}

// OpenWithOptions is Open with Options applied.
func OpenWithOptions(ctx context.Context, protocol int, groups []uint32, opts Options) (*Conn, error) {
	return openSocket(ctx, protocol, groups, opts, socketOps{
		socket: unix.Socket, bind: unix.Bind, setInt: unix.SetsockoptInt,
		close: unix.Close, wrap: wrapSocket,
	})
}

func openSocket(ctx context.Context, protocol int, groups []uint32, opts Options, ops socketOps) (*Conn, error) {
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
	if opts.RcvBuf < 0 {
		return nil, ErrConfig
	}
	fd, err := ops.socket(unix.AF_NETLINK, unix.SOCK_RAW|unix.SOCK_NONBLOCK|unix.SOCK_CLOEXEC, protocol)
	if err != nil {
		return nil, fmt.Errorf("netlink socket: %w", err)
	}
	if err = configureSocket(fd, groups, opts, ops); err != nil {
		return nil, errors.Join(err, ops.close(fd))
	}
	// wrap takes ownership even on error; never raw-close fd after this point.
	file, raw, err := ops.wrap(fd)
	if err != nil {
		return nil, err
	}
	c := &Conn{file: file, raw: raw, recv: recvmsgNetlink, send: unix.SendmsgN, buffer: make([]byte, initialBuffer)}
	if err := ctx.Err(); err != nil {
		return nil, errors.Join(err, c.Close())
	}
	return c, nil
}

func configureSocket(fd int, groups []uint32, opts Options, ops socketOps) error {
	if opts.RcvBuf > 0 {
		err := ops.setInt(fd, unix.SOL_SOCKET, unix.SO_RCVBUFFORCE, opts.RcvBuf)
		if errors.Is(err, unix.EPERM) {
			err = ops.setInt(fd, unix.SOL_SOCKET, unix.SO_RCVBUF, opts.RcvBuf)
		}
		if err != nil {
			return fmt.Errorf("netlink receive buffer %d: %w", opts.RcvBuf, err)
		}
	}
	if err := ops.bind(fd, &unix.SockaddrNetlink{Family: unix.AF_NETLINK}); err != nil {
		return fmt.Errorf("netlink bind: %w", err)
	}
	for _, group := range groups {
		if err := ops.setInt(fd, unix.SOL_NETLINK, unix.NETLINK_ADD_MEMBERSHIP, int(group)); err != nil {
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

// neverStarted is operation's stop for a context without a cancellation
// hook: the hook never started, so there is nothing to wait for.
func neverStarted() bool { return true }

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
	// That wait leaves cancelDone empty after every operation, so the callback
	// and its channel are built once per Conn and reused. A context that is
	// never canceled (Done is nil) needs no hook at all.
	stop := neverStarted
	if ctx.Done() != nil {
		if c.cancel == nil {
			c.cancelDone = make(chan error, 1)
			c.cancel = func() { c.cancelDone <- c.Close() }
		}
		stop = context.AfterFunc(ctx, c.cancel)
	}
	defer func() {
		if !stop() {
			err = errors.Join(err, <-c.cancelDone)
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
