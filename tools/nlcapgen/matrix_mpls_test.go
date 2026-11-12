package main

import (
	"bytes"
	"fmt"
	"slices"
	"strings"
	"testing"
)

func mplsRows(t *testing.T) []row {
	t.Helper()
	rows, err := matrixRows(families["mpls"](), matrixOpts{Seed: 1, Random: 20})
	if err != nil {
		t.Fatal(err)
	}
	if err := checkRows(rows); err != nil {
		t.Fatal(err)
	}
	return rows
}

// TestMplsRows holds the named rows and the refusals to the captures the
// family was calibrated on (goip-parity-next.md, NC2-T01).
func TestMplsRows(t *testing.T) {
	byID := map[string]row{}
	for _, r := range mplsRows(t) {
		byID[r.ID] = r
	}
	cases := []struct {
		description string
		id          string
		expected    string // cmd, rc, refused by, errno
	}{
		{"positive: the first row", "mpls/ec-001", "ip -f mpls route add 100 via inet 10.0.0.2 dev d0 0 none "},
		{"boundary: a reserved label", "mpls/ng-001", "ip -f mpls route add 15 via inet 10.0.0.2 dev d0 2 kernel EINVAL"},
		{"negative: implicit null as the label", "mpls/ng-002", "ip -f mpls route add 3 via inet 10.0.0.2 dev d0 2 kernel EINVAL"},
		{"boundary: platform_labels itself", "mpls/ng-003", "ip -f mpls route add 1000 via inet 10.0.0.2 dev d0 2 kernel EINVAL"},
		{"boundary: a label past 20 bits", "mpls/ng-005", "ip -f mpls route add 1048576 via inet 10.0.0.2 dev d0 1 iproute2 "},
		{"negative: as with no labels", "mpls/ng-011", "ip -f mpls route add 100 as via inet 10.0.0.2 dev d0 1 iproute2 "},
		{"negative: two vias", "mpls/ng-013", "ip -f mpls route add 100 via inet 10.0.0.2 via inet 10.0.0.3 dev d0 255 iproute2 "},
		{"negative: no such device", "mpls/ng-014", "ip -f mpls route add 100 via inet 10.0.0.2 dev nosuch 1 kernel ENODEV"},
		{"negative: no next hop", "mpls/ng-017", "ip -f mpls route add 100 2 kernel ENODEV"},
		{"negative: ttl-propagate maybe", "mpls/ng-018", "ip -f mpls route add 100 ttl-propagate maybe via inet 10.0.0.2 dev d0 255 iproute2 "},
		{"negative: a duplicate", "mpls/dup", "ip -f mpls route add 100 via inet 10.0.0.2 dev d0 2 kernel EEXIST"},
		{"corner: a del of a label that isn't there", "mpls/del-absent", "ip -f mpls route del 100 0 none "},
		{"negative: no platform labels", "mpls/no-platform", "ip -f mpls route add 100 via inet 10.0.0.2 dev d0 2 kernel EINVAL"},
		{"corner: as twice", "mpls/as-twice", "ip -f mpls route add 100 as 200 as 300 via inet 10.0.0.2 dev d0 0 none "},
		{"negative: a get of a label that isn't there", "mpls/get-absent", "ip -f mpls route get 101 2 kernel ENETUNREACH"},
		{"negative: ttl-propagate after the hops", "mpls/ttl-after-hops",
			"ip -f mpls route add 100 nexthop as 300 via inet 10.0.0.2 dev d0 nexthop as 400 via inet 10.0.0.3 dev d0 ttl-propagate enabled 255 ack-then-fail "},
		{"corner: as on an IPv4 route", "mpls/inet-as", "ip route add 10.1.0.0/24 as 200 via 10.0.0.2 0 none "},
		{"negative: as on an IPv6 route", "mpls/inet6-as", "ip -6 route add 2001:db8:9::/64 as 200 via 2001:db8::2 1 iproute2 "},
		{"negative: as on an IPv4 get", "mpls/inet-get-as", "ip route get 10.0.0.2 as 200 2 kernel EINVAL"},
	}
	for _, tc := range cases {
		t.Run(tc.description, func(t *testing.T) {
			r, ok := byID[tc.id]
			if !ok {
				t.Fatalf("no row %s", tc.id)
			}
			if got := fmt.Sprintf("%s %d %s %s", r.Cmd, *r.Expect.RC, r.Expect.RefusedBy, r.Expect.Errno); got != tc.expected {
				t.Errorf("got %q, want %q", got, tc.expected)
			}
		})
	}
}

