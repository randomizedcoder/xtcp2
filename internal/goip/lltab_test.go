package goip

import (
	"testing"

	"github.com/randomizedcoder/xtcp2/pkg/xtcpnl"
	"golang.org/x/sys/unix"
)

// fixtureLinks is a small stand-in for a filled cache, shaped after the
// committed 7.1.8 dump: an up loopback, an up NIC with an altname, and a
// down bridge.
func fixtureLinks() []xtcpnl.LinkInfo {
	return []xtcpnl.LinkInfo{
		{Index: 1, Name: "lo", Type: unix.ARPHRD_LOOPBACK,
			Flags: unix.IFF_UP | unix.IFF_RUNNING | unix.IFF_LOOPBACK},
		{Index: 2, Name: "enp1s0", Type: unix.ARPHRD_ETHER,
			Flags:    unix.IFF_UP | unix.IFF_RUNNING | unix.IFF_BROADCAST,
			AltNames: []string{"enxe04f43e628ef"}},
		{Index: 8, Name: "docker0", Type: unix.ARPHRD_ETHER,
			Flags: unix.IFF_BROADCAST},
	}
}

// TestLLTabIndexToName covers ll_index_to_name's three answers.
//
// go test ./internal/goip/ -run TestLLTabIndexToName
func TestLLTabIndexToName(t *testing.T) {
	tab := NewLLTab()
	tab.Fill(fixtureLinks())

	tests := []struct {
		description string
		idx         int32
		want        string
	}{
		{
			description: "positive: a cached index resolves to its name",
			idx:         2, want: "enp1s0",
		},
		{
			description: "positive: index 1 is lo",
			idx:         1, want: "lo",
		},
		{
			// lib/ll_map.c:313-314. Index 0 is the kernel's "no interface",
			// and `ip` prints an asterisk for it rather than an error or an
			// empty string. It is reachable: RTA_OIF can be 0.
			description: "boundary: index 0 is \"*\", not \"\" and not an error",
			idx:         0, want: "*",
		},
		{
			// lib/ll_map.c:327. An index the cache does not hold renders as
			// "if%u". For a read-only show this means the link appeared after
			// the dump was taken.
			description: "boundary: an uncached index renders as if%u",
			idx:         42, want: "if42",
		},
		{
			// Negative indices are not legal interface indices, but the field
			// is signed and a corrupt message could carry one. The fallback
			// must produce something rather than panic.
			description: "corner: a negative index still renders rather than panicking",
			idx:         -1, want: "if-1",
		},
		{
			description: "corner: a cached index whose name is empty falls through to if%u",
			idx:         99, want: "if99",
		},
	}

	// The last row needs an entry with a cached-but-empty name, which a real
	// dump never produces (IFLA_IFNAME is on all 11 links) but a truncated
	// message could.
	tab.Fill([]xtcpnl.LinkInfo{{Index: 99, Name: ""}})

	for _, tc := range tests {
		t.Run(tc.description, func(t *testing.T) {
			if got := tab.IndexToName(tc.idx); got != tc.want {
				t.Errorf("IndexToName(%d) = %q, want %q", tc.idx, got, tc.want)
			}
		})
	}
}

