package main

import (
	"fmt"
	"slices"
	"strings"
	"testing"
	"testing/fstest"
)

// ksTestFile is the script description the miner tests use: one namespace
// named by $ns1, the tests listed in $TESTS by function name.
var ksTestFile = ksFile{Name: "t.sh", Space: "t", OwnNS: []string{"ns1"}, TestsVar: "TESTS"}

// ksScript wraps a test body as the only test of a script.
func ksScript(top, body string) string {
	return "TESTS=\"t1\"\n" + top + "t1()\n{\n" + body + "\n}\n"
}

// ksRowString is a row in short: its class, command, rc, pre steps after
// the namespace setup (~ marks a tolerated one), modules and whether it is
// marked unchecked.
func ksRowString(r *row) string {
	class, _, _ := strings.Cut(r.Desc, ":")
	rc := "-"
	if r.Expect.RC != nil {
		rc = fmt.Sprint(*r.Expect.RC)
	}
	pre := make([]string, 0, len(r.Pre)-1)
	for _, p := range r.Pre[1:] {
		mark := ""
		if len(p.OK) > 0 {
			mark = "~"
		}
		pre = append(pre, mark+p.Cmd)
	}
	s := fmt.Sprintf("%s %s rc=%s pre=[%s]", class, r.Cmd, rc, strings.Join(pre, "; "))
	if len(r.Modules) > 0 {
		s += " mods=" + strings.Join(r.Modules, ",")
	}
	if r.Source.Unchecked != "" {
		s += " unchecked"
	}
	return s
}

