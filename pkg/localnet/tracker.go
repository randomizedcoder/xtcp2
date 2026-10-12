package localnet

import (
	"bytes"
	"cmp"
	"maps"
	"slices"

	"golang.org/x/sys/unix"

	"github.com/randomizedcoder/xtcp2/pkg/xtcpnl"
)

// Tracker folds a namespace's rtnetlink notifications into the links,
// addresses and routes BuildSnapshot reads, so a listener (pkg/rtnlmon) can
// keep a Snapshot current without re-dumping. Reset replaces the state with a
// dump; Apply folds one notification. A Tracker is not safe for concurrent
// use, and it does not import rtnlmon: it takes the dump's slices.
//
// The fold is the kernel's identity for each object, so a notification that
// repeats or refreshes one (an IPv6 address's lifetime update, a NEWLINK that
// only changes counters) changes nothing:
//   - an address is (ifindex, family, IFA_LOCAL), plus the prefix length and
//     IFA_ADDRESS for IPv4, whose peer addresses are separate entries;
//   - a route is (family, table, dst, src, tos, priority), plus the egress
//     interface and gateway for IPv6, which keeps one route per device for the
//     same prefix and metric (fe80::/64 on every link).
//
// An IPv4 `ip route replace` that moves a route keeps one entry; an IPv6 one,
// or an IPv4 `ip route prepend`, can leave the old route in the fold until the
// next Reset, because the notification does not say what it replaced.
type Tracker struct {
	links  map[uint32]trackedLink
	addrs  map[addrKey]xtcpnl.AddrInfo
	routes map[routeKey]xtcpnl.RouteInfo
}

type trackedLink struct {
	name string
	up   bool
}

type addrKey struct {
	index     uint32
	family    uint8
	prefixlen uint8
	local     string
	address   string
}

type routeKey struct {
	family, dstLen, srcLen, tos uint8
	table, priority, oif        uint32
	dst, src, gateway           string
}

// NewTracker returns an empty Tracker; its Snapshot classifies everything but
// loopback as remote.
func NewTracker() *Tracker {
	t := &Tracker{}
	t.Reset(nil, nil, nil)
	return t
}

// Reset replaces the state with one namespace's dumps.
func (t *Tracker) Reset(links []xtcpnl.LinkInfo, addrs []xtcpnl.AddrInfo, routes []xtcpnl.RouteInfo) {
	t.links = make(map[uint32]trackedLink, len(links))
	t.addrs = make(map[addrKey]xtcpnl.AddrInfo, len(addrs))
	t.routes = make(map[routeKey]xtcpnl.RouteInfo, len(routes))
	for i := range links {
		t.links[uint32(links[i].Index)] = trackedLink{name: links[i].Name, up: links[i].Flags&unix.IFF_UP != 0}
	}
	for i := range addrs {
		t.addrs[keyOfAddr(&addrs[i])] = addrs[i]
	}
	for i := range routes {
		if tracked(&routes[i]) {
			t.routes[keyOfRoute(&routes[i])] = routes[i]
		}
	}
}

// Apply folds one xtcpnl.LinkEvent, AddrEvent or RouteEvent, and reports
// whether anything a Snapshot reads changed. Any other event is ignored.
func (t *Tracker) Apply(ev any) bool {
	switch e := ev.(type) {
	case xtcpnl.LinkEvent:
		return t.applyLink(&e)
	case xtcpnl.AddrEvent:
		k := keyOfAddr(&e.Addr)
		if e.Action == xtcpnl.EventActionDel {
			return deleteKey(t.addrs, k)
		}
		// The key holds everything BuildSnapshot reads of an address, so
		// only a new one changes the Snapshot.
		_, ok := t.addrs[k]
		t.addrs[k] = e.Addr
		return !ok
	case xtcpnl.RouteEvent:
		if !tracked(&e.Route) {
			return false
		}
		k := keyOfRoute(&e.Route)
		if e.Action == xtcpnl.EventActionDel {
			return deleteKey(t.routes, k)
		}
		old, ok := t.routes[k]
		t.routes[k] = e.Route
		return !ok || !sameClass(&old, &e.Route)
	}
	return false
}

