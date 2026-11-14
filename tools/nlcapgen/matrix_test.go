package main

import (
	"bytes"
	"maps"
	"strings"
	"testing"
)

// grid is a space of n axes named a, b, c ... each with values 0..m-1,
// whose rows' cmd lists the values, e.g. "a0 b2 c1".
func grid(n, m int) *space {
	sp := &space{Name: "grid"}
	for i := range n {
		name := string(rune('a' + i))
		ax := axis{Name: name}
		for j := range m {
			val := string(rune('0' + j))
			ax.Values = append(ax.Values, value{Name: val, Arg: name + val, Facts: map[string]string{name: val}})
		}
		sp.Axes = append(sp.Axes, ax)
	}
	sp.Build = func(vals []value) row {
		parts := make([]string, len(vals))
		for i, val := range vals {
			parts[i] = val.Arg
		}
		return row{Cmd: "x " + strings.Join(parts, " "), Expect: expect{RC: intp(0)}, Axes: mergeFacts(vals)}
	}
	return sp
}

// assignments decodes rows' axes back into one value name per axis.
func assignments(rows []row, sp *space) [][]string {
	out := make([][]string, 0, len(rows))
	for k := range rows {
		r := &rows[k]
		a := make([]string, len(sp.Axes))
		for i, ax := range sp.Axes {
			a[i] = r.Axes[ax.Name]
		}
		out = append(out, a)
	}
	return out
}

func positives(rows []row) []row {
	var out []row
	for i := range rows {
		if !strings.HasPrefix(rows[i].Desc, "negative:") {
			out = append(out, rows[i])
		}
	}
	return out
}

func TestMatrixPairwise(t *testing.T) {
	cases := []struct {
		description string
		n, m        int
		expected    int // most rows allowed
	}{
		{"positive: 3 axes of 3 values: every one of the 27 pairs", 3, 3, 15},
		{"positive: 4 axes of 3 values: every one of the 54 pairs", 4, 3, 15},
		{"boundary: 2 axes of 1 value: one row", 2, 1, 1},
		{"corner: 5 axes of 2 values", 5, 2, 10},
	}
	for _, tc := range cases {
		t.Run(tc.description, func(t *testing.T) {
			sp := grid(tc.n, tc.m)
			c, err := compile(sp)
			if err != nil {
				t.Fatal(err)
			}
			rows, infeasible := c.pairwise()
			if infeasible != 0 || len(rows) > tc.expected {
				t.Fatalf("%d rows (want <= %d), %d infeasible", len(rows), tc.expected, infeasible)
			}
			seen := map[pairKey]bool{}
			for _, a := range rows {
				for i := range a {
					for j := i + 1; j < len(a); j++ {
						seen[pairKey{i, a[i], j, a[j]}] = true
					}
				}
			}
			if want := len(c.pairs()); len(seen) != want {
				t.Errorf("%d pairs covered, want %d", len(seen), want)
			}
		})
	}
}

func TestMatrixEachChoice(t *testing.T) {
	cases := []struct {
		description string
		sp          *space
		expected    int // values that must appear
	}{
		{"positive: route4: every value of every axis", route4Space(), -1},
		{"positive: route6: every value of every axis", route6Space(), -1},
		{"boundary: one axis of one value", grid(1, 1), 1},
	}
	for _, tc := range cases {
		t.Run(tc.description, func(t *testing.T) {
			c, err := compile(tc.sp)
			if err != nil {
				t.Fatal(err)
			}
			want := tc.expected
			if want < 0 {
				want = 0
				for _, ax := range tc.sp.Axes {
					want += len(ax.Values)
				}
			}
			seen := map[[2]int]bool{}
			for _, a := range c.eachChoice() {
				for i, val := range a {
					seen[[2]int{i, val}] = true
				}
			}
			if len(seen) != want {
				t.Errorf("%d values appear, want %d", len(seen), want)
			}
		})
	}
}

func render(t *testing.T, sp *space, opts matrixOpts) []byte {
	t.Helper()
	rows, err := generate(sp, opts)
	if err != nil {
		t.Fatal(err)
	}
	var b bytes.Buffer
	if err := writeRows(&b, nil, rows); err != nil {
		t.Fatal(err)
	}
	return b.Bytes()
}

