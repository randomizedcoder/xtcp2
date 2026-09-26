//go:build enrich_locality

package xtcp

import (
	"encoding/binary"
	"errors"
	"sort"
	"syscall"
	"testing"
	"time"

	"github.com/prometheus/client_golang/prometheus/testutil"
	"golang.org/x/sys/unix"

	"github.com/randomizedcoder/xtcp2/pkg/localnet"
	"github.com/randomizedcoder/xtcp2/pkg/xtcpnl"
)

// newLocalityFixture builds the shared metrics fixture plus an installed
// locality index, returning both. The tests drive the index's seams (clock,
// dumper) and assert on its published map and negative cache directly, while
// entering through x.refreshLocality — the untagged dispatcher the reconcile
// path actually calls.
func newLocalityFixture(t *testing.T, interval time.Duration) (*XTCP, *localityIndex) {
	t.Helper()
	x := newMetricsFixture(t, interval)
	li := newLocalityIndex(x)
	x.locality = li
	return x, li
}

// ---- refreshLocality lifecycle ------------------------------------------------
//
// refreshLocality is driven with an injected dumper and clock, so every rule —
// full vs partial pass, keep-last-good, negative cache + backoff, loopback-only
// re-dump, the per-pass cap, vanished-namespace cleanup — is asserted without
// setns or a kernel.

// Snapshot fixtures: a "real" namespace (non-loopback self address) and a
// freshly-created one that only has lo.
var (
	realSnap = localnet.BuildSnapshot([]xtcpnl.AddrInfo{{Family: unix.AF_INET, Index: 2, Local: []byte{10, 0, 0, 1}}}, nil, map[uint32]string{2: "eth0"})
	loSnap   = localnet.BuildSnapshot([]xtcpnl.AddrInfo{{Family: unix.AF_INET, Index: 1, Local: []byte{127, 0, 0, 1}}}, nil, map[uint32]string{1: "lo"})
)

// snapKind names what a namespace's published snapshot should look like.
type snapKind int

const (
	snapNone snapKind = iota // no snapshot published for the inode
	snapReal                 // realSnap
	snapLo                   // loSnap
)

// dumpOutcome scripts the dumper's reply for one inode on one pass.
type dumpOutcome int

const (
	dumpReal dumpOutcome = iota // success, non-loopback self
	dumpLo                      // success, loopback only
	dumpFail                    // hard failure (open/setns/dump error)
)

// pass is one reconcile: the clock offset it runs at, the namespaces present,
// how the dumper answers, and what must be true afterwards.
type pass struct {
	description string
	at          time.Duration          // clock offset from t0
	nss         []uint64               // namespace inodes present this reconcile
	outcomes    map[uint64]dumpOutcome // dumper reply per inode; missing = dumpReal
	wantDumped  []uint64               // exact set of inodes the dumper is asked for (nil = use wantDumpCount)
	wantDumps   int                    // number of dumper calls when the set is not deterministic (cap tests)
	wantSnaps   map[uint64]snapKind    // expected published snapshot for the listed inodes
	wantSnapLen int                    // expected len of the published map; checked when > 0 or when wantSnaps is all snapNone
	wantRetry   map[uint64]time.Duration
}

func seq(from, to uint64) []uint64 {
	out := make([]uint64, 0, to-from+1)
	for i := from; i <= to; i++ {
		out = append(out, i)
	}
	return out
}

