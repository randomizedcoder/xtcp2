package xtcp

import (
	"encoding/binary"
	"slices"
	"syscall"
	"testing"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/testutil"
	"golang.org/x/sys/unix"

	"github.com/randomizedcoder/xtcp2/pkg/xtcpnl"
)

// familyKernel is a fake kernel behind the sendDumpRequest seam. It records
// every request sent to each socket and, like netlink_dump_start, answers a
// request sent while that socket's dump is still running with EBUSY (the
// real kernel sends no NLMSG_DONE then, so the poll would hang until its
// timeout).
type familyKernel struct {
	sends    map[int][]sentRequest
	inflight map[int]bool
	ebusy    int
	failFam  uint8 // a send of this family fails with EBADF; 0 for none
}

// sentRequest is one request as the fake kernel saw it.
type sentRequest struct {
	family   uint8
	seq      uint32
	bytecode bool
	states   uint32
}

func newFamilyKernel(t *testing.T, failFam uint8) *familyKernel {
	t.Helper()
	k := &familyKernel{sends: map[int][]sentRequest{}, inflight: map[int]bool{}, failFam: failFam}
	prev := sendDumpRequest
	t.Cleanup(func() { sendDumpRequest = prev })
	sendDumpRequest = func(fd int, b []byte) error {
		if k.inflight[fd] {
			k.ebusy++
			return nil
		}
		r, _ := xtcpnl.ParseInetDiagRequest(b[16:])
		k.sends[fd] = append(k.sends[fd], sentRequest{
			family:   b[16],
			seq:      binary.LittleEndian.Uint32(b[8:12]),
			bytecode: r.HasBytecode,
			states:   binary.LittleEndian.Uint32(b[20:24]),
		})
		if b[16] == k.failFam {
			return syscall.EBADF
		}
		k.inflight[fd] = true
		return nil
	}
	return k
}

// done ends fd's running dump, as its NLMSG_DONE does, and hands the
// poller the netlinker's done signal.
func (k *familyKernel) done(x *XTCP, fd, count int) int {
	k.inflight[fd] = false
	return x.handleNetlinkerDone(netlinkerDone{fd: fd}, count)
}

// families returns the families sent to fd, in order.
func (k *familyKernel) families(fd int) []uint8 {
	out := make([]uint8, 0, len(k.sends[fd]))
	for _, s := range k.sends[fd] {
		out = append(out, s.family)
	}
	return out
}

// newFamilyPoller is a poller with real requests and namespaces whose
// sockets are fds (-1: reserved, not open yet).
func newFamilyPoller(t *testing.T, fds []int) *XTCP {
	t.Helper()
	x := newPollerFixture(t)
	x.config.ReconcileBeforePoll = false
	x.initNetlinkRequests()
	for i, fd := range fds {
		x.nsMap.Store(uint64(i+1), netNSitem{inode: uint64(i + 1), socketFD: fd})
	}
	return x
}

