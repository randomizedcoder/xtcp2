package xtcp

import (
	"context"
	"log"
)

// nsAdd reserves the namespace's slot (keyed by inode) and starts its
// netNamespaceInstance goroutine, which enters the namespace via
// /proc/<pid>/ns/net and opens its netlink socket.
//
// The per-ns context + cancel are created HERE and stored in nsMap *before* the
// goroutine is launched, so nsDelete can always reach cancel() even if the
// namespace disappears during the (possibly slow, retrying) setns — see the
// thread-exhaustion regression in ns_churn_race_test.go. LoadOrStore makes the
// "already present?" check and the slot reservation atomic.
func (x *XTCP) nsAdd(ctx context.Context, id nsIdentity) {

	x.pC.WithLabelValues("add", "store", "counter").Inc()

	if x.debugLevel > 10 {
		log.Printf("add: inode=%d pid=%d name=%s\n", id.inode, id.pid, id.name)
	}

	// Copy the name so the netNSitem / record-label *string has stable backing
	// independent of the caller's nsIdentity value.
	name := id.name
	nsCtx, nsCancel := context.WithCancel(ctx)

	if _, loaded := x.nsMap.LoadOrStore(id.inode, netNSitem{
		inode:    id.inode,
		pid:      id.pid,
		name:     &name,
		ctx:      nsCtx,
		cancel:   nsCancel,
		socketFD: -1, // not opened yet; netNamespaceInstance fills it in
	}); loaded {
		// Already tracked — release the context we just made and bail.
		nsCancel()
		x.pC.WithLabelValues("add", "duplicate", "counter").Inc()
		if x.debugLevel > 10 {
			log.Printf("add duplicate: inode=%d\n", id.inode)
		}
		return
	}

	go x.netNamespaceInstance(nsCtx, nsCancel, id.inode, id.pid, &name)
}
