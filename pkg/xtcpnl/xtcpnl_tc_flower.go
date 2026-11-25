package xtcpnl

import (
	"encoding/binary"
	"errors"
	"fmt"

	"golang.org/x/sys/unix"
)

// This file is flower's step between ParseTcFopt and tc/f_flower.c's
// flower_print_opt (iproute2 v7.2.0). The top level is ParseTcFopt's Tb,
// parse_rtattr_nested(tb, TCA_FLOWER_MAX, opt). The nests the printer opens
// itself are decoded here the way it opens them: with plain parse_rtattr
// (flags 0, so a nest flagged NLA_F_NESTED inside them is a type above max
// and dropped), first occurrence winning, and the "!!!Deficit" a walk leaves
// kept as an AttrDeficit for the printer to report.
//
// Where flower_print_opt dereferences an attribute it never tested (geneve's
// class, type and data; vxlan's gbp; erspan's, gtp's and pfcp's fields), C
// reads through NULL. The decoders return ErrTcFlowerOptMissing there, naming
// the attribute, and keep what was decoded before it.

// TcFKindFlower is cls_flower's kind (tc/f_flower.c filter_util.id).
const TcFKindFlower = "flower"

// TCA_FLOWER_* from include/uapi/linux/pkt_cls.h. The enum has no explicit
// values; each is its position.
const (
	TcaFlowerClassid          uint16 = 1   // TCA_FLOWER_CLASSID: u32
	TcaFlowerIndev            uint16 = 2   // TCA_FLOWER_INDEV: string
	TcaFlowerAct              uint16 = 3   // TCA_FLOWER_ACT: nest of actions
	TcaFlowerKeyEthDst        uint16 = 4   // TCA_FLOWER_KEY_ETH_DST: ETH_ALEN
	TcaFlowerKeyEthDstMask    uint16 = 5   // TCA_FLOWER_KEY_ETH_DST_MASK: ETH_ALEN
	TcaFlowerKeyEthSrc        uint16 = 6   // TCA_FLOWER_KEY_ETH_SRC: ETH_ALEN
	TcaFlowerKeyEthSrcMask    uint16 = 7   // TCA_FLOWER_KEY_ETH_SRC_MASK: ETH_ALEN
	TcaFlowerKeyEthType       uint16 = 8   // TCA_FLOWER_KEY_ETH_TYPE: be16
	TcaFlowerKeyIPProto       uint16 = 9   // TCA_FLOWER_KEY_IP_PROTO: u8
	TcaFlowerKeyIPv4Src       uint16 = 10  // TCA_FLOWER_KEY_IPV4_SRC: be32
	TcaFlowerKeyIPv4SrcMask   uint16 = 11  // TCA_FLOWER_KEY_IPV4_SRC_MASK: be32
	TcaFlowerKeyIPv4Dst       uint16 = 12  // TCA_FLOWER_KEY_IPV4_DST: be32
	TcaFlowerKeyIPv4DstMask   uint16 = 13  // TCA_FLOWER_KEY_IPV4_DST_MASK: be32
	TcaFlowerKeyIPv6Src       uint16 = 14  // TCA_FLOWER_KEY_IPV6_SRC: struct in6_addr
	TcaFlowerKeyIPv6SrcMask   uint16 = 15  // TCA_FLOWER_KEY_IPV6_SRC_MASK: struct in6_addr
	TcaFlowerKeyIPv6Dst       uint16 = 16  // TCA_FLOWER_KEY_IPV6_DST: struct in6_addr
	TcaFlowerKeyIPv6DstMask   uint16 = 17  // TCA_FLOWER_KEY_IPV6_DST_MASK: struct in6_addr
	TcaFlowerKeyTCPSrc        uint16 = 18  // TCA_FLOWER_KEY_TCP_SRC: be16
	TcaFlowerKeyTCPDst        uint16 = 19  // TCA_FLOWER_KEY_TCP_DST: be16
	TcaFlowerKeyUDPSrc        uint16 = 20  // TCA_FLOWER_KEY_UDP_SRC: be16
	TcaFlowerKeyUDPDst        uint16 = 21  // TCA_FLOWER_KEY_UDP_DST: be16
	TcaFlowerFlags            uint16 = 22  // TCA_FLOWER_FLAGS: u32
	TcaFlowerKeyVlanID        uint16 = 23  // TCA_FLOWER_KEY_VLAN_ID: be16
	TcaFlowerKeyVlanPrio      uint16 = 24  // TCA_FLOWER_KEY_VLAN_PRIO: u8
	TcaFlowerKeyVlanEthType   uint16 = 25  // TCA_FLOWER_KEY_VLAN_ETH_TYPE: be16
	TcaFlowerKeyEncKeyID      uint16 = 26  // TCA_FLOWER_KEY_ENC_KEY_ID: be32
	TcaFlowerKeyEncIPv4Src    uint16 = 27  // TCA_FLOWER_KEY_ENC_IPV4_SRC: be32
	TcaFlowerKeyEncIPv4SrcMsk uint16 = 28  // TCA_FLOWER_KEY_ENC_IPV4_SRC_MASK: be32
	TcaFlowerKeyEncIPv4Dst    uint16 = 29  // TCA_FLOWER_KEY_ENC_IPV4_DST: be32
	TcaFlowerKeyEncIPv4DstMsk uint16 = 30  // TCA_FLOWER_KEY_ENC_IPV4_DST_MASK: be32
	TcaFlowerKeyEncIPv6Src    uint16 = 31  // TCA_FLOWER_KEY_ENC_IPV6_SRC: struct in6_addr
	TcaFlowerKeyEncIPv6SrcMsk uint16 = 32  // TCA_FLOWER_KEY_ENC_IPV6_SRC_MASK: struct in6_addr
	TcaFlowerKeyEncIPv6Dst    uint16 = 33  // TCA_FLOWER_KEY_ENC_IPV6_DST: struct in6_addr
	TcaFlowerKeyEncIPv6DstMsk uint16 = 34  // TCA_FLOWER_KEY_ENC_IPV6_DST_MASK: struct in6_addr
	TcaFlowerKeyTCPSrcMask    uint16 = 35  // TCA_FLOWER_KEY_TCP_SRC_MASK: be16
	TcaFlowerKeyTCPDstMask    uint16 = 36  // TCA_FLOWER_KEY_TCP_DST_MASK: be16
	TcaFlowerKeyUDPSrcMask    uint16 = 37  // TCA_FLOWER_KEY_UDP_SRC_MASK: be16
	TcaFlowerKeyUDPDstMask    uint16 = 38  // TCA_FLOWER_KEY_UDP_DST_MASK: be16
	TcaFlowerKeySCTPSrcMask   uint16 = 39  // TCA_FLOWER_KEY_SCTP_SRC_MASK: be16
	TcaFlowerKeySCTPDstMask   uint16 = 40  // TCA_FLOWER_KEY_SCTP_DST_MASK: be16
	TcaFlowerKeySCTPSrc       uint16 = 41  // TCA_FLOWER_KEY_SCTP_SRC: be16
	TcaFlowerKeySCTPDst       uint16 = 42  // TCA_FLOWER_KEY_SCTP_DST: be16
	TcaFlowerKeyEncUDPSrcPort uint16 = 43  // TCA_FLOWER_KEY_ENC_UDP_SRC_PORT: be16
	TcaFlowerKeyEncUDPSrcPMsk uint16 = 44  // TCA_FLOWER_KEY_ENC_UDP_SRC_PORT_MASK: be16
	TcaFlowerKeyEncUDPDstPort uint16 = 45  // TCA_FLOWER_KEY_ENC_UDP_DST_PORT: be16
	TcaFlowerKeyEncUDPDstPMsk uint16 = 46  // TCA_FLOWER_KEY_ENC_UDP_DST_PORT_MASK: be16
	TcaFlowerKeyFlags         uint16 = 47  // TCA_FLOWER_KEY_FLAGS: be32
	TcaFlowerKeyFlagsMask     uint16 = 48  // TCA_FLOWER_KEY_FLAGS_MASK: be32
	TcaFlowerKeyICMPv4Code    uint16 = 49  // TCA_FLOWER_KEY_ICMPV4_CODE: u8
	TcaFlowerKeyICMPv4CodeMsk uint16 = 50  // TCA_FLOWER_KEY_ICMPV4_CODE_MASK: u8
	TcaFlowerKeyICMPv4Type    uint16 = 51  // TCA_FLOWER_KEY_ICMPV4_TYPE: u8
	TcaFlowerKeyICMPv4TypeMsk uint16 = 52  // TCA_FLOWER_KEY_ICMPV4_TYPE_MASK: u8
	TcaFlowerKeyICMPv6Code    uint16 = 53  // TCA_FLOWER_KEY_ICMPV6_CODE: u8
	TcaFlowerKeyICMPv6CodeMsk uint16 = 54  // TCA_FLOWER_KEY_ICMPV6_CODE_MASK: u8
	TcaFlowerKeyICMPv6Type    uint16 = 55  // TCA_FLOWER_KEY_ICMPV6_TYPE: u8
	TcaFlowerKeyICMPv6TypeMsk uint16 = 56  // TCA_FLOWER_KEY_ICMPV6_TYPE_MASK: u8
	TcaFlowerKeyArpSip        uint16 = 57  // TCA_FLOWER_KEY_ARP_SIP: be32
	TcaFlowerKeyArpSipMask    uint16 = 58  // TCA_FLOWER_KEY_ARP_SIP_MASK: be32
	TcaFlowerKeyArpTip        uint16 = 59  // TCA_FLOWER_KEY_ARP_TIP: be32
	TcaFlowerKeyArpTipMask    uint16 = 60  // TCA_FLOWER_KEY_ARP_TIP_MASK: be32
	TcaFlowerKeyArpOp         uint16 = 61  // TCA_FLOWER_KEY_ARP_OP: u8
	TcaFlowerKeyArpOpMask     uint16 = 62  // TCA_FLOWER_KEY_ARP_OP_MASK: u8
	TcaFlowerKeyArpSha        uint16 = 63  // TCA_FLOWER_KEY_ARP_SHA: ETH_ALEN
	TcaFlowerKeyArpShaMask    uint16 = 64  // TCA_FLOWER_KEY_ARP_SHA_MASK: ETH_ALEN
	TcaFlowerKeyArpTha        uint16 = 65  // TCA_FLOWER_KEY_ARP_THA: ETH_ALEN
	TcaFlowerKeyArpThaMask    uint16 = 66  // TCA_FLOWER_KEY_ARP_THA_MASK: ETH_ALEN
	TcaFlowerKeyMplsTTL       uint16 = 67  // TCA_FLOWER_KEY_MPLS_TTL: u8
	TcaFlowerKeyMplsBos       uint16 = 68  // TCA_FLOWER_KEY_MPLS_BOS: u8
	TcaFlowerKeyMplsTc        uint16 = 69  // TCA_FLOWER_KEY_MPLS_TC: u8
	TcaFlowerKeyMplsLabel     uint16 = 70  // TCA_FLOWER_KEY_MPLS_LABEL: be32
	TcaFlowerKeyTCPFlags      uint16 = 71  // TCA_FLOWER_KEY_TCP_FLAGS: be16
	TcaFlowerKeyTCPFlagsMask  uint16 = 72  // TCA_FLOWER_KEY_TCP_FLAGS_MASK: be16
	TcaFlowerKeyIPTos         uint16 = 73  // TCA_FLOWER_KEY_IP_TOS: u8
	TcaFlowerKeyIPTosMask     uint16 = 74  // TCA_FLOWER_KEY_IP_TOS_MASK: u8
	TcaFlowerKeyIPTTL         uint16 = 75  // TCA_FLOWER_KEY_IP_TTL: u8
	TcaFlowerKeyIPTTLMask     uint16 = 76  // TCA_FLOWER_KEY_IP_TTL_MASK: u8
	TcaFlowerKeyCvlanID       uint16 = 77  // TCA_FLOWER_KEY_CVLAN_ID: be16
	TcaFlowerKeyCvlanPrio     uint16 = 78  // TCA_FLOWER_KEY_CVLAN_PRIO: u8
	TcaFlowerKeyCvlanEthType  uint16 = 79  // TCA_FLOWER_KEY_CVLAN_ETH_TYPE: be16
	TcaFlowerKeyEncIPTos      uint16 = 80  // TCA_FLOWER_KEY_ENC_IP_TOS: u8
	TcaFlowerKeyEncIPTosMask  uint16 = 81  // TCA_FLOWER_KEY_ENC_IP_TOS_MASK: u8
	TcaFlowerKeyEncIPTTL      uint16 = 82  // TCA_FLOWER_KEY_ENC_IP_TTL: u8
	TcaFlowerKeyEncIPTTLMask  uint16 = 83  // TCA_FLOWER_KEY_ENC_IP_TTL_MASK: u8
	TcaFlowerKeyEncOpts       uint16 = 84  // TCA_FLOWER_KEY_ENC_OPTS: nest
	TcaFlowerKeyEncOptsMask   uint16 = 85  // TCA_FLOWER_KEY_ENC_OPTS_MASK: nest
	TcaFlowerInHwCount        uint16 = 86  // TCA_FLOWER_IN_HW_COUNT: u32
	TcaFlowerKeyPortSrcMin    uint16 = 87  // TCA_FLOWER_KEY_PORT_SRC_MIN: be16
	TcaFlowerKeyPortSrcMax    uint16 = 88  // TCA_FLOWER_KEY_PORT_SRC_MAX: be16
	TcaFlowerKeyPortDstMin    uint16 = 89  // TCA_FLOWER_KEY_PORT_DST_MIN: be16
	TcaFlowerKeyPortDstMax    uint16 = 90  // TCA_FLOWER_KEY_PORT_DST_MAX: be16
	TcaFlowerKeyCtState       uint16 = 91  // TCA_FLOWER_KEY_CT_STATE: u16
	TcaFlowerKeyCtStateMask   uint16 = 92  // TCA_FLOWER_KEY_CT_STATE_MASK: u16
	TcaFlowerKeyCtZone        uint16 = 93  // TCA_FLOWER_KEY_CT_ZONE: u16
	TcaFlowerKeyCtZoneMask    uint16 = 94  // TCA_FLOWER_KEY_CT_ZONE_MASK: u16
	TcaFlowerKeyCtMark        uint16 = 95  // TCA_FLOWER_KEY_CT_MARK: u32
	TcaFlowerKeyCtMarkMask    uint16 = 96  // TCA_FLOWER_KEY_CT_MARK_MASK: u32
	TcaFlowerKeyCtLabels      uint16 = 97  // TCA_FLOWER_KEY_CT_LABELS: u128
	TcaFlowerKeyCtLabelsMask  uint16 = 98  // TCA_FLOWER_KEY_CT_LABELS_MASK: u128
	TcaFlowerKeyMplsOpts      uint16 = 99  // TCA_FLOWER_KEY_MPLS_OPTS: nest
	TcaFlowerKeyHash          uint16 = 100 // TCA_FLOWER_KEY_HASH: u32
	TcaFlowerKeyHashMask      uint16 = 101 // TCA_FLOWER_KEY_HASH_MASK: u32
	TcaFlowerKeyNumOfVlans    uint16 = 102 // TCA_FLOWER_KEY_NUM_OF_VLANS: u8
	TcaFlowerKeyPppoeSid      uint16 = 103 // TCA_FLOWER_KEY_PPPOE_SID: be16
	TcaFlowerKeyPppProto      uint16 = 104 // TCA_FLOWER_KEY_PPP_PROTO: be16
	TcaFlowerKeyL2tpv3Sid     uint16 = 105 // TCA_FLOWER_KEY_L2TPV3_SID: be32
	TcaFlowerL2Miss           uint16 = 106 // TCA_FLOWER_L2_MISS: u8
	TcaFlowerKeyCfm           uint16 = 107 // TCA_FLOWER_KEY_CFM: nest
	TcaFlowerKeySpi           uint16 = 108 // TCA_FLOWER_KEY_SPI: be32
	TcaFlowerKeySpiMask       uint16 = 109 // TCA_FLOWER_KEY_SPI_MASK: be32
	TcaFlowerKeyEncFlags      uint16 = 110 // TCA_FLOWER_KEY_ENC_FLAGS: be32
	TcaFlowerKeyEncFlagsMask  uint16 = 111 // TCA_FLOWER_KEY_ENC_FLAGS_MASK: be32
	// TcaFlowerMaxKnown is TCA_FLOWER_MAX, the same in iproute2 v7.2.0's
	// copy of pkt_cls.h and the kernel's at v7.4-rc4.
	TcaFlowerMaxKnown = TcaFlowerKeyEncFlagsMask

	// TCA_FLOWER_KEY_ENC_OPTS_* (pkt_cls.h).
	TcaFlowerKeyEncOptsGeneve uint16 = 1
	TcaFlowerKeyEncOptsVxlan  uint16 = 2
	TcaFlowerKeyEncOptsErspan uint16 = 3
	TcaFlowerKeyEncOptsGtp    uint16 = 4
	TcaFlowerKeyEncOptsPfcp   uint16 = 5
	// TcaFlowerKeyEncOptsMaxKnown is TCA_FLOWER_KEY_ENC_OPTS_MAX.
	TcaFlowerKeyEncOptsMaxKnown = TcaFlowerKeyEncOptsPfcp

	// TCA_FLOWER_KEY_ENC_OPT_GENEVE_* (pkt_cls.h).
	TcaFlowerKeyEncOptGeneveClass uint16 = 1 // be16
	TcaFlowerKeyEncOptGeneveType  uint16 = 2 // u8
	TcaFlowerKeyEncOptGeneveData  uint16 = 3 // up to 124 bytes
	// TcaFlowerKeyEncOptGeneveMaxKnown is TCA_FLOWER_KEY_ENC_OPT_GENEVE_MAX.
	TcaFlowerKeyEncOptGeneveMaxKnown = TcaFlowerKeyEncOptGeneveData

	// TCA_FLOWER_KEY_ENC_OPT_VXLAN_* (pkt_cls.h).
	TcaFlowerKeyEncOptVxlanGbp      uint16 = 1 // u32
	TcaFlowerKeyEncOptVxlanMaxKnown        = TcaFlowerKeyEncOptVxlanGbp

	// TCA_FLOWER_KEY_ENC_OPT_ERSPAN_* (pkt_cls.h).
	TcaFlowerKeyEncOptErspanVer      uint16 = 1 // u8
	TcaFlowerKeyEncOptErspanIndex    uint16 = 2 // be32
	TcaFlowerKeyEncOptErspanDir      uint16 = 3 // u8
	TcaFlowerKeyEncOptErspanHwid     uint16 = 4 // u8
	TcaFlowerKeyEncOptErspanMaxKnown        = TcaFlowerKeyEncOptErspanHwid

	// TCA_FLOWER_KEY_ENC_OPT_GTP_* (pkt_cls.h).
	TcaFlowerKeyEncOptGtpPduType  uint16 = 1 // u8
	TcaFlowerKeyEncOptGtpQfi      uint16 = 2 // u8
	TcaFlowerKeyEncOptGtpMaxKnown        = TcaFlowerKeyEncOptGtpQfi

	// TCA_FLOWER_KEY_ENC_OPT_PFCP_* (pkt_cls.h).
	TcaFlowerKeyEncOptPfcpType     uint16 = 1 // u8
	TcaFlowerKeyEncOptPfcpSeid     uint16 = 2 // be64
	TcaFlowerKeyEncOptPfcpMaxKnown        = TcaFlowerKeyEncOptPfcpSeid

	// TCA_FLOWER_KEY_MPLS_OPTS_LSE (pkt_cls.h), the only type in
	// TCA_FLOWER_KEY_MPLS_OPTS.
	TcaFlowerKeyMplsOptsLse uint16 = 1

	// TCA_FLOWER_KEY_MPLS_OPT_LSE_* (pkt_cls.h).
	TcaFlowerKeyMplsOptLseDepth    uint16 = 1 // u8
	TcaFlowerKeyMplsOptLseTTL      uint16 = 2 // u8
	TcaFlowerKeyMplsOptLseBos      uint16 = 3 // u8
	TcaFlowerKeyMplsOptLseTc       uint16 = 4 // u8
	TcaFlowerKeyMplsOptLseLabel    uint16 = 5 // u32
	TcaFlowerKeyMplsOptLseMaxKnown        = TcaFlowerKeyMplsOptLseLabel

	// TCA_FLOWER_KEY_CFM_* (pkt_cls.h).
	TcaFlowerKeyCfmMdLevel     uint16 = 1 // u8
	TcaFlowerKeyCfmOpcode      uint16 = 2 // u8
	TcaFlowerKeyCfmOptMaxKnown        = TcaFlowerKeyCfmOpcode

	// TCA_FLOWER_KEY_FLAGS_* (pkt_cls.h): KEY_FLAGS and KEY_ENC_FLAGS bits.
	TcaFlowerKeyFlagsIsFragment         = 1 << 0
	TcaFlowerKeyFlagsFragIsFirst        = 1 << 1
	TcaFlowerKeyFlagsTunnelCsum         = 1 << 2
	TcaFlowerKeyFlagsTunnelDontFragment = 1 << 3
	TcaFlowerKeyFlagsTunnelOam          = 1 << 4
	TcaFlowerKeyFlagsTunnelCritOpt      = 1 << 5

	// TCA_FLOWER_KEY_CT_FLAGS_* (pkt_cls.h): CT_STATE bits.
	TcaFlowerKeyCtFlagsNew         = 1 << 0
	TcaFlowerKeyCtFlagsEstablished = 1 << 1
	TcaFlowerKeyCtFlagsRelated     = 1 << 2
	TcaFlowerKeyCtFlagsTracked     = 1 << 3
	TcaFlowerKeyCtFlagsInvalid     = 1 << 4
	TcaFlowerKeyCtFlagsReply       = 1 << 5
)

