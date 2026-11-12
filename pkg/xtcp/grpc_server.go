package xtcp

import (
	"context"
	"errors"
	"log"
	"net"
	"time"

	linkmonitorpb "github.com/randomizedcoder/xtcp2/gen/go/linkmonitor"
	"github.com/randomizedcoder/xtcp2/gen/go/xtcp_config"
	"github.com/randomizedcoder/xtcp2/gen/go/xtcp_flat_record"
	"github.com/randomizedcoder/xtcp2/pkg/health"
	"github.com/randomizedcoder/xtcp2/pkg/listeners"
	"google.golang.org/grpc"
	_ "google.golang.org/grpc/encoding/gzip"
	grpchealth "google.golang.org/grpc/health"
	healthpb "google.golang.org/grpc/health/grpc_health_v1"
	"google.golang.org/grpc/keepalive"
	"google.golang.org/grpc/reflection"
)

// PLEASE NOTE by importing GRPC encoding, this will register the compression.
// _ "google.golang.org/grpc/encoding/gzip"
// https://github.com/grpc/grpc-go/blob/master/encoding/gzip/gzip.go#L42

const (
	ReadBufferSize  = 64 * 1000
	WriteBufferSize = 64 * 1000

	MaxRecvMsgSize = 4 * 1000 * 1000

	MaxConcurrentStreams = 20

	// default 2 hours, setting to less than 5 minutes
	KeepaliveTime = 299 * time.Second
	// default 20s
	KeepaliveTimeout  = 20 * time.Second
	MaxConnectionIdle = 15 * time.Minute

	KeepaliveMinTime = 60 * time.Second
)

// GRPCListener is a bound listener that serves gRPC endpoint classes. The
// caller (cmd/xtcp2) binds every listener before the daemon starts, so a bad
// address fails startup, and hands the gRPC ones to SetGRPCListeners.
type GRPCListener struct {
	Spec     listeners.Spec
	Listener net.Listener
}

// SetGRPCListeners gives the daemon the gRPC listeners to serve. Call it
// after NewXTCP and before Run. With none, no gRPC server runs: a class no
// listener names is not served.
func (x *XTCP) SetGRPCListeners(ls []GRPCListener) {
	x.grpcListeners = ls
}

// grpcServerOptions is the transport tuning every gRPC listener shares.
func grpcServerOptions() []grpc.ServerOption {
	return []grpc.ServerOption{
		grpc.ReadBufferSize(ReadBufferSize),
		grpc.WriteBufferSize(WriteBufferSize),
		// https://pkg.go.dev/google.golang.org/grpc#MaxRecvMsgSize
		// https://github.com/grpc/grpc-go/blob/0fe49e823fcd9904afba6cd5e5980da4390d1899/server.go#L58
		grpc.MaxRecvMsgSize(MaxRecvMsgSize),
		// GRPCMaxConcurrentStreams comes from here
		// https://github.com/grpc/grpc-go/blob/87eb5b7502493f758e76c4d09430c0049a81a557/internal/transport/defaults.go#L26
		grpc.MaxConcurrentStreams(MaxConcurrentStreams),
		grpc.KeepaliveParams(
			keepalive.ServerParameters{
				Time:              KeepaliveTime,
				Timeout:           KeepaliveTimeout,
				MaxConnectionIdle: MaxConnectionIdle,
			}),
		// https://github.com/grpc/grpc-go/blob/724f450f77a0/examples/features/keepalive/server/main.go
		grpc.KeepaliveEnforcementPolicy(
			keepalive.EnforcementPolicy{
				MinTime:             KeepaliveMinTime,
				PermitWithoutStream: true,
			}),
	}
}

