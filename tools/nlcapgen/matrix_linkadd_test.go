package main

import (
	"strings"
	"testing"
)

func TestLinkAddJudge(t *testing.T) {
	cases := []struct {
		description string
		kind, mtu   string
		expected    string // RefusedBy and errno, "" for success
	}{
		{"negative: a vlan's mtu past its lower device's", "vlan", "9000", "kernel EINVAL"},
		{"negative: so is a macvlan's", "macvlan-private", "9000", "kernel EINVAL"},
		{"boundary: a vlan at its lower device's mtu", "vlan-4094", "1500", ""},
		{"positive: a veth has no lower device", "veth", "9000", ""},
		{"positive: nor does a bridge", "bridge", "9000", ""},
		{"corner: a three-letter kind", "ifb", "9000", ""},
		{"boundary: no mtu", "vlan", "absent", ""},
	}
	for _, tc := range cases {
		t.Run(tc.description, func(t *testing.T) {
			exp, why := linkAddJudge(map[string]string{"kind": tc.kind, "mtu": tc.mtu})
			got := ""
			if exp != nil {
				got = strings.TrimSpace(string(exp.RefusedBy) + " " + exp.Errno)
				if why == "" {
					t.Error("a refusal with no reason")
				}
			}
			if got != tc.expected {
				t.Errorf("refused by %q (%s), want %q", got, why, tc.expected)
			}
		})
	}
}

func TestBuildLinkAdd(t *testing.T) {
	sp := linkAddSpace()
	cases := []struct {
		description string
		names       map[string]string
		cmd         string
		kind        string
	}{
		{"positive: a bare name and a dummy", nil, "ip link add n0 type dummy", "dummy"},
		{"positive: a vlan names its lower device before type", map[string]string{"kind": "vlan"},
			"ip link add n0 link d0 type vlan id 10", "vlan"},
		{"corner: dev before the name, options before the kind", map[string]string{"name": "dev", "mtu": "1400", "kind": "veth-mtu"},
			"ip link add dev n0 mtu 1400 type veth peer name p0 mtu 1400", "veth"},
		{"positive: up and both queue counts", map[string]string{"up": "up", "queues": "both", "kind": "bridge-stp"},
			"ip link add n0 up numtxqueues 2 numrxqueues 3 type bridge stp_state 1", "bridge"},
	}
	for _, tc := range cases {
		t.Run(tc.description, func(t *testing.T) {
			r := sp.Build(sp.assign(tc.names))
			if r.Cmd != tc.cmd {
				t.Errorf("cmd %q, want %q", r.Cmd, tc.cmd)
			}
			if r.Axes["kind"] != tc.kind || r.Axes["name"] != "n0" || r.Pre[0].Cmd != linkAddSetup ||
				r.Readback[0].Cmd != "ip -d link show dev n0" {
				t.Errorf("axes %v pre %q readback %q", r.Axes, r.Pre[0].Cmd, r.Readback[0].Cmd)
			}
		})
	}
}

func TestDeriveLinkAddDup(t *testing.T) {
	ok := func(id string, rc int) row {
		return row{ID: id, Desc: "corner: x", Cmd: "ip link add " + id, Pre: []preStep{{Cmd: linkAddSetup}}, Expect: expect{RC: intp(rc)}}
	}
	pos := []row{ok("a", 2), ok("b", 0), ok("c", 0), ok("d", 0), ok("e", 0)}
	out := deriveLinkAddDup(pos)
	cases := []struct {
		description string
		got, want   string
	}{
		{"positive: three rows at most", string(rune('0' + len(out))), "3"},
		{"negative: a refused row is not repeated", out[0].Cmd, "ip link add b"},
		{"positive: its setup adds the link first", lastPre(out[0]), "ip link add b"},
		{"positive: it expects EEXIST", out[0].Expect.Errno + " " + string(out[0].Expect.RefusedBy), "EEXIST kernel"},
		{"positive: ids count from 1", out[0].ID + " " + out[2].ID, "linkadd/dup-1 linkadd/dup-3"},
		{"positive: a negative desc", out[0].Desc, "negative: x: added twice (EEXIST)"},
		{"corner: the source row keeps its setup", lastPre(pos[1]), linkAddSetup},
	}
	for _, tc := range cases {
		t.Run(tc.description, func(t *testing.T) {
			if tc.got != tc.want {
				t.Errorf("got %q, want %q", tc.got, tc.want)
			}
		})
	}
}

func TestMatrixLinkAddRows(t *testing.T) {
	rows, err := matrixRows([]*space{linkAddSpace()}, matrixOpts{Seed: 1, Random: 40})
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
		{"positive: every row adds n0", func(r row) bool { return strings.HasPrefix(r.Cmd, "ip link add ") }},
		{"positive: the kind and its options come last", func(r row) bool {
			i := strings.Index(r.Cmd, " type ")
			if i < 0 {
				return false
			}
			after := r.Cmd[i:]
			for _, opt := range []string{" up", " txqueuelen ", " address ", " numtxqueues ", " numrxqueues "} {
				if strings.Contains(after, opt) {
					return false
				}
			}
			return true
		}},
		{"positive: a vlan or macvlan names d0", func(r row) bool {
			k := r.Axes["kind"]
			return (k != "vlan" && k != "macvlan") || r.Axes["link"] == "d0" || neg(r)
		}},
		{"negative: no positive stacked link passes its lower device's mtu", func(r row) bool {
			k := r.Axes["kind"]
			return neg(r) || (k != "vlan" && k != "macvlan") || r.Axes["mtu"] != "9000"
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
	var dups int
	for _, r := range rows {
		if strings.Contains(r.ID, "/dup-") {
			dups++
		}
	}
	if dups != 3 {
		t.Errorf("%d repeated adds, want 3", dups)
	}
}
