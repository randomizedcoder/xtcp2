package xtcpnl

import (
	"bytes"
	"encoding/hex"
	"errors"
	"net/netip"
	"strings"
	"testing"

	"golang.org/x/sys/unix"
)

// This file tests the link write encoders (link-write-path.md Phase 5): the
// request layout iplink_modify imposes, and the bond, bond_slave and veth
// data nests. The argv side is internal/goip's link_bond_args_test.go.
//
// There is no captured write request in the tree yet; the write rows of the
// capture corpus are Phase 6. So the two worked examples in the design are
// pinned here as hex written out by hand from the layout rules, not produced
// by any encoder, and every other row is checked against the rtattr test
// helper, which is a second, independent encoder.

// hexBytes decodes a hex literal, ignoring spaces and newlines.
func hexBytes(t *testing.T, s string) []byte {
	t.Helper()
	b, err := hex.DecodeString(strings.Join(strings.Fields(s), ""))
	if err != nil {
		t.Fatal(err)
	}
	return b
}

// TestBuildLinkWriteRequestWorkedExamples is the design's two worked
// examples, byte for byte.
//
// go test ./pkg/xtcpnl/ -run TestBuildLinkWriteRequestWorkedExamples
func TestBuildLinkWriteRequestWorkedExamples(t *testing.T) {
	bond := BondOptionSpecByID
	data, err := EncodeBondInfoData([]BondSetting{
		{Spec: bond(unix.IFLA_BOND_MODE), Value: 4},
		{Spec: bond(unix.IFLA_BOND_AD_LACP_RATE), Value: 1},
		{Spec: bond(unix.IFLA_BOND_MIIMON), Value: 100},
		{Spec: bond(unix.IFLA_BOND_XMIT_HASH_POLICY), Value: 1},
	})
	if err != nil {
		t.Fatal(err)
	}

	tests := []struct {
		description string
		write       LinkWrite
		expected    string
	}{
		{
			description: "positive: example 1, `link add bond0 type bond mode 802.3ad lacp_rate fast miimon 100 xmit_hash_policy layer3+4`, 96 bytes",
			write: LinkWrite{
				MsgType: uint16(unix.RTM_NEWLINK),
				Flags:   uint16(unix.NLM_F_CREATE | unix.NLM_F_EXCL | unix.NLM_F_ACK),
				Attrs:   []LinkAttr{LinkAttrString(uint16(unix.IFLA_IFNAME), "bond0")},
				Kind:    "bond",
				Data:    data,
				HasData: true,
			},
			expected: `
				60000000 1000 0506 2a000000 00000000
				00000000 00000000 00000000 00000000
				0a000300 626f6e64 30000000
				34001200
				  09000100 626f6e64 00000000
				  24000200
				    05000100 04000000
				    05001500 01000000
				    08000300 64000000
				    05000e00 01000000`,
		},
		{
			description: "positive: example 2, `link set eth1 down master bond0`, 40 bytes, by index",
			write: LinkWrite{
				MsgType:  uint16(unix.RTM_NEWLINK),
				Flags:    uint16(unix.NLM_F_ACK),
				Index:    2,
				IfChange: unix.IFF_UP,
				Attrs:    []LinkAttr{LinkAttrU32(uint16(unix.IFLA_MASTER), 4)},
			},
			expected: `
				28000000 1000 0500 2a000000 00000000
				00000000 02000000 00000000 01000000
				08000a00 04000000`,
		},
	}

	for _, tc := range tests {
		t.Run(tc.description, func(t *testing.T) {
			got, err := BuildLinkWriteRequest(tc.write, 42)
			if err != nil {
				t.Fatal(err)
			}
			if want := hexBytes(t, tc.expected); !bytes.Equal(got, want) {
				t.Errorf("request =\n% x\nwant\n% x", got, want)
			}
		})
	}
}