// TestLLTabIndexToFlags is the one the plan singles out, and the -1 row is
// the reason.
//
// go test ./internal/goip/ -run TestLLTabIndexToFlags
func TestLLTabIndexToFlags(t *testing.T) {
	tab := NewLLTab()
	tab.Fill(fixtureLinks())

	tests := []struct {
		description string
		idx         int32
		wantFlags   int64
		// wantMDown is the value print_name_and_link would compute from these
		// flags: m_flag = !(flags & IFF_UP).
		wantMDown bool
	}{
		{
			description: "positive: an up interface's flags have IFF_UP, so no M-DOWN",
			idx:         2,
			wantFlags:   unix.IFF_UP | unix.IFF_RUNNING | unix.IFF_BROADCAST,
			wantMDown:   false,
		},
		{
			description: "positive: a down interface's flags lack IFF_UP, so M-DOWN",
			idx:         8,
			wantFlags:   unix.IFF_BROADCAST,
			wantMDown:   true,
		},
		{
			// lib/ll_map.c:347-348 returns 0 for index 0, and 0 & IFF_UP is 0,
			// so index 0 does yield M-DOWN. Distinct from the miss below, and
			// that is the point of having both rows.
			description: "boundary: index 0 returns 0 flags, which does yield M-DOWN",
			idx:         0,
			wantFlags:   0,
			wantMDown:   true,
		},
		{
			// **The corner row the whole method signature exists for.**
			// ll_index_to_flags returns -1 on a miss. `-1 & IFF_UP` is IFF_UP,
			// which is non-zero, so `m_flag = !(...)` is 0 and there is NO
			// M-DOWN. A cache miss therefore behaves like "the peer is up".
			//
			// Returning 0 for "not found" — the natural Go reflex, and what a
			// (uint32, bool) signature would invite — inverts this and prints
			// M-DOWN on every peer the cache cannot resolve. This row is the
			// executable form of that warning: flip the -1 to 0 in
			// IndexToFlags and it goes red on wantMDown, not on wantFlags.
			description: "corner: a cache miss returns -1, and -1 & IFF_UP != 0 means NO M-DOWN",
			idx:         42,
			wantFlags:   -1,
			wantMDown:   false,
		},
		{
			description: "negative: a negative index is a miss, so -1 and no M-DOWN",
			idx:         -7,
			wantFlags:   -1,
			wantMDown:   false,
		},
	}

	for _, tc := range tests {
		t.Run(tc.description, func(t *testing.T) {
			got := tab.IndexToFlags(tc.idx)
			if got != tc.wantFlags {
				t.Errorf("IndexToFlags(%d) = %d, want %d", tc.idx, got, tc.wantFlags)
			}
			if mdown := got&unix.IFF_UP == 0; mdown != tc.wantMDown {
				t.Errorf("m_flag from %d = %v, want %v", got, mdown, tc.wantMDown)
			}
		})
	}
}

// TestLLTabIndexToType covers ll_index_to_type, which until the neigh lladdr
// work had no callers and no test.
//
// # Why the sentinel question is different here
//
// IndexToFlags needs its -1 because 0 and "missing" render differently. This
// method has no sentinel and needs none: ll_index_to_type answers 0 on a miss,
// 0 is ARPHRD_NETROM, and ARPHRD_NETROM has no special case in ll_addr_n2a —
// so a miss and an unremarkable type produce the same output. The two rows at
// the bottom assert exactly that equivalence rather than assuming it, because
// "these two cases are indistinguishable" is a claim that can rot.
//
// The fixture is extended locally rather than in fixtureLinks so the other
// tests in this file keep the corpus they were written against.
//
// go test ./internal/goip/ -run TestLLTabIndexToType
func TestLLTabIndexToType(t *testing.T) {
	links := append(fixtureLinks(),
		xtcpnl.LinkInfo{Index: 10, Name: "gre1", Type: unix.ARPHRD_IPGRE},
		xtcpnl.LinkInfo{Index: 12, Name: "ip6tnl1", Type: unix.ARPHRD_TUNNEL6},
		// A device whose type really is ARPHRD_NETROM, so the "miss looks
		// like zero" rows below have a genuine zero to compare against.
		xtcpnl.LinkInfo{Index: 14, Name: "nr0", Type: unix.ARPHRD_NETROM},
	)
	tab := NewLLTab()
	tab.Fill(links)

	tests := []struct {
		description string
		idx         int32
		want        uint16
	}{
		{
			description: "positive: a tunnel device reports ARPHRD_IPGRE, the type ll_addr_n2a keys on",
			idx:         10,
			want:        unix.ARPHRD_IPGRE,
		},
		{
			description: "positive: a v6 tunnel reports ARPHRD_TUNNEL6",
			idx:         12,
			want:        unix.ARPHRD_TUNNEL6,
		},
		{
			description: "positive: an ordinary NIC reports ARPHRD_ETHER",
			idx:         2,
			want:        unix.ARPHRD_ETHER,
		},
		{
			description: "positive: loopback reports ARPHRD_LOOPBACK",
			idx:         1,
			want:        unix.ARPHRD_LOOPBACK,
		},
		{
			// ARPHRD_NETROM is 0. A cached device of this type is
			// indistinguishable from a miss, by construction and not by
			// accident — see the two rows below.
			description: "boundary: a cached ARPHRD_NETROM device reports 0",
			idx:         14,
			want:        unix.ARPHRD_NETROM,
		},
		{
			description: "negative: an uncached index reports 0, the same as ARPHRD_NETROM",
			idx:         42,
			want:        0,
		},
		{
			description: "negative: a negative index is a miss, so 0",
			idx:         -7,
			want:        0,
		},
		{
			// Index 0 is not a valid interface index and reaches the same
			// zero value. Worth its own row because IndexToName and
			// IndexToFlags both special-case it and this method does not.
			description: "boundary: index 0 reports 0 with no special case",
			idx:         0,
			want:        0,
		},
	}

	for _, tc := range tests {
		t.Run(tc.description, func(t *testing.T) {
			if got := tab.IndexToType(tc.idx); got != tc.want {
				t.Errorf("IndexToType(%d) = %d, want %d", tc.idx, got, tc.want)
			}
		})
	}

	// The equivalence the missing sentinel rests on, asserted once rather
	// than implied by two rows that happen to share a number.
	if tab.IndexToType(14) != tab.IndexToType(42) {
		t.Errorf("a cached ARPHRD_NETROM (%d) and a miss (%d) differ; the no-sentinel design assumes they do not",
			tab.IndexToType(14), tab.IndexToType(42))
	}
}

