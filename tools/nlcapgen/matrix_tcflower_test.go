package main

import (
	"bytes"
	"fmt"
	"regexp"
	"strings"
	"testing"
)

// TestTcFlowerSpace pins the tc flower rows (gotc 11C): `filter show/get`
// of flower filters over five fixtures, their batches and their refusals.
// Rows are keyed by command, fixture and batch file.
//
// go test ./tools/nlcapgen/ -run TestTcFlowerSpace
func TestTcFlowerSpace(t *testing.T) {
	rows, err := matrixRows(families["tcflower"](), matrixOpts{Seed: 1})
	if err != nil {
		t.Fatal(err)
	}
	if err := checkRows(rows); err != nil {
		t.Fatal(err)
	}
	if len(rows) != 97 {
		t.Errorf("%d rows, want 97 (44 commands plain and -j, 9 negatives)", len(rows))
	}
	byKey := map[string]row{}
	key := func(r row) string { return r.Cmd + "|" + r.Axes["fix"] + "|" + r.Axes["batch"] }
	for _, r := range rows {
		if !strings.HasPrefix(r.ID, "tcflower/") {
			t.Errorf("%s: not in tcflower/", r.ID)
		}
		if r.InitNS || r.Serial || r.Proto != "route" || strings.Join(r.Modules, ",") != strings.Join(tcFlowerModules, ",") {
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
		{"positive: link-layer keys", "tc filter show dev d0 ingress|fll2|", "positive 0 none  | 15"},
		{"positive: L3/L4 keys in JSON (FL4, FL5, FL6)", "tc -j filter show dev d0 ingress|fll3|", "positive 0 none  | 13"},
		{"positive: enc keys and options (FL1, FL2)", "tc -j filter show dev d0 ingress|flenc|", "positive 0 none  | 11"},
		{"positive: ct keys", "tc filter show dev d0 ingress|flct|", "positive 0 none  | 4"},
		{"positive: a geneve get", "tc filter get dev d0 ingress protocol ip pref 3 handle 1 flower|flenc|",
			"positive 0 none  | 11"},
		{"positive: a shared block", "tc filter show block 20|flmisc|", "positive 0 none  | 10"},
		{"positive: two gets in a batch", "tc -batch b.txt|fll3|two-gets", "positive 0 none  | 14"},
		// Negative.
		{"negative: a block get with no parent (FL22)",
			"tc filter get block 20 protocol ip pref 1 handle 1 flower|flmisc|", "negative 255 iproute2  | 10"},
		{"negative: a strict handle (FL16)",
			"tc filter get dev d0 ingress protocol ip pref 1 handle 1junk flower|fll3|", "negative 1 ack-then-fail  | 13"},
		{"negative: a handle past 2^32 (FL16)",
			"tc filter get dev d0 ingress protocol ip pref 1 handle 0x1ffffffff flower|fll3|", "negative 1 ack-then-fail  | 13"},
		{"negative: get's keys left behind (FL21)",
			"tc filter get dev d0 ingress protocol ip pref 1 handle 1 flower ip_proto tcp|fll3|", "negative 255 ack-then-fail  | 13"},
		{"negative: an unknown flower word",
			"tc filter get dev d0 ingress protocol ip pref 1 handle 1 flower bogus|fll3|", "negative 1 ack-then-fail  | 13"},
		{"negative: flower help",
			"tc filter get dev d0 ingress protocol ip pref 1 handle 1 flower help|fll3|", "negative 1 ack-then-fail  | 13"},
		{"negative: no such handle",
			"tc filter get dev d0 ingress protocol ip pref 1 handle 9 flower|fll3|", "negative 2 kernel ENOENT | 13"},
		{"negative: get with no handle",
			"tc filter get dev d0 ingress protocol ip pref 1 flower|fll3|", "negative 255 ack-then-fail  | 13"},
		{"negative: a flower write",
			"tc filter add dev nosuch ingress protocol ip flower skip_sw action pass|bare|", "negative 1 kernel ENODEV | 0"},
		// Boundary.
		{"boundary: the largest handle",
			"tc filter get dev d0 ingress protocol ip pref 4 handle 0xffffffff flower|flmisc|", "boundary 0 none  | 10"},
		{"boundary: a pfcp seid past 2^32 in JSON (FL2)",
			"tc -j filter get dev d0 ingress protocol ip pref 8 handle 1 flower|flenc|", "boundary 0 none  | 11"},
		// Corner.
		{"corner: terse (FL20)", "tc -br filter show dev d0 ingress|fll2|", "corner 0 none  | 15"},
		{"corner: terse in JSON", "tc -j -br filter show dev d0 ingress|flmisc|", "corner 0 none  | 10"},
		{"corner: protocol all sends no ETH_TYPE (FL17)",
			"tc filter get dev d0 ingress protocol all pref 11 handle 1 flower|fll2|", "corner 0 none  | 15"},
		{"corner: an unnamed eth_type (FL7)",
			"tc filter get dev d0 ingress protocol 0x88b5 pref 13 handle 1 flower|fll2|", "corner 0 none  | 15"},
		{"corner: gtp in JSON (FL1)", "tc -j filter get dev d0 ingress protocol ip pref 7 handle 1 flower|flenc|",
			"corner 0 none  | 11"},
		{"corner: classid read as hw_tc (FL3)",
			"tc filter get dev d0 ingress protocol ip pref 2 handle 1 flower|flmisc|", "corner 0 none  | 10"},
		{"corner: a block get with any parent (FL22)",
			"tc filter get block 20 parent 1: protocol ip pref 1 handle 1 flower|flmisc|", "corner 0 none  | 10"},
		{"corner: -br in a batch", "tc -br -batch b.txt|flmisc|brief", "corner 0 none  | 11"},
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

	write := regexp.MustCompile(`\b(add|del|delete|change|replace)\b`)
	fixtures := map[string]bool{}
	for k, r := range byKey {
		fixtures[r.Axes["fix"]] = true
		// Writes appear only as refusals.
		if write.MatchString(r.Cmd) && !strings.HasPrefix(r.Desc, "negative:") {
			t.Errorf("%s: %q writes but is not a negative", r.ID, r.Cmd)
		}
		// Every positive command runs plain and -j.
		if strings.HasPrefix(r.Desc, "negative:") || strings.HasPrefix(r.Cmd, "tc -j ") {
			continue
		}
		cmd, rest, _ := strings.Cut(k, "|")
		if _, ok := byKey[strings.Replace(cmd, "tc ", "tc -j ", 1)+"|"+rest]; !ok {
			t.Errorf("%s: %q has no -j row", r.ID, cmd)
		}
	}
	for _, fix := range []string{"bare", "fll2", "fll3", "flenc", "flct", "flmisc"} {
		if !fixtures[fix] {
			t.Errorf("fixture %s has no row", fix)
		}
	}
	// Each enc option kind, and the boundary values the leads name, are in
	// a fixture or a row.
	all := new(strings.Builder)
	for _, r := range rows {
		all.WriteString(r.Cmd + "\n")
		for _, p := range r.Pre {
			all.WriteString(p.Cmd + "\n")
		}
	}
	for _, s := range []string{"geneve_opts", "vxlan_opts", "erspan_opts 1:", "erspan_opts 2:", "gtp_opts", "pfcp_opts 1:123456789",
		"handle 0xffffffff", "classid 1:ffe0", "tcp_flags 0x2/0x12", "type 8/15", "protocol 0x88b5", "mpls lse", "cfm mdl"} {
		if !strings.Contains(all.String(), s) {
			t.Errorf("no row or fixture has %q", s)
		}
	}

	// Determinism: the same seed writes the same bytes.
	var a, b bytes.Buffer
	for _, buf := range []*bytes.Buffer{&a, &b} {
		rows, err := matrixRows(families["tcflower"](), matrixOpts{Seed: 1})
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