// tcFlowerGeneveHdrCst is what flower_print_geneve_opts adds to an option's
// data length to step to the next one: the CLASS (4+2, padded to 8), TYPE
// (4+1, padded to 8) and DATA headers, as if every option were exactly those
// three attributes with 4-byte-aligned data (FL10).
const tcFlowerGeneveHdrCst = 20

// ErrTcFlowerOptMissing is an attribute flower_print_opt reads without
// testing (rta_getattr_* on NULL in C), absent or shorter than the read.
var ErrTcFlowerOptMissing = errors.New("xtcpnl: flower option attribute missing")

// AttrDeficit is what parse_rtattr_flags leaves when RTA_OK stops before
// the end: "!!!Deficit %d, rta_len=%d" on stderr. Len is the signed
// remainder C holds (negative when the last attribute's padding runs past
// the end); RtaLen is the rta_len C reads at the stop, valid when HasRtaLen
// (C reads past the buffer otherwise).
type AttrDeficit struct {
	Len       int
	RtaLen    uint16
	HasRtaLen bool
}

// Any is `if (len)`: whether C prints the line.
func (d AttrDeficit) Any() bool { return d.Len != 0 }

// ParseAttrTableLoose is parse_rtattr_flags as C runs it on a malformed
// stream: the attributes RTA_OK accepts, first occurrence winning, and the
// walk's deficit instead of an error.
func ParseAttrTableLoose(data []byte, maxType, flags uint16) (AttrTable, AttrDeficit) {
	if maxType > attrTableMaxCst {
		return AttrTable{}, AttrDeficit{}
	}
	t := AttrTable{vals: make([][]byte, int(maxType)+1)}
	d := walkRawAttrsLoose(data, func(raw uint16, val []byte) {
		atype := raw &^ flags
		if int(atype) < len(t.vals) && t.vals[atype] == nil {
			t.vals[atype] = val[:len(val):len(val)]
		}
	})
	return t, d
}

