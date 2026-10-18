package render

import (
	"encoding/json"
	"testing"

	"github.com/randomizedcoder/xtcp2/pkg/xtcpnl"
	"golang.org/x/sys/unix"
)

// briefNames holds the same-namespace peer the link rows resolve.
var briefNames = fakeNames{9: {name: "v1", flags: unix.IFF_UP}}

// briefLink is an up dummy-like link with IFLA_OPERSTATE present.
func briefLink(name string, state uint8) xtcpnl.LinkInfo {
	return xtcpnl.LinkInfo{Index: 3, Name: name, HasOperState: true, OperState: state}
}

// TestLinkBriefText is print_linkinfo_brief's name and state columns.
//
// go test ./internal/goip/render/ -run TestLinkBriefText
func TestLinkBriefText(t *testing.T) {
	veth := func(li xtcpnl.LinkInfo) xtcpnl.LinkInfo {
		li.HasLink, li.Link = true, 9
		return li
	}
	tests := []struct {
		description string
		link        xtcpnl.LinkInfo
		expected    string
	}{
		{description: "positive: a plain link pads the name to 16 and the state to 14",
			link:     briefLink("d0", xtcpnl.IfOperUp),
			expected: "d0               UP             "},
		{description: "boundary: a 15-character name leaves one pad before the separator",
			link:     briefLink("d0123456789abcd", xtcpnl.IfOperUp),
			expected: "d0123456789abcd  UP             "},
		{description: "boundary: name@link longer than the column widens it by the overflow",
			link:     veth(briefLink("v0123456789abcd", xtcpnl.IfOperUp)),
			expected: "v0123456789abcd@v1 UP             "},
		{description: "positive: a same-namespace peer is name@peer",
			link:     veth(briefLink("v0", xtcpnl.IfOperUp)),
			expected: "v0@v1            UP             "},
		{description: "corner: IFLA_LINK 0 is name@NONE",
			link: func() xtcpnl.LinkInfo {
				li := briefLink("t0", xtcpnl.IfOperUnknown)
				li.HasLink = true
				return li
			}(),
			expected: "t0@NONE          UNKNOWN        "},
		{description: "corner: a peer in another namespace is name@ifN",
			link: func() xtcpnl.LinkInfo {
				li := veth(briefLink("v0", xtcpnl.IfOperUp))
				li.Link, li.HasLinkNetnsID = 3, true
				return li
			}(),
			expected: "v0@if3           UP             "},
		{description: "negative: no IFLA_OPERSTATE prints the name column only",
			link:     xtcpnl.LinkInfo{Index: 3, Name: "d0"},
			expected: "d0               "},
		{description: "corner: an operstate past the table prints as state and hex",
			link:     briefLink("d0", 9),
			expected: "d0               state 0x9"},
	}
	for _, tc := range tests {
		t.Run(tc.description, func(t *testing.T) {
			if got := LinkBriefViewOf(tc.link, briefNames).Text(); got != tc.expected {
				t.Errorf("got %q, want %q", got, tc.expected)
			}
		})
	}
}

// TestAddrBriefJSON is the `-br -j addr show` object, member by member.
//
// go test ./internal/goip/render/ -run TestAddrBriefJSON
func TestAddrBriefJSON(t *testing.T) {
	v4addr := xtcpnl.AddrInfo{Family: unix.AF_INET, Prefixlen: 24,
		Local: v4(192, 0, 2, 1), Address: v4(192, 0, 2, 1)}
	peer := xtcpnl.AddrInfo{Family: unix.AF_INET, Prefixlen: 32,
		Local: v4(10, 0, 0, 1), Address: v4(10, 0, 0, 2), HasRtPriority: true, RtPriority: 50}
	view := func(li xtcpnl.LinkInfo, addrs ...xtcpnl.AddrInfo) AddrBriefView {
		g := AddrBriefView{LinkBriefView: LinkBriefViewOf(li, briefNames)}
		for _, a := range addrs {
			g.AddrInfo = append(g.AddrInfo, AddrBriefEntryOf(AddrViewOf(a)))
		}
		return g
	}
	tests := []struct {
		description string
		view        AddrBriefView
		expected    string
	}{
		{description: "positive: ifname, operstate, then addr_info with local and prefixlen",
			view:     view(briefLink("d0", xtcpnl.IfOperUnknown), v4addr),
			expected: `{"ifname":"d0","operstate":"UNKNOWN","addr_info":[{"local":"192.0.2.1","prefixlen":24}]}`},
		{description: "positive: a peer and a metric, in print_addrinfo's order",
			view:     view(briefLink("d0", xtcpnl.IfOperUnknown), peer),
			expected: `{"ifname":"d0","operstate":"UNKNOWN","addr_info":[{"local":"10.0.0.1","address":"10.0.0.2","prefixlen":32,"metric":50}]}`},
		{description: "corner: link comes before ifname",
			view: func() AddrBriefView {
				li := briefLink("v0", xtcpnl.IfOperUp)
				li.HasLink, li.Link = true, 9
				return view(li)
			}(),
			expected: `{"link":"v1","ifname":"v0","operstate":"UP","addr_info":[]}`},
		{description: "corner: @NONE is link null",
			view: func() AddrBriefView {
				li := briefLink("t0", xtcpnl.IfOperUnknown)
				li.HasLink = true
				return view(li)
			}(),
			expected: `{"link":null,"ifname":"t0","operstate":"UNKNOWN","addr_info":[]}`},
		{description: "boundary: no addresses is an empty addr_info",
			view:     view(briefLink("d2", xtcpnl.IfOperUnknown)),
			expected: `{"ifname":"d2","operstate":"UNKNOWN","addr_info":[]}`},
		{description: "corner: an operstate past the table is operstate_index",
			view:     view(briefLink("d0", 9)),
			expected: `{"ifname":"d0","operstate_index":9,"addr_info":[]}`},
	}
	for _, tc := range tests {
		t.Run(tc.description, func(t *testing.T) {
			got, err := json.Marshal(tc.view)
			if err != nil {
				t.Fatal(err)
			}
			if string(got) != tc.expected {
				t.Errorf("got  %s\nwant %s", got, tc.expected)
			}
		})
	}
}

