package goip

import (
	"testing"

	"golang.org/x/sys/unix"
)

// TestProtoNames checks getprotobyname and getprotobynumber over the
// parity guest's /etc/protocols, iana-etc's: a name or its alias, each
// number's one name, and the names and numbers it does not have.
//
// go test ./internal/goip/ -run TestProtoNames
func TestProtoNames(t *testing.T) {
	tests := []struct {
		description string
		name        string
		number      uint8
		byName      bool   // name is getprotobyname's
		expected    string // getprotobynumber(number)'s name, "" for none
	}{
		{"positive: tcp", "tcp", unix.IPPROTO_TCP, true, "tcp"},
		{"positive: ipv6-icmp, iana-etc's spelling", "ipv6-icmp", unix.IPPROTO_ICMPV6, true, "ipv6-icmp"},
		{"positive: ospfigp, 89", "ospfigp", 89, true, "ospfigp"},
		{"positive: TCP, the alias, by name only", "TCP", unix.IPPROTO_TCP, true, "tcp"},
		{"boundary: hopopt, protocol 0", "hopopt", 0, true, "hopopt"},
		{"boundary: bit-emu, 147, the highest named", "bit-emu", 147, true, "bit-emu"},
		{"boundary: 148, the first unnamed past it", "", 148, false, ""},
		{"negative: ospf, not a name", "ospf", 89, false, "ospfigp"},
		{"negative: mobility-header, 135, has no line", "mobility-header", unix.IPPROTO_MH, false, ""},
		{"negative: 255, reserved, no line", "", 255, false, ""},
		{"corner: Tcp, names are case-sensitive", "Tcp", unix.IPPROTO_TCP, false, "tcp"},
		{"corner: 3pc, a name starting with a digit", "3pc", 34, true, "3pc"},
	}
	for _, tc := range tests {
		t.Run(tc.description, func(t *testing.T) {
			if tc.name != "" {
				p, ok := protoByName(tc.name)
				if ok != tc.byName || ok && p != tc.number {
					t.Errorf("protoByName(%q) = %d, %v; want %d, %v", tc.name, p, ok, tc.number, tc.byName)
				}
			}
			name, ok := protoByNumber(tc.number)
			if ok != (tc.expected != "") || name != tc.expected {
				t.Errorf("protoByNumber(%d) = %q, %v; want %q", tc.number, name, ok, tc.expected)
			}
		})
	}
}
