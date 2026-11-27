package xtcpnl

import (
	"encoding/binary"
	"errors"
	"fmt"

	"github.com/randomizedcoder/xtcp2/pkg/cint"
)

// This file decodes the AQM and leaf qdisc kinds (gotc 16P): red, gred,
// choke, sfb, codel, pie, fq_pie, hhf, cake, cake_mq, dualpi2, plug and
// skbprio, each read the way its tc/q_*.c printer reads it (iproute2
// v7.2.0). The constants and structures are include/uapi/linux/pkt_sched.h
// at linux v7.4-rc4.
//
// The decoders keep C's widths and walks, so the printers' quirks stay
// visible to gotc: red's FLAGS value lands in an unsigned char
// (q_red.c:180); gred reads VQ_STAT_BYTES, a u64 on the wire, as a u32
// (q_gred.c:333, Q70) and walks its VQ list by unaligned rta_len
// (q_gred.c:312). Where tc reads past an attribute, crashes or loops, the
// decoder returns an error or a flag gotc refuses on: a short scalar tc
// reads unchecked is ErrTcShort; gred without TCA_GRED_DPS is ErrTcMissing
// (Q67); a gred VQ entry with rta_len under 4 is ErrTcShort, where tc
// spins (Q71); a VQ_DP of MAX_DPs or more sets TcGred.VqDpOver, where tc
// writes past infos[] (Q68); a cake tin missing an attribute tin 0 has
// sets TcCakeXstats.TinGap, where tc's text table dereferences NULL
// (Q78).

// Qdisc kinds of increment 16 (tc/q_*.c qdisc_util.id).
const (
	TcKindRed     = "red"
	TcKindGred    = "gred"
	TcKindChoke   = "choke"
	TcKindSfb     = "sfb"
	TcKindCodel   = "codel"
	TcKindPie     = "pie"
	TcKindFqPie   = "fq_pie"
	TcKindHhf     = "hhf"
	TcKindCake    = "cake"
	TcKindCakeMq  = "cake_mq"
	TcKindDualpi2 = "dualpi2"
	TcKindPlug    = "plug"
	TcKindSkbprio = "skbprio"
)

// Structure sizes (include/uapi/linux/pkt_sched.h).
const (
	// TcRedQoptSizeCst is sizeof(struct tc_red_qopt) and of struct
	// tc_choke_qopt: three u32 and four unsigned char.
	TcRedQoptSizeCst = 16
	// TcRedXstatsSizeCst is sizeof(struct tc_red_xstats): four u32.
	TcRedXstatsSizeCst = 16
	// TcChokeXstatsSizeCst is sizeof(struct tc_choke_xstats): five u32.
	TcChokeXstatsSizeCst = 20
	// TcGredQoptSizeCst is sizeof(struct tc_gred_qopt): ten u32, four u8
	// and two u32.
	TcGredQoptSizeCst = 52
	// TcGredSoptSizeCst is sizeof(struct tc_gred_sopt): two u32, two u8
	// and a u16.
	TcGredSoptSizeCst = 12
	// TcSfbQoptSizeCst is sizeof(struct tc_sfb_qopt): nine u32.
	TcSfbQoptSizeCst = 36
	// TcSfbXstatsSizeCst is sizeof(struct tc_sfb_xstats): nine u32.
	TcSfbXstatsSizeCst = 36
	// TcCodelXstatsSizeCst is sizeof(struct tc_codel_xstats): nine
	// 32-bit fields.
	TcCodelXstatsSizeCst = 36
	// TcPieXstatsSizeCst is sizeof(struct tc_pie_xstats): a u64 and eight
	// u32.
	TcPieXstatsSizeCst = 40
	// TcFqPieXstatsSizeCst is sizeof(struct tc_fq_pie_xstats): nine u32.
	TcFqPieXstatsSizeCst = 36
	// TcHhfXstatsSizeCst is sizeof(struct tc_hhf_xstats): four u32.
	TcHhfXstatsSizeCst = 16
	// TcDualpi2XstatsSizeCst is sizeof(struct tc_dualpi2_xstats): twelve
	// 32-bit fields.
	TcDualpi2XstatsSizeCst = 48
	// TcSkbprioQoptSizeCst is sizeof(struct tc_skbprio_qopt): u32 limit.
	TcSkbprioQoptSizeCst = 4

	// GredMaxDPsCst is MAX_DPs.
	GredMaxDPsCst = 16
	// CakeMaxTinsCst is TC_CAKE_MAX_TINS.
	CakeMaxTinsCst = 8
	// SfbMaxProbCst is SFB_MAX_PROB.
	SfbMaxProbCst = 0xFFFF
	// SkbprioMaxPriorityCst is SKBPRIO_MAX_PRIORITY.
	SkbprioMaxPriorityCst = 64
)

// TC_RED_* flags (struct tc_red_qopt.flags).
const (
	TcRedEcn        = 1
	TcRedHarddrop   = 2
	TcRedAdaptative = 4
	TcRedNodrop     = 8
)

// TCA_RED_* attributes.
const (
	TcaRedParms          uint16 = 1
	TcaRedStab           uint16 = 2
	TcaRedMaxP           uint16 = 3
	TcaRedFlags          uint16 = 4
	TcaRedEarlyDropBlock uint16 = 5
	TcaRedMarkBlock      uint16 = 6
	// TcaRedMaxKnown is TCA_RED_MAX.
	TcaRedMaxKnown = TcaRedMarkBlock
)

// TCA_CHOKE_* attributes.
const (
	TcaChokeParms uint16 = 1
	TcaChokeStab  uint16 = 2
	TcaChokeMaxP  uint16 = 3
	// TcaChokeMaxKnown is TCA_CHOKE_MAX.
	TcaChokeMaxKnown = TcaChokeMaxP
)

// TCA_GRED_* attributes.
const (
	TcaGredParms  uint16 = 1
	TcaGredStab   uint16 = 2
	TcaGredDps    uint16 = 3
	TcaGredMaxP   uint16 = 4
	TcaGredLimit  uint16 = 5
	TcaGredVqList uint16 = 6
	// TcaGredMaxKnown is TCA_GRED_MAX.
	TcaGredMaxKnown = TcaGredVqList

	// TcaGredVqEntry is TCA_GRED_VQ_ENTRY.
	TcaGredVqEntry uint16 = 1
)

