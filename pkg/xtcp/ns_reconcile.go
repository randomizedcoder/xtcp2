package xtcp

import (
	"context"
	"log"
	"maps"
	"sync"
	"time"

	"github.com/randomizedcoder/xtcp2/pkg/nsdiscover"
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

	// The background ticker is a floor/fallback: with reconcileBeforePoll the
	// Poller reconciles every cycle, so this mainly covers the poller-idle /
	// poller-disabled cases. A configured 0 disables it entirely — the initial
	// reconcile above still ran, so a poller-driven daemon keeps discovering via
	// its pre-poll reconcile; block until shutdown.
	freq := x.backgroundReconcileFrequency()
	if freq <= 0 {
		if x.debugLevel > 10 {
			log.Printf("mapReconciler background ticker disabled (reconcileFrequency=0)")
		}
		<-ctx.Done()
		return
	}

	t := time.NewTicker(freq)
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
//     canceled, tearing down the netlinkers + closing the socket);
//   - live inodes not yet in nsMap are added via nsAdd (which enters the
//     namespace by /proc/<pid>/ns/net and opens its netlink socket);
//   - inodes present in both are left alone — the inode IS the identity, so
//     there is no "value drift" to chase (unlike the old path-keyed map).
//
// Returns the count of deletes and adds. The namespace set is keyed by inode, so
// two pids in the same namespace collapse to one entry, and a namespace whose
// representative pid died but that still has other live processes keeps a fresh
// pid on the next scan.
// backgroundReconcileFrequency is the mapReconciler ticker period, from config
// when set, else the package default. 0 disables the background ticker.
func (x *XTCP) backgroundReconcileFrequency() time.Duration {
	if x.config != nil && x.config.ReconcileFrequency != nil {
		return x.config.ReconcileFrequency.AsDuration()
	}
	return reconcileFrequency
}

// reconcileBeforePollEnabled reports whether the Poller reconciles before each
// poll cycle. Defaults to true (the production flag default) when config is
// absent, so tests and the daemon agree.
func (x *XTCP) reconcileBeforePollEnabled() bool {
	return x.config == nil || x.config.ReconcileBeforePoll
}

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

	lim := x.scanLimits(startTime)
	live, stop := x.discoverNamespaces(lim)
	if stop != nsdiscover.ScanComplete {
		x.reconcileIncomplete(stop, len(live), startTime)
	}

	// After a timeout the scan didn't see every pid, so a namespace it
	// didn't report may still be live: delete nothing this time. (A capped
	// scan saw every pid and reported every tracked namespace still live.)
	// The cost: while scans keep timing out, a namespace that died keeps
	// its slot. Checking a tracked namespace's last-seen pid instead would
	// let ordinary process churn during a flood evict live, monitored
	// namespaces; xtcp_reconcile_incomplete_total{reason="timeout"} shows
	// either way.
	if stop != nsdiscover.ScanTimeout {
		dels = x.deleteGone(live)
	}
	stores = x.addNew(ctx, live, stop, startTime)
	return dels, stores
}

// scanLimits bounds one reconcile's scan: -reconcileTimeout from start, and
// at most 4 x -maxNetns new namespaces reported. Tracked and waiting
// namespaces are reported past that, so the cap only limits how many new
// ones a scan takes in. 0 turns either off.
func (x *XTCP) scanLimits(start time.Time) nsdiscover.ScanLimits {
	var lim nsdiscover.ScanLimits
	if d := x.config.GetReconcileTimeout().AsDuration(); d > 0 {
		lim.Deadline = start.Add(d)
	}
	if n := x.config.GetMaxNetns(); n > 0 {
		lim.MaxNamespaces = scanCapPerMaxNetns * int(n)
		lim.Keep = func(inode uint64) bool {
			if _, ok := x.nsWaiting[inode]; ok {
				return true
			}
			_, ok := x.nsMap.Load(inode)
			return ok
		}
	}
	return lim
}

// scanCapPerMaxNetns sizes the scan cap from -maxNetns.
const scanCapPerMaxNetns = 4

// Reasons for xtcp_reconcile_incomplete_total.
const (
	reconcileIncompleteTimeoutCst = "timeout"
	reconcileIncompleteScanCapCst = "scan_cap"
)

