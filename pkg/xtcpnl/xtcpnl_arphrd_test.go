package xtcpnl

import (
	"os"
	"strings"
	"testing"

	"golang.org/x/sys/unix"
)

// This file covers the ARPHRD_* name table and the hardware-address formatter.
//
// Both exist for one reason: "link/ether aa:bb:cc:dd:ee:ff brd ff:ff:ff:ff:ff:ff"
// is the second line of every `ip link show` entry, and neither half of it can
// be produced from the netlink bytes without these two functions. So the
// expectations here are quoted from the committed ip_link_n sidecar rather than
// derived from the constants.

// TestARPHRDName covers the ifi_type -> name table transcribed from iproute2's
// lib/ll_types.c.
//
// go test ./pkg/xtcpnl/ -run TestARPHRDName
func TestARPHRDName(t *testing.T) {
	tests := []struct {
		description string
		ifiType     uint16
		want        string
	}{
		{
			// ip_link_n:4 "    link/ether e0:4f:43:e6:28:ef ..."
			description: "positive: ARPHRD_ETHER renders as ether, per ip_link_n:4",
			ifiType:     unix.ARPHRD_ETHER,
			want:        "ether",
		},
		{
			// ip_link_n:2 "    link/loopback 00:00:00:00:00:00 ..."
			description: "positive: ARPHRD_LOOPBACK renders as loopback, per ip_link_n:2",
			ifiType:     unix.ARPHRD_LOOPBACK,
			want:        "loopback",
		},
		{
			// ip_link_n:33 "    link/netlink  promiscuity 0 ..."
			description: "positive: ARPHRD_NETLINK renders as netlink, per ip_link_n:33",
			ifiType:     unix.ARPHRD_NETLINK,
			want:        "netlink",
		},
		{
			// The spelling is not derivable from the constant name. Three of the
			// entries rename outright, and these are the ones a from-memory
			// table gets wrong.
			description: "positive: ARPHRD_TUNNEL renders as ipip, not tunnel",
			ifiType:     unix.ARPHRD_TUNNEL,
			want:        "ipip",
		},
		{
			description: "positive: ARPHRD_IPDDP renders as ip/ddp, with the slash",
			ifiType:     unix.ARPHRD_IPDDP,
			want:        "ip/ddp",
		},
		{
			description: "positive: ARPHRD_IEEE80211_RADIOTAP renders as ieee802.11/radiotap",
			ifiType:     unix.ARPHRD_IEEE80211_RADIOTAP,
			want:        "ieee802.11/radiotap",
		},
		{
			description: "positive: ARPHRD_IPGRE renders as gre and ARPHRD_IP6GRE as gre6",
			ifiType:     unix.ARPHRD_IP6GRE,
			want:        "gre6",
		},
		{
			// FCFABRIC + n are thirteen sequential entries in the C table,
			// written here as constant arithmetic. This row pins that the
			// arithmetic lands where the names say it does.
			description: "boundary: ARPHRD_FCFABRIC+12 is the last of the fcfb block",
			ifiType:     unix.ARPHRD_FCFABRIC + 12,
			want:        "fcfb12",
		},
		{
			// Type 0 is not "unset", it is ARPHRD_NETROM. A decoder treating 0
			// as absent would print "[0]" for a real netrom device.
			description: "boundary: type 0 is ARPHRD_NETROM, a named entry, not unknown",
			ifiType:     0,
			want:        "netrom",
		},
		{
			// 0xffff is the natural "obviously invalid" probe and it is wrong:
			// ARPHRD_VOID is a real named entry, so it can never exercise the
			// fallback.
			description: "boundary: 0xffff is ARPHRD_VOID, so it is named rather than bracketed",
			ifiType:     0xffff,
			want:        "void",
		},
		{
			description: "boundary: 0xfffe is ARPHRD_NONE, likewise named",
			ifiType:     0xfffe,
			want:        "none",
		},
		{
			// ARPHRD_CISCO and ARPHRD_HDLC are both 513 in unix; iproute2 names
			// the value "hdlc", and only one spelling can exist in the table.
			description: "corner: 513 is both ARPHRD_CISCO and ARPHRD_HDLC, named hdlc",
			ifiType:     unix.ARPHRD_HDLC,
			want:        "hdlc",
		},
		{
			// The fallback is iproute2's own "[%d]" — decimal, bracketed. A hex
			// fallback would be a silent divergence from `ip`.
			description: "negative: ARPHRD_RAWIP is in unix but not iproute2's table -> [519]",
			ifiType:     unix.ARPHRD_RAWIP,
			want:        "[519]",
		},
		{
			description: "negative: ARPHRD_VSOCKMON is not in iproute2's table -> bracketed decimal",
			ifiType:     unix.ARPHRD_VSOCKMON,
			want:        "[826]",
		},
		{
			description: "negative: an unallocated type falls back to bracketed decimal",
			ifiType:     1000,
			want:        "[1000]",
		},
		{
			description: "corner: the largest unnamed type below ARPHRD_NONE",
			ifiType:     0xfffd,
			want:        "[65533]",
		},
	}

	for _, tc := range tests {
		t.Run(tc.description, func(t *testing.T) {
			if got := ARPHRDName(tc.ifiType); got != tc.want {
				t.Errorf("ARPHRDName(%d) = %q, want %q", tc.ifiType, got, tc.want)
			}
		})
	}
}

