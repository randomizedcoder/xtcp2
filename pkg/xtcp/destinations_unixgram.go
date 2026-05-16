package xtcp

import (
	"context"
	"fmt"
	"log"
	"net"
	"os"
	"strings"

	xio "github.com/randomizedcoder/xtcp2/pkg/io_uring"
)

// unixgramDest writes each marshaled record to a Unix datagram socket.
// One Write == one datagram == one record; no framing is required because
// the kernel preserves message boundaries. Records exceeding SO_SNDBUF
// (~208 KB on Linux by default) fail with EMSGSIZE; xtcp records today
// are well below that.
type unixgramDest struct {
	x    *XTCP
	conn net.Conn
	fd   int
}

func newUnixGramDest(ctx context.Context, x *XTCP) (Destination, error) {
	path := strings.TrimPrefix(x.config.Dest, "unixgram:")
	if x.debugLevel > 10 {
		log.Printf("InitDestUnixGram config.Dest:%s path:%s", x.config.Dest, path)
	}
	// Dialing unixgram does not verify the peer exists, so pre-check via
	// os.Stat to preserve the "fail loudly at startup" contract.
	if _, err := os.Stat(path); err != nil {
		return nil, fmt.Errorf("InitDestUnixGram socket %q does not exist: %w", path, err)
	}
	var dialer net.Dialer
	conn, err := dialer.DialContext(ctx, "unixgram", path)
	if err != nil {
		return nil, fmt.Errorf("InitDestUnixGram net.Dial(unixgram, %q): %w", path, err)
	}
	d := &unixgramDest{x: x, conn: conn}
	if x.config.IoUring {
		var fd int
		fd, err = extractFD(conn)
		if err != nil {
			return nil, fmt.Errorf("InitDestUnixGram extractFD: %w", err)
		}
		d.fd = fd
	}
	return d, nil
}

func (d *unixgramDest) Send(ctx context.Context, b *[]byte) (int, error) {
	if d.x.config.IoUring {
		ring := ringFromContext(ctx)
		if ring == nil {
			d.x.pC.WithLabelValues("destUnixGramIoUring", "noRing", "error").Inc()
			return 0, errNoRingInCtx
		}
		if _, err := ring.EnqueueSend(d.fd, b, xio.OpSendUnixGram); err != nil {
			d.x.pC.WithLabelValues("destUnixGramIoUring", "EnqueueSend", "error").Inc()
			if d.x.debugLevel > 100 {
				log.Printf("destUnixGramIoUring EnqueueSend err:%v", err)
			}
			return 0, err
		}
		return 1, nil
	}

	written, err := d.conn.Write(*b)
	if err != nil {
		d.x.pC.WithLabelValues("destUnixGram", "Write", "error").Inc()
		if d.x.debugLevel > 100 {
			log.Printf("destUnixGram Write err:%v", err)
		}
		return 0, err
	}
	d.x.pC.WithLabelValues("destUnixGram", "Writes", "count").Inc()
	d.x.pC.WithLabelValues("destUnixGram", "WriteBytes", "count").Add(float64(written))
	return 1, nil
}

func (d *unixgramDest) Close() error {
	if d.conn != nil {
		return d.conn.Close()
	}
	return nil
}

func init() {
	RegisterDestination("unixgram", newUnixGramDest)
}
