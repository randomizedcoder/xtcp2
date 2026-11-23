package main

import (
	"bytes"
	"fmt"
	"strings"
	"testing"
)

// TestTipcSpace pins the tipc rows (genl-clones 5C): every row runs in a
// fresh namespace (TIPCv2 is netnsok true, its state per-netns), loads
// tipc, captures generic netlink, and carries the expected outcome for its
// axes. Positives are keyed by command and json; negatives by command.
//
// go test ./tools/nlcapgen/ -run TestTipcSpace
func TestTipcSpace(t *testing.T) {
	rows, err := matrixRows(families["tipc"](), matrixOpts{Seed: 1})
	if err != nil {
		t.Fatal(err)
	}
	if err := checkRows(rows); err != nil {
		t.Fatal(err)
	}
	byCmd := map[string]row{}
	byFix := map[string][]row{}
	for _, r := range rows {
		if r.InitNS || r.Serial {
			t.Errorf("%s: initns %t serial %t, want neither", r.ID, r.InitNS, r.Serial)
		}
		if r.Proto != "generic" || len(r.Modules) != 1 || r.Modules[0] != "tipc" {
			t.Errorf("%s: proto %q modules %v, want generic and [tipc]", r.ID, r.Proto, r.Modules)
		}
		if _, dup := byCmd[r.Cmd+"|"+r.Axes["fix"]]; dup {
			t.Errorf("%s: %q in fixture %q twice", r.ID, r.Cmd, r.Axes["fix"])
		}
		byCmd[r.Cmd+"|"+r.Axes["fix"]] = r
		byFix[r.Axes["fix"]] = append(byFix[r.Axes["fix"]], r)
	}

	neg := []struct {
		description string
		cmd         string
		expected    string // rc refused-by errno extack | pre count
	}{
		{"negative: an unknown option is getopt's, before the family", "tipc -z node get address", "1 iproute2   | 0"},
		{"boundary: no object prints the usage after the lookup", "tipc", "255 ack-then-fail   | 0"},
		{"negative: help is no object", "tipc help", "255 ack-then-fail   | 0"},
		{"negative: an unknown object", "tipc badobj", "255 ack-then-fail   | 0"},
		{"corner: n is ambiguous, nametable or node", "tipc n", "255 ack-then-fail   | 0"},
		{"negative: an unknown link sub-command", "tipc link badcmd", "255 ack-then-fail   | 0"},
		{"negative: link get with no link name", "tipc link get priority", "255 ack-then-fail   | 0"},
		{"negative: the kernel has no such link", "tipc link get priority link nosuch", "255 kernel EINVAL  | 0"},
		{"negative: an unknown node property", "tipc node get badprop", "255 ack-then-fail   | 0"},
		{"negative: an eth bearer with no device", "tipc bearer get priority media eth", "255 ack-then-fail   | 0"},
		{"corner: mtu is refused for eth, after the eth setup", "tipc bearer get mtu media eth device d0",
			"255 ack-then-fail   | 4"},
		{"negative: the kernel has no such bearer", "tipc bearer get priority media udp name nosuch",
			"255 kernel EINVAL Bearer not found | 0"},
		{"negative: the kernel has no such media", "tipc media get priority media nosuch",
			"255 kernel EINVAL Media not found | 0"},
	}
	for _, tc := range neg {
		t.Run(tc.description, func(t *testing.T) {
			r, ok := byCmd[tc.cmd+"|"]
			if !ok { // a bad option keeps its base's command, and its fixture
				r, ok = byCmd[tc.cmd+"|bare"]
			}
			if !ok {
				t.Fatalf("no negative row runs %q", tc.cmd)
			}
			if !strings.HasPrefix(r.ID, "tipc/ng-") {
				t.Errorf("%s: %q is not a negative row", r.ID, tc.cmd)
			}
			got := fmt.Sprintf("%d %s %s %s | %d", *r.Expect.RC, r.Expect.RefusedBy, r.Expect.Errno, r.Expect.Extack, len(r.Pre))
			if got != tc.expected {
				t.Errorf("got %q, want %q", got, tc.expected)
			}
		})
	}

	pos := []struct {
		description string
		cmd         string
		fix         string
		expected    string // rc refused-by | pre count
	}{
		{"positive: bare node get address", "tipc node get address", "bare", "0 none | 0"},
		{"boundary: bare bearer list, an empty dump", "tipc bearer list", "bare", "0 none | 0"},
		{"corner: a stats filter no link holds, an empty dump", "tipc link statistics show link nosuch", "bare", "0 none | 0"},
		{"boundary: an identity alone leaves address 0", "tipc node get address", "ident", "0 none | 1"},
		{"positive: eth bearer list, json", "tipc -j bearer list", "eth", "0 none | 4"},
		{"positive: eth node address, after the wait", "tipc node get address", "eth", "0 none | 4"},
		{"corner: -j link get broadcast dies of SIGSEGV", "tipc -j link get broadcast", "eth", "139 ack-then-fail | 4"},
		{"positive: plain link get broadcast", "tipc link get broadcast", "eth", "0 none | 4"},
		{"positive: udp bearer mtu", "tipc bearer get mtu media udp name u0", "udp", "0 none | 4"},
		{"positive: peer link get, its link by name", "tipc link get priority link " + tipcPeerLink, "peer", "0 none | 4"},
		{"positive: peer monitor list, json", "tipc -j link monitor list", "peer", "0 none | 4"},
		{"corner: -j kept on node list, which prints text", "tipc -j node list", "peer", "0 none | 4"},
		{"positive: socket list over held sockets", "tipc socket list", "sock", "0 none | 2"},
	}
	for _, tc := range pos {
		t.Run(tc.description, func(t *testing.T) {
			r, ok := byCmd[tc.cmd+"|"+tc.fix]
			if !ok {
				t.Fatalf("no row runs %q in fixture %q", tc.cmd, tc.fix)
			}
			got := fmt.Sprintf("%d %s | %d", *r.Expect.RC, r.Expect.RefusedBy, len(r.Pre))
			if got != tc.expected {
				t.Errorf("got %q, want %q", got, tc.expected)
			}
		})
	}

	// Every fixture after bare sets the identity first, so a node's address
	// is the identity's hash on every boot.
	for fix, rs := range byFix {
		for _, r := range rs {
			if fix != "bare" && fix != "" && (len(r.Pre) == 0 || r.Pre[0].Cmd != tipcIdent) {
				t.Errorf("%s: fixture %s does not set the identity first", r.ID, fix)
			}
		}
	}
	// The peer step makes the veth pair before the row's bearer names v0,
	// and the sock step waits for its sockets.
	peer := byCmd["tipc link list|peer"]
	if len(peer.Pre) != 4 || !strings.Contains(peer.Pre[1].Cmd, "ip link add v0") ||
		!strings.Contains(peer.Pre[2].Cmd, "device v0") {
		t.Errorf("peer pre out of order: %+v", peer.Pre)
	}
	if sock := byCmd["tipc socket list|sock"]; !strings.Contains(sock.Pre[len(sock.Pre)-1].Cmd, `[ -e "$f" ]`) {
		t.Errorf("sock pre does not wait for its sockets: %+v", sock.Pre)
	}
	if len(rows) != 93 {
		t.Errorf("%d rows, want 93 (40 commands x 2 opts + 13 negatives)", len(rows))
	}
	// Two generations of the space are the same rows file.
	again, err := matrixRows(families["tipc"](), matrixOpts{Seed: 1})
	if err != nil {
		t.Fatal(err)
	}
	var a, b bytes.Buffer
	if err := writeRows(&a, nil, rows); err != nil {
		t.Fatal(err)
	}
	if err := writeRows(&b, nil, again); err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(a.Bytes(), b.Bytes()) {
		t.Error("two generations of the tipc space differ")
	}
}
