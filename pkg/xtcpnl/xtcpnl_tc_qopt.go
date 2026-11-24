package xtcpnl

import (
	"encoding/binary"
	"errors"
	"fmt"

	"github.com/randomizedcoder/xtcp2/pkg/cint"
	"golang.org/x/sys/unix"
)

// This file decodes a qdisc's or class's TCA_OPTIONS and TCA_XSTATS (or
// TCA_STATS_APP) for the kinds gotc prints, read the way each tc/q_*.c
// printer reads them (iproute2 v7.2.0). The structures are from
// include/uapi/linux/pkt_sched.h (linux v7.4-rc4); each *SizeCst is its
// sizeof on every Linux ABI, the structures having no pointer or long.
//
// tc tests a fixed structure's payload with `RTA_PAYLOAD(a) < sizeof(*s)`
// and gives up (return -1) when it is short, reading the prefix of a
// longer one; the Decode functions do the same, returning ErrTcShort. The
// xstats tc MIN-copies (fq_codel, fq, netem) are decoded from TcMinCopy.

const (
	// TcRateSpecSizeCst is sizeof(struct tc_ratespec).
	TcRateSpecSizeCst = 12
	// TcPrioQoptSizeCst is sizeof(struct tc_prio_qopt): int bands and
	// TC_PRIO_MAX+1 priomap bytes.
	TcPrioQoptSizeCst = 20
	// TcFifoQoptSizeCst is sizeof(struct tc_fifo_qopt).
	TcFifoQoptSizeCst = 4
	// TcHtbOptSizeCst is sizeof(struct tc_htb_opt).
	TcHtbOptSizeCst = 44
	// TcHtbGlobSizeCst is sizeof(struct tc_htb_glob).
	TcHtbGlobSizeCst = 20
	// TcHtbXstatsSizeCst is sizeof(struct tc_htb_xstats).
	TcHtbXstatsSizeCst = 20
	// TcTbfQoptSizeCst is sizeof(struct tc_tbf_qopt).
	TcTbfQoptSizeCst = 36
	// TcSfqQoptSizeCst is sizeof(struct tc_sfq_qopt).
	TcSfqQoptSizeCst = 20
	// TcSfqQoptV1SizeCst is sizeof(struct tc_sfq_qopt_v1).
	TcSfqQoptV1SizeCst = 72
	// TcSfqXstatsSizeCst is sizeof(struct tc_sfq_xstats).
	TcSfqXstatsSizeCst = 4
	// TcNetemQoptSizeCst is sizeof(struct tc_netem_qopt).
	TcNetemQoptSizeCst = 24
	// TcNetemCorrSizeCst is sizeof(struct tc_netem_corr).
	TcNetemCorrSizeCst = 12
	// TcNetemProbCorrSizeCst is sizeof(struct tc_netem_reorder) and
	// sizeof(struct tc_netem_corrupt), the same two u32s.
	TcNetemProbCorrSizeCst = 8
	// TcNetemRateSizeCst is sizeof(struct tc_netem_rate).
	TcNetemRateSizeCst = 16
	// TcNetemSlotSizeCst is sizeof(struct tc_netem_slot).
	TcNetemSlotSizeCst = 40
	// TcNetemGimodelSizeCst is sizeof(struct tc_netem_gimodel).
	TcNetemGimodelSizeCst = 20
	// TcNetemGemodelSizeCst is sizeof(struct tc_netem_gemodel).
	TcNetemGemodelSizeCst = 16
	// TcNetemXstatsSizeCst is sizeof(struct tc_netem_xstats).
	TcNetemXstatsSizeCst = 56
	// TcFqCodelXstatsSizeCst is sizeof(struct tc_fq_codel_xstats): u32 type
	// and a union whose larger member, tc_fq_codel_qd_stats, is 36 bytes.
	TcFqCodelXstatsSizeCst = 40
	// TcFqQdStatsSizeCst is sizeof(struct tc_fq_qd_stats) with FQ_BANDS 3.
	TcFqQdStatsSizeCst = 152
	// TcSizespecSizeCst is sizeof(struct tc_sizespec).
	TcSizespecSizeCst = 24

	// TcPrioMaxCst is TC_PRIO_MAX: a priomap has TcPrioMaxCst+1 bands.
	TcPrioMaxCst = 15
	// FqBandsCst is FQ_BANDS.
	FqBandsCst = 3

	// TCA_PRIO_* (tc/q_prio.c's compat nest).
	TcaPrioMQ uint16 = 1
	// TcaPrioMaxKnown is TCA_PRIO_MAX.
	TcaPrioMaxKnown = TcaPrioMQ

	// TCA_FQ_CODEL_*.
	TcaFqCodelTarget              uint16 = 1
	TcaFqCodelLimit               uint16 = 2
	TcaFqCodelInterval            uint16 = 3
	TcaFqCodelEcn                 uint16 = 4
	TcaFqCodelFlows               uint16 = 5
	TcaFqCodelQuantum             uint16 = 6
	TcaFqCodelCeThreshold         uint16 = 7
	TcaFqCodelDropBatchSize       uint16 = 8
	TcaFqCodelMemoryLimit         uint16 = 9
	TcaFqCodelCeThresholdSelector uint16 = 10
	TcaFqCodelCeThresholdMask     uint16 = 11
	// TcaFqCodelMaxKnown is TCA_FQ_CODEL_MAX.
	TcaFqCodelMaxKnown = TcaFqCodelCeThresholdMask

	// TCA_FQ_CODEL_XSTATS_* , tc_fq_codel_xstats.type.
	TcaFqCodelXstatsQdisc uint32 = 0
	TcaFqCodelXstatsClass uint32 = 1

	// TCA_FQ_*.
	TcaFqPlimit           uint16 = 1
	TcaFqFlowPlimit       uint16 = 2
	TcaFqQuantum          uint16 = 3
	TcaFqInitialQuantum   uint16 = 4
	TcaFqRateEnable       uint16 = 5
	TcaFqFlowDefaultRate  uint16 = 6
	TcaFqFlowMaxRate      uint16 = 7
	TcaFqBucketsLog       uint16 = 8
	TcaFqFlowRefillDelay  uint16 = 9
	TcaFqOrphanMask       uint16 = 10
	TcaFqLowRateThreshold uint16 = 11
	TcaFqCeThreshold      uint16 = 12
	TcaFqTimerSlack       uint16 = 13
	TcaFqHorizon          uint16 = 14
	TcaFqHorizonDrop      uint16 = 15
	TcaFqPriomap          uint16 = 16
	TcaFqWeights          uint16 = 17
	TcaFqOffloadHorizon   uint16 = 18
	TcaFqMaxKnown                = TcaFqOffloadHorizon

	// tcFqWeightsSizeCst is TCA_FQ_WEIGHTS' FQ_BANDS ints.
	tcFqWeightsSizeCst = FqBandsCst * 4
	// tcFqBandDropsOffsetCst is offsetof(struct tc_fq_qd_stats, band_drops):
	// eight u64s, four u32s, four u64s.
	tcFqBandDropsOffsetCst = 112
	// tcFqBandPktCountOffsetCst is offsetof(..., band_pkt_count).
	tcFqBandPktCountOffsetCst = tcFqBandDropsOffsetCst + FqBandsCst*8
	// tcNetemXstatsFieldsCst is tc_netem_xstats' u64 count.
	tcNetemXstatsFieldsCst = TcNetemXstatsSizeCst / 8
	// tcFqCodelXstatsQdiscFieldsCst is tc_fq_codel_qd_stats' u32 count.
	tcFqCodelXstatsQdiscFieldsCst = 9

	// TCA_HTB_*.
	TcaHtbParms      uint16 = 1
	TcaHtbInit       uint16 = 2
	TcaHtbCtab       uint16 = 3
	TcaHtbRtab       uint16 = 4
	TcaHtbDirectQlen uint16 = 5
	TcaHtbRate64     uint16 = 6
	TcaHtbCeil64     uint16 = 7
	TcaHtbPad        uint16 = 8
	TcaHtbOffload    uint16 = 9
	TcaHtbMaxKnown          = TcaHtbOffload

	// TCA_TBF_*.
	TcaTbfParms    uint16 = 1
	TcaTbfRtab     uint16 = 2
	TcaTbfPtab     uint16 = 3
	TcaTbfRate64   uint16 = 4
	TcaTbfPrate64  uint16 = 5
	TcaTbfBurst    uint16 = 6
	TcaTbfPburst   uint16 = 7
	TcaTbfPad      uint16 = 8
	TcaTbfMaxKnown        = TcaTbfPad

	// TCA_NETEM_*.
	TcaNetemCorr      uint16 = 1
	TcaNetemDelayDist uint16 = 2
	TcaNetemReorder   uint16 = 3
	TcaNetemCorrupt   uint16 = 4
	TcaNetemLoss      uint16 = 5
	TcaNetemRate      uint16 = 6
	TcaNetemEcn       uint16 = 7
	TcaNetemRate64    uint16 = 8
	TcaNetemPad       uint16 = 9
	TcaNetemLatency64 uint16 = 10
	TcaNetemJitter64  uint16 = 11
	TcaNetemSlot      uint16 = 12
	TcaNetemSlotDist  uint16 = 13
	TcaNetemPrngSeed  uint16 = 14
	TcaNetemMaxKnown         = TcaNetemPrngSeed

	// NETEM_LOSS_*.
	NetemLossGI uint16 = 1
	NetemLossGE uint16 = 2
	// NetemLossMaxKnown is NETEM_LOSS_MAX.
	NetemLossMaxKnown = NetemLossGE

	// TCA_STAB_*.
	TcaStabBase uint16 = 1
	TcaStabData uint16 = 2
	// TcaStabMaxKnown is TCA_STAB_MAX.
	TcaStabMaxKnown = TcaStabData

	// tcNestedFlags is parse_rtattr_nested's flags: NLA_F_NESTED masked.
	tcNestedFlags = uint16(unix.NLA_F_NESTED)
)

