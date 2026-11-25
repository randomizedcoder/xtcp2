package xtcpnl

import (
	"bytes"
	"encoding/binary"

	"github.com/randomizedcoder/xtcp2/pkg/cint"
)

// This file decodes the options of the last six tc actions the way their
// printers in tc/m_<kind>.c read them (iproute2 v7.2.0): pedit, tunnel_key,
// ife, ct, gate and bpf, plus bpf_print_ops (lib/bpf_legacy.c), which the
// bpf action and the bpf filter share. Each printer opens TCA_ACT_OPTIONS
// with parse_rtattr_nested(tb, TCA_<KIND>_MAX, arg), so the table is
// first-wins and NLA_F_NESTED is masked off.
//
// As in xtcpnl_tc_act_kinds.go, each parms struct is MIN-copied (the
// printers cast it unchecked, AC40). Where a printer goes further and
// walks a list or dereferences an attribute it never tested (pedit's keys
// and KEY_EX children, tunnel_key's option children, ct's NAT bounds and
// label mask, the bpf ops), the decode keeps what is there and flags what
// C would read past or through NULL, so the printer can refuse.

// The action kinds' strings (tc/m_<kind>.c action_util.id).
const (
	TcActKindPedit     = "pedit"
	TcActKindTunnelKey = "tunnel_key"
	TcActKindIfe       = "ife"
	TcActKindCt        = "ct"
	TcActKindGate      = "gate"
	TcActKindBpf       = "bpf"
)

// TCA_PEDIT_* from include/uapi/linux/tc_act/tc_pedit.h.
const (
	TcaPeditTm       uint16 = 1 // TCA_PEDIT_TM: struct tcf_t
	TcaPeditParms    uint16 = 2 // TCA_PEDIT_PARMS: struct tc_pedit_sel
	TcaPeditPad      uint16 = 3 // TCA_PEDIT_PAD
	TcaPeditParmsEx  uint16 = 4 // TCA_PEDIT_PARMS_EX: struct tc_pedit_sel
	TcaPeditKeysEx   uint16 = 5 // TCA_PEDIT_KEYS_EX: nest of KEY_EX
	TcaPeditKeyEx    uint16 = 6 // TCA_PEDIT_KEY_EX: nest of KEY_EX_*
	TcaPeditMaxKnown        = TcaPeditKeyEx

	TcaPeditKeyExHtype    uint16 = 1 // TCA_PEDIT_KEY_EX_HTYPE: u16
	TcaPeditKeyExCmd      uint16 = 2 // TCA_PEDIT_KEY_EX_CMD: u16
	TcaPeditKeyExMaxKnown        = TcaPeditKeyExCmd

	// enum pedit_header_type.
	PeditHtypeNetwork = 0
	PeditHtypeEth     = 1
	PeditHtypeIP4     = 2
	PeditHtypeIP6     = 3
	PeditHtypeTCP     = 4
	PeditHtypeUDP     = 5
	// enum pedit_cmd.
	PeditCmdSet = 0
	PeditCmdAdd = 1

	// struct tc_pedit_sel: tc_gen, nkeys and flags (u8 each), padded to
	// the keys' 4-byte alignment; then nkeys struct tc_pedit_key.
	TcPeditSelSizeCst = 24
	TcPeditKeySizeCst = 24
)

