package goip

import (
	"bytes"
	"encoding/json"
	"io"
	"os"
	"reflect"
	"sort"
	"strings"
	"testing"

	"golang.org/x/sys/unix"
)

const neighDumpPcap = "../../pkg/xtcpnl/testdata/7_1_4/dumps/netlink_route_getneigh.pcap"

// neighDevPcap is `ip neigh show dev goip0`. Its replies are the same set as
// neighDumpPcap's — the interesting difference is 8 bytes on the second
// request, which only internal/goip/req can see.
const neighDevPcap = "../../pkg/xtcpnl/testdata/7_1_4/dumps/netlink_route_getneigh_dev.pcap"

// neighProxyPcap is `ip neigh show proxy`. Its replies are NOT a subset of
// neighDumpPcap's and are not a superset either: the kernel picks
// pneigh_dump_table over neigh_dump_table on ndm_flags == NTF_PROXY
// (net/core/neighbour.c:2955-2957), so the two tables are disjoint and the two
// proxy entries here appear in no other capture. That is the difference from
// neighDevPcap above, and the reason the rows below could not have been
// written against the bare command's fixture with a filter.
//
// It is also the only capture in the repo whose RTM_NEWNEIGH replies carry
// ndm_state 0 — pneigh_fill_info never sets one (:2722-2749) — which is what
// makes the state-suppression in render.NeighView reachable from a real
// kernel rather than only from a constructed byte slice.
const neighProxyPcap = "../../pkg/xtcpnl/testdata/7_1_4/dumps/netlink_route_getneigh_proxy.pcap"

// neighTunnelPcap and neighTunnelDevPcap are the tunnel namespace's neighbor
// dumps, and they are the only captures in the corpus whose NDA_LLADDR does
// not sit on an ARPHRD_ETHER device — so they are the only ones where
// ll_addr_n2a's type-driven arms are reachable from a neighbor.
//
// # What the topology could and could not produce
//
// Two entries were requested on gre1 and one survives, with a destination of
// 0.0.0.0 rather than the 203.0.113.5 that was asked for. That is not a
// capture fault; it is what the kernel does. gre1 is <POINTOPOINT,NOARP>, so
// its neighbor keys degenerate to the all-zero address and both entries land
// on one key, the second overwriting the first. The lladdr is truncated to
// dev->addr_len, which for ARPHRD_IPGRE is 4 — which is why a request for
// 02:00:00:00:00:07 is stored, and rendered, as the four bytes 02:00:00:00.
//
// Two consequences worth stating rather than discovering later:
//
//   - The 4-byte v4 arm IS proven here, on bytes `ip` wrote. The dotted quad
//     "2.0.0.0" can only come from inet_ntop(AF_INET) on an ARPHRD_IPGRE
//     device; a hex loop gives "02:00:00:00".
//   - A SIX-byte lladdr on a tunnel type is not capturable at all, because
//     the kernel will not store more than dev->addr_len. That negative stays
//     constructed, in render/neigh_test.go, and it is unreachable rather than
//     merely unwritten.
//
// A multipoint GRE — `type gre` with a local and no remote — would give real
// destinations and is the obvious improvement; it costs another capture run.
const neighTunnelPcap = "../../pkg/xtcpnl/testdata/7_1_4/dumps/tunnel/netlink_route_getneigh.pcap"

const neighTunnelDevPcap = "../../pkg/xtcpnl/testdata/7_1_4/dumps/tunnel/netlink_route_getneigh_dev.pcap"

// neighDumpPortid is the portid `ip neigh show` used during the capture.
// Replay filters replies on it, so a wrong value yields an empty listing
// rather than an error.
//
// It is a property of the RUN, not of the command: it is the netlink socket's
// portid, which on Linux defaults to the pid. Re-capturing
// netlink_route_getneigh.pcap therefore changes it and every row that filters
// on it goes quiet — an empty listing compared against a non-empty sidecar,
// which fails loudly, but for a reason that looks nothing like its cause.
// Read it back out of the new pcap rather than guessing.
const neighDumpPortid = "1198"

const neighSidecarDir = "../../pkg/xtcpnl/testdata/7_1_4/dumps/"

