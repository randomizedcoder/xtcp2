package xtcp

import (
	"context"
	"log"
	"time"

	"github.com/bufbuild/protovalidate-go"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
	"github.com/randomizedcoder/xtcp2/pkg/xtcp_config"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

type xtcpConfigService struct {
	xtcp_config.UnimplementedConfigServiceServer

	ctx context.Context

	config *xtcp_config.XtcpConfig

	changePollFrequencyCh *chan time.Duration

	pC *prometheus.CounterVec
	pH *prometheus.SummaryVec

	debugLevel uint32
}

// NewXtcpConfigService builds the gRPC ConfigService. The reg argument
// is the prometheus.Registerer the service's CounterVec + SummaryVec are
// registered into; pass nil to use prometheus.DefaultRegisterer (the
// production default). Tests inject a fresh prometheus.NewRegistry() so
// the constructor can be called more than once per process.
func NewXtcpConfigService(
	ctx context.Context,
	reg prometheus.Registerer,
	config *xtcp_config.XtcpConfig,
	changePollFrequencyCh *chan time.Duration,
	debugLevel uint32) *xtcpConfigService {

	c := new(xtcpConfigService)

	c.debugLevel = debugLevel
	c.ctx = ctx

	c.config = config

	c.changePollFrequencyCh = changePollFrequencyCh

	if reg == nil {
		reg = prometheus.DefaultRegisterer
	}
	factory := promauto.With(reg)

	c.pC = factory.NewCounterVec(
		prometheus.CounterOpts{
			Subsystem: "xtcp_config_grpc",
			Name:      promNameCounts,
			Help:      promHelpCounts,
		},
		promLabels,
	)

	c.pH = factory.NewSummaryVec(
		prometheus.SummaryOpts{
			Subsystem: "xtcp_config_grpc",
			Name:      promNameHistograms,
			Help:      promHelpHistograms,
			Objectives: map[float64]float64{
				0.1:  quantileError,
				0.5:  quantileError,
				0.99: quantileError,
			},
			MaxAge: summaryVecMaxAge,
		},
		promLabels,
	)

	return c
}

func (c *xtcpConfigService) Get(
	ctx context.Context, in *xtcp_config.GetRequest) (*xtcp_config.GetResponse, error) {

	c.pC.WithLabelValues("Get", "start", "counter").Inc()

	if err := protovalidate.Validate(in); err != nil {
		c.pC.WithLabelValues("Get", "Validate", "error").Inc()
		if c.debugLevel > 10 {
			log.Println("Get config validation failed:", err)
		}
		err = status.Error(codes.InvalidArgument, err.Error())
		return nil, err
	}

	resp := &xtcp_config.GetResponse{
		Config: c.config,
	}

	return resp, nil
}

func (c *xtcpConfigService) Set(
	ctx context.Context, in *xtcp_config.SetRequest) (*xtcp_config.SetResponse, error) {

	c.pC.WithLabelValues("Set", "start", "counter").Inc()

	if err := protovalidate.Validate(in); err != nil {
		c.pC.WithLabelValues("Set", "Validate", "error").Inc()
		if c.debugLevel > 10 {
			log.Println("Set config validation failed:", err)
		}
		err = status.Error(codes.InvalidArgument, err.Error())
		return nil, err
	}

	err := status.Error(codes.Unimplemented, "unimplemented")
	return nil, err

	// resp := &xtcp_config.SetResponse{
	// 	Config: c.config,
	// }

	// return resp, nil
}

func (c *xtcpConfigService) SetPollFrequency(
	ctx context.Context, in *xtcp_config.SetPollFrequencyRequest) (*xtcp_config.SetPollFrequencyResponse, error) {

	c.pC.WithLabelValues("SetPollFrequency", "start", "counter").Inc()

	if c.debugLevel > 10 {
		log.Printf("SetPollFrequency in.PollFrequency:%0.2f in.PollTimeout:%0.2f",
			in.PollFrequency.AsDuration().Seconds(), in.PollTimeout.AsDuration().Seconds())
	}

	if err := protovalidate.Validate(in); err != nil {
		c.pC.WithLabelValues("SetPollFrequency", "Validate", "error").Inc()
		if c.debugLevel > 10 {
			log.Println("SetPollFrequency config validation failed:", err)
		}
		err = status.Error(codes.InvalidArgument, err.Error())
		return nil, err
	}

	c.config.PollFrequency = in.PollFrequency
	c.config.PollTimeout = in.PollTimeout

	// Send the new poll frequency to the poller. The channel is buffered
	// (size 2), so two sends can succeed without a reader; the third
	// would block forever — pegging the gRPC handler goroutine — if the
	// poller stopped reading (mid-shutdown, paused, etc.). Use ctx-aware
	// select with a non-blocking default fallback so a coalesced
	// frequency-change is dropped (the next caller will resend) rather
	// than wedging the RPC.
	select {
	case *c.changePollFrequencyCh <- c.config.PollFrequency.AsDuration():
	case <-ctx.Done():
		c.pC.WithLabelValues("SetPollFrequency", "ctxDone", "count").Inc()
		return nil, status.Error(codes.Canceled, ctx.Err().Error())
	case <-c.ctx.Done():
		c.pC.WithLabelValues("SetPollFrequency", "serverCtxDone", "count").Inc()
		return nil, status.Error(codes.Unavailable, "server shutting down")
	default:
		c.pC.WithLabelValues("SetPollFrequency", "chFull", "count").Inc()
	}

	if c.debugLevel > 10 {
		log.Printf("SetPollFrequency c.config.PollFrequency:%0.2f c.config.PollTimeout:%0.2f",
			c.config.PollFrequency.AsDuration().Seconds(), c.config.PollTimeout.AsDuration().Seconds())
	}

	// err := status.Error(codes.Unimplemented, "unimplemented")
	return nil, nil
}
