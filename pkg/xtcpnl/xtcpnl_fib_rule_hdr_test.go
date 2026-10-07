package xtcpnl

// WARNING: this file contains Go reflection (binary.Read / reflect).
//
// The reflection code here is only for performance comparison, and it is
// strongly recommended that it is NOT used in production. It lives in a
// _test.go file so that it never reaches the shipped library: pkg/xtcpnl
// ships zero reflection, and every production Deserialize* reads fields at
// fixed byte offsets instead.
//
// If reflection is ever measured as even close to a manual decoder, that
// indicates a problem rather than a license to use it. See
// xtcpnl_reflection_twins_test.go for the rationale and
// xtcpnl_perf_gate_test.go for the gate that fails on convergence.

import (
	"encoding/binary"
	"errors"
	"reflect"
	"testing"

	"golang.org/x/sys/unix"
)

// ---- byte builders -----------------------------------------------------------
//
// These complement the rtnetlink builders in xtcpnl_rtnetlink_test.go (rtattr,
// concat, le32, v4b, mustV6 are reused from there — same package, same test
// binary).

// fibRuleHdr encodes a 12-byte fib_rule_hdr with both reserved bytes zero,
// which is how the kernel sends it. The two rows that care about res1/res2
// build their bytes inline instead, because a builder that cannot set a field
// cannot test that the field is carried.
func fibRuleHdr(family, dstLen, srcLen, tos, table, action uint8, flags uint32) []byte {
	b := make([]byte, FibRuleHdrSizeCst)
	b[0] = family
	b[1] = dstLen
	b[2] = srcLen
	b[3] = tos
	b[4] = table
	// b[5] res1 and b[6] res2 stay zero.
	b[7] = action
	binary.LittleEndian.PutUint32(b[8:12], flags)
	return b
}

// le16 is the two-byte counterpart of le32.
func le16(v uint16) []byte {
	b := make([]byte, 2)
	binary.LittleEndian.PutUint16(b, v)
	return b
}

// be32 and be64 exist because three attributes in this file are big-endian on
// the wire while every other integer is little-endian — they are exactly the
// three arms of setRuleBigEndianAttr, which is named for that invariant for the
// same reason these builders are. Writing them with an explicitly named
// big-endian builder is what keeps a row from silently agreeing with a decoder
// that swapped the wrong way.
func be32(v uint32) []byte {
	b := make([]byte, 4)
	binary.BigEndian.PutUint32(b, v)
	return b
}

func be64(v uint64) []byte {
	b := make([]byte, 8)
	binary.BigEndian.PutUint64(b, v)
	return b
}

// uidRangeBytes encodes a struct fib_rule_uid_range value.
func uidRangeBytes(start, end uint32) []byte { return concat(le32(start), le32(end)) }

// portRangeBytes encodes a struct fib_rule_port_range value. HOST byte order,
// which is the whole point — see the FibRulePortRange doc.
func portRangeBytes(start, end uint16) []byte { return concat(le16(start), le16(end)) }

// ---- deserializer tests ------------------------------------------------------