func TestMatrixSeed(t *testing.T) {
	cases := []struct {
		description string
		a, b        matrixOpts
		expected    bool // same bytes
	}{
		{"positive: the same seed gives the same file", matrixOpts{Seed: 1, Random: 20}, matrixOpts{Seed: 1, Random: 20}, true},
		{"positive: another seed gives other random rows", matrixOpts{Seed: 1, Random: 20}, matrixOpts{Seed: 2, Random: 20}, false},
		{"boundary: zero random rows ignore the seed", matrixOpts{Seed: 1}, matrixOpts{Seed: 2}, true},
	}
	for _, tc := range cases {
		t.Run(tc.description, func(t *testing.T) {
			got := bytes.Equal(render(t, route4Space(), tc.a), render(t, route4Space(), tc.b))
			if got != tc.expected {
				t.Errorf("same bytes %v, want %v", got, tc.expected)
			}
		})
	}
}

func TestMatrixRandomCount(t *testing.T) {
	cases := []struct {
		description string
		random      int
		expected    int
	}{
		{"boundary: zero random rows: only the deterministic ones", 0, 0},
		{"positive: ten random rows, less any that repeat an earlier row", 10, 10},
	}
	for _, tc := range cases {
		t.Run(tc.description, func(t *testing.T) {
			rows, err := generate(route4Space(), matrixOpts{Seed: 7, Random: tc.random})
			if err != nil {
				t.Fatal(err)
			}
			n := 0
			for _, r := range rows {
				if strings.Contains(r.ID, "/rn7-") {
					n++
				}
			}
			if n > tc.expected || (tc.expected > 0 && n == 0) {
				t.Errorf("%d random rows, want at most %d and some if any", n, tc.expected)
			}
		})
	}
}

func TestMatrixCompileErrors(t *testing.T) {
	cases := []struct {
		description string
		edit        func(sp *space)
		expected    string // in the error
	}{
		{"negative: an exclusion naming an unknown axis", func(sp *space) {
			sp.Excl = []exclusion{{A: "nosuch=0", B: "a=0"}}
		}, `no axis "nosuch"`},
		{"negative: an exclusion naming an unknown value", func(sp *space) {
			sp.Excl = []exclusion{{A: "a=9", B: "b=0"}}
		}, `axis a has no value "9"`},
		{"negative: an exclusion that is not axis=value", func(sp *space) {
			sp.Excl = []exclusion{{A: "a", B: "b=0"}}
		}, "is not axis=value"},
		{"corner: an exclusion naming one axis twice", func(sp *space) {
			sp.Excl = []exclusion{{A: "a=0", B: "a=1"}}
		}, "names one axis twice"},
		{"negative: a bad value for an unknown axis", func(sp *space) {
			sp.Bad = []badValue{{Axis: "nosuch"}}
		}, `no axis "nosuch"`},
		{"negative: an empty axis", func(sp *space) {
			sp.Axes[1].Values = nil
		}, `axis "b" is empty`},
		{"corner: the exclusions leave no row", func(sp *space) {
			sp.Axes = sp.Axes[:2]
			for _, a := range []string{"0", "1"} {
				for _, b := range []string{"0", "1"} {
					sp.Excl = append(sp.Excl, exclusion{A: "a=" + a, B: "b=" + b})
				}
			}
		}, "no rows"},
	}
	for _, tc := range cases {
		t.Run(tc.description, func(t *testing.T) {
			sp := grid(3, 2)
			tc.edit(sp)
			_, err := generate(sp, matrixOpts{})
			if err == nil || !strings.Contains(err.Error(), tc.expected) {
				t.Errorf("err %v, want it to contain %q", err, tc.expected)
			}
		})
	}
}

