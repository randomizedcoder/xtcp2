package xtcpnl

import (
	"encoding/binary"
	"errors"
	"fmt"

	"golang.org/x/sys/unix"
)

// This file is the per-kind step between ParseTcMsg and a tc printer: a
// qdisc's or class's TCA_OPTIONS and xstats decoded the way the kind's
// tc/q_*.c print_qopt/print_copt and print_xstats read them (iproute2
// v7.2.0), and the dump request tc_qdisc_list and tc_class_list send.

// Qdisc kinds gotc prints (tc/q_*.c qdisc_util.id).
const (
	TcKindNoqueue       = "noqueue"
	TcKindMq            = "mq"
	TcKindPfifoFast     = "pfifo_fast"
	TcKindPrio          = "prio"
	TcKindPfifo         = "pfifo"
	TcKindBfifo         = "bfifo"
	TcKindPfifoHeadDrop = "pfifo_head_drop"
	TcKindFqCodel       = "fq_codel"
	TcKindFq            = "fq"
	TcKindHtb           = "htb"
	TcKindTbf           = "tbf"
	TcKindSfq           = "sfq"
	TcKindNetem         = "netem"
	TcKindIngress       = "ingress"
	TcKindClsact        = "clsact"
)

// ErrTcCompatNest is a compat nest (__parse_rtattr_nested_compat) whose
// inner rtattr claims more bytes than follow it; tc would read past the
// attribute.
var ErrTcCompatNest = errors.New("xtcpnl: tc compat nest overruns its attribute")

// TcQopt is a kind's TCA_OPTIONS decoded. Kind says which fields are set.
// Tb is the kind's attribute table: parse_rtattr_nested of the options for
// fq_codel, fq, htb and tbf, the compat nest for prio and pfifo_fast, and
// netem's tail after struct tc_netem_qopt (parse_rtattr, no flag masked).
// Fixed structures tc tests with `RTA_PAYLOAD < sizeof` are decoded, and a
// short one is ErrTcShort, where tc's printer returns -1.
type TcQopt struct {
	Kind string
	Tb   AttrTable

	Prio    TcPrioQopt // prio, pfifo_fast
	HasPrio bool
	Fifo    TcFifoQopt // pfifo, bfifo, pfifo_head_drop
	HasFifo bool

	FqPriomap    TcPrioQopt // TCA_FQ_PRIOMAP, when at least sizeof
	HasFqPriomap bool
	FqWeights    [FqBandsCst]int32 // TCA_FQ_WEIGHTS, when at least FQ_BANDS ints
	HasFqWeights bool

	HtbParms    TcHtbOpt
	HasHtbParms bool
	HtbInit     TcHtbGlob
	HasHtbInit  bool

	Tbf TcTbfQopt // TCA_TBF_PARMS, which tbf_print_opt requires

	Sfq      TcSfqQopt
	SfqV1    TcSfqQoptV1 // when the payload is at least sizeof(v1)
	HasSfqV1 bool

	Netem           TcNetemQopt
	NetemCorr       TcNetemCorr
	HasNetemCorr    bool
	NetemReorder    TcNetemProbCorr
	HasNetemReorder bool
	NetemCorrupt    TcNetemProbCorr
	HasNetemCorrupt bool
	NetemRate       TcNetemRate
	HasNetemRate    bool
	NetemSlot       TcNetemSlot
	HasNetemSlot    bool
	NetemGi         TcNetemGimodel
	HasNetemGi      bool
	NetemGe         TcNetemGemodel
	HasNetemGe      bool

	Sched TcSched // drr, qfq, hfsc, ets, mqprio, taprio, multiq, cbs, etf
}

// ParseTcQopt decodes opt, a TCA_OPTIONS payload, for kind. A kind whose
// printer reads no options (noqueue, mq, ingress, clsact) decodes to Kind
// alone. An unknown kind is ErrTcKindUnknown.
func ParseTcQopt(kind string, opt []byte) (TcQopt, error) {
	q := TcQopt{Kind: kind}
	var err error
	switch kind {
	case TcKindNoqueue, TcKindMq, TcKindIngress, TcKindClsact:
		return q, nil
	case TcKindPrio, TcKindPfifoFast:
		err = q.parsePrio(opt)
	case TcKindPfifo, TcKindBfifo, TcKindPfifoHeadDrop:
		q.Fifo, err = DecodeTcFifoQopt(opt)
		q.HasFifo = err == nil
	case TcKindFqCodel:
		q.Tb, err = ParseAttrTableFlags(opt, TcaFqCodelMaxKnown, tcNestedFlags)
	case TcKindFq:
		err = q.parseFq(opt)
	case TcKindHtb:
		err = q.parseHtb(opt)
	case TcKindTbf:
		err = q.parseTbf(opt)
	case TcKindSfq:
		err = q.parseSfq(opt)
	case TcKindNetem:
		err = q.parseNetem(opt)
	default:
		if ok, serr := q.parseSched(kind, opt, false); ok {
			return q, serr
		}
		return q, fmt.Errorf("%w: %q", ErrTcKindUnknown, kind)
	}
	return q, err
}

