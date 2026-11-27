package main

import (
	"bytes"
	"fmt"
	"regexp"
	"strings"
	"testing"
)

// TestTcAqmSpace pins the AQM and leaf qdisc rows (gotc 16C): `qdisc show`
// of red, gred, choke, sfb, codel, pie, fq_pie, hhf, cake, cake_mq,
// dualpi2, plug and skbprio over 26 fixtures, `class show` where a kind
// has classes, two batches and the refusals. Rows are keyed by command,
// fixture and batch file.
//
// go test ./tools/nlcapgen/ -run TestTcAqmSpace
func TestTcAqmSpace(t *testing.T) {
	rows, err := matrixRows(families["tcaqm"](), matrixOpts{Seed: 1})
	if err != nil {
		t.Fatal(err)
	}
	if err := checkRows(rows); err != nil {
		t.Fatal(err)
	}
	if len(rows) != 132 {
		t.Errorf("%d rows, want 132 (64 commands plain and -j, 4 negatives)", len(rows))
	}
	byKey := map[string]row{}
	key := func(r row) string { return r.Cmd + "|" + r.Axes["fix"] + "|" + r.Axes["batch"] }
	for _, r := range rows {
		if !strings.HasPrefix(r.ID, "tcaqm/") {
			t.Errorf("%s: not in tcaqm/", r.ID)
		}
		if r.InitNS || r.Serial || r.Proto != "route" || strings.Join(r.Modules, ",") != strings.Join(tcAqmModules, ",") {
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
	}

	tests := []struct {
		description string
		key         string // cmd|fix|batch
		expected    string // kind rc refused-by errno | pre count
	}{
		// Positive.
		{"positive: red", "tc qdisc show dev d0|aqmred|", "positive 0 none  | 2"},
		{"positive: red flags in JSON (Q87)", "tc -j qdisc show dev d0|aqmred2|", "positive 0 none  | 2"},
		{"positive: choke", "tc qdisc show dev d0|aqmchoke|", "positive 0 none  | 2"},
		{"positive: gred with two VQs", "tc qdisc show dev d0|aqmgred|", "positive 0 none  | 4"},
		{"positive: cake default (K38, Q80, Q81)", "tc -j qdisc show dev d0|aqmcake|", "positive 0 none  | 2"},
		{"positive: cake_mq", "tc qdisc show dev d1|aqmcakemq|", "positive 0 none  | 2"},
		{"positive: dualpi2 keys (Q83, Q84)", "tc -j qdisc show dev d0|aqmdpi2|", "positive 0 none  | 2"},
		{"positive: skbprio", "tc qdisc show dev d0|aqmskbprio|", "positive 0 none  | 2"},
		{"positive: a batch of cake shows under -s", "tc -s -batch b.txt|aqmcake|qdisc-then-class", "positive 0 none  | 3"},
		// Negative.
		{"negative: cake_mq under plug", "tc qdisc add dev d0 parent 1: handle 2: cake_mq|aqmplug|", "negative 2 kernel ENOTSUP | 2"},
		{"negative: cake_mq with autorate (K39)", "tc qdisc replace dev d1 root handle 1: cake_mq autorate-ingress|aqmcakemq|",
			"negative 2 kernel ENOTSUP | 2"},
		{"negative: gred change over plug", "tc qdisc change dev d0 root handle 1: gred vq 0 limit 60KB min 15KB max 45KB avpkt 1000|aqmplug|",
			"negative 2 kernel EINVAL | 2"},
		{"negative: no such device", "tc qdisc show dev nosuch|aqmred|", "negative 1 kernel ENODEV | 2"},
		// Boundary.
		{"boundary: gred with no VQ configured", "tc qdisc show dev d0|aqmgred0|", "boundary 0 none  | 2"},
		{"boundary: sfb rehash past 4294967 ms (Q73)", "tc qdisc show dev d0|aqmsfb2|", "boundary 0 none  | 2"},
		// Corner.
		{"corner: red -d with a qevent (Q66)", "tc -d qdisc show dev d0|aqmred2|", "corner 0 none  | 2"},
		{"corner: cake -s -o (Q79)", "tc -s -o qdisc show dev d0|aqmcake|", "corner 0 none  | 2"},
		{"corner: dualpi2 -s (Q86)", "tc -s qdisc show dev d0|aqmdpi|", "corner 0 none  | 2"},
		{"corner: plug prints no options (K41)", "tc -j -s qdisc show dev d0|aqmplug|", "corner 0 none  | 2"},
		{"corner: skbprio classes (K42)", "tc class show dev d0|aqmskbprio|", "corner 0 none  | 2"},
		{"corner: red -d twice in a batch", "tc -d -batch b.txt|aqmred2|red-twice", "corner 0 none  | 3"},
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
	for k, r := range byKey {
		fix := r.Axes["fix"]
		fixtures[fix] = true
		neg := strings.HasPrefix(r.Desc, "negative:")
		if write.MatchString(r.Cmd) && !neg {
			t.Errorf("%s: %q writes but is not a negative", r.ID, r.Cmd)
		}
		// cake_mq runs on the four-queue dummy only, apart from its
		// negative.
		for _, p := range r.Pre {
			if strings.Contains(p.Cmd, "cake_mq") && fix != "aqmcakemq" {
				t.Errorf("%s: %q outside the multi-queue fixture", r.ID, p.Cmd)
			}
			if fix == "aqmcakemq" && strings.HasPrefix(p.Cmd, "ip link add") && !strings.Contains(p.Cmd, "numtxqueues 4") {
				t.Errorf("%s: %q is not a four-queue dummy", r.ID, p.Cmd)
			}
		}
		if strings.Contains(r.Cmd, "cake_mq") && r.Axes["fix"] != "aqmcakemq" && !strings.Contains(r.Desc, "classless parent") {
			t.Errorf("%s: %q: cake_mq off the multi-queue fixture", r.ID, r.Cmd)
		}
		// Every positive command runs plain and -j.
		if neg || strings.HasPrefix(r.Cmd, "tc -j ") {
			continue
		}
		cmd, rest, _ := strings.Cut(k, "|")
		if _, ok := byKey[strings.Replace(cmd, "tc ", "tc -j ", 1)+"|"+rest]; !ok {
			t.Errorf("%s: %q has no -j row", r.ID, cmd)
		}
	}
	if len(fixtures) != 26 {
		t.Errorf("%d fixtures, want 26", len(fixtures))
	}
	all := new(strings.Builder)
	for _, r := range rows {
		all.WriteString(r.Cmd + "\n")
		for _, p := range r.Pre {
			all.WriteString(p.Cmd + "\n")
		}
	}
	for _, kind := range []string{"red", "gred", "choke", "sfb", "codel", "pie", "fq_pie", "hhf", "cake", "cake_mq",
		"dualpi2", "plug", "skbprio"} {
		if !regexp.MustCompile(`qdisc add .*handle \w+: ` + kind + `( |$|\n)`).MatchString(all.String()) {
			t.Errorf("no fixture adds %s", kind)
		}
	}
	// Every cake diffserv mode: diffserv3 is the default.
	for _, s := range []string{"cake\n", "diffserv4", "diffserv8", "besteffort", "precedence",
		"qevent early_drop block 10", "rehash 5000000", "no_split_gso", "step_thresh 1ms", "ack-filter-aggressive",
		"fwmark 0xff", "gred vq 1"} {
		if !strings.Contains(all.String(), s) {
			t.Errorf("no row or fixture has %q", s)
		}
	}
	// No packets: the counters, delays and probabilities must not move.
	if pk := regexp.MustCompile(`\b(ping|mausezahn|mz|nc|iperf3?|trafgen)\b`); pk.MatchString(all.String()) {
		t.Errorf("a row sends packets: %q", pk.FindString(all.String()))
	}

	// Determinism: the same seed writes the same bytes.
	var a, b bytes.Buffer
	for _, buf := range []*bytes.Buffer{&a, &b} {
		rows, err := matrixRows(families["tcaqm"](), matrixOpts{Seed: 1})
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
