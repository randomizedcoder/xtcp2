package xtcpnl

import (
	"encoding/binary"
	"errors"
	"fmt"

	"github.com/randomizedcoder/xtcp2/pkg/cint"
)

// This file decodes the classful and scheduling qdisc kinds (gotc 15P):
// drr, qfq, hfsc, ets, mqprio, taprio, multiq, cbs and etf, each read the
// way its tc/q_*.c printer reads it (iproute2 v7.2.0). The constants and
// structures are include/uapi/linux/pkt_sched.h at linux v7.4-rc4.
//
// The decoders read at C's widths and keep C's walks, so the printers'
// quirks stay visible to gotc: taprio's TXTIME_DELAY, a u32 on the wire,
// is read as s32 (rta_getattr_s32, q_taprio.c:610); ets's quanta and
// priomap loops step by rta_len (q_ets.c:234, 263); mqprio's options must
// reach RTA_ALIGN(sizeof(struct tc_mqprio_qopt)) (q_mqprio.c:328). Where C
// writes past a fixed array, the decoder stops at the array (Q53).

// Qdisc kinds of increment 15 (tc/q_*.c qdisc_util.id).
const (
	TcKindDrr    = "drr"
	TcKindQfq    = "qfq"
	TcKindHfsc   = "hfsc"
	TcKindEts    = "ets"
	TcKindMqprio = "mqprio"
	TcKindTaprio = "taprio"
	TcKindMultiq = "multiq"
	TcKindCbs    = "cbs"
	TcKindEtf    = "etf"
)

