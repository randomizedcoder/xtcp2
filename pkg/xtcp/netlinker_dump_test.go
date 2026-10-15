package xtcp

import (
	"context"
	"encoding/binary"
	"fmt"
	"math"
	"sync"
	"syscall"
	"testing"
	"time"

	"github.com/prometheus/client_golang/prometheus/testutil"

	"github.com/randomizedcoder/xtcp2/gen/go/xtcp_flat_record"
	"github.com/randomizedcoder/xtcp2/pkg/xtcpnl"
)

// These tests pin the end-of-dump contract between the netlinkers and the
// poller (docs/investigations/envelope-post-flush-drop.md): the done signal
// for a socket must not go out until every packet of that socket's dump has
// been decoded. They drive createNetlinkers, the production entry point,
// over a SOCK_DGRAM socketpair (which keeps packet boundaries, like a
// netlink socket) with real inet_diag records, and play the poller
// themselves: install an envelope, write the dump, flush on the done
// signal, then count the rows that made it into the envelope and the
// records dropped after the flush.

const (
	dumpDoneTimeout   = 5 * time.Second
	dumpHoldWindow    = 300 * time.Millisecond
	dumpQuiesceWindow = 5 * time.Second
	dumpRecordsPerPkt = 60
	dumpBufferSize    = 64 * 1024
)

// dumpCorpus holds the INET_DIAG records of a real inet_diag capture
// and a done message.
type dumpCorpus struct {
	records [][]byte
	done    []byte
}

func loadDumpCorpus(tb testing.TB) dumpCorpus {
	tb.Helper()
	b := loadRealMultipart(tb)
	var c dumpCorpus
	for off := 0; off+xtcpnl.NlMsgHdrSizeCst <= len(b); {
		l := int(binary.LittleEndian.Uint32(b[off : off+4]))
		typ := binary.LittleEndian.Uint16(b[off+4 : off+6])
		if l < xtcpnl.NlMsgHdrSizeCst || off+l > len(b) {
			tb.Fatalf("corpus: bad nlmsg_len %d at offset %d", l, off)
		}
		if typ == xtcpnl.NlMsgHdrTypeInetDiagCst {
			c.records = append(c.records, b[off:off+l])
		}
		off += (l + 3) &^ 3
	}
	if len(c.records) == 0 {
		tb.Fatal("corpus: no INET_DIAG records")
	}
	c.done = doneMessage()
	return c
}

// doneMessage is the 20-byte NLMSG_DONE the kernel ends a dump with
// (netlink_dump_done): a header with NLM_F_MULTI and a 4-byte errno of 0.
func doneMessage() []byte {
	const nlmFMulti = 0x2
	b := make([]byte, xtcpnl.NlMsgHdrSizeCst+4)
	binary.LittleEndian.PutUint32(b[0:4], uint32(len(b)))
	binary.LittleEndian.PutUint16(b[4:6], xtcpnl.NlMsgHdrTypeDoneCst)
	binary.LittleEndian.PutUint16(b[6:8], nlmFMulti)
	return b
}

// dataPacket concatenates n records, cycling through the corpus.
func (c dumpCorpus) dataPacket(n int) []byte {
	var p []byte
	for i := range n {
		p = append(p, c.records[i%len(c.records)]...)
	}
	return p
}

// dump returns dataPkts packets of dumpRecordsPerPkt records followed by a
// separate done packet, the way the kernel sends a multi-packet dump.
func (c dumpCorpus) dump(dataPkts int) [][]byte {
	var pkts [][]byte
	for range dataPkts {
		pkts = append(pkts, c.dataPacket(dumpRecordsPerPkt))
	}
	return append(pkts, c.done)
}

// dumpHarness runs createNetlinkers on the read side of a socketpair and
// lets the test write packets to the other side and act as the poller.
type dumpHarness struct {
	x       *XTCP
	writeFD int
	cancel  context.CancelFunc
	wg      *sync.WaitGroup
}

func newDumpHarness(t testing.TB, netlinkers uint32, hook func(fd int, packet []byte)) *dumpHarness {
	t.Helper()
	x := newTestDeserializeXTCP(t)
	x.fdToNsMap = &sync.Map{}
	x.packetBufferPool.Init(func() *[]byte {
		b := make([]byte, dumpBufferSize)
		return &b
	})
	x.decodeHook = hook
	// The harness flushes per dump itself; keep the mid-poll size/row cap
	// flushes (which need the real envelope pool and marshaller) out of it.
	x.config.EnvelopeFlushThresholdRows = math.MaxUint32
	x.config.EnvelopeFlushThresholdBytes = math.MaxUint32
	x.Netlinker = func(ctx context.Context, wg *sync.WaitGroup, nsName *string, inode uint64, fd int, id uint32) {
		x.netlinkerSyscall(ctx, wg, nsName, inode, fd, id)
	}

	readFD, writeFD, _ := makeSocketPair(t)
	setShortRecvTimeout(t, readFD)
	x.fdToNsMap.Store(readFD, uint64(1))
	x.pollTime.Store(readFD, time.Now())

	ctx, cancel := context.WithCancel(context.Background())
	wg := &sync.WaitGroup{}
	nsName := "test-ns"
	wg.Add(1)
	go x.createNetlinkers(ctx, wg, &nsName, 1, readFD, netlinkers)

	h := &dumpHarness{x: x, writeFD: writeFD, cancel: cancel, wg: wg}
	t.Cleanup(func() {
		cancel()
		if !waitGroupTimeout(wg, dumpQuiesceWindow) {
			t.Errorf("netlinkers did not exit within %s of cancel", dumpQuiesceWindow)
		}
	})
	return h
}

