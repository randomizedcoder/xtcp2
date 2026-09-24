// Package localnet classifies a socket endpoint IP as belonging to the local
// host (self), a directly-connected local subnet, or somewhere remote, using the
// local addresses and routing table of a specific network namespace.
//
// It is the consumer side of the rtnetlink discovery machinery in pkg/xtcpnl:
// xtcp2 dumps each monitored namespace's RTM_GETADDR + RTM_GETROUTE (and
// RTM_GETLINK) replies, feeds the parsed AddrInfo/RouteInfo into BuildSnapshot,
// and publishes the immutable Snapshot atomically. On xtcp2's per-socket
// enrichment hot path a destination address is classified with Resolve before
// the internet IP->ASN lookup: self and local-subnet destinations never reach
// the ASN feed.
//
// A Snapshot is built once (off the hot path) and never mutated; Classify is a
// pure, allocation-free, lock-free read, mirroring pkg/ipasn's contract.
package localnet

import (
	"net/netip"

	"github.com/gaissmai/bart"
	"golang.org/x/sys/unix"

	"github.com/randomizedcoder/xtcp2/pkg/xtcpnl"
)

// Locality is how a socket endpoint relates to a namespace's local network. The
// values match the xtcp_flat_record Locality enum so a Locality can be stored
// directly as the record field.
type Locality uint8

const (
	// LocalityUnspecified means the endpoint could not be classified (e.g. an
	// unparseable address, or no snapshot yet).
	LocalityUnspecified Locality = 0
	// LocalitySelf means the endpoint is one of this namespace's own addresses
	// (or loopback) — traffic that terminates on this host.
	LocalitySelf Locality = 1
	// LocalitySubnet means the endpoint is on a directly-connected subnet (a
	// scope-link route with no gateway) — one L2 hop away, no routing.
	LocalitySubnet Locality = 2
	// LocalityRemote means the endpoint is reached via a gateway — the only
	// class that should fall through to the IP->ASN lookup.
	LocalityRemote Locality = 3
)

// String renders the Locality for logs/columns.
func (l Locality) String() string {
	switch l {
	case LocalitySelf:
		return "self"
	case LocalitySubnet:
		return "local_subnet"
	case LocalityRemote:
		return "remote"
	default:
		return "unspecified"
	}
}

// routeEntry is the value stored per prefix in the trie: how a destination in
// that prefix is classified, plus the egress interface index of the route it
// came from (0 when unknown, e.g. for self addresses without an owning route).
type routeEntry struct {
	loc Locality
	oif uint32 // egress interface index (route Oif / address ifindex)
}

// Snapshot is an immutable per-namespace view of local addresses, connected
// subnets and the routing table. Self addresses are stored as host prefixes
// (/32, /128), connected subnets as their network prefix, and gateway routes
// (including the default route) as their prefix — all in a single
// longest-prefix-match trie keyed to a routeEntry, so a self host address wins
// over its containing subnet and the most-specific route supplies the egress
// interface in one Lookup. ifnames resolves an interface index (the route's Oif
// or a socket's kernel idiag_if) to a human name from the RTM_GETLINK dump. The
// zero value classifies everything as remote; build with BuildSnapshot.
type Snapshot struct {
	tbl     *bart.Table[routeEntry]
	ifnames map[uint32]string
	// nonLoopbackSelf records that at least one self entry (an interface
	// address or an RTN_LOCAL route) lies outside 127.0.0.0/8 and ::1. A
	// namespace with only loopback is usually one whose veth has not been
	// plumbed yet (container start-up race), so the daemon re-dumps it on the
	// next reconcile instead of waiting a full refresh interval.
	nonLoopbackSelf bool
}

// HasNonLoopbackSelf reports whether the snapshot saw any self address beyond
// loopback. false means the namespace looked freshly created (lo only) at dump
// time and is worth re-dumping soon. The zero and nil Snapshot report false.
func (s *Snapshot) HasNonLoopbackSelf() bool {
	return s != nil && s.nonLoopbackSelf
}

