package xtcpnl

import (
	"encoding/binary"
	"fmt"
)

// This file decodes an ematch tree (TCA_BASIC_EMATCHES, TCA_CGROUP_EMATCHES,
// TCA_FLOW_EMATCHES) the way tc/m_ematch.c's print_ematch reads it, and the
// payloads of the cmp, u32, nbyte, canid and meta ematches as their
// tc/em_*.c print_eopt read them (iproute2 v7.2.0).

// TCA_EMATCH_TREE_* and the ematch constants from
// include/uapi/linux/pkt_cls.h.
const (
	TcaEmatchTreeHdr  uint16 = 1 // TCA_EMATCH_TREE_HDR: struct tcf_ematch_tree_hdr
	TcaEmatchTreeList uint16 = 2 // TCA_EMATCH_TREE_LIST: nest of matches 1..nmatches
	// TcaEmatchTreeMaxKnown is TCA_EMATCH_TREE_MAX.
	TcaEmatchTreeMaxKnown = TcaEmatchTreeList

	// TCF_EM_* relation and flag bits of tcf_ematch_hdr.flags.
	TcfEmRelEnd  = 0
	TcfEmRelAnd  = 1 << 0
	TcfEmRelOr   = 1 << 1
	TcfEmInvert  = 1 << 2
	TcfEmSimple  = 1 << 3
	TcfEmRelMask = 3

	// TCF_EM_* kinds: tcf_ematch_hdr.kind.
	TcfEmKindContainer = 0
	TcfEmKindCmp       = 1
	TcfEmKindNbyte     = 2
	TcfEmKindU32       = 3
	TcfEmKindMeta      = 4
	TcfEmKindText      = 5
	TcfEmKindVlan      = 6
	TcfEmKindCanid     = 7
	TcfEmKindIpset     = 8
	TcfEmKindIpt       = 9

	// TCF_EM_OPND_* (pkt_cls.h).
	TcfEmOpndEq = 0
	TcfEmOpndGt = 1
	TcfEmOpndLt = 2

	// TcfEmatchTreeHdrSizeCst is sizeof(struct tcf_ematch_tree_hdr).
	TcfEmatchTreeHdrSizeCst = 4
	// TcfEmatchHdrSizeCst is sizeof(struct tcf_ematch_hdr).
	TcfEmatchHdrSizeCst = 8
)

// The cmp, nbyte and canid payloads (include/uapi/linux/tc_ematch/
// tc_em_cmp.h, tc_em_nbyte.h; include/uapi/linux/can.h).
const (
	// TcfEmCmpSizeCst is sizeof(struct tcf_em_cmp).
	TcfEmCmpSizeCst = 12
	// TCF_EM_ALIGN_* and TCF_EM_CMP_TRANS.
	TcfEmAlignU8   = 1
	TcfEmAlignU16  = 2
	TcfEmAlignU32  = 4
	TcfEmCmpTrans  = 1
	TcfEmNbyteSize = 4 // sizeof(struct tcf_em_nbyte)
	// CanFilterSizeCst is sizeof(struct can_filter).
	CanFilterSizeCst = 8
	// CAN_EFF_FLAG, CAN_EFF_MASK and CAN_SFF_MASK.
	CanEffFlag = 0x80000000
	CanEffMask = 0x1FFFFFFF
	CanSffMask = 0x000007FF
)

// TCA_EM_META_* and the meta object encoding (tc_ematch/tc_em_meta.h).
const (
	TcaEmMetaHdr    uint16 = 1 // TCA_EM_META_HDR: struct tcf_meta_hdr
	TcaEmMetaLvalue uint16 = 2 // TCA_EM_META_LVALUE
	TcaEmMetaRvalue uint16 = 3 // TCA_EM_META_RVALUE
	// TcaEmMetaMaxKnown is TCA_EM_META_MAX.
	TcaEmMetaMaxKnown = TcaEmMetaRvalue
	// TcfMetaHdrSizeCst is sizeof(struct tcf_meta_hdr): two tcf_meta_val.
	TcfMetaHdrSizeCst = 8
	// TCF_META_TYPE_VAR and TCF_META_TYPE_INT; TCF_META_ID_VALUE.
	TcfMetaTypeVar = 0
	TcfMetaTypeInt = 1
	TcfMetaIDValue = 0
)

