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

// The typ fields are what make the ll_addr_n2a rows below possible. A
// neighbor's ndmsg carries no type, so print_neigh formats NDA_LLADDR with
// ll_index_to_type(ndm_ifindex) (ip/ipneigh.c:428-430) — the type lives here,
// on the DEVICE, and not on the entry being printed.
//
// goip0 is spelled ARPHRD_ETHER rather than left zero so the ether rows assert
// a type rather than a default; the rendering is the same either way, which is
// exactly why leaving it implicit would be worth nothing.
var neighTabNames = fakeNames{
	1:  {name: "lo", typ: unix.ARPHRD_LOOPBACK},
	3:  {name: "goip0", typ: unix.ARPHRD_ETHER},
	10: {name: "gre1", typ: unix.ARPHRD_IPGRE},
	12: {name: "ip6tnl1", typ: unix.ARPHRD_TUNNEL6},
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
			description: "positive: NTF_EXT_MANAGED prints managed, from NDA_FLAGS_EXT rather than ndm_flags",
			in: xtcpnl.NeighInfo{
				Family: unix.AF_INET, Ifindex: 3, State: unix.NUD_REACHABLE,
				FlagsExt: xtcpnl.NtfExtManaged,
				Dst:      []byte{192, 0, 2, 70},
			},
			want: "192.0.2.70 dev goip0 managed REACHABLE \n",
		},
		{
			description: "positive: NTF_EXT_EXT_VALIDATED prints extern_valid",
			in: xtcpnl.NeighInfo{
				Family: unix.AF_INET, Ifindex: 3, State: unix.NUD_PERMANENT,
				FlagsExt: xtcpnl.NtfExtExtValidated,
				Dst:      []byte{192, 0, 2, 71},
			},
			want: "192.0.2.71 dev goip0 extern_valid PERMANENT \n",
		},
		{
			// The row the whole two-word table exists for, and the only one
			// that can fail if the ext tokens are appended instead of
			// interleaved. print_neigh's order is router, proxy, managed,
			// extern_learn, offload, extern_valid (ip/ipneigh.c:440-451) —
			// the two ext tokens sit at positions THREE and SIX, not five and
			// six. Concatenating an ndm_flags run with an ext run would give
			// "router proxy extern_learn offload managed extern_valid" and
			// pass every other row in this file.
			description: "corner: all six tokens print interleaved in source order, not grouped by which word they came from",
			in: xtcpnl.NeighInfo{
				Family: unix.AF_INET, Ifindex: 3, State: unix.NUD_STALE,
				Flags:    unix.NTF_ROUTER | unix.NTF_PROXY | unix.NTF_EXT_LEARNED | unix.NTF_OFFLOADED,
				FlagsExt: xtcpnl.NtfExtManaged | xtcpnl.NtfExtExtValidated,
				Dst:      []byte{192, 0, 2, 72},
				LLAddr:   []byte{0x02, 0, 0, 0, 0, 0x03},
			},
			want: "192.0.2.72 dev goip0 lladdr 02:00:00:00:00:03 router proxy managed extern_learn offload extern_valid STALE \n",
		},
		{
			// NTF_EXT_LOCKED is a real, defined bit that iproute2 prints —
			// but only from bridge/fdb.c:121, for a bridge FDB entry.
			// ip/ipneigh.c never tests it, so `ip neigh` renders a locked
			// entry identically to an unflagged one.
			description: "negative: NTF_EXT_LOCKED is defined but has no ip-neigh token, so it prints nothing",
			in: xtcpnl.NeighInfo{
				Family: unix.AF_INET, Ifindex: 3, State: unix.NUD_STALE,
				FlagsExt: xtcpnl.NtfExtLocked,
				Dst:      []byte{192, 0, 2, 73},
			},
			want: "192.0.2.73 dev goip0 STALE \n",
		},
		{
			// The A to the next row's B. NTF_USE is bit 0 of ndm_flags and
			// NTF_EXT_MANAGED is bit 0 of ext_flags; the same numeric 1 in
			// the other word must produce the opposite output. A renderer
			// that tested one mask against both words would print "managed"
			// here.
			description: "corner: bit 0 in ndm_flags is NTF_USE and prints nothing",
			in: xtcpnl.NeighInfo{
				Family: unix.AF_INET, Ifindex: 3, State: unix.NUD_STALE,
				Flags: unix.NTF_USE,
				Dst:   []byte{192, 0, 2, 74},
			},
			want: "192.0.2.74 dev goip0 STALE \n",
		},
		{
			description: "corner: the same bit 0 in NDA_FLAGS_EXT is NTF_EXT_MANAGED and does print",
			in: xtcpnl.NeighInfo{
				Family: unix.AF_INET, Ifindex: 3, State: unix.NUD_STALE,
				FlagsExt: 1,
				Dst:      []byte{192, 0, 2, 74},
			},
			want: "192.0.2.74 dev goip0 managed STALE \n",
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
		{
			// ll_addr_n2a's v4 arm, reached through the DEVICE's type. The
			// bytes c0 00 02 63 are 192.0.2.99; a renderer that formatted
			// them without consulting gre1's ARPHRD_IPGRE prints
			// "c0:00:02:63", which is well-formed, plausible, and wrong.
			description: "positive: a 4-byte lladdr on an ARPHRD_IPGRE device is a dotted quad",
			in: xtcpnl.NeighInfo{
				Family: unix.AF_INET, Ifindex: 10, State: unix.NUD_PERMANENT,
				Dst:    []byte{203, 0, 113, 5},
				LLAddr: []byte{192, 0, 2, 99},
			},
			want: "203.0.113.5 dev gre1 lladdr 192.0.2.99 PERMANENT \n",
		},
		{
			// The v6 arm on a TUNNEL6 device, and the reason netip is used
			// rather than net.IP: both render an ordinary 16-byte value the
			// same, so this row is the ordinary case and the v4-mapped corner
			// is pinned in xtcpnl_arphrd_test.go.
			description: "positive: a 16-byte lladdr on an ARPHRD_TUNNEL6 device is an IPv6 address",
			in: xtcpnl.NeighInfo{
				Family: unix.AF_INET6, Ifindex: 12, State: unix.NUD_PERMANENT,
				Dst: []byte{
					0x20, 0x01, 0x0d, 0xb8, 1, 0, 0, 0,
					0, 0, 0, 0, 0, 0, 0, 0x05,
				},
				LLAddr: []byte{
					0x20, 0x01, 0x0d, 0xb8, 0, 0, 0, 0,
					0, 0, 0, 0, 0, 0, 0, 0x63,
				},
			},
			want: "2001:db8:100::5 dev ip6tnl1 lladdr 2001:db8::63 PERMANENT \n",
		},
		{
			// The negative that makes the two above mean something: the same
			// four bytes on an ARPHRD_ETHER device stay colon-hex. ll_addr_n2a
			// tests a length AND a type, so neither alone licenses the
			// conversion.
			description: "negative: the same 4 bytes on an ARPHRD_ETHER device stay colon-hex",
			in: xtcpnl.NeighInfo{
				Family: unix.AF_INET, Ifindex: 3, State: unix.NUD_PERMANENT,
				Dst:    []byte{192, 0, 2, 60},
				LLAddr: []byte{192, 0, 2, 99},
			},
			want: "192.0.2.60 dev goip0 lladdr c0:00:02:63 PERMANENT \n",
		},
		{
			// The other half of "length AND type": six bytes on a tunnel type
			// has no special case either, so it falls through to the hex loop.
			description: "negative: a 6-byte lladdr on an ARPHRD_IPGRE device stays colon-hex",
			in: xtcpnl.NeighInfo{
				Family: unix.AF_INET, Ifindex: 10, State: unix.NUD_STALE,
				Dst:    []byte{203, 0, 113, 6},
				LLAddr: []byte{0x02, 0, 0, 0, 0, 0x07},
			},
			want: "203.0.113.6 dev gre1 lladdr 02:00:00:00:00:07 STALE \n",
		},
		{
			// An index the cache does not hold. ll_index_to_type answers 0,
			// which is ARPHRD_NETROM and has no special case, so the four
			// bytes stay hex — and the dev token falls back to "if%u". Both
			// halves of the miss on one line.
			description: "boundary: an uncached index types as 0, so a 4-byte lladdr stays colon-hex",
			in: xtcpnl.NeighInfo{
				Family: unix.AF_INET, Ifindex: 77, State: unix.NUD_STALE,
				Dst:    []byte{192, 0, 2, 61},
				LLAddr: []byte{192, 0, 2, 99},
			},
			want: "192.0.2.61 dev if77 lladdr c0:00:02:63 STALE \n",
		},
		{
			// The interaction nobody would think to check: `dev NAME`
			// suppresses the printed token, and the TYPE lookup must still
			// happen on the real index. A renderer that reused the suppressed
			// value would format every entry as ARPHRD_NETROM and regress
			// this line to colon-hex while looking correct everywhere else.
			description: "corner: `dev NAME` suppresses the token but not the type lookup",
			in: xtcpnl.NeighInfo{
				Family: unix.AF_INET, Ifindex: 10, State: unix.NUD_PERMANENT,
				Dst:    []byte{203, 0, 113, 5},
				LLAddr: []byte{192, 0, 2, 99},
			},
			filter: NeighShowFilter{IndexSet: true},
			want:   "203.0.113.5 lladdr 192.0.2.99 PERMANENT \n",
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
			description: "positive: NTF_EXT_MANAGED emits a null-valued managed key, like the ndm_flags tokens",
			in: xtcpnl.NeighInfo{
				Family: unix.AF_INET, Ifindex: 3, State: unix.NUD_REACHABLE,
				FlagsExt: xtcpnl.NtfExtManaged,
				Dst:      []byte{192, 0, 2, 70},
			},
			wantKeys:   []string{`"managed":null`},
			absentKeys: []string{`"managed":true`, `"extern_valid"`},
		},
		{
			// One substring rather than six, because the claim is the ORDER.
			// Six independent presence checks pass for any permutation, and
			// permuting is exactly what appending the ext tokens instead of
			// interleaving them would do.
			description: "corner: all six flag keys appear as one ordered run, managed third and extern_valid last",
			in: xtcpnl.NeighInfo{
				Family: unix.AF_INET, Ifindex: 3, State: unix.NUD_STALE,
				Flags:    unix.NTF_ROUTER | unix.NTF_PROXY | unix.NTF_EXT_LEARNED | unix.NTF_OFFLOADED,
				FlagsExt: xtcpnl.NtfExtManaged | xtcpnl.NtfExtExtValidated,
				Dst:      []byte{192, 0, 2, 72},
			},
			wantKeys: []string{
				`"router":null,"proxy":null,"managed":null,"extern_learn":null,"offload":null,"extern_valid":null`,
			},
		},
		{
			// The state key's POSITION, which every other row here is blind
			// to because they test for `"state"` on its own and a substring
			// match does not care what precedes it. print_neigh prints the
			// flag run at :440-451 and the state at :462-463, and `ip -j`
			// writes keys in print order, so the flags come first.
			//
			// Confirmed against the pinned iproute2 7.1.0 on a live
			// namespace rather than inferred from the print order:
			//
			//	$ ip -j neigh show
			//	[{"dst":"192.0.2.53","dev":"goip0",
			//	  "lladdr":"02:00:00:00:00:04","router":null,
			//	  "state":["PERMANENT"]}]
			//
			// The absent key is the whole point of the row: it is the exact
			// output a `json:"state,omitempty"` tag produces, because a
			// tagged field marshals before members spliced onto the end.
			description: "corner: the state key follows the flag run, because ip -j writes keys in print order",
			in: xtcpnl.NeighInfo{
				Family: unix.AF_INET, Ifindex: 3, State: unix.NUD_PERMANENT,
				Flags:  unix.NTF_ROUTER,
				Dst:    []byte{192, 0, 2, 53},
				LLAddr: []byte{0x02, 0, 0, 0, 0, 0x04},
			},
			wantKeys:   []string{`"router":null,"state":["PERMANENT"]`},
			absentKeys: []string{`"state":["PERMANENT"],"router":null`},
		},
		{
			// The same claim at full width: six flags and a state in one
			// substring. If the state were tagged rather than spliced it
			// would sit before `"router"`, and if the ext tokens were
			// appended rather than interleaved `managed` would move — so
			// this single string is the only row that fails for either
			// mistake.
			description: "corner: six flag keys then state, one unbroken run across both words",
			in: xtcpnl.NeighInfo{
				Family: unix.AF_INET, Ifindex: 3, State: unix.NUD_STALE,
				Flags:    unix.NTF_ROUTER | unix.NTF_PROXY | unix.NTF_EXT_LEARNED | unix.NTF_OFFLOADED,
				FlagsExt: xtcpnl.NtfExtManaged | xtcpnl.NtfExtExtValidated,
				Dst:      []byte{192, 0, 2, 75},
			},
			wantKeys: []string{
				`"router":null,"proxy":null,"managed":null,"extern_learn":null,"offload":null,"extern_valid":null,"state":["STALE"]`,
			},
		},
		{
			// State with no flags now takes the splice path too, where
			// before it was emitted by the struct tag. Same bytes, different
			// code, so it needs its own row: the key must still be there,
			// still be an array, and still sit directly after lladdr with no
			// stray comma between them.
			description: "boundary: a flagless entry still emits state, which now comes from the splice rather than a tag",
			in: xtcpnl.NeighInfo{
				Family: unix.AF_INET, Ifindex: 3, State: unix.NUD_REACHABLE,
				Dst:    []byte{192, 0, 2, 76},
				LLAddr: []byte{0x02, 0, 0, 0, 0, 0x0a},
			},
			wantKeys:   []string{`"lladdr":"02:00:00:00:00:0a","state":["REACHABLE"]`},
			absentKeys: []string{`,,`},
		},
		{
			// Neither splice fires. The object has to come back exactly as
			// json.Marshal produced it, with no trailing comma and no empty
			// `"state":[]` — this is the early-return path, and it is the
			// one an added `b.WriteByte(',')` outside a guard would break.
			description: "negative: an entry with no flags and no state emits neither, and closes cleanly",
			in: xtcpnl.NeighInfo{
				Family: unix.AF_INET, Ifindex: 3,
				Dst: []byte{192, 0, 2, 77},
			},
			wantKeys:   []string{`"dst":"192.0.2.77","dev":"goip0"}`},
			absentKeys: []string{`"state"`, `,}`},
		},
		{
			description: "negative: NTF_EXT_LOCKED emits no key, having no ip-neigh token to emit one from",
			in: xtcpnl.NeighInfo{
				Family: unix.AF_INET, Ifindex: 3, State: unix.NUD_STALE,
				FlagsExt: xtcpnl.NtfExtLocked,
				Dst:      []byte{192, 0, 2, 73},
			},
			wantKeys:   []string{`"state":["STALE"]`},
			absentKeys: []string{`"locked"`, `"managed"`, `"extern_valid"`},
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

// TestNeighFlagTokens sweeps NeighFlagTokens directly, which nothing else in
// this package does.
//
// Every other test here reaches it through NeighViewOf, and that is enough to
// check the tokens a REALISTIC entry produces — but not enough to sweep the
// input space. The function takes two words whose bit sets overlap
// numerically, and the interesting inputs are the ones a captured neighbor
// never has: both words saturated, a bit set in the wrong word, a word whose
// only bits are unnamed. Those belong here rather than in a view test, where
// each would need a whole NeighInfo built around it.
//
// go test ./internal/goip/render/ -run TestNeighFlagTokens
func TestNeighFlagTokens(t *testing.T) {
	// allNdm is every ndm_flags bit, named or not, and allExt the same for
	// NDA_FLAGS_EXT. Saturation is the cheapest way to assert that the table
	// selects rather than accumulates.
	const (
		allNdm uint8  = 0xFF
		allExt uint32 = 0xFFFFFFFF
	)

	tests := []struct {
		description string
		flags       uint8
		flagsExt    uint32
		want        []string
	}{
		{
			description: "positive: NTF_ROUTER alone yields router",
			flags:       unix.NTF_ROUTER,
			want:        []string{"router"},
		},
		{
			description: "positive: NTF_PROXY alone yields proxy",
			flags:       unix.NTF_PROXY,
			want:        []string{"proxy"},
		},
		{
			description: "positive: NTF_EXT_LEARNED alone yields extern_learn",
			flags:       unix.NTF_EXT_LEARNED,
			want:        []string{"extern_learn"},
		},
		{
			description: "positive: NTF_OFFLOADED alone yields offload",
			flags:       unix.NTF_OFFLOADED,
			want:        []string{"offload"},
		},
		{
			description: "positive: NTF_EXT_MANAGED alone yields managed, from the other word",
			flagsExt:    xtcpnl.NtfExtManaged,
			want:        []string{"managed"},
		},
		{
			description: "positive: NTF_EXT_EXT_VALIDATED alone yields extern_valid",
			flagsExt:    xtcpnl.NtfExtExtValidated,
			want:        []string{"extern_valid"},
		},
		{
			// print_neigh's order across both words, which is the property
			// the single interleaved table exists to hold.
			description: "boundary: every named bit in both words yields all six in print order",
			flags:       unix.NTF_ROUTER | unix.NTF_PROXY | unix.NTF_EXT_LEARNED | unix.NTF_OFFLOADED,
			flagsExt:    xtcpnl.NtfExtManaged | xtcpnl.NtfExtExtValidated,
			want:        []string{"router", "proxy", "managed", "extern_learn", "offload", "extern_valid"},
		},
		{
			// Saturating both words must give the SAME six and no more: the
			// table selects named bits rather than walking the word. A
			// renderer that emitted a residue token — as print_ifa_flags
			// does — would differ here and nowhere else in this file.
			description: "boundary: saturating both words still yields exactly the six named tokens",
			flags:       allNdm,
			flagsExt:    allExt,
			want:        []string{"router", "proxy", "managed", "extern_learn", "offload", "extern_valid"},
		},
		{
			description: "negative: both words zero yields nil, not an empty slice",
			want:        nil,
		},
		{
			description: "negative: only unnamed ndm_flags bits yields nil",
			flags:       unix.NTF_USE | unix.NTF_SELF | unix.NTF_MASTER | ntfStickyTestCst,
			want:        nil,
		},
		{
			description: "negative: NTF_EXT_LOCKED is the only named ext bit ip neigh ignores, so it yields nil",
			flagsExt:    xtcpnl.NtfExtLocked,
			want:        nil,
		},
		{
			// The collision, from both sides. The same numeric 1 means
			// NTF_USE in one word and NTF_EXT_MANAGED in the other, so a
			// function testing one mask against both would answer the same
			// for these two rows instead of opposite.
			description: "corner: bit 0 of ndm_flags is NTF_USE and names nothing",
			flags:       1,
			want:        nil,
		},
		{
			description: "corner: bit 0 of NDA_FLAGS_EXT is NTF_EXT_MANAGED and names managed",
			flagsExt:    1,
			want:        []string{"managed"},
		},
		{
			// The mirror of the pair above, on the bit where the ndm side is
			// the one that names something: 1<<7 is NTF_ROUTER in ndm_flags
			// and is unnamed in ext_flags.
			description: "corner: bit 7 names router in ndm_flags and nothing in NDA_FLAGS_EXT",
			flagsExt:    1 << 7,
			want:        nil,
		},
		{
			description: "corner: that same bit 7 set in ndm_flags does name router",
			flags:       1 << 7,
			want:        []string{"router"},
		},
		{
			// An ext bit far above anything defined must not wrap, alias or
			// panic — the word is compared with &, so the only risk would be
			// a table entry stored in too narrow a type.
			description: "corner: the top ext bit is unnamed and yields nil",
			flagsExt:    1 << 31,
			want:        nil,
		},
	}

	for _, tt := range tests {
		t.Run(tt.description, func(t *testing.T) {
			got := NeighFlagTokens(tt.flags, tt.flagsExt)

			// nil and []string{} both render as no tokens, but only nil
			// keeps MarshalJSON off its append path, so the distinction is
			// asserted rather than smoothed over by a length comparison.
			if tt.want == nil && got != nil {
				t.Fatalf("NeighFlagTokens(%#x, %#x) = %v, want nil", tt.flags, tt.flagsExt, got)
			}
			if len(got) != len(tt.want) {
				t.Fatalf("NeighFlagTokens(%#x, %#x) = %v, want %v", tt.flags, tt.flagsExt, got, tt.want)
			}
			for i := range tt.want {
				if got[i] != tt.want[i] {
					t.Errorf("token %d = %q, want %q (full: %v, want %v)",
						i, got[i], tt.want[i], got, tt.want)
				}
			}
		})
	}
}

// statsFilter is `ip -s neigh show`; statsDevFilter adds a `dev` selector.
var statsFilter = NeighShowFilter{Stats: true}
var statsDevFilter = NeighShowFilter{IndexSet: true, Stats: true}

// nci builds the NdaCacheInfo a decoded NDA_CACHEINFO would have produced.
//
// The parameter order is the KERNEL STRUCT's — confirmed, used, updated,
// refcnt — deliberately, so that reading a call against `struct
// nda_cacheinfo` works. print_cacheinfo emits them in a different order
// (ref, used, confirmed, updated), which is the transposition this helper
// makes visible rather than hides.
func nci(confirmed, used, updated, refcnt uint32) xtcpnl.NdaCacheInfo {
	return xtcpnl.NdaCacheInfo{
		Confirmed: confirmed, Used: used, Updated: updated, Refcnt: refcnt,
	}
}

// TestNeighViewOfStatsText drives the `-s` block: print_cacheinfo
// (ip/ipneigh.c:220-234) and the `probes` token (:457-459), both inside the
// one `if (show_stats)` at :453-460.
//
// # The spacing is the hard part and it is upstream's
//
// print_cacheinfo's formats carry a LEADING space and no trailing one, while
// `probes %u ` is the reverse. The visible result on a real entry is a
// DOUBLED space after the lladdr and `probes` running straight on from the
// last counter with no space at all. Measured, not guessed — `ip -s neigh
// show` in a throwaway namespace printed
//
//	192.0.2.2 dev dummy0 lladdr 02:00:00:00:00:02  used 0/0/0probes 0 PERMANENT
//
// and `cat -A` confirmed the two spaces and the missing one. A renderer that
// "tidies" either diverges.
//
// The sharper consequence is in the rows where NDA_PROBES is absent: nothing
// then supplies a trailing space at all, so the state runs straight on as
// `used 3/3/3REACHABLE`. That reads like a bug in this renderer and is not —
// it is what print_cacheinfo's trailing `"/%u"` produces when the
// `probes %u ` that normally follows is missing. Several `want` strings
// below were first written with a space there, failed, and were corrected to
// upstream's output rather than the other way around.
//
// Those rows are constructed and unreachable from a real kernel:
// neigh_fill_info emits NDA_PROBES and NDA_CACHEINFO together in one `||`
// chain (kernel net/core, :2690-2692 — grep the function name, the file is
// spelled the way iproute2 spells the command alias), so an entry carries
// both or neither. Worth noting while reading that function — it puts
// NDA_PROBES on the wire BEFORE NDA_CACHEINFO, while `ip` prints them the
// other way round. Attribute order is not print order.
//
// # Position
//
// The block sits between the flag run and the state, not at the end of the
// line. A block appended after the state would satisfy a keyword-set check
// and fail the ordering rows below.
//
// go test ./internal/goip/render/ -run TestNeighViewOfStatsText
func TestNeighViewOfStatsText(t *testing.T) {
	tests := []struct {
		description string
		in          xtcpnl.NeighInfo
		filter      NeighShowFilter
		want        string
	}{
		{
			description: "positive: a reachable entry with refcnt renders ` ref 1 used 3/3/3` — leading space, no trailing space, then probes",
			in: xtcpnl.NeighInfo{
				Family: unix.AF_INET, Ifindex: 3, State: unix.NUD_REACHABLE,
				Dst: []byte{192, 0, 2, 50}, LLAddr: []byte{0x02, 0, 0, 0, 0, 0x01},
				HasCacheInfo: true, CacheInfo: nci(300, 300, 300, 1),
				Probes: 0, HasProbes: true,
			},
			filter: statsFilter,
			want:   "192.0.2.50 dev goip0 lladdr 02:00:00:00:00:01  ref 1 used 3/3/3probes 0 REACHABLE \n",
		},
		{
			description: "corner: the DOUBLED space after lladdr and the MISSING space before probes, on one line — both are upstream format-string artifacts",
			in: xtcpnl.NeighInfo{
				Family: unix.AF_INET, Ifindex: 3, State: unix.NUD_REACHABLE,
				Dst: []byte{192, 0, 2, 50}, LLAddr: []byte{0x02, 0, 0, 0, 0, 0x01},
				HasCacheInfo: true, CacheInfo: nci(0, 0, 0, 1),
				Probes: 3, HasProbes: true,
			},
			filter: statsFilter,
			want:   "192.0.2.50 dev goip0 lladdr 02:00:00:00:00:01  ref 1 used 0/0/0probes 3 REACHABLE \n",
		},
		{
			description: "corner: the counters render in PRINT order (ref, used, confirmed, updated), which is not the struct's order (confirmed, used, updated, refcnt)",
			in: xtcpnl.NeighInfo{
				Family: unix.AF_INET, Ifindex: 3, State: unix.NUD_REACHABLE,
				Dst: []byte{192, 0, 2, 50}, LLAddr: []byte{0x02, 0, 0, 0, 0, 0x01},
				// confirmed=200, used=100, updated=300, refcnt=4 — four
				// distinct values, so any transposition is visible.
				HasCacheInfo: true, CacheInfo: nci(200, 100, 300, 4),
			},
			filter: statsFilter,
			want:   "192.0.2.50 dev goip0 lladdr 02:00:00:00:00:01  ref 4 used 1/2/3REACHABLE \n",
		},
		{
			description: "negative: the same entry WITHOUT -s renders no block at all — the gate is show_stats, not attribute presence",
			in: xtcpnl.NeighInfo{
				Family: unix.AF_INET, Ifindex: 3, State: unix.NUD_REACHABLE,
				Dst: []byte{192, 0, 2, 50}, LLAddr: []byte{0x02, 0, 0, 0, 0, 0x01},
				HasCacheInfo: true, CacheInfo: nci(300, 300, 300, 1),
				Probes: 3, HasProbes: true,
			},
			filter: NeighShowFilter{},
			want:   "192.0.2.50 dev goip0 lladdr 02:00:00:00:00:01 REACHABLE \n",
		},
		{
			description: "negative: NDA_CACHEINFO absent under -s prints no counters — NOT ` ref 0 used 0/0/0`",
			in: xtcpnl.NeighInfo{
				Family: unix.AF_INET, Ifindex: 3, State: unix.NUD_REACHABLE,
				Dst: []byte{192, 0, 2, 50}, LLAddr: []byte{0x02, 0, 0, 0, 0, 0x01},
			},
			filter: statsFilter,
			want:   "192.0.2.50 dev goip0 lladdr 02:00:00:00:00:01 REACHABLE \n",
		},
		{
			description: "negative: NDA_PROBES absent prints no probes token, while the cacheinfo block still renders — they are siblings under one guard, not one unit",
			in: xtcpnl.NeighInfo{
				Family: unix.AF_INET, Ifindex: 3, State: unix.NUD_REACHABLE,
				Dst: []byte{192, 0, 2, 50}, LLAddr: []byte{0x02, 0, 0, 0, 0, 0x01},
				HasCacheInfo: true, CacheInfo: nci(300, 300, 300, 1),
			},
			filter: statsFilter,
			want:   "192.0.2.50 dev goip0 lladdr 02:00:00:00:00:01  ref 1 used 3/3/3REACHABLE \n",
		},
		{
			description: "corner: NDA_PROBES present with NO cacheinfo prints probes alone — the other half of the sibling relationship",
			in: xtcpnl.NeighInfo{
				Family: unix.AF_INET, Ifindex: 3, State: unix.NUD_REACHABLE,
				Dst: []byte{192, 0, 2, 50}, LLAddr: []byte{0x02, 0, 0, 0, 0, 0x01},
				Probes: 7, HasProbes: true,
			},
			filter: statsFilter,
			want:   "192.0.2.50 dev goip0 lladdr 02:00:00:00:00:01 probes 7 REACHABLE \n",
		},
		{
			description: "boundary: ndm_refcnt == 0 suppresses ` ref ` alone — the only one of the four counters with a guard",
			in: xtcpnl.NeighInfo{
				Family: unix.AF_INET, Ifindex: 3, State: unix.NUD_STALE,
				Dst: []byte{192, 0, 2, 51}, LLAddr: []byte{0x02, 0, 0, 0, 0, 0x02},
				HasCacheInfo: true, CacheInfo: nci(300, 300, 300, 0),
			},
			filter: statsFilter,
			want:   "192.0.2.51 dev goip0 lladdr 02:00:00:00:00:02  used 3/3/3STALE \n",
		},
		{
			description: "boundary: an all-zero cacheinfo still prints ` used 0/0/0` — only refcnt is guarded, so present-and-zero is a visible output here",
			in: xtcpnl.NeighInfo{
				Family: unix.AF_INET, Ifindex: 3, State: unix.NUD_STALE,
				Dst: []byte{192, 0, 2, 51}, LLAddr: []byte{0x02, 0, 0, 0, 0, 0x02},
				HasCacheInfo: true, CacheInfo: nci(0, 0, 0, 0),
			},
			filter: statsFilter,
			want:   "192.0.2.51 dev goip0 lladdr 02:00:00:00:00:02  used 0/0/0STALE \n",
		},
		{
			description: "boundary: ndm_used 149 renders `used 1` — USER_HZ division truncates rather than rounding",
			in: xtcpnl.NeighInfo{
				Family: unix.AF_INET, Ifindex: 3, State: unix.NUD_STALE,
				Dst: []byte{192, 0, 2, 51}, LLAddr: []byte{0x02, 0, 0, 0, 0, 0x02},
				HasCacheInfo: true, CacheInfo: nci(0, 149, 0, 0),
			},
			filter: statsFilter,
			want:   "192.0.2.51 dev goip0 lladdr 02:00:00:00:00:02  used 1/0/0STALE \n",
		},
		{
			description: "boundary: NDA_PROBES present and zero prints `probes 0` — present-and-zero differs from absent, which is why HasProbes exists",
			in: xtcpnl.NeighInfo{
				Family: unix.AF_INET, Ifindex: 3, State: unix.NUD_STALE,
				Dst: []byte{192, 0, 2, 51}, LLAddr: []byte{0x02, 0, 0, 0, 0, 0x02},
				Probes: 0, HasProbes: true,
			},
			filter: statsFilter,
			want:   "192.0.2.51 dev goip0 lladdr 02:00:00:00:00:02 probes 0 STALE \n",
		},
		{
			description: "corner: an INCOMPLETE entry with no lladdr still renders the block, between the (empty) flag run and the state",
			in: xtcpnl.NeighInfo{
				Family: unix.AF_INET, Ifindex: 3, State: unix.NUD_INCOMPLETE,
				Dst:          []byte{192, 0, 2, 52},
				HasCacheInfo: true, CacheInfo: nci(6000, 0, 0, 0),
				Probes: 1, HasProbes: true,
			},
			filter: statsFilter,
			want:   "192.0.2.52 dev goip0  used 0/60/0probes 1 INCOMPLETE \n",
		},
		{
			description: "corner: POSITION — with a router flag set, the order is flags then block then state, so the block lands BEFORE `router` would be wrong and before STALE is right",
			in: xtcpnl.NeighInfo{
				Family: unix.AF_INET6, Ifindex: 3, State: unix.NUD_STALE,
				Flags: unix.NTF_ROUTER,
				Dst: []byte{
					0xfd, 0x99, 0, 0, 0, 0, 0, 0,
					0, 0, 0, 0, 0, 0, 0, 0x02,
				},
				LLAddr:       []byte{0x02, 0, 0, 0, 0, 0x06},
				HasCacheInfo: true, CacheInfo: nci(100, 100, 100, 0),
				Probes: 1, HasProbes: true,
			},
			filter: statsFilter,
			want:   "fd99::2 dev goip0 lladdr 02:00:00:00:00:06 router  used 1/1/1probes 1 STALE \n",
		},
		{
			description: "corner: under a `dev` selector the block is unaffected — -s and the device filter compose without interacting",
			in: xtcpnl.NeighInfo{
				Family: unix.AF_INET, Ifindex: 3, State: unix.NUD_REACHABLE,
				Dst: []byte{192, 0, 2, 50}, LLAddr: []byte{0x02, 0, 0, 0, 0, 0x01},
				HasCacheInfo: true, CacheInfo: nci(0, 0, 0, 1),
				Probes: 0, HasProbes: true,
			},
			filter: statsDevFilter,
			want:   "192.0.2.50 lladdr 02:00:00:00:00:01  ref 1 used 0/0/0probes 0 REACHABLE \n",
		},
		{
			description: "corner: a zero ndm_state prints no state, so the block is the LAST thing on the line and its missing trailing space shows",
			in: xtcpnl.NeighInfo{
				Family: unix.AF_INET, Ifindex: 3, State: 0,
				Dst:          []byte{192, 0, 2, 53},
				HasCacheInfo: true, CacheInfo: nci(0, 0, 0, 2),
			},
			filter: statsFilter,
			want:   "192.0.2.53 dev goip0  ref 2 used 0/0/0\n",
		},
	}

	for _, tt := range tests {
		t.Run(tt.description, func(t *testing.T) {
			got := NeighViewOf(tt.in, neighTabNames, tt.filter).Text()
			if got != tt.want {
				t.Errorf("got  %q\nwant %q", got, tt.want)
			}
		})
	}
}

// TestNeighViewStatsJSON is the JSON half of the `-s` block.
//
// Three things here are not simply the text form restated:
//
//  1. KEY ORDER. The five keys must land after the flags and before the
//     state, which is why they are `json:"-"` and hand-written in
//     MarshalJSON. A struct tag would have put them ahead of every flag.
//  2. One text token is THREE keys. `used 3/3/3` comes from three separate
//     print_uint calls (ip/ipneigh.c:231-234) whose text formats concatenate;
//     in JSON they are "used", "confirmed" and "updated" with no such
//     merging.
//  3. The counters are the DIVIDED values, as in text. print_uint is handed
//     `ci->ndm_used / hz`, so JSON carries seconds, not ticks.
//
// Expectations verified against real `ip -s -j neigh show` in a throwaway
// namespace, whose entries came back as
// {"dst":…,"lladdr":…,"used":0,"confirmed":60,"updated":0,"probes":0,
//
//	"state":["STALE"]}.
//
// go test ./internal/goip/render/ -run TestNeighViewStatsJSON
func TestNeighViewStatsJSON(t *testing.T) {
	tests := []struct {
		description string
		in          xtcpnl.NeighInfo
		filter      NeighShowFilter
		wantExact   string
		absentKeys  []string
	}{
		{
			description: "positive: the full key order — dst, dev, lladdr, then the five -s keys, then state",
			in: xtcpnl.NeighInfo{
				Family: unix.AF_INET, Ifindex: 3, State: unix.NUD_STALE,
				Dst: []byte{192, 0, 2, 51}, LLAddr: []byte{0x02, 0, 0, 0, 0, 0x02},
				HasCacheInfo: true, CacheInfo: nci(6000, 0, 0, 0),
				Probes: 0, HasProbes: true,
			},
			filter: statsFilter,
			wantExact: `{"dst":"192.0.2.51","dev":"goip0","lladdr":"02:00:00:00:00:02",` +
				`"used":0,"confirmed":60,"updated":0,"probes":0,"state":["STALE"]}`,
		},
		{
			description: "positive: refcnt leads the five when non-zero",
			in: xtcpnl.NeighInfo{
				Family: unix.AF_INET, Ifindex: 3, State: unix.NUD_REACHABLE,
				Dst: []byte{192, 0, 2, 50}, LLAddr: []byte{0x02, 0, 0, 0, 0, 0x01},
				HasCacheInfo: true, CacheInfo: nci(0, 0, 0, 1),
				Probes: 0, HasProbes: true,
			},
			filter: statsFilter,
			wantExact: `{"dst":"192.0.2.50","dev":"goip0","lladdr":"02:00:00:00:00:01",` +
				`"refcnt":1,"used":0,"confirmed":0,"updated":0,"probes":0,"state":["REACHABLE"]}`,
		},
		{
			description: "corner: POSITION — a flag pushes the -s keys after it and keeps state last, which a struct tag could not express",
			in: xtcpnl.NeighInfo{
				Family: unix.AF_INET6, Ifindex: 3, State: unix.NUD_STALE,
				Flags: unix.NTF_ROUTER,
				Dst: []byte{
					0xfd, 0x99, 0, 0, 0, 0, 0, 0,
					0, 0, 0, 0, 0, 0, 0, 0x02,
				},
				LLAddr:       []byte{0x02, 0, 0, 0, 0, 0x06},
				HasCacheInfo: true, CacheInfo: nci(100, 100, 100, 0),
				Probes: 1, HasProbes: true,
			},
			filter: statsFilter,
			wantExact: `{"dst":"fd99::2","dev":"goip0","lladdr":"02:00:00:00:00:06",` +
				`"router":null,"used":1,"confirmed":1,"updated":1,"probes":1,"state":["STALE"]}`,
		},
		{
			description: "negative: without -s none of the five keys appears, and the object is exactly what it was before this change",
			in: xtcpnl.NeighInfo{
				Family: unix.AF_INET, Ifindex: 3, State: unix.NUD_STALE,
				Dst: []byte{192, 0, 2, 51}, LLAddr: []byte{0x02, 0, 0, 0, 0, 0x02},
				HasCacheInfo: true, CacheInfo: nci(6000, 0, 0, 2),
				Probes: 3, HasProbes: true,
			},
			filter:     NeighShowFilter{},
			wantExact:  `{"dst":"192.0.2.51","dev":"goip0","lladdr":"02:00:00:00:00:02","state":["STALE"]}`,
			absentKeys: []string{"refcnt", "used", "confirmed", "updated", "probes"},
		},
		{
			description: "negative: NDA_CACHEINFO absent under -s emits no counter keys — not zeros",
			in: xtcpnl.NeighInfo{
				Family: unix.AF_INET, Ifindex: 3, State: unix.NUD_STALE,
				Dst: []byte{192, 0, 2, 51}, LLAddr: []byte{0x02, 0, 0, 0, 0, 0x02},
				Probes: 3, HasProbes: true,
			},
			filter: statsFilter,
			wantExact: `{"dst":"192.0.2.51","dev":"goip0","lladdr":"02:00:00:00:00:02",` +
				`"probes":3,"state":["STALE"]}`,
			absentKeys: []string{"refcnt", "used", "confirmed", "updated"},
		},
		{
			description: "boundary: refcnt 0 drops its key while the other three stay — the same independent suppression as the text form",
			in: xtcpnl.NeighInfo{
				Family: unix.AF_INET, Ifindex: 3, State: unix.NUD_STALE,
				Dst: []byte{192, 0, 2, 51}, LLAddr: []byte{0x02, 0, 0, 0, 0, 0x02},
				HasCacheInfo: true, CacheInfo: nci(0, 0, 0, 0),
			},
			filter: statsFilter,
			wantExact: `{"dst":"192.0.2.51","dev":"goip0","lladdr":"02:00:00:00:00:02",` +
				`"used":0,"confirmed":0,"updated":0,"state":["STALE"]}`,
			absentKeys: []string{"refcnt", "probes"},
		},
		{
			description: "corner: a zero ndm_state emits the -s keys and NO state key, so the object ends on probes",
			in: xtcpnl.NeighInfo{
				Family: unix.AF_INET, Ifindex: 3, State: 0,
				Dst:          []byte{192, 0, 2, 53},
				HasCacheInfo: true, CacheInfo: nci(0, 0, 0, 2),
				Probes: 1, HasProbes: true,
			},
			filter: statsFilter,
			wantExact: `{"dst":"192.0.2.53","dev":"goip0",` +
				`"refcnt":2,"used":0,"confirmed":0,"updated":0,"probes":1}`,
			absentKeys: []string{"state"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.description, func(t *testing.T) {
			b, err := json.Marshal(NeighViewOf(tt.in, neighTabNames, tt.filter))
			if err != nil {
				t.Fatalf("Marshal: %v", err)
			}
			if got := string(b); got != tt.wantExact {
				t.Errorf("got  %s\nwant %s", got, tt.wantExact)
			}
			for _, k := range tt.absentKeys {
				if strings.Contains(string(b), `"`+k+`":`) {
					t.Errorf("key %q present, want absent: %s", k, b)
				}
			}
		})
	}
}
