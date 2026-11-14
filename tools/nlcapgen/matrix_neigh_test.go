package main

import (
	"strings"
	"testing"
)

func TestNeighJudge(t *testing.T) {
	sp := neigh4Space()
	pick := func(kv ...string) map[string]string {
		m := pickOf(sp.Axes, sp.assign(nil))
		for i := 0; i+1 < len(kv); i += 2 {
			m[kv[i]] = kv[i+1]
		}
		return m
	}
	cases := []struct {
		description string
		pick        map[string]string
		expected    string // RefusedBy and errno, "" for success
	}{
		{"negative: a permanent entry with no link-layer address", pick(), "kernel EINVAL"},
		{"positive: a permanent entry with one", pick("lladdr", "mac"), ""},
		{"negative: lladdr null sends none", pick("lladdr", "null"), "kernel EINVAL"},
		{"corner: a multicast destination starts NUD_NOARP and needs none", pick("dst", "mcast"), ""},
		{"corner: so does a broadcast one", pick("dst", "bcast"), ""},
		{"positive: an invalid state needs none", pick("nud", "incomplete"), ""},
		{"positive: use returns before the check", pick("flags", "use"), ""},
		{"positive: managed sets NUD_NONE", pick("flags", "managed", "nud", "perm"), ""},
		{"negative: extern_valid in an invalid state", pick("flags", "extern_valid", "nud", "failed", "lladdr", "mac"), "kernel EINVAL"},
		{"negative: extern_valid with nud none", pick("flags", "extern_valid", "nud", "none"), "kernel EINVAL"},
		{"positive: extern_valid in a valid state with an address", pick("flags", "extern_valid", "lladdr", "mac"), ""},
		{"positive: a proxy entry takes anything else", pick("dst", "proxy"), ""},
		{"negative: a proxy entry with managed", pick("dst", "proxy", "flags", "managed"), "kernel EINVAL"},
		{"negative: a proxy entry with extern_valid", pick("dst", "proxy", "flags", "extern_valid"), "kernel EINVAL"},
		{"negative: deleting what could not be added", pick("verb", "del"), "kernel ENOENT"},
		{"negative: changing what could not be added", pick("verb", "change"), "kernel ENOENT"},
		{"boundary: replace refuses as add does", pick("verb", "replace"), "kernel EINVAL"},
	}
	for _, tc := range cases {
		t.Run(tc.description, func(t *testing.T) {
			exp, why := neighJudge(tc.pick)
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

func TestBuildNeigh(t *testing.T) {
	sp4, sp6 := neigh4Space(), neigh6Space()
	cases := []struct {
		description string
		sp          *space
		names       map[string]string
		cmd         string
		preAdd      string // the last pre step, when it adds the entry
		readback    string
		state       string
	}{
		{"positive: an add names the device last, and is permanent unless told", sp4, map[string]string{"lladdr": "mac"},
			"ip -4 neigh add 192.0.2.1 lladdr 02:00:00:00:00:01 dev d0", "", "ip -4 neigh show dev d0", "128"},
		{"positive: a delete first adds its entry", sp6, map[string]string{"verb": "del", "lladdr": "mac", "nud": "stale"},
			"ip -6 neigh del 2001:db8:1::1 lladdr 02:00:00:00:00:01 nud stale dev d0",
			"ip -6 neigh add 2001:db8:1::1 lladdr 02:00:00:00:00:01 nud stale dev d0", "ip -6 neigh show dev d0", "4"},
		{"negative: a change whose add is refused adds nothing first", sp4, map[string]string{"verb": "change"},
			"ip -4 neigh change 192.0.2.1 dev d0", "", "ip -4 neigh show dev d0", "128"},
		{"corner: a proxy entry reads back the proxy table", sp4, map[string]string{"dst": "proxy"},
			"ip -4 neigh add proxy 192.0.2.50 dev d0", "", "ip -4 neigh show proxy dev d0", "128"},
		{"corner: managed after nud wins the state", sp6, map[string]string{"nud": "reachable", "flags": "managed"},
			"ip -6 neigh add 2001:db8:1::1 nud reachable managed dev d0", "", "ip -6 neigh show dev d0", "0"},
	}
	for _, tc := range cases {
		t.Run(tc.description, func(t *testing.T) {
			r := tc.sp.Build(tc.sp.assign(tc.names))
			if r.Cmd != tc.cmd {
				t.Errorf("cmd %q, want %q", r.Cmd, tc.cmd)
			}
			last := lastPre(r)
			if adds := strings.Contains(last, " neigh add "); adds != (tc.preAdd != "") || (adds && last != tc.preAdd) {
				t.Errorf("last pre %q, want the add %q", last, tc.preAdd)
			}
			if r.Readback[0].Cmd != tc.readback || r.Axes["state"] != tc.state || r.Pre[0].Cmd != neighSetup {
				t.Errorf("readback %q state %q first pre %q", r.Readback[0].Cmd, r.Axes["state"], r.Pre[0].Cmd)
			}
		})
	}
}

func TestNeighCanDup(t *testing.T) {
	cases := []struct {
		description string
		axes        map[string]string
		expected    bool
	}{
		{"positive: an entry added twice is EEXIST", map[string]string{"dst": "192.0.2.1"}, true},
		{"negative: a proxy entry added twice is found, not refused", map[string]string{"dst": "192.0.2.50", "proxy": "1"}, false},
		{"boundary: a row with no axes", nil, true},
	}
	for _, tc := range cases {
		t.Run(tc.description, func(t *testing.T) {
			if got := neighCanDup(&row{Axes: tc.axes}); got != tc.expected {
				t.Errorf("got %v, want %v", got, tc.expected)
			}
		})
	}
}

func TestDeriveNeighChangeGone(t *testing.T) {
	ok := func(id, verb, proxy string) row {
		ax := map[string]string{"verb": verb}
		if proxy != "" {
			ax["proxy"] = proxy
		}
		return row{ID: id, Desc: "corner: x", Cmd: "ip -4 neigh " + verb + " y", Axes: ax,
			Pre: []preStep{{Cmd: neighSetup}, {Cmd: "ip -4 neigh add y"}}, Expect: expect{RC: intp(0)}}
	}
	refused := ok("n/c9", "change", "")
	refused.Expect.RC = intp(2)
	pos := []row{ok("n/a1", "add", ""), ok("n/c1", "change", "1"), refused, ok("n/c2", "change", ""), ok("n/c3", "change", ""),
		ok("n/c4", "change", ""), ok("n/c5", "change", "")}
	out := deriveNeighChangeGone("n", pos)
	cases := []struct {
		description string
		got, want   string
	}{
		{"positive: three rows at most", string(rune('0' + len(out))), "3"},
		{"positive: the first is the first plain change that succeeded", out[0].Cmd + " " + out[0].ID, "ip -4 neigh change y n/chgone-1"},
		{"negative: a proxy change is not taken", out[0].Axes["proxy"], ""},
		{"positive: its add is dropped", lastPre(out[0]), neighSetup},
		{"positive: it expects ENOENT from the kernel", out[0].Expect.Errno + " " + string(out[0].Expect.RefusedBy), "ENOENT kernel"},
		{"negative: it is a negative row", out[0].Desc[:9], "negative:"},
		{"corner: the source row keeps its add", lastPre(pos[3]), "ip -4 neigh add y"},
	}
	for _, tc := range cases {
		t.Run(tc.description, func(t *testing.T) {
			if tc.got != tc.want {
				t.Errorf("got %q, want %q", tc.got, tc.want)
			}
		})
	}
}

func TestMatrixNeighRows(t *testing.T) {
	rows, err := matrixRows(families["neigh"](), matrixOpts{Seed: 1, Random: 40})
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
			return (v != "change" && v != "del") || neg(r) || strings.Contains(lastPre(r), " neigh add ")
		}},
		{"positive: every row has a state", func(r row) bool { return r.Axes["state"] != "" }},
		{"negative: no positive non-proxy row stores a valid state without an address", func(r row) bool {
			valid := r.Axes["state"] != "0" && r.Axes["state"] != "1" && r.Axes["state"] != "32"
			noarp := r.Axes["dst"] == "192.0.2.255" || r.Axes["dst"] == "224.0.0.5" || r.Axes["dst"] == "ff02::1"
			handed := r.Axes["ntf"] == "1" || r.Axes["ntf_ext"] == "1"
			return neg(r) || r.Axes["proxy"] != "" || !valid || noarp || handed || r.Axes["lladdr"] != "" || r.Axes["verb"] == "del"
		}},
		{"positive: a proxy row reads back the proxy table", func(r row) bool {
			return r.Axes["proxy"] == "" || strings.Contains(r.Readback[0].Cmd, " proxy ")
		}},
		{"positive: every row runs on d0", func(r row) bool { return strings.HasSuffix(r.Cmd, " dev d0") }},
		{"positive: the change-of-nothing rows add nothing first", func(r row) bool {
			return !strings.Contains(r.ID, "/chgone-") || !strings.Contains(lastPre(r), " neigh add ")
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
	var chgone int
	for _, r := range rows {
		if strings.Contains(r.ID, "/chgone-") {
			chgone++
		}
	}
	if chgone == 0 {
		t.Error("no change-of-nothing (ENOENT) row")
	}
}
