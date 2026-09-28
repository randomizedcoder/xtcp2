package render

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/randomizedcoder/xtcp2/pkg/xtcpnl"
	"golang.org/x/sys/unix"
)

// The four positive rows below are the four lines of
// pkg/xtcpnl/testdata/7_1_4/dumps/ip_neigh_n, the `ip neigh show` sidecar
// captured in the gated microVM topology, transcribed token for token
// including the trailing space `ip` leaves on every line:
//
//	192.0.2.50 dev goip0 lladdr 02:00:00:00:00:01 PERMANENT
//	192.0.2.51 dev goip0 lladdr 02:00:00:00:00:02 STALE
//	192.0.2.52 dev goip0 INCOMPLETE
//	2001:db8::50 dev goip0 lladdr 02:00:00:00:00:03 PERMANENT
//
// The third line is the one worth naming: an INCOMPLETE entry has no
// link-layer address yet, so `lladdr` is absent rather than empty, and a
// renderer that printed `lladdr ` unconditionally would diverge on it.

var neighTabNames = fakeNames{
	1: {name: "lo"},
	3: {name: "goip0"},
}

func TestNeighViewOfText(t *testing.T) {
	tests := []struct {
		description string
		in          xtcpnl.NeighInfo
		want        string
	}{
		{
			description: "positive: ip_neigh_n:1 — a permanent v4 entry with a link-layer address",
			in: xtcpnl.NeighInfo{
				Family: unix.AF_INET, Ifindex: 3, State: unix.NUD_PERMANENT,
				Dst:    []byte{192, 0, 2, 50},
				LLAddr: []byte{0x02, 0, 0, 0, 0, 0x01},
			},
			want: "192.0.2.50 dev goip0 lladdr 02:00:00:00:00:01 PERMANENT \n",
		},
		{
			description: "positive: ip_neigh_n:2 — a stale v4 entry",
			in: xtcpnl.NeighInfo{
				Family: unix.AF_INET, Ifindex: 3, State: unix.NUD_STALE,
				Dst:    []byte{192, 0, 2, 51},
				LLAddr: []byte{0x02, 0, 0, 0, 0, 0x02},
			},
			want: "192.0.2.51 dev goip0 lladdr 02:00:00:00:00:02 STALE \n",
		},
		{
			// The absence case. An INCOMPLETE entry is mid-resolution, so
			// NDA_LLADDR has not arrived and the token must not be emitted.
			description: "negative: ip_neigh_n:3 — an incomplete entry prints no lladdr token",
			in: xtcpnl.NeighInfo{
				Family: unix.AF_INET, Ifindex: 3, State: unix.NUD_INCOMPLETE,
				Dst: []byte{192, 0, 2, 52},
			},
			want: "192.0.2.52 dev goip0 INCOMPLETE \n",
		},
		{
			description: "positive: ip_neigh_n:4 — a v6 entry renders its address in v6 form",
			in: xtcpnl.NeighInfo{
				Family: unix.AF_INET6, Ifindex: 3, State: unix.NUD_PERMANENT,
				Dst: []byte{
					0x20, 0x01, 0x0d, 0xb8, 0, 0, 0, 0,
					0, 0, 0, 0, 0, 0, 0, 0x50,
				},
				LLAddr: []byte{0x02, 0, 0, 0, 0, 0x03},
			},
			want: "2001:db8::50 dev goip0 lladdr 02:00:00:00:00:03 PERMANENT \n",
		},
		{
			// ndm_state is a bitmask, not an enum, so more than one bit can be
			// set and `ip` prints every name. NUD_REACHABLE|NUD_PERMANENT is
			// not a configuration the topology produces, which is why this row
			// is constructed rather than transcribed.
			description: "corner: a multi-bit state prints every name, space separated",
			in: xtcpnl.NeighInfo{
				Family: unix.AF_INET, Ifindex: 3,
				State: unix.NUD_REACHABLE | unix.NUD_PERMANENT,
				Dst:   []byte{192, 0, 2, 53},
			},
			want: "192.0.2.53 dev goip0 REACHABLE PERMANENT \n",
		},
		{
			// NudStateString maps a zero mask to NUD_NONE, and the NUD_ prefix
			// strip leaves "NONE". `ip` renders a zero state as the empty
			// string, so this is a documented divergence rather than parity —
			// and unreachable from a kernel, which never dumps state 0.
			description: "boundary: a zero state renders NONE, xtcpnl's name for an empty mask",
			in: xtcpnl.NeighInfo{
				Family: unix.AF_INET, Ifindex: 3, Dst: []byte{192, 0, 2, 54},
			},
			want: "192.0.2.54 dev goip0 NONE \n",
		},
		{
			// An index the cache does not hold falls back to if%u, exactly as
			// ll_index_to_name does when its live single-get finds nothing.
			description: "negative: an unresolvable ifindex renders the if%u fallback",
			in: xtcpnl.NeighInfo{
				Family: unix.AF_INET, Ifindex: 99, State: unix.NUD_STALE,
				Dst: []byte{192, 0, 2, 55},
			},
			want: "192.0.2.55 dev if99 STALE \n",
		},
		{
			// A neighbor with no NDA_DST at all. The kernel does not dump one,
			// but netip.AddrFromSlice fails on a nil slice and the renderer
			// must leave Dst empty rather than panic or invent 0.0.0.0.
			description: "corner: a missing NDA_DST leaves the destination empty",
			in: xtcpnl.NeighInfo{
				Family: unix.AF_INET, Ifindex: 3, State: unix.NUD_STALE,
			},
			want: " dev goip0 STALE \n",
		},
		{
			// A 3-byte destination is neither 4 nor 16 bytes, so
			// netip.AddrFromSlice rejects it. Truncated attributes are the one
			// input a hostile sender fully controls, so the renderer must drop
			// the value rather than reinterpret the bytes.
			description: "corner: a destination that is neither 4 nor 16 bytes is dropped, not reinterpreted",
			in: xtcpnl.NeighInfo{
				Family: unix.AF_INET, Ifindex: 3, State: unix.NUD_STALE,
				Dst: []byte{192, 0, 2},
			},
			want: " dev goip0 STALE \n",
		},
		{
			// A link with no link-layer address — a tunnel or an IPv6-over-
			// nothing device — gives NDA_LLADDR length 0. len(LLAddr) > 0
			// gates the token, so an empty-but-present attribute prints
			// nothing, which is what `ip` does with it.
			description: "boundary: a zero-length NDA_LLADDR prints no lladdr token",
			in: xtcpnl.NeighInfo{
				Family: unix.AF_INET, Ifindex: 3, State: unix.NUD_REACHABLE,
				Dst: []byte{192, 0, 2, 56}, LLAddr: []byte{},
			},
			want: "192.0.2.56 dev goip0 REACHABLE \n",
		},
		{
			// InfiniBand link-layer addresses are 20 bytes, so the colon
			// grouping must not assume six octets.
			description: "boundary: a 20-byte link-layer address is fully colon-grouped",
			in: xtcpnl.NeighInfo{
				Family: unix.AF_INET, Ifindex: 3, State: unix.NUD_REACHABLE,
				Dst: []byte{192, 0, 2, 57},
				LLAddr: []byte{
					0x80, 0x00, 0x02, 0x08, 0xfe, 0x80, 0x00, 0x00, 0x00, 0x00,
					0x00, 0x00, 0x00, 0x11, 0x22, 0x33, 0x44, 0x55, 0x66, 0x77,
				},
			},
			want: "192.0.2.57 dev goip0 lladdr 80:00:02:08:fe:80:00:00:00:00:00:00:00:11:22:33:44:55:66:77 REACHABLE \n",
		},
	}

	for _, tc := range tests {
		t.Run(tc.description, func(t *testing.T) {
			got := NeighViewOf(tc.in, neighTabNames).Text()
			if got != tc.want {
				t.Errorf("Text() = %q, want %q", got, tc.want)
			}
		})
	}
}

