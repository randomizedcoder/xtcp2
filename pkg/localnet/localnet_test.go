package localnet

import (
	"net/netip"
	"testing"

	"golang.org/x/sys/unix"

	"github.com/randomizedcoder/xtcp2/pkg/xtcpnl"
)

// v4 returns the 4-byte network-order form of an IPv4 dotted string.
func v4(t *testing.T, s string) []byte {
	t.Helper()
	a := netip.MustParseAddr(s)
	if !a.Is4() {
		t.Fatalf("v4: %q is not IPv4", s)
	}
	b := a.As4()
	return b[:]
}

// v6 returns the 16-byte network-order form of an IPv6 string.
func v6(t *testing.T, s string) []byte {
	t.Helper()
	a := netip.MustParseAddr(s)
	if !a.Is6() {
		t.Fatalf("v6: %q is not IPv6", s)
	}
	b := a.As16()
	return b[:]
}

// connectedRoute builds a directly-connected (scope-link, gatewayless, unicast)
// main-table route to dst/bits.
func connectedRoute(family uint8, dst []byte, bits uint8) xtcpnl.RouteInfo {
	return xtcpnl.RouteInfo{
		Family: family,
		DstLen: bits,
		Table:  unix.RT_TABLE_MAIN,
		Type:   unix.RTN_UNICAST,
		Scope:  unix.RT_SCOPE_LINK,
		Dst:    dst,
	}
}

// gatewayRoute builds a main-table route reached via a next-hop gateway (NOT
// connected).
func gatewayRoute(family uint8, dst []byte, bits uint8, gw []byte) xtcpnl.RouteInfo {
	return xtcpnl.RouteInfo{
		Family:  family,
		DstLen:  bits,
		Table:   unix.RT_TABLE_MAIN,
		Type:    unix.RTN_UNICAST,
		Scope:   unix.RT_SCOPE_UNIVERSE,
		Dst:     dst,
		Gateway: gw,
	}
}

// defaultRoute builds the kernel's form of a default route: no RTA_DST at all
// (Dst nil, DstLen 0) — BuildSnapshot must synthesize the family /0. gw may be
// nil for a gatewayless `default dev <iface>` (point-to-point) route.
func defaultRoute(family uint8, gw []byte, oif uint32) xtcpnl.RouteInfo {
	return xtcpnl.RouteInfo{
		Family:  family,
		DstLen:  0,
		Table:   unix.RT_TABLE_MAIN,
		Type:    unix.RTN_UNICAST,
		Scope:   unix.RT_SCOPE_UNIVERSE,
		Gateway: gw,
		Oif:     oif,
	}
}

// localRoute builds an RTN_LOCAL host route (a locally-attached address) in the
// local table.
func localRoute(family uint8, dst []byte) xtcpnl.RouteInfo {
	return localRangeRoute(family, dst, uint8(len(dst)*8))
}

// localRangeRoute builds an RTN_LOCAL route covering dst/bits, as the kernel
// installs for `local 127.0.0.0/8 dev lo` or a `local` route added by hand.
func localRangeRoute(family uint8, dst []byte, bits uint8) xtcpnl.RouteInfo {
	return xtcpnl.RouteInfo{
		Family: family,
		DstLen: bits,
		Table:  unix.RT_TABLE_LOCAL,
		Type:   unix.RTN_LOCAL,
		Scope:  unix.RT_SCOPE_HOST,
		Dst:    dst,
	}
}

// inTable returns r with its routing table id replaced (policy-routing tables).
func inTable(r xtcpnl.RouteInfo, table uint32) xtcpnl.RouteInfo {
	r.Table = table
	return r
}

// withType returns r with its rtm_type replaced (RTN_BROADCAST, RTN_BLACKHOLE, …).
func withType(r xtcpnl.RouteInfo, typ uint8) xtcpnl.RouteInfo {
	r.Type = typ
	return r
}

// TestClassify feeds a BuildSnapshot-produced Snapshot a range of destination
// addresses and asserts the classification. Every row carries a description and
// the expected Locality, covering positive, negative, boundary and corner cases.
//
// go test ./pkg/localnet/ -run TestClassify
func TestClassify(t *testing.T) {
	// A representative dual-stack namespace:
	//   - self v4 10.0.0.5 (from IFA_LOCAL)   self v6 2001:db8::5
	//   - connected subnet 10.0.0.0/24        connected v6 2001:db8::/64
	//   - a local-table host route 172.16.0.1 (RTN_LOCAL)
	addrs := []xtcpnl.AddrInfo{
		{Family: unix.AF_INET, Prefixlen: 24, Local: v4(t, "10.0.0.5"), Address: v4(t, "10.0.0.5")},
		{Family: unix.AF_INET6, Prefixlen: 64, Address: v6(t, "2001:db8::5")},
	}
	routes := []xtcpnl.RouteInfo{
		connectedRoute(unix.AF_INET, v4(t, "10.0.0.0"), 24),
		connectedRoute(unix.AF_INET6, v6(t, "2001:db8::"), 64),
		gatewayRoute(unix.AF_INET, v4(t, "0.0.0.0"), 0, v4(t, "10.0.0.1")), // default via gw — must not create a subnet
		localRoute(unix.AF_INET, v4(t, "172.16.0.1")),
	}
	snap := BuildSnapshot(addrs, routes, nil)

	tests := []struct {
		description string
		addr        string
		want        Locality
	}{
		// positive
		{"self IPv4 address (IFA_LOCAL) -> self", "10.0.0.5", LocalitySelf},
		{"self IPv6 address -> self", "2001:db8::5", LocalitySelf},
		{"RTN_LOCAL host route dest -> self", "172.16.0.1", LocalitySelf},
		{"peer in connected IPv4 subnet -> local_subnet", "10.0.0.42", LocalitySubnet},
		{"peer in connected IPv6 subnet -> local_subnet", "2001:db8::1234", LocalitySubnet},
		// negative
		{"public IPv4 not in any set -> remote", "8.8.8.8", LocalityRemote},
		{"public IPv6 not in any set -> remote", "2606:4700::1111", LocalityRemote},
		{"address only reachable via gateway -> remote", "93.184.216.34", LocalityRemote},
		{"IPv4 just outside connected /24 -> remote", "10.0.1.1", LocalityRemote},
		// boundary
		{"network address of connected subnet -> local_subnet", "10.0.0.0", LocalitySubnet},
		{"broadcast-ish last host of /24 -> local_subnet", "10.0.0.255", LocalitySubnet},
		{"self host /32 wins over containing /24 subnet", "10.0.0.5", LocalitySelf},
		// corner
		{"IPv4 loopback short-circuits -> self", "127.0.0.1", LocalitySelf},
		{"IPv6 loopback short-circuits -> self", "::1", LocalitySelf},
		{"IPv4 unspecified (LISTEN peer) is no destination -> unspecified", "0.0.0.0", LocalityUnspecified},
		{"IPv6 unspecified (LISTEN peer) is no destination -> unspecified", "::", LocalityUnspecified},
		{"IPv4-mapped IPv6 of a self address -> self", "::ffff:10.0.0.5", LocalitySelf},
		{"IPv4-mapped IPv6 of a subnet peer -> local_subnet", "::ffff:10.0.0.9", LocalitySubnet},
	}

	for _, tc := range tests {
		t.Run(tc.description, func(t *testing.T) {
			got := snap.Classify(netip.MustParseAddr(tc.addr))
			if got != tc.want {
				t.Errorf("Classify(%s) = %v, want %v", tc.addr, got, tc.want)
			}
		})
	}
}

