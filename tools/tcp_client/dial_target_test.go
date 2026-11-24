package main

import "testing"

// TestDialAddress pins the network and address tcp_client dials for
// -network, -connect, -srcaddr and a port. Before -network existed, every
// row but last_port's address failed: the network was always "tcp" and
// "%s:%d" left an IPv6 literal unbracketed.
func TestDialAddress(t *testing.T) {
	type expectation struct {
		network, addr, err string
	}
	tests := []struct {
		name        string
		category    string
		description string
		network     string
		connect     string
		srcaddr     string
		port        int
		expected    expectation
	}{
		{
			name: "v4_literal", category: "positive",
			description: "an IPv4 target",
			network:     "tcp4", connect: "127.0.0.1", port: 4000,
			expected: expectation{network: "tcp4", addr: "127.0.0.1:4000"},
		},
		{
			name: "v6_literal", category: "positive",
			description: "an IPv6 target is bracketed",
			network:     "tcp6", connect: "::1", port: 4000,
			expected: expectation{network: "tcp6", addr: "[::1]:4000"},
		},
		{
			name: "bad_network", category: "negative",
			description: "an unsupported network is an error",
			network:     "udp", connect: "127.0.0.1", port: 4000,
			expected: expectation{err: `-network: "udp" is not tcp, tcp4 or tcp6`},
		},
		{
			name: "v6_src_on_tcp4", category: "negative",
			description: "a source address of the wrong family is an error, before any dial",
			network:     "tcp4", connect: "127.0.0.1", srcaddr: "::1", port: 4000,
			expected: expectation{err: "-srcaddr ::1 is not an IPv4 address, as -network tcp4 needs"},
		},
		{
			name: "last_port", category: "boundary",
			description: "the top valid port",
			network:     "tcp", connect: "127.0.0.1", port: 65535,
			expected: expectation{network: "tcp", addr: "127.0.0.1:65535"},
		},
		{
			name: "zone", category: "corner",
			description: "a link-local target keeps its zone inside the brackets",
			network:     "tcp6", connect: "fe80::1%lo", port: 4000,
			expected: expectation{network: "tcp6", addr: "[fe80::1%lo]:4000"},
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			var got expectation
			var err error
			got.network, got.addr, err = dialTarget(tc.network, tc.connect, tc.srcaddr, tc.port)
			if err != nil {
				got = expectation{err: err.Error()}
			}
			if got != tc.expected {
				t.Errorf("%s (%s): %s\n got      %+v\n expected %+v", tc.name, tc.category, tc.description, got, tc.expected)
			}
		})
	}
}
