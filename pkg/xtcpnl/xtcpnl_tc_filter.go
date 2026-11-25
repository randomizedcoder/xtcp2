package xtcpnl

import (
	"encoding/binary"
	"errors"
	"fmt"

	"golang.org/x/sys/unix"
)

// This file is the per-kind step between ParseTcMsg and a tc filter
// printer: a filter's TCA_OPTIONS decoded the way the kind's tc/f_*.c
// print_fopt reads it (iproute2 v7.2.0), and the requests tc_filter_list
// and tc_filter_get send (tc/tc_filter.c).
//
// Every printer starts with parse_rtattr_nested(tb, TCA_<KIND>_MAX, opt),
// which is Tb here. The fixed structures it casts are decoded beside it,
// each with a Short flag where tc's `RTA_PAYLOAD < sizeof` test fails: the
// printer has already printed its first fields by then, so a short
// structure is not an error of the whole decode. Where tc reads past a
// structure (u32's keys[i] and kcnts[i], L5 and F12), only the bytes that
// are there are decoded, and the count tc wanted is kept.

// Filter kinds gotc prints (tc/f_*.c filter_util.id).
const (
	TcFKindU32      = "u32"
	TcFKindFw       = "fw"
	TcFKindRoute    = "route"
	TcFKindBasic    = "basic"
	TcFKindCgroup   = "cgroup"
	TcFKindFlow     = "flow"
	TcFKindMatchall = "matchall"
)

// TCA_U32_* from include/uapi/linux/pkt_cls.h.
const (
	TcaU32Classid uint16 = 1  // TCA_U32_CLASSID: u32
	TcaU32Hash    uint16 = 2  // TCA_U32_HASH: u32
	TcaU32Link    uint16 = 3  // TCA_U32_LINK: u32
	TcaU32Divisor uint16 = 4  // TCA_U32_DIVISOR: u32
	TcaU32Sel     uint16 = 5  // TCA_U32_SEL: struct tc_u32_sel and its keys
	TcaU32Police  uint16 = 6  // TCA_U32_POLICE: nest
	TcaU32Act     uint16 = 7  // TCA_U32_ACT: nest of actions
	TcaU32Indev   uint16 = 8  // TCA_U32_INDEV: string
	TcaU32Pcnt    uint16 = 9  // TCA_U32_PCNT: struct tc_u32_pcnt and its counts
	TcaU32Mark    uint16 = 10 // TCA_U32_MARK: struct tc_u32_mark
	TcaU32Flags   uint16 = 11 // TCA_U32_FLAGS: u32
	TcaU32Pad     uint16 = 12 // TCA_U32_PAD
	// TcaU32MaxKnown is TCA_U32_MAX.
	TcaU32MaxKnown = TcaU32Pad

	// TC_U32_* sel flags (pkt_cls.h).
	TcU32Terminal  = 1
	TcU32Offset    = 2
	TcU32VarOffset = 4
	TcU32Eat       = 8

	// TCA_CLS_FLAGS_* (pkt_cls.h): u32's and matchall's FLAGS.
	TcaClsFlagsSkipHw  = 1 << 0
	TcaClsFlagsSkipSw  = 1 << 1
	TcaClsFlagsInHw    = 1 << 2
	TcaClsFlagsNotInHw = 1 << 3
)

// TCA_FW_* (pkt_cls.h).
const (
	TcaFwClassid uint16 = 1 // TCA_FW_CLASSID: u32
	TcaFwPolice  uint16 = 2 // TCA_FW_POLICE: nest
	TcaFwIndev   uint16 = 3 // TCA_FW_INDEV: string
	TcaFwAct     uint16 = 4 // TCA_FW_ACT: nest of actions
	TcaFwMask    uint16 = 5 // TCA_FW_MASK: u32
	// TcaFwMaxKnown is TCA_FW_MAX.
	TcaFwMaxKnown = TcaFwMask
)

// TCA_ROUTE4_* (pkt_cls.h).
const (
	TcaRoute4Classid uint16 = 1 // TCA_ROUTE4_CLASSID: u32
	TcaRoute4To      uint16 = 2 // TCA_ROUTE4_TO: u32 realm
	TcaRoute4From    uint16 = 3 // TCA_ROUTE4_FROM: u32 realm
	TcaRoute4Iif     uint16 = 4 // TCA_ROUTE4_IIF: u32 ifindex
	TcaRoute4Police  uint16 = 5 // TCA_ROUTE4_POLICE: nest
	TcaRoute4Act     uint16 = 6 // TCA_ROUTE4_ACT: nest of actions
	// TcaRoute4MaxKnown is TCA_ROUTE4_MAX.
	TcaRoute4MaxKnown = TcaRoute4Act
)

