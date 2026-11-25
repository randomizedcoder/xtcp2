package xtcpnl

import (
	"encoding/binary"

	"golang.org/x/sys/unix"
)

// This file decodes the options of twelve tc actions the way their printers
// in tc/m_<kind>.c read them (iproute2 v7.2.0): police, mirred, skbedit,
// vlan, csum, nat, simple, connmark, ctinfo, sample, skbmod and mpls. Each
// printer opens TCA_ACT_OPTIONS with parse_rtattr_nested(tb, TCA_<KIND>_MAX,
// arg), so the table is first-wins and NLA_F_NESTED is masked off.
//
// Every printer but police casts its parms struct from RTA_DATA without a
// length check (AC21), and mirred's BLOCKID and skbedit's FLAGS are read
// the same way (AC17). So each struct here is MIN-copied: the bytes that
// are there, the rest zero. ParmsLen keeps the length the kernel sent, so
// the printer can tell a short one from a full one.
//
// The requests are tc_act_list_or_flush's list and tc_action_gd's get
// (tc/m_action.c), RTM_GETACTION with a tcamsg.

// The action kinds' strings (tc/m_<kind>.c action_util.id).
const (
	TcActKindPolice   = "police"
	TcActKindMirred   = "mirred"
	TcActKindSkbedit  = "skbedit"
	TcActKindVlan     = "vlan"
	TcActKindCsum     = "csum"
	TcActKindNat      = "nat"
	TcActKindSimple   = "simple"
	TcActKindConnmark = "connmark"
	TcActKindCtinfo   = "ctinfo"
	TcActKindSample   = "sample"
	TcActKindSkbmod   = "skbmod"
	TcActKindMpls     = "mpls"
)

// TCA_POLICE_* from include/uapi/linux/pkt_cls.h.
const (
	TcaPoliceTbf        uint16 = 1  // TCA_POLICE_TBF: struct tc_police
	TcaPoliceRate       uint16 = 2  // TCA_POLICE_RATE: rate table
	TcaPolicePeakrate   uint16 = 3  // TCA_POLICE_PEAKRATE: rate table
	TcaPoliceAvrate     uint16 = 4  // TCA_POLICE_AVRATE: u32
	TcaPoliceResult     uint16 = 5  // TCA_POLICE_RESULT: u32
	TcaPoliceTm         uint16 = 6  // TCA_POLICE_TM: struct tcf_t
	TcaPolicePad        uint16 = 7  // TCA_POLICE_PAD
	TcaPoliceRate64     uint16 = 8  // TCA_POLICE_RATE64: u64
	TcaPolicePeakrate64 uint16 = 9  // TCA_POLICE_PEAKRATE64: u64
	TcaPolicePktrate64  uint16 = 10 // TCA_POLICE_PKTRATE64: u64
	TcaPolicePktburst64 uint16 = 11 // TCA_POLICE_PKTBURST64: u64
	TcaPoliceMaxKnown          = TcaPolicePktburst64
	TcPoliceSizeCst            = 56 // sizeof(struct tc_police)
)

// TCA_MIRRED_* from include/uapi/linux/tc_act/tc_mirred.h.
const (
	TcaMirredTm       uint16 = 1 // TCA_MIRRED_TM: struct tcf_t
	TcaMirredParms    uint16 = 2 // TCA_MIRRED_PARMS: struct tc_mirred
	TcaMirredPad      uint16 = 3 // TCA_MIRRED_PAD
	TcaMirredBlockid  uint16 = 4 // TCA_MIRRED_BLOCKID: u32
	TcaMirredMaxKnown        = TcaMirredBlockid
	TcMirredSizeCst          = 28 // sizeof(struct tc_mirred)

	// TCA_EGRESS_REDIR and friends, tc_mirred's eaction.
	TcaEgressRedirCst   = 1
	TcaEgressMirrorCst  = 2
	TcaIngressRedirCst  = 3
	TcaIngressMirrorCst = 4
)

