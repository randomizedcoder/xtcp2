package main

import (
	"bytes"
	"fmt"
	"strings"
	"testing"
)

// tnlSpaceRows generates a family's rows twice (determinism), checks them
// and keys them by command, fixture and batch name. Each row must carry the
// family's modules and the tnlsys readback of its own arguments; a write
// verb may appear only in a negative row; and every non-negative command
// runs plain and -j.
func tnlSpaceRows(t *testing.T, family string, modules []string) ([]row, map[string]*row) {
	t.Helper()
	rows, err := matrixRows(families[family](), matrixOpts{Seed: 1})
	if err != nil {
		t.Fatal(err)
	}
	if err := checkRows(rows); err != nil {
		t.Fatal(err)
	}
	var a, b bytes.Buffer
	if err := writeRows(&a, nil, rows); err != nil {
		t.Fatal(err)
	}
	again, err := matrixRows(families[family](), matrixOpts{Seed: 1})
	if err != nil {
		t.Fatal(err)
	}
	if err := writeRows(&b, nil, again); err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(a.Bytes(), b.Bytes()) {
		t.Errorf("%s: two runs with seed 1 differ", family)
	}
	byKey := map[string]*row{}
	for i := range rows {
		r := &rows[i]
		k := r.Cmd + "|" + r.Axes["fix"] + "|" + r.Axes["batch"]
		if !strings.HasPrefix(r.ID, family+"/") {
			t.Errorf("%s: not in %s/", r.ID, family)
		}
		if r.InitNS || r.Proto != "route" || strings.Join(r.Modules, ",") != strings.Join(modules, ",") {
			t.Errorf("%s: initns %t proto %q modules %v", r.ID, r.InitNS, r.Proto, r.Modules)
		}
		if _, dup := byKey[k]; dup {
			t.Errorf("%s: %q twice", r.ID, k)
		}
		byKey[k] = r
		want := "tnlsnap -- " + strings.TrimPrefix(r.Cmd, "ip ")
		if len(r.Readback) != 1 || r.Readback[0].Name != "tnlsys" || r.Readback[0].Cmd != want {
			t.Errorf("%s: readback %+v, want tnlsys %q", r.ID, r.Readback, want)
		}
		neg := strings.HasPrefix(r.Desc, "negative:")
		for _, w := range []string{" add", " del", " change", " prl", " 6rd"} {
			if strings.Contains(r.Cmd+" ", w+" ") && !neg {
				t.Errorf("%s: %q writes outside a negative row", r.ID, r.Cmd)
			}
		}
		if neg && strings.Contains(r.Cmd, " -j ") {
			t.Errorf("%s: a negative row in -j", r.ID)
		}
		if strings.Contains(r.Cmd, "-batch") {
			if len(r.Pre) == 0 || !strings.HasSuffix(r.Pre[len(r.Pre)-1].Cmd, "> b.txt") {
				t.Errorf("%s: batch row whose last pre step doesn't write b.txt", r.ID)
			}
		}
	}
	for k, r := range byKey {
		if strings.HasPrefix(r.Desc, "negative:") || strings.Contains(r.Cmd, " -j ") {
			continue
		}
		cmd, rest, _ := strings.Cut(k, "|")
		if _, ok := byKey[strings.Replace(cmd, "ip ", "ip -j ", 1)+"|"+rest]; !ok {
			t.Errorf("%s: %q has no -j row", r.ID, cmd)
		}
	}
	return rows, byKey
}

type tnlSpaceCase struct {
	description string
	key         string // cmd|fix|batch
	expected    string // kind rc refused-by errno serial | pre count
}

func runTnlSpaceCases(t *testing.T, byKey map[string]*row, tests []tnlSpaceCase) {
	t.Helper()
	for _, tc := range tests {
		t.Run(tc.description, func(t *testing.T) {
			r, ok := byKey[tc.key]
			if !ok {
				t.Fatalf("no row %q", tc.key)
			}
			kind, _, _ := strings.Cut(r.Desc, ":")
			got := fmt.Sprintf("%s %d %s %s %t | %d", kind, *r.Expect.RC, r.Expect.RefusedBy, r.Expect.Errno, r.Serial, len(r.Pre))
			if got != tc.expected {
				t.Errorf("got %q, want %q", got, tc.expected)
			}
		})
	}
}

