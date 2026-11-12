package render

import (
	"encoding/binary"
	"testing"

	"golang.org/x/sys/unix"
)

// TestAddrStringMpls is addrString over AF_MPLS bytes: mpls_ntop1's labels
// up to the bottom-of-stack bit (lib/mpls_ntop.c).
//
// go test ./internal/goip/render/ -run TestAddrStringMpls
func TestAddrStringMpls(t *testing.T) {
	// entry is one label stack entry: the label << 12, BoS at 0x100, and a
	// TTL in the low byte, which mpls_ntop1 ignores.
	entry := func(label uint32, bos bool) []byte {
		e := label<<12 | 0x40
		if bos {
			e |= 1 << 8
		}
		return binary.BigEndian.AppendUint32(nil, e)
	}
	cat := func(e ...[]byte) []byte {
		var b []byte
		for _, x := range e {
			b = append(b, x...)
		}
		return b
	}
	tests := []struct {
		description string
		in          []byte
		expected    string
	}{
		{description: "positive: one label", in: entry(100, true), expected: "100"},
		{description: "positive: a stack, BoS on the last", in: cat(entry(200, false), entry(300, true)), expected: "200/300"},
		{description: "boundary: the largest label", in: entry(1048575, true), expected: "1048575"},
		{description: "boundary: label 0", in: entry(0, true), expected: "0"},
		{description: "corner: an entry after the bottom of the stack is not printed", in: cat(entry(100, true), entry(200, true)), expected: "100"},
		{description: "corner: a stack with no BoS stops at the end of the bytes", in: cat(entry(100, false), entry(200, false)), expected: "100/200"},
		{description: "negative: a length that is not whole entries is hex", in: []byte{0, 6, 0x41}, expected: "000641"},
		{description: "negative: no bytes", in: nil, expected: ""},
	}
	for _, tc := range tests {
		t.Run(tc.description, func(t *testing.T) {
			if got := addrString(tc.in, unix.AF_MPLS); got != tc.expected {
				t.Errorf("addrString(%x) = %q, want %q", tc.in, got, tc.expected)
			}
		})
	}
}
