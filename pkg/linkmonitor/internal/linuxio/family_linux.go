package linuxio

import (
	"context"
	"math"

	"github.com/randomizedcoder/xtcp2/pkg/xtcpnl"
	"golang.org/x/sys/unix"
)

const ethtoolFamilyName = "ethtool"

// Family is an immutable discovery result tied to its client and socket epoch.
// Rediscover after request-socket recovery or an external family change. Group
// IDs are valid only for that discovery; event adapters must resubscribe too.
type Family struct {
	owner  *Client
	epoch  uint64
	id     uint16
	name   string
	groups map[string]uint32
}

// ID returns the dynamically resolved generic family ID.
func (f Family) ID() uint16 { return f.id }

// Group returns a discovered numeric multicast group ID, never a bit mask.
func (f Family) Group(name string) (uint32, bool) {
	id, ok := f.groups[name]
	return id, ok
}

// DiscoverFamily resolves controller data on the current request socket. There
// is no global or cross-epoch cache. A missing family remains a kernel error.
func (c *Client) DiscoverFamily(ctx context.Context, name string) (Family, error) {
	if c.protocol != unix.NETLINK_GENERIC {
		return Family{}, ErrRequest
	}
	result, err := execute(ctx, c, request[xtcpnl.GenericNetlinkFamily]{
		build: func(seq uint32, _ uint64) ([]byte, error) {
			return xtcpnl.BuildGetFamilyRequest(name, seq)
		},
		match: familyMatcher(name), family: xtcpnl.GenlControllerID,
	})
	if err != nil {
		return Family{}, err
	}
	f := result.Values[0]
	return Family{owner: c, epoch: result.Epoch, id: f.ID, name: f.Name, groups: f.MulticastGroups}, nil
}

func familyMatcher(name string) func(xtcpnl.NetlinkEnvelope) (xtcpnl.GenericNetlinkFamily, bool, error) {
	return func(e xtcpnl.NetlinkEnvelope) (xtcpnl.GenericNetlinkFamily, bool, error) {
		match, err := genericCommand(e.Body, 1) // CTRL_CMD_NEWFAMILY
		if err != nil || !match {
			return xtcpnl.GenericNetlinkFamily{}, false, err
		}
		f, err := xtcpnl.ParseGenericNetlinkFamily(e.Body)
		if err != nil || f.Name != name {
			return xtcpnl.GenericNetlinkFamily{}, false, err
		}
		if err := validateGroups(f.MulticastGroups); err != nil {
			return xtcpnl.GenericNetlinkFamily{}, false, err
		}
		return f, true, nil
	}
}

func validateGroups(groups map[string]uint32) error {
	seen := make(map[uint32]bool, len(groups))
	for _, id := range groups {
		if id == 0 || id > math.MaxInt32 || seen[id] {
			return ErrReply
		}
		seen[id] = true
	}
	return nil
}

// GetEthtool performs a read-only single-device GET using a current discovery.
// Unknown/unsupported capabilities retain their kernel errors for the adapter.
func (c *Client) GetEthtool(ctx context.Context, f Family, kind xtcpnl.EthtoolKind, index uint32) (Result[xtcpnl.EthtoolMessage], error) {
	if c.protocol != unix.NETLINK_GENERIC || f.owner != c || f.name != ethtoolFamilyName {
		return Result[xtcpnl.EthtoolMessage]{}, ErrFamily
	}
	command, err := ethtoolReplyCommand(kind)
	if err != nil {
		return Result[xtcpnl.EthtoolMessage]{}, err
	}
	return execute(ctx, c, request[xtcpnl.EthtoolMessage]{
		build: func(seq uint32, epoch uint64) ([]byte, error) {
			if f.epoch != epoch {
				return nil, ErrFamily
			}
			return xtcpnl.BuildGetEthtoolRequest(f.id, kind, index, seq)
		},
		match: ethtoolMatcher(command, index), family: f.id,
	})
}

func ethtoolMatcher(command uint8, index uint32) func(xtcpnl.NetlinkEnvelope) (xtcpnl.EthtoolMessage, bool, error) {
	return func(e xtcpnl.NetlinkEnvelope) (xtcpnl.EthtoolMessage, bool, error) {
		match, err := genericCommand(e.Body, command)
		if err != nil || !match {
			return xtcpnl.EthtoolMessage{}, false, err
		}
		m, err := xtcpnl.ParseEthtool(e.Body, e.Header.Flags)
		if err != nil {
			return xtcpnl.EthtoolMessage{}, false, err
		}
		if m.Header.DeviceIndex == nil {
			return xtcpnl.EthtoolMessage{}, false, ErrReply
		}
		return m, *m.Header.DeviceIndex == index, nil
	}
}

func ethtoolReplyCommand(kind xtcpnl.EthtoolKind) (uint8, error) {
	switch kind {
	case xtcpnl.EthtoolLinkInfoKind:
		return 2, nil
	case xtcpnl.EthtoolLinkModesKind:
		return 4, nil
	case xtcpnl.EthtoolLinkStateKind:
		return 6, nil
	case xtcpnl.EthtoolRingsKind:
		return 16, nil
	case xtcpnl.EthtoolChannelsKind:
		return 18, nil
	case xtcpnl.EthtoolPauseKind:
		return 22, nil
	case xtcpnl.EthtoolFECKind:
		return 30, nil
	default:
		return 0, xtcpnl.ErrUnsupportedEthtool
	}
}