// TestNeighShowMatchesCapturedSidecars replays the committed RTM_GETNEIGH dump
// and compares goip's output with the `ip neigh show` sidecars captured
// alongside it.
//
// The comparison target is `ip_neigh`, the plain form, not `ip_neigh_n`. The
// `_n` suffix in this corpus means `ip -d` (nix/capture-netlink-fixtures.nix:389
// says so, and nix/microvms/mkVm.nix:1928 writes it with `ip -d neigh show`).
// The two files are byte-identical here, because `-d` adds nothing to a
// neighbor line the way it adds `unicast`, `table main` and `scope global` to
// a route line — but asserting against the plain form is what keeps that an
// observation rather than a dependency. goip DOES implement -d now, and the
// `_n` half of all three namespaces' pairs is asserted in
// TestNeighShowIgnoresDetails, which compares the two sidecars to each other.
//
// # The empty rows
//
// build_mesh adds no neighbors and neither it nor build_tunnel adds a proxy
// entry, so six of the sidecars below are empty listings. wantEmpty makes that
// the assertion rather than letting an empty-against-empty comparison pass
// vacuously: the pcap holds a real request and a real NLMSG_DONE, so what is
// being asserted is that goip asked and got nothing back, not that it never
// asked.
//
// go test ./internal/goip/ -run TestNeighShowMatchesCapturedSidecars
func TestNeighShowMatchesCapturedSidecars(t *testing.T) {
	tests := []struct {
		description string
		args        []string
		// pcap overrides neighDumpPcap for a row whose command has a capture
		// of its own.
		pcap    string
		sidecar string
		// jsonEquivalent compares decoded JSON instead of raw bytes, because
		// `ip -j -p` pretty-prints and goip emits one compact line.
		jsonEquivalent bool
		// unordered compares the lines as a multiset. See the row that sets
		// it for why a neighbor listing needs this and a link listing does
		// not.
		unordered bool
		// noPortid replays the whole file instead of filtering on
		// neighDumpPortid, which is the right thing for a capture taken in
		// the quiet microVM and the wrong thing for the older ones.
		noPortid bool
		// wantEmpty makes emptiness the assertion rather than something an
		// empty-against-empty comparison lets through. It is checked in both
		// directions — a row that sets it over a non-empty sidecar fails, and
		// so does a row that leaves it unset over an empty one.
		wantEmpty bool
	}{
		{
			// # Why this is a multiset and not byte equality
			//
			// goip sorts neighbors (model.SortNeighbors) and `ip` does not —
			// it prints them in kernel hash order, which is a property of the
			// run rather than of the topology. These rows WERE byte-exact,
			// and passed only because the capture they cited happened to come
			// out ascending. The comment that stood here said so, and said
			// that a re-capture would break them "without anything being
			// wrong, and the fix then is to bring them here".
			//
			// That is what happened. The capture that added the flagged
			// entries emitted .51, .56, .54, .52, .50, .55, .53 — hash order
			// — and the four byte-exact rows failed exactly as predicted. So
			// the prediction is now the behavior: compared as a multiset of
			// lines, which is the same position internal/goipparity takes
			// (FacetLines is a multiset, and stdout.go says it is "blind to a
			// reordering"), so these rows match the gate rather than being
			// stricter than it.
			//
			// What is NOT given up: every line must still be present, exactly
			// once each, byte for byte including the trailing space. Only the
			// order between lines is free.
			description: "positive: `neigh show` reproduces ip_neigh line for line",
			args:        []string{"neigh", "show"},
			sidecar:     "ip_neigh",
			unordered:   true,
		},
		{
			// `ip neigh` with no verb lists, so a bare object must not be a
			// usage error.
			description: "positive: a bare `neigh` is a show",
			args:        []string{"neigh"},
			sidecar:     "ip_neigh",
			unordered:   true,
		},
		{
			description: "positive: `neigh list` is the same listing as `neigh show`",
			args:        []string{"neigh", "list"},
			sidecar:     "ip_neigh",
			unordered:   true,
		},
		{
			// The third spelling, ip/ipneigh.c:759. It is not a prefix of
			// anything else and reads like a typo, which is exactly why goip
			// carried show and list and dropped it — the verb test was a
			// hand-written pair of conditions rather than the list the C is.
			description: "positive: `neigh lst` is the third accepted spelling of the listing",
			args:        []string{"neigh", "lst"},
			sidecar:     "ip_neigh",
			unordered:   true,
		},
		{
			// matches() again, so every one of the three abbreviates. "ls" is
			// a prefix of "lst" and of nothing else in the chain.
			description: "boundary: `neigh ls` abbreviates lst, because the verbs go through matches()",
			args:        []string{"neigh", "ls"},
			sidecar:     "ip_neigh",
			unordered:   true,
		},
		{
			// The `-d` variant, asserted because it IS identical for this
			// corpus: if a future capture makes it differ, this row fails and
			// says so rather than leaving the claim in a comment.
			description: "boundary: ip_neigh_n is identical to ip_neigh, because -d adds no neighbor token",
			args:        []string{"neigh", "show"},
			sidecar:     "ip_neigh_n",
			unordered:   true,
		},
		{
			// unordered for the same reason as the text rows above, and it
			// is not redundant with jsonEquivalent: that one decodes both
			// sides but still walks them BY INDEX, so it is every bit as
			// order-sensitive as bytes.Equal. The entries are matched on
			// `dst` instead, which is unique per listing.
			description:    "positive: `-json neigh show` reproduces ip_neigh_json's keys and values",
			args:           []string{"-json", "neigh", "show"},
			sidecar:        "ip_neigh_json",
			jsonEquivalent: true,
			unordered:      true,
		},
		{
			// `neigh show dev goip0`, against its own capture. Compared as a
			// multiset of lines rather than byte for byte, and the reason is
			// measured rather than defensive: the run that produced this
			// sidecar emitted 192.0.2.52, .51, .50 — kernel hash order —
			// where the committed ip_neigh from an earlier run emitted them
			// ascending. Same topology, same command, different order, which
			// is exactly what model.SortNeighbors' doc says to expect.
			//
			// goip normalizes that order and `ip` does not, so byte equality
			// on a neighbor listing is a property of which run the fixture
			// came from. internal/goipparity takes the same position for the
			// same reason — FacetLines is a multiset and stdout.go:164-165
			// says it is "blind to a reordering" — so this row matches the gate
			// rather than being stricter than it.
			//
			// The four rows above ARE byte-exact, and they pass because the
			// capture they cite happens to be ascending. That is worth
			// knowing rather than relying on: a re-capture of
			// netlink_route_getneigh.pcap can break them without anything
			// being wrong, and the fix then is to bring them here.
			description: "positive: `neigh show dev` reproduces ip_neigh_dev, which is ip_neigh minus one token per line",
			args:        []string{"neigh", "show", "dev", "goip0"},
			pcap:        neighDevPcap,
			sidecar:     "ip_neigh_dev",
			unordered:   true,
			// neighDumpPortid belongs to the capture the rows above replay.
			// This file is from a later run with its own portid, and it is
			// single-command clean, so the filter is unnecessary here and
			// would hide every reply — the same call the route captures make
			// (obj_route_test.go:26-39).
			noPortid: true,
		},
		{
			// `neigh show proxy` against its own capture, and this row is the
			// one that decides whether the renderer is right, because two of
			// its three assertions could not be made anywhere else in the
			// corpus.
			//
			// The `proxy` TOKEN: print_neigh's flag run (ip/ipneigh.c:441)
			// prints it between lladdr and the state, and no other committed
			// reply sets a named ndm_flags bit.
			//
			// The ABSENT state: these entries carry ndm_state 0, print_neigh
			// guards the whole state call on `if (r->ndm_state)` at :462, and
			// print_neigh_state opens its JSON array from inside that guard
			// at :239-240. So `ip` emits neither a token nor a key. goip
			// printed NONE here until this capture existed to say otherwise —
			// byte equality against a file `ip` wrote is what caught it,
			// which is the argument for the sidecars generally.
			//
			// And the TRAILING SPACE: every token in that run carries its own
			// from "%s ", so the line ends "proxy \n" rather than "proxy\n".
			// A strings.Join renderer gets the token right and this wrong.
			description: "positive: `neigh show proxy` reproduces ip_neigh_proxy, tokens and trailing space alike",
			args:        []string{"neigh", "show", "proxy"},
			pcap:        neighProxyPcap,
			sidecar:     "ip_neigh_proxy",
			noPortid:    true,
		},
		{
			// The JSON half, and it asserts a literal `ip` gets from
			// print_null and goip could easily get from print_bool: the value
			// is null, not true. AddrView.MarshalJSON produces the other
			// shape for exactly the flags iproute2 prints with print_bool, so
			// the two marshalers differ on purpose and this row is what says
			// which is which.
			//
			// jsonEquivalent decodes both sides, so it compares the key SET
			// and the values — an omitted "state" key here is a real
			// assertion rather than whitespace luck.
			description:    "positive: `-json neigh show proxy` emits a null-valued proxy key and no state key",
			args:           []string{"-json", "neigh", "show", "proxy"},
			pcap:           neighProxyPcap,
			sidecar:        "ip_neigh_proxy_json",
			jsonEquivalent: true,
			noPortid:       true,
		},
		{
			// A repeat is accepted and changes nothing, because :571-572 has
			// no duparg and no NEXT_ARG — it is a bare assignment, so the
			// second one assigns the same bit. Idempotent by accident of how
			// iproute2 is written rather than by design, which is why it gets
			// a row: a goip that "tidied it up" into an error would diverge
			// on a command line `ip` accepts.
			description: "corner: `neigh show proxy proxy` is accepted and identical to one proxy",
			args:        []string{"neigh", "show", "proxy", "proxy"},
			pcap:        neighProxyPcap,
			sidecar:     "ip_neigh_proxy",
			noPortid:    true,
		},
		{
			// Kept last because it is the negative: the proxy fixture is the
			// only one in the corpus whose entries the DEFAULT state mask
			// cannot match, so replaying it as a bare `neigh show` must
			// produce the entries anyway — via the NTF_PROXY escape at
			// :335-339 and not via the mask. A goip that wrote the skip as
			// "NUD_NOARP only" passes this by luck; one that wrote it as the
			// mask alone prints nothing and fails.
			//
			// Not a claim about what `ip neigh show` returns on the host —
			// the kernel would never put these in that dump. It is a claim
			// about print_neigh, replayed.
			description: "boundary: a state-0 reply survives the default state mask through the NTF_PROXY escape",
			args:        []string{"neigh", "show"},
			pcap:        neighProxyPcap,
			sidecar:     "ip_neigh_proxy",
			// This file is from a later run with its own portid, and it is
			// single-command clean, so the filter is unnecessary here and
			// would hide every reply — the same call the route captures make
			// (obj_route_test.go:26-39).
			noPortid: true,
		},
		{
			// **The third consumer of ll_addr_n2a, on real bytes.** Every
			// other neighbor in the corpus sits on an ARPHRD_ETHER device,
			// where the type-aware path and a plain hex loop agree. These two
			// do not:
			//
			//	0.0.0.0         dev gre1     lladdr 2.0.0.0       (4 bytes, ARPHRD_IPGRE)
			//	2001:db8:100::5 dev ip6tnl1  lladdr 2001:db8::63  (16 bytes, ARPHRD_TUNNEL6)
			//
			// A renderer that formatted NDA_LLADDR without resolving the
			// DEVICE's ifi_type prints "02:00:00:00" and
			// "20:01:0d:b8:...:63" — well-formed, plausible, and wrong on
			// both lines. `ip` resolves it with
			// ll_index_to_type(r->ndm_ifindex) (ip/ipneigh.c:428-430) because
			// struct ndmsg carries no type of its own.
			description: "positive: tunnel `neigh show` renders lladdr through the DEVICE's ifi_type",
			args:        []string{"neigh", "show"},
			pcap:        neighTunnelPcap,
			sidecar:     "tunnel/ip_neigh",
			noPortid:    true,
		},
		{
			description:    "positive: the JSON half carries the same two type-driven lladdr values",
			args:           []string{"-json", "neigh", "show"},
			pcap:           neighTunnelPcap,
			sidecar:        "tunnel/ip_neigh_json",
			jsonEquivalent: true,
			noPortid:       true,
		},
		{
			// The interaction that is invisible until it breaks. `dev NAME`
			// suppresses the printed `dev` token, and the TYPE lookup must
			// still run on the real ndm_ifindex. A renderer that reused the
			// suppressed value would resolve type 0, ARPHRD_NETROM, and
			// regress this one line to "02:00:00:00" while every other row
			// in this file still passed.
			//
			// There is a constructed row for it in render/neigh_test.go; this
			// is the same claim against bytes `ip` wrote.
			description: "corner: `neigh show dev` on a tunnel keeps the type lookup after suppressing the token",
			args:        []string{"neigh", "show", "dev", "gre1"},
			pcap:        neighTunnelDevPcap,
			sidecar:     "tunnel/ip_neigh_dev",
			noPortid:    true,
		},

		// THE EMPTY LISTINGS.
		//
		// Six sidecars, all of them captures of a table that has nothing in
		// it, and all of them committed unread until now. What they pin is a
		// precondition the rest of the corpus depends on without stating:
		// build_mesh creates a bridge and a veth pair and adds no NEIGHBOR,
		// and neither it nor build_tunnel adds a PROXY entry. Every
		// expectation elsewhere that counts neighbors in a namespace is
		// relying on that, and nothing checked it.
		//
		// They are also the only rows in the file where the DUMP SHAPE is
		// visible on its own. A request that the kernel refused, a filter
		// that matched nothing, and a table that is genuinely empty all print
		// the same empty string — so these rows are paired with
		// TestNeighShowDumpShape, which pins the request, and the empty
		// output is the other end of that.
		{
			description: "negative: the mesh namespace's neighbor table is empty by construction",
			args:        []string{"neigh", "show"},
			pcap:        guestDumpsDir + "mesh/netlink_route_getneigh.pcap",
			sidecar:     "mesh/ip_neigh",
			noPortid:    true,
			wantEmpty:   true,
		},
		{
			// `[ ]` from `ip -j -p`, `[]` from goip: the brackets are what is
			// being asserted, not the whitespace. An empty listing must still
			// be a JSON ARRAY — a renderer that emitted `null` for an empty
			// slice is the common Go mistake here, and it would pass the text
			// row above and fail this one.
			description:    "negative: and in JSON it is an empty array, not null",
			args:           []string{"-json", "neigh", "show"},
			pcap:           guestDumpsDir + "mesh/netlink_route_getneigh.pcap",
			sidecar:        "mesh/ip_neigh_json",
			jsonEquivalent: true,
			noPortid:       true,
			wantEmpty:      true,
		},
		{
			// `neigh show dev` over the same empty table, which is a
			// different request — NDA_IFINDEX is set — reaching the same
			// empty answer.
			description: "negative: `neigh show dev veth0` is empty too, off a filtered request",
			args:        []string{"neigh", "show", "dev", "veth0"},
			pcap:        guestDumpsDir + "mesh/netlink_route_getneigh_dev.pcap",
			sidecar:     "mesh/ip_neigh_dev",
			noPortid:    true,
			wantEmpty:   true,
		},
		{
			// The proxy table in both namespaces. The clean namespace HAS
			// two proxy entries (the row above), so these two are the
			// negative half of that positive: `neigh show proxy` renders the
			// entries where they exist and nothing where they do not, off the
			// same NTF_PROXY request shape.
			description: "negative: the mesh namespace has no proxy entry, where the clean namespace has two",
			args:        []string{"neigh", "show", "proxy"},
			pcap:        guestDumpsDir + "mesh/netlink_route_getneigh_proxy.pcap",
			sidecar:     "mesh/ip_neigh_proxy",
			noPortid:    true,
			wantEmpty:   true,
		},
		{
			description:    "negative: the mesh proxy listing in JSON is an empty array",
			args:           []string{"-json", "neigh", "show", "proxy"},
			pcap:           guestDumpsDir + "mesh/netlink_route_getneigh_proxy.pcap",
			sidecar:        "mesh/ip_neigh_proxy_json",
			jsonEquivalent: true,
			noPortid:       true,
			wantEmpty:      true,
		},
		{
			// The tunnel namespace, which DOES have two neighbors — the two
			// with the type-driven lladdrs above — and still no proxy entry.
			// That split is what makes this row more than a repeat of the
			// mesh one: it separates "the namespace is empty" from "the proxy
			// table is empty", and only a namespace with a populated unicast
			// table can.
			description: "corner: the tunnel namespace has two neighbors and no proxy entry, which separates the two tables",
			args:        []string{"neigh", "show", "proxy"},
			pcap:        guestDumpsDir + "tunnel/netlink_route_getneigh_proxy.pcap",
			sidecar:     "tunnel/ip_neigh_proxy",
			noPortid:    true,
			wantEmpty:   true,
		},
		{
			description:    "corner: the tunnel proxy listing in JSON is an empty array",
			args:           []string{"-json", "neigh", "show", "proxy"},
			pcap:           guestDumpsDir + "tunnel/netlink_route_getneigh_proxy.pcap",
			sidecar:        "tunnel/ip_neigh_proxy_json",
			jsonEquivalent: true,
			noPortid:       true,
			wantEmpty:      true,
		},
	}

	for _, tc := range tests {
		t.Run(tc.description, func(t *testing.T) {
			want, err := os.ReadFile(neighSidecarDir + tc.sidecar)
			if err != nil {
				t.Fatal(err)
			}
			pcap := tc.pcap
			if pcap == "" {
				pcap = neighDumpPcap
			}
			t.Setenv("GOIP_REPLAY", pcap)
			if !tc.noPortid {
				t.Setenv("GOIP_REPLAY_PORTID", neighDumpPortid)
			}
			var stdout, stderr bytes.Buffer
			if code := Run(tc.args, &stdout, &stderr); code != ExitOK {
				t.Fatalf("Run(%q) = %d, stderr=%s", tc.args, code, stderr.String())
			}
			// Counted before the comparison, and on both sides, because the
			// comparison itself cannot tell an empty listing from a missing
			// one: assertJSONEntriesEqual passes on two zero-length arrays
			// and bytes.Equal on two zero-length strings. Checking the
			// sidecar as well as the output is what makes the direction
			// matter — a row that claims emptiness over a populated capture
			// is as wrong as one that renders nothing where `ip` rendered
			// something, and only the first of those is otherwise silent.
			gotEntries := neighEntryCount(t, stdout.Bytes(), tc.jsonEquivalent)
			wantEntries := neighEntryCount(t, want, tc.jsonEquivalent)
			if (wantEntries == 0) != tc.wantEmpty {
				t.Fatalf("sidecar %s holds %d entries but wantEmpty = %v; the row "+
					"and the fixture disagree about what was captured",
					tc.sidecar, wantEntries, tc.wantEmpty)
			}
			if (gotEntries == 0) != tc.wantEmpty {
				t.Fatalf("goip rendered %d entries, want empty = %v\n%s",
					gotEntries, tc.wantEmpty, stdout.String())
			}

			switch {
			case tc.jsonEquivalent:
				assertJSONEntriesEqual(t, stdout.Bytes(), want, tc.unordered)
			case tc.unordered:
				assertSameLines(t, stdout.String(), string(want))
			default:
				if !bytes.Equal(stdout.Bytes(), want) {
					t.Fatalf("output mismatch\n got: %q\nwant: %q", stdout.Bytes(), want)
				}
			}
		})
	}
}

