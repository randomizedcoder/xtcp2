package goip

import (
	"bytes"
	"errors"
	"slices"
	"strings"
	"testing"

	"github.com/randomizedcoder/xtcp2/pkg/xtcpnl"
)

// The `type macvlan` and `type macvtap` tests (docs/netlink/goip-link-kinds.md,
// "KG2-T02 The macvlan and macvtap words"), against KC3-T02's rows of the
// 7.3.0 kind corpus.

// KC3-T02's rows, a directory per kind.
const (
	macvlanKindDir = "../../pkg/xtcpnl/testdata/7_3_0/link-write/kinds/macvlan/"
	macvtapKindDir = "../../pkg/xtcpnl/testdata/7_3_0/link-write/kinds/macvtap/"
)

// The corpus's two source addresses, kind-write-corpus.exp's MAC1 and MAC2.
const (
	macvlanMAC1 = "02:00:00:00:00:01"
	macvlanMAC2 = "02:00:00:00:00:02"
)

// macvlanCorpusArgv is each KC3-T02 macvlan row's argv after `ip link`,
// keyed by slug.
func macvlanCorpusArgv() map[string][]string {
	out := map[string][]string{}
	for slug, w := range map[string]string{
		"add_default":                "",
		"add_mode_private":           "mode private",
		"add_mode_vepa":              "mode vepa",
		"add_mode_bridge":            "mode bridge",
		"add_mode_passthru":          "mode passthru",
		"add_mode_source":            "mode source",
		"add_flag_nodst":             "mode bridge flag nodst",
		"add_nodst":                  "mode bridge nodst",
		"add_flag_null":              "mode bridge flag null",
		"add_passthru_nopromisc":     "mode passthru nopromisc",
		"add_bcqueuelen":             "bcqueuelen 2000",
		"add_bclim":                  "bclim 10",
		"add_bclim_negative":         "bclim -1",
		"add_macaddr_add":            "mode source macaddr add " + macvlanMAC1,
		"add_macaddr_set":            "mode source macaddr set " + macvlanMAC1 + " " + macvlanMAC2,
		"add_abbrev":                 "mo bridge",
		"add_macaddr_not_source":     "mode bridge macaddr add " + macvlanMAC1,
		"add_macaddr_multicast":      "mode source macaddr add 01:00:5e:00:00:01",
		"add_nopromisc_not_passthru": "mode bridge nopromisc",
		"add_mode_bad":               "mode bogus",
		"add_flag_bad":               "flag bogus",
		"add_macaddr_op_bad":         "macaddr bogus",
		"add_macaddr_bad":            "mode source macaddr add zz",
		"add_bcqueuelen_bad":         "bcqueuelen x",
		"add_bclim_bad":              "bclim x",
		"add_bclim_abbrev":           "bcl 10",
		"add_no_value":               "mode",
	} {
		out[slug] = stackedArgv("d0", "m0", kindMacvlanCst, w)
	}
	out["add_second_passthru"] = stackedArgv("d0", "m1", kindMacvlanCst, "mode passthru")
	out["add_on_loopback"] = stackedArgv("lo", "m0", kindMacvlanCst, "")
	for slug, w := range map[string]string{
		"set_mode":          "mode private",
		"set_macaddr_del":   "macaddr del " + macvlanMAC1,
		"set_macaddr_flush": "macaddr flush",
		"set_bcqueuelen":    "bcqueuelen 500",
		"set_passthru":      "mode passthru",
	} {
		out[slug] = stackedSetArgv("m0", kindMacvlanCst, w)
	}
	return out
}

// macvtapCorpusArgv is the same for the macvtap rows, whose device is t0.
func macvtapCorpusArgv() map[string][]string {
	out := map[string][]string{}
	for slug, w := range map[string]string{
		"add_default":       "",
		"add_mode_bridge":   "mode bridge",
		"add_mode_passthru": "mode passthru",
		"add_bcqueuelen":    "bcqueuelen 2000",
		"add_mode_bad":      "mode bogus",
	} {
		out[slug] = stackedArgv("d0", "t0", kindMacvtapCst, w)
	}
	out["set_mode"] = stackedSetArgv("t0", kindMacvtapCst, "mode private")
	return out
}

