package xtcp

import (
	"context"
	"errors"
	"fmt"
	"log"
	"math"
	"net"
	"strings"
	"sync"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"

	"github.com/randomizedcoder/xtcp2/gen/go/xtcp_config"
	"github.com/randomizedcoder/xtcp2/pkg/misc"
)

// tcpDest writes each marshaled payload to a connected TCP socket:
// `-dest tcp:host:port`. TCP is the reliable, ordered transport most log/
// metric shippers (Vector, Logstash, Fluentd, `nc`) expect — pair it with a
// line-delimited marshaller (jsonl/csv/tsv). Framing is the marshaller's job;
// tcpDest writes bytes verbatim so it never corrupts a length-delimited
// stream (e.g. protobufList).
//
// Writes flow through a bounded queue to one background worker, so the
// destination a stall can never touch is the poller (docs/redteam-plan.md
// rt-11-5, issue #345): Send never blocks — a wedged or unreachable sink
// costs queued envelopes (oldest first, counted in
// xtcp_tcp_dropped_total), not poll cycles. The worker bounds every write
// with SetWriteDeadline(tcp_write_timeout) — a blackholed route stalls
// delivery for the timeout, not the TCP retransmit window (~15.4 min at
// the prod-default tcp_retries2=15, which used to freeze collection).
// A failed write closes the conn and the next attempt re-dials under
// full-jitter backoff (docs/design-jitter-and-backoff.md), so a sink
// restart heals delivery without a daemon restart (issue #339).
type tcpDest struct {
	x    *XTCP
	addr string

	queue *envelopeQueue

	writeTimeout time.Duration // per-write and per-dial bound
	backoffCap   time.Duration // cap on a single retry's jitter window
	attempts     int           // write attempts per envelope before dropping
	drainTimeout time.Duration // Close's wait on the worker (tests shrink it)

	// dial opens the sink conn; a field so tests can substitute fakes.
	dial func(ctx context.Context) (net.Conn, error)

	// jitterDur draws a full-jitter sleep in [0, d]; sleep is the ctx-aware
	// backoff sleep. Injectable for tests, like the s3parquet worker.
	jitterDur func(time.Duration) time.Duration
	sleep     func(ctx context.Context, d time.Duration) bool

	metrics *tcpQueueMetrics

	connMu   sync.Mutex
	conn     net.Conn
	connDown bool // a write/dial failed since the last good conn

	closedCh   chan struct{}
	workerDone chan struct{}
	closeOnce  sync.Once
}

// errTCPClosed is returned by Send when the destination has been Close'd.
// Callers in flushEnvelope log + counter-bump; the daemon itself doesn't
// treat this as fatal (shutdown is in progress).
var errTCPClosed = errors.New("tcp destination closed")

// tcpWriteTimeoutDefaultCst is the per-write deadline when tcp_write_timeout
// is unset (0). The floor and ceiling are the Go-side belt beside the
// config's validate rule; the rule is the constant, the belt is
// defense-in-depth for configs built without validation.
const (
	tcpWriteTimeoutDefaultCst = 30 * time.Second
	tcpWriteTimeoutMinCst     = 1 * time.Second
	tcpWriteTimeoutMaxCst     = time.Hour
)

// tcpWriteMaxAttemptsCst bounds the write attempts (dial + write) per
// envelope before it is dropped and counted; the backoff between attempts
// grows full-jitter from tcpBackoffBaseCst toward the derived cap.
const (
	tcpWriteMaxAttemptsCst = 3
	tcpBackoffBaseCst      = time.Second
)

// resolveTCPWriteTimeout returns the per-write deadline: tcp_write_timeout,
// or tcpWriteTimeoutDefaultCst when unset (0), clamped to the belt range.
func resolveTCPWriteTimeout(c *xtcp_config.XtcpConfig) time.Duration {
	d := c.GetTcpWriteTimeout().AsDuration()
	if d <= 0 {
		return tcpWriteTimeoutDefaultCst
	}
	if d < tcpWriteTimeoutMinCst {
		return tcpWriteTimeoutMinCst
	}
	if d > tcpWriteTimeoutMaxCst {
		return tcpWriteTimeoutMaxCst
	}
	return d
}

// resolveTCPQueueDepth returns the envelope depth bound: tcp_queue_depth,
// or tcpQueueDepth defaultCst when unset (0), capped like the config rule.
func resolveTCPQueueDepth(c *xtcp_config.XtcpConfig) int {
	if c.GetTcpQueueDepth() == 0 {
		return queueDepthDefaultCst
	}
	return int(min(c.GetTcpQueueDepth(), queueDepthMaxCst))
}