// TCA_SKBEDIT_* from include/uapi/linux/tc_act/tc_skbedit.h.
const (
	TcaSkbeditTm              uint16 = 1  // TCA_SKBEDIT_TM: struct tcf_t
	TcaSkbeditParms           uint16 = 2  // TCA_SKBEDIT_PARMS: struct tc_skbedit
	TcaSkbeditPriority        uint16 = 3  // TCA_SKBEDIT_PRIORITY: u32
	TcaSkbeditQueueMapping    uint16 = 4  // TCA_SKBEDIT_QUEUE_MAPPING: u16
	TcaSkbeditMark            uint16 = 5  // TCA_SKBEDIT_MARK: u32
	TcaSkbeditPad             uint16 = 6  // TCA_SKBEDIT_PAD
	TcaSkbeditPtype           uint16 = 7  // TCA_SKBEDIT_PTYPE: u16
	TcaSkbeditMask            uint16 = 8  // TCA_SKBEDIT_MASK: u32
	TcaSkbeditFlags           uint16 = 9  // TCA_SKBEDIT_FLAGS: u64
	TcaSkbeditQueueMappingMax uint16 = 10 // TCA_SKBEDIT_QUEUE_MAPPING_MAX: u16
	TcaSkbeditMaxKnown               = TcaSkbeditQueueMappingMax
	// SKBEDIT_F_INHERITDSFIELD, the one flag tc prints.
	SkbeditFInheritDsfield = 1 << 0
)

// TCA_VLAN_* from include/uapi/linux/tc_act/tc_vlan.h.
const (
	TcaVlanTm               uint16 = 1 // TCA_VLAN_TM: struct tcf_t
	TcaVlanParms            uint16 = 2 // TCA_VLAN_PARMS: struct tc_vlan
	TcaVlanPushVlanID       uint16 = 3 // TCA_VLAN_PUSH_VLAN_ID: u16
	TcaVlanPushVlanProtocol uint16 = 4 // TCA_VLAN_PUSH_VLAN_PROTOCOL: be16
	TcaVlanPad              uint16 = 5 // TCA_VLAN_PAD
	TcaVlanPushVlanPriority uint16 = 6 // TCA_VLAN_PUSH_VLAN_PRIORITY: u8
	TcaVlanPushEthDst       uint16 = 7 // TCA_VLAN_PUSH_ETH_DST: ETH_ALEN
	TcaVlanPushEthSrc       uint16 = 8 // TCA_VLAN_PUSH_ETH_SRC: ETH_ALEN
	TcaVlanMaxKnown                = TcaVlanPushEthSrc
	TcVlanSizeCst                  = 24 // sizeof(struct tc_vlan)
)

// TCA_CSUM_* from include/uapi/linux/tc_act/tc_csum.h.
const (
	TcaCsumParms    uint16 = 1 // TCA_CSUM_PARMS: struct tc_csum
	TcaCsumTm       uint16 = 2 // TCA_CSUM_TM: struct tcf_t
	TcaCsumPad      uint16 = 3 // TCA_CSUM_PAD
	TcaCsumMaxKnown        = TcaCsumPad
	TcCsumSizeCst          = 24 // sizeof(struct tc_csum)
)

// TCA_NAT_* from include/uapi/linux/tc_act/tc_nat.h.
const (
	TcaNatParms    uint16 = 1 // TCA_NAT_PARMS: struct tc_nat
	TcaNatTm       uint16 = 2 // TCA_NAT_TM: struct tcf_t
	TcaNatPad      uint16 = 3 // TCA_NAT_PAD
	TcaNatMaxKnown        = TcaNatPad
	TcNatSizeCst          = 36 // sizeof(struct tc_nat)
	// TCA_NAT_FLAG_EGRESS.
	TcaNatFlagEgress = 1
)