// go test -ldflags=-checklinkname=0 ./pkg/xtcp/ -run TestRefreshLocalityLifecycle
func TestRefreshLocalityLifecycle(t *testing.T) {
	const refresh = 60 * time.Second

	tests := []struct {
		description string
		interval    time.Duration
		passes      []pass
	}{
		// positive
		{
			description: "first pass is full: every namespace dumped and published",
			interval:    refresh,
			passes: []pass{
				{description: "p1", at: 0, nss: []uint64{1, 2}, wantDumped: []uint64{1, 2},
					wantSnaps: map[uint64]snapKind{1: snapReal, 2: snapReal}},
			},
		},
		{
			description: "partial pass reuses existing snapshots and dumps only the new namespace",
			interval:    refresh,
			passes: []pass{
				{description: "p1 full", at: 0, nss: []uint64{1, 2}, wantDumped: []uint64{1, 2},
					wantSnaps: map[uint64]snapKind{1: snapReal, 2: snapReal}},
				{description: "p2 +10s partial, ns 3 appears", at: 10 * time.Second, nss: []uint64{1, 2, 3}, wantDumped: []uint64{3},
					wantSnaps: map[uint64]snapKind{1: snapReal, 2: snapReal, 3: snapReal}},
			},
		},
		{
			description: "full pass after the refresh interval re-dumps everything",
			interval:    refresh,
			passes: []pass{
				{description: "p1 full", at: 0, nss: []uint64{1, 2}, wantDumped: []uint64{1, 2}},
				{description: "p2 +59s still partial", at: 59 * time.Second, nss: []uint64{1, 2}, wantDumped: []uint64{}},
				{description: "p3 +60s full", at: 60 * time.Second, nss: []uint64{1, 2}, wantDumped: []uint64{1, 2}},
			},
		},
		{
			description: "namespace that disappears is dropped from the published map",
			interval:    refresh,
			passes: []pass{
				{description: "p1", at: 0, nss: []uint64{1, 2}, wantDumped: []uint64{1, 2}},
				{description: "p2 ns 2 gone", at: 5 * time.Second, nss: []uint64{1}, wantDumped: []uint64{},
					wantSnaps: map[uint64]snapKind{1: snapReal}, wantSnapLen: 1},
			},
		},

		// negative — hard failures and the negative cache
		{
			description: "failed dump keeps the last good snapshot and enters a 30s backoff",
			interval:    refresh,
			passes: []pass{
				{description: "p1 ok", at: 0, nss: []uint64{1}, wantDumped: []uint64{1}, wantSnaps: map[uint64]snapKind{1: snapReal}},
				{description: "p2 +60s full, dump fails -> keep, backoff 30s", at: 60 * time.Second, nss: []uint64{1},
					outcomes: map[uint64]dumpOutcome{1: dumpFail}, wantDumped: []uint64{1},
					wantSnaps: map[uint64]snapKind{1: snapReal}, wantRetry: map[uint64]time.Duration{1: 30 * time.Second}},
				{description: "p3 +70s inside backoff -> not dumped, still served", at: 70 * time.Second, nss: []uint64{1},
					wantDumped: []uint64{}, wantSnaps: map[uint64]snapKind{1: snapReal}, wantRetry: map[uint64]time.Duration{1: 30 * time.Second}},
				{description: "p4 +90s window open -> dumped ok, retry cleared", at: 90 * time.Second, nss: []uint64{1},
					wantDumped: []uint64{1}, wantSnaps: map[uint64]snapKind{1: snapReal}, wantRetry: map[uint64]time.Duration{}},
			},
		},
		{
			description: "brand-new namespace whose first dump fails has no snapshot and is negative-cached",
			interval:    refresh,
			passes: []pass{
				{description: "p1 fail", at: 0, nss: []uint64{1}, outcomes: map[uint64]dumpOutcome{1: dumpFail},
					wantDumped: []uint64{1}, wantSnaps: map[uint64]snapKind{1: snapNone}, wantSnapLen: 0,
					wantRetry: map[uint64]time.Duration{1: 30 * time.Second}},
			},
		},
		{
			description: "repeated failures double the backoff 30s,60s,120s,240s and cap at 5m",
			interval:    refresh,
			passes: []pass{
				{description: "fail#1 @0 -> 30s", at: 0, nss: []uint64{1}, outcomes: map[uint64]dumpOutcome{1: dumpFail},
					wantDumped: []uint64{1}, wantRetry: map[uint64]time.Duration{1: 30 * time.Second}},
				{description: "fail#2 @30s -> 60s", at: 30 * time.Second, nss: []uint64{1}, outcomes: map[uint64]dumpOutcome{1: dumpFail},
					wantDumped: []uint64{1}, wantRetry: map[uint64]time.Duration{1: 60 * time.Second}},
				{description: "fail#3 @90s -> 120s", at: 90 * time.Second, nss: []uint64{1}, outcomes: map[uint64]dumpOutcome{1: dumpFail},
					wantDumped: []uint64{1}, wantRetry: map[uint64]time.Duration{1: 120 * time.Second}},
				{description: "fail#4 @210s -> 240s", at: 210 * time.Second, nss: []uint64{1}, outcomes: map[uint64]dumpOutcome{1: dumpFail},
					wantDumped: []uint64{1}, wantRetry: map[uint64]time.Duration{1: 240 * time.Second}},
				{description: "fail#5 @450s -> 300s cap", at: 450 * time.Second, nss: []uint64{1}, outcomes: map[uint64]dumpOutcome{1: dumpFail},
					wantDumped: []uint64{1}, wantRetry: map[uint64]time.Duration{1: 5 * time.Minute}},
				{description: "fail#6 @750s -> stays 300s", at: 750 * time.Second, nss: []uint64{1}, outcomes: map[uint64]dumpOutcome{1: dumpFail},
					wantDumped: []uint64{1}, wantRetry: map[uint64]time.Duration{1: 5 * time.Minute}},
			},
		},
		{
			description: "a namespace inside its backoff is skipped even by a full pass",
			interval:    10 * time.Second,
			passes: []pass{
				{description: "p1 fail -> 30s backoff", at: 0, nss: []uint64{1, 2}, outcomes: map[uint64]dumpOutcome{1: dumpFail},
					wantDumped: []uint64{1, 2}, wantRetry: map[uint64]time.Duration{1: 30 * time.Second}},
				{description: "p2 +10s full: ns 2 re-dumped, ns 1 skipped", at: 10 * time.Second, nss: []uint64{1, 2},
					wantDumped: []uint64{2}, wantRetry: map[uint64]time.Duration{1: 30 * time.Second}},
				{description: "p3 +30s full: ns 1 window open -> both dumped", at: 30 * time.Second, nss: []uint64{1, 2},
					wantDumped: []uint64{1, 2}, wantRetry: map[uint64]time.Duration{}},
			},
		},
		{
			description: "retry state of a vanished namespace is dropped",
			interval:    refresh,
			passes: []pass{
				{description: "p1 ns 2 fails", at: 0, nss: []uint64{1, 2}, outcomes: map[uint64]dumpOutcome{2: dumpFail},
					wantDumped: []uint64{1, 2}, wantRetry: map[uint64]time.Duration{2: 30 * time.Second}},
				{description: "p2 ns 2 gone", at: 5 * time.Second, nss: []uint64{1}, wantDumped: []uint64{},
					wantSnaps: map[uint64]snapKind{1: snapReal}, wantSnapLen: 1, wantRetry: map[uint64]time.Duration{}},
			},
		},

		// corner — loopback-only namespaces
		{
			description: "loopback-only snapshot is published, re-dumped next pass, then backs off",
			interval:    refresh,
			passes: []pass{
				{description: "p1 lo-only -> published, retry next pass", at: 0, nss: []uint64{1}, outcomes: map[uint64]dumpOutcome{1: dumpLo},
					wantDumped: []uint64{1}, wantSnaps: map[uint64]snapKind{1: snapLo}, wantRetry: map[uint64]time.Duration{1: 0}},
				{description: "p2 +5s still lo -> re-dumped, now 30s backoff", at: 5 * time.Second, nss: []uint64{1}, outcomes: map[uint64]dumpOutcome{1: dumpLo},
					wantDumped: []uint64{1}, wantSnaps: map[uint64]snapKind{1: snapLo}, wantRetry: map[uint64]time.Duration{1: 30 * time.Second}},
				{description: "p3 +10s inside backoff -> not dumped", at: 10 * time.Second, nss: []uint64{1},
					wantDumped: []uint64{}, wantSnaps: map[uint64]snapKind{1: snapLo}, wantRetry: map[uint64]time.Duration{1: 30 * time.Second}},
				{description: "p4 +40s veth plumbed -> real snapshot, retry cleared", at: 40 * time.Second, nss: []uint64{1},
					wantDumped: []uint64{1}, wantSnaps: map[uint64]snapKind{1: snapReal}, wantRetry: map[uint64]time.Duration{}},
			},
		},
		{
			description: "loopback-only then hard failure: failure keeps the lo snapshot and moves to 30s",
			interval:    refresh,
			passes: []pass{
				{description: "p1 lo", at: 0, nss: []uint64{1}, outcomes: map[uint64]dumpOutcome{1: dumpLo},
					wantDumped: []uint64{1}, wantSnaps: map[uint64]snapKind{1: snapLo}, wantRetry: map[uint64]time.Duration{1: 0}},
				{description: "p2 fail", at: time.Second, nss: []uint64{1}, outcomes: map[uint64]dumpOutcome{1: dumpFail},
					wantDumped: []uint64{1}, wantSnaps: map[uint64]snapKind{1: snapLo}, wantRetry: map[uint64]time.Duration{1: 30 * time.Second}},
			},
		},

		// boundary — per-pass cap
		{
			description: "partial pass dumps at most 32 new namespaces, defers the rest to the next pass",
			interval:    refresh,
			passes: []pass{
				{description: "p1 full, 1 ns", at: 0, nss: []uint64{1}, wantDumped: []uint64{1}},
				{description: "p2 +5s: 40 new -> 32 dumped, 8 deferred", at: 5 * time.Second, nss: seq(1, 41), wantDumps: 32, wantSnapLen: 33},
				{description: "p3 +10s: remaining 8 dumped", at: 10 * time.Second, nss: seq(1, 41), wantDumps: 8, wantSnapLen: 41},
				{description: "p4 +15s: nothing left", at: 15 * time.Second, nss: seq(1, 41), wantDumps: 0, wantSnapLen: 41},
			},
		},
		{
			description: "exactly 32 new namespaces fit in one partial pass",
			interval:    refresh,
			passes: []pass{
				{description: "p1", at: 0, nss: []uint64{1}, wantDumped: []uint64{1}},
				{description: "p2 32 new", at: 5 * time.Second, nss: seq(1, 33), wantDumps: 32, wantSnapLen: 33},
			},
		},
		{
			description: "full pass is uncapped",
			interval:    refresh,
			passes: []pass{
				{description: "p1 full with 100 namespaces", at: 0, nss: seq(1, 100), wantDumps: 100, wantSnapLen: 100},
			},
		},
		{
			description: "expired retries count against the partial-pass cap too",
			interval:    refresh,
			passes: []pass{
				{description: "p1 full: 40 ns all fail", at: 0, nss: seq(1, 40), outcomes: allFail(seq(1, 40)), wantDumps: 40,
					wantSnaps: map[uint64]snapKind{1: snapNone, 40: snapNone}, wantSnapLen: 0},
				{description: "p2 +30s: windows open -> 32 retried, 8 deferred", at: 30 * time.Second, nss: seq(1, 40), wantDumps: 32, wantSnapLen: 32},
			},
		},

		// corner — interval 0
		{
			description: "interval 0: never another full pass, new namespaces and retries still handled",
			interval:    0,
			passes: []pass{
				{description: "p1 full (first ever), ns 1 fails", at: 0, nss: []uint64{1, 2}, outcomes: map[uint64]dumpOutcome{1: dumpFail},
					wantDumped: []uint64{1, 2}, wantRetry: map[uint64]time.Duration{1: 30 * time.Second}},
				{description: "p2 +1h: ns 1 retry open, ns 2 NOT refreshed, ns 3 new", at: time.Hour, nss: []uint64{1, 2, 3},
					wantDumped: []uint64{1, 3}, wantSnaps: map[uint64]snapKind{1: snapReal, 2: snapReal, 3: snapReal}, wantRetry: map[uint64]time.Duration{}},
				{description: "p3 +2h: nothing to do", at: 2 * time.Hour, nss: []uint64{1, 2, 3}, wantDumped: []uint64{}},
			},
		},
	}

	for _, tc := range tests {
		t.Run(tc.description, func(t *testing.T) {
			x, li := newLocalityFixture(t, tc.interval)
			t0 := time.Date(2026, 9, 22, 12, 0, 0, 0, time.UTC)
			now := t0
			li.clock = func() time.Time { return now }

			var asked []uint64
			var outcomes map[uint64]dumpOutcome
			li.dumper = func(id nsIdentity) (*localnet.Snapshot, bool) {
				asked = append(asked, id.inode)
				switch outcomes[id.inode] {
				case dumpFail:
					return nil, false
				case dumpLo:
					return loSnap, true
				default:
					return realSnap, true
				}
			}

			for _, p := range tc.passes {
				now = t0.Add(p.at)
				asked = asked[:0]
				outcomes = p.outcomes

				nss := make(map[uint64]nsIdentity, len(p.nss))
				for _, in := range p.nss {
					nss[in] = nsIdentity{inode: in, pid: int(in) + 1000}
				}

				// Retry entries untouched by this pass must keep their deadline.
				prevRetry := make(map[uint64]localityRetryState, len(li.retry))
				for k, v := range li.retry {
					prevRetry[k] = v
				}

				x.refreshLocality(nss)

				// dumper calls
				if p.wantDumped != nil {
					got := append([]uint64(nil), asked...)
					want := append([]uint64(nil), p.wantDumped...)
					sort.Slice(got, func(i, j int) bool { return got[i] < got[j] })
					sort.Slice(want, func(i, j int) bool { return want[i] < want[j] })
					if !equalU64(got, want) {
						t.Errorf("%s: dumped %v, want %v", p.description, got, want)
					}
				} else if len(asked) != p.wantDumps {
					t.Errorf("%s: dumper called %d times, want %d", p.description, len(asked), p.wantDumps)
				}
				if len(uniqueU64(asked)) != len(asked) {
					t.Errorf("%s: a namespace was dumped twice in one pass: %v", p.description, asked)
				}

				// published map
				pub := li.byInode.Load()
				if pub == nil {
					t.Fatalf("%s: no snapshot map published", p.description)
				}
				m := *pub
				for inode, kind := range p.wantSnaps {
					got := snapNone
					switch m[inode] {
					case realSnap:
						got = snapReal
					case loSnap:
						got = snapLo
					}
					if got != kind {
						t.Errorf("%s: snapshot[%d] = %v, want %v", p.description, inode, got, kind)
					}
				}
				// Length is asserted when the pass states a positive count, or
				// when every listed expectation is snapNone (an explicit "empty").
				if p.wantSnapLen > 0 || (len(p.wantSnaps) > 0 && allNone(p.wantSnaps)) {
					if len(m) != p.wantSnapLen {
						t.Errorf("%s: published %d snapshots, want %d", p.description, len(m), p.wantSnapLen)
					}
				}
				for inode := range m {
					if _, present := nss[inode]; !present {
						t.Errorf("%s: published snapshot for vanished namespace %d", p.description, inode)
					}
				}

				// retry map
				if p.wantRetry != nil {
					if len(li.retry) != len(p.wantRetry) {
						t.Errorf("%s: retry map has %d entries %v, want %d %v", p.description, len(li.retry), li.retry, len(p.wantRetry), p.wantRetry)
					}
					for inode, backoff := range p.wantRetry {
						st, ok := li.retry[inode]
						if !ok {
							t.Errorf("%s: retry[%d] missing, want backoff %s", p.description, inode, backoff)
							continue
						}
						wantNext := now.Add(backoff) // (re)set by a dump on this pass
						if _, dumpedNow := uniqueU64(asked)[inode]; !dumpedNow {
							wantNext = prevRetry[inode].nextRetry // skipped: deadline unchanged
						}
						if st.backoff != backoff || !st.nextRetry.Equal(wantNext) {
							t.Errorf("%s: retry[%d] = {next:%s backoff:%s}, want {next:%s backoff:%s}",
								p.description, inode, st.nextRetry.Format(time.TimeOnly), st.backoff, wantNext.Format(time.TimeOnly), backoff)
						}
					}
				}
				for inode := range li.retry {
					if _, present := nss[inode]; !present {
						t.Errorf("%s: retry state kept for vanished namespace %d", p.description, inode)
					}
				}

				// gauges track the published map and the retry set
				if g := testutil.ToFloat64(x.pGV.WithLabelValues("refreshLocality", "namespaces", "gauge")); int(g) != len(m) {
					t.Errorf("%s: namespaces gauge = %v, want %d", p.description, g, len(m))
				}
				if g := testutil.ToFloat64(x.pGV.WithLabelValues("refreshLocality", "retryBackoff", "gauge")); int(g) != len(li.retry) {
					t.Errorf("%s: retryBackoff gauge = %v, want %d", p.description, g, len(li.retry))
				}
			}
		})
	}
}

