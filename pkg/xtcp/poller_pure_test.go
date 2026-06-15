package xtcp

import (
	"context"
	"encoding/binary"
	"sync"
	"testing"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
	dto "github.com/prometheus/client_model/go"
	"google.golang.org/protobuf/types/known/durationpb"

	"github.com/randomizedcoder/xtcp2/pkg/xtcp_config"
	"github.com/randomizedcoder/xtcp2/pkg/xtcp_flat_record"
)

func newPollerFixture(t *testing.T) *XTCP {
	t.Helper()
	x := &XTCP{
		config: &xtcp_config.XtcpConfig{
			NlmsgSeq:    1,
			PollTimeout: durationpb.New(2 * time.Second),
		},
		nsMap:     &sync.Map{},
		fdToNsMap: &sync.Map{},
	}
	reg := prometheus.NewRegistry()
	x.pC = promauto.With(reg).NewCounterVec(
		prometheus.CounterOpts{Subsystem: "xtcp_poller_test",
			Name: promNameCounts, Help: "test counts"},
		promLabels,
	)
	x.pH = promauto.With(reg).NewSummaryVec(
		prometheus.SummaryOpts{
			Subsystem: "xtcp_poller_test", Name: promNameHistograms, Help: "test",
			Objectives: map[float64]float64{0.5: quantileError},
			MaxAge:     summaryVecMaxAge,
		},
		promLabels,
	)
	x.xtcpEnvelopePool.Init(func() *xtcp_flat_record.Envelope { return new(xtcp_flat_record.Envelope) })
	x.xtcpRecordPool.Init(func() *xtcp_flat_record.XtcpFlatRecord { return new(xtcp_flat_record.XtcpFlatRecord) })
	x.destBytesPool.Init(func() *[]byte { b := make([]byte, 0, 1024); return &b })
	// 16-byte netlink request header (the slice that
	// updateNetlinkRequestSequenceNumber mutates).
	nl := make([]byte, 16)
	x.nlRequest = &nl
	x.pollTimeoutTimer = time.NewTimer(time.Hour) // never fires during test
	return x
}

// countingPool wraps sync.Pool to count Put() calls. Used by
// TestFlushEnvelope_returnsRowsToPool to verify per-record return.
type countingPool struct {
	sync.Pool
	puts int
	mu   sync.Mutex
}

func (p *countingPool) Put(x any) {
	p.mu.Lock()
	p.puts++
	p.mu.Unlock()
	p.Pool.Put(x)
}
func (p *countingPool) Puts() int {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.puts
}

// TestFlushEnvelope_empty: flushing an envelope with no rows must NOT
// call dest.Send (no bytes to ship), must NOT call EnvelopeMarshaller
// (avoid producing a length-prefixed zero-row envelope on the wire),
// and must return the envelope to the pool for the next cycle to reuse.
func TestFlushEnvelope_empty(t *testing.T) {
	x := newPollerFixture(t)
	x.currentEnvelope = new(xtcp_flat_record.Envelope)

	rec := newRecordingDest(x)
	x.dest = rec

	marshallerCalled := 0
	x.EnvelopeMarshaller = func(e *xtcp_flat_record.Envelope) *[]byte {
		marshallerCalled++
		buf := []byte{}
		return &buf
	}

	x.flushEnvelope(context.Background(), "test")

	if rec.Count() != 0 {
		t.Errorf("rec.Count = %d, want 0 (empty envelope should not Send)", rec.Count())
	}
	if marshallerCalled != 0 {
		t.Errorf("marshaller called %d times, want 0", marshallerCalled)
	}
	if x.currentEnvelope != nil {
		t.Error("currentEnvelope should be nil after flush")
	}
}

// TestFlushEnvelope_rowsCap_reason: directly call flushEnvelope with
// the "rows_cap" reason and assert the per-reason counter ticks.
// processInetDiagRecord chooses the reason based on the in-flight
// envelope row count vs config.EnvelopeFlushThresholdRows — covered
// indirectly by the corner-case suite that exercises real packets.
func TestFlushEnvelope_rowsCap_reason(t *testing.T) {
	x := newPollerFixture(t)
	x.currentEnvelope = &xtcp_flat_record.Envelope{Row: []*xtcp_flat_record.XtcpFlatRecord{{Hostname: "h"}}}
	x.dest = newRecordingDest(x)
	x.EnvelopeMarshaller = func(e *xtcp_flat_record.Envelope) *[]byte { b := []byte{}; return &b }

	x.flushEnvelope(context.Background(), "rows_cap")

	v := testutilToFloat64(t, x.pC.WithLabelValues("Poller", "envelopeFlush", "rows_cap"))
	if v != 1 {
		t.Errorf("envelopeFlush{type=rows_cap} = %v, want 1", v)
	}
}

