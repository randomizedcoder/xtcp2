package xtcpnl

import (
	"encoding/binary"
	"errors"

	"github.com/randomizedcoder/xtcp2/pkg/cint"
)

// AF_BRIDGE RTM_NEWLINK: what `bridge link show`, `bridge vlan show`,
// `bridge vlan tunnelshow` and `bridge mst show` receive for a
// PF_BRIDGE RTM_GETLINK dump. The kernel's side is br_getlink and
// br_fill_ifinfo (net/bridge/br_netlink.c): an ifinfomsg, IFLA_IFNAME,
// IFLA_MASTER (always, even for the bridge itself: K1 in
// docs/netlink/gobridge.md), IFLA_MTU and IFLA_OPERSTATE, the port's
// IFLA_PROTINFO nest (the IFLA_BRPORT_* enum, as IFLA_INFO_SLAVE_DATA
// carries it), and an IFLA_AF_SPEC nest holding what the request's
// IFLA_EXT_MASK asked for: the VLAN list (RTEXT_FILTER_BRVLAN, or ranges
// with _COMPRESSED), the VLAN-to-tunnel map, and the MST states
// (RTEXT_FILTER_MST). An empty AF_SPEC nest is canceled, so a port with
// no VLANs, or with MST off, has none.
//
// bridge(8)'s side is bridge/link.c print_linkinfo, bridge/vlan.c
// print_vlan and bridge/mst.c print_msts; the decoder keeps the attributes
// as they read them (AttrTable for the top level and PROTINFO) and the
// AF_SPEC lists in wire order, ranges uncombined, so the clone can make the
// same decisions on the same bytes.

// RTEXT_FILTER_* (rtnetlink.h): the IFLA_EXT_MASK bits bridge sends,
// beside RTEXT_FILTER_VF and RTEXT_FILTER_SKIP_STATS.
const (
	RtextFilterBrvlan           = 1 << 1
	RtextFilterBrvlanCompressed = 1 << 2
	RtextFilterMst              = 1 << 7
)

// IflaMaxCst is the highest IFLA_* bridge's tb[] holds: IFLA_MAX as of the
// pinned headers. A larger type is dropped, as parse_rtattr drops it.
const IflaMaxCst = 70

// IflaBrportMaxCst bounds the PROTINFO table (IFLA_BRPORT_MAX and room).
const IflaBrportMaxCst = 64

// IFLA_BRIDGE_* (if_bridge.h): the attributes inside an AF_BRIDGE
// IFLA_AF_SPEC.
const (
	IflaBridgeFlags          = 0 // u16, BRIDGE_FLAGS_MASTER / _SELF
	IflaBridgeMode           = 1 // u16, BRIDGE_MODE_VEB / _VEPA
	IflaBridgeVlanInfo       = 2 // struct bridge_vlan_info, repeated
	IflaBridgeVlanTunnelInfo = 3 // nest, repeated
	IflaBridgeMrp            = 4
	IflaBridgeCfm            = 5
	IflaBridgeMst            = 6 // nest of IFLA_BRIDGE_MST_ENTRY
)

// IFLA_BRIDGE_VLAN_TUNNEL_* (if_bridge.h).
const (
	IflaBridgeVlanTunnelID    = 1 // u32
	IflaBridgeVlanTunnelVid   = 2 // u16
	IflaBridgeVlanTunnelFlags = 3 // u16, BRIDGE_VLAN_INFO_RANGE_*
	iflaBridgeVlanTunnelMax   = 3
)

// IFLA_BRIDGE_MST_* (if_bridge.h).
const (
	IflaBridgeMstEntry      = 1 // nest
	IflaBridgeMstEntryMsti  = 1 // u16
	IflaBridgeMstEntryState = 2 // u8, BR_STATE_*
	iflaBridgeMstEntryMax   = 2
)

var (
	// ErrBridgeLinkSmall is a body shorter than ifinfomsg.
	ErrBridgeLinkSmall = errors.New("xtcpnl: data too small for an AF_BRIDGE ifinfomsg")
	// ErrBridgeVlanInfoSmall is an IFLA_BRIDGE_VLAN_INFO shorter than
	// struct bridge_vlan_info. bridge casts RTA_DATA to the struct without
	// checking (bug class C3); the decoder refuses it instead.
	ErrBridgeVlanInfoSmall = errors.New("xtcpnl: IFLA_BRIDGE_VLAN_INFO too small for bridge_vlan_info")
)

// BridgeVlanInfo is one IFLA_BRIDGE_VLAN_INFO: a VID and its flags. A
// compressed range is two of them, RANGE_BEGIN then RANGE_END.
type BridgeVlanInfo struct {
	Flags uint16
	Vid   uint16
}

// BridgeVlanTunnel is one IFLA_BRIDGE_VLAN_TUNNEL_INFO nest.
type BridgeVlanTunnel struct {
	Attrs AttrTable // IFLA_BRIDGE_VLAN_TUNNEL_*
}

// BridgeMstEntry is one IFLA_BRIDGE_MST_ENTRY nest.
type BridgeMstEntry struct {
	Attrs AttrTable // IFLA_BRIDGE_MST_ENTRY_*
}

