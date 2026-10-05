package goip

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"os"
	"strconv"
	"strings"
	"testing"

	"github.com/randomizedcoder/xtcp2/internal/goip/model"
	"github.com/randomizedcoder/xtcp2/pkg/xtcpnl"
	"golang.org/x/sys/unix"
)

// The route captures are the gated microVM's, one command each, taken with no
// other netlink traffic on the host:
//
//	netlink_route_getroute.pcap            `ip route show`
//	netlink_route_getroute6.pcap           `ip -6 route show`
//	netlink_route_getroute_table_all.pcap  `ip route show table all`
//	mesh/…                                 the same three on the linkdown topology
//
// # Why these replays set GOIP_REPLAY and deliberately NOT GOIP_REPLAY_PORTID
//
// A route capture is not one conversation. `ip route show` runs its dump on the
// main socket and then opens a FRESH socket per lazy name lookup
// (ll_link_get, lib/ll_map.c:280), so the RTM_NEWLINK replies carry a different
// portid from the RTM_NEWROUTE replies — in netlink_route_getroute.pcap, 2603126454
// against the dump's 812. ReplaySource.Dump filters on a single portid, so
// setting one to the dump's value would hide every RTM_NEWLINK reply and every
// `dev` token with it. Leaving it unset replays the whole file, which is
// correct here precisely because the capture is single-command clean.
//
// That is the opposite of the neigh and bulk-addr captures, which were taken on
// a busy desktop and NEED the portid filter. The difference is a property of
// the capture, not a preference.
const (
	routeDumpPcap     = "../../pkg/xtcpnl/testdata/7_1_4/dumps/netlink_route_getroute.pcap"
	routeDump6Pcap    = "../../pkg/xtcpnl/testdata/7_1_4/dumps/netlink_route_getroute6.pcap"
	routeDumpAllPcap  = "../../pkg/xtcpnl/testdata/7_1_4/dumps/netlink_route_getroute_table_all.pcap"
	routeMeshPcap     = "../../pkg/xtcpnl/testdata/7_1_4/dumps/mesh/netlink_route_getroute.pcap"
	routeMesh6Pcap    = "../../pkg/xtcpnl/testdata/7_1_4/dumps/mesh/netlink_route_getroute6.pcap"
	routeMeshAllPcap  = "../../pkg/xtcpnl/testdata/7_1_4/dumps/mesh/netlink_route_getroute_table_all.pcap"
	routeSidecarDir   = "../../pkg/xtcpnl/testdata/7_1_4/dumps/"
	routeMeshSidecars = "../../pkg/xtcpnl/testdata/7_1_4/dumps/mesh/"

	// The tunnel set. Same four commands again, on the one namespace where
	// every route belongs to a device that has no link-layer address at all:
	// gre1's `link/gre` is an IP pair, not a MAC, so these captures are the
	// only route evidence whose `dev` token resolves to a non-ARPHRD_ETHER
	// device through the lazy single-get path.
	routeTunnelPcap     = "../../pkg/xtcpnl/testdata/7_1_4/dumps/tunnel/netlink_route_getroute.pcap"
	routeTunnel6Pcap    = "../../pkg/xtcpnl/testdata/7_1_4/dumps/tunnel/netlink_route_getroute6.pcap"
	routeTunnelAllPcap  = "../../pkg/xtcpnl/testdata/7_1_4/dumps/tunnel/netlink_route_getroute_table_all.pcap"
	routeTunnelDevPcap  = "../../pkg/xtcpnl/testdata/7_1_4/dumps/tunnel/netlink_route_getroute_dev.pcap"
	routeTunnelSidecars = "../../pkg/xtcpnl/testdata/7_1_4/dumps/tunnel/"

	// `ip route show dev NAME`, the one route capture whose FIRST message is
	// an RTM_GETLINK rather than the dump. The mesh one's named device owns no
	// routes, so its dump is answered by NLMSG_DONE alone — the only empty
	// listing in the corpus, and the reason `route show dev` can be shown to
	// cost the same two transactions whatever the answer is.
	routeDevPcap     = "../../pkg/xtcpnl/testdata/7_1_4/dumps/netlink_route_getroute_dev.pcap"
	routeMeshDevPcap = "../../pkg/xtcpnl/testdata/7_1_4/dumps/mesh/netlink_route_getroute_dev.pcap"
)

// TestRouteShowMatchesCapturedSidecars replays each committed route dump and
// compares goip's stdout with the `ip route show` sidecar captured beside it.
//
// The comparison target is the plain sidecar, never the `_n` one: `_n` in this
// corpus means `ip -d` (nix/microvms/mkVm.nix writes it with `ip -d route
// show`), which adds `unicast`, `table main` and `scope global` to every line.
// goip does implement -d, and the `_n` sidecars for all three namespaces are
// compared in TestRouteShowDetailMatchesSidecars; they do not belong here
// because every row in this table drives goip WITHOUT -d, and a row whose
// expectation came from a different command line is the one mistake a table
// this uniform makes easy.
//
// go test ./internal/goip/ -run TestRouteShowMatchesCapturedSidecars
func TestRouteShowMatchesCapturedSidecars(t *testing.T) {
	tests := []struct {
		description string
		pcap        string
		args        []string
		sidecar     string
		// jsonEquivalent compares decoded JSON instead of raw bytes, because
		// `ip -j -p` pretty-prints and goip emits one compact line.
		jsonEquivalent bool
	}{
		{
			description: "positive: `route show` reproduces ip_route_main line for line, trailing spaces included",
			pcap:        routeDumpPcap,
			args:        []string{"route", "show"},
			sidecar:     "ip_route_main",
		},
		{
			// do_iproute falls through to a list when argc is 0
			// (ip/iproute.c:2420), so a bare object must not be a usage error.
			description: "positive: a bare `route` is a list",
			pcap:        routeDumpPcap,
			args:        []string{"route"},
			sidecar:     "ip_route_main",
		},
		{
			description: "positive: `-6 route show` reproduces ip_route6, whose lines end flush at `pref medium`",
			pcap:        routeDump6Pcap,
			args:        []string{"-6", "route", "show"},
			sidecar:     "ip_route6",
		},
		{
			// The only form that prints a `table` token, and the only one that
			// reaches a second device — `lo` — and so a second single-get.
			description: "positive: `route show table all` reproduces ip_route_table_all, across both families and two tables",
			pcap:        routeDumpAllPcap,
			args:        []string{"route", "show", "table", "all"},
			sidecar:     "ip_route_table_all",
		},
		{
			// The golden that proves the `dev` selector is not a text filter.
			// ip_route_dev is NOT ip_route_main with the non-matching lines
			// deleted: every main line has LOST its `dev goip0` token, because
			// print_route guards it on `filter.oifmask != -1` (:900-901) —
			// while both nexthops of the multipath route have KEPT theirs,
			// because the nexthop tokens at :743 and :751 have no guard at
			// all. The word the command named appears nowhere on the lines it
			// selected and twice on the lines it did not, and a line-by-line
			// comparison against a real capture is the only thing that can
			// state both halves at once.
			description: "positive: `route show dev` reproduces ip_route_dev, which is not a subset of ip_route_main",
			pcap:        routeDevPcap,
			args:        []string{"route", "show", "dev", "goip0"},
			sidecar:     "ip_route_dev",
		},
		{
			// `oif` is not an abbreviation of anything and not a second
			// filter: ip/iproute.c:2145-2151 assigns both keywords to the same
			// local `od`, so this must be byte-identical to the row above.
			description: "positive: `route show oif` is the same command as `route show dev`",
			pcap:        routeDevPcap,
			args:        []string{"route", "show", "oif", "goip0"},
			sidecar:     "ip_route_dev",
		},
		{
			description:    "positive: `-json route show` reproduces ip_route_main_json's keys and values",
			pcap:           routeDumpPcap,
			args:           []string{"-json", "route", "show"},
			sidecar:        "ip_route_main_json",
			jsonEquivalent: true,
		},

		// ---------------------------------------------------------------
		// `-s` on routes: every row below is a NEGATIVE, because
		// `-s route show` is a no-op — which is NOT what the plan that
		// added RTA_CACHEINFO predicted, and is worth stating here rather
		// than only in the docs.
		//
		// print_rta_cacheinfo gates exactly three of its eight members on
		// show_stats: rta_clntref (`users`), rta_used and rta_lastuse
		// (`age`), ip/iproute.c:500-532. All three are written only inside
		// rtnl_put_cacheinfo's `if (dst)` arm
		// (net/core/rtnetlink.c:1028-1052), and no route DUMP ever takes
		// it — rt_fill_info passes a dst on a `route get` only, and the v4
		// FIB dump never calls rtnl_put_cacheinfo at all
		// (net/ipv4/route.c:3074); rt6_fill_node serves both the v6 dump
		// and the get but passes a dst only on the get
		// (net/ipv6/route.c:5944).
		//
		// Measured against these very fixtures by
		// xtcpnl.TestParseNewRouteCacheinfo: the v4 dumps carry no
		// RTA_CACHEINFO at all and the v6 dumps carry one per route with
		// all 32 bytes zero. So the attribute now decodes and the three
		// gated members have nothing to print.
		//
		// The rows compare against the SAME sidecar each non-`-s` row
		// uses, which is what makes them assert the no-op rather than
		// re-assert the rendering. The complementary claim — that
		// IPROUTE2's `-s` also changes nothing, measured on a real `ip`
		// transcript — is TestRouteStatsSidecarsAreIdentical's.
		//
		// What is NOT a no-op, and is not visible here: rta_expires is
		// reachable without a dst (net/ipv6/route.c:5931) and
		// print_rta_cacheinfo prints it OUTSIDE the show_stats guard, so
		// plain `-6 route show` was dropping `expires Nsec` until that was
		// fixed. No route in any committed capture has a finite lifetime,
		// which is exactly why no row here can see it; render/route_test.go
		// carries the constructed rows that can.
		// ---------------------------------------------------------------
		{
			description: "negative: `-s route show` is byte-identical to `route show` — the three members -s gates are structurally zero on a dump",
			pcap:        routeDumpPcap,
			args:        []string{"-s", "route", "show"},
			sidecar:     "ip_route_main",
		},
		{
			// The family that actually carries the attribute. Every route
			// in this dump has an RTA_CACHEINFO on the wire, all 32 bytes
			// zero, so this row is the one that distinguishes "suppressed
			// because the values are zero" from "suppressed because the
			// attribute is not decoded" — the second would also pass the
			// v4 row above.
			description: "negative: `-s -6 route show` equals ip_route6, even though every route in that dump carries an RTA_CACHEINFO",
			pcap:        routeDump6Pcap,
			args:        []string{"-s", "-6", "route", "show"},
			sidecar:     "ip_route6",
		},
		{
			description: "negative: `-s route show table all` equals ip_route_table_all across both families and two tables",
			pcap:        routeDumpAllPcap,
			args:        []string{"-s", "route", "show", "table", "all"},
			sidecar:     "ip_route_table_all",
		},
		{
			// A JSON key is the failure mode a text comparison cannot see:
			// a stats block that rendered as an empty object, or as keys
			// with zero values, would print nothing in text and surface
			// here as extra members.
			description: "negative: `-s -json route show` adds no JSON key to ip_route_main_json",
			pcap:        routeDumpPcap,
			args:        []string{"-s", "-json", "route", "show"},
			sidecar:     "ip_route_main_json",

			jsonEquivalent: true,
		},
		{
			// The `dev` form, because its print path differs from the bare
			// one in a way that touches the same guard: print_route
			// suppresses `dev NAME` on the selected lines and keeps it on
			// the nexthops, and the cacheinfo block would land after both.
			description: "negative: `-s route show dev` equals ip_route_dev, so -s composes with the selector by changing nothing",
			pcap:        routeDevPcap,
			args:        []string{"-s", "route", "show", "dev", "goip0"},
			sidecar:     "ip_route_dev",
		},
	}

	for _, tc := range tests {
		t.Run(tc.description, func(t *testing.T) {
			want, err := os.ReadFile(routeSidecarDir + tc.sidecar)
			if err != nil {
				t.Fatal(err)
			}
			t.Setenv("GOIP_REPLAY", tc.pcap)
			var stdout, stderr bytes.Buffer
			if code := Run(tc.args, &stdout, &stderr); code != ExitOK {
				t.Fatalf("Run(%q) = %d, stderr=%s", tc.args, code, stderr.String())
			}
			if tc.jsonEquivalent {
				assertJSONEntriesEqual(t, stdout.Bytes(), want, false)
				return
			}
			if !bytes.Equal(stdout.Bytes(), want) {
				t.Fatalf("output mismatch\n got: %q\nwant: %q", stdout.Bytes(), want)
			}
		})
	}
}

