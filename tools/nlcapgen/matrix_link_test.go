package main

import (
	"strconv"
	"strings"
	"testing"
)

func TestBuildLinkSet(t *testing.T) {
	sp := linkSetSpace()
	cases := []struct {
		description string
		names       map[string]string
		cmd         string
		axes        string // the axes named in want, as k=v
		want        []string
	}{
		{"boundary: every axis absent names only the device", nil, "ip link set dev d0", "", nil},
		{"positive: up with an mtu", map[string]string{"updown": "up", "mtu": "1400"}, "ip link set dev d0 up mtu 1400",
			"up=1 mtu=1400", []string{"up", "mtu"}},
		{"corner: arp off sets IFF_NOARP on", map[string]string{"flag": "noarp"}, "ip link set dev d0 arp off",
			"flag=128 flag_on=1", []string{"flag", "flag_on"}},
		{"corner: a rename comes last", map[string]string{"name": "rename", "txqlen": "abbrev"}, "ip link set dev d0 txq 100 name d9",
			"txqlen=100 name=d9", []string{"txqlen", "name"}},
		{"boundary: the longest alias is IFALIASZ - 1 characters", map[string]string{"alias": "255"},
			"ip link set dev d0 alias " + strings.Repeat("a", 255), "alias=" + strings.Repeat("a", 255), []string{"alias"}},
	}
	for _, tc := range cases {
		t.Run(tc.description, func(t *testing.T) {
			r := sp.Build(sp.assign(tc.names))
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
			if r.Pre[0].Cmd != linkSetup || r.Readback[0].Cmd != "ip -d link show" {
				t.Errorf("pre %q readback %q", r.Pre[0].Cmd, r.Readback[0].Cmd)
			}
		})
	}
}

func TestMatrixLinkRows(t *testing.T) {
	rows, err := matrixRows(families["link"](), matrixOpts{Seed: 1, Random: 40})
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
		{"positive: every row sets d0", func(r row) bool { return strings.HasPrefix(r.Cmd, "ip link set dev d0") }},
		{"positive: a flag always says whether it is on", func(r row) bool {
			_, flag := r.Axes["flag"]
			_, on := r.Axes["flag_on"]
			return flag == on
		}},
		{"negative: no positive row names a txqueuelen past S16_MAX", func(r row) bool {
			n, err := strconv.Atoi(r.Axes["txqlen"])
			return neg(r) || err != nil || n <= 32767
		}},
		{"positive: a negative row changes one axis of a row with no rename", func(r row) bool {
			return !neg(r) || !strings.Contains(r.Cmd, " name d9")
		}},
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
