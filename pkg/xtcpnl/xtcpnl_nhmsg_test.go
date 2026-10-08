package xtcpnl

import (
	"bytes"
	"encoding/hex"
	"errors"
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

// TestParseNewNexthop decodes RTM_NEWNEXTHOP bodies, anchored on the real reply
// netlink_route_getroute_detail recorded for the clean topology's `nexthop add
// id 1 via 192.0.2.10 dev goip0`.
//
// go test ./pkg/xtcpnl/ -run TestParseNewNexthop
func TestParseNewNexthop(t *testing.T) {
	// nhmsg {family AF_INET, scope link(253), proto 0} + NHA_ID 1 + NHA_OIF 3 +
	// NHA_GATEWAY 192.0.2.10. Verified byte-for-byte against the pcap.
	captured := "02fd0000000000000800010001000000080005000300000008000600c000020a"

	tests := []struct {
		description string
		body        []byte
		wantErr     error
		wantFamily  uint8
		wantScope   uint8
		wantProto   uint8
		wantID      uint32
		wantOIF     int32
		wantGateway []byte
		wantBlack   bool
		wantGroup   bool
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
			description: "boundary: a header-only reply decodes the nhmsg and leaves every attribute at its zero value",
			body:        mustHex(t, "02fe000000000000"),
			wantFamily:  unix.AF_INET,
			wantScope:   254, // RT_SCOPE_HOST, to prove the byte is read and not assumed
		},
		{
			description: "negative: a body shorter than the nhmsg header is an error, not a zero nexthop",
			body:        mustHex(t, "02fd00"),
			wantErr:     ErrNhmsgSmall,
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
			description: "corner: NHA_GROUP marks a group so the renderer can decline it",
			// nhmsg {AF_INET} + NHA_ID 8 + NHA_GROUP (one 8-byte group entry)
			body:       mustHex(t, "020000000000000008000100080000000c0002000100000001000000"),
			wantFamily: unix.AF_INET,
			wantID:     8,
			wantGroup:  true,
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
