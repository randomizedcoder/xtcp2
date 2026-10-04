package xtcpnl

import (
	"bytes"
	"encoding/binary"
	"errors"

	"golang.org/x/sys/unix"
)

// FibRuleHdr mirrors the kernel's `struct fib_rule_hdr` — the family header of
// an RTM_*RULE message (the routing policy database, `ip rule`).
//
//	struct fib_rule_hdr {
//		__u8		family;
//		__u8		dst_len;
//		__u8		src_len;
//		__u8		tos;
//
//		__u8		table;
//		__u8		res1;   /* reserved */
//		__u8		res2;	/* reserved */
//		__u8		action;
//
//		__u32		flags;
//	};
//
// The two reserved bytes are carried rather than skipped, for the same reason
// NdMsg carries Pad1 and Pad2: a decoder that silently drops bytes cannot be
// used to prove a request round-trips, and the layout oracle compares fields.
//
// # `table` is eight bits and the table id is thirty-two
//
// This is the one trap in the struct. A table id above 255 cannot fit here, so
// the kernel sends RT_TABLE_UNSPEC (0) in the header and the real id in an
// FRA_TABLE attribute. iproute2 resolves the pair in frh_get_table
// (ip/iprule.c:90-96), which is reproduced as RuleInfo.Table — read that field,
// never this one.
//
// Reference: https://github.com/torvalds/linux/blob/master/include/uapi/linux/fib_rules.h
type FibRuleHdr struct {
	Family uint8  // 1
	DstLen uint8  // 1 = 2
	SrcLen uint8  // 1 = 3
	Tos    uint8  // 1 = 4
	Table  uint8  // 1 = 5
	Res1   uint8  // 1 = 6
	Res2   uint8  // 1 = 7
	Action uint8  // 1 = 8
	Flags  uint32 // 4 = 12 ( 12 / 4 = 3 )
}

const (
	FibRuleHdrSizeCst = 12
	FibRuleHdrReadCst = FibRuleHdrSizeCst

	// FibRuleUidRangeSizeCst is `struct fib_rule_uid_range`: two __u32.
	FibRuleUidRangeSizeCst = 8

	// FibRulePortRangeSizeCst is `struct fib_rule_port_range`: two __u16.
	FibRulePortRangeSizeCst = 4
)

// ErrFibRuleHdrSmall is the short-body error, named like ErrNdMsgSmall and
// ErrRtMsgSmall so the three read the same at a call site.
var ErrFibRuleHdrSmall = errors.New("data too small for FibRuleHdr")

// FRA_* attributes that golang.org/x/sys/unix v0.47.0 does not export.
//
// Its FRA_ run stops at FRA_DPORT_RANGE = 0x18 (24), six short of the current
// UAPI, which continues:
//
//	FRA_DSCP            25
//	FRA_FLOWLABEL       26
//	FRA_FLOWLABEL_MASK  27
//	FRA_SPORT_MASK      28
//	FRA_DPORT_MASK      29
//	FRA_DSCP_MASK       30
//
// Declared here for the same reason NdaFlagsExt and IflaNetnsImmutable are, and
// with the same guard: TestFraUnexportedValues pins each one to an offset from
// the last constant x/sys DOES define, so a miscount fails a test instead of
// decoding a neighbor of the intended attribute. That failure mode is not
// theoretical for this run — FRA_SPORT_MASK and FRA_DPORT_MASK are u16 sitting
// two apart, and FRA_DSCP and FRA_DSCP_MASK are u8 sitting five apart, so an
// off-by-one inside the run yields a well-formed value of the wrong thing.
//
// Reference: https://github.com/torvalds/linux/blob/master/include/uapi/linux/fib_rules.h
const (
	FraDscp          uint16 = 25
	FraFlowlabel     uint16 = 26
	FraFlowlabelMask uint16 = 27
	FraSportMask     uint16 = 28
	FraDportMask     uint16 = 29
	FraDscpMask      uint16 = 30
)

// FibRuleUidRange mirrors `struct fib_rule_uid_range` (FRA_UID_RANGE).
//
//	struct fib_rule_uid_range {
//		__u32		start;
//		__u32		end;
//	};
//
// Reference: https://github.com/torvalds/linux/blob/master/include/uapi/linux/fib_rules.h
type FibRuleUidRange struct {
	Start uint32 // 4 = 4
	End   uint32 // 4 = 8
}

