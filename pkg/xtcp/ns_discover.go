package xtcp

import (
	"log"
	"time"

	"github.com/randomizedcoder/xtcp2/pkg/nsdiscover"
)

// discoverNamespaces scans /proc for the current set of live network namespaces
// (Method B) and returns them keyed by inode, with a best-effort name resolved
// for each. This replaces the old /run/netns + /run/docker/netns directory scan:
// it sees every namespace that has a live process — including anonymous
// container/pod namespaces with no bind mount — which is the whole point.
//
// xtcp's own netns (x.selfNsInode) is included in the result; the reconcile step
// dedups it against the default socket openDefaultNetLinkSocket already holds.
//
// Not safe for concurrent use: nsScanner and nsResolver are reused single-owner
// state, so only the reconcile owner (mapReconciler) may call this.
func (x *XTCP) discoverNamespaces() map[uint64]nsIdentity {

	startTime := time.Now()
	defer func() {
		x.pH.WithLabelValues("discoverNamespaces", "complete", "counter").Observe(time.Since(startTime).Seconds())
	}()
	x.pC.WithLabelValues("discoverNamespaces", "start", "counter").Inc()

	// Rebuild the bind-mount name index so freshly-named namespaces get their
	// friendly name this cycle.
	x.nsResolver.Refresh()

	out := make(map[uint64]nsIdentity)
	skipped := x.nsScanner.Scan(func(ns nsdiscover.Namespace) {
		out[ns.Inode] = nsIdentity{
			inode: ns.Inode,
			pid:   ns.Pid,
			name:  x.nsResolver.Name(ns.Inode, ns.Pid),
		}
	})

	x.pC.WithLabelValues("discoverNamespaces", "found", "counter").Add(float64(len(out)))
	if skipped > 0 {
		x.pC.WithLabelValues("discoverNamespaces", "skipped", "counter").Add(float64(skipped))
	}
	if x.debugLevel > 10 {
		log.Printf("discoverNamespaces found:%d skipped:%d", len(out), skipped)
	}
	return out
}
