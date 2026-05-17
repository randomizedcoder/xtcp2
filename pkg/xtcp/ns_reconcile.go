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

// reconcileMaps reconciles srcMap into destMap, comparing both keys AND
// values. The dest is mutated to converge with src:
//
//   - Entries in dest that are missing from src, or whose value differs,
//     are deleted. (A stale value counts as out-of-sync; the second pass
//     re-stores the fresh value.)
//   - Entries in src that are now missing from dest are stored — in
//     production via x.nsAdd which kicks the namespace-instance goroutine;
//     in `testing=true` callers the raw value is copied over.
//
// Returns the count of deletes and stores observed during the pass.
func (x *XTCP) reconcileMaps(ctx context.Context, srcMap, destMap *sync.Map, testing bool) (deleteCount, storeCount int) {

	destMap.Range(func(key, value interface{}) bool {
		// Delete when the key is gone from src OR its value drifted.
		if srcValue, ok := srcMap.Load(key); !ok || srcValue != value {
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
