package linuxio

import (
	"context"
	"errors"

	"github.com/randomizedcoder/xtcp2/pkg/nlconn"
	"github.com/randomizedcoder/xtcp2/pkg/xtcpnl"
	"golang.org/x/sys/unix"
)

var (
	rdmaMonitor = xtcpnl.RdmaNlGetType(xtcpnl.RdmaNlNldev, xtcpnl.RdmaNldevCmdMonitor)
	rdmaSysGet  = xtcpnl.RdmaNlGetType(xtcpnl.RdmaNlNldev, xtcpnl.RdmaNldevCmdSysGet)
)

const (
	rdmaEventType   = xtcpnl.RdmaNldevAttrEventType
	rdmaMonitorMode = xtcpnl.RdmaNldevSysAttrMonitorMode
)

// RDMANotifications owns a kernel-authenticated NLDEV lifecycle subscription.
type RDMANotifications struct{ conn transport }

// OpenRDMANotifications joins before probing the read-only system monitor mode.
// Joining a group alone does not prove that the kernel emits notifications.
func OpenRDMANotifications(ctx context.Context) (*RDMANotifications, error) {
	conn, err := nlconn.Open(ctx, unix.NETLINK_RDMA, []uint32{xtcpnl.RdmaNlGroupNotify})
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
	return &RDMANotifications{conn: recordTransport(unix.NETLINK_RDMA, conn)}, nil
}

func (c *Client) monitorMode(ctx context.Context) error {
	result, err := execute(ctx, c, request[bool]{family: rdmaSysGet,
		build: func(seq uint32, _ uint64) ([]byte, error) { return rdmaRequest(rdmaSysGet, 0, 0, seq, false) },
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

// decodeMonitorMode reads MONITOR_MODE, which a kernel before it existed
// does not send. The mode is a boolean; any other value is malformed.
func decodeMonitorMode(body []byte) (bool, error) {
	var seen [rdmaMonitorMode + 1]bool
	if err := admitRDMAAttrs(body, []uint16{rdmaMonitorMode}, seen[:]); err != nil {
		return false, err
	}
	s, err := xtcpnl.ParseRdmaSys(body)
	if err != nil {
		return false, errors.Join(ErrReply, err)
	}
	if s.MonitorMode > 1 {
		return false, ErrReply
	}
	return s.MonitorMode == 1, nil
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

// RunInto is Run reporting what each notification is.
func (n *RDMANotifications) RunInto(ctx context.Context, emit func(RDMANotification) bool) error {
	for {
		_, err := n.conn.Receive(ctx, func(data []byte) error {
			return DecodeRDMANotificationsInto(data, emit)
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
	return DecodeRDMANotificationsInto(data, func(RDMANotification) bool { return emit() })
}

// RDMANotifyKind is an NLDEV monitor event's type. The zero value is an event
// type this package does not know, which still invalidates.
type RDMANotifyKind uint8

// The kinds, one per enum rdma_nl_notify_event_type value.
const (
	RDMANotifyUnknown RDMANotifyKind = iota
	RDMANotifyRegister
	RDMANotifyUnregister
	RDMANotifyNetdevAttach
	RDMANotifyNetdevDetach
	RDMANotifyRename
	RDMANotifyNetdevRename
)

// rdmaNotifyKinds maps enum rdma_nl_notify_event_type to RDMANotifyKind.
var rdmaNotifyKinds = [...]RDMANotifyKind{
	xtcpnl.RdmaRegisterEvent:     RDMANotifyRegister,
	xtcpnl.RdmaUnregisterEvent:   RDMANotifyUnregister,
	xtcpnl.RdmaNetdevAttachEvent: RDMANotifyNetdevAttach,
	xtcpnl.RdmaNetdevDetachEvent: RDMANotifyNetdevDetach,
	xtcpnl.RdmaRenameEvent:       RDMANotifyRename,
	xtcpnl.RdmaNetdevRenameEvent: RDMANotifyNetdevRename,
}

func rdmaNotifyKind(event uint8) RDMANotifyKind {
	if int(event) < len(rdmaNotifyKinds) {
		return rdmaNotifyKinds[event]
	}
	return RDMANotifyUnknown
}

// RDMANotification is one validated NLDEV monitor event. The identifiers are
// the ones rdma_nl_notify_event sent, which depend on the kind and on whether
// the netdev is in this namespace, so each has a presence flag.
type RDMANotification struct {
	Kind           RDMANotifyKind
	EventType      uint8
	DevIndex       uint32
	Port           uint32
	NetdevIndex    uint32
	HasDevIndex    bool
	HasPort        bool
	HasNetdevIndex bool
}

// Fatal reports whether n unregisters a device the caller holds, which ends
// that device's event stream. Every other notification, an unregister of a
// device not held included, is a topology hint.
func (n RDMANotification) Fatal(held func(devIndex uint32) bool) bool {
	return n.Kind == RDMANotifyUnregister && n.HasDevIndex && held(n.DevIndex)
}

// DecodeRDMANotificationsInto is DecodeRDMANotifications reporting what each
// notification is. Admission is identical: a notification is emitted only
// after its envelope and every consumed attribute are valid, and an unknown
// event type is emitted as RDMANotifyUnknown rather than dropped.
func DecodeRDMANotificationsInto(data []byte, emit func(RDMANotification) bool) error {
	return xtcpnl.WalkNetlinkEnvelopes(data, func(e xtcpnl.NetlinkEnvelope) error {
		if e.Control != xtcpnl.NetlinkData || e.Header.Type != rdmaMonitor || e.Header.Seq != 0 || e.Header.Pid != 0 || e.Header.Flags != 0 {
			return ErrReply
		}
		n, err := decodeNotification(e.Body)
		if err != nil {
			return err
		}
		if !emit(n) {
			return unix.ENOBUFS
		}
		return nil
	})
}

// rdmaEventAttrs are the attributes an event is admitted on.
var rdmaEventAttrs = []uint16{xtcpnl.RdmaNldevAttrDevIndex, xtcpnl.RdmaNldevAttrDevName,
	xtcpnl.RdmaNldevAttrPortIndex, xtcpnl.RdmaNldevAttrNdevIndex, xtcpnl.RdmaNldevAttrNdevName, rdmaEventType}

// decodeNotification requires only EVENT_TYPE. The kernel's
// rdma_nl_notify_event omits the device handle for NETDEV_RENAME (netdev
// index and name only) and sends EVENT_TYPE alone when the netdev is in
// another namespace, so identifiers are validated when present, never required.
func decodeNotification(body []byte) (RDMANotification, error) {
	var seen [rdmaEventType + 1]bool
	if err := admitRDMAAttrs(body, rdmaEventAttrs, seen[:]); err != nil {
		return RDMANotification{}, err
	}
	e, err := xtcpnl.ParseRdmaEvent(body)
	if err != nil {
		return RDMANotification{}, errors.Join(ErrReply, err)
	}
	if !e.HasEventType {
		return RDMANotification{}, ErrReply
	}
	if err := admitRDMAIdentity(e.RdmaHandle, e.RdmaNetdev); err != nil {
		return RDMANotification{}, err
	}
	return RDMANotification{Kind: rdmaNotifyKind(e.EventType), EventType: e.EventType,
		DevIndex: e.DevIndex, HasDevIndex: e.HasDevIndex, Port: e.PortIndex, HasPort: e.HasPortIndex,
		NetdevIndex: e.NdevIndex, HasNetdevIndex: e.HasNdevIndex}, nil
}