// TestTunnelSpace pins the ip tunnel and ip -6 tunnel read rows (goip 8C).
//
// go test ./tools/nlcapgen/ -run TestTunnelSpace
func TestTunnelSpace(t *testing.T) {
	rows, byKey := tnlSpaceRows(t, "tunnel", tunnelModules)
	if len(rows) != 171 {
		t.Errorf("%d rows, want 171 (72 commands plain and -j, 27 negatives)", len(rows))
	}
	for _, r := range rows {
		if r.Serial {
			t.Errorf("%s: serial; only tuntap -d rows walk /proc", r.ID)
		}
	}
	// isatap tunnels come from `ip tunnel add`, so that the kernel keeps
	// protocol IPPROTO_IPV6 and ip reads their routers (K2).
	if len(tnlPrl16Fix) != 3 || !strings.HasPrefix(tnlPrl16Fix[1], "ip tunnel add isa16 mode isatap") ||
		!strings.Contains(tnlPrl16Fix[2], "i=10; while [ $i -lt 26 ]") {
		t.Errorf("tnlPrl16Fix: want ip tunnel add isa16, then routers .10 to .25: %q", tnlPrl16Fix)
	}
	runTnlSpaceCases(t, byKey, []tnlSpaceCase{
		// Positive.
		{"positive: the v4 list", "ip tunnel show|tnl4|", "positive 0 none  false | 12"},
		{"positive: -j the v4 list", "ip -j tunnel show|tnl4|", "positive 0 none  false | 12"},
		{"positive: the v6 list", "ip -6 tunnel show|tnl6|", "positive 0 none  false | 6"},
		{"positive: v6 named, no final newline (T9)", "ip -6 tunnel show ip6t1|tnl6|", "positive 0 none  false | 6"},
		{"positive: a gre name takes the named path (T5)", "ip tunnel show gre1|tnl4|", "positive 0 none  false | 12"},
		{"positive: -s stats inside -j (T1)", "ip -j -s tunnel show|tnl4|", "positive 0 none  false | 12"},
		// Negative.
		{"negative: gre9 ENODEV is rc 1 (T5)", "ip tunnel show gre9|tnl4|", "negative 1 iproute2  false | 12"},
		{"negative: keys without a mode (T6)", "ip tunnel show key 5|tnl4|", "negative 1 iproute2  false | 12"},
		{"negative: unknown mode, exit(-1)", "ip tunnel show mode bogus|bare|", "negative 255 iproute2  false | 0"},
		{"negative: get_addr32 exit(1)", "ip tunnel show remote bogus|bare|", "negative 1 iproute2  false | 0"},
		{"negative: dev nosuch asks the kernel", "ip tunnel show dev nosuch|bare|", "negative 1 kernel ENODEV false | 0"},
		{"negative: a write is refused before any ioctl", "ip tunnel add|bare|", "negative 1 iproute2  false | 0"},
		{"negative: v6 named ENODEV", "ip -6 tunnel show nosuch|tnl6|", "negative 1 iproute2  false | 6"},
		// Boundary.
		{"boundary: fallback devices only", "ip tunnel show|bare|", "boundary 0 none  false | 0"},
		{"boundary: ttl 300, two newlines (T11)", "ip tunnel show ttl 300|bare|", "negative 255 iproute2  false | 0"},
		{"boundary: 16 routers, 15 printed (T8)", "ip tunnel show isa16|prl16|", "positive 0 none  false | 3"},
		{"boundary: a 21-bit flowlabel", "ip -6 tunnel show flowlabel 0x100000|bare|", "negative 255 iproute2  false | 0"},
		// Corner.
		{"corner: mode any is v6 (T10)", "ip tunnel show mode any|tnl4|", "corner 0 none  false | 12"},
		{"corner: ip6gre through the v4 named path (T19)", "ip tunnel show gre6x|tnl6|", "corner 0 none  false | 6"},
		{"corner: encaplimit 300 unchecked (T20)", "ip -6 tunnel show encaplimit 300|tnl6|", "corner 0 none  false | 6"},
		{"corner: the tunl alias", "ip tunl show|tnl4|", "corner 0 none  false | 12"},
		{"corner: mode any sticks across batch lines", "ip -batch b.txt|tnl4|any-sticks", "corner 0 none  false | 13"},
		{"corner: v6 named then the list, on stdin", "ip -batch - < b.txt|tnl6|v6-named-then-list", "corner 0 none  false | 7"},
		{"corner: external and Ethernet kinds", "ip tunnel show|ext|", "corner 0 none  false | 5"},
	})
}