// TCA_TUNNEL_KEY_* from include/uapi/linux/tc_act/tc_tunnel_key.h.
const (
	TcaTunnelKeyTm         uint16 = 1  // TCA_TUNNEL_KEY_TM: struct tcf_t
	TcaTunnelKeyParms      uint16 = 2  // TCA_TUNNEL_KEY_PARMS: struct tc_tunnel_key
	TcaTunnelKeyEncIPv4Src uint16 = 3  // be32
	TcaTunnelKeyEncIPv4Dst uint16 = 4  // be32
	TcaTunnelKeyEncIPv6Src uint16 = 5  // struct in6_addr
	TcaTunnelKeyEncIPv6Dst uint16 = 6  // struct in6_addr
	TcaTunnelKeyEncKeyID   uint16 = 7  // be64 in the header; the kernel puts a be32
	TcaTunnelKeyPad        uint16 = 8  // TCA_TUNNEL_KEY_PAD
	TcaTunnelKeyEncDstPort uint16 = 9  // be16
	TcaTunnelKeyNoCsum     uint16 = 10 // u8
	TcaTunnelKeyEncOpts    uint16 = 11 // nest of ENC_OPTS_*
	TcaTunnelKeyEncTos     uint16 = 12 // u8
	TcaTunnelKeyEncTTL     uint16 = 13 // u8
	TcaTunnelKeyNoFrag     uint16 = 14 // flag
	TcaTunnelKeyMaxKnown          = TcaTunnelKeyNoFrag
	TcTunnelKeySizeCst            = 24 // sizeof(struct tc_tunnel_key)
	TcaTunnelKeyActSet            = 1  // TCA_TUNNEL_KEY_ACT_SET
	TcaTunnelKeyActRelease        = 2  // TCA_TUNNEL_KEY_ACT_RELEASE

	TcaTunnelKeyEncOptsGeneve   uint16 = 1 // nest of OPT_GENEVE_*, repeated per option
	TcaTunnelKeyEncOptsVxlan    uint16 = 2 // nest of OPT_VXLAN_*
	TcaTunnelKeyEncOptsErspan   uint16 = 3 // nest of OPT_ERSPAN_*
	TcaTunnelKeyEncOptsMaxKnown        = TcaTunnelKeyEncOptsErspan

	TcaTunnelKeyEncOptGeneveClass    uint16 = 1 // be16
	TcaTunnelKeyEncOptGeneveType     uint16 = 2 // u8
	TcaTunnelKeyEncOptGeneveData     uint16 = 3 // 4 to 128 bytes
	TcaTunnelKeyEncOptGeneveMaxKnown        = TcaTunnelKeyEncOptGeneveData

	TcaTunnelKeyEncOptVxlanGbp      uint16 = 1 // u32
	TcaTunnelKeyEncOptVxlanMaxKnown        = TcaTunnelKeyEncOptVxlanGbp

	TcaTunnelKeyEncOptErspanVer      uint16 = 1 // u8
	TcaTunnelKeyEncOptErspanIndex    uint16 = 2 // be32
	TcaTunnelKeyEncOptErspanDir      uint16 = 3 // u8
	TcaTunnelKeyEncOptErspanHwid     uint16 = 4 // u8
	TcaTunnelKeyEncOptErspanMaxKnown        = TcaTunnelKeyEncOptErspanHwid

	// tcGeneveStrideCst is what tunnel_key_print_geneve_options adds to
	// DATA's length to step to the next option: CLASS (8 aligned), TYPE (8
	// aligned) and DATA's header (4).
	tcGeneveStrideCst = 20
)

// TCA_IFE_* from include/uapi/linux/tc_act/tc_ife.h, IFE_META_* from
// include/uapi/linux/ife.h.
const (
	TcaIfeParms    uint16 = 1 // TCA_IFE_PARMS: struct tc_ife
	TcaIfeTm       uint16 = 2 // TCA_IFE_TM: struct tcf_t
	TcaIfeDmac     uint16 = 3 // TCA_IFE_DMAC: ETH_ALEN
	TcaIfeSmac     uint16 = 4 // TCA_IFE_SMAC: ETH_ALEN
	TcaIfeType     uint16 = 5 // TCA_IFE_TYPE: u16
	TcaIfeMetalst  uint16 = 6 // TCA_IFE_METALST: nest of IFE_META_*
	TcaIfePad      uint16 = 7 // TCA_IFE_PAD
	TcaIfeMaxKnown        = TcaIfePad
	TcIfeSizeCst          = 24 // sizeof(struct tc_ife): tc_gen, u16 flags, padded
	IfeEncode             = 1  // IFE_ENCODE

	IfeMetaSkbmark  uint16 = 1 // IFE_META_SKBMARK: u32, or empty for "allow"
	IfeMetaHashid   uint16 = 2 // IFE_META_HASHID
	IfeMetaPrio     uint16 = 3 // IFE_META_PRIO: u32
	IfeMetaQmap     uint16 = 4 // IFE_META_QMAP
	IfeMetaTcindex  uint16 = 5 // IFE_META_TCINDEX: u16
	IfeMetaMaxKnown        = IfeMetaTcindex
)