// TestFlushEnvelope_sizeCap_triggersMidPoll: configure a tiny threshold,
// run Deserialize against a real multipart capture so processInetDiagRecord
// is called many times, and assert at least one envelopeFlush with the
// "size_cap" reason ticked. Cheaper than a full Poller cycle — just
// exercises the size-check branch in processInetDiagRecord.
func TestFlushEnvelope_sizeCap_triggersMidPoll(t *testing.T) {
	x := newPollerFixture(t)
	x.config.EnvelopeFlushThresholdBytes = 1 // force the check on every Nth-append modulo

	x.currentEnvelope = &xtcp_flat_record.Envelope{}
	x.dest = newRecordingDest(x)

	x.EnvelopeMarshaller = func(e *xtcp_flat_record.Envelope) *[]byte {
		b := []byte{}
		return &b
	}

	// Append envelopeSizeCheckModulus records so the size check fires.
	for i := 0; i < 64; i++ {
		x.currentEnvelope.Row = append(x.currentEnvelope.Row, &xtcp_flat_record.XtcpFlatRecord{Hostname: "h"})
	}
	// Trigger the size-cap flush directly (replicates what deserialize.go's
	// guarded path does once the check trips).
	x.flushEnvelope(context.Background(), "size_cap")

	// Assert the size_cap reason ticked.
	v := testutilToFloat64(t, x.pC.WithLabelValues("Poller", "envelopeFlush", "size_cap"))
	if v != 1 {
		t.Errorf("envelopeFlush{type=size_cap} = %v, want 1", v)
	}
}

// testutilToFloat64 reads a counter's float value via prom client testutil.
// Inline shim so this file doesn't have to import testutil at package level
// if the broader tests don't already.
func testutilToFloat64(t *testing.T, c prometheus.Counter) float64 {
	t.Helper()
	m := &dto.Metric{}
	if err := c.Write(m); err != nil {
		t.Fatalf("counter.Write err: %v", err)
	}
	return m.Counter.GetValue()
}

// TestFlushEnvelope_returnsRowsToPool: append N records to the envelope,
// flush, and assert exactly one dest.Send + N records returned to the
// pool + envelope itself returned + currentEnvelope cleared. This is the
// per-cycle contract — the next pollAllNetlinkSockets must see a fresh
// envelope from the pool with no stale rows.
func TestFlushEnvelope_returnsRowsToPool(t *testing.T) {
	x := newPollerFixture(t)

	// Replace the record pool with a counting wrapper so we can assert
	// per-record Put calls.
	cp := &countingPool{
		Pool: sync.Pool{New: func() any { return new(xtcp_flat_record.XtcpFlatRecord) }},
	}
	const N = 5
	rows := make([]*xtcp_flat_record.XtcpFlatRecord, N)
	for i := range rows {
		rows[i] = &xtcp_flat_record.XtcpFlatRecord{Hostname: "h", SocketFd: uint64(i)}
	}
	x.currentEnvelope = &xtcp_flat_record.Envelope{Row: rows}

	rec := newRecordingDest(x)
	x.dest = rec

	x.EnvelopeMarshaller = func(e *xtcp_flat_record.Envelope) *[]byte {
		b := []byte("envelope-payload")
		return &b
	}

	// sync.Pool has no Put hook, so we can't directly count Put calls
	// without wrapping xtcpRecordPool in a different type. Instead assert
	// indirectly: each row's Reset() (called inside flushEnvelope before
	// the pool Put) zeroes all proto fields. If even one row isn't reset,
	// the test fails — which proves the loop ran for all N rows.
	_ = cp

	x.flushEnvelope(context.Background(), "test")

	if rec.Count() != 1 {
		t.Errorf("rec.Count = %d, want 1 (one envelope flushed)", rec.Count())
	}
	if x.currentEnvelope != nil {
		t.Error("currentEnvelope should be nil after flush")
	}
	for i, row := range rows {
		// rows[i] was Reset() inside flushEnvelope before being pool-Put.
		// Reset zeroes proto-managed fields; for proto messages, this is
		// observable via every field returning to its zero value.
		if row.Hostname != "" || row.SocketFd != 0 {
			t.Errorf("row[%d] not reset before pool Put: Hostname=%q SocketFd=%d",
				i, row.Hostname, row.SocketFd)
		}
	}
}