func TestMatrixNegatives(t *testing.T) {
	sp := grid(3, 2)
	sp.Excl = []exclusion{
		{A: "a=0", B: "b=1", Expect: &expect{RC: intp(2)}, Why: "the kernel refuses it"},
		{A: "b=0", B: "c=1", Why: "dropped, no row"},
	}
	sp.Bad = []badValue{
		{Axis: "a", Value: value{Name: "x", Arg: "ax"}, Expect: expect{RC: intp(255)}, Why: "out of range"},
		{Axis: "c", Value: value{Name: "y", Arg: "cy"}, Expect: expect{RC: intp(1)}, Why: "out of range"},
	}
	rows, err := generate(sp, matrixOpts{Seed: 1, Random: 30})
	if err != nil {
		t.Fatal(err)
	}
	base := positives(rows)[0]
	cases := []struct {
		description string
		id          string
		expected    string // the cmd
		rc          int
		changed     int // words differing from the first positive row; -1 not checked
	}{
		{"positive: an excluded pair with an expectation is one negative row", "grid/ng-001", "x a0 b1", 2, -1},
		{"positive: a bad value changes one axis of the first row", "grid/ng-002", "", 255, 1},
		{"positive: another bad value, another axis, one change", "grid/ng-003", "", 1, 1},
	}
	byID := map[string]row{}
	for _, r := range rows {
		byID[r.ID] = r
	}
	for _, tc := range cases {
		t.Run(tc.description, func(t *testing.T) {
			r, ok := byID[tc.id]
			if !ok {
				t.Fatalf("no row %s", tc.id)
			}
			if !strings.HasPrefix(r.Desc, "negative:") || *r.Expect.RC != tc.rc {
				t.Errorf("desc %q rc %d, want negative: and rc %d", r.Desc, *r.Expect.RC, tc.rc)
			}
			if tc.expected != "" && !strings.HasPrefix(r.Cmd, tc.expected) {
				t.Errorf("cmd %q, want it to start %q", r.Cmd, tc.expected)
			}
			if tc.changed >= 0 {
				if n := wordsDiffer(base.Cmd, r.Cmd); n != tc.changed {
					t.Errorf("%q differs from %q in %d words, want %d", r.Cmd, base.Cmd, n, tc.changed)
				}
			}
		})
	}
	t.Run("negative: no positive row holds an excluded pair", func(t *testing.T) {
		for _, a := range assignments(positives(rows), sp) {
			if (a[0] == "0" && a[1] == "1") || (a[1] == "0" && a[2] == "1") {
				t.Errorf("a positive row holds an excluded pair: %v", a)
			}
		}
	})
}

func wordsDiffer(a, b string) int {
	x, y := strings.Fields(a), strings.Fields(b)
	if len(x) != len(y) {
		return -1
	}
	n := 0
	for i := range x {
		if x[i] != y[i] {
			n++
		}
	}
	return n
}

func TestMatrixBoundaryRows(t *testing.T) {
	sp := grid(3, 3)
	sp.Axes[0].Values[2].Boundary = true
	sp.Axes[1].Values[0].Boundary = true
	sp.Axes[1].Values[2].Boundary = true
	c, err := compile(sp)
	if err != nil {
		t.Fatal(err)
	}
	cases := []struct {
		description string
		row         int
		expected    []int
	}{
		{"boundary: the first row takes each axis's first boundary value", 0, []int{2, 0, 0}},
		{"boundary: the second row cycles a with one boundary value, and takes b's second", 1, []int{2, 2, 0}},
	}
	rows := c.boundaryRows()
	if len(rows) != len(cases) {
		t.Fatalf("%d boundary rows, want %d", len(rows), len(cases))
	}
	for _, tc := range cases {
		t.Run(tc.description, func(t *testing.T) {
			for i, want := range tc.expected {
				if rows[tc.row][i] != want {
					t.Errorf("row %d = %v, want %v", tc.row, rows[tc.row], tc.expected)
				}
			}
		})
	}
}