// Classify returns how addr relates to this snapshot's namespace. Loopback
// short-circuits to self; the unspecified address (no destination, e.g. a LISTEN
// socket's peer) and an invalid address are LocalityUnspecified; a valid address
// that matches a connected subnet is LocalitySubnet; anything else is
// LocalityRemote. Pure and allocation-free.
func (s *Snapshot) Classify(addr netip.Addr) Locality {
	loc, _, _ := s.Lookup(addr)
	return loc
}

// Lookup is the richer form of Classify used on the enrichment hot path: it
// returns the destination's Locality, the egress interface index of the route it
// longest-prefix matches (0 when unknown), and ok=false only for an invalid
// address. The unspecified address (0.0.0.0 / ::) is LocalityUnspecified with no
// egress: it is not a destination. Loopback is always self; its egress is the
// loopback route's Oif when the snapshot has a self entry covering it (the
// kernel's `local 127.0.0.0/8 dev lo` / `local ::1 dev lo`), and 0 otherwise —
// never the egress of a covering gateway/default route, which loopback traffic
// does not use. Pure and allocation-free.
func (s *Snapshot) Lookup(addr netip.Addr) (loc Locality, egressIfindex uint32, ok bool) {
	a := addr.Unmap()
	if !a.IsValid() {
		return LocalityUnspecified, 0, false
	}
	if a.IsUnspecified() {
		return LocalityUnspecified, 0, true
	}
	loopback := a.IsLoopback()
	if s != nil && s.tbl != nil {
		if e, found := s.tbl.Lookup(a); found {
			if loopback {
				if e.loc != LocalitySelf {
					return LocalitySelf, 0, true
				}
				return LocalitySelf, e.oif, true
			}
			return e.loc, e.oif, true
		}
	}
	if loopback {
		return LocalitySelf, 0, true
	}
	return LocalityRemote, 0, true
}

// IfName resolves an interface index (a route's egress Oif or a socket's kernel
// idiag_if) to its name from this namespace's RTM_GETLINK dump, or "" when the
// index is 0 or unknown. Pure and allocation-free.
func (s *Snapshot) IfName(index uint32) string {
	if s == nil || index == 0 {
		return ""
	}
	return s.ifnames[index]
}

// Resolution is the alloc-free result of classifying one destination against a
// snapshot: the destination's Locality, its egress interface (the matched route's
// Oif, as index and resolved name), the resolved name of the socket's own bound
// interface (the kernel idiag_if), and whether the destination is remote (the
// only class that should fall through to the internet IP->ASN lookup).
type Resolution struct {
	Locality      Locality
	EgressIfindex uint32
	EgressIfname  string
	BoundIfname   string
	// Remote is false for self, local-subnet and unspecified destinations (skip
	// the ASN feed) and true for LocalityRemote — including the no-match case,
	// where a non-self destination falls through to LocalityRemote and the ASN
	// lookup runs exactly as before locality enrichment existed.
	Remote bool
}

// Resolve folds the enrichment hot path's per-socket interface/locality work into
// one call: it looks up dst's locality + egress interface, resolves both the
// egress Oif and the socket's bound interface index (the kernel idiag_if) to
// names, and reports whether the destination is remote. A no-match destination
// resolves to LocalityRemote with empty names, matching the pre-locality path.
// Pure and allocation-free.
func (s *Snapshot) Resolve(dst netip.Addr, boundIfindex uint32) Resolution {
	loc, egressIf, _ := s.Lookup(dst)
	return Resolution{
		Locality:      loc,
		EgressIfindex: egressIf,
		EgressIfname:  s.IfName(egressIf),
		BoundIfname:   s.IfName(boundIfindex),
		Remote:        loc == LocalityRemote,
	}
}

