package xtcp

import (
	"context"
	"encoding/binary"
	"log"
	"sync"
	"syscall"
	"time"

	"github.com/randomizedcoder/xtcp2/pkg/misc"
	"golang.org/x/sys/unix"
)

func (x *XTCP) Poller(ctx context.Context, wg *sync.WaitGroup) {

	defer wg.Done()
	// Detached context for the final flush: by the time this defer runs,
	// the parent ctx may already be Done (shutdown is what caused us to
	// return), and a canceled ctx would short-circuit Send and lose the
	// in-flight envelope. The destination's own Close() drains pending
	// produces with a 5s flush window.
	defer x.flushEnvelope(context.WithoutCancel(ctx), "shutdown")

	if x.debugLevel > 10 {
		log.Printf("Poller started")
	}

	<-x.DestinationReady
	if x.debugLevel > 10 {
		log.Printf("Poller DestinationReady")
	}

	ticker := time.NewTicker(x.config.PollFrequency.AsDuration())
	defer ticker.Stop()
	x.pollTimeoutTimer = time.NewTimer(x.config.PollTimeout.AsDuration())
	defer x.pollTimeoutTimer.Stop()

	count := x.pollAllNetlinkSockets(0)
	lastPollTime := time.Now()

	for pollingLoops := uint64(1); misc.MaxLoopsOrForEver(pollingLoops, x.config.MaxLoops); pollingLoops++ {

		x.pC.WithLabelValues("Poller", "pollingLoops", "count").Inc()
		if x.debugLevel > 10 {
			log.Printf("Poller pollingLoops:%d count:%d", pollingLoops, count)
		}

		select {
		case <-ctx.Done():
			x.pC.WithLabelValues("Poller", "complete", "count").Inc()
			return
		case <-ticker.C:
			x.handlePollerTick(pollingLoops, count)
		case <-x.pollRequestCh:
			next, polled := x.handlePollRequest(pollingLoops, count, lastPollTime)
			if !polled {
				continue
			}
			count = next
			lastPollTime = time.Now()
		case d := <-x.changePollFrequencyCh:
			x.handleChangePollFrequency(d, ticker, pollingLoops, count)
		case doneReceived := <-x.netlinkerDoneCh:
			count = x.handleNetlinkerDone(doneReceived, count)
			if count == 0 {
				x.flushEnvelope(ctx, "poll_end")
			}
		case <-x.pollTimeoutTimer.C:
			count = x.handlePollTimeout()
			x.flushEnvelope(ctx, "poll_timeout")
		}

		x.recordPollerCycleDuration(pollingLoops)
	}

	x.pC.WithLabelValues("Poller", "complete", "count").Inc()
}

// flushEnvelope drains x.currentEnvelope: marshals the accumulated rows,
// hands the bytes to the destination, then returns each *XtcpFlatRecord
// and the Envelope itself to their sync.Pools. After this returns,
// x.currentEnvelope is nil; the next pollAllNetlinkSockets re-acquires
// a fresh envelope from the pool. Concurrent appends in deserialize.go
// (processInetDiagRecord) take envelopeMu and tolerate nil by dropping
// the record (no-op + counter bump) — that path only fires for in-flight
// netlinkers during shutdown after the final flush.
func (x *XTCP) flushEnvelope(ctx context.Context, reason string) {
	x.envelopeMu.Lock()
	e := x.currentEnvelope
	x.currentEnvelope = nil
	x.currentEnvelopeBytes = 0
	x.envelopeMu.Unlock()

	if e == nil {
		return
	}
	if len(e.Row) == 0 {
		x.xtcpEnvelopePool.Put(e)
		return
	}

	rows := len(e.Row)
	buf := x.EnvelopeMarshaller(e)
	_, err := x.dest.Send(ctx, buf)

	// type label tracks the trigger: poll_end (all netlinkers done),
	// poll_timeout (timer fired first), size_cap (mid-poll early flush),
	// shutdown (final defer). Dashboards filter by this to spot whether
	// production is hitting size_cap excessively (envelope sizes too
	// large) or poll_timeout excessively (slow netns / dropped DONE).
	x.pC.WithLabelValues("Poller", "envelopeFlush", reason).Inc()
	x.pC.WithLabelValues("Poller", "envelopeRows", "count").Add(float64(rows))
	if err != nil {
		x.pC.WithLabelValues("Poller", "envelopeFlush", "error").Inc()
		if x.debugLevel > 10 {
			log.Printf("flushEnvelope reason=%s dest.Send err:%v rows:%d", reason, err, rows)
		}
	}

	for _, r := range e.Row {
		r.Reset()
		x.xtcpRecordPool.Put(r)
	}
	x.EnvelopeZero(e)
	x.xtcpEnvelopePool.Put(e)
}

