package xtcp

import (
	"context"
	"log"
)

// add checks if the namespace already has an open netlink socket
// if not, starts a new goroutine netNamespaceInstance, which will
// open a netlink socket in the target network namespace, and
// store the socketFD in the nsMap
func (x *XTCP) nsAdd(ctx context.Context, nsName *string) {

	x.pC.WithLabelValues("add", "store", "counter").Inc()

	if x.debugLevel > 10 {
		log.Printf("add: %s\n", *nsName)
	}

	_, ok := x.nsMap.Load(*nsName)
	if ok {
		x.pC.WithLabelValues("add", "duplicate", "counter").Inc()
		if x.debugLevel > 10 {
			log.Printf("add duplicate: %s\n", *nsName)
		}
		return
	}

	go x.netNamespaceInstance(ctx, nsName)
}
