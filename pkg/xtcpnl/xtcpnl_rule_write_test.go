package xtcpnl

import (
	"bytes"
	"errors"
	"reflect"
	"testing"

	"golang.org/x/sys/unix"
)

// This file tests xtcpnl_rule_write.go, the typed counterpart of the argv
// path in internal/goip/rule_write.go. The hex is written out by hand from
// the layout rules of iprule_modify and lib/libnetlink.c, not produced by
// any encoder; ParseRule is the independent reader for the rest. The
// typed-equals-argv claim is internal/goip's test
// (TestRuleWriteTypedMatchesArgv), where both encoders are visible.

// TestBuildRuleWriteRequest is the fib_rule_hdr, FRA_TABLE's placement and
// the plain attributes.
//
// go test ./pkg/xtcpnl/ -run TestBuildRuleWriteRequest
func TestBuildRuleWriteRequest(t *testing.T) {
	const addAck = uint16(unix.NLM_F_CREATE | unix.NLM_F_EXCL | unix.NLM_F_ACK)
	v6dst := []byte{0x20, 0x01, 0x0d, 0xb8, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 1}
	treatment := func(msgType, flags uint16, family, action uint8) RuleWrite {
		return RuleWrite{
			MsgType: msgType, Flags: flags, Family: family, Action: action, Table: 10002,
			Attrs: []RuleAttr{
				RuleAttrU32(unix.FRA_PRIORITY, 30000), RuleAttrU8(unix.FRA_IP_PROTO, unix.IPPROTO_TCP),
				RuleAttrName(unix.FRA_IIFNAME, "lo"), RuleAttrU8(unix.FRA_PROTOCOL, 242),
			},
		}
	}
	// Bytes 16: of the add below, so the delete and family rows can share it.
	treatmentBody := `02000000 00000001 00000000
	                  0800 0f00 12270000
	                  0800 0600 30750000
	                  0500 1600 06000000
	                  0700 0300 6c6f0000
	                  0500 1500 f2000000`

	tests := []struct {
		description string
		w           RuleWrite
		expected    []byte
		wantErr     error
	}{
		{
			description: "positive: the treatment rule, `add table 10002 priority 30000 ipproto tcp iif lo protocol 242`, is FRA_TABLE first and 68 bytes",
			w:           treatment(unix.RTM_NEWRULE, addAck, unix.AF_INET, unix.FR_ACT_TO_TBL),
			expected:    hexBytes(t, `44000000 2000 0506 01000000 00000000 `+treatmentBody),
		},
		{
			description: "positive: the same rule's delete is RTM_DELRULE, NLM_F_ACK alone and FR_ACT_UNSPEC, selectors kept",
			w:           treatment(unix.RTM_DELRULE, unix.NLM_F_ACK, unix.AF_INET, unix.FR_ACT_UNSPEC),
			expected: hexBytes(t, `44000000 2100 0500 01000000 00000000
			                       02000000 00000000 00000000
			                       0800 0f00 12270000
			                       0800 0600 30750000
			                       0500 1600 06000000
			                       0700 0300 6c6f0000
			                       0500 1500 f2000000`),
		},
		{
			description: "positive: `add to 2001:db8::1/128 table 10002` is AF_INET6, dst_len 128 and a 16-byte FRA_DST after FRA_TABLE",
			w: RuleWrite{
				MsgType: unix.RTM_NEWRULE, Flags: addAck, Family: unix.AF_INET6, DstLen: 128, Action: unix.FR_ACT_TO_TBL, Table: 10002,
				Attrs: []RuleAttr{RuleAttrBytes(unix.FRA_DST, v6dst)},
			},
			expected: hexBytes(t, `38000000 2000 0506 01000000 00000000
			                       0a800000 00000001 00000000
			                       0800 0f00 12270000
			                       1400 0100 20010db8 00000000 00000000 00000001`),
		},
		{
			description: "boundary: table 255 is the last id the header byte holds; no FRA_TABLE",
			w:           RuleWrite{MsgType: unix.RTM_NEWRULE, Flags: addAck, Family: unix.AF_INET, Action: unix.FR_ACT_TO_TBL, Table: 255},
			expected:    hexBytes(t, `1c000000 2000 0506 01000000 00000000 02000000 ff000001 00000000`),
		},
		{
			description: "boundary: table 256 is the first id that moves to FRA_TABLE, the header byte RT_TABLE_UNSPEC",
			w:           RuleWrite{MsgType: unix.RTM_NEWRULE, Flags: addAck, Family: unix.AF_INET, Action: unix.FR_ACT_TO_TBL, Table: 256},
			expected:    hexBytes(t, `24000000 2000 0506 01000000 00000000 02000000 00000001 00000000 0800 0f00 00010000`),
		},
		{
			description: "boundary: table 0 sends neither byte nor attribute, as `ip rule del pref 7` does",
			w:           RuleWrite{MsgType: unix.RTM_DELRULE, Flags: unix.NLM_F_ACK, Family: unix.AF_INET, Attrs: []RuleAttr{RuleAttrU32(unix.FRA_PRIORITY, 7)}},
			expected:    hexBytes(t, `24000000 2100 0500 01000000 00000000 02000000 00000000 00000000 0800 0600 07000000`),
		},
		{
			description: "corner: `not` is FIB_RULE_INVERT in fib_rule_hdr.flags, little-endian",
			w:           RuleWrite{MsgType: unix.RTM_NEWRULE, Flags: addAck, Family: unix.AF_INET, Action: unix.FR_ACT_TO_TBL, Table: 254, FrhFlags: unix.FIB_RULE_INVERT},
			expected:    hexBytes(t, `1c000000 2000 0506 01000000 00000000 02000000 fe000001 02000000`),
		},
		{
			description: "negative: RTM_NEWROUTE is admitted by BuildWriteRequest but is not a rule write",
			w:           RuleWrite{MsgType: unix.RTM_NEWROUTE, Flags: addAck, Family: unix.AF_INET, Table: 10002},
			wantErr:     ErrRuleWriteType,
		},
		{
			description: "negative: an attribute past iprule_modify's 1024-byte buffer is refused rather than truncated",
			w: RuleWrite{MsgType: unix.RTM_NEWRULE, Flags: addAck, Family: unix.AF_INET, Action: unix.FR_ACT_TO_TBL, Table: 254,
				Attrs: []RuleAttr{RuleAttrBytes(unix.FRA_DST, make([]byte, ruleWriteBufCst))}},
			wantErr: ErrAttrNoSpace,
		},
	}
	for _, tc := range tests {
		t.Run(tc.description, func(t *testing.T) {
			got, err := BuildRuleWriteRequest(tc.w, 1)
			if tc.wantErr != nil {
				if !errors.Is(err, tc.wantErr) {
					t.Fatalf("%s: err=%v, want %v", tc.description, err, tc.wantErr)
				}
				return
			}
			if err != nil {
				t.Fatalf("%s: %v", tc.description, err)
			}
			if !bytes.Equal(got, tc.expected) {
				t.Fatalf("%s:\n got %x\nwant %x", tc.description, got, tc.expected)
			}
		})
	}
}

