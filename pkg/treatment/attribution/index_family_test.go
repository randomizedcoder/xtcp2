package attribution

import (
	"net/netip"
	"testing"
)

// TestIndexV4Mapped pins how Stamp matches an AF_INET6 socket whose
// destination is v4-mapped (a dual-stack socket talking to an IPv4 peer):
// against the IPv4 route, as an AF_INET socket to that peer would be.
// Before, mapped_matches_v4 and mapped_zero failed: the 16-byte form was
// looked up as an IPv6 address.
func TestIndexV4Mapped(t *testing.T) {
	tests := []struct {
		name        string
		category    string
		description string
		route       string
		family      uint32
		destination []byte
		expected    bool // attributed
	}{
		{
			name: "mapped_matches_v4", category: "positive",
			description: "a v4-mapped destination finds its IPv4 route",
			route:       "192.0.0.0/32", family: 10,
			destination: []byte{0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0xff, 0xff, 192, 0, 0, 0},
			expected:    true,
		},
		{
			name: "real_v6_not_unmapped", category: "negative",
			description: "a real IPv6 destination with the same low bytes is not an IPv4 address",
			route:       "192.0.0.0/32", family: 10,
			destination: []byte{0x20, 1, 0xd, 0xb8, 0, 0, 0, 0, 0, 0, 0, 0, 192, 0, 0, 0},
			expected:    false,
		},
		{
			name: "mapped_zero", category: "boundary",
			description: "::ffff:0.0.0.0 is 0.0.0.0 and matches only a 0.0.0.0 route",
			route:       "0.0.0.0/32", family: 10,
			destination: []byte{0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0xff, 0xff, 0, 0, 0, 0},
			expected:    true,
		},
		{
			name: "four_byte_form", category: "corner",
			description: "an AF_INET socket's padded 16-byte buffer still matches, as before",
			route:       "192.0.0.0/32", family: 2,
			destination: []byte{192, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0},
			expected:    true,
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			s, r := fixture(1)
			s.Records[0].Assignment.Treatment.Destination = netip.MustParsePrefix(tc.route)
			r.InetDiagMsgFamily = tc.family
			r.InetDiagMsgSocketDestination = tc.destination
			Build(s, testIdentity, true).Stamp(r)
			if got := r.TreatmentAssignmentId != ""; got != tc.expected {
				t.Errorf("%s (%s): %s\n attributed %t, expected %t", tc.name, tc.category, tc.description, got, tc.expected)
			}
		})
	}
}