func TestMatrixRouteRows(t *testing.T) {
	rows, err := matrixRows(families["route"](), matrixOpts{Seed: 1, Random: 40})
	if err != nil {
		t.Fatal(err)
	}
	if err := checkRows(rows); err != nil {
		t.Fatal(err)
	}
	cases := []struct {
		description string
		check       func(r row) bool
		expected    bool // every row passes
	}{
		{"positive: a delete's setup adds the route it deletes", func(r row) bool {
			if r.Axes["verb"] != "del" || strings.HasPrefix(r.Desc, "negative:") {
				return true
			}
			return strings.Contains(r.Pre[len(r.Pre)-1].Cmd, " route add ")
		}, true},
		{"positive: a nexthop list is the command's last argument", func(r row) bool {
			i := strings.Index(r.Cmd, " nexthop ")
			if i < 0 {
				return true
			}
			for _, w := range []string{" table ", " metric ", " proto ", " src ", " mtu ", " pref "} {
				if strings.Contains(r.Cmd[i:], w) {
					return false
				}
			}
			return true
		}, true},
		{"positive: every row names its family in its axes", func(r row) bool {
			return r.Axes["family"] == "4" || r.Axes["family"] == "6"
		}, true},
		{"positive: a row with an nhid has the nexthop it names set up", func(r row) bool {
			if r.Axes["nhid"] == "" {
				return true
			}
			for _, p := range r.Pre {
				if strings.Contains(p.Cmd, "nexthop add id "+r.Axes["nhid"]) {
					return true
				}
			}
			return false
		}, true},
		{"negative: no positive IPv4 reject route carries a device", func(r row) bool {
			reject := r.Axes["family"] == "4" && (r.Axes["type"] == "blackhole" || r.Axes["type"] == "prohibit")
			return !reject || r.Axes["oif"] == "" || strings.HasPrefix(r.Desc, "negative:")
		}, true},
	}
	for _, tc := range cases {
		t.Run(tc.description, func(t *testing.T) {
			for _, r := range rows {
				if tc.check(r) != tc.expected {
					t.Errorf("%s: %s", r.ID, r.Cmd)
				}
			}
		})
	}
}

func TestMatrixJudge(t *testing.T) {
	sp := grid(3, 2)
	// a0 b0 c0 is refused, though no pair of it is.
	sp.Judge = func(p map[string]string) (*expect, string) {
		if p["a"] == "0" && p["b"] == "0" && p["c"] == "0" {
			return &expect{RC: intp(2), Errno: "EINVAL"}, "all three zero"
		}
		return nil, ""
	}
	sp.Bad = []badValue{{Axis: "c", Value: value{Name: "y", Arg: "cy"}, Expect: expect{RC: intp(1)}, Why: "out of range"}}
	rows, err := generate(sp, matrixOpts{Seed: 1})
	if err != nil {
		t.Fatal(err)
	}
	var judged, bad *row
	for i := range rows {
		switch {
		case strings.HasPrefix(rows[i].Cmd, "x a0 b0 c0"):
			judged = &rows[i]
		case strings.HasSuffix(rows[i].Cmd, " cy"):
			bad = &rows[i]
		}
	}
	cases := []struct {
		description string
		got         bool
		expected    bool
	}{
		{"positive: the judged row is generated", judged != nil, true},
		{"positive: the judged row is a negative with the judge's reason",
			judged != nil && judged.Desc == "negative: a=0 b=0 c=0: all three zero", true},
		{"positive: the judged row carries the judge's expectation",
			judged != nil && *judged.Expect.RC == 2 && judged.Expect.Errno == "EINVAL", true},
		{"negative: a bad value is not built on the judged row",
			bad != nil && strings.HasPrefix(bad.Cmd, "x a0 b0 "), false},
	}
	for _, tc := range cases {
		t.Run(tc.description, func(t *testing.T) {
			if tc.got != tc.expected {
				t.Errorf("got %v, want %v", tc.got, tc.expected)
			}
		})
	}
}

