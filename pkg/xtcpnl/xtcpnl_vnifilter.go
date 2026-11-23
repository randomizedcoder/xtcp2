package xtcpnl

import (
	"encoding/binary"
	"errors"

	"golang.org/x/sys/unix"
)

// RTM_NEWTUNNEL / RTM_DELTUNNEL: a VXLAN device's VNI filter, as `bridge
// vni show` and `bridge monitor vni` receive it. The kernel's side is
// drivers/net/vxlan/vxlan_vnifilter.c (vxlan_vnifilter_dump_dev,
// vxlan_fill_dev_vnis): a struct tunnel_msg, then one VXLAN_VNIFILTER_ENTRY
// nest per VNI or VNI range, each with START, END (a range), GROUP or
// GROUP6 (the per-VNI remote), and a STATS nest when the dump asked for
// TUNNEL_MSG_FLAG_STATS. bridge's side is bridge/vni.c print_vnifilter_rtm.

// TunnelMsgSizeCst is sizeof(struct tunnel_msg): u8 family, u8 flags,
// u16 reserved2, u32 ifindex.
const TunnelMsgSizeCst = 8

// TunnelMsgFlagStats is TUNNEL_MSG_FLAG_STATS: include each VNI's counters.
const TunnelMsgFlagStats = 0x01

// VXLAN_VNIFILTER_* (if_link.h).
const (
	VxlanVnifilterEntry       = 1 // nest, repeated
	VxlanVnifilterEntryStart  = 1 // u32
	VxlanVnifilterEntryEnd    = 2 // u32
	VxlanVnifilterEntryGroup  = 3 // in_addr, network order
	VxlanVnifilterEntryGroup6 = 4 // in6_addr
	VxlanVnifilterEntryStats  = 5 // nest of VNIFILTER_ENTRY_STATS_*
	vxlanVnifilterEntryMax    = 5
)

// VNIFILTER_ENTRY_STATS_* (if_link.h): u64 counters.
const (
	VnifilterEntryStatsRxBytes  = 1
	VnifilterEntryStatsRxPkts   = 2
	VnifilterEntryStatsRxDrops  = 3
	VnifilterEntryStatsRxErrors = 4
	VnifilterEntryStatsTxBytes  = 5
	VnifilterEntryStatsTxPkts   = 6
	VnifilterEntryStatsTxDrops  = 7
	VnifilterEntryStatsTxErrors = 8
	vnifilterEntryStatsMax      = 9 // VNIFILTER_ENTRY_STATS_PAD
)

// ErrTunnelMsgSmall is a body shorter than tunnel_msg.
var ErrTunnelMsgSmall = errors.New("xtcpnl: data too small for tunnel_msg")

// VniEntry is one VXLAN_VNIFILTER_ENTRY nest.
type VniEntry struct {
	Attrs AttrTable // VXLAN_VNIFILTER_ENTRY_*
	// Stats is the STATS nest's table, when Attrs has one.
	Stats AttrTable
}

// TunnelMsg is a decoded RTM_NEWTUNNEL / RTM_DELTUNNEL body.
type TunnelMsg struct {
	Family  uint8
	Flags   uint8
	Ifindex uint32
	Entries []VniEntry // in wire order
}

// ParseTunnelMsg decodes an RTM_NEWTUNNEL body (the bytes after the
// nlmsghdr). Attributes other than VXLAN_VNIFILTER_ENTRY are skipped, as
// print_vnifilter_rtm skips them. A malformed stream is an error.
func ParseTunnelMsg(body []byte) (TunnelMsg, error) {
	if len(body) < TunnelMsgSizeCst {
		return TunnelMsg{}, ErrTunnelMsgSmall
	}
	m := TunnelMsg{
		Family:  body[0],
		Flags:   body[1],
		Ifindex: binary.LittleEndian.Uint32(body[4:8]),
	}
	var inner error
	err := WalkRTAttrs(body[TunnelMsgSizeCst:], func(atype uint16, val []byte) {
		if atype != VxlanVnifilterEntry || inner != nil {
			return
		}
		var e VniEntry
		if e.Attrs, inner = ParseAttrTable(val, vxlanVnifilterEntryMax); inner != nil {
			return
		}
		if s := e.Attrs.Bytes(VxlanVnifilterEntryStats); s != nil {
			if e.Stats, inner = ParseAttrTable(s, vnifilterEntryStatsMax); inner != nil {
				return
			}
		}
		m.Entries = append(m.Entries, e)
	})
	if err != nil {
		return TunnelMsg{}, err
	}
	if inner != nil {
		return TunnelMsg{}, inner
	}
	return m, nil
}

// BuildDumpTunnelRequest builds the RTM_GETTUNNEL dump `bridge vni show`
// sends (rtnl_tunneldump_req, lib/libnetlink.c): a tunnel_msg with family
// PF_BRIDGE, the flags (TunnelMsgFlagStats under -s) and the `dev`
// filter's ifindex, 0 for every device; no attributes.
func BuildDumpTunnelRequest(ifindex uint32, flags uint8, seq uint32) []byte {
	hdr := make([]byte, TunnelMsgSizeCst)
	hdr[0] = unix.AF_BRIDGE
	hdr[1] = flags
	binary.LittleEndian.PutUint32(hdr[4:8], ifindex)
	return BuildDumpRequest(uint16(unix.RTM_GETTUNNEL), seq, hdr)
}

// BRIDGE_VLANDB_DUMP_* (if_bridge.h): the RTM_GETVLAN dump's flags
// attribute and its bits.
const (
	BridgeVlandbDumpFlags   = 1 // u32
	BridgeVlandbDumpfStats  = 1 << 0
	BridgeVlandbDumpfGlobal = 1 << 1
)

// BuildDumpBrVlanRequest builds the RTM_GETVLAN dump `bridge -d vlan show`
// and `bridge vlan global show` send (rtnl_brvlandump_req,
// lib/libnetlink.c): a br_vlan_msg with family PF_BRIDGE and ifindex 0,
// then BRIDGE_VLANDB_DUMP_FLAGS, always present, 0 when no flag is set.
func BuildDumpBrVlanRequest(dumpFlags, seq uint32) ([]byte, error) {
	hdr := make([]byte, BrVlanMsgSizeCst)
	hdr[0] = unix.AF_BRIDGE
	var raw [reqAttrBufCst]byte
	ab := NewAttrBuilder(raw[:])
	if err := ab.PutU32(BridgeVlandbDumpFlags, dumpFlags); err != nil {
		return nil, err
	}
	return BuildRequest(uint16(unix.RTM_GETVLAN), uint16(unix.NLM_F_DUMP), seq, hdr, ab.Bytes())
}
