package xtcp

import (
	"sync"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
)

func (x *XTCP) InitPromethus(wg *sync.WaitGroup) {

	defer wg.Done()

	x.pC = promauto.NewCounterVec(
		prometheus.CounterOpts{
			Subsystem: "xtcp",
			Name:      "counts",
			Help:      "xtcp counts",
		},
		[]string{"function", "variable", "type"},
	)

	x.pH = promauto.NewSummaryVec(
		prometheus.SummaryOpts{
			Subsystem: "xtcp",
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

	x.pG = promauto.NewGauge(
		prometheus.GaugeOpts{
			Subsystem: "xtcp",
			Name:      "gauge",
			Help:      "xtcp network namespace gauge",
		},
	)

}
