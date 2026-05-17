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
	reg := prometheus.NewRegistry()
	c := &xtcpConfigService{
		ctx: context.Background(),
		config: &xtcp_config.XtcpConfig{
			PollFrequency: durationpb.New(time.Second),
			PollTimeout:   durationpb.New(time.Second / 2),
		},
		changePollFrequencyCh: chPtr,
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
// Set — always returns Unimplemented (current behaviour)
// ───────────────────────────────────────────────────────────────────────

func TestConfigService_Set(t *testing.T) {
	c, _ := newConfigServiceFixture(t)
	_, err := c.Set(context.Background(), &xtcp_config.SetRequest{})
	if err == nil {
		t.Fatal("Set should return Unimplemented error")
	}
	if st, ok := status.FromError(err); !ok || st.Code() != codes.Unimplemented {
		t.Errorf("expected Unimplemented status; got %v", err)
	}
}

// ───────────────────────────────────────────────────────────────────────
// SetPollFrequency — mutates config + signals on the channel
// ───────────────────────────────────────────────────────────────────────

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
		t.Error("SetPollFrequency should have signalled on changePollFrequencyCh")
	}
}
