//go:build enrich_locality

// Locality enrichment. Gated behind `enrich_locality` because pkg/localnet
// holds a gaissmai/bart prefix trie per network namespace, and because the
// per-namespace rtnetlink discovery below is a meaningful amount of code to
// carry in a build that will never run it. See enrich_core.go for the registry
// and the seam; pkg/xtcpnl stays untagged (generic netlink, used elsewhere).

package xtcp

import (
	"context"
	"errors"
	"log"
	"net/netip"
	"runtime"
	"sync/atomic"
	"time"

	"golang.org/x/sys/unix"

	"github.com/randomizedcoder/xtcp2/gen/go/xtcp_flat_record"
	"github.com/randomizedcoder/xtcp2/pkg/localnet"
	"github.com/randomizedcoder/xtcp2/pkg/xtcpnl"
)

func init() {
	RegisterEnricher(EnricherLocality, func(_ context.Context, x *XTCP) {
		x.initLocalityEnricher()
	})
}

// localityRecvTimeout bounds each rtnetlink dump's recv so a missing NLMSG_DONE
// degrades to a discovery error (that namespace stays unclassified) instead of
// blocking the reconcile owner.
var localityRecvTimeout = unix.Timeval{Sec: 2}

const (
	// localityRetryMinCst / localityRetryMaxCst bound the per-namespace
	// negative-cache backoff: the first hard failure (open/setns/dump error)
	// waits 30s, doubling each further failure up to 5m. A loopback-only
	// snapshot starts one notch earlier — it is re-dumped on the very next
	// reconcile, then follows the same schedule if lo is still all there is.
	localityRetryMinCst = 30 * time.Second
	localityRetryMaxCst = 5 * time.Minute

	// localityNewNsPerPassCst caps how many namespaces a NON-full reconcile
	// pass may dump (new namespaces plus expired retries). A container burst
	// of hundreds of namespaces is then classified over a few reconciles
	// instead of stalling one reconcile for every dump; the remainder are
	// counted as deferred and picked up next pass. A full pass (every
	// locality_refresh_interval) is uncapped — re-dumping everything is its
	// job.
	localityNewNsPerPassCst = 32

	// localityDumpRetriesCst is how many times one rtnetlink dump is re-issued
	// when the kernel flags NLM_F_DUMP_INTR (table changed mid-dump) before the
	// whole namespace is treated as a failed dump for this pass.
	localityDumpRetriesCst = 3
)

// localityRetryState is one namespace's negative-cache entry: when it may next
// be dumped and the backoff that produced that time (0 = "next pass").
type localityRetryState struct {
	nextRetry time.Time
	backoff   time.Duration
}

// nextLocalityRetry computes the retry state after one unsuccessful dump.
// retrying says whether prev is a live entry (vs. the zero value for a
// namespace with no history); hard distinguishes a failed dump from a
// loopback-only snapshot, which is a softer signal (the namespace is fine, the
// veth just is not plumbed yet) and therefore gets one immediate re-dump before
// backing off.
func nextLocalityRetry(prev localityRetryState, retrying bool, now time.Time, hard bool) localityRetryState {
	var b time.Duration
	switch {
	case !retrying && !hard:
		b = 0 // first loopback-only sighting: again on the very next pass
	case !retrying || prev.backoff == 0:
		b = localityRetryMinCst
	default:
		b = prev.backoff * 2
		if b > localityRetryMaxCst {
			b = localityRetryMaxCst
		}
	}
	return localityRetryState{nextRetry: now.Add(b), backoff: b}
}

// localityIndex is the locality enricher: it owns the per-namespace snapshot
// map, the negative cache that throttles re-dumping a namespace that keeps
// failing, and the refresh clock. All of this used to live on the XTCP struct;
// it moved here so that *localnet.Snapshot — and therefore gaissmai/bart —
// appears nowhere an untagged build can see it, which is what lets the linker
// drop pkg/localnet entirely. XTCP holds only the localityEnricher interface.
//
// The x back-pointer is for the shared daemon state the discovery path needs:
// the Prometheus vectors, debugLevel and the config's refresh interval.
type localityIndex struct {
	x *XTCP

	// byInode maps a socket's netns inode -> that namespace's local
	// address/route snapshot. Published atomically by Refresh, read lock-free
	// on the stamping path.
	byInode atomic.Pointer[map[uint64]*localnet.Snapshot]

	// lastRefresh throttles the full re-dump to locality_refresh_interval;
	// retry is the per-namespace negative cache. Both are touched only from
	// Refresh, which runs on the single-owner reconcile path under
	// reconcileMu, so neither needs a lock.
	lastRefresh time.Time
	retry       map[uint64]localityRetryState

	// dumper / clock are test seams: nil means nsLocalitySnapshot (setns +
	// rtnetlink dumps) and time.Now.
	dumper func(nsIdentity) (*localnet.Snapshot, bool)
	clock  func() time.Time
}

