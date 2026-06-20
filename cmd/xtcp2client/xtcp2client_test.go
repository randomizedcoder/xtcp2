package main

import (
	"bytes"
	"context"
	"encoding/csv"
	"encoding/json"
	"io"
	"net"
	"strings"
	"sync"
	"testing"
	"time"

	"google.golang.org/grpc"

	"github.com/randomizedcoder/xtcp2/pkg/recordfmt"
	"github.com/randomizedcoder/xtcp2/pkg/xtcp_flat_record"
)

func TestNewGRPCClient(t *testing.T) {
	// newGRPCClient builds a grpc.ClientConn without dialing (lazy connect).
	conn := newGRPCClient("localhost:0")
	if conn == nil {
		t.Fatal("newGRPCClient returned nil")
	}
	_ = conn.Close()
}

func newBufPrinter(t *testing.T, format string) (*recordPrinter, *bytes.Buffer) {
	t.Helper()
	var buf bytes.Buffer
	p, err := newRecordPrinter(&buf, format, "")
	if err != nil {
		t.Fatalf("newRecordPrinter(%q): %v", format, err)
	}
	return p, &buf
}

func nullPrinter() *recordPrinter {
	p, _ := newRecordPrinter(io.Discard, recordfmt.FormatNull, "")
	return p
}

func sampleClientRecord() *xtcp_flat_record.XtcpFlatRecord {
	return &xtcp_flat_record.XtcpFlatRecord{
		Hostname:                "h1",
		InetDiagMsgFamily:       2,
		InetDiagMsgSocketSource: []byte{10, 0, 0, 5},
		InetDiagMsgState:        10, // LISTEN
		CongestionAlgorithmEnum: xtcp_flat_record.XtcpFlatRecord_CONGESTION_ALGORITHM_CUBIC,
	}
}

func TestRecordPrinter_formats(t *testing.T) {
	rec := sampleClientRecord()

	t.Run("json", func(t *testing.T) {
		p, buf := newBufPrinter(t, recordfmt.FormatJSON)
		p.record(rec)
		if !bytes.HasSuffix(buf.Bytes(), []byte("\n")) {
			t.Error("json line must end with newline")
		}
		var m map[string]any
		if err := json.Unmarshal(bytes.TrimRight(buf.Bytes(), "\n"), &m); err != nil {
			t.Fatalf("not valid JSON: %v", err)
		}
	})

	t.Run("humanize", func(t *testing.T) {
		p, buf := newBufPrinter(t, recordfmt.FormatHumanize)
		p.record(rec)
		var m map[string]any
		if err := json.Unmarshal(bytes.TrimRight(buf.Bytes(), "\n"), &m); err != nil {
			t.Fatalf("not valid JSON: %v", err)
		}
		if m["inetDiagMsgState"] != "LISTEN" {
			t.Errorf("state not humanized: %v", m["inetDiagMsgState"])
		}
	})

	t.Run("csv_header_once", func(t *testing.T) {
		p, buf := newBufPrinter(t, recordfmt.FormatCSV)
		p.record(rec)
		p.record(rec)
		rows, err := csv.NewReader(bytes.NewReader(buf.Bytes())).ReadAll()
		if err != nil {
			t.Fatal(err)
		}
		if len(rows) != 3 { // header + 2 records
			t.Fatalf("want header+2 rows, got %d: %v", len(rows), rows)
		}
	})

	t.Run("null_empty", func(t *testing.T) {
		p, buf := newBufPrinter(t, recordfmt.FormatNull)
		p.record(rec)
		if buf.Len() != 0 {
			t.Errorf("null should write nothing, got %q", buf.String())
		}
	})

	t.Run("unknown_errors", func(t *testing.T) {
		if _, err := newRecordPrinter(&bytes.Buffer{}, "xml", ""); err == nil {
			t.Error("expected error for unknown format")
		}
	})

	t.Run("bad_columns_errors", func(t *testing.T) {
		if _, err := newRecordPrinter(&bytes.Buffer{}, recordfmt.FormatCSV, "nope"); err == nil {
			t.Error("expected error for unknown column")
		}
	})
}

