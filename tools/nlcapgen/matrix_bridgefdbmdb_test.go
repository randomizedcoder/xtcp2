package main

import (
	"bytes"
	"fmt"
	"strings"
	"testing"
)

// fdbMdbSpaceRows generates a family's rows twice (determinism), checks
// them, and keys them by command, fixture and batch name.
func fdbMdbSpaceRows(t *testing.T, family string) ([]row, map[string]*row) {
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
		if r.InitNS || r.Serial || r.Proto != "route" || strings.Join(r.Modules, ",") != "bridge,vxlan" {
			t.Errorf("%s: initns %t serial %t proto %q modules %v", r.ID, r.InitNS, r.Serial, r.Proto, r.Modules)
		}
		if _, dup := byKey[k]; dup {
			t.Errorf("%s: %q twice", r.ID, k)
		}
		byKey[k] = r
		// Read spaces: no captured command writes.
		for _, w := range []string{" add ", " del ", " replace ", " append ", " flush"} {
			if strings.Contains(r.Cmd+" ", w) {
				t.Errorf("%s: %q writes", r.ID, r.Cmd)
			}
		}
		// A batch row's file is its last pre step's.
		if strings.Contains(r.Cmd, "-batch b.txt") {
			if len(r.Pre) == 0 || !strings.HasSuffix(r.Pre[len(r.Pre)-1].Cmd, "> b.txt") {
				t.Errorf("%s: batch row whose last pre step doesn't write b.txt", r.ID)
			}
		}
	}
	// Every non-negative command runs plain and -j.
	for k, r := range byKey {
		if strings.HasPrefix(r.Desc, "negative:") || strings.Contains(r.Cmd, " -j ") {
			continue
		}
		cmd, rest, _ := strings.Cut(k, "|")
		if _, ok := byKey[strings.Replace(cmd, "bridge ", "bridge -j ", 1)+"|"+rest]; !ok {
			t.Errorf("%s: %q has no -j row", r.ID, cmd)
		}
	}
	return rows, byKey
}

type fdbMdbSpaceCase struct {
	description string
	key         string // cmd|fix|batch
	expected    string // kind rc refused-by errno extack | pre count
}

func runFdbMdbSpaceCases(t *testing.T, byKey map[string]*row, tests []fdbMdbSpaceCase) {
	t.Helper()
	for _, tc := range tests {
		t.Run(tc.description, func(t *testing.T) {
			r, ok := byKey[tc.key]
			if !ok {
				t.Fatalf("no row %q", tc.key)
			}
			kind, _, _ := strings.Cut(r.Desc, ":")
			got := fmt.Sprintf("%s %d %s %s %s | %d", kind, *r.Expect.RC, r.Expect.RefusedBy, r.Expect.Errno, r.Expect.Extack, len(r.Pre))
			if got != tc.expected {
				t.Errorf("got %q, want %q", got, tc.expected)
			}
		})
	}
}

// TestBridgefdbSpace pins the fdb read rows (gobridge 7C): fdb show and
// get, plain and -j, over the fixed-MAC fdb and fdbvx fixtures.
//
// go test ./tools/nlcapgen/ -run TestBridgefdbSpace
func TestBridgefdbSpace(t *testing.T) {
	rows, byKey := fdbMdbSpaceRows(t, "bridgefdb")
	if len(rows) != 94 {
		t.Errorf("%d rows, want 94 (37 commands plain and -j, 20 negatives)", len(rows))
	}
	// fdbVx orders its nexthop group before the nhid entry that uses it.
	nh, nhid := -1, -1
	for i, s := range fdbVxFix {
		if strings.Contains(s, "ip nexthop add id 10 group 1 fdb") {
			nh = i
		}
		if strings.Contains(s, "nhid 10") {
			nhid = i
		}
	}
	if nh < 0 || nhid < 0 || nh > nhid {
		t.Errorf("fdbVxFix: nexthop group at %d, nhid entry at %d", nh, nhid)
	}
	runFdbMdbSpaceCases(t, byKey, []fdbMdbSpaceCase{
		// Positive.
		{"positive: fdb show", "bridge fdb show|fdb|", "positive 0 none   | 10"},
		{"positive: -j fdb show", "bridge -j fdb show|fdb|", "positive 0 none   | 10"},
		{"positive: br filter", "bridge fdb show br br0|fdb|", "positive 0 none   | 10"},
		{"positive: fdb get by br and vlan", "bridge fdb get 02:00:00:00:0a:01 br br0 vlan 10|fdb|", "positive 0 none   | 10"},
		{"positive: vxlan remotes", "bridge -d fdb show dev vx0|fdbvx|", "positive 0 none   | 7"},
		// Negative.
		{"negative: unknown sub-command", "bridge fdb badcmd|bare|", "negative 255 ack-then-fail   | 0"},
		{"negative: a prefix of help in show", "bridge fdb show h|bare|", "negative 255 ack-then-fail   | 0"},
		{"negative: no such dev", "bridge fdb show dev nosuch|bare|", "negative 255 kernel ENODEV  | 0"},
		{"negative: no such entry", "bridge fdb get 02:00:00:00:0f:0f br br0|fdb|", "negative 254 kernel ENOENT Fdb entry not found | 10"},
		{"negative: K4 br with dev", "bridge fdb get 02:00:00:00:0a:01 br br0 dev d1|fdb|",
			"negative 254 kernel EINVAL Master and device are mutually exclusive | 10"},
		{"negative: dummy has no fdb get", "bridge fdb get 02:00:00:00:0a:07 dev d1 self|fdb|",
			"negative 254 kernel ENOTSUP Fdb get operation not supported by device | 10"},
		{"negative: F2 second vlan line is a duparg", "bridge -force -batch b.txt|fdb|sticky-vlan-duparg", "negative 255 ack-then-fail   | 11"},
		// Boundary.
		{"boundary: no fixture", "bridge fdb show|bare|", "boundary 0 none   | 0"},
		{"boundary: F5 state 0 is no filter", "bridge fdb show state 0|fdb|", "boundary 0 none   | 10"},
		{"boundary: vlan 0 is sent", "bridge fdb get 02:00:00:00:0a:01 br br0 vlan 0|fdb|", "negative 254 kernel EINVAL invalid vlan id | 10"},
		{"boundary: F13 vlan 65535 is unset", "bridge fdb get 02:00:00:00:0a:01 br br0 vlan 65535|fdb|", "negative 254 kernel ENOENT Fdb entry not found | 10"},
		{"boundary: F13 vlan 70000 wraps", "bridge fdb get 02:00:00:00:0a:01 br br0 vlan 70000|fdb|", "negative 254 kernel EINVAL invalid vlan id | 10"},
		{"boundary: F13 a 25-bit vni", "bridge fdb get 02:00:00:00:0a:01 br br0 vni 16777216|fdb|", "negative 255 ack-then-fail   | 10"},
		// Corner.
		{"corner: F5 state s is static", "bridge fdb show state s|fdb|", "corner 0 none   | 10"},
		{"corner: F4 dynamic keeps statics", "bridge fdb show dynamic|fdb|", "corner 0 none   | 10"},
		{"corner: F1 dev twice", "bridge fdb show dev d1 dev d2|fdb|", "corner 0 none   | 10"},
		{"corner: F11 -o ignored", "bridge -o fdb show|fdb|", "corner 0 none   | 10"},
		{"corner: fdb g is get", "bridge fdb g 02:00:00:00:0a:01 br br0 vlan 10|fdb|", "corner 0 none   | 10"},
		{"corner: F2 state ORs across lines", "bridge -batch b.txt|fdb|state-or", "corner 0 none   | 11"},
		{"corner: F2 dynamic on a get leaks into show", "bridge -j -batch b.txt|fdb|get-dynamic-leak", "corner 0 none   | 11"},
	})
}

