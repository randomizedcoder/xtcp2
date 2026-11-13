package xtcp

import (
	"encoding/binary"
	"slices"
	"sync"
	"testing"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/testutil"

	"github.com/randomizedcoder/xtcp2/gen/go/xtcp_config"
	"github.com/randomizedcoder/xtcp2/pkg/xtcpnl"
)

// TestDumpTracker_byteCap pins assign's byte accounting: which packets of a
// dump are past -maxDumpBytes, which one is the first, and that NLMSG_DONE
// starts the next dump from zero.
func TestDumpTracker_byteCap(t *testing.T) {
	done := doneMessage() // 20 bytes
	data := func(n int) []byte { return make([]byte, n) }

	type pkt struct {
		b     []byte
		drop  bool
		first bool
	}
	tests := []struct {
		name        string
		category    string
		description string
		maxBytes    uint64
		packets     []pkt
	}{
		{
			name: "no_cap", category: "P",
			description: "cap 0 never drops, however large the dump",
			maxBytes:    0,
			packets:     []pkt{{b: data(1 << 20)}, {b: data(1 << 20)}, {b: done}},
		},
		{
			name: "under_caps", category: "P",
			description: "a dump smaller than the cap is all kept",
			maxBytes:    1000,
			packets:     []pkt{{b: data(100)}, {b: data(100)}, {b: done}},
		},
		{
			name: "exact", category: "B",
			description: "bytes equal to the cap are kept; the next byte is not",
			maxBytes:    200,
			packets:     []pkt{{b: data(100)}, {b: data(100)}, {b: done, drop: true, first: true}},
		},
		{
			name: "plus_one", category: "B",
			description: "the packet that crosses the cap is the first dropped",
			maxBytes:    199,
			packets:     []pkt{{b: data(100)}, {b: data(100), drop: true, first: true}, {b: done, drop: true}},
		},
		{
			name: "stays_truncated", category: "C",
			description: "after the cap, a packet that would fit is still dropped, so the kept records are a prefix",
			maxBytes:    150,
			packets:     []pkt{{b: data(100)}, {b: data(100), drop: true, first: true}, {b: data(1), drop: true}, {b: done, drop: true}},
		},
		{
			name: "single_packet_over", category: "C",
			description: "a first packet larger than the cap is dropped",
			maxBytes:    10,
			packets:     []pkt{{b: data(100), drop: true, first: true}, {b: done, drop: true}},
		},
		{
			name: "reset_next_dump", category: "C",
			description: "NLMSG_DONE in a dropped packet still ends the dump; the next one starts at 0 bytes",
			maxBytes:    150,
			packets: []pkt{
				{b: data(100)}, {b: data(100), drop: true, first: true}, {b: done, drop: true},
				{b: data(100)}, {b: done}, // 120 bytes: under the cap
			},
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			tr := newDumpTracker(tc.maxBytes, 0)
			var dumps []*dumpState
			for i, p := range tc.packets {
				a := tr.assign(p.b)
				if a.drop != p.drop || a.bytesCut != p.first {
					t.Errorf("%s: packet %d: drop=%v first=%v, expected drop=%v first=%v",
						tc.description, i, a.drop, a.bytesCut, p.drop, p.first)
				}
				if len(dumps) == 0 || dumps[len(dumps)-1] != a.dump {
					dumps = append(dumps, a.dump)
				}
			}
			// Every dump ended with done has its total stored.
			for i, d := range dumps {
				if d.total.Load() == 0 {
					t.Errorf("%s: dump %d has no total", tc.description, i)
				}
			}
		})
	}
}

// diagPacket is a packet of k bare inet_diag message headers, enough for
// packetScan to count; done appends NLMSG_DONE.
func diagPacket(k int, done bool) []byte {
	var p []byte
	for range k {
		h := make([]byte, xtcpnl.NlMsgHdrSizeCst)
		binary.LittleEndian.PutUint32(h[0:4], uint32(len(h)))
		binary.LittleEndian.PutUint16(h[4:6], xtcpnl.NlMsgHdrTypeInetDiagCst)
		p = append(p, h...)
	}
	if done {
		p = append(p, doneMessage()...)
	}
	return p
}