// neighEntryCount counts the neighbor entries in a listing: array elements for
// the JSON forms, non-blank lines for the text forms.
//
// It exists because byte length cannot express emptiness uniformly across the
// two. An empty text listing is nothing at all, while an empty JSON listing is
// `[ ]` from `ip -j -p` and `[]` from goip — four bytes and two, neither of
// them zero, and both meaning the same thing.
//
// The `null` rejection is not belt-and-braces. `null` is what a Go renderer
// emits for a nil slice, and it is invisible to every other comparison in this
// file: assertJSONEntriesEqual unmarshals both sides into []map[string]any, and
// `null` and `[]` both decode to length 0, so `null` against the `[ ]` sidecar
// passes it. Measured, not assumed — json.Unmarshal leaves the slice nil for
// `null` and sets it to a non-nil empty slice for `[]` and `[ ]`, which is the
// only place in the pipeline the two are distinguishable.
func neighEntryCount(t *testing.T, raw []byte, isJSON bool) int {
	t.Helper()
	if isJSON {
		var entries []json.RawMessage
		if err := json.Unmarshal(raw, &entries); err != nil {
			t.Fatalf("unmarshal %q as a JSON array: %v", raw, err)
		}
		if entries == nil {
			t.Fatalf("%q decodes to JSON null, not an array; an empty listing "+
				"must still be []", raw)
		}
		return len(entries)
	}
	n := 0
	for _, line := range strings.Split(string(raw), "\n") {
		if strings.TrimSpace(line) != "" {
			n++
		}
	}
	return n
}

