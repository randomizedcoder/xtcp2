package xtcpnl

import (
	"bytes"
	"encoding/binary"
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
// op-flag, resilient groups with their NHA_RES_GROUP args (buckets, the three
// timers, the 64-bit PAD, an unknown sub-attr, a truncated sub-attr), and a group
// payload that is not a whole number of nexthop_grp entries — none of which the
// bare-show capture exercises. The real id-20 resilient reply is its own positive
// row, verified byte-for-byte against netlink_route_getnexthop_res.pcap.
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
	// The captured resilient group id 20: NHA_GROUP_TYPE resilient(1), members
	// {1,2} equal weight, and NHA_RES_GROUP {buckets 8, idle_timer 12000ct,
	// unbalanced_timer 0, unbalanced_time 0}. Verified byte-for-byte against
	// netlink_route_getnexthop_res.pcap.
	group20res := "000000000000000008000100140000000600030001000000140002000100000000000000020000000000000028000c80060001000800000008000200e02e000008000300000000000c000400000000000000000008000e0000000080"
	// The captured fdb nexthop id 5: nhmsg {AF_INET, scope link} + NHA_ID 5 +
	// NHA_FDB (zero-length flag) + NHA_GATEWAY 192.0.2.20, no NHA_OIF. Verified
	// byte-for-byte against netlink_route_getnexthop_fdb.pcap.
	fdb5 := "02fd000000000000080001000500000004000b0008000600c0000214"

	// Builders for the crafted resilient rows, which are easier to read as
	// structured attrs than as one long hex string.
	nhmsg := []byte{unix.AF_INET, 0, 0, 0, 0, 0, 0, 0}
	cat := func(parts ...[]byte) []byte {
		var b []byte
		for _, p := range parts {
			b = append(b, p...)
		}
		return b
	}
	le64 := func(v uint64) []byte { return append(le32(uint32(v)), le32(uint32(v>>32))...) }
	grpMember := func(id uint32, low, high uint8) []byte {
		return cat(le32(id), []byte{low, high, 0, 0})
	}
	resNested := func(buckets uint16, idle, unbTimer uint32, unbTime uint64) []byte {
		return cat(
			rtattr(NhaResGroupBuckets, le16(buckets)),
			rtattr(NhaResGroupIdleTimer, le32(idle)),
			rtattr(NhaResGroupUnbalancedTimer, le32(unbTimer)),
			rtattr(NhaResGroupUnbalancedTime, le64(unbTime)),
		)
	}

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
		wantFdb      bool
		wantGroup    bool
		wantMembers  []GroupMember
		wantGrpType  uint16
		wantResGroup bool
		wantRes      ResGroup
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
			description:  "positive: captured resilient group id 20 — members {1,2}, type resilient, buckets 8, idle_timer 12000ct",
			body:         mustHex(t, group20res),
			wantID:       20,
			wantGroup:    true,
			wantMembers:  []GroupMember{{ID: 1, Weight: 1}, {ID: 2, Weight: 1}},
			wantGrpType:  NexthopGrpTypeRes,
			wantResGroup: true,
			wantRes:      ResGroup{Buckets: 8, IdleTimer: 12000, UnbalancedTimer: 0, UnbalancedTime: 0},
			wantOpFlags:  NhaOpFlagRespGrpResvd0,
		},
		{
			description: "positive: captured fdb nexthop id 5 — NHA_FDB flag, gateway, link scope, no oif",
			body:        mustHex(t, fdb5),
			wantFamily:  unix.AF_INET,
			wantScope:   unix.RT_SCOPE_LINK,
			wantID:      5,
			wantGateway: []byte{192, 0, 2, 20},
			wantFdb:     true,
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
			description: "boundary: NHA_RES_GROUP with an empty nested payload sets HasResGroup, leaves ResGroup zero",
			// NHA_ID 21 + NHA_GROUP {id 9} + NHA_RES_GROUP (empty nested attr)
			body:         mustHex(t, "02000000000000000800010"+"0150000000c0002000900000000000000"+"04000c00"),
			wantFamily:   unix.AF_INET,
			wantID:       21,
			wantGroup:    true,
			wantMembers:  []GroupMember{{ID: 9, Weight: 1}},
			wantResGroup: true,
			wantRes:      ResGroup{},
		},
		{
			description: "positive: crafted resilient group — weighted members and all four res args decoded",
			body: cat(nhmsg,
				rtattr(NhaID, le32(20)),
				rtattr(NhaGroup, cat(grpMember(1, 1, 0), grpMember(2, 2, 0))),
				rtattr(NhaGroupType, le16(NexthopGrpTypeRes)),
				rtattr(NhaResGroup, resNested(16, 6000, 3000, 12345)),
				rtattr(NhaOpFlags, le32(NhaOpFlagRespGrpResvd0)),
			),
			wantFamily:   unix.AF_INET,
			wantID:       20,
			wantGroup:    true,
			wantMembers:  []GroupMember{{ID: 1, Weight: 2}, {ID: 2, Weight: 3}},
			wantGrpType:  NexthopGrpTypeRes,
			wantResGroup: true,
			wantRes:      ResGroup{Buckets: 16, IdleTimer: 6000, UnbalancedTimer: 3000, UnbalancedTime: 12345},
			wantOpFlags:  NhaOpFlagRespGrpResvd0,
		},
		{
			description: "boundary: NHA_RES_GROUP carrying only BUCKETS leaves the timers zero",
			body: cat(nhmsg,
				rtattr(NhaID, le32(22)),
				rtattr(NhaGroup, grpMember(1, 0, 0)),
				rtattr(NhaGroupType, le16(NexthopGrpTypeRes)),
				rtattr(NhaResGroup, rtattr(NhaResGroupBuckets, le16(8))),
			),
			wantFamily:   unix.AF_INET,
			wantID:       22,
			wantGroup:    true,
			wantMembers:  []GroupMember{{ID: 1, Weight: 1}},
			wantGrpType:  NexthopGrpTypeRes,
			wantResGroup: true,
			wantRes:      ResGroup{Buckets: 8},
		},
		{
			description: "boundary: NHA_RES_GROUP_UNBALANCED_TIME at u64 max decodes without overflow",
			body: cat(nhmsg,
				rtattr(NhaID, le32(23)),
				rtattr(NhaGroup, grpMember(1, 0, 0)),
				rtattr(NhaGroupType, le16(NexthopGrpTypeRes)),
				rtattr(NhaResGroup, rtattr(NhaResGroupUnbalancedTime, le64(^uint64(0)))),
			),
			wantFamily:   unix.AF_INET,
			wantID:       23,
			wantGroup:    true,
			wantMembers:  []GroupMember{{ID: 1, Weight: 1}},
			wantGrpType:  NexthopGrpTypeRes,
			wantResGroup: true,
			wantRes:      ResGroup{UnbalancedTime: ^uint64(0)},
		},
		{
			description: "corner: a type-0 PAD attr before the u64 is skipped, the real sub-attrs still decode",
			body: cat(nhmsg,
				rtattr(NhaID, le32(24)),
				rtattr(NhaGroup, grpMember(1, 0, 0)),
				rtattr(NhaGroupType, le16(NexthopGrpTypeRes)),
				rtattr(NhaResGroup, cat(
					rtattr(NhaResGroupIdleTimer, le32(12000)),
					rtattr(0, nil), // NHA_RES_GROUP_PAD
					rtattr(NhaResGroupUnbalancedTime, le64(0)),
				)),
			),
			wantFamily:   unix.AF_INET,
			wantID:       24,
			wantGroup:    true,
			wantMembers:  []GroupMember{{ID: 1, Weight: 1}},
			wantGrpType:  NexthopGrpTypeRes,
			wantResGroup: true,
			wantRes:      ResGroup{IdleTimer: 12000},
		},
		{
			description: "corner: an unknown NHA_RES_GROUP sub-attr is skipped, known fields still decode",
			body: cat(nhmsg,
				rtattr(NhaID, le32(25)),
				rtattr(NhaGroup, grpMember(1, 0, 0)),
				rtattr(NhaGroupType, le16(NexthopGrpTypeRes)),
				rtattr(NhaResGroup, cat(
					rtattr(NhaResGroupBuckets, le16(4)),
					rtattr(99, le32(0xdeadbeef)),
				)),
			),
			wantFamily:   unix.AF_INET,
			wantID:       25,
			wantGroup:    true,
			wantMembers:  []GroupMember{{ID: 1, Weight: 1}},
			wantGrpType:  NexthopGrpTypeRes,
			wantResGroup: true,
			wantRes:      ResGroup{Buckets: 4},
		},
		{
			description: "corner: NHA_RES_GROUP alongside NHA_GROUP_TYPE mpath is parsed verbatim (parser does not reconcile)",
			body: cat(nhmsg,
				rtattr(NhaID, le32(26)),
				rtattr(NhaGroup, grpMember(1, 0, 0)),
				rtattr(NhaGroupType, le16(NexthopGrpTypeMpath)),
				rtattr(NhaResGroup, rtattr(NhaResGroupBuckets, le16(2))),
			),
			wantFamily:   unix.AF_INET,
			wantID:       26,
			wantGroup:    true,
			wantMembers:  []GroupMember{{ID: 1, Weight: 1}},
			wantGrpType:  NexthopGrpTypeMpath,
			wantResGroup: true,
			wantRes:      ResGroup{Buckets: 2},
		},
		{
			description: "negative: a truncated sub-attr inside NHA_RES_GROUP is a parse error, not a zero ResGroup",
			body: cat(nhmsg,
				rtattr(NhaID, le32(27)),
				rtattr(NhaGroup, grpMember(1, 0, 0)),
				rtattr(NhaGroupType, le16(NexthopGrpTypeRes)),
				// BUCKETS claims 8 bytes but only the 4-byte header is present.
				rtattr(NhaResGroup, rtattrOverrunning(NhaResGroupBuckets, 8)),
			),
			wantErr: ErrRTAttrSmall,
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
			if nh.Fdb != tc.wantFdb {
				t.Errorf("Fdb = %v, want %v", nh.Fdb, tc.wantFdb)
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
			if nh.ResGroup != tc.wantRes {
				t.Errorf("ResGroup = %+v, want %+v", nh.ResGroup, tc.wantRes)
			}
			if nh.RespOpFlags != tc.wantOpFlags {
				t.Errorf("RespOpFlags = 0x%x, want 0x%x", nh.RespOpFlags, tc.wantOpFlags)
			}
		})
	}
}

