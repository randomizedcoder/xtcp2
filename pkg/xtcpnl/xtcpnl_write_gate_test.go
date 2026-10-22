package xtcpnl

import (
	"bytes"
	"encoding/binary"
	"errors"
	"testing"

	"golang.org/x/sys/unix"
)

// This file tests the write half of xtcpnl_rtattr_encode.go: the closed write
// gate (link-write-path.md Phase 4, table 1) and the encoder pieces the write
// builders need that the read path never did — nests, u16 and s32.

// admittedWriteTypes is the write gate's whole set. A type joins it only with
// an encoder and a parity target behind it. The address pair is the one
// exception in timing: its parity target is the captured link-write/addr
// corpus, and the plan admits it one task ahead of its encoder
// (addr-write-path-plan.md, A4-T01 then A5-T01). The neighbor pair joined
// with goip's neigh writes and flush (G3-T01), the rule pair with its writes,
// flush and restore (G3-T02), the nexthop pair with its writes and flush
// (G3-T03), the addrlabel pair with its writes and flush (G3-T04), and
// RTM_SETNEIGHTBL and RTM_SETSTATS with ntable change and stats set (G3-T05),
// the link property pair with link property add and del (G3-T06), and
// RTM_SETLINK with token set and del (G4-T03); `link set` still sends
// RTM_NEWLINK.
var admittedWriteTypes = map[uint16]bool{
	uint16(unix.RTM_NEWROUTE):     true,
	uint16(unix.RTM_DELROUTE):     true,
	uint16(unix.RTM_NEWLINK):      true,
	uint16(unix.RTM_DELLINK):      true,
	uint16(unix.RTM_NEWADDR):      true,
	uint16(unix.RTM_DELADDR):      true,
	uint16(unix.RTM_NEWNEIGH):     true,
	uint16(unix.RTM_DELNEIGH):     true,
	uint16(unix.RTM_NEWRULE):      true,
	uint16(unix.RTM_DELRULE):      true,
	uint16(unix.RTM_NEWNEXTHOP):   true,
	uint16(unix.RTM_DELNEXTHOP):   true,
	uint16(unix.RTM_NEWADDRLABEL): true,
	uint16(unix.RTM_DELADDRLABEL): true,
	uint16(unix.RTM_SETNEIGHTBL):  true,
	uint16(unix.RTM_SETSTATS):     true,
	uint16(unix.RTM_NEWLINKPROP):  true,
	uint16(unix.RTM_DELLINKPROP):  true,
	uint16(unix.RTM_SETLINK):      true,
}