// assertSameLines compares two listings as multisets of lines, reporting the
// symmetric difference in both directions so a missing line and an extra one
// are distinguishable.
//
// A multiset and not a set: two identical lines are two entries, and a
// renderer that emitted one of a duplicated pair would otherwise pass.
func assertSameLines(t *testing.T, got, want string) {
	t.Helper()
	count := func(s string) map[string]int {
		m := map[string]int{}
		for _, line := range strings.Split(strings.TrimSuffix(s, "\n"), "\n") {
			m[line]++
		}
		return m
	}
	g, w := count(got), count(want)
	for line, n := range w {
		if g[line] != n {
			t.Errorf("line %q appears %d times, want %d", line, g[line], n)
		}
	}
	for line, n := range g {
		if _, ok := w[line]; !ok {
			t.Errorf("unexpected line %q, %d times", line, n)
		}
	}
}

// assertJSONEntriesEqual compares two `ip -j`-shaped arrays of objects key by
// key in both directions, so a missing key and an extra key are both reported.
//
// unordered sorts both sides on `dst` first, for the neighbor listings where
// goip's sort and the kernel's hash order disagree. Decoding the JSON does
// NOT make the comparison order-insensitive on its own — the loop below walks
// got[i] against want[i] — so a caller that needs that has to say so. The key
// is `dst` because it is the one member every entry has and no two entries in
// a listing share; sorting on the marshaled form instead would be sensitive
// to the member order this file's fixtures exist to pin.
func assertJSONEntriesEqual(t *testing.T, gotRaw, wantRaw []byte, unordered bool) {
	t.Helper()
	var got, want []map[string]any
	if err := json.Unmarshal(gotRaw, &got); err != nil {
		t.Fatalf("goip JSON: %v (%s)", err, gotRaw)
	}
	if err := json.Unmarshal(wantRaw, &want); err != nil {
		t.Fatalf("sidecar JSON: %v", err)
	}
	if len(got) != len(want) {
		t.Fatalf("got %d entries, want %d", len(got), len(want))
	}
	if unordered {
		byDst := func(s []map[string]any) {
			sort.Slice(s, func(i, j int) bool {
				a, _ := s[i]["dst"].(string)
				b, _ := s[j]["dst"].(string)
				return a < b
			})
		}
		byDst(got)
		byDst(want)
	}
	for i := range want {
		for k, wv := range want[i] {
			gv, ok := got[i][k]
			if !ok {
				t.Errorf("entry %d missing key %q", i, k)
				continue
			}
			if !jsonEqual(gv, wv) {
				t.Errorf("entry %d key %q = %#v, want %#v", i, k, gv, wv)
			}
		}
		for k := range got[i] {
			if _, ok := want[i][k]; !ok {
				t.Errorf("entry %d has extra key %q", i, k)
			}
		}
	}
}

// jsonEqual compares two decoded JSON values structurally.
//
// Arrays are walked elementwise so a failure names the entry that differs;
// everything else falls through to reflect.DeepEqual. The fallback is not
// decoration: `==` on an `any` holding a map PANICS, and route rendering
// produces maps — `via` is a nested object, and `metrics` and `nexthops` are
// arrays of them — where neighbor rendering produces only strings.
func jsonEqual(a, b any) bool {
	as, aok := a.([]any)
	bs, bok := b.([]any)
	if aok != bok {
		return false
	}
	if !aok {
		return reflect.DeepEqual(a, b)
	}
	if len(as) != len(bs) {
		return false
	}
	for i := range as {
		if !jsonEqual(as[i], bs[i]) {
			return false
		}
	}
	return true
}

