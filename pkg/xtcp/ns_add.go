package xtcp

import (
	"context"
	"log"
)

// nsAdd reserves the namespace's slot and starts its netNamespaceInstance
// goroutine.
//
// The per-ns context + cancel are created HERE and stored in nsMap *before*
// the goroutine is launched. Previously the cancel was created deep inside
// netNamespaceInstance — only after LockOSThread + setns + socket bind (which
// can take milliseconds, or up to seconds when setns retries). A namespace
// deleted during that init window (trivial under heavy `ip netns add/del`
// churn) found no nsMap entry, so nsDelete never called cancel(); the instance
// then blocked forever on <-nsCtx.Done() holding a locked OS thread. Those
// leaked threads accumulate to the SetMaxThreads (-maxThreads, default 2000)
// ceiling and crash the daemon with "fatal error: thread exhaustion".
// Reserving the cancel up front guarantees nsDelete can always reach it, so a
// delete-during-init reliably unblocks the instance.
//
// LoadOrStore makes the "already present?" check and the slot reservation
// atomic, closing a second race where two adds for the same name could both
// pass a Load() check and launch duplicate goroutines.
func (x *XTCP) nsAdd(ctx context.Context, nsName *string) {

	x.pC.WithLabelValues("add", "store", "counter").Inc()

	if x.debugLevel > 10 {
		log.Printf("add: %s\n", *nsName)
	}

	// Copy the name: callers (the fsnotify watch loop) reuse the backing
	// string variable, so we must not retain their pointer across the
	// goroutine's lifetime.
	name := *nsName
	nsCtx, nsCancel := context.WithCancel(ctx)

	if _, loaded := x.nsMap.LoadOrStore(name, netNSitem{
		name:     &name,
		ctx:      nsCtx,
		cancel:   nsCancel,
		socketFD: -1, // not opened yet; netNamespaceInstance fills it in
	}); loaded {
		// Already tracked — release the context we just made and bail.
		nsCancel()
		x.pC.WithLabelValues("add", "duplicate", "counter").Inc()
		if x.debugLevel > 10 {
			log.Printf("add duplicate: %s\n", *nsName)
		}
		return
	}

	go x.netNamespaceInstance(nsCtx, nsCancel, &name)
}