var (
	// ErrTcShort is a fixed tc structure shorter than its sizeof, where tc's
	// printer returns -1.
	ErrTcShort = errors.New("xtcpnl: tc structure payload is short")
	// ErrTcMissing is an attribute tc's printer requires and returns -1
	// without (tbf's TCA_TBF_PARMS).
	ErrTcMissing = errors.New("xtcpnl: tc attribute missing")
	// ErrTcKindUnknown is a kind this file has no decoder for.
	ErrTcKindUnknown = errors.New("xtcpnl: tc kind has no decoder")
)

// tcRd reads little-endian fields from a fixed structure in order. Each
// read is bounds-checked, so a decoder that checked the length up front
// cannot read past it whatever the field list says.
type tcRd struct {
	b   []byte
	off int
}

func (r *tcRd) u8() uint8 {
	if r.off+1 > len(r.b) {
		return 0
	}
	v := r.b[r.off]
	r.off++
	return v
}

func (r *tcRd) u16() uint16 {
	if r.off+2 > len(r.b) {
		return 0
	}
	v := binary.LittleEndian.Uint16(r.b[r.off : r.off+2])
	r.off += 2
	return v
}

func (r *tcRd) u32() uint32 {
	if r.off+4 > len(r.b) {
		return 0
	}
	v := binary.LittleEndian.Uint32(r.b[r.off : r.off+4])
	r.off += 4
	return v
}