// TestRunNeighArgs covers the CLI surface: the verbs runNeigh accepts, and the
// selectors it refuses rather than silently ignores. Ignoring a selector would
// be worse than refusing it, because the parity harness drives `ip` and `goip`
// with the same argv and would then be comparing two different questions.
//
// go test ./internal/goip/ -run TestRunNeighArgs
func TestRunNeighArgs(t *testing.T) {
	const firstEntry = "192.0.2.50 dev goip0 "

	tests := []struct {
		description      string
		args             []string
		wantCode         int
		wantStdoutPrefix string
		wantStderrSubstr string
	}{
		{
			description:      "positive: `neigh show` renders the first captured entry",
			args:             []string{"neigh", "show"},
			wantCode:         ExitOK,
			wantStdoutPrefix: firstEntry,
		},
		{
			// "n" reaches neigh, and the verb abbreviates through matches() too.
			description:      "positive: `n s` abbreviates both the object and the verb",
			args:             []string{"n", "s"},
			wantCode:         ExitOK,
			wantStdoutPrefix: firstEntry,
		},
		{
			// The kernel's own spelling, and the one the object table registers.
			description:      "positive: `neighbour show` spells the object out",
			args:             []string{"neighbour", "show"},
			wantCode:         ExitOK,
			wantStdoutPrefix: firstEntry,
		},
		{
			description:      "negative: an unknown neigh verb is refused",
			args:             []string{"neigh", "frobnicate"},
			wantCode:         ExitUsage,
			wantStderrSubstr: "not implemented",
		},
		{
			// A write verb must stay unreachable: goip is read-only, and `add`
			// reaching a handler at all would be the wrong kind of bug.
			description:      "negative: the write verb `add` is refused",
			args:             []string{"neigh", "add"},
			wantCode:         ExitUsage,
			wantStderrSubstr: "not implemented",
		},
		{
			// The selector, replayed against the BARE command's capture. That
			// works — and is worth doing here rather than only against the
			// dev capture — because the replay answers any RTM_GETNEIGH with
			// the recorded bodies, so what this row exercises is the
			// client-side half: print_neigh's :331 index test and the :415
			// token suppression. The request half needs real bytes and is
			// TestTierANeighShowDevRequests' job.
			description:      "positive: `neigh show dev goip0` lists, with no dev token on the line",
			args:             []string{"neigh", "show", "dev", "goip0"},
			wantCode:         ExitOK,
			wantStdoutPrefix: "192.0.2.50 lladdr ",
		},
		{
			// Resolution is from ll_init_map's cache, so a name absent from
			// the link dump fails locally. ExitFailure and not ExitUsage: the
			// selector IS implemented, so this is a bad command line rather
			// than a missing feature.
			description:      "negative: a device not in the link dump is an error, not an empty listing",
			args:             []string{"neigh", "show", "dev", "nosuch0"},
			wantCode:         ExitFailure,
			wantStderrSubstr: `cannot find device "nosuch0"`,
		},
		{
			// The empty name is a real command line, and it must reach the
			// resolver rather than being read as "no device given" — the same
			// distinction routeSelectors.DevSet exists for.
			description:      "corner: `dev \"\"` resolves nothing and fails, rather than listing everything",
			args:             []string{"neigh", "show", "dev", ""},
			wantCode:         ExitFailure,
			wantStderrSubstr: `cannot find device ""`,
		},
		{
			// duparg (ip/ipneigh.c:526-527). This is where neigh parts company
			// with route, whose `dev`/`oif` pair assigns one variable and lets
			// the last one win.
			description:      "negative: a second `dev` is duparg, not last-wins",
			args:             []string{"neigh", "show", "dev", "goip0", "dev", "lo"},
			wantCode:         ExitFailure,
			wantStderrSubstr: "duplicate",
		},
		{
			// NEXT_ARG() on an exhausted argv.
			description:      "boundary: a trailing `dev` with no value is refused",
			args:             []string{"neigh", "show", "dev"},
			wantCode:         ExitFailure,
			wantStderrSubstr: "missing its value",
		},
		{
			// `proxy` and `dev` compose: do_show_or_flush has one argument
			// loop and no mutual exclusion between them, so `ip` accepts the
			// pair and sets both halves of the request — ndm_flags at offset
			// 10 and NDA_IFINDEX after it, in that write order (:490, :493).
			//
			// Replayed against the BARE capture, for the reason the `dev` row
			// above gives: the replay answers any RTM_GETNEIGH with the
			// recorded bodies, so what this asserts is that goip accepts the
			// combination and still applies the :415 dev-token suppression to
			// whatever comes back. The request bytes are
			// xtcpnl_rtnetlink_requests_test.go's, which checks the two
			// selectors land in the right order.
			description:      "corner: `proxy` and `dev` compose, and `dev` still suppresses the token",
			args:             []string{"neigh", "show", "proxy", "dev", "goip0"},
			wantCode:         ExitOK,
			wantStdoutPrefix: "192.0.2.50 lladdr ",
		},
		{
			// Order-independent, because the loop assigns rather than
			// sequences. Worth a row of its own: a hand-written parser that
			// treated `proxy` as a mode switch instead of a flag assignment
			// would plausibly accept one order and not the other.
			description:      "corner: `dev` before `proxy` is the same command",
			args:             []string{"neigh", "show", "dev", "goip0", "proxy"},
			wantCode:         ExitOK,
			wantStdoutPrefix: "192.0.2.50 lladdr ",
		},
		{
			// strcmp at :571, so `proxy` does not abbreviate either. In `ip`
			// this falls to the else-arm and is read as a destination prefix;
			// goip refuses it, the same position it takes on `d`.
			description:      "negative: `prox` does not abbreviate `proxy`",
			args:             []string{"neigh", "show", "prox"},
			wantCode:         ExitUsage,
			wantStderrSubstr: "not implemented",
		},
		{
			// strcmp, not matches(): `d` is not an abbreviation of `dev`
			// here. In `ip` it falls to the else-arm and is read as a
			// destination prefix; goip refuses it, which is the honest
			// version of not implementing `to PREFIX`.
			description:      "negative: `d` does not abbreviate `dev`",
			args:             []string{"neigh", "show", "d", "goip0"},
			wantCode:         ExitUsage,
			wantStderrSubstr: "not implemented",
		},
		{
			description:      "boundary: a bare `neigh` lists rather than erroring",
			args:             []string{"neigh"},
			wantCode:         ExitOK,
			wantStdoutPrefix: firstEntry,
		},
		{
			// runNeigh rejects args[1] before it sends anything, so a second
			// argument fails even when the first one is a valid verb.
			description:      "boundary: one extra argument after a valid verb is refused",
			args:             []string{"neigh", "show", "nud"},
			wantCode:         ExitUsage,
			wantStderrSubstr: "not implemented",
		},
		{
			// Options precede the object in iproute2, so this is a selector
			// named "-4", not a family option.
			description:      "corner: an option after the object is not an option",
			args:             []string{"neigh", "show", "-4"},
			wantCode:         ExitUsage,
			wantStderrSubstr: "not implemented",
		},
	}

	for _, tc := range tests {
		t.Run(tc.description, func(t *testing.T) {
			t.Setenv("GOIP_REPLAY", neighDumpPcap)
			t.Setenv("GOIP_REPLAY_PORTID", neighDumpPortid)
			var stdout, stderr bytes.Buffer
			code := Run(tc.args, &stdout, &stderr)
			if code != tc.wantCode {
				t.Errorf("Run(%q) = %d, want %d; stderr: %s", tc.args, code, tc.wantCode, stderr.String())
			}
			if tc.wantStdoutPrefix != "" && !strings.HasPrefix(stdout.String(), tc.wantStdoutPrefix) {
				t.Errorf("stdout = %q, want prefix %q", stdout.String(), tc.wantStdoutPrefix)
			}
			if tc.wantStderrSubstr != "" && !strings.Contains(stderr.String(), tc.wantStderrSubstr) {
				t.Errorf("stderr = %q, want to contain %q", stderr.String(), tc.wantStderrSubstr)
			}
		})
	}
}

