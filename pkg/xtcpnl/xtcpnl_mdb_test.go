package xtcpnl

import (
	"encoding/binary"
	"errors"
	"reflect"
	"testing"

	"golang.org/x/sys/unix"
)

// mdbEntryBytes builds struct br_mdb_entry; group is copied into addr.u.
func mdbEntryBytes(ifindex uint32, state, flags uint8, vid, proto uint16, group []byte) []byte {
	b := make([]byte, MdbEntrySizeCst)
	binary.LittleEndian.PutUint32(b[0:4], ifindex)
	b[4], b[5] = state, flags
	binary.LittleEndian.PutUint16(b[6:8], vid)
	copy(b[8:24], group)
	binary.BigEndian.PutUint16(b[24:26], proto)
	return b
}

// brPortMsg builds struct br_port_msg.
func brPortMsg(ifindex uint32) []byte {
	b := make([]byte, BrPortMsgSizeCst)
	b[0] = unix.AF_BRIDGE
	binary.LittleEndian.PutUint32(b[4:8], ifindex)
	return b
}

var (
	mdbG4  = []byte{239, 1, 1, 1}
	mdbG6  = []byte{0xff, 0x0e, 15: 1}
	mdbL2  = []byte{1, 0, 0x5e, 1, 1, 1}
	mdbS4  = []byte{192, 0, 2, 1}
	mdbS4b = []byte{192, 0, 2, 2}
)

func TestParseMdbEntry(t *testing.T) {
	tests := []struct {
		description string
		in          []byte
		expected    MdbEntry
		err         error
	}{
		{
			description: "positive: an IPv4 group, permanent, VLAN 10",
			in:          mdbEntryBytes(5, 1, 0, 10, MdbProtoIP, mdbG4),
			expected:    MdbEntry{Ifindex: 5, State: 1, Vid: 10, Proto: MdbProtoIP, Group: mdbG4},
		},
		{
			description: "positive: an IPv6 group keeps all 16 bytes",
			in:          mdbEntryBytes(5, 0, 0, 0, MdbProtoIPv6, mdbG6),
			expected:    MdbEntry{Ifindex: 5, Proto: MdbProtoIPv6, Group: mdbG6},
		},
		{
			description: "positive: an L2 group (proto 0) is the MAC's 6 bytes",
			in:          mdbEntryBytes(4, 1, 8, 0, 0, mdbL2),
			expected:    MdbEntry{Ifindex: 4, State: 1, Flags: 8, Group: mdbL2},
		},
		{
			description: "negative: 27 bytes",
			in:          mdbEntryBytes(5, 1, 0, 0, MdbProtoIP, mdbG4)[:27],
			err:         ErrMdbEntrySize,
		},
		{
			description: "boundary: 29 bytes are refused too",
			in:          append(mdbEntryBytes(5, 1, 0, 0, MdbProtoIP, mdbG4), 0),
			err:         ErrMdbEntrySize,
		},
		{
			description: "corner: an unknown proto reads as an L2 group",
			in:          mdbEntryBytes(5, 1, 0, 4095, 0x1234, mdbL2),
			expected:    MdbEntry{Ifindex: 5, State: 1, Vid: 4095, Proto: 0x1234, Group: mdbL2},
		},
	}
	for _, tc := range tests {
		t.Run(tc.description, func(t *testing.T) {
			got, err := ParseMdbEntry(tc.in)
			if !errors.Is(err, tc.err) {
				t.Fatalf("err %v, want %v", err, tc.err)
			}
			if !reflect.DeepEqual(got, tc.expected) {
				t.Errorf("got %+v\nwant %+v", got, tc.expected)
			}
		})
	}
}