func (r *tcRd) u64() uint64 {
	if r.off+8 > len(r.b) {
		return 0
	}
	v := binary.LittleEndian.Uint64(r.b[r.off : r.off+8])
	r.off += 8
	return v
}

// s16, s32 and s64 reinterpret the bits as C's signed fields do.
func (r *tcRd) s16() int16 { return cint.Short(r.u16()) }
func (r *tcRd) s32() int32 { return cint.Int(r.u32()) }
func (r *tcRd) s64() int64 { return cint.LongLong(r.u64()) }

// tcNeed is the printers' `RTA_PAYLOAD(a) < sizeof(*s)` test.
func tcNeed(b []byte, size int, what string) error {
	if len(b) < size {
		return fmt.Errorf("%w: %s is %d bytes, want %d", ErrTcShort, what, len(b), size)
	}
	return nil
}

// TcRateSpec is struct tc_ratespec.
type TcRateSpec struct {
	CellLog   uint8
	Linklayer uint8
	Overhead  uint16
	CellAlign int16
	Mpu       uint16
	Rate      uint32
}

func (r *tcRd) rateSpec() TcRateSpec {
	return TcRateSpec{
		CellLog: r.u8(), Linklayer: r.u8(), Overhead: r.u16(),
		CellAlign: r.s16(), Mpu: r.u16(), Rate: r.u32(),
	}
}

