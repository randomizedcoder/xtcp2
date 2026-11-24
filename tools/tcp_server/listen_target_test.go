package main

import "testing"

// TestListenAddress pins the network and address tcp_server listens on for
// -network, -bind and a port. Before -network existed, every row but
// v4_literal's address failed: the network was always "tcp" and "%s:%d"
// left an IPv6 literal unbracketed.
func TestListenAddress(t *testing.T) {
	type expectation struct {
		network, addr, err string
	}
	tests := []struct {
		name        string
		category    string
		description string
		network     string
		bind        string
		port        int
		expected    expectation
	}{
		{
			name: "v4_literal", category: "positive",
			description: "an IPv4 bind",
			network:     "tcp4", bind: "127.0.0.1", port: 4000,
			expected: expectation{network: "tcp4", addr: "127.0.0.1:4000"},
		},
		{
			name: "v6_literal", category: "positive",
			description: "an IPv6 bind is bracketed",
			network:     "tcp6", bind: "::1", port: 4000,
			expected: expectation{network: "tcp6", addr: "[::1]:4000"},
		},
		{
			name: "bad_network", category: "negative",
			description: "an unsupported network is an error",
			network:     "udp", bind: "127.0.0.1", port: 4000,
			expected: expectation{err: `-network: "udp" is not tcp, tcp4 or tcp6`},
		},
		{
			name: "v4_on_tcp6", category: "negative",
			description: "an IPv4 bind for tcp6 is an error, not a silent IPv4 socket",
			network:     "tcp6", bind: "127.0.0.1", port: 4000,
			expected: expectation{err: "-bind 127.0.0.1 is not an IPv6 address, as -network tcp6 needs"},
		},
		{
			name: "top_port", category: "boundary",
			description: "the port range runs past 65535 (start 65535, -count 2)",
			network:     "tcp", bind: "0.0.0.0", port: 65536,
			expected: expectation{err: "port 65536 is outside 1-65535"},
		},
		{
			name: "zone", category: "corner",
			description: "a link-local address keeps its zone inside the brackets",
			network:     "tcp6", bind: "fe80::1%lo", port: 4000,
			expected: expectation{network: "tcp6", addr: "[fe80::1%lo]:4000"},
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			var got expectation
			var err error
			got.network, got.addr, err = listenTarget(tc.network, tc.bind, tc.port)
			if err != nil {
				got = expectation{err: err.Error()}
			}
			if got != tc.expected {
				t.Errorf("%s (%s): %s\n got      %+v\n expected %+v", tc.name, tc.category, tc.description, got, tc.expected)
			}
		})
	}
}
