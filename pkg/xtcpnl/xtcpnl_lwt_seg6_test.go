package xtcpnl

import (
	"encoding/hex"
	"errors"
	"strings"
	"testing"
)

// The test addresses: S1 to S3 are 2001:db8:9::1 to ::3, the forms' segments.
const (
	seg6S1Hex  = "20010db8000900000000000000000001"
	seg6S2Hex  = "20010db8000900000000000000000002"
	seg6S3Hex  = "20010db8000900000000000000000003"
	seg6ZeroSl = "00000000000000000000000000000000"
)

func seg6Addr(t *testing.T, h string) [16]byte {
	t.Helper()
	b, err := hex.DecodeString(h)
	if err != nil || len(b) != 16 {
		t.Fatalf("seg6Addr(%q): %v", h, err)
	}
	return [16]byte(b)
}

// TestSRH is parse_srh: the header, the slots written last to first, and
// the HMAC TLV. The goldens are hand-built from include/uapi/linux/seg6.h.
//
// go test ./pkg/xtcpnl/ -run TestSRH
func TestSRH(t *testing.T) {
	s1, s2, s3 := seg6Addr(t, seg6S1Hex), seg6Addr(t, seg6S2Hex), seg6Addr(t, seg6S3Hex)
	tests := []struct {
		description string
		nsegs       int
		segs        [][16]byte
		hmac        uint32
		expected    string
		err         error
	}{
		{description: "positive: one segment", nsegs: 1, segs: [][16]byte{s1},
			expected: "0002040000000000" + seg6S1Hex},
		{description: "positive: three segments, the first in argv last", nsegs: 3, segs: [][16]byte{s1, s2, s3},
			expected: "0006040202000000" + seg6S3Hex + seg6S2Hex + seg6S1Hex},
		{description: "positive: an hmac key sets the flag and appends the TLV", nsegs: 1, segs: [][16]byte{s1}, hmac: 7,
			expected: "0007040000080000" + seg6S1Hex + "0526000000000007" + strings.Repeat("00", 32)},
		{description: "negative: no slots", nsegs: 0, err: ErrSRH},
		{description: "negative: more addresses than slots", nsegs: 1, segs: [][16]byte{s1, s2}, err: ErrSRH},
		{description: "boundary: 127 slots, hdrlen 254", nsegs: 127,
			expected: "00fe047e7e000000" + strings.Repeat(seg6ZeroSl, 127)},
		{description: "boundary: 125 slots and an hmac, hdrlen 255", nsegs: 125, hmac: 1,
			expected: "00ff047c7c080000" + strings.Repeat(seg6ZeroSl, 125) + "0526000000000001" + strings.Repeat("00", 32)},
		{description: "boundary: 128 slots, past hdrlen's u8", nsegs: 128, err: ErrSRH},
		{description: "corner: inline's extra slot is slot 0, zero", nsegs: 2, segs: [][16]byte{s1},
			expected: "0004040101000000" + seg6ZeroSl + seg6S1Hex},
		{description: "corner: an empty field between commas is a zero slot", nsegs: 3, segs: [][16]byte{s1, s3},
			expected: "0006040202000000" + seg6ZeroSl + seg6S3Hex + seg6S1Hex},
		{description: "corner: no segments, mode -1's one zero slot", nsegs: 1,
			expected: "0002040000000000" + seg6ZeroSl},
	}
	for i := range tests {
		tt := &tests[i]
		t.Run(tt.description, func(t *testing.T) {
			b, err := SRH(tt.nsegs, tt.segs, tt.hmac)
			if !errors.Is(err, tt.err) {
				t.Fatalf("err = %v, want %v", err, tt.err)
			}
			if got := hex.EncodeToString(b); got != tt.expected {
				t.Errorf("got  %s\nwant %s", got, tt.expected)
			}
		})
	}
}