// TestAddrBriefText is the brief line: the link columns, each address, "\n".
//
// go test ./internal/goip/render/ -run TestAddrBriefText
func TestAddrBriefText(t *testing.T) {
	tests := []struct {
		description string
		addrs       []xtcpnl.AddrInfo
		expected    string
	}{
		{description: "positive: two addresses, each with its own trailing space",
			addrs: []xtcpnl.AddrInfo{
				{Family: unix.AF_INET, Prefixlen: 24, Local: v4(192, 0, 2, 1), Address: v4(192, 0, 2, 1)},
				{Family: unix.AF_INET, Prefixlen: 24, Local: v4(192, 0, 2, 9), Address: v4(192, 0, 2, 9)},
			},
			expected: "d0               UNKNOWN        192.0.2.1/24 192.0.2.9/24 \n"},
		{description: "positive: a metric follows the prefix length",
			addrs: []xtcpnl.AddrInfo{{Family: unix.AF_INET, Prefixlen: 24, Local: v4(192, 0, 2, 1),
				Address: v4(192, 0, 2, 1), HasRtPriority: true, RtPriority: 50}},
			expected: "d0               UNKNOWN        192.0.2.1/24 metric 50 \n"},
		{description: "positive: a peer is local peer address/plen",
			addrs:    []xtcpnl.AddrInfo{{Family: unix.AF_INET, Prefixlen: 32, Local: v4(10, 0, 0, 1), Address: v4(10, 0, 0, 2)}},
			expected: "d0               UNKNOWN        10.0.0.1 peer 10.0.0.2/32 \n"},
		{description: "boundary: no addresses is the link columns and the newline",
			expected: "d0               UNKNOWN        \n"},
		{description: "corner: a message with no address adds nothing to the line",
			addrs:    []xtcpnl.AddrInfo{{Family: unix.AF_INET, Multicast: v4(224, 0, 0, 1)}},
			expected: "d0               UNKNOWN        \n"},
	}
	for _, tc := range tests {
		t.Run(tc.description, func(t *testing.T) {
			g := AddrBriefView{LinkBriefView: LinkBriefViewOf(briefLink("d0", xtcpnl.IfOperUnknown), briefNames)}
			for _, a := range tc.addrs {
				g.AddrInfo = append(g.AddrInfo, AddrBriefEntryOf(AddrViewOf(a)))
			}
			if got := g.Text(); got != tc.expected {
				t.Errorf("got %q, want %q", got, tc.expected)
			}
		})
	}
}

// TestAddrOnelineText is the `-o` address line.
//
// go test ./internal/goip/render/ -run TestAddrOnelineText
func TestAddrOnelineText(t *testing.T) {
	forever := xtcpnl.IfaCacheinfo{Preferred: xtcpnl.IfaLifetimeInfinityCst, Valid: xtcpnl.IfaLifetimeInfinityCst}
	tests := []struct {
		description string
		addr        xtcpnl.AddrInfo
		index       int32
		dev         string
		expected    string
	}{
		{description: "positive: a v4 line with a label ends flush against the backslash",
			addr: xtcpnl.AddrInfo{Flags: unix.IFA_F_PERMANENT, Family: unix.AF_INET, Prefixlen: 24, Local: v4(192, 0, 2, 1),
				Address: v4(192, 0, 2, 1), Label: "d0:1", HasCacheInfo: true, CacheInfo: forever},
			index: 3, dev: "d0",
			expected: `3: d0    inet 192.0.2.1/24 scope global d0:1\       valid_lft forever preferred_lft forever` + "\n"},
		{description: "positive: a v6 line keeps its trailing space before the backslash",
			addr: xtcpnl.AddrInfo{Flags: unix.IFA_F_PERMANENT, Family: unix.AF_INET6, Prefixlen: 64, Local: v6(t, "2001:db8::1"),
				Address: v6(t, "2001:db8::1"), HasCacheInfo: true, CacheInfo: forever},
			index: 3, dev: "d0",
			expected: `3: d0    inet6 2001:db8::1/64 scope global \       valid_lft forever preferred_lft forever` + "\n"},
		{description: "boundary: no cacheinfo is one line with no backslash",
			addr: xtcpnl.AddrInfo{Flags: unix.IFA_F_PERMANENT, Family: unix.AF_INET, Prefixlen: 24, Local: v4(192, 0, 2, 1),
				Address: v4(192, 0, 2, 1), Label: "d0"},
			index: 3, dev: "d0",
			expected: "3: d0    inet 192.0.2.1/24 scope global d0\n"},
		{description: "corner: the prefix is the index and the device, with no @link",
			addr: xtcpnl.AddrInfo{Flags: unix.IFA_F_PERMANENT, Family: unix.AF_INET6, Prefixlen: 64, Local: v6(t, "2001:db8::5"),
				Address: v6(t, "2001:db8::5")},
			index: 7, dev: "v0",
			expected: "7: v0    inet6 2001:db8::5/64 scope global \n"},
	}
	for _, tc := range tests {
		t.Run(tc.description, func(t *testing.T) {
			if got := AddrViewOf(tc.addr).OnelineText(tc.index, tc.dev); got != tc.expected {
				t.Errorf("got  %q\nwant %q", got, tc.expected)
			}
		})
	}
}
