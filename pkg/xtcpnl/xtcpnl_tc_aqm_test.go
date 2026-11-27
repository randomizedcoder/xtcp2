package xtcpnl

import (
	"errors"
	"fmt"
	"math"
	"path/filepath"
	"testing"

	"golang.org/x/sys/unix"
)

// tcaqmMsgs is every kernel reply of kind in a tcaqm row's capture:
// qdiscs, or classes when class.
func tcaqmMsgs(t testing.TB, row, kind string, class bool) []TcInfo {
	t.Helper()
	want := uint16(unix.RTM_NEWQDISC)
	if class {
		want = unix.RTM_NEWTCLASS
	}
	var out []TcInfo
	for _, m := range pcapNlMsgs(t, filepath.Join("testdata", "7_3_1", "ws", "tcaqm", row, "ip.pcap")) {
		if m.proto != unix.NETLINK_ROUTE || m.h.Flags&unix.NLM_F_REQUEST != 0 || m.h.Type != want {
			continue
		}
		ti, err := ParseTcMsg(m.body)
		if err != nil {
			t.Fatal(err)
		}
		if ti.Kind == kind {
			out = append(out, ti)
		}
	}
	if len(out) == 0 {
		t.Fatalf("%s: no %s message", row, kind)
	}
	return out
}

func aqmErr(err error) string {
	for _, e := range []error{ErrTcGredSmall, ErrTcShort, ErrTcMissing, ErrTcKindUnknown, ErrRTAttrSmall} {
		if errors.Is(err, e) {
			return e.Error()
		}
	}
	return err.Error()
}

// aqmVals is the values tc read from v, attribute by attribute.
func aqmVals(v TcVals, maxType uint16) string {
	s := ""
	for t := uint16(1); t <= maxType; t++ {
		if x, ok := v.Get(t); ok {
			s += fmt.Sprintf(" %d=%d", t, x)
		}
	}
	return s
}

func aqmSum(q TcQopt, err error) string {
	if err != nil {
		return "err " + aqmErr(err)
	}
	a := q.Aqm
	switch q.Kind {
	case TcKindRed, TcKindChoke:
		r := a.Red
		o := r.Qopt
		return fmt.Sprintf("limit %d min %d max %d ewma %d scell %d flags %#x p %.6g early %d %t mark %d %t",
			o.Limit, o.QthMin, o.QthMax, o.Wlog, o.ScellLog, o.Flags, float64(r.MaxP)/math.Pow(2, 32),
			r.EarlyDrop, r.HasEarlyDrop, r.Mark, r.HasMark)
	case TcKindGred:
		g := *a.Gred
		s := fmt.Sprintf("vqs %d def %d grio %d limit %d %t maxp %t vq %t over %t warn %d", g.Sopt.DPs, g.Sopt.DefDP,
			g.Sopt.Grio, g.Limit, g.HasLimit, g.HasMaxP, g.HasVQ, g.VqDpOver, len(g.Warn))
		for i, o := range g.Qopts {
			if o.DP < GredMaxDPsCst {
				s += fmt.Sprintf("; dp %d prio %d limit %d min %d max %d p %.6g flags %t %d", o.DP, o.Prio, o.Limit,
					o.QthMin, o.QthMax, float64(g.MaxP[i])/math.Pow(2, 32), g.Info[i].FlagsPresent, g.Info[i].Flags)
			}
		}
		return s
	case TcKindSfb:
		f := a.Sfb
		return fmt.Sprintf("rehash %d db %d (us %d) limit %d max %d target %d inc %d dec %d rate %d burst %d",
			f.RehashInterval, f.WarmupTime, f.RehashInterval*1000, f.Limit, f.Max, f.BinSize, f.Increment,
			f.Decrement, f.PenaltyRate, f.PenaltyBurst)
	case TcKindSkbprio:
		return fmt.Sprintf("limit %d %t", a.Skbprio, a.HasSkbprio)
	case TcKindPlug:
		return fmt.Sprint(a.Vals == TcVals{} && a.Red == TcRed{} && !a.HasSkbprio)
	case TcKindCodel:
		return aqmVals(a.Vals, TcaCodelMaxKnown)
	case TcKindPie:
		return aqmVals(a.Vals, TcaPieMaxKnown)
	case TcKindFqPie:
		return aqmVals(a.Vals, TcaFqPieMaxKnown)
	case TcKindHhf:
		return aqmVals(a.Vals, TcaHhfMaxKnown)
	case TcKindDualpi2:
		return aqmVals(a.Vals, TcaDualpi2MaxKnown)
	case TcKindCake, TcKindCakeMq:
		return aqmVals(a.Vals, TcaCakeMaxKnown)
	}
	return "?"
}