// walkRawAttrsLoose is the RTA_OK/RTA_NEXT loop on a signed remainder, as
// C runs it, returning the deficit.
func walkRawAttrsLoose(data []byte, fn func(raw uint16, val []byte)) AttrDeficit {
	off, rem := 0, len(data)
	for rem >= RTAttrSizeCst {
		alen := int(binary.LittleEndian.Uint16(data[off : off+2]))
		if alen < RTAttrSizeCst || alen > rem {
			break
		}
		fn(binary.LittleEndian.Uint16(data[off+2:off+4]), data[off+RTAttrSizeCst:off+alen])
		adv := alen + FourByteAlignPadding(alen)
		off += adv
		rem -= adv
	}
	d := AttrDeficit{Len: rem}
	if rem > 0 && off+2 <= len(data) {
		d.RtaLen, d.HasRtaLen = binary.LittleEndian.Uint16(data[off:off+2]), true
	}
	return d
}

// rawTypeOf is the raw rta_type, flags included, of the first attribute
// whose type with flags masked is atype: what tb[atype]->rta_type is after
// parse_rtattr_flags.
func rawTypeOf(data []byte, atype, flags uint16) (uint16, bool) {
	var raw uint16
	found := false
	walkRawAttrsLoose(data, func(t uint16, _ []byte) {
		if !found && t&^flags == atype {
			raw, found = t, true
		}
	})
	return raw, found
}