// TestBuildWriteRequestAdmitsOnlyListedWrites walks every named rtnetlink type
// through BuildWriteRequest: the admitted writes build, every other write is
// ErrNotAnAdmittedWrite, and so is every GET, which belongs on BuildRequest.
// It complements TestBuildRequestRejectsEveryNamedWriteType, which is
// unchanged: the read gate still refuses every admitted write.
//
// go test ./pkg/xtcpnl/ -run TestBuildWriteRequestAdmitsOnlyListedWrites
func TestBuildWriteRequestAdmitsOnlyListedWrites(t *testing.T) {
	type row struct {
		description string
		msgType     uint16
		expected    error
	}

	tests := make([]row, 0, len(namedRtmWriteTypes)+len(namedRtmGetTypes)+3)
	for _, w := range namedRtmWriteTypes {
		switch {
		case admittedWriteTypes[w.msgType]:
			tests = append(tests, row{"positive: " + w.name + " is admitted", w.msgType, nil})
		default:
			tests = append(tests, row{"negative: " + w.name + " is not in the set", w.msgType, ErrNotAnAdmittedWrite})
		}
	}
	for _, g := range namedRtmGetTypes {
		tests = append(tests, row{"negative: " + g.name + " belongs on BuildRequest", g.msgType, ErrNotAnAdmittedWrite})
	}
	tests = append(tests,
		row{"corner: NLMSG_NOOP is not a write", uint16(unix.NLMSG_NOOP), ErrNotAnAdmittedWrite},
		row{"corner: NLMSG_ERROR is not a write", uint16(unix.NLMSG_ERROR), ErrNotAnAdmittedWrite},
		row{"boundary: message type 0 is not a write", 0, ErrNotAnAdmittedWrite},
	)

	admitted := 0
	for _, tc := range tests {
		t.Run(tc.description, func(t *testing.T) {
			var hdr []byte
			if n := FamilyHdrLen(tc.msgType); n > 0 {
				hdr = make([]byte, n)
			}
			got, err := BuildWriteRequest(tc.msgType, 0, testSeq, hdr, nil)
			if tc.expected != nil {
				if !errors.Is(err, tc.expected) {
					t.Fatalf("err = %v, want %v", err, tc.expected)
				}
				if got != nil {
					t.Errorf("request = % x, want nil on refusal", got)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected err = %v", err)
			}
			want := nlmsg(tc.msgType, uint16(unix.NLM_F_REQUEST), testSeq, hdr)
			if !bytes.Equal(got, want) {
				t.Errorf("request = % x\nwant      % x", got, want)
			}
		})
		if tc.expected == nil {
			admitted++
		}
	}
	if admitted != len(admittedWriteTypes) {
		t.Errorf("%d types admitted, want %d: namedRtmWriteTypes lost one of the set", admitted, len(admittedWriteTypes))
	}
}

// TestBuildWriteRequestLayout covers what the type walk above does not: the
// flags, the family header check and the attributes.
//
// go test ./pkg/xtcpnl/ -run TestBuildWriteRequestLayout
func TestBuildWriteRequestLayout(t *testing.T) {
	linkHdr := make([]byte, IfInfomsgSizeCst)
	linkHdr[0] = unix.AF_UNSPEC
	attrs := rtattr(uint16(unix.IFLA_IFNAME), []byte("bond0\x00"))
	const createExcl = uint16(unix.NLM_F_CREATE | unix.NLM_F_EXCL)
	const createExclAck = createExcl | uint16(unix.NLM_F_ACK)

	tests := []struct {
		description string
		msgType     uint16
		flags       uint16
		hdr         []byte
		attrs       []byte
		expected    []byte
		wantErr     error
	}{
		{
			description: "positive: `link add` flags (CREATE|EXCL|ACK, as rtnl_talk sends them) pass through with REQUEST",
			msgType:     uint16(unix.RTM_NEWLINK),
			flags:       createExclAck,
			hdr:         linkHdr,
			attrs:       attrs,
			expected:    nlmsg(uint16(unix.RTM_NEWLINK), uint16(unix.NLM_F_REQUEST)|createExclAck, testSeq, concat(linkHdr, attrs)),
		},
		{
			description: "boundary: 0x600 is CREATE|EXCL on a NEW and ATOMIC|MATCH on a GET; the gate ignores it",
			msgType:     uint16(unix.RTM_NEWLINK),
			flags:       uint16(unix.NLM_F_ATOMIC | unix.NLM_F_MATCH),
			hdr:         linkHdr,
			expected:    nlmsg(uint16(unix.RTM_NEWLINK), uint16(unix.NLM_F_REQUEST)|createExcl, testSeq, linkHdr),
		},
		{
			description: "boundary: NLM_F_REQUEST already set is not doubled",
			msgType:     uint16(unix.RTM_DELLINK),
			flags:       uint16(unix.NLM_F_REQUEST | unix.NLM_F_ACK),
			hdr:         linkHdr,
			expected:    nlmsg(uint16(unix.RTM_DELLINK), uint16(unix.NLM_F_REQUEST|unix.NLM_F_ACK), testSeq, linkHdr),
		},
		{
			description: "positive: a route write takes the 12-byte rtmsg",
			msgType:     uint16(unix.RTM_NEWROUTE),
			flags:       uint16(unix.NLM_F_CREATE | unix.NLM_F_REPLACE),
			hdr:         make([]byte, RtMsgSizeCst),
			expected:    nlmsg(uint16(unix.RTM_NEWROUTE), uint16(unix.NLM_F_REQUEST|unix.NLM_F_CREATE|unix.NLM_F_REPLACE), testSeq, make([]byte, RtMsgSizeCst)),
		},
		{
			description: "corner: a link write with a 12-byte header is refused, it would shift every attribute",
			msgType:     uint16(unix.RTM_NEWLINK),
			hdr:         make([]byte, 12),
			wantErr:     ErrBadFamilyHdr,
		},
		{
			description: "negative: a link write with no header is refused",
			msgType:     uint16(unix.RTM_DELLINK),
			wantErr:     ErrBadFamilyHdr,
		},
		{
			description: "negative: a route write with the 16-byte ifinfomsg is refused",
			msgType:     uint16(unix.RTM_DELROUTE),
			hdr:         linkHdr,
			wantErr:     ErrBadFamilyHdr,
		},
		{
			description: "positive: an address write takes the 8-byte ifaddrmsg",
			msgType:     uint16(unix.RTM_NEWADDR),
			hdr:         make([]byte, IfAddrmsgSizeCst),
			expected:    nlmsg(uint16(unix.RTM_NEWADDR), uint16(unix.NLM_F_REQUEST), testSeq, make([]byte, IfAddrmsgSizeCst)),
		},
		{
			description: "boundary: an address write with a 7-byte header is refused",
			msgType:     uint16(unix.RTM_NEWADDR),
			hdr:         make([]byte, IfAddrmsgSizeCst-1),
			wantErr:     ErrBadFamilyHdr,
		},
		{
			description: "negative: an address delete with the 16-byte ifinfomsg is refused",
			msgType:     uint16(unix.RTM_DELADDR),
			hdr:         linkHdr,
			wantErr:     ErrBadFamilyHdr,
		},
		{
			description: "corner: 0x300 (NLM_F_ROOT|NLM_F_MATCH on a GET) passes through an address write verbatim",
			msgType:     uint16(unix.RTM_NEWADDR),
			flags:       0x300,
			hdr:         make([]byte, IfAddrmsgSizeCst),
			expected:    nlmsg(uint16(unix.RTM_NEWADDR), uint16(unix.NLM_F_REQUEST)|0x300, testSeq, make([]byte, IfAddrmsgSizeCst)),
		},
		{
			description: "corner: the type is checked before the header, so a GET with a bad header is a gate refusal",
			msgType:     uint16(unix.RTM_GETLINK),
			hdr:         make([]byte, 3),
			wantErr:     ErrNotAnAdmittedWrite,
		},
	}

	for _, tc := range tests {
		t.Run(tc.description, func(t *testing.T) {
			got, err := BuildWriteRequest(tc.msgType, tc.flags, testSeq, tc.hdr, tc.attrs)
			if tc.wantErr != nil {
				if !errors.Is(err, tc.wantErr) || got != nil {
					t.Fatalf("got % x, %v; want nil, %v", got, err, tc.wantErr)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected err = %v", err)
			}
			if !bytes.Equal(got, tc.expected) {
				t.Errorf("request = % x\nwant      % x", got, tc.expected)
			}
		})
	}
}

// TestAttrBuilderWriteShapes is the encoder for the shapes the write builders
// add: u16, s32, and nests. Buffers are prefilled with 0xAA, as in
// TestAttrBuilder, so unzeroed padding and a write on failure both show.
//
// go test ./pkg/xtcpnl/ -run TestAttrBuilderWriteShapes
func TestAttrBuilderWriteShapes(t *testing.T) {
	const prefill = 0xAA
	const (
		linkinfo = uint16(unix.IFLA_LINKINFO)
		kind     = uint16(unix.IFLA_INFO_KIND)
		data     = uint16(unix.IFLA_INFO_DATA)
		mode     = uint16(unix.IFLA_BOND_MODE)
	)
	u16 := func(v uint16) []byte { b := make([]byte, 2); binary.LittleEndian.PutUint16(b, v); return b }
	nest := func(a *AttrBuilder, atype uint16, inner func(*AttrBuilder) error) error {
		m, err := a.BeginNest(atype)
		if err != nil {
			return err
		}
		if err := inner(a); err != nil {
			return err
		}
		return a.EndNest(m)
	}
	none := func(*AttrBuilder) error { return nil }

	tests := []struct {
		description string
		bufLen      int
		put         func(a *AttrBuilder) error
		expected    []byte // a.Bytes() after put; on error, what is left
		wantErr     error
	}{
		{
			description: "positive: PutU16 is rta_len 6 with two zeroed padding bytes",
			bufLen:      16,
			put:         func(a *AttrBuilder) error { return a.PutU16(7, 0xbeef) },
			expected:    []byte{6, 0, 7, 0, 0xef, 0xbe, 0, 0},
		},
		{
			description: "boundary: PutU16 of 0xffff",
			bufLen:      8,
			put:         func(a *AttrBuilder) error { return a.PutU16(7, 0xffff) },
			expected:    rtattr(7, u16(0xffff)),
		},
		{
			description: "negative: PutU16 with 7 bytes left writes nothing",
			bufLen:      7,
			put:         func(a *AttrBuilder) error { return a.PutU16(7, 1) },
			expected:    []byte{},
			wantErr:     ErrAttrNoSpace,
		},
		{
			description: "positive: PutS32 of -1 is the all-ones word",
			bufLen:      8,
			put:         func(a *AttrBuilder) error { return a.PutS32(9, -1) },
			expected:    rtattr(9, []byte{0xff, 0xff, 0xff, 0xff}),
		},
		{
			description: "boundary: PutS32 of the minimum int32",
			bufLen:      8,
			put:         func(a *AttrBuilder) error { return a.PutS32(9, -1<<31) },
			expected:    rtattr(9, le32(0x80000000)),
		},
		{
			description: "boundary: an empty nest is a bare 4-byte header, rta_len 4",
			bufLen:      8,
			put:         func(a *AttrBuilder) error { return nest(a, data, none) },
			expected:    rtattr(data, nil),
		},
		{
			description: "positive: rta_len covers the inner padding: KIND \"bond\" is 9 bytes padded to 12",
			bufLen:      32,
			put: func(a *AttrBuilder) error {
				return nest(a, linkinfo, func(a *AttrBuilder) error { return a.PutString(kind, "bond") })
			},
			expected: rtattr(linkinfo, rtattr(kind, []byte("bond\x00"))),
		},
		{
			description: "positive: the `type bond mode 802.3ad` nest within a nest, closed innermost first",
			bufLen:      64,
			put: func(a *AttrBuilder) error {
				return nest(a, linkinfo, func(a *AttrBuilder) error {
					if err := a.PutString(kind, "bond"); err != nil {
						return err
					}
					return nest(a, data, func(a *AttrBuilder) error { return a.PutU8(mode, 4) })
				})
			},
			expected: rtattr(linkinfo, concat(rtattr(kind, []byte("bond\x00")), rtattr(data, rtattr(mode, []byte{4})))),
		},
		{
			description: "corner: the nest type is written as given, NLA_F_NESTED is not added",
			bufLen:      8,
			put:         func(a *AttrBuilder) error { return nest(a, linkinfo, none) },
			expected:    []byte{4, 0, byte(linkinfo), 0},
		},
		{
			description: "negative: BeginNest with 3 bytes left writes nothing",
			bufLen:      3,
			put:         func(a *AttrBuilder) error { return nest(a, data, none) },
			expected:    []byte{},
			wantErr:     ErrAttrNoSpace,
		},
		{
			description: "negative: EndNest with a mark from before a Reset is refused",
			bufLen:      16,
			put: func(a *AttrBuilder) error {
				if err := a.PutU32(1, 1); err != nil {
					return err
				}
				m, err := a.BeginNest(data)
				if err != nil {
					return err
				}
				a.Reset()
				return a.EndNest(m)
			},
			expected: []byte{},
			wantErr:  ErrNestNotOpen,
		},
		{
			description: "boundary: the largest nest rta_len can hold, 0xfffc after alignment, closes",
			bufLen:      0x10000,
			put: func(a *AttrBuilder) error {
				return nest(a, data, func(a *AttrBuilder) error { return a.PutBytes(1, make([]byte, 0xfffc-8)) })
			},
			expected: rtattr(data, rtattr(1, make([]byte, 0xfffc-8))),
		},
		{
			description: "boundary: one word more overflows rta_len; the whole nest is removed, the attr before it kept",
			bufLen:      0x10010,
			put: func(a *AttrBuilder) error {
				if err := a.PutU32(2, 5); err != nil {
					return err
				}
				return nest(a, data, func(a *AttrBuilder) error { return a.PutBytes(1, make([]byte, 0xfffc-4)) })
			},
			expected: rtattr(2, le32(5)),
			wantErr:  ErrAttrTooLong,
		},
	}

	for _, tc := range tests {
		t.Run(tc.description, func(t *testing.T) {
			buf := bytes.Repeat([]byte{prefill}, tc.bufLen)
			ab := NewAttrBuilder(buf)
			err := tc.put(&ab)
			if !errors.Is(err, tc.wantErr) {
				t.Fatalf("err = %v, want %v", err, tc.wantErr)
			}
			if got := ab.Bytes(); !bytes.Equal(got, tc.expected) {
				if len(got) > 64 || len(tc.expected) > 64 {
					t.Fatalf("Bytes() is %d bytes, want %d", len(got), len(tc.expected))
				}
				t.Fatalf("Bytes() = % x\nwant      % x", got, tc.expected)
			}
		})
	}
}