// TCA_DEF_* from include/uapi/linux/tc_act/tc_defact.h: simple's.
const (
	TcaDefTm       uint16 = 1 // TCA_DEF_TM: struct tcf_t
	TcaDefParms    uint16 = 2 // TCA_DEF_PARMS: struct tc_defact
	TcaDefData     uint16 = 3 // TCA_DEF_DATA: string
	TcaDefPad      uint16 = 4 // TCA_DEF_PAD
	TcaDefMaxKnown        = TcaDefPad
)

// TCA_CONNMARK_* from include/uapi/linux/tc_act/tc_connmark.h.
const (
	TcaConnmarkParms    uint16 = 1 // TCA_CONNMARK_PARMS: struct tc_connmark
	TcaConnmarkTm       uint16 = 2 // TCA_CONNMARK_TM: struct tcf_t
	TcaConnmarkPad      uint16 = 3 // TCA_CONNMARK_PAD
	TcaConnmarkMaxKnown        = TcaConnmarkPad
	TcConnmarkSizeCst          = 24 // sizeof(struct tc_connmark)
)

// TCA_CTINFO_* from include/uapi/linux/tc_act/tc_ctinfo.h.
const (
	TcaCtinfoPad                uint16 = 1  // TCA_CTINFO_PAD
	TcaCtinfoTm                 uint16 = 2  // TCA_CTINFO_TM: struct tcf_t
	TcaCtinfoAct                uint16 = 3  // TCA_CTINFO_ACT: struct tc_ctinfo
	TcaCtinfoZone               uint16 = 4  // TCA_CTINFO_ZONE: u16
	TcaCtinfoParmsDscpMask      uint16 = 5  // TCA_CTINFO_PARMS_DSCP_MASK: u32
	TcaCtinfoParmsDscpStatemask uint16 = 6  // TCA_CTINFO_PARMS_DSCP_STATEMASK: u32
	TcaCtinfoParmsCpmarkMask    uint16 = 7  // TCA_CTINFO_PARMS_CPMARK_MASK: u32
	TcaCtinfoStatsDscpSet       uint16 = 8  // TCA_CTINFO_STATS_DSCP_SET: u64
	TcaCtinfoStatsDscpError     uint16 = 9  // TCA_CTINFO_STATS_DSCP_ERROR: u64
	TcaCtinfoStatsCpmarkSet     uint16 = 10 // TCA_CTINFO_STATS_CPMARK_SET: u64
	TcaCtinfoMaxKnown                  = TcaCtinfoStatsCpmarkSet
)

// TCA_SAMPLE_* from include/uapi/linux/tc_act/tc_sample.h.
const (
	TcaSampleTm           uint16 = 1 // TCA_SAMPLE_TM: struct tcf_t
	TcaSampleParms        uint16 = 2 // TCA_SAMPLE_PARMS: struct tc_sample
	TcaSampleRate         uint16 = 3 // TCA_SAMPLE_RATE: u32
	TcaSampleTruncSize    uint16 = 4 // TCA_SAMPLE_TRUNC_SIZE: u32
	TcaSamplePsampleGroup uint16 = 5 // TCA_SAMPLE_PSAMPLE_GROUP: u32
	TcaSamplePad          uint16 = 6 // TCA_SAMPLE_PAD
	TcaSampleMaxKnown            = TcaSamplePad
)

// TCA_SKBMOD_* from include/uapi/linux/tc_act/tc_skbmod.h.
const (
	TcaSkbmodTm       uint16 = 1 // TCA_SKBMOD_TM: struct tcf_t
	TcaSkbmodParms    uint16 = 2 // TCA_SKBMOD_PARMS: struct tc_skbmod
	TcaSkbmodDmac     uint16 = 3 // TCA_SKBMOD_DMAC: ETH_ALEN
	TcaSkbmodSmac     uint16 = 4 // TCA_SKBMOD_SMAC: ETH_ALEN
	TcaSkbmodEtype    uint16 = 5 // TCA_SKBMOD_ETYPE: u16
	TcaSkbmodPad      uint16 = 6 // TCA_SKBMOD_PAD
	TcaSkbmodMaxKnown        = TcaSkbmodPad
	// struct tc_skbmod: tc_gen, then a u64 at offset 24.
	TcSkbmodSizeCst     = 32
	tcSkbmodFlagsOffCst = 24
	SkbmodFSwapmac      = 1 << 3 // SKBMOD_F_SWAPMAC
	SkbmodFEcn          = 1 << 4 // SKBMOD_F_ECN
)