const (
	// TcMultiqQoptSizeCst is sizeof(struct tc_multiq_qopt): u16 bands and
	// u16 max_bands.
	TcMultiqQoptSizeCst = 4
	// TcHfscQoptSizeCst is sizeof(struct tc_hfsc_qopt): u16 defcls.
	TcHfscQoptSizeCst = 2
	// TcServiceCurveSizeCst is sizeof(struct tc_service_curve): u32 m1, d
	// and m2.
	TcServiceCurveSizeCst = 12
	// TcHfscStatsSizeCst is sizeof(struct tc_hfsc_stats): u64 work and
	// rtwork, u32 period and level.
	TcHfscStatsSizeCst = 24
	// TcDrrStatsSizeCst is sizeof(struct tc_drr_stats): u32 deficit.
	TcDrrStatsSizeCst = 4
	// TcQfqStatsSizeCst is sizeof(struct tc_qfq_stats): u32 weight and lmax.
	TcQfqStatsSizeCst = 8
	// TcMqprioQoptSizeCst is sizeof(struct tc_mqprio_qopt): u8 num_tc,
	// TC_QOPT_BITMASK+1 map bytes, u8 hw, then TC_QOPT_MAX_QUEUE u16
	// counts and offsets.
	TcMqprioQoptSizeCst = 82
	// TcCbsQoptSizeCst is sizeof(struct tc_cbs_qopt): u8 offload, 3 pad
	// bytes and four s32s.
	TcCbsQoptSizeCst = 20
	// TcEtfQoptSizeCst is sizeof(struct tc_etf_qopt): s32 delta, s32
	// clockid, u32 flags.
	TcEtfQoptSizeCst = 12

	// TcQoptMaxQueueCst is TC_QOPT_MAX_QUEUE.
	TcQoptMaxQueueCst = 16
	// TcQoptBitmaskCst is TC_QOPT_BITMASK: a map has TcQoptBitmaskCst+1
	// entries.
	TcQoptBitmaskCst = 15
	// TcqEtsMaxBandsCst is TCQ_ETS_MAX_BANDS.
	TcqEtsMaxBandsCst = 16

	// TCA_DRR_*.
	TcaDrrQuantum uint16 = 1
	// TcaDrrMaxKnown is TCA_DRR_MAX.
	TcaDrrMaxKnown = TcaDrrQuantum

	// TCA_QFQ_*.
	TcaQfqWeight uint16 = 1
	TcaQfqLmax   uint16 = 2
	// TcaQfqMaxKnown is TCA_QFQ_MAX.
	TcaQfqMaxKnown = TcaQfqLmax

	// TCA_HFSC_*: the class's service curves.
	TcaHfscRsc uint16 = 1
	TcaHfscFsc uint16 = 2
	TcaHfscUsc uint16 = 3
	// TcaHfscMaxKnown is TCA_HFSC_MAX.
	TcaHfscMaxKnown = TcaHfscUsc

	// TCA_ETS_*.
	TcaEtsNbands      uint16 = 1
	TcaEtsNstrict     uint16 = 2
	TcaEtsQuanta      uint16 = 3
	TcaEtsQuantaBand  uint16 = 4
	TcaEtsPriomap     uint16 = 5
	TcaEtsPriomapBand uint16 = 6
	// TcaEtsMaxKnown is TCA_ETS_MAX.
	TcaEtsMaxKnown = TcaEtsPriomapBand

	// TCA_MQPRIO_*.
	TcaMqprioMode      uint16 = 1
	TcaMqprioShaper    uint16 = 2
	TcaMqprioMinRate64 uint16 = 3
	TcaMqprioMaxRate64 uint16 = 4
	TcaMqprioTcEntry   uint16 = 5
	// TcaMqprioMaxKnown is TCA_MQPRIO_MAX.
	TcaMqprioMaxKnown = TcaMqprioTcEntry
	// TCA_MQPRIO_TC_ENTRY_*.
	TcaMqprioTcEntryIndex uint16 = 1
	TcaMqprioTcEntryFp    uint16 = 2
	// TcaMqprioTcEntryMaxKnown is TCA_MQPRIO_TC_ENTRY_MAX.
	TcaMqprioTcEntryMaxKnown = TcaMqprioTcEntryFp

	// TC_MQPRIO_MODE_* and TC_MQPRIO_SHAPER_*.
	TcMqprioModeDcb       uint16 = 0
	TcMqprioModeChannel   uint16 = 1
	TcMqprioShaperDcb     uint16 = 0
	TcMqprioShaperBwRate  uint16 = 1
	TcFpExpress           uint32 = 1
	TcFpPreemptible       uint32 = 2
	tcMqprioQoptAlignedSz        = 84 // RTA_ALIGN(sizeof(struct tc_mqprio_qopt))

	// TCA_TAPRIO_ATTR_*.
	TcaTaprioAttrPriomap                 uint16 = 1
	TcaTaprioAttrSchedEntryList          uint16 = 2
	TcaTaprioAttrSchedBaseTime           uint16 = 3
	TcaTaprioAttrSchedSingleEntry        uint16 = 4
	TcaTaprioAttrSchedClockid            uint16 = 5
	TcaTaprioAttrPad                     uint16 = 6
	TcaTaprioAttrAdminSched              uint16 = 7
	TcaTaprioAttrSchedCycleTime          uint16 = 8
	TcaTaprioAttrSchedCycleTimeExtension uint16 = 9
	TcaTaprioAttrFlags                   uint16 = 10
	TcaTaprioAttrTxtimeDelay             uint16 = 11
	TcaTaprioAttrTcEntry                 uint16 = 12
	// TcaTaprioAttrMaxKnown is TCA_TAPRIO_ATTR_MAX.
	TcaTaprioAttrMaxKnown = TcaTaprioAttrTcEntry
	// TCA_TAPRIO_SCHED_ENTRY_*.
	TcaTaprioSchedEntryIndex    uint16 = 1
	TcaTaprioSchedEntryCmd      uint16 = 2
	TcaTaprioSchedEntryGateMask uint16 = 3
	TcaTaprioSchedEntryInterval uint16 = 4
	// TcaTaprioSchedEntryMaxKnown is TCA_TAPRIO_SCHED_ENTRY_MAX.
	TcaTaprioSchedEntryMaxKnown = TcaTaprioSchedEntryInterval
	// TCA_TAPRIO_TC_ENTRY_*.
	TcaTaprioTcEntryIndex  uint16 = 1
	TcaTaprioTcEntryMaxSdu uint16 = 2
	TcaTaprioTcEntryFp     uint16 = 3
	// TcaTaprioTcEntryMaxKnown is TCA_TAPRIO_TC_ENTRY_MAX.
	TcaTaprioTcEntryMaxKnown = TcaTaprioTcEntryFp
	// TCA_TAPRIO_OFFLOAD_STATS_*.
	TcaTaprioOffloadStatsPad         uint16 = 1
	TcaTaprioOffloadStatsWindowDrops uint16 = 2
	TcaTaprioOffloadStatsTxOverruns  uint16 = 3
	// TcaTaprioOffloadStatsMaxKnown is TCA_TAPRIO_OFFLOAD_STATS_MAX.
	TcaTaprioOffloadStatsMaxKnown = TcaTaprioOffloadStatsTxOverruns
	// TCA_TAPRIO_ATTR_FLAG_*.
	TcaTaprioAttrFlagTxtimeAssist uint32 = 1 << 0
	TcaTaprioAttrFlagFullOffload  uint32 = 1 << 1

	// TCA_CBS_* and TCA_ETF_*.
	TcaCbsParms uint16 = 1
	// TcaCbsMaxKnown is TCA_CBS_MAX.
	TcaCbsMaxKnown        = TcaCbsParms
	TcaEtfParms    uint16 = 1
	// TcaEtfMaxKnown is TCA_ETF_MAX.
	TcaEtfMaxKnown = TcaEtfParms
	// TC_ETF_* flags.
	TcEtfDeadlineModeOn  uint32 = 1 << 0
	TcEtfOffloadOn       uint32 = 1 << 1
	TcEtfSkipSockCheckOn uint32 = 1 << 2
)

// ErrTcSchedList is an ets quanta or priomap list tc's printer gives up on
// ("No ETS band quantum", "No ETS priomap band"), or a list longer than
// TCQ_ETS_MAX_BANDS, more than any kernel sends. mqprio's rate lists stop
// at TC_QOPT_MAX_QUEUE instead, the entries past it dropped (Q53).
var ErrTcSchedList = errors.New("xtcpnl: tc schedule list malformed")

// TcMultiqQopt is struct tc_multiq_qopt.
type TcMultiqQopt struct {
	Bands    uint16
	MaxBands uint16
}

// DecodeTcMultiqQopt decodes a struct tc_multiq_qopt.
func DecodeTcMultiqQopt(b []byte) (TcMultiqQopt, error) {
	if err := tcNeed(b, TcMultiqQoptSizeCst, "tc_multiq_qopt"); err != nil {
		return TcMultiqQopt{}, err
	}
	r := tcRd{b: b}
	return TcMultiqQopt{Bands: r.u16(), MaxBands: r.u16()}, nil
}

