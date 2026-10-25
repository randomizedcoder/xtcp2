package xtcpnl

import "testing"

func TestLLProtoN2A(t *testing.T) {
	tests := []struct {
		description string
		id          uint16
		expected    string
	}{
		{"positive: a named protocol", 0x88a8, "802.1ad"},
		{"positive: the last row", 0x88cc, "LLDP"},
		{"boundary: id 0, which has no name", 0, "[0]"},
		{"corner: 0x0800's first name, not ipv4", 0x0800, "ip"},
		{"corner: 0x8100, listed twice", 0x8100, "802.1Q"},
		{"negative: an unnamed id is proto_n2a's [%d]", 0x1234, "[4660]"},
	}
	for _, tc := range tests {
		t.Run(tc.description, func(t *testing.T) {
			if got := LLProtoN2A(tc.id); got != tc.expected {
				t.Errorf("LLProtoN2A(%#x) = %q, want %q", tc.id, got, tc.expected)
			}
		})
	}
}
