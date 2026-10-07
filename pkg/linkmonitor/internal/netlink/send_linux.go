package netlink

import (
	"context"
	"errors"
	"io"

	"golang.org/x/sys/unix"
)

// Send writes one datagram to the kernel through the runtime poller. data must
// remain unchanged until return. The transaction layer supplies validated GET
// bytes and matching policy. A short write is an error, never a second datagram.
func (c *Conn) Send(ctx context.Context, data []byte) error {
	if len(data) == 0 || len(data) > maxBuffer {
		return ErrLength
	}
	return c.operation(ctx, c.file.SetWriteDeadline, func() error {
		var sendErr error
		var n int
		err := c.raw.Write(func(fd uintptr) bool {
			for {
				if sendErr = ctx.Err(); sendErr != nil {
					return true
				}
				n, sendErr = c.send(int(fd), data, nil, &unix.SockaddrNetlink{Family: unix.AF_NETLINK}, unix.MSG_DONTWAIT)
				if errors.Is(sendErr, unix.EINTR) {
					continue
				}
				return !errors.Is(sendErr, unix.EAGAIN)
			}
		})
		if err != nil {
			return err
		}
		if sendErr != nil {
			return sendErr
		}
		if n != len(data) {
			return io.ErrShortWrite
		}
		return nil
	})
}
