package goip

import (
	"bytes"
	"testing"
)

// useIP730 runs a test as iproute2 7.3.0: every knob at its default. It
// is for the ws-ip730 corpus, and for rows about the 7.3.0 side of a knob.
// Not for a parallel test: the knobs are package state.
func useIP730(t *testing.T) {
	t.Helper()
	prev := LLAddrStrict
	LLAddrStrict = true
	t.Cleanup(func() { LLAddrStrict = prev })
}

// TestLLAddrA2NStrict is 7.3.0's ll_addr_a2n (5faf18f6, lib/ll_addr.c:64-85):
// each part from a hex digit to a ':' or the end, at most 255, no more parts
// than the buffer, and the whole argument named on a refusal.
//
// go test ./internal/goip/ -run TestLLAddrA2NStrict
func TestLLAddrA2NStrict(t *testing.T) {
	useIP730(t)
	type expected struct {
		addr []byte
		n    int
		bad  string
	}
	tests := []struct {
		description string
		arg         string
		expected    expected
	}{
		{"positive: an address", macvlanMAC1, expected{addr: []byte{2, 0, 0, 0, 0, 1}, n: 6}},
		{"positive: an IPv4 address is four bytes", "10.0.0.1", expected{addr: []byte{10, 0, 0, 1}, n: 4}},
		{"positive: one-digit parts", "2:0:0:0:0:1", expected{addr: []byte{2, 0, 0, 0, 0, 1}, n: 6}},
		{"positive: a 0x prefix is strtoul's", "0x2:0:0:0:0:1", expected{addr: []byte{2, 0, 0, 0, 0, 1}, n: 6}},
		{"boundary: one part short is silent", "02:00:00:00:01", expected{addr: []byte{2, 0, 0, 0, 1}, n: 5}},
		{"boundary: ff is the largest part", "ff:ff:ff:ff:ff:ff", expected{addr: []byte{255, 255, 255, 255, 255, 255}, n: 6}},
		{"negative: 1ff is past 255", "1ff", expected{n: -1, bad: "1ff"}},
		{"boundary: 100 is 256, one past ff", "02:100", expected{n: -1, bad: "02:100"}},
		{"negative: trailing garbage after a part, which sscanf skipped", "0aq", expected{n: -1, bad: "0aq"}},
		{"negative: a part that isn't hex names the whole argument", "00:11:zz", expected{n: -1, bad: "00:11:zz"}},
		{"negative: a dash is not a separator", "00-11", expected{n: -1, bad: "00-11"}},
		{"negative: garbage between two hex parts", "0aq1", expected{n: -1, bad: "0aq1"}},
		{"negative: a trailing colon is an empty part", "00:11:", expected{n: -1, bad: "00:11:"}},
		{"negative: an empty argument", "", expected{n: -1, bad: ""}},
		{"negative: a sign is not a hex digit", "+2:0:0:0:0:1", expected{n: -1, bad: "+2:0:0:0:0:1"}},
		{"negative: a leading space is not a hex digit", " 2", expected{n: -1, bad: " 2"}},
		{"negative: an IPv4 address that won't parse is named whole", "10.0.0.256", expected{n: -1, bad: "10.0.0.256"}},
		{"boundary: seven parts are refused, not read past the buffer", macvlanMAC1 + ":07", expected{n: -1, bad: macvlanMAC1 + ":07"}},
		{"corner: a word that starts hex is read up to its first non-hex byte", "bcqueuelen", expected{n: -1, bad: "bcqueuelen"}},
	}
	for _, tc := range tests {
		t.Run(tc.description, func(t *testing.T) {
			b, n, bad := llAddrA2NPart(tc.arg, macvlanAddrLenCst)
			if !bytes.Equal(b, tc.expected.addr) || n != tc.expected.n || bad != tc.expected.bad {
				t.Errorf("got (% x, %d, %q), want (% x, %d, %q)", b, n, bad, tc.expected.addr, tc.expected.n, tc.expected.bad)
			}
		})
	}
}

// TestLLAddrCutStrict is the word PREV_ARG re-reads: 7.3.0 leaves argv
// whole, where 7.2.0 cut it at the first ':'.
//
// go test ./internal/goip/ -run TestLLAddrCutStrict
func TestLLAddrCutStrict(t *testing.T) {
	tests := []struct {
		description string
		strict      bool
		arg         string
		expected    string
	}{
		{description: "positive: 7.3.0 leaves the word whole", strict: true, arg: "00:11:zz", expected: "00:11:zz"},
		{description: "positive: 7.2.0 cuts at the first colon", strict: false, arg: "00:11:zz", expected: "00"},
		{description: "corner: 7.2.0 leaves an IPv4 address whole", strict: false, arg: "10.0.0.1", expected: "10.0.0.1"},
		{description: "corner: no colon, nothing to cut", strict: false, arg: "bcqueuelen", expected: "bcqueuelen"},
	}
	for _, tc := range tests {
		t.Run(tc.description, func(t *testing.T) {
			prev := LLAddrStrict
			LLAddrStrict = tc.strict
			defer func() { LLAddrStrict = prev }()
			if got := llAddrCut(tc.arg); got != tc.expected {
				t.Errorf("llAddrCut(%q) = %q, want %q", tc.arg, got, tc.expected)
			}
		})
	}
}
