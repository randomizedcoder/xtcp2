package main

import (
	"fmt"
	"slices"
	"strings"
	"testing"
)

// TestStatsFamily holds the stats rows to the probe batch (V022).
func TestStatsFamily(t *testing.T) {
	rows := statsFamily()
	if err := checkRows(rows); err != nil {
		t.Fatal(err)
	}
	byID := map[string]row{}
	for _, r := range rows {
		byID[r.ID] = r
	}
	cases := []struct {
		description string
		id          string
		expected    string // cmd, rc, refuser, then the mask, offload and dev axes
	}{
		{"positive: the bare show", "stats/show-clean-text", "ip stats show 0 none 31  "},
		{"positive: the bare show as JSON with stats", "stats/show-bond-json-stats", "ip -j -s stats show 0 none 31  "},
		{"positive: an offload subgroup narrows the sub-mask", "stats/l3-stats-text", "ip stats show group offload subgroup l3_stats 0 none 8 6 "},
		{"positive: a show of one device", "stats/l3-stats-dev-text", "ip stats show dev d0 group offload subgroup l3_stats 0 none 8 6 d0"},
		{"boundary: two subgroups make the full sub-mask, so no nest", "stats/two-subgroups-text",
			"ip stats show group offload subgroup cpu_hit subgroup l3_stats 0 none 8  "},
		{"corner: link afstats dev dumps, then filters", "stats/link-afstats-dev-text", "ip link afstats dev d0 0 none 16  "},
		{"positive: xstats_slave", "stats/xstats-bond-slave-text", "ip link xstats type bond_slave 0 none 4  "},
		{"negative: a subgroup with no group", "stats/err-orphan-subgroup", "ip stats show subgroup l3_stats 22 iproute2   "},
		{"negative: dev twice", "stats/err-dev-twice", "ip stats show dev d0 dev d0 255 iproute2   "},
		{"negative: a device that isn't there", "stats/err-nosuch-dev", "ip stats show dev nosuch 1 kernel   "},
		{"corner: help is rc 0", "stats/err-help", "ip stats help 0 none   "},
		{"negative: stats set x", "stats/set-x", "ip stats set dev d0 l3_stats x 22 iproute2   "},
	}
	for _, tc := range cases {
		t.Run(tc.description, func(t *testing.T) {
			r, ok := byID[tc.id]
			if !ok {
				t.Fatalf("no row %s", tc.id)
			}
			got := fmt.Sprintf("%s %d %s %s %s %s", r.Cmd, *r.Expect.RC, r.Expect.RefusedBy, r.Axes["mask"], r.Axes["offload"], r.Axes["dev"])
			if got != tc.expected {
				t.Errorf("got %q, want %q", got, tc.expected)
			}
		})
	}
	props := []struct {
		description string
		check       func(r row) bool
	}{
		{"positive: every row loads bonding and mpls_router", func(r row) bool { return slices.Equal(r.Modules, []string{"bonding", "mpls_router"}) }},
		{"positive: every setup is ip-only, run outside the capture", func(r row) bool {
			return len(r.Pre) == 1 && strings.HasPrefix(r.Pre[0].Cmd, "ip link add d0 type dummy")
		}},
		{"positive: a show comes in four forms", func(r row) bool {
			if r.Axes["mask"] == "" {
				return true
			}
			base := r.ID
			for _, s := range []string{"-json-stats", "-json", "-stats", "-text"} {
				if strings.HasSuffix(r.ID, s) {
					base = strings.TrimSuffix(r.ID, s)
					break
				}
			}
			n := 0
			for _, s := range []string{"-text", "-json", "-stats", "-json-stats"} {
				if _, ok := byID[base+s]; ok {
					n++
				}
			}
			return n == 4
		}},
		{"corner: l3_stats on is in the setup, not the capture", func(r row) bool {
			return !strings.HasPrefix(r.ID, "stats/show-l3-") || strings.HasSuffix(r.Pre[0].Cmd, "ip stats set dev d0 l3_stats on")
		}},
		{"negative: an error row reads no stats", func(r row) bool {
			return !strings.HasPrefix(r.ID, "stats/err-") || r.Axes == nil
		}},
	}
	for _, p := range props {
		t.Run(p.description, func(t *testing.T) {
			for _, r := range rows {
				if !p.check(r) {
					t.Errorf("%s: %s", r.ID, r.Cmd)
				}
			}
		})
	}
	if len(rows) != 101 {
		t.Errorf("%d rows, want 101", len(rows))
	}
}