// TCA_BASIC_* (pkt_cls.h).
const (
	TcaBasicClassid  uint16 = 1 // TCA_BASIC_CLASSID: u32
	TcaBasicEmatches uint16 = 2 // TCA_BASIC_EMATCHES: ematch tree nest
	TcaBasicAct      uint16 = 3 // TCA_BASIC_ACT: nest of actions
	TcaBasicPolice   uint16 = 4 // TCA_BASIC_POLICE: nest
	TcaBasicPcnt     uint16 = 5 // TCA_BASIC_PCNT: struct tc_basic_pcnt, which tc never prints
	TcaBasicPad      uint16 = 6 // TCA_BASIC_PAD
	// TcaBasicMaxKnown is TCA_BASIC_MAX.
	TcaBasicMaxKnown = TcaBasicPad
)

// TCA_CGROUP_* (pkt_cls.h).
const (
	TcaCgroupAct      uint16 = 1 // TCA_CGROUP_ACT: nest of actions
	TcaCgroupPolice   uint16 = 2 // TCA_CGROUP_POLICE: nest
	TcaCgroupEmatches uint16 = 3 // TCA_CGROUP_EMATCHES: ematch tree nest
	// TcaCgroupMaxKnown is TCA_CGROUP_MAX.
	TcaCgroupMaxKnown = TcaCgroupEmatches
)

// TCA_FLOW_* and FLOW_* (pkt_cls.h).
const (
	TcaFlowKeys      uint16 = 1  // TCA_FLOW_KEYS: u32 key mask
	TcaFlowMode      uint16 = 2  // TCA_FLOW_MODE: u32
	TcaFlowBaseclass uint16 = 3  // TCA_FLOW_BASECLASS: u32
	TcaFlowRshift    uint16 = 4  // TCA_FLOW_RSHIFT: u32
	TcaFlowAddend    uint16 = 5  // TCA_FLOW_ADDEND: u32
	TcaFlowMask      uint16 = 6  // TCA_FLOW_MASK: u32
	TcaFlowXor       uint16 = 7  // TCA_FLOW_XOR: u32
	TcaFlowDivisor   uint16 = 8  // TCA_FLOW_DIVISOR: u32
	TcaFlowAct       uint16 = 9  // TCA_FLOW_ACT: nest of actions
	TcaFlowPolice    uint16 = 10 // TCA_FLOW_POLICE: nest
	TcaFlowEmatches  uint16 = 11 // TCA_FLOW_EMATCHES: ematch tree nest
	TcaFlowPerturb   uint16 = 12 // TCA_FLOW_PERTURB: u32 seconds
	// TcaFlowMaxKnown is TCA_FLOW_MAX.
	TcaFlowMaxKnown = TcaFlowPerturb

	// FlowModeMap and FlowModeHash are FLOW_MODE_MAP and FLOW_MODE_HASH.
	FlowModeMap  = 0
	FlowModeHash = 1
	// FlowKeyMaxCst is FLOW_KEY_MAX: FLOW_KEY_RXHASH, the 18th key.
	FlowKeyMaxCst = 17
)

// TCA_MATCHALL_* (pkt_cls.h).
const (
	TcaMatchallClassid uint16 = 1 // TCA_MATCHALL_CLASSID: u32
	TcaMatchallAct     uint16 = 2 // TCA_MATCHALL_ACT: nest of actions
	TcaMatchallFlags   uint16 = 3 // TCA_MATCHALL_FLAGS: u32
	TcaMatchallPcnt    uint16 = 4 // TCA_MATCHALL_PCNT: struct tc_matchall_pcnt
	TcaMatchallPad     uint16 = 5 // TCA_MATCHALL_PAD
	// TcaMatchallMaxKnown is TCA_MATCHALL_MAX.
	TcaMatchallMaxKnown = TcaMatchallPad
)

// The fixed structures' sizes.
const (
	// TcU32SelSizeCst is sizeof(struct tc_u32_sel): its header, no keys.
	TcU32SelSizeCst = 16
	// TcU32KeySizeCst is sizeof(struct tc_u32_key).
	TcU32KeySizeCst = 16
	// TcU32MarkSizeCst is sizeof(struct tc_u32_mark).
	TcU32MarkSizeCst = 12
	// TcU32PcntSizeCst is sizeof(struct tc_u32_pcnt): rcnt and rhit.
	TcU32PcntSizeCst = 16
	// TcMatchallPcntSizeCst is sizeof(struct tc_matchall_pcnt).
	TcMatchallPcntSizeCst = 8

	tcU64SizeCst = 8
)

