//go:build dest_s3parquet

package xtcp

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"log"
	"path"
	"strings"
	"sync"
	"time"

	"github.com/minio/minio-go/v7"
	"github.com/minio/minio-go/v7/pkg/credentials"
	"github.com/parquet-go/parquet-go"
	"google.golang.org/protobuf/encoding/protodelim"

	"github.com/randomizedcoder/xtcp2/pkg/misc"
	"github.com/randomizedcoder/xtcp2/pkg/xtcp_config"
	"github.com/randomizedcoder/xtcp2/pkg/xtcp_flat_record"
)

// S3ParquetFlushThresholdBytesCst is the default soft cap (≈63 MiB) on
// the in-memory Parquet builder's accumulated uncompressed row bytes.
// Output Parquet objects will be smaller after column compression but
// bounded above by this value. Operator-tunable via config / env / flag.
const S3ParquetFlushThresholdBytesCst = 63 * 1024 * 1024

// s3ParquetDestQueueCapacity bounds the in-flight backlog between
// Send() and the worker. Full queue → Send blocks; queueFull counter
// bumps so operators can spot back-pressure.
const s3ParquetDestQueueCapacity = 16

// s3ParquetWorkerDrainTimeout caps how long Close() will wait for the
// worker to flush its final partial Parquet to S3 before giving up.
const s3ParquetWorkerDrainTimeout = 30 * time.Second

// s3ParquetUploadMaxAttempts is the default cap on upload attempts (original +
// retries) when the config leaves s3_upload_max_attempts unset (0). Retries use
// full-jitter exponential backoff. See docs/design-jitter-and-backoff.md.
const s3ParquetUploadMaxAttempts = 10

// s3FlushIntervalFloorCst is the floor for the derived staleness-ceiling flush
// interval (used when s3_flush_interval is 0). Sitting just above the typical
// byte-cap fill time keeps the byte cap the dominant driver at high poll
// frequencies, so the fleet doesn't storm the bucket with tiny objects.
const s3FlushIntervalFloorCst = 30 * time.Minute

// Upload retry backoff bounds. The per-attempt window grows as
// base<<(attempt-1) and is clamped to [min,max]; full jitter then draws the
// actual sleep in [0, window]. When s3_upload_backoff_cap is 0 the cap is
// derived as clamp(PollFrequency/10, min, max).
const (
	s3UploadBackoffBaseCst   = 1 * time.Second
	s3UploadBackoffCapMinCst = 1 * time.Second
	s3UploadBackoffCapMaxCst = 1 * time.Hour
)

// parquetUploader is the surface the worker needs from a backing object
// store. Real production uses a minio.Client wrapper; tests use a fake
// (recording / error-injecting) implementation so the worker logic can
// be exercised without a live S3 endpoint.
type parquetUploader interface {
	PutObject(ctx context.Context, bucket, key string, body io.Reader, size int64) error
}

// minioUploader adapts *minio.Client to the parquetUploader interface.
type minioUploader struct{ client *minio.Client }

func (m *minioUploader) PutObject(ctx context.Context, bucket, key string, body io.Reader, size int64) error {
	_, err := m.client.PutObject(ctx, bucket, key, body, size, minio.PutObjectOptions{
		ContentType: "application/octet-stream",
	})
	return err
}

type s3ParquetDest struct {
	x         *XTCP
	uploader  parquetUploader
	bucket    string
	prefix    string // optional path prefix WITHIN the bucket; may be ""
	threshold int    // accumulated uncompressed bytes before finalize

	// Fleet jitter & backoff knobs, resolved from config in newS3ParquetDest.
	// See docs/design-jitter-and-backoff.md.
	flushInterval      time.Duration // staleness ceiling (already floored)
	flushJitterPct     uint32        // jitter % applied to flushInterval
	thresholdJitterPct uint32        // per-object downward jitter % on threshold
	maxAttempts        int           // upload attempts before dropping
	backoffCap         time.Duration // cap on a single retry's jitter window

	// Seams (per-instance → race-free; production defaults set in
	// newS3ParquetDest, tests inject deterministic stubs). jitterDur/jitterInt
	// draw jitter; sleep is the ctx-aware backoff sleep; newTimer builds the
	// worker's flush timer (tests hand back a channel they fire manually).
	jitterDur func(time.Duration) time.Duration
	jitterInt func(int) int
	sleep     func(context.Context, time.Duration) bool
	newTimer  func(time.Duration) (<-chan time.Time, func() bool)

	// queueCh carries marshalled envelopes from Send to the worker.
	// IMPORTANT: never closed by Close (sending on a closed channel
	// panics, and Close races with concurrent Sends). The worker exits
	// via closedCh instead, draining queueCh's residual items first.
	queueCh chan envelopeBytes

	// closedCh is closed by Close exactly once. Send checks it before
	// each channel-send and bails with errSendOnClosed if closed.
	closedCh chan struct{}

	workerDone chan struct{}
	closeOnce  sync.Once
}