// TestRouteStatsSidecarsAreIdentical compares the `-s` route goldens against
// their non-`-s` counterparts, and is a claim about IPROUTE2 rather than about
// goip.
//
// The `-s` rows in TestRouteShowMatchesCapturedSidecars replay a pcap through
// goip and compare the result with a transcript of `ip` taken WITHOUT `-s`.
// That catches a goip which started emitting a token, and nothing else: if a
// future iproute2 began reading show_stats in print_route, every one of those
// rows would still pass, because none of them runs `ip`. These rows do — they
// read two real `ip` transcripts, one given `-s` and one not — so they are the
// only offline evidence that the no-op belongs to upstream.
//
// The live form of the same claim is the route_show_stats row in
// internal/goipparity/commands.go, which is stronger because both binaries run
// milliseconds apart in one boot. It needs /dev/kvm, so on a plain `go test`
// nothing but this function checks it.
//
// The negative row is load-bearing. Every other row here asserts equality, so
// a helper that always reported "identical" would make the whole table
// vacuous; `ip_route_main` against `ip_route_main_n` is a pair known to differ,
// on this very corpus, for a reason recorded in the row.
//
// go test ./internal/goip/ -run TestRouteStatsSidecarsAreIdentical
func TestRouteStatsSidecarsAreIdentical(t *testing.T) {
	tests := []struct {
		description string
		a, b        string
		want        bool
	}{
		{
			// The three members print_rta_cacheinfo gates on show_stats come
			// from rtnl_put_cacheinfo's `if (dst)` arm, which no dump takes.
			description: "positive: ip_route_main_stats and ip_route_main are byte-identical, `-s` being a no-op on a v4 dump that carries no RTA_CACHEINFO at all",
			a:           "ip_route_main_stats", b: "ip_route_main", want: true,
		},
		{
			// The family where the attribute IS on the wire — one per route,
			// all 32 bytes zero. This is the row that separates "nothing to
			// print" from "nothing arrived", and it is why ip_route6_stats
			// was captured even though the sweep's plan listed only the v4
			// golden.
			description: "positive: ip_route6_stats and ip_route6 are byte-identical, even though every route in that dump carries an RTA_CACHEINFO",
			a:           "ip_route6_stats", b: "ip_route6", want: true,
		},
		{
			// The negative that keeps the two rows above honest: a pair of
			// real transcripts of the same table that are known to differ.
			// `-d` unsuppresses the route type, `table`, `proto` and `scope`
			// on every line (ip/iproute.c:828, :903, :909, :916), so a
			// comparison that reported everything identical fails here.
			description: "negative: ip_route_main and ip_route_main_n differ, because -d unsuppresses four tokens on every line",
			a:           "ip_route_main", b: "ip_route_main_n", want: false,
		},
		{
			// Two more namespaces, captured independently. Spelled out per
			// row rather than looped, so a missing file fails rather than
			// passing as "nothing to compare" — the same reason
			// TestRuleSidecarsAreIdentical enumerates its pairs.
			description: "corner: the mesh namespace's `-s` listing is a no-op too, on an independently captured topology",
			a:           "mesh/ip_route_main_stats", b: "mesh/ip_route_main", want: true,
		},
		{
			description: "corner: the tunnel namespace's `-s` listing is a no-op too",
			a:           "tunnel/ip_route_main_stats", b: "tunnel/ip_route_main", want: true,
		},
		{
			description: "corner: the mesh namespace's IPv6 `-s` listing is a no-op too",
			a:           "mesh/ip_route6_stats", b: "mesh/ip_route6", want: true,
		},
		{
			description: "corner: the tunnel namespace's IPv6 `-s` listing is a no-op too",
			a:           "tunnel/ip_route6_stats", b: "tunnel/ip_route6", want: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.description, func(t *testing.T) {
			a, err := os.ReadFile(routeSidecarDir + tt.a)
			if err != nil {
				t.Fatal(err)
			}
			b, err := os.ReadFile(routeSidecarDir + tt.b)
			if err != nil {
				t.Fatal(err)
			}
			if got := bytes.Equal(a, b); got != tt.want {
				t.Errorf("bytes.Equal(%s, %s) = %v, want %v\n %s = %q\n %s = %q",
					tt.a, tt.b, got, tt.want, tt.a, a, tt.b, b)
			}
		})
	}
}

