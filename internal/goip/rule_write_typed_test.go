package goip

import (
	"bytes"
	"strings"
	"testing"

	"github.com/randomizedcoder/xtcp2/pkg/xtcpnl"
	"golang.org/x/sys/unix"
)

// TestRuleWriteTypedMatchesArgv is the claim xtcpnl_rule_write.go makes and
// cannot test alone: a RuleWrite and iprule_modify's argv loop produce the
// same bytes for the same rule. Both encoders are visible here. The argv is
// spelled with `table` first because the typed builder writes FRA_TABLE
// first; the kernel reads the attributes as a set, so internal/goip/actuate
// and `ip rule` agree on the request even though the CLI would let an
// operator type the keywords in another order.
//
// go test ./internal/goip -run TestRuleWriteTypedMatchesArgv
func TestRuleWriteTypedMatchesArgv(t *testing.T) {
	const addAck = uint16(unix.NLM_F_CREATE | unix.NLM_F_EXCL | unix.NLM_F_ACK)
	treatmentAttrs := []xtcpnl.RuleAttr{
		xtcpnl.RuleAttrU32(unix.FRA_PRIORITY, 30000), xtcpnl.RuleAttrU8(unix.FRA_IP_PROTO, unix.IPPROTO_TCP),
		xtcpnl.RuleAttrName(unix.FRA_IIFNAME, "lo"), xtcpnl.RuleAttrU8(unix.FRA_PROTOCOL, 242),
	}
	tests := []struct {
		description string
		family      uint8
		typed       xtcpnl.RuleWrite
		argv        string
	}{
		{
			description: "positive: the IPv4 treatment rule add is byte for byte `ip rule add table 10002 priority 30000 ipproto tcp iif lo protocol 242`",
			family:      unix.AF_INET,
			typed:       xtcpnl.RuleWrite{MsgType: unix.RTM_NEWRULE, Flags: addAck, Family: unix.AF_INET, Action: unix.FR_ACT_TO_TBL, Table: 10002, Attrs: treatmentAttrs},
			argv:        "table 10002 priority 30000 ipproto tcp iif lo protocol 242",
		},
		{
			description: "positive: the IPv6 treatment rule is the same request under `ip -6`",
			family:      unix.AF_INET6,
			typed:       xtcpnl.RuleWrite{MsgType: unix.RTM_NEWRULE, Flags: addAck, Family: unix.AF_INET6, Action: unix.FR_ACT_TO_TBL, Table: 10002, Attrs: treatmentAttrs},
			argv:        "table 10002 priority 30000 ipproto tcp iif lo protocol 242",
		},
		{
			description: "positive: the delete keeps every selector and drops the action and the create flags",
			family:      unix.AF_INET,
			typed:       xtcpnl.RuleWrite{MsgType: unix.RTM_DELRULE, Flags: unix.NLM_F_ACK, Family: unix.AF_INET, Action: unix.FR_ACT_UNSPEC, Table: 10002, Attrs: treatmentAttrs},
			argv:        "table 10002 priority 30000 ipproto tcp iif lo protocol 242",
		},
		{
			description: "boundary: table 255 stays in the header byte on both paths",
			family:      unix.AF_INET,
			typed:       xtcpnl.RuleWrite{MsgType: unix.RTM_NEWRULE, Flags: addAck, Family: unix.AF_INET, Action: unix.FR_ACT_TO_TBL, Table: 255, Attrs: treatmentAttrs[:1]},
			argv:        "table 255 priority 30000",
		},
		{
			description: "boundary: a /32 destination is FRA_DST with the full length, as `to 192.0.2.1/32`",
			family:      unix.AF_INET,
			typed: xtcpnl.RuleWrite{MsgType: unix.RTM_NEWRULE, Flags: addAck, Family: unix.AF_INET, DstLen: 32, Action: unix.FR_ACT_TO_TBL, Table: 10002,
				Attrs: []xtcpnl.RuleAttr{xtcpnl.RuleAttrBytes(unix.FRA_DST, []byte{192, 0, 2, 1})}},
			argv: "table 10002 to 192.0.2.1/32",
		},
		{
			description: "boundary: a /128 destination is sixteen bytes of FRA_DST, as `to 2001:db8::1/128`",
			family:      unix.AF_INET6,
			typed: xtcpnl.RuleWrite{MsgType: unix.RTM_NEWRULE, Flags: addAck, Family: unix.AF_INET6, DstLen: 128, Action: unix.FR_ACT_TO_TBL, Table: 10002,
				Attrs: []xtcpnl.RuleAttr{xtcpnl.RuleAttrBytes(unix.FRA_DST, []byte{0x20, 0x01, 0x0d, 0xb8, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 1})}},
			argv: "table 10002 to 2001:db8::1/128",
		},
		{
			description: "corner: `not` is the header flag on both paths",
			family:      unix.AF_INET,
			typed:       xtcpnl.RuleWrite{MsgType: unix.RTM_NEWRULE, Flags: addAck, Family: unix.AF_INET, Action: unix.FR_ACT_TO_TBL, FrhFlags: unix.FIB_RULE_INVERT, Table: 10002, Attrs: treatmentAttrs[:1]},
			argv:        "not table 10002 priority 30000",
		},
	}
	for _, tc := range tests {
		t.Run(tc.description, func(t *testing.T) {
			want, err := parseRuleWriteArgs(tc.family, tc.typed.MsgType, strings.Fields(tc.argv))
			if err != nil {
				t.Fatalf("%s: argv: %v", tc.description, err)
			}
			expected, err := xtcpnl.BuildWriteRequest(tc.typed.MsgType, tc.typed.Flags, 1, want.frh(), want.attrs.Bytes())
			if err != nil {
				t.Fatalf("%s: argv build: %v", tc.description, err)
			}
			got, err := xtcpnl.BuildRuleWriteRequest(tc.typed, 1)
			if err != nil {
				t.Fatalf("%s: typed: %v", tc.description, err)
			}
			if !bytes.Equal(got, expected) {
				t.Fatalf("%s:\n typed %x\n argv  %x", tc.description, got, expected)
			}
		})
	}
}