// errSendOnClosed is returned by Send when the destination has been
// Close'd. Callers in flushEnvelope log + counter-bump; the daemon
// itself doesn't treat this as fatal (shutdown is in progress).
var errSendOnClosed = errors.New("s3parquet destination closed")

// envelopeBytes is the queue payload — pointer to the pooled marshalled
// envelope. The worker is responsible for returning *buf to destBytesPool
// after consuming it.
type envelopeBytes struct {
	buf *[]byte
}

// newS3ParquetDest dials MinIO/S3 from the configured endpoint + creds,
// validates the bucket exists, and spawns the background worker. Fails
// fast on config errors so a misconfigured deployment doesn't enter a
// half-broken state.
func newS3ParquetDest(ctx context.Context, x *XTCP) (Destination, error) {
	endpoint := strings.TrimPrefix(x.config.Dest, schemeS3Parquet+":")
	if endpoint == "" {
		endpoint = x.config.S3Endpoint
	}
	if endpoint == "" {
		return nil, errors.New("newS3ParquetDest endpoint is empty (set -dest s3parquet:<endpoint> or S3_ENDPOINT)")
	}
	// minio.New expects host:port without scheme. Strip http:// or https://
	// for the Endpoint field; the boolean Secure flag controls TLS.
	secure := false
	if strings.HasPrefix(endpoint, "https://") {
		secure = true
		endpoint = strings.TrimPrefix(endpoint, "https://")
	} else if strings.HasPrefix(endpoint, "http://") {
		endpoint = strings.TrimPrefix(endpoint, "http://")
	}

	bucket := x.config.S3Bucket
	if bucket == "" {
		return nil, errors.New("newS3ParquetDest S3_BUCKET is empty")
	}
	accessKey := x.config.S3AccessKey
	secretKey := x.config.S3SecretKey
	region := x.config.S3Region
	if region == "" {
		region = "us-east-1"
	}

	client, err := minio.New(endpoint, &minio.Options{
		Creds:  credentials.NewStaticV4(accessKey, secretKey, ""),
		Secure: secure,
		Region: region,
	})
	if err != nil {
		return nil, fmt.Errorf("newS3ParquetDest minio.New: %w", err)
	}

	// Bucket existence probe — separate context so it can't be canceled by
	// the parent before we've decided whether to dial. BucketExists issues a
	// HeadBucket, which requires s3:ListBucket; skip it when the credential is
	// deliberately scoped to s3:PutObject only (write-only key). We then trust
	// the operator that the bucket exists and let the first upload surface any
	// misconfiguration instead of failing fast here.
	if x.config.S3SkipBucketProbe {
		log.Printf("newS3ParquetDest S3_SKIP_BUCKET_PROBE set: skipping BucketExists probe for bucket %q on %q", bucket, endpoint)
	} else {
		bucketCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
		defer cancel()
		exists, err := client.BucketExists(bucketCtx, bucket)
		if err != nil {
			return nil, fmt.Errorf("newS3ParquetDest BucketExists(%q): %w", bucket, err)
		}
		if !exists {
			return nil, fmt.Errorf("newS3ParquetDest bucket %q does not exist on %q", bucket, endpoint)
		}
	}

	threshold := int(x.config.S3ParquetFlushThresholdBytes)
	if threshold == 0 {
		threshold = S3ParquetFlushThresholdBytesCst
	}

	maxAttempts := int(x.config.S3UploadMaxAttempts)
	if maxAttempts == 0 {
		maxAttempts = s3ParquetUploadMaxAttempts
	}

	d := &s3ParquetDest{
		x:                  x,
		uploader:           &minioUploader{client: client},
		bucket:             bucket,
		prefix:             x.config.S3Prefix,
		threshold:          threshold,
		flushInterval:      resolveFlushInterval(x.config),
		flushJitterPct:     x.config.S3FlushJitterPct,
		thresholdJitterPct: x.config.S3FlushThresholdJitterPct,
		maxAttempts:        maxAttempts,
		backoffCap:         resolveBackoffCap(x.config),
		jitterDur:          misc.JitterDuration,
		jitterInt:          misc.JitterIntN,
		sleep:              misc.SleepCtx,
		newTimer:           func(dur time.Duration) (<-chan time.Time, func() bool) { t := time.NewTimer(dur); return t.C, t.Stop },
		queueCh:            make(chan envelopeBytes, s3ParquetDestQueueCapacity),
		closedCh:           make(chan struct{}),
		workerDone:         make(chan struct{}),
	}
	go d.worker()
	return d, nil
}