// TcPrioQopt is struct tc_prio_qopt, also fq's TCA_FQ_PRIOMAP.
type TcPrioQopt struct {
	Bands   int32
	Priomap [TcPrioMaxCst + 1]uint8
}

// DecodeTcPrioQopt decodes a struct tc_prio_qopt.
func DecodeTcPrioQopt(b []byte) (TcPrioQopt, error) {
	var q TcPrioQopt
	if err := tcNeed(b, TcPrioQoptSizeCst, "tc_prio_qopt"); err != nil {
		return q, err
	}
	r := tcRd{b: b}
	q.Bands = r.s32()
	for i := range q.Priomap {
		q.Priomap[i] = r.u8()
	}
	return q, nil
}

// TcFifoQopt is struct tc_fifo_qopt.
type TcFifoQopt struct{ Limit uint32 }

// DecodeTcFifoQopt decodes a struct tc_fifo_qopt.
func DecodeTcFifoQopt(b []byte) (TcFifoQopt, error) {
	if err := tcNeed(b, TcFifoQoptSizeCst, "tc_fifo_qopt"); err != nil {
		return TcFifoQopt{}, err
	}
	r := tcRd{b: b}
	return TcFifoQopt{Limit: r.u32()}, nil
}

// TcHtbOpt is struct tc_htb_opt, TCA_HTB_PARMS.
type TcHtbOpt struct {
	Rate    TcRateSpec
	Ceil    TcRateSpec
	Buffer  uint32
	Cbuffer uint32
	Quantum uint32
	Level   uint32
	Prio    uint32
}

// DecodeTcHtbOpt decodes a struct tc_htb_opt.
func DecodeTcHtbOpt(b []byte) (TcHtbOpt, error) {
	if err := tcNeed(b, TcHtbOptSizeCst, "tc_htb_opt"); err != nil {
		return TcHtbOpt{}, err
	}
	r := tcRd{b: b}
	return TcHtbOpt{
		Rate: r.rateSpec(), Ceil: r.rateSpec(), Buffer: r.u32(), Cbuffer: r.u32(),
		Quantum: r.u32(), Level: r.u32(), Prio: r.u32(),
	}, nil
}

// TcHtbGlob is struct tc_htb_glob, TCA_HTB_INIT.
type TcHtbGlob struct {
	Version      uint32
	Rate2Quantum uint32
	Defcls       uint32
	Debug        uint32
	DirectPkts   uint32
}

// DecodeTcHtbGlob decodes a struct tc_htb_glob.
func DecodeTcHtbGlob(b []byte) (TcHtbGlob, error) {
	if err := tcNeed(b, TcHtbGlobSizeCst, "tc_htb_glob"); err != nil {
		return TcHtbGlob{}, err
	}
	r := tcRd{b: b}
	return TcHtbGlob{Version: r.u32(), Rate2Quantum: r.u32(), Defcls: r.u32(), Debug: r.u32(), DirectPkts: r.u32()}, nil
}

