package xtcp

import (
	"context"
	"encoding/binary"
	"fmt"
	"log"
	"net"
	"strings"
)

// unixDest writes each marshalled record to a Unix stream socket, framed
// with a varint length prefix so the daemon reader can recover record
// boundaries. Wire format per record:
//
//	[varint(len(payload))] [payload bytes...]
//
// Daemon-side: read the varint via binary.ReadUvarint, then exactly that
// many payload bytes via io.ReadFull.
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
	if _, err := d.conn.Write(hdr[:hdrLen]); err != nil {
		d.x.pC.WithLabelValues("destUnix", "Write", "error").Inc()
		if d.x.debugLevel > 100 {
			log.Printf("destUnix header Write err:%v", err)
		}
		return 0, err
	}
	written, err := d.conn.Write(*b)
	if err != nil {
		d.x.pC.WithLabelValues("destUnix", "Write", "error").Inc()
		if d.x.debugLevel > 100 {
			log.Printf("destUnix payload Write err:%v", err)
		}
		return 0, err
	}
	d.x.pC.WithLabelValues("destUnix", "Writes", "count").Inc()
	d.x.pC.WithLabelValues("destUnix", "WriteBytes", "count").Add(float64(hdrLen + written))
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
