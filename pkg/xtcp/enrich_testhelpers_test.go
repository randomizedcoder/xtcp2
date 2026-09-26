package xtcp

import (
	"os"
	"testing"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
	"google.golang.org/protobuf/types/known/durationpb"

	"github.com/randomizedcoder/xtcp2/gen/go/xtcp_config"
)

// Helpers shared by the enrichment tests. Deliberately untagged: the tests
// that use them live in `//go:build enrich_asn` / `//go:build enrich_locality`
// files, so a helper carrying either tag would be missing from the other
// build. Untagged they compile in every flavor, at the cost of `go vet`
// flagging nothing — unused test helpers are not an error.

// newMetricsFixture builds an XTCP with just enough state for the enrichment
// paths: config with both enrichers enabled, and the three fresh Prometheus
// vectors every enricher reports through. Each call gets its own registry so
// counter assertions are independent between subtests.
func newMetricsFixture(t *testing.T, interval time.Duration) *XTCP {
	t.Helper()
	x := new(XTCP)
	x.config = &xtcp_config.XtcpConfig{
		EnrichLocalityEnable:    true,
		LocalityRefreshInterval: durationpb.New(interval),
	}
	reg := prometheus.NewRegistry()
	x.pC = promauto.With(reg).NewCounterVec(
		prometheus.CounterOpts{Subsystem: "xtcp_loctest", Name: promNameCounts, Help: promNameCounts},
		promLabels,
	)
	x.pH = promauto.With(reg).NewSummaryVec(
		prometheus.SummaryOpts{
			Subsystem: "xtcp_loctest", Name: promNameHistograms, Help: promNameHistograms,
			Objectives: map[float64]float64{0.5: quantileError, 0.99: quantileError},
			MaxAge:     summaryVecMaxAge,
		},
		promLabels,
	)
	x.pGV = promauto.With(reg).NewGaugeVec(
		prometheus.GaugeOpts{Subsystem: "xtcp_loctest", Name: promNameGauges, Help: promNameGauges},
		promLabels,
	)
	return x
}

// waitFor polls cond until it is true or the deadline passes. Used by the
// tests that drive a background refresh loop rather than calling it directly.
func waitFor(t *testing.T, what string, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		if cond() {
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatalf("timed out waiting for %s", what)
}

// writeBytesFile writes raw bytes to path, for the corrupt-artifact rows.
func writeBytesFile(t *testing.T, path string, b []byte) {
	t.Helper()
	if err := os.WriteFile(path, b, 0o600); err != nil {
		t.Fatal(err)
	}
}
