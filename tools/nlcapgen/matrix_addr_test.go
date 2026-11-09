package main

import (
	"strings"
	"testing"
)

// addrPick is a pick of every axis's first value, with kv's changes.
func addrPick(sp *space, kv ...string) map[string]string {
	m := pickOf(sp.Axes, sp.assign(nil))
	for i := 0; i+1 < len(kv); i += 2 {
		m[kv[i]] = kv[i+1]
	}
	return m
}

func TestAddrJudge(t *testing.T) {
	sp4, sp6 := addr4Space(), addr6Space()
	cases := []struct {
		description string
		judge       func(map[string]string) (*expect, string)
		pick        map[string]string
		expected    string // RefusedBy and errno, "" for success
	}{
		{"positive: a plain IPv4 add", addr4Judge, addrPick(sp4), ""},
		{"negative: IPv4 autojoin on a unicast address", addr4Judge, addrPick(sp4, "flags", "autojoin"), "ack-then-fail"},
		{"positive: IPv4 autojoin on a multicast address", addr4Judge, addrPick(sp4, "flags", "autojoin", "local", "mcast"), ""},
		{"corner: adding 0.0.0.0 succeeds", addr4Judge, addrPick(sp4, "local", "zero"), ""},
		{"negative: deleting 0.0.0.0 finds nothing", addr4Judge, addrPick(sp4, "local", "zero", "verb", "del"), "kernel EADDRNOTAVAIL"},
		{"positive: a plain IPv6 add", addr6Judge, addrPick(sp6), ""},
		{"negative: IPv6 autojoin on a unicast address", addr6Judge, addrPick(sp6, "flags", "autojoin"), "ack-then-fail"},
		{"negative: an IPv6 multicast address without autojoin", addr6Judge, addrPick(sp6, "local", "mcast"), "kernel EADDRNOTAVAIL"},
		{"positive: an IPv6 multicast address with autojoin", addr6Judge, addrPick(sp6, "local", "mcast", "flags", "autojoin"), ""},
		{"negative: the unspecified IPv6 address", addr6Judge, addrPick(sp6, "local", "any"), "kernel EADDRNOTAVAIL"},
		{"positive: mngtmpaddr on a /64", addr6Judge, addrPick(sp6, "flags", "mngtmpaddr"), ""},
		{"negative: mngtmpaddr on a /128", addr6Judge, addrPick(sp6, "flags", "mngtmpaddr", "local", "128"), "kernel EINVAL"},
		{"boundary: mngtmpaddr on a /128 with a /64 peer sends /64", addr6Judge, addrPick(sp6, "flags", "mngtmpaddr", "local", "128", "peer", "64"), ""},
		{"negative: mngtmpaddr on a /64 with a /128 peer sends /128", addr6Judge, addrPick(sp6, "flags", "mngtmpaddr", "peer", "128"), "kernel EINVAL"},
		{"negative: deleting what mngtmpaddr kept from being added", addr6Judge, addrPick(sp6, "flags", "mngtmpaddr", "local", "0", "verb", "del"), "kernel EADDRNOTAVAIL"},
		{"corner: mngtmpaddr's EINVAL comes before the unspecified address's refusal", addr6Judge, addrPick(sp6, "flags", "mngtmpaddr", "local", "any", "peer", "128"), "kernel EINVAL"},
	}
	for _, tc := range cases {
		t.Run(tc.description, func(t *testing.T) {
			exp, why := tc.judge(tc.pick)
			got := ""
			if exp != nil {
				got = strings.TrimSpace(exp.RefusedBy + " " + exp.Errno)
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

func TestBuildAddr(t *testing.T) {
	sp4, sp6 := addr4Space(), addr6Space()
	cases := []struct {
		description string
		sp          *space
		names       map[string]string
		cmd         string
		preAdd      string // the last pre step, when it adds the address
	}{
		{"positive: an add names the device last", sp4, nil, "ip -4 address add 192.0.2.1/24 dev d0", ""},
		{"positive: a delete first adds its address", sp4, map[string]string{"verb": "del", "label": "alias"},
			"ip -4 address del 192.0.2.1/24 label d0:1 dev d0", "ip -4 address add 192.0.2.1/24 label d0:1 dev d0"},
		{"positive: change first adds its address", sp6, map[string]string{"verb": "change", "flags": "nodad"},
			"ip -6 address change 2001:db8:1::1/64 nodad dev d0", "ip -6 address add 2001:db8:1::1/64 nodad dev d0"},
		{"positive: replace adds nothing first", sp6, map[string]string{"verb": "replace"}, "ip -6 address replace 2001:db8:1::1/64 dev d0", ""},
		{"negative: a delete whose add is refused adds nothing first", sp4, map[string]string{"verb": "del", "flags": "autojoin"},
			"ip -4 address del 192.0.2.1/24 autojoin dev d0", ""},
		{"corner: a delete of 0.0.0.0 still adds it first, which stores nothing", sp4, map[string]string{"verb": "del", "local": "zero"},
			"ip -4 address del 0.0.0.0/24 dev d0", "ip -4 address add 0.0.0.0/24 dev d0"},
	}
	for _, tc := range cases {
		t.Run(tc.description, func(t *testing.T) {
			r := tc.sp.Build(tc.sp.assign(tc.names))
			if r.Cmd != tc.cmd {
				t.Errorf("cmd %q, want %q", r.Cmd, tc.cmd)
			}
			last := lastPre(r)
			if adds := strings.Contains(last, " address add "); adds != (tc.preAdd != "") || (adds && last != tc.preAdd) {
				t.Errorf("last pre %q, want the add %q", last, tc.preAdd)
			}
			if r.Pre[0].Cmd != addrSetup || r.Axes["family"] != tc.cmd[4:5] {
				t.Errorf("first pre %q, axes %v", r.Pre[0].Cmd, r.Axes)
			}
		})
	}
}

func TestSpaceAssign(t *testing.T) {
	sp := addr4Space()
	cases := []struct {
		description string
		names       map[string]string
		expected    string // axis=value pairs of the result, for the axes named
		axes        []string
	}{
		{"positive: a named value is taken", map[string]string{"local": "mcast"}, "local=mcast peer=absent", []string{"local", "peer"}},
		{"boundary: no names gives each axis's first value", nil, "verb=add local=24", []string{"verb", "local"}},
		{"negative: an unknown value leaves the first", map[string]string{"local": "nosuch"}, "local=24", []string{"local"}},
		{"corner: a name for no axis changes nothing", map[string]string{"nosuch": "x"}, "verb=add", []string{"verb"}},
	}
	for _, tc := range cases {
		t.Run(tc.description, func(t *testing.T) {
			pick := pickOf(sp.Axes, sp.assign(tc.names))
			got := make([]string, 0, len(tc.axes))
			for _, a := range tc.axes {
				got = append(got, a+"="+pick[a])
			}
			if s := strings.Join(got, " "); s != tc.expected {
				t.Errorf("got %q, want %q", s, tc.expected)
			}
		})
	}
}

func TestMatrixAddrRows(t *testing.T) {
	rows, err := matrixRows(families["addr"](), matrixOpts{Seed: 1, Random: 40})
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
		{"positive: a change or delete's setup adds what it works on", func(r row) bool {
			v := r.Axes["verb"]
			return (v != "change" && v != "del") || neg(r) || strings.Contains(lastPre(r), " address add ")
		}},
		{"positive: every row names its family and a local address", func(r row) bool {
			return (r.Axes["family"] == "4" || r.Axes["family"] == "6") && (r.Axes["local"] != "" || neg(r))
		}},
		{"negative: no positive IPv4 row has autojoin with a peer", func(r row) bool {
			return neg(r) || r.Axes["family"] != "4" || r.Axes["flags"] != "1024" || r.Axes["peer"] == ""
		}},
		{"negative: no positive row has autojoin on a unicast address", func(r row) bool {
			return neg(r) || r.Axes["flags"] != "1024" || r.Axes["local"] == "239.1.1.1" || r.Axes["local"] == "ff0e::1:1"
		}},
		{"positive: a repeated add (EEXIST) stores something the first time", func(r row) bool {
			return !strings.Contains(r.ID, "/dup-") || r.Axes["noop"] == ""
		}},
		{"positive: every row runs on d0 alone", func(r row) bool {
			return strings.HasSuffix(r.Cmd, " dev d0") && r.Pre[0].Cmd == addrSetup
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
	if !strings.Contains(rows[len(rows)-1].ID, "addr6/") {
		t.Errorf("last row %s, want the IPv6 space's", rows[len(rows)-1].ID)
	}
}

func TestAddrCanDup(t *testing.T) {
	cases := []struct {
		description string
		axes        map[string]string
		expected    bool
	}{
		{"positive: a stored address can be added twice", map[string]string{"local": "192.0.2.1"}, true},
		{"negative: 0.0.0.0 stores nothing, so a second add succeeds", map[string]string{"local": "0.0.0.0", "noop": "1"}, false},
		{"boundary: a row with no axes", nil, true},
	}
	for _, tc := range cases {
		t.Run(tc.description, func(t *testing.T) {
			if got := addrCanDup(&row{Axes: tc.axes}); got != tc.expected {
				t.Errorf("got %v, want %v", got, tc.expected)
			}
		})
	}
}

// TestAddr4Exclusions: IPv4 autojoin joins ifa_address, which a peer
// replaces, so no row may pair the two; Derive writes the negative.
func TestAddr4Exclusions(t *testing.T) {
	sp := addr4Space()
	c, err := compile(sp)
	if err != nil {
		t.Fatal(err)
	}
	cases := []struct {
		description string
		a, b        string
		expected    bool // excluded
	}{
		{"negative: autojoin with a /32 peer", "peer=32", "flags=autojoin", true},
		{"negative: autojoin with a remote prefix", "peer=remote", "flags=autojoin", true},
		{"positive: autojoin without a peer", "peer=absent", "flags=autojoin", false},
		{"corner: another flag with a peer", "peer=32", "flags=noprefixroute", false},
	}
	for _, tc := range cases {
		t.Run(tc.description, func(t *testing.T) {
			k, err := sp.pair(exclusion{A: tc.a, B: tc.b})
			if err != nil {
				t.Fatal(err)
			}
			if got := c.excl[k]; got != tc.expected {
				t.Errorf("excluded %v, want %v", got, tc.expected)
			}
		})
	}
	var found bool
	for _, r := range sp.Derive(nil) {
		if r.ID == "addr4/mcpeer-1" {
			found = true
			if r.Expect.Errno != "EINVAL" || !strings.Contains(r.Cmd, " peer ") || !strings.Contains(r.Cmd, " autojoin ") {
				t.Errorf("%s: %s expects %+v", r.ID, r.Cmd, r.Expect)
			}
		}
	}
	if !found {
		t.Error("Derive wrote no autojoin-with-a-peer negative")
	}
}
