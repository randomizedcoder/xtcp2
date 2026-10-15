package xtcp

import (
	"time"

	"github.com/prometheus/client_golang/prometheus"
)

// decodeMetrics are the series Deserialize records into, looked up once per
// decoder instead of once per call. WithLabelValues hashes and checks its
// three labels every time it's called, which at one call per socket record
// cost more CPU than decoding the record (docs/performance.md).
//
// The per-packet series are always on. The per-record ones sit behind
// record, which is recordMetricsOff unless debug_prometheus is set.
type decodeMetrics struct {
	start                 prometheus.Counter
	complete              prometheus.Observer
	cont                  prometheus.Counter
	envelopeAppend        prometheus.Counter
	envelopePostFlushDrop prometheus.Counter
	record                recordMetrics
}

// recordMetrics are the per-record series: chosen once, when the decoder
// starts, so the off case is an empty method call with no time.Now.
type recordMetrics interface {
	// msg counts a netlink message (Deserialize "n").
	msg()
	// now is the start time for done; the zero time when off.
	now() time.Time
	// done observes how long a record took from start (nlPacketComplete).
	done(start time.Time)
}

type recordMetricsOff struct{}

func (recordMetricsOff) msg()           {}
func (recordMetricsOff) now() time.Time { return time.Time{} }
func (recordMetricsOff) done(time.Time) {}

type recordMetricsOn struct {
	n        prometheus.Counter
	complete prometheus.Observer
}

func (m recordMetricsOn) msg()         { m.n.Inc() }
func (recordMetricsOn) now() time.Time { return time.Now() }

func (m recordMetricsOn) done(start time.Time) {
	m.complete.Observe(time.Since(start).Seconds())
}

func newDecodeMetrics(pC *prometheus.CounterVec, pH *prometheus.SummaryVec, debug bool) *decodeMetrics {
	m := &decodeMetrics{
		start:                 pC.WithLabelValues("Deserialize", "start", "count"),
		complete:              pH.WithLabelValues("Deserialize", "complete", "count"),
		cont:                  pC.WithLabelValues("Deserialize", "continue", "count"),
		envelopeAppend:        pC.WithLabelValues("Deserialize", "envelopeAppend", "count"),
		envelopePostFlushDrop: pC.WithLabelValues("Deserialize", "envelopePostFlushDrop", "error"),
		record:                recordMetricsOff{},
	}
	if debug {
		m.record = recordMetricsOn{
			n:        pC.WithLabelValues("Deserialize", "n", "count"),
			complete: pH.WithLabelValues("Deserialize", "nlPacketComplete", "count"),
		}
	}
	return m
}
