package xtcpnl

// WARNING: this file contains Go reflection (binary.Read / reflect).
//
// The reflection code here is only for performance comparison, and it is
// strongly recommended that it is NOT used in production. It lives in a
// _test.go file so that it never reaches the shipped library: pkg/xtcpnl
// ships zero reflection, and every production Deserialize* reads fields at
// fixed byte offsets instead.
//
// If reflection is ever measured as even close to a manual decoder, that
// indicates a problem rather than a license to use it. See
// xtcpnl_reflection_twins_test.go for the rationale and
// xtcpnl_perf_gate_test.go for the gate that fails on convergence.

import (
	"os"
	"reflect"
	"testing"

	"golang.org/x/sys/unix"
)

// This file holds the REAL-fixture-driven deserialize tests for the rtnetlink
// dump parsers. Unlike xtcpnl_rtnetlink_test.go (which synthesizes exact
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

	// Detail is cleared before comparison, so the rows below say nothing
	// about it.
	//
	// Not a convenience. This test identifies a link by stating ALL of the
	// fields a link stanza renders from, which is what makes it catch a
	// decoder that put the right value in the wrong field. Keeping that
	// property means each want literal has to stay complete — and a complete
	// one now has to spell out twenty-odd `ip -d` attributes per row, on
	// eleven rows, none of which is what any row here is asking about.
	//
	// The detail group has its own fixture test over the same dump and the
	// same sidecar: TestParseNewLinkDetailRealFixture. Splitting them is what
	// keeps each want literal short enough to be checked by eye against the
	// line of ip_link_n quoted above it.
	links := make([]LinkInfo, 0, len(bodies))
	for i, b := range bodies {
		li, err := ParseNewLink(b)
		if err != nil {
			t.Fatalf("ParseNewLink(msg %d): %v", i, err)
		}
		li.Detail = LinkDetail{}
		links = append(links, li)
	}

	// Every row below carries HasOperState, HasCarrier, HasMTU and
	// HasLinkMode set, on all eleven links, and that uniformity is a finding
	// rather than boilerplate: rtnl_fill_ifinfo puts all four inside one
	// unconditional `if (... || ...)` chain — IFLA_OPERSTATE, IFLA_LINKMODE
	// and IFLA_MTU at net/core/rtnetlink.c:2083,2086,2088 and IFLA_CARRIER at
	// :2117 — so an RTM_GETLINK dump reply always carries them.
	//
	// The consequence is that *absence* of any of the four is unreachable
	// from this corpus, and the constructed-bytes rows in TestParseNewLink
	// are the only place it can be asserted at all. That is the division of
	// labor between the two tables: real captures own the values, constructed
	// bytes own the shapes a kernel does not produce. Deleting the
	// constructed rows on the grounds that real captures now exist would
	// leave the presence flags untested in the one direction that matters.
	//
	// IFLA_TXQLEN is in that same chain (:2082) and yet HasTxQLen has a
	// reachable false: `ip -6 addr show`'s link dump is answered by
	// inet6_dump_ifinfo, not rtnl_fill_ifinfo, and that one omits it. Which
	// fill function answered is therefore part of what these flags record.
	tests := []struct {
		description string
		want        LinkInfo
	}{
		{
			// ip_link_n:1  "1: lo: <LOOPBACK,UP,LOWER_UP> mtu 65536 qdisc noqueue
			//               state UNKNOWN mode DEFAULT group default qlen 1000"
			// ip_link_n:2  "    link/loopback 00:00:00:00:00:00 brd 00:00:00:00:00:00"
			// -> ifi_type ARPHRD_LOOPBACK (772).
			//
			// Every rendered token on those two lines is asserted here, which is
			// the point: qdisc, mode, group and qlen each come from a separate
			// attribute, and "mode DEFAULT group default" is LinkMode 0 / Group 0
			// — values a "only set it if still zero" guard could not tell from an
			// absent attribute.
			//
			// A loopback reports state UNKNOWN, i.e. IF_OPER_UNKNOWN, even though
			// it is perfectly usable — which is exactly why IsUp() reads ifi_flags
			// rather than IFLA_OPERSTATE.
			description: "positive: loopback lo renders every token of ip_link_n:1-2",
			want: LinkInfo{
				Index: 1, Flags: 0x10049, Name: "lo", Type: 772,
				OperState: IfOperUnknown, HasOperState: true,
				Carrier: 1, HasCarrier: true,
				MTU: 65536, HasMTU: true,
				LinkMode: 0, HasLinkMode: true,
				Address:   []byte{0, 0, 0, 0, 0, 0},
				Broadcast: []byte{0, 0, 0, 0, 0, 0},
				Qdisc:     "noqueue", TxQLen: 1000, HasTxQLen: true,
				Group: 0, HasGroup: true,
			},
		},
		{
			// ip_link_n:3  "2: enp1s0: <BROADCAST,MULTICAST,UP,LOWER_UP> mtu 1500
			//               qdisc mq state UP mode DEFAULT group default qlen 1000"
			// ip_link_n:4  "    link/ether e0:4f:43:e6:28:ef brd ff:ff:ff:ff:ff:ff"
			//
			// ip_link_n:5  "    altname enxe04f43e628ef"
			//
			// A physical NIC has no IFLA_LINKINFO, so Kind is "" — the absence
			// that makes `ip` print no third line for it. It does carry
			// IFLA_PROP_LIST, which is the udev-assigned MAC-derived altname,
			// and that one IS printed by a plain `ip link show`.
			//
			// PermAddress EQUALS Address, which is the normal case for a NIC
			// whose MAC has never been overridden — and it is why ip_link_n
			// has no "permaddr" token anywhere despite three of its links
			// carrying the attribute. `ip` guards the token on the two values
			// differing (ip/ipaddress.c:1097-1100), so decoding presence and
			// rendering on presence would add a token `ip` does not print.
			description: "positive: primary NIC enp1s0 carries a real MAC, qdisc mq and one altname",
			want: LinkInfo{
				Index: 2, Flags: 0x11043, Name: "enp1s0", Type: 1,
				OperState: IfOperUp, HasOperState: true,
				Carrier: 1, HasCarrier: true,
				MTU: 1500, HasMTU: true,
				LinkMode: 0, HasLinkMode: true,
				Address:     []byte{0xe0, 0x4f, 0x43, 0xe6, 0x28, 0xef},
				Broadcast:   []byte{0xff, 0xff, 0xff, 0xff, 0xff, 0xff},
				PermAddress: []byte{0xe0, 0x4f, 0x43, 0xe6, 0x28, 0xef},
				Qdisc:       "mq", TxQLen: 1000, HasTxQLen: true, HasGroup: true,
				AltNames: []string{"enxe04f43e628ef"},
			},
		},
		{
			// ip_link_n:6  "3: enp35s0f0np0: ... mtu 1500 qdisc mq state UP"
			// ip_link_n:7  "    link/ether 04:09:73:cf:d8:d0 brd ff:ff:ff:ff:ff:ff"
			// ip_link_n:8  "    altname enx040973cfd8d0"
			description: "positive: NIC enp35s0f0np0, index 3, with its altname",
			want: LinkInfo{
				Index: 3, Flags: 0x11043, Name: "enp35s0f0np0", Type: 1,
				OperState: IfOperUp, HasOperState: true,
				Carrier: 1, HasCarrier: true,
				MTU: 1500, HasMTU: true,
				LinkMode: 0, HasLinkMode: true,
				Address:     []byte{0x04, 0x09, 0x73, 0xcf, 0xd8, 0xd0},
				Broadcast:   []byte{0xff, 0xff, 0xff, 0xff, 0xff, 0xff},
				PermAddress: []byte{0x04, 0x09, 0x73, 0xcf, 0xd8, 0xd0},
				Qdisc:       "mq", TxQLen: 1000, HasTxQLen: true, HasGroup: true,
				AltNames: []string{"enx040973cfd8d0"},
			},
		},
		{
			// ip_link_n:21 "58: ve-nfb-vpn@if2: ... qdisc noqueue state UP"
			// ip_link_n:22 "    link/ether 6e:05:d5:51:50:25 brd ff:.. link-netnsid 1"
			// ip_link_n:23 "    veth ..."
			//
			// The pair `ip` needs for the "@if2" suffix: IFLA_LINK is the peer's
			// index (2) and IFLA_LINK_NETNSID says the peer lives in another
			// netns, so the name cannot be resolved locally and `ip` falls back to
			// printing "@if%d". Without IFLA_LINK it would print no suffix at all;
			// without IFLA_LINK_NETNSID it would try ll_link_get and emit a whole
			// extra netlink transaction.
			//
			// Kind comes from inside IFLA_LINKINFO, so this row is also the
			// nested-descent assertion.
			description: "positive: veth ve-nfb-vpn carries IFLA_LINK, IFLA_LINK_NETNSID and kind veth",
			want: LinkInfo{
				Index: 58, Flags: 0x11043, Name: "ve-nfb-vpn", Type: 1,
				OperState: IfOperUp, HasOperState: true,
				Carrier: 1, HasCarrier: true,
				MTU: 1500, HasMTU: true,
				LinkMode: 0, HasLinkMode: true,
				Address:   []byte{0x6e, 0x05, 0xd5, 0x51, 0x50, 0x25},
				Broadcast: []byte{0xff, 0xff, 0xff, 0xff, 0xff, 0xff},
				Qdisc:     "noqueue", TxQLen: 1000, HasTxQLen: true, HasGroup: true,
				Kind: "veth",
				Link: 2, HasLink: true, LinkNetnsID: 1, HasLinkNetnsID: true,
			},
		},
		{
			// ip_link_n:28 "60: veth179a698@if2: ... qdisc noqueue master
			//               br-3a5828b2963a state UP mode DEFAULT group default"
			// ip_link_n:18 names index 9 as br-3a5828b2963a, so Master 9 is the
			// bridge `ip` resolves to that word.
			//
			// Note the missing "qlen": this link carries IFLA_TXQLEN with value
			// 0 — HasTxQLen true, TxQLen 0 — and the pinned `ip` omits the token
			// rather than printing "qlen 0". Both halves matter: presence is what
			// distinguishes this from an AF_INET6 link dump, and the zero value is
			// what the render.RenderQlenZero skew is about.
			description: "positive: bridge member veth179a698 carries IFLA_MASTER 9 and txqlen 0",
			want: LinkInfo{
				Index: 60, Flags: 0x11043, Name: "veth179a698", Type: 1,
				OperState: IfOperUp, HasOperState: true,
				Carrier: 1, HasCarrier: true,
				MTU: 1500, HasMTU: true,
				LinkMode: 0, HasLinkMode: true,
				Address:   []byte{0xaa, 0x1f, 0xd4, 0x5f, 0xc8, 0xd6},
				Broadcast: []byte{0xff, 0xff, 0xff, 0xff, 0xff, 0xff},
				Qdisc:     "noqueue", TxQLen: 0, HasTxQLen: true, HasGroup: true,
				// The only link in the corpus with two kinds, and the pair
				// does not read the way the sidecar does: ip_link_n:30 says
				// "    bridge_slave state forwarding …" while the wire
				// attribute holds plain "bridge". The `_slave` lives in
				// iproute2's format string, so it reaches the text form and
				// not the JSON one — see render.LinkView.detailText.
				//
				// HasInfoSlaveData without HasInfoData is the other half of
				// the shape: the port's own kind, veth, sends no per-kind
				// blob, and everything after "bridge_slave" on that line
				// comes out of the SLAVE blob. It is why this is one of the
				// four links `goip -d` refuses.
				Kind: "veth", SlaveKind: "bridge", HasInfoSlaveData: true,
				Link: 2, HasLink: true, Master: 9, LinkNetnsID: 3, HasLinkNetnsID: true,
			},
		},
		{
			// ip_link_n:12 "7: virbr0: <NO-CARRIER,...,UP> ... state DOWN"
			// ip_link_n:14 "    bridge forward_delay 200 ..."
			//
			// The other kind in the nest. Carrier 0 with IFF_UP set is the
			// carrier-down case IsCarrierDown() exists for, and it is a bridge
			// with no members rather than an unplugged cable.
			description: "positive: bridge virbr0 decodes kind bridge and carrier 0",
			want: LinkInfo{
				Index: 7, Flags: 0x1003, Name: "virbr0", Type: 1,
				// Carrier 0 WITH HasCarrier true is the row that earns the
				// flag: IFLA_CARRIER is present and says zero, which is a
				// different fact from the attribute being absent, and the
				// two are indistinguishable without it.
				OperState: IfOperDown, HasOperState: true,
				Carrier: 0, HasCarrier: true,
				MTU: 1500, HasMTU: true,
				LinkMode: 0, HasLinkMode: true,
				Address:   []byte{0x52, 0x54, 0x00, 0x52, 0x00, 0x04},
				Broadcast: []byte{0xff, 0xff, 0xff, 0xff, 0xff, 0xff},
				Qdisc:     "noqueue", TxQLen: 1000, HasTxQLen: true, HasGroup: true,
				// HasInfoData, with no decoded contents behind it: the nest
				// holds 800-odd bytes of bridge parameters that
				// ip_link_n:14 prints in full and this package deliberately
				// does not decode. Recording the presence is what lets
				// `goip -d` refuse this link instead of printing a bare
				// "    bridge " where `ip` prints "    bridge forward_delay
				// 200 hello_time 200 …".
				Kind: "bridge", HasInfoData: true,
			},
		},
		{
			// ip_link_n:24 "59: ve-nordlayepDd-@if2" — kernel truncates the name
			// at IFNAMSIZ, so IFLA_IFNAME carries the truncated form.
			// ip_link_n:27 "    altname ve-nordlayer-vpn"
			//
			// **The one message where both halves of the naming story are on the
			// wire at once.** IFLA_IFNAME is 15 usable bytes plus a NUL, so
			// "ve-nordlayer-vpn" (16) does not fit and the kernel hands back
			// "ve-nordlayepDd-" — a truncation with a udev-style suffix, not a
			// simple prefix. The full name survives as an IFLA_ALT_IFNAME, which
			// is exactly the problem alternative names were added to solve, and
			// this row is why AltNames has to be decoded rather than dismissed as
			// an `ip -d` detail: without it the untruncated name is unrecoverable
			// from the message.
			description: "corner: long veth name truncated in IFNAME but intact in AltNames, index 59",
			want: LinkInfo{
				Index: 59, Flags: 0x11043, Name: "ve-nordlayepDd-", Type: 1,
				OperState: IfOperUp, HasOperState: true,
				Carrier: 1, HasCarrier: true,
				MTU: 1500, HasMTU: true,
				LinkMode: 0, HasLinkMode: true,
				Address:   []byte{0x66, 0xcf, 0x08, 0xaa, 0x09, 0xd9},
				Broadcast: []byte{0xff, 0xff, 0xff, 0xff, 0xff, 0xff},
				Qdisc:     "noqueue", TxQLen: 1000, HasTxQLen: true, HasGroup: true,
				Kind: "veth",
				Link: 2, HasLink: true, LinkNetnsID: 2, HasLinkNetnsID: true,
				AltNames: []string{"ve-nordlayer-vpn"},
			},
		},
		{
			// ip_link_n:32 "161: nlmon0: <NOARP,UP,LOWER_UP> mtu 3776 ... state UNKNOWN"
			// ip_link_n:33 "    link/netlink  promiscuity 0 ..." -> ARPHRD_NETLINK (824).
			//
			// **The boundary row: no IFLA_ADDRESS at all.** Ten of the eleven links
			// carry a 6-byte one; a netlink monitor device has no hardware address,
			// the kernel omits the attribute, and the sidecar shows `ip` printing
			// "link/netlink" followed by two spaces and nothing else. So a nil
			// Address is a real state to render, distinct from lo's all-zero one —
			// and the pair of rows is what stops a decoder from conflating them.
			description: "boundary: nlmon0 has no IFLA_ADDRESS and no IFLA_BROADCAST",
			want: LinkInfo{
				Index: 161, Flags: 0x100c1, Name: "nlmon0", Type: 824,
				OperState: IfOperUnknown, HasOperState: true,
				Carrier: 1, HasCarrier: true,
				MTU: 3776, HasMTU: true,
				LinkMode: 0, HasLinkMode: true,
				Address: nil, Broadcast: nil,
				Qdisc: "noqueue", TxQLen: 1000, HasTxQLen: true, HasGroup: true,
				Kind: "nlmon",
			},
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
			// ip_addr_n:3-4 "inet 127.0.0.1/8 scope host lo"
			//               "   valid_lft forever preferred_lft forever"
			// -> both lifetimes at INFINITY_LIFE_TIME, which is what "forever"
			// is. Flags 0x80 is IFA_F_PERMANENT, and no IFA_BROADCAST: a host
			// address has none, so `ip` prints no "brd" token.
			description: "positive v4: loopback 127.0.0.1/8 scope host on lo (idx 1)",
			fixture:     v4.path,
			want: AddrInfo{
				Family: unix.AF_INET, Prefixlen: 8, Scope: unix.RT_SCOPE_HOST, Index: 1,
				Address: v4b(127, 0, 0, 1), Local: v4b(127, 0, 0, 1), Label: "lo",
				Flags: unix.IFA_F_PERMANENT, HasCacheInfo: true,
				CacheInfo: IfaCacheinfo{
					Preferred: IfaLifetimeInfinityCst, Valid: IfaLifetimeInfinityCst,
					Cstamp: 108, Tstamp: 108,
				},
			},
		},
		{
			// ip_addr_n:10 "inet 172.16.50.219/24 brd 172.16.50.255 scope global
			//               dynamic noprefixroute enp1s0"
			// ip_addr_n:11 "   valid_lft 47871sec preferred_lft 47871sec"
			//
			// The only DHCP address in the corpus, and the only row where the
			// sidecar and the pcap disagree: the pcap says 47877 because `ip`
			// was run about six seconds after the capture. Nothing is wrong —
			// it is the clearest evidence in the fixtures that lifetimes are
			// wall-clock state, which is why a parity comparison normalizes
			// them.
			//
			// Flags 0x200 is IFA_F_NOPREFIXROUTE with IFA_F_PERMANENT *clear* —
			// that absence is what `ip` renders as "dynamic".
			description: "positive v4: dynamic 172.16.50.219/24 on enp1s0 carries a finite lifetime and a broadcast",
			fixture:     v4.path,
			want: AddrInfo{
				Family: unix.AF_INET, Prefixlen: 24, Scope: unix.RT_SCOPE_UNIVERSE, Index: 2,
				Address: v4b(172, 16, 50, 219), Local: v4b(172, 16, 50, 219), Label: "enp1s0",
				Broadcast: v4b(172, 16, 50, 255),
				Flags:     unix.IFA_F_NOPREFIXROUTE, HasCacheInfo: true,
				CacheInfo: IfaCacheinfo{
					Preferred: 47877, Valid: 47877, Cstamp: 2101, Tstamp: 71860384,
				},
			},
		},
		{
			// ip_addr_n:23 "inet 10.10.4.2/29 scope global enp35s0f0np0"
			description: "positive v4: connected-subnet host 10.10.4.2/29 on enp35s0f0np0 (idx 3)",
			fixture:     v4.path,
			want: AddrInfo{
				Family: unix.AF_INET, Prefixlen: 29, Scope: unix.RT_SCOPE_UNIVERSE, Index: 3,
				Address: v4b(10, 10, 4, 2), Local: v4b(10, 10, 4, 2), Label: "enp35s0f0np0",
				Flags: unix.IFA_F_PERMANENT, HasCacheInfo: true,
				CacheInfo: IfaCacheinfo{
					Preferred: IfaLifetimeInfinityCst, Valid: IfaLifetimeInfinityCst,
					Cstamp: 53768267, Tstamp: 53768267,
				},
			},
		},
		{
			// ip_addr_n:62 "inet 10.98.0.1/32 scope global ve-nfb-vpn"
			description: "boundary v4: /32 host address 10.98.0.1 on veth (idx 58)",
			fixture:     v4.path,
			want: AddrInfo{
				Family: unix.AF_INET, Prefixlen: 32, Scope: unix.RT_SCOPE_UNIVERSE, Index: 58,
				Address: v4b(10, 98, 0, 1), Local: v4b(10, 98, 0, 1), Label: "ve-nfb-vpn",
				Flags: unix.IFA_F_PERMANENT, HasCacheInfo: true,
				CacheInfo: IfaCacheinfo{
					Preferred: IfaLifetimeInfinityCst, Valid: IfaLifetimeInfinityCst,
					Cstamp: 53768600, Tstamp: 53768600,
				},
			},
		},
		{
			// ip_addr_n:5  "inet6 ::1/128 scope host noprefixroute"
			//
			// v6 carries IFA_ADDRESS only and no IFA_LABEL, so Local here is
			// the alias ip/ipaddress.c:1531-1534 installs — nil before the
			// aliasing, and a renderer reading Local would print nothing.
			// Flags 0x280 is PERMANENT|NOPREFIXROUTE, the second of which is
			// the "noprefixroute" token on the sidecar line.
			description: "boundary v6: loopback ::1/128 has no IFA_LOCAL, so Local is aliased from IFA_ADDRESS",
			fixture:     v6.path,
			want: AddrInfo{
				Family: unix.AF_INET6, Prefixlen: 128, Scope: unix.RT_SCOPE_HOST, Index: 1,
				Address: mustV6(t, "::1"), Local: mustV6(t, "::1"),
				Flags: unix.IFA_F_PERMANENT | unix.IFA_F_NOPREFIXROUTE, HasCacheInfo: true,
				CacheInfo: IfaCacheinfo{
					Preferred: IfaLifetimeInfinityCst, Valid: IfaLifetimeInfinityCst,
					Cstamp: 108, Tstamp: 108,
				},
			},
		},
		{
			// ip_addr_n:25 "inet6 fd10:10:4::2/64 scope global nodad"
			// -> 0x82 is PERMANENT|NODAD; "nodad" is the operator's flag, and
			// it is the reason the parity topology in the goip plan adds its
			// IPv6 address with `nodad`: a tentative address would come and go
			// between two runs.
			description: "positive v6: ULA fd10:10:4::2/64 carries IFA_F_NODAD",
			fixture:     v6.path,
			want: AddrInfo{
				Family: unix.AF_INET6, Prefixlen: 64, Scope: unix.RT_SCOPE_UNIVERSE, Index: 3,
				Address: mustV6(t, "fd10:10:4::2"), Local: mustV6(t, "fd10:10:4::2"),
				Flags: unix.IFA_F_PERMANENT | unix.IFA_F_NODAD, HasCacheInfo: true,
				CacheInfo: IfaCacheinfo{
					Preferred: IfaLifetimeInfinityCst, Valid: IfaLifetimeInfinityCst,
					Cstamp: 53768268, Tstamp: 53768268,
				},
			},
		},
		{
			// ip_addr_n:18 "inet6 fe80::b5c8:b23e:9a98:a37c/64 scope link noprefixroute"
			description: "positive v6: link-local fe80::…a37c/64 scope link on enp1s0 (idx 2)",
			fixture:     v6.path,
			want: AddrInfo{
				Family: unix.AF_INET6, Prefixlen: 64, Scope: unix.RT_SCOPE_LINK, Index: 2,
				Address: mustV6(t, "fe80::b5c8:b23e:9a98:a37c"),
				Local:   mustV6(t, "fe80::b5c8:b23e:9a98:a37c"),
				Flags:   unix.IFA_F_PERMANENT | unix.IFA_F_NOPREFIXROUTE, HasCacheInfo: true,
				CacheInfo: IfaCacheinfo{
					Preferred: IfaLifetimeInfinityCst, Valid: IfaLifetimeInfinityCst,
					Cstamp: 1887, Tstamp: 1887,
				},
			},
		},
		{
			// ip_addr_n:16 "inet6 2603:…:6adf:8a2f:21ae:d6a7/64 scope global
			//               dynamic mngtmpaddr noprefixroute"
			//
			// **The row the 8-bit header field cannot express.** Flags 0x300 is
			// IFA_F_MANAGETEMPADDR (0x100) | IFA_F_NOPREFIXROUTE (0x200); both
			// are above the byte, so a decoder reading only ifa_flags sees 0
			// and prints neither token. This is the real capture behind the
			// constructed 0x300 row in TestParseNewAddr.
			description: "positive v6: SLAAC 2603:…d6a7/64 carries mngtmpaddr|noprefixroute above the header byte",
			fixture:     v6.path,
			want: AddrInfo{
				Family: unix.AF_INET6, Prefixlen: 64, Scope: unix.RT_SCOPE_UNIVERSE, Index: 2,
				Address: mustV6(t, "2603:8002:ea00:6800:6adf:8a2f:21ae:d6a7"),
				Local:   mustV6(t, "2603:8002:ea00:6800:6adf:8a2f:21ae:d6a7"),
				Flags:   unix.IFA_F_MANAGETEMPADDR | unix.IFA_F_NOPREFIXROUTE, HasCacheInfo: true,
				CacheInfo: IfaCacheinfo{
					Preferred: 86400, Valid: 86400, Cstamp: 2107, Tstamp: 73930035,
				},
			},
		},
		{
			// ip_addr_n:14-15 "inet6 2603:…:827f:e158:2c1c:13b4/64 scope global
			//                  temporary deprecated dynamic"
			//                 "   valid_lft 34941sec preferred_lft 0sec"
			//
			// preferred_lft 0 with a non-zero valid_lft is the deprecated state,
			// and the kernel sets IFA_F_DEPRECATED (0x20) alongside
			// IFA_F_TEMPORARY (0x01) — so the flag and the lifetime agree, which
			// is what makes IsDeprecated() checkable against a real capture
			// rather than against its own definition.
			description: "corner v6: deprecated temporary address has preferred_lft 0 and IFA_F_DEPRECATED",
			fixture:     v6.path,
			want: AddrInfo{
				Family: unix.AF_INET6, Prefixlen: 64, Scope: unix.RT_SCOPE_UNIVERSE, Index: 2,
				Address: mustV6(t, "2603:8002:ea00:6800:827f:e158:2c1c:13b4"),
				Local:   mustV6(t, "2603:8002:ea00:6800:827f:e158:2c1c:13b4"),
				Flags:   unix.IFA_F_TEMPORARY | unix.IFA_F_DEPRECATED, HasCacheInfo: true,
				CacheInfo: IfaCacheinfo{
					Preferred: 0, Valid: 34946, Cstamp: 60144546, Tstamp: 73930035,
				},
			},
		},
		{
			// ip_addr_n:27 "inet6 fe80::609:73ff:fecf:d8d0/64 scope link proto kernel_ll"
			//
			// The only attribute in the corpus that `ip` renders from IFA_PROTO.
			// Seven of the fifteen v6 addresses carry it; every one is a
			// kernel-generated link-local, hence IFAPROT_KERNEL_LL.
			description: "positive v6: kernel link-local carries IFA_PROTO = IFAPROT_KERNEL_LL",
			fixture:     v6.path,
			want: AddrInfo{
				Family: unix.AF_INET6, Prefixlen: 64, Scope: unix.RT_SCOPE_LINK, Index: 3,
				Address: mustV6(t, "fe80::609:73ff:fecf:d8d0"),
				Local:   mustV6(t, "fe80::609:73ff:fecf:d8d0"),
				Flags:   unix.IFA_F_PERMANENT,
				Proto:   IfaProtoKernelLL, HasProto: true,
				HasCacheInfo: true,
				CacheInfo: IfaCacheinfo{
					Preferred: IfaLifetimeInfinityCst, Valid: IfaLifetimeInfinityCst,
					Cstamp: 1382, Tstamp: 1382,
				},
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
				Gateway: v4b(172, 16, 50, 1), PrefSrc: v4b(172, 16, 50, 219), Oif: 2,
				Priority: 100, HasPriority: true,
				// No HasPref: RTA_PREF is an ICMPv6 router preference, so the
				// kernel attaches it to IPv6 routes only — 48 of the 74 routes
				// in this dump carry it, which is exactly its IPv6 half. Hence
				// no `pref` token on any v4 line of ip_route_table_all_n.
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
				Dst: mustV6(t, "fd10:10:4::"), Oif: 3,
				Priority: 256, HasPriority: true,
				// pref=0 is ICMPV6_ROUTER_PREF_MEDIUM, which renders as
				// `pref medium` — the value a kernel-installed route gets. The
				// flag is what makes it printable: Pref 0 alone is
				// indistinguishable from the attribute being absent, which is
				// the v4 case immediately above.
				Pref: 0, HasPref: true,
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
				Gateway: mustV6(t, "fe80::e638:83ff:fe36:8f0d"), Oif: 2,
				Priority: 100, HasPriority: true,
				// The one non-medium preference in the corpus, and the reason
				// Pref is decoded as a value rather than a bool: this route was
				// learned from a router advertisement that set
				// ICMPV6_ROUTER_PREF_HIGH (0x1), and the sidecar prints
				// `pref high`. Every other v6 route here is medium.
				Pref: 1, HasPref: true,
			},
		},
		{
			// ip_route_table_all_n:40
			// "local ::1 dev lo table local proto kernel scope global metric 0
			//  pref medium"
			// Note: unlike the IPv4 loopback local route (scope HOST), the v6 ::1
			// local route is scope GLOBAL.
			description: "boundary v6: local host route ::1/128 (type LOCAL, table LOCAL, scope GLOBAL)",
			want: RouteInfo{
				Family: unix.AF_INET6, DstLen: 128, Table: unix.RT_TABLE_LOCAL,
				Scope: unix.RT_SCOPE_UNIVERSE, Type: unix.RTN_LOCAL, Protocol: unix.RTPROT_KERNEL,
				Dst: mustV6(t, "::1"), Oif: 1,
				// The boundary this row is named for, now that presence is
				// tracked: RTA_PRIORITY is PRESENT and carries 0, and the
				// sidecar prints `metric 0`. The v4 rows above omit the
				// attribute entirely and print no metric token. Priority 0 with
				// HasPriority false and Priority 0 with HasPriority true are
				// therefore different renderings, which is what the flag buys.
				Priority: 0, HasPriority: true,
				Pref: 0, HasPref: true,
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