// gredQopt is a struct tc_gred_qopt for slot dp: limit, min, max and prio.
func gredQopt(dp, limit, lo, hi uint32, prio uint8) []byte {
	b := cat(le32(limit), le32(lo), le32(hi), le32(dp), make([]byte, 24), []byte{3, 0, 13, prio}, make([]byte, 8))
	return b
}

// gredOpts is gred's options: vqs, default 0, the given slots configured
// and the rest the kernel's empty-slot marker, plus extra attributes.
func gredOpts(dps uint32, configured int, extra ...[]byte) []byte {
	var parms []byte
	for i := range uint32(GredMaxDPsCst) {
		if int(i) < configured {
			parms = cat(parms, gredQopt(i, 61440, 15360, 46080, uint8(i+1)))
		} else {
			parms = cat(parms, gredQopt(GredMaxDPsCst+i, 0, 0, 0, 0))
		}
	}
	return cat(append([][]byte{rtattr(TcaGredDps, cat(le32(dps), le32(0), []byte{1, 0}, le16(0))),
		rtattr(TcaGredParms, parms)}, extra...)...)
}

// gredVq is a TCA_GRED_VQ_LIST of one entry with the given attributes.
func gredVq(kids ...[]byte) []byte {
	return rtattr(TcaGredVqList, rtattr(TcaGredVqEntry, cat(kids...)))
}

// cakeTin is a tin stats nest of the given attributes.
func cakeTin(i uint16, kids ...[]byte) []byte { return nest(i, kids...) }

