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
// recv-CQE handler, which copies the packet out so its buffer can go back
// to the kernel, or the send-CQE handler. It reports whether a recv CQE
// came without More: that was the multishot recv's last, so the caller
// arms it again.
func (x *XTCP) iouringProcessResults(ring *xio.Ring, fd int, packetCh chan<- dumpPacket, tracker *dumpTracker, results []xio.Result) (recvEnded bool) {
	for _, res := range results {
		switch res.Op {
		case xio.OpRecvMultishot:
			x.handleRecvCQE(fd, res, packetCh, tracker)
			if res.Data != nil {
				ring.RecycleRecvBuf(res.BufID)
			}
			if !res.More {
				recvEnded = true
			}
		case xio.OpCancel:
			// Only Close cancels, and it doesn't hand CQEs here.
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
	return recvEnded
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

// netlinkerIoUring is the io_uring receiver for one socket. It keeps one
// multishot recv armed on fd, receiving into the ring's provided buffers,
// copies each packet into a packetBufferPool buffer, gives the ring
// buffer straight back to the kernel, numbers the packet per dump
// (dumpTracker) and hands it to the decoders through packetCh. It closes
// packetCh on return, which stops the decoders once they've drained it.
//
// Numbering in CQE order needs the CQEs in the order the socket queued
// the packets. One multishot recv posts them in that order: it is a
// single request, dequeuing one packet per CQE. Several single-shot
// recvmsg SQEs don't: armed on an idle socket, a burst wakes them all and
// their CQEs can swap (TestIoUringReceiveOrder), which counted packets
// into the wrong dump and dropped their records after the flush.
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
		RecvBufSize:   x.packetBufferSize(),
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

	if aerr := x.iouringArmRecv(ring, fd); aerr != nil {
		// A single namespace's failure shouldn't kill the whole daemon
		// (gRPC services, poller, every other namespace's netlinkers).
		// Log + return so the deferred ring.Close + close(packetCh) +
		// UnlockOSThread fire normally.
		log.Printf("netlinkerIoUring fd:%d arm recv: %v", fd, aerr)
		return
	}
	if _, serr := ring.Submit(); serr != nil {
		log.Printf("netlinkerIoUring fd:%d initial Submit: %v", fd, serr)
	}

	// Use a Timespec equal to the netlink timeout so cancel polling and
	// "kernel has no more data" detection share one knob.
	nlTimeout := iouringResolveTimeout(x.config.NlTimeoutMilliseconds)

	tracker := newDumpTracker()
	armed := true
	packets := uint64(0)
	for !x.checkDoneNonBlocking(ctx) {
		if !armed {
			// A failed arm is retried after the next wait times out.
			x.pC.WithLabelValues("NetlinkerIoUring", "Rearm", "count").Inc()
			armed = x.iouringArmRecv(ring, fd) == nil
			if _, serr := ring.Submit(); serr != nil {
				x.pC.WithLabelValues("NetlinkerIoUring", "Submit", "error").Inc()
			}
		}
		results, werr := x.iouringWaitWithTimeout(ring, nlTimeout)
		if werr != nil {
			x.iouringRecordWaitErr(fd, werr)
			continue
		}
		if x.iouringProcessResults(ring, fd, packetCh, tracker, results) {
			armed = false
		}
		packets++
		x.maybeForceGCIoUring(packets)
	}
}

// iouringArmRecv enqueues the multishot recv on fd; the caller submits
// it. A failure is counted and returned.
func (x *XTCP) iouringArmRecv(ring *xio.Ring, fd int) error {
	if _, err := ring.EnqueueRecvMultishot(fd); err != nil {
		x.pC.WithLabelValues("NetlinkerIoUring", "Arm", "error").Inc()
		if x.debugLevel > 10 {
			log.Printf("netlinkerIoUring fd:%d arm recv: %v", fd, err)
		}
		return err
	}
	return nil
}

// packetBufferSize is the size of packetBufferPool's buffers, and so of
// the io_uring ring's receive buffers.
func (x *XTCP) packetBufferSize() int {
	buf := x.packetBufferPool.Get()
	n := cap(*buf)
	x.packetBufferPool.Put(buf)
	return n
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

// handleRecvCQE copies a received packet into a packetBufferPool buffer
// and hands it to the decoders, numbered by tracker, or counts a recv
// error. A decoder returns the buffer of a packet it's handed; the caller
// recycles res.Data's ring buffer.
func (x *XTCP) handleRecvCQE(fd int, res xio.Result, packetCh chan<- dumpPacket, tracker *dumpTracker) {
	x.pC.WithLabelValues("NetlinkerIoUring", "recv", "count").Inc()
	if res.Res < 0 {
		// CQE result is -errno on error.
		errno := syscall.Errno(-res.Res)
		switch {
		case errno == syscall.ENOBUFS:
			// Every ring buffer was in use; the packets wait in the
			// socket for the re-armed recv.
			x.pC.WithLabelValues("NetlinkerIoUring", "NoBufs", "count").Inc()
		case isTimeoutErrno(errno):
			x.pC.WithLabelValues("NetlinkerIoUring", "Timeout", "count").Inc()
		default:
			x.pC.WithLabelValues("NetlinkerIoUring", "RecvErr", "count").Inc()
			if x.debugLevel > 10 {
				log.Printf("netlinkerIoUring fd:%d recv err: %v", fd, errno)
			}
		}
		return
	}

	// A success CQE that took no ring buffer: a zero-length recv, or a
	// CQE drainOnce couldn't match. There's no packet to hand over.
	if res.Data == nil {
		x.pC.WithLabelValues("NetlinkerIoUring", "OrphanCQE", "error").Inc()
		return
	}

	n := len(res.Data)
	x.pC.WithLabelValues("NetlinkerIoUring", "packets", "count").Inc()
	x.pC.WithLabelValues("NetlinkerIoUring", "n", "count").Add(float64(n))

	buf := x.packetBufferPool.Get()
	*buf = (*buf)[:cap(*buf)]
	n = copy(*buf, res.Data)
	packetCh <- dumpPacket{buf: buf, n: n, dump: tracker.assign((*buf)[:n])}
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
// returns leftover send buffers to destBytesPool. Recv CQEs carry no pool
// buffer (their Data is in the ring's own buffers), and a packet still
// arriving at teardown is dropped with its dump. It reports whether it
// put a buffer back, for the tests.
func (x *XTCP) onRingClosedResult(res xio.Result) (put bool) {
	if res.Buf == nil {
		return false
	}
	x.destBytesPool.Put(res.Buf)
	return true
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
