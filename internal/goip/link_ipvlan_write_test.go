//go:build goip_write

package goip

import (
	"encoding/binary"
	"errors"
	"strings"
	"testing"

	"github.com/randomizedcoder/xtcp2/pkg/xtcpnl"
	"golang.org/x/sys/unix"
)

// ipvlanCorpusArgv is each KC3-T03 ipvlan row's argv after `ip link`, keyed
// by slug.
func ipvlanCorpusArgv() map[string][]string {
	out := map[string][]string{}
	for slug, w := range map[string]string{
		"add_default":       "",
		"add_mode_l2":       "mode l2",
		"add_mode_l3":       "mode l3",
		"add_mode_l3s":      "mode l3s",
		"add_private":       "private",
		"add_vepa":          "vepa",
		"add_bridge":        "bridge",
		"add_mode_and_flag": "mode l2 vepa",
		"add_abbrev":        "mo l2",
		"add_on_busy_lower": "",
		"add_two_flags":     "private vepa",
		"add_mode_bad":      "mode l4",
		"add_unknown":       "bogus",
		"add_no_value":      "mode",
	} {
		out[slug] = stackedArgv("d0", "i0", kindIpvlanCst, w)
	}
	out["add_on_loopback"] = stackedArgv("lo", "i0", kindIpvlanCst, "")
	out["add_no_link"] = []string{"add", "name", "i0", "type", kindIpvlanCst}
	out["set_mode"] = stackedSetArgv("i0", kindIpvlanCst, "mode l2")
	out["set_flag"] = stackedSetArgv("i0", kindIpvlanCst, "vepa")
	return out
}

// ipvtapCorpusArgv is the same for the ipvtap rows, whose device is j0.
func ipvtapCorpusArgv() map[string][]string {
	return map[string][]string{
		"add_default":  stackedArgv("d0", "j0", kindIpvtapCst, ""),
		"add_mode_l2":  stackedArgv("d0", "j0", kindIpvtapCst, "mode l2"),
		"add_mode_bad": stackedArgv("d0", "j0", kindIpvtapCst, "mode l4"),
		"set_mode":     stackedSetArgv("j0", kindIpvtapCst, "mode l2"),
	}
}

// TestIpvlanCorpusArgv holds the argv tables to the corpus: one entry per
// row, 18 ipvlan and 4 ipvtap.
func TestIpvlanCorpusArgv(t *testing.T) {
	tests := []struct {
		description string
		dir         string
		argv        map[string][]string
		expected    int
	}{
		{"positive: every ipvlan row has an argv", ipvlanKindDir, ipvlanCorpusArgv(), 18},
		{"positive: every ipvtap row has an argv", ipvtapKindDir, ipvtapCorpusArgv(), 4},
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

// TestLinkIpvlanWriteMatchesIp replays every KC3-T03 row that sent a
// request: of ipvlan's, the 11 the kernel took and the 3 it refused; of
// ipvtap's, its 3.
func TestLinkIpvlanWriteMatchesIp(t *testing.T) {
	useLLMap720(t)
	replayKindRows(t, ipvlanKindDir, ipvlanCorpusArgv(), map[string]string{
		"add_default": "positive: no option, no data",
		"add_mode_l2": "boundary: the flags sent as 0",
		"set_mode":    "corner: a set of the mode clears the flags",
		"add_abbrev":  "corner: an abbreviation",
	}, 14)
	replayKindRows(t, ipvtapKindDir, ipvtapCorpusArgv(), map[string]string{
		"add_mode_l2": "positive: ipvtap is ipvlan's parser",
	}, 3)
}

// TestLinkIpvlanRefusals is goip's status and first stderr line for each
// refusal ipvlan_parse_opt makes. Each sends d0's get first.
func TestLinkIpvlanRefusals(t *testing.T) {
	useLLMap720(t)
	tests := corpusRefusals(t, ipvlanKindDir, ipvlanCorpusArgv(), "add_two_flags", "add_mode_bad", "add_unknown",
		"add_no_value")
	tests = append(tests, corpusRefusals(t, ipvtapKindDir, ipvtapCorpusArgv(), "add_mode_bad")...)
	d0 := ipvlanKindDir + "add_mode_l2/ip.pcap"
	i0 := func(kind, words string) []string { return stackedArgv("d0", "i0", kind, words) }
	tests = append(tests,
		kindRefusal{"negative: ipvtap's unknown option names ipvtap", i0(kindIpvtapCst, "bogus"),
			ExitUsage, `ipvtap: unknown option "bogus"?`, d0, []int{0}},
		kindRefusal{"boundary: a mode in another case", i0(kindIpvlanCst, "mode L2"),
			ExitUsage, `Error: argument of "mode" must be either "l2", "l3" or "l3s"`, d0, []int{0}},
		kindRefusal{"corner: bridge, then private", i0(kindIpvlanCst, "bridge private"),
			ExitUsage, `ipvlan: unknown option "private"?`, d0, []int{0}},
		kindRefusal{"corner: help is not goip's", i0(kindIpvlanCst, "help"),
			ExitUsage, "not implemented", d0, []int{0}},
	)
	runKindRefusals(t, tests)
}

// TestParseIpvlanArgs covers what no capture reaches: the attributes the
// words encode to, in wire order.
func TestParseIpvlanArgs(t *testing.T) {
	type attr struct{ atype, value uint16 }
	tests := []struct {
		description string
		args        string
		expected    []attr
	}{
		{"positive: two modes, both sent in order", "mode l2 mode l3",
			[]attr{{unix.IFLA_IPVLAN_MODE, 0}, {unix.IFLA_IPVLAN_MODE, 1}, {unix.IFLA_IPVLAN_FLAGS, 0}}},
		{"corner: b is bridge", "b", []attr{{unix.IFLA_IPVLAN_FLAGS, 0}}},
		{"corner: p is private", "p", []attr{{unix.IFLA_IPVLAN_FLAGS, xtcpnl.IpvlanFlagPrivate}}},
		{"corner: a flag before the mode goes after it", "vepa mode l3s",
			[]attr{{unix.IFLA_IPVLAN_MODE, 2}, {unix.IFLA_IPVLAN_FLAGS, xtcpnl.IpvlanFlagVepa}}},
		{"boundary: a mode after the flag word is still a mode", "private mode l3",
			[]attr{{unix.IFLA_IPVLAN_MODE, 1}, {unix.IFLA_IPVLAN_FLAGS, xtcpnl.IpvlanFlagPrivate}}},
	}
	for _, tc := range tests {
		t.Run(tc.description, func(t *testing.T) {
			settings, err := parseIpvlanArgs(kindIpvlanCst, strings.Fields(tc.args))
			if err != nil {
				t.Fatal(err)
			}
			b, err := xtcpnl.EncodeIpvlanInfoData(settings)
			if err != nil {
				t.Fatal(err)
			}
			var got []attr
			for len(b) >= 4 {
				n := int(binary.LittleEndian.Uint16(b))
				got = append(got, attr{binary.LittleEndian.Uint16(b[2:]), binary.LittleEndian.Uint16(b[4:])})
				b = b[min((n+3)&^3, len(b)):]
			}
			if len(got) != len(tc.expected) {
				t.Fatalf("got %v, want %v", got, tc.expected)
			}
			for i := range got {
				if got[i] != tc.expected[i] {
					t.Errorf("got %v, want %v", got, tc.expected)
				}
			}
		})
	}
	if _, err := parseIpvlanArgs(kindIpvlanCst, []string{"mode"}); !errors.Is(err, ErrLinkUsage) {
		t.Errorf("mode with no value: %v, want ErrLinkUsage", err)
	}
}