// TcHfscQopt is struct tc_hfsc_qopt.
type TcHfscQopt struct{ Defcls uint16 }

// DecodeTcHfscQopt decodes a struct tc_hfsc_qopt.
func DecodeTcHfscQopt(b []byte) (TcHfscQopt, error) {
	if err := tcNeed(b, TcHfscQoptSizeCst, "tc_hfsc_qopt"); err != nil {
		return TcHfscQopt{}, err
	}
	r := tcRd{b: b}
	return TcHfscQopt{Defcls: r.u16()}, nil
}

// TcServiceCurve is struct tc_service_curve. D is in kernel time units
// (tc_core_ktime2time converts it).
type TcServiceCurve struct {
	M1 uint32
	D  uint32
	M2 uint32
}

// DecodeTcServiceCurve decodes a struct tc_service_curve.
func DecodeTcServiceCurve(b []byte) (TcServiceCurve, error) {
	if err := tcNeed(b, TcServiceCurveSizeCst, "tc_service_curve"); err != nil {
		return TcServiceCurve{}, err
	}
	r := tcRd{b: b}
	return TcServiceCurve{M1: r.u32(), D: r.u32(), M2: r.u32()}, nil
}

// TcHfscStats is struct tc_hfsc_stats, a class's xstats.
type TcHfscStats struct {
	Work   uint64
	Rtwork uint64
	Period uint32
	Level  uint32
}

// DecodeTcHfscStats decodes a struct tc_hfsc_stats.
func DecodeTcHfscStats(b []byte) (TcHfscStats, error) {
	if err := tcNeed(b, TcHfscStatsSizeCst, "tc_hfsc_stats"); err != nil {
		return TcHfscStats{}, err
	}
	r := tcRd{b: b}
	return TcHfscStats{Work: r.u64(), Rtwork: r.u64(), Period: r.u32(), Level: r.u32()}, nil
}

// TcDrrStats is struct tc_drr_stats, a class's xstats.
type TcDrrStats struct{ Deficit uint32 }

// DecodeTcDrrStats decodes a struct tc_drr_stats.
func DecodeTcDrrStats(b []byte) (TcDrrStats, error) {
	if err := tcNeed(b, TcDrrStatsSizeCst, "tc_drr_stats"); err != nil {
		return TcDrrStats{}, err
	}
	r := tcRd{b: b}
	return TcDrrStats{Deficit: r.u32()}, nil
}

// TcQfqStats is struct tc_qfq_stats, the class xstats sch_qfq.c sends and
// tc has no printer for (Q39, K24).
type TcQfqStats struct {
	Weight uint32
	Lmax   uint32
}

// DecodeTcQfqStats decodes a struct tc_qfq_stats.
func DecodeTcQfqStats(b []byte) (TcQfqStats, error) {
	if err := tcNeed(b, TcQfqStatsSizeCst, "tc_qfq_stats"); err != nil {
		return TcQfqStats{}, err
	}
	r := tcRd{b: b}
	return TcQfqStats{Weight: r.u32(), Lmax: r.u32()}, nil
}

// TcMqprioQopt is struct tc_mqprio_qopt, mqprio's options header and
// taprio's TCA_TAPRIO_ATTR_PRIOMAP.
type TcMqprioQopt struct {
	NumTc     uint8
	PrioTcMap [TcQoptBitmaskCst + 1]uint8
	Hw        uint8
	Count     [TcQoptMaxQueueCst]uint16
	Offset    [TcQoptMaxQueueCst]uint16
}

// DecodeTcMqprioQopt decodes a struct tc_mqprio_qopt.
func DecodeTcMqprioQopt(b []byte) (TcMqprioQopt, error) {
	var q TcMqprioQopt
	if err := tcNeed(b, TcMqprioQoptSizeCst, "tc_mqprio_qopt"); err != nil {
		return q, err
	}
	r := tcRd{b: b}
	q.NumTc = r.u8()
	for i := range q.PrioTcMap {
		q.PrioTcMap[i] = r.u8()
	}
	q.Hw = r.u8()
	for i := range q.Count {
		q.Count[i] = r.u16()
	}
	for i := range q.Offset {
		q.Offset[i] = r.u16()
	}
	return q, nil
}

// TcCbsQopt is struct tc_cbs_qopt, TCA_CBS_PARMS.
type TcCbsQopt struct {
	Offload   uint8
	Hicredit  int32
	Locredit  int32
	Idleslope int32
	Sendslope int32
}

// DecodeTcCbsQopt decodes a struct tc_cbs_qopt.
func DecodeTcCbsQopt(b []byte) (TcCbsQopt, error) {
	if err := tcNeed(b, TcCbsQoptSizeCst, "tc_cbs_qopt"); err != nil {
		return TcCbsQopt{}, err
	}
	r := tcRd{b: b}
	q := TcCbsQopt{Offload: r.u8()}
	r.off += 3 // _pad[3]
	q.Hicredit, q.Locredit, q.Idleslope, q.Sendslope = r.s32(), r.s32(), r.s32(), r.s32()
	return q, nil
}

// TcEtfQopt is struct tc_etf_qopt, TCA_ETF_PARMS.
type TcEtfQopt struct {
	Delta   int32
	Clockid int32
	Flags   uint32
}

