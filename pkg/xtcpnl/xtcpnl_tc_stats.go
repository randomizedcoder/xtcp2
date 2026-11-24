package xtcpnl

import (
	"encoding/binary"

	"golang.org/x/sys/unix"
)

// This file decodes a tc object's statistics the way tc reads them
// (tc/tc_util.c print_tcstats2_attr and print_tcstats_attr, iproute2
// v7.2.0): TCA_STATS2's nested TCA_STATS_* and the older struct tc_stats in
// TCA_STATS. tc memcpy's each structure with MIN(RTA_PAYLOAD, sizeof) into a
// zeroed copy, so a short payload reads as its prefix with zero after it and
// a long one as its first sizeof bytes. The decoders here do the same, and
// keep each payload's length so a caller can tell a short one apart.
//
// The structures are from include/uapi/linux/gen_stats.h and
// include/uapi/linux/pkt_sched.h (linux v7.4-rc4).

const (
	// GnetStatsBasicSizeCst is sizeof(struct gnet_stats_basic): u64 bytes,
	// u32 packets, and 4 bytes of tail padding.
	GnetStatsBasicSizeCst = 16
	// GnetStatsRateEstSizeCst is sizeof(struct gnet_stats_rate_est).
	GnetStatsRateEstSizeCst = 8
	// GnetStatsRateEst64SizeCst is sizeof(struct gnet_stats_rate_est64).
	GnetStatsRateEst64SizeCst = 16
	// GnetStatsQueueSizeCst is sizeof(struct gnet_stats_queue).
	GnetStatsQueueSizeCst = 20
	// TcStatsSizeCst is sizeof(struct tc_stats): u64 bytes, seven u32s and 4
	// bytes of tail padding.
	TcStatsSizeCst = 40

	// TCA_STATS_* from include/uapi/linux/gen_stats.h.
	TcaStatsBasic     uint16 = 1 // TCA_STATS_BASIC: struct gnet_stats_basic
	TcaStatsRateEst   uint16 = 2 // TCA_STATS_RATE_EST: struct gnet_stats_rate_est
	TcaStatsQueue     uint16 = 3 // TCA_STATS_QUEUE: struct gnet_stats_queue
	TcaStatsApp       uint16 = 4 // TCA_STATS_APP: the kind's xstats
	TcaStatsRateEst64 uint16 = 5 // TCA_STATS_RATE_EST64: struct gnet_stats_rate_est64
	TcaStatsPad       uint16 = 6 // TCA_STATS_PAD
	TcaStatsBasicHw   uint16 = 7 // TCA_STATS_BASIC_HW: struct gnet_stats_basic
	TcaStatsPkt64     uint16 = 8 // TCA_STATS_PKT64: u64
	// TcaStatsMaxKnown is TCA_STATS_MAX at v7.4-rc4.
	TcaStatsMaxKnown = TcaStatsPkt64
)

// GnetStatsBasic is struct gnet_stats_basic.
type GnetStatsBasic struct {
	Bytes   uint64
	Packets uint32
}

// GnetStatsRateEst is struct gnet_stats_rate_est.
type GnetStatsRateEst struct {
	Bps uint32
	Pps uint32
}

// GnetStatsRateEst64 is struct gnet_stats_rate_est64.
type GnetStatsRateEst64 struct {
	Bps uint64
	Pps uint64
}

// GnetStatsQueue is struct gnet_stats_queue.
type GnetStatsQueue struct {
	Qlen       uint32
	Backlog    uint32
	Drops      uint32
	Requeues   uint32
	Overlimits uint32
}

// TcStats is struct tc_stats, the TCA_STATS compat block.
type TcStats struct {
	Bytes      uint64
	Packets    uint32
	Drops      uint32
	Overlimits uint32
	Bps        uint32
	Pps        uint32
	Qlen       uint32
	Backlog    uint32
}