// TCA_GRED_VQ_* attributes.
const (
	TcaGredVqPad            uint16 = 1
	TcaGredVqDp             uint16 = 2
	TcaGredVqStatBytes      uint16 = 3
	TcaGredVqStatPackets    uint16 = 4
	TcaGredVqStatBacklog    uint16 = 5
	TcaGredVqStatProbDrop   uint16 = 6
	TcaGredVqStatProbMark   uint16 = 7
	TcaGredVqStatForcedDrop uint16 = 8
	TcaGredVqStatForcedMark uint16 = 9
	TcaGredVqStatPdrop      uint16 = 10
	TcaGredVqStatOther      uint16 = 11
	TcaGredVqFlags          uint16 = 12
	// TcaGredVqMaxKnown is TCA_GRED_VQ_MAX.
	TcaGredVqMaxKnown = TcaGredVqFlags
)

// TCA_SFB_PARMS, the only sfb attribute.
const (
	TcaSfbParms uint16 = 1
	// TcaSfbMaxKnown is TCA_SFB_MAX.
	TcaSfbMaxKnown = TcaSfbParms
)

// TCA_CODEL_* attributes.
const (
	TcaCodelTarget      uint16 = 1
	TcaCodelLimit       uint16 = 2
	TcaCodelInterval    uint16 = 3
	TcaCodelEcn         uint16 = 4
	TcaCodelCeThreshold uint16 = 5
	// TcaCodelMaxKnown is TCA_CODEL_MAX.
	TcaCodelMaxKnown = TcaCodelCeThreshold
)

// TCA_PIE_* attributes.
const (
	TcaPieTarget          uint16 = 1
	TcaPieLimit           uint16 = 2
	TcaPieTupdate         uint16 = 3
	TcaPieAlpha           uint16 = 4
	TcaPieBeta            uint16 = 5
	TcaPieEcn             uint16 = 6
	TcaPieBytemode        uint16 = 7
	TcaPieDqRateEstimator uint16 = 8
	// TcaPieMaxKnown is TCA_PIE_MAX.
	TcaPieMaxKnown = TcaPieDqRateEstimator
)

// TCA_FQ_PIE_* attributes.
const (
	TcaFqPieLimit           uint16 = 1
	TcaFqPieFlows           uint16 = 2
	TcaFqPieTarget          uint16 = 3
	TcaFqPieTupdate         uint16 = 4
	TcaFqPieAlpha           uint16 = 5
	TcaFqPieBeta            uint16 = 6
	TcaFqPieQuantum         uint16 = 7
	TcaFqPieMemoryLimit     uint16 = 8
	TcaFqPieEcnProb         uint16 = 9
	TcaFqPieEcn             uint16 = 10
	TcaFqPieBytemode        uint16 = 11
	TcaFqPieDqRateEstimator uint16 = 12
	// TcaFqPieMaxKnown is TCA_FQ_PIE_MAX.
	TcaFqPieMaxKnown = TcaFqPieDqRateEstimator
)

// TCA_HHF_* attributes.
const (
	TcaHhfBacklogLimit uint16 = 1
	TcaHhfQuantum      uint16 = 2
	TcaHhfHhFlowsLimit uint16 = 3
	TcaHhfResetTimeout uint16 = 4
	TcaHhfAdmitBytes   uint16 = 5
	TcaHhfEvictTimeout uint16 = 6
	TcaHhfNonHhWeight  uint16 = 7
	// TcaHhfMaxKnown is TCA_HHF_MAX.
	TcaHhfMaxKnown = TcaHhfNonHhWeight
)

// TCA_CAKE_* attributes.
const (
	TcaCakePad          uint16 = 1
	TcaCakeBaseRate64   uint16 = 2
	TcaCakeDiffservMode uint16 = 3
	TcaCakeAtm          uint16 = 4
	TcaCakeFlowMode     uint16 = 5
	TcaCakeOverhead     uint16 = 6
	TcaCakeRtt          uint16 = 7
	TcaCakeTarget       uint16 = 8
	TcaCakeAutorate     uint16 = 9
	TcaCakeMemory       uint16 = 10
	TcaCakeNat          uint16 = 11
	TcaCakeRaw          uint16 = 12
	TcaCakeWash         uint16 = 13
	TcaCakeMpu          uint16 = 14
	TcaCakeIngress      uint16 = 15
	TcaCakeAckFilter    uint16 = 16
	TcaCakeSplitGso     uint16 = 17
	TcaCakeFwmark       uint16 = 18
	// TcaCakeMaxKnown is TCA_CAKE_MAX.
	TcaCakeMaxKnown = TcaCakeFwmark
)

// TCA_CAKE_STATS_* attributes.
const (
	TcaCakeStatsPad                uint16 = 1
	TcaCakeStatsCapacityEstimate64 uint16 = 2
	TcaCakeStatsMemoryLimit        uint16 = 3
	TcaCakeStatsMemoryUsed         uint16 = 4
	TcaCakeStatsAvgNetoff          uint16 = 5
	TcaCakeStatsMinNetlen          uint16 = 6
	TcaCakeStatsMaxNetlen          uint16 = 7
	TcaCakeStatsMinAdjlen          uint16 = 8
	TcaCakeStatsMaxAdjlen          uint16 = 9
	TcaCakeStatsTinStats           uint16 = 10
	TcaCakeStatsDeficit            uint16 = 11
	TcaCakeStatsCobaltCount        uint16 = 12
	TcaCakeStatsDropping           uint16 = 13
	TcaCakeStatsDropNextUs         uint16 = 14
	TcaCakeStatsPDrop              uint16 = 15
	TcaCakeStatsBlueTimerUs        uint16 = 16
	TcaCakeStatsActiveQueues       uint16 = 17
	// TcaCakeStatsMaxKnown is TCA_CAKE_STATS_MAX.
	TcaCakeStatsMaxKnown = TcaCakeStatsActiveQueues
)

