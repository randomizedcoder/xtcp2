//go:build gotc_write

package gotc

import (
	"bytes"
	"encoding/binary"
	"errors"
	"math"
	"strings"
	"testing"

	"github.com/randomizedcoder/xtcp2/pkg/xtcpnl"
)

// TestParseNetemQopt is netem_parse_opt's arms and checks, and the
// TCA_OPTIONS it lays out (gotc 17W).
//
// go test -tags gotc_write ./internal/gotc/ -run TestParseNetemQopt
func TestParseNetemQopt(t *testing.T) {
	type expected struct {
		err     error  // errKindParse, an exitCode, or nil
		stderr  string // a substring of stderr ("" is empty stderr)
		optsLen int    // TCA_OPTIONS's rta_len when err is nil
		check   func(t *testing.T, opts []byte)
	}
	attrAt := func(opts []byte, typ uint16) []byte {
		for p := 4 + xtcpnl.TcNetemQoptSizeCst; p+4 <= len(opts); {
			l := int(binary.LittleEndian.Uint16(opts[p:]))
			if binary.LittleEndian.Uint16(opts[p+2:])&^0xc000 == typ {
				return opts[p+4 : p+l]
			}
			p += rtaAlign(l)
		}
		return nil
	}
	tests := []struct {
		description string
		args        string
		expected    expected
	}{
		{"positive: delay alone is LATENCY64 after the qopt", "delay 10ms",
			expected{optsLen: 4 + 24 + 12, check: func(t *testing.T, o []byte) {
				if v := binary.LittleEndian.Uint64(attrAt(o, xtcpnl.TcaNetemLatency64)); v != 10_000_000 {
					t.Errorf("latency %d", v)
				}
			}}},
		{"positive: gemodel sends h as 1-(1-h)", "loss gemodel 1% 10% 70% 0.1%",
			expected{optsLen: 4 + 24 + 4 + 20, check: func(t *testing.T, o []byte) {
				ge := attrAt(o, xtcpnl.TcaNetemLoss)
				if h := binary.LittleEndian.Uint32(ge[4+8:]); h != math.MaxUint32-setPercent(0.7) {
					t.Errorf("h %d", h)
				}
			}}},
		{"negative: reorder without delay", "reorder 25%",
			expected{err: errKindParse, stderr: "reordering not possible without specifying some delay\nUsage: ... netem"}},
		{"negative: a second loss is explain1 with its stray newline", "loss 1% loss 2%",
			expected{err: errKindParse, stderr: "Illegal \"duplicate loss argument\n\"\n"}},
		{"negative: a missing distribution file", "delay 1ms 1ms distribution nosuch",
			expected{err: errKindParse, stderr: "No distribution data for nosuch (/usr/lib/tc//nosuch.dist: "}},
		{"negative: an unknown word", "bogus", expected{err: errKindParse, stderr: "What is \"bogus\"?\nUsage:"}},
		{"boundary: a rate at 2^32 bytes/s sends RATE64 and ~0", "rate 34359738368bit",
			expected{optsLen: 4 + 24 + 12 + 20, check: func(t *testing.T, o []byte) {
				if attrAt(o, xtcpnl.TcaNetemRate64) == nil ||
					binary.LittleEndian.Uint32(attrAt(o, xtcpnl.TcaNetemRate)) != math.MaxUint32 {
					t.Error("no RATE64 or rate not ~0")
				}
			}}},
		{"boundary: limit with no value exits", "limit", expected{err: exitCode(-1), stderr: "Command line is not complete"}},
		{"corner: a duplicate correlation alone is not sent (present[CORR] unset)", "duplicate 1% 30%",
			expected{optsLen: 4 + 24}},
		{"corner: help is the usage with no What is", "help", expected{err: errKindParse, stderr: "Usage: ... netem"}},
		{"corner: slot distribution with a zero jitter", "slot distribution normal 1ms 0",
			expected{err: errKindParse, stderr: "Non-positive jitter\n"}},
		{"corner: ecn with no loss model", "ecn", expected{err: errKindParse, stderr: "ecn requested without loss model\n"}},
	}
	for _, tc := range tests {
		t.Run(tc.description, func(t *testing.T) {
			sys := emptySys(t)
			writeFiles(t, sys.root, map[string]string{"usr/lib/tc/normal.dist": "# t\n 1 -2 3\n"})
			var errb bytes.Buffer
			r := &runner{stderr: &errb, sys: sys}
			opts, err := parseNetemQopt(r, strings.Fields(tc.args), "")
			if tc.expected.stderr == "" && errb.Len() != 0 || !strings.Contains(errb.String(), tc.expected.stderr) {
				t.Errorf("stderr %q, want it to hold %q", errb.String(), tc.expected.stderr)
			}
			if tc.expected.err != nil {
				if !errors.Is(err, tc.expected.err) {
					t.Fatalf("err %v, want %v", err, tc.expected.err)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			ab := xtcpnl.NewAttrBuilder(make([]byte, 4096))
			if err := opts(&ab); err != nil {
				t.Fatal(err)
			}
			o := ab.Bytes()
			if l := int(binary.LittleEndian.Uint16(o)); l != tc.expected.optsLen || len(o) != rtaAlign(l) {
				t.Fatalf("TCA_OPTIONS rta_len %d (%d bytes), want %d", l, len(o), tc.expected.optsLen)
			}
			if tc.expected.check != nil {
				tc.expected.check(t, o)
			}
		})
	}
}

// TestGetDistribution is get_distribution's reading of a .dist file.
func TestGetDistribution(t *testing.T) {
	tests := []struct {
		description string
		body        string
		expected    []int16
		n           int
	}{
		{"positive: comments and blank lines skipped", "# c\n\n 1 2\n-3\n", []int16{1, 2, -3}, 3},
		{"negative: an empty table is a silent failure", "# only\n", nil, 0},
		{"boundary: a long is kept as its low 16 bits", "65537 0x8000\n", []int16{1, math.MinInt16}, 2},
		{"corner: a line's junk ends that line only", "4 x 5\n6\n", []int16{4, 6}, 2},
	}
	for _, tc := range tests {
		t.Run(tc.description, func(t *testing.T) {
			sys := emptySys(t)
			writeFiles(t, sys.root, map[string]string{"usr/lib/tc/t.dist": tc.body})
			var errb bytes.Buffer
			r := &runner{stderr: &errb, sys: sys}
			got, n := r.getDistribution("t")
			if n != tc.n || len(got) != len(tc.expected) {
				t.Fatalf("got %v n=%d, want %v n=%d", got, n, tc.expected, tc.n)
			}
			for i := range got {
				if got[i] != tc.expected[i] {
					t.Fatalf("got %v, want %v", got, tc.expected)
				}
			}
		})
	}
}
