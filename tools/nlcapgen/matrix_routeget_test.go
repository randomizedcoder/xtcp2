package main

import (
	"strings"
	"testing"
)

func TestBuildRouteGet(t *testing.T) {
	sp4, sp6 := routeGet4Space(), routeGet6Space()
	cases := []struct {
		description string
		sp          *space
		names       map[string]string
		cmd         string
		axes        string // k=v for the keys in want
		want        []string
	}{
		{"positive: a bare IPv4 lookup", sp4, nil, "ip -4 route get 192.0.2.1", "family=4 dst=192.0.2.1", []string{"family", "dst"}},
		{"boundary: mark 0 is spelled but carries no fact, as ip sends no RTA_MARK", sp4, map[string]string{"mark": "0"},
			"ip -4 route get 192.0.2.1 mark 0", "mark=", []string{"mark"}},
		{"positive: an IPv6 lookup with a flow label and ports", sp6, map[string]string{"flowlabel": "label", "ports": "both"},
			"ip -6 route get 2001:db8:1::1 sport 1234 dport 443 flowlabel 0x12345", "sport=1234 dport=443 flowlabel=74565",
			[]string{"sport", "dport", "flowlabel"}},
		{"corner: dev spells oif", sp4, map[string]string{"dev": "dev"}, "ip -4 route get 192.0.2.1 dev d0", "oif=d0", []string{"oif"}},
		{"positive: each family's own ICMP", sp6, map[string]string{"ipproto": "icmp"}, "ip -6 route get 2001:db8:1::1 ipproto ipv6-icmp",
			"ipproto=58", []string{"ipproto"}},
	}
	for _, tc := range cases {
		t.Run(tc.description, func(t *testing.T) {
			r := tc.sp.Build(tc.sp.assign(tc.names))
			if r.Cmd != tc.cmd {
				t.Errorf("cmd %q, want %q", r.Cmd, tc.cmd)
			}
			got := make([]string, 0, len(tc.want))
			for _, k := range tc.want {
				got = append(got, k+"="+r.Axes[k])
			}
			if s := strings.Join(got, " "); s != tc.axes {
				t.Errorf("axes %q, want %q", s, tc.axes)
			}
			if !strings.Contains(r.Pre[0].Cmd, " && ip route add default via 192.0.2.1 dev d0") ||
				!strings.Contains(r.Pre[0].Cmd, " && ip -6 route add default via 2001:db8:1::1 dev d0") || len(r.Readback) != 0 {
				t.Errorf("pre %q readback %v", r.Pre[0].Cmd, r.Readback)
			}
		})
	}
}

func TestMatrixRouteGetRows(t *testing.T) {
	rows, err := matrixRows(families["routeget"](), matrixOpts{Seed: 1, Random: 40})
	if err != nil {
		t.Fatal(err)
	}
	if err := checkRows(rows); err != nil {
		t.Fatal(err)
	}
	neg := func(r row) bool { return strings.HasPrefix(r.Desc, "negative:") }
	cases := []struct {
		description string
		check       func(r row) bool
	}{
		{"negative: no positive lookup names a protocol rtm_getroute_parse_ip_proto refuses", func(r row) bool {
			p := r.Axes["ipproto"]
			ok := p == "" || p == "6" || p == "17" || (p == "1" && r.Axes["family"] == "4") || (p == "58" && r.Axes["family"] == "6")
			return neg(r) || ok
		}},
		{"negative: no positive IPv6 lookup asks RTM_F_NOTIFY", func(r row) bool {
			return neg(r) || r.Axes["family"] != "6" || r.Axes["notify"] == ""
		}},
		{"positive: only IPv6 rows carry a flow label", func(r row) bool {
			return r.Axes["flowlabel"] == "" || r.Axes["family"] == "6"
		}},
		{"positive: every row is a lookup", func(r row) bool { return strings.Contains(r.Cmd, " route get ") }},
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