// TestClassifyInvalidAndNil covers the invalid-address and nil/empty-snapshot
// corner cases that can't be expressed as a parseable address string.
//
// go test ./pkg/localnet/ -run TestClassifyInvalidAndNil
func TestClassifyInvalidAndNil(t *testing.T) {
	empty := BuildSnapshot(nil, nil, nil)

	tests := []struct {
		description string
		snap        *Snapshot
		addr        netip.Addr
		want        Locality
	}{
		{"invalid zero address -> unspecified", empty, netip.Addr{}, LocalityUnspecified},
		{"nil snapshot, valid remote address -> remote", nil, netip.MustParseAddr("8.8.8.8"), LocalityRemote},
		{"nil snapshot, loopback still short-circuits -> self", nil, netip.MustParseAddr("127.0.0.1"), LocalitySelf},
		{"nil snapshot, unspecified address -> unspecified (not self, not remote)", nil, netip.MustParseAddr("0.0.0.0"), LocalityUnspecified},
		{"empty snapshot, valid address -> remote", empty, netip.MustParseAddr("10.0.0.5"), LocalityRemote},
		{"empty snapshot, invalid address -> unspecified", empty, netip.Addr{}, LocalityUnspecified},
		{"empty snapshot, v6 unspecified -> unspecified", empty, netip.MustParseAddr("::"), LocalityUnspecified},
	}

	for _, tc := range tests {
		t.Run(tc.description, func(t *testing.T) {
			got := tc.snap.Classify(tc.addr)
			if got != tc.want {
				t.Errorf("Classify = %v, want %v", got, tc.want)
			}
		})
	}
}

