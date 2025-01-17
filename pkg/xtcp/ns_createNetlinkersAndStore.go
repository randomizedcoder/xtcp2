package xtcp

import (
	"context"
	"log"
	"sync"
)

func (x *XTCP) createNetlinkersAndStore(ctx context.Context, nsName *string, fd int) {

	x.pC.WithLabelValues("createWorksAndStore", "start", "counter").Inc()

	if x.config.NlTimeoutMilliseconds > 0 {
		x.setSocketTimeoutViaSyscall(int64(x.config.NlTimeoutMilliseconds), fd)
	}

	nsCtx, nsCancel := context.WithCancel(ctx)

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
