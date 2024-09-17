package xtcp

import (
	"context"
	"encoding/binary"
	"log"
	"sync"
	"time"

	"github.com/randomizedcoder/xtcp2/pkg/misc"
	"golang.org/x/sys/unix"
)

func (x *XTCP) Poller(ctx context.Context, wg *sync.WaitGroup) {

	defer wg.Done()

	var (
		startPollTime time.Time
		polling       bool
	)
	ticker := time.NewTicker(*x.config.PollingFrequency)

	startPollTime, polling = x.Poll(0)

breakPoint:
	for pollingLoops := 1; misc.MaxLoopsOrForEver(pollingLoops, *x.config.MaxLoops); pollingLoops++ {

		x.pC.WithLabelValues("Poller", "pollingLoops", "count").Inc()

		select {

		case <-ctx.Done():
			break breakPoint

		case <-ticker.C:
			x.pC.WithLabelValues("Poller", "ticker", "count").Inc()
			if x.debugLevel > 10 {
				log.Printf("Poller <-ticker.C")
			}
			if !polling {
				startPollTime, polling = x.Poll(pollingLoops)
			}

		case doneReceivedTime := <-x.netlinkerDoneCh:
			x.pC.WithLabelValues("Poller", "done", "count").Inc()
			x.pH.WithLabelValues("Poller", "pollToDoneDuration", "count").Observe(doneReceivedTime.Sub(startPollTime).Seconds())
			polling = false
			if x.debugLevel > 10 {
				log.Printf("Poller <-x.netlinkerDoneC, after: %0.6fs %dms", doneReceivedTime.Sub(startPollTime).Seconds(), doneReceivedTime.Sub(startPollTime).Microseconds())
			}
		}

	}

	x.pC.WithLabelValues("Poller", "complete", "count").Inc()
}

func (x *XTCP) Poll(pollingLoops int) (startPollTime time.Time, polling bool) {

	binary.LittleEndian.PutUint32((*x.nlRequest)[8:12], uint32(*x.config.NlmsgSeq+pollingLoops))

	startPollTime = time.Now()
	x.pollTime.Store(startPollTimeKeyCst, startPollTime)

	x.SendNetlinkDumpRequestPtr(x.socketFD, x.socketAddress, x.nlRequest)
	//x.SendNetlinkDumpRequestPtrIOUring(x.socketFD, x.nlRequest)
	polling = true
	x.pC.WithLabelValues("Poller", "poll", "count").Inc()
	if x.debugLevel > 10 {
		log.Printf("Poller SendNetlinkDumpRequestPtr: %s", startPollTime.Format(time.RFC3339))
	}

	return startPollTime, polling
}

func (x *XTCP) SendNetlinkDumpRequestPtr(
	socketFileDescriptor int,
	socketAddress *unix.SockaddrNetlink,
	packetBytes *[]byte,
) {
	// Send the netlink dump request
	// https://godoc.org/golang.org/x/sys/unix#Sendto
	err := unix.Sendto(socketFileDescriptor, *packetBytes, 0, socketAddress)
	if err != nil {
		log.Fatalf("unix.Sendto:%s", err)
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