// TestDumpTracker_recordCap pins assign's record budget: how many records
// each packet may keep, which packets are drained, the records counted out,
// and that the cap is cut once per dump.
func TestDumpTracker_recordCap(t *testing.T) {
	type pkt struct {
		k, keep, dropped int
		done, drop, cut  bool
	}
	tests := []struct {
		name        string
		category    string
		description string
		maxRecords  uint64
		maxBytes    uint64
		packets     []pkt
	}{
		{name: "off", category: "P", description: "cap 0 keeps every record",
			packets: []pkt{{k: 1000}, {k: 1000, done: true}}},
		{name: "records_cap_exact", category: "B", description: "records equal to the cap are all kept",
			maxRecords: 100, packets: []pkt{{k: 60}, {k: 40}, {k: 0, done: true}}},
		{name: "records_cap_plus_one", category: "N", description: "one record over: the crossing packet keeps all but one",
			maxRecords: 99, packets: []pkt{{k: 60}, {k: 40, keep: 39, dropped: 1, cut: true}, {k: 0, done: true, drop: true}}},
		{name: "records_cap_mid_packet", category: "N", description: "the crossing packet keeps its budget, later ones are drained",
			maxRecords: 100, packets: []pkt{{k: 60}, {k: 60, keep: 40, dropped: 20, cut: true}, {k: 60, dropped: 60, drop: true}, {k: 0, done: true, drop: true}}},
		{name: "exact_then_more", category: "B", description: "a dump exactly at the cap drains the next record packet whole",
			maxRecords: 60, packets: []pkt{{k: 60}, {k: 1, dropped: 1, drop: true, cut: true}, {k: 0, done: true, drop: true}}},
		{name: "cap_reset_next_dump", category: "C", description: "NLMSG_DONE resets the budget",
			maxRecords: 50, packets: []pkt{{k: 60, keep: 50, dropped: 10, cut: true, done: true}, {k: 50, done: true}}},
		{name: "bytes_cap_first", category: "C", description: "a packet past the byte cap is drained and not counted against the record cap",
			maxRecords: 50, maxBytes: 16 * 40,
			packets: []pkt{{k: 40}, {k: 40, drop: true}, {k: 0, done: true, drop: true}}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			tr := newDumpTracker(tc.maxBytes, tc.maxRecords)
			for i, p := range tc.packets {
				a := tr.assign(diagPacket(p.k, p.done))
				if a.keep != p.keep || a.recordsDropped != p.dropped || a.drop != p.drop || a.recordsCut != p.cut {
					t.Errorf("%s: packet %d: keep=%d dropped=%d drop=%v cut=%v, expected keep=%d dropped=%d drop=%v cut=%v",
						tc.description, i, a.keep, a.recordsDropped, a.drop, a.recordsCut, p.keep, p.dropped, p.drop, p.cut)
				}
			}
		})
	}
}

// newCapsXTCP is newTestDeserializeXTCP with the cap metrics registered.
func newCapsXTCP(tb testing.TB) *XTCP {
	tb.Helper()
	x := newTestDeserializeXTCP(tb)
	x.fdToNsMap = &sync.Map{}
	x.pDumpDropped = prometheus.NewCounterVec(prometheus.CounterOpts{Name: "dump_dropped_total"}, []string{"reason"})
	x.pNetnsDropped = prometheus.NewCounter(prometheus.CounterOpts{Name: "netns_dropped_total"})
	return x
}

