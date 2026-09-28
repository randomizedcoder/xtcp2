package render

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/randomizedcoder/xtcp2/pkg/xtcpnl"
	"golang.org/x/sys/unix"
)

// fakeNames is a fixed NameTab, so the renderer tests never touch a socket.
//
// It reproduces the two fallbacks that matter and nothing else: an index it
// does not hold renders as "if%u" and reports -1 flags, which is what the real
// LLTab does and what print_name_and_link's M-DOWN test depends on.
type fakeNames map[int32]struct {
	name  string
	flags uint32
}

func (f fakeNames) IndexToName(idx int32) string {
	if idx == 0 {
		return "*"
	}
	if e, ok := f[idx]; ok {
		return e.name
	}
	return "if" + itoa(idx)
}

func (f fakeNames) IndexToFlags(idx int32) int64 {
	if idx == 0 {
		return 0
	}
	if e, ok := f[idx]; ok {
		return int64(e.flags)
	}
	return -1
}

func itoa(v int32) string {
	if v == 0 {
		return "0"
	}
	neg := v < 0
	var buf [12]byte
	i := len(buf)
	u := v
	if neg {
		u = -u
	}
	for u > 0 {
		i--
		buf[i] = byte('0' + u%10)
		u /= 10
	}
	if neg {
		i--
		buf[i] = '-'
	}
	return string(buf[i:])
}

// TestFlagTokens covers print_link_flags.
//
// Every positive row's expectation is lifted from a `<...>` group in
// pkg/xtcpnl/testdata/7_1_8/ip_link_n, with the raw ifi_flags read out of the
// matching message in the committed dump — so the flag ordering is asserted
// against what `ip` actually printed rather than against the table this file
// also contains.
//
// go test ./internal/goip/render/ -run TestFlagTokens
func TestFlagTokens(t *testing.T) {
	tests := []struct {
		description string
		flags       uint32
		mdown       bool
		want        string
	}{
		{
			// ip_link_n:1 "1: lo: <LOOPBACK,UP,LOWER_UP>"
			description: "positive: lo, flags 0x10049",
			flags:       0x10049,
			want:        "LOOPBACK,UP,LOWER_UP",
		},
		{
			// ip_link_n:3 "2: enp1s0: <BROADCAST,MULTICAST,UP,LOWER_UP>".
			// Note UP sorts after MULTICAST: the table order is iproute2's, not
			// numeric. Reordering linkFlagNames breaks this row.
			description: "positive: enp1s0, flags 0x11043, proving table order is not numeric order",
			flags:       0x11043,
			want:        "BROADCAST,MULTICAST,UP,LOWER_UP",
		},
		{
			// ip_link_n:12 "7: virbr0: <NO-CARRIER,BROADCAST,MULTICAST,UP>".
			// IFF_UP set, IFF_RUNNING clear. NO-CARRIER is synthetic — there is
			// no IFF_NO_CARRIER bit — and it comes first.
			description: "positive: virbr0, flags 0x1003, synthesizes NO-CARRIER first",
			flags:       0x1003,
			want:        "NO-CARRIER,BROADCAST,MULTICAST,UP",
		},
		{
			// ip_link_n:32 "161: nlmon0: <NOARP,UP,LOWER_UP>"
			description: "positive: nlmon0, flags 0x100c1",
			flags:       0x100c1,
			want:        "NOARP,UP,LOWER_UP",
		},
		{
			// IFF_RUNNING is consumed by the NO-CARRIER decision and then
			// cleared, so it is never a token. With UP also set there is no
			// NO-CARRIER either, which leaves nothing at all — the row that
			// proves RUNNING is not simply missing from the table by oversight.
			description: "boundary: IFF_RUNNING alone with IFF_UP produces no token for RUNNING",
			flags:       unix.IFF_UP | unix.IFF_RUNNING,
			want:        "UP",
		},
		{
			description: "boundary: zero flags produce no tokens at all",
			flags:       0,
			want:        "",
		},
		{
			// print_hex(PRINT_ANY, NULL, "%x", flags) for whatever the table
			// did not name — bare lowercase hex, no name, no 0x prefix.
			description: "boundary: an unrecognized bit is printed as bare lowercase hex",
			flags:       unix.IFF_UP | unix.IFF_RUNNING | 0x800000,
			want:        "UP,800000",
		},
		{
			description: "negative: IFF_RUNNING without IFF_UP does not synthesize NO-CARRIER",
			flags:       unix.IFF_RUNNING,
			want:        "",
		},
		{
			// M-DOWN is comma-prefixed unconditionally in the C
			// (`print_string(PRINT_ANY, NULL, ",%s", "M-DOWN")`), so it joins
			// like any other trailing token.
			description: "corner: M-DOWN is appended after everything else",
			flags:       0x11043,
			mdown:       true,
			want:        "BROADCAST,MULTICAST,UP,LOWER_UP,M-DOWN",
		},
		{
			// Faithful to the C: with no other flags, the comma-prefixed
			// M-DOWN is the only token, so `<M-DOWN>` rather than `<,M-DOWN>`
			// — because this function returns tokens and the caller joins
			// them, which is the one place goip's structure tidies iproute2's
			// output rather than reproducing it. Unreachable in practice:
			// m_flag is only computed on a link that has IFLA_LINK, which
			// means it has at least IFF_BROADCAST.
			description: "corner: M-DOWN with no other flags yields it alone, not a leading comma",
			flags:       0,
			mdown:       true,
			want:        "M-DOWN",
		},
	}

	for _, tc := range tests {
		t.Run(tc.description, func(t *testing.T) {
			got := strings.Join(FlagTokens(tc.flags, tc.mdown), ",")
			if got != tc.want {
				t.Errorf("FlagTokens(%#x, %v) = %q, want %q", tc.flags, tc.mdown, got, tc.want)
			}
		})
	}
}