func waitGroupTimeout(wg *sync.WaitGroup, d time.Duration) bool {
	ch := make(chan struct{})
	go func() {
		wg.Wait()
		close(ch)
	}()
	select {
	case <-ch:
		return true
	case <-time.After(d):
		return false
	}
}

func (h *dumpHarness) send(t testing.TB, pkts ...[]byte) {
	t.Helper()
	if err := h.write(pkts); err != nil {
		t.Fatal(err)
	}
}

func (h *dumpHarness) write(pkts [][]byte) error {
	for _, p := range pkts {
		if _, err := syscall.Write(h.writeFD, p); err != nil {
			return fmt.Errorf("write %d-byte packet: %w", len(p), err)
		}
	}
	return nil
}

// startDump installs a fresh envelope, as pollAllNetlinkSockets does.
func (h *dumpHarness) startDump() {
	h.x.envelopeMu.Lock()
	h.x.currentEnvelope = &xtcp_flat_record.Envelope{}
	h.x.envelopeMu.Unlock()
}

// flush ends the poll as flushEnvelope("poll_end") does: it takes the
// envelope's rows and leaves currentEnvelope nil.
func (h *dumpHarness) flush() int {
	e := h.takeEnvelope()
	if e == nil {
		return 0
	}
	h.recycle(e)
	return len(e.Row)
}

// takeEnvelope ends the poll, leaving currentEnvelope nil, and returns the
// envelope without touching its rows.
func (h *dumpHarness) takeEnvelope() *xtcp_flat_record.Envelope {
	h.x.envelopeMu.Lock()
	defer h.x.envelopeMu.Unlock()
	e := h.x.currentEnvelope
	h.x.currentEnvelope = nil
	return e
}

// recycle returns an envelope's rows to the record pool.
func (h *dumpHarness) recycle(e *xtcp_flat_record.Envelope) {
	for _, r := range e.Row {
		r.Reset()
		h.x.xtcpRecordPool.Put(r)
	}
}

func (h *dumpHarness) drops() int {
	return int(testutil.ToFloat64(h.x.pC.WithLabelValues("Deserialize", "envelopePostFlushDrop", "error")))
}

func (h *dumpHarness) appends() int {
	return int(testutil.ToFloat64(h.x.pC.WithLabelValues("Deserialize", "envelopeAppend", "count")))
}

// waitDone waits up to d for one done signal.
func (h *dumpHarness) waitDone(d time.Duration) bool {
	select {
	case <-h.x.netlinkerDoneCh:
		return true
	case <-time.After(d):
		return false
	}
}

// quiesce waits until every record sent so far has been either appended
// or dropped, so the counters are final.
func (h *dumpHarness) quiesce(t testing.TB, records int) {
	t.Helper()
	deadline := time.Now().Add(dumpQuiesceWindow)
	for h.appends()+h.drops() < records {
		if time.Now().After(deadline) {
			t.Fatalf("only %d of %d records decoded within %s", h.appends()+h.drops(), records, dumpQuiesceWindow)
		}
		time.Sleep(time.Millisecond)
	}
}

// dumpResult is what the poller saw over all dumps.
type dumpResult struct {
	rows       int // rows taken by the flushes
	drops      int // envelopePostFlushDrop
	dones      int // done signals received
	earlyDones int // done signals received while a decoder was held
}

func countRecords(pkts [][]byte) int {
	n := 0
	for _, p := range pkts {
		for off := 0; off+xtcpnl.NlMsgHdrSizeCst <= len(p); {
			l := int(binary.LittleEndian.Uint32(p[off : off+4]))
			if binary.LittleEndian.Uint16(p[off+4:off+6]) == xtcpnl.NlMsgHdrTypeInetDiagCst {
				n++
			}
			off += (l + 3) &^ 3
		}
	}
	return n
}