// handlePollerTick reacts to the periodic Ticker.C signal: bumps the
// ticker counter, optionally logs, and non-blocking-sends one pollReq
// to the pollRequestCh (default-arm drops the send if a poll is already
// queued — back-pressure prevents tick storms during long dumps).
func (x *XTCP) handlePollerTick(pollingLoops uint64, count int) {
	x.pC.WithLabelValues("Poller", "ticker", "count").Inc()
	if x.debugLevel > 10 {
		log.Printf("Poller <-ticker.C pollingLoops:%d count:%d", pollingLoops, count)
	}
	select {
	case x.pollRequestCh <- struct{}{}:
	default:
		// non-blocking
	}
}

// handleChangePollFrequency reacts to the gRPC-driven poll-frequency
// change: resets the ticker to the new duration and counts the reset.
func (x *XTCP) handleChangePollFrequency(d time.Duration, ticker *time.Ticker, pollingLoops uint64, count int) {
	ticker.Reset(d)
	x.pC.WithLabelValues("Poller", "ticker.Reset", "count").Inc()
	if x.debugLevel > 10 {
		log.Printf("Poller pollingLoops:%d count:%d ticker.Reset:%s", pollingLoops, count, d.Round(time.Millisecond).String())
	}
}

// handleNetlinkerDone reacts to a per-fd "I'm done draining" signal
// from a Netlinker. Bumps the histogram via observeNetlinkerDone and
// returns count-1; caller assigns into its own count variable.
func (x *XTCP) handleNetlinkerDone(d netlinkerDone, count int) int {
	x.observeNetlinkerDone(d, count)
	count--
	if x.debugLevel > 1000 {
		log.Printf("Poller <-x.netlinkerDoneCh, count:%d", count)
	}
	return count
}

// handlePollTimeout reacts to the per-cycle PollTimeoutTimer firing:
// zeroes count so the next tick triggers a fresh dump, regardless of
// whether netlinkers reported done.
func (x *XTCP) handlePollTimeout() int {
	x.pC.WithLabelValues("Poller", "PollTimeout", "count").Inc()
	if x.debugLevel > 10 {
		log.Println("Poller <-time.After(*x.config.PollTimeout)")
	}
	return 0
}

// recordPollerCycleDuration observes the per-iteration duration since
// pollStartTime. Extracted from the loop tail for symmetry with the
// case-handlers above.
func (x *XTCP) recordPollerCycleDuration(pollingLoops uint64) {
	pollDuration := time.Since(x.pollStartTime)
	x.pH.WithLabelValues("Poller", "pollToDoneDuration", "count").Observe(pollDuration.Seconds())
	if x.debugLevel > 10 {
		log.Printf("Poller pollingLoops:%d pollDuration:%0.4fs %dms",
			pollingLoops, pollDuration.Seconds(), pollDuration.Milliseconds())
	}
}

// handlePollRequest reacts to a poll-request tick. Returns (newCount, true)
// when a fresh dump was issued, or (count, false) when the previous dump
// is still in flight and the request was coalesced.
func (x *XTCP) handlePollRequest(pollingLoops uint64, count int, lastPollTime time.Time) (int, bool) {
	x.pC.WithLabelValues("Poller", "pollRequestCh", "count").Inc()
	if x.debugLevel > 10 {
		log.Printf("Poller <-x.pollRequestCh pollingLoops:%d count:%d", pollingLoops, count)
	}

	if count > 0 {
		x.pC.WithLabelValues("Poller", "alreadyPolling", "count").Inc()
		if x.debugLevel > 10 {
			log.Printf("Poller pollingLoops:%d count:%d alreadyPolling", pollingLoops, count)
		}
		return count, false
	}
	if x.debugLevel > 10 {
		log.Printf("Poller <-ticker.C pollingLoops:%d timeSinceLastPoll:%0.3fs",
			pollingLoops, time.Since(lastPollTime).Seconds())
	}
	return x.pollAllNetlinkSockets(pollingLoops), true
}