// TCA_MPLS_* from include/uapi/linux/tc_act/tc_mpls.h.
const (
	TcaMplsTm       uint16 = 1 // TCA_MPLS_TM: struct tcf_t
	TcaMplsParms    uint16 = 2 // TCA_MPLS_PARMS: struct tc_mpls
	TcaMplsPad      uint16 = 3 // TCA_MPLS_PAD
	TcaMplsProto    uint16 = 4 // TCA_MPLS_PROTO: be16
	TcaMplsLabel    uint16 = 5 // TCA_MPLS_LABEL: u32
	TcaMplsTC       uint16 = 6 // TCA_MPLS_TC: u8
	TcaMplsTTL      uint16 = 7 // TCA_MPLS_TTL: u8
	TcaMplsBos      uint16 = 8 // TCA_MPLS_BOS: u8
	TcaMplsMaxKnown        = TcaMplsBos
	TcMplsSizeCst          = 24 // sizeof(struct tc_mpls)
)

// TcGen is tc_gen, the five fields every action's parms struct opens with.
type TcGen struct {
	Index   uint32
	Capab   uint32
	Action  int32
	Refcnt  int32
	Bindcnt int32
}

func (r *tcRd) tcGen() TcGen {
	return TcGen{Index: r.u32(), Capab: r.u32(), Action: r.s32(), Refcnt: r.s32(), Bindcnt: r.s32()}
}

// TcActCommon is what every kind's decode shares: the printer's table, the
// parms attribute's length as sent (0 when absent) and the tcf_t.
type TcActCommon struct {
	Tb       AttrTable
	HasParms bool
	ParmsLen int
	Tm       TcfT
	HasTm    bool
}

// ParmsShort reports a parms attribute shorter than its struct: C reads
// past it (AC17, AC21).
func (c TcActCommon) ParmsShort(size int) bool { return c.HasParms && c.ParmsLen < size }

// parseActKind is parse_rtattr_nested(tb, max, arg), then the parms and tm
// attributes, the parms MIN-copied to size.
func parseActKind(opt []byte, maxType, parms, tm uint16, size int) (TcActCommon, *tcRd, error) {
	var c TcActCommon
	tb, err := ParseAttrTableFlags(opt, maxType, tcNestedFlags)
	if err != nil {
		return c, nil, err
	}
	c.Tb = tb
	r := &tcRd{b: make([]byte, size)}
	if parms != 0 && tb.Has(parms) {
		b := tb.Bytes(parms)
		c.HasParms, c.ParmsLen = true, len(b)
		r.b = TcMinCopy(b, size)
	}
	if tm != 0 && tb.Has(tm) {
		c.Tm, c.HasTm = DecodeTcfT(tb.Bytes(tm)), true
	}
	return c, r, nil
}

// TcPolice is struct tc_police.
type TcPolice struct {
	Index    uint32
	Action   int32
	Limit    uint32
	Burst    uint32
	Mtu      uint32
	Rate     TcRateSpec
	PeakRate TcRateSpec
	Refcnt   int32
	Bindcnt  int32
	Capab    uint32
}

// TcPoliceOpt is police's options. print_police checks TBF's length
// (it refuses one shorter than tc_police), so P is meaningful only when
// !ParmsShort(TcPoliceSizeCst).
type TcPoliceOpt struct {
	TcActCommon
	P TcPolice
}