// FibRulePortRange mirrors `struct fib_rule_port_range` (FRA_SPORT_RANGE and
// FRA_DPORT_RANGE).
//
//	struct fib_rule_port_range {
//		__u16		start;
//		__u16		end;
//	};
//
// The two members are HOST byte order, unlike every port number elsewhere in
// netlink. fib_rules.h declares them __u16 with no __be annotation, and
// fib_rule_port_inrange compares them against ntohs(flow port) — the
// conversion happens on the packet side, not on this one. A decoder that byte
// swaps here produces `sport 2048` for `sport 8`.
//
// Reference: https://github.com/torvalds/linux/blob/master/include/uapi/linux/fib_rules.h
type FibRulePortRange struct {
	Start uint16 // 2 = 2
	End   uint16 // 2 = 4
}

// DeserializeFibRuleHdr reads a fib_rule_hdr from the front of an RTM_*RULE
// message body.
func DeserializeFibRuleHdr(data []byte, m *FibRuleHdr) (n int, err error) {
	if len(data) < FibRuleHdrSizeCst {
		return 0, ErrFibRuleHdrSmall
	}

	m.Family = data[0]
	m.DstLen = data[1]
	m.SrcLen = data[2]
	m.Tos = data[3]
	m.Table = data[4]
	m.Res1 = data[5]
	m.Res2 = data[6]
	m.Action = data[7]
	m.Flags = binary.LittleEndian.Uint32(data[8:12])

	return FibRuleHdrReadCst, nil
}

// RuleInfo is one decoded routing policy rule: the fib_rule_hdr fields plus
// every FRA_* attribute print_rule reads (ip/iprule.c:279-600).
//
// # Why so many presence bits
//
// Because print_rule branches on attribute PRESENCE for almost every token,
// not on value. `fwmark 0x0` prints when FRA_FWMARK is present and carries
// zero, and prints nothing when the attribute is absent (:350-366); the same
// holds for goto, sport, dport, uidrange, ipproto and tun_id. A bare uint32
// collapses those two states into one and makes the renderer print tokens the
// kernel never asked for. Where iproute2 itself tests only the value — Tos and
// the FIB_RULE_* flag bits, which live in the header and are always present —
// there is no bit here either.
//
// Fields are declared in print_rule's emission order, so the renderer reads
// top to bottom and a reviewer can diff the two side by side.
type RuleInfo struct {
	// Header fields. Family selects how Src and Dst are formatted, and
	// SrcLen/DstLen are prefix lengths whose "print the /N" test is against
	// af_bit_len(family) rather than against zero (:322-324, :338-340).
	Family uint8
	SrcLen uint8
	DstLen uint8
	Tos    uint8
	Action uint8
	Flags  uint32

	// RawTable is fib_rule_hdr.table, the 8-bit field. Table below is the
	// resolved value and is what a renderer wants; this is kept so a test can
	// show the two disagree on a table above 255.
	RawTable uint8

	Priority    uint32 // FRA_PRIORITY, defaulting to 0 when absent (:309-312)
	HasPriority bool

	Src []byte // FRA_SRC
	Dst []byte // FRA_DST

	Fwmark    uint32 // FRA_FWMARK
	HasFwmark bool
	Fwmask    uint32 // FRA_FWMASK
	HasFwmask bool

	IifName    string // FRA_IIFNAME, a.k.a. FRA_IFNAME
	HasIifName bool
	OifName    string // FRA_OIFNAME
	HasOifName bool

	L3mdev    uint8 // FRA_L3MDEV
	HasL3mdev bool

	UidRange    FibRuleUidRange // FRA_UID_RANGE
	HasUidRange bool

	IPProto    uint8 // FRA_IP_PROTO
	HasIPProto bool

	SportRange    FibRulePortRange // FRA_SPORT_RANGE
	HasSportRange bool
	SportMask     uint16 // FRA_SPORT_MASK
	HasSportMask  bool
	DportRange    FibRulePortRange // FRA_DPORT_RANGE
	HasDportRange bool
	DportMask     uint16 // FRA_DPORT_MASK
	HasDportMask  bool

	// TunID is FRA_TUN_ID, and it is the one big-endian integer in this
	// struct: print_rule wraps it in ntohll (:474). Stored already converted,
	// so every consumer sees host order.
	TunID    uint64
	HasTunID bool

	// Table is frh_get_table's result: FRA_TABLE when present, else
	// fib_rule_hdr.table. See the FibRuleHdr doc for why the pair exists.
	Table uint32

	SuppressPrefixlen    uint32 // FRA_SUPPRESS_PREFIXLEN
	HasSuppressPrefixlen bool
	SuppressIfgroup      uint32 // FRA_SUPPRESS_IFGROUP
	HasSuppressIfgroup   bool

	Flow    uint32 // FRA_FLOW, the realms pair packed as from<<16 | to
	HasFlow bool

	// Gateway is RTA_GATEWAY, which is not an FRA_* constant at all. print_rule
	// reads it out of the same attribute table for the RTN_NAT action
	// (:525-534); the numbering happens not to collide because RTA_GATEWAY is
	// 5 and FRA_UNUSED2 is 5, an unused slot.
	Gateway    []byte
	HasGateway bool

	Goto    uint32 // FRA_GOTO
	HasGoto bool

	Protocol    uint8 // FRA_PROTOCOL
	HasProtocol bool

	Dscp        uint8 // FRA_DSCP
	HasDscp     bool
	DscpMask    uint8 // FRA_DSCP_MASK
	HasDscpMask bool

	Flowlabel        uint32 // FRA_FLOWLABEL, big-endian on the wire
	HasFlowlabel     bool
	FlowlabelMask    uint32 // FRA_FLOWLABEL_MASK, big-endian on the wire
	HasFlowlabelMask bool
}