// TestDeserializeFibRuleHdr checks the 12-byte fib_rule_hdr decoder against both
// the manual and reflection implementations.
//
// go test ./pkg/xtcpnl/ -run TestDeserializeFibRuleHdr
func TestDeserializeFibRuleHdr(t *testing.T) {
	tests := []struct {
		description string
		data        []byte
		want        FibRuleHdr
		wantErr     error
	}{
		{
			description: "positive: the IPv4 local rule, table 255, action FR_ACT_TO_TBL",
			data:        fibRuleHdr(unix.AF_INET, 0, 0, 0, unix.RT_TABLE_LOCAL, unix.FR_ACT_TO_TBL, 0),
			want: FibRuleHdr{
				Family: unix.AF_INET, Table: unix.RT_TABLE_LOCAL, Action: unix.FR_ACT_TO_TBL,
			},
		},
		{
			description: "positive: an IPv6 rule with a /64 source prefix",
			data:        fibRuleHdr(unix.AF_INET6, 0, 64, 0, unix.RT_TABLE_MAIN, unix.FR_ACT_TO_TBL, 0),
			want: FibRuleHdr{
				Family: unix.AF_INET6, SrcLen: 64, Table: unix.RT_TABLE_MAIN,
				Action: unix.FR_ACT_TO_TBL,
			},
		},
		{
			description: "positive: FIB_RULE_INVERT in flags is a 32-bit field, not a byte",
			data:        fibRuleHdr(unix.AF_INET, 0, 0, 0, 0, unix.FR_ACT_TO_TBL, unix.FIB_RULE_INVERT),
			want: FibRuleHdr{
				Family: unix.AF_INET, Action: unix.FR_ACT_TO_TBL, Flags: unix.FIB_RULE_INVERT,
			},
		},
		{
			description: "boundary: all-zero header decodes to the zero value",
			data:        make([]byte, FibRuleHdrSizeCst),
			want:        FibRuleHdr{},
		},
		{
			description: "boundary: exactly FibRuleHdrSizeCst bytes is sufficient",
			data:        fibRuleHdr(unix.AF_INET, 0, 32, 0, 100, unix.FR_ACT_TO_TBL, 0),
			want: FibRuleHdr{
				Family: unix.AF_INET, SrcLen: 32, Table: 100, Action: unix.FR_ACT_TO_TBL,
			},
		},
		{
			description: "boundary: every byte at 0xff, so a transposed field cannot hide",
			data: []byte{
				0xff, 0xfe, 0xfd, 0xfc,
				0xfb, 0xfa, 0xf9, 0xf8,
				0xf7, 0xf6, 0xf5, 0xf4,
			},
			want: FibRuleHdr{
				Family: 0xff, DstLen: 0xfe, SrcLen: 0xfd, Tos: 0xfc,
				Table: 0xfb, Res1: 0xfa, Res2: 0xf9, Action: 0xf8,
				Flags: 0xf4f5f6f7,
			},
		},
		{
			// The kernel zeroes both, but the decoder carries them anyway — the
			// same choice NdMsg makes with ndm_pad1/ndm_pad2, and for the same
			// reason: a struct that drops bytes cannot prove a round trip.
			description: "corner: the two reserved bytes are carried, not skipped",
			data: []byte{
				unix.AF_INET, 0, 0, 0,
				0, 0xaa, 0xbb, unix.FR_ACT_TO_TBL,
				0, 0, 0, 0,
			},
			want: FibRuleHdr{
				Family: unix.AF_INET, Res1: 0xaa, Res2: 0xbb, Action: unix.FR_ACT_TO_TBL,
			},
		},
		{
			description: "boundary: trailing attribute bytes are ignored by the header decoder",
			data: append(fibRuleHdr(unix.AF_INET, 0, 0, 0, 254, unix.FR_ACT_TO_TBL, 0),
				0xde, 0xad, 0xbe, 0xef),
			want: FibRuleHdr{Family: unix.AF_INET, Table: 254, Action: unix.FR_ACT_TO_TBL},
		},
		{
			description: "corner: one byte short -> ErrFibRuleHdrSmall",
			data:        make([]byte, FibRuleHdrSizeCst-1),
			wantErr:     ErrFibRuleHdrSmall,
		},
		{
			// NdMsg and RtMsg are also 12 bytes, so "short" here cannot mean
			// "shorter than some other family header" — 11 is the only length
			// that distinguishes this check from those.
			description: "corner: empty input -> ErrFibRuleHdrSmall",
			data:        nil,
			wantErr:     ErrFibRuleHdrSmall,
		},
	}

	for _, tc := range tests {
		t.Run(tc.description, func(t *testing.T) {
			var manual, refl FibRuleHdr
			n, errM := DeserializeFibRuleHdr(tc.data, &manual)
			if !errors.Is(errM, tc.wantErr) {
				t.Fatalf("manual err = %v, want %v", errM, tc.wantErr)
			}
			if tc.wantErr != nil {
				if n != 0 {
					t.Errorf("n = %d on error, want 0", n)
				}
				// The reflection decoder reports short input as binary.Read's
				// own io error rather than ErrFibRuleHdrSmall, so only its
				// non-nil-ness is comparable — but it must agree that the input
				// is unusable.
				if _, errR := deserializeFibRuleHdrReflection(tc.data, &refl); errR == nil {
					t.Error("reflection accepted input the manual decoder rejected")
				}
				return
			}
			if n != FibRuleHdrReadCst {
				t.Errorf("n = %d, want %d", n, FibRuleHdrReadCst)
			}
			if !reflect.DeepEqual(manual, tc.want) {
				t.Errorf("DeserializeFibRuleHdr = %+v, want %+v", manual, tc.want)
			}
			if _, errR := deserializeFibRuleHdrReflection(tc.data, &refl); errR != nil {
				t.Fatalf("reflection err = %v, want nil", errR)
			}
			if !reflect.DeepEqual(refl, manual) {
				t.Errorf("reflection = %+v, manual = %+v", refl, manual)
			}
		})
	}
}

// TestFibRuleStructSizes pins the three size constants to the reflected size of
// the structs they describe.
//
// The header is checked the same way TestVerifySizeOfStructs checks the
// inet_diag family. The two small structs are checked here as well because
// neither has a Deserialize* of its own — setRuleRangeAttr reads them against
// FibRuleUidRangeSizeCst and FibRulePortRangeSizeCst, so those two constants are
// the only thing standing between a short attribute and an over-read.
//
// go test ./pkg/xtcpnl/ -run TestFibRuleStructSizes
func TestFibRuleStructSizes(t *testing.T) {
	tests := []struct {
		description string
		got         int
		want        int
	}{
		{
			description: "positive: FibRuleHdr is 12 bytes, like struct fib_rule_hdr",
			got:         binary.Size(FibRuleHdr{}),
			want:        FibRuleHdrSizeCst,
		},
		{
			description: "positive: FibRuleUidRange is 8 bytes, two __u32",
			got:         binary.Size(FibRuleUidRange{}),
			want:        FibRuleUidRangeSizeCst,
		},
		{
			description: "positive: FibRulePortRange is 4 bytes, two __u16",
			got:         binary.Size(FibRulePortRange{}),
			want:        FibRulePortRangeSizeCst,
		},
		{
			description: "boundary: the read constant equals the size constant",
			got:         FibRuleHdrReadCst,
			want:        FibRuleHdrSizeCst,
		},
		{
			// Three 12-byte family headers, which is why FamilyHdrLen switches
			// on the message type and never on a length. Stated here so the
			// coincidence is recorded next to the size rather than only in the
			// encoder's test.
			description: "corner: fib_rule_hdr is the same length as ndmsg",
			got:         FibRuleHdrSizeCst,
			want:        NdMsgSizeCst,
		},
		{
			description: "corner: and the same length as rtmsg",
			got:         FibRuleHdrSizeCst,
			want:        RtMsgSizeCst,
		},
	}

	for _, tc := range tests {
		t.Run(tc.description, func(t *testing.T) {
			if tc.got != tc.want {
				t.Errorf("= %d, want %d", tc.got, tc.want)
			}
		})
	}
}

