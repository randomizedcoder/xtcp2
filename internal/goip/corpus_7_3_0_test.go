package goip

import (
	"bytes"
	"os"
	"strings"
	"testing"

	"github.com/randomizedcoder/xtcp2/internal/goip/render"
	"github.com/randomizedcoder/xtcp2/internal/goip/req"
	"golang.org/x/sys/unix"
)

// corpus730Dir is the dump corpus captured at the iproute2 7.2.0 pin, on the
// 7.3.0 kernel that came with it (`nix run
// .#microvm-x86_64-netlink-dump-capture`): the same driver and topology as
// 7_1_4, rendered by the release goip's defaults follow.
const corpus730Dir = "../../pkg/xtcpnl/testdata/7_3_0/dumps/"

// corpus730Namespaces are the driver's three namespaces and the device each
// one's `dev $dev` rows name (capture-netlink-dumps.exp, capture_set calls).
var corpus730Namespaces = []struct{ subdir, dev string }{
	{"", devNameCst},
	{"mesh/", "veth0"},
	{"tunnel/", "gre1"},
}

// TestCorpus730MatchesSidecarsAt720 replays the 7.3.0 corpus with goip at
// its 7.2.0 defaults and compares each command with the `ip` output the
// driver saved from the same invocation (capio), in every namespace.
//
// The rest of this package runs as 7.1.0 against 7_1_4 (TestMain). This is
// the one place goip's current defaults render what `ip` 7.2.0 was sent on a
// 7.3 kernel, including the NAME_ONLY link replies with no IFLA_TXQLEN that
// cost `ip -6 addr` its `qlen 1000` between the two corpora.
//
// What it cannot see is the knobs themselves: replay ignores request bytes,
// and no link in this corpus has a txqlen of 0, so every row passes under
// either generation. req.TestTierALLRequests730 pins the 0x109 requests
// against this corpus, and render's own tests pin `qlen 0`.
//
// It is sequential on purpose: the knobs are package state, and Go runs
// every parallel top-level test only after the sequential ones return.
//
// Left out: `-s ntable show`, whose dates need the capture instant (the
// 7.1.0 test covers the renderer; nothing in it moved at 7.2.0), and the
// `side` sidecars, which come from a second `ip` run.
//
// go test ./internal/goip/ -run TestCorpus730MatchesSidecarsAt720
func TestCorpus730MatchesSidecarsAt720(t *testing.T) {
	prevNameOnly, prevQlen := req.LLMapNameOnly, render.RenderQlenZero
	req.LLMapNameOnly, render.RenderQlenZero = true, true
	t.Cleanup(func() { req.LLMapNameOnly, render.RenderQlenZero = prevNameOnly, prevQlen })

	type row struct {
		category, description, expected string
		args                            []string // `dev` is appended with the namespace's device when withDev
		withDev                         bool
		pcap, sidecar                   string
		// sameLines compares as a multiset: the neighbor dump's order is the
		// kernel's hash walk, and ip's sidecar order is not reproduced.
		sameLines bool
		// devStats runs `-s addr show dev` through linkReplay, which pairs
		// each by-name and by-index RTM_GETLINK with its own reply
		// (TestAddrShowDevStatsMatchesCapturedSidecars has the why).
		devStats bool
		// cleanOnly marks a row naming an object only the clean namespace
		// builds (nexthop ids 10/20, goip0, goipvrf). Elsewhere `ip` failed
		// and saved an empty sidecar, and goip must refuse too.
		cleanOnly bool
	}
	base := []row{
		{category: "positive", description: "-s addr show", expected: "byte-identical",
			args: []string{"-s", "addr", "show"}, pcap: "netlink_route_getaddr_stats", sidecar: "ip_addr_stats"},
		{category: "boundary", description: "-s -6 addr show, whose link lines lost qlen with the NAME_ONLY replies", expected: "byte-identical, no qlen",
			args: []string{"-s", "-6", "addr", "show"}, pcap: "netlink_route_getaddr_v6_stats", sidecar: "ip_addr_v6_stats"},
		{category: "positive", description: "-s addr show dev", expected: "byte-identical across the paired single-gets",
			args: []string{"show"}, withDev: true, pcap: "netlink_route_getaddr_dev_stats", sidecar: "ip_addr_dev_stats", devStats: true},
		{category: "positive", description: "-d route show", expected: "byte-identical",
			args: []string{"-d", "route", "show"}, pcap: "netlink_route_getroute_detail", sidecar: "ip_route_main_n"},
		{category: "positive", description: "-d route show table all", expected: "byte-identical",
			args: []string{"-d", "route", "show", "table", "all"}, pcap: "netlink_route_getroute_table_all_detail", sidecar: "ip_route_table_all_n"},
		{category: "positive", description: "nexthop show", expected: "byte-identical",
			args: []string{"nexthop", "show"}, pcap: "netlink_route_getnexthop", sidecar: "ip_nexthop"},
		{category: "corner", description: "nexthop show id 10", expected: "byte-identical, or the same empty answer",
			args: []string{"nexthop", "show", "id", "10"}, pcap: "netlink_route_getnexthop_id", sidecar: "ip_nexthop_id", cleanOnly: true},
		{category: "corner", description: "nexthop show id 20, a resilient group", expected: "byte-identical",
			args: []string{"nexthop", "show", "id", "20"}, pcap: "netlink_route_getnexthop_res", sidecar: "ip_nexthop_res", cleanOnly: true},
		{category: "positive", description: "nexthop show dev goip0, resolved by ll_link_get", expected: "byte-identical",
			args: []string{"nexthop", "show", "dev", "goip0"}, pcap: "netlink_route_getnexthop_dev", sidecar: "ip_nexthop_dev", cleanOnly: true},
		{category: "positive", description: "nexthop show master goipvrf", expected: "byte-identical",
			args: []string{"nexthop", "show", "master", "goipvrf"}, pcap: "netlink_route_getnexthop_master", sidecar: "ip_nexthop_master", cleanOnly: true},
		{category: "positive", description: "nexthop show vrf goipvrf, whose name_is_vrf get carries no NAME_ONLY", expected: "byte-identical",
			args: []string{"nexthop", "show", "vrf", "goipvrf"}, pcap: "netlink_route_getnexthop_vrf", sidecar: "ip_nexthop_vrf", cleanOnly: true},
		{category: "positive", description: "nexthop show groups", expected: "byte-identical",
			args: []string{"nexthop", "show", "groups"}, pcap: "netlink_route_getnexthop_groups", sidecar: "ip_nexthop_groups"},
		{category: "positive", description: "nexthop show fdb", expected: "byte-identical",
			args: []string{"nexthop", "show", "fdb"}, pcap: "netlink_route_getnexthop_fdb", sidecar: "ip_nexthop_fdb"},
		{category: "positive", description: "addrlabel show", expected: "byte-identical",
			args: []string{"addrlabel", "show"}, pcap: "netlink_route_getaddrlabel", sidecar: "ip_addrlabel"},
		{category: "positive", description: "ntable show", expected: "byte-identical",
			args: []string{"ntable", "show"}, pcap: "netlink_route_getneightbl", sidecar: "ip_ntable"},
		{category: "positive", description: "netconf show", expected: "byte-identical",
			args: []string{"netconf", "show"}, pcap: "netlink_route_getnetconf", sidecar: "ip_netconf"},
		{category: "positive", description: "netconf show dev", expected: "byte-identical",
			args: []string{"netconf", "show", "dev"}, withDev: true, pcap: "netlink_route_getnetconf_dev", sidecar: "ip_netconf_dev"},
		{category: "corner", description: "-4 netconf show dev", expected: "byte-identical",
			args: []string{"-4", "netconf", "show", "dev"}, withDev: true, pcap: "netlink_route_getnetconf_dev4", sidecar: "ip_netconf_dev4"},
		{category: "positive", description: "stats show group link", expected: "byte-identical",
			args: []string{"stats", "show", "group", "link"}, pcap: "netlink_route_getstats", sidecar: "ip_stats"},
		{category: "positive", description: "stats show group link dev", expected: "byte-identical",
			args: []string{"stats", "show", "group", "link", "dev"}, withDev: true, pcap: "netlink_route_getstats_dev", sidecar: "ip_stats_dev"},
		{category: "positive", description: "stats show group xstats", expected: "byte-identical",
			args: []string{"stats", "show", "group", "xstats"}, pcap: "netlink_route_getstats_xstats", sidecar: "ip_stats_xstats"},
		{category: "positive", description: "vrf show", expected: "byte-identical",
			args: []string{"vrf", "show"}, pcap: "netlink_route_getvrf", sidecar: "ip_vrf"},
		{category: "corner", description: "-s neigh show", expected: "the same lines, in the dump's order",
			args: []string{"-s", "neigh", "show"}, pcap: "netlink_route_getneigh_stats", sidecar: "ip_neigh_stats", sameLines: true},
	}

	for _, ns := range corpus730Namespaces {
		for _, r := range base {
			args := append([]string(nil), r.args...)
			if r.withDev {
				args = append(args, ns.dev)
			}
			name := strings.TrimSuffix(ns.subdir, "/")
			if name == "" {
				name = "clean"
			}
			category, expected := r.category, r.expected
			refuse := r.cleanOnly && ns.subdir != ""
			if refuse {
				category, expected = "negative", "goip refuses as ip did: non-zero exit, nothing on stdout"
			}
			t.Run(name+"/"+r.description, func(t *testing.T) {
				t.Logf("%s: %s in the %s namespace; expected: %s", category, r.description, name, expected)
				pcap := corpus730Dir + ns.subdir + r.pcap + ".pcap"
				want, err := os.ReadFile(corpus730Dir + ns.subdir + r.sidecar)
				if err != nil {
					t.Fatal(err)
				}
				if r.devStats {
					got, err := runAddressStatsWith(t, newLinkReplay(t, pcap), unix.AF_UNSPEC, args)
					if err != nil {
						t.Fatalf("runAddress: %v", err)
					}
					assertLinesEqual(t, got, string(want))
					return
				}
				t.Setenv("GOIP_REPLAY", pcap)
				var stdout, stderr bytes.Buffer
				if refuse {
					if len(want) != 0 {
						t.Fatalf("%s is not empty, so ip did not fail here", r.sidecar)
					}
					if code := Run(args, &stdout, &stderr); code == ExitOK || stdout.Len() != 0 {
						t.Errorf("Run(%q) = %d with stdout %q, want a refusal and no output", args, code, stdout.String())
					}
					return
				}
				if code := Run(args, &stdout, &stderr); code != ExitOK {
					t.Fatalf("Run(%q) = %d, stderr=%s", args, code, stderr.String())
				}
				if r.sameLines {
					assertSameLines(t, stdout.String(), string(want))
					return
				}
				assertLinesEqual(t, stdout.String(), string(want))
			})
		}
	}
}
