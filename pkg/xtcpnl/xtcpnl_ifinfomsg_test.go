package xtcpnl

// Tests for the fixed-width scalar IFLA_* group - the nine attributes
// setLinkScalarAttr decodes, and the routing that reaches it.
//
// TestParseNewLink in xtcpnl_rtnetlink_test.go and TestParseNewLinkRealFixture
// in xtcpnl_rtnetlink_realfixtures_test.go both cover whole LinkInfo structs.
// What they do not isolate, and what this file exists for, is the scalar group
// on its own: its presence bits, its length guards, and the fact that moving it
// behind one merged case clause in setLinkAttr did not swallow the default arm
// that feeds setLinkDetailAttr.
//
// Byte builders (ifinfomsgHdr, rtattr, concat, le32) are reused from
// xtcpnl_rtnetlink_test.go - same package, same test binary.

import (
	"reflect"
	"testing"

	"golang.org/x/sys/unix"
)

// linkScalars projects out of LinkInfo exactly the fields setLinkScalarAttr
// writes, plus one field it must NOT write.
//
// A projection rather than a whole-LinkInfo comparison, because the real-reply
// row would otherwise need a literal for all of LinkDetail's twenty members and
// would be asserting ParseNewLink's whole output a third time. Promiscuity is
// carried along deliberately: it belongs to the `ip -d` group reached through
// setLinkAttr's default arm, so including it in every row means every row is
// also a check that the merged scalar clause did not capture the default.
type linkScalars struct {
	OperState      uint8
	HasOperState   bool
	Carrier        uint8
	HasCarrier     bool
	MTU            uint32
	HasMTU         bool
	TxQLen         uint32
	HasTxQLen      bool
	LinkMode       uint8
	HasLinkMode    bool
	Group          uint32
	HasGroup       bool
	Link           int32
	HasLink        bool
	Master         int32
	LinkNetnsID    int32
	HasLinkNetnsID bool

	// Promiscuity is IFLA_PROMISCUITY, which is NOT part of the scalar group.
	Promiscuity U32Attr
}

func scalarsOf(li LinkInfo) linkScalars {
	return linkScalars{
		OperState: li.OperState, HasOperState: li.HasOperState,
		Carrier: li.Carrier, HasCarrier: li.HasCarrier,
		MTU: li.MTU, HasMTU: li.HasMTU,
		TxQLen: li.TxQLen, HasTxQLen: li.HasTxQLen,
		LinkMode: li.LinkMode, HasLinkMode: li.HasLinkMode,
		Group: li.Group, HasGroup: li.HasGroup,
		Link: li.Link, HasLink: li.HasLink,
		Master:      li.Master,
		LinkNetnsID: li.LinkNetnsID, HasLinkNetnsID: li.HasLinkNetnsID,
		Promiscuity: li.Detail.Promiscuity,
	}
}

// nthLinkBody returns the nth RTM_NEWLINK reply body from a committed dump-set
// capture. The counterpart of nthRouteBody in xtcpnl_rtmsg_test.go, and by
// index for the same reason.
func nthLinkBody(t *testing.T, path string, n int) []byte {
	t.Helper()
	bodies, _ := readDumpSetReplies(t, path, uint16(unix.RTM_NEWLINK))
	if n >= len(bodies) {
		t.Fatalf("%s holds %d RTM_NEWLINK replies, want at least %d", path, len(bodies), n+1)
	}
	return bodies[n]
}

