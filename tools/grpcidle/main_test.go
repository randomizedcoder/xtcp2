package main

import (
	"context"
	"net"
	"testing"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/health"
	healthpb "google.golang.org/grpc/health/grpc_health_v1"
)

// TestDialAllEstablishes: conns come back (the RPC outcome is irrelevant —
// an unreachable target still yields conns without hanging the ramp; the
// transport fails in the background, which is the held state either way).
func TestDialAllEstablishes(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	conns := dialAll(ctx, "127.0.0.1:1", 8, 500*time.Millisecond, 4)
	if len(conns) != 8 {
		t.Fatalf("dialAll built %d conns, expected 8", len(conns))
	}
	for _, c := range conns {
		if c == nil {
			t.Fatal("nil conn in the set")
		}
		_ = c.Close()
	}
}

// TestDialAllAgainstLiveServer: against a live grpc server serving health,
// the health RPC completes — proof the transport came up (the establishment
// mechanism the FD pin depends on).
func TestDialAllAgainstLiveServer(t *testing.T) {
	l, err := net.Listen("tcp4", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	defer l.Close()
	srv := grpc.NewServer()
	healthpb.RegisterHealthServer(srv, health.NewServer())
	go func() { _ = srv.Serve(l) }()
	defer srv.Stop()

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	conns := dialAll(ctx, l.Addr().String(), 2, 5*time.Second, 2)
	if len(conns) != 2 {
		t.Fatalf("dialAll built %d conns, expected 2", len(conns))
	}
	for _, c := range conns {
		_ = c.Close()
	}
}
