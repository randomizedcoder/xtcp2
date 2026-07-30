package xtcp

import (
	"context"
	"testing"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/types/known/durationpb"

	"github.com/randomizedcoder/xtcp2/pkg/xtcp_config"
)

// newConfigServiceFixture builds an xtcpConfigService directly,
// bypassing NewXtcpConfigService so the metric registration goes into
// a per-test registry instead of the package-global promauto one.
func newConfigServiceFixture(t *testing.T) (*xtcpConfigService, chan time.Duration) {
	t.Helper()
	ch := make(chan time.Duration, 1)
	chPtr := &ch
	// The new operator-control handlers signal on these; buffered so the
	// handler's non-blocking send lands and the test can read it back. Reach
	// them in tests via c.pollRequestCh / c.pollBurstCh / c.setS3FlushCh.
	pollCh := make(chan struct{}, 2)
	burstCh := make(chan pollBurst, 1)
	s3Ch := make(chan s3FlushControl, 2)
	reg := prometheus.NewRegistry()
	c := &xtcpConfigService{
		ctx: context.Background(),
		config: &xtcp_config.XtcpConfig{
			PollFrequency: durationpb.New(time.Second),
			PollTimeout:   durationpb.New(time.Second / 2),
		},
		changePollFrequencyCh: chPtr,
		pollRequestCh:         &pollCh,
		pollBurstCh:           &burstCh,
		setS3FlushCh:          &s3Ch,
		pC: promauto.With(reg).NewCounterVec(
			prometheus.CounterOpts{Subsystem: "xtcp_grpc_cs_test",
				Name: promNameCounts, Help: "test"},
			promLabels,
		),
		pH: promauto.With(reg).NewSummaryVec(
			prometheus.SummaryOpts{Subsystem: "xtcp_grpc_cs_test",
				Name: promNameHistograms, Help: "test",
				Objectives: map[float64]float64{0.5: quantileError},
				MaxAge:     summaryVecMaxAge},
			promLabels,
		),
	}
	return c, ch
}

// validSetConfig returns a fully-populated XtcpConfig that passes
// protovalidate — the shape an operator would send to Set after a Get.
func validSetConfig() *xtcp_config.XtcpConfig {
	return &xtcp_config.XtcpConfig{
		NlTimeoutMilliseconds:  5000,
		PollFrequency:          durationpb.New(10 * time.Second),
		PollTimeout:            durationpb.New(5 * time.Second),
		Netlinkers:             4,
		NetlinkersDoneChanSize: 100,
		NlmsgSeq:               1,
		Modulus:                1,
		MarshalTo:              "protobufList",
		Dest:                   "kafka:127.0.0.1:9092",
		DebugLevel:             111,
		GrpcPort:               8080,
		CapturePath:            "/tmp",
		Topic:                  "xtcp",
		XtcpProtoFile:          "xtcp.proto",
		KafkaSchemaUrl:         "http://127.0.0.1:8081",
		IoUringRecvBatchSize:   64,
		IoUringCqeBatchSize:    128,
		S3UploadMaxAttempts:    10,
	}
}

// ───────────────────────────────────────────────────────────────────────
// Get — returns the current config
// ───────────────────────────────────────────────────────────────────────

func TestConfigService_Get(t *testing.T) {
	c, _ := newConfigServiceFixture(t)
	resp, err := c.Get(context.Background(), &xtcp_config.GetRequest{})
	if err != nil {
		t.Fatalf("Get err: %v", err)
	}
	if resp == nil || resp.Config == nil {
		t.Fatal("Get returned nil Config")
	}
	if resp.Config.PollFrequency.AsDuration() != time.Second {
		t.Errorf("PollFrequency mismatch: %v", resp.Config.PollFrequency)
	}
}

// ───────────────────────────────────────────────────────────────────────
// Set — always returns Unimplemented (current behavior)
// ───────────────────────────────────────────────────────────────────────

// A nil config is rejected before anything else.
func TestConfigService_Set_nilConfig(t *testing.T) {
	c, _ := newConfigServiceFixture(t)
	_, err := c.Set(context.Background(), &xtcp_config.SetRequest{})
	if st, ok := status.FromError(err); !ok || st.Code() != codes.InvalidArgument {
		t.Errorf("expected InvalidArgument for nil config; got %v", err)
	}
}

