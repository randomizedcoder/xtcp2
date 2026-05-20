package xtcp

import (
	"context"
	"errors"
	"log"
	"runtime"
	"sync"
	"syscall"
	"time"

	xio "github.com/randomizedcoder/xtcp2/pkg/io_uring"
)

// ringCtxKey is the context.WithValue key under which netlinkerIoUring
// stashes the per-Netlinker ring so that io_uring destination functions
// (called from Deserialize → Destination chain) can find it.
type ringCtxKey struct{}

// ringFromContext returns the io_uring Ring associated with the current
// netlinker goroutine, or nil if no io_uring path is active. Destination
// functions use this to decide whether to enqueue an SQE or fall back to
// the syscall path.
func ringFromContext(ctx context.Context) *xio.Ring {
	v := ctx.Value(ringCtxKey{})
	if v == nil {
		return nil
	}
	ring, _ := v.(*xio.Ring) //nolint:errcheck // context.WithValue(ringCtxKey, ring) only writes *xio.Ring
	return ring
}

// iouringRecvBatchDefaultCst / iouringCqeBatchDefaultCst are the
// fallbacks applied when the config field is zero or negative. Pulled
// out as constants so tests can assert defaults via the same name.
const (
	iouringRecvBatchDefaultCst = 64
	iouringCqeBatchDefaultCst  = 128
	iouringTimeoutDefaultCst   = time.Second
)

// iouringResolveBatchSizes applies the production defaults to the
// recv-batch + CQE-batch config fields. Any non-positive value falls
// back to the constants above.
func iouringResolveBatchSizes(recvCfg, cqeCfg uint32) (recv, cqe int) {
	recv = int(recvCfg)
	if recv < 1 {
		recv = iouringRecvBatchDefaultCst
	}
	cqe = int(cqeCfg)
	if cqe < 1 {
		cqe = iouringCqeBatchDefaultCst
	}
	return recv, cqe
}

// iouringResolveTimeout converts NlTimeoutMilliseconds into a
// time.Duration, defaulting to 1 second when the config is zero. Takes
// uint64 to match the XtcpConfig.NlTimeoutMilliseconds field type
// (proto-generated; we don't get to pick the width).
func iouringResolveTimeout(nlTimeoutMs uint64) time.Duration {
	if nlTimeoutMs == 0 {
		return iouringTimeoutDefaultCst
	}
	return time.Duration(nlTimeoutMs) * time.Millisecond
}

// iouringRecordWaitErr classifies werr from iouringWaitWithTimeout.
// ETIME (io_uring wait timeout) bumps the Timeout counter; everything
// else bumps WaitErr and logs at debug>10. Caller always `continue`s
// after this returns.
func (x *XTCP) iouringRecordWaitErr(id uint32, werr error) {
	if isETimeError(werr) {
		x.pC.WithLabelValues("NetlinkerIoUring", "Timeout", "count").Inc()
		return
	}
	x.pC.WithLabelValues("NetlinkerIoUring", "WaitErr", "count").Inc()
	if x.debugLevel > 10 {
		log.Printf("netlinkerIoUring %d WaitOne err: %v", id, werr)
	}
}

// iouringProcessResults dispatches each CQE in results to either the
// recv-CQE handler (which also refills the slot) or the send-CQE
// handler.
func (x *XTCP) iouringProcessResults(ctxRing context.Context, ring *xio.Ring, nsName *string, fd int, id uint32, results []xio.Result) {
	for _, res := range results {
		switch res.Op {
		case xio.OpRead:
			x.handleRecvCQE(ctxRing, ring, nsName, fd, id, res)
			if rerr := x.iouringPrefillRecvs(ring, fd, 1); rerr != nil {
				x.pC.WithLabelValues("NetlinkerIoUring", "Refill", "error").Inc()
				if x.debugLevel > 10 {
					log.Printf("netlinkerIoUring %d refill err: %v", id, rerr)
				}
			}
		default:
			// Send CQEs (OpSendUDP/OpSendUnix/OpSendUnixGram) come
			// back here when io_uring destinations are active. The
			// ring's drain layer already returned res.Buf to the
			// caller; we just record the outcome.
			x.handleSendCQE(res)
		}
	}
}