// TcFlowerNested reports NLA_F_NESTED on the raw type of opt's
// TCA_FLOWER_KEY_MPLS_OPTS and TCA_FLOWER_KEY_CFM: flower_print_mpls_opts
// and flower_print_cfm print nothing unless `attr->rta_type &
// NLA_F_NESTED`, a bit ParseTcFopt's table masks off.
type TcFlowerNested struct {
	MplsOpts bool
	Cfm      bool
}

// ParseTcFlowerNested reads those two flags from opt, a TCA_OPTIONS payload.
func ParseTcFlowerNested(opt []byte) TcFlowerNested {
	var n TcFlowerNested
	if raw, ok := rawTypeOf(opt, TcaFlowerKeyMplsOpts, tcNestedFlags); ok {
		n.MplsOpts = raw&tcNestedFlags != 0
	}
	if raw, ok := rawTypeOf(opt, TcaFlowerKeyCfm, tcNestedFlags); ok {
		n.Cfm = raw&tcNestedFlags != 0
	}
	return n
}

// TcFlowerGeneveOpt is one step of flower_print_geneve_opts' walk.
type TcFlowerGeneveOpt struct {
	Class uint16 // rta_getattr_be16(tb[CLASS])
	Type  uint8  // rta_getattr_u8(tb[TYPE])
	Data  []byte // RTA_DATA/RTA_PAYLOAD of tb[DATA]
}