// DecodeTcEtfQopt decodes a struct tc_etf_qopt.
func DecodeTcEtfQopt(b []byte) (TcEtfQopt, error) {
	if err := tcNeed(b, TcEtfQoptSizeCst, "tc_etf_qopt"); err != nil {
		return TcEtfQopt{}, err
	}
	r := tcRd{b: b}
	return TcEtfQopt{Delta: r.s32(), Clockid: r.s32(), Flags: r.u32()}, nil
}

// TcEts is ets_print_opt's reads (qdisc) and ets_print_copt's (class).
type TcEts struct {
	Nbands     uint8
	Nstrict    uint8
	HasNstrict bool
	Quanta     [TcqEtsMaxBandsCst]uint32 // Quanta[:NQuanta], when HasQuanta
	NQuanta    int
	HasQuanta  bool
	Priomap    [TcPrioMaxCst + 1]uint8 // Priomap[:NPriomap]
	NPriomap   int

	Quantum    uint32 // a class's TCA_ETS_QUANTA_BAND
	HasQuantum bool
}

// TcTaprioEntry is one TCA_TAPRIO_SCHED_ENTRY: print_sched_list prints
// each field that is present.
type TcTaprioEntry struct {
	Index       uint32
	HasIndex    bool
	Cmd         uint8
	HasCmd      bool
	GateMask    uint32
	HasGateMask bool
	Interval    uint32
	HasInterval bool
}

// TcTaprioSched is print_schedule's reads, the oper schedule at the top
// level of the options or the admin one inside TCA_TAPRIO_ATTR_ADMIN_SCHED.
type TcTaprioSched struct {
	BaseTime              int64
	HasBaseTime           bool
	CycleTime             int64
	HasCycleTime          bool
	CycleTimeExtension    int64
	HasCycleTimeExtension bool
	Entries               []TcTaprioEntry
	HasEntries            bool
}

// TcTcEntries is dump_tc_entries' arrays (q_mqprio.c and q_taprio.c): per
// traffic class, MaxSdu and Fp as last set, and the highest class each was
// set for, -1 when none. Entries without an index or with one of
// TC_QOPT_MAX_QUEUE or more are skipped, as tc skips them.
type TcTcEntries struct {
	MaxSdu      [TcQoptMaxQueueCst]uint32
	MaxTcMaxSdu int
	Fp          [TcQoptMaxQueueCst]uint32
	MaxTcFp     int
}

// TcMqprio is mqprio_print_opt's reads.
type TcMqprio struct {
	Qopt TcMqprioQopt
	// Tail is whether attributes follow the header (len > 0): only then
	// are mode, shaper, rates and entries read, and mode and shaper
	// default to dcb.
	Tail       bool
	Mode       uint16
	HasMode    bool
	Shaper     uint16
	HasShaper  bool
	MinRate    [TcQoptMaxQueueCst]uint64 // MinRate[:NMinRate]
	NMinRate   int
	HasMinRate bool
	MaxRate    [TcQoptMaxQueueCst]uint64
	NMaxRate   int
	HasMaxRate bool
	TcEntries  TcTcEntries
}

// TcTaprio is taprio_print_opt's reads.
type TcTaprio struct {
	Priomap        TcMqprioQopt
	Clockid        int32
	HasClockid     bool
	Flags          uint32
	HasFlags       bool
	TxtimeDelay    int32 // rta_getattr_s32 of a u32 (Q55)
	HasTxtimeDelay bool
	Oper           TcTaprioSched
	Admin          TcTaprioSched
	HasAdmin       bool
	TcEntries      TcTcEntries
}

// TcSched is the increment-15 kinds' options decoded; TcQopt.Sched.
type TcSched struct {
	Multiq    TcMultiqQopt
	HasMultiq bool // multiq_print_opt prints nothing for a short struct
	Hfsc      TcHfscQopt
	HasHfsc   bool

	// hfsc class curves; a short one is skipped with tc's "HFSC:
	// truncated ... option" warning, so it is not an error.
	HfscRsc    TcServiceCurve
	HasHfscRsc bool
	HfscFsc    TcServiceCurve
	HasHfscFsc bool
	HfscUsc    TcServiceCurve
	HasHfscUsc bool

	DrrQuantum    uint32
	HasDrrQuantum bool
	QfqWeight     uint32
	HasQfqWeight  bool
	QfqLmax       uint32
	HasQfqLmax    bool

	Ets    TcEts
	Mqprio TcMqprio
	Taprio TcTaprio
	Cbs    TcCbsQopt
	Etf    TcEtfQopt
}