// TcHtbXstats is struct tc_htb_xstats.
type TcHtbXstats struct {
	Lends   uint32
	Borrows uint32
	Giants  uint32
	Tokens  int32
	Ctokens int32
}

// DecodeTcHtbXstats decodes a struct tc_htb_xstats.
func DecodeTcHtbXstats(b []byte) (TcHtbXstats, error) {
	if err := tcNeed(b, TcHtbXstatsSizeCst, "tc_htb_xstats"); err != nil {
		return TcHtbXstats{}, err
	}
	r := tcRd{b: b}
	return TcHtbXstats{Lends: r.u32(), Borrows: r.u32(), Giants: r.u32(), Tokens: r.s32(), Ctokens: r.s32()}, nil
}

// TcTbfQopt is struct tc_tbf_qopt, TCA_TBF_PARMS.
type TcTbfQopt struct {
	Rate     TcRateSpec
	Peakrate TcRateSpec
	Limit    uint32
	Buffer   uint32
	Mtu      uint32
}

// DecodeTcTbfQopt decodes a struct tc_tbf_qopt.
func DecodeTcTbfQopt(b []byte) (TcTbfQopt, error) {
	if err := tcNeed(b, TcTbfQoptSizeCst, "tc_tbf_qopt"); err != nil {
		return TcTbfQopt{}, err
	}
	r := tcRd{b: b}
	return TcTbfQopt{Rate: r.rateSpec(), Peakrate: r.rateSpec(), Limit: r.u32(), Buffer: r.u32(), Mtu: r.u32()}, nil
}

// TcSfqQopt is struct tc_sfq_qopt.
type TcSfqQopt struct {
	Quantum       uint32
	PerturbPeriod int32
	Limit         uint32
	Divisor       uint32
	Flows         uint32
}

// TcSfqredStats is struct tc_sfqred_stats.
type TcSfqredStats struct {
	ProbDrop       uint32
	ForcedDrop     uint32
	ProbMark       uint32
	ForcedMark     uint32
	ProbMarkHead   uint32
	ForcedMarkHead uint32
}

// TcSfqQoptV1 is struct tc_sfq_qopt_v1.
type TcSfqQoptV1 struct {
	V0       TcSfqQopt
	Depth    uint32
	Headdrop uint32
	Limit    uint32
	QthMin   uint32
	QthMax   uint32
	Wlog     uint8
	Plog     uint8
	ScellLog uint8
	Flags    uint8
	MaxP     uint32
	Stats    TcSfqredStats
}

func (r *tcRd) sfqQopt() TcSfqQopt {
	return TcSfqQopt{Quantum: r.u32(), PerturbPeriod: r.s32(), Limit: r.u32(), Divisor: r.u32(), Flows: r.u32()}
}

// DecodeTcSfqQopt decodes a struct tc_sfq_qopt.
func DecodeTcSfqQopt(b []byte) (TcSfqQopt, error) {
	if err := tcNeed(b, TcSfqQoptSizeCst, "tc_sfq_qopt"); err != nil {
		return TcSfqQopt{}, err
	}
	r := tcRd{b: b}
	return r.sfqQopt(), nil
}

// DecodeTcSfqQoptV1 decodes a struct tc_sfq_qopt_v1.
func DecodeTcSfqQoptV1(b []byte) (TcSfqQoptV1, error) {
	if err := tcNeed(b, TcSfqQoptV1SizeCst, "tc_sfq_qopt_v1"); err != nil {
		return TcSfqQoptV1{}, err
	}
	r := tcRd{b: b}
	q := TcSfqQoptV1{
		V0: r.sfqQopt(), Depth: r.u32(), Headdrop: r.u32(), Limit: r.u32(),
		QthMin: r.u32(), QthMax: r.u32(), Wlog: r.u8(), Plog: r.u8(), ScellLog: r.u8(),
		Flags: r.u8(), MaxP: r.u32(),
	}
	q.Stats = TcSfqredStats{
		ProbDrop: r.u32(), ForcedDrop: r.u32(), ProbMark: r.u32(),
		ForcedMark: r.u32(), ProbMarkHead: r.u32(), ForcedMarkHead: r.u32(),
	}
	return q, nil
}