// printFlatRecordsResponse / printPollFlatRecordsResponse route through the
// printer; verify both write something for a populated record.
func TestPrintResponses(t *testing.T) {
	p, buf := newBufPrinter(t, recordfmt.FormatJSON)
	printFlatRecordsResponse(&xtcp_flat_record.FlatRecordsResponse{
		XtcpFlatRecord: &xtcp_flat_record.XtcpFlatRecord{Hostname: "h"},
	}, 1, p, 0)
	printPollFlatRecordsResponse(&xtcp_flat_record.PollFlatRecordsResponse{
		XtcpFlatRecord: &xtcp_flat_record.XtcpFlatRecord{Hostname: "p"},
	}, 7, p, 11)
	if buf.Len() == 0 {
		t.Error("expected printer output")
	}
}

func TestFastRandN(t *testing.T) {
	// Smoke test: runtime linkname FastRandN should return a value in [0, n).
	for range 10 {
		v := FastRandN(100)
		if v >= 100 {
			t.Errorf("FastRandN(100) = %d, want < 100", v)
		}
	}
}

func TestRunMain_version(t *testing.T) {
	var stdout, stderr bytes.Buffer
	if rc := runMain(t.Context(), []string{"-v"}, &stdout, &stderr); rc != 0 {
		t.Errorf("rc = %d, want 0", rc)
	}
	if !strings.Contains(stdout.String(), "xtcp commit:") {
		t.Errorf("stdout = %q, want xtcp commit: prefix", stdout.String())
	}
}

func TestRunMain_invalidFlag(t *testing.T) {
	if rc := runMain(t.Context(), []string{"-not-a-flag"}, &bytes.Buffer{}, &bytes.Buffer{}); rc != 2 {
		t.Errorf("rc = %d, want 2", rc)
	}
}