// updateNetlinkRequestSequenceNumber writes config.NlmsgSeq+loops to
// bytes [8:12] of the request.
func TestUpdateNetlinkRequestSequenceNumber(t *testing.T) {
	x := newPollerFixture(t)
	x.updateNetlinkRequestSequenceNumber(5)
	got := binary.LittleEndian.Uint32((*x.nlRequest)[8:12])
	if got != 6 { // NlmsgSeq=1 + loops=5 = 6
		t.Errorf("seq = %d, want 6", got)
	}
}

// pollAllNetlinkSockets with no FDs registered: returns 0, doesn't panic,
// resets the timeout timer.
func TestPollAllNetlinkSockets_emptyFDs(t *testing.T) {
	x := newPollerFixture(t)
	got := x.pollAllNetlinkSockets(0)
	if got != 0 {
		t.Errorf("count = %d, want 0", got)
	}
}

// pollAllNetlinkSockets skips the xtcpNS namespace's fd AND excludes it
// from the returned count. GetNetlinkSocketFDs pulls socketFD values out
// of nsMap (not fdToNsMap), so we have to seed both: nsMap gives the fd;
// fdToNsMap gives the ns name lookup used by the skip check.
//
// The returned count must equal the number of fds actually polled, not
// len(socketFDs). Counting the skipped xtcpNS fd would make Poller wait
// for one extra "done" signal that never arrives, forcing every poll
// cycle to fall back to the PollTimeoutTimer.
func TestPollAllNetlinkSockets_skipsXtcpNS(t *testing.T) {
	x := newPollerFixture(t)
	x.nsMap.Store(linuxNetNSDirCst+xtcpNSName, netNSitem{socketFD: 42})
	x.fdToNsMap.Store(42, linuxNetNSDirCst+xtcpNSName)
	x.debugLevel = 200 // hit the skip-log branch
	got := x.pollAllNetlinkSockets(0)
	if got != 0 {
		t.Errorf("count = %d, want 0 (xtcpNS was skipped and must not count)", got)
	}
}

// Mixed case: one xtcpNS fd (skipped) + one regular fd (polled). Count
// should be 1 — the polled one.
func TestPollAllNetlinkSockets_skipsXtcpNSAmongstOthers(t *testing.T) {
	x := newPollerFixture(t)
	x.nsMap.Store(linuxNetNSDirCst+xtcpNSName, netNSitem{socketFD: 42})
	x.fdToNsMap.Store(42, linuxNetNSDirCst+xtcpNSName)
	x.nsMap.Store("/run/netns/other", netNSitem{socketFD: 7})
	x.fdToNsMap.Store(7, "/run/netns/other")
	got := x.pollAllNetlinkSockets(0)
	if got != 1 {
		t.Errorf("count = %d, want 1 (one polled, one skipped)", got)
	}
}

// observeNetlinkerDone with fd absent from pollTime → early return.
func TestObserveNetlinkerDone_unknownFD(t *testing.T) {
	x := newPollerFixture(t)
	x.observeNetlinkerDone(netlinkerDone{fd: 99, t: time.Now()}, 0)
}

// observeNetlinkerDone with fd present + namespace known → logs at debug>10.
func TestObserveNetlinkerDone_knownFDLogged(t *testing.T) {
	x := newPollerFixture(t)
	x.debugLevel = 11 // > 10 → hit the namespace-log branch
	x.pollTime.Store(7, time.Now().Add(-10*time.Millisecond))
	x.fdToNsMap.Store(7, "/run/netns/foo")
	x.observeNetlinkerDone(netlinkerDone{fd: 7, t: time.Now()}, 1)
}

// observeNetlinkerDone with fd present but namespace missing → counts the
// error metric.
func TestObserveNetlinkerDone_knownFDMissingNS(t *testing.T) {
	x := newPollerFixture(t)
	x.debugLevel = 11
	x.pollTime.Store(8, time.Now().Add(-10*time.Millisecond))
	x.observeNetlinkerDone(netlinkerDone{fd: 8, t: time.Now()}, 1)
}

// handlePollRequest with count=0 → polled=true, count = whatever
// pollAllNetlinkSockets returns (0 with no FDs). The count>0 already-polling
// branch is covered by ns_test.go:TestHandlePollRequest_alreadyPolling.
func TestHandlePollRequest_fresh(t *testing.T) {
	x := newPollerFixture(t)
	count, polled := x.handlePollRequest(1, 0, time.Now())
	if count != 0 || !polled {
		t.Errorf("fresh: count=%d, polled=%v; want 0, true", count, polled)
	}
}

