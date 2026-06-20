package xtcp

import (
	"context"
	"fmt"
	"log"
	"net"
	"strings"
)

// tcpDest writes each marshaled payload to a connected TCP socket:
// `-dest tcp:host:port`. TCP is the reliable, ordered transport most log/
// metric shippers (Vector, Logstash, Fluentd, `nc`) expect — pair it with a
// line-delimited marshaller (jsonl/csv/tsv). Framing is the marshaller's job;
// tcpDest writes bytes verbatim so it never corrupts a length-delimited
// stream (e.g. protobufList).
//
// Unlike udp, this uses the syscall write path only (no io_uring variant yet;
// it can follow the udp pattern later). Send is invoked serially.
type tcpDest struct {
	x    *XTCP
	conn net.Conn
}

func newTCPDest(ctx context.Context, x *XTCP) (Destination, error) {
	addr := strings.TrimPrefix(x.config.Dest, schemeTCP+":")
	var dialer net.Dialer
	conn, err := dialer.DialContext(ctx, "tcp", addr)
	if err != nil {
		return nil, fmt.Errorf("InitDestTCP net.Dial(tcp, %q): %w", addr, err)
	}
	return &tcpDest{x: x, conn: conn}, nil
}

func (d *tcpDest) Send(_ context.Context, b *[]byte) (int, error) {
	n, err := d.conn.Write(*b)
	if err != nil {
		d.x.pC.WithLabelValues("destTCP", "Write", "error").Inc()
		if d.x.debugLevel > 100 {
			log.Printf("destTCP conn.Write err:%v", err)
		}
		return 0, err
	}
	d.x.pC.WithLabelValues("destTCP", "Writes", "count").Inc()
	d.x.pC.WithLabelValues("destTCP", "WriteBytes", "count").Add(float64(n))
	return 1, nil
}

func (d *tcpDest) Close() error {
	if d.conn != nil {
		return d.conn.Close()
	}
	return nil
}

func init() {
	RegisterDestination(schemeTCP, newTCPDest)
}
