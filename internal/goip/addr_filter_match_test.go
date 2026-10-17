package goip

import (
	"net/netip"
	"testing"

	"golang.org/x/sys/unix"

	"github.com/randomizedcoder/xtcp2/pkg/xtcpnl"
)

// mustAddrFilter parses args the way `ip addr show` would, with the fake
// lookups (d0 is 3, bond0 is 5).
func mustAddrFilter(t *testing.T, family uint8, args ...string) addrFilter {
	t.Helper()
	f, err := parseAddrShowArgs(args, family, newFakeAddrLookups().lookups())
	if err != nil {
		t.Fatalf("parseAddrShowArgs(%q): %v", args, err)
	}
	return f
}

func addrBytes(s string) []byte {
	if s == "" {
		return nil
	}
	return netip.MustParseAddr(s).AsSlice()
}

func devNameD0(int32) string { return "d0" }

// TestInetPrefixMatch is inet_addr_match_rta (lib/utils.c:938-977) on the
// prefix `to` parses and an address's IFA_LOCAL.
//
// go test ./internal/goip/ -run TestInetPrefixMatch
func TestInetPrefixMatch(t *testing.T) {
	tests := []struct {
		description string
		prefix      string
		local       string
		expected    bool
	}{
		{description: "positive: inside a /24", prefix: "192.0.2.0/24", local: "192.0.2.255", expected: true},
		{description: "boundary: first address outside a /24", prefix: "192.0.2.0/24", local: "192.0.3.0", expected: false},
		{description: "boundary: /0 matches everything", prefix: "0.0.0.0/0", local: "203.0.113.1", expected: true},
		{description: "boundary: /32 exact", prefix: "192.0.2.1/32", local: "192.0.2.1", expected: true},
		{description: "boundary: /32 one off", prefix: "192.0.2.1/32", local: "192.0.2.2", expected: false},
		{description: "corner: /25 masks part of a byte, upper half", prefix: "192.0.2.0/25", local: "192.0.2.128", expected: false},
		{description: "corner: /25 masks part of a byte, lower half", prefix: "192.0.2.0/25", local: "192.0.2.127", expected: true},
		{description: "negative: family mismatch", prefix: "2001:db8::/32", local: "192.0.2.1", expected: false},
		{description: "positive: v6 /32", prefix: "2001:db8::/32", local: "2001:db8:1::1", expected: true},
		{description: "negative: v6 outside the /32", prefix: "2001:db8::/32", local: "2001:db9::1", expected: false},
		{description: "corner: no IFA_LOCAL or IFA_ADDRESS passes", prefix: "192.0.2.0/24", local: "", expected: true},
	}
	for _, tc := range tests {
		t.Run(tc.description, func(t *testing.T) {
			f := mustAddrFilter(t, unix.AF_UNSPEC, "to", tc.prefix)
			if got := inetPrefixMatch(&f.pfx, addrBytes(tc.local)); got != tc.expected {
				t.Fatalf("inetPrefixMatch(%s, %q) = %v, want %v", tc.prefix, tc.local, got, tc.expected)
			}
		})
	}
	t.Run("boundary: to default has bitlen -2 and matches everything", func(t *testing.T) {
		f := mustAddrFilter(t, unix.AF_INET, "to", "default")
		if !inetPrefixMatch(&f.pfx, addrBytes("198.51.100.1")) {
			t.Fatalf("to default did not match, pfx %+v", f.pfx)
		}
	})
}

