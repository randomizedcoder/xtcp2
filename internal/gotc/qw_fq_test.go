//go:build gotc_write

package gotc

import (
	"bytes"
	"encoding/hex"
	"errors"
	"strings"
	"testing"

	"github.com/randomizedcoder/xtcp2/pkg/xtcpnl"
)

// TestParseFqQopts runs fq_parse_opt's and fq_codel_parse_opt's ports on
// crafted argv: the TCA_OPTIONS bytes, or the error and stderr.
//
// go test -tags gotc_write ./internal/gotc/ -run TestParseFqQopts
func TestParseFqQopts(t *testing.T) {
	tests := []struct {
		description string
		parse       kindParser
		args        string
		expected    string // hex of the attributes, or "" on error
		err         error
		stderr      string // a substring of stderr
	}{
		{"positive: fq_codel with no options is an empty nest", parseFqCodelQopt, "",
			"04000200", nil, ""},
		{"positive: fq_codel attribute order is fixed, not argv's", parseFqCodelQopt, "flows 8 limit 100",
			"14000200" + "0800020064000000" + "0800050008000000", nil, ""},
		{"positive: fq_codel selector writes mask then selector as u8", parseFqCodelQopt,
			"ce_threshold_selector 1/3", "14000200" + "05000b0003000000" + "05000a0001000000", nil, ""},
		{"negative: fq_codel selector with no mask", parseFqCodelQopt, "ce_threshold_selector 1",
			"", errKindParse, "Missing mask"},
		{"negative: fq_codel unknown word", parseFqCodelQopt, "bogus", "", errKindParse, "What is \"bogus\"?"},
		{"boundary: fq_codel limit at UINT_MAX", parseFqCodelQopt, "limit 4294967295",
			"0c000200" + "08000200ffffffff", nil, ""},
		{"boundary: fq_codel limit past UINT_MAX", parseFqCodelQopt, "limit 4294967296",
			"", errKindParse, "Illegal \"limit\""},
		{"positive: fq buckets 1024 is log 10", parseFqQopt, "buckets 1024",
			"0c000200" + "080008000a000000", nil, ""},
		{"positive: fq horizon_drop is a u8", parseFqQopt, "horizon_drop",
			"0c000200" + "05000f0001000000", nil, ""},
		{"negative: fq orphan_mask error names initial_quantum (tc's)", parseFqQopt, "orphan_mask x",
			"", errKindParse, "Illegal \"initial_quantum\""},
		{"negative: fq bands must be 3", parseFqQopt, "bands 4 priomap", "", errKindParse, "\"bands\" must be 3"},
		{"negative: fq weight below FQ_MIN_WEIGHT", parseFqQopt, "weights 1 2 3", "", errKindParse, "too small"},
		{"boundary: fq timer_slack past 32 bits", parseFqQopt, "timer_slack 5s",
			"", errKindParse, "out of range"},
		{"corner: fq priomap short of 16", parseFqQopt, "bands 3 priomap 0 1", "", errKindParse,
			"Not enough elements in priomap"},
		{"corner: fq percent maxrate with no device", parseFqQopt, "maxrate 10%", "", errKindParse,
			"No device specified"},
	}
	for _, tc := range tests {
		t.Run(tc.description, func(t *testing.T) {
			var errb bytes.Buffer
			r := &runner{stderr: &errb, sys: emptySys(t)}
			opts, err := tc.parse(r, strings.Fields(tc.args), "")
			if !errors.Is(err, tc.err) || (tc.err == nil) != (err == nil) {
				t.Fatalf("err %v, want %v (stderr %q)", err, tc.err, errb.String())
			}
			if !strings.Contains(errb.String(), tc.stderr) {
				t.Errorf("stderr %q, want %q in it", errb.String(), tc.stderr)
			}
			if err != nil {
				return
			}
			buf := make([]byte, 1024)
			ab := xtcpnl.NewAttrBuilder(buf)
			if err := opts(&ab); err != nil {
				t.Fatal(err)
			}
			if got := hex.EncodeToString(ab.Bytes()); got != tc.expected {
				t.Errorf("attrs %s, want %s", got, tc.expected)
			}
		})
	}
}
