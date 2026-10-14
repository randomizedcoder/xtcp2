package linuxio

import (
	"context"
	"errors"
	"strings"

	"github.com/randomizedcoder/xtcp2/pkg/xtcpnl"
	"golang.org/x/sys/unix"
)

// RDMAPort holds owned NLDEV evidence. Device GET uses Port as the advertised
// port count; PORT_GET uses it as the actual port number. Index zero is valid.
// The nldev constants, request layouts and value decoders are pkg/xtcpnl's
// (xtcpnl_rdma*.go), whose constants testdata/rdma_consts.h asserts against
// the flake-pinned Linux header. What is linkmonitor's own is the admission
// below: it is stricter than the kernel's policy and than rdma's.
type RDMAPort struct {
	NetdevName            string
	Netdev                uint32
	HasNetdev             bool
	Name                  string
	Index, Port           uint32
	State, Physical       uint8
	HasState, HasPhysical bool
}

var (
	rdmaDeviceGet = xtcpnl.RdmaNlGetType(xtcpnl.RdmaNlNldev, xtcpnl.RdmaNldevCmdGet)
	rdmaPortGet   = xtcpnl.RdmaNlGetType(xtcpnl.RdmaNlNldev, xtcpnl.RdmaNldevCmdPortGet)
)

const (
	// rdmaNameMax is IB_DEVICE_NAME_MAX, the terminator included.
	rdmaNameMax = 64
	// rdmaNetdevNameMax is IFNAMSIZ, the terminator included.
	rdmaNetdevNameMax = unix.IFNAMSIZ
	// rdmaNetdevIndexMax is the largest ifindex; dev_index_reserve allocates
	// from 1 to INT_MAX.
	rdmaNetdevIndexMax = 0x7fffffff
)

// DumpRDMADevices enumerates local RDMA devices on a dedicated RDMA socket.
func (c *Client) DumpRDMADevices(ctx context.Context) (Result[RDMAPort], error) {
	return c.rdma(ctx, rdmaDeviceGet, 0, 0, true)
}

// DumpRDMAPorts enumerates a device's ports without trusting its port count.
func (c *Client) DumpRDMAPorts(ctx context.Context, index uint32) (Result[RDMAPort], error) {
	return c.rdma(ctx, rdmaPortGet, index, 0, true)
}

// GetRDMAPort queries a single local host port, never a resource or fabric dump.
func (c *Client) GetRDMAPort(ctx context.Context, index, port uint32) (Result[RDMAPort], error) {
	if port == 0 {
		return Result[RDMAPort]{}, ErrRequest
	}
	return c.rdma(ctx, rdmaPortGet, index, port, false)
}

func (c *Client) rdma(ctx context.Context, command uint16, index, port uint32, dump bool) (Result[RDMAPort], error) {
	if c.protocol != unix.NETLINK_RDMA {
		return Result[RDMAPort]{}, ErrRequest
	}
	var alias uint16
	if command == rdmaPortGet && !dump {
		// nldev_port_get_doit replies with RDMA_NLDEV_CMD_GET, not PORT_GET;
		// only the dump uses PORT_GET. Accept both should the kernel change.
		alias = rdmaDeviceGet
	}
	decode := decodeRDMAPort
	if command == rdmaDeviceGet {
		decode = decodeRDMADevice
	}
	return execute(ctx, c, request[RDMAPort]{family: command, alias: alias, dump: dump,
		build: func(sequence uint32, _ uint64) ([]byte, error) {
			return rdmaRequest(command, index, port, sequence, dump)
		}, match: func(e xtcpnl.NetlinkEnvelope) (RDMAPort, bool, error) {
			p, err := decode(e.Body)
			if err == nil && command == rdmaPortGet && (p.Index != index || (!dump && p.Port != port)) {
				err = ErrReply
			}
			return p, err == nil, err
		}})
}

// rdmaRequest is a doit, of a single port, of a port's counters (STAT_GET) or
// of SYS_GET, or a dump: of every device, or of one device's ports, which
// nldev_port_get_dumpit selects by DEV_INDEX.
//
// None asks for an ACK, unlike rdma's (xtcpnl.BuildRdmaPortGetRequest):
// the bytes are the ones the recordings in testdata/nlrecord pin, from hosts
// that cannot be re-recorded here, and a doit's reply already completes the
// transaction.
func rdmaRequest(command uint16, index, port, sequence uint32, dump bool) ([]byte, error) {
	var raw [16]byte
	ab := xtcpnl.NewAttrBuilder(raw[:])
	op, flags := xtcpnl.RdmaNlGetOp(command), uint16(0)
	if dump {
		flags = unix.NLM_F_DUMP
	}
	if op == xtcpnl.RdmaNldevCmdPortGet || op == xtcpnl.RdmaNldevCmdStatGet {
		if err := ab.PutU32(xtcpnl.RdmaNldevAttrDevIndex, index); err != nil {
			return nil, err
		}
	}
	if port != 0 {
		if err := ab.PutU32(xtcpnl.RdmaNldevAttrPortIndex, port); err != nil {
			return nil, err
		}
	}
	return xtcpnl.BuildRdmaRequest(op, flags, sequence, ab.Bytes())
}

