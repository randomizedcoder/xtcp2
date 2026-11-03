package goip

import (
	"bytes"
	"encoding/binary"
	"errors"
	"strings"
	"testing"

	"github.com/randomizedcoder/xtcp2/pkg/xtcpnl"
	"golang.org/x/sys/unix"
)

// nexthopKeywordRow is one ipnh_modify argument line: the nhmsg and
// attributes it builds, or the error and the text it carries. The fake
// resolver knows d0 (3) and veth1 (5); its calls are listed.
type nexthopKeywordRow struct {
	description string
	family      uint8
	args        string
	nhm         []byte
	attrs       []byte
	resolved    []string
	err         error
	errMsg      string
}

func runNexthopKeywordRows(t *testing.T, rows []nexthopKeywordRow) {
	t.Helper()
	for i := range rows {
		tc := &rows[i]
		t.Run(tc.description, func(t *testing.T) {
			var calls []string
			resolve := func(name string) (int32, error) {
				calls = append(calls, name)
				switch name {
				case "d0":
					return 3, nil
				case "veth1":
					return 5, nil
				}
				return 0, nil
			}
			w, err := parseNexthopWriteArgs(tc.family, resolve, strings.Fields(tc.args))
			if strings.Join(calls, " ") != strings.Join(tc.resolved, " ") {
				t.Errorf("resolved %q, want %q", calls, tc.resolved)
			}
			if tc.err != nil {
				if !errors.Is(err, tc.err) || !strings.Contains(err.Error(), tc.errMsg) {
					t.Fatalf("err = %v, want %v containing %q", err, tc.err, tc.errMsg)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if got := w.nhm(); !bytes.Equal(got, tc.nhm) {
				t.Errorf("nhmsg % x, want % x", got, tc.nhm)
			}
			if got := w.attrs.Bytes(); !bytes.Equal(got, tc.attrs) {
				t.Errorf("attrs % x\nwant  % x", got, tc.attrs)
			}
		})
	}
}

// nhmOf is a struct nhmsg: family, scope 0, protocol, a reserved byte and
// flags.
func nhmOf(family, protocol uint8, flags uint32) []byte {
	return (&nexthopWrite{family: family, protocol: protocol, flags: flags}).nhm()
}

// TestNexthopWriteHwStats is ipnh_modify's hw_stats arm
// (ip/ipnexthop.c:1131): parse_on_off into NHA_HW_STATS_ENABLE u32, where the
// loop reads it. GW3-T01 in goip-write-keywords.md.
//
// go test ./internal/goip/ -run TestNexthopWriteHwStats
func TestNexthopWriteHwStats(t *testing.T) {
	u32 := func(v uint32) []byte { return binary.LittleEndian.AppendUint32(nil, v) }
	hw := func(v uint32) []byte { return fraOf(xtcpnl.NhaHwStatsEnable, u32(v)...) }
	id1 := fraOf(xtcpnl.NhaID, u32(1)...)
	grp21 := fraOf(xtcpnl.NhaGroup, append(u32(21), 0, 0, 0, 0)...)
	cat := func(a ...[]byte) []byte { return bytes.Join(a, nil) }
	runNexthopKeywordRows(t, []nexthopKeywordRow{
		{description: "positive: on", args: "id 1 group 21 hw_stats on", nhm: nhmOf(0, 0, 0), attrs: cat(id1, grp21, hw(1))},
		{description: "positive: off", args: "id 1 group 21 hw_stats off", nhm: nhmOf(0, 0, 0), attrs: cat(id1, grp21, hw(0))},
		{description: "positive: before protocol", args: "group 21 hw_stats off protocol static",
			nhm: nhmOf(0, unix.RTPROT_STATIC, 0), attrs: cat(grp21, hw(0))},
		{description: "positive: the attribute's bytes", args: "hw_stats on", nhm: nhmOf(0, 0, 0),
			attrs: []byte{8, 0, 16, 0, 1, 0, 0, 0}},
		{description: "positive: after a dev", args: "id 10 dev d0 hw_stats on", nhm: nhmOf(unix.AF_INET, 0, 0),
			attrs: cat(fraOf(xtcpnl.NhaID, u32(10)...), fraOf(xtcpnl.NhaOIF, u32(3)...), hw(1)), resolved: []string{"d0"}},
		{description: "negative: a bad value", args: "hw_stats maybe", err: ErrInvalidArgument,
			errMsg: `Error: argument of "hw_stats" must be one of "off", "on", not "maybe"`},
		{description: "negative: no value", args: "id 1 hw_stats", err: ErrInvalidArgument, errMsg: "Command line is not complete"},
		{description: "corner: no prefix match", args: "hw_stat on", err: ErrInvalidArgument,
			errMsg: `Error: argument "hw_stat" is wrong: `},
		{description: "corner: on takes no prefix", args: "hw_stats o", err: ErrInvalidArgument,
			errMsg: `must be one of "off", "on", not "o"`},
		{description: "corner: twice is two attributes", args: "hw_stats on hw_stats off", nhm: nhmOf(0, 0, 0), attrs: cat(hw(1), hw(0))},
	})
}