// TCA_CT_* from include/uapi/linux/tc_act/tc_ct.h.
const (
	TcaCtParms        uint16 = 1  // TCA_CT_PARMS: struct tc_ct
	TcaCtTm           uint16 = 2  // TCA_CT_TM: struct tcf_t
	TcaCtAction       uint16 = 3  // u16
	TcaCtZone         uint16 = 4  // u16
	TcaCtMark         uint16 = 5  // u32
	TcaCtMarkMask     uint16 = 6  // u32
	TcaCtLabels       uint16 = 7  // u128
	TcaCtLabelsMask   uint16 = 8  // u128
	TcaCtNatIPv4Min   uint16 = 9  // be32
	TcaCtNatIPv4Max   uint16 = 10 // be32
	TcaCtNatIPv6Min   uint16 = 11 // struct in6_addr
	TcaCtNatIPv6Max   uint16 = 12 // struct in6_addr
	TcaCtNatPortMin   uint16 = 13 // be16
	TcaCtNatPortMax   uint16 = 14 // be16
	TcaCtPad          uint16 = 15 // TCA_CT_PAD
	TcaCtHelperName   uint16 = 16 // string
	TcaCtHelperFamily uint16 = 17 // u8
	TcaCtHelperProto  uint16 = 18 // u8
	TcaCtMaxKnown            = TcaCtHelperProto
	TcCtSizeCst              = 20 // sizeof(struct tc_ct): tc_gen

	TcaCtActCommit = 1 << 0 // TCA_CT_ACT_COMMIT
	TcaCtActForce  = 1 << 1 // TCA_CT_ACT_FORCE
	TcaCtActClear  = 1 << 2 // TCA_CT_ACT_CLEAR
	TcaCtActNat    = 1 << 3 // TCA_CT_ACT_NAT
	TcaCtActNatSrc = 1 << 4 // TCA_CT_ACT_NAT_SRC
	TcaCtActNatDst = 1 << 5 // TCA_CT_ACT_NAT_DST

	// tcCtLabelsOutCst is ct_print_labels' `char out[256]`: the label in
	// hex, '/', the mask in hex and the NUL must fit.
	tcCtLabelsOutCst = 256
)

// TCA_GATE_* from include/uapi/linux/tc_act/tc_gate.h.
const (
	TcaGateTm           uint16 = 1  // TCA_GATE_TM: struct tcf_t
	TcaGateParms        uint16 = 2  // TCA_GATE_PARMS: struct tc_gate
	TcaGatePad          uint16 = 3  // TCA_GATE_PAD
	TcaGatePriority     uint16 = 4  // s32
	TcaGateEntryList    uint16 = 5  // nest of ONE_ENTRY
	TcaGateBaseTime     uint16 = 6  // u64 (tc reads s64)
	TcaGateCycleTime    uint16 = 7  // u64 (tc reads s64)
	TcaGateCycleTimeExt uint16 = 8  // u64 (tc reads s64)
	TcaGateFlags        uint16 = 9  // u32
	TcaGateClockid      uint16 = 10 // s32
	TcaGateMaxKnown            = TcaGateClockid
	TcGateSizeCst              = 20 // sizeof(struct tc_gate): tc_gen

	TcaGateOneEntry uint16 = 1 // TCA_GATE_ONE_ENTRY: nest of ENTRY_*

	TcaGateEntryIndex     uint16 = 1 // u32
	TcaGateEntryGate      uint16 = 2 // flag: open
	TcaGateEntryInterval  uint16 = 3 // u32
	TcaGateEntryIpv       uint16 = 4 // s32, -1 wildcard
	TcaGateEntryMaxOctets uint16 = 5 // s32, -1 wildcard
	TcaGateEntryMaxKnown         = TcaGateEntryMaxOctets
)

// TCA_ACT_BPF_* from include/uapi/linux/tc_act/tc_bpf.h.
const (
	TcaActBpfTm       uint16 = 1 // TCA_ACT_BPF_TM: struct tcf_t
	TcaActBpfParms    uint16 = 2 // TCA_ACT_BPF_PARMS: struct tc_act_bpf
	TcaActBpfOpsLen   uint16 = 3 // u16: instructions
	TcaActBpfOps      uint16 = 4 // struct sock_filter[]
	TcaActBpfFd       uint16 = 5 // u32 (requests only)
	TcaActBpfName     uint16 = 6 // string
	TcaActBpfPad      uint16 = 7 // TCA_ACT_BPF_PAD
	TcaActBpfTag      uint16 = 8 // eBPF program tag
	TcaActBpfID       uint16 = 9 // u32: eBPF program id
	TcaActBpfMaxKnown        = TcaActBpfID
	TcActBpfSizeCst          = 20 // sizeof(struct tc_act_bpf): tc_gen

	// TcBpfInsnSizeCst is sizeof(struct sock_filter).
	TcBpfInsnSizeCst = 8
)

// tcSmallTbCst bounds the nested tables parseSmallTable fills.
const tcSmallTbCst = 8

// parseSmallTable is ParseAttrTableLoose into buf, for a maxType below
// tcSmallTbCst: a list's per-element table without an allocation each.
// The table aliases buf until the next call.
func parseSmallTable(buf *[tcSmallTbCst][]byte, data []byte, maxType, flags uint16) AttrTable {
	clear(buf[:])
	t := AttrTable{vals: buf[:int(maxType)+1]}
	walkRawAttrsLoose(data, func(raw uint16, val []byte) {
		atype := raw &^ flags
		if int(atype) < len(t.vals) && t.vals[atype] == nil {
			t.vals[atype] = val[:len(val):len(val)]
		}
	})
	return t
}