// observeNetlinkerDone records the per-fd poll→done latency and (at
// debug levels) emits a log line tagged with the netns that owns the fd.
func (x *XTCP) observeNetlinkerDone(d netlinkerDone, count int) {
	x.pC.WithLabelValues("Poller", "done", "count").Inc()

	p, ok := x.pollTime.Load(d.fd)
	if !ok {
		return
	}
	pt, ok := p.(time.Time)
	if !ok {
		return
	}
	pTime := d.t.Sub(pt)
	x.pH.WithLabelValues("Poller", "pollToDoneDuration", "count").Observe(pTime.Seconds())

	if x.debugLevel <= 10 {
		return
	}
	if ns, okNs := x.fdToNsMap.Load(d.fd); okNs {
		if nsStr, okStr := ns.(string); okStr {
			log.Printf("Poller <-x.netlinkerDoneCh, count:%d fd:%d ns:%s after: %0.3fs %dms",
				count, d.fd, nsStr, pTime.Seconds(), pTime.Milliseconds())
			return
		}
	}
	x.pC.WithLabelValues("Poller", "fdToNsMap", "error").Inc()
	log.Printf("Poller <-x.netlinkerDoneCh, count:%d fd:%d after: %0.3fs %dms",
		count, d.fd, pTime.Seconds(), pTime.Milliseconds())
}

func (x *XTCP) pollAllNetlinkSockets(pollingLoops uint64) (count int) {

	startTime := time.Now()

	x.envelopeMu.Lock()
	x.currentEnvelope = x.xtcpEnvelopePool.Get()
	x.currentEnvelopeBytes = 0
	x.pollStartTime = startTime
	x.envelopeMu.Unlock()

	x.updateNetlinkRequestSequenceNumber(pollingLoops)

	socketFDs := x.GetNetlinkSocketFDs()
	polled := 0
	for i, socketFD := range socketFDs {
		if ns, ok := x.fdToNsMap.Load(socketFD); ok {
			nsStr, okStr := ns.(string)
			// "/run/netns/xtcpNS"
			if okStr && nsStr == linuxNetNSDirCst+xtcpNSName {
				if x.debugLevel > 100 {
					log.Printf("pollAllNetlinkSockets skip "+linuxNetNSDirCst+xtcpNSName+" Poll i:%d", i)
				}
				continue
			}
			x.poll(socketFD)
			polled++
			if x.debugLevel > 10 {
				log.Printf("pollAllNetlinkSockets Poll i:%d fd:%d", i, socketFD)
			}
		}
	}

	// restart the timeout timer
	x.pollTimeoutTimer.Reset(x.config.PollTimeout.AsDuration())

	// Return the count of fds we actually issued a poll against, NOT
	// len(socketFDs). The xtcpNS fd is in socketFDs but is skipped above
	// — counting it would tell Poller to expect one more done signal
	// than will ever arrive, so count never drops to 0 and every cycle
	// waits for the PollTimeoutTimer to fire instead of advancing on
	// the natural netlinker-done signals.
	return polled
}

func (x *XTCP) updateNetlinkRequestSequenceNumber(pollingLoops uint64) {
	binary.LittleEndian.PutUint32((*x.nlRequest)[8:12], x.config.NlmsgSeq+uint32(pollingLoops))
}

func (x *XTCP) poll(fd int) {

	startTime := time.Now()

	x.pollTime.Store(fd, startTime)

	x.sendNetlinkDumpRequest(fd, x.nlRequest)
	// x.SendNetlinkDumpRequestPtr(x.socketFD, x.socketAddress, x.nlRequest)
	// x.SendNetlinkDumpRequestPtrIOUring(x.socketFD, x.nlRequest)

	x.pC.WithLabelValues("poller", "poll", "count").Inc()
	if x.debugLevel > 10 {
		log.Printf("poll fd:%d pollTime:%s", fd, startTime.Format(time.RFC3339))
	}
}

func (x *XTCP) sendNetlinkDumpRequest(fd int, packetBytes *[]byte) {
	// Send the netlink dump request
	// https://godoc.org/golang.org/x/sys/unix#Sendto
	err := unix.Sendto(
		fd,
		*packetBytes,
		0,
		&unix.SockaddrNetlink{Family: syscall.AF_NETLINK},
	)
	if err != nil {
		x.pC.WithLabelValues("SendNetlinkDumpRequest", "Sendto", "error").Inc()
		if x.debugLevel > 10 {
			log.Printf("unix.Sendto:%s", err)
		}
	}
}

// func (x *XTCP) SendNetlinkDumpRequestPtrIOUring(
// 	socketFileDescriptor int,
// 	packetBytes *[]byte,
// ) {

// 	// https://pkg.go.dev/github.com/iceber/iouring-go@v0.0.0-20230403020409-002cfd2e2a90#Send
// 	prep := iouring.Send(socketFileDescriptor, *packetBytes, 0)
// 	if _, err := x.iour.SubmitRequest(prep, x.resulter); err != nil {
// 		log.Panicf("submit write request error: %v", err)
// 	}
// }