// TestBuildSnapshot asserts which inputs create self host prefixes vs connected
// subnets vs nothing, by probing the resulting snapshot. Table columns:
// description, the addrs/routes input, a probe address, and the expected
// Locality (the observable outcome of the build).
//
// go test ./pkg/localnet/ -run TestBuildSnapshot
func TestBuildSnapshot(t *testing.T) {
	tests := []struct {
		description string
		addrs       []xtcpnl.AddrInfo
		routes      []xtcpnl.RouteInfo
		probe       string
		want        Locality
	}{
		// positive
		{
			description: "IFA_LOCAL populates the self set",
			addrs:       []xtcpnl.AddrInfo{{Family: unix.AF_INET, Local: v4(t, "10.1.2.3")}},
			probe:       "10.1.2.3",
			want:        LocalitySelf,
		},
		{
			description: "IFA_ADDRESS used when IFA_LOCAL absent",
			addrs:       []xtcpnl.AddrInfo{{Family: unix.AF_INET6, Address: v6(t, "fe80::1")}},
			probe:       "fe80::1",
			want:        LocalitySelf,
		},
		{
			description: "scope-link gatewayless unicast route (IPv4 connected subnet) -> subnet",
			routes:      []xtcpnl.RouteInfo{connectedRoute(unix.AF_INET, v4(t, "192.168.0.0"), 24)},
			probe:       "192.168.0.7",
			want:        LocalitySubnet,
		},
		{
			// Real kernels emit IPv6 connected subnets with scope
			// RT_SCOPE_UNIVERSE (0), not RT_SCOPE_LINK — confirmed by the 7.1.8
			// getroute fixture (pkg/xtcpnl/testdata/7_1_8, fd10:10:4::/64). The
			// connected-subnet rule must therefore be scope-agnostic: unicast +
			// gatewayless + has-Dst. A scope-link gate would misclassify every
			// IPv6 on-link subnet as remote.
			description: "universe-scope gatewayless unicast route (IPv6 connected subnet) -> subnet",
			routes: []xtcpnl.RouteInfo{{
				Family: unix.AF_INET6, DstLen: 64, Table: unix.RT_TABLE_MAIN, Type: unix.RTN_UNICAST,
				Scope: unix.RT_SCOPE_UNIVERSE, Dst: v6(t, "fd10:10:4::"),
			}},
			probe: "fd10:10:4::7",
			want:  LocalitySubnet,
		},
		{
			description: "RTN_LOCAL range `local 127.0.0.0/8 dev lo` covers every loopback address as self",
			routes:      []xtcpnl.RouteInfo{localRangeRoute(unix.AF_INET, v4(t, "127.0.0.0"), 8)},
			probe:       "127.0.0.5",
			want:        LocalitySelf,
		},
		{
			description: "RTN_LOCAL range (`ip route add local 10.200.0.0/24 dev lo`) makes the whole range self",
			routes:      []xtcpnl.RouteInfo{localRangeRoute(unix.AF_INET, v4(t, "10.200.0.0"), 24)},
			probe:       "10.200.0.77",
			want:        LocalitySelf,
		},
		// negative
		{
			description: "route with a gateway is NOT a connected subnet",
			routes:      []xtcpnl.RouteInfo{gatewayRoute(unix.AF_INET, v4(t, "192.168.0.0"), 24, v4(t, "192.168.0.1"))},
			probe:       "192.168.0.7",
			want:        LocalityRemote,
		},
		{
			description: "ECMP route (RTA_MULTIPATH, no RTA_GATEWAY of its own) is remote, not a subnet",
			routes: []xtcpnl.RouteInfo{func() xtcpnl.RouteInfo {
				r := connectedRoute(unix.AF_INET, v4(t, "10.20.0.0"), 16)
				r.HasMultipath = true
				return r
			}()},
			probe: "10.20.1.1",
			want:  LocalityRemote,
		},
		{
			description: "route with a cross-family gateway (RTA_VIA, no RTA_GATEWAY) is remote",
			routes: []xtcpnl.RouteInfo{func() xtcpnl.RouteInfo {
				r := connectedRoute(unix.AF_INET, v4(t, "10.30.0.0"), 16)
				r.HasVia = true
				return r
			}()},
			probe: "10.30.1.1",
			want:  LocalityRemote,
		},
		{
			description: "route pointing at a nexthop object (RTA_NH_ID, no RTA_GATEWAY) is remote",
			routes: []xtcpnl.RouteInfo{func() xtcpnl.RouteInfo {
				r := connectedRoute(unix.AF_INET, v4(t, "10.40.0.0"), 16)
				r.NhID = 7
				return r
			}()},
			probe: "10.40.1.1",
			want:  LocalityRemote,
		},
		{
			description: "connected route in a policy-routing table (100) is ignored -> remote",
			routes:      []xtcpnl.RouteInfo{inTable(connectedRoute(unix.AF_INET, v4(t, "10.50.0.0"), 24), 100)},
			probe:       "10.50.0.9",
			want:        LocalityRemote,
		},
		{
			description: "RTN_LOCAL route in a policy-routing table (100) is ignored -> remote",
			routes:      []xtcpnl.RouteInfo{inTable(localRoute(unix.AF_INET, v4(t, "10.60.0.1")), 100)},
			probe:       "10.60.0.1",
			want:        LocalityRemote,
		},
		{
			description: "route in RT_TABLE_DEFAULT (253) is ignored -> remote",
			routes:      []xtcpnl.RouteInfo{inTable(connectedRoute(unix.AF_INET, v4(t, "10.70.0.0"), 24), unix.RT_TABLE_DEFAULT)},
			probe:       "10.70.0.9",
			want:        LocalityRemote,
		},
		{
			description: "RTN_BROADCAST route (local table, `broadcast 10.0.0.255 dev eth0`) is not self or subnet",
			routes:      []xtcpnl.RouteInfo{withType(localRoute(unix.AF_INET, v4(t, "10.80.0.255")), unix.RTN_BROADCAST)},
			probe:       "10.80.0.255",
			want:        LocalityRemote,
		},
		{
			description: "RTN_BLACKHOLE route is ignored -> remote",
			routes:      []xtcpnl.RouteInfo{withType(connectedRoute(unix.AF_INET, v4(t, "10.90.0.0"), 24), unix.RTN_BLACKHOLE)},
			probe:       "10.90.0.9",
			want:        LocalityRemote,
		},
		{
			description: "RTN_UNREACHABLE route is ignored -> remote",
			routes:      []xtcpnl.RouteInfo{withType(connectedRoute(unix.AF_INET, v4(t, "10.100.0.0"), 24), unix.RTN_UNREACHABLE)},
			probe:       "10.100.0.9",
			want:        LocalityRemote,
		},
		{
			description: "gateway route more specific than a connected subnet wins -> remote",
			routes: []xtcpnl.RouteInfo{
				connectedRoute(unix.AF_INET, v4(t, "10.0.0.0"), 16),
				gatewayRoute(unix.AF_INET, v4(t, "10.0.1.0"), 24, v4(t, "10.0.0.1")),
			},
			probe: "10.0.1.5",
			want:  LocalityRemote,
		},
		// boundary
		{
			description: "/32 connected route classifies only that host",
			routes:      []xtcpnl.RouteInfo{connectedRoute(unix.AF_INET, v4(t, "10.9.9.9"), 32)},
			probe:       "10.9.9.9",
			want:        LocalitySubnet,
		},
		{
			description: "/32 connected route does not cover a neighbor",
			routes:      []xtcpnl.RouteInfo{connectedRoute(unix.AF_INET, v4(t, "10.9.9.9"), 32)},
			probe:       "10.9.9.10",
			want:        LocalityRemote,
		},
		{
			description: "gatewayless /0 with an explicit 0.0.0.0 Dst is remote, never a subnet (must not swallow everything)",
			routes:      []xtcpnl.RouteInfo{connectedRoute(unix.AF_INET, v4(t, "0.0.0.0"), 0)},
			probe:       "8.8.8.8",
			want:        LocalityRemote,
		},
		{
			description: "kernel-form gatewayless default (`default dev wg0`: Dst nil, DstLen 0) is remote, never a subnet",
			routes:      []xtcpnl.RouteInfo{defaultRoute(unix.AF_INET, nil, 7)},
			probe:       "8.8.8.8",
			want:        LocalityRemote,
		},
		{
			description: "kernel-form IPv6 default via gateway (Dst nil, DstLen 0) is remote",
			routes:      []xtcpnl.RouteInfo{defaultRoute(unix.AF_INET6, v6(t, "fe80::1"), 2)},
			probe:       "2606:4700::1111",
			want:        LocalityRemote,
		},
		{
			description: "self address dumped BEFORE a same-prefix /32 unicast route stays self",
			addrs:       []xtcpnl.AddrInfo{{Family: unix.AF_INET, Local: v4(t, "10.0.0.5")}},
			routes:      []xtcpnl.RouteInfo{connectedRoute(unix.AF_INET, v4(t, "10.0.0.5"), 32)},
			probe:       "10.0.0.5",
			want:        LocalitySelf,
		},
		{
			description: "connected /32 route dumped BEFORE the RTN_LOCAL entry for the same address -> self",
			routes: []xtcpnl.RouteInfo{
				connectedRoute(unix.AF_INET, v4(t, "10.0.0.5"), 32),
				localRoute(unix.AF_INET, v4(t, "10.0.0.5")),
			},
			probe: "10.0.0.5",
			want:  LocalitySelf,
		},
		{
			description: "RTN_LOCAL entry dumped BEFORE a same-prefix connected /32 route -> self",
			routes: []xtcpnl.RouteInfo{
				localRoute(unix.AF_INET, v4(t, "10.0.0.5")),
				connectedRoute(unix.AF_INET, v4(t, "10.0.0.5"), 32),
			},
			probe: "10.0.0.5",
			want:  LocalitySelf,
		},
		{
			description: "gateway /32 host route for a self address (any dump order) -> self",
			routes: []xtcpnl.RouteInfo{
				gatewayRoute(unix.AF_INET, v4(t, "10.0.0.5"), 32, v4(t, "10.0.0.1")),
				localRoute(unix.AF_INET, v4(t, "10.0.0.5")),
			},
			probe: "10.0.0.5",
			want:  LocalitySelf,
		},
		{
			description: "more-specific connected subnet still classifies as subnet",
			routes: []xtcpnl.RouteInfo{
				connectedRoute(unix.AF_INET, v4(t, "10.0.0.0"), 8),
				connectedRoute(unix.AF_INET, v4(t, "10.1.0.0"), 16),
			},
			probe: "10.1.2.3",
			want:  LocalitySubnet,
		},
		// corner
		{
			description: "zero-length Dst with a non-zero DstLen (malformed) is skipped, address stays remote",
			routes:      []xtcpnl.RouteInfo{{Family: unix.AF_INET, DstLen: 24, Table: unix.RT_TABLE_MAIN, Type: unix.RTN_UNICAST, Scope: unix.RT_SCOPE_LINK}},
			probe:       "10.0.0.1",
			want:        LocalityRemote,
		},
		{
			description: "synthetic route with Table 0 (RT_TABLE_UNSPEC, never emitted by the kernel) is ignored",
			routes:      []xtcpnl.RouteInfo{inTable(connectedRoute(unix.AF_INET, v4(t, "10.0.0.0"), 24), unix.RT_TABLE_UNSPEC)},
			probe:       "10.0.0.1",
			want:        LocalityRemote,
		},
		{
			description: "malformed 3-byte address is skipped",
			addrs:       []xtcpnl.AddrInfo{{Family: unix.AF_INET, Local: []byte{10, 0, 0}}},
			probe:       "10.0.0.5",
			want:        LocalityRemote,
		},
		{
			description: "prefix length beyond address width is skipped",
			routes:      []xtcpnl.RouteInfo{connectedRoute(unix.AF_INET, v4(t, "10.0.0.0"), 40)},
			probe:       "10.0.0.1",
			want:        LocalityRemote,
		},
		{
			description: "RTN_LOCAL route adds a self host address",
			routes:      []xtcpnl.RouteInfo{localRoute(unix.AF_INET6, v6(t, "2001:db8::99"))},
			probe:       "2001:db8::99",
			want:        LocalitySelf,
		},
	}

	for _, tc := range tests {
		t.Run(tc.description, func(t *testing.T) {
			snap := BuildSnapshot(tc.addrs, tc.routes, nil)
			got := snap.Classify(netip.MustParseAddr(tc.probe))
			if got != tc.want {
				t.Errorf("Classify(%s) = %v, want %v", tc.probe, got, tc.want)
			}
		})
	}
}