// A complete, valid config with no reconfigure hook wired returns Unavailable.
func TestConfigService_Set_noReconfigureHook(t *testing.T) {
	c, _ := newConfigServiceFixture(t)
	_, err := c.Set(context.Background(), &xtcp_config.SetRequest{Config: validSetConfig()})
	if st, ok := status.FromError(err); !ok || st.Code() != codes.Unavailable {
		t.Errorf("expected Unavailable when reconfigureFunc is nil; got %v", err)
	}
}

// ───────────────────────────────────────────────────────────────────────
// SetPollFrequency — mutates config + signals on the channel
// ───────────────────────────────────────────────────────────────────────

// Set validate-error branch — pass a SetRequest with an empty Config,
// which fails required-field validation on the transitively-validated
// XtcpConfig (poll_frequency, dest, etc.). debugLevel>10 exercises
// the inner log + counter branches.
func TestConfigService_Set_validateErr(t *testing.T) {
	c, _ := newConfigServiceFixture(t)
	c.debugLevel = 20
	_, err := c.Set(context.Background(), &xtcp_config.SetRequest{
		Config: &xtcp_config.XtcpConfig{},
	})
	if err == nil {
		t.Fatal("Set with empty config should fail validation")
	}
	if st, ok := status.FromError(err); !ok || st.Code() != codes.InvalidArgument {
		t.Errorf("expected InvalidArgument; got %v", err)
	}
}

// SetPollFrequency validate-error branch — empty request fails validation
// since poll_frequency and poll_timeout are both required. debugLevel>10
// exercises the inner log + counter branches.
func TestConfigService_SetPollFrequency_validateErr(t *testing.T) {
	c, _ := newConfigServiceFixture(t)
	c.debugLevel = 20
	_, err := c.SetPollFrequency(context.Background(), &xtcp_config.SetPollFrequencyRequest{})
	if err == nil {
		t.Fatal("empty SetPollFrequencyRequest should fail validation")
	}
	if st, ok := status.FromError(err); !ok || st.Code() != codes.InvalidArgument {
		t.Errorf("expected InvalidArgument; got %v", err)
	}
}

// SetPollFrequency debug-log happy path: debugLevel>10 hits the entry
// + exit Printf branches.
func TestConfigService_SetPollFrequency_debugLog(t *testing.T) {
	c, ch := newConfigServiceFixture(t)
	c.debugLevel = 20
	req := &xtcp_config.SetPollFrequencyRequest{
		PollFrequency: durationpb.New(5 * time.Second),
		PollTimeout:   durationpb.New(2 * time.Second),
	}
	if _, err := c.SetPollFrequency(context.Background(), req); err != nil {
		t.Fatalf("err: %v", err)
	}
	<-ch
}

// Get + Set + SetPollFrequency debug-log entry counters: hit the
// "start" counter and (where reachable) the debug-level log branch.
func TestConfigService_Get_debugLog(t *testing.T) {
	c, _ := newConfigServiceFixture(t)
	c.debugLevel = 20
	if _, err := c.Get(context.Background(), &xtcp_config.GetRequest{}); err != nil {
		t.Fatalf("err: %v", err)
	}
}

func TestConfigService_Set_debugLog(t *testing.T) {
	c, _ := newConfigServiceFixture(t)
	c.debugLevel = 20
	_, err := c.Set(context.Background(), &xtcp_config.SetRequest{})
	if err == nil {
		t.Fatal("Set should return Unimplemented")
	}
}

func TestConfigService_SetPollFrequency_happy(t *testing.T) {
	c, ch := newConfigServiceFixture(t)
	req := &xtcp_config.SetPollFrequencyRequest{
		PollFrequency: durationpb.New(7 * time.Second),
		PollTimeout:   durationpb.New(3 * time.Second),
	}
	_, err := c.SetPollFrequency(context.Background(), req)
	if err != nil {
		t.Fatalf("SetPollFrequency err: %v", err)
	}
	if c.config.PollFrequency.AsDuration() != 7*time.Second {
		t.Errorf("config.PollFrequency not updated: %v", c.config.PollFrequency)
	}
	if c.config.PollTimeout.AsDuration() != 3*time.Second {
		t.Errorf("config.PollTimeout not updated: %v", c.config.PollTimeout)
	}
	select {
	case d := <-ch:
		if d != 7*time.Second {
			t.Errorf("channel got %v, want 7s", d)
		}
	default:
		t.Error("SetPollFrequency should have signaled on changePollFrequencyCh")
	}
}