// TestARPHRDNameMatchesIproute2Omissions pins the constants unix exports that
// iproute2's table deliberately does not name.
//
// It is not a style assertion. `ip` prints "[519]" for a raw-IP link because
// ll_type_n2a has no ARPHRD_RAWIP entry, so a goip that helpfully added one
// would diverge on exactly the devices nobody tests against. Adding any of
// these turns this test red, which is the prompt to check the C table first.
//
// go test ./pkg/xtcpnl/ -run TestARPHRDNameMatchesIproute2Omissions
func TestARPHRDNameMatchesIproute2Omissions(t *testing.T) {
	tests := []struct {
		description string
		ifiType     uint16
	}{
		{"negative: ARPHRD_RAWIP is absent from lib/ll_types.c", unix.ARPHRD_RAWIP},
		{"negative: ARPHRD_MCTP is absent from lib/ll_types.c", unix.ARPHRD_MCTP},
		{"negative: ARPHRD_VSOCKMON is absent from lib/ll_types.c", unix.ARPHRD_VSOCKMON},
		{"negative: ARPHRD_EUI64 is absent from lib/ll_types.c", unix.ARPHRD_EUI64},
	}

	for _, tc := range tests {
		t.Run(tc.description, func(t *testing.T) {
			if name, ok := arphrdNames[tc.ifiType]; ok {
				t.Errorf("arphrdNames[%d] = %q, want no entry (iproute2 has none, so `ip` prints [%d])",
					tc.ifiType, name, tc.ifiType)
			}
		})
	}
}

// TestARPHRDNameCoversTheRealFixture asserts the three hardware types the
// committed 7.1.8 link dump actually contains all resolve to names, and that
// each name appears on the "link/" line of ip_link_n.
//
// The table above quotes the sidecar by line number; this one reads the sidecar
// and checks the claim, so a regenerated fixture cannot leave the quotes stale.
//
// go test ./pkg/xtcpnl/ -run TestARPHRDNameCoversTheRealFixture
func TestARPHRDNameCoversTheRealFixture(t *testing.T) {
	sidecar, err := os.ReadFile("testdata/7_1_8/ip_link_n")
	if err != nil {
		t.Fatalf("ReadFile(ip_link_n): %v", err)
	}
	text := string(sidecar)

	tests := []struct {
		description string
		ifiType     uint16
		want        string
	}{
		{"positive: ARPHRD_LOOPBACK (772) is lo's type", 772, "loopback"},
		{"positive: ARPHRD_ETHER (1) is every NIC, bridge and veth", 1, "ether"},
		{"positive: ARPHRD_NETLINK (824) is the capture's own nlmon0", 824, "netlink"},
	}

	for _, tc := range tests {
		t.Run(tc.description, func(t *testing.T) {
			got := ARPHRDName(tc.ifiType)
			if got != tc.want {
				t.Fatalf("ARPHRDName(%d) = %q, want %q", tc.ifiType, got, tc.want)
			}
			if !strings.Contains(text, "link/"+got) {
				t.Errorf("ip_link_n contains no %q line, so the expectation is stale", "link/"+got)
			}
		})
	}
}

