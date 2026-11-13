package xtcp

import (
	"encoding/binary"
	"math/rand/v2"
	"testing"

	"github.com/randomizedcoder/xtcp2/pkg/xtcpnl"
)

func known(inuse, tw uint64) sockstatCount { return sockstatCount{inuse: inuse, tw: tw, known: true} }

func repeatCount(c sockstatCount, n int) []sockstatCount {
	out := make([]sockstatCount, n)
	for i := range out {
		out[i] = c
	}
	return out
}

// TestFairShare covers the max-min water-filling.
func TestFairShare(t *testing.T) {
	tests := []struct {
		name            string
		category        string
		description     string
		sorted          []uint64
		budget          uint64
		expectedShare   uint64
		expectedLimited bool
	}{
		{name: "all_fit", category: "P", description: "total under budget", sorted: []uint64{1, 2, 3}, budget: 10, expectedShare: 3},
		{name: "one_heavy", category: "P", description: "small ones met, heavy one gets the rest", sorted: []uint64{10, 10, 500}, budget: 100, expectedShare: 80, expectedLimited: true},
		{name: "exact", category: "B", description: "total equals budget", sorted: []uint64{50, 50}, budget: 100, expectedShare: 50},
		{name: "plus_one", category: "B", description: "total is budget+1", sorted: []uint64{100, 100, 101}, budget: 300, expectedShare: 100, expectedLimited: true},
		{name: "empty", category: "B", description: "no namespaces", budget: 5, expectedShare: 5},
		{name: "all_equal_heavy", category: "C", description: "4096 x 1000 over 1M", sorted: func() []uint64 {
			s := make([]uint64, 4096)
			for i := range s {
				s[i] = 1000
			}
			return s
		}(), budget: 1_000_000, expectedShare: 244, expectedLimited: true},
		{name: "budget_under_n", category: "C", description: "budget smaller than the namespace count", sorted: []uint64{5, 5, 5}, budget: 2, expectedShare: 0, expectedLimited: true},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			s, l := fairShare(tc.sorted, tc.budget)
			if s != tc.expectedShare || l != tc.expectedLimited {
				t.Errorf("%s: (%d, %v), expected (%d, %v)", tc.description, s, l, tc.expectedShare, tc.expectedLimited)
			}
		})
	}
}

