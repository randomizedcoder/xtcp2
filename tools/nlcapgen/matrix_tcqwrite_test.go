package main

import (
	"bytes"
	"strings"
	"testing"
)

// TestTcQwriteSpace pins the qdisc and class write rows (gotc 17C): every
// increment-9 kind's add bodies, the other verbs on each, the frame's
// options, htb classes, a batch and the refusals.
//
// go test ./tools/nlcapgen/ -run TestTcQwriteSpace
func TestTcQwriteSpace(t *testing.T) {
	rows, err := matrixRows(families["tcqwrite"](), matrixOpts{Seed: 1})
	if err != nil {
		t.Fatal(err)
	}
	if err := checkRows(rows); err != nil {
		t.Fatal(err)
	}
	if len(rows) != 125 {
		t.Errorf("%d rows, want 125 (100 commands plain, 25 negatives)", len(rows))
	}
	byCmd := map[string]row{}
	for _, r := range rows {
		if !strings.HasPrefix(r.ID, "tcqwrite/") {
			t.Errorf("%s: not in tcqwrite/", r.ID)
		}
		if r.InitNS || r.Serial || r.Proto != "route" {
			t.Errorf("%s: initns %t serial %t proto %q", r.ID, r.InitNS, r.Serial, r.Proto)
		}
		for _, m := range r.Modules {
			if !tdcGuestModules[m] {
				t.Errorf("%s: module %s is not one the guest has", r.ID, m)
			}
		}
		// Every write targets d0 (or the missing nosuch), made in the row's
		// own namespace: no row touches lo or another netns.
		if w := tcQwriteWords(r); strings.Contains(w, "dev lo") || strings.Contains(r.Cmd, "-netns") {
			t.Errorf("%s: writes outside its dummy: %q", r.ID, r.Cmd)
		}
		if r.Expect.RefusedBy == refNone && len(r.Readback) == 0 {
			t.Errorf("%s: a positive write with no readback", r.ID)
		}
		k := r.Cmd + "|" + r.Axes["fix"]
		if _, dup := byCmd[k]; dup {
			t.Errorf("%s: %q twice", r.ID, k)
		}
		byCmd[k] = r
	}
	has := func(sub string) bool {
		for _, r := range rows {
			if strings.Contains(r.Cmd, sub) {
				return true
			}
		}
		return false
	}

	tests := []struct {
		description string
		sub         string // a substring some row's command holds
		expected    bool
	}{
		// Positive: each kind is added.
		{"positive: pfifo add", "root handle 1: pfifo limit 100", true},
		{"positive: bfifo add", "root handle 1: bfifo limit 10kb", true},
		{"positive: pfifo_head_drop add", "pfifo_head_drop limit 50", true},
		{"positive: prio priomap", "prio bands 3 priomap", true},
		{"positive: pfifo_fast add", "root handle 1: pfifo_fast", true},
		{"positive: fq_codel options", "fq_codel limit 1000 flows 512", true},
		{"positive: fq options", "fq limit 1000 flow_limit 50", true},
		{"positive: htb qdisc", "root handle 1: htb default 10 r2q 20", true},
		{"positive: tbf peakrate", "peakrate 12mbit mtu 2000", true},
		{"positive: sfq red mode", "sfq headdrop redflowlimit", true},
		{"positive: netem distribution", "distribution normal", true},
		{"positive: ingress", "qdisc add dev d0 ingress", true},
		{"positive: clsact blocks", "ingress_block 5 egress_block 6 clsact", true},
		// Positive: verbs.
		{"positive: change", "qdisc change dev d0 root handle 1: netem", true},
		{"positive: replace", "qdisc replace dev d0 root handle 1: netem", true},
		{"negative: htb has no change op", "qdisc change dev d0 root handle 1: htb", true},
		{"positive: link", "qdisc link dev d0 root handle 1: tbf", true},
		{"positive: delete", "qdisc delete dev d0 root", true},
		{"positive: class change", "class change dev d0", true},
		{"positive: class del", "class del dev d0 classid 1:10", true},
		// Leads.
		{"lead: W1 help", "root handle 1: help pfifo", true},
		{"lead: W2 handle then ingress", "handle 5: ingress", true},
		{"lead: W2 words after ingress", "qdisc add ingress dev d0", true},
		{"lead: W3 zero handle", "handle 0: handle 1:", true},
		{"lead: W4 parse failure", "pfifo limit zz", true},
		{"lead: W5 class garbage", "classid 1:10 nosuchkind", true},
		{"lead: W7 estimator", "estimator 1s 8s", true},
		{"lead: W8 no parse_qopt", "clsact foo", true},
		{"lead: W9 percent", "htb rate 10%", true},
		// Boundary and corner.
		{"boundary: htb rate64", "rate 40gbit ceil 50gbit", true},
		{"boundary: stab with only overhead", "stab overhead 4 pfifo", true},
		{"corner: verb by prefix", "qdisc a dev d0", true},
		// Negative: nothing reads.
		{"negative: no show command is captured", "qdisc show", false},
		{"negative: no packets are sent", "ping", false},
	}
	for _, tc := range tests {
		t.Run(tc.description, func(t *testing.T) {
			if got := has(tc.sub); got != tc.expected {
				t.Errorf("a row with %q: %t, want %t", tc.sub, got, tc.expected)
			}
		})
	}

	// Every kind has every verb.
	fixes := map[string]bool{}
	for _, r := range rows {
		fixes[r.Axes["fix"]] = true
	}
	for _, k := range tcQwriteKinds {
		for _, vb := range tcQwriteVerbs {
			if !fixes[k.name+"-"+vb.verb] {
				t.Errorf("%s has no %s row", k.name, vb.verb)
			}
		}
	}

	// Generation is deterministic.
	var a, b bytes.Buffer
	for i, buf := range []*bytes.Buffer{&a, &b} {
		rs, err := matrixRows(families["tcqwrite"](), matrixOpts{Seed: 1})
		if err != nil {
			t.Fatalf("run %d: %v", i, err)
		}
		if err := writeRows(buf, nil, rs); err != nil {
			t.Fatal(err)
		}
	}
	if !bytes.Equal(a.Bytes(), b.Bytes()) {
		t.Error("two generations differ")
	}
}
