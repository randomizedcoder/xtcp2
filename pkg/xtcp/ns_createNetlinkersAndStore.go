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

	// The namespace may have been deleted while netNamespaceInstance was
	// doing its setns/socket init — nsAdd made the cancel reachable, so
	// nsDelete can have already fired. Don't start netlinkers or update the
	// nsMap entry for a namespace that's gone; the caller's <-nsCtx.Done()
	// returns immediately and its deferred closeSocket cleans up.
	if nsCtx.Err() != nil {
		x.pC.WithLabelValues("createWorksAndStore", "cancelledDuringInit", "count").Inc()
		return
	}

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

	// Update the nsMap slot reserved by nsAdd with the real socketFD.
	x.nsMap.Store(*nsName, nsi)
	x.fdToNsMap.Store(fd, *nsName)
	x.incrementStoreAndGenerationCounts()

	// If a delete raced in between the guard above and the Store, undo it so
	// we don't leave a stale entry (with a real fd) for a cancelled namespace.
	if nsCtx.Err() != nil {
		x.nsMap.Delete(*nsName)
		x.fdToNsMap.Delete(fd)
		x.pC.WithLabelValues("createWorksAndStore", "cancelRacedStore", "count").Inc()
	}
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
