package xtcp

import (
	"context"
	"fmt"
	"log"
	"net"
	"time"

	"github.com/randomizedcoder/xtcp2/pkg/xtcp_config"
	"github.com/randomizedcoder/xtcp2/pkg/xtcp_flat_record"
	"google.golang.org/grpc"
	_ "google.golang.org/grpc/encoding/gzip"
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

	lis, err := net.Listen("tcp", fmt.Sprintf(":%d", x.config.GrpcPort))
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
	//grpc.ForceServerCodec(gzip.Name),

	// https://github.com/grpc/grpc-go/blob/master/Documentation/server-reflection-tutorial.md#enable-server-reflection
	// https://github.com/fullstorydev/grpcurl?tab=readme-ov-file#server-reflection
	reflection.Register(grpcServer)

	x.flatRecordService = NewXtcpFlatRecordService(ctx, &x.pollRequestCh, x.debugLevel)
	xtcp_flat_record.RegisterXTCPFlatRecordServiceServer(grpcServer, x.flatRecordService)

	x.configService = NewXtcpConfigService(ctx, x.config, &x.changePollFrequencyCh, x.debugLevel)
	xtcp_config.RegisterConfigServiceServer(grpcServer, x.configService)

	grpcServer.Serve(lis)
}
