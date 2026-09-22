package localnet

import (
	"net/netip"
	"os"
	"testing"

	"golang.org/x/sys/unix"

	"github.com/randomizedcoder/xtcp2/pkg/xtcpnl"
)

// This test drives the full feature end-to-end on REAL kernel bytes: it reads
// the same nlmon-captured 7.1.8 dump fixtures the xtcpnl parser tests use
// (pkg/xtcpnl/testdata/7_1_8, produced by nix run .#capture-netlink-fixtures),
// parses the RTM_NEWADDR + RTM_NEWROUTE messages with xtcpnl.ParseNewAddr /
// ParseNewRoute, feeds them to BuildSnapshot, and asserts Classify against the
// real addresses/subnets transcribed from that host's ip_addr_n /
// ip_route_table_all_n. It is the guard that the real wire format actually
// classifies the way the synthetic TestClassify assumes — in particular that
// IPv6 connected subnets (scope UNIVERSE, not LINK) resolve to LocalitySubnet.
//
// Fixtures live in the xtcpnl package; reference them by relative path.
const fixtureDir = "../xtcpnl/testdata/7_1_8"

// walkRealDump walks a committed *_dump.pcap the same way xtcpnl.DumpRtnetlink
// does at runtime — from PcapNetlinkOffsetCst, 4-byte-aligned nlmsghdr steps,
// stopping at NLMSG_DONE — invoking fn with each RTM_NEW* body. It reuses only
// xtcpnl's exported wire primitives so localnet has no dependency on xtcpnl test
// internals.
func walkRealDump(t *testing.T, name string, fn func(mtype uint16, body []byte)) {
	t.Helper()
	bs, err := os.ReadFile(fixtureDir + "/" + name)
	if err != nil {
		t.Fatalf("ReadFile(%s): %v", name, err)
	}
	if len(bs) < xtcpnl.PcapNetlinkOffsetCst {
		t.Fatalf("%s: fixture too small (%d bytes)", name, len(bs))
	}
	data := bs[xtcpnl.PcapNetlinkOffsetCst:]
	sawDone := false
	for len(data) >= xtcpnl.NlMsgHdrSizeCst {
		var h xtcpnl.NlMsgHdr
		if _, derr := xtcpnl.DeserializeNlMsgHdr(data, &h); derr != nil {
			t.Fatalf("%s: DeserializeNlMsgHdr: %v", name, derr)
		}
		mlen := int(h.Len)
		if mlen < xtcpnl.NlMsgHdrSizeCst || mlen > len(data) {
			t.Fatalf("%s: bad nlmsg_len %d (remaining %d)", name, mlen, len(data))
		}
		if h.Type == uint16(unix.NLMSG_DONE) {
			sawDone = true
			break
		}
		fn(h.Type, data[xtcpnl.NlMsgHdrSizeCst:mlen])
		adv := mlen + xtcpnl.FourByteAlignPadding(mlen)
		if adv <= 0 || adv > len(data) {
			break
		}
		data = data[adv:]
	}
	if !sawDone {
		t.Fatalf("%s: dump not terminated by NLMSG_DONE", name)
	}
}

// buildRealSnapshot parses the v4+v6 address dumps and the route dump into a
// Snapshot the same way the runtime locality refresh will.
func buildRealSnapshot(t *testing.T) *Snapshot {
	t.Helper()
	var addrs []xtcpnl.AddrInfo
	for _, f := range []string{"netlink_route_getaddr_v4_dump.pcap", "netlink_route_getaddr_v6_dump.pcap"} {
		walkRealDump(t, f, func(mt uint16, body []byte) {
			if mt != uint16(unix.RTM_NEWADDR) {
				return
			}
			ai, err := xtcpnl.ParseNewAddr(body)
			if err != nil {
				t.Fatalf("%s: ParseNewAddr: %v", f, err)
			}
			addrs = append(addrs, ai)
		})
	}
	var routes []xtcpnl.RouteInfo
	walkRealDump(t, "netlink_route_getroute_dump.pcap", func(mt uint16, body []byte) {
		if mt != uint16(unix.RTM_NEWROUTE) {
			return
		}
		ri, err := xtcpnl.ParseNewRoute(body)
		if err != nil {
			t.Fatalf("getroute: ParseNewRoute: %v", err)
		}
		routes = append(routes, ri)
	})

	links := make(map[uint32]string)
	walkRealDump(t, "netlink_route_getlink_dump.pcap", func(mt uint16, body []byte) {
		if mt != uint16(unix.RTM_NEWLINK) {
			return
		}
		li, err := xtcpnl.ParseNewLink(body)
		if err != nil {
			t.Fatalf("getlink: ParseNewLink: %v", err)
		}
		links[uint32(li.Index)] = li.Name
	})

	if len(addrs) != 24 { // 9 v4 + 15 v6
		t.Fatalf("parsed %d addresses, want 24", len(addrs))
	}
	if len(routes) != 74 {
		t.Fatalf("parsed %d routes, want 74", len(routes))
	}
	// ip_link_n sidecar: 1=lo, 2=enp1s0, 3=enp35s0f0np0 must be present.
	for idx, name := range map[uint32]string{1: "lo", 2: "enp1s0", 3: "enp35s0f0np0"} {
		if links[idx] != name {
			t.Fatalf("links[%d] = %q, want %q", idx, links[idx], name)
		}
	}
	return BuildSnapshot(addrs, routes, links)
}