func TestParseTcAqm(t *testing.T) {
	qopt := func(row, kind string, i int) string {
		return aqmSum(ParseTcQopt(kind, tcaqmMsgs(t, row, kind, false)[i].Options))
	}
	crafted := func(kind string, opt []byte) string { return aqmSum(ParseTcQopt(kind, opt)) }
	red := func(extra ...[]byte) []byte {
		return cat(append([][]byte{rtattr(TcaRedParms, cat(le32(400000), le32(30000), le32(90000), []byte{5, 0, 15, 0}))},
			extra...)...)
	}
	gredWith := func(vq ...[]byte) string {
		q, err := ParseTcQopt(TcKindGred, gredOpts(4, 2, gredVq(vq...)))
		if err != nil {
			return "err " + aqmErr(err)
		}
		g := *q.Aqm.Gred
		return fmt.Sprintf("over %t info1 %+v info15 %d warn %q", g.VqDpOver, g.Info[1], g.Info[15].Packets, g.Warn)
	}
	tests := []struct {
		description string
		input       func() string
		expected    string
	}{
		// Positive: each kind's options from the corpus, matching tc's text.
		{"positive: red with flags and a qevent (ec-007, Q66, Q87)", func() string { return qopt("ec-007", TcKindRed, 0) },
			"limit 400000 min 30000 max 90000 ewma 5 scell 15 flags 0x7 p 0.02 early 10 true mark 0 false"},
		{"positive: choke ecn; ewma, p and Scell_log as ec-011's -d text", func() string { return qopt("ec-009", TcKindChoke, 0) },
			"limit 1000 min 100 max 300 ewma 7 scell 17 flags 0x1 p 0.02 early 0 false mark 0 false"},
		{"positive: gred with 2 DPs, the empty slots skipped (ec-015, K33)", func() string { return qopt("ec-015", TcKindGred, 0) },
			"vqs 4 def 0 grio 1 limit 1514000 true maxp true vq true over false warn 0; " +
				"dp 0 prio 1 limit 61440 min 15360 max 46080 p 0.1 flags true 0; " +
				"dp 1 prio 2 limit 61440 min 15360 max 46080 p 0.2 flags true 1"},
		{"positive: sfb (ec-019)", func() string { return qopt("ec-019", TcKindSfb, 0) },
			"rehash 600000 db 60000 (us 600000000) limit 1000 max 25 target 20 inc 33 dec 3 rate 10 burst 20"},
		// codel's times come back a microsecond short of what was set: tc
		// prints 3999 as 4ms.
		{"positive: codel with ce_threshold (ec-024)", func() string { return qopt("ec-024", TcKindCodel, 0) },
			" 1=3999 2=500 3=49999 4=1 5=1999"},
		{"positive: pie with every flag (ec-027)", func() string { return qopt("ec-027", TcKindPie, 0) },
			" 1=10000 2=500 3=20000 4=2 5=20 6=1 7=1 8=1"},
		{"positive: fq_pie (ec-031)", func() string { return qopt("ec-031", TcKindFqPie, 0) },
			" 1=2048 2=256 3=15000 4=15000 5=2 6=20 7=1514 8=16777216 9=20 10=1 11=1 12=1"},
		{"positive: hhf (ec-034)", func() string { return qopt("ec-034", TcKindHhf, 0) },
			" 1=500 2=1514 3=1024 4=50000 5=65536 6=500000 7=4"},
		{"positive: cake with every option (ec-040)", func() string { return qopt("ec-040", TcKindCake, 0) },
			" 2=12500000 3=1 4=1 5=5 6=44 7=20000 9=0 10=4194304 11=1 13=1 14=64 15=1 16=1 17=0 18=255"},
		{"positive: cake default prints raw (ec-035, K38)", func() string { return qopt("ec-035", TcKindCake, 0) },
			" 2=0 3=0 4=0 5=7 6=0 7=100000 9=0 10=0 11=0 12=0 13=0 14=0 15=0 16=0 17=1 18=0"},
		{"positive: cake_mq (ec-049)", func() string { return qopt("ec-049", TcKindCakeMq, 0) },
			" 2=12500000 3=0 4=0 5=7 6=0 7=100000 9=0 10=0 11=0 12=0 13=0 14=0 15=0 16=0 17=1 18=0"},
		// A bare `step_thresh 10` is microseconds: the kernel sends
		// STEP_THRESH_US.
		{"positive: dualpi2 step_thresh 10 with any_ect (ec-055, Q84)", func() string { return qopt("ec-055", TcKindDualpi2, 0) },
			" 1=5000 2=4000000 3=15000 4=16000 5=40 6=818 8=10 9=0 10=2 11=1 12=1 13=20 14=3 15=0"},
		{"positive: dualpi2 in time (ec-057, K40)", func() string { return qopt("ec-057", TcKindDualpi2, 0) },
			" 1=10000 2=30280000 3=10000 4=10000 5=101 6=1535 8=1000 9=0 10=2 11=1 12=0 13=10 14=1 15=1"},
		{"positive: skbprio, a bare struct (ec-060, K42)", func() string { return qopt("ec-060", TcKindSkbprio, 0) },
			"limit 100 true"},
		// Negative.
		{"negative: red PARMS short", func() string {
			return crafted(TcKindRed, rtattr(TcaRedParms, make([]byte, TcRedQoptSizeCst-1)))
		}, "err " + ErrTcShort.Error()},
		{"negative: red without PARMS", func() string { return crafted(TcKindRed, rtattr(TcaRedMaxP, le32(1))) },
			"err " + ErrTcMissing.Error()},
		{"negative: red qevent block of 2 bytes", func() string {
			return crafted(TcKindRed, red(rtattr(TcaRedEarlyDropBlock, le16(10))))
		}, "err " + ErrTcShort.Error()},
		{"negative: gred without DPS (Q67)", func() string {
			return crafted(TcKindGred, rtattr(TcaGredParms, make([]byte, TcGredQoptSizeCst*GredMaxDPsCst)))
		}, "err " + ErrTcMissing.Error()},
		{"negative: gred PARMS of 15 slots (Q72)", func() string {
			return crafted(TcKindGred, cat(rtattr(TcaGredDps, make([]byte, TcGredSoptSizeCst)),
				rtattr(TcaGredParms, make([]byte, TcGredQoptSizeCst*(GredMaxDPsCst-1)))))
		}, "err " + ErrTcGredSmall.Error()},
		{"negative: gred VQ attribute of 2 bytes (Q69)", func() string {
			return gredWith(rtattr(TcaGredVqDp, le32(1)), rtattr(TcaGredVqStatPackets, le16(7)))
		}, "err " + ErrTcShort.Error()},
		{"negative: gred VQ entry with rta_len 2 (Q71)", func() string {
			list := rtattr(TcaGredVqList, cat(le16(2), le16(TcaGredVqEntry), make([]byte, 4)))
			return crafted(TcKindGred, gredOpts(4, 2, list))
		}, "err " + ErrTcShort.Error()},
		{"negative: sfb without PARMS", func() string { return crafted(TcKindSfb, []byte{}) }, "err " + ErrTcMissing.Error()},
		{"negative: skbprio of 3 bytes", func() string { return crafted(TcKindSkbprio, []byte{1, 2, 3}) },
			"err " + ErrTcShort.Error()},
		// Boundary.
		{"boundary: gred VQ_DP 15 kept", func() string {
			return gredWith(rtattr(TcaGredVqDp, le32(15)), rtattr(TcaGredVqStatPackets, le32(9)))
		}, `over false info1 {FlagsPresent:false Bytes:0 BytesWire:0 HasBytesWire:false Packets:0 Backlog:0 ProbDrop:0 ProbMark:0 ForcedDrop:0 ForcedMark:0 Pdrop:0 Other:0 Flags:0} info15 9 warn []`},
		{"boundary: gred VQ_DP 16 skipped, not written past (Q68)", func() string {
			return gredWith(rtattr(TcaGredVqDp, le32(16)), rtattr(TcaGredVqStatPackets, le32(9)))
		}, `over true info1 {FlagsPresent:false Bytes:0 BytesWire:0 HasBytesWire:false Packets:0 Backlog:0 ProbDrop:0 ProbMark:0 ForcedDrop:0 ForcedMark:0 Pdrop:0 Other:0 Flags:0} info15 0 warn []`},
		{"boundary: gred VQ_STAT_BYTES 2^32+5 read as tc's u32 (Q70)", func() string {
			return gredWith(rtattr(TcaGredVqDp, le32(1)), rtattr(TcaGredVqStatBytes, le64(1<<32+5)), rtattr(TcaGredVqFlags, le32(1)))
		}, `over false info1 {FlagsPresent:true Bytes:5 BytesWire:4294967301 HasBytesWire:true Packets:0 Backlog:0 ProbDrop:0 ProbMark:0 ForcedDrop:0 ForcedMark:0 Pdrop:0 Other:0 Flags:1} info15 0 warn []`},
		{"boundary: gred LIMIT of 8 bytes and MAX_P of 15 are not read", func() string {
			return crafted(TcKindGred, gredOpts(2, 0, rtattr(TcaGredLimit, le64(7)), rtattr(TcaGredMaxP, make([]byte, 60))))
		}, "vqs 2 def 0 grio 1 limit 0 false maxp false vq false over false warn 0"},
		{"boundary: dualpi2 u8 of 1 byte read, of 0 bytes skipped", func() string {
			return crafted(TcKindDualpi2, cat(rtattr(TcaDualpi2Coupling, []byte{2}), rtattr(TcaDualpi2EcnMask, nil)))
		}, " 10=2"},
		{"boundary: codel LIMIT of 3 bytes skipped, as tc does", func() string {
			return crafted(TcKindCodel, rtattr(TcaCodelLimit, []byte{1, 2, 3}))
		}, ""},
		// Corner.
		{"corner: plug with no TCA_OPTIONS (ec-058, K41)", func() string {
			ti := tcaqmMsgs(t, "ec-058", TcKindPlug, false)[0]
			return fmt.Sprint(ti.HasOptions, " ", crafted(TcKindPlug, ti.Options))
		}, "false true"},
		{"corner: red FLAGS 0x1ff kept as its low byte", func() string {
			return crafted(TcKindRed, red(rtattr(TcaRedFlags, cat(le32(0x1ff), le32(0x1ff)))))
		}, "limit 400000 min 30000 max 90000 ewma 5 scell 15 flags 0xff p 0 early 0 false mark 0 false"},
		{"corner: sfb rehash 5000000 wraps tc's u32 (ec-021, Q73)", func() string { return qopt("ec-021", TcKindSfb, 0) },
			"rehash 5000000 db 5000000 (us 705032704) limit 100 max 25 target 20 inc 33 dec 3 rate 10 burst 20"},
		{"corner: gred VQ entries without ENTRY or DP warn and go on", func() string {
			list := rtattr(TcaGredVqList, cat(rtattr(2, le32(0)), rtattr(TcaGredVqEntry, rtattr(TcaGredVqStatPackets, le32(1)))))
			q, err := ParseTcQopt(TcKindGred, gredOpts(4, 2, list))
			return fmt.Sprintf("%q %v", q.Aqm.Gred.Warn, err)
		}, `["ERROR: Virtual Queue without DP attribute" "ERROR: Virtual Queue without DP attribute"] <nil>`},
	}
	for _, tt := range tests {
		t.Run(tt.description, func(t *testing.T) {
			if got := tt.input(); got != tt.expected {
				t.Errorf("got %q, want %q", got, tt.expected)
			}
		})
	}
}

