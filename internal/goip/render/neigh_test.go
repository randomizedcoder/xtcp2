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

// ntfStickyTestCst is NTF_STICKY, `(1 << 6)` at
// include/uapi/linux/neighbour.h:52. It is spelled out here because
// golang.org/x/sys/unix exports every other NTF_* bit and not this one, and
// the rows that need it are precisely the rows about bits print_neigh does
// not name — so leaving it out would drop the only unnamed bit whose absence
// from the Go side could be mistaken for it not existing.
const ntfStickyTestCst = 1 << 6

func TestNeighViewOfText(t *testing.T) {
	tests := []struct {
		description string
		in          xtcpnl.NeighInfo
		// filter is zero — no device selector — on every row transcribed
		// from ip_neigh_n, which is the bare command's sidecar.
		filter NeighShowFilter
		want   string
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
			// This row used to expect "NONE " and called it a documented
			// divergence that was "unreachable from a kernel, which never
			// dumps state 0". Both halves were wrong. print_neigh guards
			// the state entirely on `if (r->ndm_state)` (ip/ipneigh.c:462),
			// so `ip` prints nothing — not the empty string in a slot, but
			// no slot — and pneigh entries genuinely carry ndm_state 0, so
			// `ip neigh show proxy` reaches this every time.
			//
			// The trailing space is the dev token's own, which makes this
			// the row that would catch a renderer joining the states with a
			// separator: an empty join leaves the line ending "goip0  \n".
			description: "boundary: a zero state prints no token at all, as print_neigh's guard does",
			in: xtcpnl.NeighInfo{
				Family: unix.AF_INET, Ifindex: 3, Dst: []byte{192, 0, 2, 54},
			},
			want: "192.0.2.54 dev goip0 \n",
		},
		{
			// The proxy line in full, and the shape `ip neigh show proxy`
			// actually emits: a pneigh entry has no lladdr and no state,
			// so the flag token is the last thing on the line.
			description: "positive: a proxy entry prints the proxy token and no state",
			in: xtcpnl.NeighInfo{
				Family: unix.AF_INET, Ifindex: 3, Flags: unix.NTF_PROXY,
				Dst: []byte{192, 0, 2, 60},
			},
			want: "192.0.2.60 dev goip0 proxy \n",
		},
		{
			// Order and position, asserted against the source rather than
			// inferred: the flag run (:440-451) is after lladdr and before
			// the state, and within it router precedes proxy precedes
			// extern_learn precedes offload.
			description: "corner: every named ndm_flags token prints in source order, between lladdr and state",
			in: xtcpnl.NeighInfo{
				Family: unix.AF_INET, Ifindex: 3, State: unix.NUD_STALE,
				Flags: unix.NTF_ROUTER | unix.NTF_PROXY | unix.NTF_EXT_LEARNED | unix.NTF_OFFLOADED,
				Dst:   []byte{192, 0, 2, 61},
				// A link-layer address, because the claim is about where
				// the run sits and a line without one cannot show that.
				LLAddr: []byte{0x02, 0, 0, 0, 0, 0x02},
			},
			want: "192.0.2.61 dev goip0 lladdr 02:00:00:00:00:02 router proxy extern_learn offload STALE \n",
		},
		{
			// The bits print_neigh names none of: NTF_USE 0x01, NTF_SELF
			// 0x02, NTF_MASTER 0x04 and NTF_STICKY 0x40. Unlike
			// print_ifa_flags there is no residue key, so a flags byte made
			// entirely of them must render exactly as a zero one would.
			description: "negative: unnamed ndm_flags bits print nothing and leave no residue",
			in: xtcpnl.NeighInfo{
				Family: unix.AF_INET, Ifindex: 3, State: unix.NUD_STALE,
				Flags: unix.NTF_USE | unix.NTF_SELF | unix.NTF_MASTER | ntfStickyTestCst,
				Dst:   []byte{192, 0, 2, 62},
			},
			want: "192.0.2.62 dev goip0 STALE \n",
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
		{
			// The `dev NAME` form, against the same entry as row 1. The
			// token and its keyword both go, and the single space between
			// the destination and `lladdr` is the whole remaining gap —
			// print_neigh emits `%s ` per token, so removing one token
			// removes exactly one trailing space with it.
			description: "positive: ip_neigh_dev:1 — a device filter removes the dev token and its keyword",
			in: xtcpnl.NeighInfo{
				Family: unix.AF_INET, Ifindex: 3, State: unix.NUD_PERMANENT,
				Dst:    []byte{192, 0, 2, 50},
				LLAddr: []byte{0x02, 0, 0, 0, 0, 0x01},
			},
			filter: NeighShowFilter{IndexSet: true},
			want:   "192.0.2.50 lladdr 02:00:00:00:00:01 PERMANENT \n",
		},
		{
			// The suppression is keyed on the filter alone, not on whether
			// the index matches it: print_neigh's guard is
			// `if (!filter.index && r->ndm_ifindex)` (ip/ipneigh.c:415) and
			// never compares the two. A reply on a different interface —
			// which only reaches the renderer if the earlier :331 test was
			// skipped — is printed with no dev token just the same.
			description: "corner: the filter suppresses the token without comparing indexes",
			in: xtcpnl.NeighInfo{
				Family: unix.AF_INET, Ifindex: 99, State: unix.NUD_STALE,
				Dst: []byte{192, 0, 2, 55},
			},
			filter: NeighShowFilter{IndexSet: true},
			want:   "192.0.2.55 STALE \n",
		},
		{
			// The guard's OTHER arm, and the one no command line can set.
			// ndm_ifindex 0 drops the token with no filter in play, so an
			// unfiltered render must not fall back to `if0`.
			description: "boundary: ifindex 0 drops the dev token even with no filter",
			in: xtcpnl.NeighInfo{
				Family: unix.AF_INET, Ifindex: 0, State: unix.NUD_STALE,
				Dst: []byte{192, 0, 2, 58},
			},
			want: "192.0.2.58 STALE \n",
		},
	}

	for _, tc := range tests {
		t.Run(tc.description, func(t *testing.T) {
			got := NeighViewOf(tc.in, neighTabNames, tc.filter).Text()
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
		filter      NeighShowFilter
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
			// print_neigh_state opens the JSON array from inside the
			// `if (r->ndm_state)` guard (ip/ipneigh.c:239-240, 462), so a
			// zero state emits no key. That is the reason State took
			// omitempty: a `[]string` without it marshals nil as JSON null,
			// which is neither what `ip` emits nor something a consumer
			// iterating the array survives.
			description: "boundary: a zero state omits the state key rather than emitting null or an empty array",
			in:          xtcpnl.NeighInfo{Family: unix.AF_INET, Ifindex: 3},
			wantKeys:    []string{`"dev"`},
			absentKeys:  []string{`"state"`},
		},
		{
			// print_null, not print_bool. iproute2 reaches jsonw_null_field
			// (lib/json_writer.c:336-340) and writes a bare null, where
			// print_ifa_flags's print_bool writes true — so the two
			// MarshalJSON expansions in this package emit different
			// literals on purpose.
			description: "positive: a proxy entry emits a null-valued proxy key, not true",
			in: xtcpnl.NeighInfo{
				Family: unix.AF_INET, Ifindex: 3, Flags: unix.NTF_PROXY,
				Dst: []byte{192, 0, 2, 60},
			},
			wantKeys:   []string{`"proxy":null`},
			absentKeys: []string{`"proxy":true`, `"state"`, `"router"`},
		},
		{
			// The negative for the expansion: no named bit means no
			// appended member, and the object must still be valid JSON
			// rather than one with a trailing comma.
			description: "negative: an entry with no named flag bits emits no flag keys",
			in: xtcpnl.NeighInfo{
				Family: unix.AF_INET, Ifindex: 3, State: unix.NUD_STALE,
				Flags: ntfStickyTestCst,
				Dst:   []byte{192, 0, 2, 62},
			},
			wantKeys:   []string{`"state":["STALE"]`},
			absentKeys: []string{`"proxy"`, `"router"`, `"extern_learn"`, `"offload"`, `"sticky"`},
		},
		{
			// iproute2 emits the JSON key from the same print_color_string
			// call that emits the text (ip/ipneigh.c:419-421), inside the
			// same guard, so `ip -j neigh show dev X` has no dev key at all
			// rather than an empty one. That is why Dev took omitempty.
			description: "negative: a device filter omits the dev key entirely, not as an empty string",
			in: xtcpnl.NeighInfo{
				Family: unix.AF_INET, Ifindex: 3, State: unix.NUD_PERMANENT,
				Dst: []byte{192, 0, 2, 50},
			},
			filter:     NeighShowFilter{IndexSet: true},
			wantKeys:   []string{`"dst"`, `"state"`},
			absentKeys: []string{`"dev"`},
		},
	}

	for _, tc := range tests {
		t.Run(tc.description, func(t *testing.T) {
			b, err := json.Marshal(NeighViewOf(tc.in, neighTabNames, tc.filter))
			if err != nil {
				t.Fatalf("Marshal: %v", err)
			}
			// MarshalJSON splices members into already-marshaled bytes, so
			// well-formedness is a real assertion here and not a tautology:
			// a bad splice produces a trailing comma that json.Marshal
			// itself cannot catch, and every row below is a substring test
			// that would pass anyway.
			if !json.Valid(b) {
				t.Fatalf("Marshal produced invalid JSON: %s", b)
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
