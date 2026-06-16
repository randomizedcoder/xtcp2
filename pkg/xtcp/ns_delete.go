package xtcp

import (
	"log"
)

func (x *XTCP) nsDelete(nsName *string) {

	if x.debugLevel > 10 {
		log.Printf("delete: %s\n", *nsName)
	}

	value, ok := x.nsMap.Load(*nsName)
	if !ok {
		x.pC.WithLabelValues("delete", "load", "error").Inc()
		if x.debugLevel > 10 {
			log.Printf("delete x.nsMap.Load(%s) error", *nsName)
		}
		return
	}

	netNSItem, ok := value.(netNSitem)
	if !ok {
		x.pC.WithLabelValues("delete", "assert", "error").Inc()
		if x.debugLevel > 10 {
			log.Printf("delete x.nsMap value type assertion failed for %s", *nsName)
		}
		return
	}

	// signal the go routine to close
	// ( i'm not really sure if it would automatically close )
	netNSItem.cancel()
	if x.debugLevel > 10 {
		log.Printf("delete cancel(): %s", *nsName)
	}

	fd := netNSItem.socketFD
	x.nsMap.Delete(*nsName)
	x.fdToNsMap.Delete(fd)
	// pollTime is keyed by fd (poller.go:208 x.pollTime.Store(fd, ...)).
	// Without an explicit Delete, every namespace add/remove cycle
	// leaves a stale entry — eventually filling the sync.Map with
	// dead fd numbers. The poller's observeNetlinkerDone still tries
	// to Load by fd and gets the stale time, producing a misleading
	// pollToDoneDuration histogram observation if the fd number is
	// later reused for an unrelated socket.
	x.pollTime.Delete(fd)
	x.incrementDeleteAndGenerationCounts()

	x.pC.WithLabelValues("delete", "delete", "counter").Inc()

	if x.debugLevel > 10 {
		log.Printf("delete namespace: %s", *nsName)
	}

}

func (x *XTCP) incrementDeleteAndGenerationCounts() {
	x.deleteCount.Add(1)
	x.generation.Add(1)
}