// parseSched decodes kind's qdisc options; ok is false for a kind that is
// not one of increment 15's.
func (q *TcQopt) parseSched(kind string, opt []byte, class bool) (bool, error) {
	s := &q.Sched
	var err error
	switch kind {
	case TcKindDrr:
		err = q.parseDrr(opt)
	case TcKindQfq:
		err = q.parseQfq(opt)
	case TcKindHfsc:
		if class {
			err = q.parseHfscClass(opt)
		} else {
			s.Hfsc, err = DecodeTcHfscQopt(opt)
			s.HasHfsc = err == nil
		}
	case TcKindEts:
		err = q.parseEts(opt, class)
	case TcKindMqprio:
		err = s.Mqprio.parse(opt)
	case TcKindTaprio:
		err = q.parseTaprio(opt)
	case TcKindMultiq:
		if len(opt) >= TcMultiqQoptSizeCst {
			s.Multiq, err = DecodeTcMultiqQopt(opt)
			s.HasMultiq = err == nil
		}
	case TcKindCbs:
		var b []byte
		if b, err = q.parms(opt, TcaCbsMaxKnown, TcaCbsParms, "TCA_CBS_PARMS"); err == nil {
			s.Cbs, err = DecodeTcCbsQopt(b)
		}
	case TcKindEtf:
		var b []byte
		if b, err = q.parms(opt, TcaEtfMaxKnown, TcaEtfParms, "TCA_ETF_PARMS"); err == nil {
			s.Etf, err = DecodeTcEtfQopt(b)
		}
	default:
		return false, nil
	}
	return true, err
}

// parseDrr is drr_print_opt, the qdisc's (no options: sch_drr.c has no
// .dump, K29) and the class's.
func (q *TcQopt) parseDrr(opt []byte) error {
	tb, err := ParseAttrTableFlags(opt, TcaDrrMaxKnown, tcNestedFlags)
	if err != nil {
		return err
	}
	q.Tb = tb
	if tb.Has(TcaDrrQuantum) {
		if err := tcNeed(tb.Bytes(TcaDrrQuantum), 4, "TCA_DRR_QUANTUM"); err != nil {
			return err
		}
		q.Sched.DrrQuantum, q.Sched.HasDrrQuantum = tb.U32(TcaDrrQuantum)
	}
	return nil
}

// parseQfq is qfq_print_opt, the qdisc's (none, K29) and the class's.
func (q *TcQopt) parseQfq(opt []byte) error {
	tb, err := ParseAttrTableFlags(opt, TcaQfqMaxKnown, tcNestedFlags)
	if err != nil {
		return err
	}
	q.Tb = tb
	s := &q.Sched
	if tb.Has(TcaQfqWeight) {
		if err := tcNeed(tb.Bytes(TcaQfqWeight), 4, "TCA_QFQ_WEIGHT"); err != nil {
			return err
		}
		s.QfqWeight, s.HasQfqWeight = tb.U32(TcaQfqWeight)
	}
	if tb.Has(TcaQfqLmax) {
		if err := tcNeed(tb.Bytes(TcaQfqLmax), 4, "TCA_QFQ_LMAX"); err != nil {
			return err
		}
		s.QfqLmax, s.HasQfqLmax = tb.U32(TcaQfqLmax)
	}
	return nil
}

// parseHfscClass is hfsc_print_class_opt's reads.
func (q *TcQopt) parseHfscClass(opt []byte) error {
	tb, err := ParseAttrTableFlags(opt, TcaHfscMaxKnown, tcNestedFlags)
	if err != nil {
		return err
	}
	q.Tb = tb
	s := &q.Sched
	for _, c := range []struct {
		t   uint16
		sc  *TcServiceCurve
		has *bool
	}{
		{TcaHfscRsc, &s.HfscRsc, &s.HasHfscRsc},
		{TcaHfscFsc, &s.HfscFsc, &s.HasHfscFsc},
		{TcaHfscUsc, &s.HfscUsc, &s.HasHfscUsc},
	} {
		if b := tb.Bytes(c.t); len(b) >= TcServiceCurveSizeCst {
			if *c.sc, err = DecodeTcServiceCurve(b); err != nil {
				return err
			}
			*c.has = true
		}
	}
	return nil
}

// parseEts is ets_print_opt (NBANDS and PRIOMAP required, "Incomplete ETS
// options") or, for a class, ets_print_copt.
func (q *TcQopt) parseEts(opt []byte, class bool) error {
	tb, err := ParseAttrTableFlags(opt, TcaEtsMaxKnown, tcNestedFlags)
	if err != nil {
		return err
	}
	q.Tb = tb
	e := &q.Sched.Ets
	if class {
		if tb.Has(TcaEtsQuantaBand) {
			if err := tcNeed(tb.Bytes(TcaEtsQuantaBand), 4, "TCA_ETS_QUANTA_BAND"); err != nil {
				return err
			}
			e.Quantum, e.HasQuantum = tb.U32(TcaEtsQuantaBand)
		}
		return nil
	}
	if !tb.Has(TcaEtsNbands) || !tb.Has(TcaEtsPriomap) {
		return fmt.Errorf("%w: TCA_ETS_NBANDS or TCA_ETS_PRIOMAP", ErrTcMissing)
	}
	if err := tcNeed(tb.Bytes(TcaEtsNbands), 1, "TCA_ETS_NBANDS"); err != nil {
		return err
	}
	e.Nbands, _ = tb.U8(TcaEtsNbands)
	if tb.Has(TcaEtsNstrict) {
		if err := tcNeed(tb.Bytes(TcaEtsNstrict), 1, "TCA_ETS_NSTRICT"); err != nil {
			return err
		}
		e.Nstrict, e.HasNstrict = tb.U8(TcaEtsNstrict)
	}
	if tb.Has(TcaEtsQuanta) {
		n, err := etsList(tb.Bytes(TcaEtsQuanta), TcaEtsQuantaBand, 4, 0, func(i int, v []byte) {
			e.Quanta[i] = binary.LittleEndian.Uint32(v)
		})
		if err != nil {
			return err
		}
		e.NQuanta, e.HasQuanta = n, true
	}
	n, err := etsList(tb.Bytes(TcaEtsPriomap), TcaEtsPriomapBand, 1, 3, func(i int, v []byte) {
		e.Priomap[i] = v[0]
	})
	e.NPriomap = n
	return err
}

