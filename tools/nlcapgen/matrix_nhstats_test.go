package main

import (
	"slices"
	"strconv"
	"strings"
	"testing"
)

// TestNhStatsSpace holds the nhstats family to NC8-T04's table
// (docs/netlink/goip-parity-next.md): each form's cmd and rc, the seven
// readbacks naming the row's own id, and the one traffic row.
//
// go test ./tools/nlcapgen/ -run TestNhStatsSpace
func TestNhStatsSpace(t *testing.T) {
	byID := familyRows(t, "nhstats")
	cases := []struct {
		description string
		id          string
		expected    string // cmd, then rc
	}{
		{"positive: nhstats group", "nhstats/ec-001", "ip nexthop add id 10 group 1/2 0"},
		{"positive: nhstats resilient", "nhstats/ec-002", "ip nexthop add id 11 group 1/2 type resilient buckets 8 0"},
		{"positive: nhstats hw-stats", "nhstats/ec-003", "ip nexthop add id 12 group 1/2 hw_stats on 0"},
		{"positive: nhstats weighted", "nhstats/ec-004", "ip nexthop add id 13 group 1,3/2,7 0"},
		{"positive: nhstats single", "nhstats/ec-005", "ip nexthop add id 3 via 192.0.2.3 dev d0 0"},
		{"positive: nhstats seg6local-count", "nhstats/ec-006", "ip -6 nexthop add id 20 encap seg6local action End count dev d0 0"},
		{"corner: nhstats group-one", "nhstats/ec-007", "ip nexthop add id 14 group 1 0"},
		{"negative: a duplicate nhstats group", "nhstats/dup-1", "ip nexthop add id 10 group 1/2 2"},
		{"corner: traffic", "nhstats/traffic", "ping -c 3 -W 1 -q 198.51.100.1 1"},
	}
	for _, tc := range cases {
		t.Run(tc.description, func(t *testing.T) {
			r, ok := byID[tc.id]
			if !ok {
				t.Fatalf("no row %s", tc.id)
			}
			if got := r.Cmd + " " + strconv.Itoa(*r.Expect.RC); got != tc.expected {
				t.Errorf("%s: %q, want %q", tc.id, got, tc.expected)
			}
		})
	}
	t.Run("positive: 7 forms, 3 duplicates and the traffic row", func(t *testing.T) {
		if len(byID) != 11 {
			t.Errorf("%d rows, want 11", len(byID))
		}
	})
	t.Run("positive: the seven readbacks name the row's id", func(t *testing.T) {
		for id, r := range byID {
			n := r.Axes["id"]
			var got []string
			for _, rb := range r.Readback {
				got = append(got, rb.Cmd)
			}
			want := []string{"ip nexthop show", "ip -s nexthop show", "ip -s -s nexthop show",
				"ip -j -s nexthop show", "ip -j -s -s nexthop show",
				"ip -s nexthop get id " + n, "ip -s nexthop show id " + n}
			if n == "" || !slices.Equal(got, want) {
				t.Errorf("%s: id %q, readbacks %v", id, n, got)
			}
		}
	})
	t.Run("boundary: a group row's setup adds its members first", func(t *testing.T) {
		for id, r := range byID {
			if !strings.Contains(r.Cmd, " group ") {
				continue
			}
			if len(r.Pre) < 2 || !strings.Contains(r.Pre[1].Cmd, "ip nexthop add id 1 ") {
				t.Errorf("%s: pre %v", id, r.Pre)
			}
		}
	})
	t.Run("corner: the traffic row adds group 10 and its route, then pings", func(t *testing.T) {
		r := byID["nhstats/traffic"]
		n := len(r.Pre)
		if n < 2 || r.Pre[n-2].Cmd != "ip nexthop add id 10 group 1/2" ||
			r.Pre[n-1].Cmd != "ip route add 198.51.100.0/24 nhid 10" || r.Expect.RefusedBy != refNone {
			t.Errorf("pre %v, expect %+v", r.Pre, r.Expect)
		}
	})
	t.Run("corner: the same bytes twice", func(t *testing.T) { sameTwice(t, "nhstats") })
}