// BuildSnapshot constructs a Snapshot from one namespace's parsed RTM_GETADDR,
// RTM_GETROUTE and RTM_GETLINK replies. Only routes in the main and local
// tables are considered (see inClassifiedTable). Self set = every interface
// address (IFA_LOCAL, falling back to IFA_ADDRESS) plus every RTN_LOCAL route
// prefix (host routes, and ranges such as `local 127.0.0.0/8 dev lo`). Connected
// subnets = unicast routes that carry a destination prefix and are delivered
// on-link — no RTA_GATEWAY, RTA_VIA, RTA_MULTIPATH or RTA_NH_ID (scope is NOT
// part of the test — IPv4 connected subnets are scope-link but IPv6 connected
// subnets are scope-universe). Every other unicast route (gateway, default,
// multipath, nexthop-object) is recorded as LocalityRemote so the longest-prefix
// match still yields its egress interface where one is known. A self entry is
// never overwritten by a same-prefix route, whatever order the dumps arrive in.
// links maps an interface index to its name (RTM_GETLINK) for resolving the
// egress Oif and a socket's kernel idiag_if. It is pure: no syscalls, safe to
// feed test fixtures.
func BuildSnapshot(addrs []xtcpnl.AddrInfo, routes []xtcpnl.RouteInfo, links map[uint32]string) *Snapshot {
	tbl := new(bart.Table[routeEntry])

	ifnames := make(map[uint32]string, len(links))
	for idx, name := range links {
		ifnames[idx] = name
	}

	nonLoopbackSelf := false
	for _, ai := range addrs {
		raw := ai.Local
		if len(raw) == 0 {
			raw = ai.Address
		}
		if a, ok := addrFromBytes(raw); ok {
			tbl.Insert(hostPrefix(a), routeEntry{loc: LocalitySelf, oif: ai.Index})
			if !a.IsLoopback() {
				nonLoopbackSelf = true
			}
		}
	}

	for _, ri := range routes {
		if !inClassifiedTable(ri.Table) {
			continue
		}
		switch ri.Type {
		case unix.RTN_LOCAL:
			// A locally-attached address (RT_TABLE_LOCAL, scope host). Usually a
			// host route, but the kernel also installs `local 127.0.0.0/8 dev lo`:
			// honor the prefix length so the whole range is self.
			if pfx, ok := prefixFromBytes(ri.Dst, ri.DstLen); ok {
				tbl.Insert(pfx, routeEntry{loc: LocalitySelf, oif: ri.Oif})
				if !isLoopbackPrefix(pfx) {
					nonLoopbackSelf = true
				}
			}
		case unix.RTN_UNICAST:
			pfx, ok := routePrefix(ri)
			if !ok {
				continue
			}
			switch {
			case gatewayReached(ri):
				// Reached via a next hop (includes the /0 default route): remote,
				// but keep the egress interface for the LPM when the route names
				// exactly one. A multipath list or a nexthop object has several /
				// opaque egress interfaces, so report none rather than a wrong one.
				oif := ri.Oif
				if ri.HasMultipath || ri.NhID != 0 {
					oif = 0
				}
				insertRoute(tbl, pfx, routeEntry{loc: LocalityRemote, oif: oif})
			case pfx.Bits() == 0:
				// A gatewayless default route (`default dev wg0`: a point-to-point
				// tunnel or PPP link). Everything not matched more specifically
				// leaves via that interface, but it is not a local subnet — a /0
				// must never swallow every address into LocalitySubnet.
				insertRoute(tbl, pfx, routeEntry{loc: LocalityRemote, oif: ri.Oif})
			default:
				// A directly-connected subnet (one L2 hop, no next-hop router).
				// The distinguishing signal is unicast + a destination prefix + no
				// next hop, NOT the route scope: real captures show IPv4 connected
				// subnets carry scope=RT_SCOPE_LINK (253) while IPv6 connected
				// subnets carry scope=RT_SCOPE_UNIVERSE (0), so gating on
				// RT_SCOPE_LINK silently misclassifies every IPv6 on-link subnet
				// as REMOTE.
				insertRoute(tbl, pfx, routeEntry{loc: LocalitySubnet, oif: ri.Oif})
			}
		}
	}

	return &Snapshot{tbl: tbl, ifnames: ifnames, nonLoopbackSelf: nonLoopbackSelf}
}

// isLoopbackPrefix reports whether pfx lies entirely inside the loopback
// range: 127.0.0.0/8 (any prefix at or below it) or ::1/128. Used to decide
// whether a self entry counts as real (non-loopback) connectivity.
func isLoopbackPrefix(pfx netip.Prefix) bool {
	a := pfx.Addr()
	if !a.IsLoopback() {
		return false
	}
	if a.Is4() {
		return pfx.Bits() >= 8 // 127.0.0.0/8 or narrower
	}
	return pfx.Bits() == 128 // ::1/128 only
}