// resolveFlushInterval returns the staleness-ceiling flush interval: the
// configured s3_flush_interval, or when unset (0) the derived
// max(PollFrequency, 30m) floor (see s3FlushIntervalFloorCst).
func resolveFlushInterval(c *xtcp_config.XtcpConfig) time.Duration {
	if d := c.S3FlushInterval.AsDuration(); d > 0 {
		return d
	}
	if freq := c.PollFrequency.AsDuration(); freq > s3FlushIntervalFloorCst {
		return freq
	}
	return s3FlushIntervalFloorCst
}

// resolveBackoffCap returns the cap on a single upload retry's jitter window:
// the configured s3_upload_backoff_cap, or when unset (0) the derived
// clamp(PollFrequency/10, 1s, 1h).
func resolveBackoffCap(c *xtcp_config.XtcpConfig) time.Duration {
	if d := c.S3UploadBackoffCap.AsDuration(); d > 0 {
		return d
	}
	capDur := c.PollFrequency.AsDuration() / 10
	if capDur < s3UploadBackoffCapMinCst {
		capDur = s3UploadBackoffCapMinCst
	}
	if capDur > s3UploadBackoffCapMaxCst {
		capDur = s3UploadBackoffCapMaxCst
	}
	return capDur
}

// Send enqueues the marshalled envelope for the background worker. The
// fast path is a non-blocking channel send (queue has slack); if the
// worker is behind (e.g. mid-upload), Send falls back to a blocking
// send and bumps queueFull so operators can spot the back-pressure.
//
// closedCh is checked in every select so Send never tries to write to a
// closed-and-replaced queueCh (which would panic). Sends arriving after
// Close return errSendOnClosed and refund the buffer to destBytesPool
// so the upstream pool stays warm.
//
// Returns (1, nil) on enqueue to mirror the per-record accounting the
// caller (flushEnvelope in poller.go) expects.
func (d *s3ParquetDest) Send(ctx context.Context, b *[]byte) (int, error) {
	// Closed-first fast check so Sends arriving after Close exit cheaply.
	select {
	case <-d.closedCh:
		d.refundOnReject(b)
		return 0, errSendOnClosed
	default:
	}
	// Non-blocking enqueue when queue has slack.
	select {
	case d.queueCh <- envelopeBytes{buf: b}:
		return 1, nil
	case <-d.closedCh:
		d.refundOnReject(b)
		return 0, errSendOnClosed
	default:
	}
	// Queue full → blocking path. Bump counter so back-pressure shows up
	// in dashboards.
	if d.x.pC != nil {
		d.x.pC.WithLabelValues("destS3Parquet", "queueFull", "error").Inc()
	}
	select {
	case d.queueCh <- envelopeBytes{buf: b}:
		return 1, nil
	case <-d.closedCh:
		d.refundOnReject(b)
		return 0, errSendOnClosed
	case <-ctx.Done():
		d.refundOnReject(b)
		return 0, ctx.Err()
	}
}

// refundOnReject returns a buffer to destBytesPool when Send fails
// before enqueueing — keeps the pool warm and prevents the upstream
// flushEnvelope from leaking the *[]byte.
func (d *s3ParquetDest) refundOnReject(b *[]byte) {
	*b = (*b)[:0]
	d.x.destBytesPool.Put(b)
}

// Close signals the worker to drain and waits up to
// s3ParquetWorkerDrainTimeout for the final partial Parquet to flush.
// Idempotent — second call is a no-op. Returns the drain-timeout error
// if the worker doesn't finish in time, but the daemon shuts down
// regardless (closeDestination is best-effort during teardown).
//
// Closes closedCh only — never closes queueCh, since concurrent Sends
// would panic on a send-to-closed channel. The worker drains queueCh
// via its own select on closedCh.
func (d *s3ParquetDest) Close() error {
	var err error
	d.closeOnce.Do(func() {
		close(d.closedCh)
		select {
		case <-d.workerDone:
		case <-time.After(s3ParquetWorkerDrainTimeout):
			err = fmt.Errorf("s3parquet worker drain timeout after %s", s3ParquetWorkerDrainTimeout)
		}
	})
	return err
}