// TCA_CAKE_TIN_STATS_* attributes.
const (
	TcaCakeTinStatsPad                uint16 = 1
	TcaCakeTinStatsSentPackets        uint16 = 2
	TcaCakeTinStatsSentBytes64        uint16 = 3
	TcaCakeTinStatsDroppedPackets     uint16 = 4
	TcaCakeTinStatsDroppedBytes64     uint16 = 5
	TcaCakeTinStatsAcksDroppedPackets uint16 = 6
	TcaCakeTinStatsAcksDroppedBytes64 uint16 = 7
	TcaCakeTinStatsEcnMarkedPackets   uint16 = 8
	TcaCakeTinStatsEcnMarkedBytes64   uint16 = 9
	TcaCakeTinStatsBacklogPackets     uint16 = 10
	TcaCakeTinStatsBacklogBytes       uint16 = 11
	TcaCakeTinStatsThresholdRate64    uint16 = 12
	TcaCakeTinStatsTargetUs           uint16 = 13
	TcaCakeTinStatsIntervalUs         uint16 = 14
	TcaCakeTinStatsWayIndirectHits    uint16 = 15
	TcaCakeTinStatsWayMisses          uint16 = 16
	TcaCakeTinStatsWayCollisions      uint16 = 17
	TcaCakeTinStatsPeakDelayUs        uint16 = 18
	TcaCakeTinStatsAvgDelayUs         uint16 = 19
	TcaCakeTinStatsBaseDelayUs        uint16 = 20
	TcaCakeTinStatsSparseFlows        uint16 = 21
	TcaCakeTinStatsBulkFlows          uint16 = 22
	TcaCakeTinStatsUnresponsiveFlows  uint16 = 23
	TcaCakeTinStatsMaxSkblen          uint16 = 24
	TcaCakeTinStatsFlowQuantum        uint16 = 25
	TcaCakeTinStatsMaxKnown                  = TcaCakeTinStatsFlowQuantum // TCA_CAKE_TIN_STATS_MAX
	tcaCakeTinStatsMaxKnownSlotsCst          = int(TcaCakeTinStatsMaxKnown) + 1
)

// CAKE_* enums.
const (
	CakeFlowNone      = 0
	CakeFlowSrcIP     = 1
	CakeFlowDstIP     = 2
	CakeFlowHosts     = 3
	CakeFlowFlows     = 4
	CakeFlowDualSrc   = 5
	CakeFlowDualDst   = 6
	CakeFlowTriple    = 7
	CakeFlowMax       = 8
	CakeDiffserv3     = 0
	CakeDiffserv4     = 1
	CakeDiffserv8     = 2
	CakeBesteffort    = 3
	CakePrecedence    = 4
	CakeDiffservMax   = 5
	CakeAckNone       = 0
	CakeAckFilter     = 1
	CakeAckAggressive = 2
	CakeAtmNone       = 0
	CakeAtmAtm        = 1
	CakeAtmPtm        = 2
)

// TCA_DUALPI2_* attributes.
const (
	TcaDualpi2Limit          uint16 = 1
	TcaDualpi2MemoryLimit    uint16 = 2
	TcaDualpi2Target         uint16 = 3
	TcaDualpi2Tupdate        uint16 = 4
	TcaDualpi2Alpha          uint16 = 5
	TcaDualpi2Beta           uint16 = 6
	TcaDualpi2StepThreshPkts uint16 = 7
	TcaDualpi2StepThreshUs   uint16 = 8
	TcaDualpi2MinQlenStep    uint16 = 9
	TcaDualpi2Coupling       uint16 = 10
	TcaDualpi2DropOverload   uint16 = 11
	TcaDualpi2DropEarly      uint16 = 12
	TcaDualpi2CProtection    uint16 = 13
	TcaDualpi2EcnMask        uint16 = 14
	TcaDualpi2SplitGso       uint16 = 15
	TcaDualpi2Pad            uint16 = 16
	// TcaDualpi2MaxKnown is TCA_DUALPI2_MAX.
	TcaDualpi2MaxKnown = TcaDualpi2Pad
)

// TC_DUALPI2_ECN_MASK_* values.
const (
	TcDualpi2EcnMaskL4sEct = 1
	TcDualpi2EcnMaskClaEct = 2
	TcDualpi2EcnMaskAnyEct = 3
)

// ErrTcGredSmall is gred's "GRED received message smaller than expected":
// TCA_GRED_DPS or TCA_GRED_PARMS short, which tc prints to stdout, inside
// any open JSON object, before returning -1 (q_gred.c:440, Q72).
var ErrTcGredSmall = fmt.Errorf("%w: gred message smaller than expected", ErrTcShort)

// ErrTcVals is a TcVals table over a kind with more attributes than it
// holds; no uapi kind here has.
var ErrTcVals = errors.New("xtcpnl: tc value table too small")

// tcValsSlotsCst is TcVals' capacity: every kind's MAX here is under it.
const tcValsSlotsCst = 32

// TcVals is a kind's scalar attributes as its printer reads them: V[t]
// holds attribute t's value at the width tc reads, Has bit t says tc read
// it (the attribute present and, for the guarded reads, at least that
// wide). Signed fields hold their bits; take them with S32.
type TcVals struct {
	V   [tcValsSlotsCst]uint64
	Has uint32
}

// Get is attribute t's value and whether tc read it.
func (v *TcVals) Get(t uint16) (uint64, bool) {
	if int(t) >= tcValsSlotsCst || v.Has&(1<<t) == 0 {
		return 0, false
	}
	return v.V[t], true
}

// U32 is Get for a u32 read.
func (v *TcVals) U32(t uint16) (uint32, bool) {
	x, ok := v.Get(t)
	return uint32(x & 0xffffffff), ok
}

// U8 is Get for a u8 read.
func (v *TcVals) U8(t uint16) (uint8, bool) {
	x, ok := v.Get(t)
	return uint8(x & 0xff), ok
}

// S32 is Get for an s32 read (`*(__s32 *)RTA_DATA`).
func (v *TcVals) S32(t uint16) (int32, bool) {
	x, ok := v.U32(t)
	return cint.Int(x), ok
}

// tcPresenceCst marks an attribute tc tests for presence only (cake's RAW).
const tcPresenceCst = -1

// loose fills v from tb as the printers' `if (tb[t] && RTA_PAYLOAD(tb[t])
// >= sizeof(...)) v = rta_getattr_*(tb[t])` reads: widths[t] is the read's
// width, 0 for an attribute tc does not read, tcPresenceCst for `if
// (tb[t])`. A present attribute shorter than its width is skipped, as tc
// skips it; short gets its bit.
func (v *TcVals) loose(tb AttrTable, widths []int8) (short uint32, err error) {
	if len(widths) > tcValsSlotsCst {
		return 0, ErrTcVals
	}
	for t := uint16(0); int(t) < len(widths); t++ {
		w := widths[t]
		if w == 0 || !tb.Has(t) {
			continue
		}
		if w == tcPresenceCst {
			v.Has |= 1 << t
			continue
		}
		b := tb.Bytes(t)
		if len(b) < int(w) {
			short |= 1 << t
			continue
		}
		v.V[t] = leUint(b, int(w))
		v.Has |= 1 << t
	}
	return short, nil
}

// leUint reads the first w (1, 4 or 8) bytes of b little-endian, as
// rta_getattr_u8/u32/u64 read a longer payload's head.
func leUint(b []byte, w int) uint64 {
	if len(b) < w {
		return 0
	}
	switch w {
	case 1:
		return uint64(b[0])
	case 4:
		return uint64(binary.LittleEndian.Uint32(b))
	default:
		return binary.LittleEndian.Uint64(b)
	}
}

