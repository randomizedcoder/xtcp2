package xtcpnl

// Tests for the request side of the wire: xtcpnl_rtattr_encode.go.
//
// Two things here are not ordinary encoder tests.
//
// The positive AttrBuilder and BuildRequest rows do not compare against
// hand-written expectations. They compare against the bytes `ip link show`
// actually put on the wire, read out of the committed nlmon capture
// testdata/7_1_8/netlink_route_getlink.pcap. The whole point of the encoder is
// wire parity with iproute2, so its positive expectation has to come from
// iproute2, not from re-reading lib/libnetlink.c and re-deriving the same
// layout twice.
//
// The negative BuildRequest rows — RTM_NEWLINK, RTM_DELROUTE, RTM_SETLINK —
// are the read-only invariant. docs/netlink/coverage-expansion.md states it as
// prose; these rows are the only thing that makes it fail a build.
//
// Constructed bytes appear only in the boundary, negative and corner rows, per
// the standing rule that positive netlink fixtures are real captures. Expected
// attribute bytes are built with rtattr() from xtcpnl_rtnetlink_test.go, which
// is an independently written encoder — comparing reserve() against it is a
// two-implementation check, not a tautology.

import (
	"bytes"
	"encoding/binary"
	"errors"
	"os"
	"testing"

	"golang.org/x/sys/unix"
)

// capturedGetLinkReqLenCst is the measured length of iproute2 7.1.0's
// `ip link show` request: 16-byte nlmsghdr + 16-byte ifinfomsg + one 8-byte
// IFLA_EXT_MASK attribute. rtnl_linkdump_req_filter_fn sends nlmsg_len rather
// than sizeof(req) (lib/libnetlink.c:618), so unlike the addr/route/neigh dumps
// there is no zeroed oversend tail and the datagram is exactly 40 bytes.
const capturedGetLinkReqLenCst = 40

// capturedExtMaskOffCst is where the IFLA_EXT_MASK attribute starts inside that
// request: past the nlmsghdr and the ifinfomsg.
const capturedExtMaskOffCst = NlMsgHdrSizeCst + IfInfomsgSizeCst

// capturedIPLinkShowRequest returns the `ip link show` request datagram from
// the first record of the getlink bulk capture.
//
// It asserts the record really is the request, not a reply: nlmsg_pid == 0 and
// NLM_F_REQUEST set is the only reliable direction signal in an nlmon capture
// (sll_pkttype is PACKET_OUTGOING for both, because AF_PACKET's
// dev_queue_xmit_nit overwrites what __netlink_deliver_tap_skb set). If a
// regenerated fixture ever puts a reply first, this fails loudly instead of
// silently comparing the encoder against kernel output.
func capturedIPLinkShowRequest(t *testing.T) []byte {
	t.Helper()

	bs, err := os.ReadFile(tdRouteBulkGetLink_7_1_8)
	if err != nil {
		t.Fatalf("ReadFile(%s): %v", tdRouteBulkGetLink_7_1_8, err)
	}
	if len(bs) < PcapNetlinkOffsetCst+capturedGetLinkReqLenCst {
		t.Fatalf("%s: too small (%d bytes) to hold a first record", tdRouteBulkGetLink_7_1_8, len(bs))
	}
	msg := bs[PcapNetlinkOffsetCst : PcapNetlinkOffsetCst+capturedGetLinkReqLenCst]

	if got := binary.LittleEndian.Uint32(msg[0:4]); got != capturedGetLinkReqLenCst {
		t.Fatalf("first record nlmsg_len = %d, want %d", got, capturedGetLinkReqLenCst)
	}
	if got := binary.LittleEndian.Uint16(msg[4:6]); got != uint16(unix.RTM_GETLINK) {
		t.Fatalf("first record nlmsg_type = %d, want RTM_GETLINK (%d)", got, unix.RTM_GETLINK)
	}
	if flags := binary.LittleEndian.Uint16(msg[6:8]); flags&uint16(unix.NLM_F_REQUEST) == 0 {
		t.Fatalf("first record nlmsg_flags = %#x, NLM_F_REQUEST not set: this is a reply, not a request", flags)
	}
	if pid := binary.LittleEndian.Uint32(msg[12:16]); pid != 0 {
		t.Fatalf("first record nlmsg_pid = %d, want 0: requests carry pid 0, replies carry the requester's portid", pid)
	}
	return msg
}

// zeroSeqPid returns a copy of a request with nlmsg_seq and nlmsg_pid cleared.
// rtnl_open seeds seq from time(NULL) (lib/libnetlink.c:249), so the captured
// value is a timestamp and cannot be reproduced; every other byte can.
func zeroSeqPid(msg []byte) []byte {
	out := make([]byte, len(msg))
	copy(out, msg)
	for i := 8; i < 16; i++ {
		out[i] = 0
	}
	return out
}

// mustAttrs runs puts against a fresh builder over a buffer of bufLen and
// returns the encoded attribute stream, failing the test on any error.
func mustAttrs(t *testing.T, bufLen int, puts ...func(*AttrBuilder) error) []byte {
	t.Helper()
	ab := NewAttrBuilder(make([]byte, bufLen))
	for i, p := range puts {
		if err := p(&ab); err != nil {
			t.Fatalf("put %d: %v", i, err)
		}
	}
	return ab.Bytes()
}

// ---- AttrBuilder -------------------------------------------------------------