// maybeForceGCIoUring fires runtime.GC every forceGCModulesCst packets.
// Unlike the syscall path's helper, the loop counter starts at 0 and
// increments BEFORE the modulus check, so packets==0 never enters
// here. Skipping again on packets==0 is belt-and-suspenders against
// future refactors of the loop body.
func (x *XTCP) maybeForceGCIoUring(packets uint64) {
	if packets == 0 || packets%forceGCModulesCst != 0 {
		return
	}
	x.pC.WithLabelValues("NetlinkerIoUring", "runtime.GC()", "count").Inc()
	runtime.GC()
}

// netlinkerIoUring is the opt-in io_uring variant of the Netlinker
// goroutine. It pre-submits a configurable batch of recvmsg SQEs against
// the netlink fd, drains CQEs as they arrive, refills each completed
// slot, and feeds the bytes into x.Deserialize exactly like the syscall
// path. Send SQEs queued by io_uring destination variants share the same
// ring and are flushed by the same Submit calls (one io_uring_enter per
// drain iteration).
//
// Periodic xtcp polling means the loop is mostly idle between dump
// cycles. WaitCQETimeout caps each wait at config.NlTimeoutMilliseconds
// so ctx cancellation is observed within that bound.
//
// The body was previously a gocyclo-18 monolith mixing config defaults,
// ring init, prefill, wait+drain+dispatch, refill, and GC bookkeeping.
// Each concern moved to a helper above; the remaining shell is just
// "init, defer cleanup, drive the loop" (gocyclo 7).
func (x *XTCP) netlinkerIoUring(ctx context.Context, wg *sync.WaitGroup, nsName *string, fd int, id uint32) {

	defer wg.Done()

	if x.debugLevel > 10 {
		log.Printf("NetlinkerIoUring %d started ns:%s fd:%d", id, *nsName, fd)
	}

	// Pin to the netns'd OS thread for the ring's lifetime. The kernel
	// associates io_uring fds with the netns of the creating task; the
	// fd we recv from must be in the same netns.
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()

	batch, cqeBatch := iouringResolveBatchSizes(x.config.IoUringRecvBatchSize, x.config.IoUringCqeBatchSize)

	ring, err := xio.New(xio.Config{
		RecvBatchSize: batch,
		CQEBatchSize:  cqeBatch,
	})
	if err != nil {
		// The previous code did `wg.Done(); UnlockOSThread(); log.Fatalf(...)`
		// with the explicit cleanup justified as "log.Fatalf skips the
		// deferred Done." That reasoning fails in two directions:
		//   * Production: log.Fatalf calls os.Exit, killing every
		//     goroutine — wg cleanup is irrelevant either way.
		//   * Tests where fatalf is mocked to return: the function would
		//     continue past Fatalf with ring==nil, panic on ring.Close
		//     in the deferred teardown, and the deferred wg.Done would
		//     then DOUBLE-decrement the WaitGroup that the explicit
		//     wg.Done already touched.
		// Just log + return; the deferred wg.Done and UnlockOSThread
		// handle cleanup once, and a mocked fatalf no longer leaves the
		// function half-initialized.
		log.Printf("netlinkerIoUring %d ring init: %v", id, err)
		return
	}
	x.rings.Store(id, ring)
	defer func() {
		x.rings.Delete(id)
		ring.Close(time.Second, func(res xio.Result) {
			x.onRingClosedResult(res)
		})
	}()

	ctxRing := context.WithValue(ctx, ringCtxKey{}, ring)

	// Pre-fill the SQ with `batch` recvmsg SQEs from the pool. Each one
	// gets pinned in the ring's in-flight map; the kernel will fill them
	// as netlink datagrams arrive.
	if perr := x.iouringPrefillRecvs(ring, fd, batch); perr != nil {
		// Demoted from log.Fatalf: a single namespace's prefill failure
		// shouldn't kill the whole daemon (gRPC services, poller, every
		// other namespace's netlinkers). Log + return so the deferred
		// ring.Close + wg.Done + UnlockOSThread fire normally.
		log.Printf("netlinkerIoUring %d prefill: %v", id, perr)
		return
	}
	if _, serr := ring.Submit(); serr != nil {
		log.Printf("netlinkerIoUring %d initial Submit: %v", id, serr)
	}

	// Use a Timespec equal to the netlink timeout so cancel polling and
	// "kernel has no more data" detection share one knob.
	nlTimeout := iouringResolveTimeout(x.config.NlTimeoutMilliseconds)

	packets := uint64(0)
	for !x.checkDoneNonBlocking(ctx) {
		results, werr := x.iouringWaitWithTimeout(ring, nlTimeout)
		if werr != nil {
			x.iouringRecordWaitErr(id, werr)
			continue
		}
		x.iouringProcessResults(ctxRing, ring, nsName, fd, id, results)
		if _, serr := ring.Submit(); serr != nil {
			x.pC.WithLabelValues("NetlinkerIoUring", "Submit", "error").Inc()
		}
		packets++
		x.maybeForceGCIoUring(packets)
	}

	x.pC.WithLabelValues("NetlinkerIoUring", "complete", "count").Inc()
}