// TcfEmatchTreeHdr is struct tcf_ematch_tree_hdr.
type TcfEmatchTreeHdr struct {
	NMatches uint16
	ProgID   uint16
}

// TcEmatchTree is an ematch tree nest decoded for print_ematch. Tb is
// parse_rtattr_nested(tb, TCA_EMATCH_TREE_MAX, rta). When the header is
// there and long enough, List is parse_rtattr_nested(tb, nmatches, list):
// matches 1..nmatches, the first of each winning. tc indexes it one past
// nmatches when the last match carries AND or OR (F2), which List.Has
// answers false for.
type TcEmatchTree struct {
	Tb       AttrTable
	Hdr      TcfEmatchTreeHdr
	HasHdr   bool
	HdrShort bool
	List     AttrTable
}

// ParseTcEmatchTree decodes an ematch tree nest. A malformed attribute
// stream is the walk's error, and so is an nmatches past the table bound.
func ParseTcEmatchTree(nest []byte) (TcEmatchTree, error) {
	var t TcEmatchTree
	tb, err := ParseAttrTableFlags(nest, TcaEmatchTreeMaxKnown, tcNestedFlags)
	if err != nil {
		return t, err
	}
	t.Tb = tb
	b := tb.Bytes(TcaEmatchTreeHdr)
	if b == nil {
		return t, nil
	}
	t.HasHdr = true
	if len(b) < TcfEmatchTreeHdrSizeCst {
		t.HdrShort = true
		return t, nil
	}
	t.Hdr = TcfEmatchTreeHdr{
		NMatches: binary.LittleEndian.Uint16(b[0:2]),
		ProgID:   binary.LittleEndian.Uint16(b[2:4]),
	}
	if list := tb.Bytes(TcaEmatchTreeList); list != nil && t.Hdr.NMatches > 0 {
		if t.List, err = ParseAttrTableFlags(list, t.Hdr.NMatches, tcNestedFlags); err != nil {
			return t, err
		}
	}
	return t, nil
}

// TcfEmatchHdr is struct tcf_ematch_hdr.
type TcfEmatchHdr struct {
	MatchID uint16
	Kind    uint16
	Flags   uint16
	Pad     uint16
}

// DecodeTcfEmatch splits one match's payload into its header and data, as
// print_ematch_seq does; a payload shorter than the header is ErrTcShort
// (dlen < 0, return -1).
func DecodeTcfEmatch(b []byte) (TcfEmatchHdr, []byte, error) {
	if len(b) < TcfEmatchHdrSizeCst {
		return TcfEmatchHdr{}, nil, tcNeed(b, TcfEmatchHdrSizeCst, "tcf_ematch_hdr")
	}
	hdr, data := b[:TcfEmatchHdrSizeCst], b[TcfEmatchHdrSizeCst:]
	r := tcRd{b: hdr}
	h := TcfEmatchHdr{MatchID: r.u16(), Kind: r.u16(), Flags: r.u16(), Pad: r.u16()}
	return h, data, nil
}

// TcfEmCmp is struct tcf_em_cmp; its four 4-bit fields are the low nibble
// first, as GCC lays out bitfields on a little-endian target.
type TcfEmCmp struct {
	Val   uint32
	Mask  uint32
	Off   uint16
	Align uint8
	Flags uint8
	Layer uint8
	Opnd  uint8
}