// TcMinCopy is tc's `memcpy(&st, RTA_DATA(a), MIN(RTA_PAYLOAD(a),
// sizeof(st)))` into a zeroed st: the first size bytes of b, zero-filled
// when b is shorter. The result is always size bytes and never aliases b.
func TcMinCopy(b []byte, size int) []byte {
	if size <= 0 {
		return []byte{}
	}
	out := make([]byte, 0, size)
	if len(b) > size {
		b = b[:size]
	}
	out = append(out, b...)
	for len(out) < size {
		out = append(out, 0)
	}
	return out
}

// DecodeGnetStatsBasic reads b as tc does: its MIN-copy prefix.
func DecodeGnetStatsBasic(b []byte) GnetStatsBasic {
	p := TcMinCopy(b, GnetStatsBasicSizeCst)
	if len(p) < GnetStatsBasicSizeCst {
		return GnetStatsBasic{}
	}
	return GnetStatsBasic{
		Bytes:   binary.LittleEndian.Uint64(p[0:8]),
		Packets: binary.LittleEndian.Uint32(p[8:12]),
	}
}

// DecodeGnetStatsRateEst reads b as tc does: its MIN-copy prefix.
func DecodeGnetStatsRateEst(b []byte) GnetStatsRateEst {
	p := TcMinCopy(b, GnetStatsRateEstSizeCst)
	if len(p) < GnetStatsRateEstSizeCst {
		return GnetStatsRateEst{}
	}
	return GnetStatsRateEst{
		Bps: binary.LittleEndian.Uint32(p[0:4]),
		Pps: binary.LittleEndian.Uint32(p[4:8]),
	}
}

// DecodeGnetStatsRateEst64 reads b as tc does: its MIN-copy prefix.
func DecodeGnetStatsRateEst64(b []byte) GnetStatsRateEst64 {
	p := TcMinCopy(b, GnetStatsRateEst64SizeCst)
	if len(p) < GnetStatsRateEst64SizeCst {
		return GnetStatsRateEst64{}
	}
	return GnetStatsRateEst64{
		Bps: binary.LittleEndian.Uint64(p[0:8]),
		Pps: binary.LittleEndian.Uint64(p[8:16]),
	}
}

// DecodeGnetStatsQueue reads b as tc does: its MIN-copy prefix.
func DecodeGnetStatsQueue(b []byte) GnetStatsQueue {
	p := TcMinCopy(b, GnetStatsQueueSizeCst)
	if len(p) < GnetStatsQueueSizeCst {
		return GnetStatsQueue{}
	}
	return GnetStatsQueue{
		Qlen:       binary.LittleEndian.Uint32(p[0:4]),
		Backlog:    binary.LittleEndian.Uint32(p[4:8]),
		Drops:      binary.LittleEndian.Uint32(p[8:12]),
		Requeues:   binary.LittleEndian.Uint32(p[12:16]),
		Overlimits: binary.LittleEndian.Uint32(p[16:20]),
	}
}

// DecodeTcStats reads a TCA_STATS payload as print_tcstats_attr does: its
// MIN-copy prefix ("handle case where kernel returns more/less than we know
// about").
func DecodeTcStats(b []byte) TcStats {
	p := TcMinCopy(b, TcStatsSizeCst)
	if len(p) < TcStatsSizeCst {
		return TcStats{}
	}
	return TcStats{
		Bytes:      binary.LittleEndian.Uint64(p[0:8]),
		Packets:    binary.LittleEndian.Uint32(p[8:12]),
		Drops:      binary.LittleEndian.Uint32(p[12:16]),
		Overlimits: binary.LittleEndian.Uint32(p[16:20]),
		Bps:        binary.LittleEndian.Uint32(p[20:24]),
		Pps:        binary.LittleEndian.Uint32(p[24:28]),
		Qlen:       binary.LittleEndian.Uint32(p[28:32]),
		Backlog:    binary.LittleEndian.Uint32(p[32:36]),
	}
}

