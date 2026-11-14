package xtcp

import (
	"encoding/binary"
	"math/rand/v2"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/testutil"

	"github.com/randomizedcoder/xtcp2/pkg/xtcpnl"
	"golang.org/x/sys/unix"
)

// fakeKernelDump applies a captured request the way tcp_diag_dump does to
// a synthetic socket table: the idiag_states check, then the bytecode
// (xtcpnl's model of inet_diag_bc_run). It returns the records the dump
// would hold.
func fakeKernelDump(t *testing.T, req []byte, table []fakeSock) int {
	t.Helper()
	r, err := xtcpnl.ParseInetDiagRequest(req[16:])
	if err != nil {
		t.Fatalf("request: %v", err)
	}
	// The collector writes idiag_states big-endian (see init.go); the kernel
	// reads it host order, which is what the degraded requests write.
	states := binary.LittleEndian.Uint32(req[20:24])
	if states&(1<<3) != 0 {
		states |= 1 << 12 // tcp_diag.c: SYN_RECV implies NEW_SYN_RECV
	}
	n := 0
	for _, s := range table {
		if states&(1<<s.state) == 0 {
			continue
		}
		if r.HasBytecode && !xtcpnl.InetDiagBcRun(r.Bytecode, &xtcpnl.InetDiagEntry{Family: 2, Sport: s.sport, Dport: s.dport}) {
			continue
		}
		n++
	}
	return n
}

type fakeSock struct {
	state        uint8
	sport, dport uint16
}

// syntheticTable is n server-side sockets on :443 with ephemeral client
// ports, plus tw TIME_WAIT and syn SYN_RECV ones.
func syntheticTable(n, tw, syn int) []fakeSock {
	rng := rand.New(rand.NewPCG(3, 4))
	eph := func() uint16 { return defaultPortRange.lo + uint16(rng.IntN(65536-int(defaultPortRange.lo))) }
	out := make([]fakeSock, 0, n+tw+syn)
	for range n {
		out = append(out, fakeSock{state: 1, sport: 443, dport: eph()})
	}
	for range tw {
		out = append(out, fakeSock{state: 6, sport: 443, dport: eph()})
	}
	for range syn {
		out = append(out, fakeSock{state: 12, sport: 443, dport: eph()})
	}
	return out
}

func writeSockstat(t *testing.T, inuse, tw string) int {
	t.Helper()
	p := filepath.Join(t.TempDir(), "sockstat")
	if err := os.WriteFile(p, []byte(sockstatFile(inuse, tw)), 0o600); err != nil {
		t.Fatal(err)
	}
	f, err := os.Open(p)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = f.Close() })
	return int(f.Fd())
}