// Read widths per kind, indexed by attribute: each printer's guarded
// reads.
var (
	codelWidths = []int8{TcaCodelTarget: 4, TcaCodelLimit: 4, TcaCodelInterval: 4, TcaCodelEcn: 4,
		TcaCodelCeThreshold: 4}
	pieWidths = []int8{TcaPieTarget: 4, TcaPieLimit: 4, TcaPieTupdate: 4, TcaPieAlpha: 4, TcaPieBeta: 4,
		TcaPieEcn: 4, TcaPieBytemode: 4, TcaPieDqRateEstimator: 4}
	fqPieWidths = []int8{TcaFqPieLimit: 4, TcaFqPieFlows: 4, TcaFqPieTarget: 4, TcaFqPieTupdate: 4,
		TcaFqPieAlpha: 4, TcaFqPieBeta: 4, TcaFqPieQuantum: 4, TcaFqPieMemoryLimit: 4, TcaFqPieEcnProb: 4,
		TcaFqPieEcn: 4, TcaFqPieBytemode: 4, TcaFqPieDqRateEstimator: 4}
	hhfWidths = []int8{TcaHhfBacklogLimit: 4, TcaHhfQuantum: 4, TcaHhfHhFlowsLimit: 4, TcaHhfResetTimeout: 4,
		TcaHhfAdmitBytes: 4, TcaHhfEvictTimeout: 4, TcaHhfNonHhWeight: 4}
	// dualpi2's u8 attributes are guarded by sizeof(__u8).
	dualpi2Widths = []int8{TcaDualpi2Limit: 4, TcaDualpi2MemoryLimit: 4, TcaDualpi2Target: 4,
		TcaDualpi2Tupdate: 4, TcaDualpi2Alpha: 4, TcaDualpi2Beta: 4, TcaDualpi2StepThreshPkts: 4,
		TcaDualpi2StepThreshUs: 4, TcaDualpi2MinQlenStep: 4, TcaDualpi2Coupling: 1, TcaDualpi2DropOverload: 1,
		TcaDualpi2DropEarly: 1, TcaDualpi2CProtection: 1, TcaDualpi2EcnMask: 1, TcaDualpi2SplitGso: 1,
		TcaDualpi2Pad: 0}
	// cake reads no TARGET (K38) and tests RAW for presence.
	cakeWidths = []int8{TcaCakeBaseRate64: 8, TcaCakeDiffservMode: 4, TcaCakeAtm: 4, TcaCakeFlowMode: 4,
		TcaCakeOverhead: 4, TcaCakeRtt: 4, TcaCakeTarget: 0, TcaCakeAutorate: 4, TcaCakeMemory: 4, TcaCakeNat: 4,
		TcaCakeRaw: tcPresenceCst, TcaCakeWash: 4, TcaCakeMpu: 4, TcaCakeIngress: 4, TcaCakeAckFilter: 4,
		TcaCakeSplitGso: 4, TcaCakeFwmark: 4}
	// cake's stats getters are unchecked (GET_STAT_*, q_cake.c:627-629);
	// the tin stats nest is walked separately.
	cakeStatsWidths = []int8{TcaCakeStatsCapacityEstimate64: 8, TcaCakeStatsMemoryLimit: 4,
		TcaCakeStatsMemoryUsed: 4, TcaCakeStatsAvgNetoff: 4, TcaCakeStatsMinNetlen: 4, TcaCakeStatsMaxNetlen: 4,
		TcaCakeStatsMinAdjlen: 4, TcaCakeStatsMaxAdjlen: 4, TcaCakeStatsTinStats: 0, TcaCakeStatsDeficit: 4,
		TcaCakeStatsCobaltCount: 4, TcaCakeStatsDropping: 4, TcaCakeStatsDropNextUs: 4, TcaCakeStatsPDrop: 4,
		TcaCakeStatsBlueTimerUs: 4, TcaCakeStatsActiveQueues: 4}
	// The twenty tin attributes tc prints, text and JSON alike.
	cakeTinWidths = []int8{TcaCakeTinStatsSentPackets: 4, TcaCakeTinStatsSentBytes64: 8,
		TcaCakeTinStatsDroppedPackets: 4, TcaCakeTinStatsAcksDroppedPackets: 4,
		TcaCakeTinStatsEcnMarkedPackets: 4, TcaCakeTinStatsBacklogBytes: 4, TcaCakeTinStatsThresholdRate64: 8,
		TcaCakeTinStatsTargetUs: 4, TcaCakeTinStatsIntervalUs: 4, TcaCakeTinStatsWayIndirectHits: 4,
		TcaCakeTinStatsWayMisses: 4, TcaCakeTinStatsWayCollisions: 4, TcaCakeTinStatsPeakDelayUs: 4,
		TcaCakeTinStatsAvgDelayUs: 4, TcaCakeTinStatsBaseDelayUs: 4, TcaCakeTinStatsSparseFlows: 4,
		TcaCakeTinStatsBulkFlows: 4, TcaCakeTinStatsUnresponsiveFlows: 4, TcaCakeTinStatsMaxSkblen: 4,
		TcaCakeTinStatsFlowQuantum: 4}
)

// TcRedQopt is struct tc_red_qopt, and struct tc_choke_qopt, which has
// the same layout.
type TcRedQopt struct {
	Limit    uint32
	QthMin   uint32
	QthMax   uint32
	Wlog     uint8
	Plog     uint8
	ScellLog uint8
	Flags    uint8
}

// DecodeTcRedQopt decodes a struct tc_red_qopt.
func DecodeTcRedQopt(b []byte) (TcRedQopt, error) {
	if err := tcNeed(b, TcRedQoptSizeCst, "tc_red_qopt"); err != nil {
		return TcRedQopt{}, err
	}
	r := tcRd{b: b}
	return TcRedQopt{Limit: r.u32(), QthMin: r.u32(), QthMax: r.u32(), Wlog: r.u8(), Plog: r.u8(),
		ScellLog: r.u8(), Flags: r.u8()}, nil
}

// TcRed is red_print_opt's and choke_print_opt's reads. Qopt.Flags is
// TCA_RED_FLAGS' value when that is at least a struct nla_bitfield32,
// truncated to the unsigned char it is stored in. EarlyDrop and Mark are
// red's qevent blocks (tc_qevent.c read_block_idx), read unchecked.
type TcRed struct {
	Qopt         TcRedQopt
	MaxP         uint32 // 0 when absent or short, as tc's max_P
	EarlyDrop    uint32
	HasEarlyDrop bool
	Mark         uint32
	HasMark      bool
}