func TestMineScript(t *testing.T) {
	tests := []struct {
		description string
		top         string // top-level lines before the test function
		body        string
		want        []string // ksRowString of each row
		wantSkips   []string // "line: reason" of each skip
	}{
		{
			description: "positive: a literal $IP route add makes one row, $IP becoming ip",
			body:        "\t$IP route add 10.0.0.0/24 dev lo",
			want:        []string{"corner ip route add 10.0.0.0/24 dev lo rc=- pre=[]"},
		},
		{
			description: "positive: an $IP the script sets to the own namespace is dropped to ip",
			top:         "IP=\"$(which ip) -netns $ns1\"\n",
			body:        "\t$IP -6 route add 2001:db8::/64 dev lo",
			want:        []string{"corner ip -6 route add 2001:db8::/64 dev lo rc=- pre=[]"},
		},
		{
			description: "positive: a for over a literal list of 3 makes 3 rows",
			body:        "\tfor p in 1 2 3; do\n\t\t$IP route add 10.0.$p.0/24 dev lo\n\tdone",
			want: []string{
				"corner ip route add 10.0.1.0/24 dev lo rc=- pre=[]",
				"corner ip route add 10.0.2.0/24 dev lo rc=- pre=[ip route add 10.0.1.0/24 dev lo]",
				"corner ip route add 10.0.3.0/24 dev lo rc=- pre=[ip route add 10.0.1.0/24 dev lo; ip route add 10.0.2.0/24 dev lo]",
			},
		},
		{
			description: "corner: a for list ending in ; with do on the next line",
			body:        "\tfor p in 1 2;\n\tdo\n\t\t$IP route add 10.0.$p.0/24 dev lo\n\tdone",
			want: []string{
				"corner ip route add 10.0.1.0/24 dev lo rc=- pre=[]",
				"corner ip route add 10.0.2.0/24 dev lo rc=- pre=[ip route add 10.0.1.0/24 dev lo]",
			},
		},
		{
			description: "boundary: a for over $(seq 1 2) makes 2 rows",
			body:        "\tfor p in $(seq 1 2); do\n\t\t$IP rule add pref $p table 10\n\tdone",
			want: []string{
				"corner ip rule add pref 1 table 10 rc=- pre=[]",
				"corner ip rule add pref 2 table 10 rc=- pre=[ip rule add pref 1 table 10]",
			},
		},
		{
			description: "corner: a keyword newer than the workbench's iproute2 drops the rc and tolerates the pre step",
			body:        "\tset -e\n\t$IP nexthop add id 1 via 10.0.0.2 fdb dst_port 4790\n\t$IP nexthop add id 2 blackhole",
			want: []string{
				"corner ip nexthop add id 1 via 10.0.0.2 fdb dst_port 4790 rc=- pre=[] unchecked",
				"positive ip nexthop add id 2 blackhole rc=0 pre=[~ip nexthop add id 1 via 10.0.0.2 fdb dst_port 4790]",
			},
		},
		{
			description: "boundary: $(seq 2) counts from 1",
			body:        "\tfor p in $(seq 2); do\n\t\t$IP rule add pref $p table 10\n\tdone",
			want: []string{
				"corner ip rule add pref 1 table 10 rc=- pre=[]",
				"corner ip rule add pref 2 table 10 rc=- pre=[ip rule add pref 1 table 10]",
			},
		},
		{
			description: "negative: an if on an earlier rc is not walked",
			body: "\t$IP rule add dsfield 1 table 5 2>/dev/null\n\trc=$?\n\tlog_test $rc 2 \"ecn\"\n" +
				"\tif [ $rc -eq 0 ]; then\n\t\t$IP rule del dsfield 1 table 5\n\tfi",
			want:      []string{"negative ip rule add dsfield 1 table 5 rc=2 pre=[]"},
			wantSkips: []string{"8: under a test of an earlier rc"},
		},
		{
			description: "corner: || true under set -e is not required to succeed",
			body:        "\tset -e\n\t$IP route append 10.0.0.0/24 dev lo 2>/dev/null || true",
			want:        []string{"corner ip route append 10.0.0.0/24 dev lo rc=- pre=[]"},
		},
		{
			description: "positive: a variable naming ip netns exec runs a sysctl pre step",
			top:         "NS_EXEC=\"ip netns exec $ns1\"\n",
			body:        "\t$NS_EXEC sysctl -wq net.ipv6.conf.all.keep_addr_on_down=1\n\t$IP route add 10.0.0.0/24 dev lo",
			want:        []string{"corner ip route add 10.0.0.0/24 dev lo rc=- pre=[~sysctl -wq net.ipv6.conf.all.keep_addr_on_down=1]"},
		},
		{
			description: "positive: up and down in another namespace are kept for a device moved there",
			body: "\t$IP link add veth1 type veth peer name veth2\n\t$IP link set veth2 netns $peer up\n" +
				"\tip -netns $peer link set dev veth2 down\n\tip -netns $peer addr add 10.0.0.2/24 dev veth2\n" +
				"\tip -netns $peer link set veth1 down\n\t$IP route add 10.0.0.0/24 dev veth1",
			want: []string{"corner ip route add 10.0.0.0/24 dev veth1 rc=- pre=[ip link add veth1 type veth peer name veth2; " +
				"ip link set veth2 up; ip link set veth2 down] mods=veth"},
			wantSkips: []string{"7: runs in another namespace", "8: runs in another namespace"},
		},
		{
			description: "corner: a peer moved by link add is the one kept for up and down",
			body: "\t$IP link add veth1 type veth peer veth2 netns $ns2\n\tip -netns $ns2 link set veth2 up\n" +
				"\t$IP route add 10.0.0.0/24 dev veth1",
			want: []string{"corner ip route add 10.0.0.0/24 dev veth1 rc=- pre=[ip link add veth1 type veth peer veth2; " +
				"ip link set veth2 up] mods=veth"},
		},
		{
			description: "corner: a step after a skipped write loses its rc and is marked",
			body:        "\t$IP route add $X dev lo\n\trun_cmd \"$IP route add 10.0.0.0/24 dev lo\"\n\tlog_test $? 0 \"x\"",
			want:        []string{"corner ip route add 10.0.0.0/24 dev lo rc=- pre=[] unchecked"},
			wantSkips:   []string{"4: an unresolved variable $X"},
		},
		{
			description: "corner: a step after a skipped write is not required under set -e",
			body:        "\tset -e\n\t$IP route add $X dev lo\n\t$IP link add d0 type dummy\n\t$IP route add 10.0.0.0/24 dev d0",
			want:        []string{"corner ip route add 10.0.0.0/24 dev d0 rc=- pre=[~ip link add d0 type dummy] mods=dummy unchecked"},
			wantSkips:   []string{"5: an unresolved variable $X"},
		},
		{
			description: "positive: a skipped read does not cost later steps their rc",
			body:        "\t$IP route show $(x)\n\trun_cmd \"$IP route add 10.0.0.0/24 dev lo\"\n\tlog_test $? 0 \"x\"",
			want:        []string{"positive ip route add 10.0.0.0/24 dev lo rc=0 pre=[]"},
			wantSkips:   []string{"4: a command substitution"},
		},
		{
			description: "negative: a for over a computed list is skipped",
			body:        "\tfor p in $LIST; do\n\t\t$IP route add 10.0.$p.0/24 dev lo\n\tdone",
			wantSkips:   []string{"5: a loop over a computed list"},
		},
		{
			description: "negative: a computed command is skipped with a reason",
			body:        "\t$IP $(foo)",
			wantSkips:   []string{"4: a command substitution"},
		},
		{
			description: "corner: a line continuation is one command",
			body:        "\t$IP route add 10.0.0.0/24 \\\n\t\tdev lo",
			want:        []string{"corner ip route add 10.0.0.0/24 dev lo rc=- pre=[]"},
		},
		{
			description: "positive: run_cmd then log_test $? 2 attaches rc 2",
			body:        "\trun_cmd \"$IP route add 10.1.0.0/24 dev nosuch\"\n\tlog_test $? 2 \"no such device\"",
			want:        []string{"negative ip route add 10.1.0.0/24 dev nosuch rc=2 pre=[]"},
		},
		{
			description: "positive: log_test $? 0 makes the row positive",
			body:        "\trun_cmd \"$IP nexthop add id 1 blackhole\"\n\tlog_test $? 0 \"blackhole\"",
			want:        []string{"positive ip nexthop add id 1 blackhole rc=0 pre=[]"},
		},
		{
			description: "corner: in a run_cmd sequence only the last before log_test is checked",
			body: "\trun_cmd \"$IP nexthop add id 1 blackhole\"\n\trun_cmd \"$IP nexthop add id 2 group 1\"\n" +
				"\tlog_test $? 0 \"group\"",
			want: []string{
				"corner ip nexthop add id 1 blackhole rc=- pre=[]",
				"positive ip nexthop add id 2 group 1 rc=0 pre=[~ip nexthop add id 1 blackhole]",
			},
		},
		{
			description: "negative: a command in another namespace is skipped",
			body:        "\tip -netns $ns2 route add 10.0.0.0/24 dev lo",
			wantSkips:   []string{"4: runs in another namespace"},
		},
		{
			description: "positive: a link moved to another namespace stays here, unaddressed",
			body:        "\t$IP link add d0 type dummy netns $peer\n\t$IP route add 10.0.0.0/24 dev lo",
			want:        []string{"corner ip route add 10.0.0.0/24 dev lo rc=- pre=[ip link add d0 type dummy] mods=dummy"},
		},
		{
			description: "positive: an inlined function gets its arguments as $1",
			top:         "add_rt()\n{\n\t$IP route add $1 dev lo\n}\n",
			body:        "\tadd_rt 10.2.0.0/24",
			want:        []string{"corner ip route add 10.2.0.0/24 dev lo rc=- pre=[]"},
		},
		{
			description: "positive: ${1} in an inlined function is its first argument",
			top:         "create_ns()\n{\n\tlocal n=${1}\n\tip netns exec ${n} sysctl -qw net.ipv6.conf.all.keep_addr_on_down=1\n}\n",
			body:        "\tcreate_ns $ns1\n\tcreate_ns $peer\n\t$IP route add 10.0.0.0/24 dev lo",
			want:        []string{"corner ip route add 10.0.0.0/24 dev lo rc=- pre=[~sysctl -qw net.ipv6.conf.all.keep_addr_on_down=1]"},
			wantSkips:   []string{"5: runs in another namespace"},
		},
		{
			description: "corner: an inlined function's local is restored after the call",
			top:         "f()\n{\n\tlocal T=20\n}\n",
			body:        "\tT=10\n\tf\n\t$IP route add 10.0.0.0/24 dev lo table $T",
			want:        []string{"corner ip route add 10.0.0.0/24 dev lo table 10 rc=- pre=[]"},
		},
		{
			description: "positive: an if's then branch is walked, its else skipped",
			body: "\tif [ x = x ]; then\n\t\t$IP route add 10.0.0.0/24 dev lo\n\telse\n" +
				"\t\t$IP route add 10.9.0.0/24 dev lo\n\tfi",
			want:      []string{"corner ip route add 10.0.0.0/24 dev lo rc=- pre=[]"},
			wantSkips: []string{"7: an else branch"},
		},
		{
			description: "negative: a then branch that returns is a failure check, skipped",
			body: "\tif [ $? -ne 0 ]; then\n\t\t$IP route show\n\t\treturn 1\n\tfi\n" +
				"\t$IP route add 10.0.0.0/24 dev lo",
			want:      []string{"corner ip route add 10.0.0.0/24 dev lo rc=- pre=[]"},
			wantSkips: []string{"5: under a failure check"},
		},
		{
			description: "positive: under set -e a command must succeed",
			body:        "\tset -e\n\t$IP route add 10.0.0.0/24 dev lo",
			want:        []string{"positive ip route add 10.0.0.0/24 dev lo rc=0 pre=[]"},
		},
		{
			description: "corner: a log_test after set -e does not override its rc 0",
			body:        "\tset -e\n\t$IP route add 10.0.0.0/24 dev lo\n\tlog_test $? 2 \"x\"",
			want:        []string{"positive ip route add 10.0.0.0/24 dev lo rc=0 pre=[]"},
		},
		{
			description: "corner: an ip piped to grep is mined unchecked, and log_test is not attached to it",
			body:        "\t$IP route show | grep -q 10.0\n\tlog_test $? 0 \"found\"",
			want:        []string{"corner ip route show rc=- pre=[] unchecked"},
		},
		{
			description: "corner: an ip piped to a reader is a tolerated pre step",
			body:        "\t$IP route show | wc -l\n\t$IP route add 10.0.0.0/24 dev lo",
			want: []string{
				"corner ip route show rc=- pre=[] unchecked",
				"corner ip route add 10.0.0.0/24 dev lo rc=- pre=[~ip route show]",
			},
		},
		{
			description: "corner: only the word after type names a module",
			body:        "\t$IP link add veth type dummy\n\t$IP route add 10.0.0.0/24 dev veth",
			want:        []string{"corner ip route add 10.0.0.0/24 dev veth rc=- pre=[ip link add veth type dummy] mods=dummy"},
		},
		{
			description: "corner: || and a redirection make a pre step tolerated",
			body:        "\t$IP link add d0 type dummy || true\n\t$IP link set d0 up >/dev/null 2>&1\n\t$IP route add 10.0.0.0/24 dev d0",
			want: []string{
				"corner ip route add 10.0.0.0/24 dev d0 rc=- pre=[~ip link add d0 type dummy; ~ip link set d0 up] mods=dummy",
			},
		},
		{
			description: "positive: abbreviated objects are matched as ip matches them",
			body:        "\t$IP a add 192.0.2.1/24 dev lo\n\t$IP ro add 10.0.0.0/24 dev lo\n\t$IP nex add id 1 blackhole",
			want: []string{
				"corner ip ro add 10.0.0.0/24 dev lo rc=- pre=[ip a add 192.0.2.1/24 dev lo]",
				"corner ip nex add id 1 blackhole rc=- pre=[ip a add 192.0.2.1/24 dev lo; ip ro add 10.0.0.0/24 dev lo]",
			},
		},
		{
			description: "positive: a sysctl in the own namespace becomes a tolerated pre step only",
			body:        "\tip netns exec $ns1 sysctl -wq net.ipv4.ip_forward=1\n\t$IP route add 10.0.0.0/24 dev lo",
			want:        []string{"corner ip route add 10.0.0.0/24 dev lo rc=- pre=[~sysctl -wq net.ipv4.ip_forward=1]"},
		},
		{
			description: "negative: an ip netns add is the workbench's, skipped",
			body:        "\tip netns add ns9",
			wantSkips:   []string{"4: ip netns: the workbench gives every row its own namespace"},
		},
		{
			description: "boundary: a return ends the test, so nothing after it is mined",
			body:        "\treturn 0\n\t$IP route add 10.0.0.0/24 dev lo",
		},
		{
			description: "boundary: a help command is not a row",
			body:        "\t$IP route help",
		},
		{
			description: "negative: an unresolved variable is skipped",
			body:        "\t$IP route add $PFX dev lo",
			wantSkips:   []string{"4: an unresolved variable $PFX"},
		},
		{
			description: "negative: a batch file is skipped",
			body:        "\t$IP -batch /tmp/x",
			wantSkips:   []string{"4: a batch file"},
		},
		{
			description: "negative: a compound command is skipped",
			body:        "\t$IP route add 10.0.0.0/24 dev lo && echo ok",
			wantSkips:   []string{"4: a compound command"},
		},
		{
			description: "corner: a line under a while loop is skipped as under a condition",
			body:        "\twhile true; do\n\t\t$IP route add 10.0.0.0/24 dev lo\n\tdone",
			wantSkips:   []string{"5: under a condition"},
		},
	}
	for _, tc := range tests {
		t.Run(tc.description, func(t *testing.T) {
			rows, skips, err := mineScript(&ksTestFile, ksScript(tc.top, tc.body))
			if err != nil {
				t.Fatal(err)
			}
			got := make([]string, 0, len(rows))
			for i := range rows {
				if rows[i].Pre[0].Cmd != ksNsSetup {
					t.Errorf("row %s: the first pre step is %q, not the namespace setup", rows[i].ID, rows[i].Pre[0].Cmd)
				}
				got = append(got, ksRowString(&rows[i]))
			}
			if !slices.Equal(got, tc.want) {
				t.Errorf("rows:\n got %q\nwant %q", got, tc.want)
			}
			gotSkips := make([]string, 0, len(skips))
			for _, s := range skips {
				gotSkips = append(gotSkips, fmt.Sprintf("%d: %s", s.Line, s.Reason))
			}
			if !slices.Equal(gotSkips, tc.wantSkips) {
				t.Errorf("skips:\n got %q\nwant %q", gotSkips, tc.wantSkips)
			}
		})
	}
}

