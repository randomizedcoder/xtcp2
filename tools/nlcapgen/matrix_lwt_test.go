package main

import (
	"bytes"
	"slices"
	"strconv"
	"strings"
	"testing"
)

func lwtRows(t *testing.T) map[string]row {
	t.Helper()
	rows, err := matrixRows(families["lwt"](), matrixOpts{Seed: 1})
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

// TestLwtSpaces holds the lwt family to goip-lwt-encap.md's forms table,
// row by row. LE3-T01.
func TestLwtSpaces(t *testing.T) {
	byID := lwtRows(t)
	cases := []struct {
		description string
		id          string
		expected    string // cmd, then rc
	}{
		{"positive: mpls, one label", "lwtroute4/ec-001", "ip -4 route add 198.51.100.0/24 encap mpls 100 via 192.0.2.1 dev d0 0"},
		{"positive: mpls, a stack and ttl", "lwtroute4/ec-002", "ip -4 route add 198.51.100.0/24 encap mpls 100/200 ttl 10 via 192.0.2.1 dev d0 0"},
		{"boundary: mpls ttl 0", "lwtroute4/ec-003", "ip -4 route add 198.51.100.0/24 encap mpls 100 ttl 0 via 192.0.2.1 dev d0 0"},
		{"positive: ip, id and dst", "lwtroute4/ec-004", "ip -4 route add 198.51.100.0/24 encap ip id 1 dst 203.0.113.5 dev d0 0"},
		{"positive: ip, every scalar", "lwtroute4/ec-005", "ip -4 route add 198.51.100.0/24 encap ip id 1 dst 203.0.113.5 src 192.0.2.10 tos 0x10 ttl 64 key csum seq dev d0 0"},
		{"positive: ip geneve_opts", "lwtroute4/ec-006", "ip -4 route add 198.51.100.0/24 encap ip id 1 dst 203.0.113.5 geneve_opts 0x0102:0x80:00112233 dev d0 0"},
		{"corner: ip geneve_opts, base 0", "lwtroute4/ec-007", "ip -4 route add 198.51.100.0/24 encap ip id 1 dst 203.0.113.5 geneve_opts 0102:80:00112233 dev d0 0"},
		{"positive: ip, two geneve options", "lwtroute4/ec-008", "ip -4 route add 198.51.100.0/24 encap ip id 1 dst 203.0.113.5 geneve_opts 0x0102:0x80:00112233,0x0102:0x81:44556677 dev d0 0"},
		{"positive: ip vxlan_opts", "lwtroute4/ec-009", "ip -4 route add 198.51.100.0/24 encap ip id 1 dst 203.0.113.5 vxlan_opts 0x800 dev d0 0"},
		{"positive: ip erspan_opts", "lwtroute4/ec-010", "ip -4 route add 198.51.100.0/24 encap ip id 1 dst 203.0.113.5 erspan_opts 1:0x1234:: dev d0 0"},
		{"negative: mpls implicit null", "lwtroute4/ng-001", "ip -4 route add 198.51.100.0/24 encap mpls 3 via 192.0.2.1 dev d0 2"},
		{"negative: geneve data not a multiple of 4", "lwtroute4/ng-002", "ip -4 route add 198.51.100.0/24 encap ip id 1 dst 203.0.113.5 geneve_opts 0x0102:0x80:001122 dev d0 2"},
		{"negative: two option kinds", "lwtroute4/ng-003", "ip -4 route add 198.51.100.0/24 encap ip id 1 dst 203.0.113.5 geneve_opts 0x0102:0x80:00112233 vxlan_opts 1 dev d0 255"},
		{"negative: an unknown type", "lwtroute4/ng-004", "ip -4 route add 198.51.100.0/24 encap foo 1 dev d0 255"},
		{"negative: the type's word last", "lwtroute4/ng-005", "ip -4 route add 198.51.100.0/24 dev d0 encap ip key 255"},
		{"positive: ip6, id and dst", "lwtroute6/ec-001", "ip -6 route add 2001:db8:2::/64 encap ip6 id 1 dst 2001:db8:9::1 dev d0 0"},
		{"positive: ip6, every scalar", "lwtroute6/ec-002", "ip -6 route add 2001:db8:2::/64 encap ip6 id 1 dst 2001:db8:9::1 src 2001:db8:1::10 tc 0x10 hoplimit 64 key csum seq dev d0 0"},
		{"positive: ip6 geneve_opts", "lwtroute6/ec-003", "ip -6 route add 2001:db8:2::/64 encap ip6 id 1 dst 2001:db8:9::1 geneve_opts 0x0102:0x80:00112233 dev d0 0"},
		{"positive: mpls on IPv6", "lwtroute6/ec-004", "ip -6 route add 2001:db8:2::/64 encap mpls 100 via 2001:db8:1::1 dev d0 0"},
		{"corner: ip on an IPv6 route", "lwtroute6/ec-005", "ip -6 route add 2001:db8:2::/64 encap ip id 1 dst 203.0.113.5 dev d0 0"},
		{"positive: a hop's mpls", "lwthop/ec-001", "ip -4 route add 198.51.100.0/24 nexthop encap mpls 100 via 192.0.2.1 dev d0 nexthop via 192.0.2.2 dev d0 0"},
		{"positive: both hops ip", "lwthop/ec-002", "ip -4 route add 198.51.100.0/24 nexthop encap ip id 1 dst 203.0.113.5 via 192.0.2.1 dev d0 nexthop encap ip id 2 dst 203.0.113.6 via 192.0.2.2 dev d0 0"},
		{"positive: a hop's ip6", "lwthop/ec-003", "ip -6 route add 2001:db8:2::/64 nexthop encap ip6 dst 2001:db8:9::1 via 2001:db8:1::1 dev d0 nexthop via 2001:db8:1::2 dev d0 0"},
		{"negative: a hop's unknown type", "lwthop/ng-001", "ip -4 route add 198.51.100.0/24 nexthop encap foo 1 via 192.0.2.1 dev d0 255"},
		{"positive: a nexthop ip", "lwtnh/ec-001", "ip nexthop add id 61 encap ip id 1 dst 203.0.113.5 via 192.0.2.1 dev d0 0"},
		{"positive: a nexthop ip6", "lwtnh/ec-002", "ip nexthop add id 62 encap ip6 dst 2001:db8:9::1 via 2001:db8:1::1 dev d0 0"},
		{"positive: a nexthop ip geneve_opts", "lwtnh/ec-003", "ip nexthop add id 63 encap ip id 1 dst 203.0.113.5 geneve_opts 0x0102:0x80:00112233 via 192.0.2.1 dev d0 0"},
		{"negative: a duplicate route add", "lwtroute4/dup-1", "ip -4 route add 198.51.100.0/24 encap mpls 100 via 192.0.2.1 dev d0 2"},
		{"negative: a duplicate multipath add", "lwthop/dup-2", "ip -4 route add 198.51.100.0/24 nexthop encap ip id 1 dst 203.0.113.5 via 192.0.2.1 dev d0 nexthop encap ip id 2 dst 203.0.113.6 via 192.0.2.2 dev d0 2"},
		{"negative: a duplicate nexthop add, ip6", "lwtnh/dup-2", "ip nexthop add id 62 encap ip6 dst 2001:db8:9::1 via 2001:db8:1::1 dev d0 2"},
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
		const forms, dups = 27, 12
		var formRows, dupRows int
		rcs := map[int]int{}
		for id, r := range byID {
			if strings.Contains(id, "/dup-") {
				dupRows++
				continue
			}
			formRows++
			rcs[*r.Expect.RC]++
		}
		if formRows != forms || dupRows != dups {
			t.Errorf("%d form rows and %d duplicates, want %d and %d", formRows, dupRows, forms, dups)
		}
		if rcs[0] != 21 || rcs[2] != 2 || rcs[255] != 4 || len(rcs) != 3 {
			t.Errorf("form rcs %v, want 21 at 0, 2 at 2 and 4 at 255", rcs)
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
	t.Run("boundary: every row loads the MPLS tunnel's modules", func(t *testing.T) {
		for id, r := range byID {
			if !slices.Equal(r.Modules, kwNhModules) {
				t.Errorf("%s: modules %v, want %v", id, r.Modules, kwNhModules)
			}
		}
	})
	t.Run("corner: a nexthop's id is not the tunnel's", func(t *testing.T) {
		r := byID["lwtnh/ec-001"]
		if got := r.Axes["id"] + " " + r.Axes["tun_id"]; got != "61 1" {
			t.Errorf("id and tun_id %q, want \"61 1\"", got)
		}
	})
	t.Run("corner: the same bytes twice", func(t *testing.T) {
		var out [2]bytes.Buffer
		for i := range out {
			rows, err := matrixRows(families["lwt"](), matrixOpts{Seed: 1})
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
