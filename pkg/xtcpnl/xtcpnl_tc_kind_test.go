package xtcpnl

import (
	"bytes"
	"encoding/binary"
	"errors"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/randomizedcoder/xtcp2/pkg/cint"
	"golang.org/x/sys/unix"
)

// The per-kind decoders (xtcpnl_tc_kind.go, xtcpnl_tc_qopt.go,
// xtcpnl_tc_stats.go) against hand-built payloads at each tc printer's
// length checks, and against every qdisc and class the workbench corpus
// holds.
//
// go test ./pkg/xtcpnl/ -run 'TestParseTc|TestBuildTcDump|TestWsTcKinds|TestSplitTcCompat'

// tcRateSpecBytes is a struct tc_ratespec.
// cellAlign is the C short's bits: 0xffff is -1.
func tcRateSpecBytes(cellLog, linklayer uint8, overhead, cellAlign, mpu uint16, rate uint32) []byte {
	return cat([]byte{cellLog, linklayer}, le16(overhead), le16(cellAlign), le16(mpu), le32(rate))
}

// tcHtbOptBytes is a struct tc_htb_opt with the given rate, ceil and prio.
func tcHtbOptBytes(rate, ceil, quantum, level, prio uint32) []byte {
	return cat(tcRateSpecBytes(0, 1, 0, 0xffff, 0, rate), tcRateSpecBytes(0, 1, 0, 0xffff, 0, ceil),
		le32(1875), le32(3750), le32(quantum), le32(level), le32(prio))
}

// tcPrioBytes is a struct tc_prio_qopt with pfifo_fast's default priomap.
func tcPrioBytes(bands uint32) []byte {
	return cat(le32(bands), []byte{1, 2, 2, 2, 1, 2, 0, 0, 1, 1, 1, 1, 1, 1, 1, 1})
}

func tcNetemQoptBytes(limit uint32) []byte {
	return cat(le32(100000), le32(limit), le32(0), le32(0), le32(0), le32(10000))
}