func TestMineScriptRowFields(t *testing.T) {
	src := ksScript("", "\t$IP route add 10.0.0.0/24 dev lo\n\trun_cmd \"$IP rule add pref 5 table 9\"\n\tlog_test $? 0 \"rule\"")
	rows, _, err := mineScript(&ksTestFile, src)
	if err != nil {
		t.Fatal(err)
	}
	tests := []struct {
		description string
		got         string
		expected    string
	}{
		{"positive: ids count within the group", rows[0].ID + " " + rows[1].ID, "t/t1-001 t/t1-002"},
		{"positive: the source names the file, line and function", rows[1].Source.Selftest, "t.sh:5 t1"},
		{"positive: a checked row's desc carries log_test's message", rows[1].Desc, "positive: rule (t.sh t1)"},
		{"corner: an unchecked row's desc says so", rows[0].Desc,
			"corner: a step (t.sh t1) does not check: ip route add 10.0.0.0/24 dev lo"},
		{"boundary: the first row's only pre step is the namespace setup", fmt.Sprint(len(rows[0].Pre)), "1"},
	}
	if len(rows) != 2 {
		t.Fatalf("got %d rows, want 2", len(rows))
	}
	for _, tc := range tests {
		t.Run(tc.description, func(t *testing.T) {
			if tc.got != tc.expected {
				t.Errorf("got %q, want %q", tc.got, tc.expected)
			}
		})
	}
}