// etsList is ets_print_opt_quanta's and ets_print_opt_priomap's loop: at
// each offset, parse_rtattr (no flag masked) from there to the end takes
// the first attribute of type band, and the offset steps by the rta_len
// at that offset plus pad (3 for the priomap's u8s). A step with no band
// attribute, or a band shorter than width, is ErrTcSchedList, where tc
// prints "No ETS ..." (or reads past the attribute). At most
// TCQ_ETS_MAX_BANDS (and TC_PRIO_MAX+1) entries are kept.
func etsList(b []byte, band uint16, width, pad int, set func(i int, v []byte)) (int, error) {
	n := 0
	for off := 0; off < len(b); {
		var val []byte
		found := false
		walkRawAttrsLoose(b[off:], func(raw uint16, v []byte) {
			if !found && raw == band {
				val, found = v, true
			}
		})
		if len(b)-off < RTAttrSizeCst || !found {
			return n, fmt.Errorf("%w: no band attribute at offset %d", ErrTcSchedList, off)
		}
		if len(val) < width {
			return n, fmt.Errorf("%w: band attribute of %d bytes at offset %d", ErrTcSchedList, len(val), off)
		}
		if n == TcqEtsMaxBandsCst {
			return n, fmt.Errorf("%w: more than %d bands", ErrTcSchedList, TcqEtsMaxBandsCst)
		}
		set(n, val)
		n++
		off += int(binary.LittleEndian.Uint16(b[off:off+2])) + pad
	}
	return n, nil
}

// parse is mqprio_print_opt's reads: the header ("options size error"
// short of RTA_ALIGN(sizeof)), then parse_rtattr over what follows.
func (m *TcMqprio) parse(opt []byte) error {
	if len(opt) < tcMqprioQoptAlignedSz {
		return fmt.Errorf("%w: mqprio options are %d bytes, want %d", ErrTcShort, len(opt), tcMqprioQoptAlignedSz)
	}
	var err error
	if m.Qopt, err = DecodeTcMqprioQopt(opt); err != nil {
		return err
	}
	tail := opt[tcMqprioQoptAlignedSz:]
	m.TcEntries = TcTcEntries{MaxTcMaxSdu: -1, MaxTcFp: -1}
	if len(tail) == 0 {
		return nil
	}
	m.Tail = true
	tb, err := ParseAttrTableFlags(tail, TcaMqprioMaxKnown, 0)
	if err != nil {
		return err
	}
	if tb.Has(TcaMqprioMode) {
		if m.Mode, m.HasMode = tb.U16(TcaMqprioMode); !m.HasMode {
			return fmt.Errorf("%w: TCA_MQPRIO_MODE", ErrTcShort)
		}
	}
	if tb.Has(TcaMqprioShaper) {
		if m.Shaper, m.HasShaper = tb.U16(TcaMqprioShaper); !m.HasShaper {
			return fmt.Errorf("%w: TCA_MQPRIO_SHAPER", ErrTcShort)
		}
	}
	if tb.Has(TcaMqprioMinRate64) {
		if m.NMinRate, err = mqprioRates(tb.Bytes(TcaMqprioMinRate64), TcaMqprioMinRate64, &m.MinRate); err != nil {
			return err
		}
		m.HasMinRate = true
	}
	if tb.Has(TcaMqprioMaxRate64) {
		if m.NMaxRate, err = mqprioRates(tb.Bytes(TcaMqprioMaxRate64), TcaMqprioMaxRate64, &m.MaxRate); err != nil {
			return err
		}
		m.HasMaxRate = true
	}
	tcEntries(tail, TcaMqprioTcEntry, TcaMqprioTcEntryMaxKnown, TcaMqprioTcEntryIndex, 0, TcaMqprioTcEntryFp, &m.TcEntries)
	return nil
}

// mqprioRates is the MIN_RATE64/MAX_RATE64 RTA_OK loop: every member must
// be of the list's own type (tc returns -1 otherwise) and a u64. tc writes
// each into a 16-entry stack array with no bound (Q53); the entries past
// it are dropped here.
func mqprioRates(b []byte, t uint16, out *[TcQoptMaxQueueCst]uint64) (int, error) {
	n := 0
	var bad error
	walkRawAttrsLoose(b, func(raw uint16, v []byte) {
		switch {
		case bad != nil:
		case raw != t:
			bad = fmt.Errorf("%w: rate attribute type %d in a list of %d", ErrTcSchedList, raw, t)
		case len(v) < 8:
			bad = fmt.Errorf("%w: rate attribute of %d bytes", ErrTcShort, len(v))
		case n < len(out):
			out[n] = binary.LittleEndian.Uint64(v)
			n++
		}
	})
	return n, bad
}