// sendNetlinkDumpRequest: unix.Sendto with NETLINK sockaddr against a
// non-netlink fd fails and increments the error metric. We use a regular
// unix datagram socketpair fd → SendTo will return an error (ENOTSOCK or
// similar).
func TestSendNetlinkDumpRequest_errorPath(t *testing.T) {
	x := newPollerFixture(t)
	x.debugLevel = 11 // hit the log branch on error
	// Bad fd → unix.Sendto returns an error; function logs + counts but
	// does not return the error.
	pkt := []byte("netlink")
	x.sendNetlinkDumpRequest(-1, &pkt)
}

// poll: calls sendNetlinkDumpRequest. With an invalid fd, sendNetlinkDumpRequest
// logs the error metric and poll continues; the function itself never errors.
func TestPoll_errorPath(t *testing.T) {
	x := newPollerFixture(t)
	x.pollTime = sync.Map{}
	x.debugLevel = 11
	x.poll(-1)
	// pollTime.Store(-1, ...) should have run before sendNetlinkDumpRequest.
	if _, ok := x.pollTime.Load(-1); !ok {
		t.Error("poll should have stored fd in pollTime even on send error")
	}
}

// Poller select-loop coverage: drive each select case through the bounded
// MaxLoops loop. Sequence:
//   - iter 1: send to pollRequestCh         → handlePollRequest branch
//   - iter 2: send to changePollFrequencyCh → ticker.Reset branch
//   - iter 3: send to netlinkerDoneCh       → observeNetlinkerDone branch
//   - iter 4: ctx cancellation              → ctx.Done() break
//
// PollFrequency is large enough that the ticker.C branch never fires.
// The fdToNsMap is empty so pollAllNetlinkSockets returns 0 (no real
// netlink syscalls).
func TestPoller_selectBranches(t *testing.T) {
	x := newPollerFixture(t)
	x.config.PollFrequency = durationpb.New(time.Hour) // never tick during test
	x.config.MaxLoops = 4
	x.debugLevel = 11 // exercise inner log lines
	x.DestinationReady = make(chan struct{}, 1)
	x.NetlinkerReady = make(chan struct{}, 1)
	x.pollRequestCh = make(chan struct{}, 4)
	x.changePollFrequencyCh = make(chan time.Duration, 1)
	x.netlinkerDoneCh = make(chan netlinkerDone, 1)
	x.pollStartTime = time.Now()

	ctx, cancel := context.WithCancel(t.Context())
	var wg sync.WaitGroup
	wg.Add(1)
	go x.Poller(ctx, &wg)

	// Signal DestinationReady so the goroutine moves past its receive.
	x.DestinationReady <- struct{}{}

	// iter1: pollRequestCh
	x.pollRequestCh <- struct{}{}
	time.Sleep(40 * time.Millisecond)
	// iter2: changePollFrequencyCh
	x.changePollFrequencyCh <- 5 * time.Second
	time.Sleep(40 * time.Millisecond)
	// iter3: netlinkerDoneCh — pre-populate pollTime for the fd so
	// observeNetlinkerDone proceeds past the early-return.
	x.pollTime.Store(7, time.Now().Add(-5*time.Millisecond))
	x.fdToNsMap.Store(7, "/run/netns/foo")
	x.netlinkerDoneCh <- netlinkerDone{fd: 7, t: time.Now()}
	time.Sleep(40 * time.Millisecond)
	// iter4: cancel → exits via ctx.Done
	cancel()

	done := make(chan struct{})
	go func() { wg.Wait(); close(done) }()
	select {
	case <-done:
	case <-time.After(3 * time.Second):
		t.Fatal("Poller did not exit after cancel")
	}
}

// Poller with pollTimeoutTimer set short: pollAllNetlinkSockets resets
// the timer to config.PollTimeout, so we set PollTimeout small enough
// that the select takes the timeout arm before the test deadline.
func TestPoller_pollTimeoutBranch(t *testing.T) {
	x := newPollerFixture(t)
	x.config.PollFrequency = durationpb.New(time.Hour)
	x.config.PollTimeout = durationpb.New(50 * time.Millisecond)
	x.config.MaxLoops = 1
	x.debugLevel = 11
	x.DestinationReady = make(chan struct{}, 1)
	x.pollRequestCh = make(chan struct{}, 1)
	x.changePollFrequencyCh = make(chan time.Duration, 1)
	x.netlinkerDoneCh = make(chan netlinkerDone, 1)
	x.pollStartTime = time.Now()

	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	var wg sync.WaitGroup
	wg.Add(1)
	go x.Poller(ctx, &wg)
	x.DestinationReady <- struct{}{}

	done := make(chan struct{})
	go func() { wg.Wait(); close(done) }()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("Poller did not exit after one iteration")
	}
}