func TestKsGroups(t *testing.T) {
	mapped := ksFile{Name: "m.sh", Space: "m", OwnNS: []string{"ns1"}, TestsVar: "TESTS", CaseMapped: true}
	tests := []struct {
		description string
		f           *ksFile
		src         string
		expected    string // the groups, or the error
	}{
		{"positive: tests listed by function name, in order, once each", &ksTestFile,
			"TESTS=\"b a b\"\na()\n{\n\ttrue\n}\nb()\n{\n\ttrue\n}\n", "b a"},
		{"positive: a case arm maps a test name to its function", &mapped,
			"TESTS=\"x y\"\nfx()\n{\n\ttrue\n}\nfy()\n{\n\ttrue\n}\ncase $t in\nx|xx) fx;;\ny) fy;;\nesac\n", "fx fy"},
		{"negative: no tests listed", &ksTestFile, "TESTS=\"\"\n", "t.sh: no tests in $TESTS"},
		{"negative: a test with no case arm", &mapped, "TESTS=\"x\"\nfx()\n{\n\ttrue\n}\n",
			"m.sh: test \"x\" has no case arm"},
		{"negative: a test naming no function", &ksTestFile, "TESTS=\"nosuch\"\n",
			"t.sh: test \"nosuch\" names no function nosuch"},
	}
	for _, tc := range tests {
		t.Run(tc.description, func(t *testing.T) {
			funcs, top := shFuncs(shLines(tc.src))
			vars := map[string]string{}
			w := &ksWalker{file: tc.f, funcs: funcs, vars: vars}
			for _, l := range top {
				w.assign(l.Text)
			}
			groups, err := ksGroups(tc.f, funcs, top, vars)
			got := strings.Join(groups, " ")
			if err != nil {
				got = err.Error()
			}
			if got != tc.expected {
				t.Errorf("got %q, want %q", got, tc.expected)
			}
		})
	}
}