func allFail(inodes []uint64) map[uint64]dumpOutcome {
	m := make(map[uint64]dumpOutcome, len(inodes))
	for _, in := range inodes {
		m[in] = dumpFail
	}
	return m
}

func allNone(m map[uint64]snapKind) bool {
	for _, k := range m {
		if k != snapNone {
			return false
		}
	}
	return true
}

func equalU64(a, b []uint64) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

func uniqueU64(a []uint64) map[uint64]struct{} {
	m := make(map[uint64]struct{}, len(a))
	for _, v := range a {
		m[v] = struct{}{}
	}
	return m
}

// TestRefreshLocalityDeferredCounter checks the deferred / dumped counters and
// that a partial pass's duration is observed, on the cap scenario.
//
// go test -ldflags=-checklinkname=0 ./pkg/xtcp/ -run TestRefreshLocalityDeferredCounter
func TestRefreshLocalityDeferredCounter(t *testing.T) {
	tests := []struct {
		description  string
		firstNs      []uint64 // namespaces on the (full) first pass
		secondNs     []uint64 // namespaces on the partial second pass
		wantDumped   float64  // total dumper calls over both passes
		wantDeferred float64  // deferred on the second pass
	}{
		{"40 new on a partial pass -> 32 dumped, 8 deferred", []uint64{1}, seq(1, 41), 1 + 32, 8},
		{"32 new fit exactly -> nothing deferred", []uint64{1}, seq(1, 33), 1 + 32, 0},
		{"no new namespaces -> nothing dumped or deferred", []uint64{1, 2}, []uint64{1, 2}, 2, 0},
		{"33 new -> exactly one deferred", []uint64{1}, seq(1, 34), 1 + 32, 1},
	}
	for _, tc := range tests {
		t.Run(tc.description, func(t *testing.T) {
			x, li := newLocalityFixture(t, time.Minute)
			now := time.Date(2026, 9, 22, 12, 0, 0, 0, time.UTC)
			li.clock = func() time.Time { return now }
			li.dumper = func(nsIdentity) (*localnet.Snapshot, bool) { return realSnap, true }

			mk := func(in []uint64) map[uint64]nsIdentity {
				m := make(map[uint64]nsIdentity, len(in))
				for _, i := range in {
					m[i] = nsIdentity{inode: i}
				}
				return m
			}
			x.refreshLocality(mk(tc.firstNs))
			now = now.Add(5 * time.Second)
			x.refreshLocality(mk(tc.secondNs))

			if got := testutil.ToFloat64(x.pC.WithLabelValues("refreshLocality", "dumped", "count")); got != tc.wantDumped {
				t.Errorf("dumped counter = %v, want %v", got, tc.wantDumped)
			}
			if got := testutil.ToFloat64(x.pC.WithLabelValues("refreshLocality", "deferred", "count")); got != tc.wantDeferred {
				t.Errorf("deferred counter = %v, want %v", got, tc.wantDeferred)
			}
			if got := testutil.ToFloat64(x.pC.WithLabelValues("refreshLocality", "full", "count")); got != 1 {
				t.Errorf("full-pass counter = %v, want 1", got)
			}
			if got := testutil.ToFloat64(x.pC.WithLabelValues("refreshLocality", "partial", "count")); got != 1 {
				t.Errorf("partial-pass counter = %v, want 1", got)
			}
		})
	}
}