func (x *XTCP) reconcileIncomplete(stop nsdiscover.ScanStop, found int, start time.Time) {
	reason, what := reconcileIncompleteTimeoutCst, "nothing deleted this reconcile"
	if stop == nsdiscover.ScanCap {
		reason, what = reconcileIncompleteScanCapCst, "some new namespaces not seen this reconcile"
	}
	if x.pReconcileIncomplete != nil {
		x.pReconcileIncomplete.WithLabelValues(reason).Inc()
	}
	log.Printf("reconcile: scan incomplete (%s) with %d namespaces in %s; %s",
		reason, found, time.Since(start).Round(time.Millisecond), what)
}

// deleteGone deletes tracked entries whose namespace is no longer live.
// xtcp's own netns is live by construction (every unlocked thread is in
// it), and its default socket belongs to openDefaultNetLinkSocket, so it is
// never deleted. The scan cannot vouch for it: it skips xtcp's own pid,
// and when xtcp is alone in its netns (a container without pid_mode:host)
// no other pid shows it.
func (x *XTCP) deleteGone(live map[uint64]nsIdentity) (dels int) {
	x.nsMap.Range(func(key, _ interface{}) bool {
		inode, ok := key.(uint64)
		if !ok || inode == x.selfNsInode {
			return true
		}
		if _, stillLive := live[inode]; !stillLive {
			x.nsDelete(inode)
			dels++
		}
		return true
	})
	return dels
}

// ownerResolveMinCst is how many candidates' owners a reconcile resolves
// even past its deadline, oldest waiters first, so a slow scan can't stop
// admission altogether.
const ownerResolveMinCst = 64

// addNew adds live namespaces not yet tracked, within the admission caps
// (admitNamespaces). Tracked ones keep their slots; the rest wait in the
// order first seen and stay candidates on every later reconcile. Names are
// resolved only for the namespaces admitted.
func (x *XTCP) addNew(ctx context.Context, live map[uint64]nsIdentity, stop nsdiscover.ScanStop, start time.Time) (stores int) {
	var candidates []nsIdentity
	for inode, id := range live {
		if _, tracked := x.nsMap.Load(inode); !tracked {
			candidates = append(candidates, id)
		}
	}
	lim := admitLimits{
		maxNetns:    x.config.GetMaxNetns(),
		maxPerOwner: x.config.GetMaxNetnsPerOwner(),
		maxAdds:     x.config.GetMaxNetnsAddsPerReconcile(),
		self:        x.selfNsInode,
	}
	if len(candidates) == 0 && stop != nsdiscover.ScanTimeout {
		clear(x.nsWaiting)
		clear(x.nsOwners)
		return 0
	}

	tracked := 0
	var owners map[string]int
	if lim.maxNetns != 0 {
		tracked = lenSyncMap(x.nsMap)
	}
	resolvable := candidates
	if lim.maxPerOwner != 0 {
		owners = x.trackedOwners()
		var deadline time.Time
		if d := x.config.GetReconcileTimeout().AsDuration(); d > 0 {
			deadline = start.Add(d)
		}
		resolvable = x.resolveOwners(candidates, deadline)
	}

	admit, drops := admitNamespaces(resolvable, tracked, owners, lim, x.nsWaiting)
	if lim.any() {
		var prev map[uint64]uint64
		if stop == nsdiscover.ScanTimeout {
			prev = maps.Clone(x.nsWaiting) // queueWaiting may clear it in place
		}
		// The full candidate list: one whose owner wasn't read keeps its
		// place too.
		var overflow int
		// A namespace whose owner is at quota gets no queue place: one owner
		// can't fill the queue, or keep its namespaces reported past the
		// scan cap. Its place only matters once the owner is under quota.
		overQuota := func(id nsIdentity) bool {
			if lim.maxPerOwner == 0 || id.pid <= 0 {
				return false
			}
			if _, resolved := x.nsOwners[id.inode]; !resolved {
				return false
			}
			return owners[id.owner] >= int(lim.maxPerOwner)
		}
		x.nsWaiting, x.nsWaitingSeq, overflow = queueWaiting(x.nsWaiting, x.nsWaitingSeq, candidates, admit, scanCapPerMaxNetns*int(lim.maxNetns), overQuota)
		if overflow > 0 {
			x.pC.WithLabelValues("reconcile", "queue", "full").Add(float64(overflow))
		}
		if stop == nsdiscover.ScanTimeout {
			// A waiter the scan didn't reach keeps its place.
			x.nsWaiting = keepUnseenWaiters(x.nsWaiting, prev, live)
		} else {
			x.pruneOwners(candidates, admit)
		}
	}
	for _, id := range admit {
		id.name = x.nsResolver.Name(id.inode, id.pid)
		x.nsAdd(ctx, id)
		stores++
	}
	x.recordDrops(drops, lim)
	return stores
}