func newLocalityIndex(x *XTCP) *localityIndex {
	return &localityIndex{x: x, retry: make(map[uint64]localityRetryState)}
}

// initLocalityEnricher installs the locality index. The actual per-namespace
// discovery is driven by the single-owner reconcile path (refreshLocality,
// called from discoverNamespaces), so there is nothing to load or spawn here —
// this just publishes intent and bumps a counter, mirroring the other
// initEnrichers gates.
func (x *XTCP) initLocalityEnricher() {
	if x.config == nil || !x.config.EnrichLocalityEnable {
		return
	}
	x.locality = newLocalityIndex(x)
	x.pC.WithLabelValues("initEnrichers", "locality", "enabled").Inc()
	if x.debugLevel > 10 {
		log.Printf("initLocalityEnricher: locality enrichment enabled (refresh:%s); per-namespace discovery runs on the reconcile path",
			x.config.GetLocalityRefreshInterval().AsDuration())
	}
}

// Active implements localityEnricher: true once any pass has published a map,
// which is what keeps the stamping path a true no-op before the first
// reconcile.
func (l *localityIndex) Active() bool {
	return l.byInode.Load() != nil
}

// Resolve implements localityEnricher, flattening localnet.Resolution into the
// untagged localityResult so that type stays inside this build.
func (l *localityIndex) Resolve(inode uint64, dst netip.Addr, boundIfindex uint32) (localityResult, bool) {
	m := l.byInode.Load()
	if m == nil {
		return localityResult{}, false
	}
	snap := (*m)[inode]
	if snap == nil {
		return localityResult{}, false
	}
	res := snap.Resolve(dst, boundIfindex)
	return localityResult{
		Locality:      xtcp_flat_record.XtcpFlatRecord_Locality(res.Locality),
		EgressIfindex: res.EgressIfindex,
		EgressIfname:  res.EgressIfname,
		BoundIfname:   res.BoundIfname,
		Remote:        res.Remote,
	}, true
}

// now is the reconcile clock (time.Now unless a test injected one).
func (l *localityIndex) now() time.Time {
	if l.clock != nil {
		return l.clock()
	}
	return time.Now()
}

// dump performs one namespace's discovery (nsLocalitySnapshot unless a test
// injected a dumper).
func (l *localityIndex) dump(id nsIdentity) (*localnet.Snapshot, bool) {
	if l.dumper != nil {
		return l.dumper(id)
	}
	return l.x.nsLocalitySnapshot(id)
}