// startGRPCServers serves each gRPC listener on its own server, with that
// listener's credentials. The services are built once and shared. A listener
// serving grpc-admin gets every method; one serving only grpc-read gets
// read-only adapters, so the admin handlers are not registered there at all
// and answer Unimplemented whatever the auth policy says.
func (x *XTCP) startGRPCServers(ctx context.Context) {
	if len(x.grpcListeners) == 0 {
		return
	}
	// Built here, synchronously in Run before the poller starts, so the
	// poll path's flatRecordServiceSend sees it set or (no gRPC) nil.
	x.flatRecordService = NewXtcpFlatRecordService(ctx, x.registry, &x.pollRequestCh, x.debugLevel)
	x.configService = NewXtcpConfigService(ctx, x.registry, x.config, &x.changePollFrequencyCh, &x.pollRequestCh, &x.pollBurstCh, &x.setS3FlushCh, &x.envelopeMu, x.reconfigureFunc, x.debugLevel)
	linkMonitorService, err := x.newLinkMonitorService()
	if err != nil {
		x.fatalf("link monitor service: %v", err)
		return
	}
	// Standard gRPC health service (grpc.health.v1) so k8s gRPC probes work.
	// Starts NOT_SERVING; setReady flips it to SERVING once the daemon polls.
	x.grpcHealth = grpchealth.NewServer()
	x.grpcHealth.SetServingStatus("", healthpb.HealthCheckResponse_NOT_SERVING)
	methods := grpcMethodAccess()

	for i := range x.grpcListeners {
		l := &x.grpcListeners[i]
		policy, err := l.Spec.Policy(methods)
		if err != nil {
			x.fatalf("gRPC listener %q auth: %v", l.Spec.Name, err)
			return
		}
		opts := grpcServerOptions()
		if policy.Enabled() {
			opts = append(opts, grpc.UnaryInterceptor(policy.UnaryServerInterceptor()), grpc.StreamInterceptor(policy.StreamServerInterceptor()))
		}
		srv := grpc.NewServer(opts...)
		// https://github.com/grpc/grpc-go/blob/master/Documentation/server-reflection-tutorial.md#enable-server-reflection
		reflection.Register(srv)
		healthpb.RegisterHealthServer(srv, x.grpcHealth)
		if l.Spec.Serves(listeners.GRPCAdmin) {
			xtcp_flat_record.RegisterXTCPFlatRecordServiceServer(srv, x.flatRecordService)
			xtcp_config.RegisterConfigServiceServer(srv, x.configService)
			linkmonitorpb.RegisterLinkMonitorServiceServer(srv, linkMonitorService)
		} else {
			xtcp_flat_record.RegisterXTCPFlatRecordServiceServer(srv, readOnlyFlatRecords{s: x.flatRecordService})
			xtcp_config.RegisterConfigServiceServer(srv, readOnlyConfig{s: x.configService})
			linkmonitorpb.RegisterLinkMonitorServiceServer(srv, readOnlyLinkMonitor{s: linkMonitorService})
		}
		go serveGRPC(ctx, *l, srv)
	}
}

// serveGRPC serves one listener until ctx ends. grpc.Server.Serve is not
// ctx-aware, so GracefulStop runs on ctx.Done, draining in-flight RPCs;
// without it the server would outlive Run in an embedded or test caller.
func serveGRPC(ctx context.Context, l GRPCListener, srv *grpc.Server) {
	go func() {
		<-ctx.Done()
		srv.GracefulStop()
	}()
	// Serve returns grpc.ErrServerStopped after GracefulStop: the normal
	// shutdown, not an error worth logging.
	if err := srv.Serve(l.Listener); err != nil && !errors.Is(err, grpc.ErrServerStopped) {
		log.Printf("gRPC listener %q: serve: %v", l.Spec.Name, err)
	}
}

// readOnlyFlatRecords serves FlatRecords (observing polls) and leaves
// PollFlatRecords (driving them) Unimplemented.
type readOnlyFlatRecords struct {
	xtcp_flat_record.UnimplementedXTCPFlatRecordServiceServer
	s *xtcpFlatRecordService
}

func (r readOnlyFlatRecords) FlatRecords(req *xtcp_flat_record.FlatRecordsRequest, stream grpc.ServerStreamingServer[xtcp_flat_record.FlatRecordsResponse]) error {
	return r.s.FlatRecords(req, stream)
}

// readOnlyConfig serves Get and leaves every mutating method Unimplemented.
type readOnlyConfig struct {
	xtcp_config.UnimplementedConfigServiceServer
	s *xtcpConfigService
}

func (r readOnlyConfig) Get(ctx context.Context, in *xtcp_config.GetRequest) (*xtcp_config.GetResponse, error) {
	return r.s.Get(ctx, in)
}

// readOnlyLinkMonitor serves the status and list methods and leaves
// Rebaseline and Resync Unimplemented.
type readOnlyLinkMonitor struct {
	linkmonitorpb.UnimplementedLinkMonitorServiceServer
	s linkmonitorpb.LinkMonitorServiceServer
}

func (r readOnlyLinkMonitor) GetStatus(ctx context.Context, in *linkmonitorpb.GetStatusRequest) (*linkmonitorpb.GetStatusResponse, error) {
	return r.s.GetStatus(ctx, in)
}

func (r readOnlyLinkMonitor) ListInterfaces(ctx context.Context, in *linkmonitorpb.ListInterfacesRequest) (*linkmonitorpb.ListInterfacesResponse, error) {
	return r.s.ListInterfaces(ctx, in)
}

func (r readOnlyLinkMonitor) ListCollectors(ctx context.Context, in *linkmonitorpb.ListCollectorsRequest) (*linkmonitorpb.ListCollectorsResponse, error) {
	return r.s.ListCollectors(ctx, in)
}

// setReady flips the process readiness in one place: the HTTP /readyz flag and
// the gRPC health status move together. The daemon calls setReady(true) once it
// starts polling and setReady(false) on shutdown.
func (x *XTCP) setReady(r bool) {
	health.SetReady(r)
	if x.grpcHealth != nil {
		status := healthpb.HealthCheckResponse_NOT_SERVING
		if r {
			status = healthpb.HealthCheckResponse_SERVING
		}
		x.grpcHealth.SetServingStatus("", status)
	}
}