// TestPollRequestsBothFamilies pins what each namespace's socket is sent in
// one poll: AF_INET, then AF_INET6 once the AF_INET dump's NLMSG_DONE is in,
// never both at once. Before the AF_INET6 dump existed, the positive rows
// failed: each socket got only AF_INET.
func TestPollRequestsBothFamilies(t *testing.T) {
	type expectation struct {
		polled     int
		families   map[int][]uint8 // per fd, in send order
		finalCount int
		ebusy      int
		sendErrors float64
	}
	both := []uint8{unix.AF_INET, unix.AF_INET6}
	tests := []struct {
		name        string
		category    string
		description string
		fds         []int
		gate        bool
		failFam     uint8
		expected    expectation
	}{
		{
			name: "one_ns", category: "positive",
			description: "one namespace gets AF_INET, then AF_INET6 after the first DONE",
			fds:         []int{100},
			expected:    expectation{polled: 1, families: map[int][]uint8{100: both}},
		},
		{
			name: "three_ns", category: "positive",
			description: "each of three namespaces gets both families, one dump at a time",
			fds:         []int{100, 101, 102},
			expected:    expectation{polled: 3, families: map[int][]uint8{100: both, 101: both, 102: both}},
		},
		{
			name: "gated_shards", category: "positive",
			description: "with the socket-count gate on, a sampled namespace's shard request is sent for both families",
			fds:         []int{100},
			gate:        true,
			expected:    expectation{polled: 1, families: map[int][]uint8{100: both}},
		},
		{
			name: "not_ready_fd", category: "negative",
			description: "a reserved slot (fd -1) is sent nothing and not counted",
			fds:         []int{-1, 100},
			expected:    expectation{polled: 1, families: map[int][]uint8{100: both}},
		},
		{
			name: "v6_send_error", category: "negative",
			description: "the AF_INET6 send fails: the namespace counts as done at once, the error is counted",
			fds:         []int{100},
			failFam:     unix.AF_INET6,
			expected:    expectation{polled: 1, families: map[int][]uint8{100: both}, sendErrors: 1},
		},
		{
			name: "zero_ns", category: "boundary",
			description: "nothing to poll: no sends, count 0",
			fds:         nil,
			expected:    expectation{polled: 0, families: map[int][]uint8{}},
		},
		{
			name: "ebusy_guard", category: "corner",
			description: "the fake kernel answers a send during a running dump with EBUSY; it is never hit",
			fds:         []int{100, 101},
			expected:    expectation{polled: 2, families: map[int][]uint8{100: both, 101: both}, ebusy: 0},
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			x := newFamilyPoller(t, nil)
			for i, fd := range tc.fds {
				item := netNSitem{inode: uint64(i + 1), socketFD: fd}
				if tc.gate {
					item.stat = newStatHandle(writeSockstat(t, "500000", "0"), -1)
				}
				x.nsMap.Store(uint64(i+1), item)
			}
			if tc.gate {
				x.config.MaxRecordsPerPoll = 200_000
				x.config.MaxRecordsPerNetns = 262_144
				x.pPollSockets = prometheus.NewGauge(prometheus.GaugeOpts{Name: "s"})
				x.pPollDegraded = prometheus.NewCounterVec(prometheus.CounterOpts{Name: "d"}, []string{"level"})
				defer x.closeGate()
			}
			k := newFamilyKernel(t, tc.failFam)

			count := x.pollAllNetlinkSockets(7)
			got := expectation{polled: count, families: map[int][]uint8{}}
			// Deliver each running dump's DONE until the poll ends (or a
			// bound, so a poller that never ends fails instead of hanging).
			for range 4 * (len(tc.fds) + 1) {
				if count <= 0 {
					break
				}
				for _, fd := range tc.fds {
					if k.inflight[fd] {
						count = k.done(x, fd, count)
					}
				}
			}
			got.finalCount = count
			got.ebusy = k.ebusy
			got.sendErrors = testutil.ToFloat64(x.pC.WithLabelValues("SendNetlinkDumpRequest", "Sendto", "error"))
			for fd := range k.sends {
				got.families[fd] = k.families(fd)
			}

			fail := func(what string, g, e any) {
				t.Errorf("%s (%s): %s\n %s: got %v, expected %v", tc.name, tc.category, tc.description, what, g, e)
			}
			if got.polled != tc.expected.polled {
				fail("polled", got.polled, tc.expected.polled)
			}
			if len(got.families) != len(tc.expected.families) {
				fail("sockets sent to", got.families, tc.expected.families)
			}
			for fd, want := range tc.expected.families {
				if !slices.Equal(got.families[fd], want) {
					fail("families sent", got.families, tc.expected.families)
					break
				}
			}
			if got.finalCount != tc.expected.finalCount {
				fail("count after every DONE", got.finalCount, tc.expected.finalCount)
			}
			if got.ebusy != tc.expected.ebusy {
				fail("EBUSY sends", got.ebusy, tc.expected.ebusy)
			}
			if got.sendErrors != tc.expected.sendErrors {
				fail("send errors", got.sendErrors, tc.expected.sendErrors)
			}
			for _, sends := range k.sends {
				for i := 1; i < len(sends); i++ {
					if sends[i].seq != sends[0].seq || sends[i].bytecode != sends[0].bytecode || sends[i].states != sends[0].states {
						fail("the AF_INET6 request differs from the AF_INET one beyond the family", sends, "same seq, level and states")
					}
				}
				if tc.gate && len(sends) > 0 && !sends[0].bytecode {
					fail("gated request", sends[0], "a shard (bytecode) request")
				}
			}
		})
	}
}

