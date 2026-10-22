package render

import (
	"encoding/json"
	"testing"

	"github.com/randomizedcoder/xtcp2/pkg/xtcpnl"
)

// TestNsidView is print_nsid's line and JSON object. The text rows with a
// name were measured: `ip netns list-id`, iproute2-7.1.0 in an unshare -rmn
// namespace with p1 and p2 added.
//
// go test ./internal/goip/render/ -run TestNsidView
func TestNsidView(t *testing.T) {
	type result struct{ text, json string }
	tests := []struct {
		description string
		ni          xtcpnl.NsidInfo
		deleted     bool
		name        string
		expected    result
	}{
		{description: "positive: an nsid and its name (measured)",
			ni: xtcpnl.NsidInfo{NSID: 1, HasNSID: true}, name: "p1",
			expected: result{"nsid 1 (iproute2 netns name: p1)\n", `{"nsid":1,"name":"p1"}`}},
		{description: "positive: a current nsid names the line (measured)",
			ni: xtcpnl.NsidInfo{NSID: -1, HasNSID: true, CurrentNSID: 1, HasCurrentNSID: true}, name: "p1",
			expected: result{"nsid unassigned current-nsid 1 (iproute2 netns name: p1)\n",
				`{"current-nsid":1,"name":"p1"}`}},
		{description: "boundary: nsid 0 is assigned",
			ni:       xtcpnl.NsidInfo{NSID: 0, HasNSID: true},
			expected: result{"nsid 0 \n", `{"nsid":0}`}},
		{description: "corner: both unassigned, no name, is an empty object",
			ni:       xtcpnl.NsidInfo{NSID: -1, HasNSID: true, CurrentNSID: -2, HasCurrentNSID: true},
			expected: result{"nsid unassigned current-nsid unassigned \n", `{}`}},
		{description: "negative: a current nsid not in the reply is not printed",
			ni:       xtcpnl.NsidInfo{NSID: 3, HasNSID: true, CurrentNSID: 4},
			expected: result{"nsid 3 \n", `{"nsid":3}`}},
		{description: "positive: an RTM_DELNSID is Deleted",
			ni: xtcpnl.NsidInfo{NSID: 2, HasNSID: true}, deleted: true, name: "p2",
			expected: result{"Deleted nsid 2 (iproute2 netns name: p2)\n", `{"deleted":true,"nsid":2,"name":"p2"}`}},
	}
	for _, tc := range tests {
		t.Run(tc.description, func(t *testing.T) {
			v := NsidViewOf(&tc.ni, tc.deleted, tc.name)
			j, err := json.Marshal(v)
			if err != nil {
				t.Fatal(err)
			}
			if got := (result{v.Text(), string(j)}); got != tc.expected {
				t.Errorf("got %+v, want %+v", got, tc.expected)
			}
		})
	}
}