// ParseTcCopt decodes opt, a class's TCA_OPTIONS, for kind: print_copt's
// reads. hfsc's and ets's class options differ from their qdisc's (service
// curves; TCA_ETS_QUANTA_BAND); every other kind's are read as its qdisc
// options are (htb, drr and qfq share one printer).
func ParseTcCopt(kind string, opt []byte) (TcQopt, error) {
	if kind != TcKindHfsc && kind != TcKindEts {
		return ParseTcQopt(kind, opt)
	}
	q := TcQopt{Kind: kind}
	_, err := q.parseSched(kind, opt, true)
	return q, err
}

// SplitTcCompat is parse_rtattr_nested_compat(tb, max, rta, data, len):
// opt's first hdrLen bytes are the fixed structure, and when at least an
// rtattr header follows RTA_ALIGN(hdrLen), that rtattr's payload is the
// nest. nest is nil when there is none. A payload shorter than hdrLen is
// ErrTcShort.
func SplitTcCompat(opt []byte, hdrLen int) (hdr, nest []byte, err error) {
	if hdrLen < 0 || len(opt) < hdrLen {
		return nil, nil, fmt.Errorf("%w: compat header is %d bytes, want %d", ErrTcShort, len(opt), hdrLen)
	}
	hdr = opt[:hdrLen]
	at := nlmsgAlign(hdrLen)
	if len(opt) < at+RTAttrSizeCst {
		return hdr, nil, nil
	}
	inner := opt[at:]
	if len(inner) < RTAttrSizeCst {
		return hdr, nil, nil
	}
	rlen := int(binary.LittleEndian.Uint16(inner[0:2]))
	if rlen < RTAttrSizeCst {
		// RTA_PAYLOAD is negative: parse_rtattr's RTA_OK loop sees nothing.
		return hdr, []byte{}, nil
	}
	if rlen > len(inner) {
		return hdr, nil, ErrTcCompatNest
	}
	return hdr, inner[RTAttrSizeCst:rlen], nil
}

func (q *TcQopt) parsePrio(opt []byte) error {
	hdr, nest, err := SplitTcCompat(opt, TcPrioQoptSizeCst)
	if err != nil {
		return err
	}
	if q.Prio, err = DecodeTcPrioQopt(hdr); err != nil {
		return err
	}
	q.HasPrio = true
	if nest == nil {
		return nil
	}
	q.Tb, err = ParseAttrTableFlags(nest, TcaPrioMaxKnown, tcNestedFlags)
	return err
}

func (q *TcQopt) parseFq(opt []byte) error {
	tb, err := ParseAttrTableFlags(opt, TcaFqMaxKnown, tcNestedFlags)
	if err != nil {
		return err
	}
	q.Tb = tb
	if b := tb.Bytes(TcaFqPriomap); len(b) >= TcPrioQoptSizeCst {
		if q.FqPriomap, err = DecodeTcPrioQopt(b); err != nil {
			return err
		}
		q.HasFqPriomap = true
	}
	if b := tb.Bytes(TcaFqWeights); len(b) >= tcFqWeightsSizeCst {
		r := tcRd{b: b}
		for i := range q.FqWeights {
			q.FqWeights[i] = r.s32()
		}
		q.HasFqWeights = true
	}
	return nil
}

func (q *TcQopt) parseHtb(opt []byte) error {
	tb, err := ParseAttrTableFlags(opt, TcaHtbMaxKnown, tcNestedFlags)
	if err != nil {
		return err
	}
	q.Tb = tb
	if tb.Has(TcaHtbParms) {
		if q.HtbParms, err = DecodeTcHtbOpt(tb.Bytes(TcaHtbParms)); err != nil {
			return err
		}
		q.HasHtbParms = true
	}
	if tb.Has(TcaHtbInit) {
		if q.HtbInit, err = DecodeTcHtbGlob(tb.Bytes(TcaHtbInit)); err != nil {
			return err
		}
		q.HasHtbInit = true
	}
	return nil
}

