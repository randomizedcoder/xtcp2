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

// ringCtxKey is the context.WithValue key for a ring that io_uring
// destination sends may enqueue on. The ring must belong to the goroutine
// doing the send (rings are SetupSingleIssuer). No production goroutine
// that sends owns a ring today: flushes run on the decoders and the
// poller, and the netlinker's ring stays with its receiver goroutine.
type ringCtxKey struct{}

// ringFromContext returns the io_uring Ring stashed in ctx, or nil.
// Destination functions enqueue an SQE when there is one and write with
// a syscall when there isn't.
func ringFromContext(ctx context.Context) *xio.Ring {
	v := ctx.Value(ringCtxKey{})
	if v == nil {
		return nil
	}
	ring, ok := v.(*xio.Ring)
	if !ok {
		return nil
	}
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
func (x *XTCP) iouringRecordWaitErr(fd int, werr error) {
	if isETimeError(werr) {
		x.pC.WithLabelValues("NetlinkerIoUring", "Timeout", "count").Inc()
		return
	}
	x.pC.WithLabelValues("NetlinkerIoUring", "WaitErr", "count").Inc()
	if x.debugLevel > 10 {
		log.Printf("netlinkerIoUring fd:%d WaitOne err: %v", fd, werr)
	}
}

// iouringProcessResults dispatches each CQE in results to either the
// recv-CQE handler (which also refills the slot) or the send-CQE
// handler.
func (x *XTCP) iouringProcessResults(ring *xio.Ring, fd int, packetCh chan<- dumpPacket, tracker *dumpTracker, results []xio.Result) {
	for _, res := range results {
		switch res.Op {
		case xio.OpRead:
			x.handleRecvCQE(fd, res, packetCh, tracker)
			if rerr := x.iouringPrefillRecvs(ring, fd, 1); rerr != nil {
				x.pC.WithLabelValues("NetlinkerIoUring", "Refill", "error").Inc()
				if x.debugLevel > 10 {
					log.Printf("netlinkerIoUring fd:%d refill err: %v", fd, rerr)
				}
			}
		default:
			// Send CQEs (OpSendUDP/OpSendUnix/OpSendUnixGram) only come
			// back on a ring that a destination enqueued on (see
			// ringFromContext); the netlinker's ring only receives, but
			// the drain still handles them. The ring's drain layer
			// already returned res.Buf to the caller; we just record the
			// outcome.
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

// netlinkersIoUring is the io_uring path for one socket: one ring
// goroutine receives (netlinkerIoUring) and `netlinkers` decoder goroutines
// deserialize in parallel, with the same dump accounting as the syscall
// path (dumpState in netlinker.go), so the done signal goes out only after
// the dump's last packet has been decoded. Earlier, each of `netlinkers`
// rings read and decoded its own packets, and the ring that decoded
// NLMSG_DONE signaled done while the others were still decoding
// (docs/investigations/envelope-post-flush-drop.md).
//
// The ring goroutine only receives. A ring can only be driven by the
// goroutine that created it (SetupSingleIssuer), and destination sends
// happen on the decoders (cap flushes) and the poller (poll-end flushes),
// so those send without a ring (see ringFromContext).
func (x *XTCP) netlinkersIoUring(ctx context.Context, wg *sync.WaitGroup, nsName *string, inode uint64, fd int, netlinkers uint32) {

	defer wg.Done()

	packetCh, decodersWG := x.startDecoders(ctx, nsName, inode, fd, netlinkers, "NetlinkerIoUring")

	x.netlinkerIoUring(ctx, nsName, fd, packetCh)

	decodersWG.Wait()

	x.pC.WithLabelValues("NetlinkerIoUring", "complete", "count").Inc()
}

// netlinkerIoUring is the io_uring receiver for one socket. It keeps a
// configurable batch of recvmsg SQEs in flight on fd, drains CQEs as they
// arrive, numbers each packet per dump (dumpTracker), hands it to the
// decoders through packetCh, and refills the slot. It closes packetCh on
// return, which stops the decoders once they've drained it.
//
// Numbering in CQE order relies on CQEs arriving in the order the kernel
// queued the packets. They do on one ring with single-shot recvmsg: each
// recvmsg dequeues its packet and posts its CQE in one step, on this
// ring's task (DEFER_TASKRUN, no SQPOLL, no IOSQE_ASYNC), so the dequeues
// and the CQEs happen in the same order. A multishot recv would make that
// a documented guarantee; the dump tests and envelopePostFlushDrop would
// show it breaking.
//
// Periodic xtcp polling means the loop is mostly idle between dump
// cycles. WaitCQETimeout caps each wait at config.NlTimeoutMilliseconds
// so ctx cancellation is observed within that bound.
func (x *XTCP) netlinkerIoUring(ctx context.Context, nsName *string, fd int, packetCh chan<- dumpPacket) {

	// Registered first so it runs last, after ring.Close has returned the
	// in-flight buffers.
	defer close(packetCh)

	if x.debugLevel > 10 {
		log.Printf("NetlinkerIoUring started ns:%s fd:%d", *nsName, fd)
	}

	// Pin to the netns'd OS thread for the ring's lifetime. The kernel
	// associates io_uring fds with the netns of the creating task; the
	// fd we recv from must be in the same netns.
	runtime.LockOSThread()
	defer runtime.UnlockOSThread() //nolint:forbidigo // safe: this goroutine never modifies thread-global namespace state — io_uring rings just need a stable kernel-task identity for the ring's lifetime, so unlock-on-return is safe.

	batch, cqeBatch := iouringResolveBatchSizes(x.config.IoUringRecvBatchSize, x.config.IoUringCqeBatchSize)

	ring, err := xio.New(xio.Config{
		RecvBatchSize: batch,
		CQEBatchSize:  cqeBatch,
	})
	if err != nil {
		// Log + return rather than log.Fatalf: in production Fatalf exits
		// anyway, and in tests where fatalf is mocked to return the
		// function would carry on with ring==nil. The deferred close,
		// UnlockOSThread and the caller's wg.Done run once either way.
		log.Printf("netlinkerIoUring fd:%d ring init: %v", fd, err)
		return
	}
	defer ring.Close(time.Second, func(res xio.Result) {
		x.onRingClosedResult(res)
	})

	// Pre-fill the SQ with `batch` recvmsg SQEs from the pool. Each one
	// gets pinned in the ring's in-flight map; the kernel will fill them
	// as netlink datagrams arrive.
	if perr := x.iouringPrefillRecvs(ring, fd, batch); perr != nil {
		// Demoted from log.Fatalf: a single namespace's prefill failure
		// shouldn't kill the whole daemon (gRPC services, poller, every
		// other namespace's netlinkers). Log + return so the deferred
		// ring.Close + close(packetCh) + UnlockOSThread fire normally.
		log.Printf("netlinkerIoUring fd:%d prefill: %v", fd, perr)
		return
	}
	if _, serr := ring.Submit(); serr != nil {
		log.Printf("netlinkerIoUring fd:%d initial Submit: %v", fd, serr)
	}

	// Use a Timespec equal to the netlink timeout so cancel polling and
	// "kernel has no more data" detection share one knob.
	nlTimeout := iouringResolveTimeout(x.config.NlTimeoutMilliseconds)

	tracker := newDumpTracker()
	packets := uint64(0)
	for !x.checkDoneNonBlocking(ctx) {
		results, werr := x.iouringWaitWithTimeout(ring, nlTimeout)
		if werr != nil {
			x.iouringRecordWaitErr(fd, werr)
			continue
		}
		x.iouringProcessResults(ring, fd, packetCh, tracker, results)
		if _, serr := ring.Submit(); serr != nil {
			x.pC.WithLabelValues("NetlinkerIoUring", "Submit", "error").Inc()
		}
		packets++
		x.maybeForceGCIoUring(packets)
	}
}

// iouringPrefillRecvs gets n buffers from packetBufferPool and submits
// one recvmsg SQE per buffer. Each buffer is pinned in the ring's
// in-flight map until its CQE fires.
func (x *XTCP) iouringPrefillRecvs(ring *xio.Ring, fd int, n int) error {
	for i := 0; i < n; i++ {
		buf := x.packetBufferPool.Get()
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
// wait-timeout signal) — either as a bare syscall.Errno or anywhere in
// the unwrap chain (e.g. wrapped by fmt.Errorf("...: %w", err) from a
// downstream library). errors.Is walks Unwrap for us, so this also
// covers the giouring helpers' future wrapping.
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

// handleRecvCQE hands a received packet to the decoders, numbered by
// tracker, or on a recv error counts it and returns the buffer to the pool.
// A decoder returns the buffer of a packet it's handed.
func (x *XTCP) handleRecvCQE(fd int, res xio.Result, packetCh chan<- dumpPacket, tracker *dumpTracker) {
	x.pC.WithLabelValues("NetlinkerIoUring", "recv", "count").Inc()
	if res.Res < 0 {
		// CQE result is -errno on error.
		errno := syscall.Errno(-res.Res)
		if isTimeoutErrno(errno) {
			x.pC.WithLabelValues("NetlinkerIoUring", "Timeout", "count").Inc()
		} else {
			x.pC.WithLabelValues("NetlinkerIoUring", "RecvErr", "count").Inc()
			if x.debugLevel > 10 {
				log.Printf("netlinkerIoUring fd:%d recv err: %v", fd, errno)
			}
		}
		if res.Buf != nil {
			x.putPacketBuffer(res.Buf)
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

	packetCh <- dumpPacket{buf: res.Buf, n: n, dump: tracker.assign((*res.Buf)[:n])}
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