// TcU32Key is struct tc_u32_key. Mask and Val are __be32 on the wire and
// kept as ntohl gives them; Off and Offmask are host ints.
type TcU32Key struct {
	Mask    uint32
	Val     uint32
	Off     int32
	Offmask int32
}

// DecodeTcU32Key decodes a struct tc_u32_key; short is ErrTcShort.
func DecodeTcU32Key(b []byte) (TcU32Key, error) {
	if err := tcNeed(b, TcU32KeySizeCst, "tc_u32_key"); err != nil {
		return TcU32Key{}, err
	}
	return decodeTcU32Key(b), nil
}

func decodeTcU32Key(b []byte) TcU32Key {
	if len(b) < TcU32KeySizeCst {
		return TcU32Key{}
	}
	r := tcRd{b: b, off: 2 * 4}
	return TcU32Key{
		Mask: binary.BigEndian.Uint32(b[0:4]),
		Val:  binary.BigEndian.Uint32(b[4:8]),
		Off:  r.s32(), Offmask: r.s32(),
	}
}

// TcU32Sel is struct tc_u32_sel. Offmask and Hmask are big-endian on the
// wire and kept as ntohs/ntohl give them. NKeys is the count the header
// claims; Keys holds only the keys within the payload, and KeysShort says
// tc's `for (i = 0; i < sel->nkeys; i++)` reads past it (L5).
type TcU32Sel struct {
	Flags     uint8
	Offshift  uint8
	NKeys     uint8
	Offmask   uint16
	Off       uint16
	Offoff    int16
	Hoff      int16
	Hmask     uint32
	Keys      []TcU32Key
	KeysShort bool
}

// DecodeTcU32Sel decodes a TCA_U32_SEL payload; one shorter than the
// header is ErrTcShort, where u32_print_opt returns -1.
func DecodeTcU32Sel(b []byte) (TcU32Sel, error) {
	if err := tcNeed(b, TcU32SelSizeCst, "tc_u32_sel"); err != nil {
		return TcU32Sel{}, err
	}
	r := tcRd{b: b}
	s := TcU32Sel{Flags: r.u8(), Offshift: r.u8(), NKeys: r.u8()}
	r.u8() // padding before offmask
	s.Offmask = binary.BigEndian.Uint16(b[4:6])
	r.off = 6
	s.Off = r.u16()
	s.Offoff = r.s16()
	s.Hoff = r.s16()
	s.Hmask = binary.BigEndian.Uint32(b[12:16])
	keys := b[TcU32SelSizeCst:]
	n := min(int(s.NKeys), len(keys)/TcU32KeySizeCst)
	s.Keys = make([]TcU32Key, n)
	for i := range s.Keys {
		s.Keys[i] = decodeTcU32Key(keys[i*TcU32KeySizeCst:])
	}
	s.KeysShort = n < int(s.NKeys)
	return s, nil
}

// TcU32Pcnt is struct tc_u32_pcnt. Kcnts holds the counts within the
// payload; tc reads kcnts[i] for every key the sel claims, past it when
// there are fewer (F12).
type TcU32Pcnt struct {
	Rcnt  uint64
	Rhit  uint64
	Kcnts []uint64
}

// DecodeTcU32Pcnt decodes a TCA_U32_PCNT payload; one shorter than rcnt
// and rhit is ErrTcShort ("Broken perf counters").
func DecodeTcU32Pcnt(b []byte) (TcU32Pcnt, error) {
	if err := tcNeed(b, TcU32PcntSizeCst, "tc_u32_pcnt"); err != nil {
		return TcU32Pcnt{}, err
	}
	r := tcRd{b: b}
	p := TcU32Pcnt{Rcnt: r.u64(), Rhit: r.u64()}
	p.Kcnts = make([]uint64, (len(b)-TcU32PcntSizeCst)/tcU64SizeCst)
	for i := range p.Kcnts {
		p.Kcnts[i] = r.u64()
	}
	return p, nil
}

// TcU32Mark is struct tc_u32_mark.
type TcU32Mark struct {
	Val     uint32
	Mask    uint32
	Success uint32
}