// TestNeighViewJSONKeys pins the JSON key names against `ip -j neigh`, which
// is a separate contract from Text(): a consumer parsing `ip -j` output must
// not have to special-case goip.
func TestNeighViewJSONKeys(t *testing.T) {
	tests := []struct {
		description string
		in          xtcpnl.NeighInfo
		wantKeys    []string
		absentKeys  []string
	}{
		{
			description: "positive: a complete entry emits dst, dev, lladdr and state",
			in: xtcpnl.NeighInfo{
				Family: unix.AF_INET, Ifindex: 3, State: unix.NUD_PERMANENT,
				Dst:    []byte{192, 0, 2, 50},
				LLAddr: []byte{0x02, 0, 0, 0, 0, 0x01},
			},
			wantKeys: []string{`"dst"`, `"dev"`, `"lladdr"`, `"state"`},
		},
		{
			// lladdr carries omitempty, so an INCOMPLETE entry drops the key
			// rather than emitting `"lladdr": ""`. `ip -j neigh` does the same,
			// and a consumer testing for the key's presence is the reason it
			// matters.
			description: "negative: an incomplete entry omits the lladdr key entirely",
			in: xtcpnl.NeighInfo{
				Family: unix.AF_INET, Ifindex: 3, State: unix.NUD_INCOMPLETE,
				Dst: []byte{192, 0, 2, 52},
			},
			wantKeys:   []string{`"dst"`, `"dev"`, `"state"`},
			absentKeys: []string{`"lladdr"`},
		},
		{
			// state has no omitempty and is a slice, so it must marshal as an
			// array even when the mask is zero — never as JSON null, which
			// would break a consumer iterating it.
			description: "boundary: state is always an array, never null",
			in:          xtcpnl.NeighInfo{Family: unix.AF_INET, Ifindex: 3},
			wantKeys:    []string{`"state":["NONE"]`},
			absentKeys:  []string{`"state":null`},
		},
	}

	for _, tc := range tests {
		t.Run(tc.description, func(t *testing.T) {
			b, err := json.Marshal(NeighViewOf(tc.in, neighTabNames))
			if err != nil {
				t.Fatalf("Marshal: %v", err)
			}
			got := string(b)
			for _, k := range tc.wantKeys {
				if !strings.Contains(got, k) {
					t.Errorf("JSON %s missing %s", got, k)
				}
			}
			for _, k := range tc.absentKeys {
				if strings.Contains(got, k) {
					t.Errorf("JSON %s unexpectedly contains %s", got, k)
				}
			}
		})
	}
}
