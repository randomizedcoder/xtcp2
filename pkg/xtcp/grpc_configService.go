package xtcp

import (
	"context"
	"log"
	"strings"
	"time"

	"github.com/bufbuild/protovalidate-go"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
	"github.com/randomizedcoder/xtcp2/gen/go/xtcp_config"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/proto"
)

type xtcpConfigService struct {
	xtcp_config.UnimplementedConfigServiceServer

	ctx context.Context

	config *xtcp_config.XtcpConfig

	changePollFrequencyCh *chan time.Duration
	pollRequestCh         *chan struct{}
	pollBurstCh           *chan pollBurst
	setS3FlushCh          *chan s3FlushControl

	// reconfigureFunc is the process-level soft-restart hook (see
	// XTCP.reconfigureFunc). Set implements a full-config change by invoking
	// it; nil → Set returns Unavailable.
	reconfigureFunc func(*xtcp_config.XtcpConfig)

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
	pollRequestCh *chan struct{},
	pollBurstCh *chan pollBurst,
	setS3FlushCh *chan s3FlushControl,
	reconfigureFunc func(*xtcp_config.XtcpConfig),
	debugLevel uint32) *xtcpConfigService {

	c := new(xtcpConfigService)

	c.debugLevel = debugLevel
	c.ctx = ctx

	c.config = config

	c.changePollFrequencyCh = changePollFrequencyCh
	c.pollRequestCh = pollRequestCh
	c.pollBurstCh = pollBurstCh
	c.setS3FlushCh = setS3FlushCh
	c.reconfigureFunc = reconfigureFunc

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

	// Redact S3 credentials: the gRPC surface has no auth, so Get must not
	// hand out live secrets. Clone first — never mutate the running config —
	// then blank the credential fields. Set treats empty credentials as
	// "unchanged", so a Get → edit → Set round-trip preserves them.
	redacted, ok := proto.Clone(c.config).(*xtcp_config.XtcpConfig)
	if !ok {
		c.pC.WithLabelValues("Get", "clone", "error").Inc()
		return nil, status.Error(codes.Internal, "config clone failed")
	}
	redacted.S3SecretKey = ""
	redacted.S3AccessKey = ""

	resp := &xtcp_config.GetResponse{
		Config: redacted,
	}

	return resp, nil
}

func (c *xtcpConfigService) Set(
	ctx context.Context, in *xtcp_config.SetRequest) (*xtcp_config.SetResponse, error) {

	c.pC.WithLabelValues("Set", "start", "counter").Inc()

	if in.Config == nil {
		c.pC.WithLabelValues("Set", "nilConfig", "error").Inc()
		return nil, status.Error(codes.InvalidArgument, "config is required")
	}

	// Validate the incoming full config (all XtcpConfig required-field /
	// CEL rules) before we commit to a restart.
	if err := protovalidate.Validate(in.Config); err != nil {
		c.pC.WithLabelValues("Set", "Validate", "error").Inc()
		if c.debugLevel > 10 {
			log.Println("Set config validation failed:", err)
		}
		return nil, status.Error(codes.InvalidArgument, err.Error())
	}

	// Secret preservation: an operator does Get → edit → Set, and Get redacts
	// the S3 credentials (see below). Empty credential fields therefore mean
	// "unchanged", not "clear" — inherit them from the running config so a
	// round-trip can never wipe the daemon's creds.
	if in.Config.S3SecretKey == "" {
		in.Config.S3SecretKey = c.config.S3SecretKey
	}
	if in.Config.S3AccessKey == "" {
		in.Config.S3AccessKey = c.config.S3AccessKey
	}

	// The soft-restart hook is only wired in the real daemon (cmd/xtcp2).
	// Embedded/test callers leave it nil — there is nothing to restart.
	if c.reconfigureFunc == nil {
		c.pC.WithLabelValues("Set", "noReconfigure", "error").Inc()
		return nil, status.Error(codes.Unavailable, "runtime reconfigure is not available in this deployment")
	}

	if c.debugLevel > 10 {
		log.Printf("Set: reconfigure requested (dest:%q marshal:%q) — soft restart",
			in.Config.Dest, in.Config.MarshalTo)
	}

	// Hand the validated config to the process-level hook, which records it
	// and triggers a graceful shutdown ending in syscall.Exec into the new
	// config. The cancel is delayed slightly there so this response flushes
	// to the client first.
	c.reconfigureFunc(in.Config)
	c.pC.WithLabelValues("Set", "reconfigure", "counter").Inc()

	return &xtcp_config.SetResponse{Config: in.Config}, nil
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

	return &xtcp_config.SetPollFrequencyResponse{Config: c.config}, nil
}