// TestPollEndWaitsForBothDumps pins when a namespace's poll is over: after
// its AF_INET6 dump's DONE, not its AF_INET dump's. count is the number of
// namespaces still dumping; poll_end flushes at 0. Before the AF_INET6
// dump existed, both_done, v6_never_done and poll_while_pending failed.
func TestPollEndWaitsForBothDumps(t *testing.T) {
	type expectation struct {
		counts      []int   // count after each step
		families    []uint8 // sent to the socket, in order
		seqs        []uint32
		alreadyPoll float64
	}
	const fd = 100
	tests := []struct {
		name        string
		category    string
		description string
		seq         uint32
		steps       []string // "done", "timeout", "poll", "request"
		expected    expectation
	}{
		{
			name: "both_done", category: "positive",
			description: "the poll ends after the second DONE, not the first",
			seq:         1, steps: []string{"poll", "done", "done"},
			expected: expectation{counts: []int{1, 1, 0}, families: []uint8{unix.AF_INET, unix.AF_INET6}, seqs: []uint32{8, 8}},
		},
		{
			name: "v6_never_done", category: "negative",
			description: "the AF_INET6 dump never finishes: the timeout ends the poll and the next poll starts again with AF_INET",
			seq:         1, steps: []string{"poll", "done", "timeout", "poll"},
			expected: expectation{counts: []int{1, 1, 0, 1}, families: []uint8{unix.AF_INET, unix.AF_INET6, unix.AF_INET}, seqs: []uint32{8, 8, 8}},
		},
		{
			name: "seq_wrap", category: "boundary",
			description: "the sequence number wraps: the AF_INET6 request carries the AF_INET request's seq",
			seq:         0xffffffff - 6, steps: []string{"poll", "done", "done"},
			expected: expectation{counts: []int{1, 1, 0}, families: []uint8{unix.AF_INET, unix.AF_INET6}, seqs: []uint32{0, 0}},
		},
		{
			name: "poll_while_pending", category: "corner",
			description: "a poll tick between the two dumps is coalesced; nothing extra is sent",
			seq:         1, steps: []string{"poll", "done", "request", "done"},
			expected: expectation{counts: []int{1, 1, 1, 0}, families: []uint8{unix.AF_INET, unix.AF_INET6}, seqs: []uint32{8, 8}, alreadyPoll: 1},
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			x := newFamilyPoller(t, []int{fd})
			x.config.NlmsgSeq = tc.seq
			k := newFamilyKernel(t, 0)
			var got expectation
			count := 0
			for _, step := range tc.steps {
				switch step {
				case "poll":
					k.inflight[fd] = false
					count = x.pollAllNetlinkSockets(7)
				case "done":
					count = k.done(x, fd, count)
				case "timeout":
					count = x.handlePollTimeout()
				case "request":
					count, _ = x.handlePollRequest(t.Context(), 7, count, x.pollStartTime)
				}
				got.counts = append(got.counts, count)
			}
			for _, s := range k.sends[fd] {
				got.families = append(got.families, s.family)
				got.seqs = append(got.seqs, s.seq)
			}
			got.alreadyPoll = testutil.ToFloat64(x.pC.WithLabelValues("Poller", "alreadyPolling", "count"))
			if !slices.Equal(got.counts, tc.expected.counts) || !slices.Equal(got.families, tc.expected.families) ||
				!slices.Equal(got.seqs, tc.expected.seqs) || got.alreadyPoll != tc.expected.alreadyPoll {
				t.Errorf("%s (%s): %s\n got      %+v\n expected %+v", tc.name, tc.category, tc.description, got, tc.expected)
			}
		})
	}
}

