package xtcp

import (
	"context"
	"encoding/binary"
	"log"
	"sync"
	"syscall"
	"time"

	"github.com/randomizedcoder/xtcp2/pkg/misc"
	"github.com/randomizedcoder/xtcp2/pkg/xtcp_flat_record"
	"golang.org/x/sys/unix"
)

func (x *XTCP) Poller(ctx context.Context, wg *sync.WaitGroup) {

	defer wg.Done()

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

	// wf := x.config.DestWriteFiles

	lastPollTime := time.Now()

breakPoint:
	for pollingLoops := uint64(1); misc.MaxLoopsOrForEver(pollingLoops, x.config.MaxLoops); pollingLoops++ {

		x.pC.WithLabelValues("Poller", "pollingLoops", "count").Inc()
		if x.debugLevel > 10 {
			log.Printf("Poller pollingLoops:%d count:%d", pollingLoops, count)
		}

		select {

		case <-ctx.Done():
			break breakPoint

		case <-ticker.C:
			x.pC.WithLabelValues("Poller", "ticker", "count").Inc()

			if x.debugLevel > 10 {
				log.Printf("Poller <-ticker.C pollingLoops:%d count:%d", pollingLoops, count)
			}

			select {
			case x.pollRequestCh <- struct{}{}:
			default:
				// non-blocking
			}

		case <-x.pollRequestCh:
			next, polled := x.handlePollRequest(pollingLoops, count, lastPollTime)
			if !polled {
				continue
			}
			count = next
			lastPollTime = time.Now()

		case d := <-x.changePollFrequencyCh:
			ticker.Reset(d)
			x.pC.WithLabelValues("Poller", "ticker.Reset", "count").Inc()
			if x.debugLevel > 10 {
				log.Printf("Poller pollingLoops:%d count:%d ticker.Reset:%s", pollingLoops, count, d.Round(time.Millisecond).String())
			}

		case doneReceived := <-x.netlinkerDoneCh:
			x.observeNetlinkerDone(doneReceived, count)
			count--
			if x.debugLevel > 1000 {
				log.Printf("Poller <-x.netlinkerDoneCh, count:%d", count)
			}

		case <-x.pollTimeoutTimer.C:
			x.pC.WithLabelValues("Poller", "PollTimeout", "count").Inc()
			count = 0
			if x.debugLevel > 10 {
				log.Println("Poller <-time.After(*x.config.PollTimeout)")
			}

		}

		pollDuration := time.Since(x.pollStartTime)
		x.pH.WithLabelValues("Poller", "pollToDoneDuration", "count").Observe(pollDuration.Seconds())

		if x.debugLevel > 10 {
			log.Printf("Poller pollingLoops:%d pollDuration:%0.4fs %dms",
				pollingLoops, pollDuration.Seconds(), pollDuration.Milliseconds())
		}
	}

	x.pC.WithLabelValues("Poller", "complete", "count").Inc()
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
	pt, _ := p.(time.Time) //nolint:errcheck // pollTime Store sites all use time.Time
	pTime := d.t.Sub(pt)
	x.pH.WithLabelValues("Poller", "pollToDoneDuration", "count").Observe(pTime.Seconds())

	if x.debugLevel <= 10 {
		return
	}
	if ns, okNs := x.fdToNsMap.Load(d.fd); okNs {
		nsStr, _ := ns.(string) //nolint:errcheck // fdToNsMap values are strings
		log.Printf("Poller <-x.netlinkerDoneCh, count:%d fd:%d ns:%s after: %0.3fs %dms",
			count, d.fd, nsStr, pTime.Seconds(), pTime.Milliseconds())
		return
	}
	x.pC.WithLabelValues("Poller", "fdToNsMap", "error").Inc()
	log.Printf("Poller <-x.netlinkerDoneCh, count:%d fd:%d after: %0.3fs %dms",
		count, d.fd, pTime.Seconds(), pTime.Milliseconds())
}

func (x *XTCP) pollAllNetlinkSockets(pollingLoops uint64) (count int) {

	startTime := time.Now()

	x.envelopeMu.Lock()
	x.currentEnvelope, _ = x.xtcpEnvelopePool.Get().(*xtcp_flat_record.Envelope) //nolint:errcheck // pool.New returns *Envelope
	x.pollStartTime = startTime
	x.envelopeMu.Unlock()

	x.updateNetlinkRequestSequenceNumber(pollingLoops)

	socketFDs := x.GetNetlinkSocketFDs()
	polled := 0
	for i, socketFD := range socketFDs {
		if ns, ok := x.fdToNsMap.Load(socketFD); ok {
			nsStr, _ := ns.(string) //nolint:errcheck // fdToNsMap values are strings
			// "/run/netns/xtcpNS"
			if nsStr == linuxNetNSDirCst+xtcpNSName {
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
