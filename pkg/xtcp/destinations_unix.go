package xtcp

import (
	"context"
	"encoding/binary"
	"fmt"
	"log"
	"net"
	"strings"
)

// unixDest writes each marshaled record to a Unix stream socket, framed
// with a varint length prefix so the daemon reader can recover record
// boundaries. Wire format per record:
//
//	[varint(len(payload))] [payload bytes...]
//
// Daemon-side: read the varint via binary.ReadUvarint, then exactly that
// many payload bytes via io.ReadFull.
//
// Both paths (syscall + io_uring) ship the header and payload atomically:
// the syscall path uses net.Buffers (lowered to one writev(2) on
// *net.UnixConn) and the io_uring path uses EnqueueWritevUnix. Either
// way, a partial-write failure cannot leave a varint header on the
// receiver without its payload, which would otherwise wedge the
// daemon-side binary.ReadUvarint + io.ReadFull recovery loop.
type unixDest struct {
	x    *XTCP
	conn net.Conn
	fd   int
}

func newUnixDest(_ context.Context, x *XTCP) (Destination, error) {
	path := strings.TrimPrefix(x.config.Dest, "unix:")
	if x.debugLevel > 10 {
		log.Printf("InitDestUnix config.Dest:%s path:%s", x.config.Dest, path)
	}
	conn, err := net.Dial("unix", path)
	if err != nil {
		return nil, fmt.Errorf("InitDestUnix net.Dial(unix, %q): %w", path, err)
	}
	d := &unixDest{x: x, conn: conn}
	if x.config.IoUring {
		fd, err := extractFD(conn)
		if err != nil {
			return nil, fmt.Errorf("InitDestUnix extractFD: %w", err)
		}
		d.fd = fd
	}
	return d, nil
}

func (d *unixDest) Send(ctx context.Context, b *[]byte) (int, error) {
	if d.x.config.IoUring {
		ring := ringFromContext(ctx)
		if ring == nil {
			d.x.pC.WithLabelValues("destUnixIoUring", "noRing", "error").Inc()
			return 0, errNoRingInCtx
		}
		// Same varint framing as the syscall path, but delivered atomically
		// as a single writev SQE so the daemon receiver sees one frame per
		// record with no chance of partial-write interleaving.
		var hdr [binary.MaxVarintLen64]byte
		hdrLen := binary.PutUvarint(hdr[:], uint64(len(*b)))
		if _, err := ring.EnqueueWritevUnix(d.fd, hdr[:hdrLen], b); err != nil {
			d.x.pC.WithLabelValues("destUnixIoUring", "EnqueueWritev", "error").Inc()
			if d.x.debugLevel > 100 {
				log.Printf("destUnixIoUring EnqueueWritev err:%v", err)
			}
			return 0, err
		}
		return 1, nil
	}

	var hdr [binary.MaxVarintLen64]byte
	hdrLen := binary.PutUvarint(hdr[:], uint64(len(*b)))
	bufs := net.Buffers{hdr[:hdrLen], *b}
	written, err := bufs.WriteTo(d.conn)
	if err != nil {
		d.x.pC.WithLabelValues("destUnix", "Write", "error").Inc()
		if d.x.debugLevel > 100 {
			log.Printf("destUnix WriteTo err:%v written:%d", err, written)
		}
		return 0, err
	}
	d.x.pC.WithLabelValues("destUnix", "Writes", "count").Inc()
	d.x.pC.WithLabelValues("destUnix", "WriteBytes", "count").Add(float64(written))
	return 1, nil
}

func (d *unixDest) Close() error {
	if d.conn != nil {
		return d.conn.Close()
	}
	return nil
}

func init() {
	RegisterDestination("unix", newUnixDest)
}
