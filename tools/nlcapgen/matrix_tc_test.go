package main

import (
	"bytes"
	"fmt"
	"regexp"
	"strings"
	"testing"
)

// TestTcSpace pins the tc show rows (gotc 9C): `qdisc show` and `class
// show` over nine fixtures, their batches and their refusals. Rows are
// keyed by command, fixture and batch file.
//
// go test ./tools/nlcapgen/ -run TestTcSpace
func TestTcSpace(t *testing.T) {
	byKey := map[string]row{}
	key := func(r row) string { return r.Cmd + "|" + r.Axes["fix"] + "|" + r.Axes["batch"] }
	counts := map[string]int{}
	for _, fam := range []string{"tcqdisc", "tcclass"} {
		rows, err := matrixRows(families[fam](), matrixOpts{Seed: 1})
		if err != nil {
			t.Fatal(err)
		}
		if err := checkRows(rows); err != nil {
			t.Fatal(err)
		}
		counts[fam] = len(rows)
		for _, r := range rows {
			if !strings.HasPrefix(r.ID, fam+"/") {
				t.Errorf("%s: not in %s/", r.ID, fam)
			}
			if r.InitNS || r.Serial || r.Proto != "route" || strings.Join(r.Modules, ",") != strings.Join(tcModules, ",") {
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
			if strings.Contains(r.Cmd, "-batch b.txt") || strings.Contains(r.Cmd, "-batch - <") {
				if len(r.Pre) == 0 || !strings.HasSuffix(r.Pre[len(r.Pre)-1].Cmd, "> b.txt") {
					t.Errorf("%s: batch row whose last pre step doesn't write b.txt", r.ID)
				}
			}
		}
	}
	if counts["tcqdisc"] != 142 || counts["tcclass"] != 82 {
		t.Errorf("rows %v, want tcqdisc 142 (60 commands plain and -j, 22 negatives), tcclass 82 (35 and 12)", counts)
	}

	tests := []struct {
		description string
		key         string // cmd|fix|batch
		expected    string // kind rc refused-by errno | pre count
	}{
		// Positive.
		{"positive: every qdisc in the namespace", "tc qdisc show|qbase|", "positive 0 none  | 4"},
		{"positive: -s, requeues twice (Q18)", "tc -j -s qdisc show|qbase|", "positive 0 none  | 4"},
		{"positive: htb classes -d (Q11)", "tc -d class show dev d0|qhtb|", "positive 0 none  | 7"},
		{"positive: a two-line batch", "tc -batch b.txt|qbase|two", "positive 0 none  | 5"},
		{"positive: fq -s (Q1)", "tc -s qdisc show|qfq|", "positive 0 none  | 3"},
		// Negative.
		{"negative: an unknown option", "tc -zz qdisc|bare|", "negative 255 iproute2  | 0"},
		{"negative: -c is -cf (Q24)", "tc -c qdisc show|bare|", "negative 255 iproute2  | 0"},
		{"negative: no such device", "tc qdisc show dev nosuch|bare|", "negative 1 kernel ENODEV | 0"},
		{"negative: class duparg dev", "tc class show dev lo dev lo|bare|", "negative 255 iproute2  | 0"},
		{"negative: a batch file opened after the link dump", "tc -batch nosuch|bare|", "negative 1 ack-then-fail  | 0"},
		{"negative: the second root is an invarg exit (Q29)", "tc -force -batch b.txt|qbase|sticky-root",
			"negative 255 ack-then-fail  | 5"},
		{"negative: -g twice aborts (Q34)", "tc -g -batch b.txt|qhtb|graph-twice", "negative 134 ack-then-fail  | 8"},
		{"negative: a qdisc write", "tc qdisc add dev nosuch root handle 1: pfifo|bare|", "negative 1 kernel ENODEV | 0"},
		// Boundary.
		{"boundary: qdisc alone is show", "tc qdisc|bare|", "boundary 0 none  | 0"},
		{"boundary: class show with no dev", "tc -j class show|bare|", "boundary 0 none  | 0"},
		{"boundary: tbf's latency past 2^32 us (Q6)", "tc qdisc show dev d2|qtbf|", "boundary 0 none  | 4"},
		// Corner.
		{"corner: show help dumps anyway (Q25)", "tc qdisc show help|bare|", "corner 0 none  | 0"},
		{"corner: the dev filter sticks (Q29)", "tc -batch b.txt|qbase|sticky-dev", "corner 0 none  | 5"},
		{"corner: -nm in a batch (Q31)", "tc -nm -batch b.txt|qhtb|names", "corner 0 none  | 8"},
		{"corner: -nm -cf with no default database (Q33)", "tc -nm -cf cls.txt class show dev d0|qhtb|", "corner 0 none  | 8"},
		{"corner: class l is list", "tc class l dev d3|qbase|", "corner 0 none  | 4"},
		{"corner: -r in JSON (Q12)", "tc -j -r class show dev d0|qhtb|", "corner 0 none  | 7"},
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

	write := regexp.MustCompile(`\b(add|del|delete|change|replace|link)\b`)
	fixtures := map[string]bool{}
	readbacks := 0
	for k, r := range byKey {
		readbacks += len(r.Readback)
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
	// Every fixture has a row, and one row reads /proc/net/psched.
	for _, fix := range []string{"bare", "qbase", "qfifo", "qcodel", "qfq", "qhtb", "qtbf", "qsfq", "qnetem", "qingress"} {
		if !fixtures[fix] {
			t.Errorf("fixture %s has no row", fix)
		}
	}
	if r := byKey["tc qdisc show|bare|"]; readbacks != 1 || len(r.Readback) != 1 || r.Readback[0].Cmd != "cat /proc/net/psched" {
		t.Errorf("%d readbacks; the bare qdisc show has %v", readbacks, r.Readback)
	}

	// Determinism: the same seed writes the same bytes.
	for _, fam := range []string{"tcqdisc", "tcclass"} {
		var a, b bytes.Buffer
		for _, buf := range []*bytes.Buffer{&a, &b} {
			rows, err := matrixRows(families[fam](), matrixOpts{Seed: 1})
			if err != nil {
				t.Fatal(err)
			}
			if err := writeRows(buf, nil, rows); err != nil {
				t.Fatal(err)
			}
		}
		if !bytes.Equal(a.Bytes(), b.Bytes()) {
			t.Errorf("%s: two runs with seed 1 differ", fam)
		}
	}
}

// TestTcDummies checks the device steps the fixtures start with.
//
// go test ./tools/nlcapgen/ -run TestTcDummies
func TestTcDummies(t *testing.T) {
	tests := []struct {
		description string
		names       []string
		expected    string
	}{
		{"positive: one device", []string{"d0"},
			"ip link add d0 address 02:00:00:00:02:00 type dummy && ip link set d0 addrgenmode none && ip link set d0 up"},
		{"positive: a multiqueue device", []string{"d0", "d1/4"},
			"ip link add d0 address 02:00:00:00:02:00 type dummy && ip link set d0 addrgenmode none && ip link set d0 up" +
				" && ip link add d1 address 02:00:00:00:02:01 numtxqueues 4 type dummy && ip link set d1 addrgenmode none && ip link set d1 up"},
		{"boundary: no devices", nil, ""},
		{"corner: a slash with nothing after it", []string{"d0/"},
			"ip link add d0 address 02:00:00:00:02:00 numtxqueues  type dummy && ip link set d0 addrgenmode none && ip link set d0 up"},
	}
	for _, tc := range tests {
		t.Run(tc.description, func(t *testing.T) {
			if got := tcDummies(tc.names...); got != tc.expected {
				t.Errorf("got %q\nwant %q", got, tc.expected)
			}
		})
	}
}