// TestNetlinkerDump_doneWaitsForDecode is the test-first reproduction of
// envelopePostFlushDrop. Each row runs a number of dumps through
// createNetlinkers and checks that every record reached the envelope,
// that none were dropped after the flush, and that each dump produced
// exactly one done signal.
//
// holdFirst holds the decoder of each dump's first packet (via
// x.decodeHook) until the rest of the dump has been written and the
// poller has waited dumpHoldWindow: a done signal in that window means
// the done went out while a sibling was still decoding.
func TestNetlinkerDump_doneWaitsForDecode(t *testing.T) {
	c := loadDumpCorpus(t)
	dataPlusDone := append(c.dataPacket(dumpRecordsPerPkt), c.done...)

	tests := []struct {
		description string
		netlinkers  uint32
		dumps       int
		packets     [][]byte
		holdFirst   bool
		expected    dumpResult
	}{
		{
			description: "negative: a sibling is held mid-decode when the done packet is read",
			netlinkers:  4,
			dumps:       1,
			packets:     c.dump(1),
			holdFirst:   true,
			expected:    dumpResult{rows: dumpRecordsPerPkt, drops: 0, dones: 1, earlyDones: 0},
		},
		{
			description: "negative: a sibling is held mid-decode, multi-packet dump",
			netlinkers:  4,
			dumps:       1,
			packets:     c.dump(3),
			holdFirst:   true,
			expected:    dumpResult{rows: 3 * dumpRecordsPerPkt, drops: 0, dones: 1, earlyDones: 0},
		},
		{
			description: "positive: one netlinker decodes in arrival order",
			netlinkers:  1,
			dumps:       20,
			packets:     c.dump(3),
			expected:    dumpResult{rows: 20 * 3 * dumpRecordsPerPkt, drops: 0, dones: 20},
		},
		{
			description: "positive: many netlinkers, many dumps, no hook",
			netlinkers:  4,
			dumps:       1000,
			packets:     c.dump(5),
			expected:    dumpResult{rows: 1000 * 5 * dumpRecordsPerPkt, drops: 0, dones: 1000},
		},
		{
			description: "boundary: a dump with only the done packet (an empty namespace)",
			netlinkers:  4,
			dumps:       10,
			packets:     [][]byte{c.done},
			expected:    dumpResult{rows: 0, drops: 0, dones: 10},
		},
		{
			description: "boundary: the done message at the end of a data packet",
			netlinkers:  4,
			dumps:       10,
			packets:     [][]byte{dataPlusDone},
			expected:    dumpResult{rows: 10 * dumpRecordsPerPkt, drops: 0, dones: 10},
		},
		{
			description: "corner: the done message ends the last data packet of a multi-packet dump, first packet held",
			netlinkers:  4,
			dumps:       1,
			packets:     [][]byte{c.dataPacket(dumpRecordsPerPkt), dataPlusDone},
			holdFirst:   true,
			expected:    dumpResult{rows: 2 * dumpRecordsPerPkt, drops: 0, dones: 1, earlyDones: 0},
		},
	}

	for _, tc := range tests {
		t.Run(tc.description, func(t *testing.T) {
			var (
				mu      sync.Mutex
				holding bool
				held    chan struct{}
				release chan struct{}
			)
			var hook func(int, []byte)
			if tc.holdFirst {
				first := tc.packets[0]
				hook = func(_ int, p []byte) {
					mu.Lock()
					if holding || len(p) != len(first) || held == nil {
						mu.Unlock()
						return
					}
					holding = true
					h, r := held, release
					held = nil
					mu.Unlock()
					close(h)
					<-r
				}
			}
			h := newDumpHarness(t, tc.netlinkers, hook)
			perDump := countRecords(tc.packets)

			var got dumpResult
			for i := 0; i < tc.dumps; i++ {
				h.startDump()
				if tc.holdFirst {
					mu.Lock()
					holding = false
					held = make(chan struct{})
					release = make(chan struct{})
					hc, rc := held, release
					mu.Unlock()

					h.send(t, tc.packets[0])
					select {
					case <-hc:
					case <-time.After(dumpDoneTimeout):
						t.Fatalf("dump %d: the first packet never reached a decoder", i)
					}
					h.send(t, tc.packets[1:]...)
					if h.waitDone(dumpHoldWindow) {
						got.earlyDones++
						got.dones++
						got.rows += h.flush()
					}
					close(rc)
				} else {
					h.send(t, tc.packets...)
				}
				if got.dones < i+1 {
					if !h.waitDone(dumpDoneTimeout) {
						t.Fatalf("dump %d: no done signal within %s", i, dumpDoneTimeout)
					}
					got.dones++
					got.rows += h.flush()
				}
				h.quiesce(t, (i+1)*perDump)
			}
			// Any further done signal is a duplicate.
			for h.waitDone(50 * time.Millisecond) {
				got.dones++
			}
			got.drops = h.drops()

			if got != tc.expected {
				t.Errorf("got %+v, expected %+v", got, tc.expected)
			}
		})
	}
}

