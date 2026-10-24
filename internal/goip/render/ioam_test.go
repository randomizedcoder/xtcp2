package render

import "testing"

// TestCAltHex checks C's %#0<width>x: the prefix counted in the width, and
// zero with no prefix.
//
// go test ./internal/goip/render/ -run TestCAltHex
func TestCAltHex(t *testing.T) {
	tests := []struct {
		description string
		v           uint64
		width       int
		expected    string
	}{
		{description: "positive: %#010x of 0x2a pads to ten with the prefix", v: 0x2a, width: 10, expected: "0x0000002a"},
		{description: "positive: %#08x of a trace type that fills it", v: 0x80000000, width: 8, expected: "0x80000000"},
		{description: "boundary: %#08x of 0xc00000, eight with the prefix", v: 0xc00000, width: 8, expected: "0xc00000"},
		{description: "boundary: zero is width zeros, no prefix", v: 0, width: 10, expected: "0000000000"},
		{description: "boundary: %#018x of all ones overflows nothing", v: 1<<64 - 1, width: 18, expected: "0xffffffffffffffff"},
		{description: "corner: wider than the width is not cut", v: 0x123456789, width: 8, expected: "0x123456789"},
		{description: "corner: zero at width 0 is still one 0", v: 0, width: 0, expected: "0"},
	}
	for _, tc := range tests {
		t.Run(tc.description, func(t *testing.T) {
			if got := CAltHex(tc.v, tc.width); got != tc.expected {
				t.Errorf("CAltHex(%#x, %d) = %q, want %q", tc.v, tc.width, got, tc.expected)
			}
		})
	}
}