// TestLLTabNameToIndex covers name resolution, including altnames.
//
// go test ./internal/goip/ -run TestLLTabNameToIndex
func TestLLTabNameToIndex(t *testing.T) {
	tab := NewLLTab()
	tab.Fill(fixtureLinks())

	tests := []struct {
		description string
		name        string
		want        int32
	}{
		{
			description: "positive: a primary name resolves",
			name:        "enp1s0", want: 2,
		},
		{
			// `ip link show dev enxe04f43e628ef` works because
			// ll_name_to_index falls through to if_nametoindex, which the
			// kernel answers for alternative names too. Registering them here
			// is what stops goip rejecting a name `ip` accepts.
			description: "positive: an alternative name resolves to the same index",
			name:        "enxe04f43e628ef", want: 2,
		},
		{
			// 0 is iproute2's "no such device" answer, not an error value.
			description: "negative: an unknown name is 0",
			name:        "nosuch0", want: 0,
		},
		{
			description: "negative: the empty name is 0",
			name:        "", want: 0,
		},
		{
			description: "corner: name lookup is case-sensitive",
			name:        "ENP1S0", want: 0,
		},
	}

	for _, tc := range tests {
		t.Run(tc.description, func(t *testing.T) {
			if got := tab.NameToIndex(tc.name); got != tc.want {
				t.Errorf("NameToIndex(%q) = %d, want %d", tc.name, got, tc.want)
			}
		})
	}
}

// TestLLTabFillReplaces covers the update semantics, which are deliberately
// NOT the first-wins rule the attribute decoders use.
//
// go test ./internal/goip/ -run TestLLTabFillReplaces
func TestLLTabFillReplaces(t *testing.T) {
	tests := []struct {
		description string
		fills       [][]xtcpnl.LinkInfo
		idx         int32
		wantName    string
	}{
		{
			// ll_remember_index is an update path: a later RTM_NEWLINK for the
			// same index is newer information, so it replaces. That is the
			// opposite of parse_rtattr's first-wins rule for *attributes*
			// inside one message, and conflating the two would make a rename
			// event invisible.
			description: "positive: a later Fill replaces an earlier entry for the same index",
			fills: [][]xtcpnl.LinkInfo{
				{{Index: 3, Name: "old0"}},
				{{Index: 3, Name: "new0"}},
			},
			idx: 3, wantName: "new0",
		},
		{
			description: "boundary: Fill with an empty slice changes nothing",
			fills: [][]xtcpnl.LinkInfo{
				{{Index: 3, Name: "keep0"}},
				{},
			},
			idx: 3, wantName: "keep0",
		},
		{
			description: "corner: Fill with a nil slice changes nothing",
			fills: [][]xtcpnl.LinkInfo{
				{{Index: 3, Name: "keep0"}},
				nil,
			},
			idx: 3, wantName: "keep0",
		},
	}

	for _, tc := range tests {
		t.Run(tc.description, func(t *testing.T) {
			tab := NewLLTab()
			for _, f := range tc.fills {
				tab.Fill(f)
			}
			if got := tab.IndexToName(tc.idx); got != tc.wantName {
				t.Errorf("IndexToName(%d) = %q, want %q", tc.idx, got, tc.wantName)
			}
		})
	}
}