// TestPlanPoll simulates the kernel reporting large counts: the counts are
// inputs, so 500k sockets cost nothing to test.
func TestPlanPoll(t *testing.T) {
	small := repeatCount(known(1000, 0), 10)
	tests := []struct {
		name        string
		category    string
		description string
		counts      []sockstatCount
		budget      uint64
		unknownAs   uint64
		loop        uint64
		// expected
		levels  map[int]pollLevel // index → level; others pollFull
		shards  map[int]uint32
		maxSum  uint64 // planned total must not exceed (0: unchecked)
		unknown int
	}{
		{name: "under_budget", category: "P", description: "10 x 1k, budget 200k: all full",
			counts: small, budget: 200_000, maxSum: 10_000},
		{name: "budget_zero", category: "P", description: "gate off: all full even at 500k",
			counts: []sockstatCount{known(500_000, 0)}, budget: 0},
		{name: "one_heavy_500k", category: "N", description: "500k + ten at 1k, budget 200k: heavy one sampled, total within budget",
			counts: append([]sockstatCount{known(500_000, 0)}, small...), budget: 200_000,
			levels: map[int]pollLevel{0: pollSample}, shards: map[int]uint32{0: 3}, maxSum: 200_000},
		{name: "tw_heavy", category: "N", description: "inuse 50k, tw 450k: drop states only",
			counts: append([]sockstatCount{known(50_000, 450_000)}, small...), budget: 200_000,
			levels: map[int]pollLevel{0: pollStates}, maxSum: 200_000},
		{name: "two_heavy", category: "N", description: "500k + 400k, budget 200k: both sampled with equal shares",
			counts: []sockstatCount{known(500_000, 0), known(400_000, 0)}, budget: 200_000,
			levels: map[int]pollLevel{0: pollSample, 1: pollSample}, shards: map[int]uint32{0: 5, 1: 4}, maxSum: 200_000},
		{name: "exact_budget", category: "B", description: "total equals the budget: all full",
			counts: []sockstatCount{known(100_000, 0), known(100_000, 0)}, budget: 200_000, maxSum: 200_000},
		{name: "budget_plus_one", category: "B", description: "total budget+1: one level drop, on the largest",
			counts: []sockstatCount{known(100, 0), known(100, 0), known(100, 1)}, budget: 300,
			levels: map[int]pollLevel{2: pollStates}, maxSum: 300},
		{name: "all_equal_heavy", category: "C", description: "4096 x 1k, budget 1M: share 244, all sampled K=5",
			counts: repeatCount(known(1000, 0), 4096), budget: 1_000_000, maxSum: 1_000_000},
		{name: "unknown_count", category: "C", description: "an unreadable sockstat is taken as the per-netns cap",
			counts: []sockstatCount{{}, known(10, 0)}, budget: 100_000, unknownAs: 262_144,
			levels: map[int]pollLevel{0: pollSample}, shards: map[int]uint32{0: 3}, unknown: 1},
		{name: "unknown_no_cap", category: "C", description: "unknown with the per-netns cap off is taken as the budget",
			counts: []sockstatCount{{}}, budget: 1000, unknown: 1},
		{name: "huge_vs_tiny_share", category: "C", description: "K is capped at maxShardsCst",
			counts: []sockstatCount{known(50_000_000, 0), known(1, 0)}, budget: 1000,
			levels: map[int]pollLevel{0: pollSample}, shards: map[int]uint32{0: maxShardsCst}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			var p pollPlanner
			plans := p.plan(tc.counts, tc.budget, tc.unknownAs, tc.loop)
			if len(plans) != len(tc.counts) {
				t.Fatalf("%s: %d plans for %d namespaces", tc.description, len(plans), len(tc.counts))
			}
			var sum uint64
			unknown := 0
			for i, pl := range plans {
				want := tc.levels[i]
				if tc.name == "all_equal_heavy" {
					want = pollSample
				}
				if pl.level != want {
					t.Errorf("%s: ns %d level %s, expected %s", tc.description, i, pl.level, want)
				}
				if k, ok := tc.shards[i]; ok && pl.shards != k {
					t.Errorf("%s: ns %d K=%d, expected %d", tc.description, i, pl.shards, k)
				}
				if tc.name == "all_equal_heavy" && pl.shards != 5 {
					t.Errorf("%s: ns %d K=%d, expected 5", tc.description, i, pl.shards)
				}
				if pl.level == pollSample && pl.shard >= pl.shards {
					t.Errorf("%s: ns %d shard %d >= K %d", tc.description, i, pl.shard, pl.shards)
				}
				if pl.unknown {
					unknown++
				}
				sum += pl.planned
			}
			if tc.maxSum > 0 && sum > tc.maxSum {
				t.Errorf("%s: planned %d, expected <= %d", tc.description, sum, tc.maxSum)
			}
			if unknown != tc.unknown {
				t.Errorf("%s: %d unknown, expected %d", tc.description, unknown, tc.unknown)
			}
		})
	}
}

// TestPlanPollShardRotation: shard index follows pollingLoops mod K and
// stays below K when K changes.
func TestPlanPollShardRotation(t *testing.T) {
	tests := []struct {
		name        string
		category    string
		description string
		inuse       uint64
		loops       []uint64
		expected    []uint32
		expectedK   uint32
	}{
		{name: "k3", category: "C", description: "K=3 over polls 0..5", inuse: 300, loops: []uint64{0, 1, 2, 3, 4, 5}, expected: []uint32{0, 1, 2, 0, 1, 2}, expectedK: 3},
		{name: "k_changes", category: "C", description: "K drops 4 -> 3 at poll 7: index stays < K", inuse: 300, loops: []uint64{7}, expected: []uint32{1}, expectedK: 3},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			var p pollPlanner
			for i, loop := range tc.loops {
				pl := p.plan([]sockstatCount{known(tc.inuse, 0), known(0, 0)}, 100, 0, loop)[0]
				if pl.shards != tc.expectedK || pl.shard != tc.expected[i] {
					t.Errorf("%s: loop %d shard %d/%d, expected %d/%d", tc.description, loop, pl.shard, pl.shards, tc.expected[i], tc.expectedK)
				}
			}
		})
	}
}

