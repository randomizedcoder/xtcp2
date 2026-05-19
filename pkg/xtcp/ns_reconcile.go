package xtcp

import (
	"context"
	"log"
	"sync"
	"time"
)

// mapReconciler is a ticking loop around reconcile, which
// will reconcile xtcp's list of network namespaces, and the file system's
func (x *XTCP) mapReconciler(ctx context.Context, wg *sync.WaitGroup) {

	defer wg.Done()

	x.pC.WithLabelValues("mapReconciler", "start", "count").Inc()
	defer x.pC.WithLabelValues("mapReconciler", "complete", "count").Inc()

	dels, stores := x.reconcile(ctx)
	if x.debugLevel > 10 {
		log.Printf("mapReconciler dels:%d, stores:%d", dels, stores)
	}

	t := time.NewTicker(reconcileFrequency)
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

// reconcile performs reconsiliation between network namespaces on the file system,
// and the list of network namespaces xtcp has ( this app has )
// this is to ensure the kernel and the app don't get out of sync.  they should not
// get out of sync frequently, but it could happen
func (x *XTCP) reconcile(ctx context.Context) (int, int) {
	startTime := time.Now()
	defer func() {
		x.pH.WithLabelValues("reconcile", "complete", "counter").Observe(time.Since(startTime).Seconds())
	}()
	x.pC.WithLabelValues("reconcile", "start", "count").Inc()

	return x.reconcileMaps(ctx, x.discoverAllNamespaces(), x.nsMap, false)
}

// reconcileMaps reconciles srcMap into destMap. The dest is mutated to
// converge with src:
//
//   - Entries in dest that are missing from src are deleted.
//   - Entries in dest whose src value is non-nil AND differs from the
//     dest value are also deleted; the second pass re-stores the fresh
//     src value. (The "stale value" branch — kept so existing tests
//     that pass non-nil src values still exercise replace-on-drift.)
//   - In production discoverNamespaces stores keys with nil values;
//     that nil must NOT count as "drift" — comparing nil against the
//     destMap's netNSitem struct would otherwise delete every entry
//     every cycle, orphaning each existing netNamespaceInstance
//     goroutine + its open netlink socketFD.
//   - Entries in src that are now missing from dest are stored — in
//     production via x.nsAdd which kicks the namespace-instance goroutine;
//     in `testing=true` callers the raw value is copied over.
//
// Returns the count of deletes and stores observed during the pass.
func (x *XTCP) reconcileMaps(ctx context.Context, srcMap, destMap *sync.Map, testing bool) (deleteCount, storeCount int) {

	destMap.Range(func(key, value interface{}) bool {
		// Delete when the key is gone from src OR (src has a non-nil
		// value that differs from dest). Treating nil src values as
		// drift would incorrectly delete every production entry —
		// discoverNamespaces stores all its values as nil.
		srcValue, ok := srcMap.Load(key)
		if !ok || (srcValue != nil && srcValue != value) {
			// In production, destMap values are netNSitem structs that
			// own a cancel func + an in-flight netNamespaceInstance
			// goroutine + open netlink socketFD. Just deleting the map
			// entry leaves all of that orphaned. Cancel first so the
			// per-ns ctx fires, the netlinkers exit, and the deferred
			// closeSocket in netNamespaceInstance runs. testing=true
			// callers may pass arbitrary value types; only invoke
			// cancel when the value is actually a netNSitem.
			if !testing {
				if item, isItem := value.(netNSitem); isItem && item.cancel != nil {
					item.cancel()
				}
			}
			destMap.Delete(key)
			deleteCount++
		}
		return true
	})

	srcMap.Range(func(key, value interface{}) bool {
		if _, ok := destMap.Load(key); !ok {
			if testing {
				destMap.Store(key, value)
			} else {
				nsName, _ := key.(string) //nolint:errcheck // sourceMap.Range keys are strings
				x.nsAdd(ctx, &nsName)
			}
			storeCount++
		}
		return true
	})

	return deleteCount, storeCount
}
