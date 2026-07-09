package xtcp

import (
	"context"
	"errors"
	"fmt"
	"log"
	"net"
	"time"

	"github.com/randomizedcoder/xtcp2/pkg/health"
	"github.com/randomizedcoder/xtcp2/pkg/ipsockopt"
	"github.com/randomizedcoder/xtcp2/pkg/xtcp_config"
	"github.com/randomizedcoder/xtcp2/pkg/xtcp_flat_record"
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

func (x *XTCP) startGRPCflatRecordService(ctx context.Context) {

	// Clamp the IPv4 TTL / IPv6 hop limit on the gRPC listener too (0 = kernel
	// default), so its replies can't travel far if the host is internet-exposed.
	lc := net.ListenConfig{Control: ipsockopt.Control(x.config.Ipv4Ttl, x.config.Ipv6HopLimit)}
	lis, err := lc.Listen(ctx, "tcp", fmt.Sprintf(":%d", x.config.GrpcPort))
	if err != nil {
		log.Fatalf("failed to listen: %v", err)
	}

	grpcServer := grpc.NewServer(
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
	)
	// grpc.ForceServerCodec(gzip.Name),

	// https://github.com/grpc/grpc-go/blob/master/Documentation/server-reflection-tutorial.md#enable-server-reflection
	// https://github.com/fullstorydev/grpcurl?tab=readme-ov-file#server-reflection
	reflection.Register(grpcServer)

	x.flatRecordService = NewXtcpFlatRecordService(ctx, x.registry, &x.pollRequestCh, x.debugLevel)
	xtcp_flat_record.RegisterXTCPFlatRecordServiceServer(grpcServer, x.flatRecordService)

	x.configService = NewXtcpConfigService(ctx, x.registry, x.config, &x.changePollFrequencyCh, x.debugLevel)
	xtcp_config.RegisterConfigServiceServer(grpcServer, x.configService)

	// Standard gRPC health service (grpc.health.v1) so k8s gRPC probes work.
	// Starts NOT_SERVING; setReady flips it to SERVING once the daemon polls.
	x.grpcHealth = grpchealth.NewServer()
	x.grpcHealth.SetServingStatus("", healthpb.HealthCheckResponse_NOT_SERVING)
	healthpb.RegisterHealthServer(grpcServer, x.grpcHealth)

	// Stop the gRPC server when ctx fires. grpcServer.Serve blocks
	// indefinitely on lis.Accept and is NOT ctx-aware on its own —
	// without this goroutine the gRPC server outlives Run() and would
	// leak in any embedded / test caller that runs the daemon more
	// than once in a process. GracefulStop drains in-flight RPCs
	// before closing the listener.
	go func() {
		<-ctx.Done()
		grpcServer.GracefulStop()
	}()

	// grpcServer.Serve returns grpc.ErrServerStopped when
	// GracefulStop / Stop completes — that's the normal shutdown path
	// here, not an error worth logging. Filter it so a clean SIGTERM
	// doesn't produce a misleading "Serve err:..." log line every run.
	if serveErr := grpcServer.Serve(lis); serveErr != nil && !errors.Is(serveErr, grpc.ErrServerStopped) {
		log.Printf("startGRPCflatRecordService grpcServer.Serve err:%v", serveErr)
	}
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