// TestScalarNames covers the three small name tables together, because each is
// a handful of rows and they share the same shape: a table hit, a fallback,
// and the boundary between them.
//
// go test ./internal/goip/render/ -run TestScalarNames
func TestScalarNames(t *testing.T) {
	tests := []struct {
		description string
		got         string
		want        string
	}{
		{
			description: "positive: operstate 6 is UP, as ip_link_n:3 prints",
			got:         operStateName(6), want: "UP",
		},
		{
			description: "positive: operstate 0 is UNKNOWN, as ip_link_n:1 prints for lo",
			got:         operStateName(0), want: "UNKNOWN",
		},
		{
			description: "positive: operstate 2 is DOWN, as ip_link_n:12 prints for virbr0",
			got:         operStateName(2), want: "DOWN",
		},
		{
			description: "boundary: operstate 6 is the last named state",
			got:         operStateName(6), want: "UP",
		},
		{
			// print_0xhex(PRINT_FP, NULL, "state %#llx", state) — so `%#x`,
			// with the 0x prefix, unlike the bare-hex flags fallback.
			description: "boundary: operstate 7 is one past the table and renders as %#x",
			got:         operStateName(7), want: "0x7",
		},
		{
			description: "corner: operstate 255 renders as %#x, not as a panic",
			got:         operStateName(255), want: "0xff",
		},
		{
			description: "positive: linkmode 0 is DEFAULT, which every link in the dump carries",
			got:         linkModeName(0), want: "DEFAULT",
		},
		{
			description: "positive: linkmode 1 is DORMANT",
			got:         linkModeName(1), want: "DORMANT",
		},
		{
			// print_int(PRINT_ANY, "linkmode_index", "mode %d ", mode) —
			// decimal here, not hex, unlike operstate's fallback. The two
			// fallbacks genuinely differ and both rows exist to say so.
			description: "boundary: linkmode 2 is one past the table and renders as decimal",
			got:         linkModeName(2), want: "2",
		},
		{
			description: "positive: group 0 is \"default\", the only entry iproute2 ships",
			got:         groupName(0), want: "default",
		},
		{
			// rtnl_group_n2a falls through to snprintf("%d") for an id with no
			// name, which is also what happens on a host with no group file.
			description: "boundary: group 1 has no shipped name and renders as decimal",
			got:         groupName(1), want: "1",
		},
		{
			description: "corner: a large group id renders as decimal",
			got:         groupName(4294967295), want: "4294967295",
		},
	}

	for _, tc := range tests {
		t.Run(tc.description, func(t *testing.T) {
			if tc.got != tc.want {
				t.Errorf("got %q, want %q", tc.got, tc.want)
			}
		})
	}
}