func TestParseTcAqmXstats(t *testing.T) {
	xs := func(row, kind string) (TcXstats, error) {
		return ParseTcXstats(kind, tcschedXstats(t, tcaqmMsgs(t, row, kind, false)[0]))
	}
	cake := func(b []byte) string {
		x, err := ParseTcXstats(TcKindCake, b)
		if err != nil {
			return "err " + aqmErr(err)
		}
		c := *x.Aqm.Cake
		ti, tm := c.ShortTin(true)
		ji, jm := c.ShortTin(false)
		return fmt.Sprintf("tins %d gap %t text %d %#x json %d %#x", c.NTins, c.TinGap, ti, tm, ji, jm)
	}
	tin := cakeTin
	tgt := rtattr(TcaCakeTinStatsTargetUs, le32(5000))
	pk := rtattr(TcaCakeTinStatsSentPackets, le32(1))
	tests := []struct {
		description string
		input       func() string
		expected    string
	}{
		{"positive: cake diffserv4 -s, idle (ec-041, K38)", func() string {
			x, err := xs("ec-041", TcKindCake)
			c := *x.Aqm.Cake
			mn, _ := c.Stats.U32(TcaCakeStatsMinNetlen)
			mx, _ := c.Stats.U32(TcaCakeStatsMaxNetlen)
			th, _ := c.Tins[1].Get(TcaCakeTinStatsThresholdRate64)
			return fmt.Sprint(x.Has, c.NTins, c.TinGap, mn, mx, th, err)
		}, "true 4 false 65535 0 12500000 <nil>"},
		{"positive: codel, idle (ec-023)", func() string {
			x, err := xs("ec-023", TcKindCodel)
			return fmt.Sprintf("%t %+v %v", x.Has, x.Aqm.Codel, err)
		}, "true {Maxpacket:0 Count:0 Lastcount:0 Ldelay:0 DropNext:0 DropOverlimit:0 EcnMark:0 Dropping:0 CeMark:0} <nil>"},
		{"positive: dualpi2 credit (ec-053, K40)", func() string {
			x, err := xs("ec-053", TcKindDualpi2)
			d := x.Aqm.Dualpi2
			return fmt.Sprint(x.Has, d.Credit, d.MemoryLimit, err)
		}, "true -121120 30280000 <nil>"},
		{"positive: every -s kind decodes (red, choke, sfb, pie, fq_pie, hhf)", func() string {
			s := ""
			for _, c := range []struct{ row, kind string }{{"ec-006", TcKindRed}, {"ec-010", TcKindChoke},
				{"ec-020", TcKindSfb}, {"ec-026", TcKindPie}, {"ec-030", TcKindFqPie}, {"ec-033", TcKindHhf}} {
				x, err := xs(c.row, c.kind)
				s += fmt.Sprintf("%s %t %v; ", c.kind, x.Has, err)
			}
			return s
		}, "red true <nil>; choke true <nil>; sfb true <nil>; pie true <nil>; fq_pie true <nil>; hhf true <nil>; "},
		{"negative: red xstats short", func() string {
			_, err := ParseTcXstats(TcKindRed, make([]byte, TcRedXstatsSizeCst-1))
			return aqmErr(err)
		}, ErrTcShort.Error()},
		{"negative: cake MEMORY_USED of 2 bytes beside MEMORY_LIMIT (Q77)", func() string {
			return cake(cat(rtattr(TcaCakeStatsMemoryUsed, le16(1)), rtattr(TcaCakeStatsMemoryLimit, le32(4))))
		}, "err " + ErrTcShort.Error()},
		{"negative: cake tin 0 attribute short is read in both modes", func() string {
			return cake(nest(TcaCakeStatsTinStats, tin(1, rtattr(TcaCakeTinStatsTargetUs, le16(1)))))
		}, "tins 1 gap false text 0 0x2000 json 0 0x2000"},
		{"negative: cake tin 1 short where tin 0 has it is read in both modes", func() string {
			return cake(nest(TcaCakeStatsTinStats, tin(1, tgt), tin(2, rtattr(TcaCakeTinStatsTargetUs, le16(1)))))
		}, "tins 2 gap false text 1 0x2000 json 1 0x2000"},
		{"corner: cake tin 1 short where tin 0 lacks it is read by JSON only (PR 353 review)", func() string {
			return cake(nest(TcaCakeStatsTinStats, tin(1), tin(2, rtattr(TcaCakeTinStatsTargetUs, []byte{1}))))
		}, "tins 2 gap false text -1 0x0 json 1 0x2000"},
		{"corner: a short tin 0 attribute counts as present for the gap (Q78)", func() string {
			return cake(nest(TcaCakeStatsTinStats, tin(1, rtattr(TcaCakeTinStatsTargetUs, le16(1))), tin(2, pk)))
		}, "tins 2 gap true text 0 0x2000 json 0 0x2000"},
		{"boundary: cake tin 1 missing an attribute tin 0 has (Q78)", func() string {
			return cake(nest(TcaCakeStatsTinStats, tin(1, tgt, pk), tin(2, pk)))
		}, "tins 2 gap true text -1 0x0 json -1 0x0"},
		{"boundary: cake tins stop at the first gap in the numbering", func() string {
			return cake(nest(TcaCakeStatsTinStats, tin(1, tgt), tin(3, tgt)))
		}, "tins 1 gap false text -1 0x0 json -1 0x0"},
		{"boundary: pie xstats of exactly sizeof", func() string {
			x, err := ParseTcXstats(TcKindPie, make([]byte, TcPieXstatsSizeCst))
			return fmt.Sprint(x.Has, err)
		}, "true <nil>"},
		{"corner: codel drop_next INT_MIN, MIN-copied (Q74)", func() string {
			x, err := ParseTcXstats(TcKindCodel, cat(make([]byte, 16), le32(0x80000000)))
			return fmt.Sprint(x.Aqm.Codel.DropNext, err)
		}, "-2147483648 <nil>"},
		{"corner: cake MEMORY_USED short alone is not read", func() string {
			return cake(rtattr(TcaCakeStatsMemoryUsed, le16(1)))
		}, "tins 0 gap false text -1 0x0 json -1 0x0"},
		{"corner: cake DROP_NEXT short under DROPPING 0 is not read", func() string {
			return cake(cat(rtattr(TcaCakeStatsDropping, le32(0)), rtattr(TcaCakeStatsDropNextUs, le16(1))))
		}, "tins 0 gap false text -1 0x0 json -1 0x0"},
		{"corner: gred, plug and skbprio have no xstats", func() string {
			s := ""
			for _, k := range []string{TcKindGred, TcKindPlug, TcKindSkbprio} {
				x, err := ParseTcXstats(k, []byte{1})
				s += fmt.Sprint(x.Has, err, " ")
			}
			return s
		}, "false <nil> false <nil> false <nil> "},
	}
	for _, tt := range tests {
		t.Run(tt.description, func(t *testing.T) {
			if got := tt.input(); got != tt.expected {
				t.Errorf("got %q, want %q", got, tt.expected)
			}
		})
	}
}

