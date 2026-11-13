package goip

import (
	"testing"

	"golang.org/x/sys/unix"
)

// TestShowSharesRealms checks that `route show` and `rule show` keep one
// rt_realms table across a dump, as one ip process does (R2, NX7e): realm 0
// prints "unknown" until an earlier realm has loaded the file, "cosmos"
// after.
//
// go test ./internal/goip/ -run TestShowSharesRealms
func TestShowSharesRealms(t *testing.T) {
	route := func(flow uint32) []byte {
		return catBytes(monRouteBody(unix.AF_INET, 0, 3), nla(unix.RTA_FLOW, le32(flow)))
	}
	rule := func(prio, flow uint32) []byte {
		return catBytes(mruleBody(unix.AF_INET, unix.RT_TABLE_MAIN, prio), nla(unix.FRA_FLOW, le32(flow)))
	}
	const r = "10.1.0.0/16 dev if3 proto static "
	tests := []struct {
		description string
		run         func(*runCtx, []string) (int, string, string)
		args        []string
		msgs        [][]byte
		expected    string
	}{
		{description: "positive: route show, realm 0 after realm 5 is cosmos", run: runRouteCtx, args: []string{"show"},
			msgs: [][]byte{route(5), route(0)}, expected: r + "realm 5 \n" + r + "realm cosmos \n"},
		{description: "negative: route show, realm 0 first is unknown", run: runRouteCtx, args: []string{"show"},
			msgs: [][]byte{route(0), route(5)}, expected: r + "realm unknown \n" + r + "realm 5 \n"},
		{description: "positive: rule show, realms 0 after realms 5 is cosmos", run: runRuleCtx, args: []string{"show"},
			msgs:     [][]byte{rule(100, 5), rule(101, 0)},
			expected: "100:\tfrom all lookup main realms 5\n101:\tfrom all lookup main realms cosmos\n"},
		{description: "boundary: rule show, realms 256 loads nothing", run: runRuleCtx, args: []string{"show"},
			msgs:     [][]byte{rule(100, 256), rule(101, 0)},
			expected: "100:\tfrom all lookup main realms 256\n101:\tfrom all lookup main realms unknown\n"},
	}
	for _, tc := range tests {
		t.Run(tc.description, func(t *testing.T) {
			c := &runCtx{src: &mruleFake{rules: tc.msgs}}
			rc, stdout, stderr := tc.run(c, tc.args)
			if rc != ExitOK || stdout != tc.expected {
				t.Errorf("rc %d, stdout %q, stderr %q; expected %q", rc, stdout, stderr, tc.expected)
			}
		})
	}
}