// oifRoute is a route helper that also sets the egress interface index.
func oifRoute(r xtcpnl.RouteInfo, oif uint32) xtcpnl.RouteInfo {
	r.Oif = oif
	return r
}

// TestLookupEgressAndIfName asserts the richer Lookup result (Locality + egress
// interface index) and IfName resolution. The snapshot models a dual-stack
// namespace on eth0 (ifindex 2) with loopback lo (ifindex 1):
//   - self v4 10.0.0.5 / v6 2001:db8::5 on eth0
//   - connected subnets 10.0.0.0/24 (eth0) and 2001:db8::/64 (eth0)
//   - IPv4 default via 10.0.0.1 on eth0 (gateway route -> remote, egress kept)
//   - RTN_LOCAL 127.0.0.1 on lo
//
// go test ./pkg/localnet/ -run TestLookupEgressAndIfName
func TestLookupEgressAndIfName(t *testing.T) {
	addrs := []xtcpnl.AddrInfo{
		{Family: unix.AF_INET, Index: 2, Local: v4(t, "10.0.0.5"), Address: v4(t, "10.0.0.5")},
		{Family: unix.AF_INET6, Index: 2, Address: v6(t, "2001:db8::5")},
	}
	routes := []xtcpnl.RouteInfo{
		oifRoute(connectedRoute(unix.AF_INET, v4(t, "10.0.0.0"), 24), 2),
		oifRoute(connectedRoute(unix.AF_INET6, v6(t, "2001:db8::"), 64), 2),
		oifRoute(gatewayRoute(unix.AF_INET, v4(t, "0.0.0.0"), 0, v4(t, "10.0.0.1")), 2),
		oifRoute(localRoute(unix.AF_INET, v4(t, "127.0.0.1")), 1),
	}
	links := map[uint32]string{1: "lo", 2: "eth0"}
	snap := BuildSnapshot(addrs, routes, links)

	tests := []struct {
		description string
		addr        string
		wantLoc     Locality
		wantOif     uint32
		wantOk      bool
		wantIfName  string // snap.IfName(gotOif)
	}{
		// positive — locality + egress interface off the matched route
		{"connected v4 peer -> subnet via eth0", "10.0.0.42", LocalitySubnet, 2, true, "eth0"},
		{"connected v6 peer -> subnet via eth0", "2001:db8::1234", LocalitySubnet, 2, true, "eth0"},
		{"self v4 -> self, egress from its address ifindex", "10.0.0.5", LocalitySelf, 2, true, "eth0"},
		{"remote v4 matches default route -> remote, egress eth0", "8.8.8.8", LocalityRemote, 2, true, "eth0"},
		// negative — no route matches, so no egress interface
		{"remote v6 with no v6 default -> remote, no egress", "2606:4700::1111", LocalityRemote, 0, true, ""},
		// boundary — loopback short-circuits to self but still resolves lo egress
		{"v4 loopback -> self via lo (RTN_LOCAL egress)", "127.0.0.1", LocalitySelf, 1, true, "lo"},
		{"v6 loopback -> self, no matching route -> no egress", "::1", LocalitySelf, 0, true, ""},
		// corner — invalid address
		{"invalid address -> unspecified, not ok", "", LocalityUnspecified, 0, false, ""},
	}

	for _, tc := range tests {
		t.Run(tc.description, func(t *testing.T) {
			var addr netip.Addr
			if tc.addr != "" {
				addr = netip.MustParseAddr(tc.addr)
			}
			loc, oif, ok := snap.Lookup(addr)
			if loc != tc.wantLoc || oif != tc.wantOif || ok != tc.wantOk {
				t.Errorf("Lookup(%q) = (%v, %d, %v), want (%v, %d, %v)",
					tc.addr, loc, oif, ok, tc.wantLoc, tc.wantOif, tc.wantOk)
			}
			if name := snap.IfName(oif); name != tc.wantIfName {
				t.Errorf("IfName(%d) = %q, want %q", oif, name, tc.wantIfName)
			}
		})
	}
}