// SetPollFrequency now returns a populated response (previously nil,nil).
func TestConfigService_SetPollFrequency_response(t *testing.T) {
	c, _ := newConfigServiceFixture(t)
	resp, err := c.SetPollFrequency(context.Background(), &xtcp_config.SetPollFrequencyRequest{
		PollFrequency: durationpb.New(6 * time.Second),
		PollTimeout:   durationpb.New(2 * time.Second),
	})
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if resp == nil || resp.Config == nil {
		t.Fatal("expected non-nil response with Config")
	}
	if resp.Config.PollFrequency.AsDuration() != 6*time.Second {
		t.Errorf("response PollFrequency mismatch: %v", resp.Config.PollFrequency)
	}
}

// ───────────────────────────────────────────────────────────────────────
// TriggerPoll — pokes pollRequestCh once
// ───────────────────────────────────────────────────────────────────────

func TestConfigService_TriggerPoll_happy(t *testing.T) {
	c, _ := newConfigServiceFixture(t)
	resp, err := c.TriggerPoll(context.Background(), &xtcp_config.TriggerPollRequest{})
	if err != nil {
		t.Fatalf("TriggerPoll err: %v", err)
	}
	if resp == nil {
		t.Fatal("nil response")
	}
	select {
	case <-*c.pollRequestCh:
	default:
		t.Error("TriggerPoll should have poked pollRequestCh")
	}
}

// A canceled request context returns Canceled (the non-blocking send only
// falls to ctx.Done when the buffer is full, so fill it first).
func TestConfigService_TriggerPoll_ctxCanceled(t *testing.T) {
	c, _ := newConfigServiceFixture(t)
	// Fill pollRequestCh (buffer 2) so the send arm blocks and ctx.Done wins.
	*c.pollRequestCh <- struct{}{}
	*c.pollRequestCh <- struct{}{}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err := c.TriggerPoll(ctx, &xtcp_config.TriggerPollRequest{})
	if st, ok := status.FromError(err); !ok || st.Code() != codes.Canceled {
		t.Errorf("expected Canceled; got %v", err)
	}
}

// ───────────────────────────────────────────────────────────────────────
// TriggerPollBurst — validates + schedules on pollBurstCh
// ───────────────────────────────────────────────────────────────────────

func TestConfigService_TriggerPollBurst_happy(t *testing.T) {
	c, _ := newConfigServiceFixture(t)
	// poll_timeout is 500ms in the fixture; 10s interval clears it.
	req := &xtcp_config.TriggerPollBurstRequest{
		Count:    6,
		Interval: durationpb.New(10 * time.Second),
	}
	resp, err := c.TriggerPollBurst(context.Background(), req)
	if err != nil {
		t.Fatalf("TriggerPollBurst err: %v", err)
	}
	if resp == nil || resp.Count != 6 || resp.Interval.AsDuration() != 10*time.Second {
		t.Errorf("unexpected response: %+v", resp)
	}
	select {
	case b := <-*c.pollBurstCh:
		if b.count != 6 || b.interval != 10*time.Second {
			t.Errorf("burst got %+v, want {6, 10s}", b)
		}
	default:
		t.Error("TriggerPollBurst should have scheduled on pollBurstCh")
	}
}

func TestConfigService_TriggerPollBurst_validateErr(t *testing.T) {
	c, _ := newConfigServiceFixture(t)
	c.debugLevel = 20
	// count=0 violates gte:1; interval unset violates required.
	_, err := c.TriggerPollBurst(context.Background(), &xtcp_config.TriggerPollBurstRequest{})
	if st, ok := status.FromError(err); !ok || st.Code() != codes.InvalidArgument {
		t.Errorf("expected InvalidArgument; got %v", err)
	}
}

// interval must exceed poll_timeout, else the poll would be coalesced away.
func TestConfigService_TriggerPollBurst_intervalTooShort(t *testing.T) {
	c, _ := newConfigServiceFixture(t)
	c.config.PollTimeout = durationpb.New(10 * time.Second)
	req := &xtcp_config.TriggerPollBurstRequest{
		Count:    3,
		Interval: durationpb.New(5 * time.Second), // <= poll_timeout
	}
	_, err := c.TriggerPollBurst(context.Background(), req)
	if st, ok := status.FromError(err); !ok || st.Code() != codes.InvalidArgument {
		t.Errorf("expected InvalidArgument for interval<=poll_timeout; got %v", err)
	}
}

