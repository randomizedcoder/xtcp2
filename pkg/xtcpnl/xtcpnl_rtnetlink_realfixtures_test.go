package xtcpnl

import (
	"os"
	"reflect"
	"testing"

	"golang.org/x/sys/unix"
)

// This file holds the REAL-fixture-driven deserialize tests for the rtnetlink
// dump parsers. Unlike xtcpnl_rtnetlink_test.go (which synthesises exact
// positive/negative/boundary/corner wire bytes in-code), these tests read the
// committed multi-message dump fixtures captured on a live 7.1.8 kernel with
// nlmon (see xtcpnl_extract_7_1_8_fixtures_test.go and
// nix/capture-netlink-fixtures.nix) and assert that ParseNewLink / ParseNewAddr
// / ParseNewRoute decode the real kernel bytes into the structs transcribed from
// the ip_link_n / ip_addr_n / ip_route_table_all_n source-of-truth sidecars.
//
// Every expected value below cites the sidecar line it came from, exactly how
// the TCPInfo cases cite ss_tcp_info_n. The fixtures are walked the same way the
// runtime DumpRtnetlink transport does: from PcapNetlinkOffsetCst through
// walkDumpPayload, stopping at NLMSG_DONE.
//
// Interface index -> name (ip_link_n): 1 lo, 2 enp1s0, 3 enp35s0f0np0,
// 4 enp35s0f1np1, 7 virbr0, 8 docker0, 9 br-3a5828b2963a, 58 ve-nfb-vpn,
// 59 ve-nordlayepDd-, 60 veth179a698, 161 nlmon0.

// readDumpFixture reads a committed *_dump.pcap, walks its multipart payload the
// same way DumpRtnetlink does (slice from PcapNetlinkOffsetCst, then
// walkDumpPayload), and returns a copy of every RTM_NEW* body of wantType along
// with whether the dump was terminated by NLMSG_DONE.
func readDumpFixture(t *testing.T, path string, wantType uint16) (bodies [][]byte, sawDone bool) {
	t.Helper()
	bs, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile(%s): %v", path, err)
	}
	if len(bs) < PcapNetlinkOffsetCst {
		t.Fatalf("%s: fixture too small (%d bytes)", path, len(bs))
	}
	walkDumpPayload(bs[PcapNetlinkOffsetCst:], func(mt uint16, body []byte) bool {
		if mt == uint16(unix.NLMSG_DONE) {
			sawDone = true
			return false
		}
		if mt == wantType {
			bodies = append(bodies, append([]byte(nil), body...))
		}
		return true
	})
	return bodies, sawDone
}

// countDeepEqual returns how many parsed entries deep-equal want.
func countDeepEqual[T any](items []T, want T) int {
	n := 0
	for _, it := range items {
		if reflect.DeepEqual(it, want) {
			n++
		}
	}
	return n
}

