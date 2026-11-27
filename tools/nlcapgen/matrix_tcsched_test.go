package main

import (
	"bytes"
	"fmt"
	"regexp"
	"strings"
	"testing"
)

// TestTcSchedSpace pins the scheduling qdisc rows (gotc 15C): `qdisc show`
// and `class show` of drr, qfq, hfsc, ets, mqprio, taprio, multiq, cbs and
// etf over twelve fixtures, their batches and their refusals. Rows are
// keyed by command, fixture and batch file.
//
// go test ./tools/nlcapgen/ -run TestTcSchedSpace
func TestTcSchedSpace(t *testing.T) {
	rows, err := matrixRows(families["tcsched"](), matrixOpts{Seed: 1})
	if err != nil {
		t.Fatal(err)
	}
	if err := checkRows(rows); err != nil {
		t.Fatal(err)
	}
	if len(rows) != 87 {
		t.Errorf("%d rows, want 87 (41 commands plain and -j, 5 negatives)", len(rows))
	}
	byKey := map[string]row{}
	key := func(r row) string { return r.Cmd + "|" + r.Axes["fix"] + "|" + r.Axes["batch"] }
	for _, r := range rows {
		if !strings.HasPrefix(r.ID, "tcsched/") {
			t.Errorf("%s: not in tcsched/", r.ID)
		}
		if r.InitNS || r.Serial || r.Proto != "route" || strings.Join(r.Modules, ",") != strings.Join(tcSchedModules, ",") {
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
		{"positive: drr classes (Q37)", "tc -j class show dev d0|schdrr|", "positive 0 none  | 4"},
		{"positive: drr deficit (Q38)", "tc -s class show dev d0|schdrr|", "positive 0 none  | 4"},
		{"positive: qfq classes", "tc class show dev d0|schqfq|", "positive 0 none  | 4"},
		{"positive: hfsc default (Q40)", "tc qdisc show dev d0|schhfsc|", "positive 0 none  | 5"},
		{"positive: hfsc curves (Q43, Q44)", "tc -j class show dev d0|schhfsc|", "positive 0 none  | 5"},
		{"positive: ets quanta (Q45)", "tc qdisc show dev d0|schets|", "positive 0 none  | 2"},
		{"positive: ets strict class (Q46)", "tc -j class show dev d0|schets|", "positive 0 none  | 2"},
		{"positive: cbs and etf (Q61, Q63)", "tc -j qdisc show dev d0|schtsn|", "positive 0 none  | 4"},
		{"positive: multiq bands (Q60)", "tc qdisc show dev d1 root|schmultiq|", "positive 0 none  | 2"},
		{"positive: mqprio (Q49-Q52)", "tc -j qdisc show dev d1 root|schmqprio|", "positive 0 none  | 2"},
		{"positive: taprio txtime (Q55, Q56)", "tc -j qdisc show dev d1 root|schtaprio|", "positive 0 none  | 2"},
		{"positive: a batch of hfsc shows", "tc -batch b.txt|schhfsc|qdisc-then-class", "positive 0 none  | 6"},
		// Negative.
		{"negative: mqprio not at the root", "tc qdisc add dev d0 parent 1:1 handle 10: mqprio num_tc 2 map 0 0 1 1 queues 1@0 1@1 hw 0|schdrr|",
			"negative 2 kernel ENOTSUP | 4"},
		{"negative: taprio with no clockid", "tc qdisc replace dev d1 root handle 100: taprio num_tc 2 map 0 0 1 1 queues 4@0 4@4 base-time 0 sched-entry S 01 300000|schmultiq|",
			"negative 2 kernel EINVAL | 2"},
		{"negative: cbs offload", "tc qdisc replace dev d0 parent 1:1 handle 10: cbs idleslope 20000 sendslope -980000 hicredit 30 locredit -1470 offload 1|schtsn|",
			"negative 2 kernel ENOTSUP | 4"},
		{"negative: etf offload", "tc qdisc add dev d0 parent 1:2 handle 20: etf clockid CLOCK_TAI delta 300000 offload|schdrr|",
			"negative 2 kernel ENOTSUP | 4"},
		{"negative: no such device", "tc class show dev nosuch|schdrr|", "negative 1 kernel ENODEV | 4"},
		// Boundary.
		{"boundary: hfsc with no default (Q40)", "tc -j qdisc show dev d0|schhfsc0|", "boundary 0 none  | 2"},
		{"boundary: ets all strict (K25)", "tc -j qdisc show dev d0|schets0|", "boundary 0 none  | 2"},
		{"boundary: taprio admin only (K28)", "tc qdisc show dev d1 root|schtaprio3|", "boundary 0 none  | 2"},
		// Corner.
		{"corner: drr qdisc without options (K29)", "tc -j qdisc show dev d0|schdrr|", "corner 0 none  | 4"},
		{"corner: qfq xstats unread (Q39)", "tc -j -s class show dev d0|schqfq|", "corner 0 none  | 4"},
		{"corner: hfsc blank -s line (Q42)", "tc -s class show dev d0|schhfsc|", "corner 0 none  | 5"},
		{"corner: mqprio under -o (Q48)", "tc -o qdisc show dev d1 root|schmqprio|", "corner 0 none  | 2"},
		{"corner: taprio oper and admin (Q57)", "tc qdisc show dev d1 root|schtaprio2|", "corner 0 none  | 3"},
		{"corner: the hfsc graph", "tc -g class show dev d0|schhfsc|", "corner 0 none  | 5"},
		{"corner: taprio shows in a batch", "tc -batch b.txt|schtaprio2|root-then-all", "corner 0 none  | 4"},
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
	kinds := map[string]map[string]bool{} // fixture -> object shown
	multiq := map[string]bool{"schmultiq": true, "schmqprio": true, "schtaprio": true, "schtaprio2": true, "schtaprio3": true}
	for k, r := range byKey {
		fix := r.Axes["fix"]
		fixtures[fix] = true
		neg := strings.HasPrefix(r.Desc, "negative:")
		// Writes appear only as refusals; offload only in them.
		if write.MatchString(r.Cmd) && !neg {
			t.Errorf("%s: %q writes but is not a negative", r.ID, r.Cmd)
		}
		if strings.Contains(r.Cmd, "offload") && !neg {
			t.Errorf("%s: %q offloads but is not a negative", r.ID, r.Cmd)
		}
		// mqprio, taprio and multiq run on the eight-queue dummy only.
		for _, p := range r.Pre {
			if regexp.MustCompile(`\b(mqprio|taprio|multiq)\b`).MatchString(p.Cmd) && !multiq[fix] {
				t.Errorf("%s: %q outside a multi-queue fixture", r.ID, p.Cmd)
			}
			if multiq[fix] && strings.HasPrefix(p.Cmd, "ip link add") && !strings.Contains(p.Cmd, "numtxqueues 8") {
				t.Errorf("%s: %q is not an eight-queue dummy", r.ID, p.Cmd)
			}
		}
		if kinds[fix] == nil {
			kinds[fix] = map[string]bool{}
		}
		for _, obj := range []string{"qdisc show", "class show", "-s class show"} {
			if strings.HasPrefix(strings.TrimPrefix(r.Cmd, "tc -j "), obj) || strings.HasPrefix(strings.TrimPrefix(r.Cmd, "tc "), obj) {
				kinds[fix][obj] = true
			}
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
	for _, fix := range []string{"schdrr", "schqfq", "schhfsc", "schhfsc0", "schets", "schets0", "schtsn",
		"schmultiq", "schmqprio", "schtaprio", "schtaprio2", "schtaprio3"} {
		if !fixtures[fix] {
			t.Errorf("fixture %s has no row", fix)
		}
		if !kinds[fix]["qdisc show"] {
			t.Errorf("fixture %s has no qdisc show", fix)
		}
	}
	// The classful kinds show their classes, with and without -s.
	for _, fix := range []string{"schdrr", "schqfq", "schhfsc", "schets"} {
		if !kinds[fix]["class show"] || !kinds[fix]["-s class show"] {
			t.Errorf("fixture %s: class show %t, -s class show %t", fix, kinds[fix]["class show"], kinds[fix]["-s class show"])
		}
	}
	all := new(strings.Builder)
	for _, r := range rows {
		all.WriteString(r.Cmd + "\n")
		for _, p := range r.Pre {
			all.WriteString(p.Cmd + "\n")
		}
	}
	for _, kind := range []string{"drr", "qfq", "hfsc", "ets", "mqprio", "taprio", "multiq", "cbs", "etf"} {
		if !regexp.MustCompile(`qdisc add .*handle \w+: ` + kind + `\b`).MatchString(all.String()) {
			t.Errorf("no fixture adds %s", kind)
		}
	}
	for _, s := range []string{"drr quantum 1514", "qfq weight 30 maxpkt 9000", "hfsc default 10",
		"rt m1 2mbit d 10ms m2 1mbit ls m2 3mbit", "rt m2 1mbit ls m2 1mbit", "ul rate", "strict 1 quanta",
		"ets strict 3", "skip_sock_check", "flags 0x1 txtime-delay 200000", "base-time 4000000000000000000", "hw 0"} {
		if !strings.Contains(all.String(), s) {
			t.Errorf("no row or fixture has %q", s)
		}
	}
	// No packets: the counters and the timers must not move.
	if pk := regexp.MustCompile(`\b(ping|mausezahn|mz|nc|iperf3?|trafgen)\b`); pk.MatchString(all.String()) {
		t.Errorf("a row sends packets: %q", pk.FindString(all.String()))
	}

	// Determinism: the same seed writes the same bytes.
	var a, b bytes.Buffer
	for _, buf := range []*bytes.Buffer{&a, &b} {
		rows, err := matrixRows(families["tcsched"](), matrixOpts{Seed: 1})
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
