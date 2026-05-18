package xtcp

import (
	"encoding/binary"
	"sync"
	"testing"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
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
	x.xtcpEnvelopePool = sync.Pool{
		New: func() any { return new(xtcp_flat_record.Envelope) },
	}
	// 16-byte netlink request header (the slice that
	// updateNetlinkRequestSequenceNumber mutates).
	nl := make([]byte, 16)
	x.nlRequest = &nl
	x.pollTimeoutTimer = time.NewTimer(time.Hour) // never fires during test
	return x
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

// pollAllNetlinkSockets skips the xtcpNS namespace's fd. GetNetlinkSocketFDs
// pulls socketFD values out of nsMap (not fdToNsMap), so we have to seed
// both: nsMap gives the fd; fdToNsMap gives the ns name lookup used by
// the skip check.
func TestPollAllNetlinkSockets_skipsXtcpNS(t *testing.T) {
	x := newPollerFixture(t)
	x.nsMap.Store(linuxNetNSDirCst+xtcpNSName, netNSitem{socketFD: 42})
	x.fdToNsMap.Store(42, linuxNetNSDirCst+xtcpNSName)
	x.debugLevel = 200 // hit the skip-log branch
	got := x.pollAllNetlinkSockets(0)
	if got != 1 { // 1 socket in nsMap, but it was skipped (not polled)
		t.Errorf("count = %d, want 1", got)
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
