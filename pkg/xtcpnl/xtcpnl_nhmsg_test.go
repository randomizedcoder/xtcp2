package xtcpnl

import (
	"bytes"
	"encoding/hex"
	"errors"
	"reflect"
	"testing"

	"golang.org/x/sys/unix"
)

// mustHex decodes a hex string used to pin a real captured datagram body.
func mustHex(t *testing.T, s string) []byte {
	t.Helper()
	b, err := hex.DecodeString(s)
	if err != nil {
		t.Fatalf("bad hex %q: %v", s, err)
	}
	return b
}

// TestParseNewNexthop decodes RTM_NEWNEXTHOP bodies. The single and group
// positives are the clean topology's nexthops, verified byte-for-byte against
// netlink_route_getnexthop.pcap (id 1/2 single, id 10 mpath group 1/2, id 11
// weighted mpath group 1,2/2,3). The replies carry NHA_OP_FLAGS 0x80000000, so
// the weight_high gate (nhgrp_weight, ip/ipnexthop.c:237) runs on real bytes.
//
// The boundary, corner and negative rows are hand-crafted wire edges —
// single-member and oversize-weight groups, the weight_high gate toggled by the
// op-flag, a resilient group type, NHA_RES_GROUP, and a group payload that is not
// a whole number of nexthop_grp entries — none of which the one capture exercises.
//
// go test ./pkg/xtcpnl/ -run TestParseNewNexthop
func TestParseNewNexthop(t *testing.T) {
	// nhmsg {family AF_INET, scope link(253), proto 0} + NHA_ID 1 + NHA_OIF 3 +
	// NHA_GATEWAY 192.0.2.10. Verified byte-for-byte against the pcap.
	captured := "02fd0000000000000800010001000000080005000300000008000600c000020a"
	// nhmsg {all zero} + NHA_ID 10 + NHA_GROUP_TYPE mpath + NHA_GROUP {1,2 equal
	// weight} + NHA_OP_FLAGS 0x80000000. The captured mpath group.
	group10 := "0000000000000000080001000a0000000600030000000000140002000100000000000000020000000000000008000e0000000080"
	// Same shape, id 11, NHA_GROUP weights {low 1, low 2} -> rendered 2 and 3.
	group11 := "0000000000000000080001000b0000000600030000000000140002000100000001000000020000000200000008000e0000000080"

	tests := []struct {
		description  string
		body         []byte
		wantErr      error
		wantFamily   uint8
		wantScope    uint8
		wantProto    uint8
		wantID       uint32
		wantOIF      int32
		wantGateway  []byte
		wantBlack    bool
		wantGroup    bool
		wantMembers  []GroupMember
		wantGrpType  uint16
		wantResGroup bool
		wantOpFlags  uint32
	}{
		{
			description: "positive: the captured single nexthop — id, oif and v4 gateway, link scope, unspec proto",
			body:        mustHex(t, captured),
			wantFamily:  unix.AF_INET,
			wantScope:   unix.RT_SCOPE_LINK,
			wantProto:   unix.RTPROT_UNSPEC,
			wantID:      1,
			wantOIF:     3,
			wantGateway: []byte{192, 0, 2, 10},
		},
		{
			description: "positive: captured mpath group id 10 — two equal-weight members, no oif or gateway",
			body:        mustHex(t, group10),
			wantID:      10,
			wantGroup:   true,
			wantMembers: []GroupMember{{ID: 1, Weight: 1}, {ID: 2, Weight: 1}},
			wantGrpType: NexthopGrpTypeMpath,
			wantOpFlags: NhaOpFlagRespGrpResvd0,
		},
		{
			description: "positive: captured weighted mpath group id 11 — members 1 weight 2, 2 weight 3",
			body:        mustHex(t, group11),
			wantID:      11,
			wantGroup:   true,
			wantMembers: []GroupMember{{ID: 1, Weight: 2}, {ID: 2, Weight: 3}},
			wantGrpType: NexthopGrpTypeMpath,
			wantOpFlags: NhaOpFlagRespGrpResvd0,
		},
		{
			description: "boundary: a header-only reply decodes the nhmsg and leaves every attribute at its zero value",
			body:        mustHex(t, "02fe000000000000"),
			wantFamily:  unix.AF_INET,
			wantScope:   254, // RT_SCOPE_HOST, to prove the byte is read and not assumed
		},
		{
			description: "boundary: a single-member group — NHA_GROUP payload exactly one 8-byte entry",
			// nhmsg {AF_INET} + NHA_ID 40 + NHA_GROUP {id 7, weight byte 0}
			body:        mustHex(t, "0200000000000000080001002800000"+"00c0002000700000000000000"),
			wantFamily:  unix.AF_INET,
			wantID:      40,
			wantGroup:   true,
			wantMembers: []GroupMember{{ID: 7, Weight: 1}},
		},
		{
			description: "boundary: weight byte 0 renders 1 and byte 255 renders 256 (1-based, high gated off)",
			// nhmsg {AF_INET} + NHA_ID 41 + NHA_GROUP {id 1 w0, id 2 w255}
			body:        mustHex(t, "02000000000000000800010029000000"+"140002000100000000000000"+"02000000ff000000"),
			wantFamily:  unix.AF_INET,
			wantID:      41,
			wantGroup:   true,
			wantMembers: []GroupMember{{ID: 1, Weight: 1}, {ID: 2, Weight: 256}},
		},
		{
			description: "corner: weight_high honored when NHA_OP_FLAGS carries bit 31",
			// NHA_ID 30 + NHA_GROUP {id 1, low 5, high 1} + NHA_OP_FLAGS 0x80000000
			body:        mustHex(t, "02000000000000000800010"+"01e0000000c0002000100000005010000"+"08000e0000000080"),
			wantFamily:  unix.AF_INET,
			wantID:      30,
			wantGroup:   true,
			wantMembers: []GroupMember{{ID: 1, Weight: 262}}, // ((1<<8)|5)+1
			wantOpFlags: NhaOpFlagRespGrpResvd0,
		},
		{
			description: "corner: the same high byte is ignored when NHA_OP_FLAGS lacks bit 31",
			// identical group, NHA_OP_FLAGS 0 -> high dropped, weight = low+1
			body:        mustHex(t, "02000000000000000800010"+"01e0000000c0002000100000005010000"+"08000e0000000000"),
			wantFamily:  unix.AF_INET,
			wantID:      30,
			wantGroup:   true,
			wantMembers: []GroupMember{{ID: 1, Weight: 6}}, // 5+1, high ignored
		},
		{
			description: "corner: NHA_GROUP_TYPE resilient is parsed (the refusal lives at the renderer, not here)",
			// NHA_ID 20 + NHA_GROUP_TYPE 1 + NHA_GROUP {id 9}
			body:        mustHex(t, "02000000000000000800010"+"0140000000600030001000000"+"0c0002000900000000000000"),
			wantFamily:  unix.AF_INET,
			wantID:      20,
			wantGroup:   true,
			wantMembers: []GroupMember{{ID: 9, Weight: 1}},
			wantGrpType: 1,
		},
		{
			description: "corner: NHA_RES_GROUP present sets HasResGroup (payload not decoded)",
			// NHA_ID 21 + NHA_GROUP {id 9} + NHA_RES_GROUP (empty nested attr)
			body:         mustHex(t, "02000000000000000800010"+"0150000000c0002000900000000000000"+"04000c00"),
			wantFamily:   unix.AF_INET,
			wantID:       21,
			wantGroup:    true,
			wantMembers:  []GroupMember{{ID: 9, Weight: 1}},
			wantResGroup: true,
		},
		{
			description: "negative: a body shorter than the nhmsg header is an error, not a zero nexthop",
			body:        mustHex(t, "02fd00"),
			wantErr:     ErrNhmsgSmall,
		},
		{
			description: "negative: an NHA_GROUP payload that is not a whole number of entries is rejected",
			// NHA_ID 50 + NHA_GROUP with a 4-byte payload (half an entry)
			body:    mustHex(t, "02000000000000000800010032000000"+"0800020001000000"),
			wantErr: ErrNhGroupBad,
		},
		{
			description: "corner: NHA_BLACKHOLE is a flag attribute with no payload",
			// nhmsg {AF_INET} + NHA_ID 7 + NHA_BLACKHOLE (bare 4-byte attr)
			body:       mustHex(t, "0200000000000000080001000700000004000400"),
			wantFamily: unix.AF_INET,
			wantID:     7,
			wantBlack:  true,
		},
		{
			description: "corner: a single-entry NHA_GROUP with weight byte 1 renders weight 2 (high gated off)",
			// nhmsg {AF_INET} + NHA_ID 8 + NHA_GROUP {id 1, low 1, high 0}
			body:        mustHex(t, "020000000000000008000100080000000c0002000100000001000000"),
			wantFamily:  unix.AF_INET,
			wantID:      8,
			wantGroup:   true,
			wantMembers: []GroupMember{{ID: 1, Weight: 2}},
		},
	}

	for _, tc := range tests {
		t.Run(tc.description, func(t *testing.T) {
			nh, err := ParseNewNexthop(tc.body)
			if tc.wantErr != nil {
				if !errors.Is(err, tc.wantErr) {
					t.Fatalf("err = %v, want %v", err, tc.wantErr)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if nh.Family != tc.wantFamily {
				t.Errorf("Family = %d, want %d", nh.Family, tc.wantFamily)
			}
			if nh.Scope != tc.wantScope {
				t.Errorf("Scope = %d, want %d", nh.Scope, tc.wantScope)
			}
			if nh.Protocol != tc.wantProto {
				t.Errorf("Protocol = %d, want %d", nh.Protocol, tc.wantProto)
			}
			if nh.ID != tc.wantID {
				t.Errorf("ID = %d, want %d", nh.ID, tc.wantID)
			}
			if nh.OIF != tc.wantOIF {
				t.Errorf("OIF = %d, want %d", nh.OIF, tc.wantOIF)
			}
			if !bytes.Equal(nh.Gateway, tc.wantGateway) {
				t.Errorf("Gateway = %v, want %v", nh.Gateway, tc.wantGateway)
			}
			if nh.Blackhole != tc.wantBlack {
				t.Errorf("Blackhole = %v, want %v", nh.Blackhole, tc.wantBlack)
			}
			if nh.HasGroup != tc.wantGroup {
				t.Errorf("HasGroup = %v, want %v", nh.HasGroup, tc.wantGroup)
			}
			if !reflect.DeepEqual(nh.Group, tc.wantMembers) {
				t.Errorf("Group = %+v, want %+v", nh.Group, tc.wantMembers)
			}
			if nh.GroupType != tc.wantGrpType {
				t.Errorf("GroupType = %d, want %d", nh.GroupType, tc.wantGrpType)
			}
			if nh.HasResGroup != tc.wantResGroup {
				t.Errorf("HasResGroup = %v, want %v", nh.HasResGroup, tc.wantResGroup)
			}
			if nh.RespOpFlags != tc.wantOpFlags {
				t.Errorf("RespOpFlags = 0x%x, want 0x%x", nh.RespOpFlags, tc.wantOpFlags)
			}
		})
	}
}

// TestBuildGetNexthopByIDRequest pins the single-get `ip -d route show` sends
// for a route with RTA_NH_ID, byte-for-byte against the request
// netlink_route_getroute_detail recorded.
//
// go test ./pkg/xtcpnl/ -run TestBuildGetNexthopByIDRequest
func TestBuildGetNexthopByIDRequest(t *testing.T) {
	// nlmsghdr{len 40, type RTM_GETNEXTHOP, flags REQUEST, seq 1, pid 0} +
	// nhmsg{all zero} + NHA_ID 1 + NHA_OP_FLAGS 0. seq is the one field the
	// comparator zeroes, so this fixes it at 1 and compares the rest.
	want := mustHex(t, "28000000"+"6a00"+"0100"+"01000000"+"00000000"+
		"0000000000000000"+"0800010001000000"+"08000e0000000000")

	got, err := BuildGetNexthopByIDRequest(unix.AF_UNSPEC, 1, 1)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !bytes.Equal(got, want) {
		t.Fatalf("request mismatch\ngot  %s\nwant %s", hex.EncodeToString(got), hex.EncodeToString(want))
	}
}