// resolveTCPMaxBufferedBytes returns the queue's byte bound: 0 (none) when
// tcp_max_buffered_bytes is unset, saturating at the largest int.
func resolveTCPMaxBufferedBytes(c *xtcp_config.XtcpConfig) int {
	return int(min(c.GetTcpMaxBufferedBytes(), uint64(math.MaxInt)))
}

// resolveTCPBackoffCap returns the cap on a single reconnect retry's jitter
// window: the derived clamp(poll_frequency/10, 1s, 1h).
func resolveTCPBackoffCap(c *xtcp_config.XtcpConfig) time.Duration {
	capDur := c.GetPollFrequency().AsDuration() / 10
	if capDur < time.Second {
		capDur = time.Second
	}
	if capDur > time.Hour {
		capDur = time.Hour
	}
	return capDur
}

func newTCPDest(ctx context.Context, x *XTCP) (Destination, error) {
	addr := strings.TrimPrefix(x.config.Dest, schemeTCP+":")
	if _, _, err := net.SplitHostPort(addr); err != nil {
		return nil, fmt.Errorf("InitDestTCP dest %q: %w", x.config.Dest, err)
	}
	d := &tcpDest{
		x:            x,
		addr:         addr,
		queue:        newEnvelopeQueue(resolveTCPQueueDepth(x.config), resolveTCPMaxBufferedBytes(x.config)),
		writeTimeout: resolveTCPWriteTimeout(x.config),
		backoffCap:   resolveTCPBackoffCap(x.config),
		attempts:     tcpWriteMaxAttemptsCst,
		drainTimeout: tcpWorkerDrainTimeoutCst,
		jitterDur:    misc.JitterDuration,
		sleep:        misc.SleepCtx,
		metrics:      newTCPQueueMetrics(x.registry),
		closedCh:     make(chan struct{}),
		workerDone:   make(chan struct{}),
	}
	// dial is a field (tests substitute fakes); production dials the sink
	// under the write timeout.
	d.dial = d.dialTCP
	go d.worker(ctx)
	return d, nil
}

// dialTCP opens the sink conn, bounding the dial with the write timeout so
// a blackholed route costs tcp_write_timeout per attempt, not the OS dial
// timeout.
func (d *tcpDest) dialTCP(ctx context.Context) (net.Conn, error) {
	dialCtx, cancel := context.WithTimeout(ctx, d.writeTimeout)
	defer cancel()
	var dialer net.Dialer
	return dialer.DialContext(dialCtx, "tcp", d.addr)
}

// Send enqueues the marshaled envelope for the background worker and never
// blocks: flushEnvelope runs on the poll goroutine, so a wedged or
// unreachable sink must not stall polling. Past the queue's depth or byte
// bound the oldest envelopes are dropped, counted (xtcp_tcp_dropped_total,
// xtcp_tcp_dropped_bytes_total and the queueFull counter) and their buffers
// returned to destBytesPool.
//
// Sends arriving after Close return errTCPClosed and refund the buffer to
// destBytesPool so the upstream pool stays warm.
//
// Returns (1, nil) on enqueue to mirror the per-record accounting the
// caller (flushEnvelope in poller.go) expects.
func (d *tcpDest) Send(_ context.Context, b *[]byte) (int, error) {
	select {
	case <-d.closedCh:
		d.x.destBytesPool.Put(b)
		return 0, errTCPClosed
	default:
	}
	for _, drop := range d.queue.push(envelopeBytes{buf: b}) {
		d.countDrop(drop)
		d.x.destBytesPool.Put(drop.item.buf)
	}
	d.observeQueue()
	return 1, nil
}

// countDrop records one envelope the queue evicted.
func (d *tcpDest) countDrop(drop droppedEnvelope) {
	if d.x.pC != nil {
		d.x.pC.WithLabelValues("destTCP", "queueFull", "error").Inc()
	}
	if d.metrics != nil {
		d.metrics.dropped.WithLabelValues(drop.reason).Inc()
		d.metrics.droppedBytes.WithLabelValues(drop.reason).Add(float64(len(*drop.item.buf)))
	}
}

// observeQueue sets the queue stage of the buffered-bytes gauge.
func (d *tcpDest) observeQueue() {
	if d.metrics == nil {
		return
	}
	_, bytes := d.queue.stats()
	d.metrics.buffered.WithLabelValues("queue").Set(float64(bytes))
}