// TestPollGateEndToEnd: pollAllNetlinkSockets with the gate on. Sockstat
// is a file per namespace (pread works on it), the sockets are fds the
// send seam captures, and the captured request is run against a synthetic
// table. The table is 1/100 of the reported counts, and so is the budget.
func TestPollGateEndToEnd(t *testing.T) {
	type ns struct {
		inuse, tw  string // sockstat as reported (500k scale)
		statFD     int    // -1: not open; 0: a sockstat file is made
		table      []fakeSock
		wantLevel  pollLevel
		wantRecMax int // records the captured request returns, at most
	}
	tests := []struct {
		name            string
		category        string
		description     string
		budget          uint64
		namespaces      []ns
		expectedSample  float64
		expectedStates  float64
		expectedUnknown float64
	}{
		{name: "gate_samples", category: "N", description: "a 500k namespace next to a small one: the heavy one gets a shard request, the small one its full dump",
			budget: 200_000,
			namespaces: []ns{
				{inuse: "500000", tw: "0", table: syntheticTable(5000, 0, 0), wantLevel: pollSample, wantRecMax: 5000/3 + 150},
				{inuse: "1000", tw: "0", table: syntheticTable(10, 0, 0), wantLevel: pollFull, wantRecMax: 10},
			},
			expectedSample: 1},
		{name: "gate_states", category: "N", description: "mostly TIME_WAIT and SYN_RECV: the states request drops them in the kernel",
			budget: 200_000,
			namespaces: []ns{
				{inuse: "50000", tw: "450000", table: syntheticTable(500, 4500, 300), wantLevel: pollStates, wantRecMax: 500},
				{inuse: "1000", tw: "0", table: syntheticTable(10, 0, 0), wantLevel: pollFull, wantRecMax: 10},
			},
			expectedStates: 1},
		{name: "under_budget", category: "P", description: "both fit: both full",
			budget: 200_000,
			namespaces: []ns{
				{inuse: "100000", tw: "0", table: syntheticTable(1000, 0, 0), wantLevel: pollFull, wantRecMax: 1000},
				{inuse: "1000", tw: "0", table: syntheticTable(10, 0, 0), wantLevel: pollFull, wantRecMax: 10},
			}},
		{name: "sockstat_fd_closed", category: "C", description: "no sockstat fd: unknown, taken as the per-netns cap, still polled",
			budget: 100_000,
			namespaces: []ns{
				{statFD: -1, table: syntheticTable(3000, 0, 0), wantLevel: pollSample, wantRecMax: 3000/2 + 150},
				{inuse: "10", tw: "0", table: syntheticTable(10, 0, 0), wantLevel: pollFull, wantRecMax: 10},
			},
			expectedSample: 1, expectedUnknown: 1},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			x := newPollerFixture(t)
			x.config.MaxRecordsPerPoll = tc.budget
			x.config.MaxRecordsPerNetns = 262_144
			req := make([]byte, xtcpnl.InetDiagRequestSizeCst)
			binary.LittleEndian.PutUint32(req[0:4], xtcpnl.InetDiagRequestSizeCst)
			binary.LittleEndian.PutUint32(req[20:24], 0x3fff)
			x.nlRequest = &req
			x.pPollSockets = prometheus.NewGauge(prometheus.GaugeOpts{Name: "s"})
			x.pPollDegraded = prometheus.NewCounterVec(prometheus.CounterOpts{Name: "d"}, []string{"level"})
			defer x.closeGate()

			sent := map[int][]byte{}
			prev := sendDumpRequest
			t.Cleanup(func() { sendDumpRequest = prev })
			sendDumpRequest = func(fd int, b []byte) error {
				sent[fd] = append([]byte(nil), b...)
				return nil
			}
			byFD := map[int]ns{}
			for i, n := range tc.namespaces {
				sock := 1000 + i
				stat := n.statFD
				if stat == 0 {
					stat = writeSockstat(t, n.inuse, n.tw)
				}
				x.nsMap.Store(uint64(i+1), netNSitem{inode: uint64(i + 1), socketFD: sock, stat: newStatHandle(stat, -1)})
				byFD[sock] = n
			}

			const loop = 7
			if got := x.pollAllNetlinkSockets(loop); got != len(tc.namespaces) {
				t.Fatalf("%s: polled %d, expected %d", tc.description, got, len(tc.namespaces))
			}
			for fd, n := range byFD {
				b, ok := sent[fd]
				if !ok {
					t.Fatalf("%s: fd %d not sent a request", tc.description, fd)
				}
				if seq := binary.LittleEndian.Uint32(b[8:12]); seq != x.config.NlmsgSeq+loop {
					t.Errorf("%s: fd %d seq %d, expected %d", tc.description, fd, seq, x.config.NlmsgSeq+loop)
				}
				level := pollFull
				if r, _ := xtcpnl.ParseInetDiagRequest(b[16:]); r.HasBytecode {
					level = pollSample
				} else if binary.LittleEndian.Uint32(b[20:24]) != 0x3fff {
					level = pollStates
				}
				if level != n.wantLevel {
					t.Errorf("%s: fd %d sent a %s request, expected %s", tc.description, fd, level, n.wantLevel)
				}
				if rec := fakeKernelDump(t, b, n.table); rec > n.wantRecMax {
					t.Errorf("%s: fd %d dump has %d records, expected <= %d", tc.description, fd, rec, n.wantRecMax)
				}
			}
			for level, want := range map[string]float64{"sample": tc.expectedSample, "states": tc.expectedStates} {
				if got := testutil.ToFloat64(x.pPollDegraded.WithLabelValues(level)); got != want {
					t.Errorf("%s: poll_degraded{%s}=%v, expected %v", tc.description, level, got, want)
				}
			}
			if got := testutil.ToFloat64(x.pC.WithLabelValues("poller", "planPoll", "unknownCount")); got != tc.expectedUnknown {
				t.Errorf("%s: unknownCount=%v, expected %v", tc.description, got, tc.expectedUnknown)
			}
		})
	}
}