// worker is the only goroutine that touches the Parquet builder.
// Receives marshalled envelopes from queueCh, decodes them, appends each
// row to the in-memory writer, and finalizes + uploads when the
// accumulated byte threshold is reached. On queue close (Close was
// called) finalizes whatever's left and exits.
func (d *s3ParquetDest) worker() {
	defer close(d.workerDone)

	var (
		buf                *bytes.Buffer
		writer             *parquet.GenericWriter[ParquetRow]
		accumBytes         int
		fileRows           int
		envelopeCt         int
		effectiveThreshold int
	)
	startBuilder := func() {
		buf = new(bytes.Buffer)
		writer = parquet.NewGenericWriter[ParquetRow](buf)
		accumBytes = 0
		fileRows = 0
		// Per-object downward threshold jitter: each object finalizes at a
		// fresh random target in [threshold*(1-pct/100), threshold], so
		// identically-loaded hosts don't cross the byte cap in lockstep.
		effectiveThreshold = jitteredThreshold(d.threshold, d.thresholdJitterPct, d.jitterInt)
	}
	startBuilder()

	finalize := func() {
		if fileRows == 0 {
			// Nothing to upload; reset for next batch.
			startBuilder()
			return
		}
		if err := writer.Close(); err != nil {
			log.Printf("destS3Parquet writer.Close: %v", err)
			if d.x.pC != nil {
				d.x.pC.WithLabelValues("destS3Parquet", "writerClose", "error").Inc()
			}
			startBuilder()
			return
		}
		uploadCtx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
		key := d.objectKey()
		d.uploadWithRetry(uploadCtx, key, buf, fileRows)
		cancel()
		startBuilder()
	}

	processItem := func(item envelopeBytes) {
		envelopeCt++
		var env xtcp_flat_record.Envelope
		if err := protodelim.UnmarshalFrom(bytes.NewReader(*item.buf), &env); err != nil {
			if d.x.pC != nil {
				d.x.pC.WithLabelValues("destS3Parquet", "unmarshal", "error").Inc()
			}
			d.returnBuf(item.buf)
			return
		}
		d.returnBuf(item.buf)
		for _, row := range env.Row {
			parquetRow := rowFromProto(row)
			if _, err := writer.Write([]ParquetRow{parquetRow}); err != nil {
				if d.x.pC != nil {
					d.x.pC.WithLabelValues("destS3Parquet", "write", "error").Inc()
				}
				continue
			}
			fileRows++
			accumBytes += approxRowBytes(row)
			if accumBytes >= effectiveThreshold {
				finalize()
			}
		}
	}

	// Staleness-ceiling flush timer. Fires on a jittered interval so the
	// fleet doesn't ceiling-flush in lockstep, and bounds how long a
	// low-volume host's data may sit unshipped. The first fire is also
	// jittered so a synchronized start doesn't produce a synchronized first
	// ceiling flush.
	flushCh, flushStop := d.newTimer(d.jitterDur(d.flushInterval))
	defer flushStop()

	for {
		select {
		case <-d.closedCh:
			// Drain any items already enqueued (a Send that won the
			// race against closedCh and got onto the channel before
			// the close), then exit.
			for {
				select {
				case item := <-d.queueCh:
					processItem(item)
				default:
					finalize()
					return
				}
			}
		case item := <-d.queueCh:
			processItem(item)
		case <-flushCh:
			finalize() // no-op when the builder is empty
			flushStop()
			flushCh, flushStop = d.newTimer(nextFlushDelay(d.flushInterval, d.flushJitterPct, d.jitterDur))
		}
	}
}

// jitteredThreshold returns the per-object finalize target: threshold minus a
// downward jitter draw in [0, threshold*pct/100]. Downward-only, so the target
// never exceeds threshold (preserving the in-memory byte bound). pct==0 (via
// jitterInt(0)==0) yields exactly threshold. jitterInt is injected so tests can
// force a value.
func jitteredThreshold(threshold int, pct uint32, jitterInt func(int) int) int {
	return threshold - jitterInt(misc.ScaleIntPct(threshold, pct))
}

