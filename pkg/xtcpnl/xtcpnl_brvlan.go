package xtcpnl

import (
	"encoding/binary"
	"errors"
)

// RTM_NEWVLAN / RTM_DELVLAN: the bridge VLAN database, one message per port,
// as `bridge -d vlan show`, `bridge vlan global show` and `bridge monitor
// vlan` receive it, and what BIRD's nl_parse_vlan reads
// (docs/netlink/coverage-status.md, "Captures an outside consumer needs").
//
// A message is a br_vlan_msg header and then any number of
// BRIDGE_VLANDB_ENTRY nests (a port's VLANs, ranges compressed) or of
// BRIDGE_VLANDB_GLOBAL_OPTIONS nests (the bridge's per-VLAN options), each
// a stream of its own attributes.
//
// Reference: https://github.com/torvalds/linux/blob/master/include/uapi/linux/if_bridge.h
// and net/bridge/br_vlan.c (br_vlan_fill_vids, br_vlan_global_opts_fill).

// BrVlanMsgSizeCst is sizeof(struct br_vlan_msg).
//
//	struct br_vlan_msg {
//		__u8 family;
//		__u8 reserved1;
//		__u16 reserved2;
//		__u32 ifindex;
//	};
const BrVlanMsgSizeCst = 8

// BridgeVlanInfoSizeCst is sizeof(struct bridge_vlan_info): __u16 flags,
// __u16 vid.
const BridgeVlanInfoSizeCst = 4

// BRIDGE_VLANDB_* (if_bridge.h): the top-level attributes.
const (
	BridgeVlandbEntry         = 1 // BRIDGE_VLANDB_ENTRY
	BridgeVlandbGlobalOptions = 2 // BRIDGE_VLANDB_GLOBAL_OPTIONS
)

// BRIDGE_VLANDB_ENTRY_*: the attributes nested in an entry.
const (
	BridgeVlandbEntryInfo  = 1 // struct bridge_vlan_info
	BridgeVlandbEntryRange = 2 // u16, the last VID of a range Info begins
	BridgeVlandbEntryState = 3 // u8, BR_STATE_*
)

// BRIDGE_VLANDB_GOPTS_*: the attributes nested in a global options entry.
const (
	BridgeVlandbGoptsID    = 1 // u16, the VID
	BridgeVlandbGoptsRange = 2 // u16, the last VID of a range ID begins
)

// BRIDGE_VLAN_INFO_* (if_bridge.h): bridge_vlan_info's flags, with
// BridgeVlanInfoPvid and BridgeVlanInfoUntagged in xtcpnl_bridge_xstats.go.
const (
	BridgeVlanInfoMaster     = 1 << 0
	BridgeVlanInfoRangeBegin = 1 << 3
	BridgeVlanInfoRangeEnd   = 1 << 4
	BridgeVlanInfoBrentry    = 1 << 5
	BridgeVlanInfoOnlyOpts   = 1 << 6
)

var (
	// ErrBrVlanMsgSmall is a body shorter than br_vlan_msg.
	ErrBrVlanMsgSmall = errors.New("xtcpnl: data too small for br_vlan_msg")

	// ErrBrVlanInfoSmall is a BRIDGE_VLANDB_ENTRY_INFO shorter than
	// bridge_vlan_info. It is an error rather than a missing field because
	// the VID is the entry: without it the rest describes nothing.
	ErrBrVlanInfoSmall = errors.New("xtcpnl: BRIDGE_VLANDB_ENTRY_INFO too small for bridge_vlan_info")
)

// BrVlanEntry is one BRIDGE_VLANDB_ENTRY: a VID, or the range Vid..Range,
// with its flags and the port's STP state in it.
type BrVlanEntry struct {
	Flags    uint16 // bridge_vlan_info.flags, BRIDGE_VLAN_INFO_*
	Vid      uint16 // bridge_vlan_info.vid
	HasInfo  bool   // BRIDGE_VLANDB_ENTRY_INFO present
	Range    uint16 // BRIDGE_VLANDB_ENTRY_RANGE, the range's last VID
	HasRange bool   // BRIDGE_VLANDB_ENTRY_RANGE present
	State    uint8  // BRIDGE_VLANDB_ENTRY_STATE, BR_STATE_*
	HasState bool   // BRIDGE_VLANDB_ENTRY_STATE present
}