func TestParseTcQopt(t *testing.T) {
	sfqV0 := cat(le32(1514), le32(10), le32(127), le32(1024), le32(128))
	sfqV1 := cat(sfqV0, le32(127), le32(0), le32(0), le32(0), le32(0), []byte{0, 0, 0, 0}, le32(0), make([]byte, 24))
	tests := []struct {
		description string
		kind        string
		opt         []byte
		expectedErr error
		expected    func(t *testing.T, q TcQopt)
	}{
		{
			description: "positive: fq_codel's nest (target 4999, limit 10240, ecn 1)",
			kind:        TcKindFqCodel,
			opt: cat(rtattr(TcaFqCodelTarget, le32(4999)), rtattr(TcaFqCodelLimit, le32(10240)),
				rtattr(TcaFqCodelInterval, le32(99999)), rtattr(TcaFqCodelEcn, le32(1)),
				rtattr(TcaFqCodelFlows, le32(1024)), rtattr(TcaFqCodelQuantum, le32(1514)),
				rtattr(TcaFqCodelCeThresholdSelector, []byte{0x40})),
			expected: func(t *testing.T, q TcQopt) {
				for _, w := range []struct {
					a uint16
					v uint32
				}{{TcaFqCodelTarget, 4999}, {TcaFqCodelLimit, 10240}, {TcaFqCodelEcn, 1}, {TcaFqCodelFlows, 1024}} {
					if v, ok := q.Tb.U32(w.a); !ok || v != w.v {
						t.Errorf("attr %d = %d %v, expected %d", w.a, v, ok, w.v)
					}
				}
				if v, ok := q.Tb.U8(TcaFqCodelCeThresholdSelector); !ok || v != 0x40 {
					t.Errorf("ce_threshold_selector = %#x %v", v, ok)
				}
			},
		},
		{
			description: "positive: htb class at rate 1000gbit: RATE64 125000000000, rate32 0xffffffff",
			kind:        TcKindHtb,
			opt: cat(rtattr(TcaHtbParms, tcHtbOptBytes(0xffffffff, 0xffffffff, 200000, 7, 0)),
				rtattr(TcaHtbRate64, le64(125000000000)), rtattr(TcaHtbCeil64, le64(125000000000))),
			expected: func(t *testing.T, q TcQopt) {
				if !q.HasHtbParms || q.HtbParms.Rate.Rate != 0xffffffff || q.HtbParms.Level != 7 || q.HtbParms.Quantum != 200000 {
					t.Errorf("parms = %+v", q.HtbParms)
				}
				if q.HtbParms.Rate.CellAlign != -1 || q.HtbParms.Rate.Linklayer != 1 || q.HtbParms.Cbuffer != 3750 {
					t.Errorf("ratespec = %+v cbuffer %d", q.HtbParms.Rate, q.HtbParms.Cbuffer)
				}
				if v, ok := q.Tb.U64(TcaHtbRate64); !ok || v != 125000000000 {
					t.Errorf("rate64 = %d %v", v, ok)
				}
			},
		},
		{
			description: "positive: htb qdisc's TCA_HTB_INIT and DIRECT_QLEN",
			kind:        TcKindHtb,
			opt:         cat(rtattr(TcaHtbInit, cat(le32(3), le32(5), le32(0x10), le32(0), le32(7))), rtattr(TcaHtbDirectQlen, le32(1000))),
			expected: func(t *testing.T, q TcQopt) {
				if !q.HasHtbInit || q.HasHtbParms || q.HtbInit != (TcHtbGlob{Version: 3, Rate2Quantum: 5, Defcls: 0x10, DirectPkts: 7}) {
					t.Errorf("init = %+v (parms %v)", q.HtbInit, q.HasHtbParms)
				}
			},
		},
		{
			description: "positive: netem LATENCY64 and a negative JITTER64 read as s64 bits",
			kind:        TcKindNetem,
			opt: cat(tcNetemQoptBytes(1000), rtattr(TcaNetemLatency64, le64(100000000)),
				rtattr(TcaNetemJitter64, le64(0xffffffffffffffff)), rtattr(TcaNetemCorr, cat(le32(1), le32(2), le32(3)))),
			expected: func(t *testing.T, q TcQopt) {
				if q.Netem.Limit != 1000 || q.Netem.Jitter != 10000 {
					t.Errorf("qopt = %+v", q.Netem)
				}
				if v, ok := q.Tb.U64(TcaNetemLatency64); !ok || v != 100000000 {
					t.Errorf("latency64 = %d", v)
				}
				if v, _ := q.Tb.U64(TcaNetemJitter64); cint.LongLong(v) != -1 {
					t.Errorf("jitter64 = %d", v)
				}
				if !q.HasNetemCorr || q.NetemCorr != (TcNetemCorr{1, 2, 3}) {
					t.Errorf("corr = %+v", q.NetemCorr)
				}
			},
		},
		{
			description: "positive: netem slot, rate and gemodel loss",
			kind:        TcKindNetem,
			opt: cat(tcNetemQoptBytes(1000),
				rtattr(TcaNetemSlot, cat(le64(1000), le64(2000), le32(uint32(0xffffffff)), le32(64), le64(0), le64(0))),
				rtattr(TcaNetemRate, cat(le32(125000), le32(0xfffffff6), le32(0), le32(0))),
				rtattr(TcaNetemLoss|unix.NLA_F_NESTED, rtattr(NetemLossGE, cat(le32(1), le32(2), le32(3), le32(4))))),
			expected: func(t *testing.T, q TcQopt) {
				if !q.HasNetemSlot || q.NetemSlot.MaxPackets != -1 || q.NetemSlot.MaxDelay != 2000 {
					t.Errorf("slot = %+v", q.NetemSlot)
				}
				if !q.HasNetemRate || q.NetemRate.PacketOverhead != -10 {
					t.Errorf("rate = %+v", q.NetemRate)
				}
				if q.HasNetemGe || q.HasNetemGi {
					t.Error("parse_rtattr masks no flag: a TCA_NETEM_LOSS with NLA_F_NESTED is past max and dropped")
				}
			},
		},
		{
			description: "positive: netem gemodel loss without the nest flag",
			kind:        TcKindNetem,
			opt: cat(tcNetemQoptBytes(1000),
				rtattr(TcaNetemLoss, rtattr(NetemLossGE|unix.NLA_F_NESTED, cat(le32(1), le32(2), le32(3), le32(4))))),
			expected: func(t *testing.T, q TcQopt) {
				if !q.HasNetemGe || q.NetemGe != (TcNetemGemodel{1, 2, 3, 4}) {
					t.Errorf("ge = %+v %v (the loss nest masks NLA_F_NESTED)", q.NetemGe, q.HasNetemGe)
				}
			},
		},
		{
			description: "positive: pfifo_fast is prio's compat struct with no nest",
			kind:        TcKindPfifoFast,
			opt:         tcPrioBytes(3),
			expected: func(t *testing.T, q TcQopt) {
				if !q.HasPrio || q.Prio.Bands != 3 || q.Prio.Priomap[0] != 1 || q.Tb.Len() != 0 {
					t.Errorf("prio = %+v tb %d", q.Prio, q.Tb.Len())
				}
			},
		},
		{
			description: "positive: prio with a TCA_PRIO_MQ compat nest",
			kind:        TcKindPrio,
			opt:         cat(tcPrioBytes(5), rtattr(TcaOptions, rtattr(TcaPrioMQ, []byte{1}))),
			expected: func(t *testing.T, q TcQopt) {
				if v, ok := q.Tb.U8(TcaPrioMQ); !ok || v != 1 || q.Prio.Bands != 5 {
					t.Errorf("mq = %d %v bands %d", v, ok, q.Prio.Bands)
				}
			},
		},
		{
			description: "positive: bfifo limit",
			kind:        TcKindBfifo,
			opt:         le32(3000),
			expected: func(t *testing.T, q TcQopt) {
				if !q.HasFifo || q.Fifo.Limit != 3000 {
					t.Errorf("fifo = %+v", q.Fifo)
				}
			},
		},
		{
			description: "positive: sfq_qopt_v1",
			kind:        TcKindSfq,
			opt:         sfqV1,
			expected: func(t *testing.T, q TcQopt) {
				if !q.HasSfqV1 || q.SfqV1.Depth != 127 || q.Sfq.PerturbPeriod != 10 || q.Sfq.Divisor != 1024 {
					t.Errorf("sfq = %+v v1 %+v", q.Sfq, q.SfqV1)
				}
			},
		},
		{
			description: "positive: fq's priomap and weights",
			kind:        TcKindFq,
			opt: cat(rtattr(TcaFqPriomap, tcPrioBytes(3)),
				rtattr(TcaFqWeights, cat(le32(65536), le32(32768), le32(16384))), rtattr(TcaFqPlimit, le32(10000))),
			expected: func(t *testing.T, q TcQopt) {
				if !q.HasFqPriomap || !q.HasFqWeights || q.FqWeights != [FqBandsCst]int32{65536, 32768, 16384} {
					t.Errorf("fq = %+v", q)
				}
			},
		},
		{
			description: "positive: tbf parms",
			kind:        TcKindTbf,
			opt:         rtattr(TcaTbfParms, cat(tcRateSpecBytes(0, 1, 0, 0xffff, 0, 1000), tcRateSpecBytes(0, 0, 0, 0, 0, 0), le32(10000), le32(1600), le32(1514))),
			expected: func(t *testing.T, q TcQopt) {
				if q.Tbf.Rate.Rate != 1000 || q.Tbf.Limit != 10000 || q.Tbf.Mtu != 1514 {
					t.Errorf("tbf = %+v", q.Tbf)
				}
			},
		},
		{
			description: "positive: noqueue decodes to its kind alone",
			kind:        TcKindNoqueue,
			expected: func(t *testing.T, q TcQopt) {
				if q.Kind != TcKindNoqueue || q.Tb.Len() != 0 {
					t.Errorf("q = %+v", q)
				}
			},
		},
		{
			description: "negative: a tc_htb_opt one byte short is ErrTcShort (htb_print_opt returns -1)",
			kind:        TcKindHtb,
			opt:         rtattr(TcaHtbParms, tcHtbOptBytes(1, 1, 1, 0, 0)[:TcHtbOptSizeCst-1]),
			expectedErr: ErrTcShort,
		},
		{
			description: "negative: a 3-byte prio TCA_OPTIONS is ErrTcShort",
			kind:        TcKindPrio,
			opt:         []byte{3, 0, 0},
			expectedErr: ErrTcShort,
		},
		{
			description: "negative: tbf without TCA_TBF_PARMS is ErrTcMissing",
			kind:        TcKindTbf,
			opt:         rtattr(TcaTbfRate64, le64(1)),
			expectedErr: ErrTcMissing,
		},
		{
			description: "negative: netem's 23 bytes is ErrTcShort (\"options size error\")",
			kind:        TcKindNetem,
			opt:         tcNetemQoptBytes(1)[:TcNetemQoptSizeCst-1],
			expectedErr: ErrTcShort,
		},
		{
			description: "negative: a short NETEM_LOSS_GI is ErrTcShort where tc reads past it (Q8)",
			kind:        TcKindNetem,
			opt:         cat(tcNetemQoptBytes(1), rtattr(TcaNetemLoss, rtattr(NetemLossGI, le32(1)))),
			expectedErr: ErrTcShort,
		},
		{
			description: "negative: a short TCA_NETEM_JITTER64 is ErrTcShort",
			kind:        TcKindNetem,
			opt:         cat(tcNetemQoptBytes(1), rtattr(TcaNetemJitter64, le32(1))),
			expectedErr: ErrTcShort,
		},
		{
			description: "negative: a compat nest whose rta_len overruns is ErrTcCompatNest",
			kind:        TcKindPrio,
			opt:         cat(tcPrioBytes(3), le16(64), le16(TcaOptions)),
			expectedErr: ErrTcCompatNest,
		},
		{
			description: "boundary: a zero-length TCA_PRIO_MQ is present but has no u8 (Q27)",
			kind:        TcKindPrio,
			opt:         cat(tcPrioBytes(3), rtattr(TcaOptions, rtattr(TcaPrioMQ, nil))),
			expected: func(t *testing.T, q TcQopt) {
				if !q.Tb.Has(TcaPrioMQ) {
					t.Error("TCA_PRIO_MQ absent")
				}
				if _, ok := q.Tb.U8(TcaPrioMQ); ok {
					t.Error("a zero-length TCA_PRIO_MQ read a u8")
				}
			},
		},
		{
			description: "boundary: prio with 3 bytes past the struct, short of an rtattr header, has no nest",
			kind:        TcKindPrio,
			opt:         cat(tcPrioBytes(3), []byte{8, 0, 1}),
			expected: func(t *testing.T, q TcQopt) {
				if q.Tb.Len() != 0 {
					t.Errorf("tb has %d slots", q.Tb.Len())
				}
			},
		},
		{
			description: "boundary: a 19-byte TCA_FQ_PRIOMAP fails fq's >= sizeof test and is skipped",
			kind:        TcKindFq,
			opt:         rtattr(TcaFqPriomap, tcPrioBytes(3)[:TcPrioQoptSizeCst-1]),
			expected: func(t *testing.T, q TcQopt) {
				if q.HasFqPriomap {
					t.Error("short priomap decoded")
				}
			},
		},
		{
			description: "boundary: sfq at 71 bytes is the v0 struct alone",
			kind:        TcKindSfq,
			opt:         sfqV1[:TcSfqQoptV1SizeCst-1],
			expected: func(t *testing.T, q TcQopt) {
				if q.HasSfqV1 || q.Sfq.Quantum != 1514 {
					t.Errorf("sfq = %+v v1 %v", q.Sfq, q.HasSfqV1)
				}
			},
		},
		{
			description: "corner: an unknown kind is ErrTcKindUnknown",
			kind:        "cake",
			opt:         rtattr(1, le32(1)),
			expectedErr: ErrTcKindUnknown,
		},
		{
			description: "corner: a compat inner rta_len below 4 is an empty nest, as RTA_OK sees nothing",
			kind:        TcKindPfifoFast,
			opt:         cat(tcPrioBytes(3), le16(2), le16(TcaOptions)),
			expected: func(t *testing.T, q TcQopt) {
				if q.Tb.Has(TcaPrioMQ) {
					t.Error("empty nest has TCA_PRIO_MQ")
				}
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.description, func(t *testing.T) {
			q, err := ParseTcQopt(tt.kind, tt.opt)
			if tt.expectedErr != nil {
				if !errors.Is(err, tt.expectedErr) {
					t.Fatalf("err = %v, expected %v", err, tt.expectedErr)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			tt.expected(t, q)
		})
	}
}

func TestSplitTcCompat(t *testing.T) {
	tests := []struct {
		description string
		opt         []byte
		hdrLen      int
		expectedHdr int
		expectedNil bool
		expectedErr error
	}{
		{"positive: header and nest", cat(make([]byte, 20), rtattr(1, []byte{1})), 20, 20, false, nil},
		{"negative: shorter than the header", make([]byte, 19), 20, 0, true, ErrTcShort},
		{"negative: a negative header length", make([]byte, 4), -1, 0, true, ErrTcShort},
		{"boundary: exactly the header", make([]byte, 20), 20, 20, true, nil},
		{"boundary: an unaligned header is followed at RTA_ALIGN", cat(make([]byte, 6), []byte{0, 0}, rtattr(1, nil)), 6, 6, false, nil},
		{"corner: an unaligned header with too little after the pad", cat(make([]byte, 6), []byte{0, 0, 4, 0, 1}), 6, 6, true, nil},
	}
	for _, tt := range tests {
		t.Run(tt.description, func(t *testing.T) {
			hdr, nest, err := SplitTcCompat(tt.opt, tt.hdrLen)
			if !errors.Is(err, tt.expectedErr) {
				t.Fatalf("err = %v, expected %v", err, tt.expectedErr)
			}
			if err != nil {
				return
			}
			if len(hdr) != tt.expectedHdr || (nest == nil) != tt.expectedNil {
				t.Fatalf("hdr %d nest %v, expected %d nil=%v", len(hdr), nest, tt.expectedHdr, tt.expectedNil)
			}
		})
	}
}

func TestParseTcStats2(t *testing.T) {
	basic := cat(le64(1234), le32(5), le32(0))
	queue := cat(le32(1), le32(2), le32(3), le32(4), le32(5))
	tests := []struct {
		description string
		nest        []byte
		expectedErr bool
		expected    func(t *testing.T, s TcStats2)
	}{
		{
			description: "positive: basic, rate_est64 and queue",
			nest: cat(rtattr(TcaStatsBasic, basic), rtattr(TcaStatsRateEst64, cat(le64(8000), le64(10))),
				rtattr(TcaStatsQueue, queue)),
			expected: func(t *testing.T, s TcStats2) {
				if s.Basic != (GnetStatsBasic{1234, 5}) || s.RateEst64 != (GnetStatsRateEst64{8000, 10}) {
					t.Errorf("s = %+v", s)
				}
				if s.Queue != (GnetStatsQueue{Qlen: 1, Backlog: 2, Drops: 3, Requeues: 4, Overlimits: 5}) || s.QueueLen != 20 {
					t.Errorf("queue = %+v", s.Queue)
				}
				if s.RateEstLen != -1 || s.BasicHwLen != -1 || s.HasApp {
					t.Errorf("absent fields: %+v", s)
				}
			},
		},
		{
			description: "positive: PKT64 after BASIC is Packets64, after BASIC_HW Packets64Hw",
			nest: cat(rtattr(TcaStatsBasic, basic), rtattr(TcaStatsPkt64, le64(1<<40)),
				rtattr(TcaStatsBasicHw, basic), rtattr(TcaStatsPkt64, le64(7))),
			expected: func(t *testing.T, s TcStats2) {
				if s.Packets64 != 1<<40 || s.Packets64Hw != 7 {
					t.Errorf("pkts %d hw %d", s.Packets64, s.Packets64Hw)
				}
			},
		},
		{
			description: "positive: APP is kept for the kind's xstats",
			nest:        rtattr(TcaStatsApp|unix.NLA_F_NESTED, le32(42)),
			expected: func(t *testing.T, s TcStats2) {
				if !s.HasApp || !bytes.Equal(s.App, le32(42)) {
					t.Errorf("app = %v %v", s.App, s.HasApp)
				}
			},
		},
		{
			description: "negative: an attribute overrunning the nest",
			nest:        rtattrOverrunning(TcaStatsBasic, 64),
			expectedErr: true,
		},
		{
			description: "boundary: an 8-byte BASIC_HW sets bytes, packets 0, length 8 (Q16)",
			nest:        rtattr(TcaStatsBasicHw, le64(99)),
			expected: func(t *testing.T, s TcStats2) {
				if s.BasicHw != (GnetStatsBasic{Bytes: 99}) || s.BasicHwLen != 8 {
					t.Errorf("hw = %+v len %d", s.BasicHw, s.BasicHwLen)
				}
			},
		},
		{
			description: "boundary: a 24-byte QUEUE reads its first 20",
			nest:        rtattr(TcaStatsQueue, cat(queue, le32(77))),
			expected: func(t *testing.T, s TcStats2) {
				if s.Queue.Overlimits != 5 || s.QueueLen != 24 {
					t.Errorf("queue = %+v len %d", s.Queue, s.QueueLen)
				}
			},
		},
		{
			description: "boundary: an empty nest",
			nest:        nil,
			expected: func(t *testing.T, s TcStats2) {
				if s.BasicLen != -1 || s.Packets64 != 0 {
					t.Errorf("s = %+v", s)
				}
			},
		},
		{
			description: "corner: a PKT64 first in the nest follows nothing and is ignored",
			nest:        cat(rtattr(TcaStatsPkt64, le64(5)), rtattr(TcaStatsBasic, basic)),
			expected: func(t *testing.T, s TcStats2) {
				if s.Packets64 != 0 {
					t.Errorf("pkts %d", s.Packets64)
				}
			},
		},
		{
			description: "corner: a second BASIC loses to the first",
			nest:        cat(rtattr(TcaStatsBasic, basic), rtattr(TcaStatsBasic, cat(le64(1), le32(1), le32(0)))),
			expected: func(t *testing.T, s TcStats2) {
				if s.Basic.Bytes != 1234 {
					t.Errorf("bytes %d", s.Basic.Bytes)
				}
			},
		},
		{
			description: "corner: a short PKT64 reads as its zero-filled prefix",
			nest:        cat(rtattr(TcaStatsBasic, basic), rtattr(TcaStatsPkt64, le32(9))),
			expected: func(t *testing.T, s TcStats2) {
				if s.Packets64 != 9 {
					t.Errorf("pkts %d", s.Packets64)
				}
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.description, func(t *testing.T) {
			s, err := ParseTcStats2(tt.nest)
			if (err != nil) != tt.expectedErr {
				t.Fatalf("err = %v, expected error %v", err, tt.expectedErr)
			}
			if err == nil {
				tt.expected(t, s)
			}
		})
	}
}

func TestDecodeTcStats(t *testing.T) {
	full := cat(le64(100), le32(1), le32(2), le32(3), le32(4), le32(5), le32(6), le32(7), le32(0))
	tests := []struct {
		description string
		in          []byte
		expected    TcStats
	}{
		{"positive: the compat struct tc_stats (Q17)", full, TcStats{100, 1, 2, 3, 4, 5, 6, 7}},
		{"boundary: a short block reads zero past its end", full[:12], TcStats{Bytes: 100, Packets: 1}},
		{"boundary: a long block reads its first 40", cat(full, le32(9)), TcStats{100, 1, 2, 3, 4, 5, 6, 7}},
		{"corner: empty", nil, TcStats{}},
	}
	for _, tt := range tests {
		t.Run(tt.description, func(t *testing.T) {
			if got := DecodeTcStats(tt.in); got != tt.expected {
				t.Fatalf("got %+v, expected %+v", got, tt.expected)
			}
		})
	}
}

func TestParseTcXstats(t *testing.T) {
	fqCodelQd := cat(le32(TcaFqCodelXstatsQdisc), le32(1514), le32(2), le32(3), le32(4), le32(5), le32(6), le32(7), le32(8), le32(9))
	fqCodelCl := cat(le32(TcaFqCodelXstatsClass), le32(0xfffffff0), le32(2), le32(3), le32(4), le32(1), le32(0xffffffff), make([]byte, 12))
	fq := make([]byte, TcFqQdStatsSizeCst)
	copy(fq[24:], le64(77))                        // throttled
	copy(fq[tcFqBandDropsOffsetCst+16:], le64(3))  // band_drops[2]
	copy(fq[tcFqBandPktCountOffsetCst:], le32(11)) // band_pkt_count[0]
	tests := []struct {
		description string
		kind        string
		in          []byte
		expectedErr error
		expected    func(t *testing.T, x TcXstats)
	}{
		{
			description: "positive: fq_codel qdisc stats",
			kind:        TcKindFqCodel, in: fqCodelQd,
			expected: func(t *testing.T, x TcXstats) {
				if x.FqCodel.Qdisc.Maxpacket != 1514 || x.FqCodel.Qdisc.DropOvermemory != 9 || !x.Has {
					t.Errorf("x = %+v", x.FqCodel)
				}
			},
		},
		{
			description: "positive: fq_codel class stats, signed deficit and drop_next",
			kind:        TcKindFqCodel, in: fqCodelCl,
			expected: func(t *testing.T, x TcXstats) {
				if x.FqCodel.Type != TcaFqCodelXstatsClass || x.FqCodel.Class.Deficit != -16 || x.FqCodel.Class.DropNext != -1 {
					t.Errorf("x = %+v", x.FqCodel.Class)
				}
			},
		},
		{
			description: "positive: fq's throttled and band arrays",
			kind:        TcKindFq, in: fq,
			expected: func(t *testing.T, x TcXstats) {
				if x.Fq.Throttled != 77 || x.Fq.BandDrops[2] != 3 || x.Fq.BandPktCount[0] != 11 {
					t.Errorf("x = %+v", x.Fq)
				}
			},
		},
		{
			description: "positive: htb xstats with negative tokens",
			kind:        TcKindHtb, in: cat(le32(1), le32(2), le32(0), le32(0xffffff00), le32(5)),
			expected: func(t *testing.T, x TcXstats) {
				if x.Htb.Tokens != -256 || x.Htb.Ctokens != 5 {
					t.Errorf("x = %+v", x.Htb)
				}
			},
		},
		{
			description: "positive: sfq allot",
			kind:        TcKindSfq, in: le32(0xfffffffe),
			expected: func(t *testing.T, x TcXstats) {
				if x.Sfq.Allot != -2 {
					t.Errorf("allot %d", x.Sfq.Allot)
				}
			},
		},
		{
			description: "negative: a short htb xstats is ErrTcShort (htb_print_xstats returns -1)",
			kind:        TcKindHtb, in: make([]byte, TcHtbXstatsSizeCst-1),
			expectedErr: ErrTcShort,
		},
		{
			description: "negative: a short sfq xstats is ErrTcShort",
			kind:        TcKindSfq, in: []byte{1, 2, 3},
			expectedErr: ErrTcShort,
		},
		{
			description: "boundary: a short fq_codel xstats is MIN-copied, the rest zero",
			kind:        TcKindFqCodel, in: fqCodelQd[:8],
			expected: func(t *testing.T, x TcXstats) {
				if x.FqCodel.Qdisc.Maxpacket != 1514 || x.FqCodel.Qdisc.DropOverlimit != 0 {
					t.Errorf("x = %+v", x.FqCodel.Qdisc)
				}
			},
		},
		{
			description: "boundary: netem's 48-byte xstats from an older kernel has no allocation_errors",
			kind:        TcKindNetem, in: cat(le64(1), le64(2), le64(3), le64(4), le64(5), le64(6)),
			expected: func(t *testing.T, x TcXstats) {
				if x.Netem.EcnMarked != 6 || x.Netem.AllocationErrors != 0 {
					t.Errorf("x = %+v", x.Netem)
				}
			},
		},
		{
			description: "corner: prio prints no xstats",
			kind:        TcKindPrio, in: le32(1),
			expected: func(t *testing.T, x TcXstats) {
				if x.Has {
					t.Error("prio xstats decoded")
				}
			},
		},
		{
			description: "corner: an unknown kind is ErrTcKindUnknown",
			kind:        "codel", in: le32(1),
			expectedErr: ErrTcKindUnknown,
		},
	}
	for _, tt := range tests {
		t.Run(tt.description, func(t *testing.T) {
			x, err := ParseTcXstats(tt.kind, tt.in)
			if tt.expectedErr != nil {
				if !errors.Is(err, tt.expectedErr) {
					t.Fatalf("err = %v, expected %v", err, tt.expectedErr)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			tt.expected(t, x)
		})
	}
}

// wsTcShowRequest is the RTM_GETQDISC or RTM_GETTCLASS request in a
// 7_3_1 workbench row's ip.pcap.
func wsTcShowRequest(t *testing.T, id string, typ uint16) []byte {
	t.Helper()
	dir := filepath.Join("testdata", "7_3_1", "ws", id)
	for _, m := range pcapNlMsgs(t, filepath.Join(dir, "ip.pcap")) {
		if m.proto != unix.NETLINK_ROUTE || m.h.Type != typ || m.h.Flags&unix.NLM_F_REQUEST == 0 {
			continue
		}
		hdr := binary.LittleEndian.AppendUint32(nil, m.h.Len)
		hdr = binary.LittleEndian.AppendUint16(hdr, m.h.Type)
		hdr = binary.LittleEndian.AppendUint16(hdr, m.h.Flags)
		hdr = binary.LittleEndian.AppendUint32(hdr, m.h.Seq)
		hdr = binary.LittleEndian.AppendUint32(hdr, m.h.Pid)
		return cat(hdr, m.body)
	}
	t.Fatalf("%s: no request of type %d", id, typ)
	return nil
}

func TestBuildTcDumpRequest(t *testing.T) {
	// The captures' fixtures have d0 as ifindex 3 and d1 as 4.
	const d0, d1 = 3, 4
	q, c := uint16(unix.RTM_GETQDISC), uint16(unix.RTM_GETTCLASS)
	tests := []struct {
		description string
		row         string // a 7_3_1 workbench row whose ip.pcap holds the request
		msgType     uint16
		ifindex     uint32
		parent      uint32
		handle      uint32
		invisible   bool
		expectedErr error
	}{
		{"positive: tc qdisc show", "tcqdisc/ec-010", q, 0, 0, 0, false, nil},
		{"positive: tc qdisc show dev d1", "tcqdisc/ec-013", q, d1, 0, 0, false, nil},
		{"positive: tc qdisc show dev d1 parent 1:2", "tcqdisc/ec-016", q, d1, 0x10002, 0, false, nil},
		{"positive: tc qdisc show handle 3:", "tcqdisc/ec-017", q, 0, 0, 0x30000, false, nil},
		{"positive: tc class show dev d0 parent 1:1", "tcclass/ec-017", c, d0, 0x10001, 0, false, nil},
		{"boundary: tc qdisc show root, parent TC_H_ROOT", "tcqdisc/ec-015", q, 0, tcHRoot, 0, false, nil},
		{"boundary: tc qdisc show ingress, parent TC_H_INGRESS", "tcqdisc/ec-052", q, 0, tcHIngress, 0, false, nil},
		{"corner: tc qdisc show invisible carries a zero-length TCA_DUMP_INVISIBLE", "tcqdisc/ec-018", q, 0, 0, 0, true, nil},
		{"negative: RTM_GETTFILTER is ErrTcDumpType", "", uint16(unix.RTM_GETTFILTER), 0, 0, 0, false, ErrTcDumpType},
		{"negative: RTM_NEWQDISC is ErrTcDumpType", "", uint16(unix.RTM_NEWQDISC), 0, 0, 0, false, ErrTcDumpType},
	}
	for _, tt := range tests {
		t.Run(tt.description, func(t *testing.T) {
			var want []byte
			seq := uint32(1)
			if tt.row != "" {
				want = wsTcShowRequest(t, tt.row, tt.msgType)
				seq = nlSeqOf(want)
			}
			got, err := BuildTcDumpRequest(tt.msgType, tt.ifindex, tt.parent, tt.handle, tt.invisible, seq)
			if !errors.Is(err, tt.expectedErr) {
				t.Fatalf("err = %v, expected %v", err, tt.expectedErr)
			}
			if err == nil && !bytes.Equal(got, want) {
				t.Fatalf("got  % x\nwant % x", got, want)
			}
		})
	}
}

// nlSeqOf is a message's nlmsg_seq.
func nlSeqOf(m []byte) uint32 {
	var h NlMsgHdr
	if _, err := DeserializeNlMsgHdr(m, &h); err != nil {
		return 0
	}
	return h.Seq
}

// wsTcSliceKinds are the kinds gotc prints; TestWsTcKinds requires each to
// appear in the corpus.
var wsTcSliceKinds = []string{
	TcKindNoqueue, TcKindMq, TcKindPfifoFast, TcKindPrio, TcKindPfifo, TcKindBfifo,
	TcKindPfifoHeadDrop, TcKindFqCodel, TcKindFq, TcKindHtb, TcKindTbf, TcKindSfq,
	TcKindNetem, TcKindIngress, TcKindClsact,
	TcKindDrr, TcKindQfq, TcKindHfsc, TcKindEts, TcKindMqprio, TcKindTaprio, TcKindMultiq,
	TcKindCbs, TcKindEtf,
}

// TestWsTcKinds decodes every qdisc and class the kernel sent in any
// workbench capture: an in-slice kind's TCA_OPTIONS, TCA_STATS2 and xstats
// must decode, and any other kind must be ErrTcKindUnknown. Each in-slice
// kind must be seen.
func TestWsTcKinds(t *testing.T) {
	seen := map[string]int{}
	var other int
	for _, dir := range wsRows(t) {
		pcaps, err := filepath.Glob(filepath.Join(dir, "*.pcap"))
		if err != nil {
			t.Fatal(err)
		}
		for _, p := range pcaps {
			if base := filepath.Base(p); base != "ip.pcap" && !strings.HasPrefix(base, "netlink_route_") {
				continue
			}
			for i, m := range pcapNlMsgs(t, p) {
				if m.proto != unix.NETLINK_ROUTE || m.h.Flags&unix.NLM_F_REQUEST != 0 ||
					(m.h.Type != unix.RTM_NEWQDISC && m.h.Type != unix.RTM_NEWTCLASS) {
					continue
				}
				ti, err := ParseTcMsg(m.body)
				if err != nil {
					t.Fatalf("%s message %d: %v", p, i, err)
				}
				if !slices.Contains(wsTcSliceKinds, ti.Kind) {
					if _, err := ParseTcQopt(ti.Kind, ti.Options); !errors.Is(err, ErrTcKindUnknown) {
						t.Errorf("%s message %d: kind %q: err %v, expected ErrTcKindUnknown", p, i, ti.Kind, err)
					}
					other++
					continue
				}
				seen[ti.Kind]++
				if err := wsTcKindProblem(ti, m.h.Type == unix.RTM_NEWTCLASS); err != nil {
					t.Errorf("%s message %d (kind %s): %v", p, i, ti.Kind, err)
				}
			}
		}
	}
	for _, k := range wsTcSliceKinds {
		if seen[k] == 0 {
			t.Errorf("kind %s is in no capture", k)
		}
	}
	t.Logf("decoded %v; %d messages of other kinds", seen, other)
}

func wsTcKindProblem(ti TcInfo, class bool) error {
	parse := ParseTcQopt
	if class {
		parse = ParseTcCopt
	}
	if ti.HasOptions || len(ti.Options) > 0 {
		if _, err := parse(ti.Kind, ti.Options); err != nil {
			return err
		}
	}
	xs, hasX := ti.Xstats, ti.HasXstats
	if ti.HasStats2 {
		s, err := ParseTcStats2(ti.Stats2)
		if err != nil {
			return err
		}
		if s.HasApp {
			xs, hasX = s.App, true
		}
	}
	if hasX {
		if _, err := ParseTcXstats(ti.Kind, xs); err != nil {
			return err
		}
	}
	return nil
}

// FuzzParseTcStats2 must never panic, and the structures it decodes never
// alias the nest.
func FuzzParseTcStats2(f *testing.F) {
	for _, b := range wsTcSeeds(f, map[uint16]bool{unix.RTM_NEWQDISC: true, unix.RTM_NEWTCLASS: true}) {
		if ti, err := ParseTcMsg(b); err == nil && ti.HasStats2 {
			f.Add(ti.Stats2)
		}
	}
	f.Add(rtattr(TcaStatsBasicHw, le64(1)))
	f.Fuzz(func(t *testing.T, in []byte) {
		s, err := ParseTcStats2(in)
		if err != nil {
			return
		}
		if s.BasicLen > len(in) || s.QueueLen > len(in) {
			t.Fatalf("lengths past the nest: %+v", s)
		}
	})
}

// FuzzParseTcQopt runs every kind's decoder, the kind being input too.
func FuzzParseTcQopt(f *testing.F) {
	for _, b := range wsTcSeeds(f, map[uint16]bool{unix.RTM_NEWQDISC: true, unix.RTM_NEWTCLASS: true}) {
		if ti, err := ParseTcMsg(b); err == nil && ti.HasOptions {
			f.Add(ti.Kind, ti.Options)
		}
	}
	for _, k := range wsTcSliceKinds {
		f.Add(k, []byte{})
	}
	f.Fuzz(func(t *testing.T, kind string, in []byte) {
		q, err := ParseTcQopt(kind, in)
		if err != nil {
			return
		}
		if q.Kind != kind {
			t.Fatalf("kind %q, expected %q", q.Kind, kind)
		}
		if _, err := ParseTcXstats(kind, in); err != nil && !errors.Is(err, ErrTcShort) {
			t.Fatalf("xstats for a decodable kind: %v", err)
		}
		if c, err := ParseTcCopt(kind, in); err == nil && c.Kind != kind {
			t.Fatalf("class kind %q, expected %q", c.Kind, kind)
		}
	})
}

func BenchmarkParseTcQopt(b *testing.B) {
	cases := []struct {
		kind string
		opt  []byte
	}{
		{TcKindFqCodel, cat(rtattr(TcaFqCodelTarget, le32(4999)), rtattr(TcaFqCodelLimit, le32(10240)), rtattr(TcaFqCodelFlows, le32(1024)))},
		{TcKindHtb, cat(rtattr(TcaHtbParms, tcHtbOptBytes(1, 1, 1, 0, 0)), rtattr(TcaHtbRate64, le64(1)))},
		{TcKindNetem, cat(tcNetemQoptBytes(1000), rtattr(TcaNetemLatency64, le64(1)), rtattr(TcaNetemJitter64, le64(1)))},
		{TcKindPfifoFast, tcPrioBytes(3)},
		{TcKindTbf, rtattr(TcaTbfParms, make([]byte, TcTbfQoptSizeCst))},
	}
	for _, c := range cases {
		b.Run(c.kind, func(b *testing.B) {
			b.ReportAllocs()
			for b.Loop() {
				if _, err := ParseTcQopt(c.kind, c.opt); err != nil {
					b.Fatal(err)
				}
			}
		})
	}
}

func BenchmarkParseTcStats2(b *testing.B) {
	nest := cat(rtattr(TcaStatsBasic, make([]byte, GnetStatsBasicSizeCst)), rtattr(TcaStatsRateEst64, make([]byte, 16)),
		rtattr(TcaStatsQueue, make([]byte, GnetStatsQueueSizeCst)), rtattr(TcaStatsApp, make([]byte, TcFqCodelXstatsSizeCst)))
	b.ReportAllocs()
	for b.Loop() {
		if _, err := ParseTcStats2(nest); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkParseTcXstats(b *testing.B) {
	for _, c := range []struct {
		kind string
		n    int
	}{{TcKindFqCodel, TcFqCodelXstatsSizeCst}, {TcKindFq, TcFqQdStatsSizeCst}, {TcKindHtb, TcHtbXstatsSizeCst}} {
		in := make([]byte, c.n)
		b.Run(c.kind, func(b *testing.B) {
			b.ReportAllocs()
			for b.Loop() {
				if _, err := ParseTcXstats(c.kind, in); err != nil {
					b.Fatal(err)
				}
			}
		})
	}
}
