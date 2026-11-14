package render

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/randomizedcoder/xtcp2/pkg/xtcpnl"
	"golang.org/x/sys/unix"
)

// TestRealmsN2A checks rtnl_rtrealm_n2a's lazy load (R2): realm 0 is
// "unknown" until a lookup of another slot below 256 has loaded rt_realms,
// whose one name is "0 cosmos" (NX7e).
//
// go test ./internal/goip/render/ -run TestRealmsN2A
func TestRealmsN2A(t *testing.T) {
	tests := []struct {
		description string
		ids         []uint32 // looked up in order in one table
		expected    string   // the last lookup's name
	}{
		{description: "positive: realm 5", ids: []uint32{5}, expected: "5"},
		{description: "positive: 0 after another realm is cosmos", ids: []uint32{1, 0}, expected: "cosmos"},
		{description: "negative: 0 first is unknown", ids: []uint32{0}, expected: "unknown"},
		{description: "boundary: 255 loads the table", ids: []uint32{255, 0}, expected: "cosmos"},
		{description: "boundary: 256 is a number and loads nothing", ids: []uint32{256, 0}, expected: "unknown"},
		{description: "corner: 0 twice stays unknown", ids: []uint32{0, 0}, expected: "unknown"},
		{description: "corner: 65535, the largest half", ids: []uint32{65535}, expected: "65535"},
	}
	for _, tc := range tests {
		t.Run(tc.description, func(t *testing.T) {
			var r Realms
			var got string
			for _, id := range tc.ids {
				got = r.N2A(id)
			}
			if got != tc.expected {
				t.Errorf("got %q, expected %q", got, tc.expected)
			}
		})
	}
}

// TestRouteViewFlow checks print_rta_flow on a route and on a multipath
// path: `realm TO ` or `realms FROM/TO `, after `mark` and after a path's
// gateway, and JSON's "flow" {"from","to"}, with the table shared across a
// dump through RouteShowFilter.Realms.
//
// go test ./internal/goip/render/ -run TestRouteViewFlow
func TestRouteViewFlow(t *testing.T) {
	route := func(flow uint32, has bool) xtcpnl.RouteInfo {
		return xtcpnl.RouteInfo{Family: unix.AF_INET, DstLen: 24, Dst: []byte{198, 51, 100, 0},
			Table: unix.RT_TABLE_MAIN, Protocol: unix.RTPROT_BOOT, Scope: unix.RT_SCOPE_LINK,
			Type: unix.RTN_UNICAST, Oif: 2, Flow: flow, HasFlow: has}
	}
	multipath := route(0, false)
	multipath.Scope = unix.RT_SCOPE_UNIVERSE
	multipath.Oif = 0
	multipath.Multipath = []xtcpnl.RouteNextHop{
		{Ifindex: 2, Gateway: []byte{192, 0, 2, 1}, Flow: 3, HasFlow: true},
		{Ifindex: 2, Gateway: []byte{192, 0, 2, 2}},
	}
	tests := []struct {
		description string
		routes      []xtcpnl.RouteInfo // one dump, the last checked
		expected    string             // in the last route's text
		json        string             // in its JSON, "" for no "flow"
	}{
		{description: "positive: realm 5", routes: []xtcpnl.RouteInfo{route(5, true)},
			expected: "scope link realm 5 \n", json: `"flow":{"to":"5"}`},
		{description: "positive: realms 1/2", routes: []xtcpnl.RouteInfo{route(1<<16|2, true)},
			expected: "scope link realms 1/2 \n", json: `"flow":{"from":"1","to":"2"}`},
		{description: "positive: a path's realm, before its dev", routes: []xtcpnl.RouteInfo{multipath},
			expected: "\tnexthop via 192.0.2.1 realm 3 dev if2 weight 1 \n\tnexthop via 192.0.2.2 dev if2 weight 1 ", json: `"flow":{"to":"3"}`},
		{description: "negative: no RTA_FLOW", routes: []xtcpnl.RouteInfo{route(0, false)}, expected: "scope link \n"},
		{description: "boundary: realm 0 first is unknown", routes: []xtcpnl.RouteInfo{route(0, true)},
			expected: "realm unknown \n", json: `"flow":{"to":"unknown"}`},
		{description: "corner: realm 0 after a realm 5 in the dump is cosmos",
			routes:   []xtcpnl.RouteInfo{route(5, true), route(0, true)},
			expected: "realm cosmos \n", json: `"flow":{"to":"cosmos"}`},
		{description: "corner: realms 2/0 load before the 0", routes: []xtcpnl.RouteInfo{route(2<<16, true)},
			expected: "realms 2/cosmos \n", json: `"flow":{"from":"2","to":"cosmos"}`},
	}
	for _, tc := range tests {
		t.Run(tc.description, func(t *testing.T) {
			f := RouteShowFilter{Realms: new(Realms)}
			var v RouteView
			for _, ri := range tc.routes {
				v = RouteViewOf(ri, fakeNames{}, f)
			}
			if text := v.Text(); !strings.Contains(text, tc.expected) {
				t.Errorf("text %q, expected %q", text, tc.expected)
			}
			b, err := json.Marshal(v)
			if err != nil {
				t.Fatal(err)
			}
			if has := strings.Contains(string(b), `"flow"`); has != (tc.json != "") ||
				tc.json != "" && !strings.Contains(string(b), tc.json) {
				t.Errorf("JSON %s, expected %q", b, tc.json)
			}
		})
	}
}

// TestRuleViewRealms checks print_rule's realms through the same table: a
// rule's realm 0 is "unknown" until an earlier lookup loaded rt_realms.
//
// go test ./internal/goip/render/ -run TestRuleViewRealms
func TestRuleViewRealms(t *testing.T) {
	rule := func(flow uint32) xtcpnl.RuleInfo {
		return xtcpnl.RuleInfo{Family: unix.AF_INET, Table: 100, Action: unix.FR_ACT_TO_TBL,
			Priority: 10, HasPriority: true, Flow: flow, HasFlow: true}
	}
	tests := []struct {
		description string
		rules       []xtcpnl.RuleInfo // one dump, the last checked
		expected    string
	}{
		{description: "positive: realms 5", rules: []xtcpnl.RuleInfo{rule(5)}, expected: "lookup 100 realms 5"},
		{description: "positive: realms 1/2", rules: []xtcpnl.RuleInfo{rule(1<<16 | 2)}, expected: "lookup 100 realms 1/2"},
		{description: "negative: realm 0 first is unknown", rules: []xtcpnl.RuleInfo{rule(0)}, expected: "lookup 100 realms unknown"},
		{description: "corner: realm 0 after a realms 5 is cosmos", rules: []xtcpnl.RuleInfo{rule(5), rule(0)},
			expected: "lookup 100 realms cosmos"},
	}
	for _, tc := range tests {
		t.Run(tc.description, func(t *testing.T) {
			var (
				d Dsfield
				r Realms
				v RuleView
			)
			for _, ri := range tc.rules {
				v = RuleViewOfIn(ri, &d, &r)
			}
			if text := v.Text(); !strings.Contains(text, tc.expected) {
				t.Errorf("text %q, expected %q", text, tc.expected)
			}
		})
	}
}
