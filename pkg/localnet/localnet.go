// Package localnet classifies a socket endpoint IP as belonging to the local
// host (self), a directly-connected subnet, or somewhere remote, using the
// local addresses and routing table of a specific network namespace.
//
// It is the consumer side of the rtnetlink discovery machinery in pkg/xtcpnl:
// xtcp2 dumps each monitored namespace's RTM_GETADDR + RTM_GETROUTE (and
// RTM_GETLINK) replies, feeds the parsed AddrInfo/RouteInfo into BuildSnapshot,
// and publishes the immutable Snapshot atomically. On xtcp2's per-socket
// enrichment hot path a destination address is classified with Classify before
// the internet IP->ASN lookup: self and connected-subnet destinations never
// reach the ASN feed.
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
		return "connected_subnet"
	case LocalityRemote:
		return "remote"
	default:
		return "unspecified"
	}
}

// Snapshot is an immutable per-namespace view of local addresses and connected
// subnets. Self addresses are stored as host prefixes (/32, /128) and connected
// subnets as their network prefix in a single longest-prefix-match trie, so a
// self host address wins over its containing subnet in one Lookup. The zero
// value classifies everything as remote; build with BuildSnapshot.
type Snapshot struct {
	tbl *bart.Table[Locality]
}

// Classify returns how addr relates to this snapshot's namespace. Loopback and
// the unspecified address short-circuit to self; a valid non-self address that
// matches a connected subnet is LocalitySubnet; anything else is LocalityRemote.
// An invalid address is LocalityUnspecified. Pure and allocation-free.
func (s *Snapshot) Classify(addr netip.Addr) Locality {
	a := addr.Unmap()
	if !a.IsValid() {
		return LocalityUnspecified
	}
	if a.IsLoopback() || a.IsUnspecified() {
		return LocalitySelf
	}
	if s == nil || s.tbl == nil {
		return LocalityRemote
	}
	if v, ok := s.tbl.Lookup(a); ok {
		return v
	}
	return LocalityRemote
}

// BuildSnapshot constructs a Snapshot from one namespace's parsed RTM_GETADDR
// and RTM_GETROUTE replies. Self set = every interface address (IFA_LOCAL,
// falling back to IFA_ADDRESS) plus every RTN_LOCAL route destination.
// Connected subnets = routes that are unicast, gatewayless and carry a
// destination prefix (scope is NOT part of the test — IPv4 connected subnets
// are scope-link but IPv6 connected subnets are scope-universe). It is pure:
// no syscalls, safe to feed test fixtures.
func BuildSnapshot(addrs []xtcpnl.AddrInfo, routes []xtcpnl.RouteInfo) *Snapshot {
	tbl := new(bart.Table[Locality])

	for _, ai := range addrs {
		raw := ai.Local
		if len(raw) == 0 {
			raw = ai.Address
		}
		if a, ok := addrFromBytes(raw); ok {
			tbl.Insert(hostPrefix(a), LocalitySelf)
		}
	}

	for _, ri := range routes {
		switch {
		case ri.Type == unix.RTN_LOCAL:
			// A locally-attached address (usually in RT_TABLE_LOCAL, scope host).
			if a, ok := addrFromBytes(ri.Dst); ok {
				tbl.Insert(hostPrefix(a), LocalitySelf)
			}
		case ri.Type == unix.RTN_UNICAST &&
			len(ri.Gateway) == 0 &&
			len(ri.Dst) > 0:
			// A directly-connected subnet (one L2 hop, no next-hop router).
			// The distinguishing signal is unicast + a destination prefix + no
			// gateway, NOT the route scope: real captures show IPv4 connected
			// subnets carry scope=RT_SCOPE_LINK (253) while IPv6 connected
			// subnets carry scope=RT_SCOPE_UNIVERSE (0), so gating on
			// RT_SCOPE_LINK silently misclassifies every IPv6 on-link subnet as
			// REMOTE.
			if pfx, ok := prefixFromBytes(ri.Dst, ri.DstLen); ok {
				// Don't let a /0 connected route swallow everything into
				// LocalitySubnet.
				if pfx.Bits() > 0 {
					tbl.Insert(pfx, LocalitySubnet)
				}
			}
		}
	}

	return &Snapshot{tbl: tbl}
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