// ParseRule decodes an RTM_NEWRULE / RTM_DELRULE message body (the bytes after
// the nlmsghdr): the fib_rule_hdr followed by FRA_* attributes.
//
// Malformed fixed-size attributes are skipped and leave their presence bit
// false, the policy ParseNeigh takes with NDA_CACHEINFO: the rest of the rule
// is still a correct answer, and a length the kernel cannot emit should not
// cost the caller the whole dump. The one thing never skipped is the header
// itself, because without it nothing that follows can be interpreted.
func ParseRule(body []byte) (RuleInfo, error) {
	var m FibRuleHdr
	if _, err := DeserializeFibRuleHdr(body, &m); err != nil {
		return RuleInfo{}, err
	}

	ri := RuleInfo{
		Family:   m.Family,
		SrcLen:   m.SrcLen,
		DstLen:   m.DstLen,
		Tos:      m.Tos,
		Action:   m.Action,
		Flags:    m.Flags,
		RawTable: m.Table,
		// frh_get_table's default, before FRA_TABLE has a chance to replace
		// it. Assigning here rather than after the walk is what makes the
		// attribute's absence mean "the header's value stands".
		Table: uint32(m.Table),
	}

	var seen attrSeen
	err := WalkRTAttrs(body[FibRuleHdrSizeCst:], func(atype uint16, val []byte) {
		if !seen.first(atype) {
			return
		}
		setRuleAttr(&ri, atype, val)
	})
	if err != nil {
		return RuleInfo{}, err
	}
	return ri, nil
}

