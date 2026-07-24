package xtcp

import (
	"log"
)

// nsDelete tears down a namespace by inode: cancels its per-ns context (which
// stops the netlinkers and unblocks netNamespaceInstance, whose deferred
// closeSocket then closes the fd), and removes it from nsMap / fdToNsMap /
// pollTime. Called by reconcile when a namespace's inode is no longer live.
func (x *XTCP) nsDelete(inode uint64) {

	if x.debugLevel > 10 {
		log.Printf("delete: inode=%d\n", inode)
	}

	value, ok := x.nsMap.Load(inode)
	if !ok {
		x.pC.WithLabelValues("delete", "load", "error").Inc()
		if x.debugLevel > 10 {
			log.Printf("delete x.nsMap.Load(%d) error", inode)
		}
		return
	}

	netNSItem, ok := value.(netNSitem)
	if !ok {
		x.pC.WithLabelValues("delete", "assert", "error").Inc()
		if x.debugLevel > 10 {
			log.Printf("delete x.nsMap value type assertion failed for inode=%d", inode)
		}
		return
	}

	// signal the go routine to close
	netNSItem.cancel()
	if x.debugLevel > 10 {
		log.Printf("delete cancel(): inode=%d", inode)
	}

	fd := netNSItem.socketFD
	x.nsMap.Delete(inode)
	x.fdToNsMap.Delete(fd)
	// pollTime is keyed by fd (poller.go x.pollTime.Store(fd, ...)). Without an
	// explicit Delete, every namespace add/remove cycle leaves a stale entry.
	x.pollTime.Delete(fd)
	x.incrementDeleteAndGenerationCounts()

	x.pC.WithLabelValues("delete", "delete", "counter").Inc()

	if x.debugLevel > 10 {
		log.Printf("delete namespace: inode=%d", inode)
	}
}

func (x *XTCP) incrementDeleteAndGenerationCounts() {
	x.deleteCount.Add(1)
	x.generation.Add(1)
}