// TestClassifyRealFixture classifies real destination addresses against a
// Snapshot built from this host's captured dumps. Every row cites the sidecar
// line the address/subnet came from.
//
// go test ./pkg/localnet/ -run TestClassifyRealFixture
func TestClassifyRealFixture(t *testing.T) {
	snap := buildRealSnapshot(t)

	tests := []struct {
		description string
		addr        string
		want        Locality
	}{
		// positive — self (host's own addresses; RTN_LOCAL and/or IFA_LOCAL)
		{"ip_addr_n:10 self v4 172.16.50.219 -> self", "172.16.50.219", LocalitySelf},
		{"ip_addr_n:23 self v4 10.10.4.2 -> self", "10.10.4.2", LocalitySelf},
		{"ip_addr_n:25 self v6 fd10:10:4::2 -> self", "fd10:10:4::2", LocalitySelf},
		{"ip_addr_n:16 self v6 2603:…:6adf:8a2f:21ae:d6a7 -> self", "2603:8002:ea00:6800:6adf:8a2f:21ae:d6a7", LocalitySelf},

		// positive — connected subnet (peer inside an on-link prefix)
		{"ip_route:2 peer in v4 connected 10.10.4.0/29 -> subnet", "10.10.4.5", LocalitySubnet},
		{"ip_route:6 peer in v4 connected 172.16.50.0/24 -> subnet", "172.16.50.100", LocalitySubnet},
		{"ip_route:29 peer in v6 connected fd10:10:4::/64 (scope UNIVERSE) -> subnet", "fd10:10:4::abcd", LocalitySubnet},
		{"ip_route:27 peer in v6 connected 2603:…:6800::/64 -> subnet", "2603:8002:ea00:6800::5", LocalitySubnet},

		// negative — remote
		{"public v4 not in any prefix -> remote", "8.8.8.8", LocalityRemote},
		{"public v6 not in any prefix -> remote", "2606:4700::1111", LocalityRemote},
		{"v4 just outside connected 10.10.4.0/29 (.8) -> remote", "10.10.4.8", LocalityRemote},

		// boundary — self /32 & /128 win over the containing connected subnet
		{"self /32 10.10.4.2 wins over 10.10.4.0/29 subnet", "10.10.4.2", LocalitySelf},
		{"self /128 fd10:10:4::2 wins over fd10:10:4::/64 subnet", "fd10:10:4::2", LocalitySelf},

		// corner — loopback short-circuits regardless of snapshot contents
		{"v4 loopback -> self", "127.0.0.1", LocalitySelf},
		{"v6 loopback -> self", "::1", LocalitySelf},
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

// TestLookupEgressRealFixture asserts the egress interface derived from the real
// routing table: the Oif of the route each destination longest-prefix matches,
// resolved to a name via the RTM_GETLINK dump. Every row cites the
// ip_route_table_all_n line (dev <iface>) the egress came from. Interface
// indices from ip_link_n: enp1s0=2, enp35s0f0np0=3, lo=1.
//
// go test ./pkg/localnet/ -run TestLookupEgressRealFixture
func TestLookupEgressRealFixture(t *testing.T) {
	snap := buildRealSnapshot(t)

	tests := []struct {
		description string
		addr        string
		wantLoc     Locality
		wantIfindex uint32
		wantIfname  string
	}{
		// connected subnets -> egress is the subnet's dev
		{"ip_route:2 10.10.4.5 in 10.10.4.0/29 dev enp35s0f0np0", "10.10.4.5", LocalitySubnet, 3, "enp35s0f0np0"},
		{"ip_route:6 172.16.50.100 in 172.16.50.0/24 dev enp1s0", "172.16.50.100", LocalitySubnet, 2, "enp1s0"},
		{"ip_route:29 fd10:10:4::abcd in fd10:10:4::/64 dev enp35s0f0np0", "fd10:10:4::abcd", LocalitySubnet, 3, "enp35s0f0np0"},
		{"ip_route:27 2603:…:6800::5 in 2603:8002:ea00:6800::/64 dev enp1s0", "2603:8002:ea00:6800::5", LocalitySubnet, 2, "enp1s0"},
		// remote -> egress from the matched default route
		{"ip_route:1 8.8.8.8 via v4 default dev enp1s0", "8.8.8.8", LocalityRemote, 2, "enp1s0"},
		{"ip_route:39 2606:4700::1111 via v6 default dev enp1s0", "2606:4700::1111", LocalityRemote, 2, "enp1s0"},
		// self -> egress from the owning interface / RTN_LOCAL route dev
		{"ip_route:19 self 172.16.50.219 dev enp1s0", "172.16.50.219", LocalitySelf, 2, "enp1s0"},
		{"ip_route:10 self 10.10.4.2 dev enp35s0f0np0", "10.10.4.2", LocalitySelf, 3, "enp35s0f0np0"},
	}

	for _, tc := range tests {
		t.Run(tc.description, func(t *testing.T) {
			loc, oif, ok := snap.Lookup(netip.MustParseAddr(tc.addr))
			if !ok || loc != tc.wantLoc || oif != tc.wantIfindex {
				t.Errorf("Lookup(%s) = (%v, %d, %v), want (%v, %d, true)",
					tc.addr, loc, oif, ok, tc.wantLoc, tc.wantIfindex)
			}
			if name := snap.IfName(oif); name != tc.wantIfname {
				t.Errorf("IfName(%d) = %q, want %q", oif, name, tc.wantIfname)
			}
		})
	}
}