// TcCString is rta_getattr_str: the bytes before the first NUL. ok is
// false when there is no NUL in b, where C reads on past the attribute
// (AC64).
func TcCString(b []byte) (string, bool) {
	before, _, found := bytes.Cut(b, []byte{0})
	return string(before), found
}

// TcPeditKey is struct tc_pedit_key, each field as the host reads it (tc
// prints ntohl of Val and Mask).
type TcPeditKey struct {
	Mask    uint32
	Val     uint32
	Off     uint32
	At      uint32
	Offmask uint32
	Shift   uint32
}

// TcPeditKeyEx is one KEY_EX nest as pedit_keys_ex_getattr reads it. An
// OK flag is false where tc's rta_getattr_u16 reads an absent attribute
// (NULL) or one shorter than 2 bytes (AC45).
type TcPeditKeyEx struct {
	Htype   uint16
	Cmd     uint16
	HtypeOK bool
	CmdOK   bool
}

// TcPeditOpt is pedit's options. P, Nkeys and Flags come from PARMS when
// it is there, else from PARMS_EX (Ex). Keys holds the keys within the
// payload, at most Nkeys; KeysShort is Nkeys keys running past it, which
// print_pedit walks anyway (AC40).
//
// KeysEx is TCA_PEDIT_KEYS_EX's children when Ex, and KeysExRet what
// pedit_keys_ex_getattr returns: -1 for a child too many or of another
// type, 1 for too few, 0 otherwise. print_pedit stops with "Netlink error"
// on any but 0, and on a PARMS_EX without KEYS_EX (!HasKeysEx).
type TcPeditOpt struct {
	TcActCommon
	P         TcGen
	Nkeys     uint8
	Flags     uint8
	Ex        bool
	Keys      []TcPeditKey
	KeysShort bool
	HasKeysEx bool
	KeysEx    []TcPeditKeyEx
	KeysExRet int
}

// ParseTcPedit decodes pedit's options (tc/m_pedit.c print_pedit).
func ParseTcPedit(opt []byte) (TcPeditOpt, error) {
	// The parms attribute is chosen from the table, so parseActKind
	// takes none and the MIN-copy is done here.
	c, r, err := parseActKind(opt, TcaPeditMaxKnown, 0, TcaPeditTm, TcPeditSelSizeCst)
	if err != nil {
		return TcPeditOpt{}, err
	}
	tb := c.Tb
	parms := TcaPeditParms
	o := TcPeditOpt{}
	if !tb.Has(parms) && tb.Has(TcaPeditParmsEx) {
		parms, o.Ex = TcaPeditParmsEx, true
	}
	if tb.Has(parms) {
		c.HasParms, c.ParmsLen = true, len(tb.Bytes(parms))
		r.b = TcMinCopy(tb.Bytes(parms), TcPeditSelSizeCst)
	}
	o.TcActCommon = c
	o.P = r.tcGen()
	o.Nkeys, o.Flags = r.u8(), r.u8()
	sel := tb.Bytes(parms)
	avail := 0
	if len(sel) > TcPeditSelSizeCst {
		avail = (len(sel) - TcPeditSelSizeCst) / TcPeditKeySizeCst
	}
	n := int(o.Nkeys)
	if n > avail {
		n, o.KeysShort = avail, true
	}
	if n > 0 {
		o.Keys = make([]TcPeditKey, n)
		kr := tcRd{b: sel[TcPeditSelSizeCst:]}
		for i := range o.Keys {
			o.Keys[i] = TcPeditKey{Mask: kr.u32(), Val: kr.u32(), Off: kr.u32(), At: kr.u32(), Offmask: kr.u32(),
				Shift: kr.u32()}
		}
	}
	if o.Ex && tb.Has(TcaPeditKeysEx) {
		o.HasKeysEx = true
		o.KeysEx, o.KeysExRet = ParseTcPeditKeysEx(tb.Bytes(TcaPeditKeysEx), int(o.Nkeys))
	}
	return o, nil
}