// TestAttrBuilder checks the attribute encoder byte-for-byte, including the
// bounds check, the alignment padding, and the guarantee that a failed Put
// leaves the caller's buffer untouched.
//
// Every buffer is prefilled with 0xAA rather than zeroed. That makes two
// properties testable that a zeroed buffer hides: that reserve() zeroes an
// attribute's payload and padding (a stale byte in the padding is a wire
// divergence no decoder would notice and a byte-for-byte parity comparison
// would), and that an error really does write nothing.
//
// go test ./pkg/xtcpnl/ -run TestAttrBuilder
func TestAttrBuilder(t *testing.T) {
	const prefill = 0xAA

	mac := macb(0x02, 0x00, 0x00, 0x00, 0x00, 0x01)
	nested := concat(rtattr(uint16(unix.IFLA_INFO_KIND), []byte("veth\x00")))
	captured := capturedIPLinkShowRequest(t)

	tests := []struct {
		description string
		bufLen      int // -1 means a nil buffer
		put         func(a *AttrBuilder) error
		want        []byte // expected a.Bytes(); nil when wantErr is set
		wantErr     error
	}{
		{
			// The expectation is the capture, not a literal: 0800 1d00 09000000.
			description: "positive: PutU32(IFLA_EXT_MASK, 0x09) reproduces the captured ip link show attribute",
			bufLen:      8,
			put: func(a *AttrBuilder) error {
				return a.PutU32(uint16(unix.IFLA_EXT_MASK), RTEXT_FILTER_VF|RTEXT_FILTER_SKIP_STATS)
			},
			want: captured[capturedExtMaskOffCst:capturedGetLinkReqLenCst],
		},
		{
			description: "positive: PutString(IFLA_IFNAME, \"lo\") is NUL-terminated inside rta_len, then padded",
			bufLen:      8,
			put:         func(a *AttrBuilder) error { return a.PutString(uint16(unix.IFLA_IFNAME), "lo") },
			want:        rtattr(uint16(unix.IFLA_IFNAME), []byte("lo\x00")),
		},
		{
			description: "positive: PutBytes of a 6-byte MAC gives rta_len 10 and advances 12",
			bufLen:      16,
			put:         func(a *AttrBuilder) error { return a.PutBytes(uint16(unix.IFLA_ADDRESS), mac) },
			want:        rtattr(uint16(unix.IFLA_ADDRESS), mac),
		},
		{
			description: "positive: PutU8 emits a 5-byte attribute padded to 8",
			bufLen:      8,
			put:         func(a *AttrBuilder) error { return a.PutU8(uint16(unix.IFLA_LINKMODE), 1) },
			want:        rtattr(uint16(unix.IFLA_LINKMODE), []byte{1}),
		},
		{
			// The second attribute must start at 8, not at 7: the cursor advances
			// by the ALIGNED length, which is what addattr_l does to nlmsg_len.
			description: "positive: a second attribute starts after the first one's padding",
			bufLen:      16,
			put: func(a *AttrBuilder) error {
				if err := a.PutString(uint16(unix.IFLA_IFNAME), "lo"); err != nil {
					return err
				}
				return a.PutU32(uint16(unix.IFLA_EXT_MASK), 0x09)
			},
			want: concat(
				rtattr(uint16(unix.IFLA_IFNAME), []byte("lo\x00")),
				rtattr(uint16(unix.IFLA_EXT_MASK), le32(0x09)),
			),
		},
		{
			// addattrstrz is addattr_l(..., strlen(s)+1), so even "" costs a byte.
			description: "boundary: the empty string still emits its NUL, rta_len 5",
			bufLen:      8,
			put:         func(a *AttrBuilder) error { return a.PutString(uint16(unix.IFLA_IFALIAS), "") },
			want:        rtattr(uint16(unix.IFLA_IFALIAS), []byte{0}),
		},
		{
			description: "boundary: a payload that exactly fills the buffer succeeds, n == len(buf)",
			bufLen:      8,
			put:         func(a *AttrBuilder) error { return a.PutU32(uint16(unix.IFLA_EXT_MASK), 0x09) },
			want:        rtattr(uint16(unix.IFLA_EXT_MASK), le32(0x09)),
		},
		{
			description: "boundary: one byte short of the aligned length is ErrAttrNoSpace, buffer unmodified",
			bufLen:      7,
			put:         func(a *AttrBuilder) error { return a.PutU32(uint16(unix.IFLA_EXT_MASK), 0x09) },
			wantErr:     ErrAttrNoSpace,
		},
		{
			// A 1-byte payload needs 5 bytes of rta_len and 8 bytes of buffer,
			// because the padding is part of what the message carries.
			description: "boundary: a 1-byte payload needs 8 bytes of buffer, not 5, because padding counts",
			bufLen:      5,
			put:         func(a *AttrBuilder) error { return a.PutU8(uint16(unix.IFLA_LINKMODE), 1) },
			wantErr:     ErrAttrNoSpace,
		},
		{
			description: "boundary: zero-length PutBytes is a legal bare 4-byte flag attribute",
			bufLen:      4,
			put:         func(a *AttrBuilder) error { return a.PutBytes(uint16(unix.IFLA_PROTINFO), nil) },
			want:        rtattr(uint16(unix.IFLA_PROTINFO), nil),
		},
		{
			description: "boundary: the largest payload rta_len can express exactly fills a 65536-byte buffer",
			bufLen:      1 + RTAttrSizeCst + RtaMaxPayloadCst,
			put: func(a *AttrBuilder) error {
				return a.PutBytes(uint16(unix.IFLA_PROTINFO), make([]byte, RtaMaxPayloadCst))
			},
			want: rtattr(uint16(unix.IFLA_PROTINFO), make([]byte, RtaMaxPayloadCst)),
		},
		{
			description: "negative: the zero-value builder has a nil buffer, so every Put is ErrAttrNoSpace",
			bufLen:      -1,
			put:         func(a *AttrBuilder) error { return a.PutU32(uint16(unix.IFLA_EXT_MASK), 0x09) },
			wantErr:     ErrAttrNoSpace,
		},
		{
			// Ample buffer, so this can only be the rta_len limit, not space.
			description: "corner: a payload one byte past RtaMaxPayloadCst overflows rta_len, ErrAttrTooLong",
			bufLen:      70000,
			put: func(a *AttrBuilder) error {
				return a.PutBytes(uint16(unix.IFLA_PROTINFO), make([]byte, RtaMaxPayloadCst+1))
			},
			wantErr: ErrAttrTooLong,
		},
		{
			// WalkRTAttrs masks NLA_F_NESTED on the way in; the encoder must not
			// mask it on the way out, or a nest would go on the wire unflagged and
			// pkg/nlparity — which compares the UNMASKED type — would report it.
			description: "corner: an atype with NLA_F_NESTED preset is written verbatim, not masked",
			bufLen:      16,
			put: func(a *AttrBuilder) error {
				return a.PutBytes(uint16(unix.NLA_F_NESTED)|uint16(unix.IFLA_LINKINFO), nested)
			},
			want: rtattr(uint16(unix.NLA_F_NESTED)|uint16(unix.IFLA_LINKINFO), nested),
		},
	}

	for _, tc := range tests {
		t.Run(tc.description, func(t *testing.T) {
			var buf []byte
			if tc.bufLen >= 0 {
				buf = bytes.Repeat([]byte{prefill}, tc.bufLen)
			}
			ab := NewAttrBuilder(buf)

			err := tc.put(&ab)

			if tc.wantErr != nil {
				if !errors.Is(err, tc.wantErr) {
					t.Fatalf("err = %v, want %v", err, tc.wantErr)
				}
				if ab.Len() != 0 {
					t.Errorf("Len() = %d after a failed Put, want 0", ab.Len())
				}
				// The documented guarantee: nothing was written.
				for i, b := range buf {
					if b != prefill {
						t.Fatalf("buf[%d] = %#x after a failed Put, want the prefill %#x: the buffer was modified", i, b, prefill)
					}
				}
				return
			}

			if err != nil {
				t.Fatalf("unexpected err = %v", err)
			}
			if got := ab.Bytes(); !bytes.Equal(got, tc.want) {
				if len(got) > 64 || len(tc.want) > 64 {
					t.Fatalf("Bytes() = %d bytes, want %d bytes (contents differ)", len(got), len(tc.want))
				}
				t.Fatalf("Bytes() = % x, want % x", got, tc.want)
			}
			if ab.Len() != len(tc.want) {
				t.Errorf("Len() = %d, want %d", ab.Len(), len(tc.want))
			}
			// Anything past the cursor is still the caller's, untouched.
			for i := ab.Len(); i < len(buf); i++ {
				if buf[i] != prefill {
					t.Fatalf("buf[%d] = %#x past the cursor, want the prefill %#x", i, buf[i], prefill)
				}
			}
		})
	}
}