func (q *TcQopt) parseTbf(opt []byte) error {
	tb, err := ParseAttrTableFlags(opt, TcaTbfMaxKnown, tcNestedFlags)
	if err != nil {
		return err
	}
	q.Tb = tb
	if !tb.Has(TcaTbfParms) {
		return fmt.Errorf("%w: TCA_TBF_PARMS", ErrTcMissing)
	}
	q.Tbf, err = DecodeTcTbfQopt(tb.Bytes(TcaTbfParms))
	return err
}

func (q *TcQopt) parseSfq(opt []byte) error {
	var err error
	if q.Sfq, err = DecodeTcSfqQopt(opt); err != nil {
		return err
	}
	if len(opt) >= TcSfqQoptV1SizeCst {
		if q.SfqV1, err = DecodeTcSfqQoptV1(opt); err != nil {
			return err
		}
		q.HasSfqV1 = true
	}
	return nil
}

// parseNetem is netem_print_opt's reads: struct tc_netem_qopt ("options
// size error" when short), then parse_rtattr over what follows it, each
// fixed structure checked as tc checks it.
func (q *TcQopt) parseNetem(opt []byte) error {
	var err error
	if q.Netem, err = DecodeTcNetemQopt(opt); err != nil {
		return err
	}
	if len(opt) <= TcNetemQoptSizeCst {
		return nil
	}
	if q.Tb, err = ParseAttrTableFlags(opt[TcNetemQoptSizeCst:], TcaNetemMaxKnown, 0); err != nil {
		return err
	}
	if err := q.parseNetemStructs(); err != nil {
		return err
	}
	return q.parseNetemLoss()
}

func (q *TcQopt) parseNetemStructs() error {
	var err error
	tb := q.Tb
	if tb.Has(TcaNetemCorr) {
		if q.NetemCorr, err = DecodeTcNetemCorr(tb.Bytes(TcaNetemCorr)); err != nil {
			return err
		}
		q.HasNetemCorr = true
	}
	if tb.Has(TcaNetemReorder) {
		if q.NetemReorder, err = DecodeTcNetemProbCorr(tb.Bytes(TcaNetemReorder)); err != nil {
			return err
		}
		q.HasNetemReorder = true
	}
	if tb.Has(TcaNetemCorrupt) {
		if q.NetemCorrupt, err = DecodeTcNetemProbCorr(tb.Bytes(TcaNetemCorrupt)); err != nil {
			return err
		}
		q.HasNetemCorrupt = true
	}
	if tb.Has(TcaNetemRate) {
		if q.NetemRate, err = DecodeTcNetemRate(tb.Bytes(TcaNetemRate)); err != nil {
			return err
		}
		q.HasNetemRate = true
	}
	if tb.Has(TcaNetemSlot) {
		if q.NetemSlot, err = DecodeTcNetemSlot(tb.Bytes(TcaNetemSlot)); err != nil {
			return err
		}
		q.HasNetemSlot = true
	}
	// ECN is a u32, RATE64, LATENCY64, JITTER64 and PRNG_SEED u64s, each
	// needing its width.
	for _, w := range []struct {
		t    uint16
		n    int
		what string
	}{
		{TcaNetemEcn, 4, "TCA_NETEM_ECN"}, {TcaNetemRate64, 8, "TCA_NETEM_RATE64"},
		{TcaNetemLatency64, 8, "TCA_NETEM_LATENCY64"}, {TcaNetemJitter64, 8, "TCA_NETEM_JITTER64"},
		{TcaNetemPrngSeed, 8, "TCA_NETEM_PRNG_SEED"},
	} {
		if tb.Has(w.t) {
			if err := tcNeed(tb.Bytes(w.t), w.n, w.what); err != nil {
				return err
			}
		}
	}
	return nil
}

