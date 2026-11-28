//go:build gotc_write

package gotc

import (
	"bytes"
	"errors"
	"testing"

	"github.com/randomizedcoder/xtcp2/pkg/xtcpnl"
)

// TestParseHtb is q_htb.c's parsers on crafted argv: the return, the
// stderr's first line and, on success, the TCA_OPTIONS length.
//
// go test -tags gotc_write ./internal/gotc/ -run TestParseHtb
func TestParseHtb(t *testing.T) {
	type expected struct {
		parseErr bool // errKindParse
		exit     bool // exitCode
		stderr   string
		optLen   int
	}
	tests := []struct {
		description string
		class       bool
		args        []string
		expected    expected
	}{
		{"positive: qdisc default r2q direct_qlen offload", false,
			[]string{"default", "10", "r2q", "5", "direct_qlen", "7", "offload"}, expected{optLen: 4 + 24 + 8 + 4}},
		{"positive: class rate alone", true, []string{"rate", "1mbit"}, expected{optLen: 4 + 48 + 2*1028}},
		{"positive: class rate 40gbit adds RATE64", true, []string{"rate", "40gbit"},
			expected{optLen: 4 + 2*12 + 48 + 2*1028}},
		{"negative: qdisc unknown word", false, []string{"bogus"},
			expected{parseErr: true, stderr: "What is \"bogus\"?\n"}},
		{"negative: class needs rate", true, []string{"prio", "1"},
			expected{parseErr: true, stderr: "\"rate\" is required.\n"}},
		{"negative: double rate", true, []string{"rate", "1mbit", "rate", "2mbit"},
			expected{parseErr: true, stderr: "Double \"rate\" spec\n"}},
		{"negative: bad mpu over 16 bits", true, []string{"rate", "1mbit", "mpu", "65536"},
			expected{parseErr: true, stderr: "Illegal \"mpu\"\n"}},
		{"negative: percent rate with no device", true, []string{"rate", "10%"},
			expected{parseErr: true, stderr: "No device specified; specify device to rate limit by percentage\n"}},
		{"boundary: r2q missing its value", false, []string{"r2q"}, expected{exit: true,
			stderr: "Command line is not complete. Try option \"help\"\n"}},
		{"boundary: mpu 65535 fits", true, []string{"rate", "1mbit", "mpu", "65535"}, expected{optLen: 4 + 48 + 2*1028}},
		{"corner: class help is explain and -1", true, []string{"help"},
			expected{parseErr: true, stderr: "Usage: ... qdisc add ... htb [default N] [r2q N]\n"}},
		{"corner: debug stops at a bad digit", false, []string{"debug", "0129"}, expected{optLen: 4 + 24}},
	}
	for _, tc := range tests {
		t.Run(tc.description, func(t *testing.T) {
			var errb bytes.Buffer
			r := &runner{stderr: &errb, sys: emptySys(t), core: tcCore{clockFactor: 1, tickInUsec: 1}}
			p := parseHtbQopt
			if tc.class {
				p = parseHtbCopt
			}
			opts, err := p(r, tc.args, "")
			var code exitCode
			if errors.Is(err, errKindParse) != tc.expected.parseErr || errors.As(err, &code) != tc.expected.exit {
				t.Fatalf("err %v, want %+v", err, tc.expected)
			}
			first, _, _ := bytes.Cut(errb.Bytes(), []byte("\n"))
			if got := string(first) + "\n"; tc.expected.stderr != "" && got != tc.expected.stderr {
				t.Errorf("stderr %q, want %q", got, tc.expected.stderr)
			}
			if err != nil {
				return
			}
			var buf [8192]byte
			ab := xtcpnl.NewAttrBuilder(buf[:])
			if err := opts(&ab); err != nil {
				t.Fatal(err)
			}
			if ab.Len() != tc.expected.optLen {
				t.Errorf("TCA_OPTIONS %d bytes, want %d", ab.Len(), tc.expected.optLen)
			}
		})
	}
}