// TestFraUnexportedValues pins the six FRA_* constants this package declares by
// hand to offsets from FRA_DPORT_RANGE, the last one golang.org/x/sys/unix
// defines.
//
// The failure this guards against is not a compile error — every value in the
// run is a plausible attribute type, so a miscount decodes a NEIGHBOR of the
// intended attribute and produces a well-formed wrong answer. Two pairs make
// that concrete: FRA_SPORT_MASK and FRA_DPORT_MASK are both u16 and sit one
// apart, and FRA_DSCP and FRA_DSCP_MASK are both u8 and sit five apart, so an
// off-by-one inside the run yields a mask attached to the wrong port or a dscp
// value read as its own mask.
//
// go test ./pkg/xtcpnl/ -run TestFraUnexportedValues
func TestFraUnexportedValues(t *testing.T) {
	// The enum tail x/sys does not carry, from
	// include/uapi/linux/fib_rules.h. FRA_DPORT_RANGE is the last one it does.
	const (
		fraDscp          = unix.FRA_DPORT_RANGE + 1 // 25
		fraFlowlabel     = unix.FRA_DPORT_RANGE + 2 // 26
		fraFlowlabelMask = unix.FRA_DPORT_RANGE + 3 // 27
		fraSportMask     = unix.FRA_DPORT_RANGE + 4 // 28
		fraDportMask     = unix.FRA_DPORT_RANGE + 5 // 29
		fraDscpMask      = unix.FRA_DPORT_RANGE + 6 // 30
	)

	tests := []struct {
		description string
		got         uint16
		want        uint16
	}{
		{
			description: "positive: FraDscp is one past FRA_DPORT_RANGE, the last FRA_* x/sys defines",
			got:         FraDscp,
			want:        fraDscp,
		},
		{
			description: "positive: and that derivation is the literal 25",
			got:         FraDscp,
			want:        25,
		},
		{
			description: "positive: FraFlowlabel is 26",
			got:         FraFlowlabel,
			want:        fraFlowlabel,
		},
		{
			description: "positive: FraFlowlabelMask is 27",
			got:         FraFlowlabelMask,
			want:        fraFlowlabelMask,
		},
		{
			description: "positive: FraSportMask is 28",
			got:         FraSportMask,
			want:        fraSportMask,
		},
		{
			description: "positive: FraDportMask is 29",
			got:         FraDportMask,
			want:        fraDportMask,
		},
		{
			description: "positive: FraDscpMask is 30, the end of the run",
			got:         FraDscpMask,
			want:        fraDscpMask,
		},
		{
			description: "boundary: one below FraDscp is FRA_DPORT_RANGE, which x/sys does define",
			got:         FraDscp - 1,
			want:        uint16(unix.FRA_DPORT_RANGE),
		},
		{
			description: "boundary: the two port masks sit one apart, and that order is sport then dport",
			got:         FraDportMask - FraSportMask,
			want:        1,
		},
		{
			description: "corner: FraDscpMask is five past FraDscp, not adjacent to it",
			got:         FraDscpMask - FraDscp,
			want:        5,
		},
		{
			description: "corner: the run is six long, FraDscp through FraDscpMask",
			got:         FraDscpMask - FraDscp + 1,
			want:        6,
		},
	}

	for _, tc := range tests {
		t.Run(tc.description, func(t *testing.T) {
			if tc.got != tc.want {
				t.Errorf("= %d, want %d", tc.got, tc.want)
			}
		})
	}

	// None of the six may collide with an attribute the setRuleAttr cascade
	// already has a case for, at any of its five levels. A switch on
	// non-constant cases is not checked for duplicates by
	// the compiler, so this does — and the collision that WOULD be legal is the
	// interesting one: RTA_GATEWAY (5) shares its number with FRA_UNUSED2 on
	// purpose, so the test enumerates what must NOT collide rather than
	// asserting the whole table is distinct.
	known := []uint16{
		uint16(unix.FRA_PRIORITY), uint16(unix.FRA_SRC), uint16(unix.FRA_DST),
		uint16(unix.FRA_FWMARK), uint16(unix.FRA_FWMASK),
		uint16(unix.FRA_IIFNAME), uint16(unix.FRA_OIFNAME),
		uint16(unix.FRA_L3MDEV), uint16(unix.FRA_UID_RANGE), uint16(unix.FRA_IP_PROTO),
		uint16(unix.FRA_SPORT_RANGE), uint16(unix.FRA_DPORT_RANGE),
		uint16(unix.FRA_TUN_ID), uint16(unix.FRA_TABLE),
		uint16(unix.FRA_SUPPRESS_PREFIXLEN), uint16(unix.FRA_SUPPRESS_IFGROUP),
		uint16(unix.FRA_FLOW), uint16(unix.RTA_GATEWAY), uint16(unix.FRA_GOTO),
		uint16(unix.FRA_PROTOCOL),
	}
	for _, declared := range []uint16{
		FraDscp, FraFlowlabel, FraFlowlabelMask, FraSportMask, FraDportMask, FraDscpMask,
	} {
		for _, k := range known {
			if declared == k {
				t.Errorf("hand-declared FRA constant %d collides with an attribute the setRuleAttr cascade already decodes", declared)
			}
		}
	}
}