// TestParseNewLinkRealFixture parses the real RTM_NEWLINK dump and asserts the
// interface index/flags/name for representative links against ip_link_n.
//
// go test ./pkg/xtcpnl/ -run TestParseNewLinkRealFixture
func TestParseNewLinkRealFixture(t *testing.T) {
	bodies, sawDone := readDumpFixture(t, tdRouteGetLinkDump_7_1_8, uint16(unix.RTM_NEWLINK))
	if !sawDone {
		t.Fatalf("%s: dump not terminated by NLMSG_DONE", tdRouteGetLinkDump_7_1_8)
	}
	// Fixture integrity: the capture holds exactly the 11 links in ip_link_n.
	if len(bodies) != 11 {
		t.Fatalf("RTM_NEWLINK count = %d, want 11", len(bodies))
	}

	links := make([]LinkInfo, 0, len(bodies))
	for i, b := range bodies {
		li, err := ParseNewLink(b)
		if err != nil {
			t.Fatalf("ParseNewLink(msg %d): %v", i, err)
		}
		links = append(links, li)
	}

	tests := []struct {
		description string
		want        LinkInfo
	}{
		{
			// ip_link_n:1  "1: lo: <LOOPBACK,UP,LOWER_UP>"
			description: "positive: loopback lo, index 1, IFF_UP|IFF_LOOPBACK set",
			want:        LinkInfo{Index: 1, Flags: 0x10049, Name: "lo"},
		},
		{
			// ip_link_n:3  "2: enp1s0: <BROADCAST,MULTICAST,UP,LOWER_UP>"
			description: "positive: primary NIC enp1s0, index 2",
			want:        LinkInfo{Index: 2, Flags: 0x11043, Name: "enp1s0"},
		},
		{
			// ip_link_n:6  "3: enp35s0f0np0: <BROADCAST,MULTICAST,UP,LOWER_UP>"
			description: "positive: NIC enp35s0f0np0, index 3",
			want:        LinkInfo{Index: 3, Flags: 0x11043, Name: "enp35s0f0np0"},
		},
		{
			// ip_link_n:24 "59: ve-nordlayepDd-@if2" — kernel truncates the name
			// at IFNAMSIZ, so the dump carries the truncated form, not the altname.
			description: "corner: long veth name truncated by the kernel, index 59",
			want:        LinkInfo{Index: 59, Flags: 0x11043, Name: "ve-nordlayepDd-"},
		},
		{
			// ip_link_n:32 "161: nlmon0: <NOARP,UP,LOWER_UP>" — the monitor iface
			// the capture itself created; NOARP set, no BROADCAST/MULTICAST.
			description: "corner: the capture's own nlmon0 monitor iface, index 161",
			want:        LinkInfo{Index: 161, Flags: 0x100c1, Name: "nlmon0"},
		},
	}

	for _, tc := range tests {
		t.Run(tc.description, func(t *testing.T) {
			if n := countDeepEqual(links, tc.want); n != 1 {
				t.Errorf("found %d links equal to %+v, want exactly 1\nall links: %+v", n, tc.want, links)
			}
		})
	}
}

