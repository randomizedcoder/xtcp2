package render

import (
	"encoding/json"
	"testing"

	"golang.org/x/sys/unix"

	"github.com/randomizedcoder/xtcp2/pkg/xtcpnl"
)

// NX7f: the name tables iproute2 reads from files, compiled in.

// TestDsfieldN2A checks rtnl_dsfield_n2a and rtnl_dscp_n2a over one
// process's table: slot 0 is "0" until a lookup of an empty slot loads the
// file, which makes it "default".
//
// go test ./internal/goip/render/ -run TestDsfieldN2A
func TestDsfieldN2A(t *testing.T) {
	tests := []struct {
		description string
		loaded      bool
		dscp        bool // rtnl_dscp_n2a, else rtnl_dsfield_n2a
		id          uint8
		expected    string
		loadedAfter bool
	}{
		{description: "positive: EF loads the file", id: 0xb8, expected: "EF", loadedAfter: true},
		{description: "positive: AF11", id: 0x28, expected: "AF11", loadedAfter: true},
		{description: "negative: 0x10 has no line, hex after the load", id: 0x10, expected: "0x10", loadedAfter: true},
		{description: "corner: 0 before the load is the built-in \"0\" and loads nothing", id: 0, expected: "0"},
		{description: "corner: 0 after the load is the file's default", loaded: true, id: 0, expected: "default",
			loadedAfter: true},
		{description: "boundary: 0xff, the last slot, has no line", id: 0xff, expected: "0xff", loadedAfter: true},
		{description: "positive: dscp 46 is EF", dscp: true, id: 46, expected: "EF", loadedAfter: true},
		{description: "positive: dscp 10 is AF11", dscp: true, id: 10, expected: "AF11", loadedAfter: true},
		{description: "negative: dscp 4 has no line, decimal", dscp: true, id: 4, expected: "4", loadedAfter: true},
		{description: "corner: dscp 0 before the load", dscp: true, id: 0, expected: "0"},
		{description: "corner: dscp 0 after the load", dscp: true, loaded: true, id: 0, expected: "default",
			loadedAfter: true},
		{description: "boundary: dscp 63, the last that fits the table", dscp: true, id: 63, expected: "63",
			loadedAfter: true},
		{description: "boundary: dscp 64 shifts past the table and loads nothing", dscp: true, id: 64,
			expected: "64"},
	}
	for _, tc := range tests {
		t.Run(tc.description, func(t *testing.T) {
			d := Dsfield{loaded: tc.loaded}
			n2a := d.N2A
			if tc.dscp {
				n2a = d.DscpN2A
			}
			got := n2a(tc.id)
			if got != tc.expected || d.loaded != tc.loadedAfter {
				t.Errorf("got %q, loaded %v; expected %q, %v", got, d.loaded, tc.expected, tc.loadedAfter)
			}
		})
	}
}

// TestDsfieldByName checks rtnl_dsfield_a2n's names, which it reads after
// loading the file.
func TestDsfieldByName(t *testing.T) {
	tests := []struct {
		description string
		name        string
		expected    uint8
		ok          bool
	}{
		{description: "positive: EF", name: "EF", expected: 0xb8, ok: true},
		{description: "positive: CS7", name: "CS7", expected: 0xe0, ok: true},
		{description: "boundary: default is 0", name: "default", expected: 0, ok: true},
		{description: "negative: ef, names are case-sensitive", name: "ef"},
		{description: "corner: \"0\" is no name once the file is loaded", name: "0"},
	}
	for _, tc := range tests {
		t.Run(tc.description, func(t *testing.T) {
			got, ok := DsfieldByName(tc.name)
			if got != tc.expected || ok != tc.ok {
				t.Errorf("got %#x, %v; expected %#x, %v", got, ok, tc.expected, tc.ok)
			}
		})
	}
}

