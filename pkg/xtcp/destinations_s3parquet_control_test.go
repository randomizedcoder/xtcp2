//go:build dest_s3parquet

package xtcp

import (
	"testing"
	"time"
)

// TestS3ParquetDest_setS3FlushControl drives a runtime upload-timing change
// through the worker's setS3FlushCh arm and verifies it (a) re-arms the flush
// timer to the new interval and (b) updates flushInterval + threshold. The
// worker owns those fields, so the test reads them only AFTER Close() has
// joined the worker goroutine (happens-before via workerDone), and uses the
// newTimer stub's delay stream as the in-flight synchronization point.
func TestS3ParquetDest_setS3FlushControl(t *testing.T) {
	timerDelays := make(chan time.Duration, 8)
	d, _, _ := newS3ParquetFixtureCustom(t, 1<<30, nil, func(dd *s3ParquetDest) {
		dd.flushInterval = time.Hour
		dd.flushJitterPct = 0 // nextFlushDelay returns exactly flushInterval
		dd.newTimer = func(delay time.Duration) (<-chan time.Time, func() bool) {
			timerDelays <- delay
			return make(chan time.Time), func() bool { return true }
		}
		dd.x.setS3FlushCh = make(chan s3FlushControl, 2)
	})

	// The worker arms its flush timer once at startup; draining it guarantees
	// the worker has entered its select loop before we send the control.
	select {
	case <-timerDelays:
	case <-time.After(2 * time.Second):
		t.Fatal("worker never armed its initial flush timer")
	}

	newIv := 5 * time.Second
	newThr := uint32(4096)
	d.x.setS3FlushCh <- s3FlushControl{interval: &newIv, thresholdBytes: &newThr}

	// The interval change re-arms the timer with exactly the new interval
	// (jitter pct 0). Observing this proves the control was consumed.
	select {
	case got := <-timerDelays:
		if got != newIv {
			t.Errorf("re-armed flush timer delay = %v, want %v", got, newIv)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("worker did not re-arm the flush timer after SetS3Upload control")
	}

	// Join the worker, then read the fields race-free.
	if err := d.Close(); err != nil {
		t.Fatalf("Close err: %v", err)
	}
	if d.flushInterval != newIv {
		t.Errorf("flushInterval = %v, want %v", d.flushInterval, newIv)
	}
	if d.threshold != int(newThr) {
		t.Errorf("threshold = %d, want %d", d.threshold, newThr)
	}
}

// TestS3ParquetDest_setS3FlushControl_thresholdOnly verifies a threshold-only
// control leaves the flush timer untouched (no re-arm) and updates only the
// byte cap.
func TestS3ParquetDest_setS3FlushControl_thresholdOnly(t *testing.T) {
	timerDelays := make(chan time.Duration, 8)
	d, _, _ := newS3ParquetFixtureCustom(t, 1<<30, nil, func(dd *s3ParquetDest) {
		dd.flushInterval = time.Hour
		dd.newTimer = func(delay time.Duration) (<-chan time.Time, func() bool) {
			timerDelays <- delay
			return make(chan time.Time), func() bool { return true }
		}
		dd.x.setS3FlushCh = make(chan s3FlushControl, 2)
	})

	select {
	case <-timerDelays:
	case <-time.After(2 * time.Second):
		t.Fatal("worker never armed its initial flush timer")
	}

	newThr := uint32(2048)
	d.x.setS3FlushCh <- s3FlushControl{thresholdBytes: &newThr}

	// No interval → no re-arm. Give the worker a moment; the timer channel
	// must stay empty.
	select {
	case got := <-timerDelays:
		t.Fatalf("threshold-only control unexpectedly re-armed the timer (delay %v)", got)
	case <-time.After(100 * time.Millisecond):
	}

	if err := d.Close(); err != nil {
		t.Fatalf("Close err: %v", err)
	}
	if d.threshold != int(newThr) {
		t.Errorf("threshold = %d, want %d", d.threshold, newThr)
	}
	if d.flushInterval != time.Hour {
		t.Errorf("flushInterval changed to %v, want unchanged (1h)", d.flushInterval)
	}
}