// TestAttrBuilderReset checks that Reset rewinds the cursor and that a rewound
// builder overwrites, rather than appends.
//
// go test ./pkg/xtcpnl/ -run TestAttrBuilderReset
func TestAttrBuilderReset(t *testing.T) {
	tests := []struct {
		description string
		steps       func(a *AttrBuilder) error
		want        []byte
	}{
		{
			description: "positive: Reset then a fresh Put writes from offset 0",
			steps: func(a *AttrBuilder) error {
				if err := a.PutU32(uint16(unix.IFLA_EXT_MASK), 0x09); err != nil {
					return err
				}
				a.Reset()
				return a.PutU32(uint16(unix.IFLA_MASTER), 7)
			},
			want: rtattr(uint16(unix.IFLA_MASTER), le32(7)),
		},
		{
			description: "boundary: Reset on an empty builder is a no-op",
			steps: func(a *AttrBuilder) error {
				a.Reset()
				return a.PutU8(uint16(unix.IFLA_LINKMODE), 1)
			},
			want: rtattr(uint16(unix.IFLA_LINKMODE), []byte{1}),
		},
		{
			description: "boundary: Reset frees the whole buffer again, so a too-big Put now fits",
			steps: func(a *AttrBuilder) error {
				if err := a.PutU32(uint16(unix.IFLA_EXT_MASK), 0x09); err != nil {
					return err
				}
				if err := a.PutBytes(uint16(unix.IFLA_ADDRESS), macb(1, 2, 3, 4, 5, 6)); !errors.Is(err, ErrAttrNoSpace) {
					return err
				}
				a.Reset()
				return a.PutBytes(uint16(unix.IFLA_ADDRESS), macb(1, 2, 3, 4, 5, 6))
			},
			want: rtattr(uint16(unix.IFLA_ADDRESS), macb(1, 2, 3, 4, 5, 6)),
		},
	}

	for _, tc := range tests {
		t.Run(tc.description, func(t *testing.T) {
			ab := NewAttrBuilder(make([]byte, 12))
			if err := tc.steps(&ab); err != nil {
				t.Fatalf("unexpected err = %v", err)
			}
			if got := ab.Bytes(); !bytes.Equal(got, tc.want) {
				t.Fatalf("Bytes() = % x, want % x", got, tc.want)
			}
		})
	}
}

// ---- the read-only invariant -------------------------------------------------

// TestIsGetRequestType checks the arithmetic GET predicate, including the two
// places the arithmetic is deliberately looser than the kernel's enum.
//
// go test ./pkg/xtcpnl/ -run TestIsGetRequestType$
func TestIsGetRequestType(t *testing.T) {
	tests := []struct {
		description string
		msgType     uint16
		want        bool
	}{
		{"positive: RTM_GETLINK", uint16(unix.RTM_GETLINK), true},
		{"positive: RTM_GETADDR", uint16(unix.RTM_GETADDR), true},
		{"positive: RTM_GETROUTE", uint16(unix.RTM_GETROUTE), true},
		{"positive: RTM_GETNEIGH", uint16(unix.RTM_GETNEIGH), true},
		// The three groups with missing members. The kernel leaves the slot empty
		// rather than shifting the group, so the residue still holds.
		{"positive: RTM_GETNEIGHTBL, whose group has no DEL", uint16(unix.RTM_GETNEIGHTBL), true},
		{"positive: RTM_GETDCB, whose group has neither NEW nor DEL", uint16(unix.RTM_GETDCB), true},
		{"positive: RTM_GETSTATS, whose group has no DEL", uint16(unix.RTM_GETSTATS), true},
		{"positive: RTM_GETTUNNEL, the tail of the enum", uint16(unix.RTM_GETTUNNEL), true},

		{"negative: RTM_NEWLINK is residue 0", uint16(unix.RTM_NEWLINK), false},
		{"negative: RTM_DELLINK is residue 1", uint16(unix.RTM_DELLINK), false},
		{"negative: RTM_SETLINK is residue 3", uint16(unix.RTM_SETLINK), false},
		{"negative: NLMSG_NOOP is below RTM_BASE", uint16(unix.NLMSG_NOOP), false},
		{"negative: NLMSG_ERROR is below RTM_BASE", uint16(unix.NLMSG_ERROR), false},
		{"negative: NLMSG_DONE is below RTM_BASE", uint16(unix.NLMSG_DONE), false},

		{"boundary: 0", 0, false},
		{"boundary: RTM_BASE itself is RTM_NEWLINK, residue 0", uint16(unix.RTM_BASE), false},
		{"boundary: RTM_BASE-1 is below the enum", uint16(unix.RTM_BASE) - 1, false},
		{"boundary: RTM_BASE+2 is the first GET", uint16(unix.RTM_BASE) + 2, true},
		{"boundary: 0xffff is residue 3", 0xffff, false},

		// The one direction the rule is imprecise in, and it is harmless: these
		// are accepted and the kernel answers with an error. A GET can never
		// mutate, so a false accept costs nothing; a hand-written list, by
		// contrast, silently rejects every family added upstream after it.
		{"corner: slot 54 has no RTM_GETPREFIX but is residue 2, accepted", 54, true},
		{"corner: 0xfffe is residue 2 with no such type, accepted", 0xfffe, true},
	}

	for _, tc := range tests {
		t.Run(tc.description, func(t *testing.T) {
			if got := IsGetRequestType(tc.msgType); got != tc.want {
				t.Errorf("IsGetRequestType(%d) = %v, want %v", tc.msgType, got, tc.want)
			}
		})
	}
}

// namedRtmGetTypes is every RTM_GET* constant golang.org/x/sys/unix v0.47.0
// exports. All of them must be accepted, including the families this package
// has no decoder for — the encoder's allowlist is about not writing, not about
// what we can parse.
var namedRtmGetTypes = []struct {
	name    string
	msgType uint16
}{
	{"RTM_GETLINK", uint16(unix.RTM_GETLINK)},
	{"RTM_GETADDR", uint16(unix.RTM_GETADDR)},
	{"RTM_GETROUTE", uint16(unix.RTM_GETROUTE)},
	{"RTM_GETNEIGH", uint16(unix.RTM_GETNEIGH)},
	{"RTM_GETRULE", uint16(unix.RTM_GETRULE)},
	{"RTM_GETQDISC", uint16(unix.RTM_GETQDISC)},
	{"RTM_GETTCLASS", uint16(unix.RTM_GETTCLASS)},
	{"RTM_GETTFILTER", uint16(unix.RTM_GETTFILTER)},
	{"RTM_GETACTION", uint16(unix.RTM_GETACTION)},
	{"RTM_GETADDRLABEL", uint16(unix.RTM_GETADDRLABEL)},
	{"RTM_GETDCB", uint16(unix.RTM_GETDCB)},
	{"RTM_GETNETCONF", uint16(unix.RTM_GETNETCONF)},
	{"RTM_GETMDB", uint16(unix.RTM_GETMDB)},
	{"RTM_GETNSID", uint16(unix.RTM_GETNSID)},
	{"RTM_GETSTATS", uint16(unix.RTM_GETSTATS)},
	{"RTM_GETCHAIN", uint16(unix.RTM_GETCHAIN)},
	{"RTM_GETNEXTHOP", uint16(unix.RTM_GETNEXTHOP)},
	{"RTM_GETLINKPROP", uint16(unix.RTM_GETLINKPROP)},
	{"RTM_GETVLAN", uint16(unix.RTM_GETVLAN)},
	{"RTM_GETNEXTHOPBUCKET", uint16(unix.RTM_GETNEXTHOPBUCKET)},
	{"RTM_GETTUNNEL", uint16(unix.RTM_GETTUNNEL)},
	{"RTM_GETNEIGHTBL", uint16(unix.RTM_GETNEIGHTBL)},
	{"RTM_GETMULTICAST", uint16(unix.RTM_GETMULTICAST)},
	{"RTM_GETANYCAST", uint16(unix.RTM_GETANYCAST)},
}