// setRuleAttr applies one FRA_* attribute to a RuleInfo.
//
// Split out of ParseRule because the switch alone is past gocyclo's limit —
// the same reason setLinkDetailAttr exists — and it keeps ParseRule's body
// about the header-then-walk shape rather than about thirty attributes.
func setRuleAttr(ri *RuleInfo, atype uint16, val []byte) {
	switch atype {
	case uint16(unix.FRA_PRIORITY):
		if v, ok := attrU32(val); ok {
			ri.Priority, ri.HasPriority = v, true
		}
	case uint16(unix.FRA_SRC):
		ri.Src = CopyBytes(val)
	case uint16(unix.FRA_DST):
		ri.Dst = CopyBytes(val)
	case uint16(unix.FRA_FWMARK):
		if v, ok := attrU32(val); ok {
			ri.Fwmark, ri.HasFwmark = v, true
		}
	case uint16(unix.FRA_FWMASK):
		if v, ok := attrU32(val); ok {
			ri.Fwmask, ri.HasFwmask = v, true
		}
	case uint16(unix.FRA_IIFNAME):
		// FRA_IFNAME is a #define for this same value (3), not a second
		// attribute, so there is deliberately no case for it.
		ri.IifName, ri.HasIifName = string(bytes.TrimRight(val, "\x00")), true
	case uint16(unix.FRA_OIFNAME):
		ri.OifName, ri.HasOifName = string(bytes.TrimRight(val, "\x00")), true
	case uint16(unix.FRA_L3MDEV):
		if v, ok := attrU8(val); ok {
			ri.L3mdev, ri.HasL3mdev = v, true
		}
	case uint16(unix.FRA_UID_RANGE):
		if len(val) >= FibRuleUidRangeSizeCst {
			ri.UidRange = FibRuleUidRange{
				Start: binary.LittleEndian.Uint32(val[0:4]),
				End:   binary.LittleEndian.Uint32(val[4:8]),
			}
			ri.HasUidRange = true
		}
	case uint16(unix.FRA_IP_PROTO):
		if v, ok := attrU8(val); ok {
			ri.IPProto, ri.HasIPProto = v, true
		}
	case uint16(unix.FRA_SPORT_RANGE):
		if r, ok := attrPortRange(val); ok {
			ri.SportRange, ri.HasSportRange = r, true
		}
	case FraSportMask:
		if v, ok := attrU16(val); ok {
			ri.SportMask, ri.HasSportMask = v, true
		}
	case uint16(unix.FRA_DPORT_RANGE):
		if r, ok := attrPortRange(val); ok {
			ri.DportRange, ri.HasDportRange = r, true
		}
	case FraDportMask:
		if v, ok := attrU16(val); ok {
			ri.DportMask, ri.HasDportMask = v, true
		}
	case uint16(unix.FRA_TUN_ID):
		// ntohll at ip/iprule.c:474. The wire value is big-endian; every
		// consumer of RuleInfo sees host order.
		if len(val) >= 8 {
			ri.TunID, ri.HasTunID = binary.BigEndian.Uint64(val[0:8]), true
		}
	case uint16(unix.FRA_TABLE):
		// frh_get_table's override. FRA_TABLE and RTA_TABLE are both 15, so
		// this one case is what iproute2 reaches through `tb[RTA_TABLE]`.
		if v, ok := attrU32(val); ok {
			ri.Table = v
		}
	case uint16(unix.FRA_SUPPRESS_PREFIXLEN):
		if v, ok := attrU32(val); ok {
			ri.SuppressPrefixlen, ri.HasSuppressPrefixlen = v, true
		}
	case uint16(unix.FRA_SUPPRESS_IFGROUP):
		if v, ok := attrU32(val); ok {
			ri.SuppressIfgroup, ri.HasSuppressIfgroup = v, true
		}
	case uint16(unix.FRA_FLOW):
		if v, ok := attrU32(val); ok {
			ri.Flow, ri.HasFlow = v, true
		}
	case uint16(unix.RTA_GATEWAY):
		// Not a typo and not an FRA_* constant: print_rule reads RTA_GATEWAY
		// from this table for the RTN_NAT action. See RuleInfo.Gateway.
		ri.Gateway, ri.HasGateway = CopyBytes(val), true
	case uint16(unix.FRA_GOTO):
		if v, ok := attrU32(val); ok {
			ri.Goto, ri.HasGoto = v, true
		}
	case uint16(unix.FRA_PROTOCOL):
		if v, ok := attrU8(val); ok {
			ri.Protocol, ri.HasProtocol = v, true
		}
	case FraDscp:
		if v, ok := attrU8(val); ok {
			ri.Dscp, ri.HasDscp = v, true
		}
	case FraDscpMask:
		if v, ok := attrU8(val); ok {
			ri.DscpMask, ri.HasDscpMask = v, true
		}
	case FraFlowlabel:
		// rta_getattr_be32 at ip/iprule.c:587, so big-endian, unlike every
		// other u32 in this switch.
		if len(val) >= 4 {
			ri.Flowlabel, ri.HasFlowlabel = binary.BigEndian.Uint32(val[0:4]), true
		}
	case FraFlowlabelMask:
		if len(val) >= 4 {
			ri.FlowlabelMask, ri.HasFlowlabelMask = binary.BigEndian.Uint32(val[0:4]), true
		}
	}
}

// attrU8, attrU16, attrU32 and attrPortRange are the length-checked readers
// this file uses in place of iproute2's rta_getattr_*, which check nothing.
//
// The divergence is deliberate and is the same one NdaFlagsExt documents: a
// short attribute over-reads in `ip` and reads as absent here. The kernel emits
// no such attribute, so the difference is confined to a malformed message,
// where refusing to guess is the correct side to be on.
func attrU8(val []byte) (uint8, bool) {
	if len(val) < 1 {
		return 0, false
	}
	return val[0], true
}

func attrU16(val []byte) (uint16, bool) {
	if len(val) < 2 {
		return 0, false
	}
	return binary.LittleEndian.Uint16(val[0:2]), true
}

func attrU32(val []byte) (uint32, bool) {
	if len(val) < 4 {
		return 0, false
	}
	return binary.LittleEndian.Uint32(val[0:4]), true
}

func attrPortRange(val []byte) (FibRulePortRange, bool) {
	if len(val) < FibRulePortRangeSizeCst {
		return FibRulePortRange{}, false
	}
	// Host byte order on purpose; see the FibRulePortRange doc.
	return FibRulePortRange{
		Start: binary.LittleEndian.Uint16(val[0:2]),
		End:   binary.LittleEndian.Uint16(val[2:4]),
	}, true
}