// TestLinkViewText covers the stanza layout, one constructed link per row.
//
// The end-to-end assertion against the real dump lives in
// internal/goip's obj_link_test.go, where it can diff every line of the
// committed fixture at once. This table is for the shapes the fixture does not
// contain — a point-to-point peer, a negative link-netnsid, a multi-altname
// link — plus the two spacing behaviors that are easiest to break.
//
// go test ./internal/goip/render/ -run TestLinkViewText
func TestLinkViewText(t *testing.T) {
	names := fakeNames{
		2: {name: "enp1s0", flags: unix.IFF_UP | unix.IFF_RUNNING},
		9: {name: "br-3a5828b2963a", flags: unix.IFF_UP | unix.IFF_RUNNING},
		// Deliberately down, for the M-DOWN row.
		5: {name: "downlink0", flags: unix.IFF_BROADCAST},
	}

	tests := []struct {
		description string
		link        xtcpnl.LinkInfo
		want        string
	}{
		{
			// ip_link_n:1-2, the whole lo stanza minus the -d attributes.
			description: "positive: lo renders both lines exactly as ip_link_n:1-2 does",
			link: xtcpnl.LinkInfo{
				Index: 1, Name: "lo", Type: unix.ARPHRD_LOOPBACK, Flags: 0x10049,
				HasTxQLen: true, HasGroup: true,
				MTU: 65536, Qdisc: "noqueue", OperState: 0, TxQLen: 1000,
				Address:   []byte{0, 0, 0, 0, 0, 0},
				Broadcast: []byte{0, 0, 0, 0, 0, 0},
			},
			want: "1: lo: <LOOPBACK,UP,LOWER_UP> mtu 65536 qdisc noqueue state UNKNOWN mode DEFAULT group default qlen 1000\n" +
				"    link/loopback 00:00:00:00:00:00 brd 00:00:00:00:00:00\n",
		},
		{
			// ip_link_n:19-20. IFLA_MASTER is an index, resolved through the
			// cache, and it sits between qdisc and state.
			description: "positive: a bridge member renders `master` between qdisc and state",
			link: xtcpnl.LinkInfo{
				Index: 60, Name: "veth179a698", Type: unix.ARPHRD_ETHER, Flags: 0x11043,
				HasTxQLen: true, HasGroup: true,
				MTU: 1500, Qdisc: "noqueue", OperState: 6, Master: 9,
				Link: 2, LinkNetnsID: 3, HasLinkNetnsID: true,
				Address:   []byte{0xaa, 0x1f, 0xd4, 0x5f, 0xc8, 0xd6},
				Broadcast: []byte{0xff, 0xff, 0xff, 0xff, 0xff, 0xff},
			},
			want: "60: veth179a698@if2: <BROADCAST,MULTICAST,UP,LOWER_UP> mtu 1500 qdisc noqueue master br-3a5828b2963a state UP mode DEFAULT group default \n" +
				"    link/ether aa:1f:d4:5f:c8:d6 brd ff:ff:ff:ff:ff:ff link-netnsid 3\n",
		},
		{
			// **The trailing-space row.** Every field's format string carries
			// its own trailing space, and qlen is suppressed at 0, so the line
			// genuinely ends in a space. ip_link_n:17 shows it for docker0.
			// Trimming it would be a one-character divergence on three of the
			// eleven links.
			description: "boundary: txqlen 0 suppresses qlen and leaves the first line ending in a space",
			link: xtcpnl.LinkInfo{
				Index: 8, Name: "docker0", Type: unix.ARPHRD_ETHER, Flags: 0x1003,
				HasTxQLen: true, HasGroup: true,
				MTU: 1500, Qdisc: "noqueue", OperState: 2, TxQLen: 0,
				Address:   []byte{0xa6, 0x56, 0x62, 0x1d, 0x73, 0x01},
				Broadcast: []byte{0xff, 0xff, 0xff, 0xff, 0xff, 0xff},
			},
			want: "8: docker0: <NO-CARRIER,BROADCAST,MULTICAST,UP> mtu 1500 qdisc noqueue state DOWN mode DEFAULT group default \n" +
				"    link/ether a6:56:62:1d:73:01 brd ff:ff:ff:ff:ff:ff\n",
		},
		{
			// **The no-MAC row.** ip_link_n:33 is "link/netlink  promiscuity 0",
			// i.e. the "    link/%s " prefix with its trailing space and
			// nothing after it. `ip` neither omits the line nor prints
			// "link/none", which was an open question in the plan's §8.7 and
			// is answered here from the sidecar.
			description: "boundary: a link with no IFLA_ADDRESS prints the link/ prefix and stops, trailing space and all",
			link: xtcpnl.LinkInfo{
				Index: 161, Name: "nlmon0", Type: 824, Flags: 0x100c1,
				HasTxQLen: true, HasGroup: true,
				MTU: 3776, Qdisc: "noqueue", OperState: 0, TxQLen: 1000,
			},
			want: "161: nlmon0: <NOARP,UP,LOWER_UP> mtu 3776 qdisc noqueue state UNKNOWN mode DEFAULT group default qlen 1000\n" +
				"    link/netlink \n",
		},
		{
			// A shape the fixture has none of: IFF_POINTOPOINT makes the
			// second address a peer rather than a broadcast, and iproute2
			// prints " peer " instead of " brd " (ip/ipaddress.c:1075-1083).
			description: "boundary: a point-to-point link prints ` peer ` instead of ` brd `",
			link: xtcpnl.LinkInfo{
				Index: 20, Name: "ppp0", Type: unix.ARPHRD_PPP,
				HasTxQLen: true, HasGroup: true,
				// IFF_LOWER_UP is a distinct bit from IFF_RUNNING: RUNNING is
				// consumed by the NO-CARRIER synthesis and then never named as a
				// token, while LOWER_UP is a token of its own. A real
				// up-with-carrier link carries both, and leaving LOWER_UP out of
				// this row is what it went red on the first time it ran.
				Flags: unix.IFF_UP | unix.IFF_RUNNING | unix.IFF_LOWER_UP | unix.IFF_POINTOPOINT,
				MTU:   1492, Qdisc: "noqueue", OperState: 6,
				Address:   []byte{0x0a, 0x00, 0x00, 0x01},
				Broadcast: []byte{0x0a, 0x00, 0x00, 0x02},
			},
			want: "20: ppp0: <POINTOPOINT,UP,LOWER_UP> mtu 1492 qdisc noqueue state UP mode DEFAULT group default \n" +
				"    link/ppp 0a:00:00:01 peer 0a:00:00:02\n",
		},
		{
			// IFLA_LINK_NETNSID of -1 is a value the kernel really sends,
			// meaning "the peer is in a namespace I cannot name". `ip` prints
			// the word rather than the number, which is why HasLinkNetnsID
			// exists separately from the value.
			description: "corner: link-netnsid -1 renders the word `unknown`, not the number",
			link: xtcpnl.LinkInfo{
				Index: 61, Name: "vethx", Type: unix.ARPHRD_ETHER, Flags: 0x11043,
				HasTxQLen: true, HasGroup: true,
				MTU: 1500, Qdisc: "noqueue", OperState: 6, TxQLen: 1000,
				Link: 2, LinkNetnsID: -1, HasLinkNetnsID: true,
				Address:   []byte{0x02, 0, 0, 0, 0, 0x01},
				Broadcast: []byte{0xff, 0xff, 0xff, 0xff, 0xff, 0xff},
			},
			want: "61: vethx@if2: <BROADCAST,MULTICAST,UP,LOWER_UP> mtu 1500 qdisc noqueue state UP mode DEFAULT group default qlen 1000\n" +
				"    link/ether 02:00:00:00:00:01 brd ff:ff:ff:ff:ff:ff link-netnsid unknown\n",
		},
		{
			// **The M-DOWN row, and the reason IFLA_LINK_NETNSID is absent
			// here.** With no netnsid, print_name_and_link resolves the peer's
			// name (so `@downlink0`, not `@if5`) *and* computes M-DOWN from the
			// peer's cached flags. The fixture contains no such link, because
			// all three of its veths carry a netnsid.
			description: "corner: with no netnsid the peer name is resolved and a down peer yields M-DOWN",
			link: xtcpnl.LinkInfo{
				Index: 70, Name: "macvlan0", Type: unix.ARPHRD_ETHER, Flags: 0x11043,
				HasTxQLen: true, HasGroup: true,
				MTU: 1500, Qdisc: "noqueue", OperState: 6, TxQLen: 1000,
				Link:      5,
				Address:   []byte{0x02, 0, 0, 0, 0, 0x02},
				Broadcast: []byte{0xff, 0xff, 0xff, 0xff, 0xff, 0xff},
			},
			want: "70: macvlan0@downlink0: <BROADCAST,MULTICAST,UP,LOWER_UP,M-DOWN> mtu 1500 qdisc noqueue state UP mode DEFAULT group default qlen 1000\n" +
				"    link/ether 02:00:00:00:00:02 brd ff:ff:ff:ff:ff:ff\n",
		},
		{
			// The companion to the row above: an *uncached* peer index. -1 &
			// IFF_UP is non-zero, so there is no M-DOWN, and the name falls
			// back to if%u. Getting the -1 wrong flips exactly this row.
			description: "corner: an uncached peer index gives if%u and NO M-DOWN, because -1 & IFF_UP != 0",
			link: xtcpnl.LinkInfo{
				Index: 71, Name: "macvlan1", Type: unix.ARPHRD_ETHER, Flags: 0x11043,
				HasTxQLen: true, HasGroup: true,
				MTU: 1500, Qdisc: "noqueue", OperState: 6, TxQLen: 1000,
				Link:      404,
				Address:   []byte{0x02, 0, 0, 0, 0, 0x03},
				Broadcast: []byte{0xff, 0xff, 0xff, 0xff, 0xff, 0xff},
			},
			want: "71: macvlan1@if404: <BROADCAST,MULTICAST,UP,LOWER_UP> mtu 1500 qdisc noqueue state UP mode DEFAULT group default qlen 1000\n" +
				"    link/ether 02:00:00:00:00:03 brd ff:ff:ff:ff:ff:ff\n",
		},
		{
			// Several altnames, each on its own continuation line. The fixture
			// has four links with exactly one each, so the multi case is only
			// reachable here.
			description: "corner: several altnames each get their own continuation line",
			link: xtcpnl.LinkInfo{
				Index: 80, Name: "eth0", Type: unix.ARPHRD_ETHER, Flags: 0x11043,
				HasTxQLen: true, HasGroup: true,
				MTU: 1500, Qdisc: "mq", OperState: 6, TxQLen: 1000,
				Address:   []byte{0x02, 0, 0, 0, 0, 0x04},
				Broadcast: []byte{0xff, 0xff, 0xff, 0xff, 0xff, 0xff},
				AltNames:  []string{"enp0s1", "enx020000000004"},
			},
			want: "80: eth0: <BROADCAST,MULTICAST,UP,LOWER_UP> mtu 1500 qdisc mq state UP mode DEFAULT group default qlen 1000\n" +
				"    link/ether 02:00:00:00:00:04 brd ff:ff:ff:ff:ff:ff\n" +
				"    altname enp0s1\n" +
				"    altname enx020000000004\n",
		},
		{
			// An unnamed ARPHRD. ll_type_n2a's fallback is "[%d]" — decimal
			// and bracketed. ARPHRD_RAWIP (519) is the probe that works,
			// because iproute2 names both 0xffff and 0xfffe.
			description: "negative: an ARPHRD iproute2 does not name renders as [519], decimal in brackets",
			link: xtcpnl.LinkInfo{
				Index: 90, Name: "rawip0", Type: 519, Flags: unix.IFF_UP | unix.IFF_RUNNING,
				HasTxQLen: true, HasGroup: true,
				MTU: 1500, Qdisc: "noop", OperState: 6,
			},
			want: "90: rawip0: <UP> mtu 1500 qdisc noop state UP mode DEFAULT group default \n" +
				"    link/[519] \n",
		},
	}

	for _, tc := range tests {
		t.Run(tc.description, func(t *testing.T) {
			got := LinkViewOf(tc.link, names).Text()
			if got != tc.want {
				t.Errorf("Text() mismatch\n got: %q\nwant: %q", got, tc.want)
			}
		})
	}
}

