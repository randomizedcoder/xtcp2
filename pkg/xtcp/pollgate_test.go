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
	eph := func() uint16 { return uint16(ephemeralLoCst + rng.IntN(65536-ephemeralLoCst)) }
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
	t.Cleanup(func() { f.Close() })
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
				x.nsMap.Store(uint64(i+1), netNSitem{inode: uint64(i + 1), socketFD: sock, stat: newStatHandle(stat)})
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
	x.nsMap.Store(uint64(1), netNSitem{inode: 1, socketFD: 5, stat: newStatHandle(1 << 20)})
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
				h := newStatHandle(fd)
				if h != nil || h.acquire() != -1 {
					t.Fatalf("fd %d: handle %v", fd, h)
				}
				h.release()
				h.close(func(int) { t.Fatal("closed a nil handle") })
			}
		}},
		{name: "acquire_open", category: "P", description: "an open handle gives its fd", run: func(t *testing.T) {
			h := newStatHandle(42)
			if fd := h.acquire(); fd != 42 {
				t.Errorf("fd %d, expected 42", fd)
			}
			h.release()
		}},
		{name: "close_waits_for_read", category: "C", description: "close blocks until the poll's read releases, then closes once", run: func(t *testing.T) {
			h := newStatHandle(42)
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
