package xtcp

import (
	"context"
	"testing"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"

	"github.com/randomizedcoder/xtcp2/pkg/xtcp_flat_record"
)

// newFlatRecordServiceFixture constructs an xtcpFlatRecordService
// directly with a per-test Prometheus registry, bypassing
// NewXtcpFlatRecordService's global promauto registration.
func newFlatRecordServiceFixture(t *testing.T) *xtcpFlatRecordService {
	t.Helper()
	reg := prometheus.NewRegistry()
	ch := make(chan struct{}, 1)
	s := &xtcpFlatRecordService{
		ctx:           context.Background(),
		pollRequestCh: &ch,
		pC: promauto.With(reg).NewCounterVec(
			prometheus.CounterOpts{Subsystem: "xtcp_grpc_fr_test",
				Name: promNameCounts, Help: "test"},
			promLabels,
		),
		pH: promauto.With(reg).NewSummaryVec(
			prometheus.SummaryOpts{Subsystem: "xtcp_grpc_fr_test",
				Name: promNameHistograms, Help: "test",
				Objectives: map[float64]float64{0.5: quantileError},
				MaxAge:     summaryVecMaxAge},
			promLabels,
		),
	}
	s.FlatRecordsResponsePool.New = func() any {
		return new(xtcp_flat_record.FlatRecordsResponse)
	}
	return s
}

// frMapCount = frStoreCount - frDeleteCount.
func TestFlatRecordService_frMapCount(t *testing.T) {
	s := newFlatRecordServiceFixture(t)
	if got := s.frMapCount(); got != 0 {
		t.Errorf("empty frMapCount = %d, want 0", got)
	}
	s.frStoreCount.Add(7)
	s.frDeleteCount.Add(2)
	if got := s.frMapCount(); got != 5 {
		t.Errorf("frMapCount = %d, want 5", got)
	}
}

// pfrMapCount = pfrStoreCount - pfrDeleteCount.
func TestFlatRecordService_pfrMapCount(t *testing.T) {
	s := newFlatRecordServiceFixture(t)
	if got := s.pfrMapCount(); got != 0 {
		t.Errorf("empty pfrMapCount = %d, want 0", got)
	}
	s.pfrStoreCount.Add(10)
	s.pfrDeleteCount.Add(3)
	if got := s.pfrMapCount(); got != 7 {
		t.Errorf("pfrMapCount = %d, want 7", got)
	}
}

// flatRecordServiceSend on an XTCP with zero registered clients
// follows the early-return path (frMapCount + pfrMapCount both 0).
// The function should not panic and should leave the record alone.
func TestFlatRecordServiceSend_noClients(t *testing.T) {
	reg := prometheus.NewRegistry()
	x := &XTCP{
		flatRecordService: newFlatRecordServiceFixture(t),
	}
	x.pC = promauto.With(reg).NewCounterVec(
		prometheus.CounterOpts{Subsystem: "xtcp_send_test",
			Name: promNameCounts, Help: "test"},
		promLabels,
	)
	x.pH = promauto.With(reg).NewSummaryVec(
		prometheus.SummaryOpts{Subsystem: "xtcp_send_test",
			Name: promNameHistograms, Help: "test",
			Objectives: map[float64]float64{0.5: quantileError},
			MaxAge:     summaryVecMaxAge},
		promLabels,
	)
	x.flatRecordServiceSend(&xtcp_flat_record.XtcpFlatRecord{Hostname: "h"})
	// No panic = pass.
}
