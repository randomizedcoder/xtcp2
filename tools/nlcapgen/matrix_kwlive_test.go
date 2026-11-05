package main

import (
	"bytes"
	"slices"
	"strconv"
	"strings"
	"testing"
)

func kwliveSpaces() []*space {
	return families["kwlive"]()
}

func kwliveRows(t *testing.T) map[string]row {
	t.Helper()
	rows, err := matrixRows(kwliveSpaces(), matrixOpts{Seed: 1})
	if err != nil {
		t.Fatal(err)
	}
	byID := make(map[string]row, len(rows))
	for i := range rows {
		if err := rows[i].validate(); err != nil {
			t.Fatal(err)
		}
		if _, dup := byID[rows[i].ID]; dup {
			t.Fatalf("id %s twice", rows[i].ID)
		}
		byID[rows[i].ID] = rows[i]
	}
	return byID
}

// TestKwliveSpaces holds the kwlive family to goip-live-forms.md's forms
// table, row by row.
func TestKwliveSpaces(t *testing.T) {
	byID := kwliveRows(t)
	cases := []struct {
		description string
		id          string
		expected    string // cmd, then rc
	}{
		{"positive: tos", "kwroute4/ec-001", "ip -4 route add 198.51.100.0/24 dev d0 tos 0x10 0"},
		{"negative: a tos with an ECN bit", "kwroute4/ng-001", "ip -4 route add 198.51.100.0/24 dev d0 tos 0x01 2"},
		{"positive: dsfield", "kwroute4/ec-002", "ip -4 route add 198.51.100.0/24 dev d0 dsfield 0x10 0"},
		{"positive: realms TO", "kwroute4/ec-003", "ip -4 route add 198.51.100.0/24 dev d0 realms 5 0"},
		{"positive: realms FROM/TO", "kwroute4/ec-004", "ip -4 route add 198.51.100.0/24 dev d0 realms 1/2 0"},
		{"positive: vrf, table below 256", "kwroute4/ec-005", "ip -4 route add 198.51.100.0/24 dev d0 vrf vrf0 0"},
		{"boundary: vrf, table above 255", "kwroute4/ec-006", "ip -4 route add 198.51.100.0/24 dev d0 vrf vrf1 0"},
		{"positive: a hop's onlink", "kwroute4/ec-007",
			"ip -4 route add 198.51.100.0/24 nexthop via 203.0.113.1 dev d0 onlink nexthop via 192.0.2.1 dev d0 0"},
		{"positive: a hop via inet6", "kwroute4/ec-008",
			"ip -4 route add 198.51.100.0/24 nexthop via inet6 2001:db8:1::1 dev d0 nexthop via 192.0.2.1 dev d0 0"},
		{"positive: a hop's realms", "kwroute4/ec-009",
			"ip -4 route add 198.51.100.0/24 nexthop via 192.0.2.1 dev d0 realms 3 nexthop via 192.0.2.2 dev d0 0"},
		{"positive: pref by number", "kwroute6/ec-001", "ip -6 route add 2001:db8:2::/64 dev d0 pref 1 0"},
		{"corner: pref not a preference", "kwroute6/ec-002", "ip -6 route add 2001:db8:2::/64 dev d0 pref 2 0"},
		{"boundary: pref 255", "kwroute6/ec-003", "ip -6 route add 2001:db8:2::/64 dev d0 pref 255 0"},
		{"positive: IPv6 vrf, table below 256", "kwroute6/ec-004", "ip -6 route add 2001:db8:2::/64 dev d0 vrf vrf0 0"},
		{"boundary: IPv6 vrf, table above 255", "kwroute6/ec-005", "ip -6 route add 2001:db8:2::/64 dev d0 vrf vrf1 0"},
		{"negative: dsfield on IPv6", "kwroute6/ng-001", "ip -6 route add 2001:db8:2::/64 dev d0 dsfield 0x10 2"},
		{"positive: set by group alone", "kwlink/ec-001", "ip link set group 5 mtu 1400 0"},
		{"corner: a group with no member", "kwlink/ec-002", "ip link set group 6 mtu 1400 0"},
		{"positive: xdpoffload off", "kwlink/ec-003", "ip link set d0 xdpoffload off 0"},
		{"positive: index on a create", "kwlink/ec-004", "ip link add d7 index 77 type dummy 0"},
		{"negative: an index in use", "kwlink/ng-001", "ip link add d8 index 1 type dummy 2"},
		{"positive: encap mpls, one label", "kwnh/ec-001", "ip nexthop add id 51 encap mpls 101 via 192.0.2.1 dev d0 0"},
		{"positive: encap mpls, two labels", "kwnh/ec-002", "ip nexthop add id 52 encap mpls 101/102 via 192.0.2.1 dev d0 0"},
		{"positive: encap mpls with ttl", "kwnh/ec-003", "ip nexthop add id 53 encap mpls 101 ttl 10 via 192.0.2.1 dev d0 0"},
		{"positive: encap mpls, an IPv6 gateway", "kwnh/ec-004", "ip nexthop add id 54 encap mpls 101 via 2001:db8:1::1 dev d0 0"},
		{"negative: hw_stats off on one nexthop", "kwnh/ng-001", "ip nexthop add id 55 via 192.0.2.1 dev d0 hw_stats off 2"},
		{"positive: rule realms FROM/TO", "kwrule/ec-001", "ip -4 rule add pref 200 realms 1/2 table 100 0"},
		{"negative: a duplicate route add", "kwroute4/dup-1", "ip -4 route add 198.51.100.0/24 dev d0 tos 0x10 2"},
		{"negative: a duplicate IPv6 route add", "kwroute6/dup-3", "ip -6 route add 2001:db8:2::/64 dev d0 pref 255 2"},
		{"negative: a duplicate nexthop add", "kwnh/dup-3", "ip nexthop add id 53 encap mpls 101 ttl 10 via 192.0.2.1 dev d0 2"},
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
	t.Run("positive: the family is the forms and the duplicates", func(t *testing.T) {
		const forms, dups = 27, 9
		var formRows, dupRows int
		for id := range byID {
			if strings.Contains(id, "/dup-") {
				dupRows++
			} else {
				formRows++
			}
		}
		if formRows != forms || dupRows != dups {
			t.Errorf("%d form rows and %d duplicates, want %d and %d", formRows, dupRows, forms, dups)
		}
	})
	t.Run("negative: every setup step is ip and tolerates nothing", func(t *testing.T) {
		for id, r := range byID {
			for _, p := range r.Pre {
				for step := range strings.SplitSeq(p.Cmd, " && ") {
					if !strings.HasPrefix(step, "ip ") || len(p.OK) != 0 {
						t.Errorf("%s: step %q (ok %v)", id, step, p.OK)
					}
				}
			}
		}
	})
	t.Run("boundary: pref 255's axes", func(t *testing.T) {
		if got := byID["kwroute6/ec-003"].Axes["pref"]; got != "255" {
			t.Errorf("pref %q, want 255", got)
		}
	})
	t.Run("corner: the kwnh modules", func(t *testing.T) {
		for id, r := range byID {
			want := []string(nil)
			if strings.HasPrefix(id, "kwnh/") {
				want = kwNhModules
			}
			if !slices.Equal(r.Modules, want) {
				t.Errorf("%s: modules %v, want %v", id, r.Modules, want)
			}
		}
	})
	t.Run("corner: the same bytes twice", func(t *testing.T) {
		var out [2]bytes.Buffer
		for i := range out {
			rows, err := matrixRows(kwliveSpaces(), matrixOpts{Seed: 1})
			if err != nil {
				t.Fatal(err)
			}
			if err := writeRows(&out[i], nil, rows); err != nil {
				t.Fatal(err)
			}
		}
		if !bytes.Equal(out[0].Bytes(), out[1].Bytes()) {
			t.Error("two runs differ")
		}
	})
}
