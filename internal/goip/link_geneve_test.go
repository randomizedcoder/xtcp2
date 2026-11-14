package goip

import (
	"errors"
	"slices"
	"strings"
	"testing"

	"github.com/randomizedcoder/xtcp2/pkg/xtcpnl"
	"golang.org/x/sys/unix"
)

// The `type geneve` tests (docs/netlink/goip-link-kinds.md, "KG3-T02 The
// geneve words"), against KC4-T02's rows of the 7.3.0 kind corpus.

// geneveKindDir is KC4-T02's rows.
const geneveKindDir = "../../pkg/xtcpnl/testdata/7_3_0/link-write/kinds/geneve/"

// geneveBase is the corpus's $GENEVE_BASE.
const geneveBase = "id 1 remote 10.0.0.1 "

// geneveAddArgv is the corpus's `plain` proc: `add <dev> type geneve <words>`.
func geneveAddArgv(dev, words string) []string {
	return append([]string{"add", dev, "type", kindGeneveCst}, strings.Fields(words)...)
}

// geneveCorpusArgv is each KC4-T02 row's argv after `ip link`, keyed by slug.
func geneveCorpusArgv() map[string][]string {
	out := map[string][]string{}
	for slug, w := range map[string]string{
		"add_default":            geneveBase,
		"add_remote6":            "id 1 remote 2001:db8::1",
		"add_external":           "external",
		"add_id_max":             "id 16777215 remote 10.0.0.1",
		"add_abbrev":             "i 1 r 10.0.0.1",
		"add_local":              geneveBase + "local 10.0.0.2",
		"add_local6":             "id 1 remote 2001:db8::1 local 2001:db8::2",
		"add_external_local":     "external local 192.168.0.1",
		"add_external_local6":    "external local 2001:db8::1",
		"add_ttl":                geneveBase + "ttl 64",
		"add_ttl_inherit":        geneveBase + "ttl inherit",
		"add_tos":                geneveBase + "tos 10",
		"add_df":                 geneveBase + "df set",
		"add_dstport":            geneveBase + "dstport 4789",
		"add_dstport_zero":       geneveBase + "dstport 0",
		"add_flowlabel":          "id 1 remote 2001:db8::1 flowlabel 0xfffff",
		"add_zero_csum6":         "id 1 remote 2001:db8::1 udp6zerocsumtx udp6zerocsumrx",
		"add_csum_cancel":        geneveBase + "udpcsum udp6zerocsumtx",
		"add_innerprotoinherit":  geneveBase + "innerprotoinherit",
		"add_no_option":          "",
		"add_remote6_link_local": "id 1 remote fe80::1",
		"add_local6_link_local":  "id 1 remote 2001:db8::1 local fe80::2",
		"add_local_family_mix":   geneveBase + "local 2001:db8::2",
		"add_flowlabel_v4":       geneveBase + "label 5",
		"add_external_udpcsum":   "external udpcsum",
		"add_no_id":              "remote 10.0.0.1",
		"add_no_remote":          "id 1",
		"add_external_and_id":    "external id 5",
		"add_remote_bad":         "id 1 remote bogus",
		"add_local_abbrev":       geneveBase + "loc 10.0.0.2",
		"add_local6_word":        "id 1 remote 2001:db8::1 local6 2001:db8::2",
		"add_id_over":            "id 16777216 remote 10.0.0.1",
		"add_remote_multicast":   "id 1 remote 239.1.1.1",
		"add_local_multicast":    geneveBase + "local 224.0.0.1",
		"add_ttl_over":           geneveBase + "ttl 256",
		"add_flowlabel_over":     "id 1 remote 2001:db8::1 flowlabel 0x100000",
		"add_external_twice":     "external noexternal",
		"add_no_value":           "id",
	} {
		out[slug] = geneveAddArgv("gn0", w)
	}
	out["add_duplicate"] = geneveAddArgv("gn1", geneveBase)
	out["add_after_external"] = geneveAddArgv("gn1", geneveBase)
	for slug, w := range map[string]string{
		"set_changeable":  "id 1 ttl 5 tos 10 df set",
		"set_remote":      "id 1 remote 10.0.0.9",
		"set_local_same":  "id 1 local 10.0.0.2",
		"set_id":          "id 2",
		"set_dstport":     "id 1 dstport 6081",
		"set_family":      "id 1 remote 2001:db8::1",
		"set_local_new":   "id 1 local 10.0.0.2",
		"set_on_external": "id 0",
		"set_no_id":       "ttl 5",
	} {
		out[slug] = stackedSetArgv("gn0", kindGeneveCst, w)
	}
	return out
}