// TestBridgemdbSpace pins the mdb read rows (gobridge 7C): mdb show and
// get, plain and -j, over the mdb and mdbvx fixtures.
//
// go test ./tools/nlcapgen/ -run TestBridgemdbSpace
func TestBridgemdbSpace(t *testing.T) {
	rows, byKey := fdbMdbSpaceRows(t, "bridgemdb")
	if len(rows) != 74 {
		t.Errorf("%d rows, want 74 (29 commands plain and -j, 16 negatives)", len(rows))
	}
	runFdbMdbSpaceCases(t, byKey, []fdbMdbSpaceCase{
		// Positive.
		{"positive: mdb show", "bridge mdb show|mdb|", "positive 0 none   | 10"},
		{"positive: -d -s with router ports", "bridge -j -d -s mdb show|mdb|", "positive 0 none   | 10"},
		{"positive: mdb get S,G", "bridge mdb get dev br0 grp 232.1.1.1 src 10.0.0.1 vid 10|mdb|", "positive 0 none   | 10"},
		{"positive: vxlan get with its src_vni", "bridge mdb get dev vx0 grp 239.1.1.2 src_vni 300|mdbvx|", "positive 0 none   | 5"},
		{"positive: vxlan get, no src_vni", "bridge mdb get dev vx0 grp 239.1.1.1|mdbvx|", "positive 0 none   | 5"},
		// Negative.
		{"negative: unknown sub-command", "bridge mdb badcmd|bare|", "negative 255 ack-then-fail   | 0"},
		{"negative: M2 mdb g is unknown", "bridge mdb g dev br0 grp 239.1.1.1|mdb|", "negative 255 ack-then-fail   | 10"},
		{"negative: no such entry", "bridge mdb get dev br0 grp 239.9.9.9|mdb|", "negative 254 kernel ENOENT MDB entry not found | 10"},
		{"negative: vxlan get missing the entry's src_vni", "bridge mdb get dev vx0 grp 239.1.1.2|mdbvx|", "negative 254 kernel ENOENT MDB entry not found | 5"},
		{"negative: no vid on a vlan_filtering bridge", "bridge mdb get dev br0 grp 239.2.2.2|mdb|", "negative 254 kernel ENOENT MDB entry not found | 10"},
		{"negative: no grp", "bridge mdb get dev br0|mdb|", "negative 255 ack-then-fail   | 10"},
		{"negative: M3 second vid line is a duparg", "bridge -force -batch b.txt|mdb|sticky-vid-duparg", "negative 255 ack-then-fail   | 11"},
		// Boundary.
		{"boundary: no fixture", "bridge mdb show|bare|", "boundary 0 none   | 0"},
		{"boundary: vid 5000", "bridge mdb get dev br0 grp 239.1.1.1 vid 5000|mdb|", "negative 254 kernel EINVAL Invalid entry VLAN id | 10"},
		// Corner.
		{"corner: M1 help ignored by show", "bridge mdb show help|mdb|", "corner 0 none   | 10"},
		{"corner: vid abc is no filter", "bridge mdb show vid abc|mdb|", "corner 0 none   | 10"},
		{"corner: a port's dev matches no bridge", "bridge mdb show dev d1|mdb|", "corner 0 none   | 10"},
		{"corner: F11 -s router lines and -o", "bridge -o -d -s mdb show|mdb|", "corner 0 none   | 10"},
		{"corner: M3 a stale vid 1 filters get's vid 10 answer away", "bridge -batch b.txt|mdb|sticky-vid-get", "corner 0 none   | 11"},
	})
}