// TestIfName covers interface-index resolution directly, including the zero,
// unknown and nil-snapshot corner cases.
//
// go test ./pkg/localnet/ -run TestIfName
func TestIfName(t *testing.T) {
	snap := BuildSnapshot(nil, nil, map[uint32]string{1: "lo", 2: "eth0"})

	tests := []struct {
		description string
		snap        *Snapshot
		index       uint32
		want        string
	}{
		{"known index -> name", snap, 2, "eth0"},
		{"index 0 -> empty (kernel idiag_if unset)", snap, 0, ""},
		{"unknown index -> empty", snap, 999, ""},
		{"nil snapshot -> empty", nil, 2, ""},
	}
	for _, tc := range tests {
		t.Run(tc.description, func(t *testing.T) {
			if got := tc.snap.IfName(tc.index); got != tc.want {
				t.Errorf("IfName(%d) = %q, want %q", tc.index, got, tc.want)
			}
		})
	}
}

// TestDefaultPrefix covers the defaultPrefix helper directly: the kernel emits a
// default route with no RTA_DST, so BuildSnapshot must synthesize the family-wide
// /0 from the rtmsg family. Positive for both families, negative for anything else.
//
// go test ./pkg/localnet/ -run TestDefaultPrefix
func TestDefaultPrefix(t *testing.T) {
	tests := []struct {
		description string
		family      uint8
		wantOk      bool
		wantPfx     string // only checked when wantOk
	}{
		// positive
		{"AF_INET -> 0.0.0.0/0", unix.AF_INET, true, "0.0.0.0/0"},
		{"AF_INET6 -> ::/0", unix.AF_INET6, true, "::/0"},
		// negative / corner — an unexpected family yields no prefix so the route
		// is dropped rather than mapped to a bogus /0.
		{"AF_UNSPEC -> not ok", unix.AF_UNSPEC, false, ""},
		{"AF_PACKET (bogus family) -> not ok", unix.AF_PACKET, false, ""},
		{"arbitrary high family byte -> not ok", 200, false, ""},
	}
	for _, tc := range tests {
		t.Run(tc.description, func(t *testing.T) {
			pfx, ok := defaultPrefix(tc.family)
			if ok != tc.wantOk {
				t.Fatalf("defaultPrefix(%d) ok = %v, want %v", tc.family, ok, tc.wantOk)
			}
			if ok && pfx != netip.MustParsePrefix(tc.wantPfx) {
				t.Errorf("defaultPrefix(%d) = %v, want %v", tc.family, pfx, tc.wantPfx)
			}
		})
	}
}