// applyLink records a link's name and state. A link going down, or away,
// takes routes with it that the kernel never reports deleting: a device going
// down marks its IPv4 nexthops dead, except host-scope ones
// (fib_sync_down_dev, net/ipv4/fib_semantics.c:1970; the local table's
// own-address routes are RT_SCOPE_NOWHERE nexthops, :1522), and
// fib_table_flush then frees the dead routes, sending RTM_DELROUTE only when
// the route's preferred source was removed (net/ipv4/fib_trie.c:2059). An
// unregister kills every nexthop on the device (fib_disable_ip(dev, event,
// true), net/ipv4/fib_frontend.c:1517). IPv6 notifies its deletions, so only
// IPv4 routes are flushed on a down; on a delete nothing can still use the
// device. Routes with several nexthops, or a nexthop object, survive one
// device: the next Reset corrects them.
func (t *Tracker) applyLink(e *xtcpnl.LinkEvent) bool {
	idx := uint32(e.Link.Index)
	old, known := t.links[idx]
	changed := false
	if e.Action == xtcpnl.EventActionDel {
		delete(t.links, idx)
		changed = known && old.name != ""
		for k := range t.addrs {
			if t.addrs[k].Index == idx {
				delete(t.addrs, k)
				changed = true
			}
		}
		return t.flushRoutes(idx, true) || changed
	}
	up := e.Link.Flags&unix.IFF_UP != 0
	t.links[idx] = trackedLink{name: e.Link.Name, up: up}
	changed = !known || old.name != e.Link.Name
	if known && old.up && !up {
		changed = t.flushRoutes(idx, false) || changed
	}
	return changed
}

// flushRoutes drops the single-path routes through idx: IPv4 ones that are
// not host scope, or, when unregistered, every one.
func (t *Tracker) flushRoutes(idx uint32, unregistered bool) bool {
	changed := false
	for k := range t.routes {
		r := t.routes[k]
		if r.Oif != idx || r.HasMultipath || r.NhID != 0 {
			continue
		}
		if !unregistered && (r.Family != unix.AF_INET || r.Scope == unix.RT_SCOPE_HOST) {
			continue
		}
		delete(t.routes, k)
		changed = true
	}
	return changed
}

// Snapshot builds a Snapshot from the folded state. Routes are handed to
// BuildSnapshot in a fixed order (by family, table, prefix, then tos and
// priority, as the kernel dumps a prefix's routes), so two routes for one
// prefix resolve the same way every time.
func (t *Tracker) Snapshot() *Snapshot {
	links := make(map[uint32]string, len(t.links))
	for idx, l := range t.links {
		if l.name != "" {
			links[idx] = l.name
		}
	}
	addrs := slices.Collect(maps.Values(t.addrs))
	routes := slices.SortedFunc(maps.Values(t.routes), compareRoutes)
	return BuildSnapshot(addrs, routes, links)
}

// tracked reports whether a route is one BuildSnapshot can read: main or
// local table, and not a cached clone.
func tracked(r *xtcpnl.RouteInfo) bool {
	return inClassifiedTable(r.Table) && r.Flags&unix.RTM_F_CLONED == 0
}

func keyOfAddr(a *xtcpnl.AddrInfo) addrKey {
	k := addrKey{index: a.Index, family: a.Family, local: string(a.Local)}
	if a.Family == unix.AF_INET {
		k.prefixlen, k.address = a.Prefixlen, string(a.Address)
	}
	return k
}

func keyOfRoute(r *xtcpnl.RouteInfo) routeKey {
	k := routeKey{
		family: r.Family, dstLen: r.DstLen, srcLen: r.SrcLen, tos: r.Tos,
		table: r.Table, priority: r.Priority, dst: string(r.Dst), src: string(r.Src),
	}
	if r.Family == unix.AF_INET6 {
		k.oif, k.gateway = r.Oif, string(r.Gateway)
	}
	return k
}

// sameClass reports whether two routes with one key classify alike: the
// fields BuildSnapshot reads.
func sameClass(a, b *xtcpnl.RouteInfo) bool {
	return a.Type == b.Type && a.Oif == b.Oif && bytes.Equal(a.Gateway, b.Gateway) &&
		a.HasVia == b.HasVia && a.HasMultipath == b.HasMultipath && a.NhID == b.NhID
}

func compareRoutes(a, b xtcpnl.RouteInfo) int {
	return cmp.Or(
		cmp.Compare(a.Family, b.Family),
		cmp.Compare(a.Table, b.Table),
		cmp.Compare(a.DstLen, b.DstLen),
		bytes.Compare(a.Dst, b.Dst),
		cmp.Compare(a.Tos, b.Tos),
		cmp.Compare(a.Priority, b.Priority),
		cmp.Compare(a.Oif, b.Oif),
		bytes.Compare(a.Gateway, b.Gateway),
	)
}

func deleteKey[K comparable, V any](m map[K]V, k K) bool {
	if _, ok := m[k]; !ok {
		return false
	}
	delete(m, k)
	return true
}