// TestRouteShowMatchesMeshSidecars is the same comparison against the second
// topology, which exists for one reason: it is the only one whose carrier is
// down, so it is the only place `linkdown` appears in a golden.
//
// go test ./internal/goip/ -run TestRouteShowMatchesMeshSidecars
func TestRouteShowMatchesMeshSidecars(t *testing.T) {
	tests := []struct {
		description string
		pcap        string
		args        []string
		sidecar     string
		// jsonEquivalent compares decoded JSON instead of raw bytes, for the
		// same reason as TestRouteShowMatchesCapturedSidecars: `ip -j -p`
		// pretty-prints and goip emits compact JSON.
		jsonEquivalent bool
	}{
		{
			description: "positive: mesh `route show` reproduces the linkdown token on both v4 routes",
			pcap:        routeMeshPcap,
			args:        []string{"route", "show"},
			sidecar:     "ip_route_main",
		},
		{
			// The ordering proof that a unit table cannot give: print_rt_flags
			// runs before print_rt_pref, so the real `ip` wrote `metric 256
			// linkdown pref medium` and goip has to as well.
			description: "positive: mesh `-6 route show` puts linkdown before pref",
			pcap:        routeMesh6Pcap,
			args:        []string{"-6", "route", "show"},
			sidecar:     "ip_route6",
		},
		{
			description: "positive: mesh `route show table all` reproduces both families and the local table",
			pcap:        routeMeshAllPcap,
			args:        []string{"route", "show", "table", "all"},
			sidecar:     "ip_route_table_all",
		},
		{
			// The empty listing, and the only golden in the corpus that is a
			// zero-byte file. `veth0` owns no routes, so the dump answers with
			// NLMSG_DONE alone and `ip` prints nothing — not a blank line, not
			// a diagnostic, and exit 0. Worth a row because "renders nothing"
			// is the easiest output for an implementation to get almost right:
			// a stray newline, or an ExitFailure on an empty answer, would
			// both pass every other test in this file.
			description: "boundary: mesh `route show dev` on a device with no routes prints nothing at all",
			pcap:        routeMeshDevPcap,
			args:        []string{"route", "show", "dev", "veth0"},
			sidecar:     "ip_route_dev",
		},
		{
			// The reason this row is worth more than the clean namespace's
			// JSON golden: print_rt_flags builds a JSON ARRAY, and every
			// `flags` array in every other committed route golden is EMPTY.
			// `[ "linkdown" ]` here is the only captured evidence that the
			// array is ever populated at all — a renderer that emitted the
			// flag as a string, or as a bare `"linkdown": true`, would match
			// the clean golden key for key and fail only here.
			description:    "corner: mesh `route show -json` is the only route golden with a non-empty flags array",
			pcap:           routeMeshPcap,
			args:           []string{"-json", "route", "show"},
			sidecar:        "ip_route_main_json",
			jsonEquivalent: true,
		},
	}

	for _, tc := range tests {
		t.Run(tc.description, func(t *testing.T) {
			want, err := os.ReadFile(routeMeshSidecars + tc.sidecar)
			if err != nil {
				t.Fatal(err)
			}
			t.Setenv("GOIP_REPLAY", tc.pcap)
			var stdout, stderr bytes.Buffer
			if code := Run(tc.args, &stdout, &stderr); code != ExitOK {
				t.Fatalf("Run(%q) = %d, stderr=%s", tc.args, code, stderr.String())
			}
			if tc.jsonEquivalent {
				assertJSONEntriesEqual(t, stdout.Bytes(), want, false)
				return
			}
			if !bytes.Equal(stdout.Bytes(), want) {
				t.Fatalf("output mismatch\n got: %q\nwant: %q", stdout.Bytes(), want)
			}
		})
	}
}

// TestRouteShowMatchesTunnelSidecars is the mesh table's sibling on the third
// namespace, and the four commands are the same four. What makes the set
// different is the device underneath every route: gre1 is ARPHRD_IPGRE, so it
// carries no link-layer address — `link/gre 192.0.2.3 peer 198.51.100.3`, an
// IPv4 pair where every other namespace's routed device has a MAC.
//
// That matters here because `route show` resolves names lazily, one RTM_GETLINK
// single-get per distinct ifindex (lib/ll_map.c:320), and the reply it parses
// is a full RTM_NEWLINK for a tunnel. These captures are the only route
// evidence where that side transaction answers for a non-Ethernet device.
//
// # Why there is no empty row here
//
// The mesh table's `route show dev` row covers the empty listing, because
// veth0 owns no routes. gre1 owns two, so the tunnel `dev` row covers the other
// half of the same shape: the `dev gre1` token is SUPPRESSED from every line,
// since `ip route show dev NAME` does not repeat the device it filtered on.
// Empty output cannot show that suppression and a populated listing can, which
// is why both rows exist and neither is redundant.
//
// go test ./internal/goip/ -run TestRouteShowMatchesTunnelSidecars
func TestRouteShowMatchesTunnelSidecars(t *testing.T) {
	tests := []struct {
		description    string
		pcap           string
		args           []string
		sidecar        string
		jsonEquivalent bool
	}{
		{
			description: "positive: tunnel `route show` renders two routes out of a gre device",
			pcap:        routeTunnelPcap,
			args:        []string{"route", "show"},
			sidecar:     "ip_route_main",
		},
		{
			description: "positive: tunnel `-6 route show` renders the gre device's own prefix and its link-local",
			pcap:        routeTunnel6Pcap,
			args:        []string{"-6", "route", "show"},
			sidecar:     "ip_route6",
		},
		{
			// Thirteen lines, and the widest route listing in the corpus by
			// route TYPE: unicast, local, broadcast and multicast all in one
			// answer, across two families and two tables. `local
			// fe80::5efe:c000:203` is the row's own best evidence — the last
			// 32 bits are gre1's local IPv4 address 192.0.2.3 written as
			// c000:0203, under the ISATAP 5efe marker, so it is a link-local
			// the kernel DERIVED rather than one goip could have guessed.
			description: "positive: tunnel `route show table all` spans four route types and both families",
			pcap:        routeTunnelAllPcap,
			args:        []string{"route", "show", "table", "all"},
			sidecar:     "ip_route_table_all",
		},
		{
			description: "boundary: tunnel `route show dev gre1` suppresses the dev token it filtered on",
			pcap:        routeTunnelDevPcap,
			args:        []string{"route", "show", "dev", "gre1"},
			sidecar:     "ip_route_dev",
		},
		{
			// prefsrc on a device with no link/ether. The key itself is not
			// new — the clean golden has one — but every other JSON route
			// golden's prefsrc belongs to an Ethernet device, and this is the
			// pair to the text row above: same pcap, same two routes, so a
			// divergence between them is a rendering fault and not a decode
			// one.
			description:    "positive: tunnel `route show -json` pairs with the text row off the same capture",
			pcap:           routeTunnelPcap,
			args:           []string{"-json", "route", "show"},
			sidecar:        "ip_route_main_json",
			jsonEquivalent: true,
		},
	}

	for _, tc := range tests {
		t.Run(tc.description, func(t *testing.T) {
			want, err := os.ReadFile(routeTunnelSidecars + tc.sidecar)
			if err != nil {
				t.Fatal(err)
			}
			if len(want) == 0 {
				t.Fatalf("%s is empty; every row in this table expects a "+
					"populated listing, and the empty case lives in the mesh "+
					"table", tc.sidecar)
			}
			t.Setenv("GOIP_REPLAY", tc.pcap)
			var stdout, stderr bytes.Buffer
			if code := Run(tc.args, &stdout, &stderr); code != ExitOK {
				t.Fatalf("Run(%q) = %d, stderr=%s", tc.args, code, stderr.String())
			}
			if tc.jsonEquivalent {
				assertJSONEntriesEqual(t, stdout.Bytes(), want, false)
				return
			}
			if !bytes.Equal(stdout.Bytes(), want) {
				t.Fatalf("output mismatch\n got: %q\nwant: %q", stdout.Bytes(), want)
			}
		})
	}
}

// routeReplay is a ReplaySource that also implements TalkSource, and records
// every request it is handed.
//
// Recording is the whole point. `route show`'s transaction shape — one dump
// plus one lazy RTM_GETLINK single-get per DISTINCT ifindex — is invisible in
// stdout: a run that re-resolved the same device six times would print exactly
// the same six lines. The parity harness catches it on the wire, and this type
// is how a unit test catches it without a microVM.
//
// ReplaySource alone would not do, because it has no Talk method, so
// service.LinkByIndex silently falls back to a dump-and-filter
// (service.go:121) — the very shortcut obj_route.go exists to avoid. Supplying
// Talk here is what puts the code under test on the single-get path.
type routeReplay struct {
	inner *ReplaySource

	// dumps counts Dump calls and gets records the ifi_index of each
	// single-get, in the order they were sent.
	dumps int
	gets  []int32

	// shapes is the same sequence with the selector fields kept, which is
	// what `route show dev NAME` needs: its FIRST get carries an
	// IFLA_IFNAME and an ifi_index of 0, so the gets slice alone cannot tell
	// it apart from a by-index get for index 0 — a request goip must never
	// send.
	shapes []getShape

	// unresolvable names indexes the replayed kernel answers nothing for, so a
	// test can exercise ll_index_to_name's does-not-cache-a-failure path.
	unresolvable map[int32]bool
}