// namedRtmWriteTypes is every RTM_NEW*, RTM_DEL* and RTM_SET* constant
// golang.org/x/sys/unix v0.47.0 exports. Every one must be rejected. This list
// is the read-only invariant in executable form: if BuildRequest ever grows a
// path that emits one of these, this table goes red by name.
//
// The notification-only NEWs (RTM_NEWPREFIX, RTM_NEWNDUSEROPT,
// RTM_NEWCACHEREPORT) are included. Nothing in userspace sends them either.
var namedRtmWriteTypes = []struct {
	name    string
	msgType uint16
}{
	{"RTM_NEWLINK", uint16(unix.RTM_NEWLINK)},
	{"RTM_DELLINK", uint16(unix.RTM_DELLINK)},
	{"RTM_SETLINK", uint16(unix.RTM_SETLINK)},
	{"RTM_NEWADDR", uint16(unix.RTM_NEWADDR)},
	{"RTM_DELADDR", uint16(unix.RTM_DELADDR)},
	{"RTM_NEWROUTE", uint16(unix.RTM_NEWROUTE)},
	{"RTM_DELROUTE", uint16(unix.RTM_DELROUTE)},
	{"RTM_NEWNEIGH", uint16(unix.RTM_NEWNEIGH)},
	{"RTM_DELNEIGH", uint16(unix.RTM_DELNEIGH)},
	{"RTM_NEWRULE", uint16(unix.RTM_NEWRULE)},
	{"RTM_DELRULE", uint16(unix.RTM_DELRULE)},
	{"RTM_NEWQDISC", uint16(unix.RTM_NEWQDISC)},
	{"RTM_DELQDISC", uint16(unix.RTM_DELQDISC)},
	{"RTM_NEWTCLASS", uint16(unix.RTM_NEWTCLASS)},
	{"RTM_DELTCLASS", uint16(unix.RTM_DELTCLASS)},
	{"RTM_NEWTFILTER", uint16(unix.RTM_NEWTFILTER)},
	{"RTM_DELTFILTER", uint16(unix.RTM_DELTFILTER)},
	{"RTM_NEWACTION", uint16(unix.RTM_NEWACTION)},
	{"RTM_DELACTION", uint16(unix.RTM_DELACTION)},
	{"RTM_NEWPREFIX", uint16(unix.RTM_NEWPREFIX)},
	{"RTM_NEWMULTICAST", uint16(unix.RTM_NEWMULTICAST)},
	{"RTM_DELMULTICAST", uint16(unix.RTM_DELMULTICAST)},
	{"RTM_NEWANYCAST", uint16(unix.RTM_NEWANYCAST)},
	{"RTM_DELANYCAST", uint16(unix.RTM_DELANYCAST)},
	{"RTM_NEWNEIGHTBL", uint16(unix.RTM_NEWNEIGHTBL)},
	{"RTM_SETNEIGHTBL", uint16(unix.RTM_SETNEIGHTBL)},
	{"RTM_NEWNDUSEROPT", uint16(unix.RTM_NEWNDUSEROPT)},
	{"RTM_NEWADDRLABEL", uint16(unix.RTM_NEWADDRLABEL)},
	{"RTM_DELADDRLABEL", uint16(unix.RTM_DELADDRLABEL)},
	{"RTM_SETDCB", uint16(unix.RTM_SETDCB)},
	{"RTM_NEWNETCONF", uint16(unix.RTM_NEWNETCONF)},
	{"RTM_DELNETCONF", uint16(unix.RTM_DELNETCONF)},
	{"RTM_NEWMDB", uint16(unix.RTM_NEWMDB)},
	{"RTM_DELMDB", uint16(unix.RTM_DELMDB)},
	{"RTM_NEWNSID", uint16(unix.RTM_NEWNSID)},
	{"RTM_DELNSID", uint16(unix.RTM_DELNSID)},
	{"RTM_NEWSTATS", uint16(unix.RTM_NEWSTATS)},
	{"RTM_SETSTATS", uint16(unix.RTM_SETSTATS)},
	{"RTM_NEWCACHEREPORT", uint16(unix.RTM_NEWCACHEREPORT)},
	{"RTM_NEWCHAIN", uint16(unix.RTM_NEWCHAIN)},
	{"RTM_DELCHAIN", uint16(unix.RTM_DELCHAIN)},
	{"RTM_NEWNEXTHOP", uint16(unix.RTM_NEWNEXTHOP)},
	{"RTM_DELNEXTHOP", uint16(unix.RTM_DELNEXTHOP)},
	{"RTM_NEWLINKPROP", uint16(unix.RTM_NEWLINKPROP)},
	{"RTM_DELLINKPROP", uint16(unix.RTM_DELLINKPROP)},
	{"RTM_NEWVLAN", uint16(unix.RTM_NEWVLAN)},
	{"RTM_DELVLAN", uint16(unix.RTM_DELVLAN)},
	{"RTM_NEWNEXTHOPBUCKET", uint16(unix.RTM_NEWNEXTHOPBUCKET)},
	{"RTM_DELNEXTHOPBUCKET", uint16(unix.RTM_DELNEXTHOPBUCKET)},
	{"RTM_NEWTUNNEL", uint16(unix.RTM_NEWTUNNEL)},
	{"RTM_DELTUNNEL", uint16(unix.RTM_DELTUNNEL)},
}

