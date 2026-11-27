//go:build gotc_write

package gotc

import (
	"bytes"
	"errors"
	"strings"
	"testing"

	"github.com/randomizedcoder/xtcp2/pkg/xtcpnl"
)

// TestParseTbfSfq is tbf_parse_opt's and sfq_parse_opt's refusals and
// messages, and their attribute layout's length (the bytes are the
// tcqwrite corpus's).
//
// go test -tags gotc_write ./internal/gotc/ -run TestParseTbfSfq
func TestParseTbfSfq(t *testing.T) {
	tests := []struct {
		description string
		parse       kindParser
		args        []string
		expected    struct {
			ok     bool
			stderr string // a substring
			attrs  int    // the opts' length, when ok and non-zero
		}
	}{}
	add := func(d string, p kindParser, args string, ok bool, stderr string, attrs int) {
		tests = append(tests, struct {
			description string
			parse       kindParser
			args        []string
			expected    struct {
				ok     bool
				stderr string
				attrs  int
			}
		}{d, p, strings.Fields(args), struct {
			ok     bool
			stderr string
			attrs  int
		}{ok, stderr, attrs}})
	}
	// TCA_OPTIONS nest 4 + PARMS 4+36 + BURST 8 + RTAB 4+1024.
	add("positive: tbf rate burst limit", parseTbfQopt, "rate 1mbit burst 10k limit 3000", true, "", 1080)
	add("positive: tbf peakrate adds PBURST and PTAB", parseTbfQopt,
		"rate 1mbit burst 10k latency 50ms peakrate 2mbit mtu 1600", true, "", 1080+8+1028)
	add("positive: tbf rate64 above 2^32 bytes/s", parseTbfQopt, "rate 40gbit burst 1m limit 10m", true, "", 1080+12)
	add("negative: tbf with nothing lists every missing one", parseTbfQopt, "", false,
		"tbf: the \"rate\" parameter is mandatory.\ntbf: the \"burst\" parameter is mandatory.\n", 0)
	add("negative: tbf limit then latency", parseTbfQopt, "limit 1k latency 1ms", false,
		"specifying both \"limit\" and \"/latency\"", 0)
	add("negative: tbf an unknown word", parseTbfQopt, "foo", false, "tbf: unknown parameter \"foo\"", 0)
	add("negative: tbf duplicate rate", parseTbfQopt, "rate 1mbit rate 2mbit", false, "duplicate \"rate\"", 0)
	add("boundary: tbf overhead 65536 is not a u16", parseTbfQopt, "overhead 65536", false,
		"illegal value for \"overhead\"", 0)
	add("corner: tbf help is explain and -1", parseTbfQopt, "help", false, "Usage: ... tbf", 0)
	add("positive: sfq perturb", parseSfqQopt, "perturb 10", true, "", 4+xtcpnl.TcSfqQoptV1SizeCst)
	add("positive: sfq no words sends no options", parseSfqQopt, "", true, "", 0)
	add("positive: sfq red mode", parseSfqQopt, "redflowlimit 100000 min 8000 max 60000 probability 0.2 ecn",
		true, "", 4+xtcpnl.TcSfqQoptV1SizeCst)
	add("negative: sfq red without redflowlimit", parseSfqQopt, "ecn", false,
		"Required parameter (redflowlimit) is missing", 0)
	add("boundary: sfq limit 1", parseSfqQopt, "limit 1", false, "Illegal \"limit\", must be > 1", 0)
	add("corner: sfq depth's message says flows", parseSfqQopt, "depth x", false, "Illegal \"flows\"", 0)
	add("corner: sfq quantum's message says limit", parseSfqQopt, "quantum x", false, "Illegal \"limit\"", 0)
	for _, tc := range tests {
		t.Run(tc.description, func(t *testing.T) {
			var errb bytes.Buffer
			r := &runner{stderr: &errb, core: tcCore{clockFactor: 1, tickInUsec: 1}}
			opts, err := tc.parse(r, tc.args, "d0")
			if (err == nil) != tc.expected.ok {
				t.Fatalf("err %v, want ok=%t; stderr %q", err, tc.expected.ok, errb.String())
			}
			if err != nil && !errors.Is(err, errKindParse) {
				t.Fatalf("err %v is not errKindParse", err)
			}
			if !strings.Contains(errb.String(), tc.expected.stderr) {
				t.Errorf("stderr %q lacks %q", errb.String(), tc.expected.stderr)
			}
			if err != nil {
				return
			}
			n := 0
			if opts != nil {
				ab := xtcpnl.NewAttrBuilder(make([]byte, 8192))
				if err := opts(&ab); err != nil {
					t.Fatal(err)
				}
				n = ab.Len()
			}
			if n != tc.expected.attrs {
				t.Errorf("attrs %d bytes, want %d", n, tc.expected.attrs)
			}
		})
	}
}

// TestRedEval is tc_red_eval_P and tc_red_eval_ewma; expected values are
// tc_red.c compiled with gcc.
func TestRedEval(t *testing.T) {
	tests := []struct {
		description      string
		qmin, qmax       uint32
		prob             float64
		burst, avpkt     uint32
		expectedP, ewmaW int
	}{
		{"positive: sfq's defaults", 8000, 60000, 0.02, 0, 1000, 22, -1},
		{"positive: a burst that fits", 8000, 60000, 0.2, 30, 1000, 18, 6},
		{"boundary: qmin == qmax is 0; burst 30 never fits", 5, 5, 0.5, 30, 1000, 0, -1},
		{"corner: qmax below qmin is -1", 6, 5, 0.5, 30, 1000, -1, -1},
	}
	for _, tc := range tests {
		t.Run(tc.description, func(t *testing.T) {
			if got := redEvalP(tc.qmin, tc.qmax, tc.prob); got != tc.expectedP {
				t.Errorf("redEvalP = %d, want %d", got, tc.expectedP)
			}
			var errb bytes.Buffer
			r := &runner{stderr: &errb}
			if got := r.redEvalEwma(tc.qmin, tc.burst, tc.avpkt); got != tc.ewmaW {
				t.Errorf("redEvalEwma = %d, want %d", got, tc.ewmaW)
			}
		})
	}
}
