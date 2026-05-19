package xtcp

import (
	"context"
	"log"
	"sync"
)

// createNetlinkersAndStore takes the per-ns context/cancel from the caller
// (netNamespaceInstance) so that nsDelete's cancel() reaches BOTH the
// netlinkers AND netNamespaceInstance's blocking <-nsCtx.Done(). Previously
// the cancel was created locally here and only reached the netlinkers,
// leaving netNamespaceInstance blocked on the parent (daemon-lifetime)
// context — its deferred closeSocket(fd) never fired on a delete, leaking
// one netlink fd + one goroutine per namespace removed.
func (x *XTCP) createNetlinkersAndStore(nsCtx context.Context, nsCancel context.CancelFunc, nsName *string, fd int) {

	x.pC.WithLabelValues("createWorksAndStore", "start", "counter").Inc()

	if x.config.NlTimeoutMilliseconds > 0 {
		x.setSocketTimeoutViaSyscall(int64(x.config.NlTimeoutMilliseconds), fd)
	}

	wg := new(sync.WaitGroup)
	nsi := netNSitem{
		name:     nsName,
		ctx:      nsCtx,
		cancel:   nsCancel,
		wg:       wg,
		socketFD: fd,
	}

	wg.Add(1)
	go x.createNetlinkers(nsCtx, wg, nsName, fd, x.config.Netlinkers)

	x.nsMap.Store(*nsName, nsi)
	x.fdToNsMap.Store(fd, *nsName)
	x.incrementStoreAndGenerationCounts()
	if x.debugLevel > 10 {
		log.Printf("createNetlinkersAndStore: ns:%s socketFD:%d Stored", *nsName, fd)
	}
}

func (x *XTCP) incrementStoreAndGenerationCounts() {
	x.storeCount.Add(1)
	x.generation.Add(1)
}

func (x *XTCP) createNetlinkers(ctx context.Context, wg *sync.WaitGroup, nsName *string, fd int, netlinkers uint32) {

	x.pC.WithLabelValues("createNetlinkers", "start", "counter").Inc()

	defer wg.Done()

	for i := uint32(0); i < netlinkers; i++ {
		wg.Add(1)
		go x.Netlinker(ctx, wg, nsName, fd, i)
		if x.debugLevel > 10 {
			log.Printf("createNetlinkers Netlinker i:%d, fd:%d", i, fd)
		}
	}
}