// TestBuildRequestRejectsEveryNamedWriteType walks the two name tables above
// and asserts BuildRequest accepts every RTM_GET* and rejects every RTM_NEW*,
// RTM_DEL* and RTM_SET* x/sys/unix knows about.
//
// This is the executable form of "pkg/xtcpnl never creates, deletes or sets".
// It is enforced on the message type and not on the flags, because the flags
// cannot express it: the kernel overloads the same bits by message type, so
// NLM_F_DUMP (ROOT|MATCH, 0x300) is bit-identical to REPLACE|EXCL
// (linux/include/uapi/linux/netlink.h:70-79). TestNlmFlagsAreOverloaded pins
// that.
//
// go test ./pkg/xtcpnl/ -run TestBuildRequestRejectsEveryNamedWriteType
func TestBuildRequestRejectsEveryNamedWriteType(t *testing.T) {
	type row struct {
		description string
		msgType     uint16
		wantErr     error
	}

	// The five is the literal control-type block appended after the loops.
	tests := make([]row, 0, len(namedRtmGetTypes)+len(namedRtmWriteTypes)+5)
	for _, g := range namedRtmGetTypes {
		tests = append(tests, row{"positive: " + g.name + " is buildable", g.msgType, nil})
	}
	for _, w := range namedRtmWriteTypes {
		tests = append(tests, row{"negative: " + w.name + " is not buildable", w.msgType, ErrNotAGetRequest})
	}
	// The control types, which are not covered by either name table.
	tests = append(tests,
		row{"boundary: NLMSG_NOOP is the one permitted control type", uint16(unix.NLMSG_NOOP), nil},
		row{"negative: NLMSG_ERROR is kernel-to-userspace only", uint16(unix.NLMSG_ERROR), ErrNotAGetRequest},
		row{"negative: NLMSG_DONE is kernel-to-userspace only", uint16(unix.NLMSG_DONE), ErrNotAGetRequest},
		row{"negative: NLMSG_OVERRUN is kernel-to-userspace only", uint16(unix.NLMSG_OVERRUN), ErrNotAGetRequest},
		row{"boundary: message type 0 is not a netlink type at all", 0, ErrNotAGetRequest},
	)

	for _, tc := range tests {
		t.Run(tc.description, func(t *testing.T) {
			// A family header of exactly the right size, so a rejection can only
			// be ErrNotAGetRequest and never ErrBadFamilyHdr.
			var hdr []byte
			if n := FamilyHdrLen(tc.msgType); n > 0 {
				hdr = make([]byte, n)
			}

			got, err := BuildRequest(tc.msgType, uint16(unix.NLM_F_DUMP), testSeq, hdr, nil)

			if tc.wantErr != nil {
				if !errors.Is(err, tc.wantErr) {
					t.Fatalf("err = %v, want %v", err, tc.wantErr)
				}
				if got != nil {
					t.Errorf("request = % x, want nil on rejection", got)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected err = %v", err)
			}
			if len(got) != NlMsgHdrSizeCst+len(hdr) {
				t.Fatalf("len(request) = %d, want %d", len(got), NlMsgHdrSizeCst+len(hdr))
			}
			if mt := binary.LittleEndian.Uint16(got[4:6]); mt != tc.msgType {
				t.Errorf("nlmsg_type = %d, want %d", mt, tc.msgType)
			}
		})
	}
}

// TestIsGetRequestTypeExhaustive sweeps all 65536 uint16 values against an
// independently written reference predicate.
//
// The reference is spelled differently on purpose — `m >= 16 && m&3 == 2`,
// valid because RTM_BASE is itself a multiple of 4 — so a bug in the
// subtract-and-modulo form under test does not reproduce in the oracle. The
// sweep is what lets the production code be arithmetic rather than a list of
// constants that goes stale on the next kernel release.
//
// go test ./pkg/xtcpnl/ -run TestIsGetRequestTypeExhaustive
func TestIsGetRequestTypeExhaustive(t *testing.T) {
	refIsGet := func(m uint16) bool { return m >= 16 && m&3 == 2 }

	tests := []struct {
		description string
		got         func(m uint16) bool
		want        func(m uint16) bool
	}{
		{
			description: "positive: IsGetRequestType accepts exactly RTM_BASE+4k+2",
			got:         IsGetRequestType,
			want:        refIsGet,
		},
		{
			description: "positive: IsBuildableRequestType is that set plus exactly NLMSG_NOOP",
			got:         IsBuildableRequestType,
			want:        func(m uint16) bool { return m == uint16(unix.NLMSG_NOOP) || refIsGet(m) },
		},
	}

	for _, tc := range tests {
		t.Run(tc.description, func(t *testing.T) {
			var accepted int
			for i := 0; i <= 0xffff; i++ {
				m := uint16(i)
				got, want := tc.got(m), tc.want(m)
				if got != want {
					t.Fatalf("msgType %d (%#x): got %v, want %v", m, m, got, want)
				}
				if want {
					accepted++
				}
			}
			// (65536-16)/4 = 16380 residue-2 slots at or above RTM_BASE.
			if accepted < 16380 {
				t.Errorf("accepted %d of 65536, want at least 16380: the sweep is not exercising the accept path", accepted)
			}
		})
	}
}

// TestNlmFlagsAreOverloaded pins the kernel's flag aliasing at runtime, because
// it is the reason the read-only invariant is enforced on the message type.
//
// If a future x/sys/unix ever stopped aliasing these — it cannot, they are
// kernel UAPI — the rejection-by-type design would deserve revisiting, and this
// test is where that would surface.
//
// go test ./pkg/xtcpnl/ -run TestNlmFlagsAreOverloaded
func TestNlmFlagsAreOverloaded(t *testing.T) {
	tests := []struct {
		description string
		got         int
		want        int
	}{
		{"positive: NLM_F_ROOT and NLM_F_REPLACE are the same bit", unix.NLM_F_ROOT, unix.NLM_F_REPLACE},
		{"positive: NLM_F_MATCH and NLM_F_EXCL are the same bit", unix.NLM_F_MATCH, unix.NLM_F_EXCL},
		{"positive: NLM_F_ATOMIC and NLM_F_CREATE are the same bit", unix.NLM_F_ATOMIC, unix.NLM_F_CREATE},
		{"corner: NLM_F_DUMP is bit-identical to REPLACE|EXCL", unix.NLM_F_DUMP, unix.NLM_F_REPLACE | unix.NLM_F_EXCL},
	}

	for _, tc := range tests {
		t.Run(tc.description, func(t *testing.T) {
			if tc.got != tc.want {
				t.Errorf("got %#x, want %#x", tc.got, tc.want)
			}
		})
	}
}

// ---- BuildRequest ------------------------------------------------------------

// TestBuildRequest checks the generic request builder byte-for-byte. Its first
// row reconstructs the whole 40-byte `ip link show` datagram from the capture,
// which is the encoder's reason to exist.
//
// go test ./pkg/xtcpnl/ -run TestBuildRequest$
func TestBuildRequest(t *testing.T) {
	captured := capturedIPLinkShowRequest(t)

	extMask := mustAttrs(t, 8, func(a *AttrBuilder) error {
		return a.PutU32(uint16(unix.IFLA_EXT_MASK), RTEXT_FILTER_VF|RTEXT_FILTER_SKIP_STATS)
	})
	mac := macb(0x02, 0x00, 0x00, 0x00, 0x00, 0x01)
	threeAttrs := mustAttrs(t, 32,
		func(a *AttrBuilder) error { return a.PutU32(uint16(unix.IFLA_EXT_MASK), 0x09) },
		func(a *AttrBuilder) error { return a.PutString(uint16(unix.IFLA_IFNAME), "lo") },
		func(a *AttrBuilder) error { return a.PutBytes(uint16(unix.IFLA_ADDRESS), mac) },
	)

	linkHdr := ifinfomsgHdr(unix.AF_PACKET, 0, 0, 0)

	// A fib_rule_hdr with only the family byte set — every other byte has to
	// be zero or the kernel's strict-mode validator refuses the dump
	// (net/core/fib_rules.c:1271-1276). Built by hand rather than through
	// BuildDumpRuleRequest so this table stays a test of BuildRequest.
	ruleHdr := make([]byte, FibRuleHdrSizeCst)
	ruleHdr[0] = unix.AF_INET

	tests := []struct {
		description string
		msgType     uint16
		flags       uint16
		seq         uint32
		familyHdr   []byte
		attrs       []byte
		want        []byte
		wantErr     error
	}{
		{
			// The expectation is iproute2's own bytes, not a literal.
			description: "positive: the ip link show request is reproduced exactly, modulo seq and pid",
			msgType:     uint16(unix.RTM_GETLINK),
			flags:       uint16(unix.NLM_F_DUMP),
			seq:         0,
			familyHdr:   linkHdr,
			attrs:       extMask,
			want:        zeroSeqPid(captured),
		},
		{
			description: "positive: RTM_GETADDR with no attributes is 24 bytes",
			msgType:     uint16(unix.RTM_GETADDR),
			flags:       uint16(unix.NLM_F_DUMP),
			seq:         testSeq,
			familyHdr:   ifaddrmsgHdr(unix.AF_INET, 0, 0, 0, 0),
			want: nlmsg(uint16(unix.RTM_GETADDR), uint16(unix.NLM_F_REQUEST|unix.NLM_F_DUMP), testSeq,
				ifaddrmsgHdr(unix.AF_INET, 0, 0, 0, 0)),
		},
		{
			description: "positive: RTM_GETROUTE carries the 12-byte rtmsg",
			msgType:     uint16(unix.RTM_GETROUTE),
			flags:       uint16(unix.NLM_F_DUMP),
			seq:         testSeq,
			familyHdr:   rtmsgHdr(unix.AF_INET, 0, 0, 0, 0, 0, 0, 0),
			want: nlmsg(uint16(unix.RTM_GETROUTE), uint16(unix.NLM_F_REQUEST|unix.NLM_F_DUMP), testSeq,
				rtmsgHdr(unix.AF_INET, 0, 0, 0, 0, 0, 0, 0)),
		},
		{
			description: "negative: RTM_NEWLINK is refused, ErrNotAGetRequest",
			msgType:     uint16(unix.RTM_NEWLINK),
			flags:       uint16(unix.NLM_F_CREATE | unix.NLM_F_EXCL),
			seq:         testSeq,
			familyHdr:   linkHdr,
			wantErr:     ErrNotAGetRequest,
		},
		{
			description: "negative: RTM_DELROUTE is refused, ErrNotAGetRequest",
			msgType:     uint16(unix.RTM_DELROUTE),
			flags:       0,
			seq:         testSeq,
			familyHdr:   rtmsgHdr(unix.AF_INET, 0, 0, 0, 0, 0, 0, 0),
			wantErr:     ErrNotAGetRequest,
		},
		{
			description: "negative: RTM_SETLINK is refused, ErrNotAGetRequest",
			msgType:     uint16(unix.RTM_SETLINK),
			flags:       0,
			seq:         testSeq,
			familyHdr:   linkHdr,
			wantErr:     ErrNotAGetRequest,
		},
		{
			// FamilyHdrLen models NLMSG_ERROR at 0, so being modeled is not being
			// buildable: the type check comes first.
			description: "negative: NLMSG_ERROR is refused even though FamilyHdrLen models it",
			msgType:     uint16(unix.NLMSG_ERROR),
			flags:       0,
			seq:         testSeq,
			wantErr:     ErrNotAGetRequest,
		},
		{
			description: "boundary: NLMSG_NOOP with a nil family header is a bare 16-byte message",
			msgType:     uint16(unix.NLMSG_NOOP),
			flags:       0,
			seq:         testSeq,
			want:        nlmsg(uint16(unix.NLMSG_NOOP), uint16(unix.NLM_F_REQUEST), testSeq, nil),
		},
		{
			description: "boundary: flags 0 still gets NLM_F_REQUEST, because nothing else is a request",
			msgType:     uint16(unix.RTM_GETLINK),
			flags:       0,
			seq:         testSeq,
			familyHdr:   linkHdr,
			want:        nlmsg(uint16(unix.RTM_GETLINK), uint16(unix.NLM_F_REQUEST), testSeq, linkHdr),
		},
		{
			description: "boundary: NLM_F_REQUEST passed explicitly is not doubled",
			msgType:     uint16(unix.RTM_GETLINK),
			flags:       uint16(unix.NLM_F_REQUEST | unix.NLM_F_DUMP),
			seq:         testSeq,
			familyHdr:   linkHdr,
			want: nlmsg(uint16(unix.RTM_GETLINK), uint16(unix.NLM_F_REQUEST|unix.NLM_F_DUMP), testSeq,
				linkHdr),
		},
		{
			description: "boundary: seq 0xffffffff round-trips",
			msgType:     uint16(unix.RTM_GETLINK),
			flags:       uint16(unix.NLM_F_DUMP),
			seq:         0xffffffff,
			familyHdr:   linkHdr,
			want: nlmsg(uint16(unix.RTM_GETLINK), uint16(unix.NLM_F_REQUEST|unix.NLM_F_DUMP), 0xffffffff,
				linkHdr),
		},
		{
			description: "corner: a family header shorter than ifinfomsg is ErrBadFamilyHdr",
			msgType:     uint16(unix.RTM_GETLINK),
			flags:       uint16(unix.NLM_F_DUMP),
			seq:         testSeq,
			familyHdr:   linkHdr[:IfInfomsgSizeCst-1],
			wantErr:     ErrBadFamilyHdr,
		},
		{
			// Too long is the worse mistake: the extra bytes land exactly where the
			// kernel reads the first rta_len and rta_type.
			description: "corner: a family header longer than ifinfomsg is ErrBadFamilyHdr",
			msgType:     uint16(unix.RTM_GETLINK),
			flags:       uint16(unix.NLM_F_DUMP),
			seq:         testSeq,
			familyHdr:   append(append([]byte{}, linkHdr...), 0, 0, 0, 0),
			wantErr:     ErrBadFamilyHdr,
		},
		{
			description: "corner: a nil family header for a type that requires one is ErrBadFamilyHdr",
			msgType:     uint16(unix.RTM_GETLINK),
			flags:       uint16(unix.NLM_F_DUMP),
			seq:         testSeq,
			wantErr:     ErrBadFamilyHdr,
		},
		{
			description: "corner: NLMSG_NOOP with a non-empty family header is ErrBadFamilyHdr",
			msgType:     uint16(unix.NLMSG_NOOP),
			flags:       0,
			seq:         testSeq,
			familyHdr:   []byte{0, 0, 0, 0},
			wantErr:     ErrBadFamilyHdr,
		},
		{
			// 16 + 16 + (8 + 8 + 12) = 60. The third attribute's 2 padding bytes
			// are inside nlmsg_len, matching addattr_l advancing by RTA_ALIGN.
			description: "corner: three attributes back-patch nlmsg_len to 16+hdr+sum of aligned attrs",
			msgType:     uint16(unix.RTM_GETLINK),
			flags:       uint16(unix.NLM_F_DUMP),
			seq:         testSeq,
			familyHdr:   linkHdr,
			attrs:       threeAttrs,
			want: nlmsg(uint16(unix.RTM_GETLINK), uint16(unix.NLM_F_REQUEST|unix.NLM_F_DUMP), testSeq,
				concat(linkHdr, threeAttrs)),
		},
		{
			// This row used to assert the opposite — that RTM_GETRULE was
			// unmodeled and its header passed through unchecked. fib_rule_hdr
			// now has a decoder, so the check applies, and the four-byte
			// header that used to be accepted is the exact mistake the check
			// exists to catch: an rtgenmsg where a 12-byte struct belongs
			// leaves the kernel reading frh.flags and the first rta_len out
			// of whatever follows. The unmodeled-passthrough property has not
			// gone untested; the RTM_GETNSID row below carries it.
			description: "corner: RTM_GETRULE is modeled now, so a 4-byte rtgenmsg is ErrBadFamilyHdr",
			msgType:     uint16(unix.RTM_GETRULE),
			flags:       uint16(unix.NLM_F_DUMP),
			seq:         testSeq,
			familyHdr:   []byte{unix.AF_INET, 0, 0, 0},
			wantErr:     ErrBadFamilyHdr,
		},
		{
			description: "positive: RTM_GETRULE with a full fib_rule_hdr is the 28-byte rule dump",
			msgType:     uint16(unix.RTM_GETRULE),
			flags:       uint16(unix.NLM_F_DUMP),
			seq:         testSeq,
			familyHdr:   ruleHdr,
			want: nlmsg(uint16(unix.RTM_GETRULE), uint16(unix.NLM_F_REQUEST|unix.NLM_F_DUMP), testSeq,
				ruleHdr),
		},
		{
			// The request pkg/nsdiscover builds by hand today: a single get with a
			// 4-byte rtgenmsg and a NETNSA_FD attribute. Once Item 3 lands its
			// builder, that hand-rolled copy collapses onto this path.
			description: "corner: RTM_GETNSID single-get with an rtgenmsg and one attribute",
			msgType:     uint16(unix.RTM_GETNSID),
			flags:       0,
			seq:         testSeq,
			familyHdr:   []byte{unix.AF_UNSPEC, 0, 0, 0},
			attrs:       mustAttrs(t, 8, func(a *AttrBuilder) error { return a.PutU32(3, 7) }),
			want: nlmsg(uint16(unix.RTM_GETNSID), uint16(unix.NLM_F_REQUEST), testSeq,
				concat([]byte{unix.AF_UNSPEC, 0, 0, 0}, rtattr(3, le32(7)))),
		},
	}

	for _, tc := range tests {
		t.Run(tc.description, func(t *testing.T) {
			got, err := BuildRequest(tc.msgType, tc.flags, tc.seq, tc.familyHdr, tc.attrs)

			if tc.wantErr != nil {
				if !errors.Is(err, tc.wantErr) {
					t.Fatalf("err = %v, want %v", err, tc.wantErr)
				}
				if got != nil {
					t.Errorf("request = % x, want nil on rejection", got)
				}
				return
			}

			if err != nil {
				t.Fatalf("unexpected err = %v", err)
			}
			if !bytes.Equal(got, tc.want) {
				t.Fatalf("request = % x, want % x", got, tc.want)
			}
			// nlmsg_len is the message's own claim about its size; assert it
			// separately from the byte compare so a wrong length is named.
			if n := binary.LittleEndian.Uint32(got[0:4]); int(n) != len(got) {
				t.Errorf("nlmsg_len = %d, want %d", n, len(got))
			}
			if pid := binary.LittleEndian.Uint32(got[12:16]); pid != 0 {
				t.Errorf("nlmsg_pid = %d, want 0: a request carrying a pid cannot be told from a reply", pid)
			}
			if flags := binary.LittleEndian.Uint16(got[6:8]); flags&uint16(unix.NLM_F_REQUEST) == 0 {
				t.Errorf("nlmsg_flags = %#x, NLM_F_REQUEST not set", flags)
			}
		})
	}
}

// TestBuildDumpRequestRejectsWriteTypes covers the infallible wrapper's
// documented behavior: it has no error to return, so a rejected message type
// yields nil.
//
// That is not a silent failure, and the last row proves it: DumpRtnetlink
// refuses a nil request with ErrShortRequest before touching the socket, so a
// caller that ignores the nil still cannot put a write on the wire. The fd is
// -1 precisely to show no syscall happens.
//
// go test ./pkg/xtcpnl/ -run TestBuildDumpRequestRejectsWriteTypes
func TestBuildDumpRequestRejectsWriteTypes(t *testing.T) {
	linkHdr := ifinfomsgHdr(unix.AF_UNSPEC, 0, 0, 0)

	tests := []struct {
		description string
		msgType     uint16
		familyHdr   []byte
		wantNil     bool
	}{
		{"positive: RTM_GETLINK builds a dump request", uint16(unix.RTM_GETLINK), linkHdr, false},
		{"positive: RTM_GETNEIGH builds a dump request", uint16(unix.RTM_GETNEIGH), make([]byte, NdMsgSizeCst), false},
		{"negative: RTM_NEWLINK yields nil", uint16(unix.RTM_NEWLINK), linkHdr, true},
		{"negative: RTM_DELLINK yields nil", uint16(unix.RTM_DELLINK), linkHdr, true},
		{"negative: RTM_SETLINK yields nil", uint16(unix.RTM_SETLINK), linkHdr, true},
		{"negative: NLMSG_ERROR yields nil", uint16(unix.NLMSG_ERROR), nil, true},
		{"boundary: NLMSG_NOOP is permitted, so it builds", uint16(unix.NLMSG_NOOP), nil, false},
	}

	for _, tc := range tests {
		t.Run(tc.description, func(t *testing.T) {
			got := BuildDumpRequest(tc.msgType, testSeq, tc.familyHdr)

			if tc.wantNil {
				if got != nil {
					t.Fatalf("request = % x, want nil", got)
				}
				// The nil is caught downstream, before any send.
				err := DumpRtnetlink(-1, got, nil, func(uint16, []byte) error { return nil })
				if !errors.Is(err, ErrShortRequest) {
					t.Errorf("DumpRtnetlink(nil request) = %v, want %v", err, ErrShortRequest)
				}
				return
			}

			want := nlmsg(tc.msgType, uint16(unix.NLM_F_REQUEST|unix.NLM_F_DUMP), testSeq, tc.familyHdr)
			if !bytes.Equal(got, want) {
				t.Fatalf("request = % x, want % x", got, want)
			}
			// BuildDumpRequest and BuildRequest share layoutRequest; assert they
			// agree, so the wrapper cannot drift from the checked path.
			viaBuildRequest, err := BuildRequest(tc.msgType, uint16(unix.NLM_F_DUMP), testSeq, tc.familyHdr, nil)
			if err != nil {
				t.Fatalf("BuildRequest: unexpected err = %v", err)
			}
			if !bytes.Equal(got, viaBuildRequest) {
				t.Fatalf("BuildDumpRequest = % x, BuildRequest = % x: the two layouts have drifted", got, viaBuildRequest)
			}
		})
	}
}

// ---- FamilyHdrLen ------------------------------------------------------------

// TestFamilyHdrLen pins the family-header sizes. This switch is load-bearing
// twice over: BuildRequest validates against it, and pkg/nlparity slices the
// attribute stream at it, so a wrong value there shifts every attribute in a
// message and produces a plausible-looking parity report rather than an error.
//
// go test ./pkg/xtcpnl/ -run TestFamilyHdrLen
func TestFamilyHdrLen(t *testing.T) {
	tests := []struct {
		description string
		msgType     uint16
		want        int
	}{
		{"positive: RTM_GETLINK is struct ifinfomsg, 16", uint16(unix.RTM_GETLINK), IfInfomsgSizeCst},
		{"positive: RTM_NEWLINK is struct ifinfomsg, 16", uint16(unix.RTM_NEWLINK), IfInfomsgSizeCst},
		{"positive: RTM_SETLINK is struct ifinfomsg, 16", uint16(unix.RTM_SETLINK), IfInfomsgSizeCst},
		{"positive: RTM_GETADDR is struct ifaddrmsg, 8", uint16(unix.RTM_GETADDR), IfAddrmsgSizeCst},
		{"positive: RTM_NEWADDR is struct ifaddrmsg, 8", uint16(unix.RTM_NEWADDR), IfAddrmsgSizeCst},
		{"positive: RTM_GETROUTE is struct rtmsg, 12", uint16(unix.RTM_GETROUTE), RtMsgSizeCst},
		{"positive: RTM_NEWROUTE is struct rtmsg, 12", uint16(unix.RTM_NEWROUTE), RtMsgSizeCst},
		{"positive: RTM_GETNEIGH is struct ndmsg, 12", uint16(unix.RTM_GETNEIGH), NdMsgSizeCst},
		{"positive: RTM_NEWNEIGH is struct ndmsg, 12", uint16(unix.RTM_NEWNEIGH), NdMsgSizeCst},

		// Three families now answer 12, so this function's value alone can no
		// longer identify which struct a caller meant. That is fine for what
		// it is for — the length is all BuildRequest needs — but it is why
		// the rows are spelled out per message type rather than grouped.
		{"positive: RTM_GETRULE is struct fib_rule_hdr, 12", uint16(unix.RTM_GETRULE), FibRuleHdrSizeCst},
		{"positive: RTM_NEWRULE is struct fib_rule_hdr, 12", uint16(unix.RTM_NEWRULE), FibRuleHdrSizeCst},
		{"positive: RTM_DELRULE is struct fib_rule_hdr, 12", uint16(unix.RTM_DELRULE), FibRuleHdrSizeCst},

		// 0 and -1 are different answers: a zero-length family header is a real
		// thing in netlink and must not be confused with "no idea".
		{"boundary: NLMSG_DONE has no family header, 0", uint16(unix.NLMSG_DONE), 0},
		{"boundary: NLMSG_NOOP has no family header, 0", uint16(unix.NLMSG_NOOP), 0},
		{"boundary: NLMSG_ERROR has no family header, 0", uint16(unix.NLMSG_ERROR), 0},

		{"negative: RTM_GETQDISC is not modeled, -1", uint16(unix.RTM_GETQDISC), -1},
		{"negative: RTM_GETNSID is not modeled, -1", uint16(unix.RTM_GETNSID), -1},
		{"negative: RTM_NEWSTATS is not modeled, -1", uint16(unix.RTM_NEWSTATS), -1},
		{"negative: message type 0 is not modeled, -1", 0, -1},
		{"corner: 0xffff is not modeled, -1", 0xffff, -1},
	}

	for _, tc := range tests {
		t.Run(tc.description, func(t *testing.T) {
			if got := FamilyHdrLen(tc.msgType); got != tc.want {
				t.Errorf("FamilyHdrLen(%d) = %d, want %d", tc.msgType, got, tc.want)
			}
		})
	}
}

// TestRtextFilterConstants pins the two hand-declared kernel constants against
// the value the capture shows `ip link show` sending.
//
// They are hand-declared because golang.org/x/sys/unix v0.47.0 does not export
// them (linux/include/uapi/linux/rtnetlink.h:835,838). A wrong value here does
// not fail to compile — it produces a request the kernel happily answers with a
// different attribute set, which is exactly the class of bug the parity work
// exists to catch.
//
// go test ./pkg/xtcpnl/ -run TestRtextFilterConstants
func TestRtextFilterConstants(t *testing.T) {
	captured := capturedIPLinkShowRequest(t)
	attr := captured[capturedExtMaskOffCst:capturedGetLinkReqLenCst]

	tests := []struct {
		description string
		got         uint32
		want        uint32
	}{
		{"positive: RTEXT_FILTER_VF is bit 0", RTEXT_FILTER_VF, 1},
		{"positive: RTEXT_FILTER_SKIP_STATS is bit 3", RTEXT_FILTER_SKIP_STATS, 8},
		{
			description: "positive: their OR is the 0x09 the captured request carries",
			got:         RTEXT_FILTER_VF | RTEXT_FILTER_SKIP_STATS,
			want:        binary.LittleEndian.Uint32(attr[RTAttrSizeCst:]),
		},
		{
			description: "boundary: the captured attribute is IFLA_EXT_MASK with rta_len 8",
			got:         uint32(binary.LittleEndian.Uint16(attr[0:2]))<<16 | uint32(binary.LittleEndian.Uint16(attr[2:4])),
			want:        8<<16 | uint32(unix.IFLA_EXT_MASK),
		},
	}

	for _, tc := range tests {
		t.Run(tc.description, func(t *testing.T) {
			if tc.got != tc.want {
				t.Errorf("got %#x, want %#x", tc.got, tc.want)
			}
		})
	}
}

// ---- fuzz --------------------------------------------------------------------

// FuzzAttrBuilderRoundTrip encodes an arbitrary attribute and walks it back,
// asserting the type and value survive.
//
// The two decoders disagree on one thing by design: WalkRTAttrs masks
// NLA_F_NESTED and NLA_F_NET_BYTEORDER via NlaTypeMaskCst so a caller's switch
// still matches, while the encoder writes the type verbatim. The comparison is
// therefore against the masked type, and TestAttrBuilder's corner row is what
// pins the unmasked write.
//
// go test ./pkg/xtcpnl/ -run FuzzAttrBuilderRoundTrip -fuzz FuzzAttrBuilderRoundTrip
func FuzzAttrBuilderRoundTrip(f *testing.F) {
	f.Add(uint16(unix.IFLA_EXT_MASK), []byte{9, 0, 0, 0})
	f.Add(uint16(unix.IFLA_IFNAME), []byte("lo\x00"))
	f.Add(uint16(unix.NLA_F_NESTED)|uint16(unix.IFLA_LINKINFO), []byte{8, 0, 1, 0, 'v', 'e', 't', 'h'})
	f.Add(uint16(0), []byte{})

	f.Fuzz(func(t *testing.T, atype uint16, payload []byte) {
		// Size the buffer generously so the only reachable error is the rta_len
		// overflow, which keeps the round-trip assertion unconditional.
		ab := NewAttrBuilder(make([]byte, RTAttrSizeCst+len(payload)+4))

		err := ab.PutBytes(atype, payload)
		if len(payload) > RtaMaxPayloadCst {
			if !errors.Is(err, ErrAttrTooLong) {
				t.Fatalf("payload of %d bytes: err = %v, want %v", len(payload), err, ErrAttrTooLong)
			}
			return
		}
		if err != nil {
			t.Fatalf("PutBytes(%d, %d bytes): %v", atype, len(payload), err)
		}

		var seen int
		if err := WalkRTAttrs(ab.Bytes(), func(gotType uint16, val []byte) {
			seen++
			if want := atype & NlaTypeMaskCst; gotType != want {
				t.Errorf("rta_type = %#x, want %#x", gotType, want)
			}
			if !bytes.Equal(val, payload) {
				t.Errorf("value = % x, want % x", val, payload)
			}
		}); err != nil {
			t.Fatalf("WalkRTAttrs: %v", err)
		}
		if seen != 1 {
			t.Fatalf("walked %d attributes, want 1", seen)
		}
	})
}