// TestLookupEgressDefaultsAndPrecedence covers the egress-interface branches not
// exercised by TestLookupEgressAndIfName: both default routes in the kernel's
// RTA_DST-less form (the defaultPrefix AF_INET / AF_INET6 paths), longest-prefix
// egress precedence (a more-specific gateway route's Oif beating the default's),
// a connected subnet whose route has no Oif, a self address whose owning
// AddrInfo has Index 0, and an egress ifindex that is absent from the
// RTM_GETLINK map (IfName -> ""). The namespace:
//   - self v4 10.0.0.5 with AddrInfo.Index 0 (no owning link recorded)
//   - connected subnet 172.16.0.0/24 with Oif 0 (route carried no RTA_OIF)
//   - IPv4 default via 10.0.0.1 on eth0 (ifindex 2), no RTA_DST
//   - IPv6 default via fe80::1 on eth0 (ifindex 2), no RTA_DST
//   - more-specific 203.0.113.0/24 via 10.0.0.1 on eth1 (ifindex 3)
//   - more-specific 198.51.100.0/24 via 10.0.0.1 on ifindex 99 (NOT in links)
//
// go test ./pkg/localnet/ -run TestLookupEgressDefaultsAndPrecedence
func TestLookupEgressDefaultsAndPrecedence(t *testing.T) {
	addrs := []xtcpnl.AddrInfo{
		{Family: unix.AF_INET, Index: 0, Local: v4(t, "10.0.0.5")},
	}
	routes := []xtcpnl.RouteInfo{
		oifRoute(connectedRoute(unix.AF_INET, v4(t, "172.16.0.0"), 24), 0),
		defaultRoute(unix.AF_INET, v4(t, "10.0.0.1"), 2),
		defaultRoute(unix.AF_INET6, v6(t, "fe80::1"), 2),
		oifRoute(gatewayRoute(unix.AF_INET, v4(t, "203.0.113.0"), 24, v4(t, "10.0.0.1")), 3),
		oifRoute(gatewayRoute(unix.AF_INET, v4(t, "198.51.100.0"), 24, v4(t, "10.0.0.1")), 99),
	}
	links := map[uint32]string{2: "eth0", 3: "eth1"} // 99 deliberately absent
	snap := BuildSnapshot(addrs, routes, links)

	tests := []struct {
		description string
		addr        string
		wantLoc     Locality
		wantOif     uint32
		wantIfName  string
	}{
		// positive — RTA_DST-less default routes (defaultPrefix AF_INET6 / AF_INET)
		{"remote v6 matches the synthesized ::/0 default -> remote via eth0", "2606:4700::1111", LocalityRemote, 2, "eth0"},
		{"remote v4 not in any specific route -> synthesized 0.0.0.0/0 default egress eth0", "8.8.8.8", LocalityRemote, 2, "eth0"},
		// positive — longest-prefix egress precedence: the /24 beats the /0
		{"remote v4 in a more-specific route -> its egress eth1", "203.0.113.9", LocalityRemote, 3, "eth1"},
		// boundary — connected subnet route with no Oif
		{"connected subnet with Oif 0 -> subnet, no egress index", "172.16.0.42", LocalitySubnet, 0, ""},
		// boundary — self address whose AddrInfo.Index is 0
		{"self v4 with AddrInfo.Index 0 -> self, no egress index", "10.0.0.5", LocalitySelf, 0, ""},
		// corner — matched route's egress ifindex is not in the RTM_GETLINK map
		{"remote v4 via ifindex absent from links -> index kept, name empty", "198.51.100.7", LocalityRemote, 99, ""},
	}

	for _, tc := range tests {
		t.Run(tc.description, func(t *testing.T) {
			loc, oif, ok := snap.Lookup(netip.MustParseAddr(tc.addr))
			if !ok {
				t.Fatalf("Lookup(%s) ok = false, want true", tc.addr)
			}
			if loc != tc.wantLoc || oif != tc.wantOif {
				t.Errorf("Lookup(%s) = (%v, %d), want (%v, %d)", tc.addr, loc, oif, tc.wantLoc, tc.wantOif)
			}
			if name := snap.IfName(oif); name != tc.wantIfName {
				t.Errorf("IfName(%d) = %q, want %q", oif, name, tc.wantIfName)
			}
		})
	}
}

// TestResolve exercises the pure hot-path fold used by pkg/xtcp's applyEnrichment
// (which can't be unit-tested in place — its test binary won't link against the
// pinned giouring). Resolve must, in one call, classify the destination, resolve
// the matched route's egress Oif to a name, resolve the socket's own bound
// interface index (the kernel idiag_if) to a name, and report whether the
// destination is remote (the ASN-lookup gate). The namespace:
//   - self v4 10.0.0.5 on eth0 (ifindex 2)
//   - connected subnet 10.0.0.0/24 on eth0
//   - IPv4 default via 10.0.0.1 on eth0
//   - more-specific 203.0.113.0/24 via 10.0.0.1 on eth1 (ifindex 3)
//   - links {1: lo, 2: eth0, 3: eth1}
//
// go test ./pkg/localnet/ -run TestResolve
func TestResolve(t *testing.T) {
	addrs := []xtcpnl.AddrInfo{
		{Family: unix.AF_INET, Index: 2, Local: v4(t, "10.0.0.5")},
	}
	routes := []xtcpnl.RouteInfo{
		oifRoute(connectedRoute(unix.AF_INET, v4(t, "10.0.0.0"), 24), 2),
		oifRoute(gatewayRoute(unix.AF_INET, v4(t, "0.0.0.0"), 0, v4(t, "10.0.0.1")), 2),
		oifRoute(gatewayRoute(unix.AF_INET, v4(t, "203.0.113.0"), 24, v4(t, "10.0.0.1")), 3),
	}
	links := map[uint32]string{1: "lo", 2: "eth0", 3: "eth1"}
	snap := BuildSnapshot(addrs, routes, links)

	tests := []struct {
		description  string
		addr         string
		boundIfindex uint32
		want         Resolution
	}{
		// positive — self: not remote, egress from address, bound resolved
		{
			"self dest, socket bound to eth0",
			"10.0.0.5", 2,
			Resolution{Locality: LocalitySelf, EgressIfindex: 2, EgressIfname: "eth0", BoundIfname: "eth0", Remote: false},
		},
		// positive — connected subnet: not remote, bound idiag_if unset (0 -> "")
		{
			"connected-subnet dest, socket bound to nothing (idiag_if 0)",
			"10.0.0.42", 0,
			Resolution{Locality: LocalitySubnet, EgressIfindex: 2, EgressIfname: "eth0", BoundIfname: "", Remote: false},
		},
		// positive — remote via default: remote gate true, both names resolved
		{
			"remote dest via default route, bound to eth0",
			"8.8.8.8", 2,
			Resolution{Locality: LocalityRemote, EgressIfindex: 2, EgressIfname: "eth0", BoundIfname: "eth0", Remote: true},
		},
		// positive — remote via a more-specific route: distinct egress interface
		{
			"remote dest via more-specific route, bound to eth1",
			"203.0.113.9", 3,
			Resolution{Locality: LocalityRemote, EgressIfindex: 3, EgressIfname: "eth1", BoundIfname: "eth1", Remote: true},
		},
		// negative — no matching route: remote, no egress, bound still resolved
		{
			"remote v6 with no v6 route -> remote, no egress, bound lo",
			"2606:4700::1111", 1,
			Resolution{Locality: LocalityRemote, EgressIfindex: 0, EgressIfname: "", BoundIfname: "lo", Remote: true},
		},
		// boundary — loopback short-circuits locality to self (not remote); the LPM
		// matches only the /0 default (a gateway route loopback traffic never
		// uses), so no egress is reported rather than the default's eth0.
		{
			"loopback dest -> self, no egress (default route is not loopback's path), bound eth0",
			"127.0.0.1", 2,
			Resolution{Locality: LocalitySelf, EgressIfindex: 0, EgressIfname: "", BoundIfname: "eth0", Remote: false},
		},
		// boundary — unspecified destination (a LISTEN socket's peer): not a
		// destination at all, so unclassified, no egress, and not remote (no ASN
		// lookup); the bound interface still resolves.
		{
			"unspecified dest (LISTEN peer 0.0.0.0) -> unspecified, not remote, bound eth0",
			"0.0.0.0", 2,
			Resolution{Locality: LocalityUnspecified, EgressIfindex: 0, EgressIfname: "", BoundIfname: "eth0", Remote: false},
		},
		// corner — bound idiag_if index absent from the RTM_GETLINK map -> ""
		{
			"remote dest, socket bound to an ifindex not in links",
			"8.8.8.8", 99,
			Resolution{Locality: LocalityRemote, EgressIfindex: 2, EgressIfname: "eth0", BoundIfname: "", Remote: true},
		},
	}

	for _, tc := range tests {
		t.Run(tc.description, func(t *testing.T) {
			got := snap.Resolve(netip.MustParseAddr(tc.addr), tc.boundIfindex)
			if got != tc.want {
				t.Errorf("Resolve(%s, %d) = %+v, want %+v", tc.addr, tc.boundIfindex, got, tc.want)
			}
		})
	}
}