// tcEntries is dump_tc_entries: an RTA_OK walk of b for raw type entry
// with NLA_F_NESTED set, each parsed nested; sdu 0 is a list without
// MAX_SDU (mqprio's).
func tcEntries(b []byte, entry, maxType, index, sdu, fp uint16, out *TcTcEntries) {
	walkRawAttrsLoose(b, func(raw uint16, v []byte) {
		if raw != entry|tcNestedFlags {
			return
		}
		var buf smallAttrBuf
		tb := parseSmallLoose(v, maxType, &buf)
		// "Missing tc entry index", "Unexpected tc entry index": skipped. A
		// short index is where tc reads past the attribute; skipped too.
		tc, ok := tb.U32(index)
		if !ok || tc >= TcQoptMaxQueueCst {
			return
		}
		if sdu != 0 && tb.Has(sdu) {
			out.MaxSdu[tc], _ = tb.U32(sdu)
			out.MaxTcMaxSdu = max(out.MaxTcMaxSdu, int(tc))
		}
		if tb.Has(fp) {
			out.Fp[tc], _ = tb.U32(fp)
			out.MaxTcFp = max(out.MaxTcFp, int(tc))
		}
	})
}

// smallAttrBuf backs the table of a small nest (a tc or schedule entry)
// on the caller's stack.
type smallAttrBuf [TcaTaprioSchedEntryMaxKnown + 1][]byte

// parseSmallLoose is parse_rtattr_nested over a nest of at most
// len(smallAttrBuf)-1 types, as ParseAttrTableLoose without its
// allocation: RTA_OK stops at a malformed attribute, keeping those before.
func parseSmallLoose(data []byte, maxType uint16, buf *smallAttrBuf) AttrTable {
	t := AttrTable{vals: buf[:min(int(maxType)+1, len(buf))]}
	walkRawAttrsLoose(data, func(raw uint16, val []byte) {
		atype := raw &^ tcNestedFlags
		if int(atype) < len(t.vals) && t.vals[atype] == nil {
			t.vals[atype] = val[:len(val):len(val)]
		}
	})
	return t
}

// parseTaprio is taprio_print_opt's reads: PRIOMAP is required (tc
// returns -1), then the top level's fields, the oper schedule, the admin
// schedule and the tc entries.
func (q *TcQopt) parseTaprio(opt []byte) error {
	tb, err := ParseAttrTableFlags(opt, TcaTaprioAttrMaxKnown, tcNestedFlags)
	if err != nil {
		return err
	}
	q.Tb = tb
	t := &q.Sched.Taprio
	if !tb.Has(TcaTaprioAttrPriomap) {
		return fmt.Errorf("%w: TCA_TAPRIO_ATTR_PRIOMAP", ErrTcMissing)
	}
	if t.Priomap, err = DecodeTcMqprioQopt(tb.Bytes(TcaTaprioAttrPriomap)); err != nil {
		return err
	}
	var v uint32
	if v, t.HasClockid, err = tcOptU32(tb, TcaTaprioAttrSchedClockid, "TCA_TAPRIO_ATTR_SCHED_CLOCKID"); err != nil {
		return err
	}
	t.Clockid = cint.Int(v)
	if t.Flags, t.HasFlags, err = tcOptU32(tb, TcaTaprioAttrFlags, "TCA_TAPRIO_ATTR_FLAGS"); err != nil {
		return err
	}
	if v, t.HasTxtimeDelay, err = tcOptU32(tb, TcaTaprioAttrTxtimeDelay, "TCA_TAPRIO_ATTR_TXTIME_DELAY"); err != nil {
		return err
	}
	t.TxtimeDelay = cint.Int(v) // rta_getattr_s32 (Q55)
	if t.Oper, err = taprioSched(tb); err != nil {
		return err
	}
	if tb.Has(TcaTaprioAttrAdminSched) {
		at, err := ParseAttrTableFlags(tb.Bytes(TcaTaprioAttrAdminSched), TcaTaprioAttrMaxKnown, tcNestedFlags)
		if err != nil {
			return err
		}
		if t.Admin, err = taprioSched(at); err != nil {
			return err
		}
		t.HasAdmin = true
	}
	t.TcEntries = TcTcEntries{MaxTcMaxSdu: -1, MaxTcFp: -1}
	tcEntries(opt, TcaTaprioAttrTcEntry, TcaTaprioTcEntryMaxKnown, TcaTaprioTcEntryIndex,
		TcaTaprioTcEntryMaxSdu, TcaTaprioTcEntryFp, &t.TcEntries)
	return nil
}

// taprioSched is print_schedule: the s64 times and the entry list, each
// entry parsed nested with its fields read when present.
func taprioSched(tb AttrTable) (TcTaprioSched, error) {
	var s TcTaprioSched
	var err error
	if s.BaseTime, s.HasBaseTime, err = tcOptS64(tb, TcaTaprioAttrSchedBaseTime, "TCA_TAPRIO_ATTR_SCHED_BASE_TIME"); err != nil {
		return s, err
	}
	if s.CycleTime, s.HasCycleTime, err = tcOptS64(tb, TcaTaprioAttrSchedCycleTime, "TCA_TAPRIO_ATTR_SCHED_CYCLE_TIME"); err != nil {
		return s, err
	}
	if s.CycleTimeExtension, s.HasCycleTimeExtension, err = tcOptS64(tb, TcaTaprioAttrSchedCycleTimeExtension,
		"TCA_TAPRIO_ATTR_SCHED_CYCLE_TIME_EXTENSION"); err != nil {
		return s, err
	}
	if !tb.Has(TcaTaprioAttrSchedEntryList) {
		return s, nil
	}
	s.HasEntries = true
	var bad error
	walkRawAttrsLoose(tb.Bytes(TcaTaprioAttrSchedEntryList), func(_ uint16, v []byte) {
		if bad != nil {
			return
		}
		var buf smallAttrBuf
		et := parseSmallLoose(v, TcaTaprioSchedEntryMaxKnown, &buf)
		e, err := taprioEntry(et)
		if err != nil {
			bad = err
			return
		}
		s.Entries = append(s.Entries, e)
	})
	return s, bad
}