func TestParseMdbRequest(t *testing.T) {
	entry := mdbEntryBytes(5, 1, 0, 10, MdbProtoIP, mdbG4)
	srcEntry := func(a []byte) []byte {
		return rtattr(MdbSrcListEntry|unix.NLA_F_NESTED, rtattr(MdbSrcAttrAddress, a))
	}
	tests := []struct {
		description string
		body        []byte
		expected    MdbRequest
		err         error
	}{
		{
			description: "positive: a host join with no attributes",
			body:        concat(brPortMsg(4), rtattr(MdbaSetEntry, mdbEntryBytes(4, 0, 0, 0, MdbProtoIP, mdbG4))),
			expected: MdbRequest{Family: unix.AF_BRIDGE, Ifindex: 4, HasEntry: true,
				Entry: MdbEntry{Ifindex: 4, Proto: MdbProtoIP, Group: mdbG4}},
		},
		{
			description: "positive: every attribute bridge mdb sends",
			body: concat(brPortMsg(4), rtattr(MdbaSetEntry, entry), rtattr(MdbaSetEntryAttrs|unix.NLA_F_NESTED, concat(
				rtattr(MdbeAttrSource, mdbS4),
				rtattr(MdbeAttrGroupMode, []byte{1}),
				rtattr(MdbeAttrSrcList|unix.NLA_F_NESTED, concat(srcEntry(mdbS4), srcEntry(mdbS4b))),
				rtattr(MdbeAttrRtProt, []byte{186}),
				rtattr(MdbeAttrDst, mdbS4b),
				rtattr(MdbeAttrDstPort, le16(4790)),
				rtattr(MdbeAttrVni, le32(200)),
				rtattr(MdbeAttrSrcVni, le32(300)),
				rtattr(MdbeAttrIfindex, le32(7)),
			))),
			expected: MdbRequest{Family: unix.AF_BRIDGE, Ifindex: 4, HasEntry: true, HasAttrs: true,
				Entry: MdbEntry{Ifindex: 5, State: 1, Vid: 10, Proto: MdbProtoIP, Group: mdbG4},
				Attrs: MdbAttrs{Source: mdbS4, GroupMode: 1, HasGroupMode: true, SrcList: [][]byte{mdbS4, mdbS4b}, HasSrcList: true,
					RtProt: 186, HasRtProt: true, Dst: mdbS4b, DstPort: 4790, HasDstPort: true, Vni: 200, HasVni: true,
					SrcVni: 300, HasSrcVni: true, Ifindex: 7, HasIfindex: true}},
		},
		{
			description: "negative: a header shorter than br_port_msg",
			body:        brPortMsg(4)[:7],
			err:         ErrBrPortMsgSmall,
		},
		{
			description: "negative: an entry of 24 bytes",
			body:        concat(brPortMsg(4), rtattr(MdbaSetEntry, entry[:24])),
			err:         ErrMdbEntrySize,
		},
		{
			description: "boundary: an empty attribute nest is present and empty",
			body:        concat(brPortMsg(4), rtattr(MdbaSetEntry, entry), rtattr(MdbaSetEntryAttrs, nil)),
			expected: MdbRequest{Family: unix.AF_BRIDGE, Ifindex: 4, HasEntry: true, HasAttrs: true,
				Entry: MdbEntry{Ifindex: 5, State: 1, Vid: 10, Proto: MdbProtoIP, Group: mdbG4}},
		},
		{
			description: "corner: short fixed-width attributes are skipped; the timer is a dump's",
			body: concat(brPortMsg(4), rtattr(MdbaSetEntry, entry), rtattr(MdbaSetEntryAttrs, concat(
				rtattr(MdbeAttrGroupMode, nil), rtattr(MdbeAttrDstPort, []byte{1}), rtattr(MdbeAttrVni, le16(1)),
			))),
			expected: MdbRequest{Family: unix.AF_BRIDGE, Ifindex: 4, HasEntry: true, HasAttrs: true,
				Entry: MdbEntry{Ifindex: 5, State: 1, Vid: 10, Proto: MdbProtoIP, Group: mdbG4}},
		},
		{
			description: "corner: a source list entry that overruns fails the request",
			body: concat(brPortMsg(4), rtattr(MdbaSetEntry, entry), rtattr(MdbaSetEntryAttrs, concat(
				rtattr(MdbeAttrSrcList, rtattr(MdbSrcListEntry, []byte{9, 0, 1, 0})),
			))),
			err: ErrRTAttrSmall,
		},
	}
	for _, tc := range tests {
		t.Run(tc.description, func(t *testing.T) {
			got, err := ParseMdbRequest(tc.body)
			if !errors.Is(err, tc.err) {
				t.Fatalf("err %v, want %v", err, tc.err)
			}
			if !reflect.DeepEqual(got, tc.expected) {
				t.Errorf("got %+v\nwant %+v", got, tc.expected)
			}
		})
	}
}