// TcGredQopt is struct tc_gred_qopt, one per DP slot.
type TcGredQopt struct {
	Limit    uint32
	QthMin   uint32
	QthMax   uint32
	DP       uint32
	Backlog  uint32
	Qave     uint32
	Forced   uint32
	Early    uint32
	Other    uint32
	Pdrop    uint32
	Wlog     uint8
	Plog     uint8
	ScellLog uint8
	Prio     uint8
	Packets  uint32
	Bytesin  uint32
}

// TcGredSopt is struct tc_gred_sopt.
type TcGredSopt struct {
	DPs   uint32
	DefDP uint32
	Grio  uint8
	Flags uint8
	Pad1  uint16
}

// TcGredInfo is q_gred.c's struct tc_gred_info, one VQ entry as
// gred_parse_vqs reads it. Bytes is tc's u32 read of VQ_STAT_BYTES (Q70);
// BytesWire is the u64 the kernel sent, when 8 bytes are there.
type TcGredInfo struct {
	FlagsPresent bool
	Bytes        uint32
	BytesWire    uint64
	HasBytesWire bool
	Packets      uint32
	Backlog      uint32
	ProbDrop     uint32
	ProbMark     uint32
	ForcedDrop   uint32
	ForcedMark   uint32
	Pdrop        uint32
	Other        uint32
	Flags        uint32
}

// TcGred is gred_print_opt's reads. Warn holds the stderr lines
// gred_parse_vqs prints for an entry it skips, in order.
type TcGred struct {
	Sopt     TcGredSopt
	Qopts    [GredMaxDPsCst]TcGredQopt
	MaxP     [GredMaxDPsCst]uint32
	HasMaxP  bool // at least MAX_DPs u32
	Limit    uint32
	HasLimit bool // exactly a u32
	Info     [GredMaxDPsCst]TcGredInfo
	HasVQ    bool
	VqDpOver bool // a VQ_DP of MAX_DPs or more: tc writes past infos[] (Q68)
	Warn     []string
}

// TcSfbQopt is struct tc_sfb_qopt.
type TcSfbQopt struct {
	RehashInterval uint32
	WarmupTime     uint32
	Max            uint32
	BinSize        uint32
	Increment      uint32
	Decrement      uint32
	Limit          uint32
	PenaltyRate    uint32
	PenaltyBurst   uint32
}

// DecodeTcSfbQopt decodes a struct tc_sfb_qopt.
func DecodeTcSfbQopt(b []byte) (TcSfbQopt, error) {
	if err := tcNeed(b, TcSfbQoptSizeCst, "tc_sfb_qopt"); err != nil {
		return TcSfbQopt{}, err
	}
	r := tcRd{b: b}
	return TcSfbQopt{RehashInterval: r.u32(), WarmupTime: r.u32(), Max: r.u32(), BinSize: r.u32(),
		Increment: r.u32(), Decrement: r.u32(), Limit: r.u32(), PenaltyRate: r.u32(), PenaltyBurst: r.u32()}, nil
}

// TcAqm is the increment-16 kinds' options decoded; TcQopt.Aqm. codel,
// pie, fq_pie, hhf, dualpi2 and cake (and cake_mq) fill Vals.
type TcAqm struct {
	Red TcRed // red, choke
	// Gred is allocated for gred alone: its sixteen slots would
	// otherwise grow every kind's TcQopt by 2 KB.
	Gred       *TcGred
	Sfb        TcSfbQopt
	Vals       TcVals
	Skbprio    uint32 // struct tc_skbprio_qopt.limit
	HasSkbprio bool
}

// parseAqm decodes kind's options; ok is false for a kind that is not one
// of increment 16's. A nil opt (no TCA_OPTIONS) is each printer's `opt ==
// NULL` return and decodes to nothing.
func (q *TcQopt) parseAqm(kind string, opt []byte) (bool, error) {
	var widths []int8
	var maxType uint16
	switch kind {
	case TcKindRed, TcKindChoke, TcKindGred, TcKindSfb, TcKindSkbprio, TcKindPlug:
	case TcKindCodel:
		widths, maxType = codelWidths, TcaCodelMaxKnown
	case TcKindPie:
		widths, maxType = pieWidths, TcaPieMaxKnown
	case TcKindFqPie:
		widths, maxType = fqPieWidths, TcaFqPieMaxKnown
	case TcKindHhf:
		widths, maxType = hhfWidths, TcaHhfMaxKnown
	case TcKindDualpi2:
		widths, maxType = dualpi2Widths, TcaDualpi2MaxKnown
	case TcKindCake, TcKindCakeMq:
		widths, maxType = cakeWidths, TcaCakeMaxKnown
	default:
		return false, nil
	}
	if opt == nil {
		return true, nil
	}
	a := &q.Aqm
	var err error
	switch kind {
	case TcKindRed:
		err = q.parseRed(opt, TcaRedMaxKnown, TcaRedParms, TcaRedMaxP, true)
	case TcKindChoke:
		err = q.parseRed(opt, TcaChokeMaxKnown, TcaChokeParms, TcaChokeMaxP, false)
	case TcKindGred:
		err = q.parseGred(opt)
	case TcKindSfb:
		var b []byte
		if b, err = q.parms(opt, TcaSfbMaxKnown, TcaSfbParms, "TCA_SFB_PARMS"); err == nil {
			a.Sfb, err = DecodeTcSfbQopt(b)
		}
	case TcKindSkbprio:
		// skbprio's options are the bare struct (K42).
		if err = tcNeed(opt, TcSkbprioQoptSizeCst, "tc_skbprio_qopt"); err == nil {
			a.Skbprio, a.HasSkbprio = binary.LittleEndian.Uint32(opt), true
		}
	case TcKindPlug:
		// sch_plug has no .dump (K41); plug_print_opt reads nothing.
	default:
		var tb AttrTable
		if tb, err = ParseAttrTableFlags(opt, maxType, tcNestedFlags); err == nil {
			q.Tb = tb
			_, err = a.Vals.loose(tb, widths)
		}
	}
	return true, err
}

