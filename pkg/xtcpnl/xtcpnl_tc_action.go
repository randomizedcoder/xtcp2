package xtcpnl

import (
	"encoding/binary"
)

// This file decodes a filter's actions (TCA_<KIND>_ACT) the way
// tc/m_action.c's tc_print_action and tc_print_one_action read them, and
// gact's options as tc/m_gact.c's print_gact does (iproute2 v7.2.0).
// ParseTcAction (xtcpnl_tcmsg.go) is the RTM_*ACTION message; this is the
// nest inside a filter.

// The action table and gact constants (include/uapi/linux/pkt_cls.h,
// tc_act/tc_gact.h).
const (
	// TcaActMaxPrioCst is TCA_ACT_MAX_PRIO: tc_dump_action's table holds
	// orders 0..32.
	TcaActMaxPrioCst = 32
	// TCA_ACT_FLAGS_* and TCA_ACT_HW_STATS_*.
	TcaActFlagsNoPercpuStats = 1 << 0
	TcaActFlagsSkipHw        = 1 << 1
	TcaActFlagsSkipSw        = 1 << 2
	TcaActHwStatsImmediate   = 1 << 0
	TcaActHwStatsDelayed     = 1 << 1

	TcaGactTm    uint16 = 1 // TCA_GACT_TM: struct tcf_t
	TcaGactParms uint16 = 2 // TCA_GACT_PARMS: struct tc_gact
	TcaGactProb  uint16 = 3 // TCA_GACT_PROB: struct tc_gact_p
	TcaGactPad   uint16 = 4 // TCA_GACT_PAD
	// TcaGactMaxKnown is TCA_GACT_MAX.
	TcaGactMaxKnown = TcaGactPad

	// TcGactSizeCst is sizeof(struct tc_gact): tc_gen.
	TcGactSizeCst = 20
	// TcGactPSizeCst is sizeof(struct tc_gact_p).
	TcGactPSizeCst = 8
	// TcfTSizeCst is sizeof(struct tcf_t).
	TcfTSizeCst = 32
	// NlaBitfield32SizeCst is sizeof(struct nla_bitfield32).
	NlaBitfield32SizeCst = 8

	// TcActKindGact is gact's kind string.
	TcActKindGact = "gact"
)

// ParseTcActTab is tc_dump_action's parse_rtattr_nested(tb,
// TCA_ACT_MAX_PRIO, arg): each action's nest by order, 0..32, the first
// of each winning, higher orders dropped.
func ParseTcActTab(nest []byte) (AttrTable, error) {
	return ParseAttrTableFlags(nest, TcaActMaxPrioCst, tcNestedFlags)
}

// ParseTcActOne is tc_print_one_action's parse_rtattr_nested(tb,
// TCA_ACT_MAX, arg).
func ParseTcActOne(nest []byte) (AttrTable, error) {
	return ParseAttrTableFlags(nest, TcaActMaxKnown, tcNestedFlags)
}

// NlaBitfield32 is struct nla_bitfield32.
type NlaBitfield32 struct {
	Value    uint32
	Selector uint32
}

// DecodeNlaBitfield32 decodes a struct nla_bitfield32. tc casts the
// payload without a length check (L28), so a short one is MIN-copied:
// the bytes that are there, the rest zero.
func DecodeNlaBitfield32(b []byte) NlaBitfield32 {
	r := tcRd{b: TcMinCopy(b, NlaBitfield32SizeCst)}
	return NlaBitfield32{Value: r.u32(), Selector: r.u32()}
}

// TcGact is struct tc_gact, tc_gen's five fields.
type TcGact struct {
	Index   uint32
	Capab   uint32
	Action  int32
	Refcnt  int32
	Bindcnt int32
}

// TcGactP is struct tc_gact_p.
type TcGactP struct {
	Ptype   uint16
	Pval    uint16
	Paction int32
}

// TcfT is struct tcf_t, in USER_HZ ticks.
type TcfT struct {
	Install  uint64
	Lastuse  uint64
	Expires  uint64
	Firstuse uint64
}

// TcGactOpt is gact's TCA_ACT_OPTIONS decoded for print_gact. Tb is
// parse_rtattr_nested(tb, TCA_GACT_MAX, arg). print_gact casts PARMS, PROB
// and TM without a length check (L28), so each is MIN-copied. Without
// PROB, tc prints a zeroed tc_gact_p all the same (F19): Prob is then
// zero and HasProb false.
type TcGactOpt struct {
	Tb       AttrTable
	Parms    TcGact
	HasParms bool
	Prob     TcGactP
	HasProb  bool
	Tm       TcfT
	HasTm    bool
}

// ParseTcGact decodes gact's options. A malformed attribute stream is the
// walk's error.
func ParseTcGact(opt []byte) (TcGactOpt, error) {
	var g TcGactOpt
	tb, err := ParseAttrTableFlags(opt, TcaGactMaxKnown, tcNestedFlags)
	if err != nil {
		return g, err
	}
	g.Tb = tb
	if b := tb.Bytes(TcaGactParms); b != nil {
		r := tcRd{b: TcMinCopy(b, TcGactSizeCst)}
		g.Parms = TcGact{Index: r.u32(), Capab: r.u32(), Action: r.s32(), Refcnt: r.s32(), Bindcnt: r.s32()}
		g.HasParms = true
	}
	if b := tb.Bytes(TcaGactProb); b != nil {
		r := tcRd{b: TcMinCopy(b, TcGactPSizeCst)}
		g.Prob = TcGactP{Ptype: r.u16(), Pval: r.u16(), Paction: r.s32()}
		g.HasProb = true
	}
	if b := tb.Bytes(TcaGactTm); b != nil {
		g.Tm, g.HasTm = DecodeTcfT(b), true
	}
	return g, nil
}

// DecodeTcfT decodes a struct tcf_t, MIN-copied as print_tm's callers
// cast it unchecked.
func DecodeTcfT(b []byte) TcfT {
	m := TcMinCopy(b, TcfTSizeCst)
	return TcfT{
		Install:  binary.LittleEndian.Uint64(m[0:8]),
		Lastuse:  binary.LittleEndian.Uint64(m[8:16]),
		Expires:  binary.LittleEndian.Uint64(m[16:24]),
		Firstuse: binary.LittleEndian.Uint64(m[24:32]),
	}
}