func TestParseMdbDump(t *testing.T) {
	info := func(e []byte, attrs ...[]byte) []byte {
		return rtattr(MdbaMdbEntryInfo, concat(append([][]byte{e}, attrs...)...))
	}
	e1 := mdbEntryBytes(5, 0, 0, 1, MdbProtoIP, mdbG4)
	e2 := mdbEntryBytes(5, 0, 0, 10, MdbProtoIP, mdbG4)
	tests := []struct {
		description string
		body        []byte
		expected    []MdbDumpEntry
		err         error
	}{
		{
			description: "positive: two entries, one per VLAN, with timer and protocol",
			body: concat(brPortMsg(4), rtattr(MdbaMdb|unix.NLA_F_NESTED, rtattr(MdbaMdbEntry|unix.NLA_F_NESTED, concat(
				info(e1, rtattr(MdbEattrTimer, le32(25000)), rtattr(MdbEattrRtProt, []byte{4})),
				info(e2, rtattr(MdbEattrGroupMode, []byte{0})),
			)))),
			expected: []MdbDumpEntry{
				{Bridge: 4, Entry: MdbEntry{Ifindex: 5, Vid: 1, Proto: MdbProtoIP, Group: mdbG4},
					Attrs: MdbAttrs{Timer: 25000, HasTimer: true, RtProt: 4, HasRtProt: true}},
				{Bridge: 4, Entry: MdbEntry{Ifindex: 5, Vid: 10, Proto: MdbProtoIP, Group: mdbG4},
					Attrs: MdbAttrs{HasGroupMode: true}},
			},
		},
		{
			description: "positive: a VXLAN entry's remote, by the dump's numbering",
			body: concat(brPortMsg(9), rtattr(MdbaMdb, rtattr(MdbaMdbEntry, info(mdbEntryBytes(9, 1, 0, 0, MdbProtoIP, mdbG4),
				rtattr(MdbEattrSource, mdbS4), rtattr(MdbEattrDst, mdbS4b), rtattr(MdbEattrDstPort, le16(4790)),
				rtattr(MdbEattrVni, le32(200)), rtattr(MdbEattrIfindex, le32(3)), rtattr(MdbEattrSrcVni, le32(300)),
				rtattr(MdbEattrSrcList, rtattr(MdbSrcListEntry, concat(rtattr(MdbSrcAttrAddress, mdbS4), rtattr(2, le32(0))))),
			)))),
			expected: []MdbDumpEntry{{Bridge: 9, Entry: MdbEntry{Ifindex: 9, State: 1, Proto: MdbProtoIP, Group: mdbG4},
				Attrs: MdbAttrs{Source: mdbS4, Dst: mdbS4b, DstPort: 4790, HasDstPort: true, Vni: 200, HasVni: true,
					Ifindex: 3, HasIfindex: true, SrcVni: 300, HasSrcVni: true, SrcList: [][]byte{mdbS4}, HasSrcList: true}}},
		},
		{
			description: "negative: an entry info shorter than br_mdb_entry",
			body:        concat(brPortMsg(4), rtattr(MdbaMdb, rtattr(MdbaMdbEntry, rtattr(MdbaMdbEntryInfo, e1[:20])))),
			err:         ErrMdbEntrySize,
		},
		{
			description: "boundary: a reply with only router ports has no entries",
			body:        concat(brPortMsg(4), rtattr(2, rtattr(1, le32(5)))),
		},
		{
			description: "corner: a header shorter than br_port_msg",
			body:        []byte{unix.AF_BRIDGE},
			err:         ErrBrPortMsgSmall,
		},
	}
	for _, tc := range tests {
		t.Run(tc.description, func(t *testing.T) {
			got, err := ParseMdbDump(tc.body)
			if !errors.Is(err, tc.err) {
				t.Fatalf("err %v, want %v", err, tc.err)
			}
			if !reflect.DeepEqual(got, tc.expected) {
				t.Errorf("got %+v\nwant %+v", got, tc.expected)
			}
		})
	}
}