// nextFlushDelay returns the next staleness-ceiling flush delay: interval
// shifted by a random offset in [-max/2, +max/2) where max = interval*pct/100,
// so the mean stays interval. pct==0 yields exactly interval.
func nextFlushDelay(interval time.Duration, pct uint32, jitter func(time.Duration) time.Duration) time.Duration {
	limit := misc.ScalePct(interval, pct)
	if limit <= 0 {
		return interval
	}
	return interval - limit/2 + jitter(limit)
}

// returnBuf zeroes the slice and returns it to destBytesPool so the
// upstream pool stays warm. Mirrors the kafkaDest callback pattern.
func (d *s3ParquetDest) returnBuf(b *[]byte) {
	*b = (*b)[:0]
	d.x.destBytesPool.Put(b)
}

// uploadWithRetry does up to d.maxAttempts PutObject calls with full-jitter
// exponential backoff between failures: the per-attempt window grows as
// base<<(attempt-1) capped at d.backoffCap, and the actual sleep is a uniform
// draw in [0, window] (AWS-style full jitter), which de-correlates a fleet's
// retries when a shared dependency recovers. The backoff sleep is ctx-aware, so
// shutdown (or the 60s upload-ctx deadline) aborts promptly. On terminal
// failure it logs + bumps an error counter and drops the batch — the daemon
// keeps running; data loss is the documented failure mode for prolonged s3
// outages. See docs/design-jitter-and-backoff.md.
func (d *s3ParquetDest) uploadWithRetry(ctx context.Context, key string, buf *bytes.Buffer, rows int) {
	body := bytes.NewReader(buf.Bytes())
	size := int64(buf.Len())
	for attempt := 1; attempt <= d.maxAttempts; attempt++ {
		if attempt > 1 {
			// Rewind to re-read the buffer for the retry. bytes.Reader.Seek
			// only errors on a negative offset, which SeekStart+0 never is;
			// check anyway rather than discard the error.
			if _, err := body.Seek(0, io.SeekStart); err != nil {
				log.Printf("destS3Parquet PUT %s/%s seek for retry: %v", d.bucket, key, err)
				break
			}
		}
		start := time.Now()
		err := d.uploader.PutObject(ctx, d.bucket, key, body, size)
		dur := time.Since(start)
		if err == nil {
			if d.x.pC != nil {
				d.x.pC.WithLabelValues("destS3Parquet", "upload", "count").Inc()
				d.x.pC.WithLabelValues("destS3Parquet", "uploadRows", "count").Add(float64(rows))
				d.x.pC.WithLabelValues("destS3Parquet", "uploadBytes", "count").Add(float64(size))
			}
			if d.x.pH != nil {
				d.x.pH.WithLabelValues("destS3Parquet", "uploadDuration", "count").Observe(dur.Seconds())
			}
			if d.x.debugLevel > 10 {
				log.Printf("destS3Parquet PUT %s/%s size=%d rows=%d attempt=%d dur=%s",
					d.bucket, key, size, rows, attempt, dur)
			}
			return
		}
		// errMsg is intentionally constructed to avoid embedding the
		// secret key — minio-go's error already includes endpoint but
		// not credentials. Defense in depth.
		errMsg := err.Error()
		log.Printf("destS3Parquet PUT %s/%s attempt %d/%d failed: %s",
			d.bucket, key, attempt, d.maxAttempts, errMsg)
		if d.x.pC != nil {
			d.x.pC.WithLabelValues("destS3Parquet", "uploadRetry", "error").Inc()
		}
		if attempt == d.maxAttempts {
			break
		}
		// Full-jitter exponential backoff; ctx-aware so shutdown aborts.
		window := backoffWindow(s3UploadBackoffBaseCst, d.backoffCap, attempt)
		if !d.sleep(ctx, d.jitterDur(window)) {
			return
		}
	}
	if d.x.pC != nil {
		d.x.pC.WithLabelValues("destS3Parquet", "upload", "error").Inc()
	}
	log.Printf("destS3Parquet PUT %s/%s permanently failed after %d attempts; dropping %d rows",
		d.bucket, key, d.maxAttempts, rows)
}

// backoffWindow returns min(cap, base<<(attempt-1)) for attempt>=1, computed
// overflow-safe (the shift is bounded before it can exceed int64).
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

