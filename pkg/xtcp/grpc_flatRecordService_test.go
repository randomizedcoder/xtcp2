package xtcp

import (
	"context"
	"errors"
	"net"
	"sync"
	"testing"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/test/bufconn"

	"github.com/randomizedcoder/xtcp2/gen/go/xtcp_flat_record"
	"github.com/randomizedcoder/xtcp2/pkg/xsync"
)

// newFlatRecordServiceFixture constructs an xtcpFlatRecordService
// directly with a per-test Prometheus registry, bypassing
// NewXtcpFlatRecordService's global promauto registration.
func newFlatRecordServiceFixture(t *testing.T) *xtcpFlatRecordService {
	t.Helper()
	reg := prometheus.NewRegistry()
	ch := make(chan struct{}, 1)
	s := &xtcpFlatRecordService{
		ctx:           context.Background(),
		pollRequestCh: &ch,
		pC: promauto.With(reg).NewCounterVec(
			prometheus.CounterOpts{Subsystem: "xtcp_grpc_fr_test",
				Name: promNameCounts, Help: "test"},
			promLabels,
		),
		pH: promauto.With(reg).NewSummaryVec(
			prometheus.SummaryOpts{Subsystem: "xtcp_grpc_fr_test",
				Name: promNameHistograms, Help: "test",
				Objectives: map[float64]float64{0.5: quantileError},
				MaxAge:     summaryVecMaxAge},
			promLabels,
		),
	}
	s.FlatRecordsResponsePool = xsync.NewPool(func() *xtcp_flat_record.FlatRecordsResponse {
		return new(xtcp_flat_record.FlatRecordsResponse)
	})
	return s
}

// frMapCount = frStoreCount - frDeleteCount.
func TestFlatRecordService_frMapCount(t *testing.T) {
	s := newFlatRecordServiceFixture(t)
	if got := s.frMapCount(); got != 0 {
		t.Errorf("empty frMapCount = %d, want 0", got)
	}
	s.frStoreCount.Add(7)
	s.frDeleteCount.Add(2)
	if got := s.frMapCount(); got != 5 {
		t.Errorf("frMapCount = %d, want 5", got)
	}
}

// pfrMapCount = pfrStoreCount - pfrDeleteCount.
func TestFlatRecordService_pfrMapCount(t *testing.T) {
	s := newFlatRecordServiceFixture(t)
	if got := s.pfrMapCount(); got != 0 {
		t.Errorf("empty pfrMapCount = %d, want 0", got)
	}
	s.pfrStoreCount.Add(10)
	s.pfrDeleteCount.Add(3)
	if got := s.pfrMapCount(); got != 7 {
		t.Errorf("pfrMapCount = %d, want 7", got)
	}
}

// flatRecordServiceSend on an XTCP with zero registered clients
// follows the early-return path (frMapCount + pfrMapCount both 0).
// The function should not panic and should leave the record alone.
func TestFlatRecordServiceSend_noClients(t *testing.T) {
	reg := prometheus.NewRegistry()
	x := &XTCP{
		flatRecordService: newFlatRecordServiceFixture(t),
	}
	x.pC = promauto.With(reg).NewCounterVec(
		prometheus.CounterOpts{Subsystem: "xtcp_send_test",
			Name: promNameCounts, Help: "test"},
		promLabels,
	)
	x.pH = promauto.With(reg).NewSummaryVec(
		prometheus.SummaryOpts{Subsystem: "xtcp_send_test",
			Name: promNameHistograms, Help: "test",
			Objectives: map[float64]float64{0.5: quantileError},
			MaxAge:     summaryVecMaxAge},
		promLabels,
	)
	x.flatRecordServiceSend(&xtcp_flat_record.XtcpFlatRecord{Hostname: "h"})
	// No panic = pass.
}

// FlatRecords + PollFlatRecords streaming RPC tests via bufconn. The
// bufconn sub-package is anchored in the dependency graph via a blank
// import in bufconn_import.go so buildGoModule includes it in the Nix
// vendored source.

func setupBufconnServer(t *testing.T, s *xtcpFlatRecordService) (*grpc.ClientConn, func()) {
	t.Helper()
	lis := bufconn.Listen(1024 * 1024)
	srv := grpc.NewServer()
	xtcp_flat_record.RegisterXTCPFlatRecordServiceServer(srv, s)
	go func() {
		_ = srv.Serve(lis)
	}()
	dialer := func(_ context.Context, _ string) (net.Conn, error) {
		return lis.Dial()
	}
	conn, err := grpc.NewClient("passthrough://bufconn",
		grpc.WithContextDialer(dialer),
		grpc.WithTransportCredentials(insecure.NewCredentials()),
	)
	if err != nil {
		t.Fatal(err)
	}
	cleanup := func() {
		_ = conn.Close()
		srv.Stop()
	}
	return conn, cleanup
}