// Refresh rebuilds the netns-inode -> locality snapshot for the current
// namespace set and publishes it atomically for the stamping path. It is called
// only from the single-owner reconcile path (discoverNamespaces) under
// reconcileMu, so lastRefresh and retry need no additional lock.
//
// Which namespaces get dumped on a pass:
//   - a FULL pass (first ever, or locality_refresh_interval elapsed) re-dumps
//     every namespace that is not sitting in a retry backoff, uncapped;
//   - any other pass dumps only namespaces without a snapshot (new since last
//     pass) and namespaces whose retry window has opened, at most
//     localityNewNsPerPassCst of them — the rest are deferred to the next
//     reconcile (they keep any previous snapshot meanwhile).
//
// What happens to a dump's outcome:
//   - success with a non-loopback self address: published, retry state cleared;
//   - success but loopback-only (container whose veth is not plumbed yet): the
//     snapshot IS published (it classifies loopback correctly) and the
//     namespace is re-dumped on the next pass, then on the 30s→5m schedule;
//   - failure (open/setns/dump error): the previous snapshot is kept, and the
//     namespace is retried on the 30s→5m schedule instead of every reconcile.
//
// Retry state for namespaces that vanished is dropped. interval <= 0 means
// there is never another full pass: each namespace is discovered once (plus
// its retries) and never refreshed.
func (l *localityIndex) Refresh(nss map[uint64]nsIdentity) {
	x := l.x
	start := l.now()
	interval := x.config.GetLocalityRefreshInterval().AsDuration()
	full := l.lastRefresh.IsZero() || (interval > 0 && start.Sub(l.lastRefresh) >= interval)

	var cur map[uint64]*localnet.Snapshot
	if p := l.byInode.Load(); p != nil {
		cur = *p
	}
	if l.retry == nil {
		l.retry = make(map[uint64]localityRetryState)
	}

	budget := localityNewNsPerPassCst
	if full {
		budget = -1 // uncapped
	}

	var dumped, failed, loOnly, deferred, reused int
	m := make(map[uint64]*localnet.Snapshot, len(nss))
	for inode, id := range nss {
		prev, had := cur[inode]
		retry, retrying := l.retry[inode]

		need := full || !had
		if retrying {
			// Negative-cached: dump only once its window has opened, even on a
			// full pass — a permanently failing namespace must not be hammered
			// every refresh interval.
			need = !start.Before(retry.nextRetry)
		}
		if !need {
			if had {
				m[inode] = prev
				reused++
			}
			continue
		}
		if budget == 0 {
			deferred++
			if had {
				m[inode] = prev
			}
			continue
		}
		if budget > 0 {
			budget--
		}

		snap, ok := l.dump(id)
		dumped++
		switch {
		case !ok:
			failed++
			l.retry[inode] = nextLocalityRetry(retry, retrying, start, true)
			if had {
				m[inode] = prev // keep the last good snapshot on a discovery failure
			}
		case !snap.HasNonLoopbackSelf():
			loOnly++
			m[inode] = snap
			l.retry[inode] = nextLocalityRetry(retry, retrying, start, false)
		default:
			m[inode] = snap
			delete(l.retry, inode)
		}
	}

	for inode := range l.retry {
		if _, present := nss[inode]; !present {
			delete(l.retry, inode)
		}
	}

	l.byInode.Store(&m)
	if full {
		l.lastRefresh = start
	}

	passType := "partial"
	if full {
		passType = "full"
	}
	x.pC.WithLabelValues("refreshLocality", passType, "count").Inc()
	x.pC.WithLabelValues("refreshLocality", "dumped", "count").Add(float64(dumped))
	x.pC.WithLabelValues("refreshLocality", "failed", "count").Add(float64(failed))
	x.pC.WithLabelValues("refreshLocality", "loopbackOnly", "count").Add(float64(loOnly))
	x.pC.WithLabelValues("refreshLocality", "deferred", "count").Add(float64(deferred))
	x.pGV.WithLabelValues("refreshLocality", "namespaces", "gauge").Set(float64(len(m)))
	x.pGV.WithLabelValues("refreshLocality", "retryBackoff", "gauge").Set(float64(len(l.retry)))
	x.pH.WithLabelValues("refreshLocality", passType, "duration").Observe(l.now().Sub(start).Seconds())

	if x.debugLevel > 10 {
		log.Printf("refreshLocality: %s pass namespaces:%d dumped:%d reused:%d failed:%d loopbackOnly:%d deferred:%d inBackoff:%d took:%s",
			passType, len(m), dumped, reused, failed, loOnly, deferred, len(l.retry), l.now().Sub(start))
	}
}

// nsLocalitySnapshot enters the namespace referenced by id, dumps its links,
// addresses and routes via rtnetlink, and returns the built snapshot. It runs on
// a dedicated OS thread that it deliberately never unlocks: after setns the
// thread is netns-tainted, so on return the Go runtime terminates it instead of
// recycling it — the same safety property netNamespaceInstance relies on to
// avoid the tainted-M thread-exhaustion regression. These dumps are infrequent
// (throttled by locality_refresh_interval and the retry backoff), so the
// per-call thread teardown is cheap. Best-effort: any error yields (nil, false).
func (x *XTCP) nsLocalitySnapshot(id nsIdentity) (*localnet.Snapshot, bool) {
	handle := id.path
	if handle == "" {
		handle = procNsPath(id.pid)
	}

	type result struct {
		snap *localnet.Snapshot
		ok   bool
	}
	ch := make(chan result, 1)

	go func() {
		runtime.LockOSThread() //nolint:forbidigo // intentional: thread is netns-tainted after setns; goroutine returns without UnlockOSThread so the runtime terminates it (no tainted-M reuse).

		fd, err := unix.Open(handle, unix.O_RDONLY|unix.O_CLOEXEC, 0)
		if err != nil {
			x.pC.WithLabelValues("refreshLocality", "open", "error").Inc()
			ch <- result{}
			return
		}
		defer func() {
			if cerr := unix.Close(fd); cerr != nil {
				x.pC.WithLabelValues("refreshLocality", "closeHandle", "error").Inc()
			}
		}()

		if err := unix.Setns(fd, unix.CLONE_NEWNET); err != nil {
			x.pC.WithLabelValues("refreshLocality", "setns", "error").Inc()
			ch <- result{}
			return
		}

		snap, err := x.dumpLocalityInNs()
		if err != nil {
			x.pC.WithLabelValues("refreshLocality", "dump", "error").Inc()
			ch <- result{}
			return
		}
		ch <- result{snap: snap, ok: true}
	}()

	r := <-ch
	return r.snap, r.ok
}