// TestMacvlanCorpusArgv holds the argv tables to the corpus: one entry per
// row, 34 macvlan and 6 macvtap.
func TestMacvlanCorpusArgv(t *testing.T) {
	tests := []struct {
		description string
		dir         string
		argv        map[string][]string
		expected    int
	}{
		{"positive: every macvlan row has an argv", macvlanKindDir, macvlanCorpusArgv(), 34},
		{"positive: every macvtap row has an argv", macvtapKindDir, macvtapCorpusArgv(), 6},
	}
	for _, tc := range tests {
		t.Run(tc.description, func(t *testing.T) {
			slugs := kindRowSlugs(t, tc.dir)
			for _, s := range slugs {
				if _, ok := tc.argv[s]; !ok {
					t.Errorf("row %s has no argv", s)
				}
			}
			if len(tc.argv) != len(slugs) || len(slugs) != tc.expected {
				t.Errorf("%d argv entries for %d rows, want %d each", len(tc.argv), len(slugs), tc.expected)
			}
		})
	}
}

// TestLinkMacvlanWriteMatchesIp replays every KC3-T02 row that sent a
// request: of macvlan's, the 20 the kernel took and the 5 it refused; of
// macvtap's, its 5.
func TestLinkMacvlanWriteMatchesIp(t *testing.T) {
	useLLMap720(t)
	replayKindRows(t, macvlanKindDir, macvlanCorpusArgv(), map[string]string{
		"add_bclim_negative": "boundary: a negative cutoff",
		"add_macaddr_add":    "corner: the mode after the ops",
		"add_macaddr_set":    "corner: a set's addresses in one nest",
		"add_flag_null":      "corner: flag null",
		"add_default":        "corner: no words, no parse",
	}, 25)
	replayKindRows(t, macvtapKindDir, macvtapCorpusArgv(), map[string]string{
		"add_mode_passthru": "positive: macvtap is macvlan's parser",
	}, 5)
}

// TestLinkMacvlanRefusals is goip's status and first stderr line for each
// refusal macvlan_parse_opt makes. Each sends d0's get first.
func TestLinkMacvlanRefusals(t *testing.T) {
	useLLMap720(t)
	tests := corpusRefusals(t, macvlanKindDir, macvlanCorpusArgv(), "add_nopromisc_not_passthru", "add_mode_bad",
		"add_flag_bad", "add_macaddr_op_bad", "add_macaddr_bad", "add_bcqueuelen_bad", "add_bclim_bad",
		"add_bclim_abbrev", "add_no_value")
	tests = append(tests, corpusRefusals(t, macvtapKindDir, macvtapCorpusArgv(), "add_mode_bad")...)
	d0 := macvlanKindDir + "add_default/ip.pcap"
	m0 := func(kind, words string) []string { return stackedArgv("d0", "m0", kind, words) }
	tests = append(tests,
		kindRefusal{"negative: macvtap's usage names macvtap", m0(kindMacvtapCst, "macaddr bogus"),
			ExitUsage, "Usage: ... macvtap mode MODE [flag MODE_FLAG]", d0, []int{0}},
		kindRefusal{"boundary: a queue length that isn't a u32", m0(kindMacvlanCst, "bcqueuelen -1"),
			ExitUsage, `Error: argument of "bcqueuelen" must be a positive integer [0-4294967295], not "-1"`, d0, []int{0}},
		kindRefusal{"boundary: a cutoff past an s32", m0(kindMacvlanCst, "bclim 2147483648"),
			ExitUsage, `Error: illegal value for "bclim": "2147483648"`, d0, []int{0}},
		kindRefusal{"corner: an address one part short, which ip refuses silently", m0(kindMacvlanCst, "mode source macaddr add 02:00:00:00:01"),
			ExitUsage, `"02:00:00:00:01" is not a 6-byte lladdr`, d0, []int{0}},
		kindRefusal{"corner: a bad part, not the whole word", m0(kindMacvlanCst, "mode source macaddr add 02:zz:00:00:00:01"),
			ExitUsage, `"zz" is invalid lladdr`, d0, []int{0}},
		kindRefusal{"corner: a macaddr op with no address", m0(kindMacvlanCst, "macaddr del"),
			ExitMinusOne, "Command line is not complete", d0, []int{0}},
		kindRefusal{"corner: help is not goip's", m0(kindMacvlanCst, "help"),
			ExitUsage, "not implemented", d0, []int{0}},
	)
	runKindRefusals(t, tests)
}

