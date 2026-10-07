package netlink

import (
	"context"
	"errors"

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

func (c *Conn) receiveOne(ctx context.Context, wait bool) ([]byte, error) {
	var n, flags int
	var from unix.Sockaddr
	var recvErr error
	attempt := func(fd uintptr) bool {
		for {
			if recvErr = ctx.Err(); recvErr != nil {
				return true
			}
			n, _, flags, from, recvErr = c.recv(int(fd), c.buffer, nil, unix.MSG_DONTWAIT)
			if errors.Is(recvErr, unix.EINTR) {
				continue
			}
			return !errors.Is(recvErr, unix.EAGAIN)
		}
	}
	var err error
	if wait {
		err = c.raw.Read(attempt)
	} else {
		// Control pins the fd against close/reuse but never parks for readiness.
		err = c.raw.Control(func(fd uintptr) { attempt(fd) })
	}
	if err != nil {
		return nil, err
	}
	if recvErr != nil {
		return nil, recvErr
	}
	if err := c.validateReceive(n, flags, from); err != nil {
		return nil, err
	}
	return c.buffer[:n:n], nil
}

func (c *Conn) validateReceive(n, flags int, from unix.Sockaddr) error {
	sender, ok := from.(*unix.SockaddrNetlink)
	if !ok || sender == nil || sender.Family != unix.AF_NETLINK || sender.Pid != 0 {
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