// TestNetlinkerDump_caps runs real dumps through both receivers with the
// byte and record caps set: the kept records, the done signal and the drop
// counters.
func TestNetlinkerDump_caps(t *testing.T) {
	c := loadDumpCorpus(t)
	pkts := c.dump(4) // 4 packets of dumpRecordsPerPkt records, then done
	pktBytes := uint64(len(pkts[0]))
	all := 4 * dumpRecordsPerPkt

	tests := []struct {
		name          string
		category      string
		description   string
		netlinkers    uint32
		dumps         int
		maxBytes      uint64
		maxRecords    uint64
		expectedRows  int // per dump
		expectedBytes float64
		expectedRecs  float64
	}{
		{name: "under_caps", category: "P", description: "caps above the dump keep every record",
			netlinkers: 2, dumps: 1, maxBytes: 1 << 30, maxRecords: 1 << 20, expectedRows: all},
		{name: "caps_zero_unlimited", category: "P", description: "caps 0 keep every record",
			netlinkers: 2, dumps: 1, expectedRows: all},
		{name: "dump_bytes_cap", category: "N", description: "a byte cap inside packet 2 keeps packet 1 and drains the rest",
			netlinkers: 2, dumps: 1, maxBytes: pktBytes + pktBytes/2, expectedRows: dumpRecordsPerPkt, expectedBytes: 1},
		{name: "dump_bytes_cap_exact", category: "B", description: "a byte cap at two packets keeps both",
			netlinkers: 2, dumps: 1, maxBytes: 2 * pktBytes, expectedRows: 2 * dumpRecordsPerPkt, expectedBytes: 1},
		{name: "records_cap_exact", category: "B", description: "a record cap equal to the dump keeps all",
			netlinkers: 2, dumps: 1, maxRecords: uint64(all), expectedRows: all},
		{name: "records_cap_plus_one", category: "N", description: "a record cap one under the dump drops one",
			netlinkers: 2, dumps: 1, maxRecords: uint64(all - 1), expectedRows: all - 1, expectedRecs: 1},
		{name: "records_cap_parallel", category: "C", description: "four decoders share the cap exactly",
			netlinkers: 4, dumps: 1, maxRecords: 100, expectedRows: 100, expectedRecs: 1},
		{name: "cap_reset_next_dump", category: "C", description: "each dump is capped afresh",
			netlinkers: 2, dumps: 3, maxBytes: pktBytes, maxRecords: 30, expectedRows: 30, expectedBytes: 3, expectedRecs: 3},
	}
	for _, variant := range dumpVariants {
		for _, tc := range tests {
			t.Run(variant+"/"+tc.name, func(t *testing.T) {
				h := newDumpHarnessWith(t, variant, tc.netlinkers, nil, func(x *XTCP) {
					x.pDumpDropped = prometheus.NewCounterVec(prometheus.CounterOpts{Name: "d"}, []string{"reason"})
					x.config.MaxDumpBytes = tc.maxBytes
					x.config.MaxRecordsPerNetns = tc.maxRecords
				})

				for i := range tc.dumps {
					h.startDump()
					h.send(t, pkts...)
					if !h.waitDone(dumpDoneTimeout) {
						t.Fatalf("%s: dump %d: no done signal", tc.description, i)
					}
					// Every kept record is appended before the done signal.
					if rows := h.flush(); rows != tc.expectedRows {
						t.Errorf("%s: dump %d: %d rows, expected %d", tc.description, i, rows, tc.expectedRows)
					}
				}
				if h.waitDone(50 * time.Millisecond) {
					t.Errorf("%s: a second done signal", tc.description)
				}
				if got := testutil.ToFloat64(h.x.pDumpDropped.WithLabelValues(dumpDropBytesCst)); got != tc.expectedBytes {
					t.Errorf("%s: dump_dropped_total{bytes}=%v, expected %v", tc.description, got, tc.expectedBytes)
				}
				if got := testutil.ToFloat64(h.x.pDumpDropped.WithLabelValues(dumpDropRecordsCst)); got != tc.expectedRecs {
					t.Errorf("%s: dump_dropped_total{records}=%v, expected %v", tc.description, got, tc.expectedRecs)
				}
				if d := h.drops(); d != 0 {
					t.Errorf("%s: %d records dropped after the flush", tc.description, d)
				}
			})
		}
	}
}

