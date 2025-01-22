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
	x.pollTimeoutTimer = time.NewTimer(x.config.PollTimeout.AsDuration())

	count := x.pollAllNetlinkSockets(0)

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
			x.pC.WithLabelValues("Poller", "pollRequestCh", "count").Inc()
			if x.debugLevel > 10 {
				log.Printf("Poller <-x.pollRequestCh pollingLoops:%d count:%d", pollingLoops, count)
			}

			if count > 0 {
				x.pC.WithLabelValues("Poller", "alreadyPolling", "count").Inc()
				if x.debugLevel > 10 {
					log.Printf("Poller pollingLoops:%d count:%d alreadyPolling", pollingLoops, count)
				}
				continue
			}
			timeSinceLastPoll := time.Since(lastPollTime)
			lastPollTime = time.Now()
			if x.debugLevel > 10 {
				log.Printf("Poller <-ticker.C pollingLoops:%d timeSinceLastPoll:%0.3fs", pollingLoops, timeSinceLastPoll.Seconds())
			}
			count = x.pollAllNetlinkSockets(pollingLoops)

		case d := <-x.changePollFrequencyCh:
			ticker.Reset(d)
			x.pC.WithLabelValues("Poller", "ticker.Reset", "count").Inc()
			if x.debugLevel > 10 {
				log.Printf("Poller pollingLoops:%d count:%d ticker.Reset:%s", pollingLoops, count, d.Round(time.Millisecond).String())
			}

		case doneReceived := <-x.netlinkerDoneCh:
			x.pC.WithLabelValues("Poller", "done", "count").Inc()

			if p, ok := x.pollTime.Load(doneReceived.fd); ok {
				pTime := doneReceived.t.Sub(p.(time.Time))
				x.pH.WithLabelValues("Poller", "pollToDoneDuration", "count").Observe(pTime.Seconds())

				if x.debugLevel > 10 {
					if ns, ok := x.fdToNsMap.Load(doneReceived.fd); ok {
						log.Printf("Poller <-x.netlinkerDoneCh, count:%d fd:%d ns:%s after: %0.3fs %dms",
							count, doneReceived.fd, ns.(string), pTime.Seconds(), pTime.Milliseconds())
					} else {
						x.pC.WithLabelValues("Poller", "fdToNsMap", "error").Inc()
						log.Printf("Poller <-x.netlinkerDoneCh, count:%d fd:%d after: %0.3fs %dms",
							count, doneReceived.fd, pTime.Seconds(), pTime.Milliseconds())
					}
				}
			}

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

			//default:
			//blocking!
		}

		// Send batch
		if count == 0 {

			x.pollTimeoutTimer.Stop()

			// TODO there is an oppertunity here so NOT marshal, in the case of null dest,
			// or alternatively if the dest is a GRPC endpoint

			var sTime time.Time

			x.envelopeMu.Lock()

			b := x.Marshaller(x.currentEnvelope)
			l := len(x.currentEnvelope.Row)
			x.currentEnvelope.Reset()
			sTime = x.pollStartTime

			x.envelopeMu.Unlock()

			n, err := x.Destination(ctx, b)
			if err != nil {
				x.pC.WithLabelValues("Deserialize", "Destination", "error").Inc()
				continue
			}
			x.pC.WithLabelValues("Deserialize", "Destination", "count").Inc()
			x.pC.WithLabelValues("Deserialize", "Destination", "countN").Add(float64(l))
			x.pC.WithLabelValues("Deserialize", "Destination", "bytes").Add(float64(n))

			pollDuration := time.Since(sTime)
			x.pH.WithLabelValues("Poller", "pollToDoneDuration", "count").Observe(pollDuration.Seconds())

			if x.debugLevel > 10 {
				log.Printf("Poller pollingLoops:%d pullDuration:%0.4fs %dms bytes:%d",
					pollingLoops, pollDuration.Seconds(), pollDuration.Milliseconds(), n)
			}

		}

	}

	x.pC.WithLabelValues("Poller", "complete", "count").Inc()
}

func (x *XTCP) pollAllNetlinkSockets(pollingLoops uint64) (count int) {

	startTime := time.Now()

	x.envelopeMu.Lock()
	x.currentEnvelope = x.xtcpEnvelopePool.Get().(*xtcp_flat_record.Envelope)
	x.pollStartTime = startTime
	x.envelopeMu.Unlock()

	x.updateNetlinkRequestSequenceNumber(pollingLoops)

	socketFDs := x.GetNetlinkSocketFDs()
	for i, socketFD := range socketFDs {
		if ns, ok := x.fdToNsMap.Load(socketFD); ok {
			// "/run/netns/xtcpNS"
			if ns.(string) == linuxNetNSDirCst+xtcpNSName {
				if x.debugLevel > 100 {
					log.Printf("pollAllNetlinkSockets skip "+linuxNetNSDirCst+xtcpNSName+" Poll i:%d", i)
				}
				continue
			}
			x.poll(socketFD)
			if x.debugLevel > 10 {
				log.Printf("pollAllNetlinkSockets Poll i:%d fd:%d", i, socketFD)
			}
		}
	}

	// restart the timeout timer
	x.pollTimeoutTimer.Reset(x.config.PollTimeout.AsDuration())

	return len(socketFDs)
}

func (x *XTCP) updateNetlinkRequestSequenceNumber(pollingLoops uint64) {
	binary.LittleEndian.PutUint32((*x.nlRequest)[8:12], x.config.NlmsgSeq+uint32(pollingLoops))
}

func (x *XTCP) poll(fd int) {

	startTime := time.Now()

	x.pollTime.Store(fd, startTime)

	x.sendNetlinkDumpRequest(fd, x.nlRequest)
	//x.SendNetlinkDumpRequestPtr(x.socketFD, x.socketAddress, x.nlRequest)
	//x.SendNetlinkDumpRequestPtrIOUring(x.socketFD, x.nlRequest)

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
