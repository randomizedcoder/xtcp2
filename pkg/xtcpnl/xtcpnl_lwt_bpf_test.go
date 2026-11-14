package xtcpnl

import (
	"encoding/hex"
	"errors"
	"testing"
)

// TestPutBpfEncap is parse_encap_bpf's request as a nexthop's NHA_ENCAP:
// each hook's nest (NLA_F_NESTED) holds LWT_BPF_PROG_FD then
// LWT_BPF_PROG_NAME with its NUL, in argv order with the headroom.
func TestPutBpfEncap(t *testing.T) {
	const bpfType = "0600070006000000"
	name := hex.EncodeToString([]byte("lwt_bpf.o:[lwt_in]")) + "0000" // the NUL and a pad byte
	prog := func(kind uint16, fd uint32) BpfEncapItem {
		return BpfEncapItem{Kind: kind, Prog: BpfProgRef{FD: fd, Name: "lwt_bpf.o:[lwt_in]"}}
	}
	enc := func(items ...BpfEncapItem) LwtEncap { return LwtEncap{Bpf: &BpfEncap{Items: items}} }
	tests := []struct {
		description string
		e           LwtEncap
		buf         int
		expected    string
		err         error
	}{
		{description: "positive: in, then a headroom", e: enc(prog(LwtBpfIn, 7), BpfEncapItem{Kind: LwtBpfXmitHeadroom, Headroom: 16}),
			expected: "30000880" + "24000180" + "0800010007000000" + "17000200" + name + "0800040010000000" + bpfType},
		{description: "positive: a hook twice is two nests", e: enc(prog(LwtBpfIn, 7), prog(LwtBpfIn, 8)),
			expected: "4c000880" + "24000180" + "0800010007000000" + "17000200" + name +
				"24000180" + "0800010008000000" + "17000200" + name + bpfType},
		{description: "positive: out and xmit nests", e: enc(prog(LwtBpfOut, 1), prog(LwtBpfXmit, 2)),
			expected: "4c000880" + "24000280" + "0800010001000000" + "17000200" + name +
				"24000380" + "0800010002000000" + "17000200" + name + bpfType},
		{description: "boundary: an empty name is its NUL alone", e: enc(BpfEncapItem{Kind: LwtBpfIn}),
			expected: "18000880" + "14000180" + "0800010000000000" + "05000200" + "00000000" + bpfType},
		{description: "corner: no items is an empty encap, the kernel's to refuse", e: enc(),
			expected: "04000880" + bpfType},
		{description: "negative: an item of no known kind", e: enc(BpfEncapItem{Kind: 9}), err: ErrBpfEncap},
		{description: "negative: bpf and rpl both", e: LwtEncap{Bpf: &BpfEncap{}, Rpl: &RplEncap{}}, err: ErrLwtEncap},
		{description: "boundary: the builder one byte short, nothing written", e: enc(prog(LwtBpfIn, 7)),
			buf: 0x28 + 8 - 1, err: ErrAttrNoSpace},
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