// TcSfqXstats is struct tc_sfq_xstats.
type TcSfqXstats struct{ Allot int32 }

// DecodeTcSfqXstats decodes a struct tc_sfq_xstats.
func DecodeTcSfqXstats(b []byte) (TcSfqXstats, error) {
	if err := tcNeed(b, TcSfqXstatsSizeCst, "tc_sfq_xstats"); err != nil {
		return TcSfqXstats{}, err
	}
	r := tcRd{b: b}
	return TcSfqXstats{Allot: r.s32()}, nil
}

// TcNetemQopt is struct tc_netem_qopt.
type TcNetemQopt struct {
	Latency   uint32
	Limit     uint32
	Loss      uint32
	Gap       uint32
	Duplicate uint32
	Jitter    uint32
}

// DecodeTcNetemQopt decodes a struct tc_netem_qopt.
func DecodeTcNetemQopt(b []byte) (TcNetemQopt, error) {
	if err := tcNeed(b, TcNetemQoptSizeCst, "tc_netem_qopt"); err != nil {
		return TcNetemQopt{}, err
	}
	r := tcRd{b: b}
	return TcNetemQopt{
		Latency: r.u32(), Limit: r.u32(), Loss: r.u32(),
		Gap: r.u32(), Duplicate: r.u32(), Jitter: r.u32(),
	}, nil
}

// TcNetemCorr is struct tc_netem_corr.
type TcNetemCorr struct {
	DelayCorr uint32
	LossCorr  uint32
	DupCorr   uint32
}

// DecodeTcNetemCorr decodes a struct tc_netem_corr.
func DecodeTcNetemCorr(b []byte) (TcNetemCorr, error) {
	if err := tcNeed(b, TcNetemCorrSizeCst, "tc_netem_corr"); err != nil {
		return TcNetemCorr{}, err
	}
	r := tcRd{b: b}
	return TcNetemCorr{DelayCorr: r.u32(), LossCorr: r.u32(), DupCorr: r.u32()}, nil
}

// TcNetemProbCorr is struct tc_netem_reorder and struct tc_netem_corrupt.
type TcNetemProbCorr struct {
	Probability uint32
	Correlation uint32
}

// DecodeTcNetemProbCorr decodes a struct tc_netem_reorder or corrupt.
func DecodeTcNetemProbCorr(b []byte) (TcNetemProbCorr, error) {
	if err := tcNeed(b, TcNetemProbCorrSizeCst, "tc_netem_reorder/corrupt"); err != nil {
		return TcNetemProbCorr{}, err
	}
	r := tcRd{b: b}
	return TcNetemProbCorr{Probability: r.u32(), Correlation: r.u32()}, nil
}

// TcNetemRate is struct tc_netem_rate.
type TcNetemRate struct {
	Rate           uint32
	PacketOverhead int32
	CellSize       uint32
	CellOverhead   int32
}

// DecodeTcNetemRate decodes a struct tc_netem_rate.
func DecodeTcNetemRate(b []byte) (TcNetemRate, error) {
	if err := tcNeed(b, TcNetemRateSizeCst, "tc_netem_rate"); err != nil {
		return TcNetemRate{}, err
	}
	r := tcRd{b: b}
	return TcNetemRate{Rate: r.u32(), PacketOverhead: r.s32(), CellSize: r.u32(), CellOverhead: r.s32()}, nil
}

// TcNetemSlot is struct tc_netem_slot.
type TcNetemSlot struct {
	MinDelay   int64
	MaxDelay   int64
	MaxPackets int32
	MaxBytes   int32
	DistDelay  int64
	DistJitter int64
}