// DecodeTcU32Mark decodes a TCA_U32_MARK payload; short is ErrTcShort
// ("Invalid mark (kernel&iproute2 mismatch)").
func DecodeTcU32Mark(b []byte) (TcU32Mark, error) {
	if err := tcNeed(b, TcU32MarkSizeCst, "tc_u32_mark"); err != nil {
		return TcU32Mark{}, err
	}
	r := tcRd{b: b}
	return TcU32Mark{Val: r.u32(), Mask: r.u32(), Success: r.u32()}, nil
}

// TcFopt is a filter's TCA_OPTIONS decoded for its print_fopt. Tb is
// parse_rtattr_nested(tb, TCA_<KIND>_MAX, opt). For u32 and matchall the
// structures the printer casts are decoded too: Has says the attribute is
// there, Short that it fails tc's size test.
type TcFopt struct {
	Kind string
	Tb   AttrTable

	U32Sel       TcU32Sel
	HasU32Sel    bool
	U32SelShort  bool
	U32Pcnt      TcU32Pcnt
	HasU32Pcnt   bool
	U32PcntShort bool
	U32Mark      TcU32Mark
	HasU32Mark   bool
	U32MarkShort bool
	MatchallRhit uint64 // tc_matchall_pcnt.rhit
	HasMaPcnt    bool
	MaPcntShort  bool
}

// tcFoptMax is each slice kind's TCA_<KIND>_MAX.
var tcFoptMax = map[string]uint16{
	TcFKindU32:      TcaU32MaxKnown,
	TcFKindFw:       TcaFwMaxKnown,
	TcFKindRoute:    TcaRoute4MaxKnown,
	TcFKindBasic:    TcaBasicMaxKnown,
	TcFKindCgroup:   TcaCgroupMaxKnown,
	TcFKindFlow:     TcaFlowMaxKnown,
	TcFKindMatchall: TcaMatchallMaxKnown,
}

// ParseTcFopt decodes opt, a TCA_OPTIONS payload, for kind. An unknown
// kind is ErrTcKindUnknown; a malformed attribute stream is the walk's
// error.
func ParseTcFopt(kind string, opt []byte) (TcFopt, error) {
	f := TcFopt{Kind: kind}
	maxType, ok := tcFoptMax[kind]
	if !ok {
		return f, fmt.Errorf("%w: %q", ErrTcKindUnknown, kind)
	}
	tb, err := ParseAttrTableFlags(opt, maxType, tcNestedFlags)
	if err != nil {
		return f, err
	}
	f.Tb = tb
	switch kind {
	case TcFKindU32:
		f.parseU32()
	case TcFKindMatchall:
		if b := tb.Bytes(TcaMatchallPcnt); b != nil {
			f.HasMaPcnt = true
			if len(b) < TcMatchallPcntSizeCst {
				f.MaPcntShort = true
			} else {
				f.MatchallRhit = binary.LittleEndian.Uint64(b)
			}
		}
	}
	return f, nil
}

func (f *TcFopt) parseU32() {
	var err error
	if b := f.Tb.Bytes(TcaU32Sel); b != nil {
		f.HasU32Sel = true
		f.U32Sel, err = DecodeTcU32Sel(b)
		f.U32SelShort = errors.Is(err, ErrTcShort)
	}
	if b := f.Tb.Bytes(TcaU32Pcnt); b != nil {
		f.HasU32Pcnt = true
		f.U32Pcnt, err = DecodeTcU32Pcnt(b)
		f.U32PcntShort = errors.Is(err, ErrTcShort)
	}
	if b := f.Tb.Bytes(TcaU32Mark); b != nil {
		f.HasU32Mark = true
		f.U32Mark, err = DecodeTcU32Mark(b)
		f.U32MarkShort = errors.Is(err, ErrTcShort)
	}
}

// ErrTcFilterReqType is a filter request type other than RTM_GETTFILTER or
// RTM_GETCHAIN.
var ErrTcFilterReqType = errors.New("xtcpnl: tc filter request type is not RTM_GETTFILTER or RTM_GETCHAIN")

// TcaDumpFlagsTerse is TCA_DUMP_FLAGS_TERSE (include/uapi/linux/rtnetlink.h).
const TcaDumpFlagsTerse uint32 = 1 << 0