// ParseTcPolice decodes police's options (tc/m_police.c print_police).
func ParseTcPolice(opt []byte) (TcPoliceOpt, error) {
	c, r, err := parseActKind(opt, TcaPoliceMaxKnown, TcaPoliceTbf, TcaPoliceTm, TcPoliceSizeCst)
	if err != nil {
		return TcPoliceOpt{}, err
	}
	p := TcPolice{Index: r.u32(), Action: r.s32(), Limit: r.u32(), Burst: r.u32(), Mtu: r.u32()}
	p.Rate = r.rateSpec()
	p.PeakRate = r.rateSpec()
	p.Refcnt, p.Bindcnt, p.Capab = r.s32(), r.s32(), r.u32()
	return TcPoliceOpt{TcActCommon: c, P: p}, nil
}

// TcMirred is struct tc_mirred.
type TcMirred struct {
	TcGen
	Eaction int32
	Ifindex uint32
}

// TcMirredOpt is mirred's options.
type TcMirredOpt struct {
	TcActCommon
	P TcMirred
}

// ParseTcMirred decodes mirred's options (tc/m_mirred.c print_mirred).
func ParseTcMirred(opt []byte) (TcMirredOpt, error) {
	c, r, err := parseActKind(opt, TcaMirredMaxKnown, TcaMirredParms, TcaMirredTm, TcMirredSizeCst)
	if err != nil {
		return TcMirredOpt{}, err
	}
	return TcMirredOpt{TcActCommon: c, P: TcMirred{TcGen: r.tcGen(), Eaction: r.s32(), Ifindex: r.u32()}}, nil
}

// TcGenOpt is the options of a kind whose parms struct is tc_gen alone:
// skbedit, simple (tc_defact), ctinfo (TCA_CTINFO_ACT) and sample.
type TcGenOpt struct {
	TcActCommon
	P TcGen
}

func parseGenKind(opt []byte, maxType, parms, tm uint16) (TcGenOpt, error) {
	c, r, err := parseActKind(opt, maxType, parms, tm, TcGactSizeCst)
	if err != nil {
		return TcGenOpt{}, err
	}
	return TcGenOpt{TcActCommon: c, P: r.tcGen()}, nil
}

// ParseTcSkbedit decodes skbedit's options (tc/m_skbedit.c print_skbedit).
func ParseTcSkbedit(opt []byte) (TcGenOpt, error) {
	return parseGenKind(opt, TcaSkbeditMaxKnown, TcaSkbeditParms, TcaSkbeditTm)
}

// ParseTcSimple decodes simple's options (tc/m_simple.c print_simple).
func ParseTcSimple(opt []byte) (TcGenOpt, error) {
	return parseGenKind(opt, TcaDefMaxKnown, TcaDefParms, TcaDefTm)
}

// ParseTcCtinfo decodes ctinfo's options (tc/m_ctinfo.c print_ctinfo).
func ParseTcCtinfo(opt []byte) (TcGenOpt, error) {
	return parseGenKind(opt, TcaCtinfoMaxKnown, TcaCtinfoAct, TcaCtinfoTm)
}

// ParseTcSample decodes sample's options (tc/m_sample.c print_sample).
func ParseTcSample(opt []byte) (TcGenOpt, error) {
	return parseGenKind(opt, TcaSampleMaxKnown, TcaSampleParms, TcaSampleTm)
}

// TcActInt is a parms struct that is tc_gen and one 32-bit field: tc_vlan's
// v_action, tc_mpls's m_action, tc_csum's update_flags.
type TcActInt struct {
	TcGen
	V uint32
}

// TcActIntOpt is the options of vlan, mpls or csum.
type TcActIntOpt struct {
	TcActCommon
	P TcActInt
}

func parseIntKind(opt []byte, maxType, parms, tm uint16, size int) (TcActIntOpt, error) {
	c, r, err := parseActKind(opt, maxType, parms, tm, size)
	if err != nil {
		return TcActIntOpt{}, err
	}
	return TcActIntOpt{TcActCommon: c, P: TcActInt{TcGen: r.tcGen(), V: r.u32()}}, nil
}