// TestHWAddr covers the hardware-address formatter for LinkInfo.Address and
// LinkInfo.Broadcast.
//
// The answer depends on BOTH the byte length and the link's ifi_type, because
// ll_addr_n2a (lib/ll_addr.c:26-44) special-cases five ARPHRD types and falls
// through to colon-hex for every other. Every row therefore states its type
// rather than leaving it at the zero value, which is ARPHRD_NETROM and was
// quietly what the whole table used to assert.
//
// go test ./pkg/xtcpnl/ -run TestHWAddr
func TestHWAddr(t *testing.T) {
	tests := []struct {
		description string
		ifiType     uint16
		in          []byte
		want        string
	}{
		{
			// ip_link_n:4 "    link/ether e0:4f:43:e6:28:ef brd ff:ff:ff:ff:ff:ff"
			description: "positive: enp1s0's 6-byte MAC, lower hex, colon separated",
			ifiType:     unix.ARPHRD_ETHER,
			in:          []byte{0xe0, 0x4f, 0x43, 0xe6, 0x28, 0xef},
			want:        "e0:4f:43:e6:28:ef",
		},
		{
			description: "positive: the all-ones broadcast address",
			ifiType:     unix.ARPHRD_ETHER,
			in:          []byte{0xff, 0xff, 0xff, 0xff, 0xff, 0xff},
			want:        "ff:ff:ff:ff:ff:ff",
		},
		{
			// lo really has this address; it is not an absent attribute. Both
			// rows exist so the two states stay distinguishable.
			description: "positive: lo's all-zero MAC prints in full, per ip_link_n:2",
			ifiType:     unix.ARPHRD_LOOPBACK,
			in:          []byte{0, 0, 0, 0, 0, 0},
			want:        "00:00:00:00:00:00",
		},
		{
			// Every byte is rendered whatever the length — the property that
			// makes a [6]byte field or a val[:6] slice wrong.
			description: "boundary: a 20-byte InfiniBand address renders all 20 bytes",
			ifiType:     unix.ARPHRD_INFINIBAND,
			in: []byte{
				0x00, 0x01, 0x02, 0x03, 0x04, 0x05, 0x06, 0x07, 0x08, 0x09,
				0x0a, 0x0b, 0x0c, 0x0d, 0x0e, 0x0f, 0x10, 0x11, 0x12, 0x13,
			},
			want: "00:01:02:03:04:05:06:07:08:09:0a:0b:0c:0d:0e:0f:10:11:12:13",
		},
		{
			description: "boundary: a single byte has no separator",
			ifiType:     unix.ARPHRD_ETHER,
			in:          []byte{0xab},
			want:        "ab",
		},
		{
			// nlmon0 in the committed dump: no IFLA_ADDRESS, and `ip` prints
			// "link/netlink" with nothing after it (ip_link_n:33).
			description: "boundary: nil is the empty string, matching ip_link_n:33",
			ifiType:     unix.ARPHRD_NETLINK,
			in:          nil,
			want:        "",
		},
		{
			description: "boundary: a zero-length non-nil slice is also the empty string",
			ifiType:     unix.ARPHRD_ETHER,
			in:          []byte{},
			want:        "",
		},
		{
			description: "corner: high nibbles use lower-case hex digits",
			ifiType:     unix.ARPHRD_ETHER,
			in:          []byte{0xde, 0xad, 0xbe, 0xef},
			want:        "de:ad:be:ef",
		},

		// The five special cases (lib/ll_addr.c:32-38). Each address below is
		// the `local` endpoint the tunnel namespace configures, so these rows
		// and dumps/tunnel/ip_link assert the same bytes from two directions.
		{
			description: "positive: ARPHRD_TUNNEL renders 4 bytes as a dotted quad, not hex",
			ifiType:     unix.ARPHRD_TUNNEL,
			in:          []byte{192, 0, 2, 1},
			want:        "192.0.2.1",
		},
		{
			description: "positive: ARPHRD_SIT renders 4 bytes as a dotted quad",
			ifiType:     unix.ARPHRD_SIT,
			in:          []byte{192, 0, 2, 2},
			want:        "192.0.2.2",
		},
		{
			description: "positive: ARPHRD_IPGRE renders 4 bytes as a dotted quad",
			ifiType:     unix.ARPHRD_IPGRE,
			in:          []byte{192, 0, 2, 3},
			want:        "192.0.2.3",
		},
		{
			description: "positive: ARPHRD_TUNNEL6 renders 16 bytes as an IPv6 literal",
			ifiType:     unix.ARPHRD_TUNNEL6,
			in: []byte{
				0x20, 0x01, 0x0d, 0xb8, 0, 0, 0, 0,
				0, 0, 0, 0, 0, 0, 0, 0x01,
			},
			want: "2001:db8::1",
		},
		{
			description: "positive: ARPHRD_IP6GRE renders 16 bytes as an IPv6 literal",
			ifiType:     unix.ARPHRD_IP6GRE,
			in: []byte{
				0x20, 0x01, 0x0d, 0xb8, 0, 0, 0, 0,
				0, 0, 0, 0, 0, 0, 0, 0x02,
			},
			want: "2001:db8::2",
		},

		// The fallback devices each module creates in every namespace. Their
		// address is present and all-zero, which is a different thing from
		// absent, and it must not come out as "00:00:00:00".
		{
			description: "boundary: tunl0's all-zero 4-byte address is 0.0.0.0, not 00:00:00:00",
			ifiType:     unix.ARPHRD_TUNNEL,
			in:          []byte{0, 0, 0, 0},
			want:        "0.0.0.0",
		},
		{
			description: "boundary: ip6tnl0's all-zero 16-byte address is ::",
			ifiType:     unix.ARPHRD_TUNNEL6,
			in:          make([]byte, 16),
			want:        "::",
		},

		// Both halves of each test are load-bearing: the conversion needs the
		// length AND the type, so three of the four combinations stay hex.
		{
			description: "negative: 4 bytes on ARPHRD_ETHER stay hex — the length alone is not enough",
			ifiType:     unix.ARPHRD_ETHER,
			in:          []byte{192, 0, 2, 1},
			want:        "c0:00:02:01",
		},
		{
			description: "negative: 6 bytes on ARPHRD_SIT stay hex — the type alone is not enough",
			ifiType:     unix.ARPHRD_SIT,
			in:          []byte{0xe0, 0x4f, 0x43, 0xe6, 0x28, 0xef},
			want:        "e0:4f:43:e6:28:ef",
		},
		{
			description: "negative: 16 bytes on ARPHRD_TUNNEL stay hex — it is a v4-only special case",
			ifiType:     unix.ARPHRD_TUNNEL,
			in: []byte{
				0x20, 0x01, 0x0d, 0xb8, 0, 0, 0, 0,
				0, 0, 0, 0, 0, 0, 0, 0x01,
			},
			want: "20:01:0d:b8:00:00:00:00:00:00:00:00:00:00:00:01",
		},
		{
			description: "negative: 4 bytes on ARPHRD_TUNNEL6 stay hex — it is a v6-only special case",
			ifiType:     unix.ARPHRD_TUNNEL6,
			in:          []byte{192, 0, 2, 1},
			want:        "c0:00:02:01",
		},
		{
			// The reason LLAddrN2A uses netip and not net.IP: net.IP.String()
			// would answer "1.2.3.4" here, where inet_ntop(AF_INET6) — the
			// function actually being mirrored — answers this.
			description: "corner: a v4-mapped 16-byte address keeps its ::ffff: prefix, as inet_ntop does",
			ifiType:     unix.ARPHRD_IP6GRE,
			in: []byte{
				0, 0, 0, 0, 0, 0, 0, 0,
				0, 0, 0xff, 0xff, 1, 2, 3, 4,
			},
			want: "::ffff:1.2.3.4",
		},
		{
			description: "corner: 255.255.255.255 on ARPHRD_IPGRE is an address, not a broadcast MAC",
			ifiType:     unix.ARPHRD_IPGRE,
			in:          []byte{0xff, 0xff, 0xff, 0xff},
			want:        "255.255.255.255",
		},
		{
			// inet_ntop compresses the longest zero run exactly once; a second
			// run stays written out. Worth a row because it is the one place a
			// hand-rolled v6 formatter would diverge.
			description: "corner: only the longest zero run is compressed on ARPHRD_TUNNEL6",
			ifiType:     unix.ARPHRD_TUNNEL6,
			in: []byte{
				0x20, 0x01, 0, 0, 0, 0x01, 0, 0,
				0, 0, 0, 0, 0, 0, 0, 0x01,
			},
			want: "2001:0:1::1",
		},
	}

	for _, tc := range tests {
		t.Run(tc.description, func(t *testing.T) {
			li := LinkInfo{Type: tc.ifiType, Address: tc.in, Broadcast: tc.in}
			if got := li.HWAddr(); got != tc.want {
				t.Errorf("HWAddr() = %q, want %q", got, tc.want)
			}
			// `ip` passes IFLA_BROADCAST through the same function with the
			// same ifi_type (ip/ipaddress.c:1085-1092), so the two can never
			// legitimately disagree.
			if got := li.BroadcastAddr(); got != tc.want {
				t.Errorf("BroadcastAddr() = %q, want %q", got, tc.want)
			}
		})
	}
}
