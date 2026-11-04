package xtcpnl

import (
	"encoding/hex"
	"errors"
	"testing"
)

// TestPutLwtEncapOther is `encap ila`, `xfrm`, `ioam6` and `rpl` through
// PutLwtEncap, into NHA_ENCAP 8|NLA_F_NESTED ("0880") and NHA_ENCAP_TYPE 7.
// The goldens are hand-built from ila.h, lwtunnel.h, ioam6.h,
// ioam6_iptunnel.h and rpl.h.
//
// go test ./pkg/xtcpnl/ -run TestPutLwtEncapOther
func TestPutLwtEncapOther(t *testing.T) {
	s1, s2 := seg6Addr(t, seg6S1Hex), seg6Addr(t, seg6S2Hex)
	src, _ := hex.DecodeString("20010db8000100000000000000000010")
	loc := [8]byte{0x20, 0x01, 0, 0, 0, 0, 0, 0x02}
	ila := func(items ...IlaItem) LwtEncap { return LwtEncap{ILA: &IlaEncap{Locator: loc, Items: items}} }
	inline := func(size uint16, typ uint32) *Ioam6Encap {
		return &Ioam6Encap{FreqK: 1, FreqN: 1, Mode: Ioam6ModeInline, TraceType: typ, TraceNS: 1, TraceSize: size}
	}
	const (
		ilaType   = "0600070003000000"
		xfrmType  = "060007000a000000"
		ioam6Type = "0600070009000000"
		rplType   = "0600070008000000"
		locator   = "0c000100" + "2001000000000002"
		freq11    = "0800040001000000" + "0800050001000000"
		modeIn    = "0500010001000000"
	)
	tests := []struct {
		description string
		e           LwtEncap
		buf         int
		expected    string
		err         error
	}{
		{description: "positive: ila's locator, groups in network order", e: ila(),
			expected: "10000880" + locator + ilaType},
		{description: "positive: ila's u8s in argv order, hook-type first",
			e:        ila(IlaItem{Kind: IlaAttrHookType, U8: 0}, IlaItem{Kind: IlaAttrCsumMode, U8: 2}),
			expected: "20000880" + locator + "0500090000000000" + "0500070002000000" + ilaType},
		{description: "positive: xfrm's if_id and link",
			e:        LwtEncap{Xfrm: &XfrmEncap{Items: []XfrmItem{{Kind: LwtXfrmIfID, U32: 1}, {Kind: LwtXfrmLink, U32: 3}}}},
			expected: "14000880" + "0800010001000000" + "0800020003000000" + xfrmType},
		{description: "positive: ioam6 inline, the trace alone", e: LwtEncap{Ioam6: inline(12, 0x800000)},
			expected: "28000880" + freq11 + modeIn + "0c000300" + "0001000380000000" + ioam6Type},
		{description: "positive: ioam6 encap, SRC before DST, both before TRACE",
			e: LwtEncap{Ioam6: &Ioam6Encap{FreqK: 1, FreqN: 2, Mode: Ioam6ModeEncap, TunSrc: src, TunDst: s1[:],
				TraceType: 0x800000, TraceNS: 1, TraceSize: 12}},
			expected: "50000880" + "0800040001000000" + "0800050002000000" + "0500010002000000" +
				"14000600" + hex.EncodeToString(src) + "14000200" + seg6S1Hex + "0c000300" + "0001000380000000" + ioam6Type},
		{description: "positive: rpl's two segments, last to first",
			e:        LwtEncap{Rpl: &RplEncap{NSegs: 2, Segs: [][16]byte{s1, s2}}},
			expected: "30000880" + "2c000100" + "0004030200000000" + seg6S2Hex + seg6S1Hex + rplType},
		{description: "negative: ioam6 inline drops SRC and DST",
			e: LwtEncap{Ioam6: &Ioam6Encap{FreqK: 1, FreqN: 1, Mode: Ioam6ModeInline, TunSrc: src, TunDst: s1[:],
				TraceType: 0x800000, TraceNS: 1, TraceSize: 12}},
			expected: "28000880" + freq11 + modeIn + "0c000300" + "0001000380000000" + ioam6Type},
		{description: "negative: an ila item of no known kind", e: ila(IlaItem{Kind: 5}), err: ErrLwtEncapOther},
		{description: "negative: an xfrm item of no known kind", e: LwtEncap{Xfrm: &XfrmEncap{Items: []XfrmItem{{Kind: 3}}}},
			err: ErrLwtEncapOther},
		{description: "negative: an ioam6 tundst of four bytes",
			e:   LwtEncap{Ioam6: &Ioam6Encap{Mode: Ioam6ModeEncap, TunDst: []byte{192, 0, 2, 1}}},
			err: ErrLwtEncapOther},
		{description: "negative: rpl with no slots", e: LwtEncap{Rpl: &RplEncap{}}, err: ErrLwtEncapOther},
		{description: "negative: ila and xfrm both", e: LwtEncap{ILA: &IlaEncap{}, Xfrm: &XfrmEncap{}}, err: ErrLwtEncap},
		{description: "boundary: ioam6's largest size, 244, is remlen 61", e: LwtEncap{Ioam6: inline(244, 0x800000)},
			expected: "28000880" + freq11 + modeIn + "0c000300" + "0001003d80000000" + ioam6Type},
		{description: "boundary: the builder one byte short, nothing written", e: ila(), buf: 0x10 + 8 - 1, err: ErrAttrNoSpace},
		{description: "corner: ioam6's 24-bit type, shifted up a byte", e: LwtEncap{Ioam6: inline(12, 0xabcdef)},
			expected: "28000880" + freq11 + modeIn + "0c000300" + "00010003abcdef00" + ioam6Type},
		{description: "corner: an ila keyword twice is written twice",
			e:        ila(IlaItem{Kind: IlaAttrIdentType, U8: 1}, IlaItem{Kind: IlaAttrIdentType, U8: 1}),
			expected: "20000880" + locator + "0500080001000000" + "0500080001000000" + ilaType},
		{description: "corner: rpl's empty field between commas is a zero slot",
			e:        LwtEncap{Rpl: &RplEncap{NSegs: 3, Segs: [][16]byte{s1, s2}}},
			expected: "40000880" + "3c000100" + "0006030300000000" + seg6ZeroSl + seg6S2Hex + seg6S1Hex + rplType},
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