// runNeighWith drives runNeigh against an arbitrary Source, which is what lets
// the transaction-shape test below count what Run() hides.
func runNeighWith(t *testing.T, src Source, family uint8, args []string) (string, error) {
	t.Helper()
	var out bytes.Buffer
	c := &runCtx{
		src:    src,
		lltab:  NewLLTab(),
		out:    &out,
		errOut: io.Discard,
		family: family,
	}
	err := runNeigh(c, args)
	return out.String(), err
}

// TestNeighShowDevTransactionShape is the assertion this whole increment
// exists for, and it is an assertion of ABSENCE: `neigh show dev NAME` sends
// two dumps and NO single-get, where every other `dev NAME` form in goip sends
// at least one.
//
// do_show_or_flush calls ll_init_map(&rth) at ip/ipneigh.c:597 before it
// resolves the name at :600, so ll_name_to_index's ll_get_by_name hits the
// cache that dump just filled and ll_link_get is never reached
// (lib/ll_map.c:354-359). A goip that reached for LinkByName here — the
// obvious thing to do, and what route, link and addr all correctly do — would
// produce byte-identical output and one extra transaction.
//
// The source is routeReplay, borrowed from obj_route_test.go rather than
// duplicated: it counts dumps and records every single-get, which is exactly
// the pair of numbers at issue. Its Talk arm is the one that must never fire.
//
// Every row replays netlink_route_getneigh.pcap, the BARE command's capture.
// The replay answers whatever is asked, so what varies between rows is only
// what goip asks and what it prints — the request bytes are Tier A's subject,
// in internal/goip/req.
//
// go test ./internal/goip/ -run TestNeighShowDevTransactionShape
func TestNeighShowDevTransactionShape(t *testing.T) {
	tests := []struct {
		description string
		args        []string
		wantDumps   int
		wantErr     bool
		// wantEmpty asserts the output is exactly empty, which no substring
		// test can do.
		wantEmpty bool
		// wantStdoutHas and wantStdoutLacks assert one token's presence or
		// absence rather than the whole rendering, which the sidecar test
		// above already pins.
		wantStdoutHas   []string
		wantStdoutLacks []string
	}{
		{
			description:   "positive: the bare form is two dumps and no single-get",
			args:          []string{"show"},
			wantDumps:     2,
			wantStdoutHas: []string{"192.0.2.50 dev goip0 "},
		},
		{
			// The delta, and there is none to count. Same dump count, same
			// empty get list — contrast TestRouteShowDevTransactionShape,
			// where the equivalent row has one get the bare form does not.
			description:     "positive: `dev NAME` costs the same two dumps and still no single-get",
			args:            []string{"show", "dev", "goip0"},
			wantDumps:       2,
			wantStdoutHas:   []string{"192.0.2.50 lladdr "},
			wantStdoutLacks: []string{"dev goip0"},
		},
		{
			// The client-side half of the filter (ip/ipneigh.c:331), which
			// the replay makes load-bearing: it answers the filtered request
			// with the whole recorded dump, so without the test every entry
			// would print. On a live socket the kernel would have filtered
			// already and this would be belt and braces, exactly as it is in
			// `ip`.
			description: "boundary: a device with no neighbors prints nothing, and still costs two dumps",
			args:        []string{"show", "dev", "lo"},
			wantDumps:   2,
			wantEmpty:   true,
		},
		{
			// Resolution is from the cache, so failure is local and costs
			// only the link dump. `ip` would send ll_link_get here and pay a
			// third transaction; goip stops, which is the position routeShow
			// takes for the same reason.
			description: "negative: an unresolvable device fails after the link dump, with no second dump",
			args:        []string{"show", "dev", "nosuch0"},
			wantDumps:   1,
			wantErr:     true,
		},
	}

	for _, tc := range tests {
		t.Run(tc.description, func(t *testing.T) {
			src := newRouteReplay(t, neighDumpPcap)
			got, err := runNeighWith(t, src, unix.AF_UNSPEC, tc.args)
			if tc.wantErr {
				if err == nil {
					t.Fatalf("runNeigh(%q) = %q, want error", tc.args, got)
				}
			} else if err != nil {
				t.Fatalf("runNeigh(%q): %v", tc.args, err)
			}
			if src.dumps != tc.wantDumps {
				t.Errorf("dumps = %d, want %d", src.dumps, tc.wantDumps)
			}
			if len(src.gets) != 0 {
				t.Errorf("single-gets = %v, want none: ll_init_map already filled the cache", src.gets)
			}
			if tc.wantEmpty && got != "" {
				t.Errorf("stdout = %q, want empty", got)
			}
			for _, want := range tc.wantStdoutHas {
				if !strings.Contains(got, want) {
					t.Errorf("stdout = %q, want it to contain %q", got, want)
				}
			}
			for _, unwanted := range tc.wantStdoutLacks {
				if strings.Contains(got, unwanted) {
					t.Errorf("stdout = %q, want it NOT to contain %q", got, unwanted)
				}
			}
		})
	}
}

// TestNeighShowEntryCount pins the entry count against the sidecar, which is
// what makes runNeigh's one filtering rule observable: iproute2's default state
// filter omits NUD_NOARP entries, and runNeigh reproduces that. If the filter
// ever dropped a wanted entry — or stopped dropping an unwanted one — the count
// moves even when every rendered line still looks right.
//
// go test ./internal/goip/ -run TestNeighShowEntryCount
// TestNeighShowEntryCount checks the listing length against the sidecar's,
// for each spelling of the verb.
//
// The expectation is DERIVED from ip_neigh rather than written as a literal,
// and that is a correction rather than a convenience. It used to say 4, which
// silently meant "the number of neighbors the topology had when this was
// written" — so adding the flagged entries to nltopo::build_clean broke it in
// a way that named neither the topology nor the capture. A literal here is a
// second, unmarked copy of a fact the fixture already states.
//
// It still fails for everything it is meant to catch: a renderer that drops
// an entry, emits one twice, or forgets the final newline. What it no longer
// does is fail for a topology change that both files agree about.
func TestNeighShowEntryCount(t *testing.T) {
	sidecar, err := os.ReadFile(neighSidecarDir + "ip_neigh")
	if err != nil {
		t.Fatal(err)
	}
	wantLines := strings.Count(string(sidecar), "\n")
	if wantLines == 0 {
		t.Fatalf("sidecar ip_neigh has no lines, so this test would assert nothing")
	}

	tests := []struct {
		description string
		args        []string
		wantLines   int
	}{
		{
			description: "positive: the listing has exactly as many entries as the sidecar",
			args:        []string{"neigh", "show"},
			wantLines:   wantLines,
		},
		{
			description: "boundary: `neigh list` yields the same count as `neigh show`",
			args:        []string{"neigh", "list"},
			wantLines:   wantLines,
		},
	}

	for _, tc := range tests {
		t.Run(tc.description, func(t *testing.T) {
			t.Setenv("GOIP_REPLAY", neighDumpPcap)
			t.Setenv("GOIP_REPLAY_PORTID", neighDumpPortid)
			var stdout, stderr bytes.Buffer
			if code := Run(tc.args, &stdout, &stderr); code != ExitOK {
				t.Fatalf("Run(%q) = %d, stderr=%s", tc.args, code, stderr.String())
			}
			if got := strings.Count(stdout.String(), "\n"); got != tc.wantLines {
				t.Errorf("lines = %d, want %d; output %q", got, tc.wantLines, stdout.String())
			}
		})
	}
}

