package xtcpnl

import (
	"encoding/binary"

	"github.com/randomizedcoder/xtcp2/pkg/cint"
)

// This file is the encode half of xtcpnl_tc_qopt.go: an Append per struct a
// tc qdisc or class write carries, laid out field for field as
// include/uapi/linux/pkt_sched.h has it at v7.4-rc4, so decode(Append(x)) is
// x. They encode and nothing more; the attribute each goes in, and the
// order, are the tc parser's business (tc/q_*.c at iproute2 v7.2.0).
//
// They are not behind a write tag: laying out a struct sends nothing, and the
// round-trip tests want every build.

// TcEstimatorSizeCst is sizeof(struct tc_estimator).
const TcEstimatorSizeCst = 2

// TcEstimator is struct tc_estimator (pkt_sched.h:46), TCA_RATE's payload:
// the interval as a power of two of a quarter second, and the EWMA log.
type TcEstimator struct {
	Interval int8
	EwmaLog  uint8
}

// DecodeTcEstimator decodes a struct tc_estimator.
func DecodeTcEstimator(b []byte) (TcEstimator, error) {
	if err := tcNeed(b, TcEstimatorSizeCst, "tc_estimator"); err != nil {
		return TcEstimator{}, err
	}
	r := tcRd{b: b}
	return TcEstimator{Interval: cint.SChar(r.u8()), EwmaLog: r.u8()}, nil
}

// Append lays out e (pkt_sched.h:46).
func (e TcEstimator) Append(b []byte) []byte {
	return append(b, cint.SCharBits(e.Interval), e.EwmaLog)
}

func appendU16(b []byte, v uint16) []byte { return binary.LittleEndian.AppendUint16(b, v) }
func appendU32(b []byte, v uint32) []byte { return binary.LittleEndian.AppendUint32(b, v) }
func appendU64(b []byte, v uint64) []byte { return binary.LittleEndian.AppendUint64(b, v) }
func appendS16(b []byte, v int16) []byte  { return appendU16(b, cint.ShortBits(v)) }
func appendS32(b []byte, v int32) []byte  { return appendU32(b, cint.Bits(v)) }
func appendS64(b []byte, v int64) []byte  { return appendU64(b, cint.U64Bits(v)) }

// Append lays out r, struct tc_ratespec (pkt_sched.h:91).
func (r TcRateSpec) Append(b []byte) []byte {
	b = append(b, r.CellLog, r.Linklayer)
	b = appendU16(b, r.Overhead)
	b = appendS16(b, r.CellAlign)
	b = appendU16(b, r.Mpu)
	return appendU32(b, r.Rate)
}

// Append lays out s, struct tc_sizespec (pkt_sched.h:102).
func (s TcSizespec) Append(b []byte) []byte {
	b = append(b, s.CellLog, s.SizeLog)
	b = appendS16(b, s.CellAlign)
	b = appendS32(b, s.Overhead)
	b = appendU32(b, s.Linklayer)
	b = appendU32(b, s.Mpu)
	b = appendU32(b, s.Mtu)
	return appendU32(b, s.Tsize)
}

// Append lays out q, struct tc_fifo_qopt (pkt_sched.h:124).
func (q TcFifoQopt) Append(b []byte) []byte { return appendU32(b, q.Limit) }

// Append lays out q, struct tc_prio_qopt (pkt_sched.h:148): bands, then
// the sixteen-byte priomap.
func (q TcPrioQopt) Append(b []byte) []byte {
	b = appendS32(b, q.Bands)
	return append(b, q.Priomap[:]...)
}

// Append lays out o, struct tc_htb_opt (pkt_sched.h:409).
func (o TcHtbOpt) Append(b []byte) []byte {
	b = o.Rate.Append(b)
	b = o.Ceil.Append(b)
	b = appendU32(b, o.Buffer)
	b = appendU32(b, o.Cbuffer)
	b = appendU32(b, o.Quantum)
	b = appendU32(b, o.Level)
	return appendU32(b, o.Prio)
}

// Append lays out g, struct tc_htb_glob (pkt_sched.h:418).
func (g TcHtbGlob) Append(b []byte) []byte {
	b = appendU32(b, g.Version)
	b = appendU32(b, g.Rate2Quantum)
	b = appendU32(b, g.Defcls)
	b = appendU32(b, g.Debug)
	return appendU32(b, g.DirectPkts)
}

