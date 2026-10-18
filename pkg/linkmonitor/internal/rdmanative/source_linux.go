package rdmanative

import (
	"context"
	"errors"
	"sync/atomic"

	"github.com/randomizedcoder/xtcp2/pkg/linkmonitor/internal/rdmaevents"
	"github.com/randomizedcoder/xtcp2/pkg/nlconn"
	"github.com/randomizedcoder/xtcp2/pkg/xtcpnl"
	"golang.org/x/sys/unix"
)

// routeConn is the subscribed socket; nlconn.Conn in production.
type routeConn interface {
	Receive(context.Context, func([]byte) error) (int, error)
	Close() error
}

// Source is the netlink-derived RDMA event source: one NETLINK_ROUTE socket
// joined to the link and address groups, translated by a Translator over a
// fixed PortMap. It is an rdmaevents.Source, so the monitor's event worker
// and schedule run it exactly as they run the verbs source.
type Source struct {
	conn      routeConn
	tr        *Translator
	malformed atomic.Uint64
}

var errStop = errors.New("rdmanative: emit refused an event")

// Open subscribes before returning, so every link and address change after
// it is reported. The translator starts with every port's state unknown, so
// the first link message per port is reported rather than compared with a
// dump: at most one extra Refresh per port, and no dump to order against the
// notifications.
func Open(ctx context.Context, m *PortMap) (*Source, error) {
	if m == nil {
		return nil, ErrPortMap
	}
	conn, err := nlconn.Open(ctx, unix.NETLINK_ROUTE, []uint32{rtnlGroupLink, rtnlGroupIPv4Addr, rtnlGroupIPv6Addr})
	if err != nil {
		return nil, err
	}
	return newSource(conn, m), nil
}

func newSource(conn routeConn, m *PortMap) *Source {
	return &Source{conn: conn, tr: NewTranslator(m)}
}

// Malformed counts notifications skipped because their header was short or
// their datagram did not walk. The source keeps running past them.
func (s *Source) Malformed() uint64 { return s.malformed.Load() }

// Run reads until ctx ends, emit refuses an event, or the socket fails.
// ENOBUFS, where the kernel dropped notifications, a datagram truncated by
// the receive buffer, and a refused event are all rdmaevents.ErrLost, which the worker's loss path resyncs from, as it
// does for the verbs source.
func (s *Source) Run(ctx context.Context, emit func(rdmaevents.Event) bool) error {
	var batch [MaxPorts]rdmaevents.Event
	visit := func(e xtcpnl.NetlinkEnvelope) error {
		if e.Control != xtcpnl.NetlinkData || !xtcpnl.IsRtnetlinkNotification(e.Header) {
			return nil
		}
		n, err := s.tr.Translate(e.Header.Type, e.Body, &batch)
		if err != nil {
			s.malformed.Add(1)
			return nil
		}
		for _, event := range batch[:n] {
			if !emit(event) {
				return errStop
			}
		}
		return nil
	}
	// Built once, not per Receive: a func literal passed to an interface
	// method escapes, so one inside the loop would allocate each batch.
	receive := func(data []byte) error {
		err := xtcpnl.WalkNetlinkEnvelopes(data, visit)
		if err != nil && !errors.Is(err, errStop) {
			s.malformed.Add(1)
			return nil
		}
		return err
	}
	for {
		_, err := s.conn.Receive(ctx, receive)
		switch {
		case err == nil:
		case ctx.Err() != nil:
			return ctx.Err()
		case errors.Is(err, errStop), errors.Is(err, unix.ENOBUFS), errors.Is(err, nlconn.ErrTruncated):
			return rdmaevents.ErrLost
		default:
			return err
		}
	}
}

// Close wakes a pending Run and releases the socket.
func (s *Source) Close() error { return s.conn.Close() }