// TestFraTableIsRtaTable records that FRA_TABLE and RTA_TABLE are the same
// number, which is what lets iproute2's frh_get_table reach the rule's table
// through `tb[RTA_TABLE]` (ip/iprule.c:90-96) while the kernel emits it as
// FRA_TABLE.
//
// It is one line of production code — a single case in setRuleU32Attr — and it
// would be indistinguishable from a typo without this.
//
// go test ./pkg/xtcpnl/ -run TestFraTableIsRtaTable
func TestFraTableIsRtaTable(t *testing.T) {
	tests := []struct {
		description string
		got         uint16
		want        uint16
	}{
		{
			description: "positive: FRA_TABLE and RTA_TABLE are both 15",
			got:         uint16(unix.FRA_TABLE),
			want:        uint16(unix.RTA_TABLE),
		},
		{
			description: "positive: and that number is 15",
			got:         uint16(unix.FRA_TABLE),
			want:        15,
		},
		{
			description: "corner: RTA_GATEWAY shares its number with FRA_UNUSED2, an unused slot",
			got:         uint16(unix.RTA_GATEWAY),
			want:        5,
		},
	}

	for _, tc := range tests {
		t.Run(tc.description, func(t *testing.T) {
			if tc.got != tc.want {
				t.Errorf("= %d, want %d", tc.got, tc.want)
			}
		})
	}
}

// ---- ParseRule ---------------------------------------------------------------

