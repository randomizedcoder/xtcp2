//go:build dest_s3parquet

package xtcp

import (
	"context"
	"slices"
	"sync"
	"testing"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	dto "github.com/prometheus/client_model/go"
)

// qItem is a queue item whose buffer is n bytes, tagged by its first byte.
func qItem(tag byte, n int) envelopeBytes {
	b := make([]byte, n)
	if n > 0 {
		b[0] = tag
	}
	return envelopeBytes{buf: &b}
}

func qTag(item envelopeBytes) byte {
	if len(*item.buf) == 0 {
		return 0
	}
	return (*item.buf)[0]
}

func TestEnvelopeQueue(t *testing.T) {
	type push struct {
		tag byte
		n   int
	}
	tests := []struct {
		name         string
		category     string
		description  string
		maxDepth     int
		maxBytes     int
		pushes       []push
		expected     []byte   // tags left in the queue, oldest first
		expectedDrop []string // reason of each drop, in order
	}{
		{name: "fifo", category: "P", description: "push 3, pop 3 keeps the order",
			maxDepth: 4, pushes: []push{{1, 1}, {2, 1}, {3, 1}}, expected: []byte{1, 2, 3}},
		{name: "depth_drop_oldest", category: "N", description: "depth 2, push 3 drops the oldest",
			maxDepth: 2, pushes: []push{{1, 1}, {2, 1}, {3, 1}}, expected: []byte{2, 3},
			expectedDrop: []string{s3DropReasonDepth}},
		{name: "bytes_drop_oldest", category: "N", description: "maxBytes 10, push 6+6 drops the first",
			maxDepth: 4, maxBytes: 10, pushes: []push{{1, 6}, {2, 6}}, expected: []byte{2},
			expectedDrop: []string{s3DropReasonBytes}},
		{name: "bytes_drop_several", category: "N", description: "a large push evicts several older items",
			maxDepth: 8, maxBytes: 10, pushes: []push{{1, 3}, {2, 3}, {3, 3}, {4, 8}}, expected: []byte{4},
			expectedDrop: []string{s3DropReasonBytes, s3DropReasonBytes, s3DropReasonBytes}},
		{name: "single_over_bytes", category: "C", description: "one 20-byte item over maxBytes 10 is kept",
			maxDepth: 4, maxBytes: 10, pushes: []push{{1, 20}}, expected: []byte{1}},
		{name: "bytes_zero_unlimited", category: "P", description: "maxBytes 0: only depth drops",
			maxDepth: 2, pushes: []push{{1, 1 << 20}, {2, 1 << 20}, {3, 1 << 20}}, expected: []byte{2, 3},
			expectedDrop: []string{s3DropReasonDepth}},
		{name: "exact_bytes", category: "B", description: "a sum equal to maxBytes drops nothing",
			maxDepth: 4, maxBytes: 10, pushes: []push{{1, 5}, {2, 5}}, expected: []byte{1, 2}},
		{name: "depth_default", category: "B", description: "maxDepth 0 means the default depth",
			maxDepth: 0, pushes: make([]push, s3QueueDepthDefaultCst+1), expected: make([]byte, s3QueueDepthDefaultCst),
			expectedDrop: []string{s3DropReasonDepth}},
		{name: "pop_empty", category: "B", description: "an empty queue pops nothing",
			maxDepth: 4},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			q := newEnvelopeQueue(tc.maxDepth, tc.maxBytes)
			var drops []string
			for _, p := range tc.pushes {
				for _, d := range q.push(qItem(p.tag, p.n)) {
					drops = append(drops, d.reason)
				}
			}
			if !slices.Equal(drops, tc.expectedDrop) {
				t.Errorf("%s: drops %v, expected %v", tc.description, drops, tc.expectedDrop)
			}
			var got []byte
			for {
				item, ok := q.pop()
				if !ok {
					break
				}
				got = append(got, qTag(item))
			}
			if !slices.Equal(got, tc.expected) {
				t.Errorf("%s: queue %v, expected %v", tc.description, got, tc.expected)
			}
			if depth, bytes := q.stats(); depth != 0 || bytes != 0 {
				t.Errorf("%s: drained queue reports depth %d bytes %d", tc.description, depth, bytes)
			}
		})
	}
}