func TestShLines(t *testing.T) {
	tests := []struct {
		description string
		src         string
		expected    []string // "no:text"
	}{
		{"positive: a comment is dropped", "a b # c\n", []string{"1:a b"}},
		{"positive: a backslash continues the line", "a \\\n  b\nc", []string{"1:a b", "3:c"}},
		{"corner: a quote left open continues the line", "a \"b\nc\"", []string{"1:a \"b c\""}},
		{"corner: a # inside quotes is not a comment", "a \"#b\"", []string{"1:a \"#b\""}},
		{"corner: a # inside a word is not a comment", "a b#c", []string{"1:a b#c"}},
		{"boundary: blank and comment-only lines vanish", "\n# x\n\n", nil},
	}
	for _, tc := range tests {
		t.Run(tc.description, func(t *testing.T) {
			lines := shLines(tc.src)
			got := make([]string, 0, len(lines))
			for _, l := range lines {
				got = append(got, fmt.Sprintf("%d:%s", l.No, l.Text))
			}
			if !slices.Equal(got, tc.expected) {
				t.Errorf("got %q, want %q", got, tc.expected)
			}
		})
	}
}

func TestShWords(t *testing.T) {
	tests := []struct {
		description string
		s           string
		expected    string // words joined by |, then ok
	}{
		{"positive: plain words", "a  b\tc", "a|b|c true"},
		{"positive: quotes group and are removed", `a "b c" 'd e'`, "a|b c|d e true"},
		{"positive: a backslash escapes", `a\ b`, "a b true"},
		{"corner: an empty quoted word is a word", `a ""`, "a| true"},
		{"negative: an open quote", `a "b`, "a|b false"},
		{"boundary: nothing", "", " true"},
	}
	for _, tc := range tests {
		t.Run(tc.description, func(t *testing.T) {
			words, ok := shWords(tc.s)
			if got := fmt.Sprintf("%s %v", strings.Join(words, "|"), ok); got != tc.expected {
				t.Errorf("got %q, want %q", got, tc.expected)
			}
		})
	}
}