// TestMplsSpace checks properties every row must have.
func TestMplsSpace(t *testing.T) {
	rows := mplsRows(t)
	cases := []struct {
		description string
		check       func(r row) bool
	}{
		{"positive: every row's setup turns MPLS on, or is the row without it", func(r row) bool {
			return r.Pre[0].Cmd == mplsSetup || r.ID == "mpls/no-platform"
		}},
		{"positive: every row loads mpls_router and mpls_iptunnel", func(r row) bool {
			return slices.Equal(r.Modules, mplsModules)
		}},
		{"positive: every row reads the MPLS table back, as text and JSON", func(r row) bool {
			return len(r.Readback) == 2 && r.Readback[0].Cmd == "ip -f mpls route show" && r.Readback[1].Cmd == "ip -j -f mpls route show"
		}},
		{"positive: a del's setup adds its label first", func(r row) bool {
			return !strings.HasPrefix(r.Cmd, "ip -f mpls route del ") || r.ID == "mpls/del-absent" ||
				(len(r.Pre) == 2 && r.Pre[1].Cmd == "ip -f mpls route add "+r.Axes["dst"]+" dev d0")
		}},
		{"negative: ttl-propagate never follows a nexthop, but in the row made to break that", func(r row) bool {
			i := strings.Index(r.Cmd, " nexthop ")
			return i < 0 || !strings.Contains(r.Cmd[i:], "ttl-propagate") || r.ID == "mpls/ttl-after-hops"
		}},
		{"corner: no doubled space", func(r row) bool { return !strings.Contains(r.Cmd, "  ") }},
	}
	for _, tc := range cases {
		t.Run(tc.description, func(t *testing.T) {
			for _, r := range rows {
				if !tc.check(r) {
					t.Errorf("%s: %s", r.ID, r.Cmd)
				}
			}
		})
	}
}

// TestMplsCoverage checks the strategies reached every value, and that
// the same seed gives the same bytes.
func TestMplsCoverage(t *testing.T) {
	rows := mplsRows(t)
	sp := mplsSpace()
	seen := map[string]bool{}
	for _, r := range rows {
		if *r.Expect.RC == 0 {
			seen[r.Cmd] = true
		}
	}
	cases := []struct {
		description string
		axis        string
		expected    int // the values no positive row's cmd carries
	}{
		{"positive: each verb", "verb", 0},
		{"positive: each label", "label", 0},
		{"positive: each as", "as", 0},
		{"positive: each ttl-propagate", "ttl", 0},
		{"positive: each via", "via", 0},
	}
	for _, tc := range cases {
		t.Run(tc.description, func(t *testing.T) {
			missing := 0
			for _, ax := range sp.Axes {
				if ax.Name != tc.axis {
					continue
				}
				for _, val := range ax.Values {
					found := false
					for cmd := range seen {
						if val.Arg == "" || strings.Contains(cmd, " "+val.Arg+" ") || strings.HasSuffix(cmd, " "+val.Arg) {
							found = true
							break
						}
					}
					if !found {
						missing++
						t.Logf("%s=%s in no positive row", tc.axis, val.Name)
					}
				}
			}
			if missing != tc.expected {
				t.Errorf("%d values missing, want %d", missing, tc.expected)
			}
		})
	}
	var a, b bytes.Buffer
	for _, buf := range []*bytes.Buffer{&a, &b} {
		if err := writeRows(buf, nil, mplsRows(t)); err != nil {
			t.Fatal(err)
		}
	}
	if !bytes.Equal(a.Bytes(), b.Bytes()) {
		t.Error("the same seed wrote different bytes")
	}
	if n := len(rows); n != 115 {
		t.Errorf("%d rows, want 115", n)
	}
}