// TestNetlinkerDump_shutdownWhileHeld: canceling the context while a
// decoder is held mid-packet must still let every netlinker goroutine
// exit once it's released (the harness cleanup waits on the WaitGroup).
func TestNetlinkerDump_shutdownWhileHeld(t *testing.T) {
	c := loadDumpCorpus(t)
	held := make(chan struct{})
	release := make(chan struct{})
	var once sync.Once
	h := newDumpHarness(t, 4, func(_ int, p []byte) {
		if len(p) == len(c.done) {
			return
		}
		once.Do(func() {
			close(held)
			<-release
		})
	})

	tests := []struct {
		description string
		expected    bool
	}{
		{description: "corner: cancel while a decoder is held, then release: all netlinkers exit", expected: true},
	}
	for _, tc := range tests {
		t.Run(tc.description, func(t *testing.T) {
			h.startDump()
			h.send(t, c.dump(2)...)
			select {
			case <-held:
			case <-time.After(dumpDoneTimeout):
				t.Fatal("no packet reached a decoder")
			}
			h.cancel()
			close(release)
			if got := waitGroupTimeout(h.wg, dumpQuiesceWindow); got != tc.expected {
				t.Errorf("netlinkers exited = %v, expected %v", got, tc.expected)
			}
		})
	}
}

// BenchmarkNetlinkerDump measures the time for one large dump, about 100k
// sockets in 1,700 packets of 60 records plus the done packet, from the
// first write until every record has been decoded, by netlinker count.
// Records dropped after the flush still count as decoded, so main and a
// fix do the same decode work. The flush only takes the envelope; its
// rows are recycled with the timer stopped, so the 100k row resets (which
// on main overlap the decoders still running after the early done signal)
// don't count against either side. Compare runs with benchstat.
func BenchmarkNetlinkerDump(b *testing.B) {
	const dataPkts = 1700
	c := loadDumpCorpus(b)
	pkts := c.dump(dataPkts)
	perDump := countRecords(pkts)

	for _, netlinkers := range []uint32{1, 2, 4, 8} {
		b.Run(fmt.Sprintf("netlinkers=%d", netlinkers), func(b *testing.B) {
			h := newDumpHarness(b, netlinkers, nil)
			b.ReportAllocs()
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				h.startDump()
				// The socketpair queue is shorter than a dump, so write
				// from a goroutine while the netlinkers drain it.
				errCh := make(chan error, 1)
				go func() { errCh <- h.write(pkts) }()
				if !h.waitDone(dumpDoneTimeout) {
					b.Fatalf("dump %d: no done signal", i)
				}
				e := h.takeEnvelope()
				h.quiesce(b, (i+1)*perDump)
				if err := <-errCh; err != nil {
					b.Fatal(err)
				}
				b.StopTimer()
				if e != nil {
					h.recycle(e)
				}
				b.StartTimer()
			}
			b.StopTimer()
			b.ReportMetric(float64(perDump), "records/dump")
		})
	}
}

// TestPacketHasDone pins the receiver's end-of-dump detection, which walks
// message headers without decoding bodies.
func TestPacketHasDone(t *testing.T) {
	c := loadDumpCorpus(t)
	// An INET_DIAG header with nlmsg_len 0, followed by a done message the
	// walk must never reach.
	zeroLenThenDone := make([]byte, xtcpnl.NlMsgHdrSizeCst+len(c.done))
	binary.LittleEndian.PutUint16(zeroLenThenDone[4:6], xtcpnl.NlMsgHdrTypeInetDiagCst)
	copy(zeroLenThenDone[xtcpnl.NlMsgHdrSizeCst:], c.done)

	tests := []struct {
		description string
		packet      []byte
		expected    bool
	}{
		{description: "positive: a done-only packet", packet: c.done, expected: true},
		{description: "positive: data then done in one packet", packet: append(c.dataPacket(3), c.done...), expected: true},
		{description: "negative: data only", packet: c.dataPacket(dumpRecordsPerPkt), expected: false},
		{description: "boundary: empty packet", packet: nil, expected: false},
		{description: "boundary: shorter than a header", packet: c.done[:xtcpnl.NlMsgHdrSizeCst-1], expected: false},
		{description: "corner: nlmsg_len 0 stops the walk instead of looping", packet: zeroLenThenDone, expected: false},
		{description: "corner: a done header with a truncated body still counts, as in Deserialize", packet: c.done[:xtcpnl.NlMsgHdrSizeCst], expected: true},
	}
	for _, tc := range tests {
		t.Run(tc.description, func(t *testing.T) {
			if got := packetHasDone(tc.packet); got != tc.expected {
				t.Errorf("packetHasDone = %v, expected %v", got, tc.expected)
			}
		})
	}
}