// go test -ldflags=-checklinkname=0 ./pkg/xtcp/ -run TestNextLocalityRetry
func TestNextLocalityRetry(t *testing.T) {
	now := time.Date(2026, 9, 22, 12, 0, 0, 0, time.UTC)
	tests := []struct {
		description string
		prev        localityRetryState
		retrying    bool
		hard        bool
		wantBackoff time.Duration
	}{
		// positive
		{"no history, hard failure -> 30s", localityRetryState{}, false, true, 30 * time.Second},
		{"no history, loopback-only -> 0 (next pass)", localityRetryState{}, false, false, 0},
		// boundary
		{"after a next-pass retry, loopback again -> 30s", localityRetryState{backoff: 0}, true, false, 30 * time.Second},
		{"after a next-pass retry, hard failure -> 30s", localityRetryState{backoff: 0}, true, true, 30 * time.Second},
		{"30s -> 60s", localityRetryState{backoff: 30 * time.Second}, true, true, 60 * time.Second},
		{"120s -> 240s", localityRetryState{backoff: 120 * time.Second}, true, false, 240 * time.Second},
		{"240s -> capped 300s", localityRetryState{backoff: 240 * time.Second}, true, true, 5 * time.Minute},
		{"300s stays 300s", localityRetryState{backoff: 5 * time.Minute}, true, true, 5 * time.Minute},
		// corner — a stale entry with a backoff above the cap is pulled back to it
		{"above-cap backoff is clamped", localityRetryState{backoff: time.Hour}, true, true, 5 * time.Minute},
	}
	for _, tc := range tests {
		t.Run(tc.description, func(t *testing.T) {
			got := nextLocalityRetry(tc.prev, tc.retrying, now, tc.hard)
			if got.backoff != tc.wantBackoff {
				t.Errorf("backoff = %s, want %s", got.backoff, tc.wantBackoff)
			}
			if !got.nextRetry.Equal(now.Add(tc.wantBackoff)) {
				t.Errorf("nextRetry = %s, want %s", got.nextRetry, now.Add(tc.wantBackoff))
			}
		})
	}
}