// TriggerPoll fires a single poll immediately without changing the configured
// cadence, by poking the poller's pollRequestCh (the same primitive the
// PollFlatRecords stream uses). The poll is coalesced if a dump is already in
// flight. The ctx-aware non-blocking select mirrors SetPollFrequency: never
// wedge the RPC goroutine if the poller has stopped reading.
func (c *xtcpConfigService) TriggerPoll(
	ctx context.Context, in *xtcp_config.TriggerPollRequest) (*xtcp_config.TriggerPollResponse, error) {

	c.pC.WithLabelValues("TriggerPoll", "start", "counter").Inc()

	if err := protovalidate.Validate(in); err != nil {
		c.pC.WithLabelValues("TriggerPoll", "Validate", "error").Inc()
		if c.debugLevel > 10 {
			log.Println("TriggerPoll validation failed:", err)
		}
		return nil, status.Error(codes.InvalidArgument, err.Error())
	}

	select {
	case *c.pollRequestCh <- struct{}{}:
	case <-ctx.Done():
		c.pC.WithLabelValues("TriggerPoll", "ctxDone", "count").Inc()
		return nil, status.Error(codes.Canceled, ctx.Err().Error())
	case <-c.ctx.Done():
		c.pC.WithLabelValues("TriggerPoll", "serverCtxDone", "count").Inc()
		return nil, status.Error(codes.Unavailable, "server shutting down")
	default:
		// pollRequestCh already has a poll queued (buffer full) — the pending
		// poll subsumes this request, so dropping it is correct, not an error.
		c.pC.WithLabelValues("TriggerPoll", "chFull", "count").Inc()
	}

	return &xtcp_config.TriggerPollResponse{}, nil
}

// TriggerPollBurst schedules a burst of in.Count polls, in.Interval apart, and
// returns immediately; pollBurstRunner drives the spacing. Records flow to the
// configured destination (and any connected PollFlatRecords stream). Requires
// interval > poll_timeout so each poll completes before the next fires (the
// pollTimeoutTimer force-zeroes the in-flight count by poll_timeout), which
// guarantees no poll is dropped as alreadyPolling.
func (c *xtcpConfigService) TriggerPollBurst(
	ctx context.Context, in *xtcp_config.TriggerPollBurstRequest) (*xtcp_config.TriggerPollBurstResponse, error) {

	c.pC.WithLabelValues("TriggerPollBurst", "start", "counter").Inc()

	if err := protovalidate.Validate(in); err != nil {
		c.pC.WithLabelValues("TriggerPollBurst", "Validate", "error").Inc()
		if c.debugLevel > 10 {
			log.Println("TriggerPollBurst validation failed:", err)
		}
		return nil, status.Error(codes.InvalidArgument, err.Error())
	}

	interval := in.Interval.AsDuration()
	pollTimeout := c.config.PollTimeout.AsDuration()
	if interval <= pollTimeout {
		c.pC.WithLabelValues("TriggerPollBurst", "intervalTooShort", "error").Inc()
		return nil, status.Errorf(codes.InvalidArgument,
			"interval %s must exceed poll_timeout %s so each poll completes before the next fires",
			interval, pollTimeout)
	}

	select {
	case *c.pollBurstCh <- pollBurst{count: in.Count, interval: interval}:
	case <-ctx.Done():
		c.pC.WithLabelValues("TriggerPollBurst", "ctxDone", "count").Inc()
		return nil, status.Error(codes.Canceled, ctx.Err().Error())
	case <-c.ctx.Done():
		c.pC.WithLabelValues("TriggerPollBurst", "serverCtxDone", "count").Inc()
		return nil, status.Error(codes.Unavailable, "server shutting down")
	default:
		// pollBurstCh is size 1 with a single consumer, so a full channel
		// means a burst is already running. Reject rather than queue.
		c.pC.WithLabelValues("TriggerPollBurst", "burstBusy", "count").Inc()
		return nil, status.Error(codes.ResourceExhausted, "a poll burst is already running")
	}

	if c.debugLevel > 10 {
		log.Printf("TriggerPollBurst scheduled count:%d interval:%s", in.Count, interval)
	}

	return &xtcp_config.TriggerPollBurstResponse{Count: in.Count, Interval: in.Interval}, nil
}