// TestPlanPollAllocs: no allocation once the planner is warm.
func TestPlanPollAllocs(t *testing.T) {
	counts := repeatCount(known(1000, 0), 4096)
	var p pollPlanner
	p.plan(counts, 1_000_000, 0, 0)
	if n := testing.AllocsPerRun(20, func() { p.plan(counts, 1_000_000, 0, 1) }); n != 0 {
		t.Errorf("plan allocates %v per poll, expected 0", n)
	}
}

// TestShardBytecode runs the shard filter through xtcpnl's model of the
// kernel's audit and bytecode run.
func TestShardBytecode(t *testing.T) {
	// 500k synthetic (sport, dport) pairs, fixed seed.
	rng := rand.New(rand.NewPCG(1, 2))
	pairs := make([][2]uint16, 500_000)
	for i := range pairs {
		pairs[i] = [2]uint16{uint16(rng.Uint32()), uint16(rng.Uint32())}
	}
	match := func(bc []byte, sport, dport uint16) bool {
		return xtcpnl.InetDiagBcRun(bc, &xtcpnl.InetDiagEntry{Family: 2, Sport: sport, Dport: dport})
	}

	tests := []struct {
		name        string
		category    string
		description string
		run         func(t *testing.T)
	}{
		{name: "audit_ok", category: "P", description: "every shard of K in {2,3,7,64,1024} passes the kernel audit without CAP_NET_ADMIN", run: func(t *testing.T) {
			for _, k := range []uint32{2, 3, 7, 64, maxShardsCst} {
				for i := range k {
					lo, hi := shardRange(i, k)
					if err := xtcpnl.InetDiagBcAudit(shardBytecode(lo, hi), false); err != nil {
						t.Fatalf("K=%d k=%d: %v", k, i, err)
					}
				}
			}
		}},
		{name: "partition_500k", category: "P", description: "K=7 over 500k pairs: every pair matched by exactly one shard, the one holding its key", run: func(t *testing.T) {
			const k = 7
			bcs := make([][]byte, k)
			for i := range uint32(k) {
				bcs[i] = shardBytecode(shardRange(i, k))
			}
			for _, p := range pairs {
				hits := 0
				for i, bc := range bcs {
					if match(bc, p[0], p[1]) {
						hits++
						if lo, hi := shardRange(uint32(i), k); shardKey(p[0], p[1]) < lo || shardKey(p[0], p[1]) > hi {
							t.Fatalf("pair %v matched shard %d, key outside [%d,%d]", p, i, lo, hi)
						}
					}
				}
				if hits != 1 {
					t.Fatalf("pair %v: %d shard hits, expected exactly 1", p, hits)
				}
			}
		}},
		{name: "k_full_range", category: "B", description: "the [0,65535] range matches everything", run: func(t *testing.T) {
			bc := shardBytecode(0, 65535)
			for _, p := range pairs[:1000] {
				if !match(bc, p[0], p[1]) {
					t.Fatalf("pair %v missed", p)
				}
			}
		}},
		{name: "range_edges", category: "B", description: "keys 0, 65535, 32767/32768 and K=3 boundaries (43690/43691, 54613/54614) land in exactly one shard", run: func(t *testing.T) {
			const k = 3
			cases := [][2]uint16{
				{80, 0}, {80, 65535}, {65535, 80}, {32768, 1}, {32767, 32767}, {80, 32768},
				{80, 43690}, {80, 43691}, {54613, 443}, {54614, 443},
			}
			for _, c := range cases {
				n := 0
				for i := range uint32(k) {
					lo, hi := shardRange(i, k)
					key := shardKey(c[0], c[1])
					in := key >= lo && key <= hi
					if got := match(shardBytecode(lo, hi), c[0], c[1]); got != in {
						t.Errorf("sport %d dport %d shard %d: match=%v, key %d in range=%v", c[0], c[1], i, got, key, in)
					}
					if in {
						n++
					}
				}
				if n != 1 {
					t.Errorf("sport %d dport %d in %d shards, expected 1", c[0], c[1], n)
				}
			}
		}},
		{name: "outside", category: "N", description: "server socket whose client port is outside the range: no match", run: func(t *testing.T) {
			if match(shardBytecode(100, 200), 443, 50000) {
				t.Error("matched")
			}
		}},
		{name: "single_listener", category: "C", description: "100k accepted sockets on sport 443, client ports ephemeral: spread within 10% of even over K=4", run: func(t *testing.T) {
			const k = 4
			per := make([]int, k)
			for _, p := range pairs[:100_000] {
				dport := ephemeralLoCst + p[1]%(65536-ephemeralLoCst)
				for i := range uint32(k) {
					if match(shardBytecode(shardRange(i, k)), 443, dport) {
						per[i]++
					}
				}
			}
			t.Logf("per-shard records, one listener on 443: %v", per)
			for i, n := range per {
				if n < 22_500 || n > 27_500 {
					t.Errorf("shard %d has %d of 100000, expected 25000 +-10%%: %v", i, n, per)
				}
			}
		}},
		{name: "same_port_pair", category: "C", description: "sockets differing only by address share one shard (bounded by -maxRecordsPerNetns)", run: func(t *testing.T) {
			n := 0
			for i := range uint32(4) {
				if match(shardBytecode(shardRange(i, 4)), 443, 443) {
					n++
				}
			}
			if n != 1 {
				t.Errorf("%d shards, expected 1", n)
			}
		}},
	}
	for _, tc := range tests {
		t.Run(tc.name, tc.run)
	}
}