// Append lays out q, struct tc_tbf_qopt (pkt_sched.h:183).
func (q TcTbfQopt) Append(b []byte) []byte {
	b = q.Rate.Append(b)
	b = q.Peakrate.Append(b)
	b = appendU32(b, q.Limit)
	b = appendU32(b, q.Buffer)
	return appendU32(b, q.Mtu)
}

// Append lays out q, struct tc_sfq_qopt (pkt_sched.h:213).
func (q TcSfqQopt) Append(b []byte) []byte {
	b = appendU32(b, q.Quantum)
	b = appendS32(b, q.PerturbPeriod)
	b = appendU32(b, q.Limit)
	b = appendU32(b, q.Divisor)
	return appendU32(b, q.Flows)
}

// Append lays out s, struct tc_sfqred_stats (pkt_sched.h:221).
func (s TcSfqredStats) Append(b []byte) []byte {
	for _, v := range [...]uint32{s.ProbDrop, s.ForcedDrop, s.ProbMark, s.ForcedMark, s.ProbMarkHead, s.ForcedMarkHead} {
		b = appendU32(b, v)
	}
	return b
}

// Append lays out q, struct tc_sfq_qopt_v1 (pkt_sched.h:230), the stats
// included: sfq_parse_opt sends the whole struct (tc/q_sfq.c).
func (q TcSfqQoptV1) Append(b []byte) []byte {
	b = q.V0.Append(b)
	b = appendU32(b, q.Depth)
	b = appendU32(b, q.Headdrop)
	b = appendU32(b, q.Limit)
	b = appendU32(b, q.QthMin)
	b = appendU32(b, q.QthMax)
	b = append(b, q.Wlog, q.Plog, q.ScellLog, q.Flags)
	b = appendU32(b, q.MaxP)
	return q.Stats.Append(b)
}

// Append lays out q, struct tc_netem_qopt (pkt_sched.h:503).
func (q TcNetemQopt) Append(b []byte) []byte {
	for _, v := range [...]uint32{q.Latency, q.Limit, q.Loss, q.Gap, q.Duplicate, q.Jitter} {
		b = appendU32(b, v)
	}
	return b
}

// Append lays out c, struct tc_netem_corr (pkt_sched.h:512).
func (c TcNetemCorr) Append(b []byte) []byte {
	b = appendU32(b, c.DelayCorr)
	b = appendU32(b, c.LossCorr)
	return appendU32(b, c.DupCorr)
}

// Append lays out p, struct tc_netem_reorder or tc_netem_corrupt
// (pkt_sched.h:518).
func (p TcNetemProbCorr) Append(b []byte) []byte {
	b = appendU32(b, p.Probability)
	return appendU32(b, p.Correlation)
}

// Append lays out r, struct tc_netem_rate (pkt_sched.h:528).
func (r TcNetemRate) Append(b []byte) []byte {
	b = appendU32(b, r.Rate)
	b = appendS32(b, r.PacketOverhead)
	b = appendU32(b, r.CellSize)
	return appendS32(b, r.CellOverhead)
}

// Append lays out s, struct tc_netem_slot (pkt_sched.h:535).
func (s TcNetemSlot) Append(b []byte) []byte {
	b = appendS64(b, s.MinDelay)
	b = appendS64(b, s.MaxDelay)
	b = appendS32(b, s.MaxPackets)
	b = appendS32(b, s.MaxBytes)
	b = appendS64(b, s.DistDelay)
	return appendS64(b, s.DistJitter)
}

// Append lays out g, struct tc_netem_gimodel (pkt_sched.h:553).
func (g TcNetemGimodel) Append(b []byte) []byte {
	for _, v := range [...]uint32{g.P13, g.P31, g.P32, g.P14, g.P23} {
		b = appendU32(b, v)
	}
	return b
}

// Append lays out g, struct tc_netem_gemodel (pkt_sched.h:562).
func (g TcNetemGemodel) Append(b []byte) []byte {
	for _, v := range [...]uint32{g.P, g.R, g.H, g.K1} {
		b = appendU32(b, v)
	}
	return b
}