// TestParseTcAqmAllocs pins the decoders' allocations: the top-level
// attribute table where a kind has one. gred's VQ entries and cake's tins
// are parsed into stack tables.
func TestParseTcAqmAllocs(t *testing.T) {
	qa := func(kind string, in []byte) float64 {
		return testing.AllocsPerRun(100, func() {
			if _, err := ParseTcQopt(kind, in); err != nil {
				t.Fatal(err)
			}
		})
	}
	xa := func(kind string, in []byte) float64 {
		return testing.AllocsPerRun(100, func() {
			if _, err := ParseTcXstats(kind, in); err != nil {
				t.Fatal(err)
			}
		})
	}
	vq4 := make([][]byte, 4)
	for i := range vq4 {
		vq4[i] = rtattr(TcaGredVqEntry, cat(rtattr(TcaGredVqDp, le32(uint32(i))), rtattr(TcaGredVqStatBytes, le64(1)),
			rtattr(TcaGredVqStatPackets, le32(1)), rtattr(TcaGredVqFlags, le32(0))))
	}
	tins := make([][]byte, CakeMaxTinsCst)
	for i := range tins {
		tins[i] = cakeTin(uint16(i+1), rtattr(TcaCakeTinStatsThresholdRate64, le64(1)),
			rtattr(TcaCakeTinStatsTargetUs, le32(1)), rtattr(TcaCakeTinStatsSentPackets, le32(1)))
	}
	tests := []struct {
		description string
		input       func() float64
		expected    float64
	}{
		{"positive: skbprio, a bare struct", func() float64 { return qa(TcKindSkbprio, le32(100)) }, 0},
		{"positive: red, its table", func() float64 {
			return qa(TcKindRed, rtattr(TcaRedParms, make([]byte, TcRedQoptSizeCst)))
		}, 1},
		{"positive: cake, its table", func() float64 { return qa(TcKindCake, rtattr(TcaCakeRtt, le32(100000))) }, 1},
		{"positive: red xstats, a fixed struct", func() float64 { return xa(TcKindRed, make([]byte, TcRedXstatsSizeCst)) }, 0},
		{"boundary: gred with 4 VQs, its table and its TcGred", func() float64 {
			return qa(TcKindGred, gredOpts(4, 4, rtattr(TcaGredVqList, cat(vq4...))))
		}, 2},
		{"boundary: cake xstats with 8 tins, its TcCakeXstats only", func() float64 {
			return xa(TcKindCake, nest(TcaCakeStatsTinStats, tins...))
		}, 1},
		{"corner: plug, no options", func() float64 { return qa(TcKindPlug, nil) }, 0},
	}
	for _, tt := range tests {
		t.Run(tt.description, func(t *testing.T) {
			if got := tt.input(); got != tt.expected {
				t.Errorf("got %v allocations, want %v", got, tt.expected)
			}
		})
	}
}