// TcFlowerGeneve is flower_print_geneve_opts' walk over a GENEVE nest.
// Each step parses everything that remains (so a step's attributes may come
// from a later option), then advances by data length + 20 whatever the
// option's real size was (FL10). Deficits are each step's parse_rtattr
// leftovers, in order.
type TcFlowerGeneve struct {
	Opts     []TcFlowerGeneveOpt
	Deficits []AttrDeficit
}

// ParseTcFlowerGeneve walks b, a TCA_FLOWER_KEY_ENC_OPTS_GENEVE payload, as
// C does. Where C would read a NULL attribute (one of the three missing from
// what remains, or a remainder gone negative) the walk stops with
// ErrTcFlowerOptMissing, keeping the steps before it.
func ParseTcFlowerGeneve(b []byte) (TcFlowerGeneve, error) {
	var g TcFlowerGeneve
	offset, rem := 0, len(b)
	for rem != 0 {
		if rem < 0 {
			return g, fmt.Errorf("%w: geneve walk %d bytes past the nest", ErrTcFlowerOptMissing, -rem)
		}
		tb, d := ParseAttrTableLoose(b[offset:], TcaFlowerKeyEncOptGeneveMaxKnown, 0)
		if d.Any() {
			g.Deficits = append(g.Deficits, d)
		}
		cls := tb.Bytes(TcaFlowerKeyEncOptGeneveClass)
		if len(cls) < 2 {
			return g, fmt.Errorf("%w: geneve class", ErrTcFlowerOptMissing)
		}
		typ := tb.Bytes(TcaFlowerKeyEncOptGeneveType)
		if len(typ) < 1 {
			return g, fmt.Errorf("%w: geneve type", ErrTcFlowerOptMissing)
		}
		if !tb.Has(TcaFlowerKeyEncOptGeneveData) {
			return g, fmt.Errorf("%w: geneve data", ErrTcFlowerOptMissing)
		}
		data := tb.Bytes(TcaFlowerKeyEncOptGeneveData)
		g.Opts = append(g.Opts, TcFlowerGeneveOpt{Class: binary.BigEndian.Uint16(cls), Type: typ[0], Data: data})
		offset += len(data) + tcFlowerGeneveHdrCst
		rem -= len(data) + tcFlowerGeneveHdrCst
	}
	return g, nil
}