// DecodeTcfEmCmp decodes a cmp payload; short is ErrTcShort ("CMP header
// size mismatch").
func DecodeTcfEmCmp(b []byte) (TcfEmCmp, error) {
	if err := tcNeed(b, TcfEmCmpSizeCst, "tcf_em_cmp"); err != nil {
		return TcfEmCmp{}, err
	}
	r := tcRd{b: b}
	c := TcfEmCmp{Val: r.u32(), Mask: r.u32(), Off: r.u16()}
	ab, lo := r.u8(), r.u8()
	c.Align, c.Flags = ab&0xF, ab>>4
	c.Layer, c.Opnd = lo&0xF, lo>>4
	return c, nil
}

// TcfEmNbyte is struct tcf_em_nbyte and the needle after it.
type TcfEmNbyte struct {
	Off    uint16
	Len    uint16 // len:12
	Layer  uint8  // layer:4
	Needle []byte
}

// DecodeTcfEmNbyte decodes an nbyte payload. One shorter than the header
// or than the header and its needle is ErrTcShort ("NByte header size
// mismatch", "NByte payload size mismatch").
func DecodeTcfEmNbyte(b []byte) (TcfEmNbyte, error) {
	if err := tcNeed(b, TcfEmNbyteSize, "tcf_em_nbyte"); err != nil {
		return TcfEmNbyte{}, err
	}
	r := tcRd{b: b}
	n := TcfEmNbyte{Off: r.u16()}
	// len:12 and layer:4 share a little-endian u16: len is the low byte
	// and the next byte's low nibble, layer that byte's high nibble.
	lo, hi := r.u8(), r.u8()
	n.Len = uint16(hi&0xF)<<8 | uint16(lo)
	n.Layer = hi >> 4
	end := TcfEmNbyteSize + int(n.Len)
	if len(b) < end {
		return n, tcNeed(b, end, "tcf_em_nbyte needle")
	}
	n.Needle = b[TcfEmNbyteSize:end]
	return n, nil
}

// CanFilter is struct can_filter.
type CanFilter struct {
	ID   uint32
	Mask uint32
}

// DecodeCanFilters decodes a canid payload: data_len / sizeof(struct
// can_filter) rules, a trailing partial one ignored, as canid_print_eopt
// counts them.
func DecodeCanFilters(b []byte) []CanFilter {
	rules := make([]CanFilter, len(b)/CanFilterSizeCst)
	r := tcRd{b: b}
	for i := range rules {
		rules[i] = CanFilter{ID: r.u32(), Mask: r.u32()}
	}
	return rules
}

// TcfMetaVal is struct tcf_meta_val.
type TcfMetaVal struct {
	Kind  uint16
	Shift uint8
	Op    uint8
}

// Type is TCF_META_TYPE(kind).
func (v TcfMetaVal) Type() uint16 { return v.Kind >> 12 & 0xF }

// ID is TCF_META_ID(kind).
func (v TcfMetaVal) ID() uint16 { return v.Kind & 0x7FF }

// TcEmMeta is a meta payload decoded for meta_print_eopt: Tb is
// parse_rtattr(tb, TCA_EM_META_MAX, data) (no flag masked), and the header
// when it is there and long enough.
type TcEmMeta struct {
	Tb       AttrTable
	Left     TcfMetaVal
	Right    TcfMetaVal
	HasHdr   bool
	HdrShort bool
}

// ParseTcEmMeta decodes a meta payload. A malformed attribute stream is
// the walk's error.
func ParseTcEmMeta(b []byte) (TcEmMeta, error) {
	var m TcEmMeta
	tb, err := ParseAttrTableFlags(b, TcaEmMetaMaxKnown, 0)
	if err != nil {
		return m, fmt.Errorf("meta: %w", err)
	}
	m.Tb = tb
	h := tb.Bytes(TcaEmMetaHdr)
	if h == nil {
		return m, nil
	}
	m.HasHdr = true
	if len(h) < TcfMetaHdrSizeCst {
		m.HdrShort = true
		return m, nil
	}
	r := tcRd{b: h}
	m.Left = TcfMetaVal{Kind: r.u16(), Shift: r.u8(), Op: r.u8()}
	m.Right = TcfMetaVal{Kind: r.u16(), Shift: r.u8(), Op: r.u8()}
	return m, nil
}