// TestParseMacvlanArgs covers what no capture reaches, through the encoder
// and back, with what the parse printed.
func TestParseMacvlanArgs(t *testing.T) {
	type expected struct {
		mode     uint32
		flags    uint16
		hasFlags bool
		addrs    [][6]byte
		hasData  bool
		queueLen uint32
		stderr   string
		err      string
	}
	a1 := [6]byte{2, 0, 0, 0, 0, 1}
	tests := []struct {
		description string
		args        string
		expected    expected
	}{
		{"positive: nopromisc before the passthru mode, since the check follows the loop", "nopromisc mode passthru",
			expected{mode: xtcpnl.MacvlanModePassthru, flags: xtcpnl.MacvlanFlagNopromisc, hasFlags: true}},
		{"positive: n is nopromisc", "n mode passthru",
			expected{mode: xtcpnl.MacvlanModePassthru, flags: xtcpnl.MacvlanFlagNopromisc, hasFlags: true}},
		{"corner: nod is nodst", "nod", expected{flags: xtcpnl.MacvlanFlagNodst, hasFlags: true}},
		{"corner: a set ends at a word that isn't an address, with ll_addr_a2n's warning",
			"macaddr set " + macvlanMAC1 + " mode source",
			expected{mode: xtcpnl.MacvlanModeSource, addrs: [][6]byte{a1}, hasData: true, stderr: "\"mode\" is invalid lladdr.\n"}},
		{"corner: a set ends silently at a word that reads as hex", "macaddr set " + macvlanMAC1 + " bcqueuelen 10",
			expected{addrs: [][6]byte{a1}, hasData: true, queueLen: 10}},
		{"boundary: a set with no addresses", "macaddr set", expected{hasData: true}},
		{"corner: the word that ends a set goes back cut at its colon", "macaddr set " + macvlanMAC1 + " mode:x source",
			expected{mode: xtcpnl.MacvlanModeSource, addrs: [][6]byte{a1}, hasData: true, stderr: "\"mode\" is invalid lladdr.\n"}},
		{"negative: seven parts end a set silently, and the cut word is unknown", "macaddr set " + macvlanMAC1 + ":07",
			expected{err: `macvlan: unknown option "02"?`}},
	}
	for _, tc := range tests {
		t.Run(tc.description, func(t *testing.T) {
			var stderr bytes.Buffer
			settings, err := parseMacvlanArgs(&stderr, kindMacvlanCst, strings.Fields(tc.args))
			if tc.expected.err != "" {
				if err == nil || !strings.Contains(err.Error(), tc.expected.err) {
					t.Fatalf("error %v, want %q in it", err, tc.expected.err)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			b, err := xtcpnl.EncodeMacvlanInfoData(settings)
			if err != nil {
				t.Fatal(err)
			}
			m := xtcpnl.ParseMacvlanInfoData(b)
			got := expected{mode: m.Mode, flags: m.Flags, hasFlags: m.HasFlags, addrs: m.Macaddrs,
				hasData: m.HasMacaddrData, queueLen: m.BcQueueLen, stderr: stderr.String()}
			if got.mode != tc.expected.mode || got.flags != tc.expected.flags || got.hasFlags != tc.expected.hasFlags ||
				!slices.Equal(got.addrs, tc.expected.addrs) || got.hasData != tc.expected.hasData ||
				got.queueLen != tc.expected.queueLen || got.stderr != tc.expected.stderr {
				t.Errorf("got %+v, want %+v", got, tc.expected)
			}
		})
	}
	if _, err := parseMacvlanArgs(&bytes.Buffer{}, kindMacvlanCst, []string{"bclim"}); !errors.Is(err, ErrLinkUsage) {
		t.Errorf("bclim with no value: %v, want ErrLinkUsage", err)
	}
}

// TestLLAddrA2NPart checks ll_addr_a2n's return and what it prints.
func TestLLAddrA2NPart(t *testing.T) {
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
		{"negative: a part that isn't hex is named", "02:zz:00:00:00:01", expected{n: -1, bad: "zz"}},
		{"negative: a part past 255 is named", "02:100:00:00:00:01", expected{n: -1, bad: "100"}},
		{"negative: an IPv4 address that won't parse is named whole", "10.0.0.256", expected{n: -1, bad: "10.0.0.256"}},
		{"boundary: one part short is silent", "02:00:00:00:01", expected{addr: []byte{2, 0, 0, 0, 1}, n: 5}},
		{"boundary: seven parts are silent", macvlanMAC1 + ":07", expected{n: 7}},
		{"corner: a part past the sixth is never read", macvlanMAC1 + ":zz", expected{n: 7}},
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