// BridgeLink is a decoded AF_BRIDGE RTM_NEWLINK (or RTM_DELLINK) body.
type BridgeLink struct {
	Family uint8
	Type   uint16 // ifi_type
	Index  int32
	Flags  uint32
	Change uint32

	// Attrs is the top level, IFLA_*.
	Attrs AttrTable

	// ProtInfoNested says IFLA_PROTINFO carried NLA_F_NESTED, the
	// IFLA_BRPORT_* nest; without it the payload is a bare u8 STP state
	// (print_protinfo's else branch). Port is the nest's table.
	ProtInfoNested bool
	Port           AttrTable

	// AFSpec is the IFLA_AF_SPEC table, for IFLA_BRIDGE_MODE and
	// IFLA_BRIDGE_FLAGS; the repeated attributes are below, in wire order.
	AFSpec  AttrTable
	Vlans   []BridgeVlanInfo
	Tunnels []BridgeVlanTunnel
	// HasMst is IFLA_BRIDGE_MST's presence; Mst its entries.
	HasMst bool
	Mst    []BridgeMstEntry
}

// ParseBridgeLink decodes an AF_BRIDGE RTM_NEWLINK body (the bytes after
// the nlmsghdr). A malformed attribute stream at any level is an error.
func ParseBridgeLink(body []byte) (BridgeLink, error) {
	if len(body) < IfInfomsgSizeCst {
		return BridgeLink{}, ErrBridgeLinkSmall
	}
	l := BridgeLink{
		Family: body[0],
		Type:   binary.LittleEndian.Uint16(body[2:4]),
		Index:  cint.Int(binary.LittleEndian.Uint32(body[4:8])),
		Flags:  binary.LittleEndian.Uint32(body[8:12]),
		Change: binary.LittleEndian.Uint32(body[12:16]),
	}
	attrs := body[IfInfomsgSizeCst:]
	var err error
	if l.Attrs, err = ParseAttrTable(attrs, IflaMaxCst); err != nil {
		return BridgeLink{}, err
	}
	// The table masks NLA_F_NESTED off the type, and print_protinfo tests
	// it on the attribute itself, so read IFLA_PROTINFO's raw type.
	if raw, ok := firstRawType(attrs, IflaProtinfoCst); ok && raw&nlaFNestedCst != 0 {
		l.ProtInfoNested = true
		if l.Port, err = ParseAttrTable(l.Attrs.Bytes(IflaProtinfoCst), IflaBrportMaxCst); err != nil {
			return BridgeLink{}, err
		}
	}
	if spec := l.Attrs.Bytes(IflaAfSpecCst); spec != nil {
		if err = l.parseAFSpec(spec); err != nil {
			return BridgeLink{}, err
		}
	}
	return l, nil
}

// IflaProtinfoCst and IflaAfSpecCst are IFLA_PROTINFO and IFLA_AF_SPEC.
const (
	IflaProtinfoCst = 12
	IflaAfSpecCst   = 26
	nlaFNestedCst   = 0x8000
)

func (l *BridgeLink) parseAFSpec(spec []byte) error {
	var err error
	if l.AFSpec, err = ParseAttrTable(spec, IflaBridgeMst); err != nil {
		return err
	}
	var inner error
	keep := func(e error) {
		if e != nil && inner == nil {
			inner = e
		}
	}
	err = WalkRTAttrs(spec, func(atype uint16, val []byte) {
		switch atype {
		case IflaBridgeVlanInfo:
			if len(val) < BridgeVlanInfoSizeCst {
				keep(ErrBridgeVlanInfoSmall)
				return
			}
			l.Vlans = append(l.Vlans, BridgeVlanInfo{
				Flags: binary.LittleEndian.Uint16(val[0:2]),
				Vid:   binary.LittleEndian.Uint16(val[2:4]),
			})
		case IflaBridgeVlanTunnelInfo:
			t, e := ParseAttrTable(val, iflaBridgeVlanTunnelMax)
			keep(e)
			if e == nil {
				l.Tunnels = append(l.Tunnels, BridgeVlanTunnel{Attrs: t})
			}
		case IflaBridgeMst:
			if l.HasMst {
				return // first wins, as aftb[] does
			}
			l.HasMst = true
			keep(WalkRTAttrs(val, func(et uint16, ev []byte) {
				if et != IflaBridgeMstEntry {
					return
				}
				t, e := ParseAttrTable(ev, iflaBridgeMstEntryMax)
				keep(e)
				if e == nil {
					l.Mst = append(l.Mst, BridgeMstEntry{Attrs: t})
				}
			}))
		}
	})
	if err != nil {
		return err
	}
	return inner
}

// firstRawType is the raw type, flags included, of the first top-level
// attribute whose masked type is t. The stream has already been validated.
func firstRawType(data []byte, t uint16) (uint16, bool) {
	for len(data) >= RTAttrSizeCst {
		alen := int(binary.LittleEndian.Uint16(data[0:2]))
		if alen < RTAttrSizeCst || alen > len(data) {
			return 0, false
		}
		raw := binary.LittleEndian.Uint16(data[2:4])
		if raw&NlaTypeMaskCst == t {
			return raw, true
		}
		adv := alen + FourByteAlignPadding(alen)
		if adv > len(data) {
			break
		}
		data = data[adv:]
	}
	return 0, false
}