func TestFlatRecords_bufconnCancelExits(t *testing.T) {
	srvSvc := newFlatRecordServiceFixture(t)
	conn, cleanup := setupBufconnServer(t, srvSvc)
	defer cleanup()

	client := xtcp_flat_record.NewXTCPFlatRecordServiceClient(conn)
	ctx, cancel := context.WithCancel(t.Context())
	stream, err := client.FlatRecords(ctx, &xtcp_flat_record.FlatRecordsRequest{})
	if err != nil {
		t.Fatalf("FlatRecords: %v", err)
	}
	time.Sleep(50 * time.Millisecond)
	if got := srvSvc.frMapCount(); got != 1 {
		t.Errorf("frMapCount = %d, want 1 after open stream", got)
	}

	// While the stream is open, drive flatRecordServiceSend through the
	// FlatRecordsClients map — exercises the frClientCount > 0 path that
	// the no-clients tests skip.
	reg := prometheus.NewRegistry()
	x := &XTCP{flatRecordService: srvSvc}
	x.pC = promauto.With(reg).NewCounterVec(
		prometheus.CounterOpts{Subsystem: "xtcp_send_buf_test",
			Name: promNameCounts, Help: "test"},
		promLabels,
	)
	x.pH = promauto.With(reg).NewSummaryVec(
		prometheus.SummaryOpts{Subsystem: "xtcp_send_buf_test",
			Name: promNameHistograms, Help: "test",
			Objectives: map[float64]float64{0.5: quantileError},
			MaxAge:     summaryVecMaxAge},
		promLabels,
	)
	x.flatRecordServiceSend(&xtcp_flat_record.XtcpFlatRecord{Hostname: "via-stream"})

	// Verify the client received the record.
	if rerr := stream.RecvMsg(&xtcp_flat_record.FlatRecordsResponse{}); rerr != nil {
		t.Errorf("RecvMsg from stream: %v", rerr)
	}

	cancel()
	_, _ = stream.Recv()
	time.Sleep(50 * time.Millisecond)
	if got := srvSvc.frMapCount(); got != 0 {
		t.Errorf("frMapCount = %d, want 0 after close", got)
	}
}

func TestPollFlatRecords_bufconn(t *testing.T) {
	srvSvc := newFlatRecordServiceFixture(t)
	conn, cleanup := setupBufconnServer(t, srvSvc)
	defer cleanup()

	client := xtcp_flat_record.NewXTCPFlatRecordServiceClient(conn)
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	stream, err := client.PollFlatRecords(ctx)
	if err != nil {
		t.Fatalf("PollFlatRecords: %v", err)
	}
	if err := stream.Send(&xtcp_flat_record.PollFlatRecordsRequest{}); err != nil {
		t.Fatalf("Send: %v", err)
	}
	time.Sleep(50 * time.Millisecond)
	if err := stream.CloseSend(); err != nil && !errors.Is(err, context.Canceled) {
		t.Errorf("CloseSend: %v", err)
	}
	time.Sleep(50 * time.Millisecond)
}

// Same flow as TestPollFlatRecords_bufconn but with debugLevel>10 so the
// io.EOF + send-success log branches fire.
func TestPollFlatRecords_bufconnDebugLog(t *testing.T) {
	srvSvc := newFlatRecordServiceFixture(t)
	srvSvc.debugLevel = 20
	conn, cleanup := setupBufconnServer(t, srvSvc)
	defer cleanup()

	client := xtcp_flat_record.NewXTCPFlatRecordServiceClient(conn)
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	stream, err := client.PollFlatRecords(ctx)
	if err != nil {
		t.Fatalf("PollFlatRecords: %v", err)
	}
	if err := stream.Send(&xtcp_flat_record.PollFlatRecordsRequest{}); err != nil {
		t.Fatalf("Send: %v", err)
	}
	time.Sleep(50 * time.Millisecond)
	if err := stream.CloseSend(); err != nil && !errors.Is(err, context.Canceled) {
		t.Errorf("CloseSend: %v", err)
	}
	time.Sleep(80 * time.Millisecond)
}

