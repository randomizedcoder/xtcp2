package xtcp

import (
	"log"
	"time"

	"github.com/randomizedcoder/xtcp2/pkg/nsdiscover"
)

// discoverNamespaces returns the current set of network namespaces keyed by
// inode, with a best-effort name resolved for each. It is a UNION of two passes:
//
//  1. the /proc scan (Method B, nsScanner): every namespace that has a live
//     process — including anonymous container/pod namespaces with no bind mount.
//     Yields a representative pid; entered via /proc/<pid>/ns/net.
//  2. the bind-mount scan (nsResolver.EachBindMount over /run/netns +
//     /run/docker/netns): every namespace with a bind mount, entered by opening
//     that path directly (setns on the fd). This needs NO host PID, so it
//     restores container/netns visibility on deployments that don't set
//     pid_mode:host — the production root cause of netns="default".
//
// The /proc-scan entry wins when both passes find the same inode (its live pid
// is the cheapest entry handle and needs no mount-readiness gate).
//
// xtcp's own netns (x.selfNsInode) is included; the reconcile step dedups it
// against the default socket openDefaultNetLinkSocket already holds.
//
// Not safe for concurrent use: nsScanner and nsResolver are reused single-owner
// state, so only the reconcile owner (mapReconciler) may call this.
func (x *XTCP) discoverNamespaces() map[uint64]nsIdentity {

	startTime := time.Now()
	defer func() {
		x.pH.WithLabelValues("discoverNamespaces", "complete", "counter").Observe(time.Since(startTime).Seconds())
	}()
	x.pC.WithLabelValues("discoverNamespaces", "start", "counter").Inc()

	// Rebuild the bind-mount name + path indexes so freshly-named/mounted
	// namespaces get their friendly name and entry path this cycle.
	x.nsResolver.Refresh()

	out := make(map[uint64]nsIdentity)

	// Pass 1: /proc scan (live-pid entry).
	skipped := x.nsScanner.Scan(func(ns nsdiscover.Namespace) {
		out[ns.Inode] = nsIdentity{
			inode: ns.Inode,
			pid:   ns.Pid,
			name:  x.nsResolver.Name(ns.Inode, ns.Pid),
		}
	})
	procFound := len(out)

	// Pass 2: bind-mount scan (pid-less entry) — only for inodes the /proc scan
	// did not already cover.
	var bindFound int
	x.nsResolver.EachBindMount(func(inode uint64, path string) {
		if _, ok := out[inode]; ok {
			return
		}
		out[inode] = nsIdentity{
			inode: inode,
			pid:   0, // no live pid; entered via path
			name:  x.nsResolver.Name(inode, 0),
			path:  path,
		}
		bindFound++
	})

	x.pC.WithLabelValues("discoverNamespaces", "found", "counter").Add(float64(len(out)))
	x.pC.WithLabelValues("discoverNamespaces", "foundProc", "counter").Add(float64(procFound))
	if bindFound > 0 {
		x.pC.WithLabelValues("discoverNamespaces", "foundBindMount", "counter").Add(float64(bindFound))
	}
	if skipped > 0 {
		x.pC.WithLabelValues("discoverNamespaces", "skipped", "counter").Add(float64(skipped))
	}
	if x.debugLevel > 10 {
		log.Printf("discoverNamespaces found:%d (proc:%d bindMount:%d) skipped:%d", len(out), procFound, bindFound, skipped)
	}

	// Opt-in nsid snapshot (usually all-zero for docker/containerd; netns_inode
	// is the stable key). Best-effort, single-owner (reconcile) — see enrich.go.
	if x.config != nil && x.config.PopulateNsid {
		x.refreshNsids(out)
	}

	return out
}