// objectKey builds the partitioned key for the next Parquet object.
// Layout: <prefix>/host=<hostname>/date=<YYYY-MM-DD>/hour=<HH>/<unix_ts>_<rand>.parquet
//
// Hostname is sanitized to prevent path-traversal or weird characters
// reaching S3 (`..`, `/`, control chars, NULs). Empty hostname collapses
// to "unknown" so we never emit a key with an empty segment.
func (d *s3ParquetDest) objectKey() string {
	host := sanitizeHostnameForS3Key(d.x.hostname)
	now := time.Now().UTC()
	dateSeg := now.Format("2006-01-02")
	hourSeg := now.Format("15")
	randHex := randomHex(8)
	name := fmt.Sprintf("%d_%s.parquet", now.Unix(), randHex)
	key := path.Join(
		strings.Trim(d.prefix, "/"),
		"host="+host,
		"date="+dateSeg,
		"hour="+hourSeg,
		name,
	)
	// path.Join collapses leading "" segments, but a leading slash would
	// confuse some S3 implementations. Defensive trim.
	return strings.TrimPrefix(key, "/")
}

// sanitizeHostnameForS3Key reduces the input to a safe S3 path segment.
// Allowed: [A-Za-z0-9._-]. Anything else (NULs, `/`, `..`, unicode,
// control chars) is replaced with `_`. Empty input becomes "unknown".
func sanitizeHostnameForS3Key(h string) string {
	if h == "" {
		return "unknown"
	}
	out := make([]byte, 0, len(h))
	for i := 0; i < len(h); i++ {
		c := h[i]
		switch {
		case c >= 'a' && c <= 'z',
			c >= 'A' && c <= 'Z',
			c >= '0' && c <= '9',
			c == '.' || c == '_' || c == '-':
			out = append(out, c)
		default:
			out = append(out, '_')
		}
	}
	// Defense in depth: even if every byte allowed, a literal ".." would
	// be three dots resolved as a parent traversal once joined. Replace
	// it specifically. Belt and braces given path.Join also normalizes.
	cleaned := string(out)
	for strings.Contains(cleaned, "..") {
		cleaned = strings.ReplaceAll(cleaned, "..", "_")
	}
	if cleaned == "" {
		return "unknown"
	}
	return cleaned
}

// randomHex returns n hex chars from crypto/rand. Used for object-key
// uniqueness within the same second. Falls back to a fixed string on
// rand failure (should never happen, but don't take the daemon down).
func randomHex(n int) string {
	b := make([]byte, (n+1)/2)
	if _, err := rand.Read(b); err != nil {
		return "00000000"
	}
	return hex.EncodeToString(b)[:n]
}

// approxRowBytes is the size-cap estimator. parquet-go doesn't expose
// "bytes written since last reset" for an in-memory writer, so we
// estimate from each row's proto.Size — a conservative upper bound on
// the uncompressed columnar bytes. Sums over rows give an
// order-of-magnitude check before the threshold finalizes the file.
//
// Exact accounting would require reading writer.Buffer().Len() after
// each Write, but parquet-go buffers row groups in memory before
// emitting to the io.Writer — so buf.Len() lags reality. The proto.Size
// upper bound is good enough for the operator-visible threshold.
func approxRowBytes(r *xtcp_flat_record.XtcpFlatRecord) int {
	// Use parquet-go's reflection-light estimate: sum of string + bytes
	// field lengths + a fixed-cost slack for the numeric columns
	// (122 fields × 4-8 bytes ≈ ~600 bytes baseline; round up to 800).
	const numericBaseline = 800
	n := numericBaseline
	n += len(r.Hostname) + len(r.Netns) + len(r.Label) + len(r.Tag) +
		len(r.CongestionAlgorithmString)
	n += len(r.InetDiagMsgSocketSource) + len(r.InetDiagMsgSocketDestination)
	return n
}