// TestProtoN2A checks inet_proto_n2a's and strxf_proto's fallbacks.
func TestProtoN2A(t *testing.T) {
	tests := []struct {
		description string
		proto       uint8
		expected    string // inet_proto_n2a
		strxf       string // strxf_proto
	}{
		{description: "positive: tcp", proto: unix.IPPROTO_TCP, expected: "tcp", strxf: "tcp"},
		{description: "positive: ipv6-icmp", proto: unix.IPPROTO_ICMPV6, expected: "ipv6-icmp", strxf: "ipv6-icmp"},
		{description: "boundary: 0 is hopopt", proto: 0, expected: "hopopt", strxf: "hopopt"},
		{description: "negative: 255 has no line", proto: 255, expected: "ipproto-255", strxf: "255"},
		{description: "corner: 135, the mobility header, has no line", proto: unix.IPPROTO_MH,
			expected: "ipproto-135", strxf: "135"},
	}
	for _, tc := range tests {
		t.Run(tc.description, func(t *testing.T) {
			if got := InetProtoN2A(tc.proto); got != tc.expected {
				t.Errorf("InetProtoN2A got %q, expected %q", got, tc.expected)
			}
			if got := StrxfProto(tc.proto); got != tc.strxf {
				t.Errorf("StrxfProto got %q, expected %q", got, tc.strxf)
			}
		})
	}
}

// TestIPProtocolsUnique checks the table's premise: one line per number
// and per name or alias, so a first match is the only match.
func TestIPProtocolsUnique(t *testing.T) {
	nums, names := map[uint8]bool{}, map[string]bool{}
	for _, p := range ipProtocols {
		if nums[p.num] || names[p.name] {
			t.Errorf("%d %s repeats", p.num, p.name)
		}
		nums[p.num], names[p.name] = true, true
		if p.alias != p.name && names[p.alias] {
			t.Errorf("alias %s repeats", p.alias)
		}
		names[p.alias] = true
	}
	if len(ipProtocols) != 137 {
		t.Errorf("%d lines, expected iana-etc 20251215's 137", len(ipProtocols))
	}
}

// ruleNamesBase is a rule print_rule prints as `N:\tfrom all lookup main`.
func ruleNamesBase(prio uint32) xtcpnl.RuleInfo {
	return xtcpnl.RuleInfo{
		Family: unix.AF_INET, Action: unix.FR_ACT_TO_TBL, Priority: prio, HasPriority: true,
		RawTable: unix.RT_TABLE_MAIN, Table: unix.RT_TABLE_MAIN,
		HasSuppressPrefixlen: true, SuppressPrefixlen: ruleSentinelCst,
	}
}

