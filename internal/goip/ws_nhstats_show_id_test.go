package goip

import (
	"path/filepath"
	"testing"
)

// TestWsNhstatsShowID replays nhstats ec-006's `ip -s nexthop show id 20`
// reply, a seg6local nexthop with counters, under other options: its
// counters print under -s alone, in text and JSON. The kernel sends
// SEG6_LOCAL_COUNTERS whatever the get's NHA_OP_FLAGS, so the reply serves
// every row.
//
// go test ./internal/goip/ -run TestWsNhstatsShowID
func TestWsNhstatsShowID(t *testing.T) {
	useLLMap720(t)
	dir := filepath.Join(wsO5SpacePath("nhstats"), "ec-006")
	tests := []struct {
		description string
		cmd         string
		expected    string
	}{
		{"positive: -s, as ip printed it", "ip -s nexthop show id 20",
			"id 20  encap seg6local action End packets 0 bytes 0 errors 0 dev d0 scope link \n"},
		{"positive: -j -s, the stats64 object", "ip -j -s nexthop show id 20",
			`[{"id":20,"encap":{"encap_type":"seg6local","action":"End","stats64":{"packets":0,"bytes":0,"errors":0}},` +
				`"dev":"d0","scope":"link","flags":[]}]` + "\n"},
		{"negative: no -s prints no counters", "ip nexthop show id 20",
			"id 20  encap seg6local action End dev d0 scope link \n"},
		{"negative: -j without -s prints no stats64", "ip -j nexthop show id 20",
			`[{"id":20,"encap":{"encap_type":"seg6local","action":"End"},"dev":"d0","scope":"link","flags":[]}]` + "\n"},
		{"boundary: -s -s prints the same counters", "ip -s -s nexthop show id 20",
			"id 20  encap seg6local action End packets 0 bytes 0 errors 0 dev d0 scope link \n"},
		{"corner: -d -s adds scope and proto, then the counters stay in the encap", "ip -d -s nexthop show id 20",
			"id 20  encap seg6local action End packets 0 bytes 0 errors 0 dev d0 scope link proto unspec \n"},
	}
	for _, tc := range tests {
		t.Run(tc.description, func(t *testing.T) {
			r := wsReadbackRun(t, dir, "show_id_s", tc.cmd)
			if r.rc != ExitOK || r.stdout != tc.expected {
				t.Errorf("rc %d stdout %q stderr %q, expected %q", r.rc, r.stdout, r.stderr, tc.expected)
			}
		})
	}
}