// TestPollGateShardRotation: a sampled namespace's shard advances once per
// gated poll, whatever pollingLoops does between polls (it advances on
// every poller event, so steps of K must not pin one shard).
func TestPollGateShardRotation(t *testing.T) {
	tests := []struct {
		name           string
		category       string
		description    string
		loops          []uint64
		expectedShards int
	}{
		{name: "consecutive", category: "P", description: "pollingLoops 1,2,3: each poll a new shard", loops: []uint64{1, 2, 3}, expectedShards: 3},
		{name: "step_k", category: "C", description: "pollingLoops step by K=3 (N netlinkerDone events per poll): still every shard", loops: []uint64{3, 6, 9}, expectedShards: 3},
		{name: "repeat_loop", category: "C", description: "the same pollingLoops value three times: still every shard", loops: []uint64{5, 5, 5}, expectedShards: 3},
		{name: "wraps", category: "B", description: "K+1 polls: the first shard comes round again", loops: []uint64{3, 6, 9, 12}, expectedShards: 3},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			x := newPollerFixture(t)
			x.config.MaxRecordsPerPoll = 200_000
			x.config.MaxRecordsPerNetns = 262_144
			req := make([]byte, xtcpnl.InetDiagRequestSizeCst)
			binary.LittleEndian.PutUint32(req[0:4], xtcpnl.InetDiagRequestSizeCst)
			binary.LittleEndian.PutUint32(req[20:24], 0x3fff)
			x.nlRequest = &req
			defer x.closeGate()

			var heavy [][]byte
			prev := sendDumpRequest
			t.Cleanup(func() { sendDumpRequest = prev })
			sendDumpRequest = func(fd int, b []byte) error {
				if fd == 1000 {
					c := append([]byte(nil), b...)
					binary.LittleEndian.PutUint32(c[8:12], 0) // seq differs per poll
					heavy = append(heavy, c)
				}
				return nil
			}
			// 500k against a ~199k share: K = 3.
			x.nsMap.Store(uint64(1), netNSitem{inode: 1, socketFD: 1000, stat: newStatHandle(writeSockstat(t, "500000", "0"), -1)})
			x.nsMap.Store(uint64(2), netNSitem{inode: 2, socketFD: 1001, stat: newStatHandle(writeSockstat(t, "1000", "0"), -1)})

			for _, l := range tc.loops {
				x.pollAllNetlinkSockets(l)
			}
			seen := map[string]bool{}
			for _, b := range heavy {
				if r, _ := xtcpnl.ParseInetDiagRequest(b[16:]); !r.HasBytecode {
					t.Fatalf("%s: heavy namespace sent an unsampled request", tc.description)
				}
				seen[string(b)] = true
			}
			if len(seen) != tc.expectedShards {
				t.Errorf("%s: %d distinct shards over %d polls, expected %d", tc.description, len(seen), len(heavy), tc.expectedShards)
			}
			if len(heavy) > tc.expectedShards && string(heavy[tc.expectedShards]) != string(heavy[0]) {
				t.Errorf("%s: poll %d did not return to the first shard", tc.description, tc.expectedShards)
			}
		})
	}
}