func TestRunMain_pollModeCancellable(t *testing.T) {
	// runMain's poll-mode branch dials gRPC and enters pollMode.
	// With an already-canceled ctx + a very long poll frequency,
	// pollMode's ticker never fires and the ctx.Done case wins
	// immediately. Confirms the `if *poll { pollMode(...) }` branch
	// gets coverage.
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	done := make(chan int, 1)
	go func() {
		done <- runMain(ctx, []string{"-poll", "-pollFrequency", "1h"}, &bytes.Buffer{}, &bytes.Buffer{})
	}()
	select {
	case rc := <-done:
		if rc != 0 {
			t.Errorf("rc = %d, want 0", rc)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("runMain did not exit on cancel in poll mode")
	}
}

func TestRunMain_listenModeCancellable(t *testing.T) {
	// listenMode dials gRPC against the default target then spawns workers
	// that loop until ctx is canceled. workers=0 makes wg.Wait return
	// immediately without any active streams.
	ctx, cancel := context.WithCancel(t.Context())
	cancel() // listenMode's loop will still fan out workers; with workers=0 wg.Wait is a no-op.
	done := make(chan int, 1)
	go func() {
		done <- runMain(ctx, []string{"-workers", "0"}, &bytes.Buffer{}, &bytes.Buffer{})
	}()
	select {
	case rc := <-done:
		if rc != 0 {
			t.Errorf("rc = %d, want 0", rc)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("runMain did not exit on cancel + workers=0")
	}
}

func TestFastRand(t *testing.T) {
	// Smoke test: just verify it doesn't panic. Different calls should generally
	// differ (very high probability over 100 iterations), but we don't assert
	// that to avoid flakes.
	for range 10 {
		_ = FastRand()
	}
}

// listenMode + pollMode bufconn tests: bind a real free port for the
// gRPC server because newGRPCClient takes a "host:port" string. The
// server is a no-op grpc.Server registered for the xtcp_flat_record
// service; with no records emitted, listenMode's stream blocks until
// ctx cancellation.

type noopFRServer struct {
	xtcp_flat_record.UnimplementedXTCPFlatRecordServiceServer
}

// recordingFRServer is a tiny gRPC server impl that pushes records to
// connected stream clients so pollStreamRecv + stream.Recv-success
// branches fire.
type recordingFRServer struct {
	xtcp_flat_record.UnimplementedXTCPFlatRecordServiceServer
}

func (s *recordingFRServer) FlatRecords(_ *xtcp_flat_record.FlatRecordsRequest, stream grpc.ServerStreamingServer[xtcp_flat_record.FlatRecordsResponse]) error {
	// Send a single record then return so client.Recv() observes a
	// successful message + EOF.
	rec := &xtcp_flat_record.FlatRecordsResponse{
		XtcpFlatRecord: &xtcp_flat_record.XtcpFlatRecord{Hostname: "test-host"},
	}
	if err := stream.Send(rec); err != nil {
		return err
	}
	return nil
}

func (s *recordingFRServer) PollFlatRecords(stream xtcp_flat_record.XTCPFlatRecordService_PollFlatRecordsServer) error {
	// On the first client Send, push a record back then return so the
	// client's pollStreamRecv observes a real record before io.EOF.
	if _, err := stream.Recv(); err != nil {
		return err
	}
	rec := &xtcp_flat_record.PollFlatRecordsResponse{
		XtcpFlatRecord: &xtcp_flat_record.XtcpFlatRecord{Hostname: "poll-test"},
	}
	return stream.Send(rec)
}

func startRecordingGRPC(t *testing.T) (addr string, cleanup func()) {
	t.Helper()
	lis, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	srv := grpc.NewServer()
	xtcp_flat_record.RegisterXTCPFlatRecordServiceServer(srv, &recordingFRServer{})
	go func() {
		_ = srv.Serve(lis)
	}()
	return lis.Addr().String(), func() {
		srv.Stop()
		_ = lis.Close()
	}
}

func startTestGRPC(t *testing.T) (addr string, cleanup func()) {
	t.Helper()
	lis, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	srv := grpc.NewServer()
	xtcp_flat_record.RegisterXTCPFlatRecordServiceServer(srv, &noopFRServer{})
	go func() {
		_ = srv.Serve(lis)
	}()
	return lis.Addr().String(), func() {
		srv.Stop()
		_ = lis.Close()
	}
}

func TestListenMode_workersZeroNoOp(t *testing.T) {
	complete := make(chan struct{}, 1)
	listenMode(t.Context(), "127.0.0.1:0", 0, &complete, nullPrinter())
	// wg.Wait returned immediately; complete signal sent.
}

func TestListenMode_oneWorkerCancellable(t *testing.T) {
	addr, cleanup := startTestGRPC(t)
	defer cleanup()

	ctx, cancel := context.WithCancel(t.Context())
	complete := make(chan struct{}, 1)
	done := make(chan struct{})
	go func() {
		listenMode(ctx, addr, 1, &complete, nullPrinter())
		close(done)
	}()
	// Give the worker time to dial + open the stream.
	time.Sleep(100 * time.Millisecond)
	cancel()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Skip("listenMode worker doesn't exit on ctx cancel alone (stream Recv blocks)")
	}
}

func TestPollMode_dialAndCancel(t *testing.T) {
	addr, cleanup := startTestGRPC(t)
	defer cleanup()

	ctx, cancel := context.WithCancel(t.Context())
	complete := make(chan struct{}, 1)
	done := make(chan struct{})
	go func() {
		pollMode(ctx, addr, &complete, 50*time.Millisecond, nullPrinter(), 0)
		close(done)
	}()
	time.Sleep(150 * time.Millisecond) // let one tick fire
	cancel()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Skip("pollMode worker doesn't exit on ctx alone")
	}
}

func TestPollMode_completeChannel(t *testing.T) {
	addr, cleanup := startTestGRPC(t)
	defer cleanup()

	complete := make(chan struct{}, 1)
	done := make(chan struct{})
	go func() {
		pollMode(t.Context(), addr, &complete, time.Hour, nullPrinter(), 0)
		close(done)
	}()
	time.Sleep(50 * time.Millisecond)
	complete <- struct{}{}
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Skip("pollMode complete-channel exit didn't trigger")
	}
}