// parseRed is red_print_opt (red, with FLAGS and the qevents) and
// choke_print_opt: PARMS is required and at least a struct, MAX_P read
// when a u32.
func (q *TcQopt) parseRed(opt []byte, maxType, parms, maxP uint16, red bool) error {
	b, err := q.parms(opt, maxType, parms, "PARMS")
	if err != nil {
		return err
	}
	r := &q.Aqm.Red
	if r.Qopt, err = DecodeTcRedQopt(b); err != nil {
		return err
	}
	tb := q.Tb
	if v, ok := tb.U32(maxP); ok {
		r.MaxP = v
	}
	if !red {
		return nil
	}
	if fb := tb.Bytes(TcaRedFlags); len(fb) >= NlaBitfield32SizeCst {
		r.Qopt.Flags = fb[0] // qopt->flags = flags_bf->value: the low byte
	}
	if r.EarlyDrop, r.HasEarlyDrop, err = tcOptU32(tb, TcaRedEarlyDropBlock, "TCA_RED_EARLY_DROP_BLOCK"); err != nil {
		return err
	}
	r.Mark, r.HasMark, err = tcOptU32(tb, TcaRedMarkBlock, "TCA_RED_MARK_BLOCK")
	return err
}

// parseGred is gred_print_opt's reads, in its order: PARMS required, MAX_P
// when at least MAX_DPs u32, LIMIT when exactly a u32, then DPS (whose
// absence tc dereferences, Q67) and PARMS sized, then the VQ list.
func (q *TcQopt) parseGred(opt []byte) error {
	tb, err := ParseAttrTableFlags(opt, TcaGredMaxKnown, tcNestedFlags)
	if err != nil {
		return err
	}
	q.Tb = tb
	g := &TcGred{}
	q.Aqm.Gred = g
	if !tb.Has(TcaGredParms) {
		return fmt.Errorf("%w: TCA_GRED_PARMS", ErrTcMissing)
	}
	if mp := tb.Bytes(TcaGredMaxP); len(mp) >= 4*GredMaxDPsCst {
		for i := range g.MaxP {
			g.MaxP[i] = binary.LittleEndian.Uint32(mp[4*i:])
		}
		g.HasMaxP = true
	}
	if lb := tb.Bytes(TcaGredLimit); len(lb) == 4 {
		g.Limit, g.HasLimit = binary.LittleEndian.Uint32(lb), true
	}
	if !tb.Has(TcaGredDps) {
		return fmt.Errorf("%w: TCA_GRED_DPS (Q67)", ErrTcMissing)
	}
	sb, pb := tb.Bytes(TcaGredDps), tb.Bytes(TcaGredParms)
	if len(sb) < TcGredSoptSizeCst || len(pb) < TcGredQoptSizeCst*GredMaxDPsCst {
		return ErrTcGredSmall
	}
	r := tcRd{b: sb}
	g.Sopt = TcGredSopt{DPs: r.u32(), DefDP: r.u32(), Grio: r.u8(), Flags: r.u8(), Pad1: r.u16()}
	for i := range g.Qopts {
		r := tcRd{b: pb[i*TcGredQoptSizeCst:]}
		g.Qopts[i] = TcGredQopt{Limit: r.u32(), QthMin: r.u32(), QthMax: r.u32(), DP: r.u32(), Backlog: r.u32(),
			Qave: r.u32(), Forced: r.u32(), Early: r.u32(), Other: r.u32(), Pdrop: r.u32(), Wlog: r.u8(),
			Plog: r.u8(), ScellLog: r.u8(), Prio: r.u8(), Packets: r.u32(), Bytesin: r.u32()}
	}
	if tb.Has(TcaGredVqList) {
		g.HasVQ = true
		return g.parseVqs(tb.Bytes(TcaGredVqList))
	}
	return nil
}

// gredVqBuf backs one VQ entry's table on the caller's stack.
type gredVqBuf [TcaGredVqMaxKnown + 1][]byte

// parseVqs is gred_parse_vqs: offset steps by each entry's rta_len,
// unaligned (q_gred.c:312); parse_rtattr from the entry over the rest
// finds the first TCA_GRED_VQ_ENTRY, raw type, no flag masked. An rta_len
// under 4 is where tc spins or walks misaligned (Q71), and fewer than 4
// bytes left is where it reads the header past the list: both ErrTcShort.
func (g *TcGred) parseVqs(b []byte) error {
	for off := 0; len(b) > off; {
		rest := b[off:]
		if len(rest) < RTAttrSizeCst {
			return fmt.Errorf("%w: gred VQ list: %d bytes left at offset %d", ErrTcShort, len(rest), off)
		}
		alen := int(binary.LittleEndian.Uint16(rest))
		if alen < RTAttrSizeCst {
			return fmt.Errorf("%w: gred VQ entry rta_len %d at offset %d (Q71)", ErrTcShort, alen, off)
		}
		off += alen
		var entry []byte
		found := false
		walkRawAttrsLoose(rest, func(raw uint16, v []byte) {
			if !found && raw == TcaGredVqEntry {
				entry, found = v, true
			}
		})
		if !found {
			g.Warn = append(g.Warn, "ERROR: Failed to parse Virtual Queue entry")
			continue
		}
		var buf gredVqBuf
		vt := parseIntoLoose(entry, buf[:])
		if !vt.Has(TcaGredVqDp) {
			g.Warn = append(g.Warn, "ERROR: Virtual Queue without DP attribute")
			continue
		}
		if err := g.vq(vt); err != nil {
			return err
		}
	}
	return nil
}

// gredVqStats are the VQ counters gred_parse_vqs reads, in TcGredInfo's
// order.
var gredVqStats = [...]uint16{TcaGredVqStatBytes, TcaGredVqStatPackets, TcaGredVqStatBacklog,
	TcaGredVqStatProbDrop, TcaGredVqStatProbMark, TcaGredVqStatForcedDrop, TcaGredVqStatForcedMark,
	TcaGredVqStatPdrop, TcaGredVqStatOther}

// gredVqStatOld is counter i's value before this entry: an absent
// attribute leaves what an earlier entry for the same DP wrote.
func gredVqStatOld(in *TcGredInfo, i int) uint32 {
	return [...]uint32{in.Bytes, in.Packets, in.Backlog, in.ProbDrop, in.ProbMark, in.ForcedDrop, in.ForcedMark,
		in.Pdrop, in.Other}[i]
}

// vq is one entry's reads: every field a u32, unchecked (Q69).
func (g *TcGred) vq(vt AttrTable) error {
	dp, _, err := tcOptU32(vt, TcaGredVqDp, "TCA_GRED_VQ_DP")
	if err != nil {
		return err
	}
	if dp >= GredMaxDPsCst {
		g.VqDpOver = true
		return nil
	}
	in := &g.Info[dp]
	if bb := vt.Bytes(TcaGredVqStatBytes); len(bb) >= 8 {
		in.BytesWire, in.HasBytesWire = binary.LittleEndian.Uint64(bb), true
	}
	var v [len(gredVqStats)]uint32
	for i, t := range gredVqStats {
		x, has, err := tcOptU32(vt, t, "TCA_GRED_VQ_STAT_*")
		if err != nil {
			return err
		}
		if has {
			v[i] = x
		} else {
			v[i] = gredVqStatOld(in, i)
		}
	}
	in.Bytes, in.Packets, in.Backlog, in.ProbDrop, in.ProbMark = v[0], v[1], v[2], v[3], v[4]
	in.ForcedDrop, in.ForcedMark, in.Pdrop, in.Other = v[5], v[6], v[7], v[8]
	in.FlagsPresent = vt.Has(TcaGredVqFlags)
	f, has, err := tcOptU32(vt, TcaGredVqFlags, "TCA_GRED_VQ_FLAGS")
	if has {
		in.Flags = f
	}
	return err
}