// ParseTcPeditKeysEx is pedit_keys_ex_getattr(attr, keys_ex, n) over a
// KEYS_EX payload: the children it reads, and its return.
func ParseTcPeditKeysEx(b []byte, n int) ([]TcPeditKeyEx, int) {
	var out []TcPeditKeyEx
	if n > 0 {
		out = make([]TcPeditKeyEx, 0, n)
	}
	var buf [tcSmallTbCst][]byte
	ret := 0
	stop := false
	// The RTA_OK loop: a malformed child ends it, as here.
	walkRawAttrsLoose(b, func(raw uint16, val []byte) {
		if stop {
			return
		}
		if len(out) == n || raw != TcaPeditKeyEx {
			ret, stop = -1, true
			return
		}
		ktb := parseSmallTable(&buf, val, TcaPeditKeyExMaxKnown, tcNestedFlags)
		var k TcPeditKeyEx
		k.Htype, k.HtypeOK = ktb.U16(TcaPeditKeyExHtype)
		k.Cmd, k.CmdOK = ktb.U16(TcaPeditKeyExCmd)
		out = append(out, k)
	})
	if !stop && len(out) < n {
		ret = 1
	}
	return out, ret
}

// TcGeneveOpt is one geneve option as tunnel_key_print_geneve_options
// reads it: each child is the first of its type from the option's start
// to the end of the list (parse_rtattr over what remains).
type TcGeneveOpt struct {
	Class uint16
	Type  uint8
	Data  []byte
}

// TcVxlanOpt is the vxlan option; GbpOK is false where tc reads an absent
// or short GBP (AC47).
type TcVxlanOpt struct {
	Gbp   uint32
	GbpOK bool
}

// TcErspanOpt is the erspan option. tc reads VER, then INDEX for version
// 1 and DIR and HWID otherwise, none of them tested; OK is false where a
// read it makes hits an absent or short attribute (AC47).
type TcErspanOpt struct {
	Ver   uint8
	Index uint32
	Dir   uint8
	Hwid  uint8
	OK    bool
}

// TcTunnelKeyEncOpts is TCA_TUNNEL_KEY_ENC_OPTS: tunnel_key_print_key_opt
// prints geneve, else vxlan, else erspan.
//
// GeneveBad is the geneve walk going wrong: a child absent from what
// remains, a CLASS or TYPE too short for its read (C dereferences NULL or
// reads past), or a stride that steps past the list's end, where C's
// `while (rem)` runs on with rem negative (AC46).
type TcTunnelKeyEncOpts struct {
	Tb        AttrTable
	Geneve    []TcGeneveOpt
	GeneveBad bool
	Vxlan     TcVxlanOpt
	Erspan    TcErspanOpt
}

// TcTunnelKeyOpt is tunnel_key's options; TAction is t_action.
type TcTunnelKeyOpt struct {
	TcActCommon
	P       TcGen
	TAction int32
	HasOpts bool
	Opts    TcTunnelKeyEncOpts
}

// ParseTcTunnelKey decodes tunnel_key's options (tc/m_tunnel_key.c
// print_tunnel_key).
func ParseTcTunnelKey(opt []byte) (TcTunnelKeyOpt, error) {
	c, r, err := parseActKind(opt, TcaTunnelKeyMaxKnown, TcaTunnelKeyParms, TcaTunnelKeyTm, TcTunnelKeySizeCst)
	if err != nil {
		return TcTunnelKeyOpt{}, err
	}
	o := TcTunnelKeyOpt{TcActCommon: c, P: r.tcGen(), TAction: r.s32()}
	if c.Tb.Has(TcaTunnelKeyEncOpts) {
		o.HasOpts = true
		o.Opts = ParseTcTunnelKeyOpts(c.Tb.Bytes(TcaTunnelKeyEncOpts))
	}
	return o, nil
}

// ParseTcTunnelKeyOpts decodes an ENC_OPTS payload as
// tunnel_key_print_key_opt and its three helpers read it.
func ParseTcTunnelKeyOpts(b []byte) TcTunnelKeyEncOpts {
	tb, _ := ParseAttrTableLoose(b, TcaTunnelKeyEncOptsMaxKnown, tcNestedFlags)
	o := TcTunnelKeyEncOpts{Tb: tb}
	if g := tb.Bytes(TcaTunnelKeyEncOptsGeneve); g != nil {
		o.Geneve, o.GeneveBad = parseTkGeneveOpts(g)
	}
	if v := tb.Bytes(TcaTunnelKeyEncOptsVxlan); v != nil {
		var buf [tcSmallTbCst][]byte
		vtb := parseSmallTable(&buf, v, TcaTunnelKeyEncOptVxlanMaxKnown, 0)
		o.Vxlan.Gbp, o.Vxlan.GbpOK = vtb.U32(TcaTunnelKeyEncOptVxlanGbp)
	}
	if e := tb.Bytes(TcaTunnelKeyEncOptsErspan); e != nil {
		o.Erspan = parseErspanOpt(e)
	}
	return o
}

