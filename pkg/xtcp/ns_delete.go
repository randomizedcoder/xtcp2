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

	netNSItem := value.(netNSitem)

	// signal the go routine to close
	// ( i'm not really sure if it would automatically close )
	netNSItem.cancel()
	if x.debugLevel > 10 {
		log.Printf("delete cancel(): %s", *nsName)
	}

	fd := netNSItem.socketFD
	x.nsMap.Delete(*nsName)
	x.fdToNsMap.Delete(fd)
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