// TestEnvelopeQueue_concurrent: 8 pushers and one popper; every item is
// popped or dropped exactly once and the byte count returns to zero.
func TestEnvelopeQueue_concurrent(t *testing.T) {
	const pushers, perPusher = 8, 2000
	q := newEnvelopeQueue(4, 64)
	var mu sync.Mutex
	dropped := 0
	var wg sync.WaitGroup
	for range pushers {
		wg.Go(func() {
			for i := range perPusher {
				n := len(q.push(qItem(1, 1+i%32)))
				mu.Lock()
				dropped += n
				mu.Unlock()
			}
		})
	}
	stop := make(chan struct{})
	popped := 0
	popDone := make(chan struct{})
	go func() {
		defer close(popDone)
		for {
			if _, ok := q.pop(); ok {
				popped++
				continue
			}
			select {
			case <-q.notify:
			case <-stop:
				for {
					if _, ok := q.pop(); !ok {
						return
					}
					popped++
				}
			}
		}
	}()
	wg.Wait()
	close(stop)
	<-popDone
	if popped+dropped != pushers*perPusher {
		t.Errorf("popped %d + dropped %d != pushed %d", popped, dropped, pushers*perPusher)
	}
	if depth, bytes := q.stats(); depth != 0 || bytes != 0 {
		t.Errorf("drained queue reports depth %d bytes %d", depth, bytes)
	}
}

func s3GaugeValue(t *testing.T, g prometheus.Gauge) float64 {
	t.Helper()
	var m dto.Metric
	if err := g.Write(&m); err != nil {
		t.Fatalf("gauge Write: %v", err)
	}
	return m.GetGauge().GetValue()
}

func s3CounterValue(t *testing.T, c prometheus.Counter) float64 {
	t.Helper()
	var m dto.Metric
	if err := c.Write(&m); err != nil {
		t.Fatalf("counter Write: %v", err)
	}
	return m.GetCounter().GetValue()
}

// TestS3ParquetDest_queueBounds holds the worker in an upload and checks
// that Send never blocks, what it drops and that the gauges settle.
func TestS3ParquetDest_queueBounds(t *testing.T) {
	tests := []struct {
		name          string
		category      string
		description   string
		depth         int
		maxBytesEnvs  int // byte bound in envelopes; 0 = none
		sends         int
		expectedQueue int
		expectedDrops map[string]int
	}{
		{name: "s3_queue_full_no_block", category: "N", description: "100 Sends behind a blocked upload drop all but depth",
			depth: 16, sends: 100, expectedQueue: 16, expectedDrops: map[string]int{s3DropReasonDepth: 84}},
		{name: "bytes_bound", category: "N", description: "a byte bound of 3 envelopes keeps 3",
			depth: 16, maxBytesEnvs: 3, sends: 10, expectedQueue: 3, expectedDrops: map[string]int{s3DropReasonBytes: 7}},
		{name: "under_bounds", category: "P", description: "fewer Sends than the depth drop nothing",
			depth: 16, sends: 5, expectedQueue: 5, expectedDrops: map[string]int{}},
		{name: "depth_one", category: "B", description: "depth 1 keeps only the newest",
			depth: 1, sends: 4, expectedQueue: 1, expectedDrops: map[string]int{s3DropReasonDepth: 3}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			hold := make(chan struct{})
			uploading := make(chan struct{}, 1)
			var envBytes int
			d, upl, x := newS3ParquetFixtureCustom(t, 1, func(_ int) error {
				select {
				case uploading <- struct{}{}:
				default:
				}
				<-hold
				return nil
			}, func(d *s3ParquetDest) {
				d.queue = newEnvelopeQueue(tc.depth, 0)
			})
			first := marshalEnvelopeBuf(t, x, mkEnvelope(1))
			envBytes = len(*first)
			if tc.maxBytesEnvs > 0 {
				// Every envelope here marshals to the same size, so set
				// the bound now; the worker hasn't seen another push.
				d.queue.mu.Lock()
				d.queue.maxBytes = tc.maxBytesEnvs * envBytes
				d.queue.mu.Unlock()
			}
			if _, err := d.Send(context.Background(), first); err != nil {
				t.Fatalf("Send: %v", err)
			}
			<-uploading
			for range tc.sends {
				buf := marshalEnvelopeBuf(t, x, mkEnvelope(1))
				start := time.Now()
				if _, err := d.Send(context.Background(), buf); err != nil {
					t.Fatalf("Send: %v", err)
				}
				if el := time.Since(start); el > time.Second {
					t.Fatalf("%s: Send took %v", tc.description, el)
				}
			}
			depth, qBytes := d.queue.stats()
			if depth != tc.expectedQueue {
				t.Errorf("%s: queue depth %d, expected %d", tc.description, depth, tc.expectedQueue)
			}
			if g := s3GaugeValue(t, d.metrics.buffered.WithLabelValues(s3StageQueue)); int(g) != qBytes {
				t.Errorf("%s: queue gauge %v, queue holds %d bytes", tc.description, g, qBytes)
			}
			if tc.maxBytesEnvs > 0 && qBytes > tc.maxBytesEnvs*envBytes {
				t.Errorf("%s: queue holds %d bytes, over the bound %d", tc.description, qBytes, tc.maxBytesEnvs*envBytes)
			}
			for _, reason := range []string{s3DropReasonDepth, s3DropReasonBytes} {
				got := int(s3CounterValue(t, d.metrics.dropped.WithLabelValues(reason)))
				if got != tc.expectedDrops[reason] {
					t.Errorf("%s: dropped{%s} = %d, expected %d", tc.description, reason, got, tc.expectedDrops[reason])
				}
				gotBytes := int(s3CounterValue(t, d.metrics.droppedBytes.WithLabelValues(reason)))
				if gotBytes != tc.expectedDrops[reason]*envBytes {
					t.Errorf("%s: dropped_bytes{%s} = %d, expected %d", tc.description, reason, gotBytes, tc.expectedDrops[reason]*envBytes)
				}
			}
			close(hold)
			if err := d.Close(); err != nil {
				t.Fatalf("Close: %v", err)
			}
			// close_drains: the held envelope and every queued one upload.
			if got := len(upl.Calls()); got != 1+tc.expectedQueue {
				t.Errorf("%s: %d uploads, expected %d", tc.description, got, 1+tc.expectedQueue)
			}
			// gauge_returns_zero
			if g := s3GaugeValue(t, d.metrics.buffered.WithLabelValues(s3StageQueue)); g != 0 {
				t.Errorf("%s: queue gauge %v after drain, expected 0", tc.description, g)
			}
			if g := s3GaugeValue(t, d.metrics.buffered.WithLabelValues(s3StageBuilder)); g != 0 {
				t.Errorf("%s: builder gauge %v after drain, expected 0", tc.description, g)
			}
		})
	}
}