// dumpLocalityInNs opens a NETLINK_ROUTE socket in the caller's current network
// namespace and dumps its links (RTM_GETLINK), addresses (RTM_GETADDR) and
// routes (RTM_GETROUTE), building a locality Snapshot. The socket is pinned to
// the namespace it is created in, so this must be called while the OS thread is
// in the target namespace (see nsLocalitySnapshot). AF_UNSPEC dumps both IPv4
// and IPv6, and the route dump returns all tables (main + local), so RTN_LOCAL
// entries are included.
func (x *XTCP) dumpLocalityInNs() (*localnet.Snapshot, error) {
	fd, err := unix.Socket(unix.AF_NETLINK, unix.SOCK_RAW|unix.SOCK_CLOEXEC, unix.NETLINK_ROUTE)
	if err != nil {
		return nil, err
	}
	defer func() {
		if cerr := unix.Close(fd); cerr != nil {
			x.pC.WithLabelValues("dumpLocality", "closeSocket", "error").Inc()
		}
	}()

	sa := &unix.SockaddrNetlink{Family: unix.AF_NETLINK}
	if err := unix.Bind(fd, sa); err != nil {
		return nil, err
	}
	tv := localityRecvTimeout
	if err := unix.SetsockoptTimeval(fd, unix.SOL_SOCKET, unix.SO_RCVTIMEO, &tv); err != nil {
		return nil, err
	}

	var seq uint32

	// Links: index -> name, used by the snapshot to resolve a route's egress Oif
	// and a socket's kernel idiag_if to a human interface name.
	var links map[uint32]string
	if err := x.dumpRetrying(fd, sa, &seq, xtcpnl.BuildDumpLinkRequest,
		func() { links = make(map[uint32]string) },
		func(mt uint16, body []byte) error {
			if mt == uint16(unix.RTM_NEWLINK) {
				li, perr := xtcpnl.ParseNewLink(body)
				if perr != nil {
					return perr
				}
				links[uint32(li.Index)] = li.Name
			}
			return nil
		}); err != nil {
		return nil, err
	}

	// Addresses (both families).
	var addrs []xtcpnl.AddrInfo
	if err := x.dumpRetrying(fd, sa, &seq, func(seq uint32) []byte { return xtcpnl.BuildDumpAddrRequest(unix.AF_UNSPEC, seq) },
		func() { addrs = addrs[:0] },
		func(mt uint16, body []byte) error {
			if mt == uint16(unix.RTM_NEWADDR) {
				ai, perr := xtcpnl.ParseNewAddr(body)
				if perr != nil {
					return perr
				}
				addrs = append(addrs, ai)
			}
			return nil
		}); err != nil {
		return nil, err
	}

	// Routes (both families, all tables).
	var routes []xtcpnl.RouteInfo
	if err := x.dumpRetrying(fd, sa, &seq, func(seq uint32) []byte { return xtcpnl.BuildDumpRouteRequest(unix.AF_UNSPEC, seq) },
		func() { routes = routes[:0] },
		func(mt uint16, body []byte) error {
			if mt == uint16(unix.RTM_NEWROUTE) {
				ri, perr := xtcpnl.ParseNewRoute(body)
				if perr != nil {
					return perr
				}
				routes = append(routes, ri)
			}
			return nil
		}); err != nil {
		return nil, err
	}

	if x.debugLevel > 10 {
		log.Printf("dumpLocalityInNs: links:%d addrs:%d routes:%d", len(links), len(addrs), len(routes))
	}
	return localnet.BuildSnapshot(addrs, routes, links), nil
}

// dumpRetrying runs one rtnetlink dump on fd, re-issuing it with a fresh
// sequence number when the kernel reports NLM_F_DUMP_INTR (the table changed
// while being dumped, so the reply may be inconsistent). reset clears the
// caller's accumulator before every attempt so a partial, interrupted stream is
// never merged with the retry. Any other error, or exhausting
// localityDumpRetriesCst, is returned to the caller. seq is advanced once per
// attempt; DumpRtnetlink filters replies by it, so a slow reply to an earlier
// attempt cannot pollute a later one.
func (x *XTCP) dumpRetrying(fd int, sa *unix.SockaddrNetlink, seq *uint32,
	build func(seq uint32) []byte, reset func(), onMsg func(msgType uint16, body []byte) error) error {
	for attempt := 0; ; attempt++ {
		reset()
		*seq++
		err := xtcpnl.DumpRtnetlink(fd, build(*seq), sa, onMsg)
		if !errors.Is(err, xtcpnl.ErrDumpInterrupted) || attempt >= localityDumpRetriesCst {
			return err
		}
		x.pC.WithLabelValues("dumpLocality", "interrupted", "retry").Inc()
	}
}