func newRouteReplay(t *testing.T, path string) *routeReplay {
	t.Helper()
	s, err := OpenReplay(path)
	if err != nil {
		t.Fatalf("OpenReplay(%s): %v", path, err)
	}
	return &routeReplay{inner: s, unresolvable: map[int32]bool{}}
}

func (s *routeReplay) Dump(request []byte, msgType uint16) ([][]byte, error) {
	s.dumps++
	return s.inner.Dump(request, msgType)
}

// Talk answers a single-get from the RTM_NEWLINK replies the capture recorded,
// selecting on whichever of ifi_index and IFLA_IFNAME the request carries.
// That is a faithful replay: the recorded replies ARE the answers `ip` got to
// the same gets.
//
// Both selectors are needed because `route show dev NAME` sends one of each.
// ll_name_to_index's get addresses the interface by NAME with ifi_index 0
// (lib/ll_map.c:287-289); every later get is print_route's by-index
// resolution. The name arm is what lets a capture taken without the device
// filter answer the resolution get, which is why the two rows below can share
// netlink_route_getroute.pcap.
func (s *routeReplay) Talk(request []byte, msgType uint16) ([]byte, error) {
	var ifi xtcpnl.IfInfomsg
	if _, err := xtcpnl.DeserializeIfInfomsg(request[xtcpnl.NlMsgHdrSizeCst:], &ifi); err != nil {
		return nil, err
	}
	g := selectorOf(request)
	s.gets = append(s.gets, ifi.Index)
	s.shapes = append(s.shapes, g)
	if s.unresolvable[ifi.Index] {
		return nil, fmt.Errorf("%w: index %d", ErrNoReplay, ifi.Index)
	}
	for _, m := range s.inner.cap.Msgs() {
		if m.IsRequest() || m.Hdr.Type != msgType {
			continue
		}
		li, err := xtcpnl.ParseNewLink(m.Body)
		if err != nil {
			continue
		}
		if g.name != "" {
			if li.Name == g.name {
				return xtcpnl.CopyBytes(m.Body), nil
			}
			continue
		}
		if li.Index == ifi.Index {
			return xtcpnl.CopyBytes(m.Body), nil
		}
	}
	return nil, fmt.Errorf("%w: RTM_GETLINK %s", ErrNoReplay, g)
}

// runRouteWith drives runRoute over a source directly, bypassing Run so the
// test can supply a TalkSource and read the counters back afterwards.
func runRouteWith(t *testing.T, src Source, family uint8, args []string) (string, error) {
	t.Helper()
	var out bytes.Buffer
	c := &runCtx{
		src:    src,
		lltab:  NewLLTab(),
		out:    &out,
		errOut: io.Discard,
		family: family,
	}
	err := runRoute(c, args)
	return out.String(), err
}

// TestRouteShowTransactionShape is the assertion stdout cannot make: how many
// netlink transactions `route show` produces, and for which indexes.
//
// `ip route show` sends NO up-front link dump — iproute_list_flush_or_save
// (ip/iproute.c:1818) never calls ll_init_map — and resolves each name while
// printing, with one RTM_GETLINK single-get per index the cache has never seen
// (lib/ll_map.c:320). An implementation that dumped links instead would print
// identical output and fail the harness's L2 request equality, which is the
// divergence this table exists to catch early.
//
// go test ./internal/goip/ -run TestRouteShowTransactionShape
func TestRouteShowTransactionShape(t *testing.T) {
	tests := []struct {
		description  string
		pcap         string
		family       uint8
		args         []string
		unresolvable []int32
		wantDumps    int
		wantGets     []int32
		wantStdout   string // when non-empty, the exact expected output
	}{
		{
			// Six routes, every one of them on ifindex 3, and exactly one get.
			// This is the lltab cache assertion: routes 2 through 6 must send
			// nothing, and the ECMP route's two nexthops — which also name
			// index 3 — must not send anything either.
			description: "positive: six routes on one device produce one dump and exactly one single-get",
			pcap:        routeDumpPcap,
			family:      unix.AF_UNSPEC,
			args:        []string{"show"},
			wantDumps:   1,
			wantGets:    []int32{3},
		},
		{
			// `table all` reaches the loopback routes too, so a SECOND distinct
			// index appears — and it appears after index 3, because resolution
			// walks routes in dump order. Two gets is exactly what the capture
			// recorded: an 816-byte reply for goip0 and a 796-byte one for lo.
			description: "positive: `table all` resolves a second device, in dump order, for two single-gets",
			pcap:        routeDumpAllPcap,
			family:      unix.AF_UNSPEC,
			args:        []string{"show", "table", "all"},
			wantDumps:   1,
			wantGets:    []int32{3, 1},
		},
		{
			description: "positive: the v6 dump names one device and sends one single-get",
			pcap:        routeDump6Pcap,
			family:      unix.AF_INET6,
			args:        []string{"show"},
			wantDumps:   1,
			wantGets:    []int32{3},
		},
		{
			// ll_index_to_name caches NOTHING when the get fails
			// (lib/ll_map.c:321-327), so the next reference retries. Seven
			// references to index 3 — five RTA_OIFs plus the ECMP route's two
			// nexthops — therefore send seven gets, and every device renders
			// as the `if%u` fallback. Reproducing the retry matters more than
			// saving it: the retry is what a parity capture would show.
			description:  "negative: a single-get that resolves nothing is not cached, so every reference retries",
			pcap:         routeDumpPcap,
			family:       unix.AF_UNSPEC,
			args:         []string{"show"},
			unresolvable: []int32{3},
			wantDumps:    1,
			wantGets:     []int32{3, 3, 3, 3, 3, 3, 3},
			wantStdout: "192.0.2.0/24 dev if3 proto kernel scope link src 192.0.2.1 \n" +
				"198.18.0.0/24 via 192.0.2.10 dev if3 \n" +
				"198.18.1.0/24 via 192.0.2.10 dev if3 mtu 1400 advmss 1300 \n" +
				"198.18.2.0/24 via inet6 2001:db8::2 dev if3 \n" +
				"198.51.100.0/24 dev if3 scope link \n" +
				"203.0.113.0/24 " +
				"\n\tnexthop via 192.0.2.10 dev if3 weight 1 " +
				"\n\tnexthop via 192.0.2.11 dev if3 weight 3 \n",
		},
		{
			// The loopback routes still print, `lo` still resolves, and only
			// the goip0 references retry — a failed lookup must not poison the
			// whole listing.
			description:  "corner: one unresolvable index does not stop the other from resolving",
			pcap:         routeDumpAllPcap,
			family:       unix.AF_UNSPEC,
			args:         []string{"show", "table", "all"},
			unresolvable: []int32{3},
			wantDumps:    1,
			// Five v4 RTA_OIFs on index 3 plus the ECMP route's two nexthops,
			// then lo — which resolves, and so is asked for ONCE even though
			// three loopback routes name it — then the remaining eleven
			// references to index 3, each of which retries.
			wantGets: []int32{
				3, 3, 3, 3, 3, 3, 3,
				1,
				3, 3, 3, 3, 3, 3, 3, 3, 3, 3, 3,
			},
		},
		{
			description: "boundary: `route list` produces the same transactions as `route show`",
			pcap:        routeDumpPcap,
			family:      unix.AF_UNSPEC,
			args:        []string{"list"},
			wantDumps:   1,
			wantGets:    []int32{3},
		},
	}

	for _, tc := range tests {
		t.Run(tc.description, func(t *testing.T) {
			src := newRouteReplay(t, tc.pcap)
			for _, idx := range tc.unresolvable {
				src.unresolvable[idx] = true
			}
			got, err := runRouteWith(t, src, tc.family, tc.args)
			if err != nil {
				t.Fatalf("runRoute(%q): %v", tc.args, err)
			}
			if src.dumps != tc.wantDumps {
				t.Errorf("dumps = %d, want %d", src.dumps, tc.wantDumps)
			}
			if !equalIndexes(src.gets, tc.wantGets) {
				t.Errorf("single-gets = %v, want %v", src.gets, tc.wantGets)
			}
			if tc.wantStdout != "" && got != tc.wantStdout {
				t.Errorf("stdout = %q\nwant     %q", got, tc.wantStdout)
			}
		})
	}
}