// TcFilterReq is the tcmsg and chain of a filter or chain request. Ifindex
// is TcmIfindexMagicBlock for a block, whose index is then Parent
// (tcm_block_index aliases tcm_parent). Protocol is the ethertype, as
// ParseTcMsg's FilterProtocol reads it back.
//
// tcm_info is TC_H_MAKE(prio << 16, protocol) in C's u32 arithmetic, so
// only Prio's low 16 bits survive: tc_filter_list never checks a pref
// against 0xFFFF, and `show pref 70000` asks for pref 0x1170 (L40).
type TcFilterReq struct {
	Ifindex  uint32
	Handle   uint32
	Parent   uint32
	Prio     uint32
	Protocol uint16
	Chain    uint32
	ChainSet bool
}

// tcFilterReqAttrsCst bounds a request's attributes: TCA_CHAIN, a
// TCA_KIND of at most FILTER_NAMESZ, and the kind's own.
const tcFilterReqAttrsCst = 256

func (q TcFilterReq) hdr() []byte {
	hdr := make([]byte, 0, TcMsgSizeCst)
	hdr = append(hdr, unix.AF_UNSPEC, 0, 0, 0) // tcm_family, tcm__pad1, tcm__pad2
	hdr = binary.LittleEndian.AppendUint32(hdr, q.Ifindex)
	hdr = binary.LittleEndian.AppendUint32(hdr, q.Handle)
	hdr = binary.LittleEndian.AppendUint32(hdr, q.Parent)
	// tcm_info: the low half holds htons(protocol), the high half prio's
	// low 16 bits; written as the bytes they are, so nothing is narrowed.
	hdr = binary.BigEndian.AppendUint16(hdr, q.Protocol)
	var prio [4]byte
	binary.LittleEndian.PutUint32(prio[:], q.Prio)
	return append(hdr, prio[0], prio[1])
}

func checkTcFilterType(msgType uint16) error {
	if msgType != uint16(unix.RTM_GETTFILTER) && msgType != uint16(unix.RTM_GETCHAIN) {
		return ErrTcFilterReqType
	}
	return nil
}

// BuildTcFilterDump builds tc_filter_list's dump (tc/tc_filter.c), for
// RTM_GETTFILTER (`filter show`) or RTM_GETCHAIN (`chain show`): the tcmsg,
// TCA_CHAIN when a chain was given, and under -br a TCA_DUMP_FLAGS
// bitfield of TCA_DUMP_FLAGS_TERSE, then rtnl_dump_request_n's NLM_F_DUMP.
func BuildTcFilterDump(msgType uint16, q TcFilterReq, terse bool, seq uint32) ([]byte, error) {
	if err := checkTcFilterType(msgType); err != nil {
		return nil, err
	}
	ab := NewAttrBuilder(make([]byte, tcFilterReqAttrsCst))
	if q.ChainSet {
		if err := ab.PutU32(TcaChain, q.Chain); err != nil {
			return nil, err
		}
	}
	if terse {
		bf := binary.LittleEndian.AppendUint32(nil, TcaDumpFlagsTerse) // value
		bf = binary.LittleEndian.AppendUint32(bf, TcaDumpFlagsTerse)   // selector
		if err := ab.PutBytes(TcaDumpFlags, bf); err != nil {
			return nil, err
		}
	}
	return BuildRequest(msgType, uint16(unix.NLM_F_DUMP), seq, q.hdr(), ab.Bytes())
}

// BuildTcFilterGet builds tc_filter_get's request (tc/tc_filter.c):
// NLM_F_ECHO, the tcmsg, TCA_CHAIN when a chain was given, then for
// RTM_GETTFILTER TCA_KIND (kind and its NUL) and kindOpts, the attributes
// the kind's parse_fopt appended (cgroup's empty TCA_OPTIONS, flow's nest
// with TCA_FLOW_MODE; nothing for the others). RTM_GETCHAIN sends no kind
// even when one was given (F9).
func BuildTcFilterGet(msgType uint16, q TcFilterReq, kind string, kindOpts []byte, seq uint32) ([]byte, error) {
	if err := checkTcFilterType(msgType); err != nil {
		return nil, err
	}
	ab := NewAttrBuilder(make([]byte, tcFilterReqAttrsCst+len(kindOpts)))
	if q.ChainSet {
		if err := ab.PutU32(TcaChain, q.Chain); err != nil {
			return nil, err
		}
	}
	attrs := ab.Bytes()
	if msgType == uint16(unix.RTM_GETTFILTER) {
		if err := ab.PutString(TcaKind, kind); err != nil {
			return nil, err
		}
		attrs = append(ab.Bytes(), kindOpts...)
	}
	return BuildRequest(msgType, uint16(unix.NLM_F_ECHO), seq, q.hdr(), attrs)
}
