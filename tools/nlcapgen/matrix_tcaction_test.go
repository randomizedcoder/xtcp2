package main

import (
	"bytes"
	"fmt"
	"regexp"
	"strings"
	"testing"
)

// TestTcActionSpace pins the tc actions rows (gotc 12C): `actions
// list/get` of 13 kinds and `filter show` of filters holding them, over
// six fixtures, their batches and their refusals. Rows are keyed by
// command, fixture and batch file.
//
// go test ./tools/nlcapgen/ -run TestTcActionSpace
func TestTcActionSpace(t *testing.T) {
	rows, err := matrixRows(families["tcaction"](), matrixOpts{Seed: 1})
	if err != nil {
		t.Fatal(err)
	}
	if err := checkRows(rows); err != nil {
		t.Fatal(err)
	}
	if len(rows) != 138 {
		t.Errorf("%d rows, want 138 (66 commands plain and -j, 6 negatives)", len(rows))
	}
	byKey := map[string]row{}
	key := func(r row) string { return r.Cmd + "|" + r.Axes["fix"] + "|" + r.Axes["batch"] }
	for _, r := range rows {
		if !strings.HasPrefix(r.ID, "tcaction/") {
			t.Errorf("%s: not in tcaction/", r.ID)
		}
		if r.InitNS || r.Serial || r.Proto != "route" || strings.Join(r.Modules, ",") != strings.Join(tcActionModules, ",") {
			t.Errorf("%s: initns %t serial %t proto %q modules %v", r.ID, r.InitNS, r.Serial, r.Proto, r.Modules)
		}
		for _, m := range r.Modules {
			if !tdcGuestModules[m] {
				t.Errorf("%s: module %s is not one the guest has", r.ID, m)
			}
		}
		if _, dup := byKey[key(r)]; dup {
			t.Errorf("%s: %q twice", r.ID, key(r))
		}
		byKey[key(r)] = r
		if strings.Contains(r.Cmd, "-batch b.txt") {
			if len(r.Pre) == 0 || !strings.HasSuffix(r.Pre[len(r.Pre)-1].Cmd, "> b.txt") {
				t.Errorf("%s: batch row whose last pre step doesn't write b.txt", r.ID)
			}
		}
	}

	tests := []struct {
		description string
		key         string // cmd|fix|batch
		expected    string // kind rc refused-by errno | pre count
	}{
		// Positive.
		{"positive: police list", "tc actions ls action police|acpol|", "positive 0 none  | 9"},
		{"positive: police in JSON (AC14, AC15)", "tc -j actions ls action police|acpol|", "positive 0 none  | 9"},
		{"positive: a police get", "tc actions get action police index 1|acpol|", "positive 0 none  | 9"},
		{"positive: a filter's police and bound police (bind 1)", "tc filter show dev d0 ingress|acpol|", "positive 0 none  | 9"},
		{"positive: mirred to a block", "tc actions get action mirred index 4|acmir|", "positive 0 none  | 7"},
		{"positive: skbedit", "tc -j actions ls action skbedit|acskb|", "positive 0 none  | 4"},
		{"positive: vlan push_eth", "tc actions get action vlan index 4|acl2|", "positive 0 none  | 15"},
		{"positive: mpls (AC30)", "tc actions ls action mpls|acl2|", "positive 0 none  | 15"},
		{"positive: nat (AC22, AC23)", "tc -j actions ls action nat|acl3|", "positive 0 none  | 11"},
		{"positive: ctinfo (AC28)", "tc actions ls action ctinfo|acl3|", "positive 0 none  | 11"},
		{"positive: simple and sample", "tc actions ls action sample|acmisc|", "positive 0 none  | 7"},
		{"positive: three kinds in one filter", "tc -j filter show dev d0 ingress|acmisc|", "positive 0 none  | 7"},
		{"positive: two gets in a batch", "tc -batch b.txt|acmisc|two-gets", "positive 0 none  | 8"},
		{"positive: a get of three kinds",
			"tc actions get action gact index 1 action simple index 1 action sample index 1|acmisc|", "positive 0 none  | 7"},
		// Negative.
		{"negative: list with no kind", "tc actions ls|bare|", "negative 255 iproute2  | 0"},
		{"negative: get with no index", "tc actions get action gact|bare|", "negative 255 iproute2  | 0"},
		{"negative: a kind past 15 bytes (AC10)",
			"tc actions get action aaaaaaaaaaaaaaaaaa index 1|bare|", "negative 255 iproute2  | 0"},
		{"negative: no such index, then a second error (AC8)",
			"tc actions get action gact index 999999|acmisc|", "negative 255 kernel ENOENT | 7"},
		{"negative: a kind not loaded", "tc actions get action pedit index 1|bare|", "negative 255 kernel EINVAL | 0"},
		{"negative: a write", "tc actions delete action gact index 7|bare|", "negative 255 kernel ENOENT | 0"},
		// Boundary.
		{"boundary: no actions", "tc actions ls action gact|bare|", "boundary 0 none  | 0"},
		{"boundary: a pps-only policer (AC16)", "tc actions get action police index 4|acpol|", "boundary 0 none  | 9"},
		{"boundary: a rate past 2^32 bit/s", "tc -j actions get action police index 5|acpol|", "boundary 0 none  | 9"},
		{"boundary: 33 actions, past one dump message", "tc actions ls action gact|acmisc|", "boundary 0 none  | 7"},
		{"boundary: the last of 33", "tc actions get action gact index 33|acmisc|", "boundary 0 none  | 7"},
		{"boundary: a get of 32 actions, TCA_ACT_MAX_PRIO", "tc actions get" + actGetRange("gact", 2, 33) + "|acmisc|",
			"boundary 0 none  | 7"},
		// Corner.
		{"corner: get with no kind (AC9)", "tc actions get|bare|", "corner 0 none  | 0"},
		{"corner: get help sends a get (AC9)", "tc actions get help|bare|", "corner 0 none  | 0"},
		{"corner: an unknown kind exits 0 (AC6)", "tc actions ls action nosuch|bare|", "corner 0 none  | 0"},
		{"corner: words around the kind ignored (AC7)", "tc actions list foo gact bar|acmisc|", "corner 0 none  | 7"},
		{"corner: police control twice (AC15)", "tc -j actions get action police index 3|acpol|", "corner 0 none  | 9"},
		{"corner: a mirred to a deleted device (AC18)", "tc actions get action mirred index 5|acmir|", "corner 0 none  | 7"},
		{"corner: skbmod's etype (AC24, AC25)", "tc actions get action skbmod index 2|acl2|", "corner 0 none  | 15"},
		{"corner: skbmod under -o (AC26)", "tc -o actions ls action skbmod|acl2|", "corner 0 none  | 15"},
		{"corner: csum with no flags (AC29)", "tc -j actions get action csum index 3|acl3|", "corner 0 none  | 11"},
		{"corner: a terse dump", "tc -br actions ls action gact|acmisc|", "corner 0 none  | 7"},
		{"corner: since", "tc actions ls action gact since 100000000|acmisc|", "corner 0 none  | 7"},
		{"corner: get then list in a batch", "tc -batch b.txt|acl3|get-then-ls", "corner 0 none  | 12"},
		{"corner: a get of 33 actions, answered with 32 (K8)", "tc -j actions get" + actGetRange("gact", 1, 33) + "|acmisc|",
			"corner 0 none  | 7"},
	}
	for _, tc := range tests {
		t.Run(tc.description, func(t *testing.T) {
			r, ok := byKey[tc.key]
			if !ok {
				t.Fatalf("no row %q", tc.key)
			}
			kind, _, _ := strings.Cut(r.Desc, ":")
			got := fmt.Sprintf("%s %d %s %s | %d", kind, *r.Expect.RC, r.Expect.RefusedBy, r.Expect.Errno, len(r.Pre))
			if got != tc.expected {
				t.Errorf("got %q, want %q", got, tc.expected)
			}
		})
	}

	write := regexp.MustCompile(`\b(add|del|delete|change|replace|flush)\b`)
	fixtures := map[string]bool{}
	reads := map[string]string{}
	for k, r := range byKey {
		fixtures[r.Axes["fix"]] = true
		// Writes appear only as refusals.
		if write.MatchString(r.Cmd) && !strings.HasPrefix(r.Desc, "negative:") {
			t.Errorf("%s: %q writes but is not a negative", r.ID, r.Cmd)
		}
		reads[r.Cmd] = r.Axes["fix"]
		// Every positive command runs plain and -j.
		if strings.HasPrefix(r.Desc, "negative:") || strings.HasPrefix(r.Cmd, "tc -j ") {
			continue
		}
		cmd, rest, _ := strings.Cut(k, "|")
		if _, ok := byKey[strings.Replace(cmd, "tc ", "tc -j ", 1)+"|"+rest]; !ok {
			t.Errorf("%s: %q has no -j row", r.ID, cmd)
		}
	}
	for _, fix := range []string{"bare", "acpol", "acmir", "acskb", "acl2", "acl3", "acmisc"} {
		if !fixtures[fix] {
			t.Errorf("fixture %s has no row", fix)
		}
	}
	// Each of the 12 kinds is listed and got; gact too. A filter show
	// holds police, mirred, skbedit and gact at bind 1.
	for _, kind := range []string{"police", "mirred", "skbedit", "vlan", "csum", "nat", "simple", "connmark",
		"ctinfo", "sample", "skbmod", "mpls", "gact"} {
		listed, got := false, false
		for cmd := range reads {
			listed = listed || strings.HasSuffix(cmd, "ls action "+kind) || strings.Contains(cmd, "ls action "+kind+" ")
			got = got || strings.Contains(cmd, "get action "+kind+" index")
		}
		// A get of connmark, csum's flags, simple and sample is in the
		// batch or the list; the list is what pins their printers.
		if !listed {
			t.Errorf("kind %s has no list row", kind)
		}
		if !got && kind != "connmark" && kind != "simple" && kind != "sample" {
			t.Errorf("kind %s has no get row", kind)
		}
	}
	all := new(strings.Builder)
	for _, r := range rows {
		all.WriteString(r.Cmd + "\n")
		for _, p := range r.Pre {
			all.WriteString(p.Cmd + "\n")
		}
	}
	for _, s := range []string{"rate 40gbit", "pkts_rate 1000", "conform-exceed drop/pipe", "u32 match ip dst 10.0.0.1/32 police",
		"matchall action police index 1", "blockid 7", "ip link del d2", "hash-type", "inheritdsfield", "push_eth", "dec_ttl",
		"mac_push", "set etype 0xbeef", "swap mac", "csum index 3", "nat egress", "ctinfo dscp 0xfc000000 index 2",
		"cpmark", "sdata hello", "trunc 64", "cookie 0102", "-le 33", "aaaaaaaaaaaaaaaaaa"} {
		if s == "hash-type" {
			// tc 7.2.0 can't add it: unit-only (AC19).
			if strings.Contains(all.String(), s) {
				t.Errorf("a row or fixture has %q, which tc 7.2.0 refuses", s)
			}
			continue
		}
		if !strings.Contains(all.String(), s) {
			t.Errorf("no row or fixture has %q", s)
		}
	}

	// Determinism: the same seed writes the same bytes.
	var a, b bytes.Buffer
	for _, buf := range []*bytes.Buffer{&a, &b} {
		rows, err := matrixRows(families["tcaction"](), matrixOpts{Seed: 1})
		if err != nil {
			t.Fatal(err)
		}
		if err := writeRows(buf, nil, rows); err != nil {
			t.Fatal(err)
		}
	}
	if !bytes.Equal(a.Bytes(), b.Bytes()) {
		t.Error("two runs with seed 1 differ")
	}
}