// TestRouteShowDevTransactionShape is the assertion that makes `route show dev
// NAME` worth implementing separately from `route show`: the selector ADDS one
// transaction at the front and REMOVES every one at the back.
//
// The front one is ll_name_to_index's throwaway get (ip/iproute.c:2008,
// lib/ll_map.c:354-372) — by NAME, ifi_index 0. The back ones vanish because
// print_route's `dev` token is guarded by `filter.oifmask != -1` (:900) and
// that token is the only caller of ll_index_to_name for RTA_OIF. So a command
// whose bare form costs 1 + one-get-per-distinct-index costs exactly 2 here,
// no matter how many routes come back — and the saving grows with the size of
// the answer, which is the opposite of what an added filter usually does.
//
// Every row shares netlink_route_getroute.pcap, whose six routes are all on
// goip0. The replay answers whatever is asked; what varies is what goip asks.
//
// go test ./internal/goip/ -run TestRouteShowDevTransactionShape
func TestRouteShowDevTransactionShape(t *testing.T) {
	const (
		devName  = "goip0"
		devIndex = int32(3)
	)
	// The resolution get: NLM_F_REQUEST alone, AF_UNSPEC, IFLA_EXT_MASK
	// first and IFLA_IFNAME second (lib/ll_map.c:289-293), ifi_index 0.
	nameGet := getShape{
		flags:     unix.NLM_F_REQUEST,
		family:    unix.AF_UNSPEC,
		firstAttr: unix.IFLA_EXT_MASK,
		name:      devName,
	}

	tests := []struct {
		description string
		args        []string
		family      uint8
		wantDumps   int
		wantShapes  []getShape
		// wantStdout, when non-empty, is the exact expected output.
		wantStdout string
		// wantStdoutHas and wantStdoutLacks are for the rows whose point is
		// one token's presence or absence rather than the whole rendering.
		wantStdoutHas   []string
		wantStdoutLacks []string
	}{
		{
			description: "positive: `show dev NAME` is one resolution get and one dump, and nothing else",
			args:        []string{"show", "dev", devName},
			family:      unix.AF_UNSPEC,
			wantDumps:   1,
			wantShapes:  []getShape{nameGet},
			wantStdout: "192.0.2.0/24 proto kernel scope link src 192.0.2.1 \n" +
				"198.18.0.0/24 via 192.0.2.10 \n" +
				"198.18.1.0/24 via 192.0.2.10 mtu 1400 advmss 1300 \n" +
				"198.18.2.0/24 via inet6 2001:db8::2 \n" +
				"198.51.100.0/24 scope link \n" +
				"203.0.113.0/24 " +
				"\n\tnexthop via 192.0.2.10 dev goip0 weight 1 " +
				"\n\tnexthop via 192.0.2.11 dev goip0 weight 3 \n",
		},
		{
			description: "positive: `oif NAME` is the same two transactions under the other spelling",
			args:        []string{"show", "oif", devName},
			family:      unix.AF_UNSPEC,
			wantDumps:   1,
			wantShapes:  []getShape{nameGet},
		},
		{
			// The bare form for comparison, in the same table so the delta is
			// one diff rather than two files: one dump, and one BY-INDEX get
			// that `dev NAME` does not send.
			description: "negative: the bare form sends a by-index get the dev form does not",
			args:        []string{"show"},
			family:      unix.AF_UNSPEC,
			wantDumps:   1,
			wantShapes: []getShape{{
				flags:     unix.NLM_F_REQUEST,
				family:    unix.AF_UNSPEC,
				firstAttr: unix.IFLA_EXT_MASK,
				index:     devIndex,
			}},
		},
		{
			// The resolution get fills the index cache, so even if the render
			// guard were removed the by-index get would not reappear — which
			// is exactly why the guard needs its own assertion and cannot be
			// inferred from a transaction count on this topology. Asserted
			// here on the STDOUT instead: no `dev goip0` on the main line.
			description:     "boundary: the dev token is gone from the main lines but kept on the nexthops",
			args:            []string{"show", "dev", devName},
			family:          unix.AF_UNSPEC,
			wantDumps:       1,
			wantShapes:      []getShape{nameGet},
			wantStdoutLacks: []string{"192.0.2.0/24 dev goip0"},
			wantStdoutHas:   []string{"nexthop via 192.0.2.10 dev goip0 weight 1 "},
		},
		{
			// `table all` widens the dump to both families and both tables,
			// and on this capture that reaches `lo` as well — but the oif
			// filter drops every lo route, so the second device is never
			// referenced and the transaction count does not move.
			description: "corner: `table all dev NAME` still costs two transactions, because the filter removes the second device",
			args:        []string{"show", "table", "all", "dev", devName},
			family:      unix.AF_UNSPEC,
			wantDumps:   1,
			wantShapes:  []getShape{nameGet},
		},
	}

	for _, tc := range tests {
		t.Run(tc.description, func(t *testing.T) {
			src := newRouteReplay(t, routeDumpPcap)
			got, err := runRouteWith(t, src, tc.family, tc.args)
			if err != nil {
				t.Fatalf("runRoute(%q): %v", tc.args, err)
			}
			if src.dumps != tc.wantDumps {
				t.Errorf("dumps = %d, want %d", src.dumps, tc.wantDumps)
			}
			if len(src.shapes) != len(tc.wantShapes) {
				t.Fatalf("single-gets = %v, want %v", src.shapes, tc.wantShapes)
			}
			for i := range tc.wantShapes {
				if src.shapes[i] != tc.wantShapes[i] {
					t.Errorf("single-get %d = %+v, want %+v", i, src.shapes[i], tc.wantShapes[i])
				}
			}
			if tc.wantStdout != "" && got != tc.wantStdout {
				t.Errorf("stdout = %q\nwant     %q", got, tc.wantStdout)
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

func equalIndexes(got, want []int32) bool {
	if len(got) != len(want) {
		return false
	}
	for i := range got {
		if got[i] != want[i] {
			return false
		}
	}
	return true
}

// TestRunRouteArgs covers the CLI surface: the verbs runRoute accepts and the
// selectors it refuses rather than silently ignores. Ignoring a selector would
// be worse than refusing it — the parity harness drives `ip` and `goip` with
// the same argv, so a dropped filter has the two tools answering different
// questions while the comparator reports a stdout divergence it cannot explain.
//
// go test ./internal/goip/ -run TestRunRouteArgs
func TestRunRouteArgs(t *testing.T) {
	const firstLine = "192.0.2.0/24 dev goip0 "

	tests := []struct {
		description      string
		pcap             string
		args             []string
		wantCode         int
		wantStdoutPrefix string
		wantStderrSubstr string
	}{
		{
			description:      "positive: `route show` renders the first captured route",
			pcap:             routeDumpPcap,
			args:             []string{"route", "show"},
			wantCode:         ExitOK,
			wantStdoutPrefix: firstLine,
		},
		{
			// `r` reaches route rather than rule only because route precedes
			// rule in the object table; see dispatch.go's comment. The row is
			// here because deleting the unimplemented `rule` entry would break
			// it silently.
			description:      "positive: `r` abbreviates to route, not rule",
			pcap:             routeDumpPcap,
			args:             []string{"r", "s"},
			wantCode:         ExitOK,
			wantStdoutPrefix: firstLine,
		},
		{
			// do_iproute tests the list group before "save", so a bare `s` is
			// show. `sa` is NOT show — it is save — and is covered below.
			description:      "boundary: `route l` and `route lst` are both the same listing",
			pcap:             routeDumpPcap,
			args:             []string{"route", "lst"},
			wantCode:         ExitOK,
			wantStdoutPrefix: firstLine,
		},
		{
			description:      "boundary: `route ls` matches lst, the third spelling of list",
			pcap:             routeDumpPcap,
			args:             []string{"route", "ls"},
			wantCode:         ExitOK,
			wantStdoutPrefix: firstLine,
		},
		{
			// "sa" is not a prefix of "show", so matches() sends it to save —
			// which goip does not implement. Accepting it as a show would make
			// goip answer a question `ip` would answer differently.
			description:      "negative: `route sa` is save, not show, and is refused",
			pcap:             routeDumpPcap,
			args:             []string{"route", "sa"},
			wantCode:         ExitUsage,
			wantStderrSubstr: "not implemented",
		},
		{
			// A write verb must stay unreachable: goip is read-only, and
			// `flush` reaching a handler at all would be the wrong kind of bug.
			description:      "negative: the write verb `flush` is refused",
			pcap:             routeDumpPcap,
			args:             []string{"route", "flush"},
			wantCode:         ExitUsage,
			wantStderrSubstr: "not implemented",
		},
		{
			// The `dev goip0` token is GONE from the first line, which is the
			// whole visible effect of the selector: print_route's guard is
			// `if (tb[RTA_OIF] && filter.oifmask != -1)` (ip/iproute.c:900),
			// so naming the device removes it from the output.
			description:      "positive: `route show dev goip0` suppresses the dev token it filtered on",
			pcap:             routeDumpPcap,
			args:             []string{"route", "show", "dev", "goip0"},
			wantCode:         ExitOK,
			wantStdoutPrefix: "192.0.2.0/24 proto kernel scope link src 192.0.2.1 \n",
		},
		{
			description:      "positive: `route show oif goip0` is the same command under the other spelling",
			pcap:             routeDumpPcap,
			args:             []string{"route", "show", "oif", "goip0"},
			wantCode:         ExitOK,
			wantStdoutPrefix: "192.0.2.0/24 proto kernel scope link src 192.0.2.1 \n",
		},
		{
			// strcmp, not matches() (ip/iproute.c:1911): `d` is not an
			// abbreviation of `dev`, it is an unrecognized token that `ip`
			// reads as a destination prefix. Accepting it would have goip
			// answering a question `ip` refuses.
			description:      "negative: `route show d goip0` is not an abbreviation of dev",
			pcap:             routeDumpPcap,
			args:             []string{"route", "show", "d", "goip0"},
			wantCode:         ExitUsage,
			wantStderrSubstr: "not implemented",
		},
		{
			description:      "negative: `route show dev` with no name is a usage error",
			pcap:             routeDumpPcap,
			args:             []string{"route", "show", "dev"},
			wantCode:         ExitUsage,
			wantStderrSubstr: "argument expected",
		},
		{
			// The name must resolve. `ip` exits 1 with `Cannot find device`;
			// goip's resolution get comes back with nothing to match.
			description:      "negative: a device that does not exist is an error, not an empty listing",
			pcap:             routeDumpPcap,
			args:             []string{"route", "show", "dev", "nosuchdev0"},
			wantCode:         ExitFailure,
			wantStderrSubstr: "nosuchdev0",
		},
		{
			description:      "negative: `route show proto kernel` is refused, not ignored",
			pcap:             routeDumpPcap,
			args:             []string{"route", "show", "proto", "kernel"},
			wantCode:         ExitUsage,
			wantStderrSubstr: "not implemented",
		},
		{
			// A selector with no verb is an error in `ip` too: do_iproute
			// matches "table" against no verb and exits. goip collapses that
			// into ErrNotImplemented, which is the outcome the harness acts on.
			description:      "corner: `route table all` with no verb is refused, as it is in ip",
			pcap:             routeDumpAllPcap,
			args:             []string{"route", "table", "all"},
			wantCode:         ExitUsage,
			wantStderrSubstr: "not implemented",
		},
		{
			// NEXT_ARG() exits with a usage error when the keyword is last.
			description:      "negative: `route show table` with no id is a usage error",
			pcap:             routeDumpPcap,
			args:             []string{"route", "show", "table"},
			wantCode:         ExitUsage,
			wantStderrSubstr: "argument expected",
		},
		{
			// Options precede the object in iproute2, so this is a selector
			// named "-4", not a family option.
			description:      "corner: an option after the object is not an option",
			pcap:             routeDumpPcap,
			args:             []string{"route", "show", "-4"},
			wantCode:         ExitUsage,
			wantStderrSubstr: "not implemented",
		},
	}

	for _, tc := range tests {
		t.Run(tc.description, func(t *testing.T) {
			t.Setenv("GOIP_REPLAY", tc.pcap)
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

// TestParseRouteShowArgs pins filter.tb, which is not cosmetic: it decides both
// the RTA_TABLE on the wire and whether every rendered line carries a `table`
// token.
//
// go test ./internal/goip/ -run TestParseRouteShowArgs
func TestParseRouteShowArgs(t *testing.T) {
	tests := []struct {
		description string
		args        []string
		want        routeSelectors
		wantErr     bool
		// wantErrIs, when set, is the sentinel errors.Is must find. Asserting
		// only that "an error happened" would let a refused keyword and a
		// malformed id collapse into one another, and they are different
		// mistakes: routeTableID wraps strconv's ErrSyntax and ErrRange
		// precisely so a caller can tell `table wombat` from `table 2^32`.
		wantErrIs error
	}{
		{
			// ip/iproute.c:1835 assigns RT_TABLE_MAIN before reading any
			// argument, which is why no line of ip_route_main has a table token.
			description: "positive: no selectors leaves filter.tb at RT_TABLE_MAIN",
			args:        nil,
			want:        routeSelectors{Table: unix.RT_TABLE_MAIN},
		},
		{
			description: "positive: `table main` is the same as the default",
			args:        []string{"table", "main"},
			want:        routeSelectors{Table: unix.RT_TABLE_MAIN},
		},
		{
			// "all" is not a table name: rtnl_rttable_a2n fails on it and the
			// fallback at ip/iproute.c:1850 sets filter.tb to 0.
			description: "positive: `table all` clears the filter to 0",
			args:        []string{"table", "all"},
			want:        routeSelectors{Table: unix.RT_TABLE_UNSPEC},
		},
		{
			// The two spellings reach 0 by different routes — "0" parses as an
			// id, "all" does not — and must land in the same place.
			description: "boundary: `table 0` is spelled differently but means `table all`",
			args:        []string{"table", "0"},
			want:        routeSelectors{Table: unix.RT_TABLE_UNSPEC},
		},
		{
			description: "positive: `table local` resolves through the built-in rt_tables hash",
			args:        []string{"table", "local"},
			want:        routeSelectors{Table: unix.RT_TABLE_LOCAL},
		},
		{
			description: "positive: `table default` is the third and last built-in name",
			args:        []string{"table", "default"},
			want:        routeSelectors{Table: unix.RT_TABLE_DEFAULT},
		},
		{
			description: "boundary: table 255 is the largest id that fits the 8-bit rtm_table field",
			args:        []string{"table", "255"},
			want:        routeSelectors{Table: 255},
		},
		{
			// Beyond 255 the id no longer fits rtm_table, which is exactly why
			// the request carries RTA_TABLE. See BuildDumpRouteRequestTable.
			description: "boundary: table 256 is the first id that needs RTA_TABLE to be expressed",
			args:        []string{"table", "256"},
			want:        routeSelectors{Table: 256},
		},
		{
			description: "corner: RT_TABLE_MAX, 4294967295, is a legal table id",
			args:        []string{"table", "4294967295"},
			want:        routeSelectors{Table: 0xFFFFFFFF},
		},
		{
			// strtoul(arg, &end, 0) — base 0, so the 0x form is accepted.
			description: "corner: a 0x-prefixed id is accepted, because strtoul uses base 0",
			args:        []string{"table", "0xff"},
			want:        routeSelectors{Table: 255},
		},
		{
			// iproute2 simply assigns filter.tb again (ip/iproute.c:1843-1858),
			// so repeating the selector is last-wins — not an error and not an
			// intersection. Worth pinning rather than leaving unspecified.
			description: "corner: a repeated `table` selector is last-wins",
			args:        []string{"table", "all", "table", "main"},
			want:        routeSelectors{Table: unix.RT_TABLE_MAIN},
		},
		{
			description: "corner: last-wins in the other direction too",
			args:        []string{"table", "main", "table", "all"},
			want:        routeSelectors{Table: unix.RT_TABLE_UNSPEC},
		},
		{
			description: "negative: `table` with no id is a usage error",
			args:        []string{"table"},
			wantErr:     true,
			wantErrIs:   ErrNotImplemented,
		},
		{
			// `route show cache` is a real iproute2 form that lists cloned
			// routes. goip has none, so it is refused by name rather than being
			// read as a malformed id.
			description: "negative: `table cache` is refused by name, not misread as an id",
			args:        []string{"table", "cache"},
			wantErr:     true,
			wantErrIs:   ErrNotImplemented,
		},
		{
			description: "negative: `table help` is refused by name",
			args:        []string{"table", "help"},
			wantErr:     true,
			wantErrIs:   ErrNotImplemented,
		},
		{
			description: "negative: a table name with no built-in entry is an error, since no rt_tables file is read",
			args:        []string{"table", "mgmt"},
			wantErr:     true,
			wantErrIs:   strconv.ErrSyntax,
		},
		{
			description: "negative: an id past 32 bits does not fit rtm_table or RTA_TABLE",
			args:        []string{"table", "4294967296"},
			wantErr:     true,
			wantErrIs:   strconv.ErrRange,
		},
		{
			description: "negative: an unimplemented selector is refused rather than skipped",
			args:        []string{"via", "192.0.2.254"},
			wantErr:     true,
			wantErrIs:   ErrNotImplemented,
		},
		{
			// `tab`, `tabl` and `t` all reach "table" through matches().
			description: "boundary: the table keyword abbreviates, as every iproute2 keyword does",
			args:        []string{"t", "local"},
			want:        routeSelectors{Table: unix.RT_TABLE_LOCAL},
		},
		{
			// ip/iproute.c:1911. The table stays at its default, which is
			// what makes `route show dev NAME` a two-attribute request.
			description: "positive: `dev NAME` sets the device and leaves the table alone",
			args:        []string{"dev", "goip0"},
			want:        routeSelectors{Table: unix.RT_TABLE_MAIN, Dev: "goip0", DevSet: true},
		},
		{
			// :1912, the same else-if arm. Not a near-synonym — the two
			// spellings assign the same local and are indistinguishable
			// downstream.
			description: "positive: `oif NAME` is an exact synonym for `dev NAME`",
			args:        []string{"oif", "goip0"},
			want:        routeSelectors{Table: unix.RT_TABLE_MAIN, Dev: "goip0", DevSet: true},
		},
		{
			description: "positive: `table all dev NAME` composes the two selectors",
			args:        []string{"table", "all", "dev", "goip0"},
			want:        routeSelectors{Table: unix.RT_TABLE_UNSPEC, Dev: "goip0", DevSet: true},
		},
		{
			description: "positive: the two selectors compose in either order",
			args:        []string{"dev", "goip0", "table", "all"},
			want:        routeSelectors{Table: unix.RT_TABLE_UNSPEC, Dev: "goip0", DevSet: true},
		},
		{
			// One local, `od`, assigned twice — so this is last-wins and not
			// two filters, exactly as a repeated `table` is.
			description: "corner: `dev` and `oif` share one slot, so mixing them is last-wins",
			args:        []string{"dev", "goip0", "oif", "lo"},
			want:        routeSelectors{Table: unix.RT_TABLE_MAIN, Dev: "lo", DevSet: true},
		},
		{
			// strcmp, not matches() (ip/iproute.c:1911). An abbreviation
			// falls through to the else-arm at :2158 and is read as a
			// DESTINATION PREFIX, so accepting it here would make goip answer
			// a question `ip` refuses.
			description: "negative: `dev` does not abbreviate, unlike `table`",
			args:        []string{"d", "goip0"},
			wantErr:     true,
			wantErrIs:   ErrNotImplemented,
		},
		{
			description: "negative: `oif` does not abbreviate either",
			args:        []string{"o", "goip0"},
			wantErr:     true,
			wantErrIs:   ErrNotImplemented,
		},
		{
			// NEXT_ARG() with nothing left.
			description: "negative: `dev` with no name is a usage error",
			args:        []string{"dev"},
			wantErr:     true,
			wantErrIs:   ErrNotImplemented,
		},
		{
			description: "negative: `oif` with no name is a usage error",
			args:        []string{"oif"},
			wantErr:     true,
			wantErrIs:   ErrNotImplemented,
		},
		{
			// The keyword consumes the next token unconditionally, so a
			// second keyword after `dev` is a device NAME and the real
			// selector is gone. `ip` does the same and then fails in
			// ll_name_to_index; goip fails one step later, when the
			// resolution get comes back empty. Either way `table` here is
			// not a table.
			description: "corner: `dev table` takes `table` as the device name",
			args:        []string{"dev", "table"},
			want:        routeSelectors{Table: unix.RT_TABLE_MAIN, Dev: "table", DevSet: true},
		},
		{
			// DevSet is why this row can exist at all: `ip route show dev ""`
			// is a device selector that names nothing, and it must not
			// collapse into the no-selector case. `ip` reaches
			// ll_name_to_index, fails all three lookups and exits with
			// `Cannot find device ""`; goip sends the resolution get and
			// fails on its reply.
			description: "corner: an empty device name is still a device selector",
			args:        []string{"dev", ""},
			want:        routeSelectors{Table: unix.RT_TABLE_MAIN, Dev: "", DevSet: true},
		},
	}

	for _, tc := range tests {
		t.Run(tc.description, func(t *testing.T) {
			got, err := parseRouteShowArgs(tc.args)
			if tc.wantErr {
				if err == nil {
					t.Fatalf("parseRouteShowArgs(%q) = %+v, want an error", tc.args, got)
				}
				if tc.wantErrIs != nil && !errors.Is(err, tc.wantErrIs) {
					t.Fatalf("parseRouteShowArgs(%q) error = %v, want errors.Is(_, %v)",
						tc.args, err, tc.wantErrIs)
				}
				return
			}
			if err != nil {
				t.Fatalf("parseRouteShowArgs(%q): %v", tc.args, err)
			}
			if got != tc.want {
				t.Errorf("parseRouteShowArgs(%q) = %+v, want %+v", tc.args, got, tc.want)
			}
		})
	}
}

// withOif returns a copy of r carrying RTA_OIF. A copy rather than a mutation
// because the base routes in TestRouteFilter are shared across rows, and a row
// that edited one in place would make every later row depend on its position
// in the table.
func withOif(r model.Route, oif uint32) model.Route {
	r.Oif = oif
	return r
}

// withNexthops returns a copy of r carrying an RTA_MULTIPATH with one entry
// per index. Only Ifindex is set: filter_multipath reads nothing else
// (ip/iproute.c:158-174).
func withNexthops(r model.Route, indexes ...int32) model.Route {
	r.Multipath = make([]xtcpnl.RouteNextHop, len(indexes))
	for i, idx := range indexes {
		r.Multipath[i] = xtcpnl.RouteNextHop{Ifindex: idx}
	}
	return r
}

// TestRouteFilter covers the four rules of filter_nlmsg that a `show` with no
// selector other than `table` and `dev` still applies. They are not redundant
// with the request: the kernel answers a filtered dump on a best-effort basis
// and `ip` re-checks every reply.
//
// go test ./internal/goip/ -run TestRouteFilter
func TestRouteFilter(t *testing.T) {
	v4Main := model.Route{Family: unix.AF_INET, Table: unix.RT_TABLE_MAIN, Type: unix.RTN_UNICAST, DstLen: 24}
	v4Local := model.Route{Family: unix.AF_INET, Table: unix.RT_TABLE_LOCAL, Type: unix.RTN_LOCAL, DstLen: 32}
	v6Main := model.Route{Family: unix.AF_INET6, Table: unix.RT_TABLE_MAIN, Type: unix.RTN_UNICAST, DstLen: 64}
	v6Local := model.Route{Family: unix.AF_INET6, Table: unix.RT_TABLE_LOCAL, Type: unix.RTN_LOCAL, DstLen: 128}

	tests := []struct {
		description     string
		in              []model.Route
		preferredFamily uint8
		table           uint32
		oif             uint32
		wantLen         int
	}{
		{
			description:     "positive: `route show` keeps the main-table v4 routes",
			in:              []model.Route{v4Main, v4Main},
			preferredFamily: unix.AF_UNSPEC,
			table:           unix.RT_TABLE_MAIN,
			wantLen:         2,
		},
		{
			// The client-side family filter is preferred_family — the
			// UNPROMOTED one — so a plain `route show` (AF_UNSPEC) drops
			// nothing here even though its request asked for AF_INET.
			description:     "boundary: an AF_UNSPEC preferred family drops nothing, whatever the request asked for",
			in:              []model.Route{v4Main, v6Main},
			preferredFamily: unix.AF_UNSPEC,
			table:           unix.RT_TABLE_UNSPEC,
			wantLen:         2,
		},
		{
			description:     "positive: `-6` drops every v4 reply",
			in:              []model.Route{v4Main, v6Main, v4Main},
			preferredFamily: unix.AF_INET6,
			table:           unix.RT_TABLE_MAIN,
			wantLen:         1,
		},
		{
			description:     "positive: `-4` drops every v6 reply",
			in:              []model.Route{v4Main, v6Main},
			preferredFamily: unix.AF_INET,
			table:           unix.RT_TABLE_MAIN,
			wantLen:         1,
		},
		{
			// `filter.cloned == !(rtm_flags & RTM_F_CLONED)` reads backwards:
			// with filter.cloned zero it is TRUE exactly when the route IS
			// cloned, so a cloned route is dropped. `route show cache` is the
			// inverse, and goip refuses it.
			description: "negative: a cloned route is dropped, which is the inverse of `route show cache`",
			in: []model.Route{
				v4Main,
				{Family: unix.AF_INET, Table: unix.RT_TABLE_MAIN, Type: unix.RTN_UNICAST, Flags: unix.RTM_F_CLONED},
			},
			preferredFamily: unix.AF_UNSPEC,
			table:           unix.RT_TABLE_MAIN,
			wantLen:         1,
		},
		{
			description:     "positive: a table filter drops the routes of every other table",
			in:              []model.Route{v4Main, v4Local},
			preferredFamily: unix.AF_UNSPEC,
			table:           unix.RT_TABLE_MAIN,
			wantLen:         1,
		},
		{
			description:     "boundary: `table all` keeps both tables and both families",
			in:              []model.Route{v4Main, v4Local, v6Main, v6Local},
			preferredFamily: unix.AF_UNSPEC,
			table:           unix.RT_TABLE_UNSPEC,
			wantLen:         4,
		},
		{
			// Before ip6_multiple_tables latches, `ip` emulates the v6 tables
			// by route TYPE, because on a kernel without
			// CONFIG_IPV6_MULTIPLE_TABLES they do not exist: `table main`
			// means "not RTN_LOCAL" and `table local` means "RTN_LOCAL".
			// v6Local is RTN_LOCAL, so a v6 `table main` drops it.
			description:     "corner: before the latch, a v6 `table main` filter is emulated by route type",
			in:              []model.Route{v6Local},
			preferredFamily: unix.AF_INET6,
			table:           unix.RT_TABLE_MAIN,
			wantLen:         0,
		},
		{
			description:     "corner: before the latch, a v6 `table local` filter keeps only RTN_LOCAL",
			in:              []model.Route{v6Main, v6Local},
			preferredFamily: unix.AF_INET6,
			table:           unix.RT_TABLE_LOCAL,
			wantLen:         1,
		},
		{
			// The latch is set by the FIRST v6 route whose table is not main,
			// and it changes how every route AFTER it is filtered — so the same
			// two routes in the other order give a different answer. The
			// ordering dependence is real and is reproduced rather than tidied.
			description: "corner: the ip6_multiple_tables latch makes the filter order-dependent",
			in: []model.Route{
				v6Local, // latches, then is itself filtered by type
				v6Main,  // filtered by table id, and its table IS main
			},
			preferredFamily: unix.AF_INET6,
			table:           unix.RT_TABLE_MAIN,
			wantLen:         1,
		},
		{
			// Without CONFIG_IPV6_MULTIPLE_TABLES there is no table to emulate
			// beyond main and local, so `ip` refuses every other id outright
			// and the listing comes back empty rather than unfiltered.
			description:     "negative: before the latch, a v6 filter on any other table drops everything",
			in:              []model.Route{v6Main, v6Local},
			preferredFamily: unix.AF_INET6,
			table:           100,
			wantLen:         0,
		},
		{
			description:     "boundary: an empty dump filters to an empty listing, not to an error",
			in:              nil,
			preferredFamily: unix.AF_UNSPEC,
			table:           unix.RT_TABLE_MAIN,
			wantLen:         0,
		},
		{
			// ip/iproute.c:331-336. The kernel applies RTA_OIF on the dump
			// side too, so on a strict socket these replies would not arrive;
			// the client-side arm is the belt to that braces, and it is what
			// a render test feeding a pcap directly relies on.
			description: "positive: an oif filter keeps only the routes out of that device",
			in: []model.Route{
				withOif(v4Main, 3), withOif(v4Main, 2), withOif(v4Main, 3),
			},
			preferredFamily: unix.AF_UNSPEC,
			table:           unix.RT_TABLE_MAIN,
			oif:             3,
			wantLen:         2,
		},
		{
			description: "negative: an oif filter that matches nothing yields an empty listing",
			in: []model.Route{
				withOif(v4Main, 2), withOif(v4Main, 4),
			},
			preferredFamily: unix.AF_UNSPEC,
			table:           unix.RT_TABLE_MAIN,
			oif:             3,
			wantLen:         0,
		},
		{
			// filter_multipath (ip/iproute.c:158-174): ANY nexthop matching
			// keeps the whole route, and the route is then printed with every
			// one of its nexthops, including the ones on other devices.
			description: "positive: a multipath route survives if any one nexthop is on the device",
			in: []model.Route{
				withNexthops(v4Main, 2, 3),
				withNexthops(v4Main, 2, 4),
			},
			preferredFamily: unix.AF_UNSPEC,
			table:           unix.RT_TABLE_MAIN,
			oif:             3,
			wantLen:         1,
		},
		{
			// The `else if` is the trap. A route with NEITHER RTA_OIF nor
			// RTA_MULTIPATH matches no arm of the C and therefore SURVIVES —
			// `ip route show dev goip0` lists every blackhole route on the
			// host. Not a bug to tidy away: on a strict socket the kernel
			// never sends these, so the fall-through is unreachable in
			// practice and observable only here.
			description: "corner: a route with neither RTA_OIF nor RTA_MULTIPATH is NOT dropped",
			in: []model.Route{
				{Family: unix.AF_INET, Table: unix.RT_TABLE_MAIN, Type: unix.RTN_BLACKHOLE, DstLen: 32},
			},
			preferredFamily: unix.AF_UNSPEC,
			table:           unix.RT_TABLE_MAIN,
			oif:             3,
			wantLen:         1,
		},
		{
			// RTA_OIF wins over RTA_MULTIPATH when both are somehow present,
			// because the C is if/else-if and not two independent tests. A
			// kernel that sent both would have the nexthops ignored.
			description: "corner: RTA_OIF short-circuits the multipath arm rather than being ORed with it",
			in: []model.Route{
				withNexthops(withOif(v4Main, 2), 3),
			},
			preferredFamily: unix.AF_UNSPEC,
			table:           unix.RT_TABLE_MAIN,
			oif:             3,
			wantLen:         0,
		},
		{
			// oif 0 is goip's spelling of `filter.oifmask == 0`, so the whole
			// arm is skipped and even a route with no device survives.
			description:     "boundary: oif 0 is not a filter",
			in:              []model.Route{withOif(v4Main, 2), v4Main},
			preferredFamily: unix.AF_UNSPEC,
			table:           unix.RT_TABLE_MAIN,
			oif:             0,
			wantLen:         2,
		},
	}

	for _, tc := range tests {
		t.Run(tc.description, func(t *testing.T) {
			got := routeFilter(tc.in, tc.preferredFamily, tc.table, tc.oif)
			if len(got) != tc.wantLen {
				t.Errorf("routeFilter() kept %d routes, want %d", len(got), tc.wantLen)
			}
		})
	}
}

// TestRouteShowFamilyPromotion pins ip/iproute.c:2000-2001, which is easy to
// mistake for the -4/-6 option and is not: it changes only what the REQUEST
// asks for, and only when a table filter is in force.
//
// The observable consequence is on the wire, so the assertion is on the request
// the source was handed rather than on stdout.
//
// go test ./internal/goip/ -run TestRouteShowFamilyPromotion
func TestRouteShowFamilyPromotion(t *testing.T) {
	tests := []struct {
		description string
		pcap        string
		family      uint8
		args        []string
		wantFamily  uint8
	}{
		{
			// AF_UNSPEC with filter.tb set is promoted to AF_INET, which is why
			// plain `ip route show` lists only IPv4 without ever being told to.
			description: "positive: a default `route show` promotes AF_UNSPEC to AF_INET in the request",
			pcap:        routeDumpPcap,
			family:      unix.AF_UNSPEC,
			args:        []string{"show"},
			wantFamily:  unix.AF_INET,
		},
		{
			// `table all` clears filter.tb, so the promotion's second condition
			// fails and the request stays AF_UNSPEC — which is how one command
			// returns both families.
			description: "boundary: `table all` clears filter.tb, so no promotion happens",
			pcap:        routeDumpAllPcap,
			family:      unix.AF_UNSPEC,
			args:        []string{"show", "table", "all"},
			wantFamily:  unix.AF_UNSPEC,
		},
		{
			// An explicit family is never promoted: the first condition is
			// `dump_family == AF_UNSPEC`.
			description: "negative: an explicit -6 is not promoted",
			pcap:        routeDump6Pcap,
			family:      unix.AF_INET6,
			args:        []string{"show"},
			wantFamily:  unix.AF_INET6,
		},
		{
			description: "corner: an explicit -4 with `table all` also stays as it was",
			pcap:        routeDumpAllPcap,
			family:      unix.AF_INET,
			args:        []string{"show", "table", "all"},
			wantFamily:  unix.AF_INET,
		},
	}

	for _, tc := range tests {
		t.Run(tc.description, func(t *testing.T) {
			src := &captureFirstRequest{inner: newRouteReplay(t, tc.pcap)}
			if _, err := runRouteWith(t, src, tc.family, tc.args); err != nil {
				t.Fatalf("runRoute(%q): %v", tc.args, err)
			}
			if src.first == nil {
				t.Fatal("no request was sent")
			}
			// rtm_family is the first byte of the rtmsg, which follows the
			// 16-byte nlmsghdr.
			var rtm xtcpnl.RtMsg
			if _, err := xtcpnl.DeserializeRtMsg(src.first[xtcpnl.NlMsgHdrSizeCst:], &rtm); err != nil {
				t.Fatalf("DeserializeRtMsg: %v", err)
			}
			if rtm.Family != tc.wantFamily {
				t.Errorf("request rtm_family = %d, want %d", rtm.Family, tc.wantFamily)
			}
		})
	}
}

// captureFirstRequest keeps the bytes of the first Dump request, which is the
// RTM_GETROUTE one — the lazy single-gets go through Talk.
type captureFirstRequest struct {
	inner *routeReplay
	first []byte
}

func (s *captureFirstRequest) Dump(request []byte, msgType uint16) ([][]byte, error) {
	if s.first == nil {
		s.first = xtcpnl.CopyBytes(request)
	}
	return s.inner.Dump(request, msgType)
}

func (s *captureFirstRequest) Talk(request []byte, msgType uint16) ([]byte, error) {
	return s.inner.Talk(request, msgType)
}