// rowFromProto translates one *xtcp_flat_record.XtcpFlatRecord into a
// ParquetRow value. Mechanical field-by-field copy. New proto fields
// surface here as a compile error (the ParquetRow struct doesn't have
// the field yet) — drift defense alongside the runtime schema test in
// destinations_s3parquet_schema_test.go.
func rowFromProto(r *xtcp_flat_record.XtcpFlatRecord) ParquetRow {
	return ParquetRow{
		TimestampNs: r.TimestampNs,

		Hostname: r.Hostname,
		Location: r.Location,

		Netns:            r.Netns,
		ContainerId:      r.ContainerId,
		ContainerRuntime: r.ContainerRuntime,
		Nsid:             r.Nsid,

		Label: r.Label,
		Tag:   r.Tag,

		RecordCounter: r.RecordCounter,
		SocketFd:      r.SocketFd,
		NetlinkerId:   r.NetlinkerId,

		InetDiagMsgFamily:                r.InetDiagMsgFamily,
		InetDiagMsgState:                 r.InetDiagMsgState,
		InetDiagMsgTimer:                 r.InetDiagMsgTimer,
		InetDiagMsgRetrans:               r.InetDiagMsgRetrans,
		InetDiagMsgSocketSourcePort:      r.InetDiagMsgSocketSourcePort,
		InetDiagMsgSocketDestinationPort: r.InetDiagMsgSocketDestinationPort,
		InetDiagMsgSocketSource:          r.InetDiagMsgSocketSource,
		InetDiagMsgSocketDestination:     r.InetDiagMsgSocketDestination,
		InetDiagMsgSocketInterface:       r.InetDiagMsgSocketInterface,
		InetDiagMsgSocketCookie:          r.InetDiagMsgSocketCookie,
		InetDiagMsgSocketDestAsn:         r.InetDiagMsgSocketDestAsn,
		InetDiagMsgSocketNextHopAsn:      r.InetDiagMsgSocketNextHopAsn,
		InetDiagMsgExpires:               r.InetDiagMsgExpires,
		InetDiagMsgRqueue:                r.InetDiagMsgRqueue,
		InetDiagMsgWqueue:                r.InetDiagMsgWqueue,
		InetDiagMsgUid:                   r.InetDiagMsgUid,
		InetDiagMsgInode:                 r.InetDiagMsgInode,

		MemInfoRmem: r.MemInfoRmem,
		MemInfoWmem: r.MemInfoWmem,
		MemInfoFmem: r.MemInfoFmem,
		MemInfoTmem: r.MemInfoTmem,

		TcpInfoState:                  r.TcpInfoState,
		TcpInfoCaState:                r.TcpInfoCaState,
		TcpInfoRetransmits:            r.TcpInfoRetransmits,
		TcpInfoProbes:                 r.TcpInfoProbes,
		TcpInfoBackoff:                r.TcpInfoBackoff,
		TcpInfoOptions:                r.TcpInfoOptions,
		TcpInfoSendScale:              r.TcpInfoSendScale,
		TcpInfoRcvScale:               r.TcpInfoRcvScale,
		TcpInfoDeliveryRateAppLimited: r.TcpInfoDeliveryRateAppLimited,
		TcpInfoFastOpenClientFailed:   r.TcpInfoFastOpenClientFailed,
		TcpInfoRto:                    r.TcpInfoRto,
		TcpInfoAto:                    r.TcpInfoAto,
		TcpInfoSndMss:                 r.TcpInfoSndMss,
		TcpInfoRcvMss:                 r.TcpInfoRcvMss,
		TcpInfoUnacked:                r.TcpInfoUnacked,
		TcpInfoSacked:                 r.TcpInfoSacked,
		TcpInfoLost:                   r.TcpInfoLost,
		TcpInfoRetrans:                r.TcpInfoRetrans,
		TcpInfoFackets:                r.TcpInfoFackets,
		TcpInfoLastDataSent:           r.TcpInfoLastDataSent,
		TcpInfoLastAckSent:            r.TcpInfoLastAckSent,
		TcpInfoLastDataRecv:           r.TcpInfoLastDataRecv,
		TcpInfoLastAckRecv:            r.TcpInfoLastAckRecv,
		TcpInfoPmtu:                   r.TcpInfoPmtu,
		TcpInfoRcvSsthresh:            r.TcpInfoRcvSsthresh,
		TcpInfoRtt:                    r.TcpInfoRtt,
		TcpInfoRttVar:                 r.TcpInfoRttVar,
		TcpInfoSndSsthresh:            r.TcpInfoSndSsthresh,
		TcpInfoSndCwnd:                r.TcpInfoSndCwnd,
		TcpInfoAdvMss:                 r.TcpInfoAdvMss,
		TcpInfoReordering:             r.TcpInfoReordering,
		TcpInfoRcvRtt:                 r.TcpInfoRcvRtt,
		TcpInfoRcvSpace:               r.TcpInfoRcvSpace,
		TcpInfoTotalRetrans:           r.TcpInfoTotalRetrans,
		TcpInfoPacingRate:             r.TcpInfoPacingRate,
		TcpInfoMaxPacingRate:          r.TcpInfoMaxPacingRate,
		TcpInfoBytesAcked:             r.TcpInfoBytesAcked,
		TcpInfoBytesReceived:          r.TcpInfoBytesReceived,
		TcpInfoSegsOut:                r.TcpInfoSegsOut,
		TcpInfoSegsIn:                 r.TcpInfoSegsIn,
		TcpInfoNotSentBytes:           r.TcpInfoNotSentBytes,
		TcpInfoMinRtt:                 r.TcpInfoMinRtt,
		TcpInfoDataSegsIn:             r.TcpInfoDataSegsIn,
		TcpInfoDataSegsOut:            r.TcpInfoDataSegsOut,
		TcpInfoDeliveryRate:           r.TcpInfoDeliveryRate,
		TcpInfoBusyTime:               r.TcpInfoBusyTime,
		TcpInfoRwndLimited:            r.TcpInfoRwndLimited,
		TcpInfoSndbufLimited:          r.TcpInfoSndbufLimited,
		TcpInfoDelivered:              r.TcpInfoDelivered,
		TcpInfoDeliveredCe:            r.TcpInfoDeliveredCe,
		TcpInfoBytesSent:              r.TcpInfoBytesSent,
		TcpInfoBytesRetrans:           r.TcpInfoBytesRetrans,
		TcpInfoDsackDups:              r.TcpInfoDsackDups,
		TcpInfoReordSeen:              r.TcpInfoReordSeen,
		TcpInfoRcvOoopack:             r.TcpInfoRcvOoopack,
		TcpInfoSndWnd:                 r.TcpInfoSndWnd,
		TcpInfoRcvWnd:                 r.TcpInfoRcvWnd,
		TcpInfoRehash:                 r.TcpInfoRehash,
		TcpInfoTotalRto:               r.TcpInfoTotalRto,
		TcpInfoTotalRtoRecoveries:     r.TcpInfoTotalRtoRecoveries,
		TcpInfoTotalRtoTime:           r.TcpInfoTotalRtoTime,

		CongestionAlgorithmString: r.CongestionAlgorithmString,
		CongestionAlgorithmEnum:   int32(r.CongestionAlgorithmEnum),

		TypeOfService: r.TypeOfService,
		TrafficClass:  r.TrafficClass,

		SkMemInfoRmemAlloc:  r.SkMemInfoRmemAlloc,
		SkMemInfoRcvBuf:     r.SkMemInfoRcvBuf,
		SkMemInfoWmemAlloc:  r.SkMemInfoWmemAlloc,
		SkMemInfoSndBuf:     r.SkMemInfoSndBuf,
		SkMemInfoFwdAlloc:   r.SkMemInfoFwdAlloc,
		SkMemInfoWmemQueued: r.SkMemInfoWmemQueued,
		SkMemInfoOptmem:     r.SkMemInfoOptmem,
		SkMemInfoBacklog:    r.SkMemInfoBacklog,
		SkMemInfoDrops:      r.SkMemInfoDrops,

		ShutdownState: r.ShutdownState,

		VegasInfoEnabled: r.VegasInfoEnabled,
		VegasInfoRttCnt:  r.VegasInfoRttCnt,
		VegasInfoRtt:     r.VegasInfoRtt,
		VegasInfoMinRtt:  r.VegasInfoMinRtt,

		DctcpInfoEnabled: r.DctcpInfoEnabled,
		DctcpInfoCeState: r.DctcpInfoCeState,
		DctcpInfoAlpha:   r.DctcpInfoAlpha,
		DctcpInfoAbEcn:   r.DctcpInfoAbEcn,
		DctcpInfoAbTot:   r.DctcpInfoAbTot,

		BbrInfoBwLo:       r.BbrInfoBwLo,
		BbrInfoBwHi:       r.BbrInfoBwHi,
		BbrInfoMinRtt:     r.BbrInfoMinRtt,
		BbrInfoPacingGain: r.BbrInfoPacingGain,
		BbrInfoCwndGain:   r.BbrInfoCwndGain,

		ClassId: r.ClassId,
		SockOpt: r.SockOpt,
		CGroup:  r.CGroup,
	}
}

func init() {
	RegisterDestination(schemeS3Parquet, newS3ParquetDest)
	// Bare scheme: the endpoint/bucket come from -s3Endpoint/S3_ENDPOINT etc.
	// (newS3ParquetDest falls back to config.S3Endpoint when the dest payload
	// is empty).
	RegisterLibraryDefaultDest(schemeS3Parquet, schemeS3Parquet+":")
}