// TcRedXstats is struct tc_red_xstats; choke's adds Matched.
type TcRedXstats struct {
	Early   uint32
	Pdrop   uint32
	Other   uint32
	Marked  uint32
	Matched uint32 // choke only
}

// TcSfbXstats is struct tc_sfb_xstats.
type TcSfbXstats struct {
	Earlydrop   uint32
	Penaltydrop uint32
	Bucketdrop  uint32
	Queuedrop   uint32
	Childdrop   uint32
	Marked      uint32
	Maxqlen     uint32
	Maxprob     uint32
	Avgprob     uint32
}

// TcCodelXstats is struct tc_codel_xstats.
type TcCodelXstats struct {
	Maxpacket     uint32
	Count         uint32
	Lastcount     uint32
	Ldelay        uint32
	DropNext      int32
	DropOverlimit uint32
	EcnMark       uint32
	Dropping      uint32
	CeMark        uint32
}

// TcPieXstats is struct tc_pie_xstats.
type TcPieXstats struct {
	Prob             uint64
	Delay            uint32
	AvgDqRate        uint32
	DqRateEstimating uint32
	PacketsIn        uint32
	Dropped          uint32
	Overlimit        uint32
	Maxq             uint32
	EcnMark          uint32
}

// TcFqPieXstats is struct tc_fq_pie_xstats.
type TcFqPieXstats struct {
	PacketsIn    uint32
	Dropped      uint32
	Overlimit    uint32
	Overmemory   uint32
	EcnMark      uint32
	NewFlowCount uint32
	NewFlowsLen  uint32
	OldFlowsLen  uint32
	MemoryUsage  uint32
}

// TcHhfXstats is struct tc_hhf_xstats.
type TcHhfXstats struct {
	DropOverlimit uint32
	HhOverlimit   uint32
	HhTotCount    uint32
	HhCurCount    uint32
}

// TcDualpi2Xstats is struct tc_dualpi2_xstats.
type TcDualpi2Xstats struct {
	Prob          uint32
	DelayC        uint32
	DelayL        uint32
	PacketsInC    uint32
	PacketsInL    uint32
	Maxq          uint32
	EcnMark       uint32
	StepMarks     uint32
	Credit        int32
	MemoryUsed    uint32
	MaxMemoryUsed uint32
	MemoryLimit   uint32
}

// TcCakeXstats is cake_print_xstats' reads: Stats the top-level values,
// Tins the tin stats tables, in order from tin 1 while present. TinShort
// is each tin's mask of printed attributes shorter than tc's getter; which
// of them tc reads depends on the mode (ShortTin). TinGap is a tin missing
// one of the printed attributes tin 0 has: tc's text table reads the NULL
// attribute (Q78); its JSON tests each tin.
type TcCakeXstats struct {
	Stats    TcVals
	Tins     [CakeMaxTinsCst]TcVals
	TinShort [CakeMaxTinsCst]uint32
	NTins    int
	TinGap   bool
}

// ShortTin is the first tin with a short attribute tc reads, and its mask,
// or -1. JSON's cake_print_json_tin reads each tin's own attributes; the
// text table reads a row only when tin 0 has that attribute (PRINT_TSTAT,
// q_cake.c:770), so a short field absent from tin 0 is never read.
func (c *TcCakeXstats) ShortTin(text bool) (tin int, mask uint32) {
	rows := ^uint32(0)
	if text {
		rows = c.Tins[0].Has | c.TinShort[0]
	}
	for i := range c.NTins {
		if m := c.TinShort[i] & rows; m != 0 {
			return i, m
		}
	}
	return -1, 0
}

// TcAqmXstats is the increment-16 kinds' xstats decoded; TcXstats.Aqm.
type TcAqmXstats struct {
	Red     TcRedXstats // red, choke
	Sfb     TcSfbXstats
	Codel   TcCodelXstats
	Pie     TcPieXstats
	FqPie   TcFqPieXstats
	Hhf     TcHhfXstats
	Dualpi2 TcDualpi2Xstats
	// Cake (cake, cake_mq) is allocated for cake alone: its eight tin
	// tables would otherwise grow every kind's TcXstats by 2.5 KB.
	Cake *TcCakeXstats
}

