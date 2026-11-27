package xtcpnl

import (
	"errors"
	"fmt"
	"path/filepath"
	"strings"
	"testing"

	"golang.org/x/sys/unix"
)

// tcschedMsgs is every kernel reply of kind in a tcsched row's capture:
// qdiscs, or classes when class.
func tcschedMsgs(t testing.TB, row, kind string, class bool) []TcInfo {
	t.Helper()
	want := uint16(unix.RTM_NEWQDISC)
	if class {
		want = unix.RTM_NEWTCLASS
	}
	var out []TcInfo
	for _, m := range pcapNlMsgs(t, filepath.Join("testdata", "7_3_1", "ws", "tcsched", row, "ip.pcap")) {
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

// tcschedXstats is a captured message's xstats as print_xstats gets them:
// TCA_STATS_APP, else TCA_XSTATS.
func tcschedXstats(t testing.TB, ti TcInfo) []byte {
	t.Helper()
	if ti.HasStats2 {
		s, err := ParseTcStats2(ti.Stats2)
		if err != nil {
			t.Fatal(err)
		}
		if s.HasApp {
			return s.App
		}
	}
	return ti.Xstats
}

// schedMqprioQopt is a struct tc_mqprio_qopt with num_tc 2, queues 4@0
// 4@4, RTA_ALIGN-padded as the kernel sends it.
func schedMqprioQopt(pad int) []byte {
	b := make([]byte, TcMqprioQoptSizeCst+pad)
	b[0] = 2
	b[3], b[4] = 1, 1
	copy(b[18:], le16(4))
	copy(b[20:], le16(4))
	copy(b[50:], le16(0))
	copy(b[52:], le16(4))
	return b
}

func nest(t uint16, kids ...[]byte) []byte { return rtattr(t|unix.NLA_F_NESTED, cat(kids...)) }

// schedTcEntry is a TCA_*_TC_ENTRY nest: index and fp.
func schedTcEntry(entry uint16, index, fp uint32) []byte {
	return nest(entry, rtattr(1, le32(index)), rtattr(TcaMqprioTcEntryFp, le32(fp)))
}

func schedTaprioOpts(extra ...[]byte) []byte {
	return cat(append([][]byte{rtattr(TcaTaprioAttrPriomap, schedMqprioQopt(0))}, extra...)...)
}

func schedSum(q TcQopt, err error) string {
	if err != nil {
		return "err " + schedErr(err)
	}
	s := q.Sched
	switch q.Kind {
	case TcKindDrr:
		return fmt.Sprintf("quantum %d %t", s.DrrQuantum, s.HasDrrQuantum)
	case TcKindQfq:
		return fmt.Sprintf("weight %d %t lmax %d %t", s.QfqWeight, s.HasQfqWeight, s.QfqLmax, s.HasQfqLmax)
	case TcKindMultiq:
		return fmt.Sprintf("bands %d/%d %t", s.Multiq.Bands, s.Multiq.MaxBands, s.HasMultiq)
	case TcKindCbs:
		c := s.Cbs
		return fmt.Sprintf("hi %d lo %d send %d idle %d off %d", c.Hicredit, c.Locredit, c.Sendslope, c.Idleslope, c.Offload)
	case TcKindEtf:
		return fmt.Sprintf("delta %d clock %d flags %#x", s.Etf.Delta, s.Etf.Clockid, s.Etf.Flags)
	case TcKindEts:
		e := s.Ets
		return fmt.Sprintf("bands %d strict %d %t quanta %t %v priomap %v quantum %d %t", e.Nbands, e.Nstrict, e.HasNstrict,
			e.HasQuanta, e.Quanta[:e.NQuanta], e.Priomap[:e.NPriomap], e.Quantum, e.HasQuantum)
	case TcKindMqprio:
		m := s.Mqprio
		return fmt.Sprintf("tc %d map %v count %v offset %v tail %t mode %d %t shaper %d %t min %v max %v fp %d %v",
			m.Qopt.NumTc, m.Qopt.PrioTcMap[:4], m.Qopt.Count[:2], m.Qopt.Offset[:2], m.Tail, m.Mode, m.HasMode,
			m.Shaper, m.HasShaper, m.MinRate[:m.NMinRate], m.MaxRate[:m.NMaxRate], m.TcEntries.MaxTcFp, m.TcEntries.Fp[:2])
	}
	return "?"
}

func schedErr(err error) string {
	for _, e := range []error{ErrTcShort, ErrTcMissing, ErrTcSchedList, ErrTcKindUnknown, ErrRTAttrSmall} {
		if errors.Is(err, e) {
			return e.Error()
		}
	}
	return err.Error()
}

// schedFrom is s from the first sep on.
func schedFrom(s, sep string) string {
	if i := strings.Index(s, sep); i >= 0 {
		return s[i:]
	}
	return s
}

func schedCurves(q TcQopt, err error) string {
	if err != nil {
		return "err " + schedErr(err)
	}
	s := q.Sched
	return fmt.Sprintf("rt %v %t ls %v %t ul %v %t", s.HfscRsc, s.HasHfscRsc, s.HfscFsc, s.HasHfscFsc, s.HfscUsc, s.HasHfscUsc)
}

func schedTaprioSum(q TcQopt, err error) string {
	if err != nil {
		return "err " + schedErr(err)
	}
	tp := q.Sched.Taprio
	sched := func(s TcTaprioSched) string {
		out := fmt.Sprintf("base %d cycle %d ext %d:", s.BaseTime, s.CycleTime, s.CycleTimeExtension)
		for _, e := range s.Entries {
			out += fmt.Sprintf(" [%d %d %#x %d]", e.Index, e.Cmd, e.GateMask, e.Interval)
		}
		return out
	}
	out := fmt.Sprintf("tc %d clock %d %t flags %#x %t txtime %d %t oper {%s}", tp.Priomap.NumTc, tp.Clockid, tp.HasClockid,
		tp.Flags, tp.HasFlags, tp.TxtimeDelay, tp.HasTxtimeDelay, sched(tp.Oper))
	if tp.HasAdmin {
		out += " admin {" + sched(tp.Admin) + "}"
	}
	return out + fmt.Sprintf(" sdu %d fp %d", tp.TcEntries.MaxTcMaxSdu, tp.TcEntries.MaxTcFp)
}

// TestParseTcSched decodes the increment-15 kinds' options from the
// tcsched captures and from crafted bytes. The captured rows' tc output
// (testdata/7_3_1/ws/tcsched/<row>/ip.out) gives the expected values.
//
// go test ./pkg/xtcpnl/ -run TestParseTcSched
func TestParseTcSched(t *testing.T) {
	cap1 := func(row, kind string, class bool, i int) TcInfo { return tcschedMsgs(t, row, kind, class)[i] }
	qopt := func(row, kind string, i int) string {
		ti := cap1(row, kind, false, i)
		return schedSum(ParseTcQopt(kind, ti.Options))
	}
	copt := func(row, kind string, i int) string {
		ti := cap1(row, kind, true, i)
		return schedSum(ParseTcCopt(kind, ti.Options))
	}
	curves := func(row string, i int) string {
		return schedCurves(ParseTcCopt(TcKindHfsc, cap1(row, TcKindHfsc, true, i).Options))
	}
	taprio := func(row string) string {
		return schedTaprioSum(ParseTcQopt(TcKindTaprio, cap1(row, TcKindTaprio, false, 0).Options))
	}
	sc := func(m1, d, m2 uint32) []byte { return cat(le32(m1), le32(d), le32(m2)) }
	quanta := func(kids ...[]byte) []byte {
		return cat(rtattr(TcaEtsNbands, []byte{3}), nest(TcaEtsQuanta, kids...),
			nest(TcaEtsPriomap, rtattr(TcaEtsPriomapBand, []byte{0})))
	}
	rates := func(n int) []byte {
		kids := make([][]byte, n)
		for i := range kids {
			kids[i] = rtattr(TcaMqprioMinRate64, le64(uint64(1000+i)))
		}
		return cat(schedMqprioQopt(2), rtattr(TcaMqprioMinRate64, cat(kids...))) // nla_nest_start_noflag
	}
	entries := func(idx ...uint32) []byte {
		b := schedMqprioQopt(2)
		for _, i := range idx {
			b = cat(b, schedTcEntry(TcaMqprioTcEntry, i, TcFpPreemptible))
		}
		return b
	}
	tests := []struct {
		description string
		input       func() string
		expected    string
	}{
		// Positive: captured.
		{"positive: drr class quantum, set and default (pw-001)", func() string { return copt("pw-001", TcKindDrr, 0) + "; " + copt("pw-001", TcKindDrr, 1) },
			"quantum 1514 true; quantum 1514 true"},
		{"positive: qfq classes (ec-006)", func() string { return copt("ec-006", TcKindQfq, 0) + "; " + copt("ec-006", TcKindQfq, 1) },
			"weight 1 true lmax 1514 true; weight 30 true lmax 9000 true"},
		{"positive: hfsc default 10 (ec-008)", func() string {
			q, err := ParseTcQopt(TcKindHfsc, cap1("ec-008", TcKindHfsc, false, 0).Options)
			return fmt.Sprintf("%#x %t %v", q.Sched.Hfsc.Defcls, q.Sched.HasHfsc, err)
		}, "0x10 true <nil>"},
		{"positive: hfsc rt and ls differ (ec-009, 1:10)", func() string { return curves("ec-009", 2) },
			"rt {250000 10000 125000} true ls {0 0 375000} true ul {0 0 0} false"},
		{"positive: hfsc rt equal to ls, printed as sc (ec-009, 1:20, Q43)", func() string { return curves("ec-009", 3) },
			"rt {0 0 125000} true ls {0 0 125000} true ul {0 0 0} false"},
		{"positive: hfsc ls and ul (ec-009, 1:1)", func() string { return curves("ec-009", 1) },
			"rt {0 0 1250000} true ls {0 0 1250000} true ul {0 0 2500000} true"},
		{"positive: ets bands 3 strict 1 (ec-015)", func() string { return qopt("ec-015", TcKindEts, 0) },
			"bands 3 strict 1 true quanta true [1500 3000] priomap [2 1 0 2 2 2 2 2 2 2 2 2 2 2 2 2] quantum 0 false"},
		{"positive: ets classes, the strict one without a quantum (ec-016, Q46)", func() string {
			return copt("ec-016", TcKindEts, 0) + "; " + copt("ec-016", TcKindEts, 1)
		}, "bands 0 strict 0 false quanta false [] priomap [] quantum 0 false; bands 0 strict 0 false quanta false [] priomap [] quantum 1500 true"},
		{"positive: cbs (ec-021)", func() string { return qopt("ec-021", TcKindCbs, 0) },
			"hi 30 lo -1470 send -980000 idle 20000 off 0"},
		{"positive: etf skip_sock_check, CLOCK_TAI (ec-021)", func() string { return qopt("ec-021", TcKindEtf, 0) },
			"delta 300000 clock 11 flags 0x4"},
		{"positive: multiq bands 8/8 (ec-026, K27)", func() string { return qopt("ec-026", TcKindMultiq, 0) }, "bands 8/8 true"},
		{"positive: mqprio hw 0, no mode attribute, 16 entries (ec-028, Q49)", func() string { return qopt("ec-028", TcKindMqprio, 0) },
			"tc 2 map [0 0 1 1] count [4 4] offset [0 4] tail true mode 0 false shaper 0 false min [] max [] fp 15 [1 1]"},
		{"positive: taprio txtime-assist (ec-033, Q55)", func() string { return taprio("ec-033") },
			"tc 2 clock 11 true flags 0x1 true txtime 200000 true oper {base 1000000000 cycle 500000 ext 0: [0 0 0x1 300000] [1 0 0x2 200000]} sdu 15 fp 15"},
		{"positive: taprio oper and admin (ec-038, Q57)", func() string { return taprio("ec-038") },
			"tc 2 clock 11 true flags 0x0 false txtime 0 false oper {base 1000000000 cycle 500000 ext 0: [0 0 0x1 300000] [1 0 0x2 200000]} " +
				"admin {base 4000000000000000000 cycle 100000 ext 0: [0 0 0x3 100000]} sdu 15 fp 15"},
		// Positive: crafted.
		{"positive: hfsc class with rt, ls and ul", func() string {
			return schedCurves(ParseTcCopt(TcKindHfsc, cat(rtattr(TcaHfscRsc, sc(1, 2, 3)), rtattr(TcaHfscFsc, sc(4, 5, 6)), rtattr(TcaHfscUsc, sc(7, 8, 9)))))
		}, "rt {1 2 3} true ls {4 5 6} true ul {7 8 9} true"},
		// Negative.
		{"negative: mqprio options of sizeof, short of RTA_ALIGN(sizeof)", func() string {
			return schedSum(ParseTcQopt(TcKindMqprio, schedMqprioQopt(1)))
		}, "err " + ErrTcShort.Error()},
		{"negative: taprio without PRIOMAP", func() string {
			return schedTaprioSum(ParseTcQopt(TcKindTaprio, rtattr(TcaTaprioAttrFlags, le32(1))))
		}, "err " + ErrTcMissing.Error()},
		{"negative: taprio entry with a 2-byte index", func() string {
			return schedTaprioSum(ParseTcQopt(TcKindTaprio, schedTaprioOpts(nest(TcaTaprioAttrSchedEntryList,
				nest(1, rtattr(TcaTaprioSchedEntryIndex, le16(5)))))))
		}, "err " + ErrTcShort.Error()},
		{"negative: taprio entry whose second attribute overruns keeps the first, as C", func() string {
			bad := cat(rtattr(TcaTaprioSchedEntryIndex, le32(5)), []byte{0x40, 0, byte(TcaTaprioSchedEntryCmd), 0})
			return schedTaprioSum(ParseTcQopt(TcKindTaprio, schedTaprioOpts(nest(TcaTaprioAttrSchedEntryList, nest(1, bad)))))
		}, "tc 2 clock 0 false flags 0x0 false txtime 0 false oper {base 0 cycle 0 ext 0: [5 0 0x0 0]} sdu -1 fp -1"},
		{"negative: cbs without PARMS", func() string { return schedSum(ParseTcQopt(TcKindCbs, rtattr(2, le32(0)))) }, "err " + ErrTcMissing.Error()},
		{"negative: etf PARMS short", func() string {
			return schedSum(ParseTcQopt(TcKindEtf, rtattr(TcaEtfParms, make([]byte, TcEtfQoptSizeCst-1))))
		}, "err " + ErrTcShort.Error()},
		{"negative: ets without PRIOMAP", func() string { return schedSum(ParseTcQopt(TcKindEts, rtattr(TcaEtsNbands, []byte{3}))) },
			"err " + ErrTcMissing.Error()},
		{"negative: an ets class's options read as a qdisc's", func() string {
			return schedSum(ParseTcQopt(TcKindEts, rtattr(TcaEtsQuantaBand, le32(1500))))
		}, "err " + ErrTcMissing.Error()},
		{"negative: ets quanta holding only a priomap band", func() string {
			return schedSum(ParseTcQopt(TcKindEts, quanta(rtattr(TcaEtsPriomapBand, le32(1)))))
		}, "err " + ErrTcSchedList.Error()},
		{"negative: mqprio rate list with a foreign member", func() string {
			return schedSum(ParseTcQopt(TcKindMqprio, cat(schedMqprioQopt(2), rtattr(TcaMqprioMaxRate64, rtattr(TcaMqprioMinRate64, le64(1))))))
		}, "err " + ErrTcSchedList.Error()},
		{"negative: an increment-16 kind", func() string { return schedSum(ParseTcQopt("red", nil)) }, "err " + ErrTcKindUnknown.Error()},
		// Boundary.
		{"boundary: ets nbands == nstrict, no QUANTA (ec-019, K25)", func() string { return qopt("ec-019", TcKindEts, 0) },
			"bands 3 strict 3 true quanta false [] priomap [2 2 2 2 2 2 2 2 2 2 2 2 2 2 2 2] quantum 0 false"},
		{"boundary: mqprio 16 min rates", func() string { return schedFrom(schedSum(ParseTcQopt(TcKindMqprio, rates(16))), "min ") },
			"min [1000 1001 1002 1003 1004 1005 1006 1007 1008 1009 1010 1011 1012 1013 1014 1015] max [] fp -1 [0 0]"},
		{"boundary: mqprio 17 min rates, the 17th dropped (Q53)", func() string { return schedFrom(schedSum(ParseTcQopt(TcKindMqprio, rates(17))), "min ") },
			"min [1000 1001 1002 1003 1004 1005 1006 1007 1008 1009 1010 1011 1012 1013 1014 1015] max [] fp -1 [0 0]"},
		{"boundary: tc entry index 15 kept, 16 skipped", func() string {
			q, err := ParseTcQopt(TcKindMqprio, entries(15, 16))
			return fmt.Sprint(q.Sched.Mqprio.TcEntries.MaxTcFp, q.Sched.Mqprio.TcEntries.Fp[15], err)
		}, "15 2 <nil>"},
		{"boundary: multiq of 3 bytes prints nothing, no error", func() string { return schedSum(ParseTcQopt(TcKindMultiq, []byte{8, 0, 8})) },
			"bands 0/0 false"},
		// Corner.
		{"corner: drr and qfq qdiscs carry no options (K29)", func() string {
			d, q := cap1("pw-002", TcKindDrr, false, 0), cap1("pw-005", TcKindQfq, false, 0)
			return fmt.Sprint(d.HasOptions, len(d.Options), q.HasOptions, len(q.Options), " ", qopt("pw-002", TcKindDrr, 0))
		}, "false 0 false 0 quantum 0 false"},
		{"corner: TXTIME_DELAY 0x80000000 reads as s32 (Q55)", func() string {
			q, err := ParseTcQopt(TcKindTaprio, schedTaprioOpts(rtattr(TcaTaprioAttrTxtimeDelay, le32(0x80000000))))
			return fmt.Sprint(q.Sched.Taprio.TxtimeDelay, err)
		}, "-2147483648 <nil>"},
		{"corner: an ets priomap band of 4 bytes misaligns the +3 step", func() string {
			in := cat(rtattr(TcaEtsNbands, []byte{3}), nest(TcaEtsPriomap, rtattr(TcaEtsPriomapBand, le32(1)), rtattr(TcaEtsPriomapBand, le32(2))))
			return schedSum(ParseTcQopt(TcKindEts, in))
		}, "err " + ErrTcSchedList.Error()},
		{"corner: a truncated hfsc curve is skipped, not an error", func() string {
			return schedCurves(ParseTcCopt(TcKindHfsc, rtattr(TcaHfscRsc, le32(1))))
		}, "rt {0 0 0} false ls {0 0 0} false ul {0 0 0} false"},
	}
	for _, tt := range tests {
		t.Run(tt.description, func(t *testing.T) {
			if got := tt.input(); got != tt.expected {
				t.Errorf("got\n%q\nwant\n%q", got, tt.expected)
			}
		})
	}
}

// TestParseTcSchedXstats decodes the class xstats drr and hfsc print, and
// taprio's offload stats.
func TestParseTcSchedXstats(t *testing.T) {
	xs := func(row, kind string, class bool, i int) string {
		ti := tcschedMsgs(t, row, kind, class)[i]
		x, err := ParseTcXstats(kind, tcschedXstats(t, ti))
		return fmt.Sprintf("%t %+v %+v %v", x.Has, x.Drr, x.Hfsc, err)
	}
	tests := []struct {
		description string
		input       func() string
		expected    string
	}{
		{"positive: drr deficit (pw-003, Q38)", func() string { return xs("pw-003", TcKindDrr, true, 0) },
			"true {Deficit:0} {Work:0 Rtwork:0 Period:0 Level:0} <nil>"},
		{"positive: hfsc root class level 2 (ec-010)", func() string { return xs("ec-010", TcKindHfsc, true, 0) },
			"true {Deficit:0} {Work:0 Rtwork:0 Period:0 Level:2} <nil>"},
		{"positive: taprio offload stats", func() string {
			x, err := ParseTcXstats(TcKindTaprio, rtattr(TcaTaprioOffloadStatsWindowDrops, le64(7)))
			return fmt.Sprint(x.Has, x.TaprioWindowDrops, x.HasTaprioWindowDrops, x.HasTaprioTxOverruns, err)
		}, "true 7 true false <nil>"},
		{"negative: drr xstats short", func() string {
			_, err := ParseTcXstats(TcKindDrr, []byte{1, 2})
			return schedErr(err)
		}, ErrTcShort.Error()},
		{"boundary: hfsc xstats of exactly sizeof", func() string {
			x, err := ParseTcXstats(TcKindHfsc, make([]byte, TcHfscStatsSizeCst))
			return fmt.Sprint(x.Has, err)
		}, "true <nil>"},
		{"corner: qfq sends xstats tc never prints (Q39, K24)", func() string {
			ti := tcschedMsgs(t, "pw-007", TcKindQfq, true)[1]
			b := tcschedXstats(t, ti)
			x, err := ParseTcXstats(TcKindQfq, b)
			st, serr := DecodeTcQfqStats(b)
			return fmt.Sprint(x.Has, err, " ", st, serr)
		}, "false <nil> {30 9000} <nil>"},
	}
	for _, tt := range tests {
		t.Run(tt.description, func(t *testing.T) {
			if got := tt.input(); got != tt.expected {
				t.Errorf("got %q, want %q", got, tt.expected)
			}
		})
	}
}

// TestParseTcSchedAllocs pins the decoders' allocations: the top-level
// attribute tables and taprio's entry slices. The tc and schedule entry
// nests are parsed into a stack table, so 16 entries cost nothing.
func TestParseTcSchedAllocs(t *testing.T) {
	allocs := func(kind string, in []byte) float64 {
		return testing.AllocsPerRun(100, func() {
			if _, err := ParseTcQopt(kind, in); err != nil {
				t.Fatal(err)
			}
		})
	}
	entryList := func(n int) []byte {
		kids := make([][]byte, n)
		for i := range kids {
			kids[i] = nest(1, rtattr(TcaTaprioSchedEntryIndex, le32(uint32(i))), rtattr(TcaTaprioSchedEntryInterval, le32(1000)))
		}
		return nest(TcaTaprioAttrSchedEntryList, kids...)
	}
	mq16 := schedMqprioQopt(2)
	for i := range uint32(16) {
		mq16 = cat(mq16, schedTcEntry(TcaMqprioTcEntry, i, TcFpExpress))
	}
	tests := []struct {
		description string
		input       func() float64
		expected    float64
	}{
		{"positive: hfsc qdisc, a fixed struct", func() float64 { return allocs(TcKindHfsc, le16(0x10)) }, 0},
		{"positive: multiq, a fixed struct", func() float64 { return allocs(TcKindMultiq, cat(le16(8), le16(8))) }, 0},
		{"positive: cbs, its table", func() float64 { return allocs(TcKindCbs, rtattr(TcaCbsParms, make([]byte, TcCbsQoptSizeCst))) }, 1},
		{"positive: etf, its table", func() float64 { return allocs(TcKindEtf, rtattr(TcaEtfParms, make([]byte, TcEtfQoptSizeCst))) }, 1},
		{"positive: taprio, oper and admin with 2 entries each", func() float64 {
			return allocs(TcKindTaprio, schedTaprioOpts(entryList(2), nest(TcaTaprioAttrAdminSched, entryList(2))))
		}, 6}, // 2 tables, 2 appends per entry list
		{"boundary: mqprio with 16 tc entries, the header table only", func() float64 { return allocs(TcKindMqprio, mq16) }, 1},
		{"corner: drr qdisc, no options", func() float64 { return allocs(TcKindDrr, nil) }, 1},
	}
	for _, tt := range tests {
		t.Run(tt.description, func(t *testing.T) {
			if got := tt.input(); got != tt.expected {
				t.Errorf("got %v allocations, want %v", got, tt.expected)
			}
		})
	}
}

func BenchmarkParseTcSched(b *testing.B) {
	mq := schedMqprioQopt(2)
	for i := range uint32(16) {
		mq = cat(mq, schedTcEntry(TcaMqprioTcEntry, i, TcFpExpress))
	}
	for _, c := range []struct {
		kind string
		opt  []byte
	}{
		{TcKindDrr, rtattr(TcaDrrQuantum, le32(1514))},
		{TcKindHfsc, le16(0x10)},
		{TcKindEts, cat(rtattr(TcaEtsNbands, []byte{3}), nest(TcaEtsQuanta, rtattr(TcaEtsQuantaBand, le32(1500))),
			nest(TcaEtsPriomap, rtattr(TcaEtsPriomapBand, []byte{0})))},
		{TcKindMqprio, mq},
		{TcKindTaprio, schedTaprioOpts(rtattr(TcaTaprioAttrSchedClockid, le32(11)),
			nest(TcaTaprioAttrSchedEntryList, nest(1, rtattr(TcaTaprioSchedEntryIndex, le32(0)))))},
		{TcKindCbs, rtattr(TcaCbsParms, make([]byte, TcCbsQoptSizeCst))},
		{TcKindEtf, rtattr(TcaEtfParms, make([]byte, TcEtfQoptSizeCst))},
		{TcKindMultiq, cat(le16(8), le16(8))},
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
	for _, c := range []struct {
		kind string
		x    []byte
	}{{TcKindDrr, le32(1)}, {TcKindHfsc, make([]byte, TcHfscStatsSizeCst)}} {
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