// countItemDrop records one envelope the worker itself dropped (attempts
// exhausted, or shutdown raced the item).
func (d *tcpDest) countItemDrop(reason string, n int) {
	if d.x.pC != nil {
		d.x.pC.WithLabelValues("destTCP", "dropped", "error").Inc()
	}
	if d.metrics != nil {
		d.metrics.dropped.WithLabelValues(reason).Inc()
		d.metrics.droppedBytes.WithLabelValues(reason).Add(float64(n))
	}
}

// worker drains the queue to the sink. One goroutine keeps envelope order;
// on Close it writes what is still queued (bounded by the drain window), so
// the shutdown flush in flushEnvelope survives a restart, then exits.
func (d *tcpDest) worker(ctx context.Context) {
	defer close(d.workerDone)
	for {
		item, ok := d.queue.pop()
		if ok {
			d.observeQueue()
			d.writeItem(ctx, item)
			continue
		}
		select {
		case <-d.closedCh:
			d.drain(ctx)
			return
		case <-d.queue.notify:
		case <-ctx.Done():
			return
		}
	}
}

// drain writes still-queued envelopes until the queue empties or the drain
// window closes; past the window the rest are dropped and counted, and their
// buffers recycled. Writes run under a fresh context so a canceled parent
// (already-exiting daemon) cannot kill the drain mid-envelope; the window is
// the bound.
func (d *tcpDest) drain(ctx context.Context) {
	dctx, cancel := context.WithTimeout(ctx, tcpWorkerDrainTimeoutCst)
	defer cancel()
	timer := time.NewTimer(tcpWorkerDrainTimeoutCst)
	defer timer.Stop()
	for {
		item, ok := d.queue.pop()
		if !ok {
			return
		}
		select {
		case <-timer.C:
			d.countItemDrop("shutdown", len(*item.buf))
			d.x.destBytesPool.Put(item.buf)
			continue
		default:
		}
		d.writeItem(dctx, item)
	}
}

// writeItem writes one envelope, reconnecting under full-jitter backoff:
// every attempt bounds its dial and write with the write timeout, a failed
// write closes the conn (the next attempt re-dials), and past d.attempts
// the envelope is dropped and counted while the worker moves on.
func (d *tcpDest) writeItem(ctx context.Context, item envelopeBytes) {
	defer d.x.destBytesPool.Put(item.buf)
	for attempt := 1; ; attempt++ {
		if ctx.Err() != nil {
			d.countItemDrop("shutdown", len(*item.buf))
			return
		}
		n, err := d.attemptWrite(ctx, item)
		if err == nil {
			if d.x.pC != nil {
				d.x.pC.WithLabelValues("destTCP", "Writes", "count").Inc()
				d.x.pC.WithLabelValues("destTCP", "WriteBytes", "count").Add(float64(n))
			}
			return
		}
		if d.x.pC != nil {
			d.x.pC.WithLabelValues("destTCP", "Write", "error").Inc()
		}
		if d.x.debugLevel > 10 {
			log.Printf("destTCP attempt %d/%d addr:%s err:%v bytes:%d",
				attempt, d.attempts, d.addr, err, len(*item.buf))
		}
		if attempt >= d.attempts {
			d.countItemDrop("attempts", len(*item.buf))
			return
		}
		d.sleep(ctx, d.jitterDur(backoffWindow(tcpBackoffBaseCst, d.backoffCap, attempt)))
	}
}

// attemptWrite returns the cached conn (believed live) or dials a fresh
// one, then writes the envelope under the write deadline.
func (d *tcpDest) attemptWrite(ctx context.Context, item envelopeBytes) (int, error) {
	conn, err := d.getConn(ctx)
	if err != nil {
		return 0, fmt.Errorf("tcp dest dial %q: %w", d.addr, err)
	}
	if err := conn.SetWriteDeadline(time.Now().Add(d.writeTimeout)); err != nil {
		d.dropConn()
		return 0, fmt.Errorf("tcp dest SetWriteDeadline: %w", err)
	}
	n, err := conn.Write(*item.buf)
	if err != nil {
		d.dropConn()
		return 0, err
	}
	return n, nil
}

