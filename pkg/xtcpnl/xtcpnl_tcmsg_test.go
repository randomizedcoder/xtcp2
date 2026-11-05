package xtcpnl

import (
	"bytes"
	"encoding/binary"
	"errors"
	"slices"
	"testing"
)

// tcmsgHdr encodes a struct tcmsg. info is written as the kernel lays it
// out for a filter: prio in the high half, the protocol in network order in
// the low half.
func tcmsgHdr(ifindex, handle, parent uint32, prio, proto uint16) []byte {
	b := make([]byte, TcMsgSizeCst)
	b[0] = 0 // AF_UNSPEC
	binary.LittleEndian.PutUint32(b[4:8], ifindex)
	binary.LittleEndian.PutUint32(b[8:12], handle)
	binary.LittleEndian.PutUint32(b[12:16], parent)
	binary.BigEndian.PutUint16(b[16:18], proto)
	binary.LittleEndian.PutUint16(b[18:20], prio)
	return b
}

func TestParseTcMsg(t *testing.T) {
	opts := cat(rtattr(1, u32le(10000)))
	cases := []struct {
		description string
		body        []byte
		expected    TcInfo
		expectedErr error
	}{
		{
			description: "positive: an fq qdisc with options",
			body: cat(tcmsgHdr(3, 0x10000, 0xFFFFFFFF, 0, 0),
				rtattr(TcaKind, []byte("fq\x00")), rtattr(TcaOptions, opts)),
			expected: TcInfo{
				Hdr:  Tcmsg{Ifindex: 3, Handle: 0x10000, Parent: 0xFFFFFFFF},
				Kind: "fq", Options: opts, HasOptions: true,
				Attrs: []uint16{TcaKind, TcaOptions},
			},
		},
		{
			description: "positive: a filter's prio and protocol come apart",
			body:        cat(tcmsgHdr(2, 0, 0xFFFFFFF1, 49152, 0x0800), rtattr(TcaKind, []byte("u32\x00")), rtattr(TcaChain, u32le(7))),
			expected: TcInfo{
				Hdr:        Tcmsg{Ifindex: 2, Parent: 0xFFFFFFF1, Info: 49152<<16 | 0x0008},
				FilterPrio: 49152, FilterProtocol: 0x0800,
				Kind: "u32", Chain: 7, HasChain: true,
				Attrs: []uint16{TcaKind, TcaChain},
			},
		},
		{
			description: "positive: shared blocks, hw offload and an extack warning",
			body: cat(tcmsgHdr(TcmIfindexMagicBlock, 0, 22, 0, 0),
				rtattr(TcaIngressBlock, u32le(22)), rtattr(TcaEgressBlock, u32le(23)),
				rtattr(TcaHwOffload, []byte{1}), rtattr(TcaExtWarnMsg, []byte("warn\x00"))),
			expected: TcInfo{
				Hdr:          Tcmsg{Ifindex: TcmIfindexMagicBlock, Parent: 22},
				IngressBlock: 22, HasIngressBlock: true, EgressBlock: 23, HasEgressBlock: true,
				HwOffload: 1, HasHwOffload: true, ExtWarnMsg: "warn",
				Attrs: []uint16{TcaIngressBlock, TcaEgressBlock, TcaHwOffload, TcaExtWarnMsg},
			},
		},
		{
			description: "boundary: a bare header, no attributes",
			body:        tcmsgHdr(1, 0, 0, 0, 0),
			expected:    TcInfo{Hdr: Tcmsg{Ifindex: 1}},
		},
		{
			description: "corner: a kind without its NUL, and an attribute type past TCA_MAX",
			body:        cat(tcmsgHdr(1, 0, 0, 0, 0), rtattr(TcaKind, []byte("htb")), rtattr(TcaMaxKnown+1, []byte{0, 0, 0, 0})),
			expected:    TcInfo{Hdr: Tcmsg{Ifindex: 1}, Kind: "htb", Attrs: []uint16{TcaKind, TcaMaxKnown + 1}},
		},
		{
			description: "boundary: a short u32 is not taken",
			body:        cat(tcmsgHdr(1, 0, 0, 0, 0), rtattr(TcaChain, []byte{1, 2})),
			expected:    TcInfo{Hdr: Tcmsg{Ifindex: 1}, Attrs: []uint16{TcaChain}},
		},
		{
			description: "negative: one byte short of a tcmsg",
			body:        make([]byte, TcMsgSizeCst-1),
			expectedErr: ErrTcmsgSmall,
		},
		{
			description: "negative: an attribute overrunning the body",
			body:        cat(tcmsgHdr(1, 0, 0, 0, 0), rtattrOverrunning(TcaKind, 64)),
			expectedErr: ErrRTAttrSmall,
		},
	}
	for _, tc := range cases {
		t.Run(tc.description, func(t *testing.T) {
			got, err := ParseTcMsg(tc.body)
			if !errors.Is(err, tc.expectedErr) {
				t.Fatalf("err %v, want %v", err, tc.expectedErr)
			}
			if tc.expectedErr != nil {
				return
			}
			if !tcInfoEqual(got, tc.expected) {
				t.Errorf("got  %+v\nwant %+v", got, tc.expected)
			}
		})
	}
}

func tcInfoEqual(a, b TcInfo) bool {
	return a.Hdr == b.Hdr && a.FilterPrio == b.FilterPrio && a.FilterProtocol == b.FilterProtocol &&
		a.Kind == b.Kind && bytes.Equal(a.Options, b.Options) && a.HasOptions == b.HasOptions &&
		a.Chain == b.Chain && a.HasChain == b.HasChain &&
		a.IngressBlock == b.IngressBlock && a.HasIngressBlock == b.HasIngressBlock &&
		a.EgressBlock == b.EgressBlock && a.HasEgressBlock == b.HasEgressBlock &&
		a.HwOffload == b.HwOffload && a.HasHwOffload == b.HasHwOffload &&
		a.ExtWarnMsg == b.ExtWarnMsg && slices.Equal(a.Attrs, b.Attrs)
}