// TestLookupGatewaylessDefaultAndLoopbackEgress covers the egress rules for
// routes that name no single next hop: a gatewayless `default dev wg0`
// (point-to-point) route supplies its egress to every unmatched destination but
// is never a local subnet; an ECMP (RTA_MULTIPATH) route and a nexthop-object
// (RTA_NH_ID) route are remote with NO egress, because they have several /
// opaque egress interfaces and a single wrong Oif is worse than none; loopback
// takes its egress only from a covering self entry (the kernel's
// `local 127.0.0.0/8 dev lo`), never from the default route. The namespace:
//   - `default dev wg0` (ifindex 7), gatewayless, no RTA_DST
//   - 10.20.0.0/16 ECMP (RTA_MULTIPATH) with RTA_OIF absent
//   - 10.40.0.0/16 via nexthop object 5 (RTA_NH_ID)
//   - `local 127.0.0.0/8 dev lo` (ifindex 1) — IPv4 only; no ::1 local route
//   - links {1: lo, 7: wg0}
//
// go test ./pkg/localnet/ -run TestLookupGatewaylessDefaultAndLoopbackEgress
func TestLookupGatewaylessDefaultAndLoopbackEgress(t *testing.T) {
	ecmp := connectedRoute(unix.AF_INET, v4(t, "10.20.0.0"), 16)
	ecmp.HasMultipath = true
	nh := connectedRoute(unix.AF_INET, v4(t, "10.40.0.0"), 16)
	nh.NhID = 5
	nh.Oif = 9 // an RTA_OIF alongside RTA_NH_ID is not trusted either
	routes := []xtcpnl.RouteInfo{
		defaultRoute(unix.AF_INET, nil, 7),
		ecmp,
		nh,
		oifRoute(localRangeRoute(unix.AF_INET, v4(t, "127.0.0.0"), 8), 1),
	}
	links := map[uint32]string{1: "lo", 7: "wg0"}
	snap := BuildSnapshot(nil, routes, links)

	tests := []struct {
		description string
		addr        string
		wantLoc     Locality
		wantOif     uint32
		wantIfName  string
	}{
		// positive — gatewayless default supplies the egress
		{"remote v4 via `default dev wg0` -> remote, egress wg0", "8.8.8.8", LocalityRemote, 7, "wg0"},
		{"address inside the /0 only is remote, not local subnet", "192.0.2.1", LocalityRemote, 7, "wg0"},
		// negative — multi-nexthop routes report no egress
		{"ECMP (RTA_MULTIPATH) dest -> remote, no egress", "10.20.3.4", LocalityRemote, 0, ""},
		{"nexthop-object (RTA_NH_ID) dest -> remote, no egress even with a stray RTA_OIF", "10.40.3.4", LocalityRemote, 0, ""},
		// boundary — loopback egress comes from the local-table range, not the default
		{"v4 loopback -> self via lo from `local 127.0.0.0/8`", "127.0.0.1", LocalitySelf, 1, "lo"},
		{"any 127/8 address -> self via lo", "127.255.255.254", LocalitySelf, 1, "lo"},
		{"v6 loopback with no ::1 local route -> self, no egress (never the default's)", "::1", LocalitySelf, 0, ""},
		// corner — unspecified is not a destination
		{"unspecified v4 -> unspecified, no egress (not the default's wg0)", "0.0.0.0", LocalityUnspecified, 0, ""},
	}

	for _, tc := range tests {
		t.Run(tc.description, func(t *testing.T) {
			loc, oif, ok := snap.Lookup(netip.MustParseAddr(tc.addr))
			if !ok {
				t.Fatalf("Lookup(%s) ok = false, want true", tc.addr)
			}
			if loc != tc.wantLoc || oif != tc.wantOif {
				t.Errorf("Lookup(%s) = (%v, %d), want (%v, %d)", tc.addr, loc, oif, tc.wantLoc, tc.wantOif)
			}
			if name := snap.IfName(oif); name != tc.wantIfName {
				t.Errorf("IfName(%d) = %q, want %q", oif, name, tc.wantIfName)
			}
		})
	}
}

