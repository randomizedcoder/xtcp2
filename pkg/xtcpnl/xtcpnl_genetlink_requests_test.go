package xtcpnl

import (
	"bytes"
	"encoding/hex"
	"math"
	"testing"

	"golang.org/x/sys/unix"
)

func TestGenericGetRequestBytes(t *testing.T) {
	family, err := BuildGetFamilyRequest("ethtool", 0x12345678)
	if err != nil {
		t.Fatal(err)
	}
	// nlmsg_len=32, controller=16, REQUEST, sequence, pid=0;
	// CTRL_CMD_GETFAMILY=3/version=2; FAMILY_NAME attr length=12.
	if got := hex.EncodeToString(family); got != "20000000100001007856341200000000030200000c000200657468746f6f6c00" {
		t.Fatalf("family request: %s", got)
	}
	rings, err := BuildGetEthtoolRequest(42, EthtoolRingsKind, 7, 0x12345678)
	if err != nil {
		t.Fatal(err)
	}
	// ETHTOOL_MSG_RINGS_GET=15, HEADER nested length20; index7, compact1.
	if got := hex.EncodeToString(rings); got != "280000002a00010078563412000000000f0100001400018008000100070000000800030001000000" {
		t.Fatalf("rings request: %s", got)
	}
}

func TestGenericGetRequestBoundaries(t *testing.T) {
	for _, row := range []struct {
		name, description string
		wantErr           bool
	}{
		{"", "empty family rejected", true},
		{"a\x00b", "embedded NUL rejected", true},
		{"123456789012345", "15-byte maximum accepted", false},
		{"1234567890123456", "16-byte overflow rejected", true},
		{"a", "short name has zero alignment padding", false},
	} {
		t.Run(row.description, func(t *testing.T) {
			data, err := BuildGetFamilyRequest(row.name, math.MaxUint32)
			if (err != nil) != row.wantErr {
				t.Fatalf("error=%v, want error=%v", err, row.wantErr)
			}
			if err == nil {
				g, parseErr := ParseGenericNetlink(data[16:])
				if parseErr != nil || len(g.Attributes) != 1 || string(g.Attributes[0].Data) != row.name+"\x00" || len(data)%4 != 0 {
					t.Fatalf("invalid request: %+v, %v", g, parseErr)
				}
				for _, b := range data[25+len(row.name):] {
					if b != 0 {
						t.Fatal("nonzero padding")
					}
				}
			}
		})
	}
	for _, row := range []struct {
		description string
		family      uint16
		index       uint32
		kind        EthtoolKind
		wantErr     bool
	}{
		{"controller is not ethtool", 16, 1, EthtoolRingsKind, true},
		{"reserved ID rejected", 15, 1, EthtoolRingsKind, true},
		{"zero index rejected", 42, 0, EthtoolRingsKind, true},
		{"signed index overflow rejected", 42, math.MaxInt32 + 1, EthtoolRingsKind, true},
		{"unknown command cannot create SET", 42, 1, "set", true},
		{"maximum family and positive index accepted", math.MaxUint16, math.MaxInt32, EthtoolChannelsKind, false},
	} {
		t.Run(row.description, func(t *testing.T) {
			_, err := BuildGetEthtoolRequest(row.family, row.kind, row.index, 0)
			if (err != nil) != row.wantErr {
				t.Fatalf("error=%v, want error=%v", err, row.wantErr)
			}
		})
	}
}

func TestEthtoolGetCommands(t *testing.T) {
	for _, row := range []struct {
		kind    EthtoolKind
		command uint8
	}{
		{EthtoolLinkInfoKind, 2}, {EthtoolLinkModesKind, 4}, {EthtoolLinkStateKind, 6},
		{EthtoolRingsKind, 15}, {EthtoolChannelsKind, 17}, {EthtoolPauseKind, 21}, {EthtoolFECKind, 29},
	} {
		t.Run(string(row.kind), func(t *testing.T) {
			t.Log("expected: correct USER GET, single device, compact bitsets, no ACK or mutation flags")
			data, err := BuildGetEthtoolRequest(42, row.kind, 123, math.MaxUint32)
			if err != nil {
				t.Fatal(err)
			}
			err = WalkNetlinkEnvelopes(data, func(e NetlinkEnvelope) error {
				if e.Header.Flags != unix.NLM_F_REQUEST || e.Header.Type != 42 || e.Header.Seq != math.MaxUint32 || e.Header.Pid != 0 {
					t.Fatalf("header=%+v", e.Header)
				}
				m, parseErr := ParseEthtool(e.Body, e.Header.Flags)
				if parseErr != nil {
					return parseErr
				}
				if m.Command != row.command || m.Kind != row.kind || !m.Request || m.Notification || m.Header.DeviceIndex == nil || *m.Header.DeviceIndex != 123 || m.Header.Flags == nil || *m.Header.Flags != 1 {
					t.Fatalf("message=%+v", m)
				}
				return nil
			})
			if err != nil {
				t.Fatal(err)
			}
		})
	}
}

// TestBuildGetFamilyRequestVersion is the GETFAMILY builder with the
// genlmsghdr version given: 0 is iproute2's genl_resolve_family.
func TestBuildGetFamilyRequestVersion(t *testing.T) {
	tests := []struct {
		description string
		name        string
		version     uint8
		expected    string
		wantErr     bool
	}{
		{
			// fou_add/ip.pcap's first record, under sequence number 0.
			description: "positive: ip's GETFAMILY for fou, version 0",
			name:        "fou", version: 0,
			expected: "1c0000001000010000000000000000000300000008000200666f7500",
		},
		{
			// strace of `ip tcp_metrics show` on iproute2-7.1.0.
			description: "boundary: tcp_metrics, 12 bytes with its terminator, no padding",
			name:        "tcp_metrics", version: 0,
			expected: "2400000010000100000000000000000003000000100002007463705f6d65747269637300",
		},
		{
			description: "positive: version 2 is BuildGetFamilyRequest's",
			name:        "fou", version: 2,
			expected: "1c0000001000010000000000000000000302000008000200666f7500",
		},
		{
			description: "negative: an empty name is refused",
			name:        "", version: 0, wantErr: true,
		},
		{
			description: "corner: a version past any the controller knows is sent as given",
			name:        "fou", version: 0xff,
			expected: "1c00000010000100000000000000000003ff000008000200666f7500",
		},
	}
	for _, tc := range tests {
		t.Run(tc.description, func(t *testing.T) {
			got, err := BuildGetFamilyRequestVersion(tc.name, tc.version, 0)
			if (err != nil) != tc.wantErr {
				t.Fatalf("err = %v, want error %v", err, tc.wantErr)
			}
			if hex.EncodeToString(got) != tc.expected {
				t.Errorf("request\n  got  %x\n  want %s", got, tc.expected)
			}
			if tc.version == 2 && !tc.wantErr {
				old, err := BuildGetFamilyRequest(tc.name, 0)
				if err != nil || !bytes.Equal(old, got) {
					t.Errorf("BuildGetFamilyRequest = %x, %v; want %x", old, err, got)
				}
			}
		})
	}
}