// TcFlowerVxlanOpt is flower_print_vxlan_opts' read.
type TcFlowerVxlanOpt struct {
	Gbp     uint32 // rta_getattr_u32(tb[GBP]), host order
	Deficit AttrDeficit
}

// ParseTcFlowerVxlan decodes a TCA_FLOWER_KEY_ENC_OPTS_VXLAN payload.
func ParseTcFlowerVxlan(b []byte) (TcFlowerVxlanOpt, error) {
	tb, d := ParseAttrTableLoose(b, TcaFlowerKeyEncOptVxlanMaxKnown, 0)
	o := TcFlowerVxlanOpt{Deficit: d}
	v, ok := tb.U32(TcaFlowerKeyEncOptVxlanGbp)
	if !ok {
		return o, fmt.Errorf("%w: vxlan gbp", ErrTcFlowerOptMissing)
	}
	o.Gbp = v
	return o, nil
}

// TcFlowerErspanOpt is flower_print_erspan_opts' read: Index for version
// 1, Dir and Hwid for any other version, the rest zero.
type TcFlowerErspanOpt struct {
	Ver     uint8
	Index   uint32 // rta_getattr_be32(tb[INDEX])
	Dir     uint8
	Hwid    uint8
	Deficit AttrDeficit
}

// ParseTcFlowerErspan decodes a TCA_FLOWER_KEY_ENC_OPTS_ERSPAN payload,
// reading in C's order: ver, then index or hwid then dir.
func ParseTcFlowerErspan(b []byte) (TcFlowerErspanOpt, error) {
	tb, d := ParseAttrTableLoose(b, TcaFlowerKeyEncOptErspanMaxKnown, 0)
	o := TcFlowerErspanOpt{Deficit: d}
	ver, ok := tb.U8(TcaFlowerKeyEncOptErspanVer)
	if !ok {
		return o, fmt.Errorf("%w: erspan ver", ErrTcFlowerOptMissing)
	}
	o.Ver = ver
	if ver == 1 {
		idx := tb.Bytes(TcaFlowerKeyEncOptErspanIndex)
		if len(idx) < 4 {
			return o, fmt.Errorf("%w: erspan index", ErrTcFlowerOptMissing)
		}
		o.Index = binary.BigEndian.Uint32(idx)
		return o, nil
	}
	if o.Hwid, ok = tb.U8(TcaFlowerKeyEncOptErspanHwid); !ok {
		return o, fmt.Errorf("%w: erspan hwid", ErrTcFlowerOptMissing)
	}
	if o.Dir, ok = tb.U8(TcaFlowerKeyEncOptErspanDir); !ok {
		return o, fmt.Errorf("%w: erspan dir", ErrTcFlowerOptMissing)
	}
	return o, nil
}