// TestPutSeg6Encap is `encap seg6` and `encap seg6local` through
// PutLwtEncap, into NHA_ENCAP 8|NLA_F_NESTED ("0880") and NHA_ENCAP_TYPE 7.
// The goldens are hand-built from seg6_iptunnel.h and seg6_local.h.
//
// go test ./pkg/xtcpnl/ -run TestPutSeg6Encap
func TestPutSeg6Encap(t *testing.T) {
	s1, s2, s3 := seg6Addr(t, seg6S1Hex), seg6Addr(t, seg6S2Hex), seg6Addr(t, seg6S3Hex)
	srh := func(nsegs int, hmac uint32, segs ...[16]byte) []byte {
		b, err := SRH(nsegs, segs, hmac)
		if err != nil {
			t.Fatal(err)
		}
		return b
	}
	seg6 := func(mode int32, srh, src []byte) LwtEncap {
		return LwtEncap{Seg6: &Seg6Encap{Mode: mode, SRH: srh, TunSrc: src}}
	}
	local := func(srh []byte, items ...Seg6LocalItem) LwtEncap {
		return LwtEncap{Seg6Local: &Seg6LocalEncap{Items: items, SRH: srh}}
	}
	action := func(a uint32) Seg6LocalItem { return Seg6LocalItem{Kind: Seg6LocalAction, U32: a} }
	nh6, _ := hex.DecodeString("20010db8000100000000000000000001")
	tunsrc, _ := hex.DecodeString("20010db8000100000000000000000010")
	const (
		seg6Type  = "0600070005000000"
		localType = "0600070007000000"
		cnt0      = "0000000000000000"
	)
	srh1 := "20000100" + "01000000" + "0002040000000000" + seg6S1Hex
	tests := []struct {
		description string
		e           LwtEncap
		buf         int
		expected    string
		err         error
	}{
		{description: "positive: one segment, encap mode", e: seg6(Seg6ModeEncap, srh(1, 0, s1), nil),
			expected: "24000880" + srh1 + seg6Type},
		{description: "positive: three segments, last to first", e: seg6(Seg6ModeEncap, srh(3, 0, s1, s2, s3), nil),
			expected: "44000880" + "40000100" + "01000000" + "0006040202000000" + seg6S3Hex + seg6S2Hex + seg6S1Hex + seg6Type},
		{description: "positive: inline adds a zero segment", e: seg6(Seg6ModeInline, srh(2, 0, s1), nil),
			expected: "34000880" + "30000100" + "00000000" + "0004040101000000" + seg6ZeroSl + seg6S1Hex + seg6Type},
		{description: "positive: hmac, the flag and the TLV", e: seg6(Seg6ModeEncap, srh(1, 7, s1), nil),
			expected: "4c000880" + "48000100" + "01000000" + "0007040000080000" + seg6S1Hex +
				"0526000000000007" + strings.Repeat("00", 32) + seg6Type},
		{description: "positive: tunsrc after the SRH", e: seg6(Seg6ModeEncap, srh(1, 0, s1), tunsrc),
			expected: "38000880" + srh1 + "14000200" + hex.EncodeToString(tunsrc) + seg6Type},
		{description: "positive: seg6local's items in argv order",
			e: local(nil, action(2), Seg6LocalItem{Kind: Seg6LocalNH6, Addr: nh6},
				Seg6LocalItem{Kind: Seg6LocalOIF, U32: 3}, Seg6LocalItem{Kind: Seg6LocalCounters}),
			expected: "50000880" + "0800010002000000" + "14000500" + hex.EncodeToString(nh6) + "0800070003000000" +
				"28000a80" + "0c000200" + cnt0 + "0c000300" + cnt0 + "0c000400" + cnt0 + localType},
		{description: "positive: next-csid with both lengths",
			e: local(nil, action(1), Seg6LocalItem{Kind: Seg6LocalFlavors, Flavors: Seg6LocalFlavorSet{
				Ops: 1 << Seg6LocalFlvOpNextCSID, LcblockBits: 32, LcnodeFnBits: 16, HasLcblock: true, HasLcnodeFn: true}}),
			expected: "28000880" + "0800010001000000" + "1c000b80" + "0800010010000000" + "0500020020000000" + "0500030010000000" + localType},
		{description: "negative: no tunsrc, no SRC", e: seg6(Seg6ModeL2Encap, srh(1, 0, s1), nil),
			expected: "24000880" + "20000100" + "02000000" + "0002040000000000" + seg6S1Hex + seg6Type},
		{description: "negative: a tunsrc of four bytes", e: seg6(Seg6ModeEncap, srh(1, 0, s1), []byte{192, 0, 2, 1}), err: ErrSeg6Encap},
		{description: "negative: no SRH", e: seg6(Seg6ModeEncap, nil, nil), err: ErrSeg6Encap},
		{description: "negative: an nh4 of 16 bytes", e: local(nil, action(6), Seg6LocalItem{Kind: Seg6LocalNH4, Addr: nh6}), err: ErrSeg6Encap},
		{description: "negative: a seg6local item of no known kind", e: local(nil, Seg6LocalItem{Kind: 8}), err: ErrSeg6Encap},
		{description: "negative: seg6 and seg6local both", e: LwtEncap{Seg6: &Seg6Encap{SRH: srh(1, 0, s1)}, Seg6Local: &Seg6LocalEncap{}},
			err: ErrLwtEncap},
		{description: "boundary: mode -1, one zero segment", e: seg6(-1, srh(1, 0), nil),
			expected: "24000880" + "20000100" + "ffffffff" + "0002040000000000" + seg6ZeroSl + seg6Type},
		{description: "boundary: the builder one byte short, nothing written", e: seg6(Seg6ModeEncap, srh(1, 0, s1), nil),
			buf: 0x24 + 8 - 1, err: ErrAttrNoSpace},
		{description: "corner: End.B6's SRH has the extra zero segment, last",
			e: local(srh(4, 0, s1, s2, s3), action(9)),
			expected: "58000880" + "0800010009000000" + "4c000200" + "0008040303000000" + seg6ZeroSl +
				seg6S3Hex + seg6S2Hex + seg6S1Hex + localType},
		{description: "corner: End.B6.Encaps' SRH has none",
			e:        local(srh(1, 0, s1), action(10)),
			expected: "28000880" + "080001000a000000" + "1c000200" + "0002040000000000" + seg6S1Hex + localType},
		{description: "corner: next-csid with neither length",
			e:        local(nil, action(1), Seg6LocalItem{Kind: Seg6LocalFlavors, Flavors: Seg6LocalFlavorSet{Ops: 1 << Seg6LocalFlvOpNextCSID}}),
			expected: "18000880" + "0800010001000000" + "0c000b80" + "0800010010000000" + localType},
	}
	for i := range tests {
		tt := &tests[i]
		t.Run(tt.description, func(t *testing.T) {
			buf := tt.buf
			if buf == 0 {
				buf = 512
			}
			ab := NewAttrBuilder(make([]byte, buf))
			err := PutLwtEncap(&ab, NhaEncap, NhaEncapType, tt.e)
			if tt.err != nil {
				if !errors.Is(err, tt.err) || ab.Len() != 0 {
					t.Fatalf("err = %v with %d bytes written, want %v and none", err, ab.Len(), tt.err)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if got := hex.EncodeToString(ab.Bytes()); got != tt.expected {
				t.Errorf("got  %s\nwant %s", got, tt.expected)
			}
		})
	}
}
