package nlconn

import (
	"context"
	"errors"
	"unsafe"

	"golang.org/x/sys/unix"
)

// Receive waits for one datagram, then drains immediately available datagrams up
// to 64 total. It never waits for a full batch. visit borrows data only until it
// returns; all decoding happens outside RawConn callbacks. A callback error
// stops the batch. The count is the number of callback invocations, even on error.
// Any error invalidates an in-progress request candidate; event callers must
// report loss/reconcile as appropriate. Cancellation/timeouts close the socket.
func (c *Conn) Receive(ctx context.Context, visit func([]byte) error) (int, error) {
	if visit == nil {
		return 0, ErrConfig
	}
	delivered := 0
	err := c.operation(ctx, c.file.SetReadDeadline, func() error {
		for delivered < maxBatch {
			data, err := c.receiveOne(ctx, delivered == 0)
			if delivered > 0 && errors.Is(err, unix.EAGAIN) {
				return nil
			}
			if err != nil {
				return err
			}
			delivered++
			if err := visit(data); err != nil {
				return err
			}
		}
		return nil
	})
	return delivered, err
}

// Drain reads and discards every datagram already queued, without waiting,
// and returns how many it discarded. A multicast listener calls it after
// ENOBUFS: what is still queued predates the events the kernel dropped, and
// the kernel delivers no further broadcast to the socket until its queue is
// empty (netlink_rcv_wake clears NETLINK_S_CONGESTED only then). A second
// ENOBUFS or a truncated datagram met while draining is discarded with the
// rest; the caller is resyncing already.
func (c *Conn) Drain(ctx context.Context) (int, error) {
	discarded := 0
	err := c.operation(ctx, c.file.SetReadDeadline, func() error {
		for {
			_, err := c.receiveOne(ctx, false)
			switch {
			case err == nil:
				discarded++
			case errors.Is(err, unix.EAGAIN):
				return nil
			case errors.Is(err, unix.ENOBUFS), errors.Is(err, ErrTruncated):
			default:
				return err
			}
		}
	})
	return discarded, err
}

// receiveState is receiveOne's working set. It lives in the Conn, which has a
// single I/O owner, so a datagram's receive allocates nothing: the RawConn
// callbacks are bound once and the sender is written into from rather than a
// new unix.Sockaddr.
type receiveState struct {
	done       <-chan struct{}
	from       unix.RawSockaddrNetlink
	n, flags   int
	err        error
	canceled   bool
	read       func(uintptr) bool
	readNoWait func(uintptr)
}

func (c *Conn) receiveOne(ctx context.Context, wait bool) ([]byte, error) {
	rx := &c.rx
	if rx.read == nil {
		rx.read = c.attempt
		rx.readNoWait = func(fd uintptr) { c.attempt(fd) }
	}
	rx.done, rx.n, rx.flags, rx.err, rx.canceled = ctx.Done(), 0, 0, nil, false
	var err error
	if wait {
		err = c.raw.Read(rx.read)
	} else {
		// Control pins the fd against close/reuse but never parks for readiness.
		err = c.raw.Control(rx.readNoWait)
	}
	rx.done = nil
	switch {
	case err != nil:
		return nil, err
	case rx.canceled:
		return nil, ctx.Err()
	case rx.err != nil:
		return nil, rx.err
	}
	if err := c.validateReceive(rx.n, rx.flags, &rx.from); err != nil {
		return nil, err
	}
	return c.buffer[:rx.n:rx.n], nil
}

// attempt is the RawConn callback: one recvmsg, retried on EINTR. It returns
// false on EAGAIN so Read parks in the poller.
func (c *Conn) attempt(fd uintptr) bool {
	rx := &c.rx
	for {
		select {
		case <-rx.done:
			rx.canceled = true
			return true
		default:
		}
		rx.n, rx.flags, rx.err = c.recv(int(fd), c.buffer, &rx.from, unix.MSG_DONTWAIT)
		if errors.Is(rx.err, unix.EINTR) {
			continue
		}
		return !errors.Is(rx.err, unix.EAGAIN)
	}
}

// recvmsgNetlink is recvmsg(2) into p, with the sender's address written into
// from. unix.Recvmsg would return a new unix.Sockaddr for every datagram. An
// address shorter than a netlink one, as from a non-netlink peer, leaves from
// zeroed, which validateReceive rejects.
func recvmsgNetlink(fd int, p []byte, from *unix.RawSockaddrNetlink, flags int) (int, int, error) {
	*from = unix.RawSockaddrNetlink{}
	iov := unix.Iovec{Base: unsafe.SliceData(p)}
	iov.SetLen(len(p))
	msg := unix.Msghdr{Name: (*byte)(unsafe.Pointer(from)), Namelen: unix.SizeofSockaddrNetlink, Iov: &iov, Iovlen: 1}
	n, _, errno := unix.Syscall(unix.SYS_RECVMSG, uintptr(fd), uintptr(unsafe.Pointer(&msg)), uintptr(flags))
	if errno != 0 {
		return 0, 0, errno
	}
	if msg.Namelen < unix.SizeofSockaddrNetlink {
		*from = unix.RawSockaddrNetlink{}
	}
	return int(n), int(msg.Flags), nil
}

func (c *Conn) validateReceive(n, flags int, from *unix.RawSockaddrNetlink) error {
	if from.Family != unix.AF_NETLINK || from.Pid != 0 {
		return ErrSender
	}
	if flags&unix.MSG_TRUNC != 0 {
		if len(c.buffer) == maxBuffer {
			return ErrOversize
		}
		c.buffer = make([]byte, min(2*len(c.buffer), maxBuffer))
		return ErrTruncated
	}
	if flags&unix.MSG_CTRUNC != 0 {
		return ErrAncillary
	}
	if n <= 0 || n > len(c.buffer) {
		return ErrLength
	}
	return nil
}