// TestLocalityString checks the human-readable rendering used in logs/columns,
// including the out-of-range corner value.
//
// go test ./pkg/localnet/ -run TestLocalityString
func TestLocalityString(t *testing.T) {
	tests := []struct {
		description string
		in          Locality
		want        string
	}{
		{"self", LocalitySelf, "self"},
		{"subnet", LocalitySubnet, "local_subnet"},
		{"remote", LocalityRemote, "remote"},
		{"unspecified zero value", LocalityUnspecified, "unspecified"},
		{"out-of-range value falls back to unspecified", Locality(200), "unspecified"},
	}
	for _, tc := range tests {
		t.Run(tc.description, func(t *testing.T) {
			if got := tc.in.String(); got != tc.want {
				t.Errorf("Locality(%d).String() = %q, want %q", tc.in, got, tc.want)
			}
		})
	}
}

// TestHasNonLoopbackSelf covers the loopback-only detector the daemon uses to
// re-dump a namespace whose veth has not been plumbed yet.
//
// go test ./pkg/localnet/ -run TestHasNonLoopbackSelf
func TestHasNonLoopbackSelf(t *testing.T) {
	lo4 := xtcpnl.AddrInfo{Family: unix.AF_INET, Index: 1, Local: v4(t, "127.0.0.1")}
	lo6 := xtcpnl.AddrInfo{Family: unix.AF_INET6, Index: 1, Address: v6(t, "::1")}

	tests := []struct {
		description string
		snap        *Snapshot
		want        bool
	}{
		// positive
		{"non-loopback IFA_LOCAL -> true",
			BuildSnapshot([]xtcpnl.AddrInfo{lo4, lo6, {Family: unix.AF_INET, Index: 2, Local: v4(t, "10.1.2.3")}}, nil, nil), true},
		{"non-loopback via IFA_ADDRESS fallback -> true",
			BuildSnapshot([]xtcpnl.AddrInfo{{Family: unix.AF_INET6, Index: 2, Address: v6(t, "fd00::1")}}, nil, nil), true},
		{"IPv6 link-local only (fe80::) still counts as non-loopback -> true",
			BuildSnapshot([]xtcpnl.AddrInfo{lo4, {Family: unix.AF_INET6, Index: 2, Address: v6(t, "fe80::1")}}, nil, nil), true},
		{"RTN_LOCAL host route with no address entry -> true",
			BuildSnapshot(nil, []xtcpnl.RouteInfo{localRoute(unix.AF_INET, v4(t, "10.1.2.3"))}, nil), true},

		// negative
		{"nil snapshot -> false", nil, false},
		{"zero snapshot -> false", &Snapshot{}, false},
		{"no addresses, no routes -> false", BuildSnapshot(nil, nil, nil), false},
		{"lo addresses + kernel lo routes only -> false",
			BuildSnapshot([]xtcpnl.AddrInfo{lo4, lo6},
				[]xtcpnl.RouteInfo{localRangeRoute(unix.AF_INET, v4(t, "127.0.0.0"), 8), localRoute(unix.AF_INET6, v6(t, "::1"))}, nil), false},
		{"connected subnet route but no self address -> false (subnet is not self)",
			BuildSnapshot([]xtcpnl.AddrInfo{lo4}, []xtcpnl.RouteInfo{connectedRoute(unix.AF_INET, v4(t, "10.0.0.0"), 24)}, nil), false},
		{"RTN_LOCAL in an ignored policy table does not count -> false",
			BuildSnapshot([]xtcpnl.AddrInfo{lo4}, []xtcpnl.RouteInfo{inTable(localRoute(unix.AF_INET, v4(t, "10.1.2.3")), 100)}, nil), false},

		// boundary
		{"127.255.255.254 (top of loopback range) -> false",
			BuildSnapshot([]xtcpnl.AddrInfo{{Family: unix.AF_INET, Index: 1, Local: v4(t, "127.255.255.254")}}, nil, nil), false},
		{"128.0.0.1 (just past loopback range) -> true",
			BuildSnapshot([]xtcpnl.AddrInfo{{Family: unix.AF_INET, Index: 2, Local: v4(t, "128.0.0.1")}}, nil, nil), true},
		{"::2 (not the v6 loopback) -> true",
			BuildSnapshot([]xtcpnl.AddrInfo{{Family: unix.AF_INET6, Index: 2, Address: v6(t, "::2")}}, nil, nil), true},

		// corner
		{"local 127.0.0.0/7 route (wider than the loopback range) -> true",
			BuildSnapshot(nil, []xtcpnl.RouteInfo{localRangeRoute(unix.AF_INET, v4(t, "126.0.0.0"), 7)}, nil), true},
		{"local ::1/127 route (wider than ::1/128) -> true",
			BuildSnapshot(nil, []xtcpnl.RouteInfo{localRangeRoute(unix.AF_INET6, v6(t, "::"), 127)}, nil), true},
		{"unparseable address bytes are ignored -> false",
			BuildSnapshot([]xtcpnl.AddrInfo{{Family: unix.AF_INET, Index: 2, Local: []byte{1, 2, 3}}}, nil, nil), false},
	}
	for _, tc := range tests {
		t.Run(tc.description, func(t *testing.T) {
			if got := tc.snap.HasNonLoopbackSelf(); got != tc.want {
				t.Errorf("HasNonLoopbackSelf() = %v, want %v", got, tc.want)
			}
		})
	}
}