// TestRuleViewIPProtoDscp checks print_rule's ipproto (:405-410) and dscp
// (:560-581), in text and JSON, from a process that has looked nothing up.
//
// go test ./internal/goip/render/ -run TestRuleViewIPProtoDscp
func TestRuleViewIPProtoDscp(t *testing.T) {
	with := func(f func(*xtcpnl.RuleInfo)) xtcpnl.RuleInfo {
		ri := ruleNamesBase(5)
		f(&ri)
		return ri
	}
	tests := []struct {
		description string
		in          xtcpnl.RuleInfo
		expected    string
		json        string
	}{
		{description: "positive: ipproto tcp after uidrange, before sport",
			in: with(func(r *xtcpnl.RuleInfo) {
				r.IPProto, r.HasIPProto = unix.IPPROTO_TCP, true
				r.UidRange, r.HasUidRange = xtcpnl.FibRuleUidRange{Start: 1, End: 2}, true
				r.SportRange, r.HasSportRange = xtcpnl.FibRulePortRange{Start: 1, End: 2}, true
			}),
			expected: "5:\tfrom all uidrange 1-2 ipproto tcp sport 1-2 lookup main\n",
			json: `{"priority":5,"src":"all","uid_start":1,"uid_end":2,"ipproto":"tcp","sport_start":1,` +
				`"sport_end":2,"table":"main"}`},
		{description: "negative: ipproto 255 has no name",
			in:       with(func(r *xtcpnl.RuleInfo) { r.IPProto, r.HasIPProto = 255, true }),
			expected: "5:\tfrom all ipproto ipproto-255 lookup main\n",
			json:     `{"priority":5,"src":"all","ipproto":"ipproto-255","table":"main"}`},
		{description: "positive: dscp EF with no mask, after proto",
			in: with(func(r *xtcpnl.RuleInfo) {
				r.Dscp, r.HasDscp = 46, true
				r.Protocol, r.HasProtocol = unix.RTPROT_STATIC, true
			}),
			expected: "5:\tfrom all lookup main proto static dscp EF\n",
			json:     `{"priority":5,"src":"all","table":"main","protocol":"static","dscp":"EF"}`},
		{description: "corner: dscp 0 with the full mask, the process's first lookup",
			in: with(func(r *xtcpnl.RuleInfo) {
				r.Dscp, r.HasDscp, r.DscpMask, r.HasDscpMask = 0, true, 0x3f, true
			}),
			expected: "5:\tfrom all lookup main dscp 0\n",
			json:     `{"priority":5,"src":"all","table":"main","dscp":"0","dscp_mask":"0x3f"}`},
		{description: "positive: a partial mask prints both in hex, JSON names the dscp",
			in: with(func(r *xtcpnl.RuleInfo) {
				r.Dscp, r.HasDscp, r.DscpMask, r.HasDscpMask = 8, true, 0x30, true
			}),
			expected: "5:\tfrom all lookup main dscp 0x8/0x30\n",
			json:     `{"priority":5,"src":"all","table":"main","dscp":"CS1","dscp_mask":"0x30"}`},
		{description: "boundary: dscp 0 under a partial mask is C's %#x of 0",
			in: with(func(r *xtcpnl.RuleInfo) {
				r.Dscp, r.HasDscp, r.DscpMask, r.HasDscpMask = 0, true, 0x30, true
			}),
			expected: "5:\tfrom all lookup main dscp 0/0x30\n",
			json:     `{"priority":5,"src":"all","table":"main","dscp":"0","dscp_mask":"0x30"}`},
		{description: "corner: a tos earlier on the line loads the table for dscp 0",
			in: with(func(r *xtcpnl.RuleInfo) {
				r.Tos = 0xb8
				r.Dscp, r.HasDscp = 0, true
			}),
			expected: "5:\tfrom all tos EF lookup main dscp default\n",
			json:     `{"priority":5,"src":"all","tos":"EF","table":"main","dscp":"default"}`},
	}
	for _, tc := range tests {
		t.Run(tc.description, func(t *testing.T) {
			if got := RuleViewOf(tc.in).Text(); got != tc.expected {
				t.Errorf("Text() = %q, expected %q", got, tc.expected)
			}
			got, err := json.Marshal(RuleViewOf(tc.in))
			if err != nil || string(got) != tc.json {
				t.Errorf("JSON %s (%v), expected %s", got, err, tc.json)
			}
		})
	}
}

// TestRuleViewOfInSharesTable checks that one process's rules share the DS
// field table: dscp 0 prints "0" until an earlier rule has loaded it.
func TestRuleViewOfInSharesTable(t *testing.T) {
	zero := ruleNamesBase(2)
	zero.HasDscp = true
	tos := ruleNamesBase(1)
	tos.Tos = 0x10
	tests := []struct {
		description string
		rules       []xtcpnl.RuleInfo
		expected    string // the last rule's dscp
	}{
		{description: "positive: a tos before loads the table", rules: []xtcpnl.RuleInfo{tos, zero},
			expected: "default"},
		{description: "corner: dscp 0 first loads nothing", rules: []xtcpnl.RuleInfo{zero, zero}, expected: "0"},
		{description: "boundary: dscp 0 alone", rules: []xtcpnl.RuleInfo{zero}, expected: "0"},
	}
	for _, tc := range tests {
		t.Run(tc.description, func(t *testing.T) {
			var d Dsfield
			var v RuleView
			for _, ri := range tc.rules {
				v = RuleViewOfIn(ri, &d)
			}
			if v.Dscp != tc.expected {
				t.Errorf("got %q, expected %q", v.Dscp, tc.expected)
			}
		})
	}
}