// iouringPrefillRecvs gets n buffers from packetBufferPool and submits
// one recvmsg SQE per buffer. Each buffer is pinned in the ring's
// in-flight map until its CQE fires.
func (x *XTCP) iouringPrefillRecvs(ring *xio.Ring, fd int, n int) error {
	for i := 0; i < n; i++ {
		buf, _ := x.packetBufferPool.Get().(*[]byte) //nolint:errcheck // pool.New returns *[]byte
		// Restore full capacity so the kernel sees a writable buffer.
		*buf = (*buf)[:cap(*buf)]
		if _, err := ring.EnqueueRecvMsg(fd, buf); err != nil {
			x.packetBufferPool.Put(buf)
			return err
		}
	}
	return nil
}

// iouringWaitWithTimeout wraps WaitCQETimeout + DrainBatch.
func (x *XTCP) iouringWaitWithTimeout(ring *xio.Ring, d time.Duration) ([]xio.Result, error) {
	// The Ring API doesn't expose a direct timeout wait; we use the
	// underlying giouring helper via the wrapper. For now do a tight
	// non-blocking peek first (fast path), then block once with a real
	// timeout. This keeps a steady poll cadence.
	results := ring.DrainBatch()
	if len(results) > 0 {
		return results, nil
	}
	return ring.WaitOneTimeout(d)
}

// isETimeError returns true if the error is ETIME (io_uring's
// wait-timeout signal) or its Go equivalent.
func isETimeError(err error) bool {
	if err == nil {
		return false
	}
	// errors.As walks the unwrap chain (e.g. fmt.Errorf("...: %w", err)
	// → syscall.Errno), which the previous direct type-assert missed.
	// Keep the existing string fallback for libraries that stringify
	// errno without exposing the typed unwrap path.
	var errno syscall.Errno
	if errors.As(err, &errno) {
		return errno == syscall.ETIME
	}
	// Fallback: match by string for wrapped errors.
	if err.Error() == "errno 62" {
		return true
	}
	return false
}

