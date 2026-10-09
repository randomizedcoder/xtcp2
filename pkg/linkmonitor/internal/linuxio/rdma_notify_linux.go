package linuxio

import (
	"context"
	"errors"

	"github.com/randomizedcoder/xtcp2/pkg/linkmonitor/internal/netlink"
	"github.com/randomizedcoder/xtcp2/pkg/xtcpnl"
	"golang.org/x/sys/unix"
)

const rdmaMonitor uint16 = 5<<10 | 28

// Checked against the flake-pinned Linux header by the RDMA runtime gate.
const (
	rdmaEventType   = 102
	rdmaMonitorMode = 103
)

// RDMANotifications owns a kernel-authenticated NLDEV lifecycle subscription.
type RDMANotifications struct{ conn *netlink.Conn }

// OpenRDMANotifications joins before probing the read-only system monitor mode.
// Joining a group alone does not prove that the kernel emits notifications.
func OpenRDMANotifications(ctx context.Context) (*RDMANotifications, error) {
	conn, err := netlink.Open(ctx, unix.NETLINK_RDMA, []uint32{4})
	if err != nil {
		return nil, err
	}
	client, err := NewClient(unix.NETLINK_RDMA)
	if err != nil {
		return nil, errors.Join(err, conn.Close())
	}
	err = errors.Join(client.monitorMode(ctx), client.Close())
	if err != nil {
		return nil, errors.Join(err, conn.Close())
	}
	return &RDMANotifications{conn: conn}, nil
}

func (c *Client) monitorMode(ctx context.Context) error {
	const command = 5<<10 | 6
	result, err := execute(ctx, c, request[bool]{family: command,
		build: func(seq uint32, _ uint64) ([]byte, error) { return rdmaRequest(command, 0, 0, seq, false), nil },
		match: func(e xtcpnl.NetlinkEnvelope) (bool, bool, error) {
			mode, err := decodeMonitorMode(e.Body)
			return mode, true, err
		}})
	if err != nil {
		return err
	}
	if len(result.Values) != 1 || !result.Values[0] {
		return unix.EOPNOTSUPP
	}
	return nil
}

func decodeMonitorMode(body []byte) (bool, error) {
	attrs, err := xtcpnl.ParseNetlinkAttributes(body)
	if err != nil {
		return false, errors.Join(ErrReply, err)
	}
	seen, enabled := false, false
	for _, a := range attrs {
		if a.ID() != rdmaMonitorMode {
			continue
		}
		if seen || a.Type != rdmaMonitorMode || len(a.Data) != 1 || a.Data[0] > 1 {
			return false, ErrReply
		}
		seen, enabled = true, a.Data[0] == 1
	}
	return enabled, nil
}

// Run delivers invalidation hints, never authoritative link observations.
func (n *RDMANotifications) Run(ctx context.Context, emit func() bool) error {
	for {
		_, err := n.conn.Receive(ctx, func(data []byte) error {
			return DecodeRDMANotifications(data, emit)
		})
		if err != nil {
			return err
		}
	}
}

// Close wakes pending I/O and releases the subscription once.
func (n *RDMANotifications) Close() error { return n.conn.Close() }

// DecodeRDMANotifications validates each envelope and consumed attribute before
// emitting a bounded topology hint. Unknown event values also invalidate.
func DecodeRDMANotifications(data []byte, emit func() bool) error {
	return xtcpnl.WalkNetlinkEnvelopes(data, func(e xtcpnl.NetlinkEnvelope) error {
		if e.Control != xtcpnl.NetlinkData || e.Header.Type != rdmaMonitor || e.Header.Seq != 0 || e.Header.Pid != 0 || e.Header.Flags != 0 {
			return ErrReply
		}
		if err := decodeNotification(e.Body); err != nil {
			return err
		}
		if !emit() {
			return unix.ENOBUFS
		}
		return nil
	})
}

func decodeNotification(body []byte) error {
	attrs, err := xtcpnl.ParseNetlinkAttributes(body)
	if err != nil {
		return errors.Join(ErrReply, err)
	}
	var seen [rdmaEventType + 1]bool
	var p RDMAPort
	for _, a := range attrs {
		id := a.ID()
		if id != 1 && id != 2 && id != 3 && id != 50 && id != 51 && id != rdmaEventType {
			continue
		}
		if seen[id] || id != a.Type {
			return ErrReply
		}
		seen[id] = true
		if id == rdmaEventType {
			if len(a.Data) != 1 {
				return ErrReply
			}
		} else if err := rdmaAttribute(&p, id, a.Data); err != nil {
			return err
		}
	}
	if !seen[1] || !seen[2] || !seen[rdmaEventType] {
		return ErrReply
	}
	return nil
}