// TestBuildGetNexthopByIDRequest pins the single-get RTM_GETNEXTHOP byte-for-byte.
// The id-20 row is grounded against the real request netlink_route_getnexthop_res
// recorded (its time-seeded seq passed through, since the capture carries it); the
// id-1 row mirrors what netlink_route_getroute_detail recorded for a route's
// RTA_NH_ID. Every built request is also checked structurally: NLM_F_REQUEST set
// and NLM_F_DUMP clear (a point-GET is not a dump), and its only attributes
// NHA_ID + NHA_OP_FLAGS (no dump-only NHA_OIF/MASTER/GROUPS).
//
// go test ./pkg/xtcpnl/ -run TestBuildGetNexthopByIDRequest
func TestBuildGetNexthopByIDRequest(t *testing.T) {
	tests := []struct {
		description string
		family      uint8
		id          uint32
		seq         uint32
		want        string
	}{
		{
			description: "positive: id 20 matches the request netlink_route_getnexthop_res recorded (capture seq)",
			family:      unix.AF_UNSPEC, id: 20, seq: 1791472176,
			want: "280000006a00010030b2c76a000000000000000000000000080001001400000008000e0000000000",
		},
		{
			description: "positive: id 1 mirrors the single-get netlink_route_getroute_detail recorded",
			family:      unix.AF_UNSPEC, id: 1, seq: 1,
			want: "28000000" + "6a00" + "0100" + "01000000" + "00000000" +
				"0000000000000000" + "0800010001000000" + "08000e0000000000",
		},
		{
			description: "boundary: NHA_ID at u32 max still encodes as one 8-byte NHA_ID attribute",
			family:      unix.AF_UNSPEC, id: ^uint32(0), seq: 1,
			want: "28000000" + "6a00" + "0100" + "01000000" + "00000000" +
				"0000000000000000" + "08000100ffffffff" + "08000e0000000000",
		},
	}

	for _, tc := range tests {
		t.Run(tc.description, func(t *testing.T) {
			want := mustHex(t, tc.want)
			got, err := BuildGetNexthopByIDRequest(tc.family, tc.id, tc.seq)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if !bytes.Equal(got, want) {
				t.Fatalf("request mismatch\ngot  %s\nwant %s", hex.EncodeToString(got), hex.EncodeToString(want))
			}
			// negative: a point-GET must carry NLM_F_REQUEST and NOT NLM_F_DUMP,
			// which is what separates it from the bare-show dump.
			flags := binary.LittleEndian.Uint16(got[4+2 : 4+4])
			if flags&unix.NLM_F_REQUEST == 0 {
				t.Errorf("flags %#x lack NLM_F_REQUEST", flags)
			}
			if flags&unix.NLM_F_DUMP != 0 {
				t.Errorf("flags %#x set NLM_F_DUMP; a point-GET is not a dump", flags)
			}
			// boundary: the attribute set is exactly {NHA_ID, NHA_OP_FLAGS}.
			var types []uint16
			for b := got[NlMsgHdrSizeCst+8:]; len(b) >= 4; {
				al := binary.LittleEndian.Uint16(b[0:2])
				types = append(types, binary.LittleEndian.Uint16(b[2:4])&0x3fff)
				adv := (int(al) + 3) &^ 3
				if al < 4 || adv > len(b) {
					break
				}
				b = b[adv:]
			}
			if len(types) != 2 || types[0] != NhaID || types[1] != NhaOpFlags {
				t.Errorf("attribute types = %v, want [%d %d]", types, NhaID, NhaOpFlags)
			}
		})
	}
}