func TestIPObject(t *testing.T) {
	tests := []struct {
		description string
		word        string
		expected    string
	}{
		{"positive: the full name", "route", "route"},
		{"positive: r is route, the first object it prefixes", "r", "route"},
		{"positive: ru is rule", "ru", "rule"},
		{"positive: n is neighbor, before netns and nexthop", "n", "neighbor"},
		{"positive: nex is nexthop", "nex", "nexthop"},
		{"positive: a is address", "a", "address"},
		{"negative: a word no object starts with", "zz", ""},
		{"negative: longer than the object", "routes", ""},
	}
	for _, tc := range tests {
		t.Run(tc.description, func(t *testing.T) {
			if got := ipObject(tc.word); got != tc.expected {
				t.Errorf("got %q, want %q", got, tc.expected)
			}
		})
	}
}

func TestMineKselftests(t *testing.T) {
	full := fstest.MapFS{}
	for _, f := range ksFiles {
		full[f.Name] = &fstest.MapFile{Data: []byte(ksScriptFor(&f))}
	}
	missing := fstest.MapFS{ksFiles[0].Name: full[ksFiles[0].Name]}
	tests := []struct {
		description string
		root        fstest.MapFS
		expected    string // the row ids, or the error
	}{
		{"positive: each script's rows, in ksFiles order", full, "fibt/t1-001 fibnh/t1-001 fibrule/t1-001"},
		{"negative: a script missing from the tree", missing, "open " + ksFiles[1].Name + ": file does not exist"},
	}
	for _, tc := range tests {
		t.Run(tc.description, func(t *testing.T) {
			rows, _, err := mineKselftests(tc.root)
			ids := make([]string, 0, len(rows))
			for i := range rows {
				ids = append(ids, rows[i].ID)
			}
			got := strings.Join(ids, " ")
			if err != nil {
				got = err.Error()
			}
			if got != tc.expected {
				t.Errorf("got %q, want %q", got, tc.expected)
			}
		})
	}
}

