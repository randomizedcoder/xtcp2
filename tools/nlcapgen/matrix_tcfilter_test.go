package main

import (
	"bytes"
	"fmt"
	"regexp"
	"strings"
	"testing"
)

// TestTcFilterSpace pins the tc filter rows (gotc 10C): `filter show/get`
// and `chain show/get` over six fixtures, their batches and their
// refusals. Rows are keyed by command, fixture and batch file.
//
// go test ./tools/nlcapgen/ -run TestTcFilterSpace
func TestTcFilterSpace(t *testing.T) {
	rows, err := matrixRows(families["tcfilter"](), matrixOpts{Seed: 1})
	if err != nil {
		t.Fatal(err)
	}
	if err := checkRows(rows); err != nil {
		t.Fatal(err)
	}
	if len(rows) != 159 {
		t.Errorf("%d rows, want 159 (66 commands plain and -j, 27 negatives)", len(rows))
	}
	byKey := map[string]row{}
	key := func(r row) string { return r.Cmd + "|" + r.Axes["fix"] + "|" + r.Axes["batch"] }
	for _, r := range rows {
		if !strings.HasPrefix(r.ID, "tcfilter/") {
			t.Errorf("%s: not in tcfilter/", r.ID)
		}
		if r.InitNS || r.Serial || r.Proto != "route" || strings.Join(r.Modules, ",") != strings.Join(tcFilterModules, ",") {
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
		{"positive: u32, two match keys (L2), eat (L1)", "tc -j filter show dev d0 ingress|fu32|", "positive 0 none  | 8"},
		{"positive: route's two name keys (F3)", "tc -j filter show dev d1 parent 1: pref 20|fsimple|", "positive 0 none  | 10"},
		{"positive: ematch text in JSON (F1)", "tc -j filter show dev d0 ingress|fematch|", "positive 0 none  | 9"},
		{"positive: flow get", "tc filter get dev d0 parent 1: protocol ip pref 30 handle 1 flow|fflow|", "positive 0 none  | 5"},
		{"positive: a shared block", "tc filter show block 10|fchain|", "positive 0 none  | 8"},
		{"positive: out-of-slice kinds tc prints", "tc filter show dev d0 ingress|fout|", "positive 0 none  | 5"},
		{"positive: a two-line batch", "tc -batch b.txt|fu32|two", "positive 0 none  | 9"},
		// Negative.
		{"negative: an unknown word (L41)", "tc filter show bogus|bare|", "negative 255 iproute2  | 0"},
		{"negative: no such device", "tc filter show dev nosuch|bare|", "negative 1 kernel ENODEV | 0"},
		{"negative: a block with no qdisc", "tc filter show block 99|bare|", "negative 1 ack-then-fail  | 0"},
		{"negative: get with no protocol", "tc filter get dev d0 egress pref 3 handle 1 matchall|fsimple|",
			"negative 255 iproute2  | 10"},
		{"negative: get with no handle, after the link lookup",
			"tc filter get dev d0 egress protocol all pref 3 matchall|fsimple|", "negative 255 ack-then-fail  | 10"},
		{"negative: a typo taken as the kind (F23)",
			"tc filter get dev d0 egress protocol all pref 3 hndle 1 matchall|fsimple|", "negative 1 ack-then-fail  | 10"},
		{"negative: no such handle (F15)",
			"tc filter get dev d0 egress protocol all pref 3 handle 9 matchall|fsimple|", "negative 2 kernel ENOENT | 10"},
		{"negative: an invalid u32 handle (F15)",
			"tc filter get dev d0 ingress protocol ip pref 1 handle zz u32|fu32|", "negative 1 ack-then-fail  | 8"},
		{"negative: cgroup's get finds nothing",
			"tc filter get dev d0 egress protocol all pref 1 handle 1 cgroup|fematch|", "negative 2 kernel ENOENT | 9"},
		{"negative: -br's terse dump of u32 (F21)", "tc -br filter show dev d0 ingress|fu32|", "negative 1 kernel EMSGSIZE | 8"},
		{"negative: a filter write", "tc filter add dev nosuch ingress matchall action pass|bare|", "negative 1 kernel ENODEV | 0"},
		{"negative: chain twice in a batch (Q29f)", "tc -batch b.txt|fchain|chain-twice", "negative 255 ack-then-fail  | 9"},
		// Boundary.
		{"boundary: filter alone is show", "tc filter|bare|", "boundary 0 none  | 0"},
		{"boundary: no dev, an empty dump (F8)", "tc -j filter show|bare|", "boundary 0 none  | 0"},
		{"boundary: pref 70000 (L40)", "tc filter show dev d0 ingress pref 70000|fu32|", "boundary 0 none  | 8"},
		{"boundary: a handle past 2^32 (F5)",
			"tc filter get dev d0 egress protocol all pref 3 handle 4294967297 matchall|fsimple|", "boundary 0 none  | 10"},
		{"boundary: an empty chain", "tc filter show dev d0 ingress chain 9|fchain|", "boundary 0 none  | 8"},
		// Corner.
		{"corner: show help dumps anyway (F7)", "tc filter show help|bare|", "corner 0 none  | 0"},
		{"corner: -p decoding (L4, F4, F13)", "tc -p filter show dev d0 ingress|fu32|", "corner 0 none  | 8"},
		{"corner: the list handle is unused (Q29h)", "tc filter show dev d0 ingress handle 9|fu32|", "corner 0 none  | 8"},
		{"corner: a strtol handle (F5)",
			"tc filter get dev d0 egress protocol all pref 3 handle 1junk matchall|fsimple|", "corner 0 none  | 10"},
		{"corner: any u32 handle separator (F6)",
			"tc filter get dev d0 ingress protocol ip pref 1 handle 800.0:1zz u32|fu32|", "corner 0 none  | 8"},
		{"corner: matchall's hard newline under -o (L47)", "tc -o filter show dev d0 egress|fsimple|", "corner 0 none  | 10"},
		{"corner: a fully filtered chain line (F22)", "tc chain show dev d0 ingress chain 5|fchain|", "corner 0 none  | 8"},
		{"corner: chain get ignores the rest (F9)",
			"tc chain get dev d0 ingress chain 9 handle 3 pref 4 protocol ip u32|fchain|", "corner 0 none  | 8"},
		{"corner: the pref filter sticks (Q29f)", "tc -batch b.txt|fu32|sticky-pref", "corner 0 none  | 9"},
		{"corner: get sets the dev filter (Q29f)", "tc -j -batch b.txt|fsimple|get-then-show", "corner 0 none  | 11"},
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
	shows, gets := map[string]bool{}, map[string]bool{}
	kinds := []string{"u32", "fw", "route", "matchall", "flow", "basic", "cgroup"}
	for k, r := range byKey {
		fixtures[r.Axes["fix"]] = true
		// Writes appear only as refusals.
		if write.MatchString(r.Cmd) && !strings.HasPrefix(r.Desc, "negative:") {
			t.Errorf("%s: %q writes but is not a negative", r.ID, r.Cmd)
		}
		if strings.Contains(r.Cmd, " filter get ") {
			for _, kind := range kinds {
				if strings.HasSuffix(r.Cmd, " "+kind) {
					gets[kind] = true
				}
			}
		}
		if strings.Contains(r.Cmd, " filter show ") && !strings.HasPrefix(r.Desc, "negative:") {
			for _, p := range r.Pre {
				for _, kind := range kinds {
					if strings.Contains(p.Cmd, " "+kind+" ") && strings.HasPrefix(p.Cmd, "tc filter add") {
						shows[kind] = true
					}
				}
			}
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
	for _, fix := range []string{"bare", "fu32", "fsimple", "fflow", "fematch", "fchain", "fout"} {
		if !fixtures[fix] {
			t.Errorf("fixture %s has no row", fix)
		}
	}
	// Each in-slice kind is shown and got; cgroup's get is the kernel's
	// refusal, since cls_cgroup_get always returns NULL.
	for _, kind := range kinds {
		if !shows[kind] || !gets[kind] {
			t.Errorf("kind %s: show %t get %t", kind, shows[kind], gets[kind])
		}
	}
	// The boundary values the leads name are in the fixtures or rows.
	all := new(strings.Builder)
	for _, r := range rows {
		all.WriteString(r.Cmd + "\n")
		for _, p := range r.Pre {
			all.WriteString(p.Cmd + "\n")
		}
	}
	for _, s := range []string{"pref 70000", "eq 0x80000000", "handle 4294967297"} {
		if !strings.Contains(all.String(), s) {
			t.Errorf("no row or fixture has %q", s)
		}
	}

	// Determinism: the same seed writes the same bytes.
	var a, b bytes.Buffer
	for _, buf := range []*bytes.Buffer{&a, &b} {
		rows, err := matrixRows(families["tcfilter"](), matrixOpts{Seed: 1})
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