// ParseTcVlan decodes vlan's options (tc/m_vlan.c print_vlan): V is
// v_action.
func ParseTcVlan(opt []byte) (TcActIntOpt, error) {
	return parseIntKind(opt, TcaVlanMaxKnown, TcaVlanParms, TcaVlanTm, TcVlanSizeCst)
}

// ParseTcMpls decodes mpls's options (tc/m_mpls.c print_mpls): V is
// m_action.
func ParseTcMpls(opt []byte) (TcActIntOpt, error) {
	return parseIntKind(opt, TcaMplsMaxKnown, TcaMplsParms, TcaMplsTm, TcMplsSizeCst)
}

// ParseTcCsum decodes csum's options (tc/m_csum.c print_csum): V is
// update_flags.
func ParseTcCsum(opt []byte) (TcActIntOpt, error) {
	return parseIntKind(opt, TcaCsumMaxKnown, TcaCsumParms, TcaCsumTm, TcCsumSizeCst)
}

// TcNat is struct tc_nat; the addresses are as sent, network order.
type TcNat struct {
	TcGen
	OldAddr [4]byte
	NewAddr [4]byte
	Mask    [4]byte
	Flags   uint32
}

// TcNatOpt is nat's options.
type TcNatOpt struct {
	TcActCommon
	P TcNat
}

// ParseTcNat decodes nat's options (tc/m_nat.c print_nat).
func ParseTcNat(opt []byte) (TcNatOpt, error) {
	c, r, err := parseActKind(opt, TcaNatMaxKnown, TcaNatParms, TcaNatTm, TcNatSizeCst)
	if err != nil {
		return TcNatOpt{}, err
	}
	p := TcNat{TcGen: r.tcGen()}
	for _, a := range []*[4]byte{&p.OldAddr, &p.NewAddr, &p.Mask} {
		for i := range a {
			a[i] = r.u8()
		}
	}
	p.Flags = r.u32()
	return TcNatOpt{TcActCommon: c, P: p}, nil
}

// TcConnmark is struct tc_connmark.
type TcConnmark struct {
	TcGen
	Zone uint16
}

// TcConnmarkOpt is connmark's options.
type TcConnmarkOpt struct {
	TcActCommon
	P TcConnmark
}

// ParseTcConnmark decodes connmark's options (tc/m_connmark.c
// print_connmark).
func ParseTcConnmark(opt []byte) (TcConnmarkOpt, error) {
	c, r, err := parseActKind(opt, TcaConnmarkMaxKnown, TcaConnmarkParms, TcaConnmarkTm, TcConnmarkSizeCst)
	if err != nil {
		return TcConnmarkOpt{}, err
	}
	return TcConnmarkOpt{TcActCommon: c, P: TcConnmark{TcGen: r.tcGen(), Zone: r.u16()}}, nil
}

// TcSkbmod is struct tc_skbmod.
type TcSkbmod struct {
	TcGen
	Flags uint64
}

// TcSkbmodOpt is skbmod's options.
type TcSkbmodOpt struct {
	TcActCommon
	P TcSkbmod
}

// ParseTcSkbmod decodes skbmod's options (tc/m_skbmod.c print_skbmod).
func ParseTcSkbmod(opt []byte) (TcSkbmodOpt, error) {
	c, r, err := parseActKind(opt, TcaSkbmodMaxKnown, TcaSkbmodParms, TcaSkbmodTm, TcSkbmodSizeCst)
	if err != nil {
		return TcSkbmodOpt{}, err
	}
	p := TcSkbmod{TcGen: r.tcGen()}
	r.b = r.b[tcSkbmodFlagsOffCst-TcGactSizeCst:]
	p.Flags = r.u64()
	return TcSkbmodOpt{TcActCommon: c, P: p}, nil
}

// tcActReqAttrsCst bounds a list or get request's attributes: TAB, its
// order nest, KIND (IFNAMSIZ with its NUL), INDEX, ROOT_FLAGS and
// TIME_DELTA.
const tcActReqAttrsCst = 3*RTAttrSizeCst + 16 + 8 + 12 + 8

