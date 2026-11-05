package goip

import (
	"strings"
	"testing"
)

// The hsr words (docs/netlink/goip-link-kinds.md, "KG5-T02 The hsr
// words"), against KC6's hsr rows.

// hsrCorpusArgv is each hsr row's argv after `ip link`, as
// kind-write-corpus.exp runs it.
func hsrCorpusArgv() map[string][]string {
	words := map[string]string{
		"add_abbrev":            "add hs0 type hsr sl d0 slave2 d1 s 7 v 1 p 0",
		"add_default":           "add hs0 type hsr slave1 d0 slave2 d1",
		"add_help":              "add hs0 type hsr help",
		"add_interlink":         "add hs0 type hsr slave1 d0 slave2 d1 interlink d2",
		"add_interlink_abbrev":  "add hs0 type hsr slave1 d0 slave2 d1 inter d2",
		"add_interlink_slave":   "add hs0 type hsr slave1 d0 slave2 d1 interlink d0",
		"add_no_option":         "add hs0 type hsr",
		"add_no_value":          "add hs0 type hsr slave1 d0 slave2 d1 supervision",
		"add_proto_2":           "add hs0 type hsr slave1 d0 slave2 d1 proto 2",
		"add_proto_prp":         "add hs0 type hsr slave1 d0 slave2 d1 proto 1",
		"add_protocol":          "add hs0 type hsr slave1 d0 slave2 d1 protocol 1",
		"add_prp_version":       "add hs0 type hsr slave1 d0 slave2 d1 proto 1 version 0",
		"add_same_slaves":       "add hs0 type hsr slave1 d0 slave2 d0",
		"add_slave1_only":       "add hs0 type hsr slave1 d0",
		"add_slave2_only":       "add hs0 type hsr slave2 d1",
		"add_slave_lo":          "add hs0 type hsr slave1 lo slave2 d1",
		"add_slave_missing":     "add hs0 type hsr slave1 nosuch slave2 d1",
		"add_supervision":       "add hs0 type hsr slave1 d0 slave2 d1 supervision 42",
		"add_supervision_bad":   "add hs0 type hsr slave1 d0 slave2 d1 supervision abc",
		"add_supervision_hex":   "add hs0 type hsr slave1 d0 slave2 d1 supervision 0xff",
		"add_supervision_max":   "add hs0 type hsr slave1 d0 slave2 d1 supervision 255",
		"add_supervision_over":  "add hs0 type hsr slave1 d0 slave2 d1 supervision 256",
		"add_supervision_twice": "add hs0 type hsr slave1 d0 slave2 d1 supervision 1 supervision 2",
		"add_unknown":           "add hs0 type hsr slave1 d0 slave2 d1 foo",
		"add_version_1":         "add hs0 type hsr slave1 d0 slave2 d1 version 1",
		"add_version_2":         "add hs0 type hsr slave1 d0 slave2 d1 version 2",
		"add_version_over":      "add hs0 type hsr slave1 d0 slave2 d1 version 256",
		"set_no_option":         "set hs0 type hsr",
		"set_option":            "set hs0 type hsr supervision 5",
	}
	out := make(map[string][]string, len(words))
	for slug, w := range words {
		out[slug] = strings.Fields(w)
	}
	return out
}

// TestHsrCorpusArgv holds the argv table to the corpus: one entry per row.
func TestHsrCorpusArgv(t *testing.T) {
	argv := hsrCorpusArgv()
	slugs := kindRowSlugs(t, noParserKindDir(kindHsrCst))
	for _, s := range slugs {
		if _, ok := argv[s]; !ok {
			t.Errorf("row %s has no argv", s)
		}
	}
	if len(argv) != len(slugs) || len(slugs) != 29 {
		t.Errorf("%d argv entries for %d rows, want 29 each", len(argv), len(slugs))
	}
}

// TestLinkHsrWriteMatchesIp replays every hsr row that sent a request: the
// 10 the kernel took and the 10 it refused.
func TestLinkHsrWriteMatchesIp(t *testing.T) {
	useLLMap720(t)
	named := map[string]string{
		"add_abbrev":            "positive: abbreviations, sl is slave1 and s supervision",
		"add_interlink":         "positive: the interlink, after both slaves' gets",
		"add_version_2":         "negative: version 2 is sent, the kernel refuses it",
		"add_proto_2":           "negative: proto 2 is sent, the kernel refuses it",
		"add_supervision_max":   "boundary: the top supervision byte",
		"add_supervision_hex":   "boundary: the top supervision byte in hex, base 0",
		"add_supervision_twice": "corner: a repeat sends twice",
		"add_no_option":         "corner: no word, no data",
		"set_no_option":         "corner: no word, no data on a set",
	}
	replayKindRows(t, noParserKindDir(kindHsrCst), hsrCorpusArgv(), named, 20)
}

// TestLinkHsrRefusals is goip's status, first stderr line and lookups for
// each hsr row `ip` refuses, and for built rows past them.
func TestLinkHsrRefusals(t *testing.T) {
	useLLMap720(t)
	dir := noParserKindDir(kindHsrCst)
	argv := hsrCorpusArgv()
	tests := corpusRefusals(t, dir, argv, "add_help", "add_interlink_abbrev", "add_protocol", "add_unknown",
		"add_no_value", "add_slave_missing", "add_supervision_bad", "add_supervision_over", "add_version_over")
	slaves := dir + "add_default/ip.pcap"
	nosuch := dir + "add_slave_missing/ip.pcap"
	hs0 := func(words string) []string { return strings.Fields("add hs0 type hsr " + words) }
	tests = append(tests,
		kindRefusal{"boundary: a version past a u8", hs0("slave1 d0 slave2 d1 version 256"),
			ExitMinusOne, `Error: argument "256" is wrong: version is invalid`, slaves, []int{0, 1}},
		kindRefusal{"boundary: proto past a u8", hs0("slave1 d0 slave2 d1 proto 256"),
			ExitMinusOne, `Error: argument "256" is wrong: protocol is invalid`, slaves, []int{0, 1}},
		kindRefusal{"boundary: a negative supervision byte", hs0("supervision -1"),
			ExitMinusOne, `Error: argument "-1" is wrong: ADDR-BYTE is invalid`, slaves, []int{}},
		kindRefusal{"corner: interlink is not a prefix word", hs0("inter d2"),
			ExitUsage, `hsr: what is "inter"?`, slaves, []int{}},
		kindRefusal{"corner: h is help", hs0("h"), ExitUsage, hsrUsageLine, slaves, []int{}},
		kindRefusal{"corner: a missing interlink", hs0("interlink nosuch"),
			ExitMinusOne, `Error: argument "nosuch" is wrong: No such interface`, nosuch, []int{0}},
	)
	runKindRefusals(t, tests)
}