// TestAdmitNamespaces pins -maxNetns: the tracked set converges to the
// lowest-pid live namespaces, xtcp2's own always among them.
func TestAdmitNamespaces(t *testing.T) {
	const self = 1
	ns := func(pid int, inode uint64) nsIdentity { return nsIdentity{pid: pid, inode: inode} }
	selfNs := ns(0, self)

	tests := []struct {
		name            string
		category        string
		description     string
		candidates      []nsIdentity
		tracked         []nsIdentity
		limit           uint32
		expectedAdmit   []uint64 // inodes, in rank order
		expectedEvict   []uint64
		expectedDropped int
	}{
		{name: "netns_cap_zero", category: "P", description: "cap 0 admits all, evicts none",
			candidates: []nsIdentity{ns(9, 90), ns(3, 30)}, tracked: []nsIdentity{selfNs, ns(5, 50)}, limit: 0,
			expectedAdmit: []uint64{90, 30}},
		{name: "under_cap", category: "P", description: "room for every candidate",
			candidates: []nsIdentity{ns(9, 90), ns(3, 30)}, tracked: []nsIdentity{selfNs}, limit: 10,
			expectedAdmit: []uint64{30, 90}},
		{name: "netns_cap", category: "N", description: "cap 3 with self tracked: the two lowest pids of five win",
			candidates: []nsIdentity{ns(50, 500), ns(20, 200), ns(40, 400), ns(10, 100), ns(30, 300)},
			tracked:    []nsIdentity{selfNs}, limit: 3, expectedAdmit: []uint64{100, 200}, expectedDropped: 3},
		{name: "netns_cap_slot_frees", category: "B", description: "a slot freed by a delete goes to the lowest-pid candidate",
			candidates: []nsIdentity{ns(40, 400), ns(30, 300)}, tracked: []nsIdentity{selfNs, ns(5, 50)}, limit: 3,
			expectedAdmit: []uint64{300}, expectedDropped: 1},
		{name: "at_cap_higher_pid", category: "B", description: "at the cap, a newer (higher-pid) candidate stays out",
			candidates: []nsIdentity{ns(90, 900)}, tracked: []nsIdentity{selfNs, ns(5, 50), ns(6, 60)}, limit: 3,
			expectedDropped: 1},
		{name: "displace_burst", category: "N", description: "a tenant's burst filled the cap; an older workload's namespace displaces the highest-pid one",
			candidates: []nsIdentity{ns(7, 70)}, tracked: []nsIdentity{selfNs, ns(1000, 1000), ns(1001, 1001)}, limit: 3,
			expectedAdmit: []uint64{70}, expectedEvict: []uint64{1001}},
		{name: "converged_stable", category: "C", description: "the evicted namespace, now a candidate, loses again: no churn",
			candidates: []nsIdentity{ns(1001, 1001)}, tracked: []nsIdentity{selfNs, ns(7, 70), ns(1000, 1000)}, limit: 3,
			expectedDropped: 1},
		{name: "bind_mount_first", category: "C", description: "a bind-mounted namespace (pid 0) outranks process ones",
			candidates: []nsIdentity{ns(0, 77)}, tracked: []nsIdentity{selfNs, ns(5, 50), ns(6, 60)}, limit: 3,
			expectedAdmit: []uint64{77}, expectedEvict: []uint64{60}},
		{name: "self_always", category: "C", description: "xtcp2's own namespace is admitted even past the cap",
			candidates: []nsIdentity{ns(7, 70), ns(99, self)}, tracked: []nsIdentity{ns(2, 20), ns(3, 30)}, limit: 1,
			expectedAdmit: []uint64{self}, expectedEvict: []uint64{20, 30}, expectedDropped: 1},
		{name: "pid_tie", category: "C", description: "equal pids order by inode",
			candidates: []nsIdentity{ns(5, 60), ns(5, 50)}, limit: 1, expectedAdmit: []uint64{50}, expectedDropped: 1},
		{name: "no_candidates", category: "C", description: "nothing new: nothing evicted, even at the cap",
			tracked: []nsIdentity{selfNs, ns(5, 50), ns(6, 60)}, limit: 2},
	}
	inodes := func(ids []nsIdentity) []uint64 {
		out := make([]uint64, 0, len(ids))
		for _, id := range ids {
			out = append(out, id.inode)
		}
		return out
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			admit, evict, dropped := admitNamespaces(slices.Clone(tc.candidates), tc.tracked, tc.limit, self)
			if got := inodes(admit); !slices.Equal(got, tc.expectedAdmit) {
				t.Errorf("%s: admitted %v, expected %v", tc.description, got, tc.expectedAdmit)
			}
			if got := inodes(evict); !slices.Equal(got, tc.expectedEvict) {
				t.Errorf("%s: evicted %v, expected %v", tc.description, got, tc.expectedEvict)
			}
			if dropped != tc.expectedDropped {
				t.Errorf("%s: dropped %d, expected %d", tc.description, dropped, tc.expectedDropped)
			}
		})
	}
}

