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
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials/insecure"
	healthpb "google.golang.org/grpc/health/grpc_health_v1"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/types/known/durationpb"

	linkmonitorpb "github.com/randomizedcoder/xtcp2/gen/go/linkmonitor"
	"github.com/randomizedcoder/xtcp2/gen/go/xtcp_config"
	"github.com/randomizedcoder/xtcp2/gen/go/xtcp_flat_record"
	"github.com/randomizedcoder/xtcp2/pkg/listener"
	"github.com/randomizedcoder/xtcp2/pkg/listeners"
)

// newGRPCServerFixture is an XTCP with what startGRPCServers needs, on a
// fresh registry so the service constructors' metrics never collide.
func newGRPCServerFixture(t *testing.T) *XTCP {
	t.Helper()
	reg := prometheus.NewRegistry()
	x := &XTCP{
		registry: reg,
		config: &xtcp_config.XtcpConfig{
			PollFrequency:          durationpb.New(time.Second),
			PollTimeout:            durationpb.New(time.Second),
			NetlinkersDoneChanSize: 1,
		},
		fatalf: func(format string, args ...any) { t.Fatalf(format, args...) },
	}
	x.pC = promauto.With(reg).NewCounterVec(
		prometheus.CounterOpts{Subsystem: "xtcp_grpc_server_test", Name: promNameCounts, Help: "test counts"},
		promLabels,
	)
	x.pollRequestCh = make(chan struct{}, 1)
	x.changePollFrequencyCh = make(chan time.Duration, 1)
	x.storeCount = atomic.Uint64{}
	x.generation = atomic.Uint64{}
	x.deleteCount = atomic.Uint64{}
	return x
}

// bindGRPC binds one gRPC listener on a socket in a temp dir with auth
// disabled. It builds the Spec directly rather than through
// listeners.Resolve, which (rightly) refuses grpc-admin without an admin
// credential: this table exercises registration, not credentials.
func bindGRPC(t *testing.T, serves string) (GRPCListener, string) {
	t.Helper()
	sock := filepath.Join(t.TempDir(), "grpc.sock")
	cls, err := listeners.ParseClass(serves)
	if err != nil {
		t.Fatal(err)
	}
	s := listeners.Spec{
		Name:      "grpc",
		Endpoint:  listener.Endpoint{Network: listener.NetworkUnix, Address: sock, UnixSocketMode: 0o660, UnlinkStaleUnixSocket: true},
		Auth:      &xtcp_config.ListenerAuth{Mode: xtcp_config.ListenerAuthMode_LISTENER_AUTH_MODE_DISABLED},
		AdminAuth: &xtcp_config.ListenerAuth{},
		Classes:   []xtcp_config.EndpointClass{cls},
	}
	ln, err := s.Listen(context.Background(), nil)
	if err != nil {
		t.Fatal(err)
	}
	return GRPCListener{Spec: s, Listener: ln}, sock
}

func dialUnix(t *testing.T, sock string) *grpc.ClientConn {
	t.Helper()
	conn, err := grpc.NewClient("passthrough:///xtcp2-grpc-test",
		grpc.WithContextDialer(func(ctx context.Context, _ string) (net.Conn, error) {
			var d net.Dialer
			return d.DialContext(ctx, "unix", sock)
		}),
		grpc.WithTransportCredentials(insecure.NewCredentials()),
	)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = conn.Close() })
	return conn
}