// DecodeTcNetemSlot decodes a struct tc_netem_slot.
func DecodeTcNetemSlot(b []byte) (TcNetemSlot, error) {
	if err := tcNeed(b, TcNetemSlotSizeCst, "tc_netem_slot"); err != nil {
		return TcNetemSlot{}, err
	}
	r := tcRd{b: b}
	return TcNetemSlot{
		MinDelay: r.s64(), MaxDelay: r.s64(), MaxPackets: r.s32(),
		MaxBytes: r.s32(), DistDelay: r.s64(), DistJitter: r.s64(),
	}, nil
}

// TcNetemGimodel is struct tc_netem_gimodel.
type TcNetemGimodel struct{ P13, P31, P32, P14, P23 uint32 }

// DecodeTcNetemGimodel decodes a struct tc_netem_gimodel. tc reads it with
// no length check (lead Q8); a short one is ErrTcShort here.
func DecodeTcNetemGimodel(b []byte) (TcNetemGimodel, error) {
	if err := tcNeed(b, TcNetemGimodelSizeCst, "tc_netem_gimodel"); err != nil {
		return TcNetemGimodel{}, err
	}
	r := tcRd{b: b}
	return TcNetemGimodel{P13: r.u32(), P31: r.u32(), P32: r.u32(), P14: r.u32(), P23: r.u32()}, nil
}

// TcNetemGemodel is struct tc_netem_gemodel.
type TcNetemGemodel struct{ P, R, H, K1 uint32 }

// DecodeTcNetemGemodel decodes a struct tc_netem_gemodel; as gimodel, tc
// does not check its length (Q8).
func DecodeTcNetemGemodel(b []byte) (TcNetemGemodel, error) {
	if err := tcNeed(b, TcNetemGemodelSizeCst, "tc_netem_gemodel"); err != nil {
		return TcNetemGemodel{}, err
	}
	r := tcRd{b: b}
	return TcNetemGemodel{P: r.u32(), R: r.u32(), H: r.u32(), K1: r.u32()}, nil
}

// TcNetemXstats is struct tc_netem_xstats.
type TcNetemXstats struct {
	Delayed          uint64
	Dropped          uint64
	Corrupted        uint64
	Duplicated       uint64
	Reordered        uint64
	EcnMarked        uint64
	AllocationErrors uint64
}

// DecodeTcNetemXstats reads netem xstats as netem_print_xstats does: the
// MIN-copy prefix, so it never fails.
func DecodeTcNetemXstats(b []byte) TcNetemXstats {
	r := tcRd{b: TcMinCopy(b, TcNetemXstatsSizeCst)}
	var f [tcNetemXstatsFieldsCst]uint64
	for i := range f {
		f[i] = r.u64()
	}
	return TcNetemXstats{
		Delayed: f[0], Dropped: f[1], Corrupted: f[2], Duplicated: f[3],
		Reordered: f[4], EcnMarked: f[5], AllocationErrors: f[6],
	}
}

// TcFqCodelQdStats is struct tc_fq_codel_qd_stats.
type TcFqCodelQdStats struct {
	Maxpacket      uint32
	DropOverlimit  uint32
	EcnMark        uint32
	NewFlowCount   uint32
	NewFlowsLen    uint32
	OldFlowsLen    uint32
	CeMark         uint32
	MemoryUsage    uint32
	DropOvermemory uint32
}

// TcFqCodelClStats is struct tc_fq_codel_cl_stats.
type TcFqCodelClStats struct {
	Deficit   int32
	Ldelay    uint32
	Count     uint32
	Lastcount uint32
	Dropping  uint32
	DropNext  int32
}

// TcFqCodelXstats is struct tc_fq_codel_xstats with both views of its
// union decoded; Type says which one the kernel filled.
type TcFqCodelXstats struct {
	Type  uint32
	Qdisc TcFqCodelQdStats
	Class TcFqCodelClStats
}