// TcFlowerGtpOpt is flower_print_gtp_opts' read.
type TcFlowerGtpOpt struct {
	PduType uint8
	Qfi     uint8
	Deficit AttrDeficit
}

// ParseTcFlowerGtp decodes a TCA_FLOWER_KEY_ENC_OPTS_GTP payload.
func ParseTcFlowerGtp(b []byte) (TcFlowerGtpOpt, error) {
	tb, d := ParseAttrTableLoose(b, TcaFlowerKeyEncOptGtpMaxKnown, 0)
	o := TcFlowerGtpOpt{Deficit: d}
	var ok bool
	if o.PduType, ok = tb.U8(TcaFlowerKeyEncOptGtpPduType); !ok {
		return o, fmt.Errorf("%w: gtp pdu_type", ErrTcFlowerOptMissing)
	}
	if o.Qfi, ok = tb.U8(TcaFlowerKeyEncOptGtpQfi); !ok {
		return o, fmt.Errorf("%w: gtp qfi", ErrTcFlowerOptMissing)
	}
	return o, nil
}

// TcFlowerPfcpOpt is flower_print_pfcp_opts' read.
type TcFlowerPfcpOpt struct {
	Type    uint8
	Seid    uint64 // rta_getattr_be64(tb[SEID])
	Deficit AttrDeficit
}