// parseTkGeneveOpts is tunnel_key_print_geneve_options' walk: parse_rtattr
// (flags 0) over what remains, take the first CLASS, TYPE and DATA, then
// step DATA's length plus 20 bytes, until rem is 0.
func parseTkGeneveOpts(b []byte) ([]TcGeneveOpt, bool) {
	// Each option is at least CLASS, TYPE and four bytes of DATA.
	out := make([]TcGeneveOpt, 0, len(b)/(tcGeneveStrideCst+4)+1)
	var buf [tcSmallTbCst][]byte
	for off := 0; off < len(b); {
		tb := parseSmallTable(&buf, b[off:], TcaTunnelKeyEncOptGeneveMaxKnown, 0)
		cls := tb.Bytes(TcaTunnelKeyEncOptGeneveClass)
		typ, okT := tb.U8(TcaTunnelKeyEncOptGeneveType)
		if len(cls) < 2 || !okT || !tb.Has(TcaTunnelKeyEncOptGeneveData) {
			return out, true
		}
		data := tb.Bytes(TcaTunnelKeyEncOptGeneveData)
		// rta_getattr_be16: the class in network order.
		out = append(out, TcGeneveOpt{Class: binary.BigEndian.Uint16(cls), Type: typ, Data: data})
		off += len(data) + tcGeneveStrideCst
		if off > len(b) {
			return out, true
		}
	}
	return out, false
}

// parseErspanOpt is tunnel_key_print_erspan_options' reads.
func parseErspanOpt(b []byte) TcErspanOpt {
	var buf [tcSmallTbCst][]byte
	tb := parseSmallTable(&buf, b, TcaTunnelKeyEncOptErspanMaxKnown, 0)
	var o TcErspanOpt
	var okV, okI, okD, okH bool
	o.Ver, okV = tb.U8(TcaTunnelKeyEncOptErspanVer)
	if o.Ver == 1 {
		ib := tb.Bytes(TcaTunnelKeyEncOptErspanIndex)
		if okI = len(ib) >= 4; okI {
			o.Index = binary.BigEndian.Uint32(ib)
		}
		o.OK = okV && okI
		return o
	}
	o.Dir, okD = tb.U8(TcaTunnelKeyEncOptErspanDir)
	o.Hwid, okH = tb.U8(TcaTunnelKeyEncOptErspanHwid)
	o.OK = okV && okD && okH
	return o
}

// TcIfeOpt is ife's options. Meta is METALST's table
// (parse_rtattr_nested(metalist, IFE_META_MAX)); print_ife reads a
// non-empty value as u32 or u16 whatever its length (AC53).
type TcIfeOpt struct {
	TcActCommon
	P       TcGen
	Flags   uint16
	HasMeta bool
	Meta    AttrTable
}

// ParseTcIfe decodes ife's options (tc/m_ife.c print_ife).
func ParseTcIfe(opt []byte) (TcIfeOpt, error) {
	c, r, err := parseActKind(opt, TcaIfeMaxKnown, TcaIfeParms, TcaIfeTm, TcIfeSizeCst)
	if err != nil {
		return TcIfeOpt{}, err
	}
	o := TcIfeOpt{TcActCommon: c, P: r.tcGen(), Flags: r.u16()}
	if m := c.Tb.Bytes(TcaIfeMetalst); m != nil {
		o.HasMeta = true
		o.Meta, _ = ParseAttrTableLoose(m, IfeMetaMaxKnown, tcNestedFlags)
	}
	return o, nil
}

// TcCInt is `*(int *)RTA_DATA(a)`: the four bytes from the attribute's
// payload on, whatever its length. Past is a read beyond the options,
// whose bytes are taken as 0 here.
type TcCInt struct {
	V    int32
	Has  bool
	Len  int
	Past bool
}

// tcCIntAt reads the first attribute of type atype (NLA_F_NESTED masked,
// as the table has it) the way ct_print_helper does.
func tcCIntAt(opt []byte, atype uint16) TcCInt {
	var v TcCInt
	off := 0
	for len(opt)-off >= RTAttrSizeCst {
		alen := int(binary.LittleEndian.Uint16(opt[off : off+2]))
		if alen < RTAttrSizeCst || alen > len(opt)-off {
			break
		}
		if binary.LittleEndian.Uint16(opt[off+2:off+4])&^tcNestedFlags == atype {
			var w [4]byte
			n := copy(w[:], opt[off+RTAttrSizeCst:])
			v.V = cint.Int(binary.LittleEndian.Uint32(w[:]))
			v.Has, v.Len, v.Past = true, alen-RTAttrSizeCst, n < len(w)
			return v
		}
		off += alen + FourByteAlignPadding(alen)
	}
	return v
}

