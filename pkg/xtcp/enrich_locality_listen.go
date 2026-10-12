//go:build enrich_locality

// -localityEvents: one pkg/rtnlmon listener per monitored namespace keeps that
// namespace's locality snapshot current from rtnetlink notifications, so a
// link, address or route change shows within localityEventDebounceCst instead
// of on the next locality_refresh_interval re-dump. Refresh leaves a namespace
// alone while its listener is live, and takes it back when the listener ends.

package xtcp

import (
	"context"
	"log"
	"sync/atomic"
	"time"

	"github.com/prometheus/client_golang/prometheus"

	"github.com/randomizedcoder/xtcp2/pkg/localnet"
	"github.com/randomizedcoder/xtcp2/pkg/rtnlmon"
)

const (
	// localityEventDebounceCst is how long a change waits for others before
	// the snapshot is rebuilt: a rebuild walks the whole folded table, so a
	// burst (a link going down takes its addresses and routes with it) is one
	// rebuild, not one per notification.
	localityEventDebounceCst = time.Second

	// localityRcvBufCst sizes each listener's receive buffer. An overrun is
	// not lost state, rtnlmon resyncs, but each one is a full dump.
	localityRcvBufCst = 1 << 20
)

// localityGroups are the tables BuildSnapshot reads. Neighbors are not one.
var localityGroups = []rtnlmon.Group{
	rtnlmon.GroupLink,
	rtnlmon.GroupIPv4Addr, rtnlmon.GroupIPv6Addr,
	rtnlmon.GroupIPv4Route, rtnlmon.GroupIPv6Route,
}

// localityListenerSeams are the listener's test seams (nil means the real
// thing) and its live count.
type localityListenerSeams struct {
	// run is rtnlmon.Run.
	run func(ctx context.Context, opts rtnlmon.Options, h rtnlmon.Handler) error
	// afterFunc is time.AfterFunc: it calls f on its own goroutine after d.
	afterFunc func(d time.Duration, f func())
	// wait sleeps d, returning false if ctx ended first.
	wait func(ctx context.Context, d time.Duration) bool

	live atomic.Int64
}

// Listen implements localityEnricher: it keeps inode's snapshot current until
// ctx ends, restarting the listener on the 30s→5m backoff when it fails. It
// must run on a goroutine whose OS thread is in that namespace: rtnlmon opens
// its sockets, every resync dump's included, in the calling thread's
// namespace. netNamespaceInstance calls it on its locked thread, which costs
// no extra thread; openDefaultNetLinkSocket on a plain goroutine for the
// daemon's own namespace.
func (l *localityIndex) Listen(ctx context.Context, inode uint64) {
	x := l.x
	slot := l.slotFor(inode)
	opts := rtnlmon.Options{
		Groups: localityGroups,
		RcvBuf: localityRcvBufCst,
		// The timed re-dump's interval becomes the listener's backstop: a
		// resync after that long catches anything the fold missed.
		ResyncAfter: x.config.GetLocalityRefreshInterval().AsDuration(),
	}
	h := &localityListener{
		l: l, slot: slot,
		events: x.pC.WithLabelValues("localityListener", "events", "count"),
	}
	var retry localityRetryState
	retrying := false
	for {
		started := l.now()
		err := l.runListener(ctx, opts, h)
		l.release(slot)
		if ctx.Err() != nil {
			return
		}
		x.pC.WithLabelValues("localityListener", "run", "error").Inc()
		if x.debugLevel > 10 {
			log.Printf("localityListener: inode=%d: %v", inode, err)
		}
		// One that ran healthy for longer than the cap starts the backoff over.
		if l.now().Sub(started) >= localityRetryMaxCst {
			retrying = false
		}
		retry = nextLocalityRetry(retry, retrying, l.now(), true)
		retrying = true
		if !l.listenWait(ctx, retry.backoff) {
			return
		}
		x.pC.WithLabelValues("localityListener", "restarts", "count").Inc()
	}
}

func (l *localityIndex) runListener(ctx context.Context, opts rtnlmon.Options, h rtnlmon.Handler) error {
	if l.listener.run != nil {
		return l.listener.run(ctx, opts, h)
	}
	return rtnlmon.Run(ctx, opts, h)
}

func (l *localityIndex) afterFunc(d time.Duration, f func()) {
	if l.listener.afterFunc != nil {
		l.listener.afterFunc(d, f)
		return
	}
	time.AfterFunc(d, f)
}

func (l *localityIndex) listenWait(ctx context.Context, d time.Duration) bool {
	if l.listener.wait != nil {
		return l.listener.wait(ctx, d)
	}
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-ctx.Done():
		return false
	case <-t.C:
		return true
	}
}

// setLive records a slot gaining (or losing) its listener; slot.mu is held.
func (l *localityIndex) setLive(slot *localitySlot, live bool) {
	if slot.live == live {
		return
	}
	slot.live = live
	delta := int64(-1)
	if live {
		delta = 1
	}
	n := l.listener.live.Add(delta)
	l.x.pGV.WithLabelValues("localityListener", "live", "gauge").Set(float64(n))
}

// release hands a slot back to Refresh when its listener ends. The snapshot
// stays, as the last good one; a flush timer still set finds nothing to do.
func (l *localityIndex) release(slot *localitySlot) {
	slot.mu.Lock()
	defer slot.mu.Unlock()
	l.setLive(slot, false)
	slot.tracker, slot.pending = nil, false
}

// flush publishes a slot's pending changes. It runs on the debounce timer's
// goroutine, which is why the listener needs no timer of its own: its
// goroutine is blocked receiving.
func (l *localityIndex) flush(slot *localitySlot) {
	slot.mu.Lock()
	defer slot.mu.Unlock()
	if slot.live && slot.pending {
		l.rebuild(slot)
	}
}

// rebuild publishes the slot's fold; slot.mu is held.
func (l *localityIndex) rebuild(slot *localitySlot) {
	start := l.now()
	slot.snap.Store(slot.tracker.Snapshot())
	slot.pending = false
	l.x.pC.WithLabelValues("localityListener", "rebuilds", "count").Inc()
	l.x.pH.WithLabelValues("localityListener", "rebuild", "duration").Observe(l.now().Sub(start).Seconds())
}

// localityListener is one namespace's rtnlmon.Handler.
type localityListener struct {
	l      *localityIndex
	slot   *localitySlot
	events prometheus.Counter
}

// Snapshot replaces the fold and publishes it at once: the first one is
// what makes the listener live, and a resync's should not wait.
func (h *localityListener) Snapshot(s rtnlmon.Snapshot, reason rtnlmon.Reason) error {
	h.l.x.pC.WithLabelValues("localityListener", "snapshot", reason.String()).Inc()
	slot := h.slot
	slot.mu.Lock()
	defer slot.mu.Unlock()
	if slot.tracker == nil {
		slot.tracker = localnet.NewTracker()
	}
	slot.tracker.Reset(s.Links, s.Addrs, s.Routes)
	h.l.rebuild(slot)
	h.l.setLive(slot, true)
	return nil
}

// Event folds one notification. The first change since the last publish sets
// the debounce timer; later ones ride on it.
func (h *localityListener) Event(ev any) error {
	h.events.Inc()
	slot := h.slot
	slot.mu.Lock()
	defer slot.mu.Unlock()
	if slot.tracker == nil || !slot.tracker.Apply(ev) || slot.pending {
		return nil
	}
	slot.pending = true
	h.l.afterFunc(localityEventDebounceCst, func() { h.l.flush(slot) })
	return nil
}
