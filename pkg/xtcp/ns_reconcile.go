package xtcp

import (
	"context"
	"log"
	"sync"
	"time"
)

// mapReconciler is a ticking loop around reconcile, which converges xtcp's set
// of tracked network namespaces (nsMap) with the live set discovered by scanning
// /proc. Under Method B this loop is the discovery mechanism itself (there is no
// inotify watcher): its first pass populates the initial set, and each tick
// re-derives ground truth and applies the delta.
func (x *XTCP) mapReconciler(ctx context.Context, wg *sync.WaitGroup) {

	defer wg.Done()

	x.pC.WithLabelValues("mapReconciler", "start", "count").Inc()
	defer x.pC.WithLabelValues("mapReconciler", "complete", "count").Inc()

	dels, stores := x.reconcile(ctx)
	if x.debugLevel > 10 {
		log.Printf("mapReconciler dels:%d, stores:%d", dels, stores)
	}

	t := time.NewTicker(reconcileFrequency)
	defer t.Stop()
	for {
		select {
		case <-t.C:
			dels, stores = x.reconcile(ctx)
			x.pC.WithLabelValues("mapReconciler", "tick", "count").Inc()
			x.pC.WithLabelValues("mapReconciler", "dels", "count").Add(float64(dels))
			x.pC.WithLabelValues("mapReconciler", "stores", "count").Add(float64(stores))
			if x.debugLevel > 10 {
				log.Printf("mapReconciler dels:%d, stores:%d", dels, stores)
			}
		case <-ctx.Done():
			return
		}
	}
}

// reconcile converges nsMap with the live namespace set from /proc:
//   - inodes in nsMap that are no longer live are deleted (their per-ns ctx is
//     cancelled, tearing down the netlinkers + closing the socket);
//   - live inodes not yet in nsMap are added via nsAdd (which enters the
//     namespace by /proc/<pid>/ns/net and opens its netlink socket);
//   - inodes present in both are left alone — the inode IS the identity, so
//     there is no "value drift" to chase (unlike the old path-keyed map).
//
// Returns the count of deletes and adds. The namespace set is keyed by inode, so
// two pids in the same namespace collapse to one entry, and a namespace whose
// representative pid died but that still has other live processes keeps a fresh
// pid on the next scan.
func (x *XTCP) reconcile(ctx context.Context) (dels, stores int) {

	// Serialize against the other caller (background mapReconciler vs. the
	// Poller's pre-poll reconcile). Beyond keeping the nsMap diff coherent, this
	// is REQUIRED for memory safety: discoverNamespaces drives nsScanner, which
	// reuses buffers and a persistent /proc fd and must never run concurrently.
	x.reconcileMu.Lock()
	defer x.reconcileMu.Unlock()

	startTime := time.Now()
	defer func() {
		x.pH.WithLabelValues("reconcile", "complete", "counter").Observe(time.Since(startTime).Seconds())
	}()
	x.pC.WithLabelValues("reconcile", "start", "count").Inc()

	live := x.discoverNamespaces()

	// Delete entries whose namespace is no longer live.
	x.nsMap.Range(func(key, _ interface{}) bool {
		inode, ok := key.(uint64)
		if !ok {
			return true
		}
		if _, stillLive := live[inode]; !stillLive {
			x.nsDelete(inode)
			dels++
		}
		return true
	})

	// Add live namespaces not yet tracked.
	for inode, id := range live {
		if _, tracked := x.nsMap.Load(inode); !tracked {
			x.nsAdd(ctx, id)
			stores++
		}
	}

	return dels, stores
}