// TestParseRule covers the attribute walk with synthesized bytes: presence
// versus value, the three big-endian attributes, the 8-bit/32-bit table pair,
// and every short-attribute skip.
//
// The real-kernel counterpart is TestDumpSetRule (xtcpnl_dumpset_test.go),
// which reads the committed netlink_route_getrule and netlink_route_getrule6
// dumps. Constructed bytes are used here because a captured rule cannot express
// a malformed attribute — the kernel does not emit one.
//
// go test ./pkg/xtcpnl/ -run TestParseRule
func TestParseRule(t *testing.T) {
	tests := []struct {
		description string
		body        []byte
		want        RuleInfo
		wantErr     error
	}{
		{
			description: "positive: the IPv4 local rule — pref 0, table 255 in the header, protocol kernel",
			body: concat(
				fibRuleHdr(unix.AF_INET, 0, 0, 0, unix.RT_TABLE_LOCAL, unix.FR_ACT_TO_TBL, 0),
				rtattr(unix.FRA_TABLE, le32(unix.RT_TABLE_LOCAL)),
				rtattr(unix.FRA_SUPPRESS_PREFIXLEN, le32(0xFFFFFFFF)),
				rtattr(unix.FRA_PROTOCOL, []byte{unix.RTPROT_KERNEL}),
			),
			want: RuleInfo{
				Family: unix.AF_INET, Action: unix.FR_ACT_TO_TBL,
				RawTable: unix.RT_TABLE_LOCAL, Table: unix.RT_TABLE_LOCAL,
				HasSuppressPrefixlen: true, SuppressPrefixlen: 0xFFFFFFFF,
				HasProtocol: true, Protocol: unix.RTPROT_KERNEL,
			},
		},
		{
			description: "positive: a from/to rule with an input interface name",
			body: concat(
				fibRuleHdr(unix.AF_INET, 24, 32, 0, 200, unix.FR_ACT_TO_TBL, 0),
				rtattr(unix.FRA_DST, v4b(10, 20, 0, 0)),
				rtattr(unix.FRA_SRC, v4b(192, 168, 1, 1)),
				rtattr(unix.FRA_IIFNAME, []byte("dummy0\x00")),
				rtattr(unix.FRA_PRIORITY, le32(200)),
				rtattr(unix.FRA_TABLE, le32(200)),
			),
			want: RuleInfo{
				Family: unix.AF_INET, DstLen: 24, SrcLen: 32, Action: unix.FR_ACT_TO_TBL,
				RawTable: 200, Table: 200,
				Dst: v4b(10, 20, 0, 0), Src: v4b(192, 168, 1, 1),
				HasIifName: true, IifName: "dummy0",
				HasPriority: true, Priority: 200,
			},
		},
		{
			// The pair the FibRuleHdr doc calls the one trap: the 8-bit field
			// cannot hold 1000, so the kernel sends RT_TABLE_UNSPEC there and
			// the real id in FRA_TABLE. RawTable and Table deliberately
			// disagree.
			description: "positive: a table id above 255 travels in FRA_TABLE with RT_TABLE_UNSPEC in the header",
			body: concat(
				fibRuleHdr(unix.AF_INET, 0, 32, 0, unix.RT_TABLE_UNSPEC, unix.FR_ACT_TO_TBL, 0),
				rtattr(unix.FRA_SRC, v4b(10, 0, 0, 1)),
				rtattr(unix.FRA_PRIORITY, le32(100)),
				rtattr(unix.FRA_TABLE, le32(1000)),
			),
			want: RuleInfo{
				Family: unix.AF_INET, SrcLen: 32, Action: unix.FR_ACT_TO_TBL,
				RawTable: unix.RT_TABLE_UNSPEC, Table: 1000,
				Src:         v4b(10, 0, 0, 1),
				HasPriority: true, Priority: 100,
			},
		},
		{
			description: "positive: an IPv6 rule with a /64 source and a flowlabel pair",
			body: concat(
				fibRuleHdr(unix.AF_INET6, 0, 64, 0, unix.RT_TABLE_MAIN, unix.FR_ACT_TO_TBL, 0),
				rtattr(unix.FRA_SRC, mustV6(t, "2001:db8::")),
				rtattr(unix.FRA_PRIORITY, le32(200)),
				rtattr(unix.FRA_TABLE, le32(unix.RT_TABLE_MAIN)),
				rtattr(FraFlowlabel, be32(0x00012345)),
				rtattr(FraFlowlabelMask, be32(0x000FFFFF)),
			),
			want: RuleInfo{
				Family: unix.AF_INET6, SrcLen: 64, Action: unix.FR_ACT_TO_TBL,
				RawTable: unix.RT_TABLE_MAIN, Table: unix.RT_TABLE_MAIN,
				Src:         mustV6(t, "2001:db8::"),
				HasPriority: true, Priority: 200,
				HasFlowlabel: true, Flowlabel: 0x00012345,
				HasFlowlabelMask: true, FlowlabelMask: 0x000FFFFF,
			},
		},
		{
			description: "positive: every attribute the setRuleAttr cascade has a case for, in one message",
			body: concat(
				fibRuleHdr(unix.AF_INET, 16, 8, 0x10, 77, unix.FR_ACT_TO_TBL, unix.FIB_RULE_INVERT),
				rtattr(unix.FRA_DST, v4b(172, 16, 0, 0)),
				rtattr(unix.FRA_SRC, v4b(10, 0, 0, 0)),
				rtattr(unix.FRA_IIFNAME, []byte("iif0\x00")),
				rtattr(unix.FRA_GOTO, le32(900)),
				rtattr(unix.FRA_PRIORITY, le32(300)),
				rtattr(unix.FRA_FWMARK, le32(0x1234)),
				rtattr(unix.FRA_FLOW, le32(1<<16|2)),
				rtattr(unix.FRA_TUN_ID, be64(42)),
				rtattr(unix.FRA_SUPPRESS_IFGROUP, le32(3)),
				rtattr(unix.FRA_SUPPRESS_PREFIXLEN, le32(0)),
				rtattr(unix.FRA_TABLE, le32(77)),
				rtattr(unix.FRA_FWMASK, le32(0xFFFF)),
				rtattr(unix.FRA_OIFNAME, []byte("oif0\x00")),
				rtattr(unix.FRA_L3MDEV, []byte{1}),
				rtattr(unix.FRA_UID_RANGE, uidRangeBytes(1000, 2000)),
				rtattr(unix.FRA_PROTOCOL, []byte{unix.RTPROT_STATIC}),
				rtattr(unix.FRA_IP_PROTO, []byte{unix.IPPROTO_TCP}),
				rtattr(unix.FRA_SPORT_RANGE, portRangeBytes(1000, 2000)),
				rtattr(unix.FRA_DPORT_RANGE, portRangeBytes(80, 80)),
				rtattr(unix.RTA_GATEWAY, v4b(192, 0, 2, 1)),
				rtattr(FraDscp, []byte{0x2E}),
				rtattr(FraDscpMask, []byte{0x3F}),
				rtattr(FraSportMask, le16(0xFF00)),
				rtattr(FraDportMask, le16(0xFFFF)),
				rtattr(FraFlowlabel, be32(0x000ABCDE)),
				rtattr(FraFlowlabelMask, be32(0x000FFFFF)),
			),
			want: RuleInfo{
				Family: unix.AF_INET, DstLen: 16, SrcLen: 8, Tos: 0x10,
				Action: unix.FR_ACT_TO_TBL, Flags: unix.FIB_RULE_INVERT,
				RawTable: 77, Table: 77,
				Dst: v4b(172, 16, 0, 0), Src: v4b(10, 0, 0, 0),
				HasIifName: true, IifName: "iif0",
				HasOifName: true, OifName: "oif0",
				HasGoto: true, Goto: 900,
				HasPriority: true, Priority: 300,
				HasFwmark: true, Fwmark: 0x1234,
				HasFwmask: true, Fwmask: 0xFFFF,
				HasFlow: true, Flow: 1<<16 | 2,
				HasTunID: true, TunID: 42,
				HasSuppressIfgroup: true, SuppressIfgroup: 3,
				HasSuppressPrefixlen: true, SuppressPrefixlen: 0,
				HasL3mdev: true, L3mdev: 1,
				HasUidRange: true, UidRange: FibRuleUidRange{Start: 1000, End: 2000},
				HasProtocol: true, Protocol: unix.RTPROT_STATIC,
				HasIPProto: true, IPProto: unix.IPPROTO_TCP,
				HasSportRange: true, SportRange: FibRulePortRange{Start: 1000, End: 2000},
				HasDportRange: true, DportRange: FibRulePortRange{Start: 80, End: 80},
				HasGateway: true, Gateway: v4b(192, 0, 2, 1),
				HasDscp: true, Dscp: 0x2E,
				HasDscpMask: true, DscpMask: 0x3F,
				HasSportMask: true, SportMask: 0xFF00,
				HasDportMask: true, DportMask: 0xFFFF,
				HasFlowlabel: true, Flowlabel: 0x000ABCDE,
				HasFlowlabelMask: true, FlowlabelMask: 0x000FFFFF,
			},
		},
		{
			description: "boundary: header only, no attributes — Table falls back to the header's byte",
			body:        fibRuleHdr(unix.AF_INET, 0, 0, 0, unix.RT_TABLE_MAIN, unix.FR_ACT_TO_TBL, 0),
			want: RuleInfo{
				Family: unix.AF_INET, Action: unix.FR_ACT_TO_TBL,
				RawTable: unix.RT_TABLE_MAIN, Table: unix.RT_TABLE_MAIN,
			},
		},
		{
			// frh_get_table returns the attribute unconditionally when it is
			// present, so a zero FRA_TABLE really does override a nonzero
			// header. The kernel never sends this pair, which is precisely why
			// only constructed bytes can reach the branch.
			description: "boundary: FRA_TABLE of zero overrides a nonzero header table",
			body: concat(
				fibRuleHdr(unix.AF_INET, 0, 0, 0, unix.RT_TABLE_MAIN, unix.FR_ACT_BLACKHOLE, 0),
				rtattr(unix.FRA_TABLE, le32(0)),
			),
			want: RuleInfo{
				Family: unix.AF_INET, Action: unix.FR_ACT_BLACKHOLE,
				RawTable: unix.RT_TABLE_MAIN, Table: 0,
			},
		},
		{
			description: "boundary: FRA_TUN_ID is big-endian, so 42 arrives as 42 and not as 1<<43",
			body: concat(
				fibRuleHdr(unix.AF_INET, 0, 0, 0, unix.RT_TABLE_MAIN, unix.FR_ACT_TO_TBL, 0),
				rtattr(unix.FRA_TUN_ID, be64(42)),
			),
			want: RuleInfo{
				Family: unix.AF_INET, Action: unix.FR_ACT_TO_TBL,
				RawTable: unix.RT_TABLE_MAIN, Table: unix.RT_TABLE_MAIN,
				HasTunID: true, TunID: 42,
			},
		},
		{
			// If a byte swap ever appeared here, 1000 would decode as 59395 —
			// the row is chosen so the two readings cannot coincide.
			description: "boundary: fib_rule_port_range is HOST byte order, unlike every other port in netlink",
			body: concat(
				fibRuleHdr(unix.AF_INET, 0, 0, 0, unix.RT_TABLE_MAIN, unix.FR_ACT_TO_TBL, 0),
				rtattr(unix.FRA_SPORT_RANGE, portRangeBytes(1000, 2000)),
			),
			want: RuleInfo{
				Family: unix.AF_INET, Action: unix.FR_ACT_TO_TBL,
				RawTable: unix.RT_TABLE_MAIN, Table: unix.RT_TABLE_MAIN,
				HasSportRange: true, SportRange: FibRulePortRange{Start: 1000, End: 2000},
			},
		},
		{
			// print_rule branches on presence for almost every token, so this
			// is the row that justifies each Has* bit: the attribute is present
			// and carries zero, which a bare uint32 could not distinguish from
			// absence.
			description: "corner: FRA_FWMARK present with value zero sets HasFwmark, unlike its absence",
			body: concat(
				fibRuleHdr(unix.AF_INET, 0, 0, 0, unix.RT_TABLE_MAIN, unix.FR_ACT_TO_TBL, 0),
				rtattr(unix.FRA_FWMARK, le32(0)),
			),
			want: RuleInfo{
				Family: unix.AF_INET, Action: unix.FR_ACT_TO_TBL,
				RawTable: unix.RT_TABLE_MAIN, Table: unix.RT_TABLE_MAIN,
				HasFwmark: true, Fwmark: 0,
			},
		},
		{
			// The cleanest presence-versus-value case in the corpus: every
			// user-added rule carries FRA_PROTOCOL with value 0, and print_rule
			// suppresses it unless -d (ip/iprule.c:551-557). The decoder must
			// keep the distinction the renderer then throws away.
			description: "corner: FRA_PROTOCOL present with RTPROT_UNSPEC sets HasProtocol",
			body: concat(
				fibRuleHdr(unix.AF_INET, 0, 0, 0, unix.RT_TABLE_MAIN, unix.FR_ACT_TO_TBL, 0),
				rtattr(unix.FRA_PROTOCOL, []byte{unix.RTPROT_UNSPEC}),
			),
			want: RuleInfo{
				Family: unix.AF_INET, Action: unix.FR_ACT_TO_TBL,
				RawTable: unix.RT_TABLE_MAIN, Table: unix.RT_TABLE_MAIN,
				HasProtocol: true, Protocol: unix.RTPROT_UNSPEC,
			},
		},
		{
			description: "corner: FRA_GOTO of zero is a real target, not an absent one",
			body: concat(
				fibRuleHdr(unix.AF_INET, 0, 0, 0, 0, unix.FR_ACT_GOTO, 0),
				rtattr(unix.FRA_GOTO, le32(0)),
			),
			want: RuleInfo{
				Family: unix.AF_INET, Action: unix.FR_ACT_GOTO,
				HasGoto: true, Goto: 0,
			},
		},
		{
			// attrSeen.first keeps the first occurrence, which matches the
			// kernel's own nla_parse. Both values are nonzero so the row cannot
			// pass by accident.
			description: "corner: a duplicate FRA_PRIORITY keeps the first value",
			body: concat(
				fibRuleHdr(unix.AF_INET, 0, 0, 0, unix.RT_TABLE_MAIN, unix.FR_ACT_TO_TBL, 0),
				rtattr(unix.FRA_PRIORITY, le32(100)),
				rtattr(unix.FRA_PRIORITY, le32(999)),
			),
			want: RuleInfo{
				Family: unix.AF_INET, Action: unix.FR_ACT_TO_TBL,
				RawTable: unix.RT_TABLE_MAIN, Table: unix.RT_TABLE_MAIN,
				HasPriority: true, Priority: 100,
			},
		},
		{
			description: "corner: a short FRA_FWMARK is skipped and the rest of the rule still decodes",
			body: concat(
				fibRuleHdr(unix.AF_INET, 0, 0, 0, unix.RT_TABLE_MAIN, unix.FR_ACT_TO_TBL, 0),
				rtattr(unix.FRA_FWMARK, []byte{0x01, 0x02}),
				rtattr(unix.FRA_PRIORITY, le32(400)),
			),
			want: RuleInfo{
				Family: unix.AF_INET, Action: unix.FR_ACT_TO_TBL,
				RawTable: unix.RT_TABLE_MAIN, Table: unix.RT_TABLE_MAIN,
				HasPriority: true, Priority: 400,
			},
		},
		{
			description: "corner: a short FRA_UID_RANGE leaves HasUidRange false",
			body: concat(
				fibRuleHdr(unix.AF_INET, 0, 0, 0, unix.RT_TABLE_MAIN, unix.FR_ACT_TO_TBL, 0),
				rtattr(unix.FRA_UID_RANGE, le32(1000)),
			),
			want: RuleInfo{
				Family: unix.AF_INET, Action: unix.FR_ACT_TO_TBL,
				RawTable: unix.RT_TABLE_MAIN, Table: unix.RT_TABLE_MAIN,
			},
		},
		{
			description: "corner: a short FRA_SPORT_RANGE leaves HasSportRange false",
			body: concat(
				fibRuleHdr(unix.AF_INET, 0, 0, 0, unix.RT_TABLE_MAIN, unix.FR_ACT_TO_TBL, 0),
				rtattr(unix.FRA_SPORT_RANGE, le16(1000)),
			),
			want: RuleInfo{
				Family: unix.AF_INET, Action: unix.FR_ACT_TO_TBL,
				RawTable: unix.RT_TABLE_MAIN, Table: unix.RT_TABLE_MAIN,
			},
		},
		{
			description: "corner: a four-byte FRA_TUN_ID is half a __be64 and is skipped",
			body: concat(
				fibRuleHdr(unix.AF_INET, 0, 0, 0, unix.RT_TABLE_MAIN, unix.FR_ACT_TO_TBL, 0),
				rtattr(unix.FRA_TUN_ID, be32(42)),
			),
			want: RuleInfo{
				Family: unix.AF_INET, Action: unix.FR_ACT_TO_TBL,
				RawTable: unix.RT_TABLE_MAIN, Table: unix.RT_TABLE_MAIN,
			},
		},
		{
			description: "corner: a two-byte FRA_FLOWLABEL is skipped rather than zero-extended",
			body: concat(
				fibRuleHdr(unix.AF_INET6, 0, 0, 0, unix.RT_TABLE_MAIN, unix.FR_ACT_TO_TBL, 0),
				rtattr(FraFlowlabel, le16(0x1234)),
			),
			want: RuleInfo{
				Family: unix.AF_INET6, Action: unix.FR_ACT_TO_TBL,
				RawTable: unix.RT_TABLE_MAIN, Table: unix.RT_TABLE_MAIN,
			},
		},
		{
			description: "corner: an empty FRA_L3MDEV is skipped, unlike a present zero",
			body: concat(
				fibRuleHdr(unix.AF_INET, 0, 0, 0, 0, unix.FR_ACT_TO_TBL, 0),
				rtattr(unix.FRA_L3MDEV, nil),
			),
			want: RuleInfo{Family: unix.AF_INET, Action: unix.FR_ACT_TO_TBL},
		},
		{
			// FRA_TABLE is the one attribute whose skip is invisible in a
			// presence bit, because there is none — a short one has to leave
			// the header's value standing.
			description: "corner: a short FRA_TABLE leaves the header's table in place",
			body: concat(
				fibRuleHdr(unix.AF_INET, 0, 0, 0, unix.RT_TABLE_MAIN, unix.FR_ACT_TO_TBL, 0),
				rtattr(unix.FRA_TABLE, le16(1000)),
			),
			want: RuleInfo{
				Family: unix.AF_INET, Action: unix.FR_ACT_TO_TBL,
				RawTable: unix.RT_TABLE_MAIN, Table: unix.RT_TABLE_MAIN,
			},
		},
		{
			// A name the kernel did not NUL-terminate. TrimRight removes
			// nothing and the whole value is the name, which is the behavior
			// that keeps a full IFNAMSIZ name intact.
			description: "corner: an FRA_IIFNAME with no trailing NUL keeps every byte",
			body: concat(
				fibRuleHdr(unix.AF_INET, 0, 0, 0, 0, unix.FR_ACT_TO_TBL, 0),
				rtattr(unix.FRA_IIFNAME, []byte("eth0")),
			),
			want: RuleInfo{
				Family: unix.AF_INET, Action: unix.FR_ACT_TO_TBL,
				HasIifName: true, IifName: "eth0",
			},
		},
		{
			description: "corner: an FRA_OIFNAME of only NULs is present with an empty name",
			body: concat(
				fibRuleHdr(unix.AF_INET, 0, 0, 0, 0, unix.FR_ACT_TO_TBL, 0),
				rtattr(unix.FRA_OIFNAME, []byte("\x00\x00")),
			),
			want: RuleInfo{
				Family: unix.AF_INET, Action: unix.FR_ACT_TO_TBL,
				HasOifName: true, OifName: "",
			},
		},
		{
			// The NAT rule's gateway, read from RTA_GATEWAY rather than from
			// any FRA_* constant. RTN_NAT (10) is outside the FR_ACT_* space,
			// which ends at FR_ACT_PROHIBIT (8) — a modern kernel accepts the
			// action and drops the gateway, so this pair is constructed.
			description: "corner: RTA_GATEWAY is decoded from the FRA_* table for the RTN_NAT action",
			body: concat(
				fibRuleHdr(unix.AF_INET, 0, 0, 0, 0, unix.RTN_NAT, 0),
				rtattr(unix.RTA_GATEWAY, v4b(192, 0, 2, 1)),
			),
			want: RuleInfo{
				Family: unix.AF_INET, Action: unix.RTN_NAT,
				HasGateway: true, Gateway: v4b(192, 0, 2, 1),
			},
		},
		{
			description: "negative: an attribute no level of the setRuleAttr cascade has a case for is ignored",
			body: concat(
				fibRuleHdr(unix.AF_INET, 0, 0, 0, unix.RT_TABLE_MAIN, unix.FR_ACT_TO_TBL, 0),
				rtattr(unix.FRA_PAD, le32(0xDEADBEEF)),
				rtattr(unix.FRA_PRIORITY, le32(500)),
			),
			want: RuleInfo{
				Family: unix.AF_INET, Action: unix.FR_ACT_TO_TBL,
				RawTable: unix.RT_TABLE_MAIN, Table: unix.RT_TABLE_MAIN,
				HasPriority: true, Priority: 500,
			},
		},
		{
			description: "negative: one byte short of the fib_rule_hdr -> ErrFibRuleHdrSmall",
			body:        make([]byte, FibRuleHdrSizeCst-1),
			wantErr:     ErrFibRuleHdrSmall,
		},
		{
			description: "negative: an empty body -> ErrFibRuleHdrSmall",
			body:        nil,
			wantErr:     ErrFibRuleHdrSmall,
		},
		{
			description: "negative: an attribute length below the RTAttr header -> walk error",
			body: concat(
				fibRuleHdr(unix.AF_INET, 0, 0, 0, unix.RT_TABLE_MAIN, unix.FR_ACT_TO_TBL, 0),
				[]byte{0x02, 0x00, byte(unix.FRA_PRIORITY), 0x00},
			),
			wantErr: ErrRTAttrSmall,
		},
		{
			description: "negative: an attribute length beyond the buffer -> walk error",
			body: concat(
				fibRuleHdr(unix.AF_INET, 0, 0, 0, unix.RT_TABLE_MAIN, unix.FR_ACT_TO_TBL, 0),
				[]byte{0xff, 0x00, byte(unix.FRA_PRIORITY), 0x00, 0x00, 0x00, 0x00, 0x00},
			),
			wantErr: ErrRTAttrSmall,
		},
	}

	for _, tc := range tests {
		t.Run(tc.description, func(t *testing.T) {
			got, err := ParseRule(tc.body)
			if tc.wantErr != nil {
				if !errors.Is(err, tc.wantErr) {
					t.Fatalf("err = %v, want %v", err, tc.wantErr)
				}
				if !reflect.DeepEqual(got, RuleInfo{}) {
					t.Errorf("ParseRule returned %+v alongside an error, want the zero value", got)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if !reflect.DeepEqual(got, tc.want) {
				t.Errorf("ParseRule = %+v, want %+v", got, tc.want)
			}
		})
	}
}

// FuzzParseRule checks ParseRule never panics on arbitrary bodies.
//
// go test ./pkg/xtcpnl/ -run FuzzParseRule -fuzz FuzzParseRule
func FuzzParseRule(f *testing.F) {
	f.Add(fibRuleHdr(unix.AF_INET, 0, 0, 0, unix.RT_TABLE_LOCAL, unix.FR_ACT_TO_TBL, 0))
	f.Add(concat(
		fibRuleHdr(unix.AF_INET, 0, 32, 0, unix.RT_TABLE_UNSPEC, unix.FR_ACT_TO_TBL, 0),
		rtattr(unix.FRA_SRC, v4b(10, 0, 0, 1)),
		rtattr(unix.FRA_PRIORITY, le32(100)),
		rtattr(unix.FRA_TABLE, le32(1000)),
		rtattr(unix.FRA_UID_RANGE, uidRangeBytes(1000, 2000)),
		rtattr(unix.FRA_TUN_ID, be64(42)),
	))
	f.Fuzz(func(_ *testing.T, body []byte) {
		_, _ = ParseRule(body)
	})
}