// ksScriptFor is a one-route script shaped as f's main loop expects.
func ksScriptFor(f *ksFile) string {
	s := f.TestsVar + "=\"t1\"\n"
	for _, p := range f.Prelude {
		s += p + "()\n{\n\ttrue\n}\n"
	}
	s += "t1()\n{\n\t$IP route add 10.0.0.0/24 dev lo\n}\n"
	if f.CaseMapped {
		s += "case $t in\nt1) t1;;\nesac\n"
	}
	return s
}

func TestMineScriptPrelude(t *testing.T) {
	f := ksFile{Name: "p.sh", Space: "p", OwnNS: []string{"ns1"}, Prelude: []string{"setup"}, TestsVar: "TESTS"}
	src := "TESTS=\"t1 t2\"\nsetup()\n{\n\t$IP link add d0 type dummy\n\t$IP $(x)\n}\n" +
		"t1()\n{\n\t$IP route add 10.1.0.0/24 dev d0\n}\nt2()\n{\n\t$IP rule add pref 2 table 2\n}\n"
	rows, skips, err := mineScript(&f, src)
	if err != nil {
		t.Fatal(err)
	}
	ids := make([]string, 0, len(rows))
	preCmds := make([]string, 0, len(rows))
	sk := make([]string, 0, len(skips))
	for i := range rows {
		ids = append(ids, rows[i].ID)
		preCmds = append(preCmds, rows[i].Pre[1].Cmd)
	}
	for _, s := range skips {
		sk = append(sk, fmt.Sprintf("%d %s: %s", s.Line, s.Func, s.Reason))
	}
	tests := []struct {
		description string
		got         []string
		expected    []string
	}{
		{"positive: each test is a group of its own", ids, []string{"p/t1-001", "p/t2-001"}},
		{"positive: the prelude runs before each group", preCmds, []string{"ip link add d0 type dummy", "ip link add d0 type dummy"}},
		{"corner: a prelude line skipped in each group is listed once", sk, []string{"5 setup: a command substitution"}},
	}
	for _, tc := range tests {
		t.Run(tc.description, func(t *testing.T) {
			if !slices.Equal(tc.got, tc.expected) {
				t.Errorf("got %q, want %q", tc.got, tc.expected)
			}
		})
	}
}
