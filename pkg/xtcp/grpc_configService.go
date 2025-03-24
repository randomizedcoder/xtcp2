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

func NewXtcpConfigService(
	ctx context.Context,
	config *xtcp_config.XtcpConfig,
	changePollFrequencyCh *chan time.Duration,
	debugLevel uint32) *xtcpConfigService {

	c := new(xtcpConfigService)

	c.debugLevel = debugLevel
	c.ctx = ctx

	c.config = config

	c.changePollFrequencyCh = changePollFrequencyCh

	c.pC = promauto.NewCounterVec(
		prometheus.CounterOpts{
			Subsystem: "xtcp_config_grpc",
			Name:      "counts",
			Help:      "xtcp counts",
		},
		[]string{"function", "variable", "type"},
	)

	c.pH = promauto.NewSummaryVec(
		prometheus.SummaryOpts{
			Subsystem: "xtcp_config_grpc",
			Name:      "histograms",
			Help:      "xtcp historgrams",
			Objectives: map[float64]float64{
				0.1:  quantileError,
				0.5:  quantileError,
				0.99: quantileError,
			},
			MaxAge: summaryVecMaxAge,
		},
		[]string{"function", "variable", "type"},
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
		err := status.Error(codes.InvalidArgument, err.Error())
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
		err := status.Error(codes.InvalidArgument, err.Error())
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
		err := status.Error(codes.InvalidArgument, err.Error())
		return nil, err
	}

	c.config.PollFrequency = in.PollFrequency
	c.config.PollTimeout = in.PollTimeout

	*c.changePollFrequencyCh <- c.config.PollFrequency.AsDuration()

	if c.debugLevel > 10 {
		log.Printf("SetPollFrequency c.config.PollFrequency:%0.2f c.config.PollTimeout:%0.2f",
			c.config.PollFrequency.AsDuration().Seconds(), c.config.PollTimeout.AsDuration().Seconds())
	}

	//err := status.Error(codes.Unimplemented, "unimplemented")
	return nil, nil
}