// A second burst while one is queued (channel full) is rejected.
func TestConfigService_TriggerPollBurst_busy(t *testing.T) {
	c, _ := newConfigServiceFixture(t)
	req := &xtcp_config.TriggerPollBurstRequest{
		Count:    2,
		Interval: durationpb.New(10 * time.Second),
	}
	if _, err := c.TriggerPollBurst(context.Background(), req); err != nil {
		t.Fatalf("first burst err: %v", err)
	}
	// pollBurstCh is size 1 and now full → second is ResourceExhausted.
	_, err := c.TriggerPollBurst(context.Background(), req)
	if st, ok := status.FromError(err); !ok || st.Code() != codes.ResourceExhausted {
		t.Errorf("expected ResourceExhausted; got %v", err)
	}
}

// ───────────────────────────────────────────────────────────────────────
// SetS3Upload — mutates config + signals the s3 worker
// ───────────────────────────────────────────────────────────────────────

func TestConfigService_SetS3Upload_happy(t *testing.T) {
	c, _ := newConfigServiceFixture(t)
	c.config.Dest = "s3parquet:http://127.0.0.1:9000"
	req := &xtcp_config.SetS3UploadRequest{
		S3FlushInterval:              durationpb.New(5 * time.Second),
		S3ParquetFlushThresholdBytes: 1024,
	}
	resp, err := c.SetS3Upload(context.Background(), req)
	if err != nil {
		t.Fatalf("SetS3Upload err: %v", err)
	}
	if resp == nil || resp.Config == nil {
		t.Fatal("nil response/config")
	}
	if c.config.S3FlushInterval.AsDuration() != 5*time.Second {
		t.Errorf("config.S3FlushInterval not updated: %v", c.config.S3FlushInterval)
	}
	if c.config.S3ParquetFlushThresholdBytes != 1024 {
		t.Errorf("config.S3ParquetFlushThresholdBytes not updated: %d", c.config.S3ParquetFlushThresholdBytes)
	}
	select {
	case ctrl := <-*c.setS3FlushCh:
		if ctrl.interval == nil || *ctrl.interval != 5*time.Second {
			t.Errorf("ctrl.interval mismatch: %v", ctrl.interval)
		}
		if ctrl.thresholdBytes == nil || *ctrl.thresholdBytes != 1024 {
			t.Errorf("ctrl.thresholdBytes mismatch: %v", ctrl.thresholdBytes)
		}
	default:
		t.Error("SetS3Upload should have signaled on setS3FlushCh")
	}
}

func TestConfigService_SetS3Upload_notS3Parquet(t *testing.T) {
	c, _ := newConfigServiceFixture(t)
	c.config.Dest = "kafka:127.0.0.1:9092"
	req := &xtcp_config.SetS3UploadRequest{S3FlushInterval: durationpb.New(5 * time.Second)}
	_, err := c.SetS3Upload(context.Background(), req)
	if st, ok := status.FromError(err); !ok || st.Code() != codes.FailedPrecondition {
		t.Errorf("expected FailedPrecondition; got %v", err)
	}
}

// Neither field set fails the at-least-one CEL rule.
func TestConfigService_SetS3Upload_validateErr(t *testing.T) {
	c, _ := newConfigServiceFixture(t)
	c.debugLevel = 20
	c.config.Dest = "s3parquet:http://127.0.0.1:9000"
	_, err := c.SetS3Upload(context.Background(), &xtcp_config.SetS3UploadRequest{})
	if st, ok := status.FromError(err); !ok || st.Code() != codes.InvalidArgument {
		t.Errorf("expected InvalidArgument; got %v", err)
	}
}

// Threshold-only change leaves interval unset in the control message.
func TestConfigService_SetS3Upload_thresholdOnly(t *testing.T) {
	c, _ := newConfigServiceFixture(t)
	c.config.Dest = "s3parquet:http://127.0.0.1:9000"
	req := &xtcp_config.SetS3UploadRequest{S3ParquetFlushThresholdBytes: 2048}
	if _, err := c.SetS3Upload(context.Background(), req); err != nil {
		t.Fatalf("err: %v", err)
	}
	select {
	case ctrl := <-*c.setS3FlushCh:
		if ctrl.interval != nil {
			t.Errorf("interval should be unset, got %v", *ctrl.interval)
		}
		if ctrl.thresholdBytes == nil || *ctrl.thresholdBytes != 2048 {
			t.Errorf("thresholdBytes mismatch: %v", ctrl.thresholdBytes)
		}
	default:
		t.Error("expected signal on setS3FlushCh")
	}
}