// The `-s neigh show` captures, one per namespace.
//
// Written by `capio`, so each pcap and the text sidecar beside it came from
// ONE invocation — which matters more here than anywhere else in the sweep.
// `ndm_used`, `ndm_confirmed` and `ndm_updated` are "jiffies since", so they
// tick between any two invocations: the text golden below says `used 93/91/91`
// and ip_neigh_stats_json, a second invocation seconds later, says 257/255/255
// for the same entry. A `side` sidecar would have forced a normalizer over the
// middle of the very line under test.
const (
	neighStatsPcap       = neighSidecarDir + "netlink_route_getneigh_stats.pcap"
	neighStatsMeshPcap   = neighSidecarDir + "mesh/netlink_route_getneigh_stats.pcap"
	neighStatsTunnelPcap = neighSidecarDir + "tunnel/netlink_route_getneigh_stats.pcap"
)

// TestNeighShowStatsMatchesCapturedSidecars diffs `goip -s neigh show` against
// the `ip -s neigh show` transcripts captured with it.
//
// # What these goldens settled
//
// Step 4 wrote print_cacheinfo's spacing out of the C and nothing could
// confirm it, because the corpus had no `-s neigh` transcript. Six `want`
// strings in render/neigh_test.go were written with a space where upstream has
// none, failed, and were corrected toward the source. These files are the
// independent check on that correction, and they agree with it on all five
// counts:
//
//   - a DOUBLED space after the lladdr or the last flag, because every
//     print_cacheinfo format carries a leading space and no trailing one;
//   - a run-on `used 115/115/115probes 0 `, because `probes %u ` is the
//     reverse — trailing space, no leading one;
//   - no ` ref ` on any line, because ndm_refcnt is zero on all of them and
//     the token is suppressed rather than printed as `ref 0`;
//   - `probes 0` printed, because NDA_PROBES arrived carrying zero and
//     present-and-zero is not absent;
//   - the block printed for 192.0.2.52, which has no lladdr at all.
//
// # Why these compare as multisets
//
// goip sorts neighbors (model.SortNeighbors) and `ip` prints kernel hash
// order, which is a property of the run. This capture came out .55 .56 .54
// .51 .53 .52 .50 — not ascending — so byte equality would fail on order
// alone. internal/goipparity takes the same position (FacetLines is a
// multiset), so these rows match the gate rather than being stricter than it.
// Every line must still be present exactly once, byte for byte, trailing
// space included.
//
// go test ./internal/goip/ -run TestNeighShowStatsMatchesCapturedSidecars
func TestNeighShowStatsMatchesCapturedSidecars(t *testing.T) {
	tests := []struct {
		description string
		args        []string
		pcap        string
		sidecar     string
		// jsonEquivalent decodes both sides and matches entries on `dst`.
		jsonEquivalent bool
		// zeroCacheCounters replaces the three jiffies-since values with 0 on
		// both sides. Set on the JSON rows alone — see zeroNeighCacheCounters
		// for the division of labor between them and the text rows.
		zeroCacheCounters bool
	}{
		{
			description: "positive: the clean topology reproduces ip_neigh_stats line for line, spacing quirks included",
			args:        []string{"-s", "neigh", "show"},
			pcap:        neighStatsPcap,
			sidecar:     "ip_neigh_stats",
		},
		{
			// The two entries no other fixture has: a 4-byte dotted-quad
			// lladdr on an ARPHRD_IPGRE device and a 16-byte one on
			// ARPHRD_TUNNEL6. They are the only lines in the corpus where
			// ll_addr_n2a's special cases and the `-s` block have to be right
			// at once, and the only ones where the three cacheinfo members
			// DISAGREE — `used 93/91/91` — so this is the row that would fail
			// if the renderer read one member three times.
			description: "positive: the tunnel topology reproduces tunnel/ip_neigh_stats, where used and confirmed differ",
			args:        []string{"-s", "neigh", "show"},
			pcap:        neighStatsTunnelPcap,
			sidecar:     "tunnel/ip_neigh_stats",
		},
		{
			// Empty by construction, and asserted rather than skipped:
			// build_mesh adds no neighbors, so `ip -s neigh show` printed
			// nothing at all. A renderer that emitted a bare ` used 0/0/0`
			// line per reply, or a header, fails here and nowhere else.
			description: "negative: the mesh topology reproduces mesh/ip_neigh_stats, which is empty because build_mesh adds no neighbors",
			args:        []string{"-s", "neigh", "show"},
			pcap:        neighStatsMeshPcap,
			sidecar:     "mesh/ip_neigh_stats",
		},
		{
			// The JSON half. Keys are `used`, `confirmed`, `updated`,
			// `probes` — and NOT `refcnt`, which print_cacheinfo suppresses
			// at zero in both encodings. The key-set comparison is what
			// asserts that absence, so this row is where "suppressed" is
			// distinguished from "present and zero" on the JSON side.
			description:       "positive: -s -json neigh show reproduces ip_neigh_stats_json's key set, refcnt absent",
			args:              []string{"-s", "-json", "neigh", "show"},
			pcap:              neighStatsPcap,
			sidecar:           "ip_neigh_stats_json",
			jsonEquivalent:    true,
			zeroCacheCounters: true,
		},
		{
			description:       "positive: the tunnel topology reproduces tunnel/ip_neigh_stats_json over both lladdr widths",
			args:              []string{"-s", "-json", "neigh", "show"},
			pcap:              neighStatsTunnelPcap,
			sidecar:           "tunnel/ip_neigh_stats_json",
			jsonEquivalent:    true,
			zeroCacheCounters: true,
		},
		{
			// `[ ]`, not `null`. open_json_array runs before the loop, so an
			// empty table is an empty array, and a marshaler that let a nil
			// slice through emits null and fails here.
			description:       "negative: the mesh topology reproduces mesh/ip_neigh_stats_json, an empty array rather than null",
			args:              []string{"-s", "-json", "neigh", "show"},
			pcap:              neighStatsMeshPcap,
			sidecar:           "mesh/ip_neigh_stats_json",
			jsonEquivalent:    true,
			zeroCacheCounters: true,
		},
	}

	for _, tc := range tests {
		t.Run(tc.description, func(t *testing.T) {
			want, err := os.ReadFile(neighSidecarDir + tc.sidecar)
			if err != nil {
				t.Fatal(err)
			}
			// No GOIP_REPLAY_PORTID: these captures are single-command
			// clean, taken one at a time in the quiet microVM, so filtering
			// on neighDumpPortid — which belongs to an older run — would
			// hide every reply and compare an empty listing against a
			// non-empty sidecar.
			t.Setenv("GOIP_REPLAY", tc.pcap)
			var stdout, stderr bytes.Buffer
			if code := Run(tc.args, &stdout, &stderr); code != ExitOK {
				t.Fatalf("Run(%q) = %d, stderr=%s", tc.args, code, stderr.String())
			}

			if tc.jsonEquivalent {
				got, w := stdout.Bytes(), want
				if tc.zeroCacheCounters {
					got, w = zeroNeighCacheCounters(t, got), zeroNeighCacheCounters(t, w)
				}
				assertJSONEntriesEqual(t, got, w, true)
				return
			}
			assertSameLines(t, stdout.String(), string(want))
		})
	}
}