// TestTrackedLive: tracked namespaces still live, with the scan's pid, and
// xtcp2's own even when the scan missed it.
func TestTrackedLive(t *testing.T) {
	const self = 1
	tests := []struct {
		name        string
		category    string
		description string
		tracked     []uint64
		live        map[uint64]nsIdentity
		expected    []nsIdentity
	}{
		{name: "live_current_pid", category: "P", description: "a live tracked namespace carries the scan's pid",
			tracked: []uint64{50}, live: map[uint64]nsIdentity{50: {inode: 50, pid: 9}},
			expected: []nsIdentity{{inode: 50, pid: 9}}},
		{name: "gone", category: "N", description: "a tracked namespace no longer live is left out",
			tracked: []uint64{50}, live: map[uint64]nsIdentity{}},
		{name: "self_unscanned", category: "C", description: "self holds a slot though the scan missed it",
			tracked: []uint64{self}, live: map[uint64]nsIdentity{}, expected: []nsIdentity{{inode: self}}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			m := &sync.Map{}
			for _, inode := range tc.tracked {
				m.Store(inode, netNSitem{inode: inode})
			}
			if got := trackedLive(m, tc.live, self); !slices.Equal(got, tc.expected) {
				t.Errorf("%s: %v, expected %v", tc.description, got, tc.expected)
			}
		})
	}
}

// TestSetPreservesCaps: ConfigService.Set cannot change the caps.
func TestSetPreservesCaps(t *testing.T) {
	running := &xtcp_config.XtcpConfig{MaxDumpBytes: 100, MaxRecordsPerNetns: 200, MaxNetns: 300}
	tests := []struct {
		name        string
		category    string
		description string
		in          *xtcp_config.XtcpConfig
	}{
		{name: "set_omits_caps", category: "N", description: "a Set without the caps would turn them off", in: &xtcp_config.XtcpConfig{}},
		{name: "set_raises_caps", category: "N", description: "a Set raising the caps", in: &xtcp_config.XtcpConfig{MaxDumpBytes: 1 << 40, MaxRecordsPerNetns: 1 << 40, MaxNetns: 1 << 20}},
		{name: "set_same_caps", category: "P", description: "a Get→Set round trip", in: &xtcp_config.XtcpConfig{MaxDumpBytes: 100, MaxRecordsPerNetns: 200, MaxNetns: 300}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			preserveCaps(tc.in, running)
			if tc.in.MaxDumpBytes != 100 || tc.in.MaxRecordsPerNetns != 200 || tc.in.MaxNetns != 300 {
				t.Errorf("%s: caps %d/%d/%d, expected the running 100/200/300",
					tc.description, tc.in.MaxDumpBytes, tc.in.MaxRecordsPerNetns, tc.in.MaxNetns)
			}
		})
	}
}
