package xtcp

import (
	"context"
	"errors"
	"fmt"
	"log"
	"net"
	"os"
	"strings"

	xio "github.com/randomizedcoder/xtcp2/pkg/io_uring"
)

// errNoRingInCtx is returned by io_uring send paths when ringFromContext
// returns nil — indicates a misconfig where config.IoUring=true but the
// netlinker variant didn't stash a ring in the deserializer context.
var errNoRingInCtx = errors.New("io_uring destination: no ring in context (config.IoUring=true but netlinker variant disagrees?)")

// extractFD returns the underlying file descriptor from a net.Conn that
// is *net.UDPConn or *net.UnixConn. Called only when config.IoUring is
// true. The fd is dup'd internally by File() — we never close the returned
// *os.File handle, so the dup stays open for the io_uring path.
//
// Important caveat: calling File() puts the underlying socket into blocking
// mode. That's fine for io_uring (the ring itself manages readiness), but
// means the syscall destination path can't share the same connection —
// io_uring mode owns the conn exclusively.
func extractFD(c net.Conn) (int, error) {
	type fileGetter interface {
		File() (*os.File, error)
	}
	g, ok := c.(fileGetter)
	if !ok {
		return -1, fmt.Errorf("extractFD: conn type %T does not expose File()", c)
	}
	f, err := g.File()
	if err != nil {
		return -1, fmt.Errorf("extractFD File(): %w", err)
	}
	return int(f.Fd()), nil
}

// udpDest writes each marshaled record to a connected UDP socket.
// When config.IoUring is set, send goes through the per-netlinker ring
// instead of a direct syscall write.
type udpDest struct {
	x    *XTCP
	conn net.Conn
	fd   int
}

func newUDPDest(ctx context.Context, x *XTCP) (Destination, error) {
	addr := strings.TrimPrefix(x.config.Dest, "udp:")
	var dialer net.Dialer
	conn, err := dialer.DialContext(ctx, "udp", addr)
	if err != nil {
		return nil, fmt.Errorf("InitDestUDP net.Dial(udp, %q): %w", addr, err)
	}
	d := &udpDest{x: x, conn: conn}
	if x.config.IoUring {
		fd, err := extractFD(conn)
		if err != nil {
			return nil, fmt.Errorf("InitDestUDP extractFD: %w", err)
		}
		d.fd = fd
	}
	return d, nil
}

func (d *udpDest) Send(ctx context.Context, b *[]byte) (int, error) {
	if d.x.config.IoUring {
		ring := ringFromContext(ctx)
		if ring == nil {
			d.x.pC.WithLabelValues("destUDPIoUring", "noRing", "error").Inc()
			return 0, errNoRingInCtx
		}
		if _, err := ring.EnqueueSend(d.fd, b, xio.OpSendUDP); err != nil {
			d.x.pC.WithLabelValues("destUDPIoUring", "EnqueueSend", "error").Inc()
			if d.x.debugLevel > 100 {
				log.Printf("destUDPIoUring EnqueueSend err:%v", err)
			}
			return 0, err
		}
		return 1, nil
	}

	n, err := d.conn.Write(*b)
	if err != nil {
		d.x.pC.WithLabelValues("Inetdiager", "udpConn.Write", "error").Inc()
		if d.x.debugLevel > 100 {
			log.Printf("udpConn.Write(XtcpRecordBinary) err:%v", err)
		}
		return 0, err
	}
	d.x.pC.WithLabelValues("Inetdiager", "udpWrites", "count").Inc()
	d.x.pC.WithLabelValues("Inetdiager", "udpWriteBytes", "count").Add(float64(n))
	return 1, nil
}

func (d *udpDest) Close() error {
	if d.conn != nil {
		return d.conn.Close()
	}
	return nil
}

func init() {
	RegisterDestination("udp", newUDPDest)
}
