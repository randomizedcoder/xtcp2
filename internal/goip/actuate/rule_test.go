package actuate

import (
	"encoding/binary"
	"reflect"
	"strings"
	"testing"

	"github.com/randomizedcoder/xtcp2/pkg/xtcpnl"
	"golang.org/x/sys/unix"
)

// TestTreatmentRuleRequest is the rule the backend owns, read back through
// xtcpnl.ParseRule: the typed builder's output carries exactly the selectors
// ownership compares on, and a backend outside the reserved ranges cannot
// build one at all. Byte equality with `ip rule` is internal/goip's
// TestRuleWriteTypedMatchesArgv.
//
// go test ./internal/goip/actuate -run TestTreatmentRuleRequest
func TestTreatmentRuleRequest(t *testing.T) {
	const addAck = uint16(unix.NLM_F_REQUEST | unix.NLM_F_ACK | unix.NLM_F_CREATE | unix.NLM_F_EXCL)
	owned := func(family uint8, action uint8, table uint32) xtcpnl.RuleInfo {
		return xtcpnl.RuleInfo{Family: family, Action: action, Table: table, Priority: 30000, HasPriority: true,
			IifName: "lo", HasIifName: true, IPProto: unix.IPPROTO_TCP, HasIPProto: true, Protocol: 242, HasProtocol: true}
	}
	tests := []struct {
		description string
		backend     Backend
		family      uint8
		del         bool
		wantType    uint16
		wantFlags   uint16
		expected    xtcpnl.RuleInfo
		wantErr     string
	}{
		{
			description: "positive: the IPv4 add is RTM_NEWRULE to the private table with priority, tcp, iif lo and the private protocol",
			backend:     Backend{Table: 10002, Priority: 30000, Protocol: 242, Enabled: true},
			family:      unix.AF_INET, wantType: unix.RTM_NEWRULE, wantFlags: addAck,
			expected: owned(unix.AF_INET, unix.FR_ACT_TO_TBL, 10002),
		},
		{
			description: "positive: the IPv6 add differs only in family",
			backend:     Backend{Table: 10002, Priority: 30000, Protocol: 242, Enabled: true},
			family:      unix.AF_INET6, wantType: unix.RTM_NEWRULE, wantFlags: addAck,
			expected: owned(unix.AF_INET6, unix.FR_ACT_TO_TBL, 10002),
		},
		{
			description: "positive: the delete keeps every selector so the kernel removes exactly this rule, with no action and no create flags",
			backend:     Backend{Table: 10002, Priority: 30000, Protocol: 242, Enabled: true},
			family:      unix.AF_INET, del: true, wantType: unix.RTM_DELRULE, wantFlags: unix.NLM_F_REQUEST | unix.NLM_F_ACK,
			expected: owned(unix.AF_INET, unix.FR_ACT_UNSPEC, 10002),
		},
		{
			description: "boundary: table 256, the first private table, travels in FRA_TABLE and reads back whole",
			backend:     Backend{Table: 256, Priority: 1, Protocol: 192, Enabled: true},
			family:      unix.AF_INET, wantType: unix.RTM_NEWRULE, wantFlags: addAck,
			expected: xtcpnl.RuleInfo{Family: unix.AF_INET, Action: unix.FR_ACT_TO_TBL, Table: 256, Priority: 1, HasPriority: true,
				IifName: "lo", HasIifName: true, IPProto: unix.IPPROTO_TCP, HasIPProto: true, Protocol: 192, HasProtocol: true},
		},
		{
			description: "corner: a disabled backend still describes its rule, so readback can recognise leftovers before Enable",
			backend:     Backend{Table: 10002, Priority: 30000, Protocol: 242},
			family:      unix.AF_INET, wantType: unix.RTM_NEWRULE, wantFlags: addAck,
			expected: owned(unix.AF_INET, unix.FR_ACT_TO_TBL, 10002),
		},
		{
			description: "negative: no table (zero) is refused at build time rather than sent as a rule into the local table",
			backend:     Backend{Priority: 30000, Protocol: 242, Enabled: true},
			family:      unix.AF_INET, wantErr: "private table",
		},
		{
			description: "negative: a well-known table (main, 254) is refused",
			backend:     Backend{Table: unix.RT_TABLE_MAIN, Priority: 30000, Protocol: 242, Enabled: true},
			family:      unix.AF_INET, wantErr: "private table",
		},
		{
			description: "boundary: priority 0 is the kernel's local rule and is refused",
			backend:     Backend{Table: 10002, Protocol: 242, Enabled: true},
			family:      unix.AF_INET, wantErr: "priority 1..32765",
		},
		{
			description: "boundary: priority 32766 is the main rule and is refused",
			backend:     Backend{Table: 10002, Priority: 32766, Protocol: 242, Enabled: true},
			family:      unix.AF_INET, wantErr: "priority 1..32765",
		},
		{
			description: "negative: an IANA protocol number (191) is refused; readback would not tell the rule from a static one",
			backend:     Backend{Table: 10002, Priority: 30000, Protocol: 191, Enabled: true},
			family:      unix.AF_INET, wantErr: "protocol 192..255",
		},
	}
	for _, tc := range tests {
		t.Run(tc.description, func(t *testing.T) {
			raw, err := tc.backend.rule(tc.family, tc.del)
			if tc.wantErr != "" {
				if err == nil || !strings.Contains(err.Error(), tc.wantErr) {
					t.Fatalf("%s: err=%v, want %q", tc.description, err, tc.wantErr)
				}
				return
			}
			if err != nil {
				t.Fatalf("%s: %v", tc.description, err)
			}
			if typ := binary.LittleEndian.Uint16(raw[4:6]); typ != tc.wantType {
				t.Fatalf("%s: nlmsg_type %d, want %d", tc.description, typ, tc.wantType)
			}
			if flags := binary.LittleEndian.Uint16(raw[6:8]); flags != tc.wantFlags {
				t.Fatalf("%s: nlmsg_flags %#x, want %#x", tc.description, flags, tc.wantFlags)
			}
			got, err := xtcpnl.ParseRule(raw[xtcpnl.NlMsgHdrSizeCst:])
			if err != nil {
				t.Fatalf("%s: %v", tc.description, err)
			}
			// FRA_TABLE is authoritative; the header byte is RT_TABLE_UNSPEC
			// for every private table, which ownRule also discards.
			got.RawTable = 0
			if !reflect.DeepEqual(got, tc.expected) {
				t.Fatalf("%s:\n got %+v\nwant %+v", tc.description, got, tc.expected)
			}
			// Ownership is decided against what the kernel reports, which is
			// always the installed (add) form; a delete carries no action.
			if !tc.del && !tc.backend.ownRule(got) {
				t.Fatalf("%s: the backend does not recognise its own rule", tc.description)
			}
		})
	}
}