// TestS3ParquetDest_sendAfterCloseRefunds: a Send after Close returns
// errSendOnClosed and leaves the queue untouched.
func TestS3ParquetDest_sendAfterCloseRefunds(t *testing.T) {
	d, _, x := newS3ParquetFixture(t, 1<<20, nil)
	if err := d.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
	buf := marshalEnvelopeBuf(t, x, mkEnvelope(1))
	n, err := d.Send(context.Background(), buf)
	if err != errSendOnClosed || n != 0 {
		t.Fatalf("Send after Close = (%d, %v), expected (0, errSendOnClosed)", n, err)
	}
	if len(*buf) != 0 {
		t.Errorf("refunded buffer not reset: len %d", len(*buf))
	}
	if depth, _ := d.queue.stats(); depth != 0 {
		t.Errorf("queue depth %d after a rejected Send", depth)
	}
}

// BenchmarkS3Send is the Send hot path, push and notify, with a popper
// draining so the queue stays shallow; chan_baseline is the buffered
// channel send it replaced.
func BenchmarkS3Send(b *testing.B) {
	buf := make([]byte, 768<<10)
	item := envelopeBytes{buf: &buf}
	b.Run("queue", func(b *testing.B) {
		q := newEnvelopeQueue(s3QueueDepthDefaultCst, s3MaxBufferedBytesDefaultCst)
		done := make(chan struct{})
		go func() {
			for {
				select {
				case <-q.notify:
					for {
						if _, ok := q.pop(); !ok {
							break
						}
					}
				case <-done:
					return
				}
			}
		}()
		defer close(done)
		b.ReportAllocs()
		for b.Loop() {
			_ = q.push(item)
		}
	})
	b.Run("chan_baseline", func(b *testing.B) {
		ch := make(chan envelopeBytes, s3QueueDepthDefaultCst)
		done := make(chan struct{})
		go func() {
			for {
				select {
				case <-ch:
				case <-done:
					return
				}
			}
		}()
		defer close(done)
		b.ReportAllocs()
		for b.Loop() {
			ch <- item
		}
	})
}