// TestRenderQlenZero covers the faceb326 version skew explicitly, both ways.
//
// go test ./internal/goip/render/ -run TestRenderQlenZero
func TestRenderQlenZero(t *testing.T) {
	link := xtcpnl.LinkInfo{
		Index: 8, Name: "docker0", Type: unix.ARPHRD_ETHER, Flags: 0x1003,
		HasTxQLen: true, HasGroup: true,
		MTU: 1500, Qdisc: "noqueue", OperState: 2, TxQLen: 0,
	}

	tests := []struct {
		description string
		knob        bool
		wantSuffix  string
	}{
		{
			// The default, the parity target, and what ip_link_n:17 shows.
			description: "positive: the default suppresses qlen 0, matching every released ip",
			knob:        false,
			wantSuffix:  "group default \n",
		},
		{
			// Post-faceb326 behavior: the `if (qlen)` guard is gone and the
			// print is unconditional once IFLA_TXQLEN is present.
			description: "boundary: with the knob set, qlen 0 is printed, matching post-faceb326 ip",
			knob:        true,
			wantSuffix:  "group default qlen 0\n",
		},
	}

	for _, tc := range tests {
		t.Run(tc.description, func(t *testing.T) {
			old := RenderQlenZero
			RenderQlenZero = tc.knob
			defer func() { RenderQlenZero = old }()

			got := LinkViewOf(link, fakeNames{}).Text()
			firstLine := got[:strings.Index(got, "\n")+1]
			if !strings.HasSuffix(firstLine, tc.wantSuffix) {
				t.Errorf("first line %q does not end with %q", firstLine, tc.wantSuffix)
			}
		})
	}
}