// TcCtOpt is ct's options. HelperFamily and HelperProto are what
// ct_print_helper reads: an int from each one-byte attribute (AC56).
//
// NatUnchecked is a NAT the printer would memcmp through an absent MIN or
// past a short one: a MAX present with its MIN absent or shorter than MAX's
// compare width (AC54). LabelsUnchecked is a LABELS that ct_print_labels
// would pair with an absent or shorter mask, or whose hex with its mask
// overflows `out[256]` (AC55). Both are judged only where C reaches them.
type TcCtOpt struct {
	TcActCommon
	P               TcGen
	CtAction        uint16
	HelperFamily    TcCInt
	HelperProto     TcCInt
	NatUnchecked    bool
	LabelsUnchecked bool
}

// ParseTcCt decodes ct's options (tc/m_ct.c print_ct).
func ParseTcCt(opt []byte) (TcCtOpt, error) {
	c, r, err := parseActKind(opt, TcaCtMaxKnown, TcaCtParms, TcaCtTm, TcCtSizeCst)
	if err != nil {
		return TcCtOpt{}, err
	}
	o := TcCtOpt{TcActCommon: c, P: r.tcGen()}
	o.CtAction, _ = c.Tb.U16(TcaCtAction)
	o.HelperFamily = tcCIntAt(opt, TcaCtHelperFamily)
	o.HelperProto = tcCIntAt(opt, TcaCtHelperProto)
	o.NatUnchecked = ctNatUnchecked(c.Tb, o.CtAction)
	o.LabelsUnchecked = ctLabelsUnchecked(c.Tb)
	return o, nil
}

// ctNatUnchecked follows ct_print_nat to its memcmps: IPv4 MAX against
// MIN (4 bytes), else IPv6 (16), then the ports (2).
func ctNatUnchecked(tb AttrTable, act uint16) bool {
	if act&TcaCtActNat == 0 || act&(TcaCtActNatSrc|TcaCtActNatDst) == 0 {
		return false
	}
	short := func(minT uint16, n int) bool { return len(tb.Bytes(minT)) < n }
	bad := false
	switch {
	case tb.Has(TcaCtNatIPv4Max):
		bad = short(TcaCtNatIPv4Min, 4) || len(tb.Bytes(TcaCtNatIPv4Max)) < 4
		// memcmp equal means the IPv6 branch is not taken either way.
	case tb.Has(TcaCtNatIPv6Max):
		bad = short(TcaCtNatIPv6Min, 16) || len(tb.Bytes(TcaCtNatIPv6Max)) < 16
	}
	if tb.Has(TcaCtNatPortMax) {
		bad = bad || short(TcaCtNatPortMin, 2) || len(tb.Bytes(TcaCtNatPortMax)) < 2
	}
	// ct_sprint_port reads a be16 from MIN when it is there.
	if p := tb.Bytes(TcaCtNatPortMin); p != nil && len(p) < 2 {
		bad = true
	}
	return bad
}

// ctLabelsUnchecked follows ct_print_labels: the mask is read for the
// label's length, and printed after a '/' unless the label is 16 bytes of
// an all-ones mask.
func ctLabelsUnchecked(tb AttrTable) bool {
	l := tb.Bytes(TcaCtLabels)
	if l == nil {
		return false
	}
	m := tb.Bytes(TcaCtLabelsMask)
	if m == nil || len(m) < len(l) {
		return true
	}
	printMask := len(l) != 16
	for _, x := range m[:len(l)] {
		printMask = printMask || x != 0xff
	}
	need := 2*len(l) + 1 // hex and NUL
	if printMask {
		need += 1 + 2*len(l)
	}
	return need > tcCtLabelsOutCst
}

// TcGateEntry is one schedule entry as print_gate_list reads it: Ipv and
// MaxOctets are -1 when absent. Short is an entry attribute shorter than
// its read.
type TcGateEntry struct {
	Index     uint32
	Gate      bool
	Interval  uint32
	Ipv       int32
	MaxOctets int32
	Short     bool
}

// TcGateOpt is gate's options.
type TcGateOpt struct {
	TcActCommon
	P       TcGen
	HasList bool
	Entries []TcGateEntry
}

// ParseTcGate decodes gate's options (tc/m_gate.c print_gate).
func ParseTcGate(opt []byte) (TcGateOpt, error) {
	c, r, err := parseActKind(opt, TcaGateMaxKnown, TcaGateParms, TcaGateTm, TcGateSizeCst)
	if err != nil {
		return TcGateOpt{}, err
	}
	o := TcGateOpt{TcActCommon: c, P: r.tcGen()}
	if l := c.Tb.Bytes(TcaGateEntryList); l != nil {
		o.HasList = true
		o.Entries = ParseTcGateEntries(l)
	}
	return o, nil
}

