package xtcp

import (
	"sync"
	"time"
)

// The bounded FIFO a destination worker drains. Lived behind the
// `dest_s3parquet` build tag until the tcp destination needed the same
// shape (#345, #339); it carries no s3-specific behavior, so it is
// untagged and shared. The s3 flavor constants and the s3 drop metrics
// stay in s3queue.go.

// queueDepthDefaultCst is the envelope depth bound when a destination's
// depth knob is unset (0); queueDepthMaxCst is the largest a resolver will
// pass (the config's validate rule enforces the same cap).
const (
	queueDepthDefaultCst = 16
	queueDepthMaxCst     = 4096
)

// Drop reasons, the reason label of the drop counters.
const (
	queueDropReasonDepth = "queue_depth"
	queueDropReasonBytes = "buffered_bytes"
)

// envelopeBytes is the queue payload — pointer to the pooled marshaled
// envelope. The worker is responsible for returning *buf to destBytesPool
// after consuming it (or dropping it).
type envelopeBytes struct {
	buf *[]byte
}

// droppedEnvelope is an envelope push evicted and why.
type droppedEnvelope struct {
	item   envelopeBytes
	reason string
}

// envelopeQueue is the bounded FIFO between Send and a destination worker.
// push never blocks: past maxDepth envelopes, or past maxBytes buffered
// bytes, it drops the oldest, so a slow or unreachable sink costs data
// rather than stalling the poller. The newest envelope is always kept,
// even when it alone is over maxBytes. notify (capacity 1) wakes the
// worker; it drains with pop.
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

func newEnvelopeQueue(maxDepth, maxBytes int) *envelopeQueue {
	if maxDepth <= 0 {
		maxDepth = queueDepthDefaultCst
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
			reason = queueDropReasonDepth
		case q.maxBytes > 0 && q.bytes > q.maxBytes && q.depth > 1:
			reason = queueDropReasonBytes
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

// backoffWindow returns min(cap, base<<(attempt-1)) for attempt>=1, computed
// overflow-safe (the shift is bounded before it can exceed int64). Moved here
// from the s3parquet file when the tcp destination grew the same reconnect
// backoff (#339); the s3 upload path shares it.
func backoffWindow(base, capDur time.Duration, attempt int) time.Duration {
	shift := attempt - 1
	if shift < 0 {
		shift = 0
	}
	if shift >= 33 {
		return capDur
	}
	w := base << shift
	if w <= 0 || w > capDur {
		return capDur
	}
	return w
}