func (q *TcQopt) parseNetemLoss() error {
	if !q.Tb.Has(TcaNetemLoss) {
		return nil
	}
	lb, err := ParseAttrTableFlags(q.Tb.Bytes(TcaNetemLoss), NetemLossMaxKnown, tcNestedFlags)
	if err != nil {
		return err
	}
	if lb.Has(NetemLossGI) {
		if q.NetemGi, err = DecodeTcNetemGimodel(lb.Bytes(NetemLossGI)); err != nil {
			return err
		}
		q.HasNetemGi = true
	}
	if lb.Has(NetemLossGE) {
		if q.NetemGe, err = DecodeTcNetemGemodel(lb.Bytes(NetemLossGE)); err != nil {
			return err
		}
		q.HasNetemGe = true
	}
	return nil
}

// TcXstats is a kind's xstats (TCA_STATS_APP, or TCA_XSTATS without it)
// decoded for its print_xstats. Has is false for a kind tc prints no
// xstats for.
type TcXstats struct {
	Kind    string
	Has     bool
	FqCodel TcFqCodelXstats
	Fq      TcFqQdStats
	Htb     TcHtbXstats
	Sfq     TcSfqXstats
	Netem   TcNetemXstats
	Drr     TcDrrStats
	Hfsc    TcHfscStats

	TaprioWindowDrops    uint64
	HasTaprioWindowDrops bool
	TaprioTxOverruns     uint64
	HasTaprioTxOverruns  bool
}

// ParseTcXstats decodes b for kind's print_xstats. fq_codel, fq and netem
// MIN-copy and never fail; htb and sfq are ErrTcShort when short. An
// unknown kind is ErrTcKindUnknown.
func ParseTcXstats(kind string, b []byte) (TcXstats, error) {
	x := TcXstats{Kind: kind}
	var err error
	switch kind {
	case TcKindNoqueue, TcKindMq, TcKindIngress, TcKindClsact, TcKindPrio, TcKindPfifoFast,
		TcKindPfifo, TcKindBfifo, TcKindPfifoHeadDrop, TcKindTbf:
		return x, nil
	case TcKindFqCodel:
		x.FqCodel = DecodeTcFqCodelXstats(b)
	case TcKindFq:
		x.Fq = DecodeTcFqQdStats(b)
	case TcKindNetem:
		x.Netem = DecodeTcNetemXstats(b)
	case TcKindHtb:
		x.Htb, err = DecodeTcHtbXstats(b)
	case TcKindSfq:
		x.Sfq, err = DecodeTcSfqXstats(b)
	default:
		if ok, serr := x.parseSchedXstats(kind, b); ok {
			return x, serr
		}
		return x, fmt.Errorf("%w: %q", ErrTcKindUnknown, kind)
	}
	x.Has = err == nil
	return x, err
}

// ErrTcDumpType is a BuildTcDumpRequest type other than RTM_GETQDISC or
// RTM_GETTCLASS.
var ErrTcDumpType = errors.New("xtcpnl: tc dump request type is not RTM_GETQDISC or RTM_GETTCLASS")

// BuildTcDumpRequest builds the dump tc_qdisc_list and tc_class_list send
// (tc/tc_qdisc.c, tc/tc_class.c): an AF_UNSPEC tcmsg carrying ifindex,
// handle and parent, rtnl_dump_request_n's NLM_F_DUMP, and for `qdisc show
// invisible` a zero-length TCA_DUMP_INVISIBLE (addattr). Class lists never
// set the handle; their callers pass 0.
func BuildTcDumpRequest(msgType uint16, ifindex, parent, handle uint32, dumpInvisible bool, seq uint32) ([]byte, error) {
	if msgType != uint16(unix.RTM_GETQDISC) && msgType != uint16(unix.RTM_GETTCLASS) {
		return nil, ErrTcDumpType
	}
	hdr := make([]byte, 0, TcMsgSizeCst)
	hdr = append(hdr, unix.AF_UNSPEC, 0, 0, 0) // tcm_family, tcm__pad1, tcm__pad2
	hdr = binary.LittleEndian.AppendUint32(hdr, ifindex)
	hdr = binary.LittleEndian.AppendUint32(hdr, handle)
	hdr = binary.LittleEndian.AppendUint32(hdr, parent)
	hdr = binary.LittleEndian.AppendUint32(hdr, 0) // tcm_info
	var attrs []byte
	if dumpInvisible {
		attrs = make([]byte, 0, RTAttrSizeCst)
		attrs = binary.LittleEndian.AppendUint16(attrs, RTAttrSizeCst)
		attrs = binary.LittleEndian.AppendUint16(attrs, TcaDumpInvisible)
	}
	return BuildRequest(msgType, uint16(unix.NLM_F_DUMP), seq, hdr, attrs)
}