// handleRecvCQE feeds the recv'd bytes into the deserializer and returns
// the buffer to the pool, mirroring the syscall path's contract.
func (x *XTCP) handleRecvCQE(ctx context.Context, ring *xio.Ring, nsName *string, fd int, id uint32, res xio.Result) {
	x.pC.WithLabelValues("NetlinkerIoUring", "recv", "count").Inc()
	if res.Res < 0 {
		// CQE result is -errno on error.
		errno := syscall.Errno(-res.Res)
		if isTimeoutErrno(errno) {
			x.pC.WithLabelValues("NetlinkerIoUring", "Timeout", "count").Inc()
		} else {
			x.pC.WithLabelValues("NetlinkerIoUring", "RecvErr", "count").Inc()
			if x.debugLevel > 10 {
				log.Printf("netlinkerIoUring %d recv err: %v", id, errno)
			}
		}
		if res.Buf != nil {
			x.packetBufferPool.Put(res.Buf)
		}
		return
	}

	n := int(res.Res)
	x.pC.WithLabelValues("NetlinkerIoUring", "packets", "count").Inc()
	x.pC.WithLabelValues("NetlinkerIoUring", "n", "count").Add(float64(n))

	// If drainOnce couldn't match the CQE to an in-flight entry (e.g.
	// post-Close stragglers, or — at >2^32 SQEs — a request-ID wrap
	// collision), res.Buf is nil. Dereferencing it would panic. Count
	// the orphan and skip; the buffer was never ours to return.
	if res.Buf == nil {
		x.pC.WithLabelValues("NetlinkerIoUring", "OrphanCQE", "error").Inc()
		return
	}

	b := (*res.Buf)[:n]
	p, errD := x.Deserialize(ctx, DeserializeArgs{
		ns:             nsName,
		fd:             fd,
		NLPacket:       &b,
		xtcpRecordPool: &x.xtcpRecordPool,
		nlhPool:        &x.nlhPool,
		rtaPool:        &x.rtaPool,
		pC:             x.pC,
		pH:             x.pH,
		id:             id,
	})
	if errD != nil {
		x.pC.WithLabelValues("NetlinkerIoUring", "ParseNLPacket", "error").Inc()
	}
	// Match the syscall netlinker (netlinker.go) — emit the parsed-socket
	// count so dashboards + the self-test see iouring activity, not just
	// the per-recv `packets` counter.
	x.pC.WithLabelValues("NetlinkerIoUring", "p", "count").Add(float64(p))
	*res.Buf = (*res.Buf)[:cap(*res.Buf)]
	x.packetBufferPool.Put(res.Buf)
}

// handleSendCQE records the outcome of an io_uring destination write.
// The ring's drainer already returned the buffer to the caller (via
// res.Buf) — destination functions arrange for the pool Put.
func (x *XTCP) handleSendCQE(res xio.Result) {
	if res.Res < 0 {
		x.pC.WithLabelValues(opLabel(res.Op), "Write", "error").Inc()
		if x.debugLevel > 100 {
			log.Printf("io_uring send err op=%d res=%d", res.Op, res.Res)
		}
	} else {
		x.pC.WithLabelValues(opLabel(res.Op), "Writes", "count").Inc()
		x.pC.WithLabelValues(opLabel(res.Op), "WriteBytes", "count").Add(float64(res.Res))
	}
	if res.Buf != nil {
		x.destBytesPool.Put(res.Buf)
	}
}

// onRingClosedResult is called for each CQE drained during ring.Close —
// returns leftover buffers to their pools.
func (x *XTCP) onRingClosedResult(res xio.Result) {
	if res.Buf == nil {
		return
	}
	switch res.Op {
	case xio.OpRead:
		*res.Buf = (*res.Buf)[:cap(*res.Buf)]
		x.packetBufferPool.Put(res.Buf)
	default:
		x.destBytesPool.Put(res.Buf)
	}
}

func opLabel(op xio.Operation) string {
	switch op {
	case xio.OpSendUDP:
		return "destUDPIoUring"
	case xio.OpSendUnix:
		return "destUnixIoUring"
	case xio.OpSendUnixGram:
		return "destUnixGramIoUring"
	default:
		return "destIoUring"
	}
}

func isTimeoutErrno(e syscall.Errno) bool {
	return e == syscall.EAGAIN || e == syscall.EWOULDBLOCK || e == syscall.ETIME
}
