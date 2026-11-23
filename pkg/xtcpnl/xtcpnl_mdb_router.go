package xtcpnl

import (
	"encoding/binary"
	"errors"
)

// The multicast router ports a bridge reports: the MDBA_ROUTER nest of an
// RTM_GETMDB dump, and the same nest inside a VLAN's global options
// (BRIDGE_VLANDB_GOPTS_MCAST_ROUTER_PORTS, RTM_GETVLAN with
// BRIDGE_VLANDB_DUMPF_GLOBAL). Both are filled by br_rports_fill_info
// (net/bridge/br_mdb.c):
//
//	[MDBA_ROUTER] {
//	    [MDBA_ROUTER_PORT] {
//	        u32 ifindex                       (nla_put_nohdr: no header)
//	        [MDBA_ROUTER_PATTR_TIMER]  u32    max of the two timers, USER_HZ
//	        [MDBA_ROUTER_PATTR_TYPE]   u8     the port-wide type (K3)
//	        [MDBA_ROUTER_PATTR_INET_TIMER]  u32   when an IPv4 router
//	        [MDBA_ROUTER_PATTR_INET6_TIMER] u32   when an IPv6 router
//	        [MDBA_ROUTER_PATTR_VID]    u16    per-VLAN contexts only
//	    } ...
//	}
//
// The kernel takes TYPE from the port's own context
// (p->multicast_ctx.multicast_router) even when it lists a VLAN's router
// ports, so for a VLAN it is the port-wide setting, not the VLAN's (K3 in
// gobridge.md). Reference: include/uapi/linux/if_bridge.h.

const (
	MdbaRouter     uint16 = 2 // MDBA_ROUTER
	MdbaRouterPort uint16 = 1 // MDBA_ROUTER_PORT

	MdbaRouterPattrTimer      uint16 = 1 // MDBA_ROUTER_PATTR_TIMER
	MdbaRouterPattrType       uint16 = 2 // MDBA_ROUTER_PATTR_TYPE
	MdbaRouterPattrInetTimer  uint16 = 3 // MDBA_ROUTER_PATTR_INET_TIMER
	MdbaRouterPattrInet6Timer uint16 = 4 // MDBA_ROUTER_PATTR_INET6_TIMER
	MdbaRouterPattrVid        uint16 = 5 // MDBA_ROUTER_PATTR_VID
	MdbaRouterPattrMax        uint16 = 5 // MDBA_ROUTER_PATTR_MAX

	// The router types (enum MDB_RTR_TYPE_*).
	MdbRtrTypeDisabled  uint8 = 0
	MdbRtrTypeTempQuery uint8 = 1
	MdbRtrTypePerm      uint8 = 2
	MdbRtrTypeTemp      uint8 = 3
)

var (
	// ErrMdbRouterPortSmall is an MDBA_ROUTER_PORT too short for its
	// ifindex.
	ErrMdbRouterPortSmall = errors.New("xtcpnl: MDBA_ROUTER_PORT shorter than its ifindex")
	// ErrMdbRouterPattrWidth is a router port attribute of the wrong
	// width.
	ErrMdbRouterPattrWidth = errors.New("xtcpnl: MDBA_ROUTER_PATTR attribute of the wrong width")
)

// MdbRouterPort is one MDBA_ROUTER_PORT. Each Has* says whether its
// attribute was sent; the first of a repeated attribute wins, as
// parse_rtattr keeps it.
type MdbRouterPort struct {
	Ifindex       uint32
	Timer         uint32 // USER_HZ ticks
	Type          uint8
	InetTimer     uint32
	Inet6Timer    uint32
	Vid           uint16
	HasTimer      bool
	HasType       bool
	HasInetTimer  bool
	HasInet6Timer bool
	HasVid        bool
}

// IsTemp is mdb.c's is_temp_mcast_rtr: temp-query and temp are temporary;
// anything else, an unknown type included, prints as permanent.
func (p *MdbRouterPort) IsTemp() bool {
	return p.Type == MdbRtrTypeTempQuery || p.Type == MdbRtrTypeTemp
}

// ParseMdbRouterPorts decodes an MDBA_ROUTER payload: its MDBA_ROUTER_PORT
// entries in wire order. Other attribute types in the nest and unknown
// router port attributes are skipped. A port shorter than its ifindex, an
// attribute of the wrong width, or a malformed stream at either level is
// an error.
func ParseMdbRouterPorts(nest []byte) ([]MdbRouterPort, error) {
	var out []MdbRouterPort
	var perr error
	err := WalkRawAttrs(nest, func(raw uint16, val []byte) {
		if perr != nil || raw&NlaTypeMaskCst != MdbaRouterPort {
			return
		}
		p, e := parseMdbRouterPort(val)
		if e != nil {
			perr = e
			return
		}
		out = append(out, p)
	})
	if err = errors.Join(err, perr); err != nil {
		return nil, err
	}
	return out, nil
}

func parseMdbRouterPort(val []byte) (MdbRouterPort, error) {
	if len(val) < 4 {
		return MdbRouterPort{}, ErrMdbRouterPortSmall
	}
	p := MdbRouterPort{Ifindex: binary.LittleEndian.Uint32(val[0:4])}
	t, err := ParseAttrTable(val[4:], MdbaRouterPattrMax)
	if err != nil {
		return MdbRouterPort{}, err
	}
	u32 := func(id uint16, v *uint32, has *bool) error {
		b := t.Bytes(id)
		if b == nil {
			return nil
		}
		if len(b) != 4 {
			return ErrMdbRouterPattrWidth
		}
		*v, *has = binary.LittleEndian.Uint32(b), true
		return nil
	}
	if err := errors.Join(
		u32(MdbaRouterPattrTimer, &p.Timer, &p.HasTimer),
		u32(MdbaRouterPattrInetTimer, &p.InetTimer, &p.HasInetTimer),
		u32(MdbaRouterPattrInet6Timer, &p.Inet6Timer, &p.HasInet6Timer),
	); err != nil {
		return MdbRouterPort{}, err
	}
	if b := t.Bytes(MdbaRouterPattrType); b != nil {
		if len(b) != 1 {
			return MdbRouterPort{}, ErrMdbRouterPattrWidth
		}
		p.Type, p.HasType = b[0], true
	}
	if b := t.Bytes(MdbaRouterPattrVid); b != nil {
		if len(b) != 2 {
			return MdbRouterPort{}, ErrMdbRouterPattrWidth
		}
		p.Vid, p.HasVid = binary.LittleEndian.Uint16(b), true
	}
	return p, nil
}