// TestGeneveCorpusArgv holds the argv table to the corpus: one entry per
// row, 49 in all.
func TestGeneveCorpusArgv(t *testing.T) {
	argv := geneveCorpusArgv()
	slugs := kindRowSlugs(t, geneveKindDir)
	for _, s := range slugs {
		if _, ok := argv[s]; !ok {
			t.Errorf("row %s has no argv", s)
		}
	}
	if len(argv) != len(slugs) || len(slugs) != 49 {
		t.Errorf("%d argv entries for %d rows, want 49 each", len(argv), len(slugs))
	}
}

// TestLinkGeneveWriteMatchesIp replays every KC4-T02 row that sent a
// request: the 22 the kernel took and the 13 it refused.
func TestLinkGeneveWriteMatchesIp(t *testing.T) {
	useLLMap720(t)
	replayKindRows(t, geneveKindDir, geneveCorpusArgv(), map[string]string{
		"add_default":     "positive: what every add sends",
		"add_local":       "positive: 7.2.0's local by family, v4",
		"add_local6":      "positive: 7.2.0's local by family, v6",
		"add_flowlabel":   "boundary: the widest flow label",
		"add_external":    "corner: external",
		"add_ttl_inherit": "corner: ttl inherit in the loop",
		"add_csum_cancel": "corner: the checksums last",
		"set_changeable":  "corner: a set sends what it's given",
		"add_abbrev":      "corner: abbreviations",
	}, 35)
}

// TestLinkGeneveRefusals is goip's status and first stderr line for each
// refusal geneve_parse_opt makes. None sends a get.
func TestLinkGeneveRefusals(t *testing.T) {
	useLLMap720(t)
	tests := corpusRefusals(t, geneveKindDir, geneveCorpusArgv(),
		"add_external_and_id", "add_local6_word", "add_local_abbrev", "add_no_id", "add_no_remote",
		"add_remote_bad", "set_no_id",
		"add_external_twice", "add_flowlabel_over", "add_id_over", "add_local_multicast", "add_no_value",
		"add_remote_multicast", "add_ttl_over")
	none := geneveKindDir + "add_default/ip.pcap"
	gn0 := func(words string) []string { return geneveAddArgv("gn0", words) }
	tests = append(tests,
		kindRefusal{"boundary: a dstport past a u16", gn0(geneveBase + "dstport 65536"),
			ExitMinusOne, `argument "65536" is wrong: dstport`, none, []int{}},
		kindRefusal{"corner: a df word that isn't one", gn0(geneveBase + "df yes"),
			ExitMinusOne, `argument "yes" is wrong: DF must be 'unset', 'set' or 'inherit'`, none, []int{}},
		kindRefusal{"corner: local twice", gn0(geneveBase + "local 10.0.0.2 local 10.0.0.3"),
			ExitMinusOne, `either "local" is duplicate, or "10.0.0.3" is garbage`, none, []int{}},
		kindRefusal{"corner: external's check comes first", gn0("id 1 external"),
			ExitUsage, "geneve: both 'external' and vni cannot be specified", none, []int{}},
		kindRefusal{"corner: help is not goip's", gn0("help"),
			ExitUsage, "not implemented", none, []int{}},
	)
	runKindRefusals(t, tests)
}