// DecodeTcFqCodelXstats reads fq_codel xstats as fq_codel_print_xstats
// does: a short payload is MIN-copied into a zeroed struct, so it never
// fails.
func DecodeTcFqCodelXstats(b []byte) TcFqCodelXstats {
	p := TcMinCopy(b, TcFqCodelXstatsSizeCst)
	r := tcRd{b: p}
	x := TcFqCodelXstats{Type: r.u32()}
	var f [tcFqCodelXstatsQdiscFieldsCst]uint32
	for i := range f {
		f[i] = r.u32()
	}
	x.Qdisc = TcFqCodelQdStats{
		Maxpacket: f[0], DropOverlimit: f[1], EcnMark: f[2], NewFlowCount: f[3],
		NewFlowsLen: f[4], OldFlowsLen: f[5], CeMark: f[6], MemoryUsage: f[7], DropOvermemory: f[8],
	}
	c := tcRd{b: p, off: 4}
	x.Class = TcFqCodelClStats{
		Deficit: c.s32(), Ldelay: c.u32(), Count: c.u32(),
		Lastcount: c.u32(), Dropping: c.u32(), DropNext: c.s32(),
	}
	return x
}

// TcFqQdStats is struct tc_fq_qd_stats.
type TcFqQdStats struct {
	GcFlows             uint64
	HighprioPackets     uint64
	TCPRetrans          uint64
	Throttled           uint64
	FlowsPlimit         uint64
	PktsTooLong         uint64
	AllocationErrors    uint64
	TimeNextDelayedFlow int64
	Flows               uint32
	InactiveFlows       uint32
	ThrottledFlows      uint32
	UnthrottleLatencyNs uint32
	CeMark              uint64
	HorizonDrops        uint64
	HorizonCaps         uint64
	FastpathPackets     uint64
	BandDrops           [FqBandsCst]uint64
	BandPktCount        [FqBandsCst]uint32
}

// DecodeTcFqQdStats reads fq xstats as fq_print_xstats does: the MIN-copy
// prefix, so it never fails.
func DecodeTcFqQdStats(b []byte) TcFqQdStats {
	r := tcRd{b: TcMinCopy(b, TcFqQdStatsSizeCst)}
	s := TcFqQdStats{
		GcFlows: r.u64(), HighprioPackets: r.u64(), TCPRetrans: r.u64(), Throttled: r.u64(),
		FlowsPlimit: r.u64(), PktsTooLong: r.u64(), AllocationErrors: r.u64(),
		TimeNextDelayedFlow: r.s64(), Flows: r.u32(), InactiveFlows: r.u32(),
		ThrottledFlows: r.u32(), UnthrottleLatencyNs: r.u32(), CeMark: r.u64(),
		HorizonDrops: r.u64(), HorizonCaps: r.u64(), FastpathPackets: r.u64(),
	}
	r.off = tcFqBandDropsOffsetCst
	for i := range s.BandDrops {
		s.BandDrops[i] = r.u64()
	}
	r.off = tcFqBandPktCountOffsetCst
	for i := range s.BandPktCount {
		s.BandPktCount[i] = r.u32()
	}
	return s
}

// TcSizespec is struct tc_sizespec, TCA_STAB_BASE.
type TcSizespec struct {
	CellLog   uint8
	SizeLog   uint8
	CellAlign int16
	Overhead  int32
	Linklayer uint32
	Mpu       uint32
	Mtu       uint32
	Tsize     uint32
}

// DecodeTcSizespec decodes a struct tc_sizespec.
func DecodeTcSizespec(b []byte) (TcSizespec, error) {
	if err := tcNeed(b, TcSizespecSizeCst, "tc_sizespec"); err != nil {
		return TcSizespec{}, err
	}
	r := tcRd{b: b}
	return TcSizespec{
		CellLog: r.u8(), SizeLog: r.u8(), CellAlign: r.s16(), Overhead: r.s32(),
		Linklayer: r.u32(), Mpu: r.u32(), Mtu: r.u32(), Tsize: r.u32(),
	}, nil
}
