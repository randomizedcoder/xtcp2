package xtcpnl

import (
	"errors"
	"reflect"
	"testing"

	"golang.org/x/sys/unix"
)

// brVlanHdr is a br_vlan_msg: family, two reserved fields, ifindex.
func brVlanHdr(family uint8, ifindex uint32) []byte {
	return concat([]byte{family, 0, 0, 0}, le32(ifindex))
}

// brVlanInfo is a struct bridge_vlan_info.
func brVlanInfo(flags, vid uint16) []byte {
	return concat(le16(flags), le16(vid))
}

// brEntry is a BRIDGE_VLANDB_ENTRY nest, flagged NLA_F_NESTED as the kernel
// sends it.
func brEntry(attrs ...[]byte) []byte {
	return rtattr(BridgeVlandbEntry|unix.NLA_F_NESTED, concat(attrs...))
}

// brGlobal is a BRIDGE_VLANDB_GLOBAL_OPTIONS nest.
func brGlobal(attrs ...[]byte) []byte {
	return rtattr(BridgeVlandbGlobalOptions|unix.NLA_F_NESTED, concat(attrs...))
}

func TestParseBrVlan(t *testing.T) {
	const pvidUntagged = BridgeVlanInfoPvid | BridgeVlanInfoUntagged
	tests := []struct {
		description string
		body        []byte
		want        BrVlanInfo
		wantErr     error
	}{
		{
			description: "positive: one port VID with flags and STP state",
			body: concat(brVlanHdr(unix.AF_BRIDGE, 4),
				brEntry(
					rtattr(BridgeVlandbEntryInfo, brVlanInfo(pvidUntagged, 1)),
					rtattr(BridgeVlandbEntryState, []byte{3}),
				)),
			want: BrVlanInfo{Family: unix.AF_BRIDGE, Ifindex: 4, Entries: []BrVlanEntry{
				{Flags: pvidUntagged, Vid: 1, HasInfo: true, State: 3, HasState: true},
			}},
		},
		{
			description: "positive: a range entry carries its last VID",
			body: concat(brVlanHdr(unix.AF_BRIDGE, 4),
				brEntry(
					rtattr(BridgeVlandbEntryInfo, brVlanInfo(0, 10)),
					rtattr(BridgeVlandbEntryRange, le16(20)),
				)),
			want: BrVlanInfo{Family: unix.AF_BRIDGE, Ifindex: 4, Entries: []BrVlanEntry{
				{Vid: 10, HasInfo: true, Range: 20, HasRange: true},
			}},
		},
		{
			description: "positive: the top level repeats, one entry per VID, in order",
			body: concat(brVlanHdr(unix.AF_BRIDGE, 4),
				brEntry(rtattr(BridgeVlandbEntryInfo, brVlanInfo(pvidUntagged, 1))),
				brEntry(rtattr(BridgeVlandbEntryInfo, brVlanInfo(0, 100))),
				brEntry(rtattr(BridgeVlandbEntryInfo, brVlanInfo(0, 200))),
			),
			want: BrVlanInfo{Family: unix.AF_BRIDGE, Ifindex: 4, Entries: []BrVlanEntry{
				{Flags: pvidUntagged, Vid: 1, HasInfo: true},
				{Vid: 100, HasInfo: true},
				{Vid: 200, HasInfo: true},
			}},
		},
		{
			description: "positive: global options with an ID and a range; the multicast options are skipped",
			body: concat(brVlanHdr(unix.AF_BRIDGE, 3),
				brGlobal(
					rtattr(BridgeVlandbGoptsID, le16(100)),
					rtattr(BridgeVlandbGoptsRange, le16(110)),
					rtattr(3, []byte{1}),
				)),
			want: BrVlanInfo{Family: unix.AF_BRIDGE, Ifindex: 3, Globals: []BrVlanGlobal{
				{ID: 100, Range: 110, HasRange: true},
			}},
		},
		{
			description: "boundary: header only is a message with no VLANs",
			body:        brVlanHdr(unix.AF_BRIDGE, 4),
			want:        BrVlanInfo{Family: unix.AF_BRIDGE, Ifindex: 4},
		},
		{
			description: "boundary: VID 4094, the highest usable, and a range ending there",
			body: concat(brVlanHdr(unix.AF_BRIDGE, 4),
				brEntry(
					rtattr(BridgeVlandbEntryInfo, brVlanInfo(0, 4000)),
					rtattr(BridgeVlandbEntryRange, le16(4094)),
				)),
			want: BrVlanInfo{Family: unix.AF_BRIDGE, Ifindex: 4, Entries: []BrVlanEntry{
				{Vid: 4000, HasInfo: true, Range: 4094, HasRange: true},
			}},
		},
		{
			description: "boundary: a short RANGE and an empty STATE leave their presence bits false",
			body: concat(brVlanHdr(unix.AF_BRIDGE, 4),
				brEntry(
					rtattr(BridgeVlandbEntryInfo, brVlanInfo(0, 5)),
					rtattr(BridgeVlandbEntryRange, []byte{9}),
					rtattr(BridgeVlandbEntryState, nil),
				)),
			want: BrVlanInfo{Family: unix.AF_BRIDGE, Ifindex: 4, Entries: []BrVlanEntry{
				{Vid: 5, HasInfo: true},
			}},
		},
		{
			description: "negative: a body one byte short of br_vlan_msg",
			body:        brVlanHdr(unix.AF_BRIDGE, 4)[:BrVlanMsgSizeCst-1],
			wantErr:     ErrBrVlanMsgSmall,
		},
		{
			description: "negative: an empty body",
			body:        nil,
			wantErr:     ErrBrVlanMsgSmall,
		},
		{
			description: "negative: a three-byte ENTRY_INFO has no VID, so the message is refused",
			body: concat(brVlanHdr(unix.AF_BRIDGE, 4),
				brEntry(rtattr(BridgeVlandbEntryInfo, []byte{0, 0, 5}))),
			wantErr: ErrBrVlanInfoSmall,
		},
		{
			description: "negative: a bad entry after a good one still fails the whole message",
			body: concat(brVlanHdr(unix.AF_BRIDGE, 4),
				brEntry(rtattr(BridgeVlandbEntryInfo, brVlanInfo(0, 1))),
				brEntry(rtattr(BridgeVlandbEntryInfo, []byte{0})),
			),
			wantErr: ErrBrVlanInfoSmall,
		},
		{
			description: "negative: a top-level attribute longer than the buffer is a walk error",
			body:        concat(brVlanHdr(unix.AF_BRIDGE, 4), []byte{0xff, 0, BridgeVlandbEntry, 0}),
			wantErr:     ErrRTAttrSmall,
		},
		{
			description: "negative: a nested attribute longer than its nest is a walk error",
			body: concat(brVlanHdr(unix.AF_BRIDGE, 4),
				brEntry([]byte{0x40, 0, BridgeVlandbEntryInfo, 0})),
			wantErr: ErrRTAttrSmall,
		},
		{
			description: "corner: inside an entry the first INFO wins",
			body: concat(brVlanHdr(unix.AF_BRIDGE, 4),
				brEntry(
					rtattr(BridgeVlandbEntryInfo, brVlanInfo(0, 7)),
					rtattr(BridgeVlandbEntryInfo, brVlanInfo(pvidUntagged, 8)),
				)),
			want: BrVlanInfo{Family: unix.AF_BRIDGE, Ifindex: 4, Entries: []BrVlanEntry{
				{Vid: 7, HasInfo: true},
			}},
		},
		{
			description: "corner: an entry with no INFO is kept with HasInfo false",
			body: concat(brVlanHdr(unix.AF_BRIDGE, 4),
				brEntry(rtattr(BridgeVlandbEntryState, []byte{4}))),
			want: BrVlanInfo{Family: unix.AF_BRIDGE, Ifindex: 4, Entries: []BrVlanEntry{
				{State: 4, HasState: true},
			}},
		},
		{
			description: "corner: entries and globals in one message both decode, and unknown top-level types are skipped",
			body: concat(brVlanHdr(unix.AF_BRIDGE, 3),
				brEntry(rtattr(BridgeVlandbEntryInfo, brVlanInfo(BridgeVlanInfoBrentry, 1))),
				rtattr(9, []byte{1, 2, 3, 4}),
				brGlobal(rtattr(BridgeVlandbGoptsID, le16(1))),
			),
			want: BrVlanInfo{
				Family: unix.AF_BRIDGE, Ifindex: 3,
				Entries: []BrVlanEntry{{Flags: BridgeVlanInfoBrentry, Vid: 1, HasInfo: true}},
				Globals: []BrVlanGlobal{{ID: 1}},
			},
		},
		{
			description: "corner: the reserved header bytes are ignored",
			body: concat([]byte{unix.AF_BRIDGE, 0xaa, 0xbb, 0xcc}, le32(4),
				brEntry(rtattr(BridgeVlandbEntryInfo, brVlanInfo(0, 2)))),
			want: BrVlanInfo{Family: unix.AF_BRIDGE, Ifindex: 4, Entries: []BrVlanEntry{
				{Vid: 2, HasInfo: true},
			}},
		},
		{
			description: "corner: a short global ID is skipped, leaving ID 0",
			body: concat(brVlanHdr(unix.AF_BRIDGE, 3),
				brGlobal(rtattr(BridgeVlandbGoptsID, []byte{5}))),
			want: BrVlanInfo{Family: unix.AF_BRIDGE, Ifindex: 3, Globals: []BrVlanGlobal{{}}},
		},
	}
	for _, tt := range tests {
		t.Run(tt.description, func(t *testing.T) {
			got, err := ParseBrVlan(tt.body)
			switch {
			case tt.wantErr != nil:
				if !errors.Is(err, tt.wantErr) {
					t.Fatalf("ParseBrVlan err = %v, want %v", err, tt.wantErr)
				}
				return
			case err != nil:
				t.Fatalf("ParseBrVlan err = %v", err)
			}
			if !reflect.DeepEqual(got, tt.want) {
				t.Errorf("ParseBrVlan =\n%+v\nwant\n%+v", got, tt.want)
			}
		})
	}
}