func TestRouteJudge(t *testing.T) {
	base := map[string]string{"verb": "add", "type": "absent", "scope": "absent", "nh": "dev"}
	with := func(kv ...string) map[string]string {
		m := maps.Clone(base)
		for i := 0; i+1 < len(kv); i += 2 {
			m[kv[i]] = kv[i+1]
		}
		return m
	}
	v4 := routeJudge(route4AddVerdict, nil)
	v6 := routeJudge(nil, route6DelVerdict)
	cases := []struct {
		description string
		judge       func(map[string]string) (*expect, string)
		pick        map[string]string
		expected    string // errno, "" for success
	}{
		{"positive: a unicast route on a device", v4, with(), ""},
		{"positive: a gateway at the default universe scope", v4, with("nh", "via"), ""},
		{"negative: host scope with multipath", v4, with("scope", "host", "nh", "multipath"), "EINVAL"},
		{"negative: host scope with a gateway", v4, with("scope", "host", "nh", "via-dev"), "EINVAL"},
		{"negative: host scope with an IPv6 gateway", v4, with("scope", "host", "nh", "via6"), "EINVAL"},
		{"negative: a local route's host scope with an nhid", v4, with("type", "local", "nh", "nhid"), "EINVAL"},
		{"negative: link scope looks a gateway up and finds none", v4, with("scope", "link", "nh", "via"), "ENETUNREACH"},
		{"negative: a broadcast route's link scope with multipath", v4, with("type", "broadcast", "nh", "multipath"), "ENETUNREACH"},
		{"negative: link scope with onlink", v4, with("type", "multicast", "nh", "onlink"), "EINVAL"},
		{"boundary: link scope takes an IPv6 gateway", v4, with("scope", "link", "nh", "via6"), ""},
		{"boundary: link scope takes an nhid", v4, with("scope", "link", "nh", "nhid"), ""},
		{"corner: a reject route skips the scope checks, even with an nhid", v4,
			with("type", "blackhole", "scope", "host", "nh", "nhid"), ""},
		{"positive: a delete whose add succeeds", v4, with("verb", "del", "nh", "via"), ""},
		{"negative: a delete whose add is refused finds nothing", v4,
			with("verb", "del", "scope", "host", "nh", "via"), "ESRCH"},
		{"positive: an IPv6 reject route is added on a device", v6, with("type", "prohibit"), ""},
		{"negative: an IPv6 reject route deleted by its device", v6, with("verb", "del", "type", "prohibit"), "ESRCH"},
		{"negative: an IPv6 reject route deleted by its gateway", v6,
			with("verb", "del", "type", "throw", "nh", "via"), "ESRCH"},
		{"boundary: an IPv6 reject route deleted by its nhid", v6,
			with("verb", "del", "type", "throw", "nh", "nhid"), ""},
		{"positive: an IPv6 unicast route deleted by its device", v6, with("verb", "del"), ""},
	}
	for _, tc := range cases {
		t.Run(tc.description, func(t *testing.T) {
			exp, why := tc.judge(tc.pick)
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

func TestBuildRouteOrder(t *testing.T) {
	sp := route4Space()
	pick := func(names ...string) []value {
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
	cases := []struct {
		description string
		vals        []value
		cmd         string
		preAdd      bool // the last pre step adds the route
	}{
		{"positive: the type comes just before the destination", pick("type", "unreachable", "nh", "none"),
			"ip -4 route add unreachable 198.51.100.0/24", false},
		{"corner: to comes before the type", pick("dst", "to", "type", "unreachable", "nh", "none"),
			"ip -4 route add to unreachable 203.0.113.0/24", false},
		{"positive: a delete first adds its route", pick("verb", "del", "nh", "via"),
			"ip -4 route del 198.51.100.0/24 via 192.0.2.1", true},
		{"negative: a delete whose add is refused has no add", pick("verb", "del", "scope", "host", "nh", "via"),
			"ip -4 route del 198.51.100.0/24 scope host via 192.0.2.1", false},
	}
	for _, tc := range cases {
		t.Run(tc.description, func(t *testing.T) {
			r := sp.Build(tc.vals)
			if r.Cmd != tc.cmd {
				t.Errorf("cmd %q, want %q", r.Cmd, tc.cmd)
			}
			last := r.Pre[len(r.Pre)-1].Cmd
			if got := strings.Contains(last, " route add "); got != tc.preAdd {
				t.Errorf("last pre %q adds the route: %v, want %v", last, got, tc.preAdd)
			}
		})
	}
}
