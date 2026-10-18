package nlconn

import (
	"context"
	"encoding/binary"
	"fmt"
	"testing"

	"golang.org/x/sys/unix"
)

// benchConn is a Conn over one end of a SOCK_SEQPACKET pair, receiving with
// the production recvmsgNetlink. The peer is not the kernel, so the sender is
// stamped AF_NETLINK, pid 0 after each receive; everything else, the poller,
// the batch loop and validateReceive, is what a netlink listener runs.
func benchConn(b *testing.B) (*Conn, int) {
	b.Helper()
	fds, err := unix.Socketpair(unix.AF_UNIX, unix.SOCK_SEQPACKET|unix.SOCK_NONBLOCK|unix.SOCK_CLOEXEC, 0)
	if err != nil {
		b.Fatal(err)
	}
	b.Cleanup(func() { _ = unix.Close(fds[1]) })
	file, raw, err := wrapSocket(fds[0])
	if err != nil {
		b.Fatal(err)
	}
	c := &Conn{file: file, raw: raw, buffer: make([]byte, initialBuffer)}
	c.recv = func(fd int, p []byte, from *unix.RawSockaddrNetlink, flags int) (int, int, error) {
		n, recvFlags, err := recvmsgNetlink(fd, p, from, flags)
		from.Family, from.Pid = unix.AF_NETLINK, 0
		return n, recvFlags, err
	}
	b.Cleanup(func() { _ = c.Close() })
	return c, fds[1]
}

// BenchmarkReceive is one Receive of a batch already queued: the poller,
// recvmsg per datagram and validateReceive, with no decoding. Datagrams are
// 64-byte RTM_NEWLINK frames. ns/datagram and allocs/op show what the receive
// layer costs per notification. Nothing is allocated per datagram, so
// allocs/op is flat across batch sizes: none with a context that is never
// canceled, and context.AfterFunc's registration with a cancelable one, as a
// listener's is.
func BenchmarkReceive(b *testing.B) {
	datagram := make([]byte, 64)
	binary.LittleEndian.PutUint32(datagram, uint32(len(datagram)))
	binary.LittleEndian.PutUint16(datagram[4:], unix.RTM_NEWLINK)
	cancelable, cancel := context.WithCancel(context.Background())
	defer cancel()
	for _, ctx := range []struct {
		name string
		ctx  context.Context
	}{{"cancelable", cancelable}, {"background", context.Background()}} {
		for _, size := range []int{1, 8, 32, 64} {
			b.Run(fmt.Sprintf("ctx=%s/batch=%d", ctx.name, size), func(b *testing.B) {
				c, peer := benchConn(b)
				visit := func([]byte) error { return nil }
				b.ReportAllocs()
				for b.Loop() {
					b.StopTimer()
					for range size {
						if _, err := unix.Write(peer, datagram); err != nil {
							b.Fatal(err)
						}
					}
					b.StartTimer()
					if n, err := c.Receive(ctx.ctx, visit); n != size || err != nil {
						b.Fatalf("Receive: %d, %v", n, err)
					}
				}
				b.ReportMetric(float64(b.Elapsed().Nanoseconds())/float64(b.N*size), "ns/datagram")
			})
		}
	}
}