// flatRecordServiceSend pfrClients>0 path: open a PollFlatRecords
// bufconn stream so PollFlatRecordsClients has a registered entry,
// then fire flatRecordServiceSend. Exercises the previously-untested
// pfr send loop (which had a type-assertion bug that would have
// panicked — fixed in the same commit).
func TestFlatRecordServiceSend_pfrStream(t *testing.T) {
	srvSvc := newFlatRecordServiceFixture(t)
	srvSvc.debugLevel = 2000 // hit the pfrSend debug branch too
	conn, cleanup := setupBufconnServer(t, srvSvc)
	defer cleanup()

	client := xtcp_flat_record.NewXTCPFlatRecordServiceClient(conn)
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	stream, err := client.PollFlatRecords(ctx)
	if err != nil {
		t.Fatalf("PollFlatRecords: %v", err)
	}
	if err := stream.Send(&xtcp_flat_record.PollFlatRecordsRequest{}); err != nil {
		t.Fatalf("Send: %v", err)
	}
	time.Sleep(80 * time.Millisecond) // let the server-side Store complete
	if got := srvSvc.pfrMapCount(); got != 1 {
		t.Errorf("pfrMapCount = %d, want 1 after open stream", got)
	}

	// Drive flatRecordServiceSend; the pfrClients>0 branch fires and
	// wraps the record in a PollFlatRecordsResponse before sending.
	reg := prometheus.NewRegistry()
	x := &XTCP{flatRecordService: srvSvc}
	x.pC = promauto.With(reg).NewCounterVec(
		prometheus.CounterOpts{Subsystem: "xtcp_send_pfr_test",
			Name: promNameCounts, Help: "test"},
		promLabels,
	)
	x.pH = promauto.With(reg).NewSummaryVec(
		prometheus.SummaryOpts{Subsystem: "xtcp_send_pfr_test",
			Name: promNameHistograms, Help: "test",
			Objectives: map[float64]float64{0.5: quantileError},
			MaxAge:     summaryVecMaxAge},
		promLabels,
	)
	x.debugLevel = 2000
	x.flatRecordServiceSend(&xtcp_flat_record.XtcpFlatRecord{Hostname: "pfr-test"})

	// Verify the client receives the record on the stream.
	resp, rerr := stream.Recv()
	if rerr != nil {
		t.Errorf("Recv: %v", rerr)
	} else if resp.GetXtcpFlatRecord().GetHostname() != "pfr-test" {
		t.Errorf("hostname = %q, want pfr-test", resp.GetXtcpFlatRecord().GetHostname())
	}
}

// TestFlatRecordServiceSend_pfrConcurrent reproduces the production
// condition that the single-threaded tests never exercise: a PollFlatRecords
// stream held open while flatRecordServiceSend fires from many goroutines at
// once (in the daemon, one per parallel Netlinker — see
// ns_createNetlinkersAndStore.go / deserialize.go). gRPC forbids concurrent
// SendMsg on a single stream; without the per-stream send lock these racing
// sends corrupt the stream's HTTP/2 framing (flagged by `go test -race`) and
// records silently fail to arrive — the root cause of `xtcp2client -poll`
// delivering zero records. The client must receive every record and the run
// must be race-clean.
func TestFlatRecordServiceSend_pfrConcurrent(t *testing.T) {
	const senders = 64

	srvSvc := newFlatRecordServiceFixture(t)
	conn, cleanup := setupBufconnServer(t, srvSvc)
	defer cleanup()

	client := xtcp_flat_record.NewXTCPFlatRecordServiceClient(conn)
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	stream, err := client.PollFlatRecords(ctx)
	if err != nil {
		t.Fatalf("PollFlatRecords: %v", err)
	}
	// Send one request so the server-side handler registers the stream in
	// PollFlatRecordsClients before we start broadcasting.
	if err := stream.Send(&xtcp_flat_record.PollFlatRecordsRequest{}); err != nil {
		t.Fatalf("Send: %v", err)
	}
	deadline := time.Now().Add(2 * time.Second)
	for srvSvc.pfrMapCount() != 1 {
		if time.Now().After(deadline) {
			t.Fatalf("pfrMapCount = %d, want 1 after open stream", srvSvc.pfrMapCount())
		}
		time.Sleep(5 * time.Millisecond)
	}

	// Receiver goroutine: count records the client actually receives.
	received := make(chan struct{}, senders)
	go func() {
		for {
			if _, rerr := stream.Recv(); rerr != nil {
				return
			}
			received <- struct{}{}
		}
	}()

	// An XTCP whose flatRecordServiceSend fans out to the open pfr stream.
	reg := prometheus.NewRegistry()
	x := &XTCP{flatRecordService: srvSvc}
	x.pC = promauto.With(reg).NewCounterVec(
		prometheus.CounterOpts{Subsystem: "xtcp_send_conc_test",
			Name: promNameCounts, Help: "test"},
		promLabels,
	)
	x.pH = promauto.With(reg).NewSummaryVec(
		prometheus.SummaryOpts{Subsystem: "xtcp_send_conc_test",
			Name: promNameHistograms, Help: "test",
			Objectives: map[float64]float64{0.5: quantileError},
			MaxAge:     summaryVecMaxAge},
		promLabels,
	)

	// Fire flatRecordServiceSend concurrently — the exact race the daemon
	// hits when many Netlinker goroutines deliver records at the same time.
	var wg sync.WaitGroup
	wg.Add(senders)
	for i := 0; i < senders; i++ {
		go func() {
			defer wg.Done()
			x.flatRecordServiceSend(&xtcp_flat_record.XtcpFlatRecord{Hostname: "conc"})
		}()
	}
	wg.Wait()

	// Every broadcast record must reach the client.
	got := 0
	timeout := time.After(5 * time.Second)
	for got < senders {
		select {
		case <-received:
			got++
		case <-timeout:
			t.Fatalf("received %d/%d records before timeout", got, senders)
		}
	}
}

// frMapCount + pfrMapCount debugLevel>1000 branches are gated by an
// extreme debug threshold; bumping s.debugLevel triggers them.
func TestFlatRecordService_mapCountDebugLog(t *testing.T) {
	s := newFlatRecordServiceFixture(t)
	s.debugLevel = 2000
	_ = s.frMapCount()
	_ = s.pfrMapCount()
}