// TestAddrFilterMatch is matchAddr, print_addrinfo's tests, on hand-built
// addresses of link d0 (index 3).
//
// go test ./internal/goip/ -run TestAddrFilterMatch
func TestAddrFilterMatch(t *testing.T) {
	d0 := xtcpnl.LinkInfo{Index: 3, Name: "d0"}
	v4 := func(mod func(*xtcpnl.AddrInfo)) xtcpnl.AddrInfo {
		ai := xtcpnl.AddrInfo{Family: unix.AF_INET, Prefixlen: 24, Index: 3,
			Address: addrBytes("192.0.2.1"), Local: addrBytes("192.0.2.1")}
		if mod != nil {
			mod(&ai)
		}
		return ai
	}
	tests := []struct {
		description string
		family      uint8
		args        []string
		addr        xtcpnl.AddrInfo
		expected    bool
	}{
		{description: "positive: no selectors keeps the address", args: nil, addr: v4(nil), expected: true},
		{description: "positive: secondary tests the merged flags word (IFA_FLAGS over ifa_flags)",
			args: []string{"secondary"}, addr: v4(func(a *xtcpnl.AddrInfo) { a.Flags = unix.IFA_F_SECONDARY }), expected: true},
		{description: "negative: secondary on a primary", args: []string{"secondary"}, addr: v4(nil), expected: false},
		{description: "corner: IFA_ADDRESS stands in for a missing IFA_LOCAL",
			args: []string{"to", "192.0.2.0/24"}, addr: v4(func(a *xtcpnl.AddrInfo) { a.Local = nil }), expected: true},
		{description: "negative: IFA_LOCAL outside the prefix wins over an IFA_ADDRESS inside it",
			args: []string{"to", "192.0.2.0/24"}, addr: v4(func(a *xtcpnl.AddrInfo) { a.Local = addrBytes("198.51.100.1") }), expected: false},
		{description: "boundary: scope all masks nothing",
			args: []string{"scope", "all"}, addr: v4(func(a *xtcpnl.AddrInfo) { a.Scope = unix.RT_SCOPE_HOST }), expected: true},
		{description: "positive: scope host on a host address",
			args: []string{"scope", "host"}, addr: v4(func(a *xtcpnl.AddrInfo) { a.Scope = unix.RT_SCOPE_HOST }), expected: true},
		{description: "negative: scope host on a global address", args: []string{"scope", "host"}, addr: v4(nil), expected: false},
		{description: "negative: -6 drops an AF_INET address", family: unix.AF_INET6, addr: v4(nil), expected: false},
		{description: "corner: proto passes an address with no IFA_PROTO", args: []string{"proto", "99"}, addr: v4(nil), expected: true},
		{description: "positive: proto equal",
			args: []string{"proto", "99"}, addr: v4(func(a *xtcpnl.AddrInfo) { a.Proto, a.HasProto = 99, true }), expected: true},
		{description: "negative: proto differs",
			args: []string{"proto", "99"}, addr: v4(func(a *xtcpnl.AddrInfo) { a.Proto, a.HasProto = 2, true }), expected: false},
		{description: "corner: label with no IFA_LABEL matches the device name", args: []string{"label", "d*"}, addr: v4(nil), expected: true},
		{description: "negative: IFA_LABEL is matched, not the device name",
			args: []string{"label", "d0"}, addr: v4(func(a *xtcpnl.AddrInfo) { a.Label = "d0:1" }), expected: false},
		{description: "positive: label glob on IFA_LABEL",
			args: []string{"label", "d0:*"}, addr: v4(func(a *xtcpnl.AddrInfo) { a.Label = "d0:1" }), expected: true},
		{description: "negative: address of another link", addr: v4(func(a *xtcpnl.AddrInfo) { a.Index = 4 }), expected: false},
	}
	for _, tc := range tests {
		t.Run(tc.description, func(t *testing.T) {
			f := mustAddrFilter(t, tc.family, tc.args...)
			if got := f.matchAddr(&tc.addr, &d0, devNameD0); got != tc.expected {
				t.Fatalf("matchAddr(%q) = %v, want %v", tc.args, got, tc.expected)
			}
		})
	}
}

