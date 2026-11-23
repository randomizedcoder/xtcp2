package main

import (
	"fmt"
	"strings"
	"testing"
)

// TestMptcpSpace pins the mptcp rows: every row runs in a fresh namespace
// (mptcp_pm is netnsok true, its endpoints and limits per-netns), loads no
// module (MPTCP is built in), captures generic netlink with rtnetlink (ip's
// ll_init_map link dump names an endpoint's dev), and carries the
// expected outcome for its axes.
//
// Positives are keyed by their (fix, json) facts, since several fixtures
// share a command and differ only in their setup; negatives are uniquely
// spelled, so they are keyed by command.
func TestMptcpSpace(t *testing.T) {
	rows, err := matrixRows(families["mptcp"](), matrixOpts{Seed: 1})
	if err != nil {
		t.Fatal(err)
	}
	if err := checkRows(rows); err != nil {
		t.Fatal(err)
	}
	byCmd := map[string]row{}
	byFixOpt := map[string]row{}
	for _, r := range rows {
		if r.InitNS || r.Serial {
			t.Errorf("%s: initns %t serial %t, want neither", r.ID, r.InitNS, r.Serial)
		}
		if r.Proto != "generic+route" || len(r.Modules) != 0 {
			t.Errorf("%s: proto %q modules %v, want generic+route and none", r.ID, r.Proto, r.Modules)
		}
		byCmd[r.Cmd] = r
		byFixOpt[r.Axes["fix"]+"/"+r.Axes["json"]] = r
	}

	neg := []struct {
		description string
		cmd         string
		expected    string // rc refused-by errno extack
	}{
		{"negative: help is refused before the wire", "ip mptcp help", "255 iproute2  "},
		{"negative: an unknown object", "ip mptcp badobj", "255 ack-then-fail  "},
		{"negative: an unknown endpoint sub-command", "ip mptcp endpoint junk", "255 ack-then-fail  "},
		{"negative: show's argument is not an id, flag or address", "ip mptcp endpoint show junk", "1 ack-then-fail  "},
		{"boundary: id 0 is refused for show", "ip mptcp endpoint show id 0", "255 ack-then-fail  "},
		{"negative: the kernel has no endpoint 9", "ip mptcp endpoint show id 9", "2 kernel EINVAL address not found"},
		{"negative: limits show's argument is not a limit", "ip mptcp limits show junk", "255 ack-then-fail  "},
		{"corner: limits has no unknown branch, falls to unknown", "ip mptcp limits junk", "255 ack-then-fail  "},
	}
	for _, tc := range neg {
		t.Run(tc.description, func(t *testing.T) {
			r, ok := byCmd[tc.cmd]
			if !ok {
				t.Fatalf("no row runs %q", tc.cmd)
			}
			got := fmt.Sprintf("%d %s %s %s", *r.Expect.RC, r.Expect.RefusedBy, r.Expect.Errno, r.Expect.Extack)
			if got != tc.expected {
				t.Fatalf("got %q, want %q", got, tc.expected)
			}
			if len(r.Pre) != 0 {
				t.Errorf("%d pre steps, want none", len(r.Pre))
			}
		})
	}

	// Positives: each fixture at opt=plain (json "0") and opt=json ("1").
	// The setup counts: topology 6, + 1 endpoint (ep-one) or 4 (multi),
	// or one limits set.
	pos := []struct {
		description string
		fix         string
		json        string
		expected    string // cmd | pre count
	}{
		{"boundary: no endpoints, plain", "ep-empty", "0", "ip mptcp endpoint show | 0"},
		{"boundary: no endpoints, json", "ep-empty", "1", "ip -j mptcp endpoint show | 0"},
		{"positive: one endpoint", "ep-one", "0", "ip mptcp endpoint show | 7"},
		{"positive: four endpoints", "ep-multi", "0", "ip mptcp endpoint show | 10"},
		{"positive: four endpoints, json", "ep-multi", "1", "ip -j mptcp endpoint show | 10"},
		{"positive: one endpoint by id", "ep-id", "0", "ip mptcp endpoint show id 2 | 10"},
		{"positive: one endpoint by id, json", "ep-id", "1", "ip -j mptcp endpoint show id 2 | 10"},
		{"corner: bare endpoint is a show", "ep-bare", "0", "ip mptcp endpoint | 10"},
		{"boundary: default limits", "lim-default", "0", "ip mptcp limits show | 0"},
		{"positive: limits set in pre", "lim-set", "0", "ip mptcp limits show | 1"},
		{"positive: limits set in pre, json", "lim-set", "1", "ip -j mptcp limits show | 1"},
		{"corner: bare limits is a show", "lim-bare", "1", "ip -j mptcp limits | 0"},
		{"corner: a set spelled into limits show", "lim-showarg", "0", "ip mptcp limits show subflows 3 | 0"},
	}
	for _, tc := range pos {
		t.Run(tc.description, func(t *testing.T) {
			r, ok := byFixOpt[tc.fix+"/"+tc.json]
			if !ok {
				t.Fatalf("no row for fixture %q json %q", tc.fix, tc.json)
			}
			got := fmt.Sprintf("%s | %d", r.Cmd, len(r.Pre))
			if got != tc.expected {
				t.Errorf("got %q, want %q", got, tc.expected)
			}
			if *r.Expect.RC != 0 || r.Expect.RefusedBy != refNone {
				t.Errorf("rc %d refused-by %s, want 0 none", *r.Expect.RC, r.Expect.RefusedBy)
			}
		})
	}

	// The topology precedes the endpoints, so a `dev d0` endpoint's device
	// exists when it is added.
	multi := byFixOpt["ep-multi/0"]
	pre := make([]string, 0, len(multi.Pre))
	for _, p := range multi.Pre {
		pre = append(pre, p.Cmd)
	}
	joined := strings.Join(pre, "\n")
	if strings.Index(joined, "ip link add d0") > strings.Index(joined, "dev d0 signal") {
		t.Errorf("ep-multi adds an endpoint on d0 before creating d0:\n%s", joined)
	}

	if len(rows) != 26 {
		t.Errorf("%d rows, want 26 (9 fixtures x 2 opts + 8 negatives)", len(rows))
	}
}