// TestParseGeneveArgs covers what no capture reaches: the abbreviations,
// the zero local, external on a set and tos inherit.
func TestParseGeneveArgs(t *testing.T) {
	id := func(v byte) []byte { return vxAttr(unix.IFLA_GENEVE_ID, v, 0, 0, 0) }
	remote := vxAttr(unix.IFLA_GENEVE_REMOTE, 10, 0, 0, 1)
	label := func(v byte) []byte { return vxAttr(unix.IFLA_GENEVE_LABEL, 0, 0, 0, v) }
	ttl := func(v byte) []byte { return vxAttr(unix.IFLA_GENEVE_TTL, v) }
	tos := func(v byte) []byte { return vxAttr(unix.IFLA_GENEVE_TOS, v) }
	zeros := slices.Concat(label(0), ttl(0), tos(0))
	tests := []struct {
		description string
		set         bool
		args        string
		expected    []byte
	}{
		{"positive: l is label", false, geneveBase + "l 5", slices.Concat(id(1), remote, label(5), ttl(0), tos(0))},
		{"positive: noudpcsum", false, geneveBase + "noudpcsum",
			slices.Concat(id(1), remote, zeros, vxAttr(unix.IFLA_GENEVE_UDP_CSUM, 0))},
		{"boundary: the zero local", false, geneveBase + "local 0.0.0.0",
			slices.Concat(id(1), remote, vxAttr(xtcpnl.IflaGeneveLocal, 0, 0, 0, 0), zeros)},
		{"corner: d is dsfield", false, geneveBase + "d 1", slices.Concat(id(1), remote, label(0), ttl(0), tos(1))},
		{"corner: external on a set", true, "external",
			slices.Concat(id(0), vxAttr(unix.IFLA_GENEVE_COLLECT_METADATA))},
		{"corner: tos inherit", false, geneveBase + "tos inherit", slices.Concat(id(1), remote, label(0), ttl(0), tos(1))},
		{"corner: ttl auto is 0", true, "id 1 ttl auto", slices.Concat(id(1), ttl(0))},
		{"corner: a v4-mapped multicast address is a remote", false, "id 1 remote ::ffff:239.1.1.1",
			slices.Concat(id(1), vxAttr(unix.IFLA_GENEVE_REMOTE6, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0xff, 0xff, 239, 1, 1, 1), zeros)},
	}
	for _, tc := range tests {
		t.Run(tc.description, func(t *testing.T) {
			settings, err := parseGeneveArgs(tc.set, strings.Fields(tc.args))
			if err != nil {
				t.Fatal(err)
			}
			got, err := xtcpnl.EncodeGeneveInfoData(settings, tc.set)
			if err != nil {
				t.Fatal(err)
			}
			if !slices.Equal(got, tc.expected) {
				t.Errorf("got  % x\nwant % x", got, tc.expected)
			}
		})
	}
}

// TestParseGeneveArgsErrors covers the refusals no table row reaches
// through runLink: the statuses' sentinels.
func TestParseGeneveArgsErrors(t *testing.T) {
	tests := []struct {
		description string
		set         bool
		args        string
		expected    error
	}{
		{"negative: a set without an id", true, "ttl 5", ErrInvalidArgument},
		{"negative: a remote with no value", false, "id 1 remote", ErrLinkUsage},
		{"boundary: ttl 255 is the top", false, geneveBase + "ttl 0x100", ErrLinkUsage},
		{"corner: any is no remote", false, "id 1 remote any", ErrLinkUsage},
		{"corner: a set needs no remote", true, "id 1", nil},
		{"corner: hoplimit is ttl's duplicate", false, geneveBase + "ttl 1 hoplimit 2", ErrLinkUsage},
	}
	for _, tc := range tests {
		t.Run(tc.description, func(t *testing.T) {
			_, err := parseGeneveArgs(tc.set, strings.Fields(tc.args))
			if !errors.Is(err, tc.expected) {
				t.Errorf("got %v, want %v", err, tc.expected)
			}
		})
	}
}