// taprioEntry is print_sched_list's reads of one entry: each field when
// present, at its width.
func taprioEntry(et AttrTable) (TcTaprioEntry, error) {
	var e TcTaprioEntry
	var err error
	if e.Index, e.HasIndex, err = tcOptU32(et, TcaTaprioSchedEntryIndex, "TCA_TAPRIO_SCHED_ENTRY_INDEX"); err != nil {
		return e, err
	}
	if et.Has(TcaTaprioSchedEntryCmd) {
		if e.Cmd, e.HasCmd = et.U8(TcaTaprioSchedEntryCmd); !e.HasCmd {
			return e, fmt.Errorf("%w: TCA_TAPRIO_SCHED_ENTRY_CMD", ErrTcShort)
		}
	}
	if e.GateMask, e.HasGateMask, err = tcOptU32(et, TcaTaprioSchedEntryGateMask, "TCA_TAPRIO_SCHED_ENTRY_GATE_MASK"); err != nil {
		return e, err
	}
	e.Interval, e.HasInterval, err = tcOptU32(et, TcaTaprioSchedEntryInterval, "TCA_TAPRIO_SCHED_ENTRY_INTERVAL")
	return e, err
}

// tcOptU32 is `if (tb[t]) v = rta_getattr_u32(tb[t])`: has is whether
// the attribute is present, and a present one shorter than a u32 is
// ErrTcShort (tc reads past it).
func tcOptU32(tb AttrTable, t uint16, what string) (uint32, bool, error) {
	if !tb.Has(t) {
		return 0, false, nil
	}
	v, ok := tb.U32(t)
	if !ok {
		return 0, false, fmt.Errorf("%w: %s", ErrTcShort, what)
	}
	return v, true, nil
}

// tcOptU64 is tcOptU32 for rta_getattr_u64.
func tcOptU64(tb AttrTable, t uint16, what string) (uint64, bool, error) {
	if !tb.Has(t) {
		return 0, false, nil
	}
	v, ok := tb.U64(t)
	if !ok {
		return 0, false, fmt.Errorf("%w: %s", ErrTcShort, what)
	}
	return v, true, nil
}

// tcOptS64 is tcOptU32 for rta_getattr_s64.
func tcOptS64(tb AttrTable, t uint16, what string) (int64, bool, error) {
	v, has, err := tcOptU64(tb, t, what)
	return cint.LongLong(v), has, err
}

// parms is cbs_print_opt's and etf_print_opt's first reads: the nest's
// PARMS, which tc requires.
func (q *TcQopt) parms(opt []byte, maxType, parms uint16, what string) ([]byte, error) {
	tb, err := ParseAttrTableFlags(opt, maxType, tcNestedFlags)
	if err != nil {
		return nil, err
	}
	q.Tb = tb
	if !tb.Has(parms) {
		return nil, fmt.Errorf("%w: %s", ErrTcMissing, what)
	}
	return tb.Bytes(parms), nil
}

// parseSchedXstats decodes the increment-15 kinds' xstats; ok is false
// for another kind. drr and hfsc print class xstats (ErrTcShort when
// short); taprio prints its offload stats nest; qfq, ets, mqprio, multiq,
// cbs and etf have no print_xstats.
func (x *TcXstats) parseSchedXstats(kind string, b []byte) (bool, error) {
	var err error
	switch kind {
	case TcKindQfq, TcKindEts, TcKindMqprio, TcKindMultiq, TcKindCbs, TcKindEtf:
		return true, nil
	case TcKindDrr:
		x.Drr, err = DecodeTcDrrStats(b)
	case TcKindHfsc:
		x.Hfsc, err = DecodeTcHfscStats(b)
	case TcKindTaprio:
		err = x.parseTaprioXstats(b)
	default:
		return false, nil
	}
	x.Has = err == nil
	return true, err
}

// parseTaprioXstats is taprio_print_xstats: the offload stats nest, each
// counter a u64 when present.
func (x *TcXstats) parseTaprioXstats(b []byte) error {
	tb, err := ParseAttrTableFlags(b, TcaTaprioOffloadStatsMaxKnown, tcNestedFlags)
	if err != nil {
		return err
	}
	if x.TaprioWindowDrops, x.HasTaprioWindowDrops, err = tcOptU64(tb, TcaTaprioOffloadStatsWindowDrops,
		"TCA_TAPRIO_OFFLOAD_STATS_WINDOW_DROPS"); err != nil {
		return err
	}
	x.TaprioTxOverruns, x.HasTaprioTxOverruns, err = tcOptU64(tb, TcaTaprioOffloadStatsTxOverruns,
		"TCA_TAPRIO_OFFLOAD_STATS_TX_OVERRUNS")
	return err
}