// keepUnseenWaiters returns next plus every entry of prev whose namespace
// the scan didn't report.
func keepUnseenWaiters(next, prev map[uint64]uint64, live map[uint64]nsIdentity) map[uint64]uint64 {
	for inode, seq := range prev {
		if _, seen := live[inode]; seen {
			continue
		}
		if next == nil {
			next = make(map[uint64]uint64)
		}
		next[inode] = seq
	}
	return next
}

// trackedOwners counts tracked namespaces per owner; xtcp2's own and
// bind-mount-only ones (no pid) are exempt from the quota.
func (x *XTCP) trackedOwners() map[string]int {
	owners := make(map[string]int)
	x.nsMap.Range(func(key, value interface{}) bool {
		item, ok := value.(netNSitem)
		if ok && item.pid > 0 && item.inode != x.selfNsInode {
			owners[item.owner]++
		}
		return true
	})
	return owners
}

// resolveOwners fills each candidate's owner from the cache, or its
// cgroup, oldest waiter first. Past deadline it still resolves
// ownerResolveMinCst, so admission always moves. It returns the candidates
// that have an owner; the rest (out of time, or the pid exited while being
// read) wait for a later reconcile, keeping their queue place.
func (x *XTCP) resolveOwners(candidates []nsIdentity, deadline time.Time) []nsIdentity {
	if x.nsOwners == nil {
		x.nsOwners = make(map[uint64]string)
	}
	sortByQueue(candidates, x.nsWaiting)
	out := candidates[:0:0]
	deferred, read := 0, 0
	for i := range candidates {
		id := &candidates[i]
		if id.pid <= 0 {
			out = append(out, *id) // exempt: no owner to read
			continue
		}
		if o, ok := x.nsOwners[id.inode]; ok {
			id.owner = o
			out = append(out, *id)
			continue
		}
		if read >= ownerResolveMinCst && !deadline.IsZero() && time.Now().After(deadline) {
			deferred++
			continue
		}
		read++
		o, ok := x.nsResolver.Owner(id.pid)
		if !ok {
			deferred++
			continue
		}
		id.owner = o
		x.nsOwners[id.inode] = o
		out = append(out, *id)
	}
	if deferred > 0 {
		x.pC.WithLabelValues("reconcile", "owner", "deferred").Add(float64(deferred))
	}
	return out
}

// pruneOwners keeps the owner cache to candidates still waiting.
func (x *XTCP) pruneOwners(candidates, admit []nsIdentity) {
	if len(x.nsOwners) == 0 {
		return
	}
	keep := make(map[uint64]struct{}, len(candidates))
	for _, id := range candidates {
		keep[id.inode] = struct{}{}
	}
	for _, id := range admit {
		delete(keep, id.inode)
	}
	for inode := range x.nsOwners {
		if _, ok := keep[inode]; !ok {
			delete(x.nsOwners, inode)
		}
	}
}

// recordDrops counts and logs the candidates each cap left out (one line
// per reconcile).
func (x *XTCP) recordDrops(drops admitDrops, lim admitLimits) {
	if drops.total() == 0 {
		return
	}
	for reason, n := range map[string]int{
		netnsDropMaxNetnsCst:   drops.maxNetns,
		netnsDropOwnerQuotaCst: drops.ownerQuota,
		netnsDropAddRateCst:    drops.addRate,
	} {
		if n == 0 {
			continue
		}
		x.pC.WithLabelValues("reconcile", reason, "drop").Add(float64(n))
		if x.pNetnsDropped != nil {
			x.pNetnsDropped.WithLabelValues(reason).Add(float64(n))
		}
	}
	log.Printf("reconcile: %d namespaces not tracked yet: max_netns:%d (-maxNetns=%d) owner_quota:%d (-maxNetnsPerOwner=%d) add_rate:%d (-maxNetnsAddsPerReconcile=%d)",
		drops.total(), drops.maxNetns, lim.maxNetns, drops.ownerQuota, lim.maxPerOwner, drops.addRate, lim.maxAdds)
}