// TestParseNewAddrRealFixtures parses the real RTM_NEWADDR v4 and v6 dumps and
// asserts representative addresses against ip_addr_n. It exercises the IPv4
// path (kernel sends both IFA_ADDRESS and IFA_LOCAL, plus IFA_LABEL) and the
// IPv6 path (IFA_ADDRESS only, no label), across host/global/link scopes.
//
// go test ./pkg/xtcpnl/ -run TestParseNewAddrRealFixtures
func TestParseNewAddrRealFixtures(t *testing.T) {
	type fixture struct {
		path      string
		wantCount int
	}
	v4 := fixture{tdRouteGetAddrV4Dump_7_1_8, 9}
	v6 := fixture{tdRouteGetAddrV6Dump_7_1_8, 15}

	parsed := map[string][]AddrInfo{}
	for _, f := range []fixture{v4, v6} {
		bodies, sawDone := readDumpFixture(t, f.path, uint16(unix.RTM_NEWADDR))
		if !sawDone {
			t.Fatalf("%s: dump not terminated by NLMSG_DONE", f.path)
		}
		if len(bodies) != f.wantCount {
			t.Fatalf("%s: RTM_NEWADDR count = %d, want %d", f.path, len(bodies), f.wantCount)
		}
		addrs := make([]AddrInfo, 0, len(bodies))
		for i, b := range bodies {
			ai, err := ParseNewAddr(b)
			if err != nil {
				t.Fatalf("%s: ParseNewAddr(msg %d): %v", f.path, i, err)
			}
			addrs = append(addrs, ai)
		}
		parsed[f.path] = addrs
	}

	tests := []struct {
		description string
		fixture     string
		want        AddrInfo
	}{
		{
			// ip_addr_n:3  "inet 127.0.0.1/8 scope host lo"
			description: "positive v4: loopback 127.0.0.1/8 scope host on lo (idx 1)",
			fixture:     v4.path,
			want: AddrInfo{
				Family: unix.AF_INET, Prefixlen: 8, Scope: unix.RT_SCOPE_HOST, Index: 1,
				Address: v4b(127, 0, 0, 1), Local: v4b(127, 0, 0, 1), Label: "lo",
			},
		},
		{
			// ip_addr_n:10 "inet 172.16.50.219/24 ... scope global ... enp1s0"
			description: "positive v4: global 172.16.50.219/24 on enp1s0 (idx 2)",
			fixture:     v4.path,
			want: AddrInfo{
				Family: unix.AF_INET, Prefixlen: 24, Scope: unix.RT_SCOPE_UNIVERSE, Index: 2,
				Address: v4b(172, 16, 50, 219), Local: v4b(172, 16, 50, 219), Label: "enp1s0",
			},
		},
		{
			// ip_addr_n:23 "inet 10.10.4.2/29 scope global enp35s0f0np0"
			description: "positive v4: connected-subnet host 10.10.4.2/29 on enp35s0f0np0 (idx 3)",
			fixture:     v4.path,
			want: AddrInfo{
				Family: unix.AF_INET, Prefixlen: 29, Scope: unix.RT_SCOPE_UNIVERSE, Index: 3,
				Address: v4b(10, 10, 4, 2), Local: v4b(10, 10, 4, 2), Label: "enp35s0f0np0",
			},
		},
		{
			// ip_addr_n:62 "inet 10.98.0.1/32 scope global ve-nfb-vpn"
			description: "boundary v4: /32 host address 10.98.0.1 on veth (idx 58)",
			fixture:     v4.path,
			want: AddrInfo{
				Family: unix.AF_INET, Prefixlen: 32, Scope: unix.RT_SCOPE_UNIVERSE, Index: 58,
				Address: v4b(10, 98, 0, 1), Local: v4b(10, 98, 0, 1), Label: "ve-nfb-vpn",
			},
		},
		{
			// ip_addr_n:5  "inet6 ::1/128 scope host" — v6 carries IFA_ADDRESS
			// only (Local nil) and no IFA_LABEL.
			description: "boundary v6: loopback ::1/128 scope host on lo (idx 1), no local/label",
			fixture:     v6.path,
			want: AddrInfo{
				Family: unix.AF_INET6, Prefixlen: 128, Scope: unix.RT_SCOPE_HOST, Index: 1,
				Address: mustV6(t, "::1"),
			},
		},
		{
			// ip_addr_n:25 "inet6 fd10:10:4::2/64 scope global nodad"
			description: "positive v6: ULA fd10:10:4::2/64 scope global on enp35s0f0np0 (idx 3)",
			fixture:     v6.path,
			want: AddrInfo{
				Family: unix.AF_INET6, Prefixlen: 64, Scope: unix.RT_SCOPE_UNIVERSE, Index: 3,
				Address: mustV6(t, "fd10:10:4::2"),
			},
		},
		{
			// ip_addr_n:18 "inet6 fe80::b5c8:b23e:9a98:a37c/64 scope link"
			description: "positive v6: link-local fe80::…a37c/64 scope link on enp1s0 (idx 2)",
			fixture:     v6.path,
			want: AddrInfo{
				Family: unix.AF_INET6, Prefixlen: 64, Scope: unix.RT_SCOPE_LINK, Index: 2,
				Address: mustV6(t, "fe80::b5c8:b23e:9a98:a37c"),
			},
		},
	}

	for _, tc := range tests {
		t.Run(tc.description, func(t *testing.T) {
			if n := countDeepEqual(parsed[tc.fixture], tc.want); n != 1 {
				t.Errorf("found %d addrs equal to %+v, want exactly 1", n, tc.want)
			}
		})
	}
}