// TestSetLinkScalarAttr drives the nine fixed-width IFLA_* scalars through
// ParseNewLink, which is how this package's helpers are normally exercised -
// unlike setRouteAttr, nothing setLinkScalarAttr writes is erased on the way
// out, because it cannot fail and ParseNewLink has nothing to discard.
//
// go test ./pkg/xtcpnl/ -run TestSetLinkScalarAttr
func TestSetLinkScalarAttr(t *testing.T) {
	// ifinfomsgHdr with the AF_UNSPEC/ARPHRD_ETHER/index-2 shape every
	// constructed row below shares, so the rows differ only in attributes.
	hdr := ifinfomsgHdr(unix.AF_UNSPEC, 1, 2, unix.IFF_UP)

	tests := []struct {
		description string
		body        []byte
		want        linkScalars
		wantErr     bool
	}{
		{
			// veth0 in the mesh namespace, which is the one captured link that
			// carries eight of the nine: it is both a veth (so IFLA_LINK names
			// its peer) and a bridge port (so IFLA_MASTER names br0). The
			// ninth, IFLA_LINK_NETNSID, is absent because the kernel emits it
			// only for a link whose peer is in a DIFFERENT netns than the one
			// being dumped, and both veth ends live in the mesh namespace -
			// which is why the LINK_NETNSID rows below are constructed.
			description: "positive: the real mesh veth0 reply sets eight of the nine scalars with their presence bits, and leaves LINK_NETNSID absent",
			body:        nthLinkBody(t, tdDumpMeshGetLink_7_1_4, 4),
			want: linkScalars{
				// IF_OPER_LOWERLAYERDOWN is 3 - the bridge port is up while
				// br0 itself has no carrier.
				OperState: 3, HasOperState: true,
				Carrier: 0, HasCarrier: true,
				MTU: 1500, HasMTU: true,
				TxQLen: 1000, HasTxQLen: true,
				LinkMode: 0, HasLinkMode: true,
				Group: 0, HasGroup: true,
				Link: 4, HasLink: true,
				Master:      3,
				Promiscuity: U32Attr{Value: 1, Present: true},
			},
		},
		{
			// Absent must leave the presence bit false, not merely the value
			// zero. An MTU of 0 is not a thing a link reports, so a parser
			// that set HasMTU unconditionally would be indistinguishable here
			// on the value alone.
			description: "negative: IFLA_MTU absent leaves MTU zero AND HasMTU false",
			body:        concat(hdr, rtattr(unix.IFLA_IFNAME, append([]byte("eth0"), 0))),
			want:        linkScalars{},
		},
		{
			// Suppressed, not reported: the length guard drops the attribute
			// and ParseNewLink still returns the link. Constructed because the
			// kernel always emits four bytes for IFLA_MTU.
			description: "negative: a 3-byte IFLA_MTU is suppressed rather than erroring — HasMTU stays false and the link still decodes (constructed)",
			body:        concat(hdr, rtattr(unix.IFLA_MTU, []byte{0xDC, 0x05, 0x00})),
			want:        linkScalars{},
		},
		{
			// IF_OPER_UP is 6. One byte is what the kernel sends.
			description: "boundary: IFLA_OPERSTATE with exactly 1 byte sets OperState and HasOperState",
			body:        concat(hdr, rtattr(unix.IFLA_OPERSTATE, []byte{6})),
			want:        linkScalars{OperState: 6, HasOperState: true},
		},
		{
			// A zero-length attribute is four bytes of header and no payload,
			// which is legal framing. The `>= 1` guard is what stops it from
			// indexing val[0].
			description: "boundary: a zero-length IFLA_OPERSTATE sets neither the value nor the presence bit (constructed)",
			body:        concat(hdr, rtattr(unix.IFLA_OPERSTATE, nil)),
			want:        linkScalars{},
		},
		{
			description: "boundary: IFLA_LINK with exactly 4 bytes sets Link and HasLink",
			body:        concat(hdr, rtattr(unix.IFLA_LINK, le32(7))),
			want:        linkScalars{Link: 7, HasLink: true},
		},
		{
			description: "corner: IFLA_MASTER absent leaves Master zero",
			body:        concat(hdr, rtattr(unix.IFLA_IFNAME, append([]byte("eth0"), 0))),
			want:        linkScalars{},
		},
		{
			// The asymmetry with IFLA_LINK, which sits immediately beside it
			// in the switch and DOES have a presence bool. Master has none
			// because index 0 is not a valid interface index, so present-and-
			// zero and absent are the same statement: no master. The row
			// exists to pin that as a decision rather than an oversight - see
			// the LinkInfo.Master field comment.
			description: "corner: IFLA_MASTER present and zero is indistinguishable from absent, by design — unlike IFLA_LINK next to it",
			body:        concat(hdr, rtattr(unix.IFLA_MASTER, le32(0))),
			want:        linkScalars{},
		},
		{
			// IFLA_LINK_NETNSID is an __s32 and -1 is a value the kernel
			// really sends, meaning "the peer is in a netns I cannot name" —
			// iproute2 prints `link-netnsid unknown` for it, which is why the
			// field has an explicit presence bool (see the LinkInfo doc
			// comment). Decoding it unsigned would render 4294967295 instead.
			// Constructed because no captured namespace has a cross-netns
			// veth peer.
			description: "corner: IFLA_LINK_NETNSID of 0xFFFFFFFF decodes as int32 -1, which iproute2 renders `link-netnsid unknown`, not 4294967295 (constructed)",
			body:        concat(hdr, rtattr(unix.IFLA_LINK_NETNSID, le32(0xFFFFFFFF))),
			want:        linkScalars{LinkNetnsID: -1, HasLinkNetnsID: true},
		},
		{
			// Mode 0 is IF_LINK_MODE_DEFAULT and every captured link reports
			// it, so the presence bit is the only thing distinguishing "the
			// kernel said default" from "the kernel said nothing".
			description: "corner: IFLA_LINKMODE present and zero sets HasLinkMode TRUE",
			body:        concat(hdr, rtattr(unix.IFLA_LINKMODE, []byte{0})),
			want:        linkScalars{LinkMode: 0, HasLinkMode: true},
		},
		{
			// THE regression guard for the merged scalar clause. IFLA_PROMISCUITY
			// belongs to no arm of setLinkAttr and must fall through its
			// default to setLinkDetailAttr. A merged clause written with too
			// wide a case list, or one that returned early, would leave
			// Promiscuity unset while the scalars beside it still decoded -
			// which is why this row carries both.
			description: "corner: IFLA_PROMISCUITY alongside the scalars still reaches setLinkDetailAttr — the merged clause must not swallow the default arm",
			body: concat(hdr,
				rtattr(unix.IFLA_MTU, le32(9000)),
				rtattr(unix.IFLA_PROMISCUITY, le32(1)),
				rtattr(unix.IFLA_OPERSTATE, []byte{6}),
			),
			want: linkScalars{
				MTU: 9000, HasMTU: true,
				OperState: 6, HasOperState: true,
				Promiscuity: U32Attr{Value: 1, Present: true},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.description, func(t *testing.T) {
			li, err := ParseNewLink(tt.body)
			if tt.wantErr {
				if err == nil {
					t.Fatalf("expected error, got %+v", li)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if got := scalarsOf(li); !reflect.DeepEqual(got, tt.want) {
				t.Errorf("scalars = %+v, want %+v", got, tt.want)
			}
		})
	}
}