// TestPollGateOffUnchanged: with -maxRecordsPerPoll 0 every namespace is
// sent the startup request and no sockstat is read.
func TestPollGateOffUnchanged(t *testing.T) {
	x := newPollerFixture(t)
	req := make([]byte, xtcpnl.InetDiagRequestSizeCst)
	x.nlRequest = &req
	var got [][]byte
	prev := sendDumpRequest
	t.Cleanup(func() { sendDumpRequest = prev })
	sendDumpRequest = func(_ int, b []byte) error { got = append(got, b); return nil }
	x.nsMap.Store(uint64(1), netNSitem{inode: 1, socketFD: 5, stat: newStatHandle(1<<20, -1)})
	x.pollAllNetlinkSockets(0)
	if len(got) != 1 || &got[0][0] != &req[0] || x.gate != nil {
		t.Errorf("gate off: sent %d requests (startup buffer=%v), gate=%v", len(got), len(got) == 1 && &got[0][0] == &req[0], x.gate)
	}
}

// TestStatHandle: the poller's read lock and the teardown's close.
func TestStatHandle(t *testing.T) {
	tests := []struct {
		name        string
		category    string
		description string
		run         func(t *testing.T)
	}{
		{name: "nil_handle", category: "B", description: "fd <= 0 gives a nil handle; acquire is -1 and release/close are no-ops", run: func(t *testing.T) {
			for _, fd := range []int{-1, 0} {
				h := newStatHandle(fd, -1)
				if h != nil || h.acquire() != -1 {
					t.Fatalf("fd %d: handle %v", fd, h)
				}
				h.release()
				h.close(func(int) { t.Fatal("closed a nil handle") })
			}
		}},
		{name: "port_range", category: "P", description: "the port-range fd is read at offset 0 every time", run: func(t *testing.T) {
			h := newStatHandle(42, writeFile(t, "ip_local_port_range", "10000\t65000\n"))
			buf := make([]byte, portRangeBufSize)
			for range 2 {
				h.acquire()
				pr, ok := h.portRange(buf)
				h.release()
				if !ok || pr != (portRange{lo: 10000, hi: 65000}) {
					t.Fatalf("portRange %v %v, expected 10000-65000", pr, ok)
				}
			}
		}},
		{name: "port_range_unopened", category: "N", description: "no port-range fd: not ok (the caller uses the default)", run: func(t *testing.T) {
			h := newStatHandle(42, -1)
			if _, ok := h.portRange(make([]byte, portRangeBufSize)); ok {
				t.Error("ok without an fd")
			}
		}},
		{name: "port_range_bad_fd", category: "N", description: "a port-range fd that isn't open: the read fails, not ok", run: func(t *testing.T) {
			h := newStatHandle(42, 1<<20)
			if _, ok := h.portRange(make([]byte, portRangeBufSize)); ok {
				t.Error("ok on a bad fd")
			}
		}},
		{name: "close_both", category: "C", description: "close closes the sockstat and port-range fds, once each", run: func(t *testing.T) {
			h := newStatHandle(42, 43)
			var closed []int
			h.close(func(fd int) { closed = append(closed, fd) })
			h.close(func(fd int) { closed = append(closed, fd) })
			if len(closed) != 2 || closed[0] != 42 || closed[1] != 43 {
				t.Errorf("closed %v, expected [42 43]", closed)
			}
		}},
		{name: "acquire_open", category: "P", description: "an open handle gives its fd", run: func(t *testing.T) {
			h := newStatHandle(42, -1)
			if fd := h.acquire(); fd != 42 {
				t.Errorf("fd %d, expected 42", fd)
			}
			h.release()
		}},
		{name: "close_waits_for_read", category: "C", description: "close blocks until the poll's read releases, then closes once", run: func(t *testing.T) {
			h := newStatHandle(42, -1)
			fd := h.acquire()
			closed := make(chan int, 2)
			go h.close(func(fd int) { closed <- fd })
			select {
			case <-closed:
				t.Fatal("closed while a read held the handle")
			case <-time.After(50 * time.Millisecond):
			}
			h.release()
			if got := <-closed; got != fd {
				t.Errorf("closed %d, expected %d", got, fd)
			}
			h.close(func(int) { t.Error("closed twice") })
			if h.acquire() != -1 {
				t.Error("acquire after close is not -1")
			}
			h.release()
		}},
	}
	for _, tc := range tests {
		t.Run(tc.name, tc.run)
	}
}