// TestAddrFilterLinkMode is linkMode, print_linkinfo's filter order.
//
// go test ./internal/goip/ -run TestAddrFilterLinkMode
func TestAddrFilterLinkMode(t *testing.T) {
	up := xtcpnl.LinkInfo{Index: 3, Name: "d0", Flags: unix.IFF_UP, Group: 5, HasGroup: true}
	tests := []struct {
		description string
		args        []string
		link        xtcpnl.LinkInfo
		expected    addrLinkMode
	}{
		{description: "positive: no selectors prints", link: up, expected: addrLinkPrint},
		{description: "positive: label is headerless and skips the group test",
			args: []string{"label", "d0", "group", "default"}, link: up, expected: addrLinkHeaderless},
		{description: "negative: down skips an up link", args: []string{"down"}, link: up, expected: addrLinkSkip},
		{description: "negative: down still skips with label (down is tested first)",
			args: []string{"down", "label", "d0"}, link: up, expected: addrLinkSkip},
		{description: "negative: group mismatch", args: []string{"group", "default"}, link: up, expected: addrLinkSkip},
		{description: "corner: nomaster is not tested client-side",
			args: []string{"nomaster"}, link: xtcpnl.LinkInfo{Index: 3, Name: "d0", Master: 5}, expected: addrLinkPrint},
		{description: "positive: master equal",
			args: []string{"master", "bond0"}, link: xtcpnl.LinkInfo{Index: 3, Name: "d0", Master: 5}, expected: addrLinkPrint},
		{description: "negative: master differs", args: []string{"master", "bond0"}, link: up, expected: addrLinkSkip},
		{description: "negative: type with no IFLA_LINKINFO",
			args: []string{"type", "dummy"}, link: up, expected: addrLinkSkip},
		{description: "positive: slave kind matches the wire value",
			args: []string{"type", "bond_slave"},
			link: xtcpnl.LinkInfo{Index: 3, Name: "d0", HasLinkInfo: true, Kind: "dummy", SlaveKind: "bond"}, expected: addrLinkPrint},
		{description: "corner: a link with no name is skipped", link: xtcpnl.LinkInfo{Index: 3}, expected: addrLinkSkip},
	}
	for _, tc := range tests {
		t.Run(tc.description, func(t *testing.T) {
			f := mustAddrFilter(t, unix.AF_UNSPEC, tc.args...)
			if got := f.linkMode(&tc.link); got != tc.expected {
				t.Fatalf("linkMode(%q) = %d, want %d", tc.args, got, tc.expected)
			}
		})
	}
}

// TestAddrFilterKeepLinks is keepLinks, ipaddr_filter then group_filter.
//
// go test ./internal/goip/ -run TestAddrFilterKeepLinks
func TestAddrFilterKeepLinks(t *testing.T) {
	links := []xtcpnl.LinkInfo{{Index: 3, Name: "d0"}, {Index: 4, Name: "d1"}}
	proto2 := xtcpnl.AddrInfo{Family: unix.AF_INET, Index: 3, Local: addrBytes("192.0.2.1"), Proto: 2, HasProto: true}
	v6 := xtcpnl.AddrInfo{Family: unix.AF_INET6, Index: 4, Local: addrBytes("2001:db8::1")}
	tests := []struct {
		description string
		family      uint8
		args        []string
		addrs       []xtcpnl.AddrInfo
		expected    []int32
	}{
		{description: "positive: rescue keeps an address-less link under UNSPEC", expected: []int32{3, 4}},
		{description: "negative: no rescue under -4", family: unix.AF_INET, expected: []int32{}},
		{description: "corner: a link with only v6 addresses is not rescued under -4",
			family: unix.AF_INET, addrs: []xtcpnl.AddrInfo{proto2, v6}, expected: []int32{3}},
		{description: "corner: proto is not an ipaddr_filter test",
			args: []string{"proto", "99"}, addrs: []xtcpnl.AddrInfo{proto2}, expected: []int32{3, 4}},
		{description: "negative: no address in the prefix drops the link",
			args: []string{"to", "198.51.100.0/24"}, addrs: []xtcpnl.AddrInfo{proto2, v6}, expected: []int32{}},
	}
	for _, tc := range tests {
		t.Run(tc.description, func(t *testing.T) {
			f := mustAddrFilter(t, tc.family, tc.args...)
			got := []int32{}
			for _, li := range f.keepLinks(links, tc.addrs, devNameD0, true) {
				got = append(got, li.Index)
			}
			if len(got) != len(tc.expected) {
				t.Fatalf("keepLinks(%q) = %v, want %v", tc.args, got, tc.expected)
			}
			for i := range got {
				if got[i] != tc.expected[i] {
					t.Fatalf("keepLinks(%q) = %v, want %v", tc.args, got, tc.expected)
				}
			}
		})
	}
}
