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
// route to dst/bits.
func connectedRoute(family uint8, dst []byte, bits uint8) xtcpnl.RouteInfo {
	return xtcpnl.RouteInfo{
		Family: family,
		DstLen: bits,
		Type:   unix.RTN_UNICAST,
		Scope:  unix.RT_SCOPE_LINK,
		Dst:    dst,
	}
}

// gatewayRoute builds a route reached via a next-hop gateway (NOT connected).
func gatewayRoute(family uint8, dst []byte, bits uint8, gw []byte) xtcpnl.RouteInfo {
	return xtcpnl.RouteInfo{
		Family:  family,
		DstLen:  bits,
		Type:    unix.RTN_UNICAST,
		Scope:   unix.RT_SCOPE_UNIVERSE,
		Dst:     dst,
		Gateway: gw,
	}
}

// localRoute builds an RTN_LOCAL route (a locally-attached host address).
func localRoute(family uint8, dst []byte) xtcpnl.RouteInfo {
	return xtcpnl.RouteInfo{
		Family: family,
		DstLen: uint8(len(dst) * 8),
		Type:   unix.RTN_LOCAL,
		Scope:  unix.RT_SCOPE_HOST,
		Dst:    dst,
	}
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
	snap := BuildSnapshot(addrs, routes)

	tests := []struct {
		description string
		addr        string
		want        Locality
	}{
		// positive
		{"self IPv4 address (IFA_LOCAL) -> self", "10.0.0.5", LocalitySelf},
		{"self IPv6 address -> self", "2001:db8::5", LocalitySelf},
		{"RTN_LOCAL host route dest -> self", "172.16.0.1", LocalitySelf},
		{"peer in connected IPv4 subnet -> connected_subnet", "10.0.0.42", LocalitySubnet},
		{"peer in connected IPv6 subnet -> connected_subnet", "2001:db8::1234", LocalitySubnet},
		// negative
		{"public IPv4 not in any set -> remote", "8.8.8.8", LocalityRemote},
		{"public IPv6 not in any set -> remote", "2606:4700::1111", LocalityRemote},
		{"address only reachable via gateway -> remote", "93.184.216.34", LocalityRemote},
		{"IPv4 just outside connected /24 -> remote", "10.0.1.1", LocalityRemote},
		// boundary
		{"network address of connected subnet -> connected_subnet", "10.0.0.0", LocalitySubnet},
		{"broadcast-ish last host of /24 -> connected_subnet", "10.0.0.255", LocalitySubnet},
		{"self host /32 wins over containing /24 subnet", "10.0.0.5", LocalitySelf},
		// corner
		{"IPv4 loopback short-circuits -> self", "127.0.0.1", LocalitySelf},
		{"IPv6 loopback short-circuits -> self", "::1", LocalitySelf},
		{"IPv4 unspecified short-circuits -> self", "0.0.0.0", LocalitySelf},
		{"IPv6 unspecified short-circuits -> self", "::", LocalitySelf},
		{"IPv4-mapped IPv6 of a self address -> self", "::ffff:10.0.0.5", LocalitySelf},
		{"IPv4-mapped IPv6 of a subnet peer -> connected_subnet", "::ffff:10.0.0.9", LocalitySubnet},
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
	empty := BuildSnapshot(nil, nil)

	tests := []struct {
		description string
		snap        *Snapshot
		addr        netip.Addr
		want        Locality
	}{
		{"invalid zero address -> unspecified", empty, netip.Addr{}, LocalityUnspecified},
		{"nil snapshot, valid remote address -> remote", nil, netip.MustParseAddr("8.8.8.8"), LocalityRemote},
		{"nil snapshot, loopback still short-circuits -> self", nil, netip.MustParseAddr("127.0.0.1"), LocalitySelf},
		{"empty snapshot, valid address -> remote", empty, netip.MustParseAddr("10.0.0.5"), LocalityRemote},
		{"empty snapshot, invalid address -> unspecified", empty, netip.Addr{}, LocalityUnspecified},
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
				Family: unix.AF_INET6, DstLen: 64, Type: unix.RTN_UNICAST,
				Scope: unix.RT_SCOPE_UNIVERSE, Dst: v6(t, "fd10:10:4::"),
			}},
			probe: "fd10:10:4::7",
			want:  LocalitySubnet,
		},
		// negative
		{
			description: "route with a gateway is NOT a connected subnet",
			routes:      []xtcpnl.RouteInfo{gatewayRoute(unix.AF_INET, v4(t, "192.168.0.0"), 24, v4(t, "192.168.0.1"))},
			probe:       "192.168.0.7",
			want:        LocalityRemote,
		},
		// boundary
		{
			description: "/32 connected route classifies only that host",
			routes:      []xtcpnl.RouteInfo{connectedRoute(unix.AF_INET, v4(t, "10.9.9.9"), 32)},
			probe:       "10.9.9.9",
			want:        LocalitySubnet,
		},
		{
			description: "/32 connected route does not cover a neighbour",
			routes:      []xtcpnl.RouteInfo{connectedRoute(unix.AF_INET, v4(t, "10.9.9.9"), 32)},
			probe:       "10.9.9.10",
			want:        LocalityRemote,
		},
		{
			description: "scope-link /0 route is dropped (must not swallow everything)",
			routes:      []xtcpnl.RouteInfo{connectedRoute(unix.AF_INET, v4(t, "0.0.0.0"), 0)},
			probe:       "8.8.8.8",
			want:        LocalityRemote,
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
			description: "zero-length Dst route skipped, address stays remote",
			routes:      []xtcpnl.RouteInfo{{Family: unix.AF_INET, DstLen: 24, Type: unix.RTN_UNICAST, Scope: unix.RT_SCOPE_LINK}},
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
			snap := BuildSnapshot(tc.addrs, tc.routes)
			got := snap.Classify(netip.MustParseAddr(tc.probe))
			if got != tc.want {
				t.Errorf("Classify(%s) = %v, want %v", tc.probe, got, tc.want)
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
		{"subnet", LocalitySubnet, "connected_subnet"},
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