// TcActKindMaxCst is the longest kind tc sends: char k[16] filled by
// strncpy(k, *argv, sizeof(k) - 1), so 15 bytes (AC10).
const TcActKindMaxCst = 15

// TCA_ACT_FLAG_LARGE_DUMP_ON and TCA_ACT_FLAG_TERSE_DUMP, a list's
// TCA_ROOT_FLAGS bits.
const (
	TcaActFlagLargeDumpOn = 1 << 0
	TcaActFlagTerseDump   = 1 << 1
)

// BuildTcActionDump is tc_act_list_or_flush's list: RTM_GETACTION with
// NLM_F_DUMP (NLM_F_ROOT|NLM_F_MATCH, through rtnl_dump_request), a zero
// tcamsg, TCA_ACT_TAB holding order 1 with TCA_ACT_KIND, TCA_ROOT_FLAGS of
// LARGE_DUMP_ON (and TERSE_DUMP for -br) in both value and selector, and
// TCA_ROOT_TIME_DELTA when since was given. Neither nest carries
// NLA_F_NESTED.
func BuildTcActionDump(kind string, terse bool, since uint32, hasSince bool, seq uint32) ([]byte, error) {
	ab := NewAttrBuilder(make([]byte, tcActReqAttrsCst))
	if err := putActTab(&ab, kind, 0); err != nil {
		return nil, err
	}
	flags := uint32(TcaActFlagLargeDumpOn)
	if terse {
		flags |= TcaActFlagTerseDump
	}
	if err := ab.PutBytes(TcaRootFlags, appendBitfield32(nil, flags, flags)); err != nil {
		return nil, err
	}
	if hasSince {
		if err := ab.PutU32(TcaRootTimeDelta, since); err != nil {
			return nil, err
		}
	}
	return BuildRequest(uint16(unix.RTM_GETACTION), uint16(unix.NLM_F_DUMP), seq, make([]byte, TcaMsgSizeCst), ab.Bytes())
}

// BuildTcActionGet is tc_action_gd's get: RTM_GETACTION with
// NLM_F_REQUEST alone, a zero tcamsg and TCA_ACT_TAB holding order 1 with
// TCA_ACT_KIND and, unless index is 0, TCA_ACT_INDEX. With no kind (`tc
// actions get` or `get help`) the tab is empty (AC9).
func BuildTcActionGet(kind string, index, seq uint32) ([]byte, error) {
	ab := NewAttrBuilder(make([]byte, tcActReqAttrsCst))
	if err := putActTab(&ab, kind, index); err != nil {
		return nil, err
	}
	return BuildRequest(uint16(unix.RTM_GETACTION), 0, seq, make([]byte, TcaMsgSizeCst), ab.Bytes())
}

// putActTab writes TCA_ACT_TAB{1{KIND[, INDEX]}}, or an empty
// TCA_ACT_TAB for no kind.
func putActTab(ab *AttrBuilder, kind string, index uint32) error {
	tab, err := ab.BeginNest(TcaRootTab)
	if err != nil {
		return err
	}
	if kind != "" {
		ord, err := ab.BeginNest(1)
		if err != nil {
			return err
		}
		if len(kind) > TcActKindMaxCst {
			kind = kind[:TcActKindMaxCst]
		}
		if err := ab.PutString(TcaActKind, kind); err != nil {
			return err
		}
		if index != 0 {
			if err := ab.PutU32(TcaActIndex, index); err != nil {
				return err
			}
		}
		if err := ab.EndNest(ord); err != nil {
			return err
		}
	}
	return ab.EndNest(tab)
}

// appendBitfield32 appends a struct nla_bitfield32.
func appendBitfield32(b []byte, value, selector uint32) []byte {
	return binary.LittleEndian.AppendUint32(binary.LittleEndian.AppendUint32(b, value), selector)
}
