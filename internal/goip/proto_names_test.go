package goip

import (
	"testing"

	"golang.org/x/sys/unix"
)

// TestProtoNames checks getprotobyname and getprotobynumber over goip's
// names, as iana-etc spells them: fou's and rule's, each number's one
// name, and the names and numbers it does not know.
//
// go test ./internal/goip/ -run TestProtoNames
func TestProtoNames(t *testing.T) {
	tests := []struct {
		description string
		name        string
		number      uint8
		expected    bool
	}{
		{"positive: tcp, rule's", "tcp", unix.IPPROTO_TCP, true},
		{"positive: ipv6-icmp, iana-etc's spelling", "ipv6-icmp", unix.IPPROTO_ICMPV6, true},
		{"positive: gre", "gre", unix.IPPROTO_GRE, true},
		{"positive: ipv4, fou's", "ipv4", unix.IPPROTO_IPIP, true},
		{"boundary: hopopt, protocol 0", "hopopt", 0, true},
		{"boundary: udplite, the highest goip knows", "udplite", unix.IPPROTO_UDPLITE, true},
		{"negative: ospf, iana-etc's 89, not goip's", "ospf", 89, false},
		{"negative: mobility-header, 135, not in iana-etc", "mobility-header", unix.IPPROTO_MH, false},
		{"corner: TCP, iana-etc's alias, not a name goip reads", "TCP", 200, false},
		{"corner: 3pc, a name starting with a digit, not goip's", "3pc", 34, false},
	}
	for _, tc := range tests {
		t.Run(tc.description, func(t *testing.T) {
			p, ok := protoByName(tc.name)
			if ok != tc.expected || ok && p != tc.number {
				t.Errorf("protoByName(%q) = %d, %v; want %d, %v", tc.name, p, ok, tc.number, tc.expected)
			}
			name, ok := protoByNumber(tc.number)
			if ok != tc.expected || ok && name != tc.name {
				t.Errorf("protoByNumber(%d) = %q, %v; want %q, %v", tc.number, name, ok, tc.name, tc.expected)
			}
		})
	}
}