// TestRuleWriteReadsBack closes the loop through ParseRule: what the builder
// writes, the decoder reports, including the table id above 255 that the
// header byte cannot hold.
//
// go test ./pkg/xtcpnl/ -run TestRuleWriteReadsBack
func TestRuleWriteReadsBack(t *testing.T) {
	const addAck = uint16(unix.NLM_F_CREATE | unix.NLM_F_EXCL | unix.NLM_F_ACK)
	tests := []struct {
		description string
		w           RuleWrite
		expected    RuleInfo
	}{
		{
			description: "positive: the treatment rule reads back as priority 30000, table 10002, tcp from lo, protocol 242",
			w: RuleWrite{MsgType: unix.RTM_NEWRULE, Flags: addAck, Family: unix.AF_INET, Action: unix.FR_ACT_TO_TBL, Table: 10002,
				Attrs: []RuleAttr{RuleAttrU32(unix.FRA_PRIORITY, 30000), RuleAttrU8(unix.FRA_IP_PROTO, unix.IPPROTO_TCP), RuleAttrName(unix.FRA_IIFNAME, "lo"), RuleAttrU8(unix.FRA_PROTOCOL, 242)}},
			expected: RuleInfo{Family: unix.AF_INET, Action: unix.FR_ACT_TO_TBL, RawTable: unix.RT_TABLE_UNSPEC, Table: 10002, Priority: 30000, HasPriority: true,
				IifName: "lo", HasIifName: true, IPProto: unix.IPPROTO_TCP, HasIPProto: true, Protocol: 242, HasProtocol: true},
		},
		{
			description: "boundary: a header-byte table reads back from the byte, FRA_TABLE absent",
			w:           RuleWrite{MsgType: unix.RTM_NEWRULE, Flags: addAck, Family: unix.AF_INET6, Action: unix.FR_ACT_TO_TBL, Table: 200},
			expected:    RuleInfo{Family: unix.AF_INET6, Action: unix.FR_ACT_TO_TBL, RawTable: 200, Table: 200},
		},
		{
			description: "corner: a /128 destination keeps its full length and all sixteen bytes",
			w: RuleWrite{MsgType: unix.RTM_DELRULE, Flags: unix.NLM_F_ACK, Family: unix.AF_INET6, DstLen: 128,
				Attrs: []RuleAttr{RuleAttrBytes(unix.FRA_DST, bytes.Repeat([]byte{0xab}, 16))}},
			expected: RuleInfo{Family: unix.AF_INET6, DstLen: 128, Dst: bytes.Repeat([]byte{0xab}, 16)},
		},
	}
	for _, tc := range tests {
		t.Run(tc.description, func(t *testing.T) {
			msg, err := BuildRuleWriteRequest(tc.w, 1)
			if err != nil {
				t.Fatalf("%s: %v", tc.description, err)
			}
			got, err := ParseRule(msg[NlMsgHdrSizeCst:])
			if err != nil {
				t.Fatalf("%s: %v", tc.description, err)
			}
			if !ruleInfoEqual(got, tc.expected) {
				t.Fatalf("%s:\n got %+v\nwant %+v", tc.description, got, tc.expected)
			}
		})
	}
}

func ruleInfoEqual(a, b RuleInfo) bool { return reflect.DeepEqual(a, b) }
