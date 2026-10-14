package xtcpnl

import (
	"errors"
	"reflect"
	"strconv"
	"testing"
)

// TestRdmaEntriesReuse covers list nests whose entries share one tb: an
// attribute one entry carries must not show in the next.
func TestRdmaEntriesReuse(t *testing.T) {
	statGet := RdmaNlGetType(RdmaNlNldev, RdmaNldevCmdStatGet)
	resGet := RdmaNlGetType(RdmaNlNldev, RdmaNldevCmdResGet)
	srqGet := RdmaNlGetType(RdmaNlNldev, RdmaNldevCmdResSRQGet)
	handle := rdBody(rdU32(RdmaNldevAttrDevIndex, 0), rdStr(RdmaNldevAttrDevName, "rxe0"))
	rxe0 := RdmaHandle{HasDevIndex: true, DevName: "rxe0", HasDevName: true}
	name := func(n string) []byte { return rdStr(RdmaNldevAttrStatHwcounterEntryName, n) }
	value := func(v uint64) []byte { return rdU64(RdmaNldevAttrStatHwcounterEntryValue, v) }
	counter := func(b ...[]byte) []byte { return rdNest(RdmaNldevAttrStatHwcounterEntry, b...) }
	stat := func(b ...[]byte) []byte { return rdBody(handle, rdNest(RdmaNldevAttrStatHwcounters, b...)) }
	summary := func(b ...[]byte) []byte {
		return rdNest(RdmaNldevAttrResSummaryEntry, b...)
	}
	srq := func(qps ...[]byte) []byte {
		return rdBody(handle, rdNest(RdmaNldevAttrResSRQ, rdNest(RdmaNldevAttrResSRQEntry,
			rdU32(RdmaNldevAttrResSRQN, 1), rdNest(RdmaNldevAttrResQP, qps...))))
	}
	lqpn := rdNest(RdmaNldevAttrResQPEntry, rdU32(RdmaNldevAttrResLQPN, 5))
	qpRange := rdNest(RdmaNldevAttrResQPEntry, rdU32(RdmaNldevAttrMinRange, 7), rdU32(RdmaNldevAttrMaxRange, 22))
	srqs := func(qps ...RdmaQPRange) RdmaResSRQs {
		return RdmaResSRQs{RdmaHandle: rxe0, Entries: []RdmaResSRQ{{SRQN: 1, HasSRQN: true, QPs: qps}}}
	}
	tests := []struct {
		description string
		msgType     uint16
		body        []byte
		expected    any
		expectedErr error
	}{
		{description: "positive: distinct values per entry", msgType: statGet,
			body: stat(counter(name("a"), value(1)), counter(name("b"), value(2)), counter(name("c"), value(3))),
			expected: RdmaStat{RdmaHandle: rxe0, HwCounters: []RdmaHwCounter{
				{Name: "a", Value: 1, HasValue: true}, {Name: "b", Value: 2, HasValue: true}, {Name: "c", Value: 3, HasValue: true}}}},
		{description: "positive: index and dynamic of the first entry do not carry to the second", msgType: statGet,
			body: stat(counter(name("a"), value(1), rdU32(RdmaNldevAttrStatHwcounterIndex, 7),
				rdU8(RdmaNldevAttrStatHwcounterDynamic, 1)), counter(name("b"), value(2))),
			expected: RdmaStat{RdmaHandle: rxe0, HwCounters: []RdmaHwCounter{
				{Name: "a", Value: 1, HasValue: true, Index: 7, HasIndex: true, Dynamic: 1, HasDynamic: true},
				{Name: "b", Value: 2, HasValue: true}}}},
		{description: "negative: an entry without a value after one with it has no value", msgType: statGet,
			body: stat(counter(name("a"), value(1)), counter(name("b"))),
			expected: RdmaStat{RdmaHandle: rxe0, HwCounters: []RdmaHwCounter{
				{Name: "a", Value: 1, HasValue: true}, {Name: "b"}}}},
		{description: "negative: an entry without a name after one with it has no name", msgType: statGet,
			body: stat(counter(name("a"), value(1)), counter(value(2))),
			expected: RdmaStat{RdmaHandle: rxe0, HwCounters: []RdmaHwCounter{
				{Name: "a", Value: 1, HasValue: true}, {Value: 2, HasValue: true}}}},
		{description: "negative: a malformed second entry fails the reply", msgType: statGet,
			body:        stat(counter(name("a"), value(1)), counter(name("b"), rdU32(RdmaNldevAttrStatHwcounterEntryValue, 2))),
			expectedErr: ErrRdmaAttrLen},
		{description: "boundary: one entry", msgType: statGet, body: stat(counter(name("a"), value(1))),
			expected: RdmaStat{RdmaHandle: rxe0, HwCounters: []RdmaHwCounter{{Name: "a", Value: 1, HasValue: true}}}},
		{description: "boundary: an empty entry after a full one", msgType: statGet,
			body:     stat(counter(name("a"), value(1)), counter()),
			expected: RdmaStat{RdmaHandle: rxe0, HwCounters: []RdmaHwCounter{{Name: "a", Value: 1, HasValue: true}, {}}}},
		{description: "boundary: an empty list nest", msgType: statGet, body: stat(),
			expected: RdmaStat{RdmaHandle: rxe0}},
		{description: "corner: the value reverts to zero with an explicit zero", msgType: statGet,
			body: stat(counter(name("a"), value(1<<63)), counter(name("b"), value(0))),
			expected: RdmaStat{RdmaHandle: rxe0, HwCounters: []RdmaHwCounter{
				{Name: "a", Value: 1 << 63, HasValue: true}, {Name: "b", HasValue: true}}}},
		{description: "corner: a RES summary entry without curr after one with it", msgType: resGet,
			body: rdBody(handle, rdNest(RdmaNldevAttrResSummary,
				summary(rdStr(RdmaNldevAttrResSummaryEntryName, "qp"), rdU64(RdmaNldevAttrResSummaryEntryCurr, 4)),
				summary(rdStr(RdmaNldevAttrResSummaryEntryName, "cq")))),
			expected: RdmaRes{RdmaHandle: rxe0, Summary: []RdmaResEntry{{Name: "qp", Curr: 4, HasCurr: true}, {Name: "cq"}}}},
		{description: "corner: an SRQ range after an LQPN is a range", msgType: srqGet, body: srq(lqpn, qpRange),
			expected: srqs(RdmaQPRange{5, 5}, RdmaQPRange{7, 22})},
		{description: "corner: an SRQ LQPN after a range is a single QP", msgType: srqGet, body: srq(qpRange, lqpn),
			expected: srqs(RdmaQPRange{7, 22}, RdmaQPRange{5, 5})},
	}
	for _, tc := range tests {
		t.Run(tc.description, func(t *testing.T) {
			got, err := ParseRdmaMsg(tc.msgType, tc.body)
			if !errors.Is(err, tc.expectedErr) {
				t.Fatalf("err %v, want %v", err, tc.expectedErr)
			}
			if err != nil {
				return
			}
			if !reflect.DeepEqual(got, tc.expected) {
				t.Errorf("got  %+v\nwant %+v", got, tc.expected)
			}
		})
	}
}