func TestParseTcAction(t *testing.T) {
	gact := cat(rtattr(TcaActKind, []byte("gact\x00")), rtattr(TcaActOptions, []byte{1, 2, 3, 4}), rtattr(TcaActIndex, u32le(8)))
	mirred := cat(rtattr(TcaActKind, []byte("mirred\x00")), rtattr(TcaActCookie, []byte{0xaa, 0xbb}))
	hdr := []byte{0, 0, 0, 0}
	cases := []struct {
		description string
		body        []byte
		expected    TcActionInfo
		expectedErr error
	}{
		{
			description: "positive: one gact action with its index",
			body:        cat(hdr, rtattr(TcaRootTab, rtattr(1, gact))),
			expected: TcActionInfo{
				Actions: []TcAction{{Order: 1, Kind: "gact", Index: 8, HasIndex: true, Options: []byte{1, 2, 3, 4},
					Attrs: []uint16{TcaActKind, TcaActOptions, TcaActIndex}}},
				Attrs: []uint16{TcaRootTab},
			},
		},
		{
			description: "positive: two actions in order, and a dump count",
			body:        cat(hdr, rtattr(TcaRootCount, u32le(2)), rtattr(TcaRootTab, cat(rtattr(1, gact), rtattr(2, mirred)))),
			expected: TcActionInfo{
				Actions: []TcAction{
					{Order: 1, Kind: "gact", Index: 8, HasIndex: true, Options: []byte{1, 2, 3, 4}, Attrs: []uint16{TcaActKind, TcaActOptions, TcaActIndex}},
					{Order: 2, Kind: "mirred", Cookie: []byte{0xaa, 0xbb}, Attrs: []uint16{TcaActKind, TcaActCookie}},
				},
				Count: 2, HasCount: true,
				Attrs: []uint16{TcaRootCount, TcaRootTab},
			},
		},
		{
			description: "boundary: the highest order, TCA_ACT_MAX_PRIO",
			body:        cat(hdr, rtattr(TcaRootTab, rtattr(32, mirred))),
			expected: TcActionInfo{
				Actions: []TcAction{{Order: 32, Kind: "mirred", Cookie: []byte{0xaa, 0xbb}, Attrs: []uint16{TcaActKind, TcaActCookie}}},
				Attrs:   []uint16{TcaRootTab},
			},
		},
		{
			description: "boundary: a bare tcamsg",
			body:        hdr,
			expected:    TcActionInfo{},
		},
		{
			description: "positive: an extack warning at the root",
			body:        cat(hdr, rtattr(TcaRootExtWarnMsg, []byte("w\x00"))),
			expected:    TcActionInfo{ExtWarnMsg: "w", Attrs: []uint16{TcaRootExtWarnMsg}},
		},
		{
			description: "boundary: order 33, past TCA_ACT_MAX_PRIO, as a large dump numbers it",
			body:        cat(hdr, rtattr(TcaRootTab, rtattr(33, mirred))),
			expected: TcActionInfo{
				Actions: []TcAction{{Order: 33, Kind: "mirred", Cookie: []byte{0xaa, 0xbb}, Attrs: []uint16{TcaActKind, TcaActCookie}}},
				Attrs:   []uint16{TcaRootTab},
			},
		},
		{
			description: "corner: a flush reply's nest 0, its TCA_FCNT reading as TCA_ACT_FLAGS",
			body:        cat(hdr, rtattr(TcaRootTab, rtattr(0, cat(rtattr(TcaActKind, []byte("gact\x00")), rtattr(TcaActFlags, u32le(3)))))),
			expected: TcActionInfo{
				Actions: []TcAction{{Order: 0, Kind: "gact", Attrs: []uint16{TcaActKind, TcaActFlags}}},
				Attrs:   []uint16{TcaRootTab},
			},
		},
		{
			description: "negative: an action nest that overruns itself",
			body:        cat(hdr, rtattr(TcaRootTab, rtattr(1, rtattrOverrunning(TcaActKind, 40)))),
			expectedErr: ErrRTAttrSmall,
		},
		{
			description: "negative: three bytes, short of a tcamsg",
			body:        []byte{0, 0, 0},
			expectedErr: ErrTcamsgSmall,
		},
	}
	for _, tc := range cases {
		t.Run(tc.description, func(t *testing.T) {
			got, err := ParseTcAction(tc.body)
			if !errors.Is(err, tc.expectedErr) {
				t.Fatalf("err %v, want %v", err, tc.expectedErr)
			}
			if tc.expectedErr != nil {
				return
			}
			if !tcActionInfoEqual(got, tc.expected) {
				t.Errorf("got  %+v\nwant %+v", got, tc.expected)
			}
		})
	}
}

func tcActionInfoEqual(a, b TcActionInfo) bool {
	if a.Family != b.Family || a.Count != b.Count || a.HasCount != b.HasCount ||
		a.ExtWarnMsg != b.ExtWarnMsg || !slices.Equal(a.Attrs, b.Attrs) || len(a.Actions) != len(b.Actions) {
		return false
	}
	for i := range a.Actions {
		x, y := a.Actions[i], b.Actions[i]
		if x.Order != y.Order || x.Kind != y.Kind || x.Index != y.Index || x.HasIndex != y.HasIndex ||
			!bytes.Equal(x.Options, y.Options) || !bytes.Equal(x.Cookie, y.Cookie) || !slices.Equal(x.Attrs, y.Attrs) {
			return false
		}
	}
	return true
}
