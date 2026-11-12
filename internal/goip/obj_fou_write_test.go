//go:build goip_write

package goip

import (
	"fmt"
	"strings"
	"testing"

	"golang.org/x/sys/unix"
)

// TestFouOptAttrs pins the attributes fou_parse_opt adds, as bytes, in its
// order: the port, the type, the family, the protocol, the addresses, the
// peer port and the device.
//
// go test ./internal/goip/ -run TestFouOptAttrs
func TestFouOptAttrs(t *testing.T) {
	local, peer, bad := "192.0.2.1", "2001:db8::2", "bogus"
	tests := []struct {
		description string
		opt         fouOpt
		expected    string
		errText     string
	}{
		{description: "boundary: port 0 and no more, which a parse never leaves, is still the three",
			opt: fouOpt{family: unix.AF_INET}, expected: "060001000000000005000400010000000500020002000000"},
		{description: "positive: every attribute over IPv4, in the parse's order",
			opt: fouOpt{port: 5555, ipproto: 4, ipprotoSet: true, family: unix.AF_INET, local: &local, peer: &local,
				peerPort: 5556, index: 3},
			expected: "0600010015b30000050004000100000005000200020000000500030004000000" +
				"08000600c0000201" + "08000800c0000201" + "06000a0015b40000" + "08000b0003000000"},
		{description: "positive: gue over IPv6, the V6 attributes",
			opt: fouOpt{port: 5555, gue: true, family: unix.AF_INET6, peer: &peer, peerPort: 1},
			expected: "0600010015b30000050004000200000005000200" + "0a000000" +
				"1400090020010db8000000000000000000000002" + "06000a0000010000"},
		{description: "corner: ip -0's family is sent, its addresses the V6 attributes, which goip cannot parse",
			opt: fouOpt{port: 1, family: unix.AF_PACKET}, expected: "06000100000100000500040001000000050002001100" + "0000"},
		{description: "negative: an address get_addr refuses", opt: fouOpt{port: 1, family: unix.AF_INET, local: &bad},
			errText: "inet address is expected"},
		{description: "negative: ip -0's address", opt: fouOpt{port: 1, family: unix.AF_PACKET, local: &local},
			errText: "not implemented"},
	}
	for _, tc := range tests {
		t.Run(tc.description, func(t *testing.T) {
			b, err := tc.opt.attrs()
			if tc.errText != "" {
				if err == nil || !strings.Contains(err.Error(), tc.errText) {
					t.Fatalf("err %v, want %q", err, tc.errText)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if got := hexOf(b); got != tc.expected {
				t.Errorf("attrs\n got %s\nwant %s", got, tc.expected)
			}
		})
	}
}

// TestFouProto is ipproto's value: the names goip knows, get_u8 base 0,
// and the refusals.
//
// go test ./internal/goip/ -run TestFouProto
func TestFouProto(t *testing.T) {
	tests := []struct {
		description string
		val         string
		expected    uint8
		exit        int
	}{
		{description: "positive: a number", val: "4", expected: 4},
		{description: "positive: ipv4, iana-etc's name for 4", val: "ipv4", expected: 4},
		{description: "positive: ipv6, 41", val: "ipv6", expected: 41},
		{description: "positive: gre, from rule's names", val: "gre", expected: 47},
		{description: "corner: hopopt is 0, which a number may not be", val: "hopopt", expected: 0},
		{description: "corner: octal, base 0", val: "010", expected: 8},
		{description: "boundary: 255", val: "255", expected: 255},
		{description: "boundary: 256", val: "256", exit: ExitMinusOne},
		{description: "negative: 0", val: "0", exit: ExitMinusOne},
		{description: "negative: empty", val: "", exit: ExitMinusOne},
		{description: "negative: a word starting with a digit", val: "4x", exit: ExitMinusOne},
		{description: "negative: a name goip does not know", val: "IPv4", exit: ExitUsage},
	}
	for _, tc := range tests {
		t.Run(tc.description, func(t *testing.T) {
			p, err := fouProto(tc.val)
			if code := ioamExit(err); code != tc.exit {
				t.Fatalf("exit %d (%v), want %d", code, err, tc.exit)
			}
			if err == nil && p != tc.expected {
				t.Errorf("got %d, want %d", p, tc.expected)
			}
		})
	}
}

// TestFouExitCodes pins the status each of fou's errors exits with.
//
// go test ./internal/goip/ -run TestFouExitCodes
func TestFouExitCodes(t *testing.T) {
	tests := []struct {
		description string
		err         error
		expected    int
	}{
		{description: "positive: usage's exit(-1)", err: ErrFouUsage, expected: 255},
		{description: "positive: a refused device or show's arguments, exit 1", err: ErrFouRefused, expected: 1},
		{description: "positive: Dump terminated, do_show's 1 negated", err: ErrFouDumpTerminated, expected: 255},
		{description: "corner: invarg wrapped as usage is 255, not invarg's 1",
			err: fouUsage(invargErr("invalid port", "0")), expected: 255},
		{description: "corner: a refused answer wrapped as Dump terminated is 255",
			err: fmt.Errorf("%w: %w", ErrFouDumpTerminated, ErrDumpTerminated), expected: 255},
	}
	for _, tc := range tests {
		t.Run(tc.description, func(t *testing.T) {
			if got := exitCodeOf(tc.err); got != tc.expected {
				t.Errorf("exitCodeOf(%v) = %d, want %d", tc.err, got, tc.expected)
			}
		})
	}
}