func BenchmarkParseTcAqm(b *testing.B) {
	vq4 := make([][]byte, 4)
	for i := range vq4 {
		vq4[i] = rtattr(TcaGredVqEntry, cat(rtattr(TcaGredVqDp, le32(uint32(i))), rtattr(TcaGredVqStatBytes, le64(1))))
	}
	for _, c := range []struct {
		kind string
		opt  []byte
	}{
		{TcKindRed, rtattr(TcaRedParms, make([]byte, TcRedQoptSizeCst))},
		{TcKindGred, gredOpts(4, 4, rtattr(TcaGredVqList, cat(vq4...)))},
		{TcKindSfb, rtattr(TcaSfbParms, make([]byte, TcSfbQoptSizeCst))},
		{TcKindCodel, cat(rtattr(TcaCodelTarget, le32(5000)), rtattr(TcaCodelLimit, le32(1000)))},
		{TcKindCake, cat(rtattr(TcaCakeBaseRate64, le64(0)), rtattr(TcaCakeRtt, le32(100000)), rtattr(TcaCakeRaw, nil))},
		{TcKindDualpi2, cat(rtattr(TcaDualpi2Limit, le32(10000)), rtattr(TcaDualpi2Coupling, []byte{2}))},
		{TcKindSkbprio, le32(100)},
	} {
		b.Run(c.kind, func(b *testing.B) {
			b.ReportAllocs()
			for b.Loop() {
				if _, err := ParseTcQopt(c.kind, c.opt); err != nil {
					b.Fatal(err)
				}
			}
		})
	}
	tins := make([][]byte, CakeMaxTinsCst)
	for i := range tins {
		tins[i] = cakeTin(uint16(i+1), rtattr(TcaCakeTinStatsThresholdRate64, le64(1)), rtattr(TcaCakeTinStatsTargetUs, le32(1)))
	}
	for _, c := range []struct {
		kind string
		x    []byte
	}{
		{TcKindCake, nest(TcaCakeStatsTinStats, tins...)},
		{TcKindCodel, make([]byte, TcCodelXstatsSizeCst)},
		{TcKindDualpi2, make([]byte, TcDualpi2XstatsSizeCst)},
	} {
		b.Run(c.kind+"-xstats", func(b *testing.B) {
			b.ReportAllocs()
			for b.Loop() {
				if _, err := ParseTcXstats(c.kind, c.x); err != nil {
					b.Fatal(err)
				}
			}
		})
	}
}