// TestBuildLinkWriteRequest is the layout table: link-write-path.md table 2
// rows that are about bytes rather than argv, plus the nest rules.
//
// go test ./pkg/xtcpnl/ -run TestBuildLinkWriteRequest$
func TestBuildLinkWriteRequest(t *testing.T) {
	const (
		ifname   = uint16(unix.IFLA_IFNAME)
		linkinfo = uint16(unix.IFLA_LINKINFO)
		kind     = uint16(unix.IFLA_INFO_KIND)
	)
	newlink := uint16(unix.RTM_NEWLINK)
	hdr := func(index int32, flags, change uint32) []byte { return EncodeIfInfomsg(0, index, flags, change) }
	req := func(typ uint16, h []byte, attrs ...[]byte) []byte {
		return nlmsg(typ, uint16(unix.NLM_F_REQUEST), 7, concat(append([][]byte{h}, attrs...)...))
	}
	kindNUL := func(k string) []byte { return rtattr(kind, append([]byte(k), 0)) }

	tests := []struct {
		description string
		write       LinkWrite
		expected    []byte
		wantErr     error
	}{
		{
			description: "positive: add addresses by name, index 0 (row 1)",
			write:       LinkWrite{MsgType: newlink, Attrs: []LinkAttr{LinkAttrString(ifname, "d0")}, Kind: "dummy"},
			expected:    req(newlink, hdr(0, 0, 0), rtattr(ifname, []byte("d0\x00")), rtattr(linkinfo, kindNUL("dummy"))),
		},
		{
			description: "positive: set addresses by index with no IFLA_IFNAME; up is flag and change (rows 2, 3)",
			write:       LinkWrite{MsgType: newlink, Index: 2, IfFlags: unix.IFF_UP, IfChange: unix.IFF_UP},
			expected:    req(newlink, hdr(2, unix.IFF_UP, unix.IFF_UP)),
		},
		{
			description: "positive: down clears the flag and keeps the change bit (row 4)",
			write:       LinkWrite{MsgType: newlink, Index: 2, IfChange: unix.IFF_UP},
			expected:    req(newlink, hdr(2, 0, unix.IFF_UP)),
		},
		{
			description: "positive: nomaster is IFLA_MASTER 0, not an absent attribute (row 6)",
			write:       LinkWrite{MsgType: newlink, Index: 3, Attrs: []LinkAttr{LinkAttrU32(uint16(unix.IFLA_MASTER), 0)}},
			expected:    req(newlink, hdr(3, 0, 0), rtattr(uint16(unix.IFLA_MASTER), le32(0))),
		},
		{
			description: "boundary: attributes go out in the order given, IFLA_LINKINFO last (row 7)",
			write: LinkWrite{MsgType: newlink, Kind: "bond", Attrs: []LinkAttr{
				LinkAttrU32(uint16(unix.IFLA_MTU), 9000), LinkAttrString(ifname, "bond0"),
			}},
			expected: req(newlink, hdr(0, 0, 0), rtattr(uint16(unix.IFLA_MTU), le32(9000)),
				rtattr(ifname, []byte("bond0\x00")), rtattr(linkinfo, kindNUL("bond"))),
		},
		{
			description: "corner: IFLA_INFO_KIND carries a NUL on write, where linkInfoKindAttrs' vrf filter does not (row 10)",
			write:       LinkWrite{MsgType: newlink, Kind: "vrf"},
			expected:    req(newlink, hdr(0, 0, 0), rtattr(linkinfo, rtattr(kind, []byte("vrf\x00")))),
		},
		{
			description: "corner: IFLA_IFALIAS is the one string with no NUL (row 11)",
			write:       LinkWrite{MsgType: newlink, Index: 2, Attrs: []LinkAttr{LinkAttrBytes(uint16(unix.IFLA_IFALIAS), []byte("uplink"))}},
			expected:    req(newlink, hdr(2, 0, 0), rtattr(uint16(unix.IFLA_IFALIAS), []byte("uplink"))),
		},
		{
			description: "corner: a rename is IFLA_IFNAME on a by-index set (row 12)",
			write:       LinkWrite{MsgType: newlink, Index: 2, Attrs: []LinkAttr{LinkAttrString(ifname, "d1")}},
			expected:    req(newlink, hdr(2, 0, 0), rtattr(ifname, []byte("d1\x00"))),
		},
		{
			description: "boundary: a bare kind has no data nest",
			write:       LinkWrite{MsgType: newlink, Kind: "bond"},
			expected:    req(newlink, hdr(0, 0, 0), rtattr(linkinfo, kindNUL("bond"))),
		},
		{
			description: "boundary: HasData with no bytes is an empty nest, which is not the same request",
			write:       LinkWrite{MsgType: newlink, Kind: "bond", HasData: true},
			expected:    req(newlink, hdr(0, 0, 0), rtattr(linkinfo, concat(kindNUL("bond"), rtattr(uint16(unix.IFLA_INFO_DATA), nil)))),
		},
		{
			description: "positive: a _slave kind's data goes in IFLA_INFO_SLAVE_DATA",
			write:       LinkWrite{MsgType: newlink, Index: 3, Kind: "bond_slave", Data: rtattr(5, []byte{1, 0}), HasData: true},
			expected: req(newlink, hdr(3, 0, 0), rtattr(linkinfo, concat(kindNUL("bond_slave"),
				rtattr(uint16(unix.IFLA_INFO_SLAVE_DATA), rtattr(5, []byte{1, 0}))))),
		},
		{
			description: "positive: delete is RTM_DELLINK by index",
			write:       LinkWrite{MsgType: uint16(unix.RTM_DELLINK), Index: 5},
			expected:    req(uint16(unix.RTM_DELLINK), hdr(5, 0, 0)),
		},
		{
			description: "negative: RTM_SETLINK is not a link write iproute2 sends",
			write:       LinkWrite{MsgType: uint16(unix.RTM_SETLINK)},
			wantErr:     ErrLinkWriteType,
		},
		{
			description: "negative: a route type passes the write gate but is not a link write",
			write:       LinkWrite{MsgType: uint16(unix.RTM_NEWROUTE)},
			wantErr:     ErrLinkWriteType,
		},
		{
			description: "negative: kind data without a kind has nowhere to go",
			write:       LinkWrite{MsgType: newlink, HasData: true},
			wantErr:     ErrLinkWriteDataWithoutKind,
		},
		{
			description: "negative: attributes past iproute2's 1024-byte buffer are refused",
			write:       LinkWrite{MsgType: newlink, Attrs: []LinkAttr{LinkAttrBytes(uint16(unix.IFLA_IFALIAS), make([]byte, 1021))}},
			wantErr:     ErrAttrNoSpace,
		},
	}

	for _, tc := range tests {
		t.Run(tc.description, func(t *testing.T) {
			got, err := BuildLinkWriteRequest(tc.write, 7)
			if tc.wantErr != nil {
				if !errors.Is(err, tc.wantErr) || got != nil {
					t.Fatalf("got % x, %v; want nil, %v", got, err, tc.wantErr)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if !bytes.Equal(got, tc.expected) {
				t.Errorf("request =\n% x\nwant\n% x", got, tc.expected)
			}
			// Row 13: no nest anywhere carries NLA_F_NESTED.
			if err := WalkRTAttrs(got[NlMsgHdrSizeCst+IfInfomsgSizeCst:], func(uint16, []byte) {}); err != nil {
				t.Fatal(err)
			}
			if i := bytes.Index(got, []byte{byte(linkinfo), 0x80}); i >= 0 && i%4 == 2 {
				t.Errorf("IFLA_LINKINFO carries NLA_F_NESTED at offset %d", i)
			}
		})
	}
}

// TestLinkKindDataType is iplink_modify's strchr(type, '_') rule.
//
// go test ./pkg/xtcpnl/ -run TestLinkKindDataType
func TestLinkKindDataType(t *testing.T) {
	data, slave := uint16(unix.IFLA_INFO_DATA), uint16(unix.IFLA_INFO_SLAVE_DATA)
	tests := []struct {
		description string
		kind        string
		expected    uint16
	}{
		{description: "positive: bond_slave is slave data", kind: "bond_slave", expected: slave},
		{description: "positive: bond is data", kind: "bond", expected: data},
		{description: "corner: the FIRST underscore decides, so foo_bar_slave is data", kind: "foo_bar_slave", expected: data},
		{description: "boundary: _slave alone is slave data", kind: "_slave", expected: slave},
		{description: "negative: slave without the underscore is data", kind: "slave", expected: data},
		{description: "negative: _slavey is not _slave", kind: "x_slavey", expected: data},
		{description: "boundary: an empty kind is data", kind: "", expected: data},
	}
	for _, tc := range tests {
		t.Run(tc.description, func(t *testing.T) {
			if got := LinkKindDataType(tc.kind); got != tc.expected {
				t.Errorf("LinkKindDataType(%q) = %d, want %d", tc.kind, got, tc.expected)
			}
		})
	}
}

// TestEncodeBondSettings is the encoder half of link-write-path.md table 3:
// one row per wire width and the target nests. The argv rows are in
// internal/goip.
//
// go test ./pkg/xtcpnl/ -run TestEncodeBondSettings
func TestEncodeBondSettings(t *testing.T) {
	bond := BondOptionSpecByID
	slave := func(id uint16) *BondOptionSpec {
		for i := range BondSlaveOptions {
			if BondSlaveOptions[i].ID == id {
				return &BondSlaveOptions[i]
			}
		}
		return nil
	}
	v4 := netip.MustParseAddr
	targets := func(n int) []netip.Addr {
		out := make([]netip.Addr, n)
		for i := range out {
			out[i] = netip.AddrFrom4([4]byte{10, 0, 0, byte(i + 1)})
		}
		return out
	}
	targetNest := func(n int) []byte {
		var b []byte
		for i := 0; i < n; i++ {
			b = append(b, rtattr(uint16(i), []byte{10, 0, 0, byte(i + 1)})...)
		}
		return rtattr(unix.IFLA_BOND_ARP_IP_TARGET, b)
	}

	tests := []struct {
		description string
		settings    []BondSetting
		expected    []byte
		wantErr     error
	}{
		{
			description: "positive: u8 is rta_len 5 padded to 8",
			settings:    []BondSetting{{Spec: bond(unix.IFLA_BOND_MODE), Value: 4}},
			expected:    rtattr(unix.IFLA_BOND_MODE, []byte{4}),
		},
		{
			description: "boundary: arp_validate is u32, not u8",
			settings:    []BondSetting{{Spec: bond(unix.IFLA_BOND_ARP_VALIDATE), Value: 3}},
			expected:    rtattr(unix.IFLA_BOND_ARP_VALIDATE, le32(3)),
		},
		{
			description: "boundary: ad_actor_sys_prio is u16, rta_len 6 padded to 8",
			settings:    []BondSetting{{Spec: bond(unix.IFLA_BOND_AD_ACTOR_SYS_PRIO), Value: 65535}},
			expected:    rtattr(unix.IFLA_BOND_AD_ACTOR_SYS_PRIO, []byte{0xff, 0xff}),
		},
		{
			description: "positive: ad_actor_system is the raw bytes, rta_len 10 padded to 12",
			settings:    []BondSetting{{Spec: bond(unix.IFLA_BOND_AD_ACTOR_SYSTEM), Bytes: []byte{2, 0, 0, 0, 0, 0x0a}}},
			expected:    rtattr(unix.IFLA_BOND_AD_ACTOR_SYSTEM, []byte{2, 0, 0, 0, 0, 0x0a}),
		},
		{
			description: "corner: a short MAC goes out short; the kernel refuses it, not the encoder",
			settings:    []BondSetting{{Spec: bond(unix.IFLA_BOND_AD_ACTOR_SYSTEM), Bytes: []byte{2, 0}}},
			expected:    rtattr(unix.IFLA_BOND_AD_ACTOR_SYSTEM, []byte{2, 0}),
		},
		{
			description: "positive: ARP targets are sub-types 0, 1 in network order",
			settings:    []BondSetting{{Spec: bond(unix.IFLA_BOND_ARP_IP_TARGET), Targets: []netip.Addr{v4("10.0.0.1"), v4("10.0.0.2")}}},
			expected:    targetNest(2),
		},
		{
			description: "boundary: sixteen targets is the maximum",
			settings:    []BondSetting{{Spec: bond(unix.IFLA_BOND_ARP_IP_TARGET), Targets: targets(16)}},
			expected:    targetNest(16),
		},
		{
			description: "negative: a seventeenth target is refused; the parser drops it before here",
			settings:    []BondSetting{{Spec: bond(unix.IFLA_BOND_ARP_IP_TARGET), Targets: targets(17)}},
			wantErr:     ErrAttrTooLong,
		},
		{
			description: "boundary: no targets is an empty nest, rta_len 4, which clears them",
			settings:    []BondSetting{{Spec: bond(unix.IFLA_BOND_ARP_IP_TARGET), Targets: []netip.Addr{}}},
			expected:    rtattr(unix.IFLA_BOND_ARP_IP_TARGET, nil),
		},
		{
			description: "positive: an IPv6 NS target is a 16-byte sub-attribute, rta_len 20",
			settings:    []BondSetting{{Spec: bond(unix.IFLA_BOND_NS_IP6_TARGET), Targets: []netip.Addr{netip.MustParseAddr("2001:db8::1")}}},
			expected:    rtattr(unix.IFLA_BOND_NS_IP6_TARGET, rtattr(0, netip.MustParseAddr("2001:db8::1").AsSlice())),
		},
		{
			description: "corner: settings go out in the order given, lacp_rate before mode",
			settings:    []BondSetting{{Spec: bond(unix.IFLA_BOND_AD_LACP_RATE), Value: 1}, {Spec: bond(unix.IFLA_BOND_MODE), Value: 4}},
			expected:    concat(rtattr(unix.IFLA_BOND_AD_LACP_RATE, []byte{1}), rtattr(unix.IFLA_BOND_MODE, []byte{4})),
		},
		{
			description: "boundary: the port's prio is s32, -1 the all-ones word",
			settings:    []BondSetting{{Spec: slave(unix.IFLA_BOND_SLAVE_PRIO), Value: 0xffffffff}},
			expected:    rtattr(unix.IFLA_BOND_SLAVE_PRIO, le32(0xffffffff)),
		},
		{
			description: "positive: the port's queue_id is u16",
			settings:    []BondSetting{{Spec: slave(unix.IFLA_BOND_SLAVE_QUEUE_ID), Value: 1}},
			expected:    rtattr(unix.IFLA_BOND_SLAVE_QUEUE_ID, []byte{1, 0}),
		},
		{
			description: "negative: AD_INFO is read-only and has no encoding",
			settings:    []BondSetting{{Spec: bond(unix.IFLA_BOND_AD_INFO)}},
			wantErr:     ErrBondSettingWire,
		},
		{
			description: "negative: a setting with no row is refused",
			settings:    []BondSetting{{}},
			wantErr:     ErrBondSettingWire,
		},
		{
			description: "boundary: no settings encode to nothing",
			settings:    nil,
			expected:    []byte{},
		},
	}

	for _, tc := range tests {
		t.Run(tc.description, func(t *testing.T) {
			got, err := EncodeBondInfoData(tc.settings)
			if tc.wantErr != nil {
				if !errors.Is(err, tc.wantErr) {
					t.Fatalf("err = %v, want %v", err, tc.wantErr)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if !bytes.Equal(got, tc.expected) {
				t.Errorf("data =\n% x\nwant\n% x", got, tc.expected)
			}
		})
	}
}

// TestEncodeVethPeer is link-write-path.md table 4 rows 4 and 5 at the byte
// level.
//
// go test ./pkg/xtcpnl/ -run TestEncodeVethPeer
func TestEncodeVethPeer(t *testing.T) {
	name := rtattr(uint16(unix.IFLA_IFNAME), []byte("v1\x00"))
	tests := []struct {
		description string
		peer        VethPeer
		expected    []byte
	}{
		{
			description: "positive: the peer payload opens with a zeroed 16-byte ifinfomsg, then its IFLA_IFNAME",
			peer:        VethPeer{Attrs: []LinkAttr{LinkAttrString(uint16(unix.IFLA_IFNAME), "v1")}},
			expected:    rtattr(VethInfoPeer, concat(make([]byte, IfInfomsgSizeCst), name)),
		},
		{
			description: "corner: `peer name v1 up` puts flag and change in the peer header",
			peer:        VethPeer{IfFlags: unix.IFF_UP, IfChange: unix.IFF_UP, Attrs: []LinkAttr{LinkAttrString(uint16(unix.IFLA_IFNAME), "v1")}},
			expected:    rtattr(VethInfoPeer, concat(EncodeIfInfomsg(0, 0, unix.IFF_UP, unix.IFF_UP), name)),
		},
		{
			description: "boundary: a peer with no attributes is the header alone, rta_len 20",
			peer:        VethPeer{},
			expected:    rtattr(VethInfoPeer, make([]byte, IfInfomsgSizeCst)),
		},
	}
	for _, tc := range tests {
		t.Run(tc.description, func(t *testing.T) {
			got, err := EncodeVethPeer(tc.peer)
			if err != nil {
				t.Fatal(err)
			}
			if !bytes.Equal(got, tc.expected) {
				t.Errorf("peer =\n% x\nwant\n% x", got, tc.expected)
			}
		})
	}
}