// TestLinkViewJSON covers the JSON key names, which are `ip -j link show`'s.
//
// Key *order* is deliberately not asserted: the informational diff is
// `jq -S` on both sides, so order is irrelevant, and iproute2's order follows
// its print_* call sequence and is not worth reproducing. What is asserted is
// membership and the omission rules, because a key that appears when `ip`
// omits it — or a null where `ip` has nothing — shows up in a sorted diff.
//
// go test ./internal/goip/render/ -run TestLinkViewJSON
func TestLinkViewJSON(t *testing.T) {
	names := fakeNames{5: {name: "downlink0", flags: unix.IFF_BROADCAST}}

	tests := []struct {
		description string
		link        xtcpnl.LinkInfo
		wantKeys    []string
		wantAbsent  []string
	}{
		{
			description: "positive: lo carries the base key set",
			link: xtcpnl.LinkInfo{
				Index: 1, Name: "lo", Type: unix.ARPHRD_LOOPBACK, Flags: 0x10049,
				HasTxQLen: true, HasGroup: true,
				MTU: 65536, Qdisc: "noqueue", TxQLen: 1000,
				Address:   []byte{0, 0, 0, 0, 0, 0},
				Broadcast: []byte{0, 0, 0, 0, 0, 0},
			},
			wantKeys: []string{
				"ifindex", "ifname", "flags", "mtu", "qdisc",
				"operstate", "linkmode", "group", "txqlen",
				"link_type", "address", "broadcast",
			},
			// No peer, no master, no namespace, no altnames.
			wantAbsent: []string{"link", "link_index", "master", "link_netnsid", "altnames"},
		},
		{
			// The netnsid path puts the peer's *index* in JSON, because the
			// name is meaningless outside its namespace — `ip` prints
			// "link_index" here and "link" on the other path, and the two are
			// mutually exclusive.
			description: "positive: a veth with a netnsid uses link_index, not link",
			link: xtcpnl.LinkInfo{
				Index: 58, Name: "ve-nfb-vpn", Type: unix.ARPHRD_ETHER, Flags: 0x11043,
				HasTxQLen: true, HasGroup: true,
				MTU: 1500, Qdisc: "noqueue", OperState: 6, TxQLen: 1000,
				Link: 2, LinkNetnsID: 1, HasLinkNetnsID: true,
			},
			wantKeys:   []string{"link_index", "link_netnsid"},
			wantAbsent: []string{"link"},
		},
		{
			description: "positive: the resolving path uses link, not link_index",
			link: xtcpnl.LinkInfo{
				Index: 70, Name: "macvlan0", Type: unix.ARPHRD_ETHER, Flags: 0x11043,
				HasTxQLen: true, HasGroup: true,
				MTU: 1500, Link: 5,
			},
			wantKeys:   []string{"link"},
			wantAbsent: []string{"link_index", "link_netnsid"},
		},
		{
			// **txqlen 0 is present in JSON even though the text form omits
			// it.** The pointer field is what makes that possible: omitempty on
			// a plain uint32 would drop a real 0, and `ip -j` does emit
			// "txqlen":0 for docker0 — the text suppression is print_queuelen's
			// doing, not the data model's.
			description: "boundary: txqlen 0 is present in JSON even though the text form suppresses it",
			link: xtcpnl.LinkInfo{
				Index: 8, Name: "docker0", Type: unix.ARPHRD_ETHER, Flags: 0x1003,
				HasTxQLen: true, HasGroup: true,
				MTU: 1500, TxQLen: 0,
			},
			wantKeys: []string{"txqlen"},
		},
		{
			// link_netnsid -1 must still appear, as a number. It is the JSON
			// form of "link-netnsid unknown", and omitting it would lose the
			// distinction from a link with no peer namespace at all.
			description: "boundary: link_netnsid -1 is present as a number, not omitted",
			link: xtcpnl.LinkInfo{
				Index: 61, Name: "vethx", Type: unix.ARPHRD_ETHER,
				HasTxQLen: true, HasGroup: true,
				Link: 2, LinkNetnsID: -1, HasLinkNetnsID: true,
			},
			wantKeys: []string{"link_netnsid"},
		},
		{
			description: "corner: a point-to-point link adds link_pointtopoint",
			link: xtcpnl.LinkInfo{
				Index: 20, Name: "ppp0", Type: unix.ARPHRD_PPP,
				HasTxQLen: true, HasGroup: true,
				Flags:     unix.IFF_UP | unix.IFF_RUNNING | unix.IFF_POINTOPOINT,
				Broadcast: []byte{0x0a, 0, 0, 2},
			},
			wantKeys: []string{"link_pointtopoint", "broadcast"},
		},
		{
			// A link with no IFLA_ADDRESS omits "address" rather than emitting
			// an empty string, which is what `ip -j` does for nlmon0.
			description: "negative: a link with no MAC omits the address key rather than emitting \"\"",
			link: xtcpnl.LinkInfo{
				Index: 161, Name: "nlmon0", Type: 824, Flags: 0x100c1, MTU: 3776,
				HasTxQLen: true, HasGroup: true,
			},
			wantAbsent: []string{"address", "broadcast"},
			wantKeys:   []string{"link_type"},
		},
	}

	for _, tc := range tests {
		t.Run(tc.description, func(t *testing.T) {
			b, err := json.Marshal(LinkViewOf(tc.link, names))
			if err != nil {
				t.Fatalf("marshal: %v", err)
			}
			var m map[string]any
			if err := json.Unmarshal(b, &m); err != nil {
				t.Fatalf("unmarshal: %v", err)
			}
			for _, k := range tc.wantKeys {
				if _, ok := m[k]; !ok {
					t.Errorf("key %q missing from %s", k, b)
				}
			}
			for _, k := range tc.wantAbsent {
				if _, ok := m[k]; ok {
					t.Errorf("key %q should be absent, got %s", k, b)
				}
			}
		})
	}
}