// BrVlanGlobal is one BRIDGE_VLANDB_GLOBAL_OPTIONS: a VID or range of the
// bridge itself. Its multicast options are not decoded.
type BrVlanGlobal struct {
	ID       uint16 // BRIDGE_VLANDB_GOPTS_ID
	Range    uint16 // BRIDGE_VLANDB_GOPTS_RANGE
	HasRange bool   // BRIDGE_VLANDB_GOPTS_RANGE present
}

// BrVlanInfo is a decoded RTM_NEWVLAN or RTM_DELVLAN body.
type BrVlanInfo struct {
	Family  uint8  // br_vlan_msg.family, AF_BRIDGE
	Ifindex uint32 // br_vlan_msg.ifindex, the port (or the bridge)
	Entries []BrVlanEntry
	Globals []BrVlanGlobal
}

// ParseBrVlan decodes an RTM_NEWVLAN / RTM_DELVLAN body (the bytes after the
// nlmsghdr). The top level repeats its attributes, one per VID or range, so
// it is not first-wins; inside each nest it is, as everywhere else.
func ParseBrVlan(body []byte) (BrVlanInfo, error) {
	if len(body) < BrVlanMsgSizeCst {
		return BrVlanInfo{}, ErrBrVlanMsgSmall
	}
	bv := BrVlanInfo{
		Family:  body[0],
		Ifindex: binary.LittleEndian.Uint32(body[4:8]),
	}
	var nestErr error
	err := WalkRTAttrs(body[BrVlanMsgSizeCst:], func(atype uint16, val []byte) {
		var aerr error
		switch atype {
		case BridgeVlandbEntry:
			var e BrVlanEntry
			if e, aerr = parseBrVlanEntry(val); aerr == nil {
				bv.Entries = append(bv.Entries, e)
			}
		case BridgeVlandbGlobalOptions:
			var g BrVlanGlobal
			if g, aerr = parseBrVlanGlobal(val); aerr == nil {
				bv.Globals = append(bv.Globals, g)
			}
		}
		if aerr != nil && nestErr == nil {
			nestErr = aerr
		}
	})
	if err != nil {
		return BrVlanInfo{}, err
	}
	if nestErr != nil {
		return BrVlanInfo{}, nestErr
	}
	return bv, nil
}

// parseBrVlanEntry decodes one BRIDGE_VLANDB_ENTRY's attributes. A short
// RANGE or STATE leaves its presence bit false.
func parseBrVlanEntry(val []byte) (BrVlanEntry, error) {
	var (
		e       BrVlanEntry
		infoErr error
		seen    attrSeen
	)
	err := WalkRTAttrsNested(val, func(atype uint16, v []byte) {
		if !seen.first(atype) {
			return
		}
		switch atype {
		case BridgeVlandbEntryInfo:
			if len(v) < BridgeVlanInfoSizeCst {
				infoErr = ErrBrVlanInfoSmall
				return
			}
			e.Flags = binary.LittleEndian.Uint16(v[0:2])
			e.Vid = binary.LittleEndian.Uint16(v[2:4])
			e.HasInfo = true
		case BridgeVlandbEntryRange:
			if len(v) >= 2 {
				e.Range = binary.LittleEndian.Uint16(v[0:2])
				e.HasRange = true
			}
		case BridgeVlandbEntryState:
			if len(v) >= 1 {
				e.State = v[0]
				e.HasState = true
			}
		}
	})
	if err != nil {
		return BrVlanEntry{}, err
	}
	if infoErr != nil {
		return BrVlanEntry{}, infoErr
	}
	return e, nil
}

// parseBrVlanGlobal decodes one BRIDGE_VLANDB_GLOBAL_OPTIONS's ID and RANGE.
func parseBrVlanGlobal(val []byte) (BrVlanGlobal, error) {
	var (
		g    BrVlanGlobal
		seen attrSeen
	)
	err := WalkRTAttrsNested(val, func(atype uint16, v []byte) {
		if !seen.first(atype) || len(v) < 2 {
			return
		}
		switch atype {
		case BridgeVlandbGoptsID:
			g.ID = binary.LittleEndian.Uint16(v[0:2])
		case BridgeVlandbGoptsRange:
			g.Range = binary.LittleEndian.Uint16(v[0:2])
			g.HasRange = true
		}
	})
	if err != nil {
		return BrVlanGlobal{}, err
	}
	return g, nil
}