// ParseTcGateEntries is print_gate_list's loop over an ENTRY_LIST payload:
// every child, whatever its type, is a nest of ENTRY_*.
func ParseTcGateEntries(b []byte) []TcGateEntry {
	n := 0
	walkRawAttrsLoose(b, func(uint16, []byte) { n++ })
	if n == 0 {
		return nil
	}
	out := make([]TcGateEntry, 0, n)
	var buf [tcSmallTbCst][]byte
	walkRawAttrsLoose(b, func(_ uint16, val []byte) {
		tb := parseSmallTable(&buf, val, TcaGateEntryMaxKnown, tcNestedFlags)
		e := TcGateEntry{Ipv: -1, MaxOctets: -1, Gate: tb.Has(TcaGateEntryGate)}
		u32 := func(t uint16) uint32 {
			v, ok := tb.U32(t)
			e.Short = e.Short || !ok
			return v
		}
		if tb.Has(TcaGateEntryIndex) {
			e.Index = u32(TcaGateEntryIndex)
		}
		if tb.Has(TcaGateEntryInterval) {
			e.Interval = u32(TcaGateEntryInterval)
		}
		if tb.Has(TcaGateEntryIpv) {
			e.Ipv = cint.Int(u32(TcaGateEntryIpv))
		}
		if tb.Has(TcaGateEntryMaxOctets) {
			e.MaxOctets = cint.Int(u32(TcaGateEntryMaxOctets))
		}
		out = append(out, e)
	})
	return out
}

// TcBpfInsn is struct sock_filter.
type TcBpfInsn struct {
	Code uint16
	Jt   uint8
	Jf   uint8
	K    uint32
}

// TcBpfOps is what bpf_print_ops prints: Len instructions from OPS. Insns
// holds those within the payload; Short is Len running past it, which C
// walks anyway (AC63).
type TcBpfOps struct {
	Len   uint16
	Insns []TcBpfInsn
	Short bool
}

// ParseTcBpfOps decodes ops, an OPS payload, for n instructions.
func ParseTcBpfOps(ops []byte, n uint16) TcBpfOps {
	o := TcBpfOps{Len: n}
	k := int(n)
	if avail := len(ops) / TcBpfInsnSizeCst; k > avail {
		k, o.Short = avail, true
	}
	if k == 0 {
		return o
	}
	o.Insns = make([]TcBpfInsn, k)
	r := tcRd{b: ops}
	for i := range o.Insns {
		o.Insns[i] = TcBpfInsn{Code: r.u16(), Jt: r.u8(), Jf: r.u8(), K: r.u32()}
	}
	return o
}

// TcBpfProg is the cBPF part the bpf action and filter print alike: the
// ops when both OPS and OPS_LEN are there (an OPS_LEN shorter than its u16
// read sets Ops.Short), and NAME.
type TcBpfProg struct {
	HasOps    bool
	Ops       TcBpfOps
	HasName   bool
	Name      string
	NameNoNUL bool
}

func parseBpfProg(tb AttrTable, opsT, opsLenT, nameT uint16) TcBpfProg {
	var p TcBpfProg
	if tb.Has(opsT) && tb.Has(opsLenT) {
		n, ok := tb.U16(opsLenT)
		p.HasOps, p.Ops = true, ParseTcBpfOps(tb.Bytes(opsT), n)
		p.Ops.Short = p.Ops.Short || !ok
	}
	if nb := tb.Bytes(nameT); nb != nil {
		var ok bool
		p.Name, ok = TcCString(nb)
		p.HasName, p.NameNoNUL = true, !ok
	}
	return p
}

// TcActBpfOpt is the bpf action's options.
type TcActBpfOpt struct {
	TcActCommon
	P TcGen
	TcBpfProg
}

// ParseTcActBpf decodes the bpf action's options (tc/m_bpf.c
// bpf_print_opt).
func ParseTcActBpf(opt []byte) (TcActBpfOpt, error) {
	c, r, err := parseActKind(opt, TcaActBpfMaxKnown, TcaActBpfParms, TcaActBpfTm, TcActBpfSizeCst)
	if err != nil {
		return TcActBpfOpt{}, err
	}
	return TcActBpfOpt{TcActCommon: c, P: r.tcGen(),
		TcBpfProg: parseBpfProg(c.Tb, TcaActBpfOps, TcaActBpfOpsLen, TcaActBpfName)}, nil
}