// TestStartGRPCServers covers what a gRPC listener serves by class. A
// grpc-read listener registers read-only adapters, so every admin RPC is
// Unimplemented there whatever the auth policy; grpc-admin registers all.
func TestStartGRPCServers(t *testing.T) {
	type rpc func(ctx context.Context, conn *grpc.ClientConn) error
	health := func(ctx context.Context, c *grpc.ClientConn) error {
		_, err := healthpb.NewHealthClient(c).Check(ctx, &healthpb.HealthCheckRequest{})
		return err
	}
	get := func(ctx context.Context, c *grpc.ClientConn) error {
		_, err := xtcp_config.NewConfigServiceClient(c).Get(ctx, &xtcp_config.GetRequest{})
		return err
	}
	setFreq := func(ctx context.Context, c *grpc.ClientConn) error {
		_, err := xtcp_config.NewConfigServiceClient(c).SetPollFrequency(ctx,
			&xtcp_config.SetPollFrequencyRequest{PollFrequency: durationpb.New(2 * time.Second), PollTimeout: durationpb.New(time.Second)})
		return err
	}
	rebaseline := func(ctx context.Context, c *grpc.ClientConn) error {
		_, err := linkmonitorpb.NewLinkMonitorServiceClient(c).Rebaseline(ctx, &linkmonitorpb.RebaselineRequest{})
		return err
	}
	pollRecords := func(ctx context.Context, c *grpc.ClientConn) error {
		s, err := xtcp_flat_record.NewXTCPFlatRecordServiceClient(c).PollFlatRecords(ctx)
		if err != nil {
			return err
		}
		_, err = s.Recv()
		return err
	}
	cases := []struct {
		name        string
		category    string
		description string
		serves      string
		call        rpc
		expected    codes.Code // codes.OK = anything but Unimplemented
	}{
		{"read_health", "positive", "grpc-read serves health", "grpc-read", health, codes.OK},
		{"read_get", "positive", "grpc-read serves ConfigService.Get", "grpc-read", get, codes.OK},
		{"read_set_unimplemented", "negative", "grpc-read has no SetPollFrequency handler", "grpc-read", setFreq, codes.Unimplemented},
		{"read_rebaseline_unimplemented", "negative", "grpc-read has no Rebaseline handler", "grpc-read", rebaseline, codes.Unimplemented},
		{"read_poll_unimplemented", "corner", "PollFlatRecords drives polls, so it is admin", "grpc-read", pollRecords, codes.Unimplemented},
		{"admin_set", "positive", "grpc-admin serves SetPollFrequency", "grpc-admin", setFreq, codes.OK},
		{"admin_get", "boundary", "grpc-admin also serves the read methods", "grpc-admin", get, codes.OK},
	}
	for _, tc := range cases {
		t.Run(tc.category+"/"+tc.name, func(t *testing.T) {
			x := newGRPCServerFixture(t)
			l, sock := bindGRPC(t, tc.serves)
			x.SetGRPCListeners([]GRPCListener{l})
			ctx, cancel := context.WithCancel(context.Background())
			t.Cleanup(cancel)
			x.startGRPCServers(ctx)

			callCtx, callCancel := context.WithTimeout(ctx, 2*time.Second)
			defer callCancel()
			err := tc.call(callCtx, dialUnix(t, sock))
			got := status.Code(err)
			if tc.expected == codes.Unimplemented {
				if got != codes.Unimplemented {
					t.Fatalf("%s: code = %v (%v), want Unimplemented", tc.description, got, err)
				}
				return
			}
			if got == codes.Unimplemented {
				t.Fatalf("%s: Unimplemented, want the method served (%v)", tc.description, err)
			}
		})
	}
}

// TestStartGRPCServers_none: with no gRPC listener there is no server and no
// flat-record service, and the poll path's send is a no-op, not a panic.
func TestStartGRPCServers_none(t *testing.T) {
	x := newGRPCServerFixture(t)
	x.startGRPCServers(context.Background())
	if x.flatRecordService != nil || x.grpcHealth != nil {
		t.Fatalf("services built with no gRPC listener: %v %v", x.flatRecordService, x.grpcHealth)
	}
	x.flatRecordServiceSend(&xtcp_flat_record.XtcpFlatRecord{})
	x.setReady(true) // nil grpcHealth is fine
}

// TestStartGRPCServers_shutdown: canceling the run stops the server and the
// Unix socket is removed.
func TestStartGRPCServers_shutdown(t *testing.T) {
	x := newGRPCServerFixture(t)
	l, sock := bindGRPC(t, "grpc-read")
	x.SetGRPCListeners([]GRPCListener{l})
	ctx, cancel := context.WithCancel(context.Background())
	x.startGRPCServers(ctx)
	st, err := os.Lstat(sock)
	if err != nil || st.Mode().Perm() != 0o660 {
		t.Fatalf("socket %s: %v mode %v, want 0660", sock, err, st)
	}
	cancel()
	deadline := time.Now().Add(2 * time.Second)
	for {
		if _, err := os.Lstat(sock); os.IsNotExist(err) {
			return
		}
		if time.Now().After(deadline) {
			t.Fatal("socket still present after shutdown")
		}
		time.Sleep(10 * time.Millisecond)
	}
}