// TcStats2 is a TCA_STATS2 nest as print_tcstats2_attr reads it. Each
// structure is decoded from its MIN-copy; its *Len is the payload length,
// -1 when the attribute is absent.
//
// BasicHw differs from tc in one place: print_tcstats_basic_hw copies into
// an uninitialized gnet_stats_basic, so a short TCA_STATS_BASIC_HW leaves
// stack bytes in the rest (lead Q16, docs/netlink/upstream/
// iproute2-tc-qdisc-class.md). Here the rest is zero.
type TcStats2 struct {
	Basic        GnetStatsBasic
	BasicLen     int
	BasicHw      GnetStatsBasic
	BasicHwLen   int
	RateEst      GnetStatsRateEst
	RateEstLen   int
	RateEst64    GnetStatsRateEst64
	RateEst64Len int
	Queue        GnetStatsQueue
	QueueLen     int
	// App is TCA_STATS_APP's payload, the kind's xstats, a view into the
	// parsed buffer; HasApp is tb[TCA_STATS_APP] != NULL.
	App    []byte
	HasApp bool
	// Packets64 and Packets64Hw are parse_packets64: a TCA_STATS_PKT64 that
	// directly follows TCA_STATS_BASIC, or TCA_STATS_BASIC_HW, in wire
	// order, the last such pair winning. 0 when there is none.
	Packets64   uint64
	Packets64Hw uint64
}

// ParseTcStats2 decodes a TCA_STATS2 payload: parse_rtattr_nested (first
// occurrence wins, NLA_F_NESTED masked) and parse_packets64. A malformed
// attribute stream is an error, where tc stops at the first bad header.
func ParseTcStats2(nest []byte) (TcStats2, error) {
	s := TcStats2{BasicLen: -1, BasicHwLen: -1, RateEstLen: -1, RateEst64Len: -1, QueueLen: -1}
	tb, err := ParseAttrTableFlags(nest, TcaStatsMaxKnown, uint16(unix.NLA_F_NESTED))
	if err != nil {
		return s, err
	}
	if b := tb.Bytes(TcaStatsBasic); tb.Has(TcaStatsBasic) {
		s.Basic, s.BasicLen = DecodeGnetStatsBasic(b), len(b)
	}
	if b := tb.Bytes(TcaStatsBasicHw); tb.Has(TcaStatsBasicHw) {
		s.BasicHw, s.BasicHwLen = DecodeGnetStatsBasic(b), len(b)
	}
	if b := tb.Bytes(TcaStatsRateEst); tb.Has(TcaStatsRateEst) {
		s.RateEst, s.RateEstLen = DecodeGnetStatsRateEst(b), len(b)
	}
	if b := tb.Bytes(TcaStatsRateEst64); tb.Has(TcaStatsRateEst64) {
		s.RateEst64, s.RateEst64Len = DecodeGnetStatsRateEst64(b), len(b)
	}
	if b := tb.Bytes(TcaStatsQueue); tb.Has(TcaStatsQueue) {
		s.Queue, s.QueueLen = DecodeGnetStatsQueue(b), len(b)
	}
	s.App, s.HasApp = tb.Bytes(TcaStatsApp), tb.Has(TcaStatsApp)
	s.Packets64, s.Packets64Hw = parsePackets64(nest)
	return s, nil
}

// parsePackets64 is tc_util.c parse_packets64: rtattr_for_each_nested over
// the raw types, flags included. rta_getattr_u64 reads 8 bytes whatever the
// payload; a short one reads as its MIN-copy here.
func parsePackets64(nest []byte) (pkts, pktsHw uint64) {
	prev := TcaStatsMaxKnown + 1 // __TCA_STATS_MAX
	err := WalkRawAttrs(nest, func(raw uint16, val []byte) {
		if raw == TcaStatsPkt64 {
			v := binary.LittleEndian.Uint64(TcMinCopy(val, 8))
			switch prev {
			case TcaStatsBasic:
				pkts = v
			case TcaStatsBasicHw:
				pktsHw = v
			}
		}
		prev = raw
	})
	if err != nil {
		// Unreachable from ParseTcStats2, which refused a malformed
		// stream before calling this.
		return 0, 0
	}
	return pkts, pktsHw
}
