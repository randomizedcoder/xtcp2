//go:build dest_s3parquet

package xtcp

import (
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
)

// s3QueueDepthDefaultCst is the envelope depth bound when s3_queue_depth is
// unset (0); s3QueueDepthMaxCst is the largest the config accepts.
const (
	s3QueueDepthDefaultCst = queueDepthDefaultCst
	s3QueueDepthMaxCst     = queueDepthMaxCst
)

// s3MaxBufferedBytesDefaultCst is the documented default for
// s3_max_buffered_bytes (the flag's default; 0 in the config means no byte
// bound, the depth bound alone).
const s3MaxBufferedBytesDefaultCst = 64 * 1024 * 1024

// Drop reasons, the reason label of the s3 drop counters. The queue itself
// emits the shared spellings; these alias them for the s3 tests and docs.
const (
	s3DropReasonDepth = queueDropReasonDepth
	s3DropReasonBytes = queueDropReasonBytes
)

// s3QueueMetrics are the bounded queue's drop counters and buffer gauge.
type s3QueueMetrics struct {
	dropped      *prometheus.CounterVec // envelopes, by reason
	droppedBytes *prometheus.CounterVec // bytes, by reason
	buffered     *prometheus.GaugeVec   // bytes held, by stage
}

func newS3QueueMetrics(reg prometheus.Registerer) *s3QueueMetrics {
	if reg == nil {
		reg = prometheus.DefaultRegisterer
	}
	f := promauto.With(reg)
	return &s3QueueMetrics{
		dropped: f.NewCounterVec(prometheus.CounterOpts{
			Subsystem: promSubsystemXTCP,
			Name:      "s3_dropped_total",
			Help:      "Envelopes the s3parquet queue dropped, oldest first: reason=queue_depth (-s3QueueDepth), buffered_bytes (-s3MaxBufferedBytes).",
		}, []string{promLabelReasonCst}),
		droppedBytes: f.NewCounterVec(prometheus.CounterOpts{
			Subsystem: promSubsystemXTCP,
			Name:      "s3_dropped_bytes_total",
			Help:      "Marshaled envelope bytes the s3parquet queue dropped, by the same reasons as s3_dropped_total.",
		}, []string{promLabelReasonCst}),
		buffered: f.NewGaugeVec(prometheus.GaugeOpts{
			Subsystem: promSubsystemXTCP,
			Name:      "s3_buffered_bytes",
			Help:      "Bytes the s3parquet destination holds: stage=queue (marshaled envelopes awaiting the worker), builder (approximate row bytes in the Parquet object being built).",
		}, []string{"stage"}),
	}
}

// Stages, the stage label of the buffered-bytes gauge.
const (
	s3StageQueue   = "queue"
	s3StageBuilder = "builder"
)