// TestPollRequests: the request each level sends.
func TestPollRequests(t *testing.T) {
	full := make([]byte, xtcpnl.InetDiagRequestSizeCst)
	binary.LittleEndian.PutUint32(full[0:4], xtcpnl.InetDiagRequestSizeCst)
	binary.LittleEndian.PutUint32(full[20:24], 0x3fff)
	r := newPollRequests(&full)

	tests := []struct {
		name           string
		category       string
		description    string
		plan           nsPlan
		expectedLen    int
		expectedStates uint32
		expectedShared bool // same buffer as the startup request
	}{
		{name: "full", category: "P", description: "full is the startup request itself", plan: nsPlan{level: pollFull},
			expectedLen: 72, expectedStates: 0x3fff, expectedShared: true},
		{name: "states", category: "P", description: "states clears TIME_WAIT, SYN_RECV, NEW_SYN_RECV", plan: nsPlan{level: pollStates},
			expectedLen: 72, expectedStates: 0x3fff &^ statesDegradeMask},
		{name: "sample", category: "P", description: "sample = states + a 56-byte bytecode attribute", plan: nsPlan{level: pollSample, shard: 1, shards: 3},
			expectedLen: 128, expectedStates: 0x3fff &^ statesDegradeMask},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			req, err := r.get(tc.plan)
			if err != nil {
				t.Fatal(err)
			}
			b := *req
			if len(b) != tc.expectedLen || int(binary.LittleEndian.Uint32(b[0:4])) != tc.expectedLen {
				t.Errorf("%s: len %d nlmsg_len %d, expected %d", tc.description, len(b), binary.LittleEndian.Uint32(b[0:4]), tc.expectedLen)
			}
			if s := binary.LittleEndian.Uint32(b[20:24]); s != tc.expectedStates {
				t.Errorf("%s: states %#x, expected %#x", tc.description, s, tc.expectedStates)
			}
			if (req == &full) != tc.expectedShared {
				t.Errorf("%s: shared=%v, expected %v", tc.description, req == &full, tc.expectedShared)
			}
			if tc.plan.level == pollSample {
				parsed, err := xtcpnl.ParseInetDiagRequest(b[16:])
				if err != nil {
					t.Fatalf("%s: kernel-model parse: %v", tc.description, err)
				}
				if err := xtcpnl.InetDiagBcAudit(parsed.Bytecode, false); err != nil {
					t.Errorf("%s: audit: %v", tc.description, err)
				}
				again, _ := r.get(tc.plan)
				if again != req {
					t.Errorf("%s: not cached", tc.description)
				}
			}
		})
	}
	if binary.LittleEndian.Uint32(full[20:24]) != 0x3fff {
		t.Error("building the degraded requests modified the startup request")
	}
}