// ───────────────────────────────────────────────────────────────────────
// Set — full-config soft restart
// ───────────────────────────────────────────────────────────────────────

func TestConfigService_Set_invalidConfig(t *testing.T) {
	c, _ := newConfigServiceFixture(t)
	c.debugLevel = 20
	// Missing required fields → protovalidate fails.
	_, err := c.Set(context.Background(), &xtcp_config.SetRequest{Config: &xtcp_config.XtcpConfig{}})
	if st, ok := status.FromError(err); !ok || st.Code() != codes.InvalidArgument {
		t.Errorf("expected InvalidArgument; got %v", err)
	}
}

func TestConfigService_Set_happy(t *testing.T) {
	c, _ := newConfigServiceFixture(t)
	var got *xtcp_config.XtcpConfig
	c.reconfigureFunc = func(cfg *xtcp_config.XtcpConfig) { got = cfg }

	in := validSetConfig()
	in.Tag = "INC-1234"
	resp, err := c.Set(context.Background(), &xtcp_config.SetRequest{Config: in})
	if err != nil {
		t.Fatalf("Set err: %v", err)
	}
	if resp == nil || resp.Config == nil || resp.Config.Tag != "INC-1234" {
		t.Errorf("unexpected response: %+v", resp)
	}
	if got == nil || got.Tag != "INC-1234" {
		t.Error("reconfigureFunc was not invoked with the new config")
	}
}

// Empty credential fields inherit the running config's values (so a
// redacted Get → Set round-trip can't wipe creds).
func TestConfigService_Set_secretPreservation(t *testing.T) {
	c, _ := newConfigServiceFixture(t)
	c.config.S3SecretKey = "live-secret"
	c.config.S3AccessKey = "live-access"
	var got *xtcp_config.XtcpConfig
	c.reconfigureFunc = func(cfg *xtcp_config.XtcpConfig) { got = cfg }

	in := validSetConfig() // no S3 creds set → empty
	if _, err := c.Set(context.Background(), &xtcp_config.SetRequest{Config: in}); err != nil {
		t.Fatalf("Set err: %v", err)
	}
	if got.S3SecretKey != "live-secret" {
		t.Errorf("S3SecretKey = %q, want inherited 'live-secret'", got.S3SecretKey)
	}
	if got.S3AccessKey != "live-access" {
		t.Errorf("S3AccessKey = %q, want inherited 'live-access'", got.S3AccessKey)
	}
}

// An explicit non-empty credential in Set overrides the running value.
func TestConfigService_Set_secretOverride(t *testing.T) {
	c, _ := newConfigServiceFixture(t)
	c.config.S3SecretKey = "live-secret"
	c.reconfigureFunc = func(*xtcp_config.XtcpConfig) {}

	in := validSetConfig()
	in.S3SecretKey = "new-secret"
	resp, err := c.Set(context.Background(), &xtcp_config.SetRequest{Config: in})
	if err != nil {
		t.Fatalf("Set err: %v", err)
	}
	if resp.Config.S3SecretKey != "new-secret" {
		t.Errorf("S3SecretKey = %q, want 'new-secret'", resp.Config.S3SecretKey)
	}
}

// Get redacts S3 credentials and does not mutate the live config.
func TestConfigService_Get_redactsSecrets(t *testing.T) {
	c, _ := newConfigServiceFixture(t)
	c.config.S3SecretKey = "live-secret"
	c.config.S3AccessKey = "live-access"
	resp, err := c.Get(context.Background(), &xtcp_config.GetRequest{})
	if err != nil {
		t.Fatalf("Get err: %v", err)
	}
	if resp.Config.S3SecretKey != "" || resp.Config.S3AccessKey != "" {
		t.Errorf("Get should redact S3 creds; got secret=%q access=%q",
			resp.Config.S3SecretKey, resp.Config.S3AccessKey)
	}
	// The live config must be untouched.
	if c.config.S3SecretKey != "live-secret" || c.config.S3AccessKey != "live-access" {
		t.Error("Get must not mutate the live config's credentials")
	}
}