// ---- dumpRetrying over a socketpair -------------------------------------------

// nlmsgT lays out one netlink message for the fake kernel.
func nlmsgT(typ, flags uint16, seq uint32, body []byte) []byte {
	b := make([]byte, xtcpnl.NlMsgHdrSizeCst+len(body))
	binary.LittleEndian.PutUint32(b[0:4], uint32(len(b)))
	binary.LittleEndian.PutUint16(b[4:6], typ)
	binary.LittleEndian.PutUint16(b[6:8], flags)
	binary.LittleEndian.PutUint32(b[8:12], seq)
	copy(b[xtcpnl.NlMsgHdrSizeCst:], body)
	return b
}

// reply kinds the fake kernel can produce for one dump attempt.
type attemptReply int

const (
	replyClean       attemptReply = iota // one RTM_NEWLINK + DONE
	replyInterrupted                     // one RTM_NEWLINK flagged DUMP_INTR + DONE
	replyENOENT                          // NLMSG_ERROR -ENOENT
)

// go test -ldflags=-checklinkname=0 ./pkg/xtcp/ -run TestDumpRetrying
func TestDumpRetrying(t *testing.T) {
	tests := []struct {
		description  string
		replies      []attemptReply // per attempt, in order
		wantAttempts int            // reset() calls == requests received
		wantErr      error          // errors.Is target, nil for success
		wantMsgs     int            // messages delivered to onMsg on the FINAL attempt
	}{
		// positive
		{"clean first attempt -> 1 attempt, 1 message", []attemptReply{replyClean}, 1, nil, 1},
		{"interrupted once, then clean -> 2 attempts, only the clean one delivered", []attemptReply{replyInterrupted, replyClean}, 2, nil, 1},
		{"interrupted three times, fourth clean -> 4 attempts, success", []attemptReply{replyInterrupted, replyInterrupted, replyInterrupted, replyClean}, 4, nil, 1},
		// negative / boundary
		{"interrupted four times -> retries exhausted, ErrDumpInterrupted", []attemptReply{replyInterrupted, replyInterrupted, replyInterrupted, replyInterrupted}, 4, xtcpnl.ErrDumpInterrupted, 0},
		{"hard error is not retried", []attemptReply{replyENOENT}, 1, syscall.ENOENT, 0},
		{"interrupted then hard error -> stops at the error", []attemptReply{replyInterrupted, replyENOENT}, 2, syscall.ENOENT, 0},
	}

	for _, tc := range tests {
		t.Run(tc.description, func(t *testing.T) {
			x := newMetricsFixture(t, time.Minute)

			fds, err := unix.Socketpair(unix.AF_UNIX, unix.SOCK_SEQPACKET|unix.SOCK_CLOEXEC, 0)
			if err != nil {
				t.Fatalf("Socketpair: %v", err)
			}
			t.Cleanup(func() { _ = unix.Close(fds[0]); _ = unix.Close(fds[1]) })
			tv := unix.Timeval{Sec: 2}
			if err := unix.SetsockoptTimeval(fds[0], unix.SOL_SOCKET, unix.SO_RCVTIMEO, &tv); err != nil {
				t.Fatalf("SO_RCVTIMEO: %v", err)
			}

			// Fake kernel: for each scripted attempt, read the request, echo its
			// seq into the reply so the seq filter accepts it.
			seqs := make(chan uint32, len(tc.replies))
			kernelDone := make(chan struct{})
			go func() {
				defer close(kernelDone)
				rb := make([]byte, 256)
				for _, r := range tc.replies {
					n, _, rerr := unix.Recvfrom(fds[1], rb, 0)
					if rerr != nil || n < xtcpnl.NlMsgHdrSizeCst {
						return
					}
					s := binary.LittleEndian.Uint32(rb[8:12])
					seqs <- s
					var out []byte
					switch r {
					case replyClean:
						out = append(out, nlmsgT(uint16(unix.RTM_NEWLINK), unix.NLM_F_MULTI, s, make([]byte, xtcpnl.IfInfomsgSizeCst))...)
						out = append(out, nlmsgT(uint16(unix.NLMSG_DONE), unix.NLM_F_MULTI, s, make([]byte, 4))...)
					case replyInterrupted:
						out = append(out, nlmsgT(uint16(unix.RTM_NEWLINK), unix.NLM_F_MULTI|unix.NLM_F_DUMP_INTR, s, make([]byte, xtcpnl.IfInfomsgSizeCst))...)
						out = append(out, nlmsgT(uint16(unix.NLMSG_DONE), unix.NLM_F_MULTI, s, make([]byte, 4))...)
					case replyENOENT:
						body := make([]byte, 4+xtcpnl.NlMsgHdrSizeCst)
						errno := int32(syscall.ENOENT)
						binary.LittleEndian.PutUint32(body[0:4], uint32(-errno))
						out = nlmsgT(uint16(unix.NLMSG_ERROR), 0, s, body)
					}
					if _, werr := unix.Write(fds[1], out); werr != nil {
						return
					}
				}
			}()

			var seq uint32
			resets, msgs := 0, 0
			err = x.dumpRetrying(fds[0], nil, &seq, xtcpnl.BuildDumpLinkRequest,
				func() { resets++; msgs = 0 },
				func(mt uint16, _ []byte) error {
					if mt == uint16(unix.RTM_NEWLINK) {
						msgs++
					}
					return nil
				})

			if tc.wantErr == nil && err != nil {
				t.Errorf("err = %v, want nil", err)
			}
			if tc.wantErr != nil && !errors.Is(err, tc.wantErr) {
				t.Errorf("err = %v, want errors.Is(%v)", err, tc.wantErr)
			}
			if resets != tc.wantAttempts {
				t.Errorf("reset called %d times, want %d", resets, tc.wantAttempts)
			}
			if int(seq) != tc.wantAttempts {
				t.Errorf("seq advanced to %d, want %d (one per attempt)", seq, tc.wantAttempts)
			}
			if msgs != tc.wantMsgs {
				t.Errorf("final attempt delivered %d messages, want %d", msgs, tc.wantMsgs)
			}
			// Every request carried a distinct, increasing seq. Wait for the fake
			// kernel to finish so closing seqs is ordered after its last send.
			select {
			case <-kernelDone:
			case <-time.After(2 * time.Second):
				t.Fatal("fake kernel did not finish")
			}
			close(seqs)
			var prev uint32
			n := 0
			for s := range seqs {
				n++
				if s <= prev {
					t.Errorf("request seq %d not greater than previous %d", s, prev)
				}
				prev = s
			}
			if n != tc.wantAttempts {
				t.Errorf("fake kernel saw %d requests, want %d", n, tc.wantAttempts)
			}
			if tc.wantErr == nil || errors.Is(err, xtcpnl.ErrDumpInterrupted) {
				retries := testutil.ToFloat64(x.pC.WithLabelValues("dumpLocality", "interrupted", "retry"))
				if wantRetries := float64(tc.wantAttempts - 1); retries != wantRetries {
					t.Errorf("interrupted-retry counter = %v, want %v", retries, wantRetries)
				}
			}
		})
	}
}