// inClassifiedTable limits classification to the tables every namespace
// consults for ordinary traffic: main (connected subnets, gateways, the default
// route) and local (the kernel's own-address entries). Policy-routing tables
// (VRFs, `ip rule` tables) describe traffic this daemon cannot attribute without
// the rule set, so their routes are ignored rather than guessed at.
func inClassifiedTable(table uint32) bool {
	return table == unix.RT_TABLE_MAIN || table == unix.RT_TABLE_LOCAL
}

// gatewayReached reports whether a unicast route hands packets to a next hop
// rather than delivering on-link: an explicit RTA_GATEWAY, a cross-family
// RTA_VIA gateway, an RTA_MULTIPATH nexthop list, or an RTA_NH_ID nexthop object
// (whose gateways live outside this message). Such a route is never a connected
// subnet even though it carries no RTA_GATEWAY of its own.
func gatewayReached(ri xtcpnl.RouteInfo) bool {
	return len(ri.Gateway) > 0 || ri.HasVia || ri.HasMultipath || ri.NhID != 0
}

// routePrefix returns the destination prefix of a unicast route. The default
// route carries no RTA_DST (empty Dst, DstLen 0) and means the family-wide /0;
// every other unicast route has an explicit prefix. A route with a prefix length
// but no destination bytes is malformed and rejected.
func routePrefix(ri xtcpnl.RouteInfo) (netip.Prefix, bool) {
	switch {
	case len(ri.Dst) > 0:
		return prefixFromBytes(ri.Dst, ri.DstLen)
	case ri.DstLen == 0:
		return defaultPrefix(ri.Family)
	}
	return netip.Prefix{}, false
}

// insertRoute records a route-derived (non-self) entry unless the exact prefix
// is already a self entry: a namespace's own address must stay self whether the
// address dump or a same-prefix unicast route (e.g. `10.0.0.5/32 dev eth0`) was
// seen first. Later routes for the same prefix otherwise overwrite earlier ones.
func insertRoute(tbl *bart.Table[routeEntry], pfx netip.Prefix, e routeEntry) {
	if cur, ok := tbl.Get(pfx); ok && cur.loc == LocalitySelf {
		return
	}
	tbl.Insert(pfx, e)
}

// addrFromBytes converts raw network-order address bytes (4 = IPv4, 16 = IPv6)
// to an unmapped netip.Addr. Any other length is rejected.
func addrFromBytes(b []byte) (netip.Addr, bool) {
	if len(b) != 4 && len(b) != 16 {
		return netip.Addr{}, false
	}
	a, ok := netip.AddrFromSlice(b)
	if !ok {
		return netip.Addr{}, false
	}
	return a.Unmap(), true
}

// defaultPrefix returns the family-wide /0 prefix for a default route, which the
// kernel emits without an RTA_DST attribute.
func defaultPrefix(family uint8) (netip.Prefix, bool) {
	switch family {
	case unix.AF_INET:
		return netip.PrefixFrom(netip.IPv4Unspecified(), 0), true
	case unix.AF_INET6:
		return netip.PrefixFrom(netip.IPv6Unspecified(), 0), true
	}
	return netip.Prefix{}, false
}

// hostPrefix returns the /32 or /128 single-host prefix for a.
func hostPrefix(a netip.Addr) netip.Prefix {
	return netip.PrefixFrom(a, a.BitLen())
}

// prefixFromBytes builds a canonical (masked) prefix from raw destination bytes
// and a prefix length, rejecting a length that exceeds the address width.
func prefixFromBytes(b []byte, bits uint8) (netip.Prefix, bool) {
	a, ok := addrFromBytes(b)
	if !ok {
		return netip.Prefix{}, false
	}
	if int(bits) > a.BitLen() {
		return netip.Prefix{}, false
	}
	return netip.PrefixFrom(a, int(bits)).Masked(), true
}