// parseAqmXstats decodes the increment-16 kinds' xstats; ok is false for
// another kind. codel and fq_pie MIN-copy and never fail; red, choke,
// sfb, pie, hhf and dualpi2 are ErrTcShort when short; gred, plug and
// skbprio have no print_xstats.
func (x *TcXstats) parseAqmXstats(kind string, b []byte) (bool, error) {
	a := &x.Aqm
	var err error
	switch kind {
	case TcKindGred, TcKindPlug, TcKindSkbprio:
		return true, nil
	case TcKindRed:
		if err = tcNeed(b, TcRedXstatsSizeCst, "tc_red_xstats"); err == nil {
			r := tcRd{b: b}
			a.Red = TcRedXstats{Early: r.u32(), Pdrop: r.u32(), Other: r.u32(), Marked: r.u32()}
		}
	case TcKindChoke:
		if err = tcNeed(b, TcChokeXstatsSizeCst, "tc_choke_xstats"); err == nil {
			r := tcRd{b: b}
			a.Red = TcRedXstats{Early: r.u32(), Pdrop: r.u32(), Other: r.u32(), Marked: r.u32(), Matched: r.u32()}
		}
	case TcKindSfb:
		if err = tcNeed(b, TcSfbXstatsSizeCst, "tc_sfb_xstats"); err == nil {
			r := tcRd{b: b}
			a.Sfb = TcSfbXstats{Earlydrop: r.u32(), Penaltydrop: r.u32(), Bucketdrop: r.u32(), Queuedrop: r.u32(),
				Childdrop: r.u32(), Marked: r.u32(), Maxqlen: r.u32(), Maxprob: r.u32(), Avgprob: r.u32()}
		}
	case TcKindCodel:
		r := tcRd{b: TcMinCopy(b, TcCodelXstatsSizeCst)}
		a.Codel = TcCodelXstats{Maxpacket: r.u32(), Count: r.u32(), Lastcount: r.u32(), Ldelay: r.u32(),
			DropNext: r.s32(), DropOverlimit: r.u32(), EcnMark: r.u32(), Dropping: r.u32(), CeMark: r.u32()}
	case TcKindPie:
		if err = tcNeed(b, TcPieXstatsSizeCst, "tc_pie_xstats"); err == nil {
			r := tcRd{b: b}
			a.Pie = TcPieXstats{Prob: r.u64(), Delay: r.u32(), AvgDqRate: r.u32(), DqRateEstimating: r.u32(),
				PacketsIn: r.u32(), Dropped: r.u32(), Overlimit: r.u32(), Maxq: r.u32(), EcnMark: r.u32()}
		}
	case TcKindFqPie:
		r := tcRd{b: TcMinCopy(b, TcFqPieXstatsSizeCst)}
		a.FqPie = TcFqPieXstats{PacketsIn: r.u32(), Dropped: r.u32(), Overlimit: r.u32(), Overmemory: r.u32(),
			EcnMark: r.u32(), NewFlowCount: r.u32(), NewFlowsLen: r.u32(), OldFlowsLen: r.u32(),
			MemoryUsage: r.u32()}
	case TcKindHhf:
		if err = tcNeed(b, TcHhfXstatsSizeCst, "tc_hhf_xstats"); err == nil {
			r := tcRd{b: b}
			a.Hhf = TcHhfXstats{DropOverlimit: r.u32(), HhOverlimit: r.u32(), HhTotCount: r.u32(), HhCurCount: r.u32()}
		}
	case TcKindDualpi2:
		if err = tcNeed(b, TcDualpi2XstatsSizeCst, "tc_dualpi2_xstats"); err == nil {
			r := tcRd{b: b}
			a.Dualpi2 = TcDualpi2Xstats{Prob: r.u32(), DelayC: r.u32(), DelayL: r.u32(), PacketsInC: r.u32(),
				PacketsInL: r.u32(), Maxq: r.u32(), EcnMark: r.u32(), StepMarks: r.u32(), Credit: r.s32(),
				MemoryUsed: r.u32(), MaxMemoryUsed: r.u32(), MemoryLimit: r.u32()}
		}
	case TcKindCake, TcKindCakeMq:
		a.Cake = &TcCakeXstats{}
		err = a.Cake.parse(b)
	default:
		return false, nil
	}
	x.Has = err == nil
	return true, err
}

// cakeStatsBuf and cakeTinBuf back cake's tables on the caller's stack.
type (
	cakeStatsBuf [TcaCakeStatsMaxKnown + 1][]byte
	cakeTinsBuf  [CakeMaxTinsCst + 1][]byte
	cakeTinBuf   [tcaCakeTinStatsMaxKnownSlotsCst][]byte
)

// bit is attribute t's mask bit in a TcVals.
func bit(t uint16) uint32 { return 1 << t }

// parse is cake_print_xstats' reads. Each value is read only where tc
// reads it: MEMORY_USED and _LIMIT together, the NETLEN and ADJLEN pairs
// together, DROP_NEXT_US under a nonzero DROPPING, BLUE_TIMER_US under
// P_DROP. A read attribute shorter than its getter is ErrTcShort (Q77),
// A short printed tin attribute is recorded in TinShort, for the printer
// to check against the reads of its mode.
func (c *TcCakeXstats) parse(b []byte) error {
	var sbuf cakeStatsBuf
	st := parseIntoLoose(b, sbuf[:])
	short, err := c.Stats.loose(st, cakeStatsWidths)
	if err != nil {
		return err
	}
	present := uint32(0)
	for t := uint16(0); t <= TcaCakeStatsMaxKnown; t++ {
		if st.Has(t) {
			present |= bit(t)
		}
	}
	reads := present &^ (bit(TcaCakeStatsMemoryUsed) | bit(TcaCakeStatsMemoryLimit) |
		bit(TcaCakeStatsMinNetlen) | bit(TcaCakeStatsMaxNetlen) | bit(TcaCakeStatsMinAdjlen) |
		bit(TcaCakeStatsMaxAdjlen) | bit(TcaCakeStatsDropNextUs) | bit(TcaCakeStatsBlueTimerUs))
	for _, pair := range [][2]uint16{{TcaCakeStatsMemoryUsed, TcaCakeStatsMemoryLimit},
		{TcaCakeStatsMinNetlen, TcaCakeStatsMaxNetlen}, {TcaCakeStatsMinAdjlen, TcaCakeStatsMaxAdjlen}} {
		if m := bit(pair[0]) | bit(pair[1]); present&m == m {
			reads |= m
		}
	}
	if d, ok := c.Stats.U32(TcaCakeStatsDropping); ok && d != 0 {
		reads |= present & bit(TcaCakeStatsDropNextUs)
	}
	if present&bit(TcaCakeStatsPDrop) != 0 {
		reads |= present & bit(TcaCakeStatsBlueTimerUs)
	}
	if bad := short & reads; bad != 0 {
		return fmt.Errorf("%w: cake stats attribute mask %#x", ErrTcShort, bad)
	}
	if !st.Has(TcaCakeStatsTinStats) {
		return nil
	}
	var tbuf cakeTinsBuf
	parseIntoLoose(st.Bytes(TcaCakeStatsTinStats), tbuf[:])
	for i := 1; i <= CakeMaxTinsCst && tbuf[i] != nil; i++ {
		var ibuf cakeTinBuf
		tt := parseIntoLoose(tbuf[i], ibuf[:])
		short, err := c.Tins[i-1].loose(tt, cakeTinWidths)
		if err != nil {
			return err
		}
		c.TinShort[i-1] = short
		c.NTins++
	}
	// tc tests presence, so a short attribute is there for the gap test.
	for i := 1; i < c.NTins; i++ {
		if (c.Tins[0].Has|c.TinShort[0])&^(c.Tins[i].Has|c.TinShort[i]) != 0 {
			c.TinGap = true
		}
	}
	return nil
}

// parseIntoLoose is parse_rtattr_nested into vals on the caller's stack:
// NLA_F_NESTED masked, the first of each type kept, RTA_OK stopping at a
// malformed attribute.
func parseIntoLoose(data []byte, vals [][]byte) AttrTable {
	walkRawAttrsLoose(data, func(raw uint16, v []byte) {
		t := raw &^ tcNestedFlags
		if int(t) < len(vals) && vals[t] == nil {
			vals[t] = v[:len(v):len(v)]
		}
	})
	return AttrTable{vals: vals}
}