// zeroNeighCacheCounters replaces `used`, `confirmed` and `updated` with 0 so
// two listings taken seconds apart compare on shape alone.
//
// # Why only the JSON rows need it, and what they give up
//
// ip_neigh_stats_json is a `side` sidecar: a second `ip` invocation, not the
// one whose replies are in the pcap. All three members are jiffies since an
// event, so they had ticked by roughly 1.2 seconds' worth — 93 became 257 on
// the tunnel entry. The text rows above need no such subtraction, because
// `capio` recorded their pcap and their sidecar from one invocation.
//
// The division of labor is therefore deliberate rather than a shortfall. The
// text goldens pin the VALUES, including the tunnel entry's `93/91/91` where
// the three members disagree and a renderer reading one of them three times
// would be caught; these rows pin the KEY SET, which is the half the text
// cannot see — in particular that `refcnt` is absent rather than zero.
//
// Rewriting rather than deleting, for the reason zeroLinkStats gives: a
// deleted key is indistinguishable from a matching one, and the member set is
// exactly what this comparison is for.
func zeroNeighCacheCounters(t *testing.T, raw []byte) []byte {
	t.Helper()

	var entries []map[string]any
	if err := json.Unmarshal(raw, &entries); err != nil {
		t.Fatalf("zeroNeighCacheCounters: %v (%s)", err, raw)
	}
	for _, e := range entries {
		for _, k := range []string{"used", "confirmed", "updated"} {
			if _, ok := e[k]; ok {
				e[k] = float64(0)
			}
		}
	}
	out, err := json.Marshal(entries)
	if err != nil {
		t.Fatalf("zeroNeighCacheCounters: %v", err)
	}
	return out
}

// TestNeighShowStatsGate is the `-s neigh show` evidence that needs no sidecar
// to state: that the block is gated on show_stats and not on attribute
// presence, and that the four tokens suppress independently.
//
// It replays the `-s` capture, whose replies carry NDA_CACHEINFO and
// NDA_PROBES on every entry — neigh_fill_info emits the pair in one `||`
// chain (kernel net/core, :2690-2692), so an entry has both or neither. That
// makes this the one fixture where "the attributes arrived" is certain, and
// therefore the only one on which the gate can be shown to be a separate
// decision.
//
// go test ./internal/goip/ -run TestNeighShowStatsGate
func TestNeighShowStatsGate(t *testing.T) {
	run := func(t *testing.T, args ...string) string {
		t.Helper()
		t.Setenv("GOIP_REPLAY", neighStatsPcap)
		var stdout, stderr bytes.Buffer
		if code := Run(args, &stdout, &stderr); code != ExitOK {
			t.Fatalf("Run(%q) = %d, stderr=%s", args, code, stderr.String())
		}
		return stdout.String()
	}

	tests := []struct {
		description string
		check       func(t *testing.T)
	}{
		{
			description: "negative: neigh show on the -s capture prints no cacheinfo, though every reply carries NDA_CACHEINFO and NDA_PROBES",
			check: func(t *testing.T) {
				out := run(t, "neigh", "show")
				if out == "" {
					t.Fatal("no entries rendered; there is nothing to gate")
				}
				for _, tok := range []string{" used ", " ref ", "probes "} {
					if strings.Contains(out, tok) {
						t.Errorf("ungated output carries %q:\n%s", tok, firstLines(out, 4))
					}
				}
			},
		},
		{
			description: "negative: -json neigh show carries none of the four keys either, so the JSON form is gated the same way",
			check: func(t *testing.T) {
				var entries []map[string]any
				if err := json.Unmarshal([]byte(run(t, "-json", "neigh", "show")), &entries); err != nil {
					t.Fatalf("unmarshal: %v", err)
				}
				if len(entries) == 0 {
					t.Fatal("no entries decoded")
				}
				for i, e := range entries {
					for _, k := range []string{"used", "confirmed", "updated", "probes", "refcnt"} {
						if _, ok := e[k]; ok {
							t.Errorf("entry %d carries key %q without -s", i, k)
						}
					}
				}
			},
		},
		{
			// `probes 0` on every line, and no `ref` on any. Both halves of
			// the same point, which is why they are one row: the two tokens
			// are suppressed by DIFFERENT rules, and the fixture happens to
			// exercise both at once. ndm_refcnt is zero and print_cacheinfo
			// guards ` ref %u` on it, so nothing prints; NDA_PROBES is also
			// zero and is printed anyway, because its guard is the
			// attribute's presence, not its value.
			description: "boundary: every line carries probes 0 and none carries ref, because the two are suppressed by different rules",
			check: func(t *testing.T) {
				lines := strings.Split(strings.TrimSuffix(run(t, "-s", "neigh", "show"), "\n"), "\n")
				if len(lines) != 9 {
					t.Fatalf("%d lines, want the capture's 9", len(lines))
				}
				for _, l := range lines {
					if !strings.Contains(l, "probes 0 ") {
						t.Errorf("line has no `probes 0 `: %q", l)
					}
					if strings.Contains(l, " ref ") {
						t.Errorf("line carries a ref token, which ndm_refcnt 0 suppresses: %q", l)
					}
				}
			},
		},
		{
			// The entry with no lladdr. print_neigh prints the `-s` block
			// from the same place whether or not NDA_LLADDR was there, so the
			// INCOMPLETE line must carry it too — and the DOUBLED space that
			// precedes it comes from the dev token's own trailing space
			// meeting print_cacheinfo's leading one, with nothing in between.
			description: "corner: the lladdr-less INCOMPLETE entry still prints the block, after a doubled space",
			check: func(t *testing.T) {
				out := run(t, "-s", "neigh", "show")
				const want = "192.0.2.52 dev goip0  used 12/72/12probes 0 INCOMPLETE "
				for _, l := range strings.Split(out, "\n") {
					if strings.HasPrefix(l, "192.0.2.52 ") {
						if l != want {
							t.Errorf("INCOMPLETE line\n got: %q\nwant: %q", l, want)
						}
						return
					}
				}
				t.Errorf("no 192.0.2.52 entry in:\n%s", firstLines(out, 12))
			},
		},
		{
			// The position claim, which a keyword check cannot make. The
			// block lands between the flag run and the state, not appended
			// at the end of the line (ip/ipneigh.c:465). This row reads the
			// one entry that has a flag, a block and a state all three.
			description: "corner: the block sits between the flag run and the state, not at the end of the line",
			check: func(t *testing.T) {
				out := run(t, "-s", "neigh", "show")
				const want = "192.0.2.56 dev goip0 lladdr 02:00:00:00:00:07 router extern_learn extern_valid  used 11/11/11probes 0 PERMANENT "
				for _, l := range strings.Split(out, "\n") {
					if strings.HasPrefix(l, "192.0.2.56 ") {
						if l != want {
							t.Errorf("flagged line\n got: %q\nwant: %q", l, want)
						}
						return
					}
				}
				t.Errorf("no 192.0.2.56 entry in:\n%s", firstLines(out, 12))
			},
		},
	}

	for _, tc := range tests {
		t.Run(tc.description, func(t *testing.T) { tc.check(t) })
	}
}
