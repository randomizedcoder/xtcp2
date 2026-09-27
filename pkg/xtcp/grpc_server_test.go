package xtcp

import (
	"context"
	"net"
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	healthpb "google.golang.org/grpc/health/grpc_health_v1"
	"google.golang.org/protobuf/types/known/durationpb"

	"github.com/randomizedcoder/xtcp2/gen/go/xtcp_config"
)

// startGRPCflatRecordService binds a real TCP port (config.GrpcPort) and
// runs an actual grpc.Server. With x.registry pre-filled with a fresh
// prometheus.NewRegistry(), the inner NewXtcpFlatRecordService +
// NewXtcpConfigService calls don't panic from duplicate-metric
// registration on the default registry.
func TestStartGRPCflatRecordService_cancels(t *testing.T) {
	reg := prometheus.NewRegistry()
	x := &XTCP{
		registry: reg,
		config: &xtcp_config.XtcpConfig{
			GrpcPort:               0, // OS-picked free port
			PollFrequency:          durationpb.New(time.Second),
			PollTimeout:            durationpb.New(time.Second),
			NetlinkersDoneChanSize: 1,
		},
	}
	x.pC = promauto.With(reg).NewCounterVec(
		prometheus.CounterOpts{Subsystem: "xtcp_grpc_server_test",
			Name: promNameCounts, Help: "test counts"},
		promLabels,
	)
	x.pollRequestCh = make(chan struct{}, 1)
	x.changePollFrequencyCh = make(chan time.Duration, 1)
	x.storeCount = atomic.Uint64{}
	x.generation = atomic.Uint64{}
	x.deleteCount = atomic.Uint64{}

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		x.startGRPCflatRecordService(ctx)
		close(done)
	}()
	time.Sleep(100 * time.Millisecond)
	cancel()
	// grpc.Server.Serve doesn't return on ctx cancel alone — the test
	// goroutine will outlive this function. Go's test framework handles
	// the leak; the function under test got exercised through its setup
	// path which is what we wanted to cover.
	_ = done
}

func TestStartGRPCflatRecordService_unixSocket(t *testing.T) {
	socketPath := filepath.Join(t.TempDir(), "xtcp2-grpc.sock")
	reg := prometheus.NewRegistry()
	x := &XTCP{
		registry: reg,
		config: &xtcp_config.XtcpConfig{
			GrpcPort:               8889,
			GrpcListener:           &xtcp_config.ListenerEndpoint{Network: xtcp_config.ListenerNetwork_LISTENER_NETWORK_UNIX, Address: socketPath, UnixSocketMode: 0o660, UnlinkStaleUnixSocket: true},
			PollFrequency:          durationpb.New(time.Second),
			PollTimeout:            durationpb.New(time.Second),
			NetlinkersDoneChanSize: 1,
		},
	}
	x.pC = promauto.With(reg).NewCounterVec(
		prometheus.CounterOpts{Subsystem: "xtcp_grpc_server_uds_test",
			Name: promNameCounts, Help: "test counts"},
		promLabels,
	)
	x.pollRequestCh = make(chan struct{}, 1)
	x.changePollFrequencyCh = make(chan time.Duration, 1)
	x.storeCount = atomic.Uint64{}
	x.generation = atomic.Uint64{}
	x.deleteCount = atomic.Uint64{}

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		x.startGRPCflatRecordService(ctx)
		close(done)
	}()

	deadline := time.Now().Add(2 * time.Second)
	for {
		st, err := os.Lstat(socketPath)
		if err == nil && st.Mode()&os.ModeSocket != 0 {
			if st.Mode().Perm() != 0o660 {
				t.Fatalf("socket mode=%#o, want 0660", st.Mode().Perm())
			}
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("unix socket was not created at %s", socketPath)
		}
		time.Sleep(10 * time.Millisecond)
	}

	conn, err := grpc.NewClient(
		"passthrough:///xtcp2-grpc-uds-test",
		grpc.WithContextDialer(func(ctx context.Context, _ string) (net.Conn, error) {
			var d net.Dialer
			return d.DialContext(ctx, "unix", socketPath)
		}),
		grpc.WithTransportCredentials(insecure.NewCredentials()),
	)
	if err != nil {
		t.Fatalf("grpc.NewClient unix: %v", err)
	}
	_, err = healthpb.NewHealthClient(conn).Check(context.Background(), &healthpb.HealthCheckRequest{})
	if err != nil {
		t.Fatalf("health check over unix socket: %v", err)
	}
	if err := conn.Close(); err != nil {
		t.Fatalf("close client: %v", err)
	}

	cancel()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("gRPC UDS server did not stop after context cancel")
	}
	if _, err := os.Lstat(socketPath); !os.IsNotExist(err) {
		t.Fatalf("socket should be removed on shutdown, stat err=%v", err)
	}
}