// TestBuildGetNexthopDumpRequest pins the wire-filtered RTM_GETNEXTHOP dump
// `ip nexthop show SELECTOR` sends, byte-for-byte against the per-selector pcaps.
// The captured filter indices are goip0=3 (NHA_OIF) and goipvrf=4 (NHA_MASTER);
// `master` and `vrf` send the identical NHA_MASTER dump. The flag selectors carry
// a single zero-length NHA_GROUPS or NHA_FDB. protocol is absent on purpose: it is
// a client-side filter and sends the bare dump. Every request is NLM_F_DUMP, which
// the by-id point-GET above is not.
//
// go test ./pkg/xtcpnl/ -run TestBuildGetNexthopDumpRequest
func TestBuildGetNexthopDumpRequest(t *testing.T) {
	tests := []struct {
		description string
		filter      NexthopDumpFilter
		seq         uint32
		want        string
	}{
		{
			description: "positive: `dev goip0` matches netlink_route_getnexthop_dev (NHA_OIF=3)",
			filter:      NexthopDumpFilter{OIF: 3}, seq: 1791497365,
			want: "200000006a0001039514c86a0000000000000000000000000800050003000000",
		},
		{
			description: "positive: `master goipvrf` matches netlink_route_getnexthop_master (NHA_MASTER=4)",
			filter:      NexthopDumpFilter{Master: 4}, seq: 1791497366,
			want: "200000006a0001039614c86a00000000000000000000000008000a0004000000",
		},
		{
			description: "positive: `vrf goipvrf` sends the same NHA_MASTER dump as master",
			filter:      NexthopDumpFilter{Master: 4}, seq: 1791497368,
			want: "200000006a0001039814c86a00000000000000000000000008000a0004000000",
		},
		{
			description: "positive: `groups` matches netlink_route_getnexthop_groups (NHA_GROUPS flag)",
			filter:      NexthopDumpFilter{Groups: true}, seq: 1791497367,
			want: "1c0000006a0001039714c86a00000000000000000000000004000900",
		},
		{
			description: "positive: `fdb` matches netlink_route_getnexthop_fdb (NHA_FDB flag)",
			filter:      NexthopDumpFilter{Fdb: true}, seq: 1791497368,
			want: "1c0000006a0001039814c86a00000000000000000000000004000b00",
		},
		{
			description: "corner: OIF and Master both set serialize in nh_dump_filter order (OIF before MASTER)",
			filter:      NexthopDumpFilter{OIF: 3, Master: 4}, seq: 1,
			want: "280000006a00010301000000000000000000000000000000" +
				"0800050003000000" + "08000a0004000000",
		},
	}

	for _, tc := range tests {
		t.Run(tc.description, func(t *testing.T) {
			want := mustHex(t, tc.want)
			got, err := BuildGetNexthopDumpRequest(unix.AF_UNSPEC, tc.filter, tc.seq)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if !bytes.Equal(got, want) {
				t.Fatalf("request mismatch\ngot  %s\nwant %s", hex.EncodeToString(got), hex.EncodeToString(want))
			}
			// negative: a dump request carries NLM_F_DUMP (it is not a point-GET).
			flags := binary.LittleEndian.Uint16(got[6:8])
			if flags&unix.NLM_F_DUMP == 0 {
				t.Errorf("flags %#x lack NLM_F_DUMP", flags)
			}
		})
	}

	// boundary: an empty filter is byte-identical to the bare dump BuildDumpNexthop
	// sends, so `ip nexthop show` and `ip nexthop show` with every selector off are
	// the same request.
	t.Run("boundary: empty filter equals the bare dump request", func(t *testing.T) {
		got, err := BuildGetNexthopDumpRequest(unix.AF_UNSPEC, NexthopDumpFilter{}, 7)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if bare := BuildDumpNexthopRequest(unix.AF_UNSPEC, 7); !bytes.Equal(got, bare) {
			t.Fatalf("empty filter != bare dump\ngot  %s\nbare %s", hex.EncodeToString(got), hex.EncodeToString(bare))
		}
	})
}
