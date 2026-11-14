//go:build goip_write

package goip

import "testing"

// TestIlaAddr64 is get_addr64: up to four hex words of at most 0xffff.
//
// go test ./internal/goip/ -run TestIlaAddr64
func TestIlaAddr64(t *testing.T) {
	tests := []struct {
		description string
		in          string
		expected    uint64
		ok          bool
	}{
		{description: "positive: four words", in: "2001:db8:1:2", expected: 0x20010db800010002, ok: true},
		{description: "boundary: every word 0xffff", in: "ffff:ffff:ffff:ffff", expected: 0xffffffffffffffff, ok: true},
		{description: "corner: one word, the rest 0", in: "1", expected: 0x0001000000000000, ok: true},
		{description: "corner: 0x, as strtoul base 16 takes it", in: "0x10:0:0:0", expected: 0x0010000000000000, ok: true},
		{description: "corner: leading space and -0, as strtoul takes them", in: " 1:-0:2:3", expected: 0x0001000000020003,
			ok: true},
		{description: "boundary: a word of 0x10000", in: "10000"},
		{description: "negative: a fifth word", in: "1:2:3:4:5"},
		{description: "negative: what follows the last word", in: "1:2:3:4x"},
		{description: "negative: empty", in: ""},
		{description: "negative: an empty word", in: "1::2"},
		{description: "negative: a trailing colon", in: "1:"},
		{description: "negative: -1, ULONG_MAX", in: "-1"},
	}
	for _, tc := range tests {
		t.Run(tc.description, func(t *testing.T) {
			got, ok := ilaAddr64(tc.in)
			if ok != tc.ok || got != tc.expected {
				t.Errorf("ilaAddr64(%q) = %#x, %v, want %#x, %v", tc.in, got, ok, tc.expected, tc.ok)
			}
		})
	}
}