// TestTuntapSpace pins the ip tuntap read rows (goip 8C).
//
// go test ./tools/nlcapgen/ -run TestTuntapSpace
func TestTuntapSpace(t *testing.T) {
	rows, byKey := tnlSpaceRows(t, "tuntap", tuntapModules)
	if len(rows) != 46 {
		t.Errorf("%d rows, want 46 (20 commands plain and -j, 6 negatives)", len(rows))
	}
	// A -d row walks /proc, which every namespace shares: it runs alone,
	// after killing earlier rows' holders. Other rows run in parallel.
	serial := 0
	for _, r := range rows {
		d := detailWords(strings.TrimPrefix(r.Cmd, "ip "))
		if r.Serial != d {
			t.Errorf("%s: serial %t, -d %t", r.ID, r.Serial, d)
		}
		if d {
			serial++
			if len(r.Pre) == 0 || r.Pre[0].Cmd != tunHolderKill {
				t.Errorf("%s: -d row whose first pre step isn't the holder kill", r.ID)
			}
		}
	}
	if serial != 10 {
		t.Errorf("%d serial rows, want 10", serial)
	}
	runTnlSpaceCases(t, byKey, []tnlSpaceCase{
		// Positive.
		{"positive: tuntap show", "ip tuntap show|tun|", "positive 0 none  false | 4"},
		{"positive: -j tuntap show", "ip -j tuntap show|tun|", "positive 0 none  false | 4"},
		{"positive: the tap alias", "ip tap show|tun|", "positive 0 none  false | 4"},
		{"positive: -d with holders (T15)", "ip -d tuntap show|proc|", "positive 0 none  true | 9"},
		// Negative.
		{"negative: unknown command", "ip tuntap bogus|bare|", "negative 255 iproute2  false | 0"},
		{"negative: add with no mode is a returned -1", "ip tuntap add|bare|", "negative 1 iproute2  false | 0"},
		{"negative: del with no mode is a returned -1", "ip tuntap del|bare|", "negative 1 iproute2  false | 0"},
		// Boundary.
		{"boundary: no tun devices", "ip tuntap show|bare|", "boundary 0 none  false | 0"},
		{"boundary: -d with no holders", "ip -d tuntap show|tun|", "positive 0 none  true | 5"},
		// Corner.
		{"corner: arguments ignored (T13)", "ip tuntap show dev tap0|tun|", "corner 0 none  false | 4"},
		{"corner: -o -d half one-line (T16)", "ip -o -d tuntap show|proc|", "corner 0 none  true | 9"},
		{"corner: another namespace's holder (T22)", "ip -d tuntap show|xns|", "corner 0 none  true | 7"},
		{"corner: batch on stdin", "ip -batch - < b.txt|tun|stdin", "corner 0 none  false | 5"},
	})
}

// TestDetailWords pins which option words make a tuntap row serial.
func TestDetailWords(t *testing.T) {
	tests := []struct {
		description string
		args        string
		expected    bool
	}{
		{"positive: -d", "-d tuntap show", true},
		{"positive: -d after -j", "-j -o -d tuntap", true},
		{"negative: no -d", "-j tuntap show", false},
		{"boundary: empty", "", false},
		{"corner: -d after the object is an argument", "tuntap show -d", false},
		{"corner: only the -d spelling (no row spells -details)", "-details tuntap", false},
	}
	for _, tc := range tests {
		t.Run(tc.description, func(t *testing.T) {
			if got := detailWords(tc.args); got != tc.expected {
				t.Errorf("detailWords(%q) = %t, want %t", tc.args, got, tc.expected)
			}
		})
	}
}
