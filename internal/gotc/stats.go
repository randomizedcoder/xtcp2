package gotc

import (
	"strconv"

	"github.com/randomizedcoder/xtcp2/pkg/xtcpnl"
)

// This is tc/tc_util.c's statistics printers: print_tcstats_attr, and
// print_tcstats2_attr with print_tcstats_basic_hw under it.

func u32s(v uint32) string { return strconv.FormatUint(uint64(v), 10) }
func u64s(v uint64) string { return strconv.FormatUint(v, 10) }

// tcStats is print_tcstats_attr over a message's TCA_STATS2, else its
// TCA_STATS. It returns the xstats a printer would get: TCA_STATS_APP, or
// failing that TCA_XSTATS; has is false for neither.
func (r *runner) tcStats(ti *xtcpnl.TcInfo, prefix string) (xstats []byte, has bool, err error) {
	if ti.HasStats2 {
		s, err := xtcpnl.ParseTcStats2(ti.Stats2)
		if err != nil {
			return nil, false, err
		}
		r.tcStats2(&s, prefix)
		if s.HasApp {
			return s.App, true, nil
		}
	} else if ti.HasStats {
		r.tcStatsCompat(xtcpnl.DecodeTcStats(ti.Stats), prefix)
	}
	// compat_xstats
	if ti.HasXstats {
		return ti.Xstats, true, nil
	}
	return nil, false, nil
}

// tcStatsCompat is print_tcstats_attr's TCA_STATS branch: its keys are
// "dropped" and "packets"... " pkts", where STATS2 has "drops" and " pkt"
// (Q17).
func (r *runner) tcStatsCompat(st xtcpnl.TcStats, prefix string) {
	o := r.o
	o.fp(prefix)
	o.u64(printAny, "bytes", "Sent "+u64s(st.Bytes)+" bytes", st.Bytes)
	o.uint(printAny, "packets", " "+u32s(st.Packets)+" pkts", st.Packets)
	o.uint(printAny, "dropped", " (dropped "+u32s(st.Drops)+",", st.Drops)
	o.uint(printAny, "overlimits", " overlimits "+u32s(st.Overlimits)+") ", st.Overlimits)
	if st.Bps == 0 && st.Pps == 0 && st.Qlen == 0 && st.Backlog == 0 {
		return
	}
	o.nl()
	o.fp(prefix)
	if st.Bps != 0 || st.Pps != 0 {
		o.fp("rate ")
		if st.Bps != 0 {
			o.rate(r.iec > 0, printAny, "rate", "", " ", uint64(st.Bps))
		}
		if st.Pps != 0 {
			o.uint(printAny, "pps", u32s(st.Pps)+"pps ", st.Pps)
		}
	}
	if st.Qlen != 0 || st.Backlog != 0 {
		o.fp("backlog ")
		if st.Backlog != 0 {
			o.size(printAny, "backlog", "", " ", uint64(st.Backlog))
		}
		if st.Qlen != 0 {
			o.uint(printAny, "qlen", u32s(st.Qlen)+"p ", st.Qlen)
		}
	}
}

// tcStats2 is print_tcstats2_attr. Its "requeues" goes out twice in text,
// once in JSON (Q18); the newline before backlog tests TCA_STATS_RATE_EST,
// not RATE_EST64.
func (r *runner) tcStats2(s *xtcpnl.TcStats2, prefix string) {
	o := r.o
	if s.BasicLen >= 0 {
		o.fp(prefix)
		o.u64(printAny, "bytes", "Sent "+u64s(s.Basic.Bytes)+" bytes", s.Basic.Bytes)
		if s.Packets64 != 0 {
			o.u64(printAny, "packets", " "+u64s(s.Packets64)+" pkt", s.Packets64)
		} else {
			o.uint(printAny, "packets", " "+u32s(s.Basic.Packets)+" pkt", s.Basic.Packets)
		}
	}
	if s.QueueLen >= 0 {
		q := s.Queue
		o.uint(printAny, "drops", " (dropped "+u32s(q.Drops), q.Drops)
		o.uint(printAny, "overlimits", ", overlimits "+u32s(q.Overlimits), q.Overlimits)
		o.uint(printAny, "requeues", " requeues "+u32s(q.Requeues)+") ", q.Requeues)
	}
	if s.BasicHwLen >= 0 {
		r.tcStatsBasicHw(s, prefix)
	}
	switch {
	case s.RateEst64Len >= 0:
		o.fp("\n" + prefix)
		o.u64(printJSON, "rate", "", s.RateEst64.Bps)
		o.rate(r.iec > 0, printFP, "", "rate ", "", s.RateEst64.Bps)
		o.u64(printAny, "pps", " "+u64s(s.RateEst64.Pps)+"pps", s.RateEst64.Pps)
	case s.RateEstLen >= 0:
		o.fp("\n" + prefix)
		o.uint(printJSON, "rate", "", s.RateEst.Bps)
		o.rate(r.iec > 0, printFP, "", "rate ", "", uint64(s.RateEst.Bps))
		o.uint(printAny, "pps", " "+u32s(s.RateEst.Pps)+"pps", s.RateEst.Pps)
	}
	if s.QueueLen >= 0 {
		q := s.Queue
		if s.RateEstLen < 0 {
			o.nl()
		}
		o.fp(prefix)
		o.size(printAny, "backlog", "backlog ", "", uint64(q.Backlog))
		o.uint(printAny, "qlen", " "+u32s(q.Qlen)+"p", q.Qlen)
		o.uint(printFP, "", " requeues "+u32s(q.Requeues), q.Requeues)
	}
}

// tcStatsBasicHw is print_tcstats_basic_hw. tc copies a short
// BASIC_HW or BASIC into an uninitialized struct (Q16); here the rest is
// zero, xtcpnl's MIN-copy.
func (r *runner) tcStatsBasicHw(s *xtcpnl.TcStats2, prefix string) {
	o := r.o
	hw := s.BasicHw
	pktsHw := s.Packets64Hw
	if pktsHw == 0 {
		pktsHw = uint64(hw.Packets)
	}
	if hw.Bytes == 0 && pktsHw == 0 {
		return
	}
	if s.BasicLen >= 0 {
		pkts := s.Packets64
		if pkts == 0 {
			pkts = uint64(s.Basic.Packets)
		}
		if s.Basic.Bytes >= hw.Bytes && pkts >= pktsHw {
			o.nl()
			o.fp(prefix)
			sw := s.Basic.Bytes - hw.Bytes
			o.u64(printAny, "sw_bytes", "Sent software "+u64s(sw)+" bytes", sw)
			o.u64(printAny, "sw_packets", " "+u64s(pkts-pktsHw)+" pkt", pkts-pktsHw)
		}
	}
	o.nl()
	o.fp(prefix)
	o.u64(printAny, "hw_bytes", "Sent hardware "+u64s(hw.Bytes)+" bytes", hw.Bytes)
	o.u64(printAny, "hw_packets", " "+u64s(pktsHw)+" pkt", pktsHw)
}