// ParseTcFlowerPfcp decodes a TCA_FLOWER_KEY_ENC_OPTS_PFCP payload.
func ParseTcFlowerPfcp(b []byte) (TcFlowerPfcpOpt, error) {
	tb, d := ParseAttrTableLoose(b, TcaFlowerKeyEncOptPfcpMaxKnown, 0)
	o := TcFlowerPfcpOpt{Deficit: d}
	var ok bool
	if o.Type, ok = tb.U8(TcaFlowerKeyEncOptPfcpType); !ok {
		return o, fmt.Errorf("%w: pfcp type", ErrTcFlowerOptMissing)
	}
	seid := tb.Bytes(TcaFlowerKeyEncOptPfcpSeid)
	if len(seid) < 8 {
		return o, fmt.Errorf("%w: pfcp seid", ErrTcFlowerOptMissing)
	}
	o.Seid = binary.BigEndian.Uint64(seid)
	return o, nil
}

// TcFlowerLse is one attribute of flower_print_mpls_opts' walk. Lse is
// false when its raw type is not exactly LSE|NLA_F_NESTED: C prints the
// "rta_type 0x%x, expecting ..." line and skips it (FL8). Otherwise Tb is
// its parse_rtattr table and Deficit that parse's leftover.
type TcFlowerLse struct {
	RawType uint16
	Lse     bool
	Tb      AttrTable
	Deficit AttrDeficit
}

// TcFlowerMplsOpts is flower_print_mpls_opts' walk: each attribute RTA_OK
// accepts, and the walk's own "!!!Deficit".
type TcFlowerMplsOpts struct {
	Lses    []TcFlowerLse
	Deficit AttrDeficit
}

// tcFlowerLseRawCst is TCA_FLOWER_KEY_MPLS_OPTS_LSE | NLA_F_NESTED, the
// only raw type flower_print_mpls_opt_lse prints.
const tcFlowerLseRawCst = TcaFlowerKeyMplsOptsLse | unix.NLA_F_NESTED

// ParseTcFlowerMplsOpts walks b, a TCA_FLOWER_KEY_MPLS_OPTS payload.
func ParseTcFlowerMplsOpts(b []byte) TcFlowerMplsOpts {
	var m TcFlowerMplsOpts
	m.Deficit = walkRawAttrsLoose(b, func(raw uint16, val []byte) {
		l := TcFlowerLse{RawType: raw, Lse: raw == tcFlowerLseRawCst}
		if l.Lse {
			l.Tb, l.Deficit = ParseAttrTableLoose(val, TcaFlowerKeyMplsOptLseMaxKnown, 0)
		}
		m.Lses = append(m.Lses, l)
	})
	return m
}

// ParseTcFlowerCfm is flower_print_cfm's parse_rtattr of a
// TCA_FLOWER_KEY_CFM payload.
func ParseTcFlowerCfm(b []byte) (AttrTable, AttrDeficit) {
	return ParseAttrTableLoose(b, TcaFlowerKeyCfmOptMaxKnown, 0)
}

// ParseTcFlowerEncOpts is flower_print_enc_opts' parse_rtattr_nested of a
// TCA_FLOWER_KEY_ENC_OPTS or _MASK payload.
func ParseTcFlowerEncOpts(b []byte) (AttrTable, AttrDeficit) {
	return ParseAttrTableLoose(b, TcaFlowerKeyEncOptsMaxKnown, tcNestedFlags)
}

// tcEthPAllCst is ETH_P_ALL (include/uapi/linux/if_ether.h).
const tcEthPAllCst = 0x0003

// tcFlowerGetOptsCst is the size of the nest TcFlowerGetOpts writes: its
// header, TCA_FLOWER_FLAGS (8) and TCA_FLOWER_KEY_ETH_TYPE (8).
const tcFlowerGetOptsCst = RTAttrSizeCst + 8 + 8

// TcFlowerGetOpts is what flower_parse_opt appends to `filter get ...
// flower` with no keys (tc/f_flower.c): a TCA_OPTIONS nest, without
// NLA_F_NESTED, of TCA_FLOWER_FLAGS 0 and, unless proto is ETH_P_ALL,
// TCA_FLOWER_KEY_ETH_TYPE as htons(proto). The kernel ignores both on a get
// (FL17). It is BuildTcFilterGet's kindOpts.
func TcFlowerGetOpts(proto uint16) ([]byte, error) {
	ab := NewAttrBuilder(make([]byte, tcFlowerGetOptsCst))
	m, err := ab.BeginNest(TcaOptions)
	if err != nil {
		return nil, err
	}
	if err := ab.PutU32(TcaFlowerFlags, 0); err != nil {
		return nil, err
	}
	if proto != tcEthPAllCst {
		if err := ab.PutBytes(TcaFlowerKeyEthType, binary.BigEndian.AppendUint16(nil, proto)); err != nil {
			return nil, err
		}
	}
	if err := ab.EndNest(m); err != nil {
		return nil, err
	}
	return ab.Bytes(), nil
}