// TestDumpTrackerSharedBudget pins that -maxRecordsPerNetns is a
// namespace's share of a poll across both family dumps: the AF_INET6 dump
// carries the poll's sequence number, so it continues the AF_INET dump's
// budget, and a dump with a new sequence number (the next poll) starts
// afresh. Before the shared budget, shared failed: the AF_INET6 dump got a
// fresh budget at the AF_INET dump's DONE.
func TestDumpTrackerSharedBudget(t *testing.T) {
	c := loadDumpCorpus(t)
	type dump struct {
		seq     uint32
		records int // in one data packet before the DONE packet; 0: DONE only
	}
	type expectation struct {
		kept, dropped, dumps int
	}
	tests := []struct {
		name        string
		category    string
		description string
		maxRecords  uint64
		dumps       []dump
		expected    expectation
	}{
		{
			name: "shared", category: "positive",
			description: "AF_INET 8 + AF_INET6 8 records under a cap of 10: 10 kept, 6 dropped",
			maxRecords:  10, dumps: []dump{{seq: 5, records: 8}, {seq: 5, records: 8}},
			expected: expectation{kept: 10, dropped: 6, dumps: 2},
		},
		{
			name: "next_poll_fresh", category: "negative",
			description: "the next poll's dump (a new seq) is not charged for this poll's records",
			maxRecords:  10, dumps: []dump{{seq: 5, records: 8}, {seq: 6, records: 8}},
			expected: expectation{kept: 16, dropped: 0, dumps: 2},
		},
		{
			name: "exact", category: "boundary",
			description: "5 + 5 records under a cap of 10: all kept",
			maxRecords:  10, dumps: []dump{{seq: 5, records: 5}, {seq: 5, records: 5}},
			expected: expectation{kept: 10, dropped: 0, dumps: 2},
		},
		{
			name: "empty_second", category: "corner",
			description: "an AF_INET6 dump that is only its DONE still ends as its own dump",
			maxRecords:  10, dumps: []dump{{seq: 5, records: 8}, {seq: 5, records: 0}},
			expected: expectation{kept: 8, dropped: 0, dumps: 2},
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			tr := newDumpTracker(0, tc.maxRecords)
			var got expectation
			for _, d := range tc.dumps {
				var pkts [][]byte
				if d.records > 0 {
					pkts = append(pkts, withSeq(c.dataPacket(d.records), d.seq))
				}
				pkts = append(pkts, withSeq(c.done, d.seq))
				for _, p := range pkts {
					first := tr.dump
					a := tr.assign(p)
					_, recs := packetScan(p)
					switch {
					case a.drop:
					case a.keep > 0:
						got.kept += a.keep
					default:
						got.kept += recs
					}
					got.dropped += a.recordsDropped
					if tr.dump != first {
						got.dumps++
					}
				}
			}
			if got != tc.expected {
				t.Errorf("%s (%s): %s\n got      %+v\n expected %+v", tc.name, tc.category, tc.description, got, tc.expected)
			}
		})
	}
}

// withSeq returns a copy of netlink packet p with every message header's
// sequence number set to seq: the poll the kernel's replies belong to. A
// header shorter than a header (a zero-filled test packet) stops the walk
// after its seq is set.
func withSeq(p []byte, seq uint32) []byte {
	p = slices.Clone(p)
	for off := 0; off+xtcpnl.NlMsgHdrSizeCst <= len(p); {
		binary.LittleEndian.PutUint32(p[off+8:off+12], seq)
		l := int(binary.LittleEndian.Uint32(p[off : off+4]))
		if l < xtcpnl.NlMsgHdrSizeCst {
			break
		}
		off += nlmsgAlign(l)
	}
	return p
}