// SetS3Upload changes the s3parquet upload timing at runtime: the
// staleness-flush timer and/or the byte-cap threshold. It mutates the shared
// config (so a subsequent Get reflects it) and signals the s3parquet worker
// via setS3FlushCh. Only effective when the destination is s3parquet; other
// destinations have no consumer, so it fails fast with FailedPrecondition.
func (c *xtcpConfigService) SetS3Upload(
	ctx context.Context, in *xtcp_config.SetS3UploadRequest) (*xtcp_config.SetS3UploadResponse, error) {

	c.pC.WithLabelValues("SetS3Upload", "start", "counter").Inc()

	if err := protovalidate.Validate(in); err != nil {
		c.pC.WithLabelValues("SetS3Upload", "Validate", "error").Inc()
		if c.debugLevel > 10 {
			log.Println("SetS3Upload validation failed:", err)
		}
		return nil, status.Error(codes.InvalidArgument, err.Error())
	}

	if !strings.HasPrefix(c.config.Dest, schemeS3Parquet+":") {
		c.pC.WithLabelValues("SetS3Upload", "notS3Parquet", "error").Inc()
		return nil, status.Errorf(codes.FailedPrecondition,
			"destination %q is not s3parquet; SetS3Upload has no effect", c.config.Dest)
	}

	var ctrl s3FlushControl
	if in.S3FlushInterval != nil {
		c.config.S3FlushInterval = in.S3FlushInterval
		d := in.S3FlushInterval.AsDuration()
		ctrl.interval = &d
	}
	if in.S3ParquetFlushThresholdBytes > 0 {
		c.config.S3ParquetFlushThresholdBytes = in.S3ParquetFlushThresholdBytes
		b := in.S3ParquetFlushThresholdBytes
		ctrl.thresholdBytes = &b
	}

	select {
	case *c.setS3FlushCh <- ctrl:
	case <-ctx.Done():
		c.pC.WithLabelValues("SetS3Upload", "ctxDone", "count").Inc()
		return nil, status.Error(codes.Canceled, ctx.Err().Error())
	case <-c.ctx.Done():
		c.pC.WithLabelValues("SetS3Upload", "serverCtxDone", "count").Inc()
		return nil, status.Error(codes.Unavailable, "server shutting down")
	default:
		// Buffered (size 2); a full channel means the worker hasn't drained
		// prior changes yet. Config is already updated; drop the signal and
		// let the next call reconcile.
		c.pC.WithLabelValues("SetS3Upload", "chFull", "count").Inc()
	}

	if c.debugLevel > 10 {
		log.Printf("SetS3Upload interval:%v thresholdBytes:%d", ctrl.interval, in.S3ParquetFlushThresholdBytes)
	}

	return &xtcp_config.SetS3UploadResponse{Config: c.config}, nil
}
