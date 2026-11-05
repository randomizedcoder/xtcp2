package main

import (
	"maps"
	"slices"
	"strings"
	"testing"
)

// pickVals takes each axis's first value, or the one named for it in
// names ("axis", "value", ...).
func pickVals(sp *space, names ...string) []value {
	vals := make([]value, len(sp.Axes))
	for i := range sp.Axes {
		vals[i] = sp.Axes[i].Values[0]
		for k := 0; k+1 < len(names); k += 2 {
			if sp.Axes[i].Name != names[k] {
				continue
			}
			for _, val := range sp.Axes[i].Values {
				if val.Name == names[k+1] {
					vals[i] = val
				}
			}
		}
	}
	return vals
}

// lastPre is a row's last setup step.
func lastPre(r row) string { return r.Pre[len(r.Pre)-1].Cmd }

func TestRuleJudge(t *testing.T) {
	base := map[string]string{}
	for _, ax := range rule4Space().Axes {
		base[ax.Name] = "absent"
	}
	base["verb"] = "add"
	with := func(kv ...string) map[string]string {
		m := maps.Clone(base)
		for i := 0; i+1 < len(kv); i += 2 {
			m[kv[i]] = kv[i+1]
		}
		return m
	}
	cases := []struct {
		description string
		pick        map[string]string
		expected    string // RefusedBy, "" for success
	}{
		{"negative: an add with no arguments", with(), "iproute2"},
		{"negative: a delete with no arguments", with("verb", "del"), "iproute2"},
		{"positive: an add with a preference", with("pref", "100"), ""},
		{"positive: an add with only an action", with("action", "nop"), ""},
		{"negative: a goto below the largest preference", with("action", "goto", "pref", "max"), "kernel"},
		{"positive: a goto above its preference", with("action", "goto", "pref", "100"), ""},
		{"corner: a goto with the preference the kernel picks", with("action", "goto"), ""},
	}
	for _, tc := range cases {
		t.Run(tc.description, func(t *testing.T) {
			exp, why := ruleJudge(tc.pick)
			got := ""
			if exp != nil {
				got = exp.RefusedBy
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

func TestBuildRule(t *testing.T) {
	sp := rule4Space()
	cases := []struct {
		description string
		vals        []value
		cmd         string
		preAdd      string // the last pre step, when it adds the rule
		table       string // the table axis, "" for none
	}{
		{"positive: from a prefix lookup 100", pickVals(sp, "from", "24", "action", "100"),
			"ip -4 rule add from 198.51.100.0/24 table 100", "", "100"},
		{"positive: an add with no action looks up main", pickVals(sp, "from", "24"),
			"ip -4 rule add from 198.51.100.0/24", "", "254"},
		{"corner: an l3mdev rule names no table", pickVals(sp, "pref", "100", "action", "l3mdev"),
			"ip -4 rule add pref 100 l3mdev", "", ""},
		{"corner: a typed action names no table", pickVals(sp, "pref", "100", "action", "blackhole"),
			"ip -4 rule add pref 100 blackhole", "", ""},
		{"positive: a delete first adds its rule", pickVals(sp, "verb", "del", "pref", "100"),
			"ip -4 rule del pref 100", "ip -4 rule add pref 100", ""},
		{"negative: a bare delete adds nothing first", pickVals(sp, "verb", "del"),
			"ip -4 rule del", "", ""},
		{"negative: a delete whose goto is refused adds nothing first", pickVals(sp, "verb", "del", "pref", "max", "action", "goto"),
			"ip -4 rule del pref 4294967295 goto 40000", "", ""},
	}
	for _, tc := range cases {
		t.Run(tc.description, func(t *testing.T) {
			r := sp.Build(tc.vals)
			if r.Cmd != tc.cmd {
				t.Errorf("cmd %q, want %q", r.Cmd, tc.cmd)
			}
			last := lastPre(r)
			if adds := strings.Contains(last, " rule add "); adds != (tc.preAdd != "") || (adds && last != tc.preAdd) {
				t.Errorf("last pre %q, want the add %q", last, tc.preAdd)
			}
			if r.Axes["table"] != tc.table || r.Axes["family"] != "4" {
				t.Errorf("axes %v, want table %q and family 4", r.Axes, tc.table)
			}
		})
	}
}

func TestNhJudge(t *testing.T) {
	cases := []struct {
		description string
		verb, id    string
		expected    string // errno, "" for success
	}{
		{"positive: an add without an id gets one", "add", "absent", ""},
		{"corner: an add with id 0 gets one", "add", "0", ""},
		{"positive: a replace with an id", "replace", "10", ""},
		{"negative: a replace without an id", "replace", "absent", "EINVAL"},
		{"negative: a replace of id 0", "replace", "0", "EINVAL"},
		{"positive: a delete with an id", "del", "1", ""},
		{"negative: a delete without an id", "del", "absent", "EINVAL"},
		{"boundary: a delete of id 0", "del", "0", "EINVAL"},
	}
	for _, tc := range cases {
		t.Run(tc.description, func(t *testing.T) {
			exp, why := nhJudge(map[string]string{"verb": tc.verb, "id": tc.id})
			got := ""
			if exp != nil {
				got = exp.Errno
				if why == "" {
					t.Error("a refusal with no reason")
				}
			}
			if got != tc.expected {
				t.Errorf("errno %q (%s), want %q", got, why, tc.expected)
			}
		})
	}
}

func TestBuildNexthop(t *testing.T) {
	nh4, grp := nh4Space(), nhGroupSpace()
	famVals := pickVals(grp)
	famVals[1] = v("inet", "-4", f("family", "4"))
	cases := []struct {
		description string
		sp          *space
		vals        []value
		cmd         string
		preAdd      string // the last pre step, when it adds what is deleted
		axes        map[string]string
	}{
		{"positive: a device nexthop", nh4, pickVals(nh4),
			"ip -4 nexthop add id 10 dev d0", "",
			map[string]string{"verb": "add", "id": "10", "oif": "d0", "family": "4"}},
		{"positive: a delete names only its id, after adding it", nh4, pickVals(nh4, "verb", "del", "kind", "via-dev", "protocol", "static"),
			"ip -4 nexthop del id 10", "ip -4 nexthop add id 10 via 192.0.2.1 dev d0 protocol static",
			map[string]string{"verb": "del", "id": "10", "family": "4"}},
		{"negative: a delete without an id adds nothing first", nh4, pickVals(nh4, "verb", "del", "id", "absent"),
			"ip -4 nexthop del", "",
			map[string]string{"verb": "del", "family": "4"}},
		{"positive: a group names no family, and its members are set up", grp, pickVals(grp),
			"ip nexthop add id 100 group 21", "",
			map[string]string{"verb": "add", "id": "100", "group": "21/1"}},
		{"positive: a group delete", grp, pickVals(grp, "verb", "del", "type", "mpath"),
			"ip nexthop del id 100", "ip nexthop add id 100 group 21 type mpath",
			map[string]string{"verb": "del", "id": "100"}},
		{"corner: the fam axis rewrites the head", grp, famVals,
			"ip -4 nexthop add id 100 group 21", "",
			map[string]string{"verb": "add", "id": "100", "group": "21/1", "family": "4"}},
	}
	for _, tc := range cases {
		t.Run(tc.description, func(t *testing.T) {
			r := tc.sp.Build(tc.vals)
			if r.Cmd != tc.cmd {
				t.Errorf("cmd %q, want %q", r.Cmd, tc.cmd)
			}
			last := lastPre(r)
			if adds := strings.Contains(last, "nexthop add id "+tc.axes["id"]+" ") && !strings.Contains(last, " && "); adds != (tc.preAdd != "") ||
				(adds && last != tc.preAdd) {
				t.Errorf("last pre %q, want the add %q", last, tc.preAdd)
			}
			if !maps.Equal(r.Axes, tc.axes) {
				t.Errorf("axes %v, want %v", r.Axes, tc.axes)
			}
			if tc.sp == grp && !slices.ContainsFunc(r.Pre, func(p preStep) bool { return p.Cmd == nhMembers }) {
				t.Error("the members are not set up")
			}
		})
	}
}

// nhMemberIDs are the ids nhMembers adds.
var nhMemberIDs = []string{"21", "22", "23", "26", "29", "31", "32"}

func TestMatrixFibRows(t *testing.T) {
	var rows []row
	for _, fam := range []string{"rule", "nexthop"} {
		r, err := matrixRows(families[fam](), matrixOpts{Seed: 1, Random: 40})
		if err != nil {
			t.Fatal(err)
		}
		if err := checkRows(r); err != nil {
			t.Fatal(err)
		}
		rows = append(rows, r...)
	}
	isRule := func(r row) bool { return strings.HasPrefix(r.ID, "rule") }
	neg := func(r row) bool { return strings.HasPrefix(r.Desc, "negative:") }
	cases := []struct {
		description string
		check       func(r row) bool
	}{
		{"positive: a rule delete's setup adds the rule it deletes", func(r row) bool {
			return !isRule(r) || r.Axes["verb"] != "del" || neg(r) || strings.Contains(lastPre(r), " rule add ")
		}},
		{"positive: every rule add names a table, an action or l3mdev", func(r row) bool {
			return !isRule(r) || r.Axes["verb"] != "add" || neg(r) || r.Axes["table"] != "" || r.Axes["action"] != "" || r.Axes["l3mdev"] != ""
		}},
		{"positive: every rule names its family", func(r row) bool {
			return !isRule(r) || r.Axes["family"] == "4" || r.Axes["family"] == "6"
		}},
		{"positive: a nexthop delete's setup adds what it deletes", func(r row) bool {
			return isRule(r) || r.Axes["verb"] != "del" || neg(r) || strings.Contains(lastPre(r), " nexthop add id "+r.Axes["id"]+" ")
		}},
		{"negative: no positive row sends dst_port, which iproute2 7.2.0 lacks", func(r row) bool {
			return neg(r) || !strings.Contains(r.Cmd, "dst_port")
		}},
		{"negative: no group takes a member's id", func(r row) bool {
			return !strings.HasPrefix(r.ID, "nhgrp/") || !slices.Contains(nhMemberIDs, r.Axes["id"])
		}},
		{"negative: no positive group is resilient without buckets", func(r row) bool {
			return neg(r) || r.Axes["grp_type"] != "1" || r.Axes["verb"] == "del" || r.Axes["buckets"] != ""
		}},
		{"positive: a positive group's members are all set up", func(r row) bool {
			g, ok := r.Axes["group"]
			if !ok || neg(r) {
				return true
			}
			for e := range strings.SplitSeq(g, ";") {
				id, _, _ := strings.Cut(e, "/")
				if !slices.Contains(nhMemberIDs, id) {
					return false
				}
			}
			return true
		}},
		{"positive: a repeated add (EEXIST) names a preference or an id, so it meets the first", func(r row) bool {
			if !strings.Contains(r.ID, "/dup-") {
				return true
			}
			if isRule(r) {
				return r.Axes["priority"] != ""
			}
			return r.Axes["id"] != "" && r.Axes["id"] != "0"
		}},
		{"positive: a single nexthop's bad value follows a device lookup when it names a protocol", func(r row) bool {
			return isRule(r) || !neg(r) || !strings.Contains(r.Cmd, "protocol 256") || strings.Contains(r.Cmd, " dev d0 ")
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

func TestFibPredicates(t *testing.T) {
	nh4 := pickVals(nh4Space())
	viaDev := pickVals(nh4Space(), "kind", "via-dev")
	del := pickVals(nh4Space(), "verb", "del")
	cases := []struct {
		description string
		got         bool
		expected    bool
	}{
		{"positive: a rule with a preference meets itself again", ruleHasPref(&row{Axes: map[string]string{"priority": "100"}}), true},
		{"negative: a rule without one gets a new preference", ruleHasPref(&row{Axes: map[string]string{"table": "100"}}), false},
		{"positive: a nexthop with an id meets itself again", nhHasID(&row{Axes: map[string]string{"id": "10"}}), true},
		{"negative: a nexthop without an id gets a new one", nhHasID(&row{Axes: map[string]string{"oif": "d0"}}), false},
		{"corner: id 0 asks for a new id", nhHasID(&row{Axes: map[string]string{"id": "0"}}), false},
		{"positive: an add of kind dev is a negative base", nhNegBase(nh4), true},
		{"negative: an add of another kind is not", nhNegBase(viaDev), false},
		{"negative: a delete is not", nhNegBase(del), false},
	}
	for _, tc := range cases {
		t.Run(tc.description, func(t *testing.T) {
			if tc.got != tc.expected {
				t.Errorf("got %v, want %v", tc.got, tc.expected)
			}
		})
	}
}

func TestNhMembers(t *testing.T) {
	var ids []string
	for step := range strings.SplitSeq(nhMembers, " && ") {
		_, rest, ok := strings.Cut(step, "nexthop add id ")
		if !ok {
			t.Fatalf("step %q adds no nexthop", step)
		}
		id, _, _ := strings.Cut(rest, " ")
		ids = append(ids, id)
	}
	cases := []struct {
		description string
		expected    []string
	}{
		{"positive: the members are the ids the group facts name, none the id axis takes", nhMemberIDs},
	}
	for _, tc := range cases {
		t.Run(tc.description, func(t *testing.T) {
			if !slices.Equal(ids, tc.expected) {
				t.Errorf("members %v, want %v", ids, tc.expected)
			}
			for _, v := range nhIDs("10").Values {
				if slices.Contains(ids, v.Facts["id"]) {
					t.Errorf("the id axis takes member id %s", v.Facts["id"])
				}
			}
		})
	}
}