// rdmaStatBody is a STAT_GET reply with n hardware counters, about the 31 of
// an mlx5 RoCE port at n=32.
func rdmaStatBody(n int) []byte {
	entries := make([][]byte, n)
	for i := range entries {
		entries[i] = rdNest(RdmaNldevAttrStatHwcounterEntry,
			rdStr(RdmaNldevAttrStatHwcounterEntryName, "counter_"+strconv.Itoa(i)),
			rdU64(RdmaNldevAttrStatHwcounterEntryValue, uint64(i)))
	}
	return rdBody(rdU32(RdmaNldevAttrDevIndex, 0), rdStr(RdmaNldevAttrDevName, "mlx5_0"),
		rdU32(RdmaNldevAttrPortIndex, 1), rdNest(RdmaNldevAttrStatHwcounters, entries...))
}

// TestParseRdmaStatAllocs bounds a reply's allocations at the counts with
// one shared entry tb: about one per counter, its name, plus the slices'
// growth. A tb per counter, about 3 KiB each, was 78 allocations and 119 KB
// for 32 counters, against 47 and 12 KB.
func TestParseRdmaStatAllocs(t *testing.T) {
	for _, tc := range []struct {
		description string
		counters    int
		expected    float64
	}{
		{description: "boundary: no counters", counters: 0, expected: 3},
		{description: "positive: one counter", counters: 1, expected: 6},
		{description: "positive: an mlx5 port's counters", counters: 32, expected: 47},
		{description: "corner: many counters", counters: 256, expected: 277},
	} {
		t.Run(tc.description, func(t *testing.T) {
			body := rdmaStatBody(tc.counters)
			got := testing.AllocsPerRun(100, func() {
				if _, err := ParseRdmaStat(body); err != nil {
					t.Fatal(err)
				}
			})
			if got > tc.expected {
				t.Fatalf("%v allocations, expected at most %v", got, tc.expected)
			}
		})
	}
}

// BenchmarkParseRdmaStat decodes a STAT_GET reply, one per RDMA port each
// poll.
func BenchmarkParseRdmaStat(b *testing.B) {
	for _, n := range []int{32, 256} {
		body := rdmaStatBody(n)
		b.Run(strconv.Itoa(n), func(b *testing.B) {
			b.Logf("%d counters; expected: about 370 B and 1.5 allocations per counter, no tb per counter", n)
			b.ReportAllocs()
			for b.Loop() {
				if _, err := ParseRdmaStat(body); err != nil {
					b.Fatal(err)
				}
			}
		})
	}
}