// TestCheckFDLimit: the startup RLIMIT_NOFILE warning.
func TestCheckFDLimit(t *testing.T) {
	tests := []struct {
		name         string
		category     string
		description  string
		maxNetns     uint32
		gate         bool
		soft         uint64
		expectedNeed uint64 // 0: no warning
	}{
		{name: "enough", category: "P", description: "1M fds cover 4096 namespaces with the gate", maxNetns: 4096, gate: true, soft: 1 << 20},
		{name: "low_gate", category: "N", description: "1024 fds, 4096 namespaces x 4 (socket, origNs, sockstat, port range) + 256", maxNetns: 4096, gate: true, soft: 1024, expectedNeed: 16640},
		{name: "low_no_gate", category: "N", description: "gate off: socket and origNs per namespace", maxNetns: 4096, soft: 1024, expectedNeed: 8448},
		{name: "exact", category: "B", description: "soft limit exactly the need", maxNetns: 100, gate: true, soft: 656},
		{name: "one_short", category: "B", description: "one below the need", maxNetns: 100, gate: true, soft: 655, expectedNeed: 656},
		{name: "netns_cap_off", category: "C", description: "-maxNetns 0: unbounded, nothing to check", soft: 16},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			prev := getNofile
			t.Cleanup(func() { getNofile = prev })
			getNofile = func() (uint64, error) { return tc.soft, nil }
			x := newPollerFixture(t)
			x.config.MaxNetns = tc.maxNetns
			if tc.gate {
				x.config.MaxRecordsPerPoll = 1
			}
			if got := x.checkFDLimit(); got != tc.expectedNeed {
				t.Errorf("%s: need %d, expected %d", tc.description, got, tc.expectedNeed)
			}
		})
	}
}

// writeFile writes contents to a temp file and returns an fd open on it.
func writeFile(t *testing.T, name, contents string) int {
	t.Helper()
	p := filepath.Join(t.TempDir(), name)
	if err := os.WriteFile(p, []byte(contents), 0o600); err != nil {
		t.Fatal(err)
	}
	fd, err := unix.Open(p, unix.O_RDONLY|unix.O_CLOEXEC, 0)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = unix.Close(fd) })
	return fd
}

// TestParsePortRange: ip_local_port_range's "lo\thi\n".
func TestParsePortRange(t *testing.T) {
	tests := []struct {
		name        string
		category    string
		description string
		in          string
		expected    portRange
		expectedOK  bool
	}{
		{name: "default", category: "P", description: "Linux's default", in: "32768\t60999\n", expected: portRange{32768, 60999}, expectedOK: true},
		{name: "wide", category: "P", description: "a tuned wide range", in: "1024\t65535\n", expected: portRange{1024, 65535}, expectedOK: true},
		{name: "spaces", category: "C", description: "spaces instead of a tab, no newline", in: "10000  65000", expected: portRange{10000, 65000}, expectedOK: true},
		{name: "equal", category: "B", description: "lo == hi, which the kernel allows", in: "5000\t5000\n", expected: portRange{5000, 5000}, expectedOK: true},
		{name: "extremes", category: "B", description: "1 and 65535", in: "1\t65535\n", expected: portRange{1, 65535}, expectedOK: true},
		{name: "zero_lo", category: "N", description: "lo 0 is outside the kernel's bounds", in: "0\t65535\n"},
		{name: "inverted", category: "N", description: "lo > hi", in: "60999\t32768\n"},
		{name: "too_big", category: "N", description: "a port over 65535", in: "1024\t65536\n"},
		{name: "one_field", category: "N", description: "only one number", in: "32768\n"},
		{name: "garbage", category: "N", description: "a trailing non-digit", in: "32768\t60999x\n"},
		{name: "empty", category: "N", description: "nothing read", in: ""},
		{name: "huge_digits", category: "C", description: "a long digit run doesn't overflow", in: "99999999999999999999999\t1\n"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, ok := parsePortRange([]byte(tc.in))
			if ok != tc.expectedOK || got != tc.expected {
				t.Errorf("%s: parsePortRange(%q) = %v, %v; expected %v, %v", tc.description, tc.in, got, ok, tc.expected, tc.expectedOK)
			}
		})
	}
}
