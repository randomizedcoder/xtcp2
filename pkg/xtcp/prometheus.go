package xtcp

import (
	"sync"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
)

// Shared metric-naming strings for every prometheus.{Counter,Summary,Gauge}Vec
// the package and its tests register. The grpc service constructors and the
// test harnesses all use the same Name/Help/Label values; centralising them
// here keeps them in lockstep and silences goconst.
const (
	promSubsystemXTCP = "xtcp"

	promNameCounts     = "counts"
	promNameHistograms = "histograms"
	promNameGauge      = "gauge"

	promHelpCounts     = "xtcp counts"
	promHelpHistograms = "xtcp historgrams" //nolint:misspell // preserved spelling from existing metric — renaming would invalidate downstream dashboards
	promHelpGauge      = "xtcp network namespace gauge"

	promLabelFunction = "function"
	promLabelVariable = "variable"
	promLabelType     = "type"
)

// promLabels is the canonical label set for the {Counter,Summary}Vec metrics
// registered by InitPromethus, NewXtcpConfigService, and
// NewXtcpFlatRecordService. Identical layout across all three so dashboards
// can join on (function, variable, type).
var promLabels = []string{promLabelFunction, promLabelVariable, promLabelType}

func (x *XTCP) InitPromethus(wg *sync.WaitGroup) {

	defer wg.Done()

	x.pC = promauto.NewCounterVec(
		prometheus.CounterOpts{
			Subsystem: promSubsystemXTCP,
			Name:      promNameCounts,
			Help:      promHelpCounts,
		},
		promLabels,
	)

	x.pH = promauto.NewSummaryVec(
		prometheus.SummaryOpts{
			Subsystem: promSubsystemXTCP,
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

	x.pG = promauto.NewGauge(
		prometheus.GaugeOpts{
			Subsystem: promSubsystemXTCP,
			Name:      promNameGauge,
			Help:      promHelpGauge,
		},
	)

}