// TestParseNewRouteRealFixture parses the real RTM_NEWROUTE dump (main + local
// tables, v4 + v6) and asserts representative routes against
// ip_route_table_all_n: a connected subnet, a default via gateway, and a local
// host route, for both families.
//
// go test ./pkg/xtcpnl/ -run TestParseNewRouteRealFixture
func TestParseNewRouteRealFixture(t *testing.T) {
	bodies, sawDone := readDumpFixture(t, tdRouteGetRouteDump_7_1_8, uint16(unix.RTM_NEWROUTE))
	if !sawDone {
		t.Fatalf("%s: dump not terminated by NLMSG_DONE", tdRouteGetRouteDump_7_1_8)
	}
	// Fixture integrity: 26 IPv4 + 48 IPv6 routes across main and local tables.
	if len(bodies) != 74 {
		t.Fatalf("RTM_NEWROUTE count = %d, want 74", len(bodies))
	}
	routes := make([]RouteInfo, 0, len(bodies))
	for i, b := range bodies {
		ri, err := ParseNewRoute(b)
		if err != nil {
			t.Fatalf("ParseNewRoute(msg %d): %v", i, err)
		}
		routes = append(routes, ri)
	}

	tests := []struct {
		description string
		want        RouteInfo
	}{
		{
			// ip_route_table_all_n:1
			// "unicast default via 172.16.50.1 dev enp1s0 table main proto dhcp
			//  scope global src 172.16.50.219 metric 100"
			description: "positive v4: default route via gateway (no RTA_DST, DstLen 0)",
			want: RouteInfo{
				Family: unix.AF_INET, DstLen: 0, Table: unix.RT_TABLE_MAIN,
				Scope: unix.RT_SCOPE_UNIVERSE, Type: unix.RTN_UNICAST, Protocol: unix.RTPROT_DHCP,
				Gateway: v4b(172, 16, 50, 1), PrefSrc: v4b(172, 16, 50, 219), Oif: 2, Priority: 100,
			},
		},
		{
			// ip_route_table_all_n:2
			// "unicast 10.10.4.0/29 dev enp35s0f0np0 table main proto kernel
			//  scope link src 10.10.4.2"
			description: "positive v4: connected subnet 10.10.4.0/29 (scope LINK, unicast, no gateway)",
			want: RouteInfo{
				Family: unix.AF_INET, DstLen: 29, Table: unix.RT_TABLE_MAIN,
				Scope: unix.RT_SCOPE_LINK, Type: unix.RTN_UNICAST, Protocol: unix.RTPROT_KERNEL,
				Dst: v4b(10, 10, 4, 0), PrefSrc: v4b(10, 10, 4, 2), Oif: 3,
			},
		},
		{
			// ip_route_table_all_n:16
			// "local 127.0.0.0/8 dev lo table local proto kernel scope host
			//  src 127.0.0.1"
			description: "positive v4: local route 127.0.0.0/8 (type LOCAL, table LOCAL, scope HOST)",
			want: RouteInfo{
				Family: unix.AF_INET, DstLen: 8, Table: unix.RT_TABLE_LOCAL,
				Scope: unix.RT_SCOPE_HOST, Type: unix.RTN_LOCAL, Protocol: unix.RTPROT_KERNEL,
				Dst: v4b(127, 0, 0, 0), PrefSrc: v4b(127, 0, 0, 1), Oif: 1,
			},
		},
		{
			// ip_route_table_all_n:29
			// "unicast fd10:10:4::/64 dev enp35s0f0np0 table main proto kernel
			//  scope global metric 256 pref medium"
			//
			// The bug-catching case: an IPv6 connected subnet is scope GLOBAL
			// (RT_SCOPE_UNIVERSE=0), NOT scope-link. localnet's connected-subnet
			// rule must therefore key on unicast+no-gateway+has-Dst, not scope.
			description: "positive v6: connected subnet fd10:10:4::/64 (scope UNIVERSE, unicast, no gateway)",
			want: RouteInfo{
				Family: unix.AF_INET6, DstLen: 64, Table: unix.RT_TABLE_MAIN,
				Scope: unix.RT_SCOPE_UNIVERSE, Type: unix.RTN_UNICAST, Protocol: unix.RTPROT_KERNEL,
				Dst: mustV6(t, "fd10:10:4::"), Oif: 3, Priority: 256,
			},
		},
		{
			// ip_route_table_all_n:39
			// "unicast default via fe80::e638:83ff:fe36:8f0d dev enp1s0 table main
			//  proto ra scope global metric 100 pref high"
			description: "positive v6: default route via link-local gateway (DstLen 0)",
			want: RouteInfo{
				Family: unix.AF_INET6, DstLen: 0, Table: unix.RT_TABLE_MAIN,
				Scope: unix.RT_SCOPE_UNIVERSE, Type: unix.RTN_UNICAST, Protocol: unix.RTPROT_RA,
				Gateway: mustV6(t, "fe80::e638:83ff:fe36:8f0d"), Oif: 2, Priority: 100,
			},
		},
		{
			// ip_route_table_all_n:40
			// "local ::1 dev lo table local proto kernel scope global metric 0"
			// Note: unlike the IPv4 loopback local route (scope HOST), the v6 ::1
			// local route is scope GLOBAL.
			description: "boundary v6: local host route ::1/128 (type LOCAL, table LOCAL, scope GLOBAL)",
			want: RouteInfo{
				Family: unix.AF_INET6, DstLen: 128, Table: unix.RT_TABLE_LOCAL,
				Scope: unix.RT_SCOPE_UNIVERSE, Type: unix.RTN_LOCAL, Protocol: unix.RTPROT_KERNEL,
				Dst: mustV6(t, "::1"), Oif: 1,
			},
		},
	}

	for _, tc := range tests {
		t.Run(tc.description, func(t *testing.T) {
			if n := countDeepEqual(routes, tc.want); n != 1 {
				t.Errorf("found %d routes equal to %+v, want exactly 1", n, tc.want)
			}
		})
	}
}

