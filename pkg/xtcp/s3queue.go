//go:build dest_s3parquet

package xtcp

import (
	"sync"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
)

// s3QueueDepthDefaultCst is the envelope depth bound when s3_queue_depth is
// unset (0); s3QueueDepthMaxCst is the largest the config accepts.
const (
	s3QueueDepthDefaultCst = 16
	s3QueueDepthMaxCst     = 4096
)

// s3MaxBufferedBytesDefaultCst is the documented default for
// s3_max_buffered_bytes (the flag's default; 0 in the config means no byte
// bound, the depth bound alone).
const s3MaxBufferedBytesDefaultCst = 64 * 1024 * 1024

// Drop reasons, the reason label of the s3 drop counters.
const (
	s3DropReasonDepth = "queue_depth"
	s3DropReasonBytes = "buffered_bytes"
)

// envelopeQueue is the bounded FIFO between Send and the S3 worker. push
// never blocks: past maxDepth envelopes, or past maxBytes buffered bytes,
// it drops the oldest, so an S3 outage costs data rather than stalling the
// poller. The newest envelope is always kept, even when it alone is over
// maxBytes. notify (capacity 1) wakes the worker; it drains with pop.
type envelopeQueue struct {
	mu       sync.Mutex
	items    []envelopeBytes // ring of cap maxDepth+1
	head     int
	depth    int
	bytes    int
	maxDepth int
	maxBytes int // 0 = no byte bound
	notify   chan struct{}
}

// droppedEnvelope is an envelope push evicted and why.
type droppedEnvelope struct {
	item   envelopeBytes
	reason string
}

func newEnvelopeQueue(maxDepth, maxBytes int) *envelopeQueue {
	if maxDepth <= 0 {
		maxDepth = s3QueueDepthDefaultCst
	}
	return &envelopeQueue{
		items:    make([]envelopeBytes, maxDepth+1),
		maxDepth: maxDepth,
		maxBytes: max(maxBytes, 0),
		notify:   make(chan struct{}, 1),
	}
}

// push appends item, evicts the oldest envelopes past either bound and
// wakes the worker. The evicted envelopes are returned for the caller to
// count and recycle outside the lock.
func (q *envelopeQueue) push(item envelopeBytes) []droppedEnvelope {
	var dropped []droppedEnvelope
	q.mu.Lock()
	q.items[(q.head+q.depth)%len(q.items)] = item
	q.depth++
	q.bytes += len(*item.buf)
	for {
		var reason string
		switch {
		case q.depth > q.maxDepth:
			reason = s3DropReasonDepth
		case q.maxBytes > 0 && q.bytes > q.maxBytes && q.depth > 1:
			reason = s3DropReasonBytes
		}
		if reason == "" {
			break
		}
		dropped = append(dropped, droppedEnvelope{item: q.popLocked(), reason: reason})
	}
	q.mu.Unlock()
	select {
	case q.notify <- struct{}{}:
	default:
	}
	return dropped
}

// pop removes the oldest envelope; ok is false when the queue is empty.
func (q *envelopeQueue) pop() (item envelopeBytes, ok bool) {
	q.mu.Lock()
	defer q.mu.Unlock()
	if q.depth == 0 {
		return envelopeBytes{}, false
	}
	return q.popLocked(), true
}

func (q *envelopeQueue) popLocked() envelopeBytes {
	item := q.items[q.head]
	q.items[q.head] = envelopeBytes{}
	q.head = (q.head + 1) % len(q.items)
	q.depth--
	q.bytes -= len(*item.buf)
	return item
}

// stats returns the queued envelope count and bytes.
func (q *envelopeQueue) stats() (depth, bytes int) {
	q.mu.Lock()
	defer q.mu.Unlock()
	return q.depth, q.bytes
}

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