// rdmaPortAttrs are the attributes a device or port reply is admitted on.
var rdmaPortAttrs = []uint16{xtcpnl.RdmaNldevAttrDevIndex, xtcpnl.RdmaNldevAttrDevName,
	xtcpnl.RdmaNldevAttrPortIndex, xtcpnl.RdmaNldevAttrPortState, xtcpnl.RdmaNldevAttrPortPhysState,
	xtcpnl.RdmaNldevAttrNdevIndex, xtcpnl.RdmaNldevAttrNdevName}

// admitRDMAAttrs is the strict pass ahead of xtcpnl's decode, over the
// attributes in allowed: each at most once, even with an equal value, and
// with no flag bits in its type. The walk rejects a partial trailer. xtcpnl
// checks the widths, of these and of every other attribute it knows; a type
// outside allowed is otherwise ignored. seen is indexed by attribute type.
func admitRDMAAttrs(body []byte, allowed []uint16, seen []bool) error {
	return admitRDMANested(body, allowed, 0, seen)
}

// admitRDMANested is admitRDMAAttrs that also accepts NLA_F_NESTED on the
// one attribute nested, which nla_nest_start sets; type zero is none.
func admitRDMANested(body []byte, allowed []uint16, nested uint16, seen []bool) error {
	attrs, err := xtcpnl.ParseNetlinkAttributes(body)
	if err != nil {
		return errors.Join(ErrReply, err)
	}
	for _, a := range attrs {
		id := a.ID()
		if !rdmaAllowed(allowed, id) {
			continue
		}
		if seen[id] || (a.Type != id && (nested == 0 || id != nested || a.Type != id|unix.NLA_F_NESTED)) {
			return ErrReply
		}
		seen[id] = true
	}
	return nil
}

func rdmaAllowed(allowed []uint16, id uint16) bool {
	for _, a := range allowed {
		if a == id {
			return true
		}
	}
	return false
}

// decodeRDMAPort decodes a PORT_GET reply, the doit's or the dump's.
func decodeRDMAPort(body []byte) (RDMAPort, error) {
	var seen [xtcpnl.RdmaNldevAttrNdevName + 1]bool
	if err := admitRDMAAttrs(body, rdmaPortAttrs, seen[:]); err != nil {
		return RDMAPort{}, err
	}
	p, err := xtcpnl.ParseRdmaPort(body)
	if err != nil {
		return RDMAPort{}, errors.Join(ErrReply, err)
	}
	return admitRDMAPort(p.RdmaHandle, p.RdmaNetdev, seen[:], RDMAPort{
		State: p.State, HasState: p.HasState, Physical: p.PhysState, HasPhysical: p.HasPhysState})
}

// decodeRDMADevice decodes a device GET reply, whose PORT_INDEX is the
// number of ports.
func decodeRDMADevice(body []byte) (RDMAPort, error) {
	var seen [xtcpnl.RdmaNldevAttrNdevName + 1]bool
	if err := admitRDMAAttrs(body, rdmaPortAttrs, seen[:]); err != nil {
		return RDMAPort{}, err
	}
	d, err := xtcpnl.ParseRdmaDev(body)
	if err != nil {
		return RDMAPort{}, errors.Join(ErrReply, err)
	}
	h := xtcpnl.RdmaHandle{DevIndex: d.DevIndex, HasDevIndex: d.HasDevIndex, DevName: d.DevName,
		HasDevName: d.HasDevName, PortIndex: d.NumPorts, HasPortIndex: d.HasNumPorts}
	return admitRDMAPort(h, xtcpnl.RdmaNetdev{}, seen[:], RDMAPort{})
}

// admitRDMAPort requires the device index, name and port, and the netdev
// index and name together or not at all, then checks their values.
func admitRDMAPort(h xtcpnl.RdmaHandle, n xtcpnl.RdmaNetdev, seen []bool, p RDMAPort) (RDMAPort, error) {
	if !h.HasDevIndex || !h.HasDevName || !h.HasPortIndex ||
		seen[xtcpnl.RdmaNldevAttrNdevIndex] != seen[xtcpnl.RdmaNldevAttrNdevName] {
		return RDMAPort{}, ErrReply
	}
	if err := admitRDMAIdentity(h, n); err != nil {
		return RDMAPort{}, err
	}
	p.Name, p.Index, p.Port = h.DevName, h.DevIndex, h.PortIndex
	p.Netdev, p.HasNetdev, p.NetdevName = n.NdevIndex, n.HasNdevIndex, n.NdevName
	return p, nil
}

// admitRDMAIdentity checks the identifiers that are present: device and
// netdev names that fit their kernel buffers and could be a path component,
// and a netdev index the kernel could allocate.
func admitRDMAIdentity(h xtcpnl.RdmaHandle, n xtcpnl.RdmaNetdev) error {
	if h.HasDevName && !rdmaName(h.DevName, rdmaNameMax) {
		return ErrReply
	}
	if n.HasNdevName && !rdmaName(n.NdevName, rdmaNetdevNameMax) {
		return ErrReply
	}
	if n.HasNdevIndex && (n.NdevIndex == 0 || n.NdevIndex > rdmaNetdevIndexMax) {
		return ErrReply
	}
	return nil
}

// rdmaName reports whether name, which xtcpnl has stripped of its
// terminator, fits max bytes with it and has no NUL, '/' or ':'.
func rdmaName(name string, limit int) bool {
	return name != "" && len(name) < limit && !strings.ContainsAny(name, "\x00/:")
}