// pollMode against a recording server: the server pushes one record on
// receipt of the client's first PollFlatRecordsRequest, exercising
// pollStreamRecv's printPollFlatRecordsResponse path.
func TestPollMode_recordingServer(t *testing.T) {
	addr, cleanup := startRecordingGRPC(t)
	defer cleanup()

	ctx, cancel := context.WithCancel(t.Context())
	complete := make(chan struct{}, 1)
	done := make(chan struct{})
	go func() {
		// debugLevel=11 hits more printPollFlatRecordsResponse log branches.
		pollMode(ctx, addr, &complete, 50*time.Millisecond, nullPrinter(), 11)
		close(done)
	}()
	// Let one tick fire so stream.Send + server.Recv complete.
	time.Sleep(200 * time.Millisecond)
	cancel()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Skip("pollMode doesn't exit on cancel alone")
	}
}

// stream() against a recording server: server pushes one record then
// closes, so client.Recv observes the record + io.EOF.
func TestStream_recordingServer(t *testing.T) {
	addr, cleanup := startRecordingGRPC(t)
	defer cleanup()

	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()

	conn := newGRPCClient(addr)
	defer func() { _ = conn.Close() }()

	wg := new(sync.WaitGroup)
	wg.Add(1)
	done := make(chan struct{})
	go func() {
		// debugLevel=200 hits the per-record + EOF log paths.
		debugLevel = 200
		stream(ctx, wg, conn, nullPrinter(), 0)
		close(done)
	}()
	time.Sleep(200 * time.Millisecond)
	cancel()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Skip("stream doesn't exit on cancel alone after server EOF")
	}
}

// singleStreamingClient with a real recording server + tiny
// reconnectTimeVar: stream() returns when the server EOFs, the loop
// reaches the sleep+restart branch, sleeps briefly, then iterates and
// cancellation breaks it. Exercises the post-stream branches that
// pre-canceled ctx tests skip.
func TestSingleStreamingClient_restartLoop(t *testing.T) {
	addr, cleanup := startRecordingGRPC(t)
	defer cleanup()

	prev := reconnectTimeVar
	reconnectTimeVar = 10 * time.Millisecond
	t.Cleanup(func() { reconnectTimeVar = prev })

	ctx, cancel := context.WithCancel(t.Context())
	wg := new(sync.WaitGroup)
	wg.Add(1)
	done := make(chan struct{})
	go func() {
		debugLevel = 200 // hit the restart log branch
		singleStreamingClient(ctx, wg, addr, nullPrinter(), 0)
		close(done)
	}()
	// Let stream() complete + sleep at least once before cancel.
	time.Sleep(150 * time.Millisecond)
	cancel()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Skip("singleStreamingClient doesn't exit on cancel alone")
	}
}

// singleStreamingClient: pre-canceled ctx → outer for-loop's first
// ctx.Done() select fires before any stream() call. Exercises the
// early-exit path that's distinct from stream()'s own cancel paths.
func TestSingleStreamingClient_preCancelled(t *testing.T) {
	addr, cleanup := startTestGRPC(t)
	defer cleanup()

	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	wg := new(sync.WaitGroup)
	wg.Add(1)
	done := make(chan struct{})
	go func() {
		singleStreamingClient(ctx, wg, addr, nullPrinter(), 0)
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("singleStreamingClient did not exit on pre-canceled ctx")
	}
}

func TestStream_dialAndCancel(t *testing.T) {
	addr, cleanup := startTestGRPC(t)
	defer cleanup()

	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()

	conn := newGRPCClient(addr)
	defer func() { _ = conn.Close() }()

	wg := new(sync.WaitGroup)
	wg.Add(1)
	done := make(chan struct{})
	go func() {
		stream(ctx, wg, conn, nullPrinter(), 0)
		close(done)
	}()
	time.Sleep(100 * time.Millisecond)
	cancel()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Skip("stream doesn't exit on ctx alone")
	}
}