// getConn returns the cached conn, or dials and caches a fresh one. A conn
// is cached only right after a successful dial; every write failure drops
// it, so a cached conn is one no attempt has yet failed on. The dial runs
// OUTSIDE connMu — it can block for the write timeout, and holding the
// mutex across it would deadlock Close's dropConn (found by the eviction
// test's goroutine dump). Reconnects — a dial that follows a drop — are
// counted.
func (d *tcpDest) getConn(ctx context.Context) (net.Conn, error) {
	d.connMu.Lock()
	if d.conn != nil {
		conn := d.conn
		d.connMu.Unlock()
		return conn, nil
	}
	d.connMu.Unlock()

	conn, err := d.dial(ctx)
	if err != nil {
		d.connMu.Lock()
		d.connDown = true
		d.connMu.Unlock()
		if d.x.pC != nil {
			d.x.pC.WithLabelValues("destTCP", "Dial", "error").Inc()
		}
		return nil, err
	}
	d.connMu.Lock()
	if d.conn != nil {
		// Another goroutine won the dial race; keep theirs.
		d.connMu.Unlock()
		_ = conn.Close() //nolint:errcheck // loser of the dial race; teardown best-effort
		return d.getConnLocked()
	}
	if d.connDown {
		d.connDown = false
		if d.x.pC != nil {
			d.x.pC.WithLabelValues("destTCP", "Reconnect", "count").Inc()
		}
	}
	d.conn = conn
	d.connMu.Unlock()
	return conn, nil
}

// getConnLocked returns the cached conn with connMu already held.
func (d *tcpDest) getConnLocked() (net.Conn, error) {
	conn := d.conn
	d.connMu.Unlock()
	if conn == nil {
		return nil, errTCPClosed
	}
	return conn, nil
}

// dropConn closes and forgets the cached conn; the next attempt re-dials.
func (d *tcpDest) dropConn() {
	d.connMu.Lock()
	conn := d.conn
	d.conn = nil
	d.connDown = true
	d.connMu.Unlock()
	if conn != nil {
		_ = conn.Close() //nolint:errcheck // teardown best-effort; the process is exiting
	}
}

func (d *tcpDest) Close() error {
	var err error
	d.closeOnce.Do(func() {
		close(d.closedCh)
		select {
		case <-d.workerDone:
			d.dropConn()
		case <-time.After(d.drainTimeout):
			// The drain window closed with a write or dial still in flight;
			// force the conn closed so the worker unwinds instead of parking
			// in a full write-deadline block.
			d.dropConn()
			err = fmt.Errorf("tcp destination worker drain timeout after %s", d.drainTimeout)
		}
	})
	return err
}

// tcpWorkerDrainTimeoutCst bounds Close's wait on the worker.
const tcpWorkerDrainTimeoutCst = 5 * time.Second

// tcpQueueMetrics are the bounded queue's drop counters and buffer gauge —
// the tcp mirror of the s3parquet queue metrics.
type tcpQueueMetrics struct {
	dropped      *prometheus.CounterVec // envelopes, by reason
	droppedBytes *prometheus.CounterVec // bytes, by reason
	buffered     *prometheus.GaugeVec   // bytes held, by stage
}

func newTCPQueueMetrics(reg prometheus.Registerer) *tcpQueueMetrics {
	if reg == nil {
		reg = prometheus.DefaultRegisterer
	}
	f := promauto.With(reg)
	return &tcpQueueMetrics{
		dropped: f.NewCounterVec(prometheus.CounterOpts{
			Subsystem: promSubsystemXTCP,
			Name:      "tcp_dropped_total",
			Help:      "Envelopes the tcp destination dropped, oldest first: reason=queue_depth (-tcpQueueDepth), buffered_bytes (-tcpMaxBufferedBytes), attempts (write attempts exhausted — the sink was unreachable for tcpWriteTimeout x attempts), shutdown (still queued at close).",
		}, []string{promLabelReasonCst}),
		droppedBytes: f.NewCounterVec(prometheus.CounterOpts{
			Subsystem: promSubsystemXTCP,
			Name:      "tcp_dropped_bytes_total",
			Help:      "Marshaled envelope bytes the tcp destination dropped, by the same reasons as tcp_dropped_total.",
		}, []string{promLabelReasonCst}),
		buffered: f.NewGaugeVec(prometheus.GaugeOpts{
			Subsystem: promSubsystemXTCP,
			Name:      "tcp_buffered_bytes",
			Help:      "Bytes the tcp destination holds: stage=queue (marshaled envelopes awaiting the worker).",
		}, []string{"stage"}),
	}
}

func init() {
	RegisterDestination(schemeTCP, newTCPDest)
}