// TestRealFixtureConnectedSubnetScope documents and guards the finding that
// distinguishes a connected subnet by family: IPv4 connected subnets carry
// scope RT_SCOPE_LINK while IPv6 connected subnets carry scope RT_SCOPE_UNIVERSE.
// A scope-gated connected-subnet rule would silently misclassify every IPv6
// on-link subnet as REMOTE — this test fails if that asymmetry ever regresses in
// the fixtures, keeping localnet.BuildSnapshot's family-agnostic rule honest.
//
// go test ./pkg/xtcpnl/ -run TestRealFixtureConnectedSubnetScope
func TestRealFixtureConnectedSubnetScope(t *testing.T) {
	bodies, _ := readDumpFixture(t, tdRouteGetRouteDump_7_1_8, uint16(unix.RTM_NEWROUTE))

	// connected returns the parsed connected-subnet route (unicast, no gateway,
	// has a Dst prefix, in the main table) whose Dst equals wantDst.
	connected := func(wantDst []byte) (RouteInfo, bool) {
		for _, b := range bodies {
			ri, err := ParseNewRoute(b)
			if err != nil {
				continue
			}
			if ri.Type != unix.RTN_UNICAST || len(ri.Gateway) != 0 || len(ri.Dst) == 0 {
				continue
			}
			if ri.Table == unix.RT_TABLE_MAIN && reflect.DeepEqual(ri.Dst, wantDst) {
				return ri, true
			}
		}
		return RouteInfo{}, false
	}

	tests := []struct {
		description string
		dst         []byte
		wantScope   uint8
	}{
		{
			description: "IPv4 connected subnet 10.10.4.0/29 is scope RT_SCOPE_LINK",
			dst:         v4b(10, 10, 4, 0),
			wantScope:   unix.RT_SCOPE_LINK,
		},
		{
			description: "IPv6 connected subnet fd10:10:4::/64 is scope RT_SCOPE_UNIVERSE (NOT link)",
			dst:         mustV6(t, "fd10:10:4::"),
			wantScope:   unix.RT_SCOPE_UNIVERSE,
		},
	}

	for _, tc := range tests {
		t.Run(tc.description, func(t *testing.T) {
			ri, ok := connected(tc.dst)
			if !ok {
				t.Fatalf("connected route for %v not found in fixture", tc.dst)
			}
			if ri.Scope != tc.wantScope {
				t.Errorf("scope = %d, want %d", ri.Scope, tc.wantScope)
			}
		})
	}
}
