package main

import (
	"fmt"
	"strings"
	"testing"
)

// TestFouSpace pins the fou rows: every row runs in a fresh namespace
// (fou_nl_family is netnsok true, per-netns), loads the fou module,
// captures generic netlink, and carries the expected outcome for its axes.
//
// Every positive fixture issues the same command (`ip fou show`, plain or
// `-j`); they differ only in the fou ports their `pre` installs, so the
// dump each captures differs. Rows are keyed by their (fix, opt) facts for
// the positives and by command for the uniquely spelled negatives.
func TestFouSpace(t *testing.T) {
	rows, err := matrixRows(families["fou"](), matrixOpts{Seed: 1})
	if err != nil {
		t.Fatal(err)
	}
	if err := checkRows(rows); err != nil {
		t.Fatal(err)
	}
	byCmd := map[string]row{}
	byFixOpt := map[string]row{}
	for _, r := range rows {
		// fou is per-netns, so rows need neither init_net nor serial.
		if r.InitNS || r.Serial {
			t.Errorf("%s: initns %t serial %t, want neither", r.ID, r.InitNS, r.Serial)
		}
		if r.Proto != "generic" || strings.Join(r.Modules, ",") != "fou" {
			t.Errorf("%s: proto %q modules %v", r.ID, r.Proto, r.Modules)
		}
		byCmd[r.Cmd] = r
		byFixOpt[r.Axes["fix"]+"/"+r.Axes["json"]] = r
	}

	// Negatives are uniquely spelled.
	neg := []struct {
		description string
		cmd         string
		expected    string // rc refused-by errno
	}{
		{"negative: help is refused before the wire", "ip fou help", "255 iproute2 "},
		{"negative: show takes no arguments", "ip fou show junk", "1 ack-then-fail "},
		{"negative: an unknown sub-command", "ip fou badobj", "255 ack-then-fail "},
	}
	for _, tc := range neg {
		t.Run(tc.description, func(t *testing.T) {
			r, ok := byCmd[tc.cmd]
			if !ok {
				t.Fatalf("no row runs %q", tc.cmd)
			}
			got := fmt.Sprintf("%d %s %s", *r.Expect.RC, r.Expect.RefusedBy, r.Expect.Errno)
			if got != tc.expected {
				t.Fatalf("got %q, want %q", got, tc.expected)
			}
		})
	}

	// Positives: each fixture at opt=plain (json "0") and opt=json ("1").
	pos := []struct {
		description string
		fix         string
		json        string
		cmd         string
		preCount    int
	}{
		{"boundary: empty fou list, plain", "empty", "0", "ip fou show", 0},
		{"boundary: empty fou list, json", "empty", "1", "ip -j fou show", 0},
		{"positive: one direct port", "direct", "0", "ip fou show", 1},
		{"positive: one gue port, json", "gue", "1", "ip -j fou show", 1},
		{"positive: several v4 ports", "multi", "0", "ip fou show", 3},
		{"positive: one v6 gue port", "v6", "0", "ip fou show", 1},
	}
	for _, tc := range pos {
		t.Run(tc.description, func(t *testing.T) {
			r, ok := byFixOpt[tc.fix+"/"+tc.json]
			if !ok {
				t.Fatalf("no row for fixture %q json %q", tc.fix, tc.json)
			}
			if r.Cmd != tc.cmd {
				t.Errorf("cmd %q, want %q", r.Cmd, tc.cmd)
			}
			if *r.Expect.RC != 0 || r.Expect.RefusedBy != refNone {
				t.Errorf("rc %d refused-by %s, want 0 none", *r.Expect.RC, r.Expect.RefusedBy)
			}
			if len(r.Pre) != tc.preCount {
				t.Errorf("%d pre steps, want %d", len(r.Pre), tc.preCount)
			}
		})
	}

	if len(rows) != 13 {
		t.Errorf("%d rows, want 13", len(rows))
	}
}
