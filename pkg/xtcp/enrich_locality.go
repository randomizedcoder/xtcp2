package xtcp

import (
	"log"
	"runtime"
	"time"

	"golang.org/x/sys/unix"

	"github.com/randomizedcoder/xtcp2/pkg/localnet"
	"github.com/randomizedcoder/xtcp2/pkg/xtcpnl"
)

// localityRecvTimeout bounds each rtnetlink dump's recv so a missing NLMSG_DONE
// degrades to a discovery error (that namespace stays unclassified) instead of
// blocking the reconcile owner.
var localityRecvTimeout = unix.Timeval{Sec: 2}

// initLocalityEnricher records that locality classification is enabled. The
// actual per-namespace discovery is driven by the single-owner reconcile path
// (refreshLocality, called from discoverNamespaces), so there is nothing to
// load or spawn here — this just logs intent and bumps a counter, mirroring the
// other initEnrichers gates.
func (x *XTCP) initLocalityEnricher() {
	if !x.config.EnrichLocalityEnable {
		return
	}
	x.pC.WithLabelValues("initEnrichers", "locality", "enabled").Inc()
	if x.debugLevel > 10 {
		log.Printf("initLocalityEnricher: locality enrichment enabled (refresh:%s); per-namespace discovery runs on the reconcile path",
			x.config.GetLocalityRefreshInterval().AsDuration())
	}
}

// refreshLocality rebuilds the netns-inode -> locality snapshot for the current
// namespace set and publishes it atomically for the stamping path. It is called
// only from the single-owner reconcile path (discoverNamespaces) under
// reconcileMu, so lastLocalityRefresh needs no additional lock.
//
// It is throttled by locality_refresh_interval: a "full" pass re-discovers every
// namespace, while intervening passes only discover namespaces that appeared
// since the last snapshot (so a new container is classified promptly without
// re-dumping every existing namespace every reconcile). A namespace whose
// discovery fails keeps its previous snapshot rather than dropping to
// unclassified. interval <= 0 means discover each namespace once and never
// refresh it (new namespaces are still picked up).
func (x *XTCP) refreshLocality(nss map[uint64]nsIdentity) {
	now := time.Now()
	interval := x.config.GetLocalityRefreshInterval().AsDuration()
	full := x.lastLocalityRefresh.IsZero() || (interval > 0 && now.Sub(x.lastLocalityRefresh) >= interval)

	var cur map[uint64]*localnet.Snapshot
	if p := x.localityByInode.Load(); p != nil {
		cur = *p
	}

	m := make(map[uint64]*localnet.Snapshot, len(nss))
	for inode, id := range nss {
		if !full {
			if snap, ok := cur[inode]; ok {
				m[inode] = snap // reuse; between full passes only new namespaces are dumped
				continue
			}
		}
		if snap, ok := x.nsLocalitySnapshot(id); ok {
			m[inode] = snap
		} else if snap, had := cur[inode]; had {
			m[inode] = snap // keep the last good snapshot on a discovery failure
		}
	}

	x.localityByInode.Store(&m)
	if full {
		x.lastLocalityRefresh = now
	}
	x.pC.WithLabelValues("refreshLocality", "namespaces", "counter").Add(float64(len(m)))
}

// nsLocalitySnapshot enters the namespace referenced by id, dumps its links,
// addresses and routes via rtnetlink, and returns the built snapshot. It runs on
// a dedicated OS thread that it deliberately never unlocks: after setns the
// thread is netns-tainted, so on return the Go runtime terminates it instead of
// recycling it — the same safety property netNamespaceInstance relies on to
// avoid the tainted-M thread-exhaustion regression. These dumps are infrequent
// (throttled by locality_refresh_interval), so the per-call thread teardown is
// cheap. Best-effort: any error yields (nil, false).
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

	// Links: traverse for a per-namespace link count (diagnostics); the
	// classification itself needs only addresses + routes.
	links := make(map[int32]string)
	seq++
	if err := xtcpnl.DumpRtnetlink(fd, xtcpnl.BuildDumpLinkRequest(seq), sa, func(mt uint16, body []byte) error {
		if mt == uint16(unix.RTM_NEWLINK) {
			li, perr := xtcpnl.ParseNewLink(body)
			if perr != nil {
				return perr
			}
			links[li.Index] = li.Name
		}
		return nil
	}); err != nil {
		return nil, err
	}

	// Addresses (both families).
	var addrs []xtcpnl.AddrInfo
	seq++
	if err := xtcpnl.DumpRtnetlink(fd, xtcpnl.BuildDumpAddrRequest(unix.AF_UNSPEC, seq), sa, func(mt uint16, body []byte) error {
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
	seq++
	if err := xtcpnl.DumpRtnetlink(fd, xtcpnl.BuildDumpRouteRequest(unix.AF_UNSPEC, seq), sa, func(mt uint16, body []byte) error {
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
	return localnet.BuildSnapshot(addrs, routes), nil
}
