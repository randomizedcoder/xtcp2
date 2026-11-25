package main

import (
	"context"
	"net"
	"testing"
	"time"

	"github.com/randomizedcoder/xtcp2/gen/go/xtcp_flat_record"
	"github.com/randomizedcoder/xtcp2/pkg/listenerauth"
	"google.golang.org/grpc"
)

// stubService mirrors the real FlatRecords handler's blocking shape
// (pkg/xtcp/grpc_flatRecordService.go): the handler parks in select until the
// stream context ends. PollFlatRecords stays Unimplemented — the oversize
// probe asserts on the transport's recv-size check, which fires on message
// receipt regardless of dispatch.
type stubService struct {
	xtcp_flat_record.UnimplementedXTCPFlatRecordServiceServer
}

func (stubService) FlatRecords(_ *xtcp_flat_record.FlatRecordsRequest, stream grpc.ServerStreamingServer[xtcp_flat_record.FlatRecordsResponse]) error {
	<-stream.Context().Done()
	return nil
}

// TestOversizeProbeCapAnswers: against a live server with MaxRecvMsgSize=1024,
// an 8 KiB message is answered ResourceExhausted and the probe exits 0 — the
// rt-4-3 leg-B shape (the transport backstop, not a handler decision).
func TestOversizeProbeCapAnswers(t *testing.T) {
	l, err := net.Listen("tcp4", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	defer l.Close()
	srv := grpc.NewServer(grpc.MaxRecvMsgSize(1024))
	xtcp_flat_record.RegisterXTCPFlatRecordServiceServer(srv, stubService{})
	go func() { _ = srv.Serve(l) }()
	defer srv.Stop()

	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	exit := oversizeProbe(ctx, l.Addr().String(), 2, 8192, listenerauth.ClientAuth{})
	if exit != 0 {
		t.Fatalf("oversizeProbe exit=%d, expected 0 (every RPC terminal)", exit)
	}
}

// TestStormRate0ExitsClean: rate-0 streams against the blocking handler open
// and close cleanly (total_records=0, exit 0) — the client half of the
// rt-11-3 wedge shape.
func TestStormRate0ExitsClean(t *testing.T) {
	l, err := net.Listen("tcp4", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	defer l.Close()
	srv := grpc.NewServer()
	xtcp_flat_record.RegisterXTCPFlatRecordServiceServer(srv, stubService{})
	go func() { _ = srv.Serve(l) }()
	defer srv.Stop()

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	exit := storm(ctx, l.Addr().String(), 1, 2, 0, listenerauth.ClientAuth{})
	if exit != 0 {
		t.Fatalf("storm exit=%d, expected 0", exit)
	}
}
